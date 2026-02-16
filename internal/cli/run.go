package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/runforge/internal/config"
	"github.com/ppiankov/runforge/internal/reporter"
	"github.com/ppiankov/runforge/internal/runner"
	"github.com/ppiankov/runforge/internal/task"
)

func newRunCmd() *cobra.Command {
	var (
		tasksFile  string
		workers    int
		verify     bool
		reposDir   string
		filter     string
		dryRun     bool
		maxRuntime time.Duration
		failFast   bool
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Execute tasks with dependency-aware parallelism",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTasks(tasksFile, workers, verify, reposDir, filter, dryRun, maxRuntime, failFast)
		},
	}

	cmd.Flags().StringVar(&tasksFile, "tasks", "runforge.json", "path to tasks JSON file")
	cmd.Flags().IntVar(&workers, "workers", 4, "max parallel runner processes")
	cmd.Flags().BoolVar(&verify, "verify", false, "run make test && make lint per repo after completion")
	cmd.Flags().StringVar(&reposDir, "repos-dir", ".", "base directory containing repos")
	cmd.Flags().StringVar(&filter, "filter", "", "only run tasks matching ID glob pattern")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show execution plan without running")
	cmd.Flags().DurationVar(&maxRuntime, "max-runtime", 30*time.Minute, "per-task timeout duration")
	cmd.Flags().BoolVar(&failFast, "fail-fast", false, "stop spawning new tasks on first failure")

	return cmd
}

func runTasks(tasksFile string, workers int, verify bool, reposDir, filter string, dryRun bool, maxRuntime time.Duration, failFast bool) error {
	// load tasks
	tf, err := config.Load(tasksFile)
	if err != nil {
		return fmt.Errorf("load tasks: %w", err)
	}

	// apply filter
	tasks := tf.Tasks
	if filter != "" {
		tasks = filterTasks(tasks, filter)
		if len(tasks) == 0 {
			return fmt.Errorf("no tasks match filter %q", filter)
		}
	}

	// resolve repos dir
	reposDir, err = filepath.Abs(reposDir)
	if err != nil {
		return fmt.Errorf("resolve repos dir: %w", err)
	}

	// validate repos exist
	filteredTF := &task.TaskFile{Tasks: tasks}
	if !dryRun {
		if err := config.ValidateRepos(filteredTF, reposDir); err != nil {
			return err
		}
	}

	// build graph
	graph, err := task.BuildGraph(tasks)
	if err != nil {
		return fmt.Errorf("build graph: %w", err)
	}

	// detect TTY for color
	isTTY := isTerminal()
	textRep := reporter.NewTextReporter(os.Stdout, isTTY)

	// dry run
	if dryRun {
		textRep.PrintHeader(len(tasks), workers)
		textRep.PrintDryRun(graph, reposDir)
		return nil
	}

	report, err := executeRun(execRunConfig{
		tasksFile:  tasksFile,
		tasks:      tasks,
		graph:      graph,
		workers:    workers,
		reposDir:   reposDir,
		filter:     filter,
		maxRuntime: maxRuntime,
		failFast:   failFast,
	})
	if err != nil {
		return err
	}

	// verify if requested
	if verify {
		ctx := context.Background()
		fmt.Fprintln(os.Stdout, "\n--- Verification ---")
		repos := collectRepos(tasks)
		for _, repo := range repos {
			repoPath := config.RepoPath(repo, reposDir)
			vr := runner.Verify(ctx, repo, repoPath, report.runDir)
			if vr.Passed {
				fmt.Fprintf(os.Stdout, "  ✓ %s\n", repo)
			} else {
				fmt.Fprintf(os.Stdout, "  ✗ %s: %s\n", repo, vr.Error)
			}
		}
	}

	return report.err()
}

// execRunConfig holds parameters for executeRun.
type execRunConfig struct {
	tasksFile  string
	tasks      []task.Task
	graph      *task.Graph
	workers    int
	reposDir   string
	filter     string
	maxRuntime time.Duration
	failFast   bool
}

// execRunResult wraps the report and run directory.
type execRunResult struct {
	report *task.RunReport
	runDir string
}

func (r *execRunResult) err() error {
	if r.report.RateLimited > 0 {
		return &RateLimitError{
			Count:    r.report.RateLimited,
			ResetsAt: r.report.ResetsAt,
		}
	}
	if r.report.Failed > 0 {
		return fmt.Errorf("%d tasks failed", r.report.Failed)
	}
	return nil
}

