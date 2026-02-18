package reporter

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ppiankov/runforge/internal/task"
)

var spinnerChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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
)

type tickMsg time.Time

// TUIModel is the Bubbletea model for runforge live display.
type TUIModel struct {
	graph      *task.Graph
	getResults func() map[string]*task.TaskResult
	cancelRun  func() // called on 'q' to cancel the run context

	results      map[string]*task.TaskResult
	scrollOffset int
	paused       bool
	frame        int
	width        int
	height       int
	done         bool // set when scheduler finishes
}

// NewTUIModel creates a new TUI model.
func NewTUIModel(graph *task.Graph, getResults func() map[string]*task.TaskResult, cancelRun func()) TUIModel {
	return TUIModel{
		graph:      graph,
		getResults: getResults,
		cancelRun:  cancelRun,
		results:    make(map[string]*task.TaskResult),
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

		case "j", "down":
			m.scrollDown(1)

		case "k", "up":
			m.scrollUp(1)

		case "g", "home":
			m.scrollOffset = 0

		case "G", "end":
			m.scrollOffset = m.maxScroll()

		case "pgdown":
			m.scrollDown(m.visibleTasks())

		case "pgup":
			m.scrollUp(m.visibleTasks())
		}

	case tickMsg:
		if !m.paused {
			m.results = m.getResults()
		}
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
	// header(2) + progress(1) + blank(1) + help(1) = 5 reserved lines
	avail := m.height - 5
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

// MarkDone signals the TUI that the scheduler has finished.
func (m *TUIModel) MarkDone() {
	m.done = true
}

// View implements tea.Model.
func (m TUIModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

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
	// tasks not yet in results are queued
	queued += total - len(m.results)

	header := fmt.Sprintf("runforge — %d tasks", total)
	if m.paused {
		header += "  " + pauseStyle.Render("⏸ PAUSED")
	}
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	// progress bar line
	progress := m.progressLine(completed, running, failed, rateLimited, queued)
	b.WriteString(progress)
	b.WriteString("\n")

	// build task lines
	taskLines := m.buildTaskLines()

	// apply scroll window
	vis := m.visibleTasks()
	start := m.scrollOffset
	end := start + vis
	if end > len(taskLines) {
		end = len(taskLines)
	}
	if start > len(taskLines) {
		start = len(taskLines)
	}

	// scroll hints
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

	// pad to fill screen
	used := 2 + (end - start) + 1 // header + progress + tasks + help
	if start > 0 {
		used++
	}
	if end < len(taskLines) {
		used++
	}
	for i := used; i < m.height-1; i++ {
		b.WriteString("\n")
	}

	// help line
	b.WriteString(helpStyle.Render("  ↑↓/jk: scroll  g/G: top/bottom  p: pause  q: quit"))

	return b.String()
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
	title := taskTitle(t)
	icon := "✗"
	label := "FAILED"
	if res.State == task.StateSkipped {
		icon = "⊘"
		label = "skipped"
	}
	errMsg := res.Error
	if len(errMsg) > 40 {
		errMsg = errMsg[:40] + "..."
	}
	return failedStyle.Render(fmt.Sprintf("  %s %-10s %-25s %-30s %s", icon, label, res.TaskID, title, errMsg))
}

func (m TUIModel) fmtRunning(res *task.TaskResult, t *task.Task, spinner string) string {
	title := taskTitle(t)
	runnerTag := ""
	if t != nil && t.Runner != "" {
		runnerTag = " [" + t.Runner + "]"
	}
	elapsed := time.Since(res.StartedAt).Truncate(time.Second)
	return runStyle.Render(fmt.Sprintf("  %s %-10s %-25s %-30s %s%s", spinner, "running", res.TaskID, title, elapsed, runnerTag))
}

func (m TUIModel) fmtDone(res *task.TaskResult, t *task.Task) string {
	title := taskTitle(t)
	dur := res.Duration.Truncate(time.Second)
	suffix := ""
	if res.RunnerUsed != "" && len(res.Attempts) > 1 {
		suffix = " [via " + res.RunnerUsed + "]"
	}
	return doneStyle.Render(fmt.Sprintf("  ✓ %-10s %-25s %-30s %s%s", "done", res.TaskID, title, dur, suffix))
}

func (m TUIModel) fmtRateLimited(res *task.TaskResult, t *task.Task) string {
	title := taskTitle(t)
	info := "rate limit"
	if !res.ResetsAt.IsZero() {
		remaining := time.Until(res.ResetsAt).Truncate(time.Minute)
		if remaining > 0 {
			info = fmt.Sprintf("resets in %s", remaining)
		}
	}
	return rlStyle.Render(fmt.Sprintf("  ⏸ %-10s %-25s %-30s %s", "rate-limit", res.TaskID, title, info))
}

func (m TUIModel) fmtQueued(t *task.Task) string {
	title := taskTitle(t)
	dep := ""
	if t != nil && len(t.DependsOn) > 0 {
		dep = "waiting: " + strings.Join(t.DependsOn, ", ")
	}
	id := ""
	if t != nil {
		id = t.ID
	}
	return dimStyle.Render(fmt.Sprintf("  ─ %-10s %-25s %-30s %s", "queued", id, title, dep))
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

func taskTitle(t *task.Task) string {
	if t == nil {
		return ""
	}
	return t.Title
}
