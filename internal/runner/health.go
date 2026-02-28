package runner

import (
	"io"
	"strings"
	"sync"
)

// connectivityPattern maps a stderr pattern to a human-readable reason.
type connectivityPattern struct {
	pattern string
	reason  string
}

var connectivityPatterns = []connectivityPattern{
	{"ssl certificate problem", "TLS certificate expired"},
	{"certificate has expired", "TLS certificate expired"},
	{"connection refused", "connection refused"},
	{"dns resolution failed", "DNS resolution failed"},
	{"could not resolve host", "DNS resolution failed"},
	{"error sending request", "request failed"},
	{"tls handshake timeout", "TLS handshake timeout"},
}

// healthWriter wraps an io.Writer (stderr) and scans for known
// connectivity error patterns. All data is passed through unchanged.
type healthWriter struct {
	file     io.Writer
	detected bool
	reason   string
	mu       sync.Mutex
}

func newHealthWriter(w io.Writer) *healthWriter {
	return &healthWriter{file: w}
}

func (hw *healthWriter) Write(p []byte) (int, error) {
	n, err := hw.file.Write(p)

	hw.mu.Lock()
	if !hw.detected {
		lower := strings.ToLower(string(p))
		for _, cp := range connectivityPatterns {
			if strings.Contains(lower, cp.pattern) {
				hw.detected = true
				hw.reason = cp.reason
				break
			}
		}
	}
	hw.mu.Unlock()

	return n, err
}

// Detected returns true if a connectivity error was found in stderr.
func (hw *healthWriter) Detected() bool {
	hw.mu.Lock()
	defer hw.mu.Unlock()
	return hw.detected
}

// Reason returns the human-readable connectivity error classification.
func (hw *healthWriter) Reason() string {
	hw.mu.Lock()
	defer hw.mu.Unlock()
	return hw.reason
}
