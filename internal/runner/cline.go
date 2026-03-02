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

// clineEvent represents a single event from Cline CLI's JSON output.
type clineEvent struct {
	Type string `json:"type"` // "say" or "ask"
	Say  string `json:"say"`  // subtype: "text", "reasoning", "api_req_started", etc.
	Ask  string `json:"ask"`  // subtype: "plan_mode_respond", etc.
	Text string `json:"text"` // message content
	TS   int64  `json:"ts"`   // timestamp ms
}

// ClineRunner spawns Cline CLI processes and parses their JSON output.
type ClineRunner struct {
	env         []string      // additional env vars for the subprocess
	idleTimeout time.Duration // kill task after this duration with no stdout events
}

// NewClineRunner creates a new ClineRunner.
func NewClineRunner(idleTimeout time.Duration) *ClineRunner {
	return &ClineRunner{idleTimeout: idleTimeout}
}

// NewClineRunnerWithProfile creates a ClineRunner with env overrides.
// Cline does not support per-invocation model selection — model is
// configured via `cline auth`.
func NewClineRunnerWithProfile(env map[string]string, idleTimeout time.Duration) *ClineRunner {
	return &ClineRunner{env: MapToEnvSlice(env), idleTimeout: idleTimeout}
}

// Name returns the runner identifier.
func (r *ClineRunner) Name() string { return "cline" }

// Run executes a Cline CLI task and returns the result.
func (r *ClineRunner) Run(ctx context.Context, t *task.Task, repoDir, outputDir string) *task.TaskResult {
	start := time.Now()

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return failedResult(t.ID, start, fmt.Sprintf("create output dir: %v", err))
	}

	args := []string{
		"-y",        // yolo mode: auto-approve all actions
		"-o",        // oneshot: full autonomous mode
		"-m", "act", // action mode (not plan)
		"-F", "json", // JSON output format
		t.Prompt,
	}

	slog.Debug("spawning cline", "task", t.ID, "repo", t.Repo, "dir", repoDir)

	// idle-aware context: kills the process if no stdout events for idleTimeout
	idleCtx, idleCancel := context.WithCancel(ctx)
	defer idleCancel()

	cmd := exec.CommandContext(idleCtx, "cline", args...)
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
		return failedResult(t.ID, start, fmt.Sprintf("start cline: %v", err))
	}

	// wrap stdout with idle detection — resets on every JSONL event
	idleReader := newIdleTimeoutReader(stdout, r.idleTimeout, idleCancel)
	defer idleReader.Stop()

	_, lastMsg := parseClineEvents(idleReader, outputDir)

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
	} else if exitErr != nil {
		result.State = task.StateFailed
		result.Error = fmt.Sprintf("cline exit: %v", exitErr)
	} else {
		result.State = task.StateCompleted
	}

	return result
}

// parseClineEvents reads NDJSON from Cline CLI stdout and extracts the last message.
// Cline uses exit codes for success/failure, so failed is always false here.
// Returns (failed bool, lastMessage string).
func parseClineEvents(r io.Reader, outputDir string) (bool, string) {
	eventsFile, _ := os.Create(filepath.Join(outputDir, "events.jsonl"))
	defer func() {
		if eventsFile != nil {
			_ = eventsFile.Close()
		}
	}()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	var lastMsg string

	for scanner.Scan() {
		line := scanner.Bytes()

		// persist raw events
		if eventsFile != nil {
			_, _ = eventsFile.Write(line)
			_, _ = eventsFile.Write([]byte("\n"))
		}

		var ev clineEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			slog.Debug("unparseable jsonl line", "error", err)
			continue
		}

		// extract last assistant text message
		if ev.Type == "say" && ev.Say == "text" && ev.Text != "" {
			lastMsg = ev.Text
		}
	}

	return false, lastMsg
}
