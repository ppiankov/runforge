package runner

import (
	"io"
	"sync/atomic"
	"testing"
)

func TestHealthWriter_TLSCertExpired(t *testing.T) {
	hw := newHealthWriter(io.Discard, nil)
	_, _ = hw.Write([]byte(`error: SSL certificate problem: certificate has expired`))

	if !hw.Detected() {
		t.Fatal("expected connectivity error detected")
	}
	if hw.Reason() != "TLS certificate expired" {
		t.Errorf("expected 'TLS certificate expired', got %q", hw.Reason())
	}
}

func TestHealthWriter_ConnectionRefused(t *testing.T) {
	hw := newHealthWriter(io.Discard, nil)
	_, _ = hw.Write([]byte(`dial tcp 10.0.0.1:443: connection refused`))

	if !hw.Detected() {
		t.Fatal("expected connectivity error detected")
	}
	if hw.Reason() != "connection refused" {
		t.Errorf("expected 'connection refused', got %q", hw.Reason())
	}
}

func TestHealthWriter_DNSFailure(t *testing.T) {
	hw := newHealthWriter(io.Discard, nil)
	_, _ = hw.Write([]byte(`DNS resolution failed for api.example.com`))

	if !hw.Detected() {
		t.Fatal("expected connectivity error detected")
	}
	if hw.Reason() != "DNS resolution failed" {
		t.Errorf("expected 'DNS resolution failed', got %q", hw.Reason())
	}
}

func TestHealthWriter_CouldNotResolveHost(t *testing.T) {
	hw := newHealthWriter(io.Discard, nil)
	_, _ = hw.Write([]byte(`curl: (6) Could not resolve host: api.zhipu.ai`))

	if !hw.Detected() {
		t.Fatal("expected connectivity error detected")
	}
	if hw.Reason() != "DNS resolution failed" {
		t.Errorf("expected 'DNS resolution failed', got %q", hw.Reason())
	}
}

func TestHealthWriter_RequestFailed(t *testing.T) {
	hw := newHealthWriter(io.Discard, nil)
	_, _ = hw.Write([]byte(`error sending request for url (https://api.example.com)`))

	if !hw.Detected() {
		t.Fatal("expected connectivity error detected")
	}
	if hw.Reason() != "request failed" {
		t.Errorf("expected 'request failed', got %q", hw.Reason())
	}
}

func TestHealthWriter_TLSHandshakeTimeout(t *testing.T) {
	hw := newHealthWriter(io.Discard, nil)
	_, _ = hw.Write([]byte(`net/http: TLS handshake timeout`))

	if !hw.Detected() {
		t.Fatal("expected connectivity error detected")
	}
	if hw.Reason() != "TLS handshake timeout" {
		t.Errorf("expected 'TLS handshake timeout', got %q", hw.Reason())
	}
}

func TestHealthWriter_NoMatch(t *testing.T) {
	hw := newHealthWriter(io.Discard, nil)
	_, _ = hw.Write([]byte(`normal stderr output from codex`))

	if hw.Detected() {
		t.Errorf("expected no detection, got reason %q", hw.Reason())
	}
}

func TestHealthWriter_CaseInsensitive(t *testing.T) {
	hw := newHealthWriter(io.Discard, nil)
	_, _ = hw.Write([]byte(`ERROR: CONNECTION REFUSED on port 443`))

	if !hw.Detected() {
		t.Fatal("expected case-insensitive match")
	}
	if hw.Reason() != "connection refused" {
		t.Errorf("expected 'connection refused', got %q", hw.Reason())
	}
}

func TestHealthWriter_PassesThrough(t *testing.T) {
	var buf []byte
	w := &testWriter{buf: &buf}
	hw := newHealthWriter(w, nil)

	data := []byte("test data")
	n, err := hw.Write(data)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != len(data) {
		t.Errorf("expected %d bytes written, got %d", len(data), n)
	}
	if string(*w.buf) != "test data" {
		t.Errorf("expected passthrough, got %q", string(*w.buf))
	}
}

func TestHealthWriter_CancelCallbackFires(t *testing.T) {
	var cancelled atomic.Bool
	cancel := func() { cancelled.Store(true) }

	hw := newHealthWriter(io.Discard, cancel)
	_, _ = hw.Write([]byte(`connection refused`))

	if !cancelled.Load() {
		t.Error("expected cancel to be called on connectivity error")
	}
}

func TestHealthWriter_CancelNilNoPanic(t *testing.T) {
	hw := newHealthWriter(io.Discard, nil)

	// should not panic with nil cancel
	_, _ = hw.Write([]byte(`connection refused`))

	if !hw.Detected() {
		t.Error("expected detection even with nil cancel")
	}
}

type testWriter struct {
	buf *[]byte
}

func (w *testWriter) Write(p []byte) (int, error) {
	*w.buf = append(*w.buf, p...)
	return len(p), nil
}
