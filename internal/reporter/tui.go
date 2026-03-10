package reporter

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ppiankov/tokencontrol/internal/task"
)

var spinnerChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Panel focus constants.
const (
	panelTasks  = 0
	panelLogs   = 1
	panelAgents = 2

	maxLogLines       = 1000 // ring buffer cap for log lines
	minHeightForSplit = 20   // below this, hide log panel
	taskPanelRatio    = 0.70 // 70% tasks, 30% logs
)

// AgentPoolInfo holds agent readiness and quota data for the TUI agent panel.
type AgentPoolInfo struct {
	Agents    []AgentInfo
	GetQuotas func() []QuotaInfo // nil-safe; called on tick
}

// AgentInfo describes one agent from ANCC.
type AgentInfo struct {
	Name   string
	Skills int
	Hooks  int
	MCP    int
	Tokens int // config token overhead
}

// QuotaInfo mirrors runner.QuotaInfo to avoid circular import.
type QuotaInfo struct {
	Provider   string
	UsedTokens int
	BurnRate   int // tokens/day
	Balance    string
	Currency   string
	Available  bool
	Error      string
}

// TUI styles
var (
	headerStyle = lipgloss.NewStyle().Bold(true)
	failedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // red
	runStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("14")) // cyan
	doneStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	rlStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // gray
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	pauseStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)

	// Panel border styles
	focusedBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("14")) // cyan
	dimBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")) // gray

	logErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9")) // red for secret/error lines
)

type tickMsg time.Time

// TUIModel is the Bubbletea model for tokencontrol live display.
type TUIModel struct {
	graph      *task.Graph
	getResults func() map[string]*task.TaskResult
	cancelRun  func() // called on 'q' to cancel the run context
	startTime  time.Time

	results      map[string]*task.TaskResult
	scrollOffset int
	paused       bool
	frame        int
	width        int
	height       int
	done         bool // set when scheduler finishes

	// Log panel state
	logPath       string   // path to run.log; empty = no log panel
	logLines      []string // ring buffer of log lines
	logOffset     int64    // file read offset for incremental reads
	logScroll     int      // scroll offset within log panel
	logAutoScroll bool     // true = follow tail
	focusedPanel  int      // panelTasks, panelLogs, or panelAgents

	// Agent panel state
	agentPool *AgentPoolInfo
}

// NewTUIModel creates a new TUI model. When logPath is non-empty, a split layout
// is shown with task panel + bottom panel (log/agents, Tab-switchable).
func NewTUIModel(graph *task.Graph, getResults func() map[string]*task.TaskResult, cancelRun func(), logPath string, startTime time.Time, agentPool *AgentPoolInfo) TUIModel {
	return TUIModel{
		graph:         graph,
		getResults:    getResults,
		cancelRun:     cancelRun,
		startTime:     startTime,
		results:       make(map[string]*task.TaskResult),
		logPath:       logPath,
		logAutoScroll: true,
		agentPool:     agentPool,
	}
}

// Init implements tea.Model.
func (m TUIModel) Init() tea.Cmd {
	return tickCmd()
}

func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update implements tea.Model.
func (m TUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if m.cancelRun != nil {
				m.cancelRun()
			}
			m.done = true
			return m, tea.Quit

		case "p", " ":
			m.paused = !m.paused

		case "tab":
			if m.showBottomPanel() {
				m.focusedPanel = (m.focusedPanel + 1) % 3
				if m.focusedPanel != panelLogs {
					m.logAutoScroll = true
				}
			}

		case "j", "down":
			if m.focusedPanel == panelLogs && m.showBottomPanel() {
				m.logAutoScroll = false
				m.logScrollDown(1)
			} else {
				m.scrollDown(1)
			}

		case "k", "up":
			if m.focusedPanel == panelLogs && m.showBottomPanel() {
				m.logAutoScroll = false
				m.logScrollUp(1)
			} else {
				m.scrollUp(1)
			}

		case "g", "home":
			if m.focusedPanel == panelLogs && m.showBottomPanel() {
				m.logAutoScroll = false
				m.logScroll = 0
			} else {
				m.scrollOffset = 0
			}

		case "G", "end":
			if m.focusedPanel == panelLogs && m.showBottomPanel() {
				m.logAutoScroll = true
				m.logScroll = m.maxLogScroll()
			} else {
				m.scrollOffset = m.maxScroll()
			}

		case "pgdown":
			if m.focusedPanel == panelLogs && m.showBottomPanel() {
				m.logAutoScroll = false
				_, logH := m.panelHeights()
				m.logScrollDown(m.visibleLogLines(logH))
			} else {
				m.scrollDown(m.visibleTasks())
			}

		case "pgup":
			if m.focusedPanel == panelLogs && m.showBottomPanel() {
				m.logAutoScroll = false
				_, logH := m.panelHeights()
				m.logScrollUp(m.visibleLogLines(logH))
			} else {
				m.scrollUp(m.visibleTasks())
			}
		}

	case tickMsg:
		if !m.paused {
			m.results = m.getResults()
		}
		m.readLogLines()
		m.frame++
		return m, tickCmd()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, nil
}

