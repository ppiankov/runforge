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

		taskCtx, taskCancel := context.WithTimeout(ctx, maxRuntime)
		start := time.Now()
		result := r.Run(taskCtx, t, repoDir, attemptDir)
		taskCancel()
		elapsed := time.Since(start)

		attempts = append(attempts, task.AttemptInfo{
			Runner:    name,
			State:     result.State,
			Duration:  elapsed,
			Error:     result.Error,
			OutputDir: attemptDir,
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
			slog.Warn("runner failed, trying next", "task", t.ID, "runner", name, "error", result.Error)
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
func buildRunnerRegistry(tf *task.TaskFile) (map[string]runner.Runner, error) {
	runners := map[string]runner.Runner{
		"codex":  runner.NewCodexRunner(),
		"claude": runner.NewClaudeRunner(),
		"script": runner.NewScriptRunner(),
	}

	for name, profile := range tf.Runners {
		resolved, err := runner.ResolveEnv(profile.Env)
		if err != nil {
			return nil, fmt.Errorf("runner %q env: %w", name, err)
		}
		switch profile.Type {
		case "codex":
			runners[name] = runner.NewCodexRunnerWithProfile(profile.Model, profile.Profile, resolved)
		case "claude":
			runners[name] = runner.NewClaudeRunnerWithProfile(profile.Model, resolved)
		case "script":
			runners[name] = runner.NewScriptRunnerWithEnv(resolved)
		default:
			return nil, fmt.Errorf("runner %q has unknown type %q", name, profile.Type)
		}
	}

	return runners, nil
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
