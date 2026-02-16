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

// claudeEvent represents a single event from Claude Code's stream-json output.
type claudeEvent struct {
	Type    string          `json:"type"`
	Role    string          `json:"role,omitempty"`
	Content []claudeContent `json:"content,omitempty"`
	Status  string          `json:"status,omitempty"`
}

type claudeContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ClaudeRunner spawns Claude Code CLI processes and parses their stream-json output.
type ClaudeRunner struct{}

// NewClaudeRunner creates a new ClaudeRunner.
func NewClaudeRunner() *ClaudeRunner {
	return &ClaudeRunner{}
}

// Name returns the runner identifier.
func (r *ClaudeRunner) Name() string { return "claude" }

// Run executes a Claude Code task and returns the result.
func (r *ClaudeRunner) Run(ctx context.Context, t *task.Task, repoDir, outputDir string) *task.TaskResult {
	start := time.Now()

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return failedResult(t.ID, start, fmt.Sprintf("create output dir: %v", err))
	}

	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--dangerously-skip-permissions",
		t.Prompt,
	}

	slog.Debug("spawning claude", "task", t.ID, "repo", t.Repo, "dir", repoDir)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = repoDir
	rlw := newRateLimitWriter(newLogWriter(outputDir, "stderr.log"))
	cmd.Stderr = rlw

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return failedResult(t.ID, start, fmt.Sprintf("stdout pipe: %v", err))
	}

	if err := cmd.Start(); err != nil {
		return failedResult(t.ID, start, fmt.Sprintf("start claude: %v", err))
	}

	failed, lastMsg := parseClaudeEvents(stdout, outputDir)

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
		result.Error = "claude result status: error"
	} else if exitErr != nil {
		slog.Warn("claude exited with error but no failure detected",
			"task", t.ID, "error", exitErr)
		result.State = task.StateCompleted
	} else {
		result.State = task.StateCompleted
	}

	return result
}

// parseClaudeEvents reads NDJSON from Claude Code stdout and detects failures.
// Returns (failed bool, lastMessage string).
func parseClaudeEvents(r io.Reader, outputDir string) (bool, string) {
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

		var ev claudeEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			slog.Debug("unparseable jsonl line", "error", err)
			continue
		}

		switch ev.Type {
		case "result":
			if ev.Status == "error" {
				failed = true
				slog.Debug("claude result: error")
			} else {
				slog.Debug("claude result: success")
			}
		case "message":
			if ev.Role == "assistant" {
				for _, c := range ev.Content {
					if c.Type == "text" && c.Text != "" {
						lastMsg = c.Text
					}
				}
			}
		}
	}

	return failed, lastMsg
}
