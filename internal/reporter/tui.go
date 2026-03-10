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
	panelAgents = 1
	panelLogs   = 2

	maxLogLines       = 1000 // ring buffer cap for log lines
	minHeightForSplit = 20   // below this, hide log panel
	topPanelRatio     = 0.65 // 65% top row, 35% bottom logs
	taskWidthRatio    = 0.65 // 65% tasks, 35% agents in top row
)

// AgentPoolInfo holds agent readiness and quota data for the TUI agent panel.
type AgentPoolInfo struct {
	Agents    []AgentInfo
	GetQuotas func() []QuotaInfo // nil-safe; called on tick

	// Graylist control — nil-safe, populated when graylist is available.
	IsGraylisted func(runner, model string) bool
	GrayEntries  func() map[string]GraylistEntry // key = "runner:model"
	GrayAdd      func(runner, model, reason string)
	GrayRemove   func(runner, model string)

	// Blacklist (read-only from TUI).
	IsBlacklisted func(runner string) bool

	// Runner profiles for tags (free, tier, fallback-only).
	Profiles map[string]RunnerProfileInfo
}

// GraylistEntry mirrors runner.GraylistInfo to avoid circular import.
type GraylistEntry struct {
	Model   string
	Reason  string
	AddedAt time.Time
}

// RunnerProfileInfo holds display-relevant fields from runner profiles.
type RunnerProfileInfo struct {
	Model        string
	Free         bool
	Tier         int
	FallbackOnly bool
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
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	overlayStyle  = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("11")).
			Padding(0, 1)
	overlaySelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
)

type tickMsg time.Time

// TaskControl holds callbacks for interactive task management.
type TaskControl struct {
	CancelTask  func(id string)
	RequeueTask func(id, runner string)
	Runners     []string // available runner names for picker
}

// overlayState tracks the runner picker modal.
type overlayState struct {
	active  bool
	taskID  string
	cursor  int
	runners []string
}

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

	// Task cursor for interactive control
	taskCursor int
	taskCtrl   *TaskControl
	overlay    overlayState

	// Log panel state
	logPath       string   // path to run.log; empty = no log panel
	logLines      []string // ring buffer of log lines
	logOffset     int64    // file read offset for incremental reads
	logScroll     int      // scroll offset within log panel
	logAutoScroll bool     // true = follow tail
	focusedPanel  int      // panelTasks, panelLogs, or panelAgents

	// Agent panel state
	agentPool   *AgentPoolInfo
	agentCursor int

	// Inline confirmation prompt
	confirm confirmState

	// Toast messages (temporary flash)
	toasts []toast
}

// confirmState manages inline confirmation prompts in the agent panel.
type confirmState struct {
	active   bool
	message  string
	warnings []string
	onYes    func()
}

// toast is a temporary flash message with auto-expiry.
type toast struct {
	message string
	expiry  time.Time
}

// GraylistEventMsg is sent to the TUI when auto-graylist fires.
type GraylistEventMsg struct {
	Runner string
	Model  string
	Reason string
}

