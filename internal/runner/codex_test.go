package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEvents_Success(t *testing.T) {
	events := []Event{
		{Type: EventThreadStarted},
		{Type: EventItemCompleted, Item: &Item{Type: "reasoning", Content: "thinking..."}},
		{Type: EventItemCompleted, Item: &Item{Type: "command_execution", Command: "go test", Status: "success"}},
		{Type: EventItemCompleted, Item: &Item{Type: "agent_message", Content: "All tests pass."}},
		{Type: EventTurnCompleted},
	}

	r := eventsToReader(t, events)
	failed, lastMsg := parseEvents(r, t.TempDir())

	if failed {
		t.Error("expected success, got failed")
	}
	if lastMsg != "All tests pass." {
		t.Errorf("expected last message 'All tests pass.', got %q", lastMsg)
	}
}

func TestParseEvents_Failure(t *testing.T) {
	events := []Event{
		{Type: EventThreadStarted},
		{Type: EventItemCompleted, Item: &Item{Type: "command_execution", Command: "go test", Status: "failure"}},
		{Type: EventTurnFailed},
	}

	r := eventsToReader(t, events)
	failed, _ := parseEvents(r, t.TempDir())

	if !failed {
		t.Error("expected failure, got success")
	}
}

func TestParseEvents_EmptyInput(t *testing.T) {
	r := strings.NewReader("")
	failed, lastMsg := parseEvents(r, t.TempDir())

	if failed {
		t.Error("empty input should not be failed")
	}
	if lastMsg != "" {
		t.Errorf("expected empty last message, got %q", lastMsg)
	}
}

func TestParseEvents_InvalidJSON(t *testing.T) {
	r := strings.NewReader("{invalid\n{also invalid\n")
	failed, lastMsg := parseEvents(r, t.TempDir())

	if failed {
		t.Error("invalid json should not trigger failure")
	}
	if lastMsg != "" {
		t.Error("expected empty last message for invalid json")
	}
}

func TestParseEvents_MultipleMessages(t *testing.T) {
	events := []Event{
		{Type: EventItemCompleted, Item: &Item{Type: "agent_message", Content: "First"}},
		{Type: EventItemCompleted, Item: &Item{Type: "agent_message", Content: "Second"}},
		{Type: EventItemCompleted, Item: &Item{Type: "agent_message", Content: "Final answer"}},
		{Type: EventTurnCompleted},
	}

	r := eventsToReader(t, events)
	_, lastMsg := parseEvents(r, t.TempDir())

	if lastMsg != "Final answer" {
		t.Errorf("expected 'Final answer', got %q", lastMsg)
	}
}

func TestParseEvents_WritesEventsFile(t *testing.T) {
	dir := t.TempDir()
	events := []Event{
		{Type: EventThreadStarted},
		{Type: EventTurnCompleted},
	}

	r := eventsToReader(t, events)
	parseEvents(r, dir)

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

func TestParseEventsFromFile(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")

	events := []Event{
		{Type: EventItemCompleted, Item: &Item{Type: "agent_message", Content: "done"}},
		{Type: EventTurnCompleted},
	}

	var lines []string
	for _, ev := range events {
		b, _ := json.Marshal(ev)
		lines = append(lines, string(b))
	}
	if err := os.WriteFile(eventsPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	failed, lastMsg, err := ParseEventsFromFile(eventsPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if failed {
		t.Error("expected success")
	}
	if lastMsg != "done" {
		t.Errorf("expected 'done', got %q", lastMsg)
	}
}

func TestParseEventsFromFile_NotFound(t *testing.T) {
	_, _, err := ParseEventsFromFile("/nonexistent/events.jsonl")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func eventsToReader(t *testing.T, events []Event) *strings.Reader {
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
