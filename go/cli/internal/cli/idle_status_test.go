package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIdleStatusJSONReportsLeaseStatesFromLeaseDirectory(t *testing.T) {
	leaseDir := t.TempDir()
	now := time.Now().UTC()

	writeTestLease(t, leaseDir, "active", map[string]any{
		"kind":             "stdio",
		"id":               "active",
		"rootPid":          os.Getpid(),
		"processGroup":     os.Getpid(),
		"user":             "harish",
		"workingDirectory": "/home/harish/projects/me",
		"command":          "codex",
		"interactive":      true,
		"startedAt":        now.Add(-5 * time.Minute).Format(time.RFC3339Nano),
		"updatedAt":        now.Add(-10 * time.Second).Format(time.RFC3339Nano),
		"lastInputAt":      now.Add(-10 * time.Second).Format(time.RFC3339Nano),
		"lastOutputAt":     now.Add(-2 * time.Minute).Format(time.RFC3339Nano),
		"idleAfter":        "30m",
		"expiresAt":        now.Add(30 * time.Minute).Format(time.RFC3339Nano),
	})
	writeTestLease(t, leaseDir, "idle", map[string]any{
		"kind":             "stdio",
		"id":               "idle",
		"rootPid":          os.Getpid(),
		"processGroup":     os.Getpid(),
		"user":             "harish",
		"workingDirectory": "/home/harish/projects/me",
		"command":          "bash",
		"interactive":      true,
		"startedAt":        now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
		"updatedAt":        now.Add(-10 * time.Second).Format(time.RFC3339Nano),
		"lastInputAt":      now.Add(-45 * time.Minute).Format(time.RFC3339Nano),
		"lastOutputAt":     now.Add(-40 * time.Minute).Format(time.RFC3339Nano),
		"idleAfter":        "30m",
		"expiresAt":        now.Add(30 * time.Minute).Format(time.RFC3339Nano),
	})
	writeTestLease(t, leaseDir, "stale", map[string]any{
		"kind":             "stdio",
		"id":               "stale",
		"rootPid":          99999999,
		"processGroup":     99999999,
		"user":             "harish",
		"workingDirectory": "/tmp",
		"command":          "claude",
		"interactive":      true,
		"startedAt":        now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
		"updatedAt":        now.Add(-10 * time.Second).Format(time.RFC3339Nano),
		"lastInputAt":      now.Add(-10 * time.Second).Format(time.RFC3339Nano),
		"idleAfter":        "30m",
		"expiresAt":        now.Add(30 * time.Minute).Format(time.RFC3339Nano),
	})
	if err := os.WriteFile(filepath.Join(leaseDir, "malformed.json"), []byte("{"), 0o644); err != nil {
		t.Fatalf("write malformed lease: %v", err)
	}
	if err := os.WriteFile(filepath.Join(leaseDir, "ignored.tmp"), []byte("{"), 0o644); err != nil {
		t.Fatalf("write ignored temp file: %v", err)
	}

	t.Setenv("ME_LEASE_DIR", leaseDir)
	var out bytes.Buffer
	cmd := NewRootCommand(BuildInfo{})
	cmd.SetArgs([]string{"idle", "status", "--json"})
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute idle status: %v", err)
	}

	report := decodeIdleStatusReport(t, out.Bytes())
	if got, want := report.LeaseDirectory, leaseDir; got != want {
		t.Fatalf("lease directory mismatch: want %q, got %q", want, got)
	}
	if got, want := report.Counts.Active, 1; got != want {
		t.Fatalf("active count mismatch: want %d, got %d", want, got)
	}
	if got, want := report.Counts.Idle, 1; got != want {
		t.Fatalf("idle count mismatch: want %d, got %d", want, got)
	}
	if got, want := report.Counts.Stale, 2; got != want {
		t.Fatalf("stale count mismatch: want %d, got %d", want, got)
	}
	if _, ok := report.Leases["ignored"]; ok {
		t.Fatal("non-json temp file should be ignored")
	}
	assertReportedLease(t, report, "active", "active", "codex", "/home/harish/projects/me")
	assertReportedLease(t, report, "idle", "idle", "bash", "/home/harish/projects/me")
	assertReportedLease(t, report, "stale", "stale", "claude", "/tmp")
	malformed := assertReportedLease(t, report, "malformed", "stale", "", "")
	if !strings.Contains(malformed.Reason, "malformed lease JSON") {
		t.Fatalf("malformed reason mismatch: %q", malformed.Reason)
	}
}

func TestIdleStatusHumanReportsEmptyLeaseDirectory(t *testing.T) {
	leaseDir := t.TempDir()

	t.Setenv("ME_LEASE_DIR", leaseDir)
	var out bytes.Buffer
	cmd := NewRootCommand(BuildInfo{})
	cmd.SetArgs([]string{"idle", "status"})
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute idle status: %v", err)
	}

	got := out.String()
	assertContains(t, got, "Idle leases: 0 active, 0 idle, 0 stale (0 total)")
	assertContains(t, got, "Lease directory: "+leaseDir)
	assertContains(t, got, "No idle lease files found.")
}

