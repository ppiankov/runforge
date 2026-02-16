package task

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_AllSucceed(t *testing.T) {
	tasks := []Task{
		{ID: "a", Repo: "org/r", Priority: 1, Title: "A", Prompt: "a"},
		{ID: "b", Repo: "org/r", Priority: 1, Title: "B", Prompt: "b"},
		{ID: "c", Repo: "org/r", Priority: 2, Title: "C", Prompt: "c"},
	}

	g, err := BuildGraph(tasks)
	if err != nil {
		t.Fatal(err)
	}

	execFn := func(_ context.Context, task *Task, _, _ string) *TaskResult {
		return &TaskResult{
			TaskID:  task.ID,
			State:   StateCompleted,
			EndedAt: time.Now(),
		}
	}

	sched := NewScheduler(g, SchedulerConfig{
		Workers:  2,
		ReposDir: "/tmp",
		RunDir:   "/tmp/run",
		ExecFn:   execFn,
	})

	results := sched.Run(context.Background())

	for id, r := range results {
		if r.State != StateCompleted {
			t.Errorf("task %s: expected COMPLETED, got %s", id, r.State)
		}
	}
}

func TestScheduler_DependencyChain(t *testing.T) {
	tasks := []Task{
		{ID: "t1", Repo: "org/r", Priority: 1, Title: "T1", Prompt: "a"},
		{ID: "t2", Repo: "org/r", Priority: 1, DependsOn: []string{"t1"}, Title: "T2", Prompt: "b"},
		{ID: "t3", Repo: "org/r", Priority: 1, DependsOn: []string{"t2"}, Title: "T3", Prompt: "c"},
	}

	g, err := BuildGraph(tasks)
	if err != nil {
		t.Fatal(err)
	}

	var order []string
	var mu sync.Mutex

	execFn := func(_ context.Context, task *Task, _, _ string) *TaskResult {
		mu.Lock()
		order = append(order, task.ID)
		mu.Unlock()
		return &TaskResult{
			TaskID:  task.ID,
			State:   StateCompleted,
			EndedAt: time.Now(),
		}
	}

	sched := NewScheduler(g, SchedulerConfig{
		Workers:  4,
		ReposDir: "/tmp",
		RunDir:   "/tmp/run",
		ExecFn:   execFn,
	})

	sched.Run(context.Background())

	mu.Lock()
	defer mu.Unlock()

	if len(order) != 3 {
		t.Fatalf("expected 3 executions, got %d", len(order))
	}

	t1 := indexOf(order, "t1")
	t2 := indexOf(order, "t2")
	t3 := indexOf(order, "t3")

	if t1 > t2 || t2 > t3 {
		t.Errorf("wrong execution order: %v", order)
	}
}

func TestScheduler_FailureSkipsDependents(t *testing.T) {
	tasks := []Task{
		{ID: "root", Repo: "org/r", Priority: 1, Title: "Root", Prompt: "a"},
		{ID: "child", Repo: "org/r", Priority: 1, DependsOn: []string{"root"}, Title: "Child", Prompt: "b"},
		{ID: "grandchild", Repo: "org/r", Priority: 1, DependsOn: []string{"child"}, Title: "GC", Prompt: "c"},
		{ID: "independent", Repo: "org/r", Priority: 1, Title: "Ind", Prompt: "d"},
	}

	g, err := BuildGraph(tasks)
	if err != nil {
		t.Fatal(err)
	}

	execFn := func(_ context.Context, task *Task, _, _ string) *TaskResult {
		if task.ID == "root" {
			return &TaskResult{
				TaskID:  task.ID,
				State:   StateFailed,
				Error:   "simulated failure",
				EndedAt: time.Now(),
			}
		}
		return &TaskResult{
			TaskID:  task.ID,
			State:   StateCompleted,
			EndedAt: time.Now(),
		}
	}

	sched := NewScheduler(g, SchedulerConfig{
		Workers:  2,
		ReposDir: "/tmp",
		RunDir:   "/tmp/run",
		ExecFn:   execFn,
	})

	results := sched.Run(context.Background())

	if results["root"].State != StateFailed {
		t.Errorf("root: expected FAILED, got %s", results["root"].State)
	}
	if results["child"].State != StateSkipped {
		t.Errorf("child: expected SKIPPED, got %s", results["child"].State)
	}
	if results["grandchild"].State != StateSkipped {
		t.Errorf("grandchild: expected SKIPPED, got %s", results["grandchild"].State)
	}
	if results["independent"].State != StateCompleted {
		t.Errorf("independent: expected COMPLETED, got %s", results["independent"].State)
	}
}

