package cli

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ppiankov/tokencontrol/internal/runner"
	"github.com/ppiankov/tokencontrol/internal/task"
)

// retryBackoffs defines the sleep duration before each retry attempt.
var retryBackoffs = []time.Duration{30 * time.Second, 60 * time.Second}

// RunWithCascade attempts to run a task using a sequence of runners.
// On rate-limit or failure, it falls to the next runner in the list.
// Transient failures (connectivity, idle timeout) are retried up to maxRetries
// times with backoff before falling to the next runner.
// It records all attempts and populates RunnerUsed on the final result.
func RunWithCascade(
	ctx context.Context,
	t *task.Task,
	repoDir, outputDir string,
	runners map[string]runner.Runner,
	runnerNames []string,
	maxRuntime time.Duration,
	maxRetries int,
	blacklist *runner.RunnerBlacklist,
	graylist *runner.RunnerGraylist,
	limiter *runner.ProviderLimiter,
	onAttemptStart func(runner string),
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

		// retry loop: try the same runner up to maxRetries+1 times on transient failures
		var result *task.TaskResult
		for retry := 0; retry <= maxRetries; retry++ {
			if retry > 0 {
				// backoff before retry
				backoff := retryBackoff(retry)
				slog.Info("retrying runner after transient failure",
					"task", t.ID, "runner", name, "retry", retry, "backoff", backoff)

				// re-check connectivity before retrying — don't waste time if still offline
				if !checkConnectivityQuick() {
					slog.Warn("still offline, skipping retry",
						"task", t.ID, "runner", name, "retry", retry)
					break
				}

				select {
				case <-ctx.Done():
					break
				case <-time.After(backoff):
				}
				if ctx.Err() != nil {
					break
				}
			}

			// determine output dir for this attempt
			attemptDir := outputDir
			if i > 0 || retry > 0 {
				label := fmt.Sprintf("attempt-%d-%s", i+1, name)
				if retry > 0 {
					label = fmt.Sprintf("retry-%d-%s", retry, name)
				}
				attemptDir = filepath.Join(outputDir, label)
				if err := os.MkdirAll(attemptDir, 0o755); err != nil {
					attempts = append(attempts, task.AttemptInfo{
						Runner: name,
						Retry:  retry,
						State:  task.StateFailed,
						Error:  fmt.Sprintf("create attempt dir: %v", err),
					})
					continue
				}
			}

			// capture HEAD before run so we can detect new commits
			headBefore := gitHead(repoDir)

			if onAttemptStart != nil {
				onAttemptStart(name)
			}
			if limiter != nil {
				limiter.Acquire(name)
			}
			taskCtx, taskCancel := context.WithTimeout(ctx, maxRuntime)
			start := time.Now()
			result = r.Run(taskCtx, t, repoDir, attemptDir)
			taskCancel()
			if limiter != nil {
				limiter.Release(name)
			}
			elapsed := time.Since(start)

			// Scan output files for leaked secrets and redact in place.
			if leaks := runner.ScanOutputDir(attemptDir); leaks > 0 {
				slog.Warn("output scan found secrets", "task", t.ID, "runner", name, "dir", attemptDir, "leaks", leaks)
			}

			attempts = append(attempts, task.AttemptInfo{
				Runner:            name,
				Retry:             retry,
				State:             result.State,
				Duration:          elapsed,
				Error:             result.Error,
				OutputDir:         attemptDir,
				ConnectivityError: result.ConnectivityError,
			})

			// on success, verify build then check for false positive
			if result.State == task.StateCompleted {
				// post-task build gate: catch broken code before accepting
				if buildErr := runner.QuickVerify(ctx, repoDir); buildErr != nil {
					slog.Warn("post-task build verification failed",
						"task", t.ID, "runner", name, "error", buildErr)
					result.State = task.StateFailed
					result.Error = fmt.Sprintf("build verification failed: %v", buildErr)
					attempts[len(attempts)-1].State = task.StateFailed
					attempts[len(attempts)-1].Error = result.Error
					break // non-transient failure, try next runner
				}

				result.RunnerUsed = name
				result.Attempts = attempts
				if isFalsePositive(attemptDir, repoDir, headBefore) {
					result.FalsePositive = true
					slog.Warn("false positive: no new commits after task completion",
						"task", t.ID, "runner", name)
				}
				return result
			}

			// only retry on transient failures
			if !isTransientFailure(result) {
				break
			}
		}

		lastResult = result

		// handle final result for this runner (after retries exhausted or non-transient)
		switch result.State {
		case task.StateRateLimited:
			slog.Warn("runner rate-limited, trying next", "task", t.ID, "runner", name)
			if !result.ResetsAt.IsZero() {
				blacklist.Block(name, result.ResetsAt)
			} else {
				blacklist.Block(name, time.Now().Add(1*time.Hour))
			}
			continue

		case task.StateFailed:
			if result.ConnectivityError != "" {
				slog.Warn("runner connectivity error, blacklisting",
					"task", t.ID, "runner", name, "error", result.ConnectivityError)
				blacklist.Block(name, time.Now().Add(1*time.Hour))
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

// isTransientFailure returns true for failures that may resolve on retry:
// connectivity errors (except permanent TLS cert issues) and idle timeouts.
// Rate limits are NOT transient here — they have their own blacklist+cascade logic.
func isTransientFailure(result *task.TaskResult) bool {
	if result.State != task.StateFailed {
		return false
	}
	if result.ConnectivityError != "" {
		return result.ConnectivityError != "TLS certificate expired"
	}
	if strings.Contains(result.Error, "idle timeout") {
		return true
	}
	return false
}

// retryBackoff returns the backoff duration for a given retry number (1-indexed).
func retryBackoff(retry int) time.Duration {
	if retry <= 0 {
		return 0
	}
	idx := retry - 1
	if idx >= len(retryBackoffs) {
		return retryBackoffs[len(retryBackoffs)-1]
	}
	return retryBackoffs[idx]
}

// checkConnectivityQuick does a fast DNS lookup to verify network is available.
func checkConnectivityQuick() bool {
	_, err := net.LookupHost("dns.google")
	return err == nil
}

// buildRunnerRegistry constructs runner instances from built-in defaults
// and task file profiles. Profiles override built-in runners of the same name.
func buildRunnerRegistry(tf *task.TaskFile, idleTimeout time.Duration) (map[string]runner.Runner, error) {
	runners := map[string]runner.Runner{
		"codex":    runner.NewCodexRunner(idleTimeout),
		"claude":   runner.NewClaudeRunner(idleTimeout),
		"gemini":   runner.NewGeminiRunner(idleTimeout),
		"opencode": runner.NewOpencodeRunner(idleTimeout),
		"cline":    runner.NewClineRunner(idleTimeout),
		"qwen":     runner.NewQwenRunner(idleTimeout),
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
		case "cline":
			runners[name] = runner.NewClineRunnerWithProfile(profile.Model, resolved, idleTimeout)
		case "qwen":
			runners[name] = runner.NewQwenRunnerWithProfile(profile.Model, resolved, idleTimeout)
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

// filterEntry records one filter's removal of runners from a cascade.
type filterEntry struct {
	Filter  string   // "graylist", "free", "secret", "tier", "data-collection"
	Removed []string // runner names removed
	Reason  string   // human-readable explanation
}

// filterLog accumulates removal reasons across the filter chain.
type filterLog struct {
	entries []filterEntry
}

func (fl *filterLog) add(filter string, removed []string, reason string) {
	if fl == nil || len(removed) == 0 {
		return
	}
	fl.entries = append(fl.entries, filterEntry{Filter: filter, Removed: removed, Reason: reason})
}

// formatCascadeError builds a detailed error message from filter log entries.
func formatCascadeError(taskID string, fl *filterLog) string {
	var b strings.Builder
	fmt.Fprintf(&b, "no eligible runners for task %s", taskID)

	if fl != nil {
		for _, e := range fl.entries {
			fmt.Fprintf(&b, "\n  %-18s removed %s (%s)", e.Filter+":", strings.Join(e.Removed, ", "), e.Reason)
		}

		suggestions := cascadeSuggestions(fl)
		if len(suggestions) > 0 {
			b.WriteString("\nSuggestions:")
			for _, s := range suggestions {
				fmt.Fprintf(&b, "\n  %s", s)
			}
		}
	}
	return b.String()
}

// cascadeSuggestions generates actionable fix suggestions from filter log entries.
func cascadeSuggestions(fl *filterLog) []string {
	if fl == nil {
		return nil
	}
	var suggestions []string
	for _, e := range fl.entries {
		switch e.Filter {
		case "graylist":
			for _, r := range e.Removed {
				suggestions = append(suggestions, fmt.Sprintf("tokencontrol graylist remove %s", r))
			}
		case "free":
			suggestions = append(suggestions, "tokencontrol run --allow-free")
		case "secret":
			suggestions = append(suggestions, "use claude or cline (safe runners with secret protection)")
		case "tier":
			suggestions = append(suggestions, "set tier: 1 on runner profile, or reduce task difficulty")
		case "data-collection":
			suggestions = append(suggestions, "remove data_collection: true from runner profile, or remove repo from private_repos")
		}
	}
	return suggestions
}

// filterDataCollectionRunners removes runners marked with data_collection: true
// from the cascade when the task targets a private repo. This is a structural
// safeguard: private code must never be sent to providers that use data for training.
func filterDataCollectionRunners(
	cascade []string,
	repo string,
	profiles map[string]*task.RunnerProfileConfig,
	privateRepos map[string]struct{},
	fl *filterLog,
) []string {
	if len(privateRepos) == 0 {
		return cascade
	}
	if _, isPrivate := privateRepos[repo]; !isPrivate {
		return cascade
	}

	var removed []string
	filtered := make([]string, 0, len(cascade))
	for _, name := range cascade {
		p, ok := profiles[name]
		if ok && p.DataCollection {
			slog.Warn("skipping data-collecting runner for private repo",
				"runner", name, "repo", repo)
			removed = append(removed, name)
			continue
		}
		filtered = append(filtered, name)
	}
	fl.add("data-collection", removed, "data-collecting runner on private repo")
	return filtered
}

// filterGraylistedRunners removes graylisted runners from fallback positions
// in the cascade. The primary runner (index 0) is never filtered — explicit
// task.Runner assignment overrides the graylist. Uses profiles to check the
// specific model, so graylisting "deepseek:deepseek-chat" does not block
// "deepseek:deepseek-reasoner".
func filterGraylistedRunners(cascade []string, graylist *runner.RunnerGraylist, profiles map[string]*task.RunnerProfileConfig, fl *filterLog) []string {
	if graylist == nil || len(cascade) <= 1 {
		return cascade
	}
	var removed []string
	filtered := []string{cascade[0]}
	for _, name := range cascade[1:] {
		model := ""
		if p, ok := profiles[name]; ok {
			model = p.Model
		}
		if graylist.IsGraylisted(name, model) {
			slog.Debug("runner graylisted, removing from fallbacks", "runner", name, "model", model)
			removed = append(removed, name)
			continue
		}
		filtered = append(filtered, name)
	}
	fl.add("graylist", removed, "false positive detected")
	return filtered
}

// filterFreeRunners removes free-tier runners from fallback positions in the
// cascade. The primary runner (index 0) is never filtered — explicit task.Runner
// assignment overrides the free filter. When allowFree is true, all runners pass.
func filterFreeRunners(cascade []string, allowFree bool, profiles map[string]*task.RunnerProfileConfig, fl *filterLog) []string {
	if allowFree || len(cascade) <= 1 {
		return cascade
	}
	var removed []string
	filtered := []string{cascade[0]}
	for _, name := range cascade[1:] {
		if p, ok := profiles[name]; ok && p.Free {
			slog.Debug("free-tier runner excluded from fallbacks", "runner", name)
			removed = append(removed, name)
			continue
		}
		filtered = append(filtered, name)
	}
	fl.add("free", removed, "free-tier excluded by default")
	return filtered
}

// filterSecretAwareRunners removes unsafe runners from fallback positions when
// the task's repo has secrets. The primary runner (index 0) is never filtered —
// explicit task.Runner assignment overrides the secret filter.
func filterSecretAwareRunners(cascade []string, repo string, secretRepos map[string]struct{}, fl *filterLog) []string {
	if len(secretRepos) == 0 || len(cascade) <= 1 {
		return cascade
	}
	if _, hasSecrets := secretRepos[repo]; !hasSecrets {
		return cascade
	}
	var removed []string
	filtered := []string{cascade[0]}
	for _, name := range cascade[1:] {
		if !runner.IsSafeRunner(name) {
			slog.Warn("removing unsafe runner for repo with secrets", "runner", name, "repo", repo)
			removed = append(removed, name)
			continue
		}
		filtered = append(filtered, name)
	}
	fl.add("secret", removed, "unsafe for repos with secrets")
	return filtered
}

// filterByTier removes runners from fallback positions that lack the capability
// tier required by the task's difficulty. The primary runner (index 0) is never
// filtered — explicit task.Runner assignment overrides tier. When difficulty is
// empty, no filtering is applied.
func filterByTier(cascade []string, difficulty string, profiles map[string]*task.RunnerProfileConfig, fl *filterLog) []string {
	if difficulty == "" || len(cascade) <= 1 {
		return cascade
	}
	minTier := task.MinTier(difficulty)
	var removed []string
	filtered := []string{cascade[0]}
	for _, name := range cascade[1:] {
		tier := resolveTier(name, profiles)
		if tier > minTier {
			slog.Debug("runner below required tier, removing from fallbacks",
				"runner", name, "tier", tier, "required", minTier, "difficulty", difficulty)
			removed = append(removed, name)
			continue
		}
		filtered = append(filtered, name)
	}
	fl.add("tier", removed, fmt.Sprintf("below required tier %d for %s tasks", minTier, difficulty))
	return filtered
}

// resolveTier determines the effective tier for a runner. Uses profile.Tier if
// set, otherwise falls back to DefaultTier(profile.Type), then DefaultTier(name).
func resolveTier(name string, profiles map[string]*task.RunnerProfileConfig) int {
	if p, ok := profiles[name]; ok {
		if p.Tier > 0 {
			return p.Tier
		}
		return task.DefaultTier(p.Type)
	}
	return task.DefaultTier(name)
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

// isFalsePositive checks whether a completed task actually produced work.
// Returns true if events.jsonl is missing or contains 0 non-empty lines.
// This is a fast, synchronous check called immediately after task completion.
// isFalsePositive checks whether a completed task actually did real work.
// Primary signal: git commits (HEAD moved). Fallback: events.jsonl non-empty.
// Returns true if neither signal indicates real work was done.
func isFalsePositive(outputDir, repoDir, headBefore string) bool {
	// primary check: did the agent create new commits?
	if headBefore != "" {
		headAfter := gitHead(repoDir)
		if headAfter != "" && headAfter != headBefore {
			return false // HEAD moved = real commits = real work
		}
	}

	// fallback: check events.jsonl for any output
	return !hasEvents(outputDir)
}

// gitHead returns the current HEAD SHA for a repo, or empty on error.
func gitHead(repoDir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// hasEvents checks whether the output directory contains a non-empty events.jsonl.
func hasEvents(outputDir string) bool {
	eventsPath := filepath.Join(outputDir, "events.jsonl")
	if _, err := os.Stat(eventsPath); err != nil {
		// check attempt subdirectories
		entries, _ := os.ReadDir(outputDir)
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "attempt-") {
				candidate := filepath.Join(outputDir, e.Name(), "events.jsonl")
				if _, err := os.Stat(candidate); err == nil {
					eventsPath = candidate
					break
				}
			}
		}
	}

	f, err := os.Open(eventsPath)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			return true
		}
	}
	return false
}
