package sentinel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/ppiankov/runforge/internal/config"
	"github.com/ppiankov/runforge/internal/scan"
	"github.com/ppiankov/runforge/internal/task"
)

// RunResult captures the outcome of an executeRun call.
type RunResult struct {
	RunID       string
	Results     map[string]*task.TaskResult
	Duration    time.Duration
	Completed   int
	Failed      int
	RateLimited int
	Skipped     int
}

// RunFunc is the function signature for executing a set of tasks.
// Injected from the CLI layer to break import cycles with executeRun.
type RunFunc func(ctx context.Context, tasks []task.Task, tf *task.TaskFile) (*RunResult, error)

// LoopConfig holds configuration for the sentinel loop daemon.
type LoopConfig struct {
	ReposDir     string
	Owner        string
	StateDir     string // persistent state directory (~/.runforge/sentinel/)
	Cooldown     time.Duration
	ScanOnly     bool
	GenerateOnly bool
	Settings     *config.Settings
	RunFn        RunFunc // injected execution function
}

// Loop is the continuous sentinel daemon: scan → dedup → run → cooldown → repeat.
type Loop struct {
	cfg     LoopConfig
	state   *SentinelState
	tracker *CompletionTracker
}

// NewLoop creates a sentinel loop.
func NewLoop(cfg LoopConfig) (*Loop, error) {
	if cfg.ReposDir == "" {
		return nil, fmt.Errorf("repos-dir is required")
	}
	if cfg.RunFn == nil {
		return nil, fmt.Errorf("run function is required")
	}
	if cfg.Cooldown == 0 {
		cfg.Cooldown = 10 * time.Minute
	}
	if cfg.StateDir == "" {
		cfg.StateDir = DefaultTrackerPath()
	}

	return &Loop{
		cfg:     cfg,
		state:   NewSentinelState(),
		tracker: NewCompletionTracker(cfg.StateDir),
	}, nil
}

// State returns the shared state for TUI consumption.
func (l *Loop) State() *SentinelState {
	return l.state
}

// Run starts the sentinel loop. Blocks until ctx is cancelled.
func (l *Loop) Run(ctx context.Context) error {
	slog.Info("sentinel loop started", "repos_dir", l.cfg.ReposDir, "cooldown", l.cfg.Cooldown)

	for {
		l.state.IncrementCycle()
		if err := l.cycle(ctx); err != nil {
			slog.Warn("sentinel cycle error", "error", err)
		}

		// cooldown
		l.state.SetPhase(PhaseCooldown, fmt.Sprintf("next scan in %s", l.cfg.Cooldown))
		l.state.SetNextScanAt(time.Now().Add(l.cfg.Cooldown))

		select {
		case <-ctx.Done():
			l.state.SetPhase(PhaseIdle, "stopped")
			return nil
		case <-time.After(l.cfg.Cooldown):
		}
	}
}

// cycle runs one scan → dedup → run iteration.
func (l *Loop) cycle(ctx context.Context) error {
	// discover tasks
	tasks, source, err := l.discover(ctx)
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}

	tasksFound := len(tasks)
	if tasksFound == 0 {
		slog.Info("sentinel: no findings", "source", source)
		return nil
	}

	// dedup
	l.state.SetPhase(PhasePlanning, fmt.Sprintf("%d findings, deduplicating", tasksFound))
	newTasks := l.tracker.FilterNew(tasks)
	tasksNew := len(newTasks)

	slog.Info("sentinel: tasks discovered", "found", tasksFound, "new", tasksNew, "source", source)

	if tasksNew == 0 {
		slog.Info("sentinel: no new tasks after dedup")
		return nil
	}

	// build task file
	tf := &task.TaskFile{
		Tasks:       newTasks,
		Description: fmt.Sprintf("Sentinel auto-generated (%s)", source),
	}

	// run
	l.state.SetPhase(PhaseRunning, fmt.Sprintf("%d tasks", tasksNew))
	l.state.SetCurrentRun(&CurrentRunState{
		StartedAt: time.Now(),
		Total:     tasksNew,
		Results:   make(map[string]*task.TaskResult),
	})

	start := time.Now()
	result, err := l.cfg.RunFn(ctx, newTasks, tf)
	duration := time.Since(start)

	if err != nil {
		slog.Warn("sentinel: run failed", "error", err)
		// still record what we can from partial results
	}

	// record completed tasks in tracker
	if result != nil {
		for id, res := range result.Results {
			if res.State == task.StateCompleted {
				l.tracker.Record(id, result.RunID, res.RunnerUsed)
			}
		}
	}

	// build summary
	summary := RunSummary{
		StartedAt:  start,
		EndedAt:    time.Now(),
		Duration:   duration,
		TasksFound: tasksFound,
		TasksNew:   tasksNew,
		Source:     source,
	}
	if result != nil {
		summary.RunID = result.RunID
		summary.Completed = result.Completed
		summary.Failed = result.Failed
		summary.RateLimited = result.RateLimited
		summary.Skipped = result.Skipped
	}

	l.state.AddHistory(summary)
	l.state.SetCurrentRun(nil)

	slog.Info("sentinel: cycle complete",
		"run_id", summary.RunID,
		"completed", summary.Completed,
		"failed", summary.Failed,
		"duration", duration.Round(time.Second))

	return nil
}

