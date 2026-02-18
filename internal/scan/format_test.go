package scan

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func sampleResult() *ScanResult {
	return &ScanResult{
		ReposScanned: []string{"app1", "app2"},
		Findings: []Finding{
			{Repo: "app1", Check: "missing-ci", Category: "structure", Severity: SeverityCritical, Message: "No CI pipeline found", Suggestion: "Create ci.yml"},
			{Repo: "app1", Check: "missing-readme", Category: "structure", Severity: SeverityWarning, Message: "No README.md found", Suggestion: "Create README.md"},
			{Repo: "app2", Check: "go-no-tests", Category: "go", Severity: SeverityCritical, Message: "No Go test files found", Suggestion: "Add tests"},
			{Repo: "app2", Check: "missing-changelog", Category: "structure", Severity: SeverityInfo, Message: "No CHANGELOG.md found", Suggestion: "Add changelog"},
		},
		Skipped: []string{"skipped-dir"},
	}
}

func TestTextFormatter_NoColor(t *testing.T) {
	var buf bytes.Buffer
	f := NewTextFormatter(false)
	if err := f.Format(&buf, sampleResult()); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "runforge scan") {
		t.Error("missing header")
	}
	if !strings.Contains(out, "2 repos") {
		t.Error("missing repo count")
	}
	if !strings.Contains(out, "4 findings") {
		t.Error("missing finding count")
	}
	if !strings.Contains(out, "app1") {
		t.Error("missing app1 section")
	}
	if !strings.Contains(out, "app2") {
		t.Error("missing app2 section")
	}
	if !strings.Contains(out, "CRITICAL") {
		t.Error("missing critical label")
	}
	if !strings.Contains(out, "1 skipped") {
		t.Error("missing skipped count")
	}
	// no ANSI codes when color disabled
	if strings.Contains(out, "\033[") {
		t.Error("ANSI codes present with color disabled")
	}
}

func TestTextFormatter_Color(t *testing.T) {
	var buf bytes.Buffer
	f := NewTextFormatter(true)
	if err := f.Format(&buf, sampleResult()); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "\033[") {
		t.Error("no ANSI codes with color enabled")
	}
}

func TestTextFormatter_EmptyResult(t *testing.T) {
	var buf bytes.Buffer
	f := NewTextFormatter(false)
	if err := f.Format(&buf, &ScanResult{}); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "0 repos") {
		t.Error("missing 0 repos in empty result")
	}
}

func TestJSONFormatter(t *testing.T) {
	var buf bytes.Buffer
	f := NewJSONFormatter()
	if err := f.Format(&buf, sampleResult()); err != nil {
		t.Fatal(err)
	}

	var decoded ScanResult
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	if len(decoded.ReposScanned) != 2 {
		t.Errorf("ReposScanned = %d, want 2", len(decoded.ReposScanned))
	}
	if len(decoded.Findings) != 4 {
		t.Errorf("Findings = %d, want 4", len(decoded.Findings))
	}
	if len(decoded.Skipped) != 1 {
		t.Errorf("Skipped = %d, want 1", len(decoded.Skipped))
	}
}

