package sentinel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ppiankov/runforge/internal/ingest"
	"github.com/ppiankov/runforge/internal/task"
)

// testPayload creates a minimal valid IngestPayload JSON.
func testPayload(woID string) []byte {
	p := ingest.IngestPayload{
		Version:    "1",
		WOID:       woID,
		IncidentID: "inc-001",
		CreatedAt:  time.Now(),
		ApprovedAt: time.Now(),
		Target: ingest.IngestTarget{
			Host:  "localhost",
			Scope: "/tmp",
		},
		Observations: []ingest.IngestObservation{
			{Type: "test", Severity: "low", Detail: "test observation"},
		},
		Constraints: ingest.IngestConstraints{
			AllowPaths: []string{"/tmp"},
			MaxSteps:   5,
		},
		ProposedGoals: []string{"verify test"},
	}
	data, _ := json.Marshal(p)
	return data
}

// fakeExecFn returns a completed result immediately.
func fakeExecFn(_ context.Context, payload *ingest.IngestPayload, _ string) *task.TaskResult {
	return &task.TaskResult{
		TaskID:     payload.WOID,
		State:      task.StateCompleted,
		RunnerUsed: "test",
		StartedAt:  time.Now(),
		EndedAt:    time.Now(),
	}
}

// failExecFn returns a failed result.
func failExecFn(_ context.Context, payload *ingest.IngestPayload, _ string) *task.TaskResult {
	return &task.TaskResult{
		TaskID:    payload.WOID,
		State:     task.StateFailed,
		Error:     "simulated failure",
		StartedAt: time.Now(),
		EndedAt:   time.Now(),
	}
}

func TestNewSentinelValidation(t *testing.T) {
	t.Run("missing ingested dir", func(t *testing.T) {
		_, err := New(Config{StateDir: "/tmp/state", ExecFn: fakeExecFn})
		if err == nil {
			t.Error("expected error for missing ingested dir")
		}
	})
	t.Run("missing state dir", func(t *testing.T) {
		_, err := New(Config{IngestedDir: "/tmp/ingested", ExecFn: fakeExecFn})
		if err == nil {
			t.Error("expected error for missing state dir")
		}
	})
	t.Run("missing exec func", func(t *testing.T) {
		_, err := New(Config{IngestedDir: "/tmp/ingested", StateDir: "/tmp/state"})
		if err == nil {
			t.Error("expected error for missing exec func")
		}
	})
	t.Run("valid config", func(t *testing.T) {
		s, err := New(Config{
			IngestedDir: "/tmp/ingested",
			StateDir:    "/tmp/state",
			ExecFn:      fakeExecFn,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.cfg.Runner != "claude" {
			t.Errorf("expected default runner 'claude', got %q", s.cfg.Runner)
		}
		if s.cfg.MaxRuntime != 30*time.Minute {
			t.Errorf("expected default max runtime 30m, got %v", s.cfg.MaxRuntime)
		}
	})
}

func TestEnsureDirs(t *testing.T) {
	root := t.TempDir()
	dirs := NewDirs(filepath.Join(root, "ingested"), filepath.Join(root, "sentinel"))

	if err := EnsureDirs(dirs); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	for _, dir := range []string{dirs.Processing, dirs.Completed, dirs.Failed} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("dir %s not created: %v", dir, err)
		} else if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
	}
}

