package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunCommandWithoutStdioFailsClearly(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand(BuildInfo{})
	cmd.SetArgs([]string{"run", "--", "echo", "hello"})
	cmd.SetOut(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected unsupported non-stdio run mode error")
	}
	if !strings.Contains(err.Error(), "non-stdio command leases are not implemented yet") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunStdioValidatesIdleAfterBeforeStartingCommand(t *testing.T) {
	tests := []struct {
		name      string
		idleAfter string
		want      string
	}{
		{name: "zero", idleAfter: "0s", want: "idle-after must be greater than zero"},
		{name: "negative", idleAfter: "-1m", want: "idle-after must be greater than zero"},
		{name: "malformed", idleAfter: "soon", want: "parse idle-after"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runnerCalled := false
			executor := stdioLeaseExecutor{
				stdinIsTerminal: func(io.Reader) bool {
					return true
				},
				stdoutIsTerminal: func(io.Writer) bool {
					return true
				},
				runExecution: func(stdioLeaseExecution) error {
					runnerCalled = true
					return nil
				},
			}
			err := executor.Run(stdioLeaseExecutionRequest{
				Command:       []string{"echo"},
				IdleAfterText: tt.idleAfter,
				Stdin:         strings.NewReader(""),
				Stdout:        &bytes.Buffer{},
			})

			if err == nil {
				t.Fatal("expected idle-after validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("unexpected error: %v", err)
			}
			if runnerCalled {
				t.Fatal("stdio runner should not start after invalid idle-after")
			}
		})
	}
}

func TestRunStdioDefaultsAndAcceptsIdleAfter(t *testing.T) {
	tests := []struct {
		name      string
		idleAfter string
		want      time.Duration
	}{
		{name: "default", idleAfter: "", want: 30 * time.Minute},
		{name: "custom", idleAfter: "45m", want: 45 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got stdioLeaseExecution
			executor := stdioLeaseExecutor{
				stdinIsTerminal: func(io.Reader) bool {
					return true
				},
				stdoutIsTerminal: func(io.Writer) bool {
					return true
				},
				runExecution: func(req stdioLeaseExecution) error {
					got = req
					return nil
				},
			}
			err := runRunCommand(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, runOptions{
				stdio:         true,
				idleAfterText: tt.idleAfter,
			}, []string{"echo", "hello"}, runDeps{stdioExecutor: executor})

			if err != nil {
				t.Fatalf("run stdio command: %v", err)
			}
			if got.IdleAfter != tt.want {
				t.Fatalf("idle-after mismatch: want %s, got %s", tt.want, got.IdleAfter)
			}
			if strings.Join(got.Command, " ") != "echo hello" {
				t.Fatalf("command mismatch: %#v", got.Command)
			}
		})
	}
}

func TestRunStdioRejectsMissingCommandBeforeStarting(t *testing.T) {
	runnerCalled := false
	executor := stdioLeaseExecutor{
		stdinIsTerminal: func(io.Reader) bool {
			return true
		},
		stdoutIsTerminal: func(io.Writer) bool {
			return true
		},
		runExecution: func(stdioLeaseExecution) error {
			runnerCalled = true
			return nil
		},
	}
	err := executor.Run(stdioLeaseExecutionRequest{
		Stdin:  strings.NewReader(""),
		Stdout: &bytes.Buffer{},
	})

	if err == nil {
		t.Fatal("expected missing command error")
	}
	if !strings.Contains(err.Error(), "missing command after --") {
		t.Fatalf("unexpected error: %v", err)
	}
	if runnerCalled {
		t.Fatal("stdio runner should not start when command is missing")
	}
}