// executeRun is the shared execution core used by both run and rerun commands.
func executeRun(cfg execRunConfig) (*execRunResult, error) {
	isTTY := isTerminal()
	textRep := reporter.NewTextReporter(os.Stdout, isTTY)

	// prepare run directory
	runDir := filepath.Join(".runforge", time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, fmt.Errorf("create run dir: %w", err)
	}

	slog.Info("starting run", "tasks", len(cfg.tasks), "workers", cfg.workers, "run_dir", runDir)
	textRep.PrintHeader(len(cfg.tasks), cfg.workers)

	// setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\ninterrupted — waiting for running tasks to finish...")
		cancel()
	}()

	// create runner registry
	runners := map[string]runner.Runner{
		"codex":  runner.NewCodexRunner(),
		"claude": runner.NewClaudeRunner(),
		"script": runner.NewScriptRunner(),
	}
	const defaultRunner = "codex"

	execFn := func(ctx context.Context, t *task.Task, repoDir, outputDir string) *task.TaskResult {
		taskCtx, taskCancel := context.WithTimeout(ctx, cfg.maxRuntime)
		defer taskCancel()

		name := t.Runner
		if name == "" {
			name = defaultRunner
		}
		r, ok := runners[name]
		if !ok {
			return &task.TaskResult{
				TaskID:  t.ID,
				State:   task.StateFailed,
				Error:   fmt.Sprintf("unknown runner: %q", name),
				EndedAt: time.Now(),
			}
		}
		return r.Run(taskCtx, t, repoDir, outputDir)
	}

	// run scheduler
	start := time.Now()
	sched := task.NewScheduler(cfg.graph, task.SchedulerConfig{
		Workers:  cfg.workers,
		ReposDir: cfg.reposDir,
		RunDir:   runDir,
		ExecFn:   execFn,
		FailFast: cfg.failFast,
		OnUpdate: func(id string, result *task.TaskResult) {
			slog.Debug("task update", "task", id, "state", result.State)
		},
	})

	// start live display if TTY
	var live *reporter.LiveReporter
	if isTTY {
		live = reporter.NewLiveReporter(os.Stdout, true, cfg.graph, sched.Results)
		live.Start()
	}

	results := sched.Run(ctx)
	totalDuration := time.Since(start)

	if live != nil {
		live.Stop()
	}

	report := buildReport(cfg.tasksFile, cfg.workers, cfg.filter, cfg.reposDir, results, totalDuration)
	textRep.PrintStatus(cfg.graph, results)
	textRep.PrintSummary(report)

	reportPath := filepath.Join(runDir, "report.json")
	if err := reporter.WriteJSONReport(report, reportPath); err != nil {
		slog.Warn("failed to write report", "error", err)
	} else {
		fmt.Fprintf(os.Stdout, "\nReport: %s\n", reportPath)
	}

	sarifPath := filepath.Join(runDir, "report.sarif")
	if err := reporter.WriteSARIFReport(report, cfg.graph, sarifPath); err != nil {
		slog.Warn("failed to write sarif report", "error", err)
	}

	return &execRunResult{report: report, runDir: runDir}, nil
}

// RateLimitError indicates the run was stopped due to API rate limiting.
// Callers should map this to exit code 4.
type RateLimitError struct {
	Count    int
	ResetsAt time.Time
}

func (e *RateLimitError) Error() string {
	if !e.ResetsAt.IsZero() {
		return fmt.Sprintf("%d tasks rate-limited (resets at %s)", e.Count, e.ResetsAt.Format(time.Kitchen))
	}
	return fmt.Sprintf("%d tasks rate-limited", e.Count)
}

func buildReport(tasksFile string, workers int, filter, reposDir string, results map[string]*task.TaskResult, duration time.Duration) *task.RunReport {
	report := &task.RunReport{
		Timestamp:     time.Now(),
		TasksFile:     tasksFile,
		Workers:       workers,
		Filter:        filter,
		ReposDir:      reposDir,
		Results:       results,
		TotalTasks:    len(results),
		TotalDuration: duration,
	}

	for _, r := range results {
		switch r.State {
		case task.StateCompleted:
			report.Completed++
		case task.StateFailed:
			report.Failed++
		case task.StateSkipped:
			report.Skipped++
		case task.StateRateLimited:
			report.RateLimited++
			if !r.ResetsAt.IsZero() && (report.ResetsAt.IsZero() || r.ResetsAt.After(report.ResetsAt)) {
				report.ResetsAt = r.ResetsAt
			}
		}
	}

	return report
}

func filterTasks(tasks []task.Task, pattern string) []task.Task {
	var filtered []task.Task
	for _, t := range tasks {
		if matchGlob(t.ID, pattern) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// matchGlob does simple glob matching supporting * wildcard.
func matchGlob(s, pattern string) bool {
	// handle exact match
	if s == pattern {
		return true
	}

	// handle * at end: "prefix*"
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(s, prefix)
	}

	// handle * at start: "*suffix"
	if strings.HasPrefix(pattern, "*") {
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(s, suffix)
	}

	// handle * in middle: "prefix*suffix"
	if idx := strings.Index(pattern, "*"); idx >= 0 {
		prefix := pattern[:idx]
		suffix := pattern[idx+1:]
		return strings.HasPrefix(s, prefix) && strings.HasSuffix(s, suffix)
	}

	return false
}

func collectRepos(tasks []task.Task) []string {
	seen := make(map[string]struct{})
	var repos []string
	for _, t := range tasks {
		if _, ok := seen[t.Repo]; ok {
			continue
		}
		seen[t.Repo] = struct{}{}
		repos = append(repos, t.Repo)
	}
	return repos
}

// isTerminal checks if stdout is a terminal.
func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
