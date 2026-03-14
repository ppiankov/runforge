package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareCodexHome_IsolatesSystemSkills(t *testing.T) {
	sharedHome := t.TempDir()
	sharedSkills := filepath.Join(sharedHome, "skills")
	if err := os.MkdirAll(filepath.Join(sharedSkills, ".system"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sharedSkills, "workledger"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sharedHome, "vendor_imports"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"auth.json", "config.toml", "AGENTS.md"} {
		if err := os.WriteFile(filepath.Join(sharedHome, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("CODEX_HOME", sharedHome)

	isolatedHome, err := prepareCodexHome(t.TempDir())
	if err != nil {
		t.Fatalf("prepareCodexHome: %v", err)
	}

	for _, name := range []string{"auth.json", "config.toml", "AGENTS.md"} {
		path := filepath.Join(isolatedHome, name)
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("expected %s link: %v", name, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s should be a symlink", name)
		}
	}

	if _, err := os.Lstat(filepath.Join(isolatedHome, "skills", "workledger")); err != nil {
		t.Fatalf("expected user skill link: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(isolatedHome, "vendor_imports")); err != nil {
		t.Fatalf("expected vendor_imports link: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(isolatedHome, "skills", ".system")); !os.IsNotExist(err) {
		t.Fatalf("expected isolated skills to exclude .system, got err=%v", err)
	}
}

func TestAppendOrReplaceEnv(t *testing.T) {
	env := []string{"PATH=/usr/bin", "CODEX_HOME=/old"}
	got := appendOrReplaceEnv(env, "CODEX_HOME", "/new")
	if got[1] != "CODEX_HOME=/new" {
		t.Fatalf("expected CODEX_HOME replacement, got %v", got)
	}

	got = appendOrReplaceEnv([]string{"PATH=/usr/bin"}, "CODEX_HOME", "/new")
	if got[len(got)-1] != "CODEX_HOME=/new" {
		t.Fatalf("expected CODEX_HOME append, got %v", got)
	}
}

func TestParseEvents_Success(t *testing.T) {
	events := []Event{
		{Type: EventThreadStarted},
		{Type: EventItemCompleted, Item: &Item{Type: "reasoning", Content: "thinking..."}},
		{Type: EventItemCompleted, Item: &Item{Type: "command_execution", Command: "go test", Status: "success"}},
		{Type: EventItemCompleted, Item: &Item{Type: "agent_message", Content: "All tests pass."}},
		{Type: EventTurnCompleted},
	}

	r := eventsToReader(t, events)
	failed, lastMsg, _ := parseEvents(r, t.TempDir())

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
	failed, _, _ := parseEvents(r, t.TempDir())

	if !failed {
		t.Error("expected failure, got success")
	}
}

func TestParseEvents_EmptyInput(t *testing.T) {
	r := strings.NewReader("")
	failed, lastMsg, _ := parseEvents(r, t.TempDir())

	if failed {
		t.Error("empty input should not be failed")
	}
	if lastMsg != "" {
		t.Errorf("expected empty last message, got %q", lastMsg)
	}
}

func TestParseEvents_InvalidJSON(t *testing.T) {
	r := strings.NewReader("{invalid\n{also invalid\n")
	failed, lastMsg, _ := parseEvents(r, t.TempDir())

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
	_, lastMsg, _ := parseEvents(r, t.TempDir())

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
