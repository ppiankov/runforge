package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ppiankov/runforge/internal/runner"
	"github.com/ppiankov/runforge/internal/task"
)

func reviewRunner(name, response string) *mockRunner {
	return &mockRunner{
		name: name,
		result: func(t *task.Task) *task.TaskResult {
			return &task.TaskResult{
				TaskID:  t.ID,
				State:   task.StateCompleted,
				LastMsg: response,
			}
		},
	}
}

func TestReviewPool_FallbackTriggersReview(t *testing.T) {
	runners := map[string]runner.Runner{
		"codex": reviewRunner("codex", "PASS looks good"),
		"zai":   reviewRunner("zai", ""),
	}

	cfg := &task.ReviewConfig{Enabled: true, FallbackOnly: true}
	bl := runner.NewRunnerBlacklist()
	pool := NewReviewPool(cfg, runners, bl, 5*time.Minute, 1)
	pool.Start(context.Background(), 1)

	runDir := t.TempDir()

	// simulate a fallback task (2 attempts → triggers review)
	result := &task.TaskResult{
		TaskID:     "task-1",
		State:      task.StateCompleted,
		RunnerUsed: "zai",
		LastMsg:    "some output",
		Attempts: []task.AttemptInfo{
			{Runner: "codex", State: task.StateRateLimited},
			{Runner: "zai", State: task.StateCompleted},
		},
	}

	tk := &task.Task{ID: "task-1", Repo: "test/repo", Title: "do stuff", Prompt: "test"}
	pool.Submit(reviewJob{taskID: "task-1", task: tk, result: result, runDir: runDir})
	pool.Wait()

	results := map[string]*task.TaskResult{"task-1": result}
	pool.ApplyResults(results)

	if result.Review == nil {
		t.Fatal("expected review result")
	}
	if result.Review.Runner != "codex" {
		t.Fatalf("expected reviewer=codex, got %s", result.Review.Runner)
	}
	if !result.Review.Passed {
		t.Fatal("expected review to pass")
	}
}

func TestReviewPool_NoFallbackNoReview(t *testing.T) {
	runners := map[string]runner.Runner{
		"codex": reviewRunner("codex", "PASS"),
	}

	cfg := &task.ReviewConfig{Enabled: true, FallbackOnly: true}
	bl := runner.NewRunnerBlacklist()
	pool := NewReviewPool(cfg, runners, bl, 5*time.Minute, 1)
	pool.Start(context.Background(), 1)

	// single attempt → no fallback → should NOT be reviewed
	result := &task.TaskResult{
		TaskID:     "task-1",
		State:      task.StateCompleted,
		RunnerUsed: "codex",
		Attempts:   []task.AttemptInfo{{Runner: "codex", State: task.StateCompleted}},
	}

	tk := &task.Task{ID: "task-1", Repo: "test/repo", Title: "stuff", Prompt: "test"}
	pool.Submit(reviewJob{taskID: "task-1", task: tk, result: result, runDir: t.TempDir()})
	pool.Wait()

	results := map[string]*task.TaskResult{"task-1": result}
	pool.ApplyResults(results)

	if result.Review != nil {
		t.Fatal("expected no review for non-fallback task")
	}
}

func TestReviewPool_ReviewAll(t *testing.T) {
	runners := map[string]runner.Runner{
		"codex":  reviewRunner("codex", "PASS all good"),
		"claude": reviewRunner("claude", ""),
	}

	cfg := &task.ReviewConfig{Enabled: true, FallbackOnly: false}
	bl := runner.NewRunnerBlacklist()
	pool := NewReviewPool(cfg, runners, bl, 5*time.Minute, 1)
	pool.Start(context.Background(), 1)

	runDir := t.TempDir()

	// single attempt, but fallback_only=false → should be reviewed
	result := &task.TaskResult{
		TaskID:     "task-1",
		State:      task.StateCompleted,
		RunnerUsed: "claude",
		LastMsg:    "output here",
		Attempts:   []task.AttemptInfo{{Runner: "claude", State: task.StateCompleted}},
	}

	tk := &task.Task{ID: "task-1", Repo: "test/repo", Title: "stuff", Prompt: "test"}
	pool.Submit(reviewJob{taskID: "task-1", task: tk, result: result, runDir: runDir})
	pool.Wait()

	results := map[string]*task.TaskResult{"task-1": result}
	pool.ApplyResults(results)

	if result.Review == nil {
		t.Fatal("expected review when fallback_only=false")
	}
	if result.Review.Runner != "codex" {
		t.Fatalf("expected reviewer=codex (auto-picked), got %s", result.Review.Runner)
	}
}

func TestReviewPool_PicksAlternateRunner(t *testing.T) {
	runners := map[string]runner.Runner{
		"codex":  reviewRunner("codex", "PASS"),
		"claude": reviewRunner("claude", "PASS"),
	}

	cfg := &task.ReviewConfig{Enabled: true, FallbackOnly: false}
	bl := runner.NewRunnerBlacklist()
	pool := NewReviewPool(cfg, runners, bl, 5*time.Minute, 1)
	pool.Start(context.Background(), 1)

	// task run by codex → reviewer should be claude (not codex)
	result := &task.TaskResult{
		TaskID:     "task-1",
		State:      task.StateCompleted,
		RunnerUsed: "codex",
		LastMsg:    "some code",
		Attempts:   []task.AttemptInfo{{Runner: "codex", State: task.StateCompleted}},
	}

	tk := &task.Task{ID: "task-1", Repo: "test/repo", Title: "stuff", Prompt: "test"}
	pool.Submit(reviewJob{taskID: "task-1", task: tk, result: result, runDir: t.TempDir()})
	pool.Wait()

	results := map[string]*task.TaskResult{"task-1": result}
	pool.ApplyResults(results)

	if result.Review == nil {
		t.Fatal("expected review")
	}
	if result.Review.Runner != "claude" {
		t.Fatalf("expected reviewer=claude (alternate), got %s", result.Review.Runner)
	}
}

