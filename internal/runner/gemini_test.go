package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGeminiEvents_Success(t *testing.T) {
	events := []geminiEvent{
		{Type: "init"},
		{Type: "message", Role: "assistant", Content: "All done."},
		{Type: "result", Status: "success"},
	}

	r := geminiEventsToReader(t, events)
	failed, lastMsg := parseGeminiEvents(r, t.TempDir())

	if failed {
		t.Error("expected success, got failed")
	}
	if lastMsg != "All done." {
		t.Errorf("expected last message 'All done.', got %q", lastMsg)
	}
}

func TestParseGeminiEvents_Failure(t *testing.T) {
	events := []geminiEvent{
		{Type: "init"},
		{Type: "message", Role: "assistant", Content: "Something went wrong."},
		{Type: "result", Status: "error"},
	}

	r := geminiEventsToReader(t, events)
	failed, _ := parseGeminiEvents(r, t.TempDir())

	if !failed {
		t.Error("expected failure, got success")
	}
}

func TestParseGeminiEvents_LastMessage(t *testing.T) {
	events := []geminiEvent{
		{Type: "message", Role: "assistant", Content: "First response"},
		{Type: "message", Role: "assistant", Content: "Second response"},
		{Type: "message", Role: "assistant", Content: "Final answer"},
		{Type: "result", Status: "success"},
	}

	r := geminiEventsToReader(t, events)
	_, lastMsg := parseGeminiEvents(r, t.TempDir())

	if lastMsg != "Final answer" {
		t.Errorf("expected 'Final answer', got %q", lastMsg)
	}
}

func TestParseGeminiEvents_EmptyInput(t *testing.T) {
	r := strings.NewReader("")
	failed, lastMsg := parseGeminiEvents(r, t.TempDir())

	if failed {
		t.Error("empty input should not be failed")
	}
	if lastMsg != "" {
		t.Errorf("expected empty last message, got %q", lastMsg)
	}
}

func TestParseGeminiEvents_InvalidJSON(t *testing.T) {
	r := strings.NewReader("{invalid\n{also invalid\n")
	failed, lastMsg := parseGeminiEvents(r, t.TempDir())

	if failed {
		t.Error("invalid json should not trigger failure")
	}
	if lastMsg != "" {
		t.Error("expected empty last message for invalid json")
	}
}

func TestParseGeminiEvents_EventsPersisted(t *testing.T) {
	dir := t.TempDir()
	events := []geminiEvent{
		{Type: "init"},
		{Type: "message", Role: "assistant", Content: "hello"},
		{Type: "result", Status: "success"},
	}

	r := geminiEventsToReader(t, events)
	parseGeminiEvents(r, dir)

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

func TestParseGeminiEvents_DeltaMessages(t *testing.T) {
	// gemini sends delta:true for streaming fragments — content still accumulates
	events := []geminiEvent{
		{Type: "init"},
		{Type: "message", Role: "assistant", Content: "partial answer"},
		{Type: "message", Role: "assistant", Content: "complete answer"},
		{Type: "result", Status: "success"},
	}

	r := geminiEventsToReader(t, events)
	failed, lastMsg := parseGeminiEvents(r, t.TempDir())

	if failed {
		t.Error("expected success")
	}
	if lastMsg != "complete answer" {
		t.Errorf("expected 'complete answer', got %q", lastMsg)
	}
}

func TestParseGeminiEvents_UserMessageIgnored(t *testing.T) {
	events := []geminiEvent{
		{Type: "message", Role: "user", Content: "do something"},
		{Type: "message", Role: "assistant", Content: "done"},
		{Type: "result", Status: "success"},
	}

	r := geminiEventsToReader(t, events)
	_, lastMsg := parseGeminiEvents(r, t.TempDir())

	if lastMsg != "done" {
		t.Errorf("expected 'done', got %q", lastMsg)
	}
}

func geminiEventsToReader(t *testing.T, events []geminiEvent) *strings.Reader {
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
