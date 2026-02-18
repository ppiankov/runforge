package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseOpencodeEvents_Success(t *testing.T) {
	events := []opencodeEvent{
		{Type: "message", Response: "Working on it..."},
		{Type: "result", Response: "All done."},
	}

	r := opencodeEventsToReader(t, events)
	failed, lastMsg := parseOpencodeEvents(r, t.TempDir())

	if failed {
		t.Error("expected success, got failed")
	}
	if lastMsg != "All done." {
		t.Errorf("expected last message 'All done.', got %q", lastMsg)
	}
}

func TestParseOpencodeEvents_Error(t *testing.T) {
	events := []opencodeEvent{
		{Type: "error"},
	}

	r := opencodeEventsToReader(t, events)
	failed, _ := parseOpencodeEvents(r, t.TempDir())

	if !failed {
		t.Error("expected failure, got success")
	}
}

func TestParseOpencodeEvents_SingleResponse(t *testing.T) {
	// OpenCode may output a single {"response":"..."} object
	r := strings.NewReader(`{"response":"Task completed successfully."}` + "\n")
	failed, lastMsg := parseOpencodeEvents(r, t.TempDir())

	if failed {
		t.Error("expected success, got failed")
	}
	if lastMsg != "Task completed successfully." {
		t.Errorf("expected response text, got %q", lastMsg)
	}
}

func TestParseOpencodeEvents_EmptyInput(t *testing.T) {
	r := strings.NewReader("")
	failed, lastMsg := parseOpencodeEvents(r, t.TempDir())

	if failed {
		t.Error("empty input should not be failed")
	}
	if lastMsg != "" {
		t.Errorf("expected empty last message, got %q", lastMsg)
	}
}

func TestParseOpencodeEvents_InvalidJSON(t *testing.T) {
	r := strings.NewReader("{invalid\n{also invalid\n")
	failed, lastMsg := parseOpencodeEvents(r, t.TempDir())

	if failed {
		t.Error("invalid json should not trigger failure")
	}
	if lastMsg != "" {
		t.Error("expected empty last message for invalid json")
	}
}

func TestParseOpencodeEvents_EventsPersisted(t *testing.T) {
	dir := t.TempDir()
	events := []opencodeEvent{
		{Type: "message", Response: "hello"},
		{Type: "result", Response: "done"},
	}

	r := opencodeEventsToReader(t, events)
	parseOpencodeEvents(r, dir)

	eventsPath := filepath.Join(dir, "events.jsonl")
	data, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("events.jsonl not created: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines in events.jsonl, got %d", len(lines))
	}
}

func TestParseOpencodeEvents_LastMessageFromResult(t *testing.T) {
	events := []opencodeEvent{
		{Type: "message", Response: "First"},
		{Type: "message", Response: "Second"},
		{Type: "result", Response: "Final"},
	}

	r := opencodeEventsToReader(t, events)
	_, lastMsg := parseOpencodeEvents(r, t.TempDir())

	if lastMsg != "Final" {
		t.Errorf("expected 'Final', got %q", lastMsg)
	}
}

func TestParseOpencodeEvents_V1TextEvents(t *testing.T) {
	// v1.x format: text content in part.text, step_finish with reason
	input := strings.Join([]string{
		`{"type":"step_start","sessionID":"ses_123","part":{"type":"step-start"}}`,
		`{"type":"text","sessionID":"ses_123","part":{"type":"text","text":"hello world"}}`,
		`{"type":"step_finish","sessionID":"ses_123","part":{"type":"step-finish","reason":"stop"}}`,
	}, "\n") + "\n"

	r := strings.NewReader(input)
	failed, lastMsg := parseOpencodeEvents(r, t.TempDir())

	if failed {
		t.Error("expected success, got failed")
	}
	if lastMsg != "hello world" {
		t.Errorf("expected 'hello world', got %q", lastMsg)
	}
}

func TestParseOpencodeEvents_V1ErrorEvent(t *testing.T) {
	input := `{"type":"error","sessionID":"ses_123","part":{"type":"error","error":"model not found"}}` + "\n"

	r := strings.NewReader(input)
	failed, _ := parseOpencodeEvents(r, t.TempDir())

	if !failed {
		t.Error("expected failure on error event")
	}
}

func TestParseOpencodeEvents_V1StepFinishError(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"text","sessionID":"ses_123","part":{"text":"partial work"}}`,
		`{"type":"step_finish","sessionID":"ses_123","part":{"reason":"error"}}`,
	}, "\n") + "\n"

	r := strings.NewReader(input)
	failed, lastMsg := parseOpencodeEvents(r, t.TempDir())

	if !failed {
		t.Error("expected failure on step_finish with error reason")
	}
	if lastMsg != "partial work" {
		t.Errorf("expected 'partial work', got %q", lastMsg)
	}
}

func opencodeEventsToReader(t *testing.T, events []opencodeEvent) *strings.Reader {
	t.Helper()
	var lines []string
	for _, ev := range events {
		b, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(b))
	}
	return strings.NewReader(strings.Join(lines, "\n") + "\n")
}