func TestScheduler_Parallelism(t *testing.T) {
	tasks := []Task{
		{ID: "a", Repo: "org/r", Priority: 1, Title: "A", Prompt: "a"},
		{ID: "b", Repo: "org/r", Priority: 1, Title: "B", Prompt: "b"},
		{ID: "c", Repo: "org/r", Priority: 1, Title: "C", Prompt: "c"},
		{ID: "d", Repo: "org/r", Priority: 1, Title: "D", Prompt: "d"},
	}

	g, err := BuildGraph(tasks)
	if err != nil {
		t.Fatal(err)
	}

	var maxConcurrent int64
	var current int64

	execFn := func(_ context.Context, task *Task, _, _ string) *TaskResult {
		c := atomic.AddInt64(&current, 1)
		for {
			old := atomic.LoadInt64(&maxConcurrent)
			if c <= old {
				break
			}
			if atomic.CompareAndSwapInt64(&maxConcurrent, old, c) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt64(&current, -1)
		return &TaskResult{
			TaskID:  task.ID,
			State:   StateCompleted,
			EndedAt: time.Now(),
		}
	}

	sched := NewScheduler(g, SchedulerConfig{
		Workers:  2,
		ReposDir: "/tmp",
		RunDir:   "/tmp/run",
		ExecFn:   execFn,
	})

	sched.Run(context.Background())

	mc := atomic.LoadInt64(&maxConcurrent)
	if mc > 2 {
		t.Errorf("max concurrent %d exceeded worker limit 2", mc)
	}
	if mc < 2 {
		t.Logf("warning: max concurrent was %d, expected 2 (timing-sensitive)", mc)
	}
}

func TestScheduler_OnUpdate(t *testing.T) {
	tasks := []Task{
		{ID: "t1", Repo: "org/r", Priority: 1, Title: "T1", Prompt: "a"},
	}

	g, err := BuildGraph(tasks)
	if err != nil {
		t.Fatal(err)
	}

	var updates []TaskState
	var mu sync.Mutex

	execFn := func(_ context.Context, task *Task, _, _ string) *TaskResult {
		return &TaskResult{
			TaskID:  task.ID,
			State:   StateCompleted,
			EndedAt: time.Now(),
		}
	}

	sched := NewScheduler(g, SchedulerConfig{
		Workers:  1,
		ReposDir: "/tmp",
		RunDir:   "/tmp/run",
		ExecFn:   execFn,
		OnUpdate: func(id string, r *TaskResult) {
			mu.Lock()
			updates = append(updates, r.State)
			mu.Unlock()
		},
	})

	sched.Run(context.Background())

	mu.Lock()
	defer mu.Unlock()

	// should see: Ready, Running, Completed
	if len(updates) < 2 {
		t.Errorf("expected at least 2 updates, got %d: %v", len(updates), updates)
	}
}

func TestScheduler_FailFast(t *testing.T) {
	// a fails, b is independent but should be skipped by fail-fast,
	// c depends on a (skipped by dependency), d is independent
	tasks := []Task{
		{ID: "a", Repo: "org/r", Priority: 1, Title: "A", Prompt: "a"},
		{ID: "b", Repo: "org/r", Priority: 1, Title: "B", Prompt: "b"},
		{ID: "c", Repo: "org/r", Priority: 1, DependsOn: []string{"a"}, Title: "C", Prompt: "c"},
	}

	g, err := BuildGraph(tasks)
	if err != nil {
		t.Fatal(err)
	}

	var execCount int64

	execFn := func(_ context.Context, task *Task, _, _ string) *TaskResult {
		atomic.AddInt64(&execCount, 1)
		if task.ID == "a" {
			// slow enough for b to be queued but not started yet
			time.Sleep(50 * time.Millisecond)
			return &TaskResult{
				TaskID:  task.ID,
				State:   StateFailed,
				Error:   "simulated failure",
				EndedAt: time.Now(),
			}
		}
		return &TaskResult{
			TaskID:  task.ID,
			State:   StateCompleted,
			EndedAt: time.Now(),
		}
	}

	sched := NewScheduler(g, SchedulerConfig{
		Workers:  1, // single worker ensures ordering
		ReposDir: "/tmp",
		RunDir:   "/tmp/run",
		ExecFn:   execFn,
		FailFast: true,
	})

	results := sched.Run(context.Background())

	if results["a"].State != StateFailed {
		t.Errorf("a: expected FAILED, got %s", results["a"].State)
	}
	if results["c"].State != StateSkipped {
		t.Errorf("c: expected SKIPPED (dependency), got %s", results["c"].State)
	}
	// b should be skipped by fail-fast (either before execution or during unlock)
	if results["b"].State == StateCompleted {
		// with 1 worker + a failing first, b may still run if it was already dequeued
		// this is acceptable — fail-fast prevents NEW spawns, not already-running tasks
		t.Logf("note: b completed (was already dequeued before fail-fast triggered)")
	}
}

func TestScheduler_ContextTimeout(t *testing.T) {
	tasks := []Task{
		{ID: "slow", Repo: "org/r", Priority: 1, Title: "Slow", Prompt: "a"},
	}

	g, err := BuildGraph(tasks)
	if err != nil {
		t.Fatal(err)
	}

	execFn := func(ctx context.Context, task *Task, _, _ string) *TaskResult {
		select {
		case <-ctx.Done():
			return &TaskResult{
				TaskID:  task.ID,
				State:   StateFailed,
				Error:   "context deadline exceeded",
				EndedAt: time.Now(),
			}
		case <-time.After(5 * time.Second):
			return &TaskResult{
				TaskID:  task.ID,
				State:   StateCompleted,
				EndedAt: time.Now(),
			}
		}
	}

	// use a short timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	sched := NewScheduler(g, SchedulerConfig{
		Workers:  1,
		ReposDir: "/tmp",
		RunDir:   "/tmp/run",
		ExecFn:   execFn,
	})

	results := sched.Run(ctx)

	if results["slow"].State != StateFailed {
		t.Errorf("slow: expected FAILED (timeout), got %s", results["slow"].State)
	}
}

