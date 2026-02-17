package runner

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderLimiter_NoLimit(t *testing.T) {
	pl := NewProviderLimiter(nil)
	// should not block or panic
	pl.Acquire("codex")
	pl.Release("codex")
}

func TestProviderLimiter_UnlimitedEntry(t *testing.T) {
	pl := NewProviderLimiter(map[string]int{"codex": 0})
	// zero limit means unlimited — should not block
	pl.Acquire("codex")
	pl.Release("codex")
}

func TestProviderLimiter_ConcurrencyRespected(t *testing.T) {
	pl := NewProviderLimiter(map[string]int{"codex": 2})

	var peak atomic.Int32
	var current atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pl.Acquire("codex")
			n := current.Add(1)
			// track peak concurrency
			for {
				old := peak.Load()
				if n <= old || peak.CompareAndSwap(old, n) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			current.Add(-1)
			pl.Release("codex")
		}()
	}

	wg.Wait()

	if p := peak.Load(); p > 2 {
		t.Fatalf("peak concurrency %d exceeded limit 2", p)
	}
	if p := peak.Load(); p < 2 {
		t.Fatalf("peak concurrency %d — expected at least 2", p)
	}
}

func TestProviderLimiter_IndependentRunners(t *testing.T) {
	pl := NewProviderLimiter(map[string]int{"codex": 1, "claude": 1})

	// both should be acquirable simultaneously since they are independent
	pl.Acquire("codex")
	pl.Acquire("claude")
	pl.Release("codex")
	pl.Release("claude")
}
