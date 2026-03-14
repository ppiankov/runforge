package reporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "", time.Now(), nil, nil, "")
	m.width = 120
	m.height = 40

	res := &task.TaskResult{
		TaskID:     "t1",
		State:      task.StateRunning,
		RunnerUsed: "codex",
	}
	line := m.fmtRunning(res, &tasks[0], "⠋", colWidths{id: 20, runner: 12, repo: 12})
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
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "", time.Now(), nil, nil, "")
	m.width = 120
	m.height = 40

	res := &task.TaskResult{
		TaskID:     "t1",
		State:      task.StateCompleted,
		RunnerUsed: "codex",
		TokensUsed: &task.TokenUsage{TotalTokens: 45200},
	}
	line := m.fmtDone(res, &tasks[0], colWidths{id: 20, runner: 12, repo: 12})
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

func TestShowBottomPanel_Hidden_WhenNoPath(t *testing.T) {
	tasks := []task.Task{{ID: "t1", Repo: "org/repo", Priority: 1, Title: "T"}}
	g, _ := task.BuildGraph(tasks)
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "", time.Now(), nil, nil, "")
	m.width = 120
	m.height = 40
	if m.showBottomPanel() {
		t.Error("log panel should be hidden when logPath is empty")
	}
}

func TestShowBottomPanel_Hidden_WhenTooShort(t *testing.T) {
	tasks := []task.Task{{ID: "t1", Repo: "org/repo", Priority: 1, Title: "T"}}
	g, _ := task.BuildGraph(tasks)
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "/tmp/run.log", time.Now(), nil, nil, "")
	m.width = 120
	m.height = 15
	if m.showBottomPanel() {
		t.Error("log panel should be hidden when height < 20")
	}
}

func TestShowBottomPanel_Visible(t *testing.T) {
	tasks := []task.Task{{ID: "t1", Repo: "org/repo", Priority: 1, Title: "T"}}
	g, _ := task.BuildGraph(tasks)
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "/tmp/run.log", time.Now(), nil, nil, "")
	m.width = 120
	m.height = 40
	if !m.showBottomPanel() {
		t.Error("log panel should be visible when logPath set and height >= 20")
	}
}

func TestPanelHeights_70_30(t *testing.T) {
	tasks := []task.Task{{ID: "t1", Repo: "org/repo", Priority: 1, Title: "T"}}
	g, _ := task.BuildGraph(tasks)
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "/tmp/run.log", time.Now(), nil, nil, "")
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
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "/tmp/run.log", time.Now(), nil, nil, "")
	m.width = 120
	m.height = 40

	if m.focusedPanel != panelTasks {
		t.Fatal("default focus should be tasks panel")
	}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	model := m2.(TUIModel)
	if model.focusedPanel != panelAgents {
		t.Error("tab should switch to agents panel")
	}

	m3, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = m3.(TUIModel)
	if model.focusedPanel != panelLogs {
		t.Error("tab should switch to logs panel")
	}

	m4, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = m4.(TUIModel)
	if model.focusedPanel != panelTasks {
		t.Error("tab should cycle back to tasks panel")
	}
}

func TestViewSplitRendersLogHeader(t *testing.T) {
	tasks := []task.Task{{ID: "t1", Repo: "org/repo", Priority: 1, Title: "T"}}
	g, _ := task.BuildGraph(tasks)
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "/tmp/test-run.log", time.Now(), nil, nil, "")
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
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "", time.Now(), nil, nil, "")
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
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, logPath, time.Now(), nil, nil, "")
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

func TestAgentStats(t *testing.T) {
	tasks := []task.Task{
		{ID: "t1", Repo: "org/a", Priority: 1, Title: "A"},
		{ID: "t2", Repo: "org/b", Priority: 2, Title: "B"},
		{ID: "t3", Repo: "org/c", Priority: 3, Title: "C"},
	}
	g, _ := task.BuildGraph(tasks)
	results := map[string]*task.TaskResult{
		"t1": {TaskID: "t1", State: task.StateCompleted, RunnerUsed: "codex", TokensUsed: &task.TokenUsage{TotalTokens: 10000}},
		"t2": {TaskID: "t2", State: task.StateFailed, RunnerUsed: "codex"},
		"t3": {TaskID: "t3", State: task.StateRunning, RunnerUsed: "claude"},
	}
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return results }, nil, "", time.Now(), nil, nil, "")
	m.results = results

	stats := m.agentStats()
	if len(stats) != 2 {
		t.Fatalf("expected 2 runner stats, got %d", len(stats))
	}

	claude, ok := stats["claude"]
	if !ok {
		t.Fatal("expected stats for claude")
	}
	if claude.Running != 1 {
		t.Errorf("expected claude Running=1, got %d", claude.Running)
	}

	codex, ok := stats["codex"]
	if !ok {
		t.Fatal("expected stats for codex")
	}
	if codex.Done != 1 || codex.Failed != 1 {
		t.Errorf("expected codex Done=1 Failed=1, got Done=%d Failed=%d", codex.Done, codex.Failed)
	}
	if codex.Tokens != 10000 {
		t.Errorf("expected codex Tokens=10000, got %d", codex.Tokens)
	}
}

