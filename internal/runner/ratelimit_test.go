package runner

import (
	"bytes"
	"sync/atomic"
	"testing"
	"time"
)

func TestRateLimitWriter_DetectsPattern(t *testing.T) {
	var buf bytes.Buffer
	rlw := newRateLimitWriter(&buf, nil)

	input := `{"type":"usage_limit_reached","message":"Rate limit exceeded","resets_at":1718000000}`
	_, err := rlw.Write([]byte(input))
	if err != nil {
		t.Fatal(err)
	}

	if !rlw.Detected() {
		t.Error("expected rate limit to be detected")
	}

	expected := time.Unix(1718000000, 0)
	if !rlw.ResetsAt().Equal(expected) {
		t.Errorf("expected resets_at %v, got %v", expected, rlw.ResetsAt())
	}

	// verify data was written through
	if buf.String() != input {
		t.Errorf("expected passthrough, got %q", buf.String())
	}
}

func TestRateLimitWriter_NoFalsePositive(t *testing.T) {
	var buf bytes.Buffer
	rlw := newRateLimitWriter(&buf, nil)

	_, _ = rlw.Write([]byte(`{"type":"turn.completed","status":"ok"}`))
	_, _ = rlw.Write([]byte(`some random stderr output`))
	_, _ = rlw.Write([]byte(`error: something failed`))

	if rlw.Detected() {
		t.Error("should not detect rate limit in normal output")
	}
	if !rlw.ResetsAt().IsZero() {
		t.Error("resets_at should be zero when not detected")
	}
}

func TestRateLimitWriter_SplitWrite(t *testing.T) {
	var buf bytes.Buffer
	rlw := newRateLimitWriter(&buf, nil)

	// write the pattern across two writes — second write contains full pattern
	_, _ = rlw.Write([]byte(`{"type":"error","msg":"first"}`))
	_, _ = rlw.Write([]byte(`{"type":"usage_limit_reached","resets_at":1718001000}`))

	if !rlw.Detected() {
		t.Error("expected rate limit detection on second write")
	}

	expected := time.Unix(1718001000, 0)
	if !rlw.ResetsAt().Equal(expected) {
		t.Errorf("expected resets_at %v, got %v", expected, rlw.ResetsAt())
	}
}

func TestRateLimitWriter_NoTimestamp(t *testing.T) {
	var buf bytes.Buffer
	rlw := newRateLimitWriter(&buf, nil)

	// pattern present but no resets_at field
	_, _ = rlw.Write([]byte(`{"type":"usage_limit_reached","message":"limit hit"}`))

	if !rlw.Detected() {
		t.Error("expected rate limit detection even without timestamp")
	}
	if !rlw.ResetsAt().IsZero() {
		t.Error("resets_at should be zero when timestamp not in payload")
	}
}

func TestRateLimitWriter_MultipleWrites(t *testing.T) {
	var buf bytes.Buffer
	rlw := newRateLimitWriter(&buf, nil)

	// first detection sets the timestamp
	_, _ = rlw.Write([]byte(`{"type":"usage_limit_reached","resets_at":1718000000}`))
	// second write with different timestamp should not overwrite
	_, _ = rlw.Write([]byte(`{"type":"usage_limit_reached","resets_at":1718009999}`))

	expected := time.Unix(1718000000, 0)
	if !rlw.ResetsAt().Equal(expected) {
		t.Errorf("expected first resets_at %v, got %v", expected, rlw.ResetsAt())
	}
}

func TestRateLimitWriter_CancelCallbackFires(t *testing.T) {
	var buf bytes.Buffer
	var cancelled atomic.Bool
	cancel := func() { cancelled.Store(true) }

	rlw := newRateLimitWriter(&buf, cancel)
	_, _ = rlw.Write([]byte(`{"type":"usage_limit_reached"}`))

	if !cancelled.Load() {
		t.Error("expected cancel to be called on rate limit detection")
	}
}