func (m *TUIModel) scrollDown(n int) {
	m.scrollOffset += n
	if max := m.maxScroll(); m.scrollOffset > max {
		m.scrollOffset = max
	}
}

func (m *TUIModel) scrollUp(n int) {
	m.scrollOffset -= n
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

func (m TUIModel) visibleTasks() int {
	if m.showBottomPanel() {
		taskH, _ := m.panelHeights()
		return m.visibleTasksInPanel(taskH)
	}
	// single panel: header(1) + progress(1) + footer(1) + help(1) = 4 reserved lines
	avail := m.height - 4
	if avail < 3 {
		return 3
	}
	return avail
}

func (m TUIModel) maxScroll() int {
	total := len(m.graph.Order())
	vis := m.visibleTasks()
	if total <= vis {
		return 0
	}
	return total - vis
}

// --- Log panel helpers ---

func (m *TUIModel) logScrollDown(n int) {
	m.logScroll += n
	if max := m.maxLogScroll(); m.logScroll > max {
		m.logScroll = max
	}
}

func (m *TUIModel) logScrollUp(n int) {
	m.logScroll -= n
	if m.logScroll < 0 {
		m.logScroll = 0
	}
}

func (m TUIModel) showBottomPanel() bool {
	return (m.logPath != "" || m.agentPool != nil) && m.height >= minHeightForSplit
}

// panelHeights returns (taskPanelHeight, logPanelHeight) including borders.
// One line is reserved for the help bar below both panels.
func (m TUIModel) panelHeights() (int, int) {
	if !m.showBottomPanel() {
		return m.height, 0
	}
	available := m.height - 1 // reserve 1 for help line
	taskH := int(float64(available) * taskPanelRatio)
	logH := available - taskH
	if logH < 5 {
		logH = 5
		taskH = available - logH
	}
	return taskH, logH
}

// visibleTasksInPanel returns how many task lines fit in a bordered panel.
// Border top(1) + bottom(1) + header(1) + progress(1) + footer(1) = 5 reserved.
func (m TUIModel) visibleTasksInPanel(panelHeight int) int {
	avail := panelHeight - 5
	if avail < 3 {
		return 3
	}
	return avail
}

// visibleLogLines returns how many log lines fit in the log panel.
// Border top(1) + bottom(1) + header(1) = 3 reserved.
func (m TUIModel) visibleLogLines(panelHeight int) int {
	avail := panelHeight - 3
	if avail < 1 {
		return 1
	}
	return avail
}

func (m TUIModel) maxLogScroll() int {
	_, logH := m.panelHeights()
	vis := m.visibleLogLines(logH)
	if len(m.logLines) <= vis {
		return 0
	}
	return len(m.logLines) - vis
}

// readLogLines incrementally reads new lines from the log file.
func (m *TUIModel) readLogLines() {
	if m.logPath == "" {
		return
	}
	f, err := os.Open(m.logPath)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	if m.logOffset > 0 {
		if _, err := f.Seek(m.logOffset, io.SeekStart); err != nil {
			return
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	newBytes := int64(0)

	for scanner.Scan() {
		line := scanner.Text()
		newBytes += int64(len(scanner.Bytes())) + 1 // +1 for newline
		m.logLines = append(m.logLines, line)
	}

	m.logOffset += newBytes

	// trim to ring buffer cap
	if len(m.logLines) > maxLogLines {
		m.logLines = m.logLines[len(m.logLines)-maxLogLines:]
	}

	if m.logAutoScroll {
		m.logScroll = m.maxLogScroll()
	}
}

// isLogError checks if a log line should be highlighted in red.
func isLogError(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "level=error") ||
		strings.Contains(lower, "secrets") ||
		strings.Contains(lower, "failed")
}

// MarkDone signals the TUI that the scheduler has finished.
func (m *TUIModel) MarkDone() {
	m.done = true
}

// View implements tea.Model.
func (m TUIModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	if !m.showBottomPanel() {
		return m.viewSinglePanel()
	}

	taskH, bottomH := m.panelHeights()

	// Task panel content inside border
	taskContent := m.renderTaskContent(taskH)
	taskBorder := panelBorderStyle(m.focusedPanel == panelTasks)
	taskPanel := taskBorder.
		Width(m.width - 2).
		Height(taskH - 2).
		Render(taskContent)

	// Bottom panel: logs or agents depending on focusedPanel
	var bottomContent string
	var bottomFocused bool
	if m.focusedPanel == panelAgents {
		bottomContent = m.renderAgentContent(bottomH)
		bottomFocused = true
	} else {
		bottomContent = m.renderLogContent(bottomH)
		bottomFocused = m.focusedPanel == panelLogs
	}
	bottomBorder := panelBorderStyle(bottomFocused)
	bottomPanel := bottomBorder.
		Width(m.width - 2).
		Height(bottomH - 2).
		Render(bottomContent)

	combined := lipgloss.JoinVertical(lipgloss.Left, taskPanel, bottomPanel)

	// Help line below both panels
	focusHint := "tasks"
	switch m.focusedPanel {
	case panelLogs:
		focusHint = "logs"
	case panelAgents:
		focusHint = "agents"
	}
	help := helpStyle.Render(fmt.Sprintf(
		"  tab: switch [%s]  ↑↓/jk: scroll  g/G: top/bottom  p: pause  q: quit",
		focusHint))

	return combined + "\n" + help
}

func panelBorderStyle(focused bool) lipgloss.Style {
	if focused {
		return focusedBorderStyle
	}
	return dimBorderStyle
}

// viewSinglePanel is the original single-panel layout (no log panel).
func (m TUIModel) viewSinglePanel() string {
	var b strings.Builder

	// header
	total := len(m.graph.Order())
	var completed, running, failed, rateLimited, queued int
	for _, res := range m.results {
		switch res.State {
		case task.StateCompleted:
			completed++
		case task.StateRunning:
			running++
		case task.StateFailed, task.StateSkipped:
			failed++
		case task.StateRateLimited:
			rateLimited++
		default:
			queued++
		}
	}
	queued += total - len(m.results)

	header := fmt.Sprintf("tokencontrol — %d tasks  %s", total, time.Since(m.startTime).Truncate(time.Second))
	if m.paused {
		header += "  " + pauseStyle.Render("⏸ PAUSED")
	}
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	progress := m.progressLine(completed, running, failed, rateLimited, queued)
	b.WriteString(progress)
	b.WriteString("\n")

	taskLines := m.buildTaskLines()

	vis := m.visibleTasks()
	start := m.scrollOffset
	end := start + vis
	if end > len(taskLines) {
		end = len(taskLines)
	}
	if start > len(taskLines) {
		start = len(taskLines)
	}

	if start > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more above", start)))
		b.WriteString("\n")
	}

	for i := start; i < end; i++ {
		b.WriteString(taskLines[i])
		b.WriteString("\n")
	}

	if end < len(taskLines) {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↓ %d more below", len(taskLines)-end)))
		b.WriteString("\n")
	}

	footer := m.summaryFooter()
	b.WriteString(footer)
	b.WriteString("\n")

	used := 2 + (end - start) + 2
	if start > 0 {
		used++
	}
	if end < len(taskLines) {
		used++
	}
	for i := used; i < m.height-1; i++ {
		b.WriteString("\n")
	}

	b.WriteString(helpStyle.Render("  ↑↓/jk: scroll  g/G: top/bottom  p: pause  q: quit"))

	return b.String()
}

// renderTaskContent builds the task panel content for the split layout.
func (m TUIModel) renderTaskContent(panelHeight int) string {
	var b strings.Builder

	total := len(m.graph.Order())
	var completed, running, failed, rateLimited, queued int
	for _, res := range m.results {
		switch res.State {
		case task.StateCompleted:
			completed++
		case task.StateRunning:
			running++
		case task.StateFailed, task.StateSkipped:
			failed++
		case task.StateRateLimited:
			rateLimited++
		default:
			queued++
		}
	}
	queued += total - len(m.results)

	header := fmt.Sprintf("tokencontrol — %d tasks  %s", total, time.Since(m.startTime).Truncate(time.Second))
	if m.paused {
		header += "  " + pauseStyle.Render("⏸ PAUSED")
	}
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	b.WriteString(m.progressLine(completed, running, failed, rateLimited, queued))
	b.WriteString("\n")

	taskLines := m.buildTaskLines()
	vis := m.visibleTasksInPanel(panelHeight)
	start := m.scrollOffset
	end := start + vis
	if end > len(taskLines) {
		end = len(taskLines)
	}
	if start > len(taskLines) {
		start = len(taskLines)
	}

	if start > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more above", start)))
		b.WriteString("\n")
	}
	for i := start; i < end; i++ {
		b.WriteString(taskLines[i])
		b.WriteString("\n")
	}
	if end < len(taskLines) {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↓ %d more below", len(taskLines)-end)))
		b.WriteString("\n")
	}

	b.WriteString(m.summaryFooter())

	return b.String()
}

