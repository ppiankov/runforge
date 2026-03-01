package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Status constants for persistent task state.
const (
	StatusCompleted   = "completed"
	StatusFailed      = "failed"
	StatusInProgress  = "in_progress"
	StatusInterrupted = "interrupted"
)

// TaskEntry represents the persistent state of a single task across runs.
type TaskEntry struct {
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started,omitempty"`
	FinishedAt time.Time `json:"finished,omitempty"`
	Runner     string    `json:"runner,omitempty"`
	Commit     string    `json:"commit,omitempty"`
	Error      string    `json:"error,omitempty"`
	RunID      string    `json:"run_id,omitempty"`
}

type stateFile struct {
	Tasks map[string]*TaskEntry `json:"tasks"`
}

// Tracker provides persistent task state tracking across runs.
// Thread-safe with sync.RWMutex. Writes are atomic (tmp → rename).
type Tracker struct {
	mu    sync.RWMutex
	tasks map[string]*TaskEntry
	path  string
}

// DefaultPath returns the default state file path.
func DefaultPath() string {
	return filepath.Join(".runforge", "state.json")
}

// Load reads the state file from disk. Returns an empty tracker if the file
// does not exist or is corrupt.
func Load(path string) *Tracker {
	t := &Tracker{
		tasks: make(map[string]*TaskEntry),
		path:  path,
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return t
	}
	var sf stateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return t
	}
	if sf.Tasks != nil {
		t.tasks = sf.Tasks
	}
	return t
}

// RecoverInterrupted marks any stale in_progress tasks as interrupted.
// Returns the number of tasks recovered.
func (t *Tracker) RecoverInterrupted() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	count := 0
	for _, e := range t.tasks {
		if e.Status == StatusInProgress {
			e.Status = StatusInterrupted
			e.FinishedAt = time.Now()
			e.Error = "interrupted: process killed before completion"
			count++
		}
	}
	if count > 0 {
		_ = t.saveLocked()
	}
	return count
}

// MarkStarted records a task as in_progress.
func (t *Tracker) MarkStarted(taskID, runID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tasks[taskID] = &TaskEntry{
		Status:    StatusInProgress,
		StartedAt: time.Now(),
		RunID:     runID,
	}
	_ = t.saveLocked()
}

// MarkCompleted records a task as successfully completed.
func (t *Tracker) MarkCompleted(taskID, runner, commit string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.tasks[taskID]
	if e == nil {
		e = &TaskEntry{StartedAt: time.Now()}
		t.tasks[taskID] = e
	}
	e.Status = StatusCompleted
	e.FinishedAt = time.Now()
	e.Runner = runner
	e.Commit = commit
	_ = t.saveLocked()
}

// MarkFailed records a task as failed.
func (t *Tracker) MarkFailed(taskID, errMsg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.tasks[taskID]
	if e == nil {
		e = &TaskEntry{StartedAt: time.Now()}
		t.tasks[taskID] = e
	}
	e.Status = StatusFailed
	e.FinishedAt = time.Now()
	e.Error = errMsg
	_ = t.saveLocked()
}

// Get returns a copy of the entry for the given task, or nil if not tracked.
func (t *Tracker) Get(taskID string) *TaskEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if e, ok := t.tasks[taskID]; ok {
		cpy := *e
		return &cpy
	}
	return nil
}

// Entries returns a copy of all tracked tasks.
func (t *Tracker) Entries() map[string]*TaskEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make(map[string]*TaskEntry, len(t.tasks))
	for k, v := range t.tasks {
		cpy := *v
		result[k] = &cpy
	}
	return result
}

// Count returns the number of tracked tasks.
func (t *Tracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.tasks)
}

// Reset removes a single task entry, allowing re-execution.
func (t *Tracker) Reset(taskID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.tasks, taskID)
	_ = t.saveLocked()
}

// Clear removes all state and deletes the state file.
func (t *Tracker) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tasks = make(map[string]*TaskEntry)
	_ = os.Remove(t.path)
}

func (t *Tracker) saveLocked() error {
	sf := stateFile{Tasks: t.tasks}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(t.path), 0o755); err != nil {
		return err
	}
	tmp := t.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, t.path)
}
