package runner

import (
	"context"

	"github.com/ppiankov/runforge/internal/task"
)

// Runner executes a task and returns its result.
// Implementations: CodexRunner (codex.go). Future: claude, script.
type Runner interface {
	Name() string
	Run(ctx context.Context, t *task.Task, repoDir, outputDir string) *task.TaskResult
}
