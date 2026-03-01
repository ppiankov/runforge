package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	_ = os.WriteFile(filepath.Join(dir, "file.txt"), []byte("initial"), 0o644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %s", args, out)
	}
	return strings.TrimSpace(string(out))
}

func TestCreateWorktree(t *testing.T) {
	repoDir := initTestRepo(t)
	reposDir := t.TempDir()

	ctx := context.Background()
	wtDir, branch, err := CreateWorktree(ctx, repoDir, reposDir, "task-1")
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}
	defer RemoveWorktree(ctx, repoDir, wtDir)

	// verify worktree directory exists
	if _, err := os.Stat(wtDir); err != nil {
		t.Fatalf("worktree directory not found: %v", err)
	}

	// verify branch name
	if branch != "runforge/task-1" {
		t.Errorf("expected branch runforge/task-1, got %s", branch)
	}

	// verify worktree path
	expected := filepath.Join(reposDir, worktreeDir, "task-1")
	if wtDir != expected {
		t.Errorf("expected worktree at %s, got %s", expected, wtDir)
	}

	// verify we're on the right branch in worktree
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = wtDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse in worktree: %v", err)
	}
	if strings.TrimSpace(string(out)) != "runforge/task-1" {
		t.Errorf("worktree on wrong branch: %s", out)
	}
}

func TestCreateWorktree_BranchExists(t *testing.T) {
	repoDir := initTestRepo(t)
	reposDir := t.TempDir()

	// create a stale branch
	runGit(t, repoDir, "branch", "runforge/task-stale")

	ctx := context.Background()
	wtDir, _, err := CreateWorktree(ctx, repoDir, reposDir, "task-stale")
	if err != nil {
		t.Fatalf("CreateWorktree should handle stale branch: %v", err)
	}
	defer RemoveWorktree(ctx, repoDir, wtDir)

	if _, err := os.Stat(wtDir); err != nil {
		t.Fatalf("worktree not created: %v", err)
	}
}

func TestRemoveWorktree(t *testing.T) {
	repoDir := initTestRepo(t)
	reposDir := t.TempDir()

	ctx := context.Background()
	wtDir, _, err := CreateWorktree(ctx, repoDir, reposDir, "task-rm")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	RemoveWorktree(ctx, repoDir, wtDir)

	if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
		t.Errorf("worktree directory should be removed, err: %v", err)
	}
}

func TestRemoveWorktree_Idempotent(t *testing.T) {
	repoDir := initTestRepo(t)

	// removing non-existent worktree should not panic
	RemoveWorktree(context.Background(), repoDir, "/nonexistent/path")
}

func TestRemoveWorktree_SafetyChecks(t *testing.T) {
	repoDir := initTestRepo(t)

	// should not remove repoDir itself
	RemoveWorktree(context.Background(), repoDir, repoDir)
	if _, err := os.Stat(repoDir); err != nil {
		t.Fatal("RemoveWorktree should not remove repoDir itself")
	}

	// should not remove empty path
	RemoveWorktree(context.Background(), repoDir, "")
}

func TestMergeBack_FastForward(t *testing.T) {
	repoDir := initTestRepo(t)
	reposDir := t.TempDir()

	ctx := context.Background()
	wtDir, branch, err := CreateWorktree(ctx, repoDir, reposDir, "task-merge")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	defer RemoveWorktree(ctx, repoDir, wtDir)

	// make a commit in the worktree
	_ = os.WriteFile(filepath.Join(wtDir, "new-file.txt"), []byte("task output"), 0o644)
	runGit(t, wtDir, "add", ".")
	runGit(t, wtDir, "commit", "-m", "task work")

	// remove worktree before merge (worktree must be removed before merging its branch)
	RemoveWorktree(ctx, repoDir, wtDir)

	// merge back
	if err := MergeBack(ctx, repoDir, branch); err != nil {
		t.Fatalf("MergeBack failed: %v", err)
	}

	// verify file exists in main repo
	if _, err := os.Stat(filepath.Join(repoDir, "new-file.txt")); err != nil {
		t.Fatal("merged file not found in main repo")
	}
}

func TestMergeBack_Conflict(t *testing.T) {
	repoDir := initTestRepo(t)
	reposDir := t.TempDir()

	ctx := context.Background()
	wtDir, branch, err := CreateWorktree(ctx, repoDir, reposDir, "task-conflict")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	// make a commit in the worktree
	_ = os.WriteFile(filepath.Join(wtDir, "file.txt"), []byte("worktree change"), 0o644)
	runGit(t, wtDir, "add", ".")
	runGit(t, wtDir, "commit", "-m", "worktree work")

	// make a different commit in main repo (creates divergence)
	_ = os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("main change"), 0o644)
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "main work")

	// remove worktree before merge
	RemoveWorktree(ctx, repoDir, wtDir)

	// merge should fail (not fast-forward)
	err = MergeBack(ctx, repoDir, branch)
	if err == nil {
		t.Fatal("expected merge conflict, got nil")
	}
}

func TestDeleteBranch(t *testing.T) {
	repoDir := initTestRepo(t)

	// create a branch
	runGit(t, repoDir, "branch", "runforge/delete-me")

	// verify it exists
	cmd := exec.Command("git", "rev-parse", "--verify", "runforge/delete-me")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatal("branch should exist before delete")
	}

	DeleteBranch(context.Background(), repoDir, "runforge/delete-me")

	// verify it's gone
	cmd = exec.Command("git", "rev-parse", "--verify", "runforge/delete-me")
	cmd.Dir = repoDir
	if err := cmd.Run(); err == nil {
		t.Fatal("branch should not exist after delete")
	}
}

func TestBranchName(t *testing.T) {
	if got := branchName("my-app-WO01"); got != "runforge/my-app-WO01" {
		t.Errorf("expected runforge/my-app-WO01, got %s", got)
	}
}

func TestWorktreePath(t *testing.T) {
	got := worktreePath("/repos", "task-1")
	expected := filepath.Join("/repos", worktreeDir, "task-1")
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}

func TestWorktreeFallback_NonGitDir(t *testing.T) {
	// non-git directory should fail worktree creation
	dir := t.TempDir()
	reposDir := t.TempDir()

	_, _, err := CreateWorktree(context.Background(), dir, reposDir, "task-nogit")
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
}
