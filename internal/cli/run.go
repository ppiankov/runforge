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
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ppiankov/neurorouter"
	"github.com/ppiankov/tokencontrol/internal/config"
	"github.com/ppiankov/tokencontrol/internal/reporter"
	"github.com/ppiankov/tokencontrol/internal/runner"
	"github.com/ppiankov/tokencontrol/internal/state"
	"github.com/ppiankov/tokencontrol/internal/task"
	"github.com/ppiankov/tokencontrol/internal/telemetry"
)

func newRunCmd() *cobra.Command {
	var (
		tasksFile      string
		workers        int
		verify         bool
		reposDir       string
		filter         string
		dryRun         bool
		maxRuntime     time.Duration
		idleTimeout    time.Duration
		failFast       bool
		tuiMode        string
		allowFree      bool
		retry          bool
		noAutoCommit   bool
		parallelRepo   bool
		noMergeResolve bool
		noVerify       bool
		maxRetries     int

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
			if noVerify {
				verify = false
			}
			return runTasks(tasksFile, workers, verify, reposDir, filter, dryRun, maxRuntime, idleTimeout, failFast, tuiMode, allowFree, retry, noAutoCommit, parallelRepo, noMergeResolve, strictReadiness, maxRetries, quotaCfg, cfg)
		},
	}

	cmd.Flags().StringVar(&tasksFile, "tasks", "tokencontrol.json", "path to tasks JSON file (supports glob patterns)")
	cmd.Flags().IntVar(&workers, "workers", 4, "max parallel runner processes")
	cmd.Flags().BoolVar(&verify, "verify", true, "run make test && make lint per repo after completion")
	cmd.Flags().BoolVar(&noVerify, "no-verify", false, "disable post-run verification")
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
	cmd.Flags().BoolVar(&noMergeResolve, "no-merge-resolve", false, "disable auto-generated merge resolution task for parallel conflicts")
	cmd.Flags().IntVar(&maxRetries, "max-retries", 2, "max retries per runner on transient failures (connectivity, idle timeout); 0 disables")
	cmd.Flags().BoolVar(&strictReadiness, "strict-readiness", false, "fail if agent readiness checks produce warnings")
	cmd.Flags().IntVar(&codexQuotaRemaining, "codex-quota-remaining", 0, "remaining codex budget in tokens; 0 disables quota preflight")
	cmd.Flags().IntVar(&codexQuotaReserve, "codex-quota-reserve", 0, "tokens to hold back from codex remaining budget")
	cmd.Flags().Float64Var(&codexQuotaSafety, "codex-quota-safety", defaultQuotaSafetyFactor, "multiplier applied to estimated codex tokens")
	cmd.Flags().BoolVar(&codexQuotaEnforce, "codex-quota-enforce", true, "block run when codex quota preflight predicts shortfall")
	cmd.Flags().IntVar(&codexQuotaLookback, "codex-quota-lookback", defaultQuotaLookbackRuns, "number of recent run reports to sample for codex token history")

	return cmd
}

