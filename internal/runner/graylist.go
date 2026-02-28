package runner

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RunnerGraylist tracks runner+model pairs demoted for quality reasons
// (e.g. false-positive success with 0 commits). Graylisted runners are
// removed from fallback cascades but can still run if explicitly assigned
// as task.Runner. Entries are keyed by "runner:model" so that graylisting
// deepseek with model "deepseek-chat" does not block "deepseek-reasoner".
// It is safe for concurrent use. Supports persistence to disk.
type RunnerGraylist struct {
	mu      sync.RWMutex
	entries map[string]graylistEntry // key = graylistKey(runner, model)
	path    string
}

// graylistEntry is the JSON-serializable form of a graylist record.
type graylistEntry struct {
	Runner  string    `json:"runner"`
	Model   string    `json:"model,omitempty"`
	Reason  string    `json:"reason,omitempty"`
	AddedAt time.Time `json:"added_at"`
}

// graylistKey builds the map key for a runner+model pair.
func graylistKey(runner, model string) string {
	if model == "" {
		return runner
	}
	return runner + ":" + model
}

// DefaultGraylistPath returns ~/.runforge/graylist.json.
func DefaultGraylistPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".runforge", "graylist.json")
}

// NewRunnerGraylist creates a new empty graylist.
func NewRunnerGraylist() *RunnerGraylist {
	return &RunnerGraylist{
		entries: make(map[string]graylistEntry),
	}
}

// LoadGraylist reads a persisted graylist from disk.
// Missing or empty file is not an error — returns an empty graylist.
func LoadGraylist(path string) *RunnerGraylist {
	gl := &RunnerGraylist{
		entries: make(map[string]graylistEntry),
		path:    path,
	}
	if path == "" {
		return gl
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return gl
	}

	var entries []graylistEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		slog.Warn("corrupt graylist file, starting fresh", "path", path, "error", err)
		return gl
	}

	for _, e := range entries {
		key := graylistKey(e.Runner, e.Model)
		gl.entries[key] = e
		slog.Info("loaded graylist entry", "runner", e.Runner, "model", e.Model, "reason", e.Reason)
	}

	return gl
}

// Add marks a runner+model pair as graylisted and persists to disk.
// If model is empty, the entry matches any model for that runner.
func (g *RunnerGraylist) Add(runner, model, reason string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	key := graylistKey(runner, model)
	g.entries[key] = graylistEntry{
		Runner:  runner,
		Model:   model,
		Reason:  reason,
		AddedAt: time.Now(),
	}
	g.saveLocked()
}

// Remove removes a runner+model pair from the graylist and persists to disk.
func (g *RunnerGraylist) Remove(runner, model string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	key := graylistKey(runner, model)
	delete(g.entries, key)
	g.saveLocked()
}

// IsGraylisted returns true if the runner+model pair is on the graylist.
// Checks both the exact runner:model key and the runner-only key (wildcard).
func (g *RunnerGraylist) IsGraylisted(runner, model string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	// exact match: runner:model
	if model != "" {
		if _, ok := g.entries[graylistKey(runner, model)]; ok {
			return true
		}
	}
	// wildcard match: runner with no model (blocks all models)
	if _, ok := g.entries[runner]; ok {
		return true
	}
	return false
}

// GraylistInfo holds public info about a graylisted runner.
type GraylistInfo struct {
	Model   string
	Reason  string
	AddedAt time.Time
}

// Entries returns a copy of all graylist entries.
func (g *RunnerGraylist) Entries() map[string]GraylistInfo {
	g.mu.RLock()
	defer g.mu.RUnlock()
	result := make(map[string]GraylistInfo, len(g.entries))
	for key, e := range g.entries {
		result[key] = GraylistInfo{Model: e.Model, Reason: e.Reason, AddedAt: e.AddedAt}
	}
	return result
}

// Clear removes all graylist entries and deletes the persistence file.
func (g *RunnerGraylist) Clear() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.entries = make(map[string]graylistEntry)
	if g.path != "" {
		_ = os.Remove(g.path)
	}
}

// saveLocked writes the current graylist to disk. Caller must hold g.mu.
func (g *RunnerGraylist) saveLocked() {
	if g.path == "" {
		return
	}

	var entries []graylistEntry
	for _, e := range g.entries {
		entries = append(entries, e)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		slog.Warn("failed to marshal graylist", "error", err)
		return
	}

	dir := filepath.Dir(g.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("failed to create graylist dir", "error", err)
		return
	}

	tmp := g.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		slog.Warn("failed to write graylist", "error", err)
		return
	}
	if err := os.Rename(tmp, g.path); err != nil {
		slog.Warn("failed to rename graylist", "error", err)
		_ = os.Remove(tmp)
	}
}
