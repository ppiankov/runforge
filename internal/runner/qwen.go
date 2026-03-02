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

// qwenEvent represents a single event from Qwen Code's stream-json output.
type qwenEvent struct {
	Type    string       `json:"type"`     // "system", "assistant", "result"
	Subtype string       `json:"subtype"`  // "init", "success", etc.
	IsError bool         `json:"is_error"` // true on failure (result events)
	Result  string       `json:"result"`   // result text (result events)
	Message *qwenMessage `json:"message"`  // assistant message payload
	Usage   *eventUsage  `json:"usage,omitempty"`
}

type qwenMessage struct {
	Role    string        `json:"role"`
	Content []qwenContent `json:"content"`
}

type qwenContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// QwenRunner spawns Qwen Code CLI processes and parses their stream-json output.
type QwenRunner struct {
	model       string        // model override (--model flag)
	env         []string      // additional env vars for the subprocess
	idleTimeout time.Duration // kill task after this duration with no stdout events
}

// NewQwenRunner creates a new QwenRunner.
func NewQwenRunner(idleTimeout time.Duration) *QwenRunner {
	return &QwenRunner{idleTimeout: idleTimeout}
}

// NewQwenRunnerWithProfile creates a QwenRunner with model and env overrides.
func NewQwenRunnerWithProfile(model string, env map[string]string, idleTimeout time.Duration) *QwenRunner {
	return &QwenRunner{model: model, env: MapToEnvSlice(env), idleTimeout: idleTimeout}
}

// Name returns the runner identifier.
func (r *QwenRunner) Name() string { return "qwen" }

// Run executes a Qwen Code task and returns the result.
func (r *QwenRunner) Run(ctx context.Context, t *task.Task, repoDir, outputDir string) *task.TaskResult {
	start := time.Now()

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return failedResult(t.ID, start, fmt.Sprintf("create output dir: %v", err))
	}

	args := []string{
		"-p", t.Prompt,
		"--yolo",
		"--output-format", "stream-json",
	}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}

	slog.Debug("spawning qwen", "task", t.ID, "repo", t.Repo, "dir", repoDir, "model", r.model)

	// idle-aware context: kills the process if no stdout events for idleTimeout
	idleCtx, idleCancel := context.WithCancel(ctx)
	defer idleCancel()

	cmd := exec.CommandContext(idleCtx, "qwen", args...)
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
		return failedResult(t.ID, start, fmt.Sprintf("start qwen: %v", err))
	}

	// wrap stdout with idle detection — resets on every JSONL event
	idleReader := newIdleTimeoutReader(stdout, r.idleTimeout, idleCancel)
	defer idleReader.Stop()

	failed, lastMsg, tokens := parseQwenEvents(idleReader, outputDir)

	exitErr := cmd.Wait()
	end := time.Now()

	result := &task.TaskResult{
		TaskID:     t.ID,
		StartedAt:  start,
		EndedAt:    end,
		Duration:   end.Sub(start),
		OutputDir:  outputDir,
		LastMsg:    lastMsg,
		TokensUsed: tokens,
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
		result.Error = "qwen result: error"
	} else if exitErr != nil {
		slog.Warn("qwen exited with error but no failure detected",
			"task", t.ID, "error", exitErr)
		result.State = task.StateCompleted
	} else {
		result.State = task.StateCompleted
	}

	return result
}

// parseQwenEvents reads NDJSON from Qwen Code stdout and detects failures.
// Returns (failed bool, lastMessage string, usage *task.TokenUsage).
func parseQwenEvents(r io.Reader, outputDir string) (bool, string, *task.TokenUsage) {
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
	var eventCount int
	var usage *task.TokenUsage

	for scanner.Scan() {
		line := scanner.Bytes()

		// persist raw events
		if eventsFile != nil {
			_, _ = eventsFile.Write(line)
			_, _ = eventsFile.Write([]byte("\n"))
		}

		eventCount++

		var ev qwenEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			slog.Debug("unparseable jsonl line", "error", err)
			continue
		}

		if ev.Usage != nil {
			usage = addUsage(usage, ev.Usage.InputTokens, ev.Usage.OutputTokens, ev.Usage.TotalTokens)
		}

		switch ev.Type {
		case "result":
			if ev.IsError || ev.Subtype != "success" {
				failed = true
				slog.Debug("qwen result: error", "subtype", ev.Subtype, "is_error", ev.IsError)
			} else {
				slog.Debug("qwen result: success")
			}
		case "assistant":
			if ev.Message != nil && ev.Message.Role == "assistant" {
				for _, c := range ev.Message.Content {
					if c.Type == "text" && c.Text != "" {
						lastMsg = c.Text
					}
				}
			}
		}
	}

	// no events at all means qwen produced no output (likely argument error)
	if eventCount == 0 {
		failed = true
	}

	return failed, lastMsg, usage
}
