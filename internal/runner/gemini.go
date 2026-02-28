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

// geminiEvent represents a single event from Gemini CLI's stream-json output.
type geminiEvent struct {
	Type    string `json:"type"`
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
	Status  string `json:"status,omitempty"`
}

// GeminiRunner spawns Gemini CLI processes and parses their stream-json output.
type GeminiRunner struct {
	model       string        // model override (--model flag)
	env         []string      // additional env vars for the subprocess
	idleTimeout time.Duration // kill task after this duration with no stdout events
}

// NewGeminiRunner creates a new GeminiRunner.
func NewGeminiRunner(idleTimeout time.Duration) *GeminiRunner {
	return &GeminiRunner{idleTimeout: idleTimeout}
}

// NewGeminiRunnerWithProfile creates a GeminiRunner with model and env overrides.
func NewGeminiRunnerWithProfile(model string, env map[string]string, idleTimeout time.Duration) *GeminiRunner {
	return &GeminiRunner{model: model, env: MapToEnvSlice(env), idleTimeout: idleTimeout}
}

// Name returns the runner identifier.
func (r *GeminiRunner) Name() string { return "gemini" }

// Run executes a Gemini CLI task and returns the result.
func (r *GeminiRunner) Run(ctx context.Context, t *task.Task, repoDir, outputDir string) *task.TaskResult {
	start := time.Now()

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return failedResult(t.ID, start, fmt.Sprintf("create output dir: %v", err))
	}

	args := []string{
		"--approval-mode=yolo",
		"--output-format", "stream-json",
	}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}
	args = append(args, t.Prompt)

	slog.Debug("spawning gemini", "task", t.ID, "repo", t.Repo, "dir", repoDir, "model", r.model)

	// idle-aware context: kills the process if no stdout events for idleTimeout
	idleCtx, idleCancel := context.WithCancel(ctx)
	defer idleCancel()

	cmd := exec.CommandContext(idleCtx, "gemini", args...)
	setupProcessGroup(cmd)
	cmd.Dir = repoDir
	if len(r.env) > 0 {
		cmd.Env = append(SanitizedEnv(), r.env...)
	}
	rlw := newRateLimitWriter(newLogWriter(outputDir, "stderr.log"), idleCancel)
	hw := newHealthWriter(rlw, idleCancel)
	cmd.Stderr = hw

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return failedResult(t.ID, start, fmt.Sprintf("stdout pipe: %v", err))
	}

	if err := cmd.Start(); err != nil {
		return failedResult(t.ID, start, fmt.Sprintf("start gemini: %v", err))
	}

	// wrap stdout with idle detection — resets on every JSONL event
	idleReader := newIdleTimeoutReader(stdout, r.idleTimeout, idleCancel)
	defer idleReader.Stop()

	failed, lastMsg := parseGeminiEvents(idleReader, outputDir)

	exitErr := cmd.Wait()
	end := time.Now()

	result := &task.TaskResult{
		TaskID:    t.ID,
		StartedAt: start,
		EndedAt:   end,
		Duration:  end.Sub(start),
		OutputDir: outputDir,
		LastMsg:   lastMsg,
	}

	// idle timeout takes highest priority
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
		result.Error = "gemini result status: error"
	} else if exitErr != nil {
		slog.Warn("gemini exited with error but no failure detected",
			"task", t.ID, "error", exitErr)
		result.State = task.StateCompleted
	} else {
		result.State = task.StateCompleted
	}

	return result
}

// parseGeminiEvents reads NDJSON from Gemini CLI stdout and detects failures.
// Returns (failed bool, lastMessage string).
func parseGeminiEvents(r io.Reader, outputDir string) (bool, string) {
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

		var ev geminiEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			slog.Debug("unparseable jsonl line", "error", err)
			continue
		}

		switch ev.Type {
		case "result":
			if ev.Status != "success" {
				failed = true
				slog.Debug("gemini result: error", "status", ev.Status)
			} else {
				slog.Debug("gemini result: success")
			}
		case "message":
			if ev.Role == "assistant" && ev.Content != "" {
				lastMsg = ev.Content
			}
		}
	}

	return failed, lastMsg
}
