package sentinel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ppiankov/runforge/internal/task"
)

func TestCompletionTracker_Empty(t *testing.T) {
	ct := NewCompletionTracker(filepath.Join(t.TempDir(), "ct.json"))
	if ct.IsCompleted("task-1") {
		t.Fatal("empty tracker should not have any completed tasks")
	}
	if ct.Count() != 0 {
		t.Fatalf("expected 0, got %d", ct.Count())
	}
}

func TestCompletionTracker_RecordAndCheck(t *testing.T) {
	ct := NewCompletionTracker(filepath.Join(t.TempDir(), "ct.json"))
	ct.Record("task-1", "run-abc", "codex")

	if !ct.IsCompleted("task-1") {
		t.Fatal("task-1 should be completed")
	}
	if ct.IsCompleted("task-2") {
		t.Fatal("task-2 should not be completed")
	}
	if ct.Count() != 1 {
		t.Fatalf("expected 1, got %d", ct.Count())
	}
}

func TestCompletionTracker_FilterNew(t *testing.T) {
	ct := NewCompletionTracker(filepath.Join(t.TempDir(), "ct.json"))
	ct.Record("task-1", "run-abc", "codex")
	ct.Record("task-3", "run-abc", "claude")

	tasks := []task.Task{
		{ID: "task-1"},
		{ID: "task-2"},
		{ID: "task-3"},
		{ID: "task-4"},
	}
	filtered := ct.FilterNew(tasks)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 new tasks, got %d", len(filtered))
	}
	if filtered[0].ID != "task-2" || filtered[1].ID != "task-4" {
		t.Fatalf("expected task-2 and task-4, got %s and %s", filtered[0].ID, filtered[1].ID)
	}
}

func TestCompletionTracker_PersistAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ct.json")
	ct := NewCompletionTracker(path)
	ct.Record("task-1", "run-abc", "codex")
	ct.Record("task-2", "run-def", "claude")

	// create new tracker from same path
	ct2 := NewCompletionTracker(path)
	if !ct2.IsCompleted("task-1") {
		t.Fatal("task-1 should survive reload")
	}
	if !ct2.IsCompleted("task-2") {
		t.Fatal("task-2 should survive reload")
	}
	if ct2.Count() != 2 {
		t.Fatalf("expected 2 after reload, got %d", ct2.Count())
	}
}

func TestCompletionTracker_MissingFile(t *testing.T) {
	ct := NewCompletionTracker(filepath.Join(t.TempDir(), "nonexistent", "ct.json"))
	if ct.Count() != 0 {
		t.Fatal("missing file should result in empty tracker")
	}
}

func TestCompletionTracker_CorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ct.json")
	_ = os.WriteFile(path, []byte("not json"), 0o644)
	ct := NewCompletionTracker(path)
	if ct.Count() != 0 {
		t.Fatal("corrupt file should result in empty tracker")
	}
}

func TestCompletionTracker_Clear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ct.json")
	ct := NewCompletionTracker(path)
	ct.Record("task-1", "run-abc", "codex")
	ct.Clear()

	if ct.Count() != 0 {
		t.Fatal("clear should remove all entries")
	}
	if ct.IsCompleted("task-1") {
		t.Fatal("task-1 should not be completed after clear")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("clear should delete the persistence file")
	}
}
