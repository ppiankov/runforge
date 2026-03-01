package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseClineEvents_Success(t *testing.T) {
	events := []clineEvent{
		{Type: "say", Say: "api_req_started", Text: "starting request", TS: 1000},
		{Type: "say", Say: "text", Text: "All done.", TS: 2000},
	}

	r := clineEventsToReader(t, events)
	failed, lastMsg := parseClineEvents(r, t.TempDir())

	if failed {
		t.Error("expected success, got failed")
	}
	if lastMsg != "All done." {
		t.Errorf("expected last message 'All done.', got %q", lastMsg)
	}
}

func TestParseClineEvents_LastMessage(t *testing.T) {
	events := []clineEvent{
		{Type: "say", Say: "text", Text: "First response", TS: 1000},
		{Type: "say", Say: "text", Text: "Second response", TS: 2000},
		{Type: "say", Say: "text", Text: "Final answer", TS: 3000},
	}

	r := clineEventsToReader(t, events)
	_, lastMsg := parseClineEvents(r, t.TempDir())

	if lastMsg != "Final answer" {
		t.Errorf("expected 'Final answer', got %q", lastMsg)
	}
}

func TestParseClineEvents_EmptyInput(t *testing.T) {
	r := strings.NewReader("")
	failed, lastMsg := parseClineEvents(r, t.TempDir())

	if failed {
		t.Error("cline uses exit codes for failure, not empty events")
	}
	if lastMsg != "" {
		t.Errorf("expected empty last message, got %q", lastMsg)
	}
}

func TestParseClineEvents_InvalidJSON(t *testing.T) {
	r := strings.NewReader("{invalid\n{also invalid\n")
	failed, lastMsg := parseClineEvents(r, t.TempDir())

	if failed {
		t.Error("invalid json should not trigger failure")
	}
	if lastMsg != "" {
		t.Error("expected empty last message for invalid json")
	}
}

func TestParseClineEvents_EventsPersisted(t *testing.T) {
	dir := t.TempDir()
	events := []clineEvent{
		{Type: "say", Say: "api_req_started", Text: "starting", TS: 1000},
		{Type: "say", Say: "text", Text: "hello", TS: 2000},
		{Type: "ask", Ask: "plan_mode_respond", Text: "approve?", TS: 3000},
	}

	r := clineEventsToReader(t, events)
	parseClineEvents(r, dir)

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

func TestParseClineEvents_AskEventsIgnored(t *testing.T) {
	events := []clineEvent{
		{Type: "say", Say: "text", Text: "real message", TS: 1000},
		{Type: "ask", Ask: "plan_mode_respond", Text: "should not be last msg", TS: 2000},
	}

	r := clineEventsToReader(t, events)
	_, lastMsg := parseClineEvents(r, t.TempDir())

	if lastMsg != "real message" {
		t.Errorf("ask events should not affect lastMsg, got %q", lastMsg)
	}
}

func TestParseClineEvents_ReasoningIgnored(t *testing.T) {
	events := []clineEvent{
		{Type: "say", Say: "reasoning", Text: "thinking...", TS: 1000},
		{Type: "say", Say: "text", Text: "actual response", TS: 2000},
		{Type: "say", Say: "reasoning", Text: "more thinking", TS: 3000},
	}

	r := clineEventsToReader(t, events)
	_, lastMsg := parseClineEvents(r, t.TempDir())

	if lastMsg != "actual response" {
		t.Errorf("reasoning events should not affect lastMsg, got %q", lastMsg)
	}
}

func TestClineRunner_Name(t *testing.T) {
	r := NewClineRunner(0)
	if r.Name() != "cline" {
		t.Errorf("expected name 'cline', got %q", r.Name())
	}
}

func clineEventsToReader(t *testing.T, events []clineEvent) *strings.Reader {
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
