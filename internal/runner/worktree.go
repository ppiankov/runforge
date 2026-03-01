package runner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const worktreeDir = ".runforge-worktrees"

// CreateWorktree creates a git worktree for isolated task execution.
// Returns the worktree directory path and branch name. The worktree is
// created from HEAD of the main repo with a deterministic branch name.
func CreateWorktree(ctx context.Context, repoDir, reposDir, taskID string) (wtDir, branch string, err error) {
	wtDir = worktreePath(reposDir, taskID)
	branch = branchName(taskID)

	// ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(wtDir), 0o755); err != nil {
		return "", "", fmt.Errorf("create worktree parent: %w", err)
	}

	// clean up stale branch if it exists (from a previous crashed run)
	deleteBranchIfExists(ctx, repoDir, branch)

	// clean up stale worktree directory if it exists
	if _, err := os.Stat(wtDir); err == nil {
		slog.Warn("removing stale worktree directory", "path", wtDir)
		removeWorktreeForce(ctx, repoDir, wtDir)
		// also try plain remove if git worktree remove failed
		_ = os.RemoveAll(wtDir)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "git", "worktree", "add", wtDir, "-b", branch, "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}

	slog.Debug("created worktree", "task", taskID, "path", wtDir, "branch", branch)
	return wtDir, branch, nil
}

// RemoveWorktree removes a git worktree and its directory. Idempotent.
func RemoveWorktree(ctx context.Context, repoDir, wtDir string) {
	if wtDir == "" || wtDir == repoDir {
		return
	}

	removeWorktreeForce(ctx, repoDir, wtDir)

	// ensure directory is gone even if git worktree remove failed
	if _, err := os.Stat(wtDir); err == nil {
		_ = os.RemoveAll(wtDir)
	}

	// prune stale worktree entries
	pruneCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(pruneCtx, "git", "worktree", "prune")
	cmd.Dir = repoDir
	_ = cmd.Run()
}

// MergeBack merges a worktree branch into the current branch using
// fast-forward only. Returns an error if the merge requires a real merge
// commit (conflict or diverged history).
func MergeBack(ctx context.Context, repoDir, branch string) error {
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "git", "merge", "--ff-only", branch)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("merge --ff-only %s: %s: %w", branch, strings.TrimSpace(string(out)), err)
	}

	slog.Debug("merged worktree branch", "branch", branch, "repo", repoDir)
	return nil
}

// DeleteBranch removes a git branch. Called after successful merge.
func DeleteBranch(ctx context.Context, repoDir, branch string) {
	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "git", "branch", "-D", branch)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Warn("failed to delete worktree branch", "branch", branch, "error", strings.TrimSpace(string(out)))
	}
}

// worktreePath returns the filesystem path for a task's worktree.
func worktreePath(reposDir, taskID string) string {
	return filepath.Join(reposDir, worktreeDir, taskID)
}

// branchName returns the git branch name for a task's worktree.
func branchName(taskID string) string {
	return "runforge/" + taskID
}

// removeWorktreeForce runs git worktree remove --force, ignoring errors.
func removeWorktreeForce(ctx context.Context, repoDir, wtDir string) {
	cmdCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "git", "worktree", "remove", "--force", wtDir)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Debug("git worktree remove failed (may be already gone)",
			"path", wtDir, "error", strings.TrimSpace(string(out)))
	}
}

// deleteBranchIfExists removes a branch if it exists, ignoring errors.
func deleteBranchIfExists(ctx context.Context, repoDir, branch string) {
	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// check if branch exists
	cmd := exec.CommandContext(cmdCtx, "git", "rev-parse", "--verify", branch)
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		return // branch doesn't exist
	}

	// branch exists — delete it
	slog.Warn("removing stale worktree branch", "branch", branch)
	delCtx, delCancel := context.WithTimeout(ctx, 10*time.Second)
	defer delCancel()
	del := exec.CommandContext(delCtx, "git", "branch", "-D", branch)
	del.Dir = repoDir
	_ = del.Run()
}
