package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/tokencontrol/internal/task"
)

// setupPRTest creates a temp dir structure with a report.json and task.json files.
func setupPRTest(t *testing.T, results map[string]*task.TaskResult, tasks map[string]*task.Task) (runDir, reposDir string) {
	t.Helper()

	base := t.TempDir()
	runDir = filepath.Join(base, ".tokencontrol", "run-2025-01-01")
	reposDir = base

	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write task.json files in each task's output dir.
	for id, tk := range tasks {
		res, ok := results[id]
		if !ok {
			continue
		}
		outDir := filepath.Join(runDir, id)
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatal(err)
		}
		res.OutputDir = outDir

		data, err := json.Marshal(tk)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(outDir, "task.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Write report.json.
	report := &task.RunReport{
		RunID:    "test-run",
		ReposDir: reposDir,
		Results:  results,
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "report.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	return runDir, reposDir
}

func fakePREnv(gitOut, ghOut string, gitErr, ghErr error) *prEnv {
	return &prEnv{
		runGit: func(_ context.Context, _ string, _ ...string) (string, error) {
			return gitOut, gitErr
		},
		runGH: func(_ context.Context, _ string, _ ...string) (string, error) {
			return ghOut, ghErr
		},
	}
}

func TestPR_HappyPath(t *testing.T) {
	results := map[string]*task.TaskResult{
		"task-1": {
			TaskID:         "task-1",
			State:          task.StateCompleted,
			WorktreeBranch: "tokencontrol/task-1",
			RunnerUsed:     "codex",
			Duration:       30 * time.Second,
		},
	}
	tasks := map[string]*task.Task{
		"task-1": {ID: "task-1", Title: "Fix auth flow", Repo: "ppiankov/myrepo"},
	}

	runDir, reposDir := setupPRTest(t, results, tasks)

	env := fakePREnv("", "https://github.com/ppiankov/myrepo/pull/42", nil, nil)
	var buf bytes.Buffer
	err := runPR(context.Background(), env, &buf, runDir, reposDir, "main", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Pushing tokencontrol/task-1") {
		t.Errorf("expected push message, got:\n%s", out)
	}
	if !strings.Contains(out, "https://github.com/ppiankov/myrepo/pull/42") {
		t.Errorf("expected PR URL, got:\n%s", out)
	}
	if !strings.Contains(out, "1 PRs created") {
		t.Errorf("expected 1 PR created in summary, got:\n%s", out)
	}
}

func TestPR_DryRun(t *testing.T) {
	results := map[string]*task.TaskResult{
		"task-1": {
			TaskID:         "task-1",
			State:          task.StateCompleted,
			WorktreeBranch: "tokencontrol/task-1",
			RunnerUsed:     "claude",
			Duration:       10 * time.Second,
		},
	}
	tasks := map[string]*task.Task{
		"task-1": {ID: "task-1", Title: "Add tests", Repo: "ppiankov/myrepo"},
	}

	runDir, reposDir := setupPRTest(t, results, tasks)

	// Env should never be called in dry-run.
	env := &prEnv{
		runGit: func(_ context.Context, _ string, _ ...string) (string, error) {
			t.Fatal("git should not be called in dry-run")
			return "", nil
		},
		runGH: func(_ context.Context, _ string, _ ...string) (string, error) {
			t.Fatal("gh should not be called in dry-run")
			return "", nil
		},
	}

	var buf bytes.Buffer
	err := runPR(context.Background(), env, &buf, runDir, reposDir, "main", true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[dry-run]") {
		t.Errorf("expected dry-run prefix, got:\n%s", out)
	}
	if !strings.Contains(out, "skipped (dry-run)") {
		t.Errorf("expected dry-run skip message, got:\n%s", out)
	}
}

func TestPR_SkipNoBranch(t *testing.T) {
	results := map[string]*task.TaskResult{
		"task-1": {
			TaskID:         "task-1",
			State:          task.StateCompleted,
			WorktreeBranch: "", // ran on main
		},
	}
	tasks := map[string]*task.Task{
		"task-1": {ID: "task-1", Title: "Fix bug", Repo: "ppiankov/myrepo"},
	}

	runDir, reposDir := setupPRTest(t, results, tasks)
	env := fakePREnv("", "", nil, nil)

	var buf bytes.Buffer
	err := runPR(context.Background(), env, &buf, runDir, reposDir, "main", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "1 skipped") {
		t.Errorf("expected 1 skipped in summary, got:\n%s", out)
	}
}

func TestPR_PushFailure(t *testing.T) {
	results := map[string]*task.TaskResult{
		"task-1": {
			TaskID:         "task-1",
			State:          task.StateCompleted,
			WorktreeBranch: "tokencontrol/task-1",
			RunnerUsed:     "codex",
			Duration:       5 * time.Second,
		},
	}
	tasks := map[string]*task.Task{
		"task-1": {ID: "task-1", Title: "Fix auth", Repo: "ppiankov/myrepo"},
	}

	runDir, reposDir := setupPRTest(t, results, tasks)
	env := fakePREnv("", "", fmt.Errorf("push failed: remote rejected"), nil)

	var buf bytes.Buffer
	err := runPR(context.Background(), env, &buf, runDir, reposDir, "main", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "failed") {
		t.Errorf("expected failed message, got:\n%s", out)
	}
	if !strings.Contains(out, "1 failed") {
		t.Errorf("expected 1 failed in summary, got:\n%s", out)
	}
}

func TestPR_AlreadyExists(t *testing.T) {
	results := map[string]*task.TaskResult{
		"task-1": {
			TaskID:         "task-1",
			State:          task.StateCompleted,
			WorktreeBranch: "tokencontrol/task-1",
			RunnerUsed:     "codex",
			Duration:       5 * time.Second,
		},
	}
	tasks := map[string]*task.Task{
		"task-1": {ID: "task-1", Title: "Fix auth", Repo: "ppiankov/myrepo"},
	}

	runDir, reposDir := setupPRTest(t, results, tasks)
	env := &prEnv{
		runGit: func(_ context.Context, _ string, _ ...string) (string, error) {
			return "", nil // push succeeds
		},
		runGH: func(_ context.Context, _ string, _ ...string) (string, error) {
			return "", fmt.Errorf("a pull request for branch already exists")
		},
	}

	var buf bytes.Buffer
	err := runPR(context.Background(), env, &buf, runDir, reposDir, "main", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "already exists") {
		t.Errorf("expected already exists message, got:\n%s", out)
	}
	if !strings.Contains(out, "1 skipped") {
		t.Errorf("expected 1 skipped in summary, got:\n%s", out)
	}
}

func TestPR_DraftFlag(t *testing.T) {
	results := map[string]*task.TaskResult{
		"task-1": {
			TaskID:         "task-1",
			State:          task.StateCompleted,
			WorktreeBranch: "tokencontrol/task-1",
			RunnerUsed:     "codex",
			Duration:       5 * time.Second,
		},
	}
	tasks := map[string]*task.Task{
		"task-1": {ID: "task-1", Title: "Add feature", Repo: "ppiankov/myrepo"},
	}

	runDir, reposDir := setupPRTest(t, results, tasks)

	var capturedArgs []string
	env := &prEnv{
		runGit: func(_ context.Context, _ string, _ ...string) (string, error) {
			return "", nil
		},
		runGH: func(_ context.Context, _ string, args ...string) (string, error) {
			capturedArgs = args
			return "https://github.com/ppiankov/myrepo/pull/1", nil
		},
	}

	var buf bytes.Buffer
	err := runPR(context.Background(), env, &buf, runDir, reposDir, "main", false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, a := range capturedArgs {
		if a == "--draft" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected --draft in gh args, got: %v", capturedArgs)
	}
}

func TestPR_SkipNonCompleted(t *testing.T) {
	results := map[string]*task.TaskResult{
		"task-1": {
			TaskID:         "task-1",
			State:          task.StateFailed,
			WorktreeBranch: "tokencontrol/task-1",
		},
		"task-2": {
			TaskID:         "task-2",
			State:          task.StateRunning,
			WorktreeBranch: "tokencontrol/task-2",
		},
	}
	tasks := map[string]*task.Task{
		"task-1": {ID: "task-1", Title: "Failed task", Repo: "ppiankov/myrepo"},
		"task-2": {ID: "task-2", Title: "Running task", Repo: "ppiankov/myrepo"},
	}

	runDir, reposDir := setupPRTest(t, results, tasks)
	env := fakePREnv("", "", nil, nil)

	var buf bytes.Buffer
	err := runPR(context.Background(), env, &buf, runDir, reposDir, "main", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "0 PRs created") {
		t.Errorf("expected 0 PRs created, got:\n%s", out)
	}
}

func TestPR_MultipleTasks(t *testing.T) {
	results := map[string]*task.TaskResult{
		"task-1": {
			TaskID:         "task-1",
			State:          task.StateCompleted,
			WorktreeBranch: "tokencontrol/task-1",
			RunnerUsed:     "codex",
			Duration:       10 * time.Second,
		},
		"task-2": {
			TaskID:         "task-2",
			State:          task.StateCompleted,
			WorktreeBranch: "", // no branch
		},
		"task-3": {
			TaskID:         "task-3",
			State:          task.StateCompleted,
			WorktreeBranch: "tokencontrol/task-3",
			RunnerUsed:     "claude",
			Duration:       20 * time.Second,
		},
	}
	tasks := map[string]*task.Task{
		"task-1": {ID: "task-1", Title: "Fix auth", Repo: "ppiankov/repo-a"},
		"task-2": {ID: "task-2", Title: "Update docs", Repo: "ppiankov/repo-b"},
		"task-3": {ID: "task-3", Title: "Add tests", Repo: "ppiankov/repo-c"},
	}

	runDir, reposDir := setupPRTest(t, results, tasks)
	env := fakePREnv("", "https://github.com/ppiankov/repo/pull/1", nil, nil)

	var buf bytes.Buffer
	err := runPR(context.Background(), env, &buf, runDir, reposDir, "main", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "2 PRs created") {
		t.Errorf("expected 2 PRs created, got:\n%s", out)
	}
	if !strings.Contains(out, "1 skipped") {
		t.Errorf("expected 1 skipped, got:\n%s", out)
	}
}

func TestBuildPRBody(t *testing.T) {
	meta := &task.Task{ID: "task-1", Title: "Fix authentication"}
	result := &task.TaskResult{
		RunnerUsed:    "codex",
		Duration:      90 * time.Second,
		AutoCommitted: true,
	}

	body := buildPRBody(meta, result)
	if !strings.Contains(body, "Fix authentication") {
		t.Errorf("expected title in body, got:\n%s", body)
	}
	if !strings.Contains(body, "codex") {
		t.Errorf("expected runner in body, got:\n%s", body)
	}
	if !strings.Contains(body, "1m30s") {
		t.Errorf("expected duration in body, got:\n%s", body)
	}
	if !strings.Contains(body, "Auto-committed") {
		t.Errorf("expected auto-committed in body, got:\n%s", body)
	}
	if !strings.Contains(body, "tokencontrol") {
		t.Errorf("expected tokencontrol link in body, got:\n%s", body)
	}
}

func TestSortedTaskIDs(t *testing.T) {
	results := map[string]*task.TaskResult{
		"task-3": {},
		"task-1": {},
		"task-2": {},
	}
	ids := sortedTaskIDs(results)
	if len(ids) != 3 {
		t.Fatalf("expected 3 ids, got %d", len(ids))
	}
	if ids[0] != "task-1" || ids[1] != "task-2" || ids[2] != "task-3" {
		t.Errorf("expected sorted order, got: %v", ids)
	}
}

func TestPrintPRSummary(t *testing.T) {
	results := []prResult{
		{TaskID: "t1", PRURL: "https://example.com/1"},
		{TaskID: "t2", Skipped: "no branch"},
		{TaskID: "t3", Error: "push failed"},
		{TaskID: "t4", PRURL: "https://example.com/4"},
	}

	var buf bytes.Buffer
	printPRSummary(&buf, results)

	out := buf.String()
	if !strings.Contains(out, "2 PRs created") {
		t.Errorf("expected 2 created, got:\n%s", out)
	}
	if !strings.Contains(out, "1 skipped") {
		t.Errorf("expected 1 skipped, got:\n%s", out)
	}
	if !strings.Contains(out, "1 failed") {
		t.Errorf("expected 1 failed, got:\n%s", out)
	}
}