// NewTUIModel creates a new TUI model. When logPath is non-empty, a split layout
// is shown with task panel + bottom panel (log/agents, Tab-switchable).
func NewTUIModel(graph *task.Graph, getResults func() map[string]*task.TaskResult, cancelRun func(), logPath string, startTime time.Time, agentPool *AgentPoolInfo, taskCtrl *TaskControl) TUIModel {
	return TUIModel{
		graph:         graph,
		getResults:    getResults,
		cancelRun:     cancelRun,
		startTime:     startTime,
		results:       make(map[string]*task.TaskResult),
		logPath:       logPath,
		logAutoScroll: true,
		agentPool:     agentPool,
		taskCtrl:      taskCtrl,
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
	case GraylistEventMsg:
		m.addToast(fmt.Sprintf("%s:%s auto-graylisted (%s)", msg.Runner, msg.Model, msg.Reason))
		return m, nil

	case tea.KeyMsg:
		// Confirm prompt intercepts all keys when active.
		if m.confirm.active {
			return m.updateConfirm(msg)
		}

		// Overlay intercepts all keys when active.
		if m.overlay.active {
			return m.updateOverlay(msg)
		}

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
			} else if m.focusedPanel == panelAgents {
				m.agentCursorDown()
			} else if m.focusedPanel == panelTasks {
				m.taskCursorDown()
			} else {
				m.scrollDown(1)
			}

		case "k", "up":
			if m.focusedPanel == panelLogs && m.showBottomPanel() {
				m.logAutoScroll = false
				m.logScrollUp(1)
			} else if m.focusedPanel == panelAgents {
				m.agentCursorUp()
			} else if m.focusedPanel == panelTasks {
				m.taskCursorUp()
			} else {
				m.scrollUp(1)
			}

		case "g":
			switch m.focusedPanel {
			case panelAgents:
				m.graylistSelected()
			case panelLogs:
				m.logAutoScroll = false
				m.logScroll = 0
			default:
				m.scrollOffset = 0
				m.taskCursor = 0
			}

		case "w":
			if m.focusedPanel == panelAgents {
				m.whitelistSelected()
			}

		case "home":
			if m.focusedPanel == panelLogs && m.showBottomPanel() {
				m.logAutoScroll = false
				m.logScroll = 0
			} else {
				m.scrollOffset = 0
				m.taskCursor = 0
			}

		case "G", "end":
			if m.focusedPanel == panelLogs && m.showBottomPanel() {
				m.logAutoScroll = true
				m.logScroll = m.maxLogScroll()
			} else {
				m.scrollOffset = m.maxScroll()
				total := len(m.buildTaskLines())
				if total > 0 {
					m.taskCursor = total - 1
				}
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

		case "x":
			// Cancel running task
			if m.focusedPanel == panelTasks && m.taskCtrl != nil {
				if id := m.cursorTaskID(); id != "" {
					if res := m.results[id]; res != nil && res.State == task.StateRunning {
						m.taskCtrl.CancelTask(id)
					}
				}
			}

		case "R":
			// Requeue failed task with same runner
			if m.focusedPanel == panelTasks && m.taskCtrl != nil {
				if id := m.cursorTaskID(); id != "" {
					if res := m.results[id]; res != nil {
						switch res.State {
						case task.StateFailed, task.StateSkipped, task.StateRateLimited:
							m.taskCtrl.RequeueTask(id, "")
						}
					}
				}
			}

		case "r":
			// Open runner picker for queued/failed task
			if m.focusedPanel == panelTasks && m.taskCtrl != nil && len(m.taskCtrl.Runners) > 0 {
				if id := m.cursorTaskID(); id != "" {
					res := m.results[id]
					canPick := res == nil // pending (no result yet)
					if res != nil {
						switch res.State {
						case task.StateFailed, task.StateSkipped, task.StateRateLimited,
							task.StatePending, task.StateReady:
							canPick = true
						}
					}
					if canPick {
						m.overlay = overlayState{
							active:  true,
							taskID:  id,
							runners: m.taskCtrl.Runners,
						}
					}
				}
			}
		}

	case tickMsg:
		if !m.paused {
			m.results = m.getResults()
		}
		m.readLogLines()
		m.pruneToasts()
		m.frame++
		return m, tickCmd()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, nil
}

// updateOverlay handles keys when the runner picker is active.
func (m TUIModel) updateOverlay(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.overlay.active = false
	case "j", "down":
		if m.overlay.cursor < len(m.overlay.runners)-1 {
			m.overlay.cursor++
		}
	case "k", "up":
		if m.overlay.cursor > 0 {
			m.overlay.cursor--
		}
	case "enter":
		if m.overlay.cursor < len(m.overlay.runners) && m.taskCtrl != nil {
			runner := m.overlay.runners[m.overlay.cursor]
			m.taskCtrl.RequeueTask(m.overlay.taskID, runner)
		}
		m.overlay.active = false
	}
	return m, nil
}

// cursorTaskID returns the task ID at the current cursor position.
func (m TUIModel) cursorTaskID() string {
	ids := m.buildTaskIDs()
	if m.taskCursor >= 0 && m.taskCursor < len(ids) {
		return ids[m.taskCursor]
	}
	return ""
}

func (m *TUIModel) taskCursorDown() {
	total := len(m.buildTaskIDs())
	if total == 0 {
		return
	}
	if m.taskCursor < total-1 {
		m.taskCursor++
	}
	// auto-scroll to keep cursor visible
	vis := m.visibleTasks()
	if m.taskCursor >= m.scrollOffset+vis {
		m.scrollOffset = m.taskCursor - vis + 1
	}
}

func (m *TUIModel) taskCursorUp() {
	if m.taskCursor > 0 {
		m.taskCursor--
	}
	// auto-scroll to keep cursor visible
	if m.taskCursor < m.scrollOffset {
		m.scrollOffset = m.taskCursor
	}
}

