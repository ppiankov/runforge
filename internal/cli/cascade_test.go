package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	result := RunWithCascade(context.Background(), tk, "/tmp", t.TempDir(), runners, []string{"codex", "zai"}, 5*time.Minute, bl, nil, nil)

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
	result := RunWithCascade(context.Background(), tk, "/tmp", t.TempDir(), runners, []string{"codex", "zai"}, 5*time.Minute, bl, nil, nil)

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
	result := RunWithCascade(context.Background(), tk, "/tmp", t.TempDir(), runners, []string{"codex", "zai"}, 5*time.Minute, bl, nil, nil)

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
	result := RunWithCascade(context.Background(), tk, "/tmp", t.TempDir(), runners, []string{"codex", "zai"}, 5*time.Minute, bl, nil, nil)

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
	result := RunWithCascade(context.Background(), tk, "/tmp", t.TempDir(), runners, []string{"codex", "zai"}, 5*time.Minute, bl, nil, nil)

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
	result := RunWithCascade(context.Background(), tk, "/tmp", t.TempDir(), runners, []string{"codex"}, 5*time.Minute, bl, nil, nil)

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
	result := RunWithCascade(context.Background(), tk, "/tmp", t.TempDir(), runners, []string{"codex", "zai", "claude-api"}, 5*time.Minute, bl, nil, nil)

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
	r1 := RunWithCascade(context.Background(), tk1, "/tmp", t.TempDir(), runners, []string{"codex", "zai"}, 5*time.Minute, bl, nil, nil)
	if r1.State != task.StateCompleted {
		t.Fatalf("task-1: expected completed, got %s", r1.State)
	}
	if codexCalls != 1 {
		t.Fatalf("expected codex called once for task-1, got %d", codexCalls)
	}

	// second task should skip codex entirely
	tk2 := &task.Task{ID: "task-2", Repo: "test/repo", Prompt: "second"}
	r2 := RunWithCascade(context.Background(), tk2, "/tmp", t.TempDir(), runners, []string{"codex", "zai"}, 5*time.Minute, bl, nil, nil)
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

func TestFilterDataCollection_PublicRepo(t *testing.T) {
	profiles := map[string]*task.RunnerProfileConfig{
		"gemini": {Type: "gemini"},
		"pickle": {Type: "opencode", DataCollection: true},
	}
	privateRepos := map[string]struct{}{"ppiankov/secret": {}}

	// public repo — no filtering
	cascade := []string{"gemini", "pickle"}
	result := filterDataCollectionRunners(cascade, "ppiankov/entropia", profiles, privateRepos)
	if len(result) != 2 {
		t.Fatalf("public repo should keep all runners, got %d", len(result))
	}
}

func TestFilterDataCollection_PrivateRepo(t *testing.T) {
	profiles := map[string]*task.RunnerProfileConfig{
		"gemini": {Type: "gemini"},
		"pickle": {Type: "opencode", DataCollection: true},
		"claude": {Type: "claude"},
	}
	privateRepos := map[string]struct{}{"ppiankov/secret": {}}

	cascade := []string{"gemini", "pickle", "claude"}
	result := filterDataCollectionRunners(cascade, "ppiankov/secret", profiles, privateRepos)

	if len(result) != 2 {
		t.Fatalf("expected 2 runners after filtering, got %d", len(result))
	}
	if result[0] != "gemini" || result[1] != "claude" {
		t.Fatalf("expected [gemini claude], got %v", result)
	}
}

func TestFilterDataCollection_NoPrivateRepos(t *testing.T) {
	profiles := map[string]*task.RunnerProfileConfig{
		"pickle": {Type: "opencode", DataCollection: true},
	}

	cascade := []string{"pickle"}
	result := filterDataCollectionRunners(cascade, "ppiankov/entropia", profiles, nil)
	if len(result) != 1 {
		t.Fatalf("no private repos configured — should keep all, got %d", len(result))
	}
}

func TestFilterDataCollection_AllFiltered(t *testing.T) {
	profiles := map[string]*task.RunnerProfileConfig{
		"pickle":  {Type: "opencode", DataCollection: true},
		"minimax": {Type: "opencode", DataCollection: true},
	}
	privateRepos := map[string]struct{}{"ppiankov/secret": {}}

	cascade := []string{"pickle", "minimax"}
	result := filterDataCollectionRunners(cascade, "ppiankov/secret", profiles, privateRepos)

	if len(result) != 0 {
		t.Fatalf("all data-collecting runners should be filtered for private repo, got %d", len(result))
	}
}

