package reporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ppiankov/runforge/internal/runner"
)

// mustMkdirAll is a test helper that creates directories or fails the test.
func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
}

// mustWriteFile is a test helper that writes a file or fails the test.
func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func TestDiscoverTasks(t *testing.T) {
	dir := t.TempDir()

	mustMkdirAll(t, filepath.Join(dir, "task-A"))
	mustMkdirAll(t, filepath.Join(dir, "task-B"))
	mustWriteFile(t, filepath.Join(dir, "report.json"), []byte("{}"))

	wr := NewWatchReporter(os.Stdout, false, dir)
	if err := wr.discoverTasks(); err != nil {
		t.Fatalf("discoverTasks: %v", err)
	}

	if len(wr.snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(wr.snapshots))
	}
	if wr.snapshots["task-A"] == nil || wr.snapshots["task-B"] == nil {
		t.Fatalf("missing expected task snapshots: %v", wr.snapshots)
	}
	if wr.snapshots["task-A"].State != "queued" {
		t.Errorf("expected state queued, got %s", wr.snapshots["task-A"].State)
	}
}

func TestDiscoverTasks_WithEventsFile(t *testing.T) {
	dir := t.TempDir()
	taskDir := filepath.Join(dir, "task-C")
	mustMkdirAll(t, taskDir)
	mustWriteFile(t, filepath.Join(taskDir, "events.jsonl"), []byte(`{"type":"thread.started"}`+"\n"))

	wr := NewWatchReporter(os.Stdout, false, dir)
	if err := wr.discoverTasks(); err != nil {
		t.Fatalf("discoverTasks: %v", err)
	}

	snap := wr.snapshots["task-C"]
	if snap == nil {
		t.Fatal("missing task-C snapshot")
	}
	if snap.State != "running" {
		t.Errorf("expected state running (events.jsonl exists), got %s", snap.State)
	}
	if snap.StartedAt.IsZero() {
		t.Error("expected StartedAt to be set from events.jsonl mtime")
	}
}

