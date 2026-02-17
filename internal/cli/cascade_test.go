package cli

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ppiankov/runforge/internal/config"
	"github.com/ppiankov/runforge/internal/runner"
	"github.com/ppiankov/runforge/internal/task"
)

// mockRunner simulates a runner with configurable behavior.
type mockRunner struct {
	name   string
	result func(t *task.Task) *task.TaskResult
}

func (m *mockRunner) Name() string { return m.name }
func (m *mockRunner) Run(_ context.Context, t *task.Task, _, _ string) *task.TaskResult {
	return m.result(t)
}

func completedResult(id string) *task.TaskResult {
	return &task.TaskResult{TaskID: id, State: task.StateCompleted}
}

func failedMockResult(id, msg string) *task.TaskResult {
	return &task.TaskResult{TaskID: id, State: task.StateFailed, Error: msg}
}

func rateLimitedResult(id string, resetsAt time.Time) *task.TaskResult {
	return &task.TaskResult{TaskID: id, State: task.StateRateLimited, ResetsAt: resetsAt}
}

func TestCascade_FirstSucceeds(t *testing.T) {
	runners := map[string]runner.Runner{
		"codex": &mockRunner{name: "codex", result: func(tk *task.Task) *task.TaskResult {
			return completedResult(tk.ID)
		}},
		"zai": &mockRunner{name: "zai", result: func(tk *task.Task) *task.TaskResult {
			t.Fatal("zai should not be called")
			return nil
		}},
	}

	tk := &task.Task{ID: "test-1", Repo: "test/repo", Prompt: "do stuff"}
	bl := runner.NewRunnerBlacklist()
	result := RunWithCascade(context.Background(), tk, "/tmp", t.TempDir(), runners, []string{"codex", "zai"}, 5*time.Minute, bl, nil)

	if result.State != task.StateCompleted {
		t.Fatalf("expected completed, got %s", result.State)
	}
	if result.RunnerUsed != "codex" {
		t.Fatalf("expected runner_used=codex, got %s", result.RunnerUsed)
	}
	if len(result.Attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(result.Attempts))
	}
}

func TestCascade_FirstRateLimited(t *testing.T) {
	resetsAt := time.Now().Add(4 * time.Hour)
	runners := map[string]runner.Runner{
		"codex": &mockRunner{name: "codex", result: func(tk *task.Task) *task.TaskResult {
			return rateLimitedResult(tk.ID, resetsAt)
		}},
		"zai": &mockRunner{name: "zai", result: func(tk *task.Task) *task.TaskResult {
			return completedResult(tk.ID)
		}},
	}

	tk := &task.Task{ID: "test-2", Repo: "test/repo", Prompt: "do stuff"}
	bl := runner.NewRunnerBlacklist()
	result := RunWithCascade(context.Background(), tk, "/tmp", t.TempDir(), runners, []string{"codex", "zai"}, 5*time.Minute, bl, nil)

	if result.State != task.StateCompleted {
		t.Fatalf("expected completed, got %s", result.State)
	}
	if result.RunnerUsed != "zai" {
		t.Fatalf("expected runner_used=zai, got %s", result.RunnerUsed)
	}
	if len(result.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(result.Attempts))
	}
}

func TestCascade_FirstFailed(t *testing.T) {
	runners := map[string]runner.Runner{
		"codex": &mockRunner{name: "codex", result: func(tk *task.Task) *task.TaskResult {
			return failedMockResult(tk.ID, "codex error")
		}},
		"zai": &mockRunner{name: "zai", result: func(tk *task.Task) *task.TaskResult {
			return completedResult(tk.ID)
		}},
	}

	tk := &task.Task{ID: "test-3", Repo: "test/repo", Prompt: "do stuff"}
	bl := runner.NewRunnerBlacklist()
	result := RunWithCascade(context.Background(), tk, "/tmp", t.TempDir(), runners, []string{"codex", "zai"}, 5*time.Minute, bl, nil)

	if result.State != task.StateCompleted {
		t.Fatalf("expected completed, got %s", result.State)
	}
	if result.RunnerUsed != "zai" {
		t.Fatalf("expected runner_used=zai, got %s", result.RunnerUsed)
	}
}

func TestCascade_AllFail(t *testing.T) {
	runners := map[string]runner.Runner{
		"codex": &mockRunner{name: "codex", result: func(tk *task.Task) *task.TaskResult {
			return failedMockResult(tk.ID, "codex error")
		}},
		"zai": &mockRunner{name: "zai", result: func(tk *task.Task) *task.TaskResult {
			return failedMockResult(tk.ID, "zai error")
		}},
	}

	tk := &task.Task{ID: "test-4", Repo: "test/repo", Prompt: "do stuff"}
	bl := runner.NewRunnerBlacklist()
	result := RunWithCascade(context.Background(), tk, "/tmp", t.TempDir(), runners, []string{"codex", "zai"}, 5*time.Minute, bl, nil)

	if result.State != task.StateFailed {
		t.Fatalf("expected failed, got %s", result.State)
	}
	if len(result.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(result.Attempts))
	}
}

