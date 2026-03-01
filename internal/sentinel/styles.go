package sentinel

import "github.com/charmbracelet/lipgloss"

// TUI styles — reuse color scheme from reporter/tui.go
var (
	headerStyle    = lipgloss.NewStyle().Bold(true)
	tabActiveStyle = lipgloss.NewStyle().Bold(true).Underline(true)
	tabStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	phaseStyle     = lipgloss.NewStyle().Bold(true)
	failedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // red
	runStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("14")) // cyan
	doneStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	warnStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
	dimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // gray
	helpStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

var spinnerChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
