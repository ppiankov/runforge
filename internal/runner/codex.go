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

	"github.com/ppiankov/runforge/internal/task"
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
	cmd.Dir = repoDir
	if len(r.env) > 0 {
		cmd.Env = append(SanitizedEnv(), r.env...)
	}
	rlw := newRateLimitWriter(newLogWriter(outputDir, "stderr.log"))
	cmd.Stderr = rlw

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
	failed, lastMsg := parseEvents(idleReader, outputDir)

	exitErr := cmd.Wait()
	end := time.Now()

	// read output file if exists
	if data, err := os.ReadFile(outputFile); err == nil && lastMsg == "" {
		lastMsg = string(data)
	}

	result := &task.TaskResult{
		TaskID:    t.ID,
		StartedAt: start,
		EndedAt:   end,
		Duration:  end.Sub(start),
		OutputDir: outputDir,
		LastMsg:   lastMsg,
	}

	// idle timeout takes highest priority — the process was killed due to inactivity
	if idleReader.Idled() {
		result.State = task.StateFailed
		result.Error = fmt.Sprintf("idle timeout: no output for %s", r.idleTimeout)
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
// Returns (failed bool, lastMessage string).
func parseEvents(r io.Reader, outputDir string) (bool, string) {
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

	return failed, lastMsg
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
	tmpDir, err := os.MkdirTemp("", "runforge-parse-*")
	if err != nil {
		return false, "", fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	failed, lastMsg = parseEvents(f, tmpDir)
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