func TestCascade_BlacklistSkips(t *testing.T) {
	callCount := 0
	runners := map[string]runner.Runner{
		"codex": &mockRunner{name: "codex", result: func(tk *task.Task) *task.TaskResult {
			t.Fatal("codex should be skipped due to blacklist")
			return nil
		}},
		"zai": &mockRunner{name: "zai", result: func(tk *task.Task) *task.TaskResult {
			callCount++
			return completedResult(tk.ID)
		}},
	}

	bl := runner.NewRunnerBlacklist()
	bl.Block("codex", time.Now().Add(4*time.Hour))

	tk := &task.Task{ID: "test-5", Repo: "test/repo", Prompt: "do stuff"}
	result := RunWithCascade(context.Background(), tk, "/tmp", t.TempDir(), runners, []string{"codex", "zai"}, 5*time.Minute, bl, nil)

	if result.State != task.StateCompleted {
		t.Fatalf("expected completed, got %s", result.State)
	}
	if result.RunnerUsed != "zai" {
		t.Fatalf("expected runner_used=zai, got %s", result.RunnerUsed)
	}
	if callCount != 1 {
		t.Fatalf("expected zai called once, got %d", callCount)
	}
	// first attempt should be a skip
	if result.Attempts[0].State != task.StateSkipped {
		t.Fatalf("expected first attempt skipped, got %s", result.Attempts[0].State)
	}
}

func TestCascade_NoFallbacks(t *testing.T) {
	runners := map[string]runner.Runner{
		"codex": &mockRunner{name: "codex", result: func(tk *task.Task) *task.TaskResult {
			return completedResult(tk.ID)
		}},
	}

	tk := &task.Task{ID: "test-6", Repo: "test/repo", Prompt: "do stuff"}
	bl := runner.NewRunnerBlacklist()
	result := RunWithCascade(context.Background(), tk, "/tmp", t.TempDir(), runners, []string{"codex"}, 5*time.Minute, bl, nil)

	if result.State != task.StateCompleted {
		t.Fatalf("expected completed, got %s", result.State)
	}
	if result.RunnerUsed != "codex" {
		t.Fatalf("expected runner_used=codex, got %s", result.RunnerUsed)
	}
	if len(result.Attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(result.Attempts))
	}
}

func TestCascade_AttemptsRecorded(t *testing.T) {
	resetsAt := time.Now().Add(1 * time.Hour)
	runners := map[string]runner.Runner{
		"codex": &mockRunner{name: "codex", result: func(tk *task.Task) *task.TaskResult {
			return rateLimitedResult(tk.ID, resetsAt)
		}},
		"zai": &mockRunner{name: "zai", result: func(tk *task.Task) *task.TaskResult {
			return failedMockResult(tk.ID, "zai broke")
		}},
		"claude-api": &mockRunner{name: "claude-api", result: func(tk *task.Task) *task.TaskResult {
			return completedResult(tk.ID)
		}},
	}

	tk := &task.Task{ID: "test-7", Repo: "test/repo", Prompt: "do stuff"}
	bl := runner.NewRunnerBlacklist()
	result := RunWithCascade(context.Background(), tk, "/tmp", t.TempDir(), runners, []string{"codex", "zai", "claude-api"}, 5*time.Minute, bl, nil)

	if result.State != task.StateCompleted {
		t.Fatalf("expected completed, got %s", result.State)
	}
	if result.RunnerUsed != "claude-api" {
		t.Fatalf("expected runner_used=claude-api, got %s", result.RunnerUsed)
	}
	if len(result.Attempts) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(result.Attempts))
	}

	// verify attempt details
	if result.Attempts[0].Runner != "codex" || result.Attempts[0].State != task.StateRateLimited {
		t.Fatalf("attempt 0 wrong: %+v", result.Attempts[0])
	}
	if result.Attempts[1].Runner != "zai" || result.Attempts[1].State != task.StateFailed {
		t.Fatalf("attempt 1 wrong: %+v", result.Attempts[1])
	}
	if result.Attempts[2].Runner != "claude-api" || result.Attempts[2].State != task.StateCompleted {
		t.Fatalf("attempt 2 wrong: %+v", result.Attempts[2])
	}
}

