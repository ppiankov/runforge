package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ppiankov/neurorouter"
	"github.com/ppiankov/tokencontrol/internal/config"
	"github.com/ppiankov/tokencontrol/internal/reporter"
	"github.com/ppiankov/tokencontrol/internal/runner"
	"github.com/ppiankov/tokencontrol/internal/state"
	"github.com/ppiankov/tokencontrol/internal/task"
)

func newRunCmd() *cobra.Command {
	var (
		tasksFile    string
		workers      int
		verify       bool
		reposDir     string
		filter       string
		dryRun       bool
		maxRuntime   time.Duration
		idleTimeout  time.Duration
		failFast     bool
		tuiMode      string
		allowFree    bool
		retry        bool
		noAutoCommit bool
		parallelRepo bool
		maxRetries   int

		strictReadiness bool

		codexQuotaRemaining int
		codexQuotaReserve   int
		codexQuotaLookback  int
		codexQuotaSafety    float64
		codexQuotaEnforce   bool
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Execute tasks with dependency-aware parallelism",
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
			if !cmd.Flags().Changed("fail-fast") && cfg.FailFast {
				failFast = cfg.FailFast
			}
			if !cmd.Flags().Changed("verify") && cfg.Verify {
				verify = cfg.Verify
			}
			if cfg.CodexQuota != nil {
				if !cmd.Flags().Changed("codex-quota-remaining") && cfg.CodexQuota.RemainingTokens > 0 {
					codexQuotaRemaining = cfg.CodexQuota.RemainingTokens
				}
				if !cmd.Flags().Changed("codex-quota-reserve") && cfg.CodexQuota.ReserveTokens >= 0 {
					codexQuotaReserve = cfg.CodexQuota.ReserveTokens
				}
				if !cmd.Flags().Changed("codex-quota-safety") && cfg.CodexQuota.SafetyFactor > 0 {
					codexQuotaSafety = cfg.CodexQuota.SafetyFactor
				}
				if !cmd.Flags().Changed("codex-quota-enforce") {
					codexQuotaEnforce = cfg.CodexQuota.Enforce
				}
				if !cmd.Flags().Changed("codex-quota-lookback") && cfg.CodexQuota.LookbackRuns > 0 {
					codexQuotaLookback = cfg.CodexQuota.LookbackRuns
				}
			}
			quotaCfg := quotaPreflightConfig{
				RemainingTokens: codexQuotaRemaining,
				ReserveTokens:   codexQuotaReserve,
				SafetyFactor:    codexQuotaSafety,
				Enforce:         codexQuotaEnforce,
				LookbackRuns:    codexQuotaLookback,
			}
			return runTasks(tasksFile, workers, verify, reposDir, filter, dryRun, maxRuntime, idleTimeout, failFast, tuiMode, allowFree, retry, noAutoCommit, parallelRepo, strictReadiness, maxRetries, quotaCfg, cfg)
		},
	}

	cmd.Flags().StringVar(&tasksFile, "tasks", "tokencontrol.json", "path to tasks JSON file (supports glob patterns)")
	cmd.Flags().IntVar(&workers, "workers", 4, "max parallel runner processes")
	cmd.Flags().BoolVar(&verify, "verify", false, "run make test && make lint per repo after completion")
	cmd.Flags().StringVar(&reposDir, "repos-dir", ".", "base directory containing repos")
	cmd.Flags().StringVar(&filter, "filter", "", "only run tasks matching ID glob pattern")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show execution plan without running")
	cmd.Flags().DurationVar(&maxRuntime, "max-runtime", 30*time.Minute, "per-task timeout duration")
	cmd.Flags().DurationVar(&idleTimeout, "idle-timeout", 5*time.Minute, "kill task after no stdout for this duration")
	cmd.Flags().BoolVar(&failFast, "fail-fast", false, "stop spawning new tasks on first failure")
	cmd.Flags().StringVar(&tuiMode, "tui", "auto", "display mode: full (interactive TUI), minimal (live status), off (no live display), auto (detect TTY)")
	cmd.Flags().BoolVar(&allowFree, "allow-free", false, "include free-tier runners in fallback cascade")
	cmd.Flags().BoolVar(&retry, "retry", false, "re-execute failed and interrupted tasks")
	cmd.Flags().BoolVar(&noAutoCommit, "no-auto-commit", false, "disable auto-commit of uncommitted changes after task completion")
	cmd.Flags().BoolVar(&parallelRepo, "parallel-repo", false, "use git worktrees for parallel same-repo task execution")
	cmd.Flags().IntVar(&maxRetries, "max-retries", 2, "max retries per runner on transient failures (connectivity, idle timeout); 0 disables")
	cmd.Flags().BoolVar(&strictReadiness, "strict-readiness", false, "fail if agent readiness checks produce warnings")
	cmd.Flags().IntVar(&codexQuotaRemaining, "codex-quota-remaining", 0, "remaining codex budget in tokens; 0 disables quota preflight")
	cmd.Flags().IntVar(&codexQuotaReserve, "codex-quota-reserve", 0, "tokens to hold back from codex remaining budget")
	cmd.Flags().Float64Var(&codexQuotaSafety, "codex-quota-safety", defaultQuotaSafetyFactor, "multiplier applied to estimated codex tokens")
	cmd.Flags().BoolVar(&codexQuotaEnforce, "codex-quota-enforce", true, "block run when codex quota preflight predicts shortfall")
	cmd.Flags().IntVar(&codexQuotaLookback, "codex-quota-lookback", defaultQuotaLookbackRuns, "number of recent run reports to sample for codex token history")

	return cmd
}