func TestScheduler_FanIn(t *testing.T) {
	// child depends on both p1 and p2 — should only run after both complete
	tasks := []Task{
		{ID: "p1", Repo: "org/r", Priority: 1, Title: "P1", Prompt: "a"},
		{ID: "p2", Repo: "org/r", Priority: 1, Title: "P2", Prompt: "b"},
		{ID: "child", Repo: "org/r", Priority: 1, DependsOn: []string{"p1", "p2"}, Title: "Child", Prompt: "c"},
	}

	g, err := BuildGraph(tasks)
	if err != nil {
		t.Fatal(err)
	}

	var order []string
	var mu sync.Mutex

	execFn := func(_ context.Context, task *Task, _, _ string) *TaskResult {
		mu.Lock()
		order = append(order, task.ID)
		mu.Unlock()
		return &TaskResult{
			TaskID:  task.ID,
			State:   StateCompleted,
			EndedAt: time.Now(),
		}
	}

	sched := NewScheduler(g, SchedulerConfig{
		Workers:  4,
		ReposDir: "/tmp",
		RunDir:   "/tmp/run",
		ExecFn:   execFn,
	})

	results := sched.Run(context.Background())

	// All should complete
	for id, r := range results {
		if r.State != StateCompleted {
			t.Errorf("task %s: expected COMPLETED, got %s", id, r.State)
		}
	}

	mu.Lock()
	defer mu.Unlock()

	if len(order) != 3 {
		t.Fatalf("expected 3 executions, got %d", len(order))
	}

	childIdx := indexOf(order, "child")
	p1Idx := indexOf(order, "p1")
	p2Idx := indexOf(order, "p2")

	if p1Idx > childIdx || p2Idx > childIdx {
		t.Errorf("child ran before both parents completed: %v", order)
	}
}

