package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireRelease(t *testing.T) {
	dir := t.TempDir()

	if err := Acquire(dir, "task-1"); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	lockPath := filepath.Join(dir, lockFileName)
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}

	Release(dir)

	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatal("lock file not removed after release")
	}
}

func TestAcquireContention(t *testing.T) {
	dir := t.TempDir()

	if err := Acquire(dir, "task-1"); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	defer Release(dir)

	err := Acquire(dir, "task-2")
	if err == nil {
		t.Fatal("expected error on contention, got nil")
	}

	// error should mention the PID
	if err.Error() == "" {
		t.Error("error message should not be empty")
	}
}

func TestAcquireStaleLock(t *testing.T) {
	dir := t.TempDir()

	// write a lock with a PID that almost certainly doesn't exist
	stalePID := 99999999
	info := LockInfo{PID: stalePID, TaskID: "stale-task"}
	data, _ := json.Marshal(info)
	lockPath := filepath.Join(dir, lockFileName)
	if err := os.WriteFile(lockPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// acquire should reclaim the stale lock
	if err := Acquire(dir, "new-task"); err != nil {
		t.Fatalf("expected stale lock reclaim, got: %v", err)
	}
	defer Release(dir)

	// verify lock now belongs to us
	got, err := ReadLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskID != "new-task" {
		t.Errorf("expected task 'new-task', got %q", got.TaskID)
	}
	if got.PID != os.Getpid() {
		t.Errorf("expected PID %d, got %d", os.Getpid(), got.PID)
	}
}

func TestReleaseIdempotent(t *testing.T) {
	dir := t.TempDir()

	// releasing a non-existent lock should not panic or error
	Release(dir)
	Release(dir)
}

func TestAcquireWritesJSON(t *testing.T) {
	dir := t.TempDir()

	if err := Acquire(dir, "json-task"); err != nil {
		t.Fatal(err)
	}
	defer Release(dir)

	info, err := ReadLock(dir)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}

	if info.PID != os.Getpid() {
		t.Errorf("PID: got %d, want %d", info.PID, os.Getpid())
	}
	if info.TaskID != "json-task" {
		t.Errorf("TaskID: got %q, want 'json-task'", info.TaskID)
	}
	if info.StartedAt.IsZero() {
		t.Error("StartedAt should not be zero")
	}
}
