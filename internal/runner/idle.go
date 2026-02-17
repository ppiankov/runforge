package runner

import (
	"io"
	"sync"
	"time"
)

// idleTimeoutReader wraps an io.Reader and fires a cancellation callback
// when no data is read for the configured timeout duration.
// Each successful Read with n > 0 resets the timer.
type idleTimeoutReader struct {
	r       io.Reader
	timer   *time.Timer
	timeout time.Duration
	cancel  func()
	idled   bool
	mu      sync.Mutex
}

// newIdleTimeoutReader creates a reader that cancels via cancel after timeout
// of inactivity (no bytes read). Pass 0 to disable idle detection.
func newIdleTimeoutReader(r io.Reader, timeout time.Duration, cancel func()) *idleTimeoutReader {
	if timeout <= 0 {
		return &idleTimeoutReader{r: r, timeout: 0}
	}
	itr := &idleTimeoutReader{
		r:       r,
		timeout: timeout,
		cancel:  cancel,
	}
	itr.timer = time.AfterFunc(timeout, itr.onTimeout)
	return itr
}

func (itr *idleTimeoutReader) Read(p []byte) (int, error) {
	n, err := itr.r.Read(p)
	if n > 0 && itr.timer != nil {
		itr.timer.Reset(itr.timeout)
	}
	return n, err
}

func (itr *idleTimeoutReader) onTimeout() {
	itr.mu.Lock()
	itr.idled = true
	itr.mu.Unlock()
	if itr.cancel != nil {
		itr.cancel()
	}
}

// Idled returns true if the idle timeout fired.
func (itr *idleTimeoutReader) Idled() bool {
	itr.mu.Lock()
	defer itr.mu.Unlock()
	return itr.idled
}

// Stop stops the idle timer. Call in defer after the reader is no longer needed.
func (itr *idleTimeoutReader) Stop() {
	if itr.timer != nil {
		itr.timer.Stop()
	}
}