func runTasks(tasksFile string, workers int, verify bool, reposDir, filter string, dryRun bool, maxRuntime, idleTimeout time.Duration, failFast bool, tuiMode string, allowFree, retry, noAutoCommit, parallelRepo, noMergeResolve, strictReadiness bool, maxRetries int, quotaCfg quotaPreflightConfig, cfg *config.Settings) error {
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
	stripeRunners(tasks, defaultRunner, tf.DefaultFallbacks, tf.Runners)

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
	var initialQuotas []*runner.QuotaInfo
	if !dryRun && !allScriptTasks(tasks) {
		quotaCtx, quotaCancel := context.WithTimeout(context.Background(), 30*time.Second)
		initialQuotas = runner.CheckAllQuotas(quotaCtx, os.Getenv)
		quotaCancel()
		for _, qi := range initialQuotas {
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
	readinessWarnings, agentSummary := checkRunReadiness(tf)
	if strictReadiness && len(readinessWarnings) > 0 {
		return fmt.Errorf("agent readiness check failed (--strict-readiness): %s",
			strings.Join(readinessWarnings, "; "))
	}
	for _, a := range agentSummary {
		slog.Info("agent readiness", "agent", a.Name, "skills", a.Skills, "hooks", a.Hooks, "tokens", formatTokenCount(a.Tokens))
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
		tasksFiles:     paths,
		taskFile:       tf,
		tasks:          tasks,
		graph:          graph,
		workers:        workers,
		reposDir:       reposDir,
		filter:         filter,
		maxRuntime:     maxRuntime,
		maxRetries:     maxRetries,
		idleTimeout:    idleTimeout,
		failFast:       failFast,
		postRun:        cfg.PostRun,
		settings:       cfg,
		tuiMode:        tuiMode,
		allowFree:      allowFree,
		secretRepos:    secretRepos,
		stateTracker:   stateTracker,
		noAutoCommit:   noAutoCommit,
		parallelRepo:   parallelRepo || (cfg != nil && cfg.ParallelRepo) || tf.ParallelRepo,
		mergeBack:      resolveMergeBack(tf, cfg),
		noMergeResolve: noMergeResolve,
		initialQuotas:  initialQuotas,
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
	tasksFiles     []string
	taskFile       *task.TaskFile // full parsed/merged file with profiles
	tasks          []task.Task
	graph          *task.Graph
	workers        int
	reposDir       string
	filter         string
	maxRuntime     time.Duration
	maxRetries     int
	idleTimeout    time.Duration
	failFast       bool
	parentRunID    string                                    // links rerun to original run
	postRun        string                                    // shell command to run after report is written
	settings       *config.Settings                          // runtime settings for limiter etc.
	tuiMode        string                                    // full, minimal, off, auto
	allowFree      bool                                      // include free-tier runners in cascade
	secretRepos    map[string]struct{}                       // repos with secrets detected by pre-scan
	noAutoCommit   bool                                      // disable auto-commit of uncommitted changes
	parallelRepo   bool                                      // use git worktrees for parallel same-repo execution
	mergeBack      bool                                      // auto-merge worktree branches back to main
	noMergeResolve bool                                      // disable auto-generated merge resolution task
	stateTracker   *state.Tracker                            // persistent task state across runs
	onProgress     func(results map[string]*task.TaskResult) // optional progress callback for sentinel
	initialQuotas  []*runner.QuotaInfo                       // pre-flight quota results to seed TUI cache
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

	// apply shared prompt conventions at runtime so exported/manual task packs
	// get the same quality rules as tasks produced by `generate`
	promptConventions := ""
	if cfg.settings != nil {
		promptConventions = cfg.settings.PromptConventions
	}
	injectPromptConventions(cfg.tasks, promptConventions, runners)

	// inject commit instructions into all agent-bound prompts (safety net)
	injectCommitInstructions(cfg.tasks, runners)

	// tell agents to write generated docs to the gitignored docs dir
	injectDocDirective(cfg.tasks, runners, cfg.settings.EffectiveDocsDir())

	// Forward-declare scheduler so execFn closure can call SetRunnerUsed.
	var sched *task.Scheduler

	execFn := func(ctx context.Context, t *task.Task, repoDir, outputDir string) *task.TaskResult {
		// show intended runner immediately so TUI displays it during lock wait
		sched.SetRunnerUsed(t.ID, t.Runner)

		// ensure agent docs dir exists (gitignored)
		_ = os.MkdirAll(filepath.Join(repoDir, cfg.settings.EffectiveDocsDir()), 0o755)

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

		// transition from Waiting → Running now that lock/worktree is acquired
		sched.SetRunning(t.ID)

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

		// sanitize agent commit messages — strip attribution and watermark trailers
		if result.State == task.StateCompleted {
			runner.SanitizeHeadCommit(ctx, execDir)
		}

		// auto-commit uncommitted changes for successful tasks
		if result.State == task.StateCompleted && !cfg.noAutoCommit {
			committed, err := runner.AutoCommit(ctx, execDir, t)
			if err != nil {
				slog.Warn("auto-commit failed", "task", t.ID, "error", err)
			} else if committed {
				result.AutoCommitted = true
			}
		}

		// post-task build verification — catch broken code, dispatch remediation
		if result.State == task.StateCompleted {
			if buildErr := runner.QuickVerify(ctx, execDir); buildErr != nil {
				result.BuildError = buildErr.Error()
				slog.Warn("build broken after task completion",
					"task", t.ID, "runner", result.RunnerUsed, "error", buildErr)

				// dispatch remediation to strong runners (tier 1)
				remResult := runRemediation(ctx, t, execDir, outputDir, buildErr,
					runners, tf, blacklist, graylist, limiter, cfg.maxRuntime, cfg.maxRetries)
				if remResult != nil && remResult.State == task.StateCompleted {
					result.Remediated = true
					result.RemediatedBy = remResult.RunnerUsed
					runner.SanitizeHeadCommit(ctx, execDir)
					slog.Info("remediation succeeded",
						"task", t.ID, "original_runner", result.RunnerUsed,
						"remediated_by", remResult.RunnerUsed)
				} else {
					result.State = task.StateFailed
					errMsg := "build broken, remediation failed"
					if remResult != nil && remResult.Error != "" {
						errMsg += ": " + remResult.Error
					}
					result.Error = errMsg
					slog.Error("remediation failed",
						"task", t.ID, "runner", result.RunnerUsed, "build_error", buildErr)
				}
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
		// Build agent pool info for TUI (ANCC data + quota refresh)
		var agentPool *reporter.AgentPoolInfo
		anccAgents := collectANCCForTUI()
		qCache := &quotaCache{}
		if len(cfg.initialQuotas) > 0 {
			qCache.set(convertQuotas(cfg.initialQuotas))
		}
		startQuotaRefresh(ctx, qCache)
		// Build runner profile info for TUI display.
		profileInfo := make(map[string]reporter.RunnerProfileInfo)
		if tf.Runners != nil {
			for name, rp := range tf.Runners {
				profileInfo[name] = reporter.RunnerProfileInfo{
					Model:        rp.Model,
					Free:         rp.Free,
					Tier:         rp.Tier,
					FallbackOnly: rp.FallbackOnly,
				}
			}
		}
		agentPool = &reporter.AgentPoolInfo{
			Agents:    anccAgents,
			GetQuotas: qCache.get,

			IsGraylisted: graylist.IsGraylisted,
			GrayEntries: func() map[string]reporter.GraylistEntry {
				raw := graylist.Entries()
				out := make(map[string]reporter.GraylistEntry, len(raw))
				for k, v := range raw {
					out[k] = reporter.GraylistEntry{Model: v.Model, Reason: v.Reason, AddedAt: v.AddedAt}
				}
				return out
			},
			GrayAdd:       graylist.Add,
			GrayRemove:    graylist.Remove,
			IsBlacklisted: blacklist.IsBlocked,
			Profiles:      profileInfo,
		}
		// Collect runner names for interactive picker.
		var runnerNames []string
		for name := range runners {
			runnerNames = append(runnerNames, name)
		}
		sort.Strings(runnerNames)

		taskCtrl := &reporter.TaskControl{
			CancelTask:  sched.CancelTask,
			RequeueTask: sched.RequeueTask,
			Runners:     runnerNames,
		}
		tuiModel := reporter.NewTUIModel(cfg.graph, sched.Results, cancel, logPath, start, agentPool, taskCtrl, runDir)
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

	// auto-resolve merge conflicts from parallel repo execution
	if cfg.parallelRepo && cfg.mergeBack && !cfg.noMergeResolve {
		conflicts := collectConflicts(results, cfg.tasks)
		if len(conflicts) > 0 {
			resolveResult := runMergeResolve(ctx, conflicts, cfg, runners, defaultRunner, tf, blacklist, graylist, limiter, runDir)
			if resolveResult != nil {
				results[resolveResult.TaskID] = resolveResult
			}
		}
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

	// mirror run artifacts to docs/tokencontrol/<run-id>/ in each repo
	mirrorRunDocs(report, cfg.tasks, cfg.reposDir, cfg.settings.EffectiveDocsDir(), runDir, cfg.tasksFiles)

	// record telemetry (best-effort)
	if telDB, err := telemetry.OpenDB(telemetry.DefaultPath()); err == nil {
		defer func() { _ = telDB.Close() }()
		if err := telemetry.Record(telDB, report, cfg.tasks, tf.Runners); err != nil {
			slog.Warn("telemetry record failed", "error", err)
		}
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
			if r.Remediated {
				report.Remediations++
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
					FallbackOnly:   rp.FallbackOnly,
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

// conflictInfo holds metadata about a merge-conflicted task for resolution.
type conflictInfo struct {
	taskID string
	branch string
	title  string
	prompt string
	repo   string
}

// collectConflicts finds tasks with unmerged worktree branches.
func collectConflicts(results map[string]*task.TaskResult, tasks []task.Task) []conflictInfo {
	// index tasks by ID for prompt/title lookup
	taskMap := make(map[string]*task.Task, len(tasks))
	for i := range tasks {
		taskMap[tasks[i].ID] = &tasks[i]
	}

	var conflicts []conflictInfo
	for _, r := range results {
		if !r.MergeConflict || r.WorktreeBranch == "" {
			continue
		}
		ci := conflictInfo{
			taskID: r.TaskID,
			branch: r.WorktreeBranch,
		}
		if t, ok := taskMap[r.TaskID]; ok {
			ci.title = t.Title
			ci.repo = t.Repo
			ci.prompt = t.Prompt
			if len(ci.prompt) > 200 {
				ci.prompt = ci.prompt[:200] + "..."
			}
		}
		conflicts = append(conflicts, ci)
	}
	return conflicts
}

// runRemediation dispatches a build-fix task to strong runners (tier 1) when a
// weaker runner produces code that doesn't compile. The strong runner sees the
// repo in its current state (with the weak runner's commits) and gets a focused
// prompt to fix the build errors.
func runRemediation(
	ctx context.Context,
	original *task.Task,
	repoDir, outputDir string,
	buildErr error,
	runners map[string]runner.Runner,
	tf *task.TaskFile,
	blacklist *runner.RunnerBlacklist,
	graylist *runner.RunnerGraylist,
	limiter *runner.ProviderLimiter,
	maxRuntime time.Duration,
	maxRetries int,
) *task.TaskResult {
	prompt := fmt.Sprintf(`The previous agent completed task "%s" but the build is broken.

Fix the build errors below. Do NOT re-implement the task — the work is already done and committed.
Only fix what is needed to make the build pass.

Build errors:
%s

After fixing, run: go build ./...
If tests exist, also run: go test ./...

Commit your fix with: git add <files> && git commit -m "fix: resolve build errors from %s"
`, original.Title, buildErr.Error(), original.ID)

	remTask := &task.Task{
		ID:         original.ID + "-remediate",
		Title:      fmt.Sprintf("Build fix: %s", original.ID),
		Prompt:     prompt,
		Repo:       original.Repo,
		Difficulty: task.DifficultySimple, // build fixes are straightforward
	}

	// only use tier 1 runners for remediation
	var strongRunners []string
	for _, name := range tf.DefaultFallbacks {
		if task.DefaultTier(name) <= 1 {
			if _, ok := runners[name]; ok {
				strongRunners = append(strongRunners, name)
			}
		}
	}
	// also check default runner
	if task.DefaultTier(tf.DefaultRunner) <= 1 {
		if _, ok := runners[tf.DefaultRunner]; ok {
			// prepend if not already in list
			found := false
			for _, n := range strongRunners {
				if n == tf.DefaultRunner {
					found = true
					break
				}
			}
			if !found {
				strongRunners = append([]string{tf.DefaultRunner}, strongRunners...)
			}
		}
	}
	if len(strongRunners) == 0 {
		slog.Warn("no tier-1 runners available for remediation", "task", original.ID)
		return nil
	}

	remOutputDir := filepath.Join(outputDir, "remediate")
	if err := os.MkdirAll(remOutputDir, 0o755); err != nil {
		slog.Warn("cannot create remediation output dir", "error", err)
		return nil
	}

	slog.Info("dispatching build remediation",
		"task", original.ID, "runners", strongRunners)
	fmt.Fprintf(os.Stderr, "  → build broken, dispatching remediation to %v\n", strongRunners)

	return RunWithCascade(ctx, remTask, repoDir, remOutputDir, runners, strongRunners,
		maxRuntime, maxRetries, blacklist, graylist, limiter, nil)
}

// runMergeResolve dispatches a synthetic merge resolution task for conflicted branches.
func runMergeResolve(
	ctx context.Context,
	conflicts []conflictInfo,
	cfg execRunConfig,
	runners map[string]runner.Runner,
	defaultRunner string,
	tf *task.TaskFile,
	blacklist *runner.RunnerBlacklist,
	graylist *runner.RunnerGraylist,
	limiter *runner.ProviderLimiter,
	runDir string,
) *task.TaskResult {
	if len(conflicts) == 0 {
		return nil
	}

	// all conflicts must be on the same repo (parallel_repo is per-repo)
	repoDir := config.RepoPath(cfg.reposDir, conflicts[0].repo)

	// build the resolution prompt
	prompt := buildMergeResolvePrompt(ctx, repoDir, conflicts)

	resolveID := "merge-resolve"
	resolveTask := &task.Task{
		ID:         resolveID,
		Repo:       conflicts[0].repo,
		Title:      fmt.Sprintf("Merge resolve: %d branches", len(conflicts)),
		Prompt:     prompt,
		Difficulty: "medium",
	}

	// resolve cascade for the synthetic task
	cascade := resolveRunnerCascade(resolveTask, defaultRunner, tf.DefaultFallbacks)
	if len(cascade) == 0 {
		slog.Warn("merge-resolve: no runners available")
		return nil
	}

	outputDir := filepath.Join(runDir, resolveID)
	_ = os.MkdirAll(outputDir, 0o755)

	fmt.Fprintf(os.Stdout, "\nMerge resolution: dispatching agent to resolve %d conflicted branches...\n", len(conflicts))

	// acquire repo lock for merge resolution (runs on main repo, not worktree)
	if err := runner.WaitAndAcquire(ctx, repoDir, resolveID); err != nil {
		slog.Warn("merge-resolve: failed to acquire repo lock", "error", err)
		return nil
	}
	defer runner.Release(repoDir)

	// live tail — stream runner stderr to stdout so the user sees agent progress
	resolveStart := time.Now()
	tickDone := make(chan struct{})
	stderrPath := filepath.Join(outputDir, "stderr.log")
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		var offset int64
		for {
			select {
			case <-tickDone:
				return
			case <-ticker.C:
				offset = tailMergeLog(stderrPath, offset, resolveStart)
			}
		}
	}()

	result := RunWithCascade(ctx, resolveTask, repoDir, outputDir, runners, cascade,
		cfg.maxRuntime, cfg.maxRetries, blacklist, graylist, limiter,
		func(runnerName string) {
			fmt.Fprintf(os.Stdout, "  merge-resolve: using runner %q\n", runnerName)
		})
	close(tickDone)
	// flush remaining output
	tailMergeLog(stderrPath, 0, resolveStart)

	if result.State == task.StateCompleted {
		runner.SanitizeHeadCommit(ctx, repoDir)
		// verify which branches were merged and clean them up
		merged := 0
		for _, c := range conflicts {
			if isBranchMerged(ctx, repoDir, c.branch) {
				runner.DeleteBranch(ctx, repoDir, c.branch)
				merged++
			}
		}
		fmt.Fprintf(os.Stdout, "Merge resolution: %d/%d branches merged successfully\n", merged, len(conflicts))
	} else {
		slog.Warn("merge-resolve task failed, branches left for manual resolution",
			"state", result.State, "error", result.Error)
	}

	return result
}

// buildMergeResolvePrompt constructs agent instructions for resolving merge conflicts.
func buildMergeResolvePrompt(ctx context.Context, repoDir string, conflicts []conflictInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You have %d unmerged branches from parallel task execution that need to be merged into the current branch.\n\n", len(conflicts))
	b.WriteString("For each branch listed below, merge it sequentially using: git merge <branch>\n")
	b.WriteString("If there are conflicts, resolve them by understanding what each task intended.\n")
	b.WriteString("Do NOT delete any branch — just merge them.\n\n")

	for _, c := range conflicts {
		fmt.Fprintf(&b, "Branch: %s\n", c.branch)
		if c.title != "" {
			fmt.Fprintf(&b, "  Task: %q\n", c.title)
		}
		files := runner.ListConflictFiles(ctx, repoDir, c.branch)
		if len(files) > 0 {
			fmt.Fprintf(&b, "  Changed files: %s\n", strings.Join(files, ", "))
		}
		b.WriteString("\n")
	}

	b.WriteString("After merging all branches, run: go build ./... && go test ./... -race -count=1\n")
	b.WriteString("If a test fails after merging, fix the issue before proceeding to the next branch.\n")
	b.WriteString("If any merge cannot be resolved, stop and report which branch failed and why.\n")

	return b.String()
}

// isBranchMerged checks if a branch has been merged into HEAD.
func isBranchMerged(ctx context.Context, repoDir, branch string) bool {
	cmd := exec.CommandContext(ctx, "git", "branch", "--merged", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == branch {
			return true
		}
	}
	return false
}

// tailMergeLog reads new lines from the merge-resolve stderr log and prints
// them to stdout with a prefix. Returns the new file offset.
func tailMergeLog(path string, offset int64, start time.Time) int64 {
	f, err := os.Open(path)
	if err != nil {
		return offset
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || info.Size() <= offset {
		return offset
	}

	if _, err := f.Seek(offset, 0); err != nil {
		return offset
	}

	buf := make([]byte, info.Size()-offset)
	n, err := f.Read(buf)
	if err != nil || n == 0 {
		return offset
	}

	lines := strings.Split(strings.TrimRight(string(buf[:n]), "\n"), "\n")
	elapsed := time.Since(start).Truncate(time.Second)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fmt.Fprintf(os.Stdout, "  [%s] %s\n", elapsed, line)
	}
	return offset + int64(n)
}

// stripeRunners distributes primary runner assignments across available
// providers for parallel utilization. Tasks without an explicit runner
// get round-robin primary assignment; each task's fallbacks contain all
// other providers. Tasks with an explicit Runner field are not modified.
func stripeRunners(tasks []task.Task, defaultRunner string, fallbacks []string, profiles map[string]*task.RunnerProfileConfig) {
	if len(fallbacks) == 0 {
		return
	}

	// Build primary-eligible list: default runner + fallbacks that aren't fallback-only.
	var primaries []string
	primaries = append(primaries, defaultRunner)
	for _, fb := range fallbacks {
		if p := profiles[fb]; p != nil && p.FallbackOnly {
			continue
		}
		if fb != defaultRunner {
			primaries = append(primaries, fb)
		}
	}

	// Full runner list for fallback cascade (all runners, including fallback-only).
	all := []string{defaultRunner}
	for _, fb := range fallbacks {
		if fb != defaultRunner {
			all = append(all, fb)
		}
	}

	for i := range tasks {
		if tasks[i].Runner != "" {
			continue
		}
		idx := i % len(primaries)
		tasks[i].Runner = primaries[idx]
		var fb []string
		for _, r := range all {
			if r != primaries[idx] {
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
		repoPath := config.RepoPath(repo, reposDir)
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
		case task.StateWaiting, task.StateRunning:
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

// autoGraylistRunners scans results for false positives and build failures,
// then auto-graylists the responsible runner+model pairs.
func autoGraylistRunners(results map[string]*task.TaskResult, graylist *runner.RunnerGraylist, profiles map[string]*task.RunnerProfileConfig, runID string) {
	if graylist == nil {
		return
	}

	type key struct{ runner, model string }
	fpCounts := make(map[key]int)   // false positive counts
	buildFails := make(map[key]int) // build verification failure counts

	for _, res := range results {
		// false positives on final result
		if res.FalsePositive && res.RunnerUsed != "" {
			model := ""
			if p, ok := profiles[res.RunnerUsed]; ok {
				model = p.Model
			}
			fpCounts[key{res.RunnerUsed, model}]++
		}

		// build failures: runner produced code that needed remediation
		if res.Remediated && res.RunnerUsed != "" {
			model := ""
			if p, ok := profiles[res.RunnerUsed]; ok {
				model = p.Model
			}
			buildFails[key{res.RunnerUsed, model}]++
		}
		// also check failed tasks with build errors
		if res.BuildError != "" && res.State == task.StateFailed && res.RunnerUsed != "" {
			model := ""
			if p, ok := profiles[res.RunnerUsed]; ok {
				model = p.Model
			}
			buildFails[key{res.RunnerUsed, model}]++
		}
	}

	if len(fpCounts) == 0 && len(buildFails) == 0 {
		return
	}

	// merge all quality signals
	type signal struct {
		fp    int
		build int
	}
	merged := make(map[key]*signal)
	for k, c := range fpCounts {
		if merged[k] == nil {
			merged[k] = &signal{}
		}
		merged[k].fp = c
	}
	for k, c := range buildFails {
		if merged[k] == nil {
			merged[k] = &signal{}
		}
		merged[k].build = c
	}

	fmt.Fprintf(os.Stdout, "\nQuality issues detected:\n")
	for k, s := range merged {
		if k.model == "" {
			slog.Warn("skipping auto-graylist: no model in runner profile",
				"runner", k.runner, "false_positives", s.fp, "build_failures", s.build)
			fmt.Fprintf(os.Stdout, "  %s: quality issues but no model in profile — add model for auto-graylist\n", k.runner)
			continue
		}

		var reasons []string
		if s.fp > 0 {
			reasons = append(reasons, fmt.Sprintf("%d false positives", s.fp))
		}
		if s.build > 0 {
			reasons = append(reasons, fmt.Sprintf("%d build failures", s.build))
		}
		reason := fmt.Sprintf("quality: %s in run %s", strings.Join(reasons, ", "), runID)
		label := k.runner + " (model: " + k.model + ")"
		if !graylist.IsGraylisted(k.runner, k.model) {
			graylist.Add(k.runner, k.model, reason)
			fmt.Fprintf(os.Stdout, "  graylisted %s (%s)\n", label, strings.Join(reasons, ", "))
		} else {
			fmt.Fprintf(os.Stdout, "  %s already graylisted (%s)\n", label, strings.Join(reasons, ", "))
		}
	}
	fmt.Fprintf(os.Stdout, "  Use 'tokencontrol graylist list' to view, 'tokencontrol graylist remove <runner>' to reinstate\n")
}

// injectPromptConventions appends shared config conventions to agent-bound task
// prompts at runtime. This closes the gap for task packs that were not created
// via `tokencontrol generate` (for example workledger exports or hand-written
// phase files). Script runner tasks are skipped because their prompts are shell
// commands, not agent instructions.
func injectPromptConventions(tasks []task.Task, conventions string, runners map[string]runner.Runner) {
	conventions = strings.TrimSpace(conventions)
	if conventions == "" {
		return
	}

	for i := range tasks {
		if r, ok := runners[tasks[i].Runner]; ok {
			if _, isScript := r.(*runner.ScriptRunner); isScript {
				continue
			}
		}
		if strings.Contains(tasks[i].Prompt, conventions) {
			continue
		}
		tasks[i].Prompt += "\n\n" + conventions
	}
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

// injectDocDirective tells agents to place generated documentation in the gitignored
// docs directory. Script runner tasks are skipped since their prompts are shell commands.
func injectDocDirective(tasks []task.Task, runners map[string]runner.Runner, docsDir string) {
	directive := fmt.Sprintf(
		"\n\nIMPORTANT: When generating documentation files (summaries, changelogs, "+
			"analysis, or any markdown artifacts), place them in %s/ inside the repository. "+
			"Do NOT place generated documentation in the repository root or docs/ directly. "+
			"The %s/ directory is reserved for agent-generated artifacts.", docsDir, docsDir)

	for i := range tasks {
		if r, ok := runners[tasks[i].Runner]; ok {
			if _, isScript := r.(*runner.ScriptRunner); isScript {
				continue
			}
		}
		if strings.Contains(tasks[i].Prompt, docsDir) {
			continue
		}
		tasks[i].Prompt += directive
	}
}

// mirrorRunDocs copies run-level artifacts (report, original task files) and per-task
// outputs to docs/tokencontrol/<run-id>/ in each repo touched by the run.
func mirrorRunDocs(report *task.RunReport, tasks []task.Task, reposDir, docsDir, runDir string, tasksFiles []string) {
	seen := make(map[string]bool)
	for _, t := range tasks {
		repoDir := config.RepoPath(t.Repo, reposDir)
		if seen[repoDir] {
			continue
		}
		seen[repoDir] = true

		destDir := filepath.Join(repoDir, docsDir, report.RunID)
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			continue
		}

		// copy report
		data, err := json.MarshalIndent(report, "", "  ")
		if err == nil {
			_ = os.WriteFile(filepath.Join(destDir, "report.json"), data, 0o644)
		}

		// copy original task file(s) that caused this run
		for _, tf := range tasksFiles {
			src, err := os.ReadFile(tf)
			if err != nil {
				continue
			}
			_ = os.WriteFile(filepath.Join(destDir, filepath.Base(tf)), src, 0o644)
		}
	}

	// copy per-task artifacts from run dir into each repo's docs
	for _, t := range tasks {
		repoDir := config.RepoPath(t.Repo, reposDir)
		taskOutputDir := filepath.Join(runDir, t.ID)
		taskDestDir := filepath.Join(repoDir, docsDir, report.RunID, t.ID)
		if err := os.MkdirAll(taskDestDir, 0o755); err != nil {
			continue
		}
		for _, name := range []string{"events.jsonl", "output.md", "output.log", "task.json"} {
			src, err := os.ReadFile(filepath.Join(taskOutputDir, name))
			if err != nil {
				continue
			}
			_ = os.WriteFile(filepath.Join(taskDestDir, name), src, 0o644)
		}
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

// agentReadiness holds per-agent ANCC data for runners used in a run.
type agentReadiness struct {
	Name   string
	Skills int
	Hooks  int
	Tokens int
}

// checkRunReadiness runs ANCC-based agent readiness checks for runners used in the task file.
// Returns warnings and per-agent summary. Both are nil if ANCC is not available.
func checkRunReadiness(tf *task.TaskFile) ([]string, []agentReadiness) {
	_, err := exec.LookPath("ancc")
	if err != nil {
		return nil, nil
	}

	out, err := exec.Command("ancc", "skills", "--format", "json").CombinedOutput()
	if err != nil {
		return nil, nil
	}

	var result anccSkillsResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, nil
	}

	agentMap := mapANCCAgents(result.Agents)
	var warnings []string
	var agents []agentReadiness

	usedRunners := collectUsedRunners(tf)
	safeRunners := map[string]bool{"claude": true, "cline": true}

	for name := range usedRunners {
		agent, found := agentMap[name]
		if !found {
			continue
		}
		agents = append(agents, agentReadiness{
			Name:   name,
			Skills: agent.Skills,
			Hooks:  agent.Hooks,
			Tokens: agent.Tokens,
		})
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

	return warnings, agents
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

// quotaCache provides thread-safe access to periodically refreshed quota data.
type quotaCache struct {
	mu     sync.RWMutex
	quotas []reporter.QuotaInfo
}

func (qc *quotaCache) get() []reporter.QuotaInfo {
	qc.mu.RLock()
	defer qc.mu.RUnlock()
	out := make([]reporter.QuotaInfo, len(qc.quotas))
	copy(out, qc.quotas)
	return out
}

func (qc *quotaCache) set(quotas []reporter.QuotaInfo) {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	qc.quotas = quotas
}

// convertQuotas converts runner.QuotaInfo to reporter.QuotaInfo (avoids circular import).
func convertQuotas(infos []*runner.QuotaInfo) []reporter.QuotaInfo {
	var out []reporter.QuotaInfo
	for _, qi := range infos {
		out = append(out, reporter.QuotaInfo{
			Provider:   qi.Provider,
			UsedTokens: qi.UsedTokens,
			BurnRate:   qi.BurnRatePerDay,
			Balance:    qi.Balance,
			Currency:   qi.Currency,
			Available:  qi.Available,
			Error:      qi.Error,
		})
	}
	return out
}

// startQuotaRefresh launches a background goroutine that refreshes quotas every 5 minutes.
func startQuotaRefresh(ctx context.Context, cache *quotaCache) {
	ticker := time.NewTicker(5 * time.Minute)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				qCtx, qCancel := context.WithTimeout(ctx, 30*time.Second)
				infos := runner.CheckAllQuotas(qCtx, os.Getenv)
				qCancel()
				if len(infos) > 0 {
					cache.set(convertQuotas(infos))
				}
			}
		}
	}()
}

// collectANCCForTUI gathers ANCC agent data for the TUI agent panel.
// Returns nil if ANCC is not available.
func collectANCCForTUI() []reporter.AgentInfo {
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

	knownRunners := []string{"codex", "claude", "gemini", "opencode", "cline", "qwen"}
	var agents []reporter.AgentInfo
	for _, name := range knownRunners {
		a, found := agentMap[name]
		if !found {
			continue
		}
		agents = append(agents, reporter.AgentInfo{
			Name:   name,
			Skills: a.Skills,
			Hooks:  a.Hooks,
			MCP:    a.MCP,
			Tokens: a.Tokens,
		})
	}
	return agents
}
