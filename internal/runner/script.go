package runner

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ppiankov/runforge/internal/task"
)

// ScriptRunner executes shell commands via sh -c.
type ScriptRunner struct {
	env []string // additional env vars for the subprocess
}

// NewScriptRunner creates a new ScriptRunner.
func NewScriptRunner() *ScriptRunner {
	return &ScriptRunner{}
}

// NewScriptRunnerWithEnv creates a ScriptRunner with custom environment variables.
func NewScriptRunnerWithEnv(env map[string]string) *ScriptRunner {
	return &ScriptRunner{env: MapToEnvSlice(env)}
}

// Name returns the runner identifier.
func (r *ScriptRunner) Name() string { return "script" }

// Run executes a shell command and returns the result.
func (r *ScriptRunner) Run(ctx context.Context, t *task.Task, repoDir, outputDir string) *task.TaskResult {
	start := time.Now()

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return failedResult(t.ID, start, fmt.Sprintf("create output dir: %v", err))
	}

	slog.Debug("spawning script", "task", t.ID, "repo", t.Repo, "dir", repoDir)

	cmd := exec.CommandContext(ctx, "sh", "-c", t.Prompt)
	cmd.Dir = repoDir
	if len(r.env) > 0 {
		cmd.Env = append(os.Environ(), r.env...)
	}
	cmd.Stdout = newLogWriter(outputDir, "output.log")
	cmd.Stderr = newLogWriter(outputDir, "stderr.log")

	err := cmd.Run()
	end := time.Now()

	// close log writers
	closeLogWriter(cmd.Stdout)
	closeLogWriter(cmd.Stderr)

	lastMsg := lastLine(filepath.Join(outputDir, "output.log"))

	result := &task.TaskResult{
		TaskID:    t.ID,
		StartedAt: start,
		EndedAt:   end,
		Duration:  end.Sub(start),
		OutputDir: outputDir,
		LastMsg:   lastMsg,
	}

	if err != nil {
		result.State = task.StateFailed
		result.Error = fmt.Sprintf("script exited: %v", err)
	} else {
		result.State = task.StateCompleted
	}

	return result
}

// lastLine reads the last non-empty line from a file.
func lastLine(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	var last string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if line := scanner.Text(); line != "" {
			last = line
		}
	}
	return last
}

// closeLogWriter closes the underlying file if the writer is an *os.File.
func closeLogWriter(w interface{}) {
	if f, ok := w.(*os.File); ok {
		_ = f.Close()
	}
}
