package runner

import (
	"bytes"
	"testing"
	"time"
)

func TestRateLimitWriter_DetectsPattern(t *testing.T) {
	var buf bytes.Buffer
	rlw := newRateLimitWriter(&buf)

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
	rlw := newRateLimitWriter(&buf)

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
	rlw := newRateLimitWriter(&buf)

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
	rlw := newRateLimitWriter(&buf)

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
	rlw := newRateLimitWriter(&buf)

	// first detection sets the timestamp
	_, _ = rlw.Write([]byte(`{"type":"usage_limit_reached","resets_at":1718000000}`))
	// second write with different timestamp should not overwrite
	_, _ = rlw.Write([]byte(`{"type":"usage_limit_reached","resets_at":1718009999}`))

	expected := time.Unix(1718000000, 0)
	if !rlw.ResetsAt().Equal(expected) {
		t.Errorf("expected first resets_at %v, got %v", expected, rlw.ResetsAt())
	}
}