func TestCascade_RateLimitBlocksForSubsequentTasks(t *testing.T) {
	resetsAt := time.Now().Add(4 * time.Hour)
	codexCalls := 0
	runners := map[string]runner.Runner{
		"codex": &mockRunner{name: "codex", result: func(tk *task.Task) *task.TaskResult {
			codexCalls++
			return rateLimitedResult(tk.ID, resetsAt)
		}},
		"zai": &mockRunner{name: "zai", result: func(tk *task.Task) *task.TaskResult {
			return completedResult(tk.ID)
		}},
	}

	bl := runner.NewRunnerBlacklist()

	// first task triggers rate limit
	tk1 := &task.Task{ID: "task-1", Repo: "test/repo", Prompt: "first"}
	r1 := RunWithCascade(context.Background(), tk1, "/tmp", t.TempDir(), runners, []string{"codex", "zai"}, 5*time.Minute, bl, nil)
	if r1.State != task.StateCompleted {
		t.Fatalf("task-1: expected completed, got %s", r1.State)
	}
	if codexCalls != 1 {
		t.Fatalf("expected codex called once for task-1, got %d", codexCalls)
	}

	// second task should skip codex entirely
	tk2 := &task.Task{ID: "task-2", Repo: "test/repo", Prompt: "second"}
	r2 := RunWithCascade(context.Background(), tk2, "/tmp", t.TempDir(), runners, []string{"codex", "zai"}, 5*time.Minute, bl, nil)
	if r2.State != task.StateCompleted {
		t.Fatalf("task-2: expected completed, got %s", r2.State)
	}
	if codexCalls != 1 {
		t.Fatalf("expected codex NOT called for task-2, but total calls is %d", codexCalls)
	}
}

func TestResolveRunnerCascade_Defaults(t *testing.T) {
	tk := &task.Task{ID: "t1"}
	cascade := resolveRunnerCascade(tk, "codex", []string{"zai", "claude-api"})
	expected := []string{"codex", "zai", "claude-api"}
	if fmt.Sprintf("%v", cascade) != fmt.Sprintf("%v", expected) {
		t.Fatalf("expected %v, got %v", expected, cascade)
	}
}

func TestResolveRunnerCascade_TaskOverride(t *testing.T) {
	tk := &task.Task{ID: "t1", Runner: "zai", Fallbacks: []string{"claude-api"}}
	cascade := resolveRunnerCascade(tk, "codex", []string{"zai", "claude-api"})
	expected := []string{"zai", "claude-api"}
	if fmt.Sprintf("%v", cascade) != fmt.Sprintf("%v", expected) {
		t.Fatalf("expected %v, got %v", expected, cascade)
	}
}

func TestResolveRunnerCascade_NoDuplicatePrimary(t *testing.T) {
	tk := &task.Task{ID: "t1"}
	cascade := resolveRunnerCascade(tk, "codex", []string{"codex", "zai"})
	expected := []string{"codex", "zai"}
	if fmt.Sprintf("%v", cascade) != fmt.Sprintf("%v", expected) {
		t.Fatalf("expected %v, got %v", expected, cascade)
	}
}

func TestStripeRunners_RoundRobin(t *testing.T) {
	tasks := make([]task.Task, 10)
	for i := range tasks {
		tasks[i] = task.Task{ID: fmt.Sprintf("t%d", i)}
	}

	stripeRunners(tasks, "codex", []string{"deepseek", "minimax", "zai", "claude"})

	// 5 providers: codex, deepseek, minimax, zai, claude
	expectedPrimary := []string{"codex", "deepseek", "minimax", "zai", "claude", "codex", "deepseek", "minimax", "zai", "claude"}
	for i, tk := range tasks {
		if tk.Runner != expectedPrimary[i] {
			t.Fatalf("task %d: expected runner=%s, got %s", i, expectedPrimary[i], tk.Runner)
		}
		if len(tk.Fallbacks) != 4 {
			t.Fatalf("task %d: expected 4 fallbacks, got %d", i, len(tk.Fallbacks))
		}
		// primary must not appear in fallbacks
		for _, fb := range tk.Fallbacks {
			if fb == tk.Runner {
				t.Fatalf("task %d: primary %s found in fallbacks", i, tk.Runner)
			}
		}
	}
}

