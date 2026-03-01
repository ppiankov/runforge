package sentinel

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ppiankov/runforge/internal/task"
)

type tuiTickMsg time.Time

// MissionControlModel is the Bubbletea model for the sentinel dashboard.
type MissionControlModel struct {
	state    *SentinelState
	snapshot StateSnapshot
	tab      int // 0=overview, 1=current run, 2=history
	scroll   int
	frame    int
	width    int
	height   int
	cancelFn func()
}

// NewMissionControlModel creates a new sentinel TUI model.
func NewMissionControlModel(state *SentinelState, cancelFn func()) MissionControlModel {
	return MissionControlModel{
		state:    state,
		cancelFn: cancelFn,
	}
}

// Init implements tea.Model.
func (m MissionControlModel) Init() tea.Cmd {
	return tuiTickCmd()
}

func tuiTickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tuiTickMsg(t)
	})
}

// Update implements tea.Model.
func (m MissionControlModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if m.cancelFn != nil {
				m.cancelFn()
			}
			return m, tea.Quit
		case "1":
			m.tab = 0
			m.scroll = 0
		case "2":
			m.tab = 1
			m.scroll = 0
		case "3":
			m.tab = 2
			m.scroll = 0
		case "tab":
			m.tab = (m.tab + 1) % 3
			m.scroll = 0
		case "j", "down":
			m.scroll++
		case "k", "up":
			if m.scroll > 0 {
				m.scroll--
			}
		case "g":
			m.scroll = 0
		}

	case tuiTickMsg:
		m.snapshot = m.state.Snapshot()
		m.frame++
		return m, tuiTickCmd()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return m, nil
}

// View implements tea.Model.
func (m MissionControlModel) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	var b strings.Builder

	// header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// tabs
	b.WriteString(m.renderTabs())
	b.WriteString("\n\n")

	// content
	contentHeight := m.height - 7 // header + tabs + footer
	if contentHeight < 3 {
		contentHeight = 3
	}
	content := m.renderContent()
	lines := strings.Split(content, "\n")

	// apply scroll
	if m.scroll > len(lines)-contentHeight {
		m.scroll = max(0, len(lines)-contentHeight)
	}
	end := m.scroll + contentHeight
	if end > len(lines) {
		end = len(lines)
	}
	visible := lines[m.scroll:end]
	b.WriteString(strings.Join(visible, "\n"))

	// pad to fill screen
	for i := len(visible); i < contentHeight; i++ {
		b.WriteString("\n")
	}

	// footer
	b.WriteString("\n")
	b.WriteString(m.renderFooter())

	return b.String()
}

func (m MissionControlModel) renderHeader() string {
	snap := m.snapshot
	uptime := time.Since(snap.StartedAt).Round(time.Second)

	spinner := ""
	if snap.Phase == PhaseScanning || snap.Phase == PhaseRunning {
		spinner = spinnerChars[m.frame%len(spinnerChars)] + " "
	}

	phase := phaseStyle.Render(snap.Phase.String())
	if snap.PhaseMsg != "" {
		phase += " " + dimStyle.Render("("+snap.PhaseMsg+")")
	}

	return headerStyle.Render("runforge sentinel") +
		dimStyle.Render(fmt.Sprintf(" — %s — cycle %d", uptime, snap.TotalCycles)) +
		"\n" + spinner + phase
}

func (m MissionControlModel) renderTabs() string {
	tabs := []string{"Overview", "Current Run", "History"}
	var parts []string
	for i, name := range tabs {
		label := fmt.Sprintf(" %d %s ", i+1, name)
		if i == m.tab {
			parts = append(parts, tabActiveStyle.Render(label))
		} else {
			parts = append(parts, tabStyle.Render(label))
		}
	}
	return strings.Join(parts, "  ")
}

func (m MissionControlModel) renderContent() string {
	switch m.tab {
	case 0:
		return m.renderOverview()
	case 1:
		return m.renderCurrentRun()
	case 2:
		return m.renderHistory()
	default:
		return ""
	}
}