func TestProcessorSuccess(t *testing.T) {
	root := t.TempDir()
	ingested := filepath.Join(root, "ingested")
	stateDir := filepath.Join(root, "sentinel")
	profileDir := filepath.Join(root, "profiles")

	dirs := NewDirs(ingested, stateDir)
	if err := EnsureDirs(dirs); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ingested, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a valid payload.
	payloadPath := filepath.Join(ingested, "wo-001.json")
	if err := os.WriteFile(payloadPath, testPayload("wo-001"), 0o644); err != nil {
		t.Fatal(err)
	}

	proc := NewProcessor(dirs, profileDir, fakeExecFn)
	if err := proc.Process(context.Background(), payloadPath); err != nil {
		t.Fatalf("process: %v", err)
	}

	// Payload should be removed from ingested.
	if _, err := os.Stat(payloadPath); !os.IsNotExist(err) {
		t.Error("payload not removed from ingested")
	}

	// Result should be in completed/.
	resultPath := filepath.Join(dirs.Completed, "wo-001.json")
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("read completed result: %v", err)
	}

	var pr ProcessResult
	if err := json.Unmarshal(data, &pr); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if pr.State != task.StateCompleted {
		t.Errorf("expected completed, got %v", pr.State)
	}
	if pr.RunnerUsed != "test" {
		t.Errorf("expected runner 'test', got %q", pr.RunnerUsed)
	}

	// Processing/ should be empty.
	entries, _ := os.ReadDir(dirs.Processing)
	if len(entries) != 0 {
		t.Errorf("processing/ not empty: %d files", len(entries))
	}
}

func TestProcessorFailure(t *testing.T) {
	root := t.TempDir()
	ingested := filepath.Join(root, "ingested")
	stateDir := filepath.Join(root, "sentinel")
	profileDir := filepath.Join(root, "profiles")

	dirs := NewDirs(ingested, stateDir)
	if err := EnsureDirs(dirs); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ingested, 0o755); err != nil {
		t.Fatal(err)
	}

	payloadPath := filepath.Join(ingested, "wo-002.json")
	if err := os.WriteFile(payloadPath, testPayload("wo-002"), 0o644); err != nil {
		t.Fatal(err)
	}

	proc := NewProcessor(dirs, profileDir, failExecFn)
	if err := proc.Process(context.Background(), payloadPath); err != nil {
		t.Fatalf("process: %v", err)
	}

	// Result should be in failed/.
	resultPath := filepath.Join(dirs.Failed, "wo-002.json")
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("read failed result: %v", err)
	}

	var pr ProcessResult
	if err := json.Unmarshal(data, &pr); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if pr.State != task.StateFailed {
		t.Errorf("expected failed, got %v", pr.State)
	}
	if pr.Error != "simulated failure" {
		t.Errorf("expected simulated failure error, got %q", pr.Error)
	}
}