func TestRunStdioRequiresTerminalBackedStdinAndStdout(t *testing.T) {
	tests := []struct {
		name           string
		stdinTerminal  bool
		stdoutTerminal bool
		want           string
	}{
		{name: "stdin", stdinTerminal: false, stdoutTerminal: true, want: "terminal-backed stdin"},
		{name: "stdout", stdinTerminal: true, stdoutTerminal: false, want: "terminal-backed stdout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runnerCalled := false
			executor := stdioLeaseExecutor{
				stdinIsTerminal: func(io.Reader) bool {
					return tt.stdinTerminal
				},
				stdoutIsTerminal: func(io.Writer) bool {
					return tt.stdoutTerminal
				},
				runExecution: func(stdioLeaseExecution) error {
					runnerCalled = true
					return nil
				},
			}
			err := executor.Run(stdioLeaseExecutionRequest{
				Command: []string{"echo"},
				Stdin:   strings.NewReader(""),
				Stdout:  &bytes.Buffer{},
			})

			if err == nil {
				t.Fatal("expected terminal requirement error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("unexpected error: %v", err)
			}
			if runnerCalled {
				t.Fatal("stdio runner should not start without terminal-backed stdio")
			}
		})
	}
}

func TestRunStdioCommandProxiesChildOutputThroughPTY(t *testing.T) {
	skipPTYIntegrationIfUnsupported(t)

	var out bytes.Buffer
	err := runTestStdioLeaseExecution(t, stdioLeaseExecutionRequest{
		Command: []string{"sh", "-c", "printf stdout; printf stderr >&2"},
		Stdin:   strings.NewReader(""),
		Stdout:  &out,
	})

	if err != nil {
		t.Fatalf("run stdio command: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "stdout") || !strings.Contains(got, "stderr") {
		t.Fatalf("expected stdout and stderr through PTY, got %q", got)
	}
}

func TestRunStdioCommandDrainsPTYOutputAfterChildExit(t *testing.T) {
	skipPTYIntegrationIfUnsupported(t)

	continuePath := filepath.Join(t.TempDir(), "continue")
	out := newBlockingOutputWriter()
	defer out.Release()
	done := make(chan error, 1)
	go func() {
		done <- runTestStdioLeaseExecution(t, stdioLeaseExecutionRequest{
			Command:       []string{"sh", "-c", "printf stdout; while [ ! -f \"$1\" ]; do sleep 0.01; done; printf stderr >&2", "sh", continuePath},
			IdleAfterText: time.Second.String(),
			Stdin:         strings.NewReader(""),
			Stdout:        out,
		})
	}()

	select {
	case <-out.firstWrite:
	case err := <-done:
		t.Fatalf("stdio command exited before first output write: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first output write")
	}

	if err := os.WriteFile(continuePath, []byte("continue"), 0o644); err != nil {
		t.Fatalf("release child command: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	out.Release()

	if err := <-done; err != nil {
		t.Fatalf("run stdio command: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "stdout") || !strings.Contains(got, "stderr") {
		t.Fatalf("expected PTY output to drain after child exit, got %q", got)
	}
}

func TestRunStdioCommandProxiesInputThroughPTY(t *testing.T) {
	skipPTYIntegrationIfUnsupported(t)

	var out bytes.Buffer
	err := runTestStdioLeaseExecution(t, stdioLeaseExecutionRequest{
		Command: []string{"sh", "-c", "stty -echo; IFS= read -r line; printf 'got:%s' \"$line\""},
		Stdin:   strings.NewReader("hello\n"),
		Stdout:  &out,
	})

	if err != nil {
		t.Fatalf("run stdio command: %v", err)
	}
	if !strings.Contains(out.String(), "got:hello") {
		t.Fatalf("expected child to receive stdin through PTY, got %q", out.String())
	}
}

func TestRunStdioCommandReturnsWrappedExitStatus(t *testing.T) {
	skipPTYIntegrationIfUnsupported(t)

	err := runTestStdioLeaseExecution(t, stdioLeaseExecutionRequest{
		Command: []string{"sh", "-c", "exit 7"},
		Stdin:   strings.NewReader(""),
		Stdout:  &bytes.Buffer{},
	})

	code, ok := CommandExitCode(err)
	if !ok {
		t.Fatalf("expected command exit error, got %v", err)
	}
	if code != 7 {
		t.Fatalf("exit status mismatch: want 7, got %d", code)
	}
}

func TestRunStdioCommandMapsSignalExitStatus(t *testing.T) {
	skipPTYIntegrationIfUnsupported(t)

	err := runTestStdioLeaseExecution(t, stdioLeaseExecutionRequest{
		Command: []string{"sh", "-c", "kill -TERM $$"},
		Stdin:   strings.NewReader(""),
		Stdout:  &bytes.Buffer{},
	})

	code, ok := CommandExitCode(err)
	if !ok {
		t.Fatalf("expected command exit error, got %v", err)
	}
	if code != 143 {
		t.Fatalf("signal exit status mismatch: want 143, got %d", code)
	}
}

func TestRunStdioCommandCreatesStdioLeaseThenRemovesOnExit(t *testing.T) {
	skipPTYIntegrationIfUnsupported(t)

	leaseDir := t.TempDir()
	t.Setenv("MYN_LEASE_DIR", leaseDir)
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- runTestStdioLeaseExecution(t, stdioLeaseExecutionRequest{
			Command:       []string{"sh", "-c", "printf ready; sleep 0.4"},
			IdleAfterText: (5 * time.Second).String(),
			Stdin:         strings.NewReader(""),
			Stdout:        &out,
		})
	}()

	leasePath := waitForSingleLeaseFile(t, leaseDir, done)
	if filepath.Ext(leasePath) != ".json" {
		t.Fatalf("lease path should use .json extension: %s", leasePath)
	}
	info, err := os.Stat(leasePath)
	if err != nil {
		t.Fatalf("stat lease file: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o644); got != want {
		t.Fatalf("lease file permissions mismatch: want %s, got %s", want, got)
	}

	data, err := os.ReadFile(leasePath)
	if err != nil {
		t.Fatalf("read lease file: %v", err)
	}
	var lease idleLease
	if err := json.Unmarshal(data, &lease); err != nil {
		t.Fatalf("decode lease JSON %s: %v", data, err)
	}
	if lease.ID == "" || filepath.Base(leasePath) != lease.ID+".json" {
		t.Fatalf("lease ID should match file name, id=%q path=%s", lease.ID, leasePath)
	}
	if lease.Kind != "stdio" {
		t.Fatalf("lease kind mismatch: %q", lease.Kind)
	}
	if lease.RootPID <= 0 {
		t.Fatalf("lease rootPid should be recorded, got %d", lease.RootPID)
	}
	if lease.ProcessGroup <= 0 {
		t.Fatalf("lease processGroup should be recorded, got %d", lease.ProcessGroup)
	}
	if lease.User == "" {
		t.Fatal("lease user should be recorded")
	}
	if lease.WorkingDirectory != workingDirectory {
		t.Fatalf("working directory mismatch: want %q, got %q", workingDirectory, lease.WorkingDirectory)
	}
	if lease.Command != "sh" {
		t.Fatalf("command should contain only argv[0], got %q", lease.Command)
	}
	if bytes.Contains(data, []byte("printf ready")) || bytes.Contains(data, []byte("sleep")) {
		t.Fatalf("lease file should not contain full command arguments: %s", data)
	}
	if !lease.Interactive {
		t.Fatal("stdio lease should be interactive")
	}
	if lease.StartedAt.IsZero() || lease.UpdatedAt.IsZero() || lease.ExpiresAt.IsZero() {
		t.Fatalf("lease should record startedAt, updatedAt, and expiresAt: %#v", lease)
	}
	if time.Duration(lease.IdleAfter) != 5*time.Second {
		t.Fatalf("idleAfter mismatch: want 5s, got %s", time.Duration(lease.IdleAfter))
	}
	if !lease.ExpiresAt.After(lease.StartedAt.Add(5 * time.Second)) {
		t.Fatalf("expiresAt should be a crash-safety TTL, got startedAt=%s expiresAt=%s", lease.StartedAt, lease.ExpiresAt)
	}

	if err := <-done; err != nil {
		t.Fatalf("run stdio command: %v", err)
	}
	waitForNoLeaseFiles(t, leaseDir)
	if !strings.Contains(out.String(), "ready") {
		t.Fatalf("expected child output through PTY, got %q", out.String())
	}
}

func TestRunStdioCommandRefreshesLeaseHeartbeat(t *testing.T) {
	skipPTYIntegrationIfUnsupported(t)

	leaseDir := t.TempDir()
	t.Setenv("MYN_LEASE_DIR", leaseDir)

	done := make(chan error, 1)
	go func() {
		done <- runTestStdioLeaseExecution(t, stdioLeaseExecutionRequest{
			Command:       []string{"sh", "-c", "sleep 0.8"},
			IdleAfterText: (300 * time.Millisecond).String(),
			Stdin:         strings.NewReader(""),
			Stdout:        &bytes.Buffer{},
		})
	}()

	leasePath := waitForSingleLeaseFile(t, leaseDir, done)
	initial := readStdioLeaseFile(t, leasePath)
	updated := waitForLeaseHeartbeatAfter(t, leasePath, initial.UpdatedAt, done)

	if !updated.UpdatedAt.After(initial.UpdatedAt) {
		t.Fatalf("updatedAt should advance, initial=%s updated=%s", initial.UpdatedAt, updated.UpdatedAt)
	}
	if !updated.ExpiresAt.After(initial.ExpiresAt) {
		t.Fatalf("expiresAt should refresh with heartbeat, initial=%s updated=%s", initial.ExpiresAt, updated.ExpiresAt)
	}

	if err := <-done; err != nil {
		t.Fatalf("run stdio command: %v", err)
	}
	waitForNoLeaseFiles(t, leaseDir)
}

func TestRunStdioCommandRecordsOutputActivityAsActiveLease(t *testing.T) {
	skipPTYIntegrationIfUnsupported(t)

	leaseDir := t.TempDir()
	t.Setenv("MYN_LEASE_DIR", leaseDir)

	done := make(chan error, 1)
	go func() {
		done <- runTestStdioLeaseExecution(t, stdioLeaseExecutionRequest{
			Command:       []string{"sh", "-c", "printf output-activity; sleep 1"},
			IdleAfterText: (5 * time.Second).String(),
			Stdin:         strings.NewReader(""),
			Stdout:        &bytes.Buffer{},
		})
	}()

	leasePath := waitForSingleLeaseFile(t, leaseDir, done)
	lease := waitForLeaseOutputActivity(t, leasePath, done)
	if lease.LastInputAt != nil {
		t.Fatalf("output-only activity should not update lastInputAt: %#v", lease.LastInputAt)
	}
	if lease.LastOutputAt == nil {
		t.Fatal("output activity should update lastOutputAt")
	}
	if lease.UpdatedAt.IsZero() {
		t.Fatal("activity flush should keep heartbeat updated")
	}

	var status bytes.Buffer
	if err := runIdleStatus(&status, idleStatusOptions{json: true}, idleStatusDeps{env: os.Getenv}); err != nil {
		t.Fatalf("run idle status: %v", err)
	}
	report := decodeIdleStatusReport(t, status.Bytes())
	reported := assertReportedLease(t, report, lease.ID, "active", "sh", lease.WorkingDirectory)
	if reported.LastOutputAt == nil {
		t.Fatal("status JSON should include lastOutputAt")
	}
	if reported.LastInputAt != nil {
		t.Fatalf("status JSON should not invent input activity: %#v", reported.LastInputAt)
	}

	if err := <-done; err != nil {
		t.Fatalf("run stdio command: %v", err)
	}
	waitForNoLeaseFiles(t, leaseDir)
}

func TestRunStdioCommandRecordsInputActivityAsActiveLease(t *testing.T) {
	skipPTYIntegrationIfUnsupported(t)

	leaseDir := t.TempDir()
	t.Setenv("MYN_LEASE_DIR", leaseDir)
	readyPath := filepath.Join(t.TempDir(), "ready")
	stdin, stdinWriter := io.Pipe()

	done := make(chan error, 1)
	go func() {
		done <- runTestStdioLeaseExecution(t, stdioLeaseExecutionRequest{
			Command:       []string{"sh", "-c", "stty -echo; : > \"$1\"; IFS= read -r line; sleep 1", "sh", readyPath},
			IdleAfterText: (5 * time.Second).String(),
			Stdin:         stdin,
			Stdout:        &bytes.Buffer{},
		})
	}()

	leasePath := waitForSingleLeaseFile(t, leaseDir, done)
	waitForFile(t, readyPath, done)
	if _, err := stdinWriter.Write([]byte("input-activity\n")); err != nil {
		t.Fatalf("write test stdin: %v", err)
	}
	if err := stdinWriter.Close(); err != nil {
		t.Fatalf("close test stdin: %v", err)
	}
	lease := waitForLeaseInputActivity(t, leasePath, done)
	if lease.LastInputAt == nil {
		t.Fatal("input activity should update lastInputAt")
	}
	if lease.LastOutputAt != nil {
		t.Fatalf("input-only activity should not update lastOutputAt: %#v", lease.LastOutputAt)
	}

	var status bytes.Buffer
	if err := runIdleStatus(&status, idleStatusOptions{json: true}, idleStatusDeps{env: os.Getenv}); err != nil {
		t.Fatalf("run idle status: %v", err)
	}
	report := decodeIdleStatusReport(t, status.Bytes())
	reported := assertReportedLease(t, report, lease.ID, "active", "sh", lease.WorkingDirectory)
	if reported.LastInputAt == nil {
		t.Fatal("status JSON should include lastInputAt")
	}
	if reported.LastOutputAt != nil {
		t.Fatalf("status JSON should not invent output activity: %#v", reported.LastOutputAt)
	}

	if err := <-done; err != nil {
		t.Fatalf("run stdio command: %v", err)
	}
	waitForNoLeaseFiles(t, leaseDir)
}

func TestRunStdioCommandReportsQuietLeaseIdleAfterIdleWindow(t *testing.T) {
	skipPTYIntegrationIfUnsupported(t)

	leaseDir := t.TempDir()
	t.Setenv("MYN_LEASE_DIR", leaseDir)

	done := make(chan error, 1)
	go func() {
		done <- runTestStdioLeaseExecution(t, stdioLeaseExecutionRequest{
			Command:       []string{"sh", "-c", "printf once; sleep 1"},
			IdleAfterText: (200 * time.Millisecond).String(),
			Stdin:         strings.NewReader(""),
			Stdout:        &bytes.Buffer{},
		})
	}()

	leasePath := waitForSingleLeaseFile(t, leaseDir, done)
	lease := waitForLeaseOutputActivity(t, leasePath, done)
	waitUntilAfter(t, lease.LastOutputAt.Add(250*time.Millisecond), done)

	var jsonStatus bytes.Buffer
	if err := runIdleStatus(&jsonStatus, idleStatusOptions{json: true}, idleStatusDeps{env: os.Getenv}); err != nil {
		t.Fatalf("run JSON idle status: %v", err)
	}
	report := decodeIdleStatusReport(t, jsonStatus.Bytes())
	assertReportedLease(t, report, lease.ID, "idle", "sh", lease.WorkingDirectory)

	var humanStatus bytes.Buffer
	if err := runIdleStatus(&humanStatus, idleStatusOptions{}, idleStatusDeps{env: os.Getenv}); err != nil {
		t.Fatalf("run human idle status: %v", err)
	}
	assertContains(t, humanStatus.String(), "Idle leases: 0 active, 1 idle, 0 stale (1 total)")
	assertContains(t, humanStatus.String(), "- "+lease.ID+" [stdio] idle: command=sh cwd="+lease.WorkingDirectory+" reason=terminal activity older than idle window")

	if err := <-done; err != nil {
		t.Fatalf("run stdio command: %v", err)
	}
	waitForNoLeaseFiles(t, leaseDir)
}

func skipPTYIntegrationIfUnsupported(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("PTY integration is unsupported on Windows")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is required for PTY integration tests")
	}
	t.Setenv("MYN_LEASE_DIR", t.TempDir())
}

func runTestStdioLeaseExecution(t *testing.T, req stdioLeaseExecutionRequest) error {
	t.Helper()
	return stdioLeaseExecutor{
		stdinIsTerminal: func(io.Reader) bool {
			return true
		},
		stdoutIsTerminal: func(io.Writer) bool {
			return true
		},
	}.Run(req)
}

func readStdioLeaseFile(t *testing.T, path string) idleLease {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read lease file: %v", err)
	}
	var lease idleLease
	if err := json.Unmarshal(data, &lease); err != nil {
		t.Fatalf("decode lease JSON %s: %v", data, err)
	}
	return lease
}

func waitForSingleLeaseFile(t *testing.T, dir string, done <-chan error) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("stdio command exited before lease file appeared: %v", err)
		default:
		}

		matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil {
			t.Fatalf("glob lease files: %v", err)
		}
		if len(matches) == 1 {
			return matches[0]
		}
		if len(matches) > 1 {
			t.Fatalf("expected one lease file, got %v", matches)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for lease file in %s", dir)
	return ""
}

func waitForLeaseHeartbeatAfter(t *testing.T, path string, previous time.Time, done <-chan error) idleLease {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("stdio command exited before heartbeat refreshed: %v", err)
		default:
		}

		lease := readStdioLeaseFile(t, path)
		if lease.UpdatedAt.After(previous) {
			return lease
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for heartbeat update in %s", path)
	return idleLease{}
}

func waitForLeaseOutputActivity(t *testing.T, path string, done <-chan error) idleLease {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("stdio command exited before output activity was recorded: %v", err)
		default:
		}

		lease := readStdioLeaseFile(t, path)
		if lease.LastOutputAt != nil {
			return lease
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for output activity in %s", path)
	return idleLease{}
}

func waitForLeaseInputActivity(t *testing.T, path string, done <-chan error) idleLease {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("stdio command exited before input activity was recorded: %v", err)
		default:
		}

		lease := readStdioLeaseFile(t, path)
		if lease.LastInputAt != nil {
			return lease
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for input activity in %s", path)
	return idleLease{}
}

func waitForFile(t *testing.T, path string, done <-chan error) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("stdio command exited before %s appeared: %v", path, err)
		default:
		}

		if _, err := os.Stat(path); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func waitUntilAfter(t *testing.T, when time.Time, done <-chan error) {
	t.Helper()
	timer := time.NewTimer(time.Until(when))
	defer timer.Stop()
	select {
	case err := <-done:
		t.Fatalf("stdio command exited before %s: %v", when, err)
	case <-timer.C:
	}
}

func waitForNoLeaseFiles(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil {
			t.Fatalf("glob lease files: %v", err)
		}
		if len(matches) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatalf("glob lease files: %v", err)
	}
	t.Fatalf("timed out waiting for lease cleanup, still found %v", matches)
}

type blockingOutputWriter struct {
	mu          sync.Mutex
	buf         bytes.Buffer
	firstWrite  chan struct{}
	release     chan struct{}
	once        sync.Once
	releaseOnce sync.Once
}

func newBlockingOutputWriter() *blockingOutputWriter {
	return &blockingOutputWriter{
		firstWrite: make(chan struct{}),
		release:    make(chan struct{}),
	}
}

func (w *blockingOutputWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	n, err := w.buf.Write(p)
	w.mu.Unlock()

	w.once.Do(func() {
		close(w.firstWrite)
		<-w.release
	})
	return n, err
}

func (w *blockingOutputWriter) Release() {
	w.releaseOnce.Do(func() {
		close(w.release)
	})
}

func (w *blockingOutputWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}