func TestIdleStatusHumanReportsLeaseDetailsReadOnly(t *testing.T) {
	leaseDir := t.TempDir()
	now := time.Now().UTC()

	writeTestLease(t, leaseDir, "active", map[string]any{
		"kind":             "stdio",
		"id":               "active",
		"rootPid":          os.Getpid(),
		"processGroup":     os.Getpid(),
		"user":             "harish",
		"workingDirectory": "/home/harish/projects/me",
		"command":          "codex",
		"interactive":      true,
		"startedAt":        now.Add(-5 * time.Minute).Format(time.RFC3339Nano),
		"updatedAt":        now.Add(-10 * time.Second).Format(time.RFC3339Nano),
		"lastInputAt":      now.Add(-10 * time.Second).Format(time.RFC3339Nano),
		"lastOutputAt":     now.Add(-2 * time.Minute).Format(time.RFC3339Nano),
		"idleAfter":        "30m",
		"expiresAt":        now.Add(30 * time.Minute).Format(time.RFC3339Nano),
	})
	writeTestLease(t, leaseDir, "idle", map[string]any{
		"kind":             "stdio",
		"id":               "idle",
		"rootPid":          os.Getpid(),
		"workingDirectory": "/home/harish/projects/me",
		"command":          "bash",
		"interactive":      true,
		"updatedAt":        now.Add(-10 * time.Second).Format(time.RFC3339Nano),
		"lastInputAt":      now.Add(-45 * time.Minute).Format(time.RFC3339Nano),
		"idleAfter":        "30m",
		"expiresAt":        now.Add(30 * time.Minute).Format(time.RFC3339Nano),
	})
	writeTestLease(t, leaseDir, "stale", map[string]any{
		"kind":             "stdio",
		"id":               "stale",
		"rootPid":          99999999,
		"workingDirectory": "/tmp",
		"command":          "claude",
		"interactive":      true,
		"updatedAt":        now.Add(-10 * time.Second).Format(time.RFC3339Nano),
		"lastInputAt":      now.Add(-10 * time.Second).Format(time.RFC3339Nano),
		"idleAfter":        "30m",
		"expiresAt":        now.Add(30 * time.Minute).Format(time.RFC3339Nano),
	})
	malformedPath := filepath.Join(leaseDir, "malformed.json")
	if err := os.WriteFile(malformedPath, []byte("{"), 0o644); err != nil {
		t.Fatalf("write malformed lease: %v", err)
	}
	if err := os.WriteFile(filepath.Join(leaseDir, "ignored.tmp"), []byte("{"), 0o644); err != nil {
		t.Fatalf("write ignored temp file: %v", err)
	}

	t.Setenv("ME_LEASE_DIR", leaseDir)
	var out bytes.Buffer
	cmd := NewRootCommand(BuildInfo{})
	cmd.SetArgs([]string{"idle", "status"})
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute idle status: %v", err)
	}

	got := out.String()
	assertContains(t, got, "Idle leases: 1 active, 1 idle, 2 stale (4 total)")
	assertContains(t, got, "Lease directory: "+leaseDir)
	assertContains(t, got, "- active [stdio] active: command=codex cwd=/home/harish/projects/me reason=terminal activity within idle window")
	assertContains(t, got, "- idle [stdio] idle: command=bash cwd=/home/harish/projects/me reason=terminal activity older than idle window")
	assertContains(t, got, "- stale [stdio] stale: command=claude cwd=/tmp reason=root process is not running")
	assertContains(t, got, "- malformed [-] stale: command=- reason=malformed lease JSON")
	if strings.Contains(got, "ignored") {
		t.Fatalf("non-json temp file should be ignored:\n%s", got)
	}
	if _, err := os.Stat(malformedPath); err != nil {
		t.Fatalf("human status should not prune stale leases: %v", err)
	}
}

func TestIdleStatusJSONCreatesMissingLeaseDirectory(t *testing.T) {
	leaseDir := filepath.Join(t.TempDir(), "missing", "leases")

	t.Setenv("ME_LEASE_DIR", leaseDir)
	var out bytes.Buffer
	cmd := NewRootCommand(BuildInfo{})
	cmd.SetArgs([]string{"idle", "status", "--json"})
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute idle status: %v", err)
	}
	if info, err := os.Stat(leaseDir); err != nil {
		t.Fatalf("stat created lease directory: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("lease path should be a directory: %s", leaseDir)
	}

	report := decodeIdleStatusReport(t, out.Bytes())
	if got, want := report.LeaseDirectory, leaseDir; got != want {
		t.Fatalf("lease directory mismatch: want %q, got %q", want, got)
	}
	if got, want := report.Counts.Total, 0; got != want {
		t.Fatalf("total count mismatch: want %d, got %d", want, got)
	}
}

