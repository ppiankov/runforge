package sentinel

import (
	"testing"
	"time"

	"github.com/ppiankov/runforge/internal/task"
)

func TestSentinelState_InitialPhase(t *testing.T) {
	s := NewSentinelState()
	snap := s.Snapshot()
	if snap.Phase != PhaseIdle {
		t.Fatalf("expected PhaseIdle, got %v", snap.Phase)
	}
	if snap.TotalCycles != 0 {
		t.Fatalf("expected 0 cycles, got %d", snap.TotalCycles)
	}
}

func TestSentinelState_SetPhase(t *testing.T) {
	s := NewSentinelState()
	s.SetPhase(PhaseScanning, "12 repos")
	snap := s.Snapshot()
	if snap.Phase != PhaseScanning {
		t.Fatalf("expected PhaseScanning, got %v", snap.Phase)
	}
	if snap.PhaseMsg != "12 repos" {
		t.Fatalf("expected '12 repos', got %q", snap.PhaseMsg)
	}
}

func TestSentinelState_CurrentRun(t *testing.T) {
	s := NewSentinelState()
	run := &CurrentRunState{
		RunID:     "abc123",
		StartedAt: time.Now(),
		Total:     5,
		Results:   make(map[string]*task.TaskResult),
	}
	s.SetCurrentRun(run)
	snap := s.Snapshot()
	if snap.CurrentRun == nil || snap.CurrentRun.RunID != "abc123" {
		t.Fatal("expected current run with RunID abc123")
	}
}

func TestSentinelState_UpdateRunResults(t *testing.T) {
	s := NewSentinelState()
	s.SetCurrentRun(&CurrentRunState{RunID: "abc", Results: nil})

	results := map[string]*task.TaskResult{
		"t1": {TaskID: "t1", State: task.StateCompleted},
	}
	s.UpdateRunResults(results)

	snap := s.Snapshot()
	if snap.CurrentRun == nil || len(snap.CurrentRun.Results) != 1 {
		t.Fatal("expected 1 result in current run")
	}
}

func TestSentinelState_History(t *testing.T) {
	s := NewSentinelState()
	s.AddHistory(RunSummary{RunID: "run-1", Completed: 3, Failed: 1})
	s.AddHistory(RunSummary{RunID: "run-2", Completed: 5, Failed: 0})

	snap := s.Snapshot()
	if len(snap.History) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(snap.History))
	}
	// most recent first
	if snap.History[0].RunID != "run-2" {
		t.Fatalf("expected run-2 first, got %s", snap.History[0].RunID)
	}
	if snap.TotalCompleted != 8 {
		t.Fatalf("expected 8 total completed, got %d", snap.TotalCompleted)
	}
	if snap.TotalFailed != 1 {
		t.Fatalf("expected 1 total failed, got %d", snap.TotalFailed)
	}
}

func TestSentinelState_HistoryCapped(t *testing.T) {
	s := NewSentinelState()
	for i := 0; i < maxHistory+10; i++ {
		s.AddHistory(RunSummary{RunID: "run"})
	}
	snap := s.Snapshot()
	if len(snap.History) != maxHistory {
		t.Fatalf("expected history capped at %d, got %d", maxHistory, len(snap.History))
	}
}

func TestSentinelState_Runners(t *testing.T) {
	s := NewSentinelState()
	s.SetRunners([]RunnerHealth{
		{Name: "codex", Available: true},
		{Name: "gemini", Graylisted: true},
	})
	snap := s.Snapshot()
	if len(snap.Runners) != 2 {
		t.Fatalf("expected 2 runners, got %d", len(snap.Runners))
	}
}

func TestSentinelState_IncrementCycle(t *testing.T) {
	s := NewSentinelState()
	s.IncrementCycle()
	s.IncrementCycle()
	snap := s.Snapshot()
	if snap.TotalCycles != 2 {
		t.Fatalf("expected 2 cycles, got %d", snap.TotalCycles)
	}
}

func TestSentinelState_SnapshotIsolation(t *testing.T) {
	s := NewSentinelState()
	s.AddHistory(RunSummary{RunID: "run-1"})
	snap := s.Snapshot()

	// mutate original state
	s.AddHistory(RunSummary{RunID: "run-2"})

	// snapshot should be unchanged
	if len(snap.History) != 1 {
		t.Fatal("snapshot should be isolated from further mutations")
	}
}

func TestSentinelState_EventsNotify(t *testing.T) {
	s := NewSentinelState()
	s.SetPhase(PhaseScanning, "test")

	// should have a pending event
	select {
	case <-s.Events():
		// ok
	default:
		t.Fatal("expected event after SetPhase")
	}

	// second read should not block (channel drained)
	select {
	case <-s.Events():
		t.Fatal("should not have second event")
	default:
		// ok
	}
}

func TestPhaseString(t *testing.T) {
	tests := []struct {
		phase Phase
		want  string
	}{
		{PhaseIdle, "IDLE"},
		{PhaseScanning, "SCANNING"},
		{PhasePlanning, "PLANNING"},
		{PhaseRunning, "RUNNING"},
		{PhaseCooldown, "COOLDOWN"},
		{Phase(99), "UNKNOWN"},
	}
	for _, tc := range tests {
		if got := tc.phase.String(); got != tc.want {
			t.Errorf("Phase(%d).String() = %q, want %q", tc.phase, got, tc.want)
		}
	}
}