// renderLogContent builds the log panel content for the split layout.
func (m TUIModel) renderLogContent(panelHeight int) string {
	var b strings.Builder

	// header with file path
	logHeader := dimStyle.Render("LOG " + m.logPath)
	if m.logAutoScroll {
		logHeader += dimStyle.Render(" [following]")
	}
	b.WriteString(logHeader)
	b.WriteString("\n")

	if len(m.logLines) == 0 {
		b.WriteString(dimStyle.Render("  (waiting for log output...)"))
		return b.String()
	}

	vis := m.visibleLogLines(panelHeight)
	start := m.logScroll
	end := start + vis
	if end > len(m.logLines) {
		end = len(m.logLines)
	}
	if start > len(m.logLines) {
		start = len(m.logLines)
	}

	maxW := m.width - 4 // borders + padding
	for i := start; i < end; i++ {
		line := m.logLines[i]
		if maxW > 0 && len(line) > maxW {
			line = line[:maxW]
		}
		if isLogError(line) {
			b.WriteString(logErrorStyle.Render(line))
		} else {
			b.WriteString(line)
		}
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

// renderAgentContent builds the agent pool panel content.
func (m TUIModel) renderAgentContent(panelHeight int) string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("AGENTS"))
	b.WriteString("\n")

	stats := m.agentStats()

	if m.agentPool != nil && len(m.agentPool.Agents) > 0 {
		for _, a := range m.agentPool.Agents {
			s := stats[a.Name]
			status := dimStyle.Render("idle")
			if s.Running > 0 {
				status = runStyle.Render("running")
			}
			b.WriteString(fmt.Sprintf("  %-12s %d skills  %d hooks  %s\n", a.Name, a.Skills, a.Hooks, status))
			b.WriteString(fmt.Sprintf("    %s%d done  %d fail  %s%s\n",
				"", s.Done, s.Failed,
				formatCompactTokens(s.Tokens), ""))
			delete(stats, a.Name)
		}
	}

	// show runners not in ANCC (stats-only)
	for name, s := range stats {
		status := dimStyle.Render("idle")
		if s.Running > 0 {
			status = runStyle.Render("running")
		}
		b.WriteString(fmt.Sprintf("  %-12s %s\n", name, status))
		b.WriteString(fmt.Sprintf("    %d done  %d fail  %s\n", s.Done, s.Failed, formatCompactTokens(s.Tokens)))
	}

	// Quota section
	var quotas []QuotaInfo
	if m.agentPool != nil && m.agentPool.GetQuotas != nil {
		quotas = m.agentPool.GetQuotas()
	}
	if len(quotas) > 0 {
		b.WriteString("\n")
		b.WriteString(headerStyle.Render("QUOTAS"))
		b.WriteString("\n")
		for _, q := range quotas {
			if q.Error != "" && q.UsedTokens == 0 && q.Balance == "" {
				continue
			}
			var info string
			if q.Balance != "" {
				avail := doneStyle.Render("available")
				if !q.Available {
					avail = failedStyle.Render("exhausted")
				}
				info = fmt.Sprintf("$%s %s  %s", q.Balance, q.Currency, avail)
			} else if q.UsedTokens > 0 {
				info = fmt.Sprintf("%s/7d  %s/day",
					formatCompactTokens(q.UsedTokens),
					formatCompactTokens(q.BurnRate))
			}
			b.WriteString(fmt.Sprintf("  %-14s %s\n", q.Provider, info))
		}
	} else {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  quotas: unavailable"))
	}

	return b.String()
}

