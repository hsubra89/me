//go:build unix

package cli

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
)

func TestRunStdioCommandPassesCtrlCThroughPTY(t *testing.T) {
	skipPTYIntegrationIfUnsupported(t)

	userPty, userTTY, err := pty.Open()
	if err != nil {
		t.Fatalf("open user PTY: %v", err)
	}
	defer func() { _ = userPty.Close() }()
	defer func() { _ = userTTY.Close() }()
	leaseDir := os.Getenv("ME_LEASE_DIR")

	out := &lockedBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- runStdioCommand(stdioRunRequest{
			Command:   []string{"sh", "-c", "trap 'printf got-int; exit 42' INT; printf ready; sleep 2; printf no-int"},
			IdleAfter: time.Second,
			Stdin:     userTTY,
			Stdout:    out,
			Stderr:    &bytes.Buffer{},
		})
	}()

	waitForBufferedOutput(t, out, "ready", done)
	if _, err := userPty.Write([]byte{3}); err != nil {
		t.Fatalf("write Ctrl-C to user PTY: %v", err)
	}

	err = <-done
	if !strings.Contains(out.String(), "got-int") {
		t.Fatalf("expected child trap to handle Ctrl-C, got output %q and error %v", out.String(), err)
	}
	code, ok := CommandExitCode(err)
	if !ok {
		t.Fatalf("expected wrapped command exit status, got %v", err)
	}
	if code != 42 {
		t.Fatalf("exit status mismatch after Ctrl-C: want 42, got %d", code)
	}
	waitForNoLeaseFiles(t, leaseDir)
}

func TestRunStdioCommandRestoresTerminalModeAfterChildExit(t *testing.T) {
	skipPTYIntegrationIfUnsupported(t)

	tests := []struct {
		name     string
		command  []string
		wantCode int
	}{
		{name: "normal exit", command: []string{"sh", "-c", "exit 0"}, wantCode: 0},
		{name: "child failure", command: []string{"sh", "-c", "exit 7"}, wantCode: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userPty, userTTY, err := pty.Open()
			if err != nil {
				t.Fatalf("open user PTY: %v", err)
			}
			defer func() { _ = userPty.Close() }()
			defer func() { _ = userTTY.Close() }()

			before, err := term.GetState(userTTY.Fd())
			if err != nil {
				t.Fatalf("get terminal state before run: %v", err)
			}

			err = runStdioCommand(stdioRunRequest{
				Command:   tt.command,
				IdleAfter: time.Second,
				Stdin:     userTTY,
				Stdout:    &bytes.Buffer{},
				Stderr:    &bytes.Buffer{},
			})
			if tt.wantCode == 0 && err != nil {
				t.Fatalf("run stdio command: %v", err)
			}
			if tt.wantCode != 0 {
				if code, ok := CommandExitCode(err); !ok || code != tt.wantCode {
					t.Fatalf("expected child exit status %d, got code=%d ok=%t err=%v", tt.wantCode, code, ok, err)
				}
			}

			after, err := term.GetState(userTTY.Fd())
			if err != nil {
				t.Fatalf("get terminal state after run: %v", err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("terminal state was not restored after child exit")
			}
		})
	}
}

func TestStdioResizeForwardingCopiesInitialAndChangedTerminalSize(t *testing.T) {
	sourcePty, sourceTTY, err := pty.Open()
	if err != nil {
		t.Fatalf("open source PTY: %v", err)
	}
	defer func() { _ = sourcePty.Close() }()
	defer func() { _ = sourceTTY.Close() }()

	targetPty, targetTTY, err := pty.Open()
	if err != nil {
		t.Fatalf("open target PTY: %v", err)
	}
	defer func() { _ = targetPty.Close() }()
	defer func() { _ = targetTTY.Close() }()

	setPTYSize(t, sourceTTY, 24, 80)
	events := make(chan os.Signal, 1)
	stopCalled := false
	stop, err := startStdioResizeForwardingWithEvents(sourceTTY, targetPty, events, func() {
		stopCalled = true
	})
	if err != nil {
		t.Fatalf("start resize forwarding: %v", err)
	}
	defer stop()
	assertPTYSize(t, targetPty, 24, 80)

	setPTYSize(t, sourceTTY, 40, 120)
	events <- os.Interrupt
	waitForPTYSize(t, targetPty, 40, 120)

	stop()
	if !stopCalled {
		t.Fatal("resize forwarding should stop signal notification")
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func waitForBufferedOutput(t *testing.T, out *lockedBuffer, want string, done <-chan error) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("stdio command exited before output %q appeared: output=%q err=%v", want, out.String(), err)
		default:
		}

		if strings.Contains(out.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for output %q, got %q", want, out.String())
}

func setPTYSize(t *testing.T, tty *os.File, rows uint16, cols uint16) {
	t.Helper()
	if err := pty.Setsize(tty, &pty.Winsize{Rows: rows, Cols: cols}); err != nil {
		t.Fatalf("set PTY size to %dx%d: %v", rows, cols, err)
	}
}

func assertPTYSize(t *testing.T, tty *os.File, rows uint16, cols uint16) {
	t.Helper()
	size, err := pty.GetsizeFull(tty)
	if err != nil {
		t.Fatalf("get PTY size: %v", err)
	}
	if size.Rows != rows || size.Cols != cols {
		t.Fatalf("PTY size mismatch: want %dx%d, got %dx%d", rows, cols, size.Rows, size.Cols)
	}
}

func waitForPTYSize(t *testing.T, tty *os.File, rows uint16, cols uint16) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		size, err := pty.GetsizeFull(tty)
		if err != nil {
			t.Fatalf("get PTY size: %v", err)
		}
		if size.Rows == rows && size.Cols == cols {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	assertPTYSize(t, tty, rows, cols)
}
