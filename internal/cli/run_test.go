package cli

import (
	"testing"
	"time"

	"github.com/ppiankov/runforge/internal/task"
)

func TestBuildReport_RunID(t *testing.T) {
	results := map[string]*task.TaskResult{
		"task-1": {TaskID: "task-1", State: task.StateCompleted},
	}

	r1 := buildReport([]string{"tasks.json"}, 2, "", "/repos", results, 5*time.Second, "")
	if r1.RunID == "" {
		t.Fatal("expected non-empty RunID")
	}
	if len(r1.RunID) != 12 {
		t.Errorf("expected 12-char RunID, got %d: %q", len(r1.RunID), r1.RunID)
	}

	// same timestamp + same files = same RunID
	r2 := buildReport([]string{"tasks.json"}, 2, "", "/repos", results, 5*time.Second, "")
	// timestamps differ (time.Now() called inside), so RunIDs differ — that's correct
	if r1.RunID == r2.RunID {
		// only equal if called in the same nanosecond, which is unlikely but acceptable
		t.Log("RunIDs happened to match (same nanosecond), this is acceptable")
	}
}

func TestBuildReport_RunIDDeterministic(t *testing.T) {
	// verify the hash is computed from timestamp + files
	results := map[string]*task.TaskResult{}

	r1 := buildReport([]string{"a.json"}, 1, "", "/repos", results, 0, "")
	r2 := buildReport([]string{"b.json"}, 1, "", "/repos", results, 0, "")

	if r1.RunID == r2.RunID {
		t.Error("different task files should produce different RunIDs")
	}
}

func TestBuildReport_ParentRunID(t *testing.T) {
	results := map[string]*task.TaskResult{}

	r1 := buildReport([]string{"tasks.json"}, 1, "", "/repos", results, 0, "")
	if r1.ParentRunID != "" {
		t.Errorf("expected empty ParentRunID for fresh run, got %q", r1.ParentRunID)
	}

	r2 := buildReport([]string{"tasks.json"}, 1, "", "/repos", results, 0, "abc123def456")
	if r2.ParentRunID != "abc123def456" {
		t.Errorf("expected ParentRunID 'abc123def456', got %q", r2.ParentRunID)
	}
}
