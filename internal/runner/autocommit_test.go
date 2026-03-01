package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ppiankov/runforge/internal/task"
)

// initGitRepo creates a temp git repo with an initial commit.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %s", args, out)
		}
	}
	runGit("init")
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644)
	runGit("add", ".")
	runGit("commit", "-m", "initial")
	return dir
}

func TestAutoCommit_CleanRepo(t *testing.T) {
	dir := initGitRepo(t)
	tk := &task.Task{ID: "t1", Title: "Fix something"}

	committed, err := AutoCommit(context.Background(), dir, tk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if committed {
		t.Fatal("clean repo should not auto-commit")
	}
}

func TestAutoCommit_UncommittedChanges(t *testing.T) {
	dir := initGitRepo(t)
	// modify existing file
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)

	tk := &task.Task{ID: "t1", Title: "Fix lint warnings"}

	committed, err := AutoCommit(context.Background(), dir, tk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !committed {
		t.Fatal("should auto-commit modified file")
	}

	// verify the commit exists
	cmd := exec.Command("git", "log", "--oneline", "-1")
	cmd.Dir = dir
	out, _ := cmd.Output()
	if !strings.Contains(string(out), "fix: lint warnings") {
		t.Fatalf("expected conventional commit message, got: %s", out)
	}
}

func TestAutoCommit_NewFile(t *testing.T) {
	dir := initGitRepo(t)
	// create new untracked file
	_ = os.WriteFile(filepath.Join(dir, "new_file.go"), []byte("package main\n"), 0o644)

	tk := &task.Task{ID: "t1", Title: "Add helper utility"}

	committed, err := AutoCommit(context.Background(), dir, tk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !committed {
		t.Fatal("should auto-commit new file")
	}

	// verify new file was committed
	cmd := exec.Command("git", "log", "--oneline", "-1")
	cmd.Dir = dir
	out, _ := cmd.Output()
	if !strings.Contains(string(out), "feat: helper utility") {
		t.Fatalf("expected 'feat:' commit message, got: %s", out)
	}
}

func TestAutoCommit_AgentAlreadyCommitted(t *testing.T) {
	dir := initGitRepo(t)
	// modify and commit (simulating agent already committed)
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = dir
	_ = cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "agent commit")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	_ = cmd.Run()

	tk := &task.Task{ID: "t1", Title: "Fix something"}

	committed, err := AutoCommit(context.Background(), dir, tk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if committed {
		t.Fatal("should not auto-commit when agent already committed")
	}
}

func TestDeriveCommitMessage(t *testing.T) {
	tests := []struct {
		title    string
		expected string
	}{
		{"Fix lint warnings", "fix: lint warnings"},
		{"Resolve nil pointer in handler", "fix: nil pointer in handler"},
		{"Repair broken tests", "fix: broken tests"},
		{"Add user authentication", "feat: user authentication"},
		{"Create migration script", "feat: migration script"},
		{"Implement retry logic", "feat: retry logic"},
		{"Document API endpoints", "docs: API endpoints"},
		{"README updates", "docs: updates"},
		{"Refactor database layer", "refactor: database layer"},
		{"Clean up unused imports", "refactor: up unused imports"},
		{"Simplify error handling", "refactor: error handling"},
		{"Test coverage for auth", "test: coverage for auth"},
		{"Add test for payment flow", "feat: test for payment flow"},
		{"Update dependencies", "chore: update dependencies"},
		{"Bump version to 2.0", "chore: bump version to 2.0"},
		{"", "chore: t1"}, // fallback to task ID
	}

	for _, tt := range tests {
		tk := &task.Task{ID: "t1", Title: tt.title}
		got := DeriveCommitMessage(tk)
		if got != tt.expected {
			t.Errorf("DeriveCommitMessage(%q) = %q, want %q", tt.title, got, tt.expected)
		}
	}
}

func TestDeriveCommitMessage_Truncation(t *testing.T) {
	long := "Fix " + strings.Repeat("a", 100)
	tk := &task.Task{ID: "t1", Title: long}
	got := DeriveCommitMessage(tk)
	if len(got) > 72 {
		t.Errorf("commit message too long: %d chars, want <= 72", len(got))
	}
}
