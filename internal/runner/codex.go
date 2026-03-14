package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ppiankov/tokencontrol/internal/task"
)

// CodexRunner spawns codex exec processes and parses their JSONL output.
type CodexRunner struct {
	model       string        // model override (--model flag)
	profile     string        // codex --profile name (references config.toml profiles)
	env         []string      // additional env vars for the subprocess
	idleTimeout time.Duration // kill task after this duration with no stdout events
}

// NewCodexRunner creates a new CodexRunner.
func NewCodexRunner(idleTimeout time.Duration) *CodexRunner {
	return &CodexRunner{idleTimeout: idleTimeout}
}

// NewCodexRunnerWithProfile creates a CodexRunner with model, profile, and env overrides.
func NewCodexRunnerWithProfile(model, profile string, env map[string]string, idleTimeout time.Duration) *CodexRunner {
	return &CodexRunner{model: model, profile: profile, env: MapToEnvSlice(env), idleTimeout: idleTimeout}
}

// Name returns the runner identifier.
func (r *CodexRunner) Name() string { return "codex" }

// Run executes a codex task and returns the result.
func (r *CodexRunner) Run(ctx context.Context, t *task.Task, repoDir, outputDir string) *task.TaskResult {
	start := time.Now()

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return failedResult(t.ID, start, fmt.Sprintf("create output dir: %v", err))
	}
	codexHome, err := prepareCodexHome(outputDir)
	if err != nil {
		return failedResult(t.ID, start, fmt.Sprintf("prepare codex home: %v", err))
	}

	outputFile := filepath.Join(outputDir, "output.md")

	args := []string{
		"exec",
		"--full-auto",
		"--json",
		"--output-last-message", outputFile,
		"-C", repoDir,
	}
	if r.profile != "" {
		args = append(args, "--profile", r.profile)
	}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}
	args = append(args, t.Prompt)

	slog.Debug("spawning codex", "task", t.ID, "repo", t.Repo, "dir", repoDir, "model", r.model, "profile", r.profile)

	// idle-aware context: kills the process if no stdout events for idleTimeout
	idleCtx, idleCancel := context.WithCancel(ctx)
	defer idleCancel()

	cmd := exec.CommandContext(idleCtx, "codex", args...)
	setupProcessGroup(cmd)
	cmd.Dir = repoDir
	cmd.Env = append(SanitizedEnv(), r.env...)
	cmd.Env = appendOrReplaceEnv(cmd.Env, "CODEX_HOME", codexHome)
	rlw := newRateLimitWriter(newLogWriter(outputDir, "stderr.log"), idleCancel)
	hw := newHealthWriter(rlw, idleCancel)
	cmd.Stderr = hw

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return failedResult(t.ID, start, fmt.Sprintf("stdout pipe: %v", err))
	}

	if err := cmd.Start(); err != nil {
		return failedResult(t.ID, start, fmt.Sprintf("start codex: %v", err))
	}

	// wrap stdout with idle detection — resets on every JSONL event
	idleReader := newIdleTimeoutReader(stdout, r.idleTimeout, idleCancel)
	defer idleReader.Stop()

	// parse JSONL events from stdout
	failed, lastMsg, tokens := parseEvents(idleReader, outputDir)

	exitErr := cmd.Wait()
	end := time.Now()

	// read output file if exists
	if data, err := os.ReadFile(outputFile); err == nil && lastMsg == "" {
		lastMsg = string(data)
	}

	result := &task.TaskResult{
		TaskID:     t.ID,
		StartedAt:  start,
		EndedAt:    end,
		Duration:   end.Sub(start),
		OutputDir:  outputDir,
		LastMsg:    lastMsg,
		TokensUsed: tokens,
	}

	// idle timeout takes highest priority — the process was killed due to inactivity
	if idleReader.Idled() {
		result.State = task.StateFailed
		result.Error = fmt.Sprintf("idle timeout: no output for %s", r.idleTimeout)
		return result
	}

	// connectivity error takes priority — blacklist the runner immediately
	if hw.Detected() {
		result.State = task.StateFailed
		result.ConnectivityError = hw.Reason()
		result.Error = hw.Reason()
		return result
	}

	// rate limit takes priority over other failure signals
	if rlw.Detected() {
		result.State = task.StateRateLimited
		result.ResetsAt = rlw.ResetsAt()
		if !result.ResetsAt.IsZero() {
			result.Error = fmt.Sprintf("rate limit reached, resets at %s", result.ResetsAt.Format(time.Kitchen))
		} else {
			result.Error = "rate limit reached"
		}
	} else if failed {
		result.State = task.StateFailed
		result.Error = "codex turn.failed event detected"
	} else if exitErr != nil {
		// exit code is unreliable — log but don't fail unless we also saw turn.failed
		slog.Warn("codex exited with error but no turn.failed detected",
			"task", t.ID, "error", exitErr)
		result.State = task.StateCompleted
	} else {
		result.State = task.StateCompleted
	}

	return result
}