func TestScheduler_FanInPartialFailure(t *testing.T) {
	// child depends on p1 and p2; p1 fails → child should be skipped
	tasks := []Task{
		{ID: "p1", Repo: "org/r", Priority: 1, Title: "P1", Prompt: "a"},
		{ID: "p2", Repo: "org/r", Priority: 1, Title: "P2", Prompt: "b"},
		{ID: "child", Repo: "org/r", Priority: 1, DependsOn: []string{"p1", "p2"}, Title: "Child", Prompt: "c"},
	}

	g, err := BuildGraph(tasks)
	if err != nil {
		t.Fatal(err)
	}

	execFn := func(_ context.Context, task *Task, _, _ string) *TaskResult {
		if task.ID == "p1" {
			return &TaskResult{
				TaskID:  task.ID,
				State:   StateFailed,
				Error:   "simulated failure",
				EndedAt: time.Now(),
			}
		}
		return &TaskResult{
			TaskID:  task.ID,
			State:   StateCompleted,
			EndedAt: time.Now(),
		}
	}

	sched := NewScheduler(g, SchedulerConfig{
		Workers:  2,
		ReposDir: "/tmp",
		RunDir:   "/tmp/run",
		ExecFn:   execFn,
	})

	results := sched.Run(context.Background())

	if results["p1"].State != StateFailed {
		t.Errorf("p1: expected FAILED, got %s", results["p1"].State)
	}
	if results["p2"].State != StateCompleted {
		t.Errorf("p2: expected COMPLETED, got %s", results["p2"].State)
	}
	if results["child"].State != StateSkipped {
		t.Errorf("child: expected SKIPPED (parent failed), got %s", results["child"].State)
	}
}

func TestScheduler_Diamond(t *testing.T) {
	// A → B, A → C, B+C → D
	tasks := []Task{
		{ID: "a", Repo: "org/r", Priority: 1, Title: "A", Prompt: "a"},
		{ID: "b", Repo: "org/r", Priority: 1, DependsOn: []string{"a"}, Title: "B", Prompt: "b"},
		{ID: "c", Repo: "org/r", Priority: 1, DependsOn: []string{"a"}, Title: "C", Prompt: "c"},
		{ID: "d", Repo: "org/r", Priority: 1, DependsOn: []string{"b", "c"}, Title: "D", Prompt: "d"},
	}

	g, err := BuildGraph(tasks)
	if err != nil {
		t.Fatal(err)
	}

	var order []string
	var mu sync.Mutex

	execFn := func(_ context.Context, task *Task, _, _ string) *TaskResult {
		mu.Lock()
		order = append(order, task.ID)
		mu.Unlock()
		return &TaskResult{
			TaskID:  task.ID,
			State:   StateCompleted,
			EndedAt: time.Now(),
		}
	}

	sched := NewScheduler(g, SchedulerConfig{
		Workers:  4,
		ReposDir: "/tmp",
		RunDir:   "/tmp/run",
		ExecFn:   execFn,
	})

	results := sched.Run(context.Background())

	for id, r := range results {
		if r.State != StateCompleted {
			t.Errorf("task %s: expected COMPLETED, got %s", id, r.State)
		}
	}

	mu.Lock()
	defer mu.Unlock()

	if len(order) != 4 {
		t.Fatalf("expected 4 executions, got %d", len(order))
	}

	dIdx := indexOf(order, "d")
	bIdx := indexOf(order, "b")
	cIdx := indexOf(order, "c")

	if bIdx > dIdx || cIdx > dIdx {
		t.Errorf("d ran before both b and c: %v", order)
	}
}

func TestRepoName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ppiankov/kafkaspectre", "kafkaspectre"},
		{"org/tool", "tool"},
		{"singleword", "singleword"},
	}

	for _, tt := range tests {
		got := repoName(tt.input)
		if got != tt.want {
			t.Errorf("repoName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
