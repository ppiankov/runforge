package runner

import (
	"sync"
	"time"
)

// RunnerBlacklist tracks runners that should be skipped due to rate limiting.
// It is safe for concurrent use.
type RunnerBlacklist struct {
	mu    sync.RWMutex
	until map[string]time.Time // runner name → blocked until
}

// NewRunnerBlacklist creates a new empty blacklist.
func NewRunnerBlacklist() *RunnerBlacklist {
	return &RunnerBlacklist{
		until: make(map[string]time.Time),
	}
}

// Block marks a runner as blocked until the given time.
func (b *RunnerBlacklist) Block(runner string, until time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// only extend the block, never shorten it
	if existing, ok := b.until[runner]; ok && existing.After(until) {
		return
	}
	b.until[runner] = until
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