func TestReviewPool_BlacklistedReviewerSkipped(t *testing.T) {
	runners := map[string]runner.Runner{
		"codex":  reviewRunner("codex", "PASS"),
		"claude": reviewRunner("claude", "PASS from claude"),
	}

	cfg := &task.ReviewConfig{Enabled: true, Runner: "codex", FallbackOnly: false}
	bl := runner.NewRunnerBlacklist()
	bl.Block("codex", time.Now().Add(4*time.Hour))
	pool := NewReviewPool(cfg, runners, bl, 5*time.Minute, 1)
	pool.Start(context.Background(), 1)

	result := &task.TaskResult{
		TaskID:     "task-1",
		State:      task.StateCompleted,
		RunnerUsed: "zai",
		LastMsg:    "output",
		Attempts: []task.AttemptInfo{
			{Runner: "codex", State: task.StateRateLimited},
			{Runner: "zai", State: task.StateCompleted},
		},
	}

	tk := &task.Task{ID: "task-1", Repo: "test/repo", Title: "stuff", Prompt: "test"}
	pool.Submit(reviewJob{taskID: "task-1", task: tk, result: result, runDir: t.TempDir()})
	pool.Wait()

	results := map[string]*task.TaskResult{"task-1": result}
	pool.ApplyResults(results)

	if result.Review == nil {
		t.Fatal("expected review")
	}
	// codex is blacklisted, should fall back to claude
	if result.Review.Runner != "claude" {
		t.Fatalf("expected reviewer=claude (codex blacklisted), got %s", result.Review.Runner)
	}
}

func TestReviewPool_ReviewPassParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"PASS looks good", true},
		{"PASS", true},
		{"pass - everything fine", true},
		{"FAIL major issues", false},
		{"fail", false},
		{"The code has issues", false},
		{"", false},
	}

	for _, tc := range tests {
		got := parseReviewVerdict(tc.input)
		if got != tc.expected {
			t.Errorf("parseReviewVerdict(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestReviewPool_Disabled(t *testing.T) {
	runners := map[string]runner.Runner{
		"codex": reviewRunner("codex", "PASS"),
	}

	// Enabled=false → pool should not be created in real usage,
	// but verify Submit still works safely if called
	cfg := &task.ReviewConfig{Enabled: false, FallbackOnly: true}
	bl := runner.NewRunnerBlacklist()
	pool := NewReviewPool(cfg, runners, bl, 5*time.Minute, 1)
	pool.Start(context.Background(), 1)

	result := &task.TaskResult{
		TaskID:     "task-1",
		State:      task.StateCompleted,
		RunnerUsed: "zai",
		LastMsg:    "output",
		Attempts: []task.AttemptInfo{
			{Runner: "codex", State: task.StateRateLimited},
			{Runner: "zai", State: task.StateCompleted},
		},
	}

	tk := &task.Task{ID: "task-1", Repo: "test/repo", Title: "stuff", Prompt: "test"}
	pool.Submit(reviewJob{taskID: "task-1", task: tk, result: result, runDir: t.TempDir()})
	pool.Wait()

	results := map[string]*task.TaskResult{"task-1": result}
	pool.ApplyResults(results)

	// pool runs but that's fine — in practice, executeRun gates on Enabled
	// The pool itself doesn't check Enabled; the caller does
}

func TestReviewPool_ReadTaskOutput_FromLastMsg(t *testing.T) {
	runners := map[string]runner.Runner{
		"codex": reviewRunner("codex", "PASS"),
	}

	cfg := &task.ReviewConfig{Enabled: true, FallbackOnly: false}
	bl := runner.NewRunnerBlacklist()
	pool := NewReviewPool(cfg, runners, bl, 5*time.Minute, 1)

	result := &task.TaskResult{
		LastMsg: "this is the output",
	}
	got := pool.readTaskOutput(result)
	if got != "this is the output" {
		t.Fatalf("expected LastMsg, got %q", got)
	}
}

func TestReviewPool_ReadTaskOutput_FromFile(t *testing.T) {
	runners := map[string]runner.Runner{
		"codex": reviewRunner("codex", "PASS"),
	}

	cfg := &task.ReviewConfig{Enabled: true, FallbackOnly: false}
	bl := runner.NewRunnerBlacklist()
	pool := NewReviewPool(cfg, runners, bl, 5*time.Minute, 1)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "output.md"), []byte("file content"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := &task.TaskResult{
		OutputDir: dir,
	}
	got := pool.readTaskOutput(result)
	if got != "file content" {
		t.Fatalf("expected 'file content', got %q", got)
	}
}

func TestBuildReviewPrompt(t *testing.T) {
	prompt := buildReviewPrompt("fix auth bug", "some diff output")
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	if !contains(prompt, "fix auth bug") {
		t.Fatal("expected title in prompt")
	}
	if !contains(prompt, "some diff output") {
		t.Fatal("expected output in prompt")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