func TestJSONFormatter_FindingFields(t *testing.T) {
	var buf bytes.Buffer
	f := NewJSONFormatter()
	result := &ScanResult{
		ReposScanned: []string{"myrepo"},
		Findings: []Finding{{
			Repo: "myrepo", Check: "missing-ci", Category: "structure",
			Severity: SeverityCritical, Message: "No CI", Suggestion: "Add CI",
		}},
	}
	if err := f.Format(&buf, result); err != nil {
		t.Fatal(err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}

	findings := decoded["findings"].([]interface{})
	finding := findings[0].(map[string]interface{})
	if finding["repo"] != "myrepo" {
		t.Errorf("repo = %v", finding["repo"])
	}
	if finding["check"] != "missing-ci" {
		t.Errorf("check = %v", finding["check"])
	}
	if finding["severity"].(float64) != float64(SeverityCritical) {
		t.Errorf("severity = %v", finding["severity"])
	}
}

func TestTaskFormatter_GeneratesTasks(t *testing.T) {
	var buf bytes.Buffer
	f := NewTaskFormatter("myorg", "codex")
	if err := f.Format(&buf, sampleResult()); err != nil {
		t.Fatal(err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}

	tasks := decoded["tasks"].([]interface{})
	// only critical and warning, not info
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks (skip info), got %d", len(tasks))
	}

	first := tasks[0].(map[string]interface{})
	if !strings.Contains(first["repo"].(string), "myorg/") {
		t.Errorf("task repo missing owner: %q", first["repo"])
	}
}

func TestTaskFormatter_DefaultOwner(t *testing.T) {
	var buf bytes.Buffer
	f := NewTaskFormatter("", "codex")
	result := &ScanResult{
		ReposScanned: []string{"r"},
		Findings: []Finding{{
			Repo: "r", Check: "x", Category: "c",
			Severity: SeverityCritical, Message: "m", Suggestion: "s",
		}},
	}
	if err := f.Format(&buf, result); err != nil {
		t.Fatal(err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}

	tasks := decoded["tasks"].([]interface{})
	task := tasks[0].(map[string]interface{})
	if task["repo"] != "ppiankov/r" {
		t.Errorf("default owner not applied: %q", task["repo"])
	}
}

func TestTaskFormatter_Priority(t *testing.T) {
	var buf bytes.Buffer
	f := NewTaskFormatter("o", "r")
	result := &ScanResult{
		ReposScanned: []string{"r"},
		Findings: []Finding{
			{Repo: "r", Check: "a", Category: "c", Severity: SeverityCritical, Message: "m", Suggestion: "s"},
			{Repo: "r", Check: "b", Category: "c", Severity: SeverityWarning, Message: "m", Suggestion: "s"},
		},
	}
	if err := f.Format(&buf, result); err != nil {
		t.Fatal(err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}

	tasks := decoded["tasks"].([]interface{})
	t0 := tasks[0].(map[string]interface{})
	t1 := tasks[1].(map[string]interface{})
	if t0["priority"].(float64) != 1 {
		t.Errorf("critical task priority = %v, want 1", t0["priority"])
	}
	if t1["priority"].(float64) != 2 {
		t.Errorf("warning task priority = %v, want 2", t1["priority"])
	}
}

func TestTaskFormatter_InfoOnly(t *testing.T) {
	var buf bytes.Buffer
	f := NewTaskFormatter("o", "r")
	result := &ScanResult{
		ReposScanned: []string{"r"},
		Findings: []Finding{
			{Repo: "r", Check: "a", Category: "c", Severity: SeverityInfo, Message: "m", Suggestion: "s"},
		},
	}
	if err := f.Format(&buf, result); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "No actionable findings") {
		t.Errorf("expected 'No actionable findings' message, got: %s", out)
	}
}

func TestTaskFormatter_EmptyFindings(t *testing.T) {
	var buf bytes.Buffer
	f := NewTaskFormatter("o", "r")
	if err := f.Format(&buf, &ScanResult{}); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "No actionable findings") {
		t.Errorf("expected 'No actionable findings' for empty result, got: %s", out)
	}
}

func TestTaskFormatter_UsesPromptField(t *testing.T) {
	var buf bytes.Buffer
	f := NewTaskFormatter("o", "codex")
	result := &ScanResult{
		ReposScanned: []string{"r"},
		Findings: []Finding{{
			Repo: "r", Check: "x", Category: "c",
			Severity:   SeverityCritical,
			Message:    "problem",
			Suggestion: "short fix",
			Prompt:     "detailed multi-line autonomous prompt with verification",
		}},
	}
	if err := f.Format(&buf, result); err != nil {
		t.Fatal(err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}

	tasks := decoded["tasks"].([]interface{})
	task := tasks[0].(map[string]interface{})
	if task["prompt"] != "detailed multi-line autonomous prompt with verification" {
		t.Errorf("expected Prompt field to be used, got: %q", task["prompt"])
	}
}

func TestTaskFormatter_FallsBackToSuggestion(t *testing.T) {
	var buf bytes.Buffer
	f := NewTaskFormatter("o", "codex")
	result := &ScanResult{
		ReposScanned: []string{"r"},
		Findings: []Finding{{
			Repo: "r", Check: "x", Category: "c",
			Severity:   SeverityCritical,
			Message:    "problem",
			Suggestion: "short fix",
			// Prompt intentionally empty
		}},
	}
	if err := f.Format(&buf, result); err != nil {
		t.Fatal(err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}

	tasks := decoded["tasks"].([]interface{})
	task := tasks[0].(map[string]interface{})
	if task["prompt"] != "short fix" {
		t.Errorf("expected Suggestion fallback, got: %q", task["prompt"])
	}
}

func TestTaskPrompt_PreferPrompt(t *testing.T) {
	f := Finding{Suggestion: "short", Prompt: "detailed"}
	if got := f.TaskPrompt(); got != "detailed" {
		t.Errorf("TaskPrompt() = %q, want %q", got, "detailed")
	}
}

func TestTaskPrompt_FallbackToSuggestion(t *testing.T) {
	f := Finding{Suggestion: "short"}
	if got := f.TaskPrompt(); got != "short" {
		t.Errorf("TaskPrompt() = %q, want %q", got, "short")
	}
}
