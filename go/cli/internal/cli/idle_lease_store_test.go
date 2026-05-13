package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIdleLeaseFileStorePublishesOnlyFinalJSONFile(t *testing.T) {
	leaseDir := t.TempDir()
	store, err := newIdleLeaseFileStore(func(key string) string {
		if key == "MYN_LEASE_DIR" {
			return leaseDir
		}
		return ""
	})
	if err != nil {
		t.Fatalf("create lease store: %v", err)
	}

	now := time.Now().UTC()
	if err := store.write(idleLease{
		Kind:             stdioLeaseKind,
		ID:               "stdio-test",
		RootPID:          os.Getpid(),
		ProcessGroup:     os.Getpid(),
		User:             "harish",
		WorkingDirectory: "/home/harish/projects/myn",
		Command:          "codex",
		Interactive:      true,
		StartedAt:        now,
		UpdatedAt:        now,
		IdleAfter:        idleLeaseDuration(30 * time.Minute),
		ExpiresAt:        now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("write lease: %v", err)
	}

	entries, err := os.ReadDir(leaseDir)
	if err != nil {
		t.Fatalf("read lease directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "stdio-test.json" {
		t.Fatalf("lease write should publish one final JSON file, got %#v", entries)
	}

	leasePath := filepath.Join(leaseDir, "stdio-test.json")
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
	if lease.ID != "stdio-test" {
		t.Fatalf("lease ID mismatch: %q", lease.ID)
	}
}

func TestStdioLeaseSessionFlushesMeaningfulActivityThenBatchesContinuingActivity(t *testing.T) {
	leaseDir := t.TempDir()
	session := &stdioLeaseSession{
		store: idleLeaseFileStore{dir: leaseDir},
		lease: idleLease{
			Kind:         stdioLeaseKind,
			ID:           "stdio-test",
			RootPID:      os.Getpid(),
			ProcessGroup: os.Getpid(),
			Interactive:  true,
			IdleAfter:    idleLeaseDuration(time.Minute),
		},
	}
	leasePath := session.store.path(session.lease.ID)
	firstActivity := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	if err := session.recordOutput(firstActivity); err != nil {
		t.Fatalf("record first output activity: %v", err)
	}
	published := readStdioLeaseFile(t, leasePath)
	if published.LastOutputAt == nil || !published.LastOutputAt.Equal(firstActivity) {
		t.Fatalf("first output activity should flush promptly, got %#v", published.LastOutputAt)
	}
	if published.LastInputAt != nil {
		t.Fatalf("output activity should not update input activity: %#v", published.LastInputAt)
	}
	if published.UpdatedAt.Equal(firstActivity) {
		t.Fatalf("updatedAt should remain heartbeat metadata, got same time as activity %s", published.UpdatedAt)
	}

	secondActivity := firstActivity.Add(10 * time.Second)
	if err := session.recordOutput(secondActivity); err != nil {
		t.Fatalf("record continuing output activity: %v", err)
	}
	batched := readStdioLeaseFile(t, leasePath)
	if batched.LastOutputAt == nil || !batched.LastOutputAt.Equal(firstActivity) {
		t.Fatalf("continuing activity should be batched until heartbeat, got %#v", batched.LastOutputAt)
	}

	heartbeat := firstActivity.Add(20 * time.Second)
	if err := session.flush(heartbeat); err != nil {
		t.Fatalf("flush heartbeat: %v", err)
	}
	flushed := readStdioLeaseFile(t, leasePath)
	if flushed.LastOutputAt == nil || !flushed.LastOutputAt.Equal(secondActivity) {
		t.Fatalf("heartbeat should persist batched activity, got %#v", flushed.LastOutputAt)
	}
	if !flushed.UpdatedAt.Equal(heartbeat) {
		t.Fatalf("heartbeat timestamp mismatch: want %s, got %s", heartbeat, flushed.UpdatedAt)
	}
}
