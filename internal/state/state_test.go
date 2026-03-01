package state

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestTracker_Empty(t *testing.T) {
	tr := Load(filepath.Join(t.TempDir(), "state.json"))
	if tr.Count() != 0 {
		t.Fatalf("expected 0 entries, got %d", tr.Count())
	}
	if e := tr.Get("nonexistent"); e != nil {
		t.Fatal("expected nil for nonexistent task")
	}
}

func TestTracker_MarkCompleted(t *testing.T) {
	tr := Load(filepath.Join(t.TempDir(), "state.json"))
	tr.MarkCompleted("task-1", "codex", "abc1234")

	e := tr.Get("task-1")
	if e == nil {
		t.Fatal("expected entry for task-1")
	}
	if e.Status != StatusCompleted {
		t.Fatalf("expected completed, got %s", e.Status)
	}
	if e.Runner != "codex" {
		t.Fatalf("expected codex, got %s", e.Runner)
	}
	if e.Commit != "abc1234" {
		t.Fatalf("expected abc1234, got %s", e.Commit)
	}
}

func TestTracker_MarkFailed(t *testing.T) {
	tr := Load(filepath.Join(t.TempDir(), "state.json"))
	tr.MarkFailed("task-2", "test failures")

	e := tr.Get("task-2")
	if e == nil {
		t.Fatal("expected entry for task-2")
	}
	if e.Status != StatusFailed {
		t.Fatalf("expected failed, got %s", e.Status)
	}
	if e.Error != "test failures" {
		t.Fatalf("expected 'test failures', got %s", e.Error)
	}
}

func TestTracker_MarkStarted(t *testing.T) {
	tr := Load(filepath.Join(t.TempDir(), "state.json"))
	tr.MarkStarted("task-3", "run-abc")

	e := tr.Get("task-3")
	if e == nil {
		t.Fatal("expected entry for task-3")
	}
	if e.Status != StatusInProgress {
		t.Fatalf("expected in_progress, got %s", e.Status)
	}
	if e.RunID != "run-abc" {
		t.Fatalf("expected run-abc, got %s", e.RunID)
	}
}

func TestTracker_RecoverInterrupted(t *testing.T) {
	tr := Load(filepath.Join(t.TempDir(), "state.json"))
	tr.MarkStarted("task-1", "")
	tr.MarkStarted("task-2", "")
	tr.MarkCompleted("task-3", "codex", "abc")

	count := tr.RecoverInterrupted()
	if count != 2 {
		t.Fatalf("expected 2 recovered, got %d", count)
	}

	e1 := tr.Get("task-1")
	if e1.Status != StatusInterrupted {
		t.Fatalf("expected interrupted, got %s", e1.Status)
	}

	// completed should not be affected
	e3 := tr.Get("task-3")
	if e3.Status != StatusCompleted {
		t.Fatalf("expected completed, got %s", e3.Status)
	}
}

func TestTracker_PersistAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	tr := Load(path)
	tr.MarkCompleted("task-1", "claude", "def567")
	tr.MarkFailed("task-2", "timeout")

	// reload from disk
	tr2 := Load(path)
	if tr2.Count() != 2 {
		t.Fatalf("expected 2 entries after reload, got %d", tr2.Count())
	}

	e1 := tr2.Get("task-1")
	if e1.Status != StatusCompleted || e1.Runner != "claude" || e1.Commit != "def567" {
		t.Fatalf("unexpected entry: %+v", e1)
	}

	e2 := tr2.Get("task-2")
	if e2.Status != StatusFailed || e2.Error != "timeout" {
		t.Fatalf("unexpected entry: %+v", e2)
	}
}

func TestTracker_MissingFile(t *testing.T) {
	tr := Load(filepath.Join(t.TempDir(), "nonexistent", "state.json"))
	if tr.Count() != 0 {
		t.Fatal("missing file should return empty tracker")
	}
}

func TestTracker_CorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	_ = os.WriteFile(path, []byte("not json"), 0o644)

	tr := Load(path)
	if tr.Count() != 0 {
		t.Fatal("corrupt file should return empty tracker")
	}
}

func TestTracker_Reset(t *testing.T) {
	tr := Load(filepath.Join(t.TempDir(), "state.json"))
	tr.MarkCompleted("task-1", "codex", "abc")
	tr.MarkCompleted("task-2", "claude", "def")

	tr.Reset("task-1")
	if tr.Get("task-1") != nil {
		t.Fatal("task-1 should be removed after reset")
	}
	if tr.Get("task-2") == nil {
		t.Fatal("task-2 should still exist")
	}
}

func TestTracker_Clear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	tr := Load(path)
	tr.MarkCompleted("task-1", "codex", "abc")
	tr.Clear()

	if tr.Count() != 0 {
		t.Fatal("expected 0 entries after clear")
	}

	// file should be removed
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("state file should be deleted after clear")
	}
}

func TestTracker_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	tr := Load(path)
	tr.MarkCompleted("task-1", "codex", "abc")

	// verify no .tmp file left behind
	tmp := path + ".tmp"
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("tmp file should not persist after successful write")
	}

	// verify main file exists and is valid
	tr2 := Load(path)
	if tr2.Count() != 1 {
		t.Fatalf("expected 1 entry, got %d", tr2.Count())
	}
}

func TestTracker_ConcurrentAccess(t *testing.T) {
	tr := Load(filepath.Join(t.TempDir(), "state.json"))
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := "task-" + string(rune('A'+n%26))
			tr.MarkStarted(id, "run")
			tr.Get(id)
			tr.MarkCompleted(id, "codex", "abc")
		}(i)
	}
	wg.Wait()

	if tr.Count() == 0 {
		t.Fatal("expected entries after concurrent writes")
	}
}

func TestTracker_SnapshotIsolation(t *testing.T) {
	tr := Load(filepath.Join(t.TempDir(), "state.json"))
	tr.MarkCompleted("task-1", "codex", "abc")

	// get a copy
	e := tr.Get("task-1")
	e.Status = "modified"

	// original should not be affected
	e2 := tr.Get("task-1")
	if e2.Status != StatusCompleted {
		t.Fatal("modifying copy should not affect tracker")
	}
}
