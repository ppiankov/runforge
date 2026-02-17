package runner

import "sync"

// ProviderLimiter limits concurrent usage of runners by name.
// A zero or negative limit means no limiting for that runner.
type ProviderLimiter struct {
	sems map[string]chan struct{}
	mu   sync.RWMutex
}

// NewProviderLimiter creates a limiter from a map of runner name → max concurrency.
// Entries with limit <= 0 are ignored (unlimited).
func NewProviderLimiter(limits map[string]int) *ProviderLimiter {
	sems := make(map[string]chan struct{})
	for name, limit := range limits {
		if limit > 0 {
			sems[name] = make(chan struct{}, limit)
		}
	}
	return &ProviderLimiter{sems: sems}
}

// Acquire blocks until a slot is available for the named runner.
// Returns immediately if no limit is configured for the name.
func (pl *ProviderLimiter) Acquire(name string) {
	pl.mu.RLock()
	sem, ok := pl.sems[name]
	pl.mu.RUnlock()
	if !ok {
		return
	}
	sem <- struct{}{}
}

// Release frees a slot for the named runner.
// Must be called after Acquire, once per acquire.
func (pl *ProviderLimiter) Release(name string) {
	pl.mu.RLock()
	sem, ok := pl.sems[name]
	pl.mu.RUnlock()
	if !ok {
		return
	}
	<-sem
}
