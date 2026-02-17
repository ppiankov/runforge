package cli

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/runforge/internal/config"
	"github.com/ppiankov/runforge/internal/reporter"
	"github.com/ppiankov/runforge/internal/task"
)

func newRerunCmd() *cobra.Command {
	var (
		runDir     string
		workers    int
		reposDir   string
		maxRuntime time.Duration
		failFast   bool
	)

	cmd := &cobra.Command{
		Use:   "rerun",
		Short: "Re-execute failed, skipped, and rate-limited tasks from a previous run",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadSettings(configFile)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if !cmd.Flags().Changed("workers") && cfg.Workers > 0 {
				workers = cfg.Workers
			}
			if !cmd.Flags().Changed("repos-dir") && cfg.ReposDir != "" {
				reposDir = cfg.ReposDir
			}
			if !cmd.Flags().Changed("max-runtime") && cfg.MaxRuntime > 0 {
				maxRuntime = cfg.MaxRuntime
			}
			if !cmd.Flags().Changed("fail-fast") && cfg.FailFast {
				failFast = cfg.FailFast
			}
			return rerunTasks(runDir, workers, reposDir, maxRuntime, failFast, cfg)
		},
	}

	cmd.Flags().StringVar(&runDir, "run-dir", "", "path to previous run directory (required)")
	cmd.Flags().IntVar(&workers, "workers", 0, "override worker count (0 = use original)")
	cmd.Flags().StringVar(&reposDir, "repos-dir", "", "override repos directory (empty = use original)")
	cmd.Flags().DurationVar(&maxRuntime, "max-runtime", 30*time.Minute, "per-task timeout duration")
	cmd.Flags().BoolVar(&failFast, "fail-fast", false, "stop spawning new tasks on first failure")
	_ = cmd.MarkFlagRequired("run-dir")

	return cmd
}

func rerunTasks(runDir string, workers int, reposDir string, maxRuntime time.Duration, failFast bool, cfg *config.Settings) error {
	// load previous report
	reportPath := filepath.Join(runDir, "report.json")
	prevReport, err := reporter.ReadJSONReport(reportPath)
	if err != nil {
		return fmt.Errorf("load previous report: %w", err)
	}

	// identify rerunnable tasks
	rerunIDs := make(map[string]bool)
	for id, result := range prevReport.Results {
		switch result.State {
		case task.StateFailed, task.StateSkipped, task.StateRateLimited:
			rerunIDs[id] = true
		}
	}

	if len(rerunIDs) == 0 {
		fmt.Println("no tasks to rerun — all tasks completed successfully")
		return nil
	}

	// warn if rate limit may still be active
	if !prevReport.ResetsAt.IsZero() && time.Now().Before(prevReport.ResetsAt) {
		remaining := time.Until(prevReport.ResetsAt).Truncate(time.Minute)
		slog.Warn("rate limit may still be active", "resets_in", remaining)
	}

	// load original task file
	tf, err := config.Load(prevReport.TasksFile)
	if err != nil {
		return fmt.Errorf("load original task file %q: %w", prevReport.TasksFile, err)
	}

	// merge current config runner profiles — rerun should use the latest
	// runner configuration, not the stale one baked into the original task file.
	// This ensures new fallbacks (e.g. deepseek) are available on rerun.
	if cfg != nil {
		if cfg.DefaultRunner != "" {
			tf.DefaultRunner = cfg.DefaultRunner
		}
		if len(cfg.DefaultFallbacks) > 0 {
			tf.DefaultFallbacks = cfg.DefaultFallbacks
		}
		if len(cfg.Runners) > 0 {
			if tf.Runners == nil {
				tf.Runners = make(map[string]*task.RunnerProfileConfig)
			}
			for name, rp := range cfg.Runners {
				tf.Runners[name] = &task.RunnerProfileConfig{
					Type:  rp.Type,
					Model: rp.Model,
					Env:   rp.Env,
				}
			}
		}
	}

	// filter to rerunnable tasks and strip completed dependencies
	var tasks []task.Task
	var missing []string
	for _, t := range tf.Tasks {
		if !rerunIDs[t.ID] {
			continue
		}
		// strip deps that reference completed tasks (not in rerun set)
		var kept []string
		for _, dep := range t.DependsOn {
			if rerunIDs[dep] {
				kept = append(kept, dep)
			}
		}
		t.DependsOn = kept
		tasks = append(tasks, t)
	}

	// stripe runner assignments for parallel provider utilization
	rerunDefault := tf.DefaultRunner
	if rerunDefault == "" {
		rerunDefault = "codex"
	}
	stripeRunners(tasks, rerunDefault, tf.DefaultFallbacks)

	// check for tasks in report but missing from task file
	found := make(map[string]bool)
	for _, t := range tasks {
		found[t.ID] = true
	}
	for id := range rerunIDs {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		slog.Warn("some tasks from report not found in task file", "missing", missing)
	}

	if len(tasks) == 0 {
		return fmt.Errorf("no rerunnable tasks found in task file %q", prevReport.TasksFile)
	}

	// use original values unless overridden
	if workers == 0 {
		workers = prevReport.Workers
	}
	if reposDir == "" {
		reposDir = prevReport.ReposDir
	}
	reposDir, err = filepath.Abs(reposDir)
	if err != nil {
		return fmt.Errorf("resolve repos dir: %w", err)
	}

	// validate repos exist
	if err := config.ValidateRepos(&task.TaskFile{Tasks: tasks}, reposDir); err != nil {
		return err
	}

	// build graph
	graph, err := task.BuildGraph(tasks)
	if err != nil {
		return fmt.Errorf("build graph: %w", err)
	}

	fmt.Printf("rerunning %d tasks (%d failed, %d skipped, %d rate-limited)\n",
		len(tasks), countState(prevReport, task.StateFailed, rerunIDs),
		countState(prevReport, task.StateSkipped, rerunIDs),
		countState(prevReport, task.StateRateLimited, rerunIDs))

	result, err := executeRun(execRunConfig{
		tasksFile:  prevReport.TasksFile,
		taskFile:   tf,
		tasks:      tasks,
		graph:      graph,
		workers:    workers,
		reposDir:   reposDir,
		maxRuntime: maxRuntime,
		failFast:   failFast,
		postRun:    cfg.PostRun,
	})
	if err != nil {
		return err
	}

	return result.err()
}

// countState counts how many tasks in the rerun set had the given state in the previous report.
func countState(report *task.RunReport, state task.TaskState, rerunIDs map[string]bool) int {
	n := 0
	for id, r := range report.Results {
		if rerunIDs[id] && r.State == state {
			n++
		}
	}
	return n
}