func (m MissionControlModel) renderOverview() string {
	snap := m.snapshot
	var b strings.Builder

	// stats
	b.WriteString(fmt.Sprintf("  Total completed: %s  Total failed: %s  Cycles: %d\n",
		doneStyle.Render(fmt.Sprintf("%d", snap.TotalCompleted)),
		failedStyle.Render(fmt.Sprintf("%d", snap.TotalFailed)),
		snap.TotalCycles))

	// cooldown timer
	if snap.Phase == PhaseCooldown && !snap.NextScanAt.IsZero() {
		remaining := time.Until(snap.NextScanAt).Round(time.Second)
		if remaining < 0 {
			remaining = 0
		}
		b.WriteString(fmt.Sprintf("  Next scan in: %s\n", warnStyle.Render(remaining.String())))
	}

	// runner health
	if len(snap.Runners) > 0 {
		b.WriteString("\n  Runners:\n")
		for _, r := range snap.Runners {
			status := doneStyle.Render("ok")
			if r.Blacklisted {
				status = failedStyle.Render("blacklisted")
			} else if r.Graylisted {
				status = warnStyle.Render("graylisted")
			} else if !r.Available {
				status = dimStyle.Render("unavailable")
			}
			b.WriteString(fmt.Sprintf("    %s: %s\n", r.Name, status))
		}
	}

	// recent runs
	if len(snap.History) > 0 {
		b.WriteString("\n  Recent runs:\n")
		count := 5
		if len(snap.History) < count {
			count = len(snap.History)
		}
		for _, h := range snap.History[:count] {
			status := doneStyle.Render("ok")
			if h.Failed > 0 {
				status = failedStyle.Render(fmt.Sprintf("%d failed", h.Failed))
			}
			b.WriteString(fmt.Sprintf("    %s  %s  %d/%d tasks  %s  %s\n",
				h.RunID, h.StartedAt.Format("15:04:05"),
				h.Completed, h.TasksNew,
				h.Duration.Round(time.Second), status))
		}
	}

	return b.String()
}

func (m MissionControlModel) renderCurrentRun() string {
	snap := m.snapshot
	if snap.CurrentRun == nil || snap.Phase != PhaseRunning {
		return "  " + dimStyle.Render("No active run")
	}

	run := snap.CurrentRun
	var b strings.Builder

	// progress
	var running, completed, failed, queued int
	for _, r := range run.Results {
		switch r.State {
		case task.StateRunning:
			running++
		case task.StateCompleted:
			completed++
		case task.StateFailed:
			failed++
		}
	}
	queued = run.Total - running - completed - failed

	elapsed := time.Since(run.StartedAt).Round(time.Second)
	b.WriteString(fmt.Sprintf("  %d/%d done  %s running  %s failed  %d queued  %s elapsed\n\n",
		completed, run.Total,
		runStyle.Render(fmt.Sprintf("%d", running)),
		failedStyle.Render(fmt.Sprintf("%d", failed)),
		queued, elapsed))

	// task list
	for id, r := range run.Results {
		var icon, style string
		switch r.State {
		case task.StateCompleted:
			icon = "✓"
			style = doneStyle.Render(fmt.Sprintf("%-10s", "done"))
		case task.StateFailed:
			icon = "✗"
			style = failedStyle.Render(fmt.Sprintf("%-10s", "FAILED"))
		case task.StateRunning:
			icon = spinnerChars[m.frame%len(spinnerChars)]
			style = runStyle.Render(fmt.Sprintf("%-10s", "running"))
		default:
			icon = "─"
			style = dimStyle.Render(fmt.Sprintf("%-10s", "queued"))
		}

		info := ""
		if r.Duration > 0 {
			info = dimStyle.Render(r.Duration.Round(time.Second).String())
		}
		if r.RunnerUsed != "" {
			info += " " + dimStyle.Render(r.RunnerUsed)
		}

		b.WriteString(fmt.Sprintf("  %s %s %-30s %s\n", icon, style, id, info))
	}

	return b.String()
}

func (m MissionControlModel) renderHistory() string {
	snap := m.snapshot
	if len(snap.History) == 0 {
		return "  " + dimStyle.Render("No completed runs")
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("  %-12s %-10s %-8s %-8s %-8s %-10s %s\n",
		"RUN ID", "TIME", "FOUND", "NEW", "DONE", "DURATION", "STATUS"))
	b.WriteString("  " + strings.Repeat("─", 70) + "\n")

	for _, h := range snap.History {
		status := doneStyle.Render("ok")
		if h.Failed > 0 {
			status = failedStyle.Render(fmt.Sprintf("%d fail", h.Failed))
		}
		if h.RateLimited > 0 {
			status += " " + warnStyle.Render(fmt.Sprintf("%d rl", h.RateLimited))
		}

		b.WriteString(fmt.Sprintf("  %-12s %-10s %-8d %-8d %-8d %-10s %s\n",
			h.RunID,
			h.StartedAt.Format("15:04:05"),
			h.TasksFound,
			h.TasksNew,
			h.Completed,
			h.Duration.Round(time.Second),
			status))
	}

	return b.String()
}

func (m MissionControlModel) renderFooter() string {
	// runner health summary in footer
	var runners []string
	for _, r := range m.snapshot.Runners {
		status := doneStyle.Render("ok")
		if r.Blacklisted {
			status = failedStyle.Render("bl")
		} else if r.Graylisted {
			status = warnStyle.Render("gl")
		}
		runners = append(runners, fmt.Sprintf("%s(%s)", r.Name, status))
	}

	footer := ""
	if len(runners) > 0 {
		footer = dimStyle.Render("runners: "+strings.Join(runners, " ")) + "\n"
	}
	footer += helpStyle.Render("1-3: tabs  tab: next  j/k: scroll  g: top  q: quit")
	return footer
}