// discover scans repos and/or generates tasks from work orders.
func (l *Loop) discover(ctx context.Context) ([]task.Task, string, error) {
	_ = ctx // reserved for future cancellation

	var allTasks []task.Task
	var sources []string

	// scan-based discovery
	if !l.cfg.GenerateOnly {
		l.state.SetPhase(PhaseScanning, "scanning repos")
		scanResult, err := scan.Scan(scan.ScanOptions{
			ReposDir:    l.cfg.ReposDir,
			MinSeverity: scan.SeverityWarning, // skip info-level
		})
		if err != nil {
			slog.Warn("sentinel: scan error", "error", err)
		} else if len(scanResult.Findings) > 0 {
			tasks := findingsToTasks(scanResult, l.cfg.Owner)
			allTasks = append(allTasks, tasks...)
			sources = append(sources, "scan")
			l.state.SetPhase(PhaseScanning, fmt.Sprintf("found %d scan findings", len(tasks)))
		}
	}

	// generate-based discovery (work orders)
	if !l.cfg.ScanOnly {
		l.state.SetPhase(PhaseScanning, "parsing work orders")
		woTasks, err := discoverWorkOrders(l.cfg.ReposDir, l.cfg.Owner, l.cfg.Settings)
		if err != nil {
			slog.Warn("sentinel: generate error", "error", err)
		} else if len(woTasks) > 0 {
			allTasks = append(allTasks, woTasks...)
			sources = append(sources, "generate")
		}
	}

	source := "scan+generate"
	if len(sources) == 1 {
		source = sources[0]
	} else if len(sources) == 0 {
		source = "none"
	}

	return allTasks, source, nil
}

// findingsToTasks converts scan findings to tasks using the TaskFormatter.
func findingsToTasks(result *scan.ScanResult, owner string) []task.Task {
	var buf bytes.Buffer
	formatter := scan.NewTaskFormatter(owner, "")
	if err := formatter.Format(&buf, result); err != nil {
		slog.Warn("sentinel: task format error", "error", err)
		return nil
	}

	if buf.Len() == 0 {
		return nil
	}

	var tf task.TaskFile
	if err := json.Unmarshal(buf.Bytes(), &tf); err != nil {
		slog.Warn("sentinel: task parse error", "error", err)
		return nil
	}
	return tf.Tasks
}

// discoverWorkOrders scans repos for docs/work-orders.md and generates tasks
// from planned ([ ]) work orders.
func discoverWorkOrders(reposDir, owner string, _ *config.Settings) ([]task.Task, error) {
	// reuse the generate command's logic via scan-based WO discovery
	// scan repos for docs/work-orders.md files and parse planned WOs
	scanResult, err := scan.Scan(scan.ScanOptions{
		ReposDir:   reposDir,
		Categories: []string{"quality"}, // WO-related checks
	})
	if err != nil {
		return nil, err
	}

	// filter for work-order related findings only
	var woFindings []scan.Finding
	for _, f := range scanResult.Findings {
		if f.Check == "stale_work_orders" || f.Check == "no_work_orders" {
			woFindings = append(woFindings, f)
		}
	}

	if len(woFindings) == 0 {
		return nil, nil
	}

	woResult := &scan.ScanResult{
		ReposScanned: scanResult.ReposScanned,
		Findings:     woFindings,
	}
	return findingsToTasks(woResult, owner), nil
}