// agentStat holds per-runner runtime statistics.
type agentStat struct {
	Done    int
	Failed  int
	Running int
	Tokens  int
}

// agentStats computes per-runner stats from current results.
func (m TUIModel) agentStats() map[string]agentStat {
	stats := make(map[string]agentStat)
	for _, res := range m.results {
		if res.RunnerUsed == "" {
			continue
		}
		s := stats[res.RunnerUsed]
		switch res.State {
		case task.StateCompleted:
			s.Done++
		case task.StateFailed:
			s.Failed++
		case task.StateRunning:
			s.Running++
		}
		if res.TokensUsed != nil {
			s.Tokens += res.TokensUsed.TotalTokens
		}
		stats[res.RunnerUsed] = s
	}
	return stats
}

func (m TUIModel) buildTaskLines() []string {
	type entry struct {
		id    string
		state task.TaskState
		res   *task.TaskResult
		t     *task.Task
	}

	// collect and sort: failed → running → completed → rate-limited → queued
	var failed, running, done, rl, queued []entry

	for _, id := range m.graph.Order() {
		t := m.graph.Task(id)
		res := m.results[id]
		e := entry{id: id, t: t, res: res}

		if res == nil {
			e.state = task.StatePending
			queued = append(queued, e)
			continue
		}

		e.state = res.State
		switch res.State {
		case task.StateFailed, task.StateSkipped:
			failed = append(failed, e)
		case task.StateRunning:
			running = append(running, e)
		case task.StateCompleted:
			done = append(done, e)
		case task.StateRateLimited:
			rl = append(rl, e)
		default:
			queued = append(queued, e)
		}
	}

	spinner := spinnerChars[m.frame%len(spinnerChars)]
	var lines []string

	for _, e := range failed {
		lines = append(lines, m.fmtFailed(e.res, e.t))
	}
	for _, e := range running {
		lines = append(lines, m.fmtRunning(e.res, e.t, spinner))
	}
	for _, e := range done {
		lines = append(lines, m.fmtDone(e.res, e.t))
	}
	for _, e := range rl {
		lines = append(lines, m.fmtRateLimited(e.res, e.t))
	}
	for _, e := range queued {
		lines = append(lines, m.fmtQueued(e.t))
	}

	return lines
}

