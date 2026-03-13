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

// gitEnv provides tokencontrol identity for git operations in CI environments.
var gitEnv = append(os.Environ(),
	"GIT_AUTHOR_NAME=tokencontrol",
	"GIT_AUTHOR_EMAIL=tokencontrol@localhost",
	"GIT_COMMITTER_NAME=tokencontrol",
	"GIT_COMMITTER_EMAIL=tokencontrol@localhost",
)

const worktreeDir = ".tokencontrol-worktrees"

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

// MergeBack merges a worktree branch into the current branch.
// Tries fast-forward first. If the target branch has moved ahead (sibling
// merges), rebases the branch onto the current HEAD and retries FF merge.
// Returns an error only for real content conflicts.
func MergeBack(ctx context.Context, repoDir, branch string) error {
	cmdCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// try fast-forward first (covers first merge or sequential case)
	cmd := exec.CommandContext(cmdCtx, "git", "merge", "--ff-only", branch)
	cmd.Dir = repoDir
	if _, err := cmd.CombinedOutput(); err == nil {
		slog.Debug("merged worktree branch (ff)", "branch", branch, "repo", repoDir)
		return nil
	}

	// FF failed — rebase branch onto current HEAD, then retry
	rebaseCmd := exec.CommandContext(cmdCtx, "git", "rebase", "HEAD", branch)
	rebaseCmd.Dir = repoDir
	rebaseCmd.Env = gitEnv
	if out, err := rebaseCmd.CombinedOutput(); err != nil {
		// real conflict — abort rebase and report
		abortCmd := exec.CommandContext(cmdCtx, "git", "rebase", "--abort")
		abortCmd.Dir = repoDir
		_ = abortCmd.Run()
		return fmt.Errorf("rebase %s: %s: %w", branch, strings.TrimSpace(string(out)), err)
	}

	// rebase succeeded — FF merge now guaranteed
	ffCmd := exec.CommandContext(cmdCtx, "git", "merge", "--ff-only", branch)
	ffCmd.Dir = repoDir
	if out, err := ffCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("merge --ff-only after rebase %s: %s: %w", branch, strings.TrimSpace(string(out)), err)
	}

	slog.Debug("merged worktree branch (rebase+ff)", "branch", branch, "repo", repoDir)
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
	return "tokencontrol/" + taskID
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

// ListConflictFiles returns file paths that differ between HEAD and the given branch.
func ListConflictFiles(ctx context.Context, repoDir, branch string) []string {
	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "git", "diff", "--name-only", "HEAD..."+branch)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files
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
