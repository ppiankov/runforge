package reporter

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/runforge/internal/task"
)

func TestTextReporter_PrintHeader(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf, false)
	r.PrintHeader(10, 4)

	out := buf.String()
	if !strings.Contains(out, "10 tasks") {
		t.Errorf("expected '10 tasks' in output, got: %s", out)
	}
	if !strings.Contains(out, "4 workers") {
		t.Errorf("expected '4 workers' in output, got: %s", out)
	}
}

func TestTextReporter_PrintDryRun(t *testing.T) {
	tasks := []task.Task{
		{ID: "t1", Repo: "org/r", Priority: 1, Title: "First", Prompt: "do first thing"},
		{ID: "t2", Repo: "org/r", Priority: 2, DependsOn: []string{"t1"}, Title: "Second", Prompt: "do second thing"},
	}

	g, err := task.BuildGraph(tasks)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	r := NewTextReporter(&buf, false)
	r.PrintDryRun(g, "/repos")

	out := buf.String()
	if !strings.Contains(out, "t1") {
		t.Error("expected t1 in dry run output")
	}
	if !strings.Contains(out, "t2") {
		t.Error("expected t2 in dry run output")
	}
	if !strings.Contains(out, "(after t1)") {
		t.Error("expected dependency note for t2")
	}
}

func TestTextReporter_PrintStatus(t *testing.T) {
	tasks := []task.Task{
		{ID: "running", Repo: "org/r", Priority: 1, Title: "Running", Prompt: "a"},
		{ID: "done", Repo: "org/r", Priority: 1, Title: "Done", Prompt: "b"},
		{ID: "fail", Repo: "org/r", Priority: 1, Title: "Fail", Prompt: "c"},
	}

	g, err := task.BuildGraph(tasks)
	if err != nil {
		t.Fatal(err)
	}

	results := map[string]*task.TaskResult{
		"running": {TaskID: "running", State: task.StateRunning, StartedAt: time.Now().Add(-30 * time.Second)},
		"done":    {TaskID: "done", State: task.StateCompleted, Duration: 45 * time.Second},
		"fail":    {TaskID: "fail", State: task.StateFailed, Duration: 10 * time.Second, Error: "boom"},
	}

	var buf bytes.Buffer
	r := NewTextReporter(&buf, false)
	r.PrintStatus(g, results)

	out := buf.String()
	if !strings.Contains(out, "RUNNING") {
		t.Error("expected RUNNING section")
	}
	if !strings.Contains(out, "COMPLETED") {
		t.Error("expected COMPLETED section")
	}
	if !strings.Contains(out, "FAILED") {
		t.Error("expected FAILED section")
	}
}

func TestTextReporter_PrintSummary(t *testing.T) {
	report := &task.RunReport{
		TotalTasks:    10,
		Completed:     7,
		Failed:        1,
		Skipped:       2,
		TotalDuration: 5 * time.Minute,
	}

	var buf bytes.Buffer
	r := NewTextReporter(&buf, false)
	r.PrintSummary(report)

	out := buf.String()
	if !strings.Contains(out, "Completed: 7") {
		t.Error("expected completed count")
	}
	if !strings.Contains(out, "Failed: 1") {
		t.Error("expected failed count")
	}
}

func TestTextReporter_NoColor(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf, false)
	r.PrintHeader(5, 2)

	out := buf.String()
	if strings.Contains(out, "\033[") {
		t.Error("expected no ANSI codes when color is false")
	}
}

func TestWriteJSONReport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")

	report := &task.RunReport{
		Timestamp:  time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC),
		TasksFiles: []string{"tasks.json"},
		Workers:    4,
		TotalTasks: 3,
		Completed:  2,
		Failed:     1,
		Results: map[string]*task.TaskResult{
			"t1": {TaskID: "t1", State: task.StateCompleted},
			"t2": {TaskID: "t2", State: task.StateFailed, Error: "oops"},
		},
	}

	if err := WriteJSONReport(report, path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}

	var loaded task.RunReport
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("parse report: %v", err)
	}

	if loaded.TotalTasks != 3 {
		t.Errorf("expected 3 total tasks, got %d", loaded.TotalTasks)
	}
	if loaded.Completed != 2 {
		t.Errorf("expected 2 completed, got %d", loaded.Completed)
	}
}
