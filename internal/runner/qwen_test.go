package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseQwenEvents_Success(t *testing.T) {
	events := []qwenEvent{
		{Type: "system", Subtype: "init"},
		{Type: "assistant", Message: &qwenMessage{
			Role:    "assistant",
			Content: []qwenContent{{Type: "text", Text: "All done."}},
		}},
		{Type: "result", Subtype: "success", IsError: false, Result: "completed"},
	}

	r := qwenEventsToReader(t, events)
	failed, lastMsg := parseQwenEvents(r, t.TempDir())

	if failed {
		t.Error("expected success, got failed")
	}
	if lastMsg != "All done." {
		t.Errorf("expected last message 'All done.', got %q", lastMsg)
	}
}

func TestParseQwenEvents_Failure(t *testing.T) {
	events := []qwenEvent{
		{Type: "system", Subtype: "init"},
		{Type: "result", Subtype: "error", IsError: true},
	}

	r := qwenEventsToReader(t, events)
	failed, _ := parseQwenEvents(r, t.TempDir())

	if !failed {
		t.Error("expected failure, got success")
	}
}

func TestParseQwenEvents_FailureSubtype(t *testing.T) {
	events := []qwenEvent{
		{Type: "result", Subtype: "error", IsError: false},
	}

	r := qwenEventsToReader(t, events)
	failed, _ := parseQwenEvents(r, t.TempDir())

	if !failed {
		t.Error("non-success subtype should be treated as failure")
	}
}

func TestParseQwenEvents_LastMessage(t *testing.T) {
	events := []qwenEvent{
		{Type: "assistant", Message: &qwenMessage{
			Role:    "assistant",
			Content: []qwenContent{{Type: "text", Text: "First response"}},
		}},
		{Type: "assistant", Message: &qwenMessage{
			Role:    "assistant",
			Content: []qwenContent{{Type: "text", Text: "Final answer"}},
		}},
		{Type: "result", Subtype: "success"},
	}

	r := qwenEventsToReader(t, events)
	_, lastMsg := parseQwenEvents(r, t.TempDir())

	if lastMsg != "Final answer" {
		t.Errorf("expected 'Final answer', got %q", lastMsg)
	}
}

func TestParseQwenEvents_EmptyInput(t *testing.T) {
	r := strings.NewReader("")
	failed, lastMsg := parseQwenEvents(r, t.TempDir())

	if !failed {
		t.Error("empty input should be treated as failure")
	}
	if lastMsg != "" {
		t.Errorf("expected empty last message, got %q", lastMsg)
	}
}

func TestParseQwenEvents_InvalidJSON(t *testing.T) {
	r := strings.NewReader("{invalid\n{also invalid\n")
	failed, lastMsg := parseQwenEvents(r, t.TempDir())

	if failed {
		t.Error("invalid json should not trigger failure (events exist but unparseable)")
	}
	if lastMsg != "" {
		t.Error("expected empty last message for invalid json")
	}
}

func TestParseQwenEvents_EventsPersisted(t *testing.T) {
	dir := t.TempDir()
	events := []qwenEvent{
		{Type: "system", Subtype: "init"},
		{Type: "assistant", Message: &qwenMessage{
			Role:    "assistant",
			Content: []qwenContent{{Type: "text", Text: "hello"}},
		}},
		{Type: "result", Subtype: "success"},
	}

	r := qwenEventsToReader(t, events)
	parseQwenEvents(r, dir)

	eventsPath := filepath.Join(dir, "events.jsonl")
	data, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("events.jsonl not created: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines in events.jsonl, got %d", len(lines))
	}
}

func TestParseQwenEvents_SystemEventsIgnored(t *testing.T) {
	events := []qwenEvent{
		{Type: "system", Subtype: "init"},
		{Type: "assistant", Message: &qwenMessage{
			Role:    "assistant",
			Content: []qwenContent{{Type: "text", Text: "actual response"}},
		}},
		{Type: "result", Subtype: "success"},
	}

	r := qwenEventsToReader(t, events)
	_, lastMsg := parseQwenEvents(r, t.TempDir())

	if lastMsg != "actual response" {
		t.Errorf("system events should not affect lastMsg, got %q", lastMsg)
	}
}

func TestQwenRunner_Name(t *testing.T) {
	r := NewQwenRunner(0)
	if r.Name() != "qwen" {
		t.Errorf("expected name 'qwen', got %q", r.Name())
	}
}

func qwenEventsToReader(t *testing.T, events []qwenEvent) *strings.Reader {
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
