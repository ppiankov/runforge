package sentinel

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMissionControl_Init(t *testing.T) {
	state := NewSentinelState()
	m := NewMissionControlModel(state, nil)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init should return a tick command")
	}
}

func TestMissionControl_TabSwitch(t *testing.T) {
	state := NewSentinelState()
	m := NewMissionControlModel(state, nil)
	m.width = 80
	m.height = 24

	// default tab is 0
	if m.tab != 0 {
		t.Fatalf("expected tab 0, got %d", m.tab)
	}

	// switch to tab 2
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	model := m2.(MissionControlModel)
	if model.tab != 1 {
		t.Fatalf("expected tab 1, got %d", model.tab)
	}

	// switch to tab 3
	m3, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	model = m3.(MissionControlModel)
	if model.tab != 2 {
		t.Fatalf("expected tab 2, got %d", model.tab)
	}
}

func TestMissionControl_ViewRenders(t *testing.T) {
	state := NewSentinelState()
	state.SetPhase(PhaseScanning, "5 repos")
	state.AddHistory(RunSummary{
		RunID:     "abc123",
		StartedAt: time.Now(),
		Duration:  2 * time.Minute,
		Completed: 3,
		Failed:    1,
		TasksNew:  4,
	})

	m := NewMissionControlModel(state, nil)
	m.width = 100
	m.height = 30
	m.snapshot = state.Snapshot()

	view := m.View()
	if !strings.Contains(view, "runforge sentinel") {
		t.Error("view should contain header")
	}
	if !strings.Contains(view, "Overview") {
		t.Error("view should contain tab names")
	}
}

func TestMissionControl_HistoryTab(t *testing.T) {
	state := NewSentinelState()
	state.AddHistory(RunSummary{
		RunID:      "run-1",
		StartedAt:  time.Now(),
		TasksFound: 10,
		TasksNew:   5,
		Completed:  4,
		Failed:     1,
		Duration:   3 * time.Minute,
	})

	m := NewMissionControlModel(state, nil)
	m.width = 100
	m.height = 30
	m.tab = 2
	m.snapshot = state.Snapshot()

	view := m.View()
	if !strings.Contains(view, "run-1") {
		t.Error("history tab should show run ID")
	}
	if !strings.Contains(view, "1 fail") {
		t.Error("history tab should show failure count")
	}
}

func TestMissionControl_QuitCancels(t *testing.T) {
	state := NewSentinelState()
	cancelled := false
	m := NewMissionControlModel(state, func() { cancelled = true })
	m.width = 80
	m.height = 24

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !cancelled {
		t.Error("q should trigger cancel function")
	}
	if cmd == nil {
		t.Error("q should return tea.Quit")
	}
}
