package runner

import (
	"bytes"
	"io"
	"regexp"
	"strconv"
	"sync"
	"time"
)

var resetsAtPattern = regexp.MustCompile(`"resets_at"\s*:\s*(\d+)`)

// rateLimitWriter wraps an io.Writer and scans each write for rate limit signals.
// It passes all data through to the underlying writer unchanged.
type rateLimitWriter struct {
	file     io.Writer
	detected bool
	resetsAt time.Time
	mu       sync.Mutex
}

// newRateLimitWriter creates a rateLimitWriter wrapping the given writer.
func newRateLimitWriter(w io.Writer) *rateLimitWriter {
	return &rateLimitWriter{file: w}
}

func (w *rateLimitWriter) Write(p []byte) (int, error) {
	n, err := w.file.Write(p)

	if w.Detected() {
		return n, err
	}

	if bytes.Contains(p, []byte("usage_limit_reached")) {
		w.mu.Lock()
		if !w.detected {
			w.detected = true
			if m := resetsAtPattern.FindSubmatch(p); len(m) == 2 {
				if ts, parseErr := strconv.ParseInt(string(m[1]), 10, 64); parseErr == nil {
					w.resetsAt = time.Unix(ts, 0)
				}
			}
		}
		w.mu.Unlock()
	}

	return n, err
}

// Detected returns true if a rate limit pattern was found.
func (w *rateLimitWriter) Detected() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.detected
}

// ResetsAt returns the parsed reset timestamp, or zero if not found.
func (w *rateLimitWriter) ResetsAt() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.resetsAt
}