func (m TUIModel) fmtFailed(res *task.TaskResult, t *task.Task) string {
	icon := "✗"
	label := "FAILED"
	if res.State == task.StateSkipped {
		icon = "⊘"
		label = "skipped"
	}
	trail := cascadeTrail(res)
	repo := repoShort(t)
	errMsg := res.Error
	if len(errMsg) > 30 {
		errMsg = errMsg[:30] + "..."
	}
	return failedStyle.Render(fmt.Sprintf("  %s %-10s %-20s %-12s %-12s %s", icon, label, res.TaskID, trail, repo, errMsg))
}

func (m TUIModel) fmtRunning(res *task.TaskResult, t *task.Task, spinner string) string {
	rn := res.RunnerUsed
	if rn == "" && t != nil {
		rn = t.Runner
	}
	repo := repoShort(t)
	elapsed := time.Since(res.StartedAt).Truncate(time.Second)
	return runStyle.Render(fmt.Sprintf("  %s %-10s %-20s %-12s %-12s %s", spinner, "running", res.TaskID, rn, repo, elapsed))
}

func (m TUIModel) fmtDone(res *task.TaskResult, t *task.Task) string {
	dur := res.Duration.Truncate(time.Second)
	rn := res.RunnerUsed
	if rn != "" && len(res.Attempts) > 1 && len(uniqueAttemptRunners(res.Attempts)) > 1 {
		rn = "via " + rn
	}
	repo := repoShort(t)
	tokens := ""
	if res.TokensUsed != nil && res.TokensUsed.TotalTokens > 0 {
		tokens = formatCompactTokens(res.TokensUsed.TotalTokens)
	}
	return doneStyle.Render(fmt.Sprintf("  ✓ %-10s %-20s %-12s %-12s %-8s %s", "done", res.TaskID, rn, repo, dur, tokens))
}

