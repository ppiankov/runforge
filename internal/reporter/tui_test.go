package reporter

import (
	"strings"
	"testing"

	"github.com/ppiankov/tokencontrol/internal/task"
)

func TestRepoShort(t *testing.T) {
	tests := []struct {
		name string
		task *task.Task
		want string
	}{
		{"nil task", nil, ""},
		{"empty repo", &task.Task{Repo: ""}, ""},
		{"owner/repo", &task.Task{Repo: "ppiankov/oracul"}, "oracul"},
		{"no slash", &task.Task{Repo: "single"}, "single"},
		{"nested", &task.Task{Repo: "a/b/c"}, "c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repoShort(tt.task)
			if got != tt.want {
				t.Errorf("repoShort() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCascadeTrail(t *testing.T) {
	tests := []struct {
		name string
		res  *task.TaskResult
		want string
	}{
		{"nil result", nil, ""},
		{"no attempts", &task.TaskResult{RunnerUsed: "codex"}, "codex"},
		{"single attempt", &task.TaskResult{
			RunnerUsed: "codex",
			Attempts:   []task.AttemptInfo{{Runner: "codex"}},
		}, "codex"},
		{"cascade fallback", &task.TaskResult{
			RunnerUsed: "zai",
			Attempts: []task.AttemptInfo{
				{Runner: "codex", State: task.StateFailed},
				{Runner: "zai", State: task.StateCompleted},
			},
		}, "codex→zai"},
		{"retries deduped", &task.TaskResult{
			RunnerUsed: "codex",
			Attempts: []task.AttemptInfo{
				{Runner: "codex", Retry: 0, State: task.StateFailed},
				{Runner: "codex", Retry: 1, State: task.StateFailed},
				{Runner: "zai", State: task.StateCompleted},
			},
		}, "codex→zai"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cascadeTrail(tt.res)
			if got != tt.want {
				t.Errorf("cascadeTrail() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFmtRunning_ShowsRunner(t *testing.T) {
	tasks := []task.Task{
		{ID: "t1", Repo: "org/oracul", Runner: "codex", Priority: 1, Title: "Build"},
	}
	g, err := task.BuildGraph(tasks)
	if err != nil {
		t.Fatal(err)
	}
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil)
	m.width = 120
	m.height = 40

	res := &task.TaskResult{
		TaskID:     "t1",
		State:      task.StateRunning,
		RunnerUsed: "codex",
	}
	line := m.fmtRunning(res, &tasks[0], "⠋")
	if !strings.Contains(line, "codex") {
		t.Error("expected runner 'codex' in running line")
	}
	if !strings.Contains(line, "oracul") {
		t.Error("expected repo 'oracul' in running line")
	}
}

func TestFmtDone_ShowsTokens(t *testing.T) {
	tasks := []task.Task{
		{ID: "t1", Repo: "org/oracul", Priority: 1, Title: "Build"},
	}
	g, err := task.BuildGraph(tasks)
	if err != nil {
		t.Fatal(err)
	}
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil)
	m.width = 120
	m.height = 40

	res := &task.TaskResult{
		TaskID:     "t1",
		State:      task.StateCompleted,
		RunnerUsed: "codex",
		TokensUsed: &task.TokenUsage{TotalTokens: 45200},
	}
	line := m.fmtDone(res, &tasks[0])
	if !strings.Contains(line, "codex") {
		t.Error("expected runner 'codex' in done line")
	}
	if !strings.Contains(line, "oracul") {
		t.Error("expected repo 'oracul' in done line")
	}
	if !strings.Contains(line, "45.2K tokens") {
		t.Errorf("expected '45.2K tokens' in done line, got: %s", line)
	}
}
