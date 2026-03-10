package reporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "")
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
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "")
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

func TestShowLogPanel_Hidden_WhenNoPath(t *testing.T) {
	tasks := []task.Task{{ID: "t1", Repo: "org/repo", Priority: 1, Title: "T"}}
	g, _ := task.BuildGraph(tasks)
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "")
	m.width = 120
	m.height = 40
	if m.showLogPanel() {
		t.Error("log panel should be hidden when logPath is empty")
	}
}

func TestShowLogPanel_Hidden_WhenTooShort(t *testing.T) {
	tasks := []task.Task{{ID: "t1", Repo: "org/repo", Priority: 1, Title: "T"}}
	g, _ := task.BuildGraph(tasks)
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "/tmp/run.log")
	m.width = 120
	m.height = 15
	if m.showLogPanel() {
		t.Error("log panel should be hidden when height < 20")
	}
}

func TestShowLogPanel_Visible(t *testing.T) {
	tasks := []task.Task{{ID: "t1", Repo: "org/repo", Priority: 1, Title: "T"}}
	g, _ := task.BuildGraph(tasks)
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "/tmp/run.log")
	m.width = 120
	m.height = 40
	if !m.showLogPanel() {
		t.Error("log panel should be visible when logPath set and height >= 20")
	}
}

func TestPanelHeights_70_30(t *testing.T) {
	tasks := []task.Task{{ID: "t1", Repo: "org/repo", Priority: 1, Title: "T"}}
	g, _ := task.BuildGraph(tasks)
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "/tmp/run.log")
	m.width = 120
	m.height = 40
	taskH, logH := m.panelHeights()
	// 40 - 1 (help) = 39 available
	if taskH+logH != 39 {
		t.Errorf("panel heights should sum to 39, got %d+%d=%d", taskH, logH, taskH+logH)
	}
	if taskH < logH {
		t.Errorf("task panel should be larger: taskH=%d, logH=%d", taskH, logH)
	}
}

func TestIsLogError(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"time=... level=ERROR msg=something", true},
		{"time=... level=WARN msg=secrets detected", true},
		{"time=... level=WARN msg=auto-commit failed", true},
		{"time=... level=INFO msg=normal", false},
		{"Secrets found in repo", true},
		{"task completed successfully", false},
	}
	for _, tt := range tests {
		if got := isLogError(tt.line); got != tt.want {
			t.Errorf("isLogError(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestTabSwitchesFocus(t *testing.T) {
	tasks := []task.Task{{ID: "t1", Repo: "org/repo", Priority: 1, Title: "T"}}
	g, _ := task.BuildGraph(tasks)
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "/tmp/run.log")
	m.width = 120
	m.height = 40

	if m.focusedPanel != panelTasks {
		t.Fatal("default focus should be tasks panel")
	}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	model := m2.(TUIModel)
	if model.focusedPanel != panelLogs {
		t.Error("tab should switch to logs panel")
	}

	m3, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = m3.(TUIModel)
	if model.focusedPanel != panelTasks {
		t.Error("tab should cycle back to tasks panel")
	}
}

func TestViewSplitRendersLogHeader(t *testing.T) {
	tasks := []task.Task{{ID: "t1", Repo: "org/repo", Priority: 1, Title: "T"}}
	g, _ := task.BuildGraph(tasks)
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "/tmp/test-run.log")
	m.width = 120
	m.height = 40

	view := m.View()
	if !strings.Contains(view, "test-run.log") {
		t.Error("split view should show log file path in log panel header")
	}
	if !strings.Contains(view, "tokencontrol") {
		t.Error("split view should still contain main header")
	}
}

func TestViewSinglePanel_WhenNoLogPath(t *testing.T) {
	tasks := []task.Task{{ID: "t1", Repo: "org/repo", Priority: 1, Title: "T"}}
	g, _ := task.BuildGraph(tasks)
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "")
	m.width = 120
	m.height = 40

	view := m.View()
	if strings.Contains(view, "LOG") {
		t.Error("single panel view should not show log panel header")
	}
	if strings.Contains(view, "switch") {
		t.Error("single panel view should not show panel-switch help")
	}
}

func TestReadLogLines(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.log")
	if err := os.WriteFile(logPath, []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tasks := []task.Task{{ID: "t1", Repo: "org/repo", Priority: 1, Title: "T"}}
	g, _ := task.BuildGraph(tasks)
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, logPath)
	m.width = 120
	m.height = 40

	m.readLogLines()
	if len(m.logLines) != 3 {
		t.Errorf("expected 3 log lines, got %d", len(m.logLines))
	}
	if m.logLines[0] != "line1" {
		t.Errorf("expected first line 'line1', got %q", m.logLines[0])
	}

	// append more and read again (incremental)
	f, _ := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString("line4\n")
	_ = f.Close()

	m.readLogLines()
	if len(m.logLines) != 4 {
		t.Errorf("expected 4 log lines after append, got %d", len(m.logLines))
	}
}