func TestRenderAgentContent_WithAgents(t *testing.T) {
	tasks := []task.Task{{ID: "t1", Repo: "org/a", Priority: 1, Title: "A"}}
	g, _ := task.BuildGraph(tasks)
	pool := &AgentPoolInfo{
		Agents: []AgentInfo{
			{Name: "codex", Skills: 3, Hooks: 0, MCP: 1, Tokens: 5000},
			{Name: "claude", Skills: 5, Hooks: 3, MCP: 2, Tokens: 12000},
		},
		GetQuotas: func() []QuotaInfo { return nil },
	}
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "", time.Now(), pool, nil, "")
	m.width = 120
	m.height = 40

	content := m.renderAgentContent(15)
	if !strings.Contains(content, "codex") {
		t.Error("agent panel should show codex")
	}
	if !strings.Contains(content, "claude") {
		t.Error("agent panel should show claude")
	}
	if !strings.Contains(content, "3 skills") {
		t.Error("agent panel should show skill count")
	}
}

func TestRenderAgentContent_WithQuotas(t *testing.T) {
	tasks := []task.Task{{ID: "t1", Repo: "org/a", Priority: 1, Title: "A"}}
	g, _ := task.BuildGraph(tasks)
	pool := &AgentPoolInfo{
		GetQuotas: func() []QuotaInfo {
			return []QuotaInfo{
				{Provider: "deepseek", Balance: "4.23", Currency: "USD", Available: true},
			}
		},
	}
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "", time.Now(), pool, nil, "")
	m.width = 120
	m.height = 40

	content := m.renderAgentContent(15)
	if !strings.Contains(content, "deepseek") {
		t.Error("agent panel should show deepseek quota")
	}
	if !strings.Contains(content, "4.23") {
		t.Error("agent panel should show balance")
	}
}

func TestShowBottomPanel_WithAgentPoolOnly(t *testing.T) {
	tasks := []task.Task{{ID: "t1", Repo: "org/repo", Priority: 1, Title: "T"}}
	g, _ := task.BuildGraph(tasks)
	pool := &AgentPoolInfo{Agents: []AgentInfo{{Name: "codex", Skills: 1}}}
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "", time.Now(), pool, nil, "")
	m.width = 120
	m.height = 40
	if !m.showBottomPanel() {
		t.Error("bottom panel should be visible with agentPool even without logPath")
	}
}

func TestSessionTimer_InHeader(t *testing.T) {
	tasks := []task.Task{{ID: "t1", Repo: "org/repo", Priority: 1, Title: "T"}}
	g, _ := task.BuildGraph(tasks)
	startTime := time.Now().Add(-5 * time.Minute)
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "", startTime, nil, nil, "")
	m.width = 120
	m.height = 40

	view := m.View()
	if !strings.Contains(view, "5m") {
		t.Error("header should show elapsed time of ~5m")
	}
}

func TestTaskCursorMovement(t *testing.T) {
	tasks := []task.Task{
		{ID: "t1", Repo: "org/a", Priority: 1, Title: "A"},
		{ID: "t2", Repo: "org/b", Priority: 2, Title: "B"},
		{ID: "t3", Repo: "org/c", Priority: 3, Title: "C"},
	}
	g, _ := task.BuildGraph(tasks)
	ctrl := &TaskControl{
		CancelTask:  func(string) {},
		RequeueTask: func(string, string) {},
		Runners:     []string{"codex", "claude"},
	}
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "", time.Now(), nil, ctrl, "")
	m.width = 120
	m.height = 40

	if m.taskCursor != 0 {
		t.Errorf("initial cursor should be 0, got %d", m.taskCursor)
	}

	// move down
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	model := m2.(TUIModel)
	if model.taskCursor != 1 {
		t.Errorf("cursor should be 1 after j, got %d", model.taskCursor)
	}

	// move down again
	m3, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	model = m3.(TUIModel)
	if model.taskCursor != 2 {
		t.Errorf("cursor should be 2 after second j, got %d", model.taskCursor)
	}

	// can't go past last
	m4, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	model = m4.(TUIModel)
	if model.taskCursor != 2 {
		t.Errorf("cursor should stay at 2, got %d", model.taskCursor)
	}

	// move up
	m5, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	model = m5.(TUIModel)
	if model.taskCursor != 1 {
		t.Errorf("cursor should be 1 after k, got %d", model.taskCursor)
	}
}

