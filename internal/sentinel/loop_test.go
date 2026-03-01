package sentinel

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ppiankov/runforge/internal/task"
)

func fakeRunFn(completed int, failed int) RunFunc {
	return func(_ context.Context, tasks []task.Task, _ *task.TaskFile) (*RunResult, error) {
		results := make(map[string]*task.TaskResult, len(tasks))
		for i, t := range tasks {
			if i < completed {
				results[t.ID] = &task.TaskResult{
					TaskID:     t.ID,
					State:      task.StateCompleted,
					RunnerUsed: "codex",
				}
			} else {
				results[t.ID] = &task.TaskResult{
					TaskID: t.ID,
					State:  task.StateFailed,
					Error:  "mock failure",
				}
			}
		}
		return &RunResult{
			RunID:     "test-run",
			Results:   results,
			Duration:  100 * time.Millisecond,
			Completed: completed,
			Failed:    failed,
		}, nil
	}
}

func TestNewLoop_RequiresReposDir(t *testing.T) {
	_, err := NewLoop(LoopConfig{RunFn: fakeRunFn(0, 0)})
	if err == nil {
		t.Fatal("expected error for missing repos-dir")
	}
}

func TestNewLoop_RequiresRunFn(t *testing.T) {
	_, err := NewLoop(LoopConfig{ReposDir: "/tmp"})
	if err == nil {
		t.Fatal("expected error for missing run function")
	}
}

func TestNewLoop_DefaultCooldown(t *testing.T) {
	l, err := NewLoop(LoopConfig{
		ReposDir: t.TempDir(),
		RunFn:    fakeRunFn(0, 0),
		StateDir: filepath.Join(t.TempDir(), "completed.json"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if l.cfg.Cooldown != 10*time.Minute {
		t.Fatalf("expected 10m default cooldown, got %v", l.cfg.Cooldown)
	}
}

func TestLoop_StateAccessible(t *testing.T) {
	l, err := NewLoop(LoopConfig{
		ReposDir: t.TempDir(),
		RunFn:    fakeRunFn(0, 0),
		StateDir: filepath.Join(t.TempDir(), "completed.json"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if l.State() == nil {
		t.Fatal("expected non-nil state")
	}
}

func TestLoop_CycleWithNoFindings(t *testing.T) {
	// empty repos dir → no scan findings
	l, err := NewLoop(LoopConfig{
		ReposDir: t.TempDir(),
		RunFn:    fakeRunFn(0, 0),
		StateDir: filepath.Join(t.TempDir(), "completed.json"),
		ScanOnly: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	err = l.cycle(ctx)
	if err != nil {
		t.Fatalf("cycle should succeed with no findings: %v", err)
	}

	snap := l.State().Snapshot()
	if len(snap.History) != 0 {
		t.Fatal("no history expected when no tasks were found")
	}
}

func TestLoop_RunCancellation(t *testing.T) {
	l, err := NewLoop(LoopConfig{
		ReposDir: t.TempDir(),
		RunFn:    fakeRunFn(0, 0),
		Cooldown: 50 * time.Millisecond,
		StateDir: filepath.Join(t.TempDir(), "completed.json"),
		ScanOnly: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = l.Run(ctx)
	if err != nil {
		t.Fatalf("Run should return nil on cancellation: %v", err)
	}

	snap := l.State().Snapshot()
	if snap.Phase != PhaseIdle {
		t.Fatalf("expected PhaseIdle after stop, got %v", snap.Phase)
	}
}

func TestCompletionTracker_Integration(t *testing.T) {
	trackerPath := filepath.Join(t.TempDir(), "completed.json")
	ct := NewCompletionTracker(trackerPath)

	// simulate a run recording
	ct.Record("task-1", "run-1", "codex")
	ct.Record("task-2", "run-1", "claude")

	tasks := []task.Task{
		{ID: "task-1"},
		{ID: "task-2"},
		{ID: "task-3"},
	}
	filtered := ct.FilterNew(tasks)
	if len(filtered) != 1 || filtered[0].ID != "task-3" {
		t.Fatalf("expected only task-3, got %v", filtered)
	}

	// reload from disk
	ct2 := NewCompletionTracker(trackerPath)
	if !ct2.IsCompleted("task-1") || !ct2.IsCompleted("task-2") {
		t.Fatal("completed tasks should survive reload")
	}
}
