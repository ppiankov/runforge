package state

import (
	"log/slog"

	"github.com/ppiankov/runforge/internal/task"
)

// SkippedTask records why a task was filtered out by state tracking.
type SkippedTask struct {
	ID     string
	Reason string
}

// FilterTasks removes tasks that should not run based on persistent state.
// Completed tasks are always skipped. Failed and interrupted tasks are skipped
// unless retry is true. Dependencies on skipped tasks are stripped to prevent
// DAG deadlock.
func FilterTasks(tasks []task.Task, tracker *Tracker, retry bool) ([]task.Task, []SkippedTask) {
	// determine which task IDs to skip
	skipIDs := make(map[string]string) // id → reason
	for _, t := range tasks {
		entry := tracker.Get(t.ID)
		if entry == nil {
			continue
		}
		switch entry.Status {
		case StatusCompleted:
			skipIDs[t.ID] = "completed in previous run"
		case StatusFailed:
			if retry {
				continue
			}
			skipIDs[t.ID] = "failed (use --retry to re-execute)"
		case StatusInterrupted:
			if retry {
				continue
			}
			skipIDs[t.ID] = "interrupted (use --retry to re-execute)"
		}
	}

	if len(skipIDs) == 0 {
		return tasks, nil
	}

	var filtered []task.Task
	var skipped []SkippedTask

	for _, t := range tasks {
		reason, shouldSkip := skipIDs[t.ID]
		if shouldSkip {
			skipped = append(skipped, SkippedTask{ID: t.ID, Reason: reason})
			slog.Info("skipping task", "task", t.ID, "reason", reason)
			continue
		}
		// strip deps on skipped tasks so children don't deadlock
		var kept []string
		for _, dep := range t.DependsOn {
			if _, skip := skipIDs[dep]; !skip {
				kept = append(kept, dep)
			}
		}
		t.DependsOn = kept
		filtered = append(filtered, t)
	}

	return filtered, skipped
}