func TestRateLimitWriter_CancelNilNoPanic(t *testing.T) {
	var buf bytes.Buffer
	rlw := newRateLimitWriter(&buf, nil)

	// should not panic with nil cancel
	_, _ = rlw.Write([]byte(`{"type":"usage_limit_reached"}`))

	if !rlw.Detected() {
		t.Error("expected detection even with nil cancel")
	}
}

func TestRateLimitWriter_ClaudePattern(t *testing.T) {
	var buf bytes.Buffer
	rlw := newRateLimitWriter(&buf, nil)

	_, _ = rlw.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Rate limited"}}`))

	if !rlw.Detected() {
		t.Error("expected rate_limit_error pattern to be detected")
	}
}

func TestRateLimitWriter_OverloadedError(t *testing.T) {
	var buf bytes.Buffer
	rlw := newRateLimitWriter(&buf, nil)

	_, _ = rlw.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))

	if !rlw.Detected() {
		t.Error("expected overloaded_error pattern to be detected")
	}
}

func TestRateLimitWriter_GeminiPattern(t *testing.T) {
	var buf bytes.Buffer
	rlw := newRateLimitWriter(&buf, nil)

	_, _ = rlw.Write([]byte(`RESOURCE_EXHAUSTED: quota exceeded for model`))

	if !rlw.Detected() {
		t.Error("expected RESOURCE_EXHAUSTED pattern to be detected")
	}
}

func TestRateLimitWriter_HTTP429(t *testing.T) {
	var buf bytes.Buffer
	rlw := newRateLimitWriter(&buf, nil)

	_, _ = rlw.Write([]byte(`HTTP/1.1 429 Too Many Requests`))

	if !rlw.Detected() {
		t.Error("expected 429 Too Many Requests to be detected")
	}
}

func TestRateLimitWriter_CaseInsensitive(t *testing.T) {
	var buf bytes.Buffer
	rlw := newRateLimitWriter(&buf, nil)

	_, _ = rlw.Write([]byte(`Error: RATE LIMIT exceeded for this API key`))

	if !rlw.Detected() {
		t.Error("expected case-insensitive rate limit detection")
	}
}

func TestRateLimitWriter_OutOfCodexMessages(t *testing.T) {
	var buf bytes.Buffer
	rlw := newRateLimitWriter(&buf, nil)

	_, _ = rlw.Write([]byte(`You're out of Codex messages until Feb 22, 2026`))

	if !rlw.Detected() {
		t.Error("expected 'out of Codex messages' to be detected")
	}
}

func TestParseResetTime_UnixTimestamp(t *testing.T) {
	p := []byte(`{"resets_at": 1718000000}`)
	got := parseResetTime(p)
	want := time.Unix(1718000000, 0)
	if !got.Equal(want) {
		t.Errorf("parseResetTime unix = %v, want %v", got, want)
	}
}

func TestParseResetTime_HumanReadable(t *testing.T) {
	p := []byte(`rate limit resets on 22 Feb 2026`)
	got := parseResetTime(p)
	if got.IsZero() {
		t.Fatal("expected non-zero time for human-readable date")
	}
	if got.Day() != 22 || got.Month() != time.February || got.Year() != 2026 {
		t.Errorf("parseResetTime date = %v, want 22 Feb 2026", got)
	}
}

func TestParseResetTime_RetryAfter(t *testing.T) {
	before := time.Now()
	p := []byte(`Retry-After: 3600`)
	got := parseResetTime(p)
	after := time.Now()

	if got.IsZero() {
		t.Fatal("expected non-zero time for Retry-After")
	}
	// should be approximately 1 hour from now
	expectedMin := before.Add(3600 * time.Second)
	expectedMax := after.Add(3600 * time.Second)
	if got.Before(expectedMin) || got.After(expectedMax) {
		t.Errorf("parseResetTime Retry-After = %v, want ~%v", got, expectedMin)
	}
}

func TestParseResetTime_NoMatch(t *testing.T) {
	p := []byte(`just some error message with no timestamp`)
	got := parseResetTime(p)
	if !got.IsZero() {
		t.Errorf("expected zero time, got %v", got)
	}
}
