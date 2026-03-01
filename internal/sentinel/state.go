package sentinel

import (
	"sync"
	"time"

	"github.com/ppiankov/runforge/internal/task"
)

// Phase represents the current sentinel loop phase.
type Phase int

const (
	PhaseIdle Phase = iota
	PhaseScanning
	PhasePlanning
	PhaseRunning
	PhaseCooldown
)

func (p Phase) String() string {
	switch p {
	case PhaseIdle:
		return "IDLE"
	case PhaseScanning:
		return "SCANNING"
	case PhasePlanning:
		return "PLANNING"
	case PhaseRunning:
		return "RUNNING"
	case PhaseCooldown:
		return "COOLDOWN"
	default:
		return "UNKNOWN"
	}
}

// RunSummary captures the outcome of a single sentinel run cycle.
type RunSummary struct {
	RunID       string
	StartedAt   time.Time
	EndedAt     time.Time
	Duration    time.Duration
	TasksFound  int
	TasksNew    int
	Completed   int
	Failed      int
	RateLimited int
	Skipped     int
	Source      string // "scan", "generate", "scan+generate"
}

// RunnerHealth tracks per-runner health status for TUI display.
type RunnerHealth struct {
	Name        string
	Available   bool
	Graylisted  bool
	Blacklisted bool
}

// CurrentRunState tracks progress of the active run.
type CurrentRunState struct {
	RunID     string
	StartedAt time.Time
	Total     int
	Results   map[string]*task.TaskResult
}

// StateSnapshot is an immutable copy of SentinelState for TUI rendering.
type StateSnapshot struct {
	Phase          Phase
	PhaseMsg       string
	CurrentRun     *CurrentRunState
	History        []RunSummary
	Runners        []RunnerHealth
	StartedAt      time.Time
	NextScanAt     time.Time
	TotalCompleted int
	TotalFailed    int
	TotalCycles    int
}

const maxHistory = 50

// SentinelState is the shared state container (Watcher pattern).
// The sentinel loop writes; the TUI reads via Snapshot().
type SentinelState struct {
	mu sync.RWMutex

	phase    Phase
	phaseMsg string

	currentRun *CurrentRunState
	history    []RunSummary
	runners    []RunnerHealth

	startedAt  time.Time
	nextScanAt time.Time

	totalCompleted int
	totalFailed    int
	totalCycles    int

	// events channel for TUI notification (buffered, non-blocking)
	events chan struct{}
}

// NewSentinelState creates a new state container.
func NewSentinelState() *SentinelState {
	return &SentinelState{
		startedAt: time.Now(),
		events:    make(chan struct{}, 1),
	}
}

// Events returns the notification channel for TUI subscription.
func (s *SentinelState) Events() <-chan struct{} {
	return s.events
}

// Snapshot returns an immutable copy of the current state.
func (s *SentinelState) Snapshot() StateSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snap := StateSnapshot{
		Phase:          s.phase,
		PhaseMsg:       s.phaseMsg,
		CurrentRun:     s.currentRun,
		StartedAt:      s.startedAt,
		NextScanAt:     s.nextScanAt,
		TotalCompleted: s.totalCompleted,
		TotalFailed:    s.totalFailed,
		TotalCycles:    s.totalCycles,
	}

	// copy slices to prevent mutation
	if len(s.history) > 0 {
		snap.History = make([]RunSummary, len(s.history))
		copy(snap.History, s.history)
	}
	if len(s.runners) > 0 {
		snap.Runners = make([]RunnerHealth, len(s.runners))
		copy(snap.Runners, s.runners)
	}

	return snap
}

// --- Writer methods (called by sentinel loop) ---

// SetPhase updates the current phase and detail message.
func (s *SentinelState) SetPhase(p Phase, msg string) {
	s.mu.Lock()
	s.phase = p
	s.phaseMsg = msg
	s.mu.Unlock()
	s.notify()
}

// SetCurrentRun sets the active run state.
func (s *SentinelState) SetCurrentRun(run *CurrentRunState) {
	s.mu.Lock()
	s.currentRun = run
	s.mu.Unlock()
	s.notify()
}

// UpdateRunResults updates the results map of the current run.
func (s *SentinelState) UpdateRunResults(results map[string]*task.TaskResult) {
	s.mu.Lock()
	if s.currentRun != nil {
		s.currentRun.Results = results
	}
	s.mu.Unlock()
	s.notify()
}

// AddHistory prepends a run summary to the history (most recent first).
func (s *SentinelState) AddHistory(summary RunSummary) {
	s.mu.Lock()
	s.history = append([]RunSummary{summary}, s.history...)
	if len(s.history) > maxHistory {
		s.history = s.history[:maxHistory]
	}
	s.totalCompleted += summary.Completed
	s.totalFailed += summary.Failed
	s.mu.Unlock()
	s.notify()
}

// SetRunners updates the runner health list.
func (s *SentinelState) SetRunners(runners []RunnerHealth) {
	s.mu.Lock()
	s.runners = runners
	s.mu.Unlock()
	s.notify()
}

// SetNextScanAt sets the time when cooldown expires.
func (s *SentinelState) SetNextScanAt(t time.Time) {
	s.mu.Lock()
	s.nextScanAt = t
	s.mu.Unlock()
	s.notify()
}

// IncrementCycle increments the total cycle counter.
func (s *SentinelState) IncrementCycle() {
	s.mu.Lock()
	s.totalCycles++
	s.mu.Unlock()
}

// notify sends a non-blocking signal to the events channel.
func (s *SentinelState) notify() {
	select {
	case s.events <- struct{}{}:
	default:
	}
}
