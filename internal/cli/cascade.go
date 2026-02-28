package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ppiankov/runforge/internal/runner"
	"github.com/ppiankov/runforge/internal/task"
)

// RunWithCascade attempts to run a task using a sequence of runners.
// On rate-limit or failure, it falls to the next runner in the list.
// It records all attempts and populates RunnerUsed on the final result.
func RunWithCascade(
	ctx context.Context,
	t *task.Task,
	repoDir, outputDir string,
	runners map[string]runner.Runner,
	runnerNames []string,
	maxRuntime time.Duration,
	blacklist *runner.RunnerBlacklist,
	limiter *runner.ProviderLimiter,
) *task.TaskResult {
	if len(runnerNames) == 0 {
		return &task.TaskResult{
			TaskID:  t.ID,
			State:   task.StateFailed,
			Error:   "no runners configured",
			EndedAt: time.Now(),
		}
	}

	var attempts []task.AttemptInfo
	var lastResult *task.TaskResult

	for i, name := range runnerNames {
		if blacklist.IsBlocked(name) {
			slog.Debug("runner blacklisted, skipping", "task", t.ID, "runner", name)
			attempts = append(attempts, task.AttemptInfo{
				Runner: name,
				State:  task.StateSkipped,
				Error:  "runner blacklisted",
			})
			continue
		}

		r, ok := runners[name]
		if !ok {
			attempts = append(attempts, task.AttemptInfo{
				Runner: name,
				State:  task.StateFailed,
				Error:  fmt.Sprintf("unknown runner: %q", name),
			})
			continue
		}

		// determine output dir for this attempt
		attemptDir := outputDir
		if i > 0 {
			attemptDir = filepath.Join(outputDir, fmt.Sprintf("attempt-%d-%s", i+1, name))
			if err := os.MkdirAll(attemptDir, 0o755); err != nil {
				attempts = append(attempts, task.AttemptInfo{
					Runner: name,
					State:  task.StateFailed,
					Error:  fmt.Sprintf("create attempt dir: %v", err),
				})
				continue
			}
		}

		if limiter != nil {
			limiter.Acquire(name)
		}
		taskCtx, taskCancel := context.WithTimeout(ctx, maxRuntime)
		start := time.Now()
		result := r.Run(taskCtx, t, repoDir, attemptDir)
		taskCancel()
		if limiter != nil {
			limiter.Release(name)
		}
		elapsed := time.Since(start)

		// Scan output files for leaked secrets and redact in place.
		if leaks := runner.ScanOutputDir(attemptDir); leaks > 0 {
			slog.Warn("output scan found secrets", "task", t.ID, "runner", name, "leaks", leaks)
		}

		attempts = append(attempts, task.AttemptInfo{
			Runner:            name,
			State:             result.State,
			Duration:          elapsed,
			Error:             result.Error,
			OutputDir:         attemptDir,
			ConnectivityError: result.ConnectivityError,
		})

		lastResult = result

		switch result.State {
		case task.StateCompleted:
			result.RunnerUsed = name
			result.Attempts = attempts
			return result

		case task.StateRateLimited:
			slog.Warn("runner rate-limited, trying next", "task", t.ID, "runner", name)
			if !result.ResetsAt.IsZero() {
				blacklist.Block(name, result.ResetsAt)
			} else {
				// block for 1 hour if no resets_at provided
				blacklist.Block(name, time.Now().Add(1*time.Hour))
			}
			continue

		case task.StateFailed:
			if result.ConnectivityError != "" {
				slog.Warn("runner connectivity error, blacklisting",
					"task", t.ID, "runner", name, "error", result.ConnectivityError)
				blacklist.Block(name, time.Now().Add(24*time.Hour))
			} else {
				slog.Warn("runner failed, trying next", "task", t.ID, "runner", name, "error", result.Error)
			}
			continue
		}
	}

	// all runners exhausted — return last result
	if lastResult == nil {
		lastResult = &task.TaskResult{
			TaskID:  t.ID,
			State:   task.StateFailed,
			Error:   "all runners skipped or unavailable",
			EndedAt: time.Now(),
		}
	}
	lastResult.RunnerUsed = runnerNames[len(runnerNames)-1]
	lastResult.Attempts = attempts
	return lastResult
}

