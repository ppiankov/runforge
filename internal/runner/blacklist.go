package runner

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RunnerBlacklist tracks runners that should be skipped due to rate limiting.
// It is safe for concurrent use. Supports persistence to disk so state
// survives across runforge invocations.
type RunnerBlacklist struct {
	mu    sync.RWMutex
	until map[string]time.Time // runner name → blocked until
	path  string               // persistence file path (empty = no persistence)
}

// NewRunnerBlacklist creates a new empty blacklist.
func NewRunnerBlacklist() *RunnerBlacklist {
	return &RunnerBlacklist{
		until: make(map[string]time.Time),
	}
}

// blacklistEntry is the JSON-serializable form of a blacklist record.
type blacklistEntry struct {
	Runner   string    `json:"runner"`
	ResetsAt time.Time `json:"resets_at"`
}

// DefaultBlacklistPath returns ~/.runforge/blacklist.json.
func DefaultBlacklistPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".runforge", "blacklist.json")
}

// LoadBlacklist reads a persisted blacklist from disk. Only entries with
// resets_at in the future are loaded; expired entries are discarded.
// Missing or empty file is not an error — returns an empty blacklist.
func LoadBlacklist(path string) *RunnerBlacklist {
	bl := &RunnerBlacklist{
		until: make(map[string]time.Time),
		path:  path,
	}
	if path == "" {
		return bl
	}

	data, err := os.ReadFile(path)
	if err != nil {
		// missing file is normal (first run)
		return bl
	}

	var entries []blacklistEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		slog.Warn("corrupt blacklist file, starting fresh", "path", path, "error", err)
		return bl
	}

	now := time.Now()
	for _, e := range entries {
		if e.ResetsAt.After(now) {
			bl.until[e.Runner] = e.ResetsAt
			slog.Info("loaded blacklist entry", "runner", e.Runner, "resets_at", e.ResetsAt.Format(time.Kitchen))
		}
	}

	return bl
}

// Block marks a runner as blocked until the given time and persists to disk.
func (b *RunnerBlacklist) Block(runner string, until time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// only extend the block, never shorten it
	if existing, ok := b.until[runner]; ok && existing.After(until) {
		return
	}
	b.until[runner] = until
	b.saveLocked()
}

// IsBlocked returns true if the runner is currently blocked.
func (b *RunnerBlacklist) IsBlocked(runner string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	until, ok := b.until[runner]
	if !ok {
		return false
	}
	return time.Now().Before(until)
}

// Entries returns a copy of all active (non-expired) blacklist entries.
func (b *RunnerBlacklist) Entries() map[string]time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()
	now := time.Now()
	result := make(map[string]time.Time)
	for runner, until := range b.until {
		if until.After(now) {
			result[runner] = until
		}
	}
	return result
}

// Clear removes all blacklist entries and deletes the persistence file.
func (b *RunnerBlacklist) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.until = make(map[string]time.Time)
	if b.path != "" {
		_ = os.Remove(b.path)
	}
}

// saveLocked writes the current blacklist to disk. Caller must hold b.mu.
func (b *RunnerBlacklist) saveLocked() {
	if b.path == "" {
		return
	}

	now := time.Now()
	var entries []blacklistEntry
	for runner, until := range b.until {
		if until.After(now) {
			entries = append(entries, blacklistEntry{Runner: runner, ResetsAt: until})
		}
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		slog.Warn("failed to marshal blacklist", "error", err)
		return
	}

	dir := filepath.Dir(b.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("failed to create blacklist dir", "error", err)
		return
	}

	// atomic write via temp file
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		slog.Warn("failed to write blacklist", "error", err)
		return
	}
	if err := os.Rename(tmp, b.path); err != nil {
		slog.Warn("failed to rename blacklist", "error", err)
		_ = os.Remove(tmp)
	}
}
