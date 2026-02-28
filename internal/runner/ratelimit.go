package runner

import (
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// resetsAtUnix extracts a Unix timestamp from JSON: "resets_at": 1718000000
var resetsAtUnix = regexp.MustCompile(`"resets_at"\s*:\s*(\d+)`)

// resetsOnDate extracts human-readable dates: "resets on 22 Feb 2026"
var resetsOnDate = regexp.MustCompile(`(?i)resets?\s+on\s+(\d{1,2}\s+\w+\s+\d{4})`)

// retryAfterSecs extracts Retry-After header seconds: "Retry-After: 3600"
var retryAfterSecs = regexp.MustCompile(`(?i)retry-after:\s*(\d+)`)

// rateLimitPatterns are case-insensitive substrings that indicate rate limiting.
var rateLimitPatterns = []string{
	"usage_limit_reached",
	"you're out of codex messages",
	"rate_limit_error",
	"overloaded_error",
	"resource_exhausted",
	"too many requests",
	"rate limit",
	"429",
}

// rateLimitWriter wraps an io.Writer and scans each write for rate limit signals.
// It passes all data through to the underlying writer unchanged.
// When a rate-limit pattern is detected, it calls the cancel callback to kill
// the runner process immediately.
type rateLimitWriter struct {
	file     io.Writer
	cancel   func() // called on first detection to kill the process
	detected bool
	resetsAt time.Time
	mu       sync.Mutex
}

// newRateLimitWriter creates a rateLimitWriter wrapping the given writer.
// cancel is called on first rate-limit detection (can be nil).
func newRateLimitWriter(w io.Writer, cancel func()) *rateLimitWriter {
	return &rateLimitWriter{file: w, cancel: cancel}
}

func (w *rateLimitWriter) Write(p []byte) (int, error) {
	n, err := w.file.Write(p)

	if w.Detected() {
		return n, err
	}

	lower := strings.ToLower(string(p))
	matched := false
	for _, pat := range rateLimitPatterns {
		if strings.Contains(lower, pat) {
			matched = true
			break
		}
	}

	if matched {
		w.mu.Lock()
		if !w.detected {
			w.detected = true
			w.resetsAt = parseResetTime(p)
			if w.cancel != nil {
				w.cancel()
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

// parseResetTime attempts to extract a reset timestamp from the payload.
// Tries: Unix timestamp, human-readable date, Retry-After seconds.
func parseResetTime(p []byte) time.Time {
	// Unix timestamp: "resets_at": 1718000000
	if m := resetsAtUnix.FindSubmatch(p); len(m) == 2 {
		if ts, err := strconv.ParseInt(string(m[1]), 10, 64); err == nil {
			return time.Unix(ts, 0)
		}
	}

	// Human-readable: "resets on 22 Feb 2026"
	if m := resetsOnDate.FindSubmatch(p); len(m) == 2 {
		for _, layout := range []string{"2 Jan 2006", "02 Jan 2006", "2 January 2006"} {
			if t, err := time.Parse(layout, string(m[1])); err == nil {
				return t
			}
		}
	}

	// Retry-After seconds: "Retry-After: 3600"
	if m := retryAfterSecs.FindSubmatch(p); len(m) == 2 {
		if secs, err := strconv.ParseInt(string(m[1]), 10, 64); err == nil && secs > 0 {
			return time.Now().Add(time.Duration(secs) * time.Second)
		}
	}

	return time.Time{}
}