// buildRunnerRegistry constructs runner instances from built-in defaults
// and task file profiles. Profiles override built-in runners of the same name.
func buildRunnerRegistry(tf *task.TaskFile, idleTimeout time.Duration) (map[string]runner.Runner, error) {
	runners := map[string]runner.Runner{
		"codex":    runner.NewCodexRunner(idleTimeout),
		"claude":   runner.NewClaudeRunner(idleTimeout),
		"gemini":   runner.NewGeminiRunner(idleTimeout),
		"opencode": runner.NewOpencodeRunner(idleTimeout),
		"script":   runner.NewScriptRunner(),
	}

	for name, profile := range tf.Runners {
		resolved, err := runner.ResolveEnv(profile.Env)
		if err != nil {
			return nil, fmt.Errorf("runner %q env: %w", name, err)
		}
		switch profile.Type {
		case "codex":
			runners[name] = runner.NewCodexRunnerWithProfile(profile.Model, profile.Profile, resolved, idleTimeout)
		case "claude":
			runners[name] = runner.NewClaudeRunnerWithProfile(profile.Model, resolved, idleTimeout)
		case "gemini":
			runners[name] = runner.NewGeminiRunnerWithProfile(profile.Model, resolved, idleTimeout)
		case "opencode":
			runners[name] = runner.NewOpencodeRunnerWithProfile(profile.Model, resolved, idleTimeout)
		case "script":
			runners[name] = runner.NewScriptRunnerWithEnv(resolved)
		default:
			return nil, fmt.Errorf("runner %q has unknown type %q", name, profile.Type)
		}
	}

	return runners, nil
}

// validateAndResolveModels checks runner models against local config files
// and auto-resolves mismatches. Mutates tf.Runners profiles in-place and
// rebuilds affected runners in the registry. Returns resolutions for logging.
func validateAndResolveModels(
	tf *task.TaskFile,
	runners map[string]runner.Runner,
	idleTimeout time.Duration,
) []runner.ModelResolution {
	if len(tf.Runners) == 0 {
		return nil
	}

	resolutions, _ := runner.ValidateModels(runners, tf.Runners)

	// rebuild runners whose models were resolved
	for _, res := range resolutions {
		profile := tf.Runners[res.RunnerProfile]
		if profile == nil {
			continue
		}
		resolved, err := runner.ResolveEnv(profile.Env)
		if err != nil {
			continue
		}
		switch profile.Type {
		case "opencode":
			runners[res.RunnerProfile] = runner.NewOpencodeRunnerWithProfile(
				profile.Model, resolved, idleTimeout)
		case "codex":
			runners[res.RunnerProfile] = runner.NewCodexRunnerWithProfile(
				profile.Model, profile.Profile, resolved, idleTimeout)
		case "claude":
			runners[res.RunnerProfile] = runner.NewClaudeRunnerWithProfile(
				profile.Model, resolved, idleTimeout)
		case "gemini":
			runners[res.RunnerProfile] = runner.NewGeminiRunnerWithProfile(
				profile.Model, resolved, idleTimeout)
		}
	}

	return resolutions
}

// buildProviderLimiter creates a ProviderLimiter from concurrency limits.
// Only entries with limit > 0 are enforced.
func buildProviderLimiter(limits map[string]int) *runner.ProviderLimiter {
	if len(limits) == 0 {
		return nil
	}
	return runner.NewProviderLimiter(limits)
}

// filterDataCollectionRunners removes runners marked with data_collection: true
// from the cascade when the task targets a private repo. This is a structural
// safeguard: private code must never be sent to providers that use data for training.
func filterDataCollectionRunners(
	cascade []string,
	repo string,
	profiles map[string]*task.RunnerProfileConfig,
	privateRepos map[string]struct{},
) []string {
	if len(privateRepos) == 0 {
		return cascade
	}
	if _, isPrivate := privateRepos[repo]; !isPrivate {
		return cascade
	}

	filtered := make([]string, 0, len(cascade))
	for _, name := range cascade {
		p, ok := profiles[name]
		if ok && p.DataCollection {
			slog.Warn("skipping data-collecting runner for private repo",
				"runner", name, "repo", repo)
			continue
		}
		filtered = append(filtered, name)
	}
	return filtered
}

// resolveRunnerCascade determines the ordered list of runners to try for a task.
func resolveRunnerCascade(t *task.Task, defaultRunner string, defaultFallbacks []string) []string {
	primary := t.Runner
	if primary == "" {
		primary = defaultRunner
	}

	names := []string{primary}

	fallbacks := t.Fallbacks
	if len(fallbacks) == 0 {
		fallbacks = defaultFallbacks
	}
	for _, fb := range fallbacks {
		if fb != primary {
			names = append(names, fb)
		}
	}

	return names
}