func (m TUIModel) fmtRateLimited(res *task.TaskResult, t *task.Task) string {
	repo := repoShort(t)
	info := "rate limit"
	if !res.ResetsAt.IsZero() {
		remaining := time.Until(res.ResetsAt).Truncate(time.Minute)
		if remaining > 0 {
			info = fmt.Sprintf("resets in %s", remaining)
		}
	}
	rn := res.RunnerUsed
	return rlStyle.Render(fmt.Sprintf("  ⏸ %-10s %-20s %-12s %-12s %s", "rate-limit", res.TaskID, rn, repo, info))
}

func (m TUIModel) fmtQueued(t *task.Task) string {
	repo := repoShort(t)
	dep := ""
	if t != nil && len(t.DependsOn) > 0 {
		dep = "waiting: " + strings.Join(t.DependsOn, ", ")
	}
	id := ""
	if t != nil {
		id = t.ID
	}
	return dimStyle.Render(fmt.Sprintf("  ─ %-10s %-20s %-12s %-12s %s", "queued", id, "", repo, dep))
}

func (m TUIModel) progressLine(done, running, failed, rateLimited, queued int) string {
	var parts []string
	if done > 0 {
		parts = append(parts, doneStyle.Render(fmt.Sprintf("%d done", done)))
	}
	if running > 0 {
		parts = append(parts, runStyle.Render(fmt.Sprintf("%d running", running)))
	}
	if failed > 0 {
		parts = append(parts, failedStyle.Render(fmt.Sprintf("%d failed", failed)))
	}
	if rateLimited > 0 {
		parts = append(parts, rlStyle.Render(fmt.Sprintf("%d rate-limited", rateLimited)))
	}
	if queued > 0 {
		parts = append(parts, dimStyle.Render(fmt.Sprintf("%d queued", queued)))
	}
	return fmt.Sprintf("  %s", strings.Join(parts, "  "))
}

func (m TUIModel) summaryFooter() string {
	total := len(m.graph.Order())
	var completed int
	var totalTokens int
	runnerCounts := make(map[string]int)

	for _, res := range m.results {
		if res.State == task.StateCompleted {
			completed++
		}
		if res.RunnerUsed != "" && (res.State == task.StateRunning || res.State == task.StateCompleted) {
			runnerCounts[res.RunnerUsed]++
		}
		if res.TokensUsed != nil {
			totalTokens += res.TokensUsed.TotalTokens
		}
	}

	parts := []string{fmt.Sprintf("%d/%d tasks", completed, total)}

	if totalTokens > 0 {
		parts = append(parts, formatCompactTokens(totalTokens))
	}

	// runner distribution: sort for deterministic output
	if len(runnerCounts) > 0 {
		var rParts []string
		for name, count := range runnerCounts {
			rParts = append(rParts, fmt.Sprintf("%s\u00d7%d", name, count))
		}
		parts = append(parts, strings.Join(rParts, " "))
	}

	return dimStyle.Render("  " + strings.Join(parts, "  "))
}

// repoShort extracts the repo name from "owner/repo" format.
func repoShort(t *task.Task) string {
	if t == nil || t.Repo == "" {
		return ""
	}
	if idx := strings.LastIndex(t.Repo, "/"); idx >= 0 {
		return t.Repo[idx+1:]
	}
	return t.Repo
}

// cascadeTrail builds a runner trail from attempts: "codex→zai".
func cascadeTrail(res *task.TaskResult) string {
	if res == nil {
		return ""
	}
	if len(res.Attempts) <= 1 {
		if res.RunnerUsed != "" {
			return res.RunnerUsed
		}
		if len(res.Attempts) == 1 {
			return res.Attempts[0].Runner
		}
		return ""
	}
	seen := make(map[string]bool)
	var trail []string
	for _, a := range res.Attempts {
		if !seen[a.Runner] {
			seen[a.Runner] = true
			trail = append(trail, a.Runner)
		}
	}
	return strings.Join(trail, "→")
}
