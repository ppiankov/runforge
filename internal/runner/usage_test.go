package runner

import (
	"strings"
	"testing"

	"github.com/ppiankov/tokencontrol/internal/task"
)

func TestAddUsage_NilTotal(t *testing.T) {
	result := addUsage(nil, 100, 50, 150)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.InputTokens != 100 || result.OutputTokens != 50 || result.TotalTokens != 150 {
		t.Errorf("got %+v", result)
	}
}

func TestAddUsage_Accumulate(t *testing.T) {
	total := &task.TokenUsage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150}
	result := addUsage(total, 200, 100, 300)
	if result.InputTokens != 300 || result.OutputTokens != 150 || result.TotalTokens != 450 {
		t.Errorf("got %+v", result)
	}
}

func TestAddUsage_ComputeTotalFromInputOutput(t *testing.T) {
	result := addUsage(nil, 1000, 500, 0)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.TotalTokens != 1500 {
		t.Errorf("expected total 1500 (computed from input+output), got %d", result.TotalTokens)
	}
	if result.InputTokens != 1000 || result.OutputTokens != 500 {
		t.Errorf("got %+v", result)
	}
}

func TestAddUsage_AllZero(t *testing.T) {
	result := addUsage(nil, 0, 0, 0)
	if result != nil {
		t.Errorf("expected nil for all-zero input, got %+v", result)
	}
}

func TestAddUsage_AllZeroPreservesExisting(t *testing.T) {
	total := &task.TokenUsage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150}
	result := addUsage(total, 0, 0, 0)
	if result != total {
		t.Error("expected same pointer back for all-zero")
	}
}

func TestParseEvents_WithUsage(t *testing.T) {
	events := `{"type":"item.completed","item":{"type":"agent_message","content":"hello"},"usage":{"input_tokens":100,"output_tokens":50,"total_tokens":150}}
{"type":"turn.completed","usage":{"input_tokens":200,"output_tokens":100,"total_tokens":300}}
`
	r := strings.NewReader(events)
	_, _, usage := parseEvents(r, t.TempDir())
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.InputTokens != 300 {
		t.Errorf("input tokens: got %d, want 300", usage.InputTokens)
	}
	if usage.OutputTokens != 150 {
		t.Errorf("output tokens: got %d, want 150", usage.OutputTokens)
	}
	if usage.TotalTokens != 450 {
		t.Errorf("total tokens: got %d, want 450", usage.TotalTokens)
	}
}

func TestParseEvents_NoUsage(t *testing.T) {
	events := `{"type":"item.completed","item":{"type":"agent_message","content":"hello"}}
{"type":"turn.completed"}
`
	r := strings.NewReader(events)
	_, _, usage := parseEvents(r, t.TempDir())
	if usage != nil {
		t.Errorf("expected nil usage when no usage events, got %+v", usage)
	}
}

func TestParseClaudeEvents_WithUsage(t *testing.T) {
	events := `{"type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":500,"output_tokens":200,"total_tokens":700}}
{"type":"result","status":"success","usage":{"input_tokens":100,"output_tokens":50,"total_tokens":150}}
`
	r := strings.NewReader(events)
	failed, _, usage := parseClaudeEvents(r, t.TempDir())
	if failed {
		t.Error("expected success")
	}
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.InputTokens != 600 {
		t.Errorf("input tokens: got %d, want 600", usage.InputTokens)
	}
	if usage.TotalTokens != 850 {
		t.Errorf("total tokens: got %d, want 850", usage.TotalTokens)
	}
}

func TestParseOpencodeEvents_WithUsage(t *testing.T) {
	events := `{"type":"text","part":{"text":"hello"},"usage":{"input_tokens":300,"output_tokens":100,"total_tokens":400}}
`
	r := strings.NewReader(events)
	_, _, usage := parseOpencodeEvents(r, t.TempDir())
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.TotalTokens != 400 {
		t.Errorf("total tokens: got %d, want 400", usage.TotalTokens)
	}
}

func TestParseQwenEvents_WithUsage(t *testing.T) {
	events := `{"type":"result","subtype":"success","usage":{"input_tokens":1000,"output_tokens":500,"total_tokens":1500}}
`
	r := strings.NewReader(events)
	failed, _, usage := parseQwenEvents(r, t.TempDir())
	if failed {
		t.Error("expected success")
	}
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.TotalTokens != 1500 {
		t.Errorf("total tokens: got %d, want 1500", usage.TotalTokens)
	}
}

func TestParseGeminiEvents_NilUsage(t *testing.T) {
	events := `{"type":"result","status":"success"}
`
	r := strings.NewReader(events)
	_, _, usage := parseGeminiEvents(r, t.TempDir())
	if usage != nil {
		t.Errorf("expected nil usage for gemini, got %+v", usage)
	}
}

func TestParseClineEvents_NilUsage(t *testing.T) {
	events := `{"type":"say","say":"text","text":"hello"}
`
	r := strings.NewReader(events)
	_, _, usage := parseClineEvents(r, t.TempDir())
	if usage != nil {
		t.Errorf("expected nil usage for cline, got %+v", usage)
	}
}