func TestProcessorInvalidPayload(t *testing.T) {
	root := t.TempDir()
	ingested := filepath.Join(root, "ingested")
	stateDir := filepath.Join(root, "sentinel")
	profileDir := filepath.Join(root, "profiles")

	dirs := NewDirs(ingested, stateDir)
	if err := EnsureDirs(dirs); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ingested, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write invalid JSON.
	payloadPath := filepath.Join(ingested, "bad-001.json")
	if err := os.WriteFile(payloadPath, []byte(`{"garbage":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	proc := NewProcessor(dirs, profileDir, fakeExecFn)
	// Process returns nil (error is recorded in failed result).
	if err := proc.Process(context.Background(), payloadPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Result should be in failed/.
	resultPath := filepath.Join(dirs.Failed, "bad-001.json")
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("read failed result: %v", err)
	}

	var pr ProcessResult
	if err := json.Unmarshal(data, &pr); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if pr.State != task.StateFailed {
		t.Errorf("expected failed state, got %v", pr.State)
	}
}

func TestRecoverOrphans(t *testing.T) {
	root := t.TempDir()
	ingested := filepath.Join(root, "ingested")
	stateDir := filepath.Join(root, "sentinel")

	dirs := NewDirs(ingested, stateDir)
	if err := EnsureDirs(dirs); err != nil {
		t.Fatal(err)
	}

	// Simulate an orphaned WO in processing/.
	orphanPath := filepath.Join(dirs.Processing, "orphan-001.json")
	if err := os.WriteFile(orphanPath, testPayload("orphan-001"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Sentinel{
		dirs: dirs,
		cfg:  Config{},
	}

	if err := s.recoverOrphans(); err != nil {
		t.Fatalf("recover orphans: %v", err)
	}

	// Orphan should be removed from processing.
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Error("orphan not removed from processing/")
	}

	// Failed result should exist.
	failedPath := filepath.Join(dirs.Failed, "orphan-001.json")
	data, err := os.ReadFile(failedPath)
	if err != nil {
		t.Fatalf("read failed result: %v", err)
	}

	var pr ProcessResult
	if err := json.Unmarshal(data, &pr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pr.State != task.StateFailed {
		t.Errorf("expected failed state, got %v", pr.State)
	}
	if pr.Error == "" {
		t.Error("expected error message for orphan")
	}
}

func TestPIDLock(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "test.pid")

	// First acquisition should succeed.
	if err := acquirePIDLock(pidPath); err != nil {
		t.Fatalf("first lock: %v", err)
	}

	// Second acquisition by same process should fail (process is still running).
	if err := acquirePIDLock(pidPath); err == nil {
		t.Error("expected error on duplicate PID lock")
	}

	// Clean up.
	_ = os.Remove(pidPath)

	// Stale PID lock should be cleaned up.
	if err := os.WriteFile(pidPath, []byte("999999999"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := acquirePIDLock(pidPath); err != nil {
		t.Fatalf("stale lock cleanup: %v", err)
	}
	_ = os.Remove(pidPath)
}

func TestScanExisting(t *testing.T) {
	root := t.TempDir()
	ingested := filepath.Join(root, "ingested")
	stateDir := filepath.Join(root, "sentinel")
	profileDir := filepath.Join(root, "profiles")

	dirs := NewDirs(ingested, stateDir)
	if err := EnsureDirs(dirs); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ingested, 0o755); err != nil {
		t.Fatal(err)
	}

	// Pre-populate ingested/ with two payloads.
	for _, id := range []string{"wo-010", "wo-011"} {
		path := filepath.Join(ingested, id+".json")
		if err := os.WriteFile(path, testPayload(id), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s := &Sentinel{
		cfg:       Config{IngestedDir: ingested},
		dirs:      dirs,
		processor: NewProcessor(dirs, profileDir, fakeExecFn),
	}

	if err := s.scanExisting(context.Background()); err != nil {
		t.Fatalf("scan existing: %v", err)
	}

	// Both should be in completed/.
	entries, err := os.ReadDir(dirs.Completed)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 completed, got %d", len(entries))
	}
}

func TestPollWatcherDetectsNewFile(t *testing.T) {
	root := t.TempDir()
	ingested := filepath.Join(root, "ingested")
	stateDir := filepath.Join(root, "sentinel")
	profileDir := filepath.Join(root, "profiles")

	dirs := NewDirs(ingested, stateDir)
	if err := EnsureDirs(dirs); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ingested, 0o755); err != nil {
		t.Fatal(err)
	}

	s := &Sentinel{
		cfg:       Config{IngestedDir: ingested, PollMode: true},
		dirs:      dirs,
		processor: NewProcessor(dirs, profileDir, fakeExecFn),
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start poll watcher in background.
	done := make(chan error, 1)
	go func() { done <- s.runPollWatcher(ctx) }()

	// Wait for first poll tick, then write a payload.
	time.Sleep(pollDefault + 500*time.Millisecond)
	payloadPath := filepath.Join(ingested, "wo-020.json")
	if err := os.WriteFile(payloadPath, testPayload("wo-020"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Wait for the poller to pick it up.
	time.Sleep(pollDefault + 500*time.Millisecond)
	cancel()
	<-done

	// Should be in completed/.
	resultPath := filepath.Join(dirs.Completed, "wo-020.json")
	if _, err := os.Stat(resultPath); os.IsNotExist(err) {
		t.Error("poll watcher did not process new payload")
	}
}

func TestIsPayloadFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"wo-001.json", true},
		{"test.json", true},
		{"file.tmp", false},
		{"file.json.tmp", false},
		{"readme.txt", false},
		{"data.bin", false},
	}
	for _, tt := range tests {
		if got := isPayloadFile(tt.name); got != tt.want {
			t.Errorf("isPayloadFile(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestMoveFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.json")
	dst := filepath.Join(dir, "dst.json")

	content := []byte("test content")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := moveFile(src, dst); err != nil {
		t.Fatalf("move: %v", err)
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source file not removed")
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(data) != string(content) {
		t.Error("content mismatch after move")
	}
}
