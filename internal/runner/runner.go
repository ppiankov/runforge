package runner

import (
	"context"

	"github.com/ppiankov/runforge/internal/task"
)

// Runner executes a task and returns its result.
// Implementations: CodexRunner, ClaudeRunner, GeminiRunner, OpencodeRunner, ScriptRunner.
type Runner interface {
	Name() string
	Run(ctx context.Context, t *task.Task, repoDir, outputDir string) *task.TaskResult
}
