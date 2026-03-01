package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ppiankov/neurorouter"
	"github.com/ppiankov/runforge/internal/config"
	"github.com/ppiankov/runforge/internal/reporter"
	"github.com/ppiankov/runforge/internal/runner"
	"github.com/ppiankov/runforge/internal/task"
)

func newRunCmd() *cobra.Command {
	var (
		tasksFile   string
		workers     int
		verify      bool
		reposDir    string
		filter      string
		dryRun      bool
		maxRuntime  time.Duration
		idleTimeout time.Duration
		failFast    bool
		tuiMode     string
		allowFree   bool
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
			return runTasks(tasksFile, workers, verify, reposDir, filter, dryRun, maxRuntime, idleTimeout, failFast, tuiMode, allowFree, cfg)
		},
	}

	cmd.Flags().StringVar(&tasksFile, "tasks", "runforge.json", "path to tasks JSON file (supports glob patterns)")
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

	return cmd
}

func runTasks(tasksFile string, workers int, verify bool, reposDir, filter string, dryRun bool, maxRuntime, idleTimeout time.Duration, failFast bool, tuiMode string, allowFree bool, cfg *config.Settings) error {
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

	// pre-validate and auto-resolve runner models
	var modelResolutions []runner.ModelResolution
	if len(tf.Runners) > 0 {
		tmpRunners, buildErr := buildRunnerRegistry(tf, idleTimeout)
		if buildErr == nil {
			modelResolutions = validateAndResolveModels(tf, tmpRunners, idleTimeout)
		}
	}

	// dry run
	if dryRun {
		textRep.PrintHeader(len(tasks), workers)
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
		textRep.PrintDryRun(graph, reposDir)
		return nil
	}

	report, err := executeRun(execRunConfig{
		tasksFiles:  paths,
		taskFile:    tf,
		tasks:       tasks,
		graph:       graph,
		workers:     workers,
		reposDir:    reposDir,
		filter:      filter,
		maxRuntime:  maxRuntime,
		idleTimeout: idleTimeout,
		failFast:    failFast,
		postRun:     cfg.PostRun,
		settings:    cfg,
		tuiMode:     tuiMode,
		allowFree:   allowFree,
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
	tasksFiles  []string
	taskFile    *task.TaskFile // full parsed/merged file with profiles
	tasks       []task.Task
	graph       *task.Graph
	workers     int
	reposDir    string
	filter      string
	maxRuntime  time.Duration
	idleTimeout time.Duration
	failFast    bool
	parentRunID string                                    // links rerun to original run
	postRun     string                                    // shell command to run after report is written
	settings    *config.Settings                          // runtime settings for limiter etc.
	tuiMode     string                                    // full, minimal, off, auto
	allowFree   bool                                      // include free-tier runners in cascade
	onProgress  func(results map[string]*task.TaskResult) // optional progress callback for sentinel
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
			// non-fatal: another runforge process may already own the port
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

	// inject commit instructions into all agent-bound prompts (safety net)
	injectCommitInstructions(cfg.tasks, runners)

	execFn := func(ctx context.Context, t *task.Task, repoDir, outputDir string) *task.TaskResult {
		if err := runner.WaitAndAcquire(ctx, repoDir, t.ID); err != nil {
			return &task.TaskResult{
				TaskID:  t.ID,
				State:   task.StateFailed,
				Error:   fmt.Sprintf("acquire lock: %v", err),
				EndedAt: time.Now(),
			}
		}
		defer runner.Release(repoDir)

		// save task metadata so output dir is self-contained
		writeTaskMeta(outputDir, t)

		cascade := resolveRunnerCascade(t, defaultRunner, tf.DefaultFallbacks)
		cascade = filterDataCollectionRunners(cascade, t.Repo, tf.Runners, privateRepos)
		cascade = filterGraylistedRunners(cascade, graylist, tf.Runners)
		cascade = filterFreeRunners(cascade, cfg.allowFree, tf.Runners)
		return RunWithCascade(ctx, t, repoDir, outputDir, runners, cascade, cfg.maxRuntime, blacklist, graylist, limiter)
	}

	// run scheduler
	start := time.Now()
	var sched *task.Scheduler
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
	switch displayMode {
	case "full":
		tuiModel := reporter.NewTUIModel(cfg.graph, sched.Results, cancel)
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
				}
			}
		}
	}
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

const statusDir = "/tmp/runforge-status.d"

// statusFilePath returns the per-process status file path.
func statusFilePath() string {
	return filepath.Join(statusDir, fmt.Sprintf("%d", os.Getpid()))
}

// writeStatusFile writes a one-line status to a per-PID file for external consumers (e.g. statusline).
// Multiple runforge processes write separate files; the statusline aggregates them.
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
		reason := fmt.Sprintf("false positive: %d tasks with 0 events in run %s", count, runID)
		label := k.runner
		if k.model != "" {
			label = k.runner + " (model: " + k.model + ")"
		}
		if !graylist.IsGraylisted(k.runner, k.model) {
			graylist.Add(k.runner, k.model, reason)
			fmt.Fprintf(os.Stdout, "  graylisted %s (%d tasks, 0 events)\n", label, count)
		} else {
			fmt.Fprintf(os.Stdout, "  %s already graylisted (%d more false positives)\n", label, count)
		}
	}
	fmt.Fprintf(os.Stdout, "  Use 'runforge graylist list' to view, 'runforge graylist remove <runner>' to reinstate\n")
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