func TestReadNewEvents_Incremental(t *testing.T) {
	dir := t.TempDir()
	evPath := filepath.Join(dir, "events.jsonl")

	var lines []string
	for i := 0; i < 5; i++ {
		ev := runner.Event{Type: runner.EventItemCompleted, Item: &runner.Item{Type: "reasoning"}}
		b, _ := json.Marshal(ev)
		lines = append(lines, string(b))
	}
	mustWriteFile(t, evPath, []byte(strings.Join(lines, "\n")+"\n"))

	snap := &TaskSnapshot{ID: "test", State: "queued"}
	wr := NewWatchReporter(os.Stdout, false, dir)
	wr.readNewEvents(snap, evPath)

	if snap.EventCount != 5 {
		t.Fatalf("expected 5 events, got %d", snap.EventCount)
	}
	if snap.fileOffset == 0 {
		t.Fatal("expected non-zero file offset")
	}

	// append 3 more events
	f, err := os.OpenFile(evPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	for i := 0; i < 3; i++ {
		ev := runner.Event{Type: runner.EventItemCompleted, Item: &runner.Item{Type: "command_execution", Command: "go test"}}
		b, _ := json.Marshal(ev)
		if _, err := f.Write(append(b, '\n')); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	wr.readNewEvents(snap, evPath)

	if snap.EventCount != 8 {
		t.Fatalf("expected 8 events after append, got %d", snap.EventCount)
	}
	if snap.LastAction != "cmd: go test" {
		t.Errorf("expected last action 'cmd: go test', got %q", snap.LastAction)
	}
}

func TestProcessEvent_StateTransitions(t *testing.T) {
	tests := []struct {
		name      string
		event     runner.Event
		wantState string
		wantFail  bool
	}{
		{
			name:      "thread started",
			event:     runner.Event{Type: runner.EventThreadStarted},
			wantState: "running",
		},
		{
			name:      "item started",
			event:     runner.Event{Type: runner.EventItemStarted, Item: &runner.Item{Type: "command_execution", Command: "ls"}},
			wantState: "running",
		},
		{
			name:      "item completed",
			event:     runner.Event{Type: runner.EventItemCompleted, Item: &runner.Item{Type: "reasoning"}},
			wantState: "running",
		},
		{
			name:      "turn completed",
			event:     runner.Event{Type: runner.EventTurnCompleted},
			wantState: "completed",
		},
		{
			name:      "turn failed",
			event:     runner.Event{Type: runner.EventTurnFailed},
			wantState: "failed",
			wantFail:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := &TaskSnapshot{ID: "test", State: "queued"}
			processEvent(snap, &tt.event)

			if snap.State != tt.wantState {
				t.Errorf("state: got %q, want %q", snap.State, tt.wantState)
			}
			if snap.Failed != tt.wantFail {
				t.Errorf("failed: got %v, want %v", snap.Failed, tt.wantFail)
			}
		})
	}
}

func TestFormatAction(t *testing.T) {
	tests := []struct {
		name string
		item runner.Item
		want string
	}{
		{
			name: "simple command",
			item: runner.Item{Type: "command_execution", Command: "go test ./..."},
			want: "cmd: go test ./...",
		},
		{
			name: "shell prefix stripped",
			item: runner.Item{Type: "command_execution", Command: "/bin/zsh -lc 'go test ./...'"},
			want: "cmd: go test ./...",
		},
		{
			name: "long command truncated",
			item: runner.Item{Type: "command_execution", Command: "/bin/zsh -lc 'go test -race -cover -count=1 -v ./internal/very/long/package/path/...'"},
			want: "cmd: go test -race -cover -count=1 -v ./inter...",
		},
		{
			name: "reasoning",
			item: runner.Item{Type: "reasoning"},
			want: "reasoning",
		},
		{
			name: "agent message",
			item: runner.Item{Type: "agent_message"},
			want: "agent_message",
		},
		{
			name: "unknown type",
			item: runner.Item{Type: "file_change"},
			want: "file_change",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAction(&tt.item)
			if got != tt.want {
				t.Errorf("formatAction: got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunCompleted(t *testing.T) {
	dir := t.TempDir()
	wr := NewWatchReporter(os.Stdout, false, dir)

	if wr.runCompleted() {
		t.Error("expected false when no report.json")
	}

	mustWriteFile(t, filepath.Join(dir, "report.json"), []byte("{}"))

	if !wr.runCompleted() {
		t.Error("expected true when report.json exists")
	}
}

func TestBuildLines_Layout(t *testing.T) {
	dir := t.TempDir()
	wr := NewWatchReporter(os.Stdout, false, dir)

	wr.snapshots["task-running"] = &TaskSnapshot{ID: "task-running", State: "running", EventCount: 42, LastAction: "reasoning"}
	wr.snapshots["task-done"] = &TaskSnapshot{ID: "task-done", State: "completed", EventCount: 100, LastAction: "agent_message"}
	wr.snapshots["task-queued"] = &TaskSnapshot{ID: "task-queued", State: "queued"}
	wr.runStart = wr.earliestDirTime()

	lines := wr.buildLines()

	if len(lines) == 0 {
		t.Fatal("expected non-empty output")
	}

	if !strings.Contains(lines[0], "runforge watch") {
		t.Errorf("expected header to contain 'runforge watch', got: %s", lines[0])
	}

	found := false
	for _, line := range lines {
		if strings.Contains(line, "TASK") && strings.Contains(line, "STATE") && strings.Contains(line, "EVENTS") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected column header line with TASK, STATE, EVENTS")
	}

	lastLine := lines[len(lines)-1]
	if !strings.Contains(lastLine, "ctrl+c to quit") {
		t.Errorf("expected footer with 'ctrl+c to quit', got: %s", lastLine)
	}

	fullOutput := strings.Join(lines, "\n")
	if !strings.Contains(fullOutput, "task-running") {
		t.Error("expected running task in output")
	}
	if !strings.Contains(fullOutput, "task-done") {
		t.Error("expected completed task in output")
	}
	if !strings.Contains(fullOutput, "task-queued") {
		t.Error("expected queued task in output")
	}
}
