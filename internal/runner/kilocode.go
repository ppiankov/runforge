package runner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/ppiankov/tokencontrol/internal/task"
)

// KilocodeRunner spawns Kilo CLI processes (Kilocode fork of OpenCode).
// Kilo uses the same JSON event format as OpenCode v1.x.
type KilocodeRunner struct {
	model       string
	env         []string
	idleTimeout time.Duration
}

// NewKilocodeRunner creates a new KilocodeRunner.
func NewKilocodeRunner(idleTimeout time.Duration) *KilocodeRunner {
	return &KilocodeRunner{idleTimeout: idleTimeout}
}

// NewKilocodeRunnerWithProfile creates a KilocodeRunner with model and env overrides.
func NewKilocodeRunnerWithProfile(model string, env map[string]string, idleTimeout time.Duration) *KilocodeRunner {
	return &KilocodeRunner{model: model, env: MapToEnvSlice(env), idleTimeout: idleTimeout}
}

// Name returns the runner identifier.
func (r *KilocodeRunner) Name() string { return "kilocode" }

// Run executes a Kilo task and returns the result.
func (r *KilocodeRunner) Run(ctx context.Context, t *task.Task, repoDir, outputDir string) *task.TaskResult {
	start := time.Now()

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return failedResult(t.ID, start, fmt.Sprintf("create output dir: %v", err))
	}

	args := []string{
		"run",
		"--auto",
		"--format", "json",
		"--dir", repoDir,
	}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}
	args = append(args, t.Prompt)

	slog.Debug("spawning kilo", "task", t.ID, "repo", t.Repo, "dir", repoDir, "model", r.model)

	idleCtx, idleCancel := context.WithCancel(ctx)
	defer idleCancel()

	cmd := exec.CommandContext(idleCtx, "kilo", args...)
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
		return failedResult(t.ID, start, fmt.Sprintf("start kilo: %v", err))
	}

	idleReader := newIdleTimeoutReader(stdout, r.idleTimeout, idleCancel)
	defer idleReader.Stop()

	// reuse OpenCode event parser — same JSON format
	failed, eventCount, lastMsg, tokens := parseOpencodeEvents(idleReader, outputDir)

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

	if idleReader.Idled() {
		result.State = task.StateFailed
		result.Error = fmt.Sprintf("idle timeout: no output for %s", r.idleTimeout)
		return result
	}

	if hw.Detected() {
		result.State = task.StateFailed
		result.ConnectivityError = hw.Reason()
		result.Error = hw.Reason()
		return result
	}

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
		result.Error = "kilo reported error"
	} else if exitErr != nil {
		slog.Warn("kilo exited with error but no failure detected",
			"task", t.ID, "error", exitErr, "eventCount", eventCount)
		if eventCount > 0 {
			result.State = task.StateCompleted
		} else {
			result.State = task.StateFailed
			result.Error = fmt.Sprintf("kilo exit: %v", exitErr)
		}
	} else {
		result.State = task.StateCompleted
	}

	return result
}
