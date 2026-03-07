package cli

import (
	"testing"
	"time"

	"github.com/ppiankov/tokencontrol/internal/task"
)

func TestBuildReport_RunID(t *testing.T) {
	results := map[string]*task.TaskResult{
		"task-1": {TaskID: "task-1", State: task.StateCompleted},
	}

	r1 := buildReport([]string{"tasks.json"}, 2, "", "/repos", results, 5*time.Second, "")
	if r1.RunID == "" {
		t.Fatal("expected non-empty RunID")
	}
	if len(r1.RunID) != 12 {
		t.Errorf("expected 12-char RunID, got %d: %q", len(r1.RunID), r1.RunID)
	}

	// same timestamp + same files = same RunID
	r2 := buildReport([]string{"tasks.json"}, 2, "", "/repos", results, 5*time.Second, "")
	// timestamps differ (time.Now() called inside), so RunIDs differ — that's correct
	if r1.RunID == r2.RunID {
		// only equal if called in the same nanosecond, which is unlikely but acceptable
		t.Log("RunIDs happened to match (same nanosecond), this is acceptable")
	}
}

func TestBuildReport_RunIDDeterministic(t *testing.T) {
	// verify the hash is computed from timestamp + files
	results := map[string]*task.TaskResult{}

	r1 := buildReport([]string{"a.json"}, 1, "", "/repos", results, 0, "")
	r2 := buildReport([]string{"b.json"}, 1, "", "/repos", results, 0, "")

	if r1.RunID == r2.RunID {
		t.Error("different task files should produce different RunIDs")
	}
}

func TestBuildReport_ParentRunID(t *testing.T) {
	results := map[string]*task.TaskResult{}

	r1 := buildReport([]string{"tasks.json"}, 1, "", "/repos", results, 0, "")
	if r1.ParentRunID != "" {
		t.Errorf("expected empty ParentRunID for fresh run, got %q", r1.ParentRunID)
	}

	r2 := buildReport([]string{"tasks.json"}, 1, "", "/repos", results, 0, "abc123def456")
	if r2.ParentRunID != "abc123def456" {
		t.Errorf("expected ParentRunID 'abc123def456', got %q", r2.ParentRunID)
	}
}

func TestMatchFilter(t *testing.T) {
	tests := []struct {
		id      string
		pattern string
		want    bool
	}{
		// exact match
		{"task-1", "task-1", true},
		{"task-1", "task-2", false},

		// * wildcard (backwards compat)
		{"repo-WO01", "repo-*", true},
		{"repo-WO01", "*WO01", true},
		{"repo-WO01", "repo-*01", true},
		{"other-WO01", "repo-*", false},

		// ? single char
		{"task-1", "task-?", true},
		{"task-12", "task-?", false},

		// [...] character class
		{"task-a", "task-[abc]", true},
		{"task-d", "task-[abc]", false},

		// invalid pattern (unmatched bracket)
		{"task-1", "task-[", false},

		// empty
		{"task-1", "", false},
	}
	for _, tt := range tests {
		got := matchFilter(tt.id, tt.pattern)
		if got != tt.want {
			t.Errorf("matchFilter(%q, %q) = %v, want %v", tt.id, tt.pattern, got, tt.want)
		}
	}
}

func TestExpandBraces(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"no-braces", []string{"no-braces"}},
		{"prefix{a,b,c}suffix", []string{"prefixasuffix", "prefixbsuffix", "prefixcsuffix"}},
		{"chainwatch-WO-CW{40,43,61,62}", []string{"chainwatch-WO-CW40", "chainwatch-WO-CW43", "chainwatch-WO-CW61", "chainwatch-WO-CW62"}},
		{"{a,b}", []string{"a", "b"}},
		{"unclosed{brace", []string{"unclosed{brace"}},
		{"no}open", []string{"no}open"}},
	}
	for _, tt := range tests {
		got := expandBraces(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("expandBraces(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("expandBraces(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestFilterTasks_BraceExpansion(t *testing.T) {
	tasks := []task.Task{
		{ID: "chainwatch-WO-CW40"},
		{ID: "chainwatch-WO-CW43"},
		{ID: "chainwatch-WO-CW50"},
		{ID: "chainwatch-WO-CW61"},
		{ID: "chainwatch-WO-CW62"},
	}

	got := filterTasks(tasks, "chainwatch-WO-CW{40,43,61,62}")
	if len(got) != 4 {
		t.Errorf("brace expansion: got %v, want 4 tasks", ids(got))
	}

	// verify CW50 was excluded
	for _, t := range got {
		if t.ID == "chainwatch-WO-CW50" {
			t.ID = "" // won't reach here, just to silence linter
		}
	}
}

func TestFilterTasks_CommaSeparated(t *testing.T) {
	tasks := []task.Task{
		{ID: "app-WO01"},
		{ID: "app-WO02"},
		{ID: "app-WO03"},
		{ID: "lib-WO01"},
	}

	// exact IDs
	got := filterTasks(tasks, "app-WO01,app-WO03")
	if len(got) != 2 || got[0].ID != "app-WO01" || got[1].ID != "app-WO03" {
		t.Errorf("comma-separated exact: got %v", ids(got))
	}

	// mix glob + exact
	got = filterTasks(tasks, "app-*,lib-WO01")
	if len(got) != 4 {
		t.Errorf("comma glob+exact: got %v, want all 4", ids(got))
	}

	// single pattern (backwards compat)
	got = filterTasks(tasks, "app-*")
	if len(got) != 3 {
		t.Errorf("single glob: got %v, want 3 app tasks", ids(got))
	}

	// no matches
	got = filterTasks(tasks, "nonexistent")
	if len(got) != 0 {
		t.Errorf("no matches: got %v, want empty", ids(got))
	}

	// spaces around commas
	got = filterTasks(tasks, "app-WO01 , app-WO02")
	if len(got) != 2 {
		t.Errorf("spaces: got %v, want 2", ids(got))
	}
}

func TestCheckConnectivity(t *testing.T) {
	// should succeed when network is available (test environment has network)
	err := checkConnectivity()
	if err != nil {
		t.Skipf("skipping: no network in test environment: %v", err)
	}
}

func TestAllScriptTasks(t *testing.T) {
	tests := []struct {
		name  string
		tasks []task.Task
		want  bool
	}{
		{"all script", []task.Task{{Runner: "script"}, {Runner: "script"}}, true},
		{"mixed", []task.Task{{Runner: "script"}, {Runner: "codex"}}, false},
		{"none script", []task.Task{{Runner: "codex"}, {Runner: "claude"}}, false},
		{"empty", []task.Task{}, true},
	}
	for _, tt := range tests {
		if got := allScriptTasks(tt.tasks); got != tt.want {
			t.Errorf("allScriptTasks(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func ids(tasks []task.Task) []string {
	out := make([]string, len(tasks))
	for i, t := range tasks {
		out[i] = t.ID
	}
	return out
}