// orderedAgents returns the list of agent names for cursor navigation.
func (m TUIModel) orderedAgents() []string {
	var names []string
	if m.agentPool != nil {
		for _, a := range m.agentPool.Agents {
			names = append(names, a.Name)
		}
	}
	stats := m.agentStats()
	for name := range stats {
		found := false
		for _, n := range names {
			if n == name {
				found = true
				break
			}
		}
		if !found {
			names = append(names, name)
		}
	}
	return names
}

func (m *TUIModel) agentCursorDown() {
	total := len(m.orderedAgents())
	if total == 0 {
		return
	}
	if m.agentCursor < total-1 {
		m.agentCursor++
	}
}

func (m *TUIModel) agentCursorUp() {
	if m.agentCursor > 0 {
		m.agentCursor--
	}
}

// selectedAgent returns the name and model of the agent at the cursor.
func (m TUIModel) selectedAgent() (string, string) {
	agents := m.orderedAgents()
	if m.agentCursor < 0 || m.agentCursor >= len(agents) {
		return "", ""
	}
	name := agents[m.agentCursor]
	model := ""
	if m.agentPool != nil && m.agentPool.Profiles != nil {
		if p, ok := m.agentPool.Profiles[name]; ok {
			model = p.Model
		}
	}
	return name, model
}

// graylistSelected initiates graylist confirm for the selected agent.
func (m *TUIModel) graylistSelected() {
	name, model := m.selectedAgent()
	if name == "" {
		return
	}
	if m.agentPool == nil || m.agentPool.GrayAdd == nil {
		return
	}
	if m.agentPool.IsGraylisted != nil && m.agentPool.IsGraylisted(name, model) {
		m.addToast(name + " already graylisted")
		return
	}
	label := name
	if model != "" {
		label = name + ":" + model
	}
	m.confirm = confirmState{
		active:  true,
		message: fmt.Sprintf("Graylist %s? [y/n]", label),
		onYes: func() {
			m.agentPool.GrayAdd(name, model, "manual")
			m.addToast(label + " graylisted")
		},
	}
}

// whitelistSelected initiates whitelist confirm with safety warnings.
func (m *TUIModel) whitelistSelected() {
	name, model := m.selectedAgent()
	if name == "" {
		return
	}
	if m.agentPool == nil || m.agentPool.GrayRemove == nil {
		return
	}
	if m.agentPool.IsGraylisted == nil || !m.agentPool.IsGraylisted(name, model) {
		m.addToast(name + " not graylisted")
		return
	}

	label := name
	if model != "" {
		label = name + ":" + model
	}

	var warnings []string
	if m.agentPool.Profiles != nil {
		if p, ok := m.agentPool.Profiles[name]; ok {
			if p.Free {
				warnings = append(warnings, "free-tier model — may produce low-quality results")
			}
			if p.Tier >= 3 {
				warnings = append(warnings, fmt.Sprintf("tier-%d runner — low capability", p.Tier))
			}
		}
	}
	// check if auto-graylisted
	if m.agentPool.GrayEntries != nil {
		for _, entry := range m.agentPool.GrayEntries() {
			if entry.Model == model && strings.Contains(entry.Reason, "false positive") {
				warnings = append(warnings, "auto-graylisted due to false positive detection")
				break
			}
		}
	}

	m.confirm = confirmState{
		active:   true,
		message:  fmt.Sprintf("Whitelist %s? [y/n]", label),
		warnings: warnings,
		onYes: func() {
			m.agentPool.GrayRemove(name, model)
			m.addToast(label + " whitelisted")
		},
	}
}

// updateConfirm handles keys during an active confirm prompt.
func (m TUIModel) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		if m.confirm.onYes != nil {
			m.confirm.onYes()
		}
		m.confirm = confirmState{}
	case "n", "esc":
		m.confirm = confirmState{}
	}
	return m, nil
}

func (m *TUIModel) addToast(msg string) {
	m.toasts = append(m.toasts, toast{message: msg, expiry: time.Now().Add(5 * time.Second)})
}

