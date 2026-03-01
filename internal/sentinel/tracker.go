package sentinel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ppiankov/runforge/internal/task"
)

// CompletedTask records when and how a task was completed.
type CompletedTask struct {
	TaskID    string    `json:"task_id"`
	RunID     string    `json:"run_id"`
	Runner    string    `json:"runner_used"`
	Timestamp time.Time `json:"timestamp"`
}

// CompletionTracker tracks which tasks have been completed to avoid re-running.
// Persists to a JSON file on disk.
type CompletionTracker struct {
	mu        sync.RWMutex
	completed map[string]*CompletedTask
	path      string
}

// DefaultTrackerPath returns the default path for the completion tracker.
func DefaultTrackerPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".runforge", "sentinel", "completed.json")
}

// NewCompletionTracker creates a tracker that persists to the given path.
// Loads existing state from disk if present.
func NewCompletionTracker(path string) *CompletionTracker {
	ct := &CompletionTracker{
		completed: make(map[string]*CompletedTask),
		path:      path,
	}
	ct.Load()
	return ct
}

// IsCompleted returns true if the task ID has been completed before.
func (ct *CompletionTracker) IsCompleted(taskID string) bool {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	_, ok := ct.completed[taskID]
	return ok
}

// Record marks a task as completed and persists to disk.
func (ct *CompletionTracker) Record(taskID, runID, runner string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.completed[taskID] = &CompletedTask{
		TaskID:    taskID,
		RunID:     runID,
		Runner:    runner,
		Timestamp: time.Now(),
	}
	_ = ct.saveLocked()
}

// FilterNew returns only tasks not previously completed.
func (ct *CompletionTracker) FilterNew(tasks []task.Task) []task.Task {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	var filtered []task.Task
	for _, t := range tasks {
		if _, ok := ct.completed[t.ID]; !ok {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// Count returns the number of completed tasks.
func (ct *CompletionTracker) Count() int {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return len(ct.completed)
}

// Load reads tracker state from disk. Missing or corrupt files are ignored.
func (ct *CompletionTracker) Load() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	data, err := os.ReadFile(ct.path)
	if err != nil {
		return
	}
	var entries []*CompletedTask
	if err := json.Unmarshal(data, &entries); err != nil {
		return
	}
	for _, e := range entries {
		ct.completed[e.TaskID] = e
	}
}

// Save persists tracker state to disk.
func (ct *CompletionTracker) Save() error {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.saveLocked()
}

func (ct *CompletionTracker) saveLocked() error {
	entries := make([]*CompletedTask, 0, len(ct.completed))
	for _, e := range ct.completed {
		entries = append(entries, e)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ct.path), 0o755); err != nil {
		return err
	}
	tmp := ct.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, ct.path)
}

// Clear removes all entries and deletes the persistence file.
func (ct *CompletionTracker) Clear() {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.completed = make(map[string]*CompletedTask)
	_ = os.Remove(ct.path)
}