// parseEvents reads JSONL from codex stdout and detects failures.
// Returns (failed bool, lastMessage string, usage *task.TokenUsage).
func parseEvents(r io.Reader, outputDir string) (bool, string, *task.TokenUsage) {
	eventsFile, _ := os.Create(filepath.Join(outputDir, "events.jsonl"))
	defer func() {
		if eventsFile != nil {
			_ = eventsFile.Close()
		}
	}()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	var failed bool
	var lastMsg string
	var usage *task.TokenUsage

	for scanner.Scan() {
		line := scanner.Bytes()

		// persist raw events
		if eventsFile != nil {
			_, _ = eventsFile.Write(line)
			_, _ = eventsFile.Write([]byte("\n"))
		}

		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			slog.Debug("unparseable jsonl line", "error", err)
			continue
		}

		if ev.Usage != nil {
			usage = addUsage(usage, ev.Usage.InputTokens, ev.Usage.OutputTokens, ev.Usage.TotalTokens)
		}

		switch ev.Type {
		case EventTurnFailed:
			failed = true
			slog.Debug("turn.failed detected")
		case EventItemCompleted:
			if ev.Item != nil && ev.Item.Type == "agent_message" && ev.Item.Content != "" {
				lastMsg = ev.Item.Content
			}
		case EventTurnCompleted:
			slog.Debug("turn.completed")
		}
	}

	return failed, lastMsg, usage
}

// ParseEventsFromFile reads a saved events.jsonl file and extracts results.
// Exported for testing and status inspection.
func ParseEventsFromFile(path string) (failed bool, lastMsg string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return false, "", err
	}
	defer func() { _ = f.Close() }()

	// use a temp dir for output to avoid overwriting the input file
	tmpDir, err := os.MkdirTemp("", "tokencontrol-parse-*")
	if err != nil {
		return false, "", fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	failed, lastMsg, _ = parseEvents(f, tmpDir)
	return failed, lastMsg, nil
}

func failedResult(id string, start time.Time, msg string) *task.TaskResult {
	now := time.Now()
	return &task.TaskResult{
		TaskID:    id,
		State:     task.StateFailed,
		StartedAt: start,
		EndedAt:   now,
		Duration:  now.Sub(start),
		Error:     msg,
	}
}

// newLogWriter creates a file writer for capturing stderr.
func newLogWriter(dir, name string) io.Writer {
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		slog.Warn("cannot create log file", "path", path, "error", err)
		return io.Discard
	}
	return f
}

// prepareCodexHome creates an isolated CODEX_HOME for a single task run.
// This avoids shared ~/.codex system-skill installation races when multiple
// Codex subprocesses start in parallel, while preserving auth/config and
// user-installed skills from the shared home.
func prepareCodexHome(outputDir string) (string, error) {
	sharedHome, err := sharedCodexHome()
	if err != nil {
		return "", err
	}

	isolatedHome := filepath.Join(outputDir, "codex-home")
	if err := os.MkdirAll(isolatedHome, 0o755); err != nil {
		return "", fmt.Errorf("mkdir isolated home: %w", err)
	}

	for _, name := range []string{"auth.json", "config.toml", "AGENTS.md"} {
		if err := symlinkIfExists(filepath.Join(sharedHome, name), filepath.Join(isolatedHome, name)); err != nil {
			return "", err
		}
	}
	if err := symlinkIfExists(filepath.Join(sharedHome, "vendor_imports"), filepath.Join(isolatedHome, "vendor_imports")); err != nil {
		return "", err
	}

	sharedSkills := filepath.Join(sharedHome, "skills")
	entries, err := os.ReadDir(sharedSkills)
	if err != nil {
		if os.IsNotExist(err) {
			return isolatedHome, nil
		}
		return "", fmt.Errorf("read shared skills: %w", err)
	}

	isolatedSkills := filepath.Join(isolatedHome, "skills")
	if err := os.MkdirAll(isolatedSkills, 0o755); err != nil {
		return "", fmt.Errorf("mkdir isolated skills: %w", err)
	}

	for _, entry := range entries {
		if entry.Name() == ".system" {
			continue
		}
		if err := symlinkIfExists(filepath.Join(sharedSkills, entry.Name()), filepath.Join(isolatedSkills, entry.Name())); err != nil {
			return "", err
		}
	}

	return isolatedHome, nil
}

func sharedCodexHome() (string, error) {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return home, nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(userHome, ".codex"), nil
}

func symlinkIfExists(target, link string) error {
	if _, err := os.Lstat(target); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", target, err)
	}
	if _, err := os.Lstat(link); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", link, err)
	}
	if err := os.Symlink(target, link); err != nil {
		return fmt.Errorf("symlink %s -> %s: %w", link, target, err)
	}
	return nil
}

func appendOrReplaceEnv(environ []string, key, value string) []string {
	prefix := key + "="
	entry := prefix + value
	for i := range environ {
		if len(environ[i]) >= len(prefix) && environ[i][:len(prefix)] == prefix {
			environ[i] = entry
			return environ
		}
	}
	return append(environ, entry)
}
