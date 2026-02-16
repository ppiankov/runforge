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

	// prepare run directory
	runDir := filepath.Join(".runforge", time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("create run dir: %w", err)
	}

	slog.Info("starting run", "tasks", len(tasks), "workers", workers, "run_dir", runDir)
	textRep.PrintHeader(len(tasks), workers)

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
		"script": runner.NewScriptRunner(),
	}
	const defaultRunner = "codex"

	execFn := func(ctx context.Context, t *task.Task, repoDir, outputDir string) *task.TaskResult {
		// apply per-task timeout
		taskCtx, taskCancel := context.WithTimeout(ctx, maxRuntime)
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
	sched := task.NewScheduler(graph, task.SchedulerConfig{
		Workers:  workers,
		ReposDir: reposDir,
		RunDir:   runDir,
		ExecFn:   execFn,
		FailFast: failFast,
		OnUpdate: func(id string, result *task.TaskResult) {
			slog.Debug("task update", "task", id, "state", result.State)
		},
	})

	// start live display if TTY
	var live *reporter.LiveReporter
	if isTTY {
		live = reporter.NewLiveReporter(os.Stdout, true, graph, sched.Results)
		live.Start()
	}

	results := sched.Run(ctx)
	totalDuration := time.Since(start)

	// stop live display before printing final status
	if live != nil {
		live.Stop()
	}

	// build report
	report := buildReport(tasksFile, workers, filter, reposDir, results, totalDuration)
	textRep.PrintStatus(graph, results)
	textRep.PrintSummary(report)

	// write JSON report
	reportPath := filepath.Join(runDir, "report.json")
	if err := reporter.WriteJSONReport(report, reportPath); err != nil {
		slog.Warn("failed to write report", "error", err)
	} else {
		fmt.Fprintf(os.Stdout, "\nReport: %s\n", reportPath)
	}

	// verify if requested
	if verify {
		fmt.Fprintln(os.Stdout, "\n--- Verification ---")
		repos := collectRepos(tasks)
		for _, repo := range repos {
			repoPath := config.RepoPath(repo, reposDir)
			vr := runner.Verify(ctx, repo, repoPath, runDir)
			if vr.Passed {
				fmt.Fprintf(os.Stdout, "  ✓ %s\n", repo)
			} else {
				fmt.Fprintf(os.Stdout, "  ✗ %s: %s\n", repo, vr.Error)
			}
		}
	}

	if report.RateLimited > 0 {
		return &RateLimitError{
			Count:    report.RateLimited,
			ResetsAt: report.ResetsAt,
		}
	}
	if report.Failed > 0 {
		return fmt.Errorf("%d tasks failed", report.Failed)
	}
	return nil
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