func TestFilterDataCollection_UnknownRunnerKept(t *testing.T) {
	// runners not in profiles (built-in defaults) should NOT be filtered
	profiles := map[string]*task.RunnerProfileConfig{
		"pickle": {Type: "opencode", DataCollection: true},
	}
	privateRepos := map[string]struct{}{"ppiankov/secret": {}}

	cascade := []string{"codex", "pickle"}
	result := filterDataCollectionRunners(cascade, "ppiankov/secret", profiles, privateRepos)

	if len(result) != 1 || result[0] != "codex" {
		t.Fatalf("expected [codex], got %v", result)
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

func TestFilterGraylistedRunners_KeepsPrimary(t *testing.T) {
	gl := runner.NewRunnerGraylist()
	gl.Add("codex", "", "test reason")
	gl.Add("gemini", "", "another reason")

	cascade := []string{"codex", "claude", "gemini"}
	result := filterGraylistedRunners(cascade, gl, nil)

	// primary (codex) must stay even though graylisted; gemini filtered from fallbacks
	expected := []string{"codex", "claude"}
	if fmt.Sprintf("%v", result) != fmt.Sprintf("%v", expected) {
		t.Fatalf("expected %v, got %v", expected, result)
	}
}

func TestFilterGraylistedRunners_FiltersFallbacks(t *testing.T) {
	gl := runner.NewRunnerGraylist()
	gl.Add("minimax-free", "", "false positive")
	gl.Add("gpt5nano", "", "0 events")

	cascade := []string{"codex", "minimax-free", "claude", "gpt5nano"}
	result := filterGraylistedRunners(cascade, gl, nil)

	expected := []string{"codex", "claude"}
	if fmt.Sprintf("%v", result) != fmt.Sprintf("%v", expected) {
		t.Fatalf("expected %v, got %v", expected, result)
	}
}

func TestFilterGraylistedRunners_NilGraylist(t *testing.T) {
	cascade := []string{"codex", "claude"}
	result := filterGraylistedRunners(cascade, nil, nil)

	if fmt.Sprintf("%v", result) != fmt.Sprintf("%v", cascade) {
		t.Fatalf("nil graylist should pass through, got %v", result)
	}
}

func TestFilterGraylistedRunners_SingleRunner(t *testing.T) {
	gl := runner.NewRunnerGraylist()
	gl.Add("codex", "", "test")

	cascade := []string{"codex"}
	result := filterGraylistedRunners(cascade, gl, nil)

	// single runner = no fallbacks to filter
	if len(result) != 1 || result[0] != "codex" {
		t.Fatalf("single runner cascade should pass through, got %v", result)
	}
}

func TestFilterGraylistedRunners_ModelAware(t *testing.T) {
	gl := runner.NewRunnerGraylist()
	gl.Add("deepseek", "deepseek-chat", "cheap model")

	profiles := map[string]*task.RunnerProfileConfig{
		"deepseek":     {Type: "opencode", Model: "deepseek-chat"},
		"deepseek-pro": {Type: "opencode", Model: "deepseek-reasoner"},
	}

	cascade := []string{"codex", "deepseek", "deepseek-pro"}
	result := filterGraylistedRunners(cascade, gl, profiles)

	// deepseek (deepseek-chat) should be filtered; deepseek-pro (deepseek-reasoner) should stay
	expected := []string{"codex", "deepseek-pro"}
	if fmt.Sprintf("%v", result) != fmt.Sprintf("%v", expected) {
		t.Fatalf("expected %v, got %v", expected, result)
	}
}

func TestIsFalsePositive_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	if !isFalsePositive(dir) {
		t.Fatal("empty dir should be false positive")
	}
}

func TestIsFalsePositive_EmptyEventsFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(""), 0o644)
	if !isFalsePositive(dir) {
		t.Fatal("empty events.jsonl should be false positive")
	}
}

func TestIsFalsePositive_WithEvents(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(`{"type":"text","part":{"text":"hello"}}`+"\n"), 0o644)
	if isFalsePositive(dir) {
		t.Fatal("non-empty events.jsonl should not be false positive")
	}
}

func TestIsFalsePositive_AttemptSubdir(t *testing.T) {
	dir := t.TempDir()
	attemptDir := filepath.Join(dir, "attempt-2-claude")
	_ = os.MkdirAll(attemptDir, 0o755)
	_ = os.WriteFile(filepath.Join(attemptDir, "events.jsonl"), []byte(`{"type":"text"}`+"\n"), 0o644)
	if isFalsePositive(dir) {
		t.Fatal("events in attempt subdir should not be false positive")
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