func TestStripeRunners_RespectsExplicitRunner(t *testing.T) {
	tasks := []task.Task{
		{ID: "t0"},
		{ID: "t1", Runner: "custom", Fallbacks: []string{"other"}},
		{ID: "t2"},
	}

	stripeRunners(tasks, "codex", []string{"deepseek", "zai"})

	if tasks[0].Runner != "codex" {
		t.Fatalf("t0: expected codex, got %s", tasks[0].Runner)
	}
	if tasks[1].Runner != "custom" {
		t.Fatalf("t1: explicit runner should be preserved, got %s", tasks[1].Runner)
	}
	if len(tasks[1].Fallbacks) != 1 || tasks[1].Fallbacks[0] != "other" {
		t.Fatalf("t1: explicit fallbacks should be preserved, got %v", tasks[1].Fallbacks)
	}
	if tasks[2].Runner != "zai" {
		t.Fatalf("t2: expected zai (index 2 %% 3), got %s", tasks[2].Runner)
	}
}

func TestStripeRunners_NoFallbacks(t *testing.T) {
	tasks := []task.Task{{ID: "t0"}, {ID: "t1"}}
	stripeRunners(tasks, "codex", nil)

	// no-op when no fallbacks configured
	if tasks[0].Runner != "" {
		t.Fatalf("expected empty runner, got %s", tasks[0].Runner)
	}
}

func TestMergeSettings_FillsDefaults(t *testing.T) {
	tf := &task.TaskFile{
		Tasks: []task.Task{{ID: "t1", Repo: "r", Prompt: "p"}},
	}
	cfg := &config.Settings{
		DefaultRunner:    "codex",
		DefaultFallbacks: []string{"deepseek", "zai"},
		Runners: map[string]*config.RunnerProfile{
			"deepseek": {Type: "codex", Profile: "deepseek"},
			"zai":      {Type: "codex", Env: map[string]string{"K": "V"}},
		},
	}

	mergeSettings(tf, cfg)

	if tf.DefaultRunner != "codex" {
		t.Fatalf("expected default_runner=codex, got %s", tf.DefaultRunner)
	}
	if len(tf.DefaultFallbacks) != 2 || tf.DefaultFallbacks[0] != "deepseek" {
		t.Fatalf("expected fallbacks from settings, got %v", tf.DefaultFallbacks)
	}
	if len(tf.Runners) != 2 {
		t.Fatalf("expected 2 runner profiles, got %d", len(tf.Runners))
	}
	if tf.Runners["zai"].Env["K"] != "V" {
		t.Fatal("expected zai env to be merged")
	}
}

func TestMergeSettings_TaskFileWins(t *testing.T) {
	tf := &task.TaskFile{
		DefaultRunner:    "custom",
		DefaultFallbacks: []string{"a"},
		Runners: map[string]*task.RunnerProfileConfig{
			"zai": {Type: "claude"},
		},
		Tasks: []task.Task{{ID: "t1", Repo: "r", Prompt: "p"}},
	}
	cfg := &config.Settings{
		DefaultRunner:    "codex",
		DefaultFallbacks: []string{"deepseek", "zai"},
		Runners: map[string]*config.RunnerProfile{
			"zai":      {Type: "codex"},
			"deepseek": {Type: "codex"},
		},
	}

	mergeSettings(tf, cfg)

	if tf.DefaultRunner != "custom" {
		t.Fatalf("task file default_runner should win, got %s", tf.DefaultRunner)
	}
	if len(tf.DefaultFallbacks) != 1 || tf.DefaultFallbacks[0] != "a" {
		t.Fatalf("task file fallbacks should win, got %v", tf.DefaultFallbacks)
	}
	if tf.Runners["zai"].Type != "claude" {
		t.Fatal("task file runner profile should not be overwritten")
	}
	if _, ok := tf.Runners["deepseek"]; !ok {
		t.Fatal("new runner profile from settings should be added")
	}
}

func TestMergeSettings_NilConfig(t *testing.T) {
	tf := &task.TaskFile{
		Tasks: []task.Task{{ID: "t1", Repo: "r", Prompt: "p"}},
	}
	mergeSettings(tf, nil)
	if tf.DefaultRunner != "" {
		t.Fatal("nil config should not modify task file")
	}
}

func TestBuildRunnerRegistry_BuiltinsOnly(t *testing.T) {
	tf := &task.TaskFile{}
	reg, err := buildRunnerRegistry(tf, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, name := range []string{"codex", "claude", "script"} {
		if _, ok := reg[name]; !ok {
			t.Fatalf("expected built-in runner %q", name)
		}
	}
}

func TestBuildRunnerRegistry_WithProfile(t *testing.T) {
	tf := &task.TaskFile{
		Runners: map[string]*task.RunnerProfileConfig{
			"zai": {
				Type: "claude",
				Env: map[string]string{
					"API_URL": "https://api.z.ai",
				},
			},
		},
	}
	reg, err := buildRunnerRegistry(tf, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := reg["zai"]; !ok {
		t.Fatal("expected zai runner in registry")
	}
	if _, ok := reg["codex"]; !ok {
		t.Fatal("expected codex runner still in registry")
	}
}
