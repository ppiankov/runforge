package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseClaudeEvents_Success(t *testing.T) {
	events := []claudeEvent{
		{Type: "init"},
		{Type: "message", Role: "assistant", Content: []claudeContent{{Type: "text", Text: "All done."}}},
		{Type: "result", Status: "success"},
	}

	r := claudeEventsToReader(t, events)
	failed, lastMsg := parseClaudeEvents(r, t.TempDir())

	if failed {
		t.Error("expected success, got failed")
	}
	if lastMsg != "All done." {
		t.Errorf("expected last message 'All done.', got %q", lastMsg)
	}
}

func TestParseClaudeEvents_Failure(t *testing.T) {
	events := []claudeEvent{
		{Type: "init"},
		{Type: "message", Role: "assistant", Content: []claudeContent{{Type: "text", Text: "I encountered an error."}}},
		{Type: "result", Status: "error"},
	}

	r := claudeEventsToReader(t, events)
	failed, _ := parseClaudeEvents(r, t.TempDir())

	if !failed {
		t.Error("expected failure, got success")
	}
}

func TestParseClaudeEvents_LastMessage(t *testing.T) {
	events := []claudeEvent{
		{Type: "message", Role: "assistant", Content: []claudeContent{{Type: "text", Text: "First response"}}},
		{Type: "message", Role: "assistant", Content: []claudeContent{{Type: "text", Text: "Second response"}}},
		{Type: "message", Role: "assistant", Content: []claudeContent{{Type: "text", Text: "Final answer"}}},
		{Type: "result", Status: "success"},
	}

	r := claudeEventsToReader(t, events)
	_, lastMsg := parseClaudeEvents(r, t.TempDir())

	if lastMsg != "Final answer" {
		t.Errorf("expected 'Final answer', got %q", lastMsg)
	}
}

func TestParseClaudeEvents_EmptyInput(t *testing.T) {
	r := strings.NewReader("")
	failed, lastMsg := parseClaudeEvents(r, t.TempDir())

	if failed {
		t.Error("empty input should not be failed")
	}
	if lastMsg != "" {
		t.Errorf("expected empty last message, got %q", lastMsg)
	}
}

func TestParseClaudeEvents_InvalidJSON(t *testing.T) {
	r := strings.NewReader("{invalid\n{also invalid\n")
	failed, lastMsg := parseClaudeEvents(r, t.TempDir())

	if failed {
		t.Error("invalid json should not trigger failure")
	}
	if lastMsg != "" {
		t.Error("expected empty last message for invalid json")
	}
}

func TestParseClaudeEvents_EventsPersisted(t *testing.T) {
	dir := t.TempDir()
	events := []claudeEvent{
		{Type: "init"},
		{Type: "message", Role: "assistant", Content: []claudeContent{{Type: "text", Text: "hello"}}},
		{Type: "result", Status: "success"},
	}

	r := claudeEventsToReader(t, events)
	parseClaudeEvents(r, dir)

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

func TestParseClaudeEvents_ToolEvents(t *testing.T) {
	events := []claudeEvent{
		{Type: "init"},
		{Type: "tool_use"},
		{Type: "tool_result"},
		{Type: "message", Role: "assistant", Content: []claudeContent{{Type: "text", Text: "Used a tool and got result."}}},
		{Type: "result", Status: "success"},
	}

	r := claudeEventsToReader(t, events)
	failed, lastMsg := parseClaudeEvents(r, t.TempDir())

	if failed {
		t.Error("expected success")
	}
	if lastMsg != "Used a tool and got result." {
		t.Errorf("expected tool result message, got %q", lastMsg)
	}
}

func claudeEventsToReader(t *testing.T, events []claudeEvent) *strings.Reader {
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
