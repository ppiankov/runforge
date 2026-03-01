package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ppiankov/runforge/internal/config"
	"github.com/ppiankov/runforge/internal/sentinel"
	"github.com/ppiankov/runforge/internal/task"
)

func newSentinelLoopCmd() *cobra.Command {
	var (
		reposDir     string
		owner        string
		workers      int
		runnerName   string
		fallbacks    []string
		cooldown     time.Duration
		maxRuntime   time.Duration
		idleTimeout  time.Duration
		failFast     bool
		scanOnly     bool
		generateOnly bool
		tui          bool
	)

	cmd := &cobra.Command{
		Use:   "loop",
		Short: "Continuous daemon: scan, generate, execute, repeat",
		Long: `Sentinel loop discovers tasks via portfolio scan and/or work order generation,
deduplicates against previously-completed work, executes them via the full
runner cascade, and loops back to discover more.

Leave it running for unattended portfolio maintenance.`,
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
			if !cmd.Flags().Changed("idle-timeout") && cfg.IdleTimeout > 0 {
				idleTimeout = cfg.IdleTimeout
			}
			if !cmd.Flags().Changed("runner") && cfg.DefaultRunner != "" {
				runnerName = cfg.DefaultRunner
			}
			if !cmd.Flags().Changed("fallbacks") && len(cfg.DefaultFallbacks) > 0 {
				fallbacks = cfg.DefaultFallbacks
			}

			absReposDir, err := filepath.Abs(reposDir)
			if err != nil {
				return fmt.Errorf("resolve repos dir: %w", err)
			}

			// build the RunFn that wires sentinel tasks into executeRun
			runFn := buildSentinelRunFn(absReposDir, workers, runnerName, fallbacks, maxRuntime, idleTimeout, failFast, cfg)

			loop, err := sentinel.NewLoop(sentinel.LoopConfig{
				ReposDir:     absReposDir,
				Owner:        owner,
				StateDir:     sentinel.DefaultTrackerPath(),
				Cooldown:     cooldown,
				ScanOnly:     scanOnly,
				GenerateOnly: generateOnly,
				Settings:     cfg,
				RunFn:        runFn,
			})
			if err != nil {
				return fmt.Errorf("init sentinel loop: %w", err)
			}

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			if tui {
				// wire progress callback into sentinel state
				state := loop.State()
				runFnWithProgress := buildSentinelRunFnWithProgress(absReposDir, workers, runnerName, fallbacks, maxRuntime, idleTimeout, failFast, cfg, state)
				// rebuild loop with progress-aware RunFn
				loop, err = sentinel.NewLoop(sentinel.LoopConfig{
					ReposDir:     absReposDir,
					Owner:        owner,
					StateDir:     sentinel.DefaultTrackerPath(),
					Cooldown:     cooldown,
					ScanOnly:     scanOnly,
					GenerateOnly: generateOnly,
					Settings:     cfg,
					RunFn:        runFnWithProgress,
				})
				if err != nil {
					return fmt.Errorf("init sentinel loop: %w", err)
				}
				state = loop.State()

				// start loop in background
				go func() {
					_ = loop.Run(ctx)
				}()

				// run TUI in foreground
				model := sentinel.NewMissionControlModel(state, cancel)
				p := tea.NewProgram(model, tea.WithAltScreen())
				_, err := p.Run()
				return err
			}

			// no TUI — run loop in foreground
			return loop.Run(ctx)
		},
	}

	cmd.Flags().StringVar(&reposDir, "repos-dir", ".", "base directory containing repos")
	cmd.Flags().StringVar(&owner, "owner", "", "GitHub owner for task generation (inferred from git if empty)")
	cmd.Flags().IntVar(&workers, "workers", 4, "max parallel runner processes")
	cmd.Flags().StringVar(&runnerName, "runner", "codex", "primary runner")
	cmd.Flags().StringSliceVar(&fallbacks, "fallbacks", nil, "fallback runners (comma-separated)")
	cmd.Flags().DurationVar(&cooldown, "cooldown", 10*time.Minute, "interval between scan cycles")
	cmd.Flags().DurationVar(&maxRuntime, "max-runtime", 30*time.Minute, "per-task timeout")
	cmd.Flags().DurationVar(&idleTimeout, "idle-timeout", 5*time.Minute, "kill task after no stdout for this duration")
	cmd.Flags().BoolVar(&failFast, "fail-fast", false, "stop spawning new tasks on first failure")
	cmd.Flags().BoolVar(&scanOnly, "scan-only", false, "only discover tasks via portfolio scan")
	cmd.Flags().BoolVar(&generateOnly, "generate-only", false, "only discover tasks via work order generation")
	cmd.Flags().BoolVar(&tui, "tui", false, "show mission control TUI dashboard")

	return cmd
}

// buildSentinelRunFn creates a RunFunc that delegates to executeRun.
func buildSentinelRunFn(reposDir string, workers int, runnerName string, fallbacks []string, maxRuntime, idleTimeout time.Duration, failFast bool, settings *config.Settings) sentinel.RunFunc {
	return buildSentinelRunFnWithProgress(reposDir, workers, runnerName, fallbacks, maxRuntime, idleTimeout, failFast, settings, nil)
}

// buildSentinelRunFnWithProgress creates a RunFunc with optional progress reporting.
func buildSentinelRunFnWithProgress(reposDir string, workers int, runnerName string, fallbacks []string, maxRuntime, idleTimeout time.Duration, failFast bool, settings *config.Settings, state *sentinel.SentinelState) sentinel.RunFunc {
	return func(ctx context.Context, tasks []task.Task, tf *task.TaskFile) (*sentinel.RunResult, error) {
		if tf.DefaultRunner == "" {
			tf.DefaultRunner = runnerName
		}
		if len(tf.DefaultFallbacks) == 0 {
			tf.DefaultFallbacks = fallbacks
		}

		// merge settings into task file
		mergeSettings(tf, settings)

		// stripe runners
		stripeRunners(tasks, tf.DefaultRunner, tf.DefaultFallbacks)

		// build graph
		graph, err := task.BuildGraph(tasks)
		if err != nil {
			return nil, fmt.Errorf("build graph: %w", err)
		}

		cfg := execRunConfig{
			taskFile:    tf,
			tasks:       tasks,
			graph:       graph,
			workers:     workers,
			reposDir:    reposDir,
			maxRuntime:  maxRuntime,
			idleTimeout: idleTimeout,
			failFast:    failFast,
			settings:    settings,
			tuiMode:     "off",
		}

		if state != nil {
			cfg.onProgress = func(results map[string]*task.TaskResult) {
				state.UpdateRunResults(results)
			}
		}

		result, err := executeRun(cfg)
		if err != nil {
			return nil, err
		}

		report := result.report
		return &sentinel.RunResult{
			RunID:       report.RunID,
			Results:     report.Results,
			Duration:    report.TotalDuration,
			Completed:   report.Completed,
			Failed:      report.Failed,
			RateLimited: report.RateLimited,
			Skipped:     report.Skipped,
		}, nil
	}
}