func TestIdleStatusJSONReportsExpiredAndOldHeartbeatLeasesAsStale(t *testing.T) {
	leaseDir := t.TempDir()
	now := time.Now().UTC()

	writeTestLease(t, leaseDir, "expired", map[string]any{
		"kind":         "stdio",
		"id":           "expired",
		"rootPid":      os.Getpid(),
		"command":      "codex",
		"updatedAt":    now.Add(-10 * time.Second).Format(time.RFC3339Nano),
		"lastOutputAt": now.Add(-10 * time.Second).Format(time.RFC3339Nano),
		"idleAfter":    "30m",
		"expiresAt":    now.Add(-1 * time.Second).Format(time.RFC3339Nano),
	})
	writeTestLease(t, leaseDir, "old-heartbeat", map[string]any{
		"kind":         "stdio",
		"id":           "old-heartbeat",
		"rootPid":      os.Getpid(),
		"command":      "claude",
		"updatedAt":    now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
		"lastOutputAt": now.Add(-10 * time.Second).Format(time.RFC3339Nano),
		"idleAfter":    "30m",
		"expiresAt":    now.Add(30 * time.Minute).Format(time.RFC3339Nano),
	})

	t.Setenv("ME_LEASE_DIR", leaseDir)
	var out bytes.Buffer
	cmd := NewRootCommand(BuildInfo{})
	cmd.SetArgs([]string{"idle", "status", "--json"})
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute idle status: %v", err)
	}

	report := decodeIdleStatusReport(t, out.Bytes())
	expired := assertReportedLease(t, report, "expired", "stale", "codex", "")
	if !strings.Contains(expired.Reason, "expired") {
		t.Fatalf("expired reason mismatch: %q", expired.Reason)
	}
	oldHeartbeat := assertReportedLease(t, report, "old-heartbeat", "stale", "claude", "")
	if !strings.Contains(oldHeartbeat.Reason, "heartbeat") {
		t.Fatalf("old heartbeat reason mismatch: %q", oldHeartbeat.Reason)
	}
}

func TestIdleStatusJSONReturnsOperationalDirectoryFailures(t *testing.T) {
	leasePath := filepath.Join(t.TempDir(), "leases")
	if err := os.WriteFile(leasePath, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write lease path file: %v", err)
	}

	t.Setenv("ME_LEASE_DIR", leasePath)
	var out bytes.Buffer
	cmd := NewRootCommand(BuildInfo{})
	cmd.SetArgs([]string{"idle", "status", "--json"})
	cmd.SetOut(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected operational directory failure")
	}
	if !strings.Contains(err.Error(), "create idle lease directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIdleStatusHumanReturnsOperationalDirectoryFailures(t *testing.T) {
	leasePath := filepath.Join(t.TempDir(), "leases")
	if err := os.WriteFile(leasePath, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write lease path file: %v", err)
	}

	t.Setenv("ME_LEASE_DIR", leasePath)
	var out bytes.Buffer
	cmd := NewRootCommand(BuildInfo{})
	cmd.SetArgs([]string{"idle", "status"})
	cmd.SetOut(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected operational directory failure")
	}
	if !strings.Contains(err.Error(), "create idle lease directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeTestLease(t *testing.T, dir, id string, fields map[string]any) {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("encode test lease: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write test lease: %v", err)
	}
}

type testIdleStatusReport struct {
	LeaseDirectory string               `json:"leaseDirectory"`
	Counts         testIdleStatusCounts `json:"counts"`
	Leases         map[string]testIdleStatusLease
}

type testIdleStatusCounts struct {
	Active int `json:"active"`
	Idle   int `json:"idle"`
	Stale  int `json:"stale"`
	Total  int `json:"total"`
}

type testIdleStatusLease struct {
	ID               string `json:"id"`
	State            string `json:"state"`
	Reason           string `json:"reason"`
	Command          string `json:"command"`
	WorkingDirectory string `json:"workingDirectory"`
}

func decodeIdleStatusReport(t *testing.T, data []byte) testIdleStatusReport {
	t.Helper()
	var raw struct {
		LeaseDirectory string                `json:"leaseDirectory"`
		Counts         testIdleStatusCounts  `json:"counts"`
		Leases         []testIdleStatusLease `json:"leases"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode idle status output %s: %v", data, err)
	}
	report := testIdleStatusReport{
		LeaseDirectory: raw.LeaseDirectory,
		Counts:         raw.Counts,
		Leases:         make(map[string]testIdleStatusLease),
	}
	for _, lease := range raw.Leases {
		report.Leases[lease.ID] = lease
	}
	return report
}

func assertReportedLease(t *testing.T, report testIdleStatusReport, id, state, command, workingDirectory string) testIdleStatusLease {
	t.Helper()
	lease, ok := report.Leases[id]
	if !ok {
		t.Fatalf("missing reported lease %q in %#v", id, report.Leases)
	}
	if got := lease.State; got != state {
		t.Fatalf("lease %q state mismatch: want %q, got %q", id, state, got)
	}
	if got := lease.Command; got != command {
		t.Fatalf("lease %q command mismatch: want %q, got %q", id, command, got)
	}
	if got := lease.WorkingDirectory; got != workingDirectory {
		t.Fatalf("lease %q working directory mismatch: want %q, got %q", id, workingDirectory, got)
	}
	if lease.Reason == "" {
		t.Fatalf("lease %q should include a state reason", id)
	}
	return lease
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("output should contain %q:\n%s", want, got)
	}
}