func (m *TUIModel) pruneToasts() {
	now := time.Now()
	var live []toast
	for _, t := range m.toasts {
		if t.expiry.After(now) {
			live = append(live, t)
		}
	}
	m.toasts = live
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

// panelHeights returns (topRowHeight, logPanelHeight) including borders.
// One line is reserved for the help bar below all panels.
func (m TUIModel) panelHeights() (int, int) {
	if !m.showBottomPanel() {
		return m.height, 0
	}
	available := m.height - 1 // reserve 1 for help line
	topH := int(float64(available) * topPanelRatio)
	logH := available - topH
	if logH < 5 {
		logH = 5
		topH = available - logH
	}
	return topH, logH
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

	topH, logH := m.panelHeights()

	// Top row: tasks (left) + agents (right) side by side
	taskW := int(float64(m.width) * taskWidthRatio)
	agentW := m.width - taskW

	taskContent := m.renderTaskContent(topH)
	taskBorder := panelBorderStyle(m.focusedPanel == panelTasks)
	taskPanel := taskBorder.
		Width(taskW - 2).
		Height(topH - 2).
		Render(taskContent)

	agentContent := m.renderAgentContent(topH)
	agentBorder := panelBorderStyle(m.focusedPanel == panelAgents)
	agentPanel := agentBorder.
		Width(agentW - 2).
		Height(topH - 2).
		Render(agentContent)

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, taskPanel, agentPanel)

	// Bottom: log panel (full width)
	logContent := m.renderLogContent(logH)
	logBorder := panelBorderStyle(m.focusedPanel == panelLogs)
	logPanel := logBorder.
		Width(m.width - 2).
		Height(logH - 2).
		Render(logContent)

	combined := lipgloss.JoinVertical(lipgloss.Left, topRow, logPanel)

	// Overlay runner picker on top of combined view if active
	if m.overlay.active {
		combined = m.renderWithOverlay(combined)
	}

	// Help line below both panels
	focusHint := "tasks"
	switch m.focusedPanel {
	case panelLogs:
		focusHint = "logs"
	case panelAgents:
		focusHint = "agents"
	}
	helpKeys := "tab: switch [%s]  ↑↓/jk: scroll  g/G: top/bottom  p: pause  q: quit"
	if m.taskCtrl != nil && m.focusedPanel == panelTasks {
		helpKeys = "tab: switch [%s]  ↑↓/jk: cursor  x: cancel  R: requeue  r: runner  q: quit"
	} else if m.focusedPanel == panelAgents {
		helpKeys = "tab: switch [%s]  ↑↓/jk: cursor  g: graylist  w: whitelist  q: quit"
	}
	help := helpStyle.Render(fmt.Sprintf("  "+helpKeys, focusHint))

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

	if m.taskCtrl != nil {
		b.WriteString(helpStyle.Render("  ↑↓/jk: cursor  x: cancel  R: requeue  r: runner  p: pause  q: quit"))
	} else {
		b.WriteString(helpStyle.Render("  ↑↓/jk: scroll  g/G: top/bottom  p: pause  q: quit"))
	}

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

	// Toasts at the top
	for _, t := range m.toasts {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("  " + t.message))
		b.WriteString("\n")
	}

	b.WriteString(headerStyle.Render("AGENTS"))
	b.WriteString("\n")

	stats := m.agentStats()
	agents := m.orderedAgents()
	showCursor := m.focusedPanel == panelAgents

	for i, name := range agents {
		s := stats[name]

		// cursor prefix
		prefix := " "
		if showCursor && i == m.agentCursor {
			prefix = cursorStyle.Render(">")
		}

		// status indicator
		indicator := doneStyle.Render("●") // green = active
		if m.agentPool != nil {
			model := ""
			if m.agentPool.Profiles != nil {
				if p, ok := m.agentPool.Profiles[name]; ok {
					model = p.Model
				}
			}
			if m.agentPool.IsBlacklisted != nil && m.agentPool.IsBlacklisted(name) {
				indicator = failedStyle.Render("✕") // red = blacklisted
			} else if m.agentPool.IsGraylisted != nil && m.agentPool.IsGraylisted(name, model) {
				indicator = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("○") // yellow = graylisted
			}
		}

		// runtime status
		status := dimStyle.Render("idle")
		if s.Running > 0 {
			status = runStyle.Render("running")
		}

		// tags
		var tags []string
		if m.agentPool != nil && m.agentPool.Profiles != nil {
			if p, ok := m.agentPool.Profiles[name]; ok {
				if p.Free {
					tags = append(tags, dimStyle.Render("[free]"))
				}
				if p.Tier > 0 {
					tags = append(tags, dimStyle.Render(fmt.Sprintf("[tier-%d]", p.Tier)))
				}
				if p.FallbackOnly {
					tags = append(tags, dimStyle.Render("[fallback]"))
				}
			}
		}
		tagStr := ""
		if len(tags) > 0 {
			tagStr = " " + strings.Join(tags, " ")
		}

		// skills count from ANCC
		skillStr := ""
		if m.agentPool != nil {
			for _, a := range m.agentPool.Agents {
				if a.Name == name && a.Skills > 0 {
					skillStr = dimStyle.Render(fmt.Sprintf("%d skills", a.Skills))
					break
				}
			}
		}

		b.WriteString(fmt.Sprintf("%s %s %-10s %s%s\n", prefix, indicator, name, status, tagStr))
		statLine := fmt.Sprintf("    %d done  %d fail  %s", s.Done, s.Failed, formatCompactTokens(s.Tokens))
		if skillStr != "" {
			statLine += "  " + skillStr
		}
		b.WriteString(statLine + "\n")
	}

	// Confirm prompt at bottom of agent panel
	if m.confirm.active {
		b.WriteString("\n")
		for _, w := range m.confirm.warnings {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("  ⚠ " + w))
			b.WriteString("\n")
		}
		b.WriteString(headerStyle.Render("  " + m.confirm.message))
		b.WriteString("\n")
		return b.String()
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
			} else if q.Error != "" {
				info = dimStyle.Render(q.Error)
			} else {
				info = doneStyle.Render("connected")
			}
			b.WriteString(fmt.Sprintf("  %-14s %s\n", q.Provider, info))
		}
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

