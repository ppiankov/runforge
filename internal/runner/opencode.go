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

// opencodeEvent represents a single event from OpenCode's JSON output.
// OpenCode v1.x emits structured events with nested part objects:
//
//	{"type":"text","part":{"text":"hello"}}
//	{"type":"step_finish","part":{"reason":"stop"}}
//	{"type":"error","part":{"error":"something went wrong"}}
type opencodeEvent struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionID,omitempty"`
	Response  string          `json:"response,omitempty"`
	Error     json.RawMessage `json:"error,omitempty"`
	Part      opencodePart    `json:"part,omitempty"`
}

// opencodePart holds the nested payload within an OpenCode event.
type opencodePart struct {
	Type   string `json:"type,omitempty"`
	Text   string `json:"text,omitempty"`
	Reason string `json:"reason,omitempty"`
	Error  string `json:"error,omitempty"`
}

// OpencodeRunner spawns OpenCode CLI processes and parses their JSON output.
type OpencodeRunner struct {
	model       string        // model override (--model flag, format: provider/model)
	env         []string      // additional env vars for the subprocess
	idleTimeout time.Duration // kill task after this duration with no stdout events
}

// NewOpencodeRunner creates a new OpencodeRunner.
func NewOpencodeRunner(idleTimeout time.Duration) *OpencodeRunner {
	return &OpencodeRunner{idleTimeout: idleTimeout}
}

// NewOpencodeRunnerWithProfile creates an OpencodeRunner with model and env overrides.
func NewOpencodeRunnerWithProfile(model string, env map[string]string, idleTimeout time.Duration) *OpencodeRunner {
	return &OpencodeRunner{model: model, env: MapToEnvSlice(env), idleTimeout: idleTimeout}
}

// Name returns the runner identifier.
func (r *OpencodeRunner) Name() string { return "opencode" }

// Run executes an OpenCode task and returns the result.
func (r *OpencodeRunner) Run(ctx context.Context, t *task.Task, repoDir, outputDir string) *task.TaskResult {
	start := time.Now()

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return failedResult(t.ID, start, fmt.Sprintf("create output dir: %v", err))
	}

	args := []string{
		"run",
		"--format", "json",
		"--dir", repoDir,
	}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}
	args = append(args, t.Prompt)

	slog.Debug("spawning opencode", "task", t.ID, "repo", t.Repo, "dir", repoDir, "model", r.model)

	// idle-aware context: kills the process if no stdout events for idleTimeout
	idleCtx, idleCancel := context.WithCancel(ctx)
	defer idleCancel()

	cmd := exec.CommandContext(idleCtx, "opencode", args...)
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
		return failedResult(t.ID, start, fmt.Sprintf("start opencode: %v", err))
	}

	// wrap stdout with idle detection — resets on every line of output
	idleReader := newIdleTimeoutReader(stdout, r.idleTimeout, idleCancel)
	defer idleReader.Stop()

	failed, lastMsg := parseOpencodeEvents(idleReader, outputDir)

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
		result.Error = "opencode reported error"
	} else if exitErr != nil {
		slog.Warn("opencode exited with error but no failure detected",
			"task", t.ID, "error", exitErr)
		result.State = task.StateFailed
		result.Error = fmt.Sprintf("opencode exit: %v", exitErr)
	} else {
		result.State = task.StateCompleted
	}

	return result
}

// parseOpencodeEvents reads JSON from OpenCode stdout and detects failures.
// OpenCode v1.x outputs structured events with nested part objects:
//
//	{"type":"text","part":{"text":"..."}}
//	{"type":"step_finish","part":{"reason":"stop"}}
//	{"type":"error","part":{"error":"..."}}
//
// Also supports legacy format: {"type":"message","response":"..."} and
// single-object format: {"response":"..."}.
// Returns (failed bool, lastMessage string).
func parseOpencodeEvents(r io.Reader, outputDir string) (bool, string) {
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

		// persist raw output
		if eventsFile != nil {
			_, _ = eventsFile.Write(line)
			_, _ = eventsFile.Write([]byte("\n"))
		}

		var ev opencodeEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			slog.Debug("unparseable json line", "error", err)
			continue
		}

		switch ev.Type {
		case "error":
			failed = true
			if ev.Part.Error != "" {
				slog.Debug("opencode error event", "error", ev.Part.Error)
			} else {
				slog.Debug("opencode error event detected")
			}
		case "text":
			// v1.x format: text content in part.text
			if ev.Part.Text != "" {
				lastMsg = ev.Part.Text
			}
		case "step_finish":
			// check for error reason in step completion
			if ev.Part.Reason == "error" {
				failed = true
			}
		case "message":
			// legacy format: response at top level
			if ev.Response != "" {
				lastMsg = ev.Response
			}
		case "result":
			// legacy format: response at top level
			if ev.Response != "" {
				lastMsg = ev.Response
			}
		case "":
			// single-object response format: {"response":"..."}
			if ev.Response != "" {
				lastMsg = ev.Response
			}
		}
	}

	return failed, lastMsg
}
