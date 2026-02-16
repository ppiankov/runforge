package reporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ppiankov/runforge/internal/task"
)

func buildTestGraph(t *testing.T, tasks []task.Task) *task.Graph {
	t.Helper()
	g, err := task.BuildGraph(tasks)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestWriteSARIFReport_FailedTasks(t *testing.T) {
	tasks := []task.Task{
		{ID: "t1", Repo: "org/repo-a", Priority: 1, Title: "Build"},
		{ID: "t2", Repo: "org/repo-b", Priority: 1, Title: "Test"},
	}
	graph := buildTestGraph(t, tasks)

	report := &task.RunReport{
		Results: map[string]*task.TaskResult{
			"t1": {TaskID: "t1", State: task.StateCompleted},
			"t2": {TaskID: "t2", State: task.StateFailed, Error: "codex turn.failed event detected"},
		},
	}

	path := filepath.Join(t.TempDir(), "report.sarif")
	if err := WriteSARIFReport(report, graph, path); err != nil {
		t.Fatal(err)
	}

	sarif := readSARIF(t, path)
	if len(sarif.Runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(sarif.Runs))
	}
	results := sarif.Runs[0].Results
	if len(results) != 1 {
		t.Fatalf("expected 1 result (failed only), got %d", len(results))
	}
	if results[0].RuleID != "t2" {
		t.Errorf("expected ruleId 't2', got %q", results[0].RuleID)
	}
	if results[0].Level != "error" {
		t.Errorf("expected level 'error', got %q", results[0].Level)
	}
	if results[0].Message.Text != "codex turn.failed event detected" {
		t.Errorf("unexpected message: %q", results[0].Message.Text)
	}
	if len(results[0].Locations) != 1 || results[0].Locations[0].PhysicalLocation.ArtifactLocation.URI != "org/repo-b" {
		t.Error("expected artifact location with repo URI")
	}
}

func TestWriteSARIFReport_SkippedTasks(t *testing.T) {
	tasks := []task.Task{
		{ID: "t1", Repo: "org/r", Priority: 1, Title: "A"},
	}
	graph := buildTestGraph(t, tasks)

	report := &task.RunReport{
		Results: map[string]*task.TaskResult{
			"t1": {TaskID: "t1", State: task.StateSkipped, Error: "dependency failed"},
		},
	}

	path := filepath.Join(t.TempDir(), "report.sarif")
	if err := WriteSARIFReport(report, graph, path); err != nil {
		t.Fatal(err)
	}

	sarif := readSARIF(t, path)
	results := sarif.Runs[0].Results
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Level != "warning" {
		t.Errorf("expected level 'warning' for skipped, got %q", results[0].Level)
	}
}

func TestWriteSARIFReport_RateLimitedTasks(t *testing.T) {
	tasks := []task.Task{
		{ID: "t1", Repo: "org/r", Priority: 1, Title: "A"},
	}
	graph := buildTestGraph(t, tasks)

	report := &task.RunReport{
		Results: map[string]*task.TaskResult{
			"t1": {TaskID: "t1", State: task.StateRateLimited, Error: "rate limit reached"},
		},
	}

	path := filepath.Join(t.TempDir(), "report.sarif")
	if err := WriteSARIFReport(report, graph, path); err != nil {
		t.Fatal(err)
	}

	sarif := readSARIF(t, path)
	results := sarif.Runs[0].Results
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Level != "warning" {
		t.Errorf("expected level 'warning' for rate-limited, got %q", results[0].Level)
	}
}

func TestWriteSARIFReport_NoFailures(t *testing.T) {
	tasks := []task.Task{
		{ID: "t1", Repo: "org/r", Priority: 1, Title: "A"},
		{ID: "t2", Repo: "org/r", Priority: 1, Title: "B"},
	}
	graph := buildTestGraph(t, tasks)

	report := &task.RunReport{
		Results: map[string]*task.TaskResult{
			"t1": {TaskID: "t1", State: task.StateCompleted},
			"t2": {TaskID: "t2", State: task.StateCompleted},
		},
	}

	path := filepath.Join(t.TempDir(), "report.sarif")
	if err := WriteSARIFReport(report, graph, path); err != nil {
		t.Fatal(err)
	}

	sarif := readSARIF(t, path)
	results := sarif.Runs[0].Results
	if len(results) != 0 {
		t.Errorf("expected 0 results for all-completed, got %d", len(results))
	}
}

func TestWriteSARIFReport_ValidStructure(t *testing.T) {
	tasks := []task.Task{
		{ID: "t1", Repo: "org/r", Priority: 1, Title: "A"},
	}
	graph := buildTestGraph(t, tasks)

	report := &task.RunReport{
		Results: map[string]*task.TaskResult{
			"t1": {TaskID: "t1", State: task.StateFailed, Error: "failed"},
		},
	}

	path := filepath.Join(t.TempDir(), "report.sarif")
	if err := WriteSARIFReport(report, graph, path); err != nil {
		t.Fatal(err)
	}

	sarif := readSARIF(t, path)
	if sarif.Schema != sarifSchema {
		t.Errorf("expected schema %q, got %q", sarifSchema, sarif.Schema)
	}
	if sarif.Version != "2.1.0" {
		t.Errorf("expected version '2.1.0', got %q", sarif.Version)
	}
	if sarif.Runs[0].Tool.Driver.Name != "runforge" {
		t.Errorf("expected tool name 'runforge', got %q", sarif.Runs[0].Tool.Driver.Name)
	}
}

func readSARIF(t *testing.T, path string) sarifReport {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sarif: %v", err)
	}
	var s sarifReport
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal sarif: %v", err)
	}
	return s
}