type taskEntry struct {
	id    string
	state task.TaskState
	res   *task.TaskResult
	t     *task.Task
}

// sortedTaskEntries returns tasks in display order: failed → running → completed → rate-limited → queued.
func (m TUIModel) sortedTaskEntries() []taskEntry {
	var failed, running, done, rl, queued []taskEntry

	for _, id := range m.graph.Order() {
		t := m.graph.Task(id)
		res := m.results[id]
		e := taskEntry{id: id, t: t, res: res}

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

	var all []taskEntry
	all = append(all, failed...)
	all = append(all, running...)
	all = append(all, done...)
	all = append(all, rl...)
	all = append(all, queued...)
	return all
}

// buildTaskIDs returns task IDs in display order.
func (m TUIModel) buildTaskIDs() []string {
	entries := m.sortedTaskEntries()
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.id
	}
	return ids
}

func (m TUIModel) buildTaskLines() []string {
	entries := m.sortedTaskEntries()
	spinner := spinnerChars[m.frame%len(spinnerChars)]
	lines := make([]string, 0, len(entries))

	showCursor := m.focusedPanel == panelTasks && m.taskCtrl != nil

	for i, e := range entries {
		prefix := " "
		if showCursor && i == m.taskCursor {
			prefix = cursorStyle.Render(">")
		}

		var line string
		switch {
		case e.res == nil || e.state == task.StatePending || e.state == task.StateReady:
			line = prefix + m.fmtQueued(e.t)
		case e.state == task.StateFailed || e.state == task.StateSkipped:
			line = prefix + m.fmtFailed(e.res, e.t)
		case e.state == task.StateRunning:
			line = prefix + m.fmtRunning(e.res, e.t, spinner)
		case e.state == task.StateCompleted:
			line = prefix + m.fmtDone(e.res, e.t)
		case e.state == task.StateRateLimited:
			line = prefix + m.fmtRateLimited(e.res, e.t)
		default:
			line = prefix + m.fmtQueued(e.t)
		}
		lines = append(lines, line)
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

// renderWithOverlay composites the runner picker over the main view.
func (m TUIModel) renderWithOverlay(base string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Select runner for %s:", m.overlay.taskID))
	b.WriteString("\n")
	for i, r := range m.overlay.runners {
		prefix := "  "
		if i == m.overlay.cursor {
			prefix = "> "
			b.WriteString(overlaySelStyle.Render(prefix + r))
		} else {
			b.WriteString(prefix + r)
		}
		b.WriteString("\n")
	}
	b.WriteString(dimStyle.Render("enter: select  esc: cancel"))

	overlay := overlayStyle.Render(b.String())

	// Place overlay at top-right of the view
	lines := strings.Split(base, "\n")
	olLines := strings.Split(overlay, "\n")

	// Start overlay at row 2 (below header)
	startRow := 2
	for i, ol := range olLines {
		row := startRow + i
		if row < len(lines) {
			// Right-align: pad existing line and append overlay
			padding := m.width - lipgloss.Width(lines[row]) - lipgloss.Width(ol) - 2
			if padding < 1 {
				padding = 1
			}
			lines[row] = lines[row] + strings.Repeat(" ", padding) + ol
		}
	}

	return strings.Join(lines, "\n")
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
