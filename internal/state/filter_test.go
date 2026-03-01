package state

import (
	"path/filepath"
	"testing"

	"github.com/ppiankov/runforge/internal/task"
)

func TestFilterTasks_NoState(t *testing.T) {
	tr := Load(filepath.Join(t.TempDir(), "state.json"))
	tasks := []task.Task{
		{ID: "t1"},
		{ID: "t2"},
	}
	filtered, skipped := FilterTasks(tasks, tr, false)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(filtered))
	}
	if len(skipped) != 0 {
		t.Fatalf("expected 0 skipped, got %d", len(skipped))
	}
}

func TestFilterTasks_CompletedSkipped(t *testing.T) {
	tr := Load(filepath.Join(t.TempDir(), "state.json"))
	tr.MarkCompleted("t1", "codex", "abc")

	tasks := []task.Task{
		{ID: "t1"},
		{ID: "t2"},
	}
	filtered, skipped := FilterTasks(tasks, tr, false)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 task, got %d", len(filtered))
	}
	if filtered[0].ID != "t2" {
		t.Fatalf("expected t2, got %s", filtered[0].ID)
	}
	if len(skipped) != 1 || skipped[0].ID != "t1" {
		t.Fatalf("expected t1 skipped, got %v", skipped)
	}
}

func TestFilterTasks_CompletedAlwaysSkipped(t *testing.T) {
	tr := Load(filepath.Join(t.TempDir(), "state.json"))
	tr.MarkCompleted("t1", "codex", "abc")

	tasks := []task.Task{{ID: "t1"}}

	// even with --retry, completed tasks are skipped
	filtered, skipped := FilterTasks(tasks, tr, true)
	if len(filtered) != 0 {
		t.Fatal("completed tasks should always be skipped, even with retry")
	}
	if len(skipped) != 1 {
		t.Fatal("expected 1 skipped")
	}
}

func TestFilterTasks_FailedSkippedWithoutRetry(t *testing.T) {
	tr := Load(filepath.Join(t.TempDir(), "state.json"))
	tr.MarkFailed("t1", "test failures")

	tasks := []task.Task{{ID: "t1"}, {ID: "t2"}}

	filtered, skipped := FilterTasks(tasks, tr, false)
	if len(filtered) != 1 || filtered[0].ID != "t2" {
		t.Fatalf("expected only t2, got %v", filtered)
	}
	if len(skipped) != 1 {
		t.Fatal("expected t1 skipped")
	}
}

func TestFilterTasks_FailedRetried(t *testing.T) {
	tr := Load(filepath.Join(t.TempDir(), "state.json"))
	tr.MarkFailed("t1", "test failures")

	tasks := []task.Task{{ID: "t1"}, {ID: "t2"}}

	filtered, skipped := FilterTasks(tasks, tr, true)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 tasks with retry, got %d", len(filtered))
	}
	if len(skipped) != 0 {
		t.Fatal("expected 0 skipped with retry")
	}
}

func TestFilterTasks_InterruptedRetried(t *testing.T) {
	tr := Load(filepath.Join(t.TempDir(), "state.json"))
	tr.MarkStarted("t1", "")
	tr.RecoverInterrupted()

	tasks := []task.Task{{ID: "t1"}}

	// without retry: skipped
	filtered, skipped := FilterTasks(tasks, tr, false)
	if len(filtered) != 0 {
		t.Fatal("interrupted should be skipped without retry")
	}
	if len(skipped) != 1 {
		t.Fatal("expected 1 skipped")
	}

	// with retry: included
	filtered, skipped = FilterTasks(tasks, tr, true)
	if len(filtered) != 1 {
		t.Fatal("interrupted should be included with retry")
	}
	if len(skipped) != 0 {
		t.Fatal("expected 0 skipped with retry")
	}
}

func TestFilterTasks_DependencyStripping(t *testing.T) {
	tr := Load(filepath.Join(t.TempDir(), "state.json"))
	tr.MarkCompleted("t1", "codex", "abc")

	tasks := []task.Task{
		{ID: "t1"},
		{ID: "t2", DependsOn: []string{"t1"}},
		{ID: "t3", DependsOn: []string{"t1", "t2"}},
	}

	filtered, _ := FilterTasks(tasks, tr, false)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(filtered))
	}

	// t2 should have t1 stripped from deps
	if len(filtered[0].DependsOn) != 0 {
		t.Fatalf("t2 deps should be empty, got %v", filtered[0].DependsOn)
	}

	// t3 should have t1 stripped but keep t2
	if len(filtered[1].DependsOn) != 1 || filtered[1].DependsOn[0] != "t2" {
		t.Fatalf("t3 deps should be [t2], got %v", filtered[1].DependsOn)
	}
}

func TestFilterTasks_AllCompleted(t *testing.T) {
	tr := Load(filepath.Join(t.TempDir(), "state.json"))
	tr.MarkCompleted("t1", "codex", "abc")
	tr.MarkCompleted("t2", "claude", "def")

	tasks := []task.Task{{ID: "t1"}, {ID: "t2"}}

	filtered, skipped := FilterTasks(tasks, tr, false)
	if len(filtered) != 0 {
		t.Fatal("expected 0 tasks when all completed")
	}
	if len(skipped) != 2 {
		t.Fatalf("expected 2 skipped, got %d", len(skipped))
	}
}