func TestCursorTaskID(t *testing.T) {
	tasks := []task.Task{
		{ID: "t1", Repo: "org/a", Priority: 1, Title: "A"},
		{ID: "t2", Repo: "org/b", Priority: 2, Title: "B"},
	}
	g, _ := task.BuildGraph(tasks)
	ctrl := &TaskControl{
		CancelTask:  func(string) {},
		RequeueTask: func(string, string) {},
		Runners:     []string{"codex"},
	}
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "", time.Now(), nil, ctrl, "")
	m.width = 120
	m.height = 40

	id := m.cursorTaskID()
	if id == "" {
		t.Error("cursorTaskID should return non-empty for valid cursor")
	}
}

func TestOverlay_OpensAndCloses(t *testing.T) {
	tasks := []task.Task{
		{ID: "t1", Repo: "org/a", Priority: 1, Title: "A", Runner: "codex"},
	}
	g, _ := task.BuildGraph(tasks)
	var requeuedRunner string
	ctrl := &TaskControl{
		CancelTask:  func(string) {},
		RequeueTask: func(id, runner string) { requeuedRunner = runner },
		Runners:     []string{"codex", "claude", "qwen"},
	}

	results := map[string]*task.TaskResult{
		"t1": {TaskID: "t1", State: task.StateFailed, Error: "test fail"},
	}
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return results }, nil, "", time.Now(), nil, ctrl, "")
	m.results = results
	m.width = 120
	m.height = 40

	// press r to open overlay on failed task
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	model := m2.(TUIModel)
	if !model.overlay.active {
		t.Error("overlay should be active after pressing r on failed task")
	}
	if model.overlay.taskID != "t1" {
		t.Errorf("overlay taskID should be t1, got %q", model.overlay.taskID)
	}

	// move cursor down to claude
	m3, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	model = m3.(TUIModel)
	if model.overlay.cursor != 1 {
		t.Errorf("overlay cursor should be 1, got %d", model.overlay.cursor)
	}

	// press enter to confirm
	m4, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = m4.(TUIModel)
	if model.overlay.active {
		t.Error("overlay should close after enter")
	}
	if requeuedRunner != "claude" {
		t.Errorf("expected requeue with 'claude', got %q", requeuedRunner)
	}
}

func TestOverlay_EscCancels(t *testing.T) {
	tasks := []task.Task{{ID: "t1", Repo: "org/a", Priority: 1, Title: "A"}}
	g, _ := task.BuildGraph(tasks)
	ctrl := &TaskControl{
		CancelTask:  func(string) {},
		RequeueTask: func(string, string) {},
		Runners:     []string{"codex", "claude"},
	}
	results := map[string]*task.TaskResult{
		"t1": {TaskID: "t1", State: task.StateFailed},
	}
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return results }, nil, "", time.Now(), nil, ctrl, "")
	m.results = results
	m.width = 120
	m.height = 40

	// open overlay
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	model := m2.(TUIModel)
	if !model.overlay.active {
		t.Fatal("overlay should be open")
	}

	// press esc
	m3, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = m3.(TUIModel)
	if model.overlay.active {
		t.Error("overlay should close on esc")
	}
}

func TestCancelKey_NoOpOnCompleted(t *testing.T) {
	tasks := []task.Task{{ID: "t1", Repo: "org/a", Priority: 1, Title: "A"}}
	g, _ := task.BuildGraph(tasks)
	cancelled := false
	ctrl := &TaskControl{
		CancelTask:  func(string) { cancelled = true },
		RequeueTask: func(string, string) {},
		Runners:     []string{"codex"},
	}
	results := map[string]*task.TaskResult{
		"t1": {TaskID: "t1", State: task.StateCompleted},
	}
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return results }, nil, "", time.Now(), nil, ctrl, "")
	m.results = results
	m.width = 120
	m.height = 40

	// press x on completed task
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if cancelled {
		t.Error("cancel should not fire on completed task")
	}
}

func TestBuildTaskLines_ShowsCursor(t *testing.T) {
	tasks := []task.Task{
		{ID: "t1", Repo: "org/a", Priority: 1, Title: "A"},
		{ID: "t2", Repo: "org/b", Priority: 2, Title: "B"},
	}
	g, _ := task.BuildGraph(tasks)
	ctrl := &TaskControl{
		CancelTask:  func(string) {},
		RequeueTask: func(string, string) {},
		Runners:     []string{"codex"},
	}
	m := NewTUIModel(g, func() map[string]*task.TaskResult { return nil }, nil, "", time.Now(), nil, ctrl, "")
	m.width = 120
	m.height = 40

	lines := m.buildTaskLines()
	if len(lines) != 2 {
		t.Fatalf("expected 2 task lines, got %d", len(lines))
	}
	// first line should have cursor prefix ">"
	if !strings.Contains(lines[0], ">") {
		t.Error("first line should have cursor marker")
	}
	if strings.Contains(lines[1], ">") {
		t.Error("second line should not have cursor marker")
	}
}