func runTasks(tasksFile string, workers int, verify bool, reposDir, filter string, dryRun bool, maxRuntime, idleTimeout time.Duration, failFast bool, tuiMode string, allowFree, retry, noAutoCommit, parallelRepo, strictReadiness bool, maxRetries int, quotaCfg quotaPreflightConfig, cfg *config.Settings) error {
	// resolve glob pattern to concrete file paths
	paths, err := config.ResolveGlob(tasksFile)
	if err != nil {
		return fmt.Errorf("resolve tasks: %w", err)
	}

	// load all task files
	taskFiles, err := config.LoadMulti(paths)
	if err != nil {
		return fmt.Errorf("load tasks: %w", err)
	}

	// merge settings into each file individually (per-file defaults)
	for _, f := range taskFiles {
		mergeSettings(f, cfg)
	}

	// merge all files into one
	tf, err := config.MergeTaskFiles(taskFiles)
	if err != nil {
		return fmt.Errorf("merge tasks: %w", err)
	}

	if len(paths) > 1 {
		slog.Info("loaded multiple task files", "files", len(paths), "total_tasks", len(tf.Tasks))
	}

	// apply filter
	tasks := tf.Tasks
	if filter != "" {
		tasks = filterTasks(tasks, filter)
		if len(tasks) == 0 {
			return fmt.Errorf("no tasks match filter %q", filter)
		}
	}

	// load persistent state and filter completed/failed tasks
	stateTracker := state.Load(state.DefaultPath())
	if recovered := stateTracker.RecoverInterrupted(); recovered > 0 {
		slog.Warn("recovered interrupted tasks from previous run", "count", recovered)
	}
	var skippedByState []state.SkippedTask
	tasks, skippedByState = state.FilterTasks(tasks, stateTracker, retry)
	if len(skippedByState) > 0 && !dryRun {
		for _, s := range skippedByState {
			slog.Info("task skipped by state", "task", s.ID, "reason", s.Reason)
		}
	}
	if len(tasks) == 0 {
		fmt.Println("All tasks already completed (use --retry to re-execute failed tasks, or 'tokencontrol state clear' to reset)")
		return nil
	}

	// warn if no cascade configured — single-runner dispatch wastes fallback potential
	if !hasCascadeConfig(tf, cfg) && !allScriptTasks(tasks) {
		slog.Warn("no runner cascade configured — all tasks will use a single runner",
			"default", "codex",
			"hint", "run 'tokencontrol init' to configure runners and fallbacks")
	}

	// stripe runner assignments for parallel provider utilization
	defaultRunner := tf.DefaultRunner
	if defaultRunner == "" {
		defaultRunner = "codex"
	}
	stripeRunners(tasks, defaultRunner, tf.DefaultFallbacks)

	// resolve repos dir
	reposDir, err = filepath.Abs(reposDir)
	if err != nil {
		return fmt.Errorf("resolve repos dir: %w", err)
	}

	if !dryRun && quotaCfg.enabled() {
		quotaResult, err := runCodexQuotaPreflight(tasks, tf.Runners, quotaCfg, ".tokencontrol")
		if err != nil {
			return err
		}
		if quotaResult != nil && quotaResult.CodexTasks > 0 {
			fmt.Fprintf(
				os.Stdout,
				"codex quota preflight: %d tasks, estimate %s, required %s, available %s (history: %d, heuristic: %d)\n",
				quotaResult.CodexTasks,
				formatTokenCount(quotaResult.EstimatedTokens),
				formatTokenCount(quotaResult.RequiredTokens),
				formatTokenCount(quotaResult.AvailableTokens),
				quotaResult.HistoricalTasks,
				quotaResult.HeuristicTasks,
			)
			if quotaResult.ShortfallTokens > 0 {
				fmt.Fprintf(
					os.Stdout,
					"  warning: estimated shortfall %s (continuing because --codex-quota-enforce=false)\n",
					formatTokenCount(quotaResult.ShortfallTokens),
				)
			}
		}
	}

	// validate repos exist
	filteredTF := &task.TaskFile{Tasks: tasks}
	if !dryRun {
		if err := config.ValidateRepos(filteredTF, reposDir); err != nil {
			return err
		}
	}

	// pre-flight connectivity check (skip for dry-run and script-only runs)
	if !dryRun && !allScriptTasks(tasks) {
		if err := checkConnectivity(); err != nil {
			return fmt.Errorf("pre-flight check: %w", err)
		}
	}

	// provider quota preflight (auto-detects from env vars)
	if !dryRun && !allScriptTasks(tasks) {
		quotaCtx, quotaCancel := context.WithTimeout(context.Background(), 30*time.Second)
		quotaInfos := runner.CheckAllQuotas(quotaCtx, os.Getenv)
		quotaCancel()
		for _, qi := range quotaInfos {
			if qi.Error != "" && qi.UsedTokens == 0 && qi.Balance == "" {
				slog.Debug("quota check", "provider", qi.Provider, "note", qi.Error)
				continue
			}
			if !qi.Available {
				return fmt.Errorf("provider %s quota exhausted: %s (run 'tokencontrol quota' for details)", qi.Provider, qi.Error)
			}
			if qi.UsedTokens > 0 {
				slog.Info("provider quota", "provider", qi.Provider, "used_7d", formatTokenCount(qi.UsedTokens), "burn_rate", formatTokenCount(qi.BurnRatePerDay)+"/day")
			}
			if qi.Balance != "" {
				slog.Info("provider quota", "provider", qi.Provider, "balance", "$"+qi.Balance+" "+qi.Currency, "available", qi.Available)
			}
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

	// pre-validate and auto-resolve runner models
	var modelResolutions []runner.ModelResolution
	if len(tf.Runners) > 0 {
		tmpRunners, buildErr := buildRunnerRegistry(tf, idleTimeout)
		if buildErr == nil {
			modelResolutions = validateAndResolveModels(tf, tmpRunners, idleTimeout)
		}
	}

	// agent readiness check (ANCC-based, optional)
	readinessWarnings := checkRunReadiness(tf)
	if strictReadiness && len(readinessWarnings) > 0 {
		return fmt.Errorf("agent readiness check failed (--strict-readiness): %s",
			strings.Join(readinessWarnings, "; "))
	}

	// pre-scan repos for secrets (used by dry-run display and execution filtering)
	secretRepos := preScanRepos(tasks, reposDir)

	// dry run
	if dryRun {
		textRep.PrintHeader(len(tasks)+len(skippedByState), workers)
		if len(skippedByState) > 0 {
			infos := make([]reporter.SkippedInfo, len(skippedByState))
			for i, s := range skippedByState {
				infos[i] = reporter.SkippedInfo{ID: s.ID, Reason: s.Reason}
			}
			textRep.PrintSkippedByState(infos)
		}
		if len(modelResolutions) > 0 {
			reps := make([]reporter.ModelResolution, len(modelResolutions))
			for i, r := range modelResolutions {
				reps[i] = reporter.ModelResolution{
					RunnerProfile: r.RunnerProfile,
					Original:      r.Original,
					Resolved:      r.Resolved,
				}
			}
			textRep.PrintModelResolutions(reps)
		}
		if len(readinessWarnings) > 0 {
			textRep.PrintReadinessWarnings(readinessWarnings)
		}
		if len(secretRepos) > 0 {
			repos := make([]string, 0, len(secretRepos))
			for r := range secretRepos {
				repos = append(repos, r)
			}
			textRep.PrintSecretRepos(repos)
		}
		textRep.PrintDryRun(graph, reposDir)
		return nil
	}

	report, err := executeRun(execRunConfig{
		tasksFiles:   paths,
		taskFile:     tf,
		tasks:        tasks,
		graph:        graph,
		workers:      workers,
		reposDir:     reposDir,
		filter:       filter,
		maxRuntime:   maxRuntime,
		maxRetries:   maxRetries,
		idleTimeout:  idleTimeout,
		failFast:     failFast,
		postRun:      cfg.PostRun,
		settings:     cfg,
		tuiMode:      tuiMode,
		allowFree:    allowFree,
		secretRepos:  secretRepos,
		stateTracker: stateTracker,
		noAutoCommit: noAutoCommit,
		parallelRepo: parallelRepo || (cfg != nil && cfg.ParallelRepo),
		mergeBack:    resolveMergeBack(tf, cfg),
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
	tasksFiles   []string
	taskFile     *task.TaskFile // full parsed/merged file with profiles
	tasks        []task.Task
	graph        *task.Graph
	workers      int
	reposDir     string
	filter       string
	maxRuntime   time.Duration
	maxRetries   int
	idleTimeout  time.Duration
	failFast     bool
	parentRunID  string                                    // links rerun to original run
	postRun      string                                    // shell command to run after report is written
	settings     *config.Settings                          // runtime settings for limiter etc.
	tuiMode      string                                    // full, minimal, off, auto
	allowFree    bool                                      // include free-tier runners in cascade
	secretRepos  map[string]struct{}                       // repos with secrets detected by pre-scan
	noAutoCommit bool                                      // disable auto-commit of uncommitted changes
	parallelRepo bool                                      // use git worktrees for parallel same-repo execution
	mergeBack    bool                                      // auto-merge worktree branches back to main
	stateTracker *state.Tracker                            // persistent task state across runs
	onProgress   func(results map[string]*task.TaskResult) // optional progress callback for sentinel
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
	runDir := filepath.Join(".tokencontrol", time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, fmt.Errorf("create run dir: %w", err)
	}

	if cfg.allowFree {
		slog.Warn("free-tier models enabled — quality may vary")
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

	// start Responses API → Chat Completions proxy if configured
	if cfg.settings != nil && cfg.settings.Proxy != nil && cfg.settings.Proxy.Enabled {
		proxyCfg, err := resolveProxyConfig(cfg.settings.Proxy)
		if err != nil {
			return nil, fmt.Errorf("proxy config: %w", err)
		}
		srv := neurorouter.NewProxy(proxyCfg)
		if _, err := srv.Start(); err != nil {
			// non-fatal: another tokencontrol process may already own the port
			slog.Warn("proxy start failed (may already be running)", "error", err)
		} else {
			defer func() {
				if err := srv.Stop(); err != nil {
					slog.Warn("proxy stop error", "error", err)
				}
			}()
		}
	}

	// build runner registry from profiles
	tf := cfg.taskFile
	if tf == nil {
		tf = &task.TaskFile{}
	}
	runners, err := buildRunnerRegistry(tf, cfg.idleTimeout)
	if err != nil {
		return nil, fmt.Errorf("build runner registry: %w", err)
	}

	// pre-validate and auto-resolve runner models
	validateAndResolveModels(tf, runners, cfg.idleTimeout)

	defaultRunner := tf.DefaultRunner
	if defaultRunner == "" {
		defaultRunner = "codex"
	}
	blacklist := runner.LoadBlacklist(runner.DefaultBlacklistPath())
	graylist := runner.LoadGraylist(runner.DefaultGraylistPath())

	// build per-provider concurrency limiter from settings
	var concurrencyLimits map[string]int
	if cfg.settings != nil {
		concurrencyLimits = make(map[string]int)
		for name, rp := range cfg.settings.Runners {
			if rp.MaxConcurrent > 0 {
				concurrencyLimits[name] = rp.MaxConcurrent
			}
		}
	}
	limiter := buildProviderLimiter(concurrencyLimits)

	// setup review pool if configured
	var reviewPool *ReviewPool
	if tf.Review != nil && tf.Review.Enabled {
		const reviewWorkers = 2
		reviewPool = NewReviewPool(tf.Review, runners, blacklist, cfg.maxRuntime, reviewWorkers)
		reviewPool.Start(ctx, reviewWorkers)
	}

	// build private repos set from settings
	privateRepos := make(map[string]struct{})
	if cfg.settings != nil {
		for _, r := range cfg.settings.PrivateRepos {
			privateRepos[r] = struct{}{}
		}
	}

	secretRepos := cfg.secretRepos

	// inject commit instructions into all agent-bound prompts (safety net)
	injectCommitInstructions(cfg.tasks, runners)

	// Forward-declare scheduler so execFn closure can call SetRunnerUsed.
	var sched *task.Scheduler

	execFn := func(ctx context.Context, t *task.Task, repoDir, outputDir string) *task.TaskResult {
		// acquire execution directory: worktree (parallel) or lock (serial)
		var execDir string
		var wtBranch string
		if cfg.parallelRepo {
			wtDir, branch, wtErr := runner.CreateWorktree(ctx, repoDir, cfg.reposDir, t.ID)
			if wtErr != nil {
				slog.Warn("worktree creation failed, falling back to lock",
					"task", t.ID, "error", wtErr)
				if err := runner.WaitAndAcquire(ctx, repoDir, t.ID); err != nil {
					return &task.TaskResult{
						TaskID:  t.ID,
						State:   task.StateFailed,
						Error:   fmt.Sprintf("acquire lock: %v", err),
						EndedAt: time.Now(),
					}
				}
				defer runner.Release(repoDir)
				execDir = repoDir
			} else {
				defer runner.RemoveWorktree(ctx, repoDir, wtDir)
				execDir = wtDir
				wtBranch = branch
			}
		} else {
			if err := runner.WaitAndAcquire(ctx, repoDir, t.ID); err != nil {
				return &task.TaskResult{
					TaskID:  t.ID,
					State:   task.StateFailed,
					Error:   fmt.Sprintf("acquire lock: %v", err),
					EndedAt: time.Now(),
				}
			}
			defer runner.Release(repoDir)
			execDir = repoDir
		}

		// mark task as in_progress in persistent state
		if cfg.stateTracker != nil {
			cfg.stateTracker.MarkStarted(t.ID, "")
		}

		// save task metadata so output dir is self-contained
		writeTaskMeta(outputDir, t)

		fl := &filterLog{}
		cascade := resolveRunnerCascade(t, defaultRunner, tf.DefaultFallbacks)
		cascade = filterDataCollectionRunners(cascade, t.Repo, tf.Runners, privateRepos, fl)
		cascade = filterGraylistedRunners(cascade, graylist, tf.Runners, fl)
		cascade = filterFreeRunners(cascade, cfg.allowFree, tf.Runners, fl)
		cascade = filterSecretAwareRunners(cascade, t.Repo, secretRepos, fl)
		cascade = filterByTier(cascade, t.Difficulty, tf.Runners, fl)
		if len(cascade) == 0 {
			return &task.TaskResult{
				TaskID:  t.ID,
				State:   task.StateFailed,
				Error:   formatCascadeError(t.ID, fl),
				EndedAt: time.Now(),
			}
		}
		result := RunWithCascade(ctx, t, execDir, outputDir, runners, cascade, cfg.maxRuntime, cfg.maxRetries, blacklist, graylist, limiter,
			func(runnerName string) { sched.SetRunnerUsed(t.ID, runnerName) },
		)

		// auto-commit uncommitted changes for successful tasks
		if result.State == task.StateCompleted && !cfg.noAutoCommit {
			committed, err := runner.AutoCommit(ctx, execDir, t)
			if err != nil {
				slog.Warn("auto-commit failed", "task", t.ID, "error", err)
			} else if committed {
				result.AutoCommitted = true
			}
		}

		// merge worktree branch back to main repo
		if wtBranch != "" && result.State == task.StateCompleted && cfg.mergeBack {
			if err := runner.WaitAndAcquire(ctx, repoDir, t.ID+"-merge"); err == nil {
				if err := runner.MergeBack(ctx, repoDir, wtBranch); err != nil {
					result.MergeConflict = true
					slog.Warn("merge failed, branch left for inspection",
						"task", t.ID, "branch", wtBranch, "error", err)
				} else {
					runner.DeleteBranch(ctx, repoDir, wtBranch)
					wtBranch = "" // clear so we don't report branch that was merged+deleted
				}
				runner.Release(repoDir)
			}
		}
		if wtBranch != "" {
			result.WorktreeBranch = wtBranch
		}

		// update persistent state with final result
		if cfg.stateTracker != nil {
			switch result.State {
			case task.StateCompleted:
				cfg.stateTracker.MarkCompleted(t.ID, result.RunnerUsed, gitHead(execDir))
			case task.StateFailed:
				cfg.stateTracker.MarkFailed(t.ID, result.Error)
			}
		}

		return result
	}

	// run scheduler
	start := time.Now()
	sched = task.NewScheduler(cfg.graph, task.SchedulerConfig{
		Workers:  cfg.workers,
		ReposDir: cfg.reposDir,
		RunDir:   runDir,
		ExecFn:   execFn,
		FailFast: cfg.failFast,
		OnUpdate: func(id string, result *task.TaskResult) {
			slog.Debug("task update", "task", id, "state", result.State)
			writeStatusFile(len(cfg.tasks), sched.Results())
			if cfg.onProgress != nil {
				cfg.onProgress(sched.Results())
			}
			if reviewPool != nil && result.State == task.StateCompleted {
				t := cfg.graph.Task(id)
				if t != nil {
					reviewPool.Submit(reviewJob{
						taskID: id,
						task:   t,
						result: result,
						runDir: runDir,
					})
				}
			}
		},
	})

	// resolve display mode: full TUI, minimal live reporter, or off
	displayMode := cfg.tuiMode
	if displayMode == "" || displayMode == "auto" {
		if isTTY {
			displayMode = "full"
		} else {
			displayMode = "off"
		}
	}

	var live *reporter.LiveReporter
	var tuiProgram *tea.Program
	var logFile *os.File
	switch displayMode {
	case "full":
		// Redirect slog to a file so log lines don't corrupt the alt-screen TUI.
		logPath := filepath.Join(runDir, "run.log")
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			logFile = f
			level := slog.LevelWarn
			if verbose {
				level = slog.LevelDebug
			}
			slog.SetDefault(slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: level})))
		}
		tuiModel := reporter.NewTUIModel(cfg.graph, sched.Results, cancel, logPath)
		tuiProgram = tea.NewProgram(tuiModel, tea.WithAltScreen())
		go func() {
			if _, err := tuiProgram.Run(); err != nil {
				slog.Warn("TUI error", "error", err)
			}
		}()
	case "minimal":
		live = reporter.NewLiveReporter(os.Stdout, isTTY, cfg.graph, sched.Results)
		live.Start()
	default:
		// "off" or unrecognized — no live display
	}

	results := sched.Run(ctx)
	totalDuration := time.Since(start)
	removeStatusFile()

	if tuiProgram != nil {
		tuiProgram.Quit()
		time.Sleep(100 * time.Millisecond)
	}
	// Restore slog to stderr after TUI exits so post-run output goes to terminal.
	if logFile != nil {
		_ = logFile.Close()
		level := slog.LevelWarn
		if verbose {
			level = slog.LevelDebug
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	}
	if live != nil {
		live.Stop()
	}

	// wait for reviews to finish before building report
	if reviewPool != nil {
		reviewPool.Wait()
		reviewPool.ApplyResults(results)
	}

	report := buildReport(cfg.tasksFiles, cfg.workers, cfg.filter, cfg.reposDir, results, totalDuration, cfg.parentRunID)
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

	// auto-graylist runners that produced false positives
	autoGraylistRunners(results, graylist, tf.Runners, report.RunID)

	// run post_run hook if configured
	if cfg.postRun != "" {
		absRunDir, _ := filepath.Abs(runDir)
		hookCmd := exec.CommandContext(ctx, "sh", "-c", cfg.postRun)
		hookCmd.Env = append(os.Environ(), "RUNFORGE_RUN_DIR="+absRunDir)
		hookCmd.Stdout = os.Stdout
		hookCmd.Stderr = os.Stderr
		fmt.Fprintf(os.Stdout, "\npost_run: %s\n", cfg.postRun)
		if err := hookCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "post_run hook FAILED: %v\n", err)
		}
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

func buildReport(tasksFiles []string, workers int, filter, reposDir string, results map[string]*task.TaskResult, duration time.Duration, parentRunID string) *task.RunReport {
	report := &task.RunReport{
		Timestamp:     time.Now(),
		TasksFiles:    tasksFiles,
		Workers:       workers,
		Filter:        filter,
		ReposDir:      reposDir,
		Results:       results,
		TotalTasks:    len(results),
		TotalDuration: duration,
		ParentRunID:   parentRunID,
	}

	// compute deterministic run ID from timestamp + task file paths
	h := sha256.New()
	fmt.Fprintf(h, "%d", report.Timestamp.UnixNano())
	for _, f := range report.TasksFiles {
		fmt.Fprintf(h, "|%s", f)
	}
	report.RunID = hex.EncodeToString(h.Sum(nil)[:6])

	for _, r := range results {
		switch r.State {
		case task.StateCompleted:
			report.Completed++
			if r.FalsePositive {
				report.FalsePositives++
			}
			if r.AutoCommitted {
				report.AutoCommits++
			}
			if r.MergeConflict {
				report.MergeConflicts++
			}
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
		// count retry attempts across all tasks
		for _, a := range r.Attempts {
			if a.Retry > 0 {
				report.Retries++
			}
		}
	}

	report.TotalTokens = aggregateTokens(results)

	return report
}

func aggregateTokens(results map[string]*task.TaskResult) *task.TokenUsage {
	var total *task.TokenUsage
	for _, r := range results {
		if r.TokensUsed != nil {
			if total == nil {
				total = &task.TokenUsage{}
			}
			total.InputTokens += r.TokensUsed.InputTokens
			total.OutputTokens += r.TokensUsed.OutputTokens
			total.TotalTokens += r.TokensUsed.TotalTokens
		}
	}
	return total
}

func filterTasks(tasks []task.Task, filter string) []task.Task {
	// expand braces first, then split on commas
	expanded := expandBraces(filter)
	var patterns []string
	for _, e := range expanded {
		for _, p := range strings.Split(e, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				patterns = append(patterns, p)
			}
		}
	}

	var filtered []task.Task
	for _, t := range tasks {
		for _, p := range patterns {
			if matchFilter(t.ID, p) {
				filtered = append(filtered, t)
				break
			}
		}
	}
	return filtered
}

// matchFilter matches an ID against a single glob pattern using filepath.Match.
func matchFilter(id, pattern string) bool {
	if id == pattern {
		return true
	}
	matched, err := filepath.Match(pattern, id)
	return err == nil && matched
}

// expandBraces expands shell-style brace patterns like "prefix{a,b,c}suffix"
// into ["prefixasuffix", "prefixbsuffix", "prefixcsuffix"].
// Patterns without braces are returned as-is.
func expandBraces(pattern string) []string {
	open := strings.IndexByte(pattern, '{')
	if open < 0 {
		return []string{pattern}
	}
	close := strings.IndexByte(pattern[open:], '}')
	if close < 0 {
		return []string{pattern}
	}
	close += open

	prefix := pattern[:open]
	suffix := pattern[close+1:]
	alternatives := strings.Split(pattern[open+1:close], ",")

	var result []string
	for _, alt := range alternatives {
		result = append(result, expandBraces(prefix+alt+suffix)...)
	}
	return result
}

// mergeSettings applies settings defaults to a task file.
// Task file values take precedence; settings fill in gaps.
func mergeSettings(tf *task.TaskFile, cfg *config.Settings) {
	if cfg == nil {
		return
	}

	// merge default_runner
	if tf.DefaultRunner == "" && cfg.DefaultRunner != "" {
		tf.DefaultRunner = cfg.DefaultRunner
	}

	// merge default_fallbacks
	if len(tf.DefaultFallbacks) == 0 && len(cfg.DefaultFallbacks) > 0 {
		tf.DefaultFallbacks = cfg.DefaultFallbacks
	}

	// merge runner profiles (settings provide base, task file overrides)
	if len(cfg.Runners) > 0 {
		if tf.Runners == nil {
			tf.Runners = make(map[string]*task.RunnerProfileConfig, len(cfg.Runners))
		}
		for name, rp := range cfg.Runners {
			if _, exists := tf.Runners[name]; !exists {
				tf.Runners[name] = &task.RunnerProfileConfig{
					Type:           rp.Type,
					Model:          rp.Model,
					Profile:        rp.Profile,
					Env:            rp.Env,
					DataCollection: rp.DataCollection,
					Free:           rp.Free,
					Tier:           rp.Tier,
				}
			}
		}
	}
}

// resolveMergeBack determines the merge_back setting. Task file overrides
// settings, which overrides default (true).
func resolveMergeBack(tf *task.TaskFile, cfg *config.Settings) bool {
	if tf != nil && tf.MergeBack != nil {
		return *tf.MergeBack
	}
	if cfg != nil && cfg.MergeBack != nil {
		return *cfg.MergeBack
	}
	return true // default: auto-merge
}

// stripeRunners distributes primary runner assignments across available
// providers for parallel utilization. Tasks without an explicit runner
// get round-robin primary assignment; each task's fallbacks contain all
// other providers. Tasks with an explicit Runner field are not modified.
func stripeRunners(tasks []task.Task, defaultRunner string, fallbacks []string) {
	if len(fallbacks) == 0 {
		return
	}
	all := []string{defaultRunner}
	all = append(all, fallbacks...)

	for i := range tasks {
		if tasks[i].Runner != "" {
			continue
		}
		idx := i % len(all)
		tasks[i].Runner = all[idx]
		var fb []string
		for j, r := range all {
			if j != idx {
				fb = append(fb, r)
			}
		}
		tasks[i].Fallbacks = fb
	}
}

// preScanRepos runs pastewatch-cli secret detection on each unique repo.
// Returns a set of repos where secrets were detected.
func preScanRepos(tasks []task.Task, reposDir string) map[string]struct{} {
	repos := collectRepos(tasks)
	secretRepos := make(map[string]struct{})
	ctx := context.Background()
	for _, repo := range repos {
		repoPath := filepath.Join(reposDir, repo)
		found, err := runner.PreScan(ctx, repoPath)
		if err != nil {
			slog.Warn("pre-scan error", "repo", repo, "error", err)
			continue
		}
		if found {
			secretRepos[repo] = struct{}{}
		}
	}
	if len(secretRepos) > 0 {
		slog.Warn("repos with secrets detected", "count", len(secretRepos))
	}
	return secretRepos
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

const statusDir = "/tmp/tokencontrol-status.d"

// statusFilePath returns the per-process status file path.
func statusFilePath() string {
	return filepath.Join(statusDir, fmt.Sprintf("%d", os.Getpid()))
}

// writeStatusFile writes a one-line status to a per-PID file for external consumers (e.g. statusline).
// Multiple tokencontrol processes write separate files; the statusline aggregates them.
func writeStatusFile(total int, results map[string]*task.TaskResult) {
	var running, completed, failed, rateLimited int
	for _, r := range results {
		switch r.State {
		case task.StateRunning:
			running++
		case task.StateCompleted:
			completed++
		case task.StateFailed:
			failed++
		case task.StateRateLimited:
			rateLimited++
		}
	}
	line := fmt.Sprintf("%d/%d done", completed, total)
	if running > 0 {
		line += fmt.Sprintf(", %d run", running)
	}
	if failed > 0 {
		line += fmt.Sprintf(", %d fail", failed)
	}
	if rateLimited > 0 {
		line += fmt.Sprintf(", %d rl", rateLimited)
	}
	_ = os.MkdirAll(statusDir, 0o755)
	_ = os.WriteFile(statusFilePath(), []byte(line+"\n"), 0o644)
}

func removeStatusFile() {
	_ = os.Remove(statusFilePath())
	// clean up directory if empty
	entries, err := os.ReadDir(statusDir)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(statusDir)
	}
}

// resolveProxyConfig converts config.ProxyConfig to neurorouter.ProxyConfig,
// resolving "env:VAR_NAME" references in API keys.
func resolveProxyConfig(pc *config.ProxyConfig) (neurorouter.ProxyConfig, error) {
	cfg := neurorouter.ProxyConfig{
		Listen:  pc.Listen,
		Targets: make(map[string]neurorouter.Target, len(pc.Targets)),
	}
	if cfg.Listen == "" {
		cfg.Listen = ":4000"
	}
	for name, t := range pc.Targets {
		apiKey := t.APIKey
		if strings.HasPrefix(apiKey, "env:") {
			envKey := strings.TrimPrefix(apiKey, "env:")
			apiKey = os.Getenv(envKey)
			if apiKey == "" {
				return neurorouter.ProxyConfig{}, fmt.Errorf("target %q: env var %q is not set", name, envKey)
			}
		}
		cfg.Targets[name] = neurorouter.Target{
			BaseURL: t.BaseURL,
			APIKey:  apiKey,
		}
	}
	return cfg, nil
}

// autoGraylistRunners scans results for false positives and auto-graylists
// the responsible runner+model pairs. Prints a summary of actions taken.
func autoGraylistRunners(results map[string]*task.TaskResult, graylist *runner.RunnerGraylist, profiles map[string]*task.RunnerProfileConfig, runID string) {
	if graylist == nil {
		return
	}

	// collect runner+model pairs that produced false positives
	type key struct{ runner, model string }
	counts := make(map[key]int)
	for _, res := range results {
		if res.FalsePositive && res.RunnerUsed != "" {
			model := ""
			if p, ok := profiles[res.RunnerUsed]; ok {
				model = p.Model
			}
			counts[key{res.RunnerUsed, model}]++
		}
	}

	if len(counts) == 0 {
		return
	}

	fmt.Fprintf(os.Stdout, "\nFalse positives detected:\n")
	for k, count := range counts {
		// refuse to auto-graylist with empty model — wildcard would block entire provider
		if k.model == "" {
			slog.Warn("skipping auto-graylist: no model in runner profile",
				"runner", k.runner, "false_positives", count)
			fmt.Fprintf(os.Stdout, "  %s: %d false positives but no model in profile — add model to runner profile for auto-graylist\n",
				k.runner, count)
			continue
		}
		reason := fmt.Sprintf("false positive: %d tasks with 0 events in run %s", count, runID)
		label := k.runner + " (model: " + k.model + ")"
		if !graylist.IsGraylisted(k.runner, k.model) {
			graylist.Add(k.runner, k.model, reason)
			fmt.Fprintf(os.Stdout, "  graylisted %s (%d tasks, 0 events)\n", label, count)
		} else {
			fmt.Fprintf(os.Stdout, "  %s already graylisted (%d more false positives)\n", label, count)
		}
	}
	fmt.Fprintf(os.Stdout, "  Use 'tokencontrol graylist list' to view, 'tokencontrol graylist remove <runner>' to reinstate\n")
}

// commitInstruction is appended to all agent-bound prompts to ensure agents
// commit their work. This is a safety net — skill files may or may not be
// deployed to every repo, but this instruction is always injected at runtime.
const commitInstruction = "\n\nIMPORTANT: After completing all changes, you MUST commit your work. " +
	"Run `git add` for changed files and `git commit` with a conventional commit message " +
	"(e.g. feat:, fix:, refactor:). A task is NOT complete until changes are committed."

// injectCommitInstructions appends commit enforcement to all agent-bound task
// prompts. Script runner tasks are skipped since their prompts are shell commands.
func injectCommitInstructions(tasks []task.Task, runners map[string]runner.Runner) {
	for i := range tasks {
		// skip script runner tasks — their prompts are shell commands, not agent instructions
		if r, ok := runners[tasks[i].Runner]; ok {
			if _, isScript := r.(*runner.ScriptRunner); isScript {
				continue
			}
		}
		// skip if prompt already contains commit instruction (e.g. from prompt_conventions)
		if strings.Contains(tasks[i].Prompt, "MUST commit") {
			continue
		}
		tasks[i].Prompt += commitInstruction
	}
}

// writeTaskMeta saves a task's metadata (id, repo, prompt, runner) to the output dir
// so each run output is self-contained without needing the original task file.
func writeTaskMeta(outputDir string, t *task.Task) {
	_ = os.MkdirAll(outputDir, 0o755)
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(outputDir, "task.json"), data, 0o644)
}

// checkConnectivity performs a quick DNS lookup to verify network availability.
// Fails fast with a clear message instead of letting tasks timeout with cryptic errors.
// Uses dns.google as a provider-neutral probe target.
func checkConnectivity() error {
	_, err := net.LookupHost("dns.google")
	if err != nil {
		return fmt.Errorf("no network connectivity — check your WiFi/ethernet connection (DNS lookup failed for dns.google): %w", err)
	}
	return nil
}

// allScriptTasks returns true if every task uses the "script" runner (no network needed).
func allScriptTasks(tasks []task.Task) bool {
	for _, t := range tasks {
		if t.Runner != "script" {
			return false
		}
	}
	return true
}

// checkRunReadiness runs ANCC-based agent readiness checks for runners used in the task file.
// Returns nil if ANCC is not available (graceful degradation).
func checkRunReadiness(tf *task.TaskFile) []string {
	_, err := exec.LookPath("ancc")
	if err != nil {
		return nil
	}

	out, err := exec.Command("ancc", "skills", "--format", "json").CombinedOutput()
	if err != nil {
		return nil
	}

	var result anccSkillsResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil
	}

	agentMap := mapANCCAgents(result.Agents)
	var warnings []string

	usedRunners := collectUsedRunners(tf)
	safeRunners := map[string]bool{"claude": true, "cline": true}

	for name := range usedRunners {
		agent, found := agentMap[name]
		if !found {
			continue
		}
		if agent.Skills == 0 {
			w := fmt.Sprintf("runner %s: 0 skills loaded", name)
			slog.Warn("agent readiness", "warning", w)
			warnings = append(warnings, w)
		}
		if safeRunners[name] && agent.Hooks == 0 {
			w := fmt.Sprintf("runner %s: 0 hooks — secrets unprotected", name)
			slog.Warn("agent readiness", "warning", w)
			warnings = append(warnings, w)
		}
	}

	return warnings
}

// hasCascadeConfig returns true if either the task file or settings define
// runner fallbacks. Without fallbacks, all tasks run on a single runner.
func hasCascadeConfig(tf *task.TaskFile, cfg *config.Settings) bool {
	if len(tf.DefaultFallbacks) > 0 {
		return true
	}
	if cfg != nil && len(cfg.DefaultFallbacks) > 0 {
		return true
	}
	for _, t := range tf.Tasks {
		if len(t.Fallbacks) > 0 {
			return true
		}
	}
	return false
}

// collectUsedRunners extracts unique runner names from a task file.
func collectUsedRunners(tf *task.TaskFile) map[string]struct{} {
	runners := make(map[string]struct{})
	if tf.DefaultRunner != "" {
		runners[tf.DefaultRunner] = struct{}{}
	}
	for _, fb := range tf.DefaultFallbacks {
		runners[fb] = struct{}{}
	}
	for _, t := range tf.Tasks {
		if t.Runner != "" {
			runners[t.Runner] = struct{}{}
		}
	}
	return runners
}
