package task

import (
	"strings"
	"testing"
)

func TestBuildGraph_NoDeps(t *testing.T) {
	tasks := []Task{
		{ID: "c", Repo: "org/r", Priority: 2, Title: "C", Prompt: "c"},
		{ID: "a", Repo: "org/r", Priority: 1, Title: "A", Prompt: "a"},
		{ID: "b", Repo: "org/r", Priority: 1, Title: "B", Prompt: "b"},
	}

	g, err := BuildGraph(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	order := g.Order()
	if len(order) != 3 {
		t.Fatalf("expected 3 tasks in order, got %d", len(order))
	}

	// priority 1 tasks should come before priority 2
	aIdx := indexOf(order, "a")
	bIdx := indexOf(order, "b")
	cIdx := indexOf(order, "c")

	if aIdx > cIdx {
		t.Error("a (priority 1) should come before c (priority 2)")
	}
	if bIdx > cIdx {
		t.Error("b (priority 1) should come before c (priority 2)")
	}
	// same priority: lexicographic
	if aIdx > bIdx {
		t.Error("a should come before b (same priority, lexicographic)")
	}
}

func TestBuildGraph_WithDeps(t *testing.T) {
	tasks := []Task{
		{ID: "t1", Repo: "org/r", Priority: 1, Title: "T1", Prompt: "a"},
		{ID: "t2", Repo: "org/r", Priority: 1, DependsOn: []string{"t1"}, Title: "T2", Prompt: "b"},
		{ID: "t3", Repo: "org/r", Priority: 1, DependsOn: []string{"t2"}, Title: "T3", Prompt: "c"},
	}

	g, err := BuildGraph(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	order := g.Order()
	if len(order) != 3 {
		t.Fatalf("expected 3, got %d", len(order))
	}

	t1 := indexOf(order, "t1")
	t2 := indexOf(order, "t2")
	t3 := indexOf(order, "t3")

	if t1 > t2 {
		t.Error("t1 must come before t2")
	}
	if t2 > t3 {
		t.Error("t2 must come before t3")
	}
}

func TestBuildGraph_Cycle(t *testing.T) {
	tasks := []Task{
		{ID: "a", Repo: "org/r", Priority: 1, DependsOn: []string{"b"}, Title: "A", Prompt: "a"},
		{ID: "b", Repo: "org/r", Priority: 1, DependsOn: []string{"a"}, Title: "B", Prompt: "b"},
	}

	_, err := BuildGraph(tasks)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected cycle in error, got: %v", err)
	}
}

func TestGraph_Roots(t *testing.T) {
	tasks := []Task{
		{ID: "root1", Repo: "org/r", Priority: 1, Title: "R1", Prompt: "a"},
		{ID: "root2", Repo: "org/r", Priority: 2, Title: "R2", Prompt: "b"},
		{ID: "child", Repo: "org/r", Priority: 1, DependsOn: []string{"root1"}, Title: "C", Prompt: "c"},
	}

	g, err := BuildGraph(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	roots := g.Roots()
	if len(roots) != 2 {
		t.Fatalf("expected 2 roots, got %d: %v", len(roots), roots)
	}
	if roots[0] != "root1" {
		t.Errorf("expected root1 first (lower priority), got %q", roots[0])
	}
}

func TestGraph_Children(t *testing.T) {
	tasks := []Task{
		{ID: "parent", Repo: "org/r", Priority: 1, Title: "P", Prompt: "a"},
		{ID: "c1", Repo: "org/r", Priority: 1, DependsOn: []string{"parent"}, Title: "C1", Prompt: "b"},
		{ID: "c2", Repo: "org/r", Priority: 1, DependsOn: []string{"parent"}, Title: "C2", Prompt: "c"},
	}

	g, err := BuildGraph(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	children := g.Children("parent")
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}
}

func TestGraph_Dependents(t *testing.T) {
	tasks := []Task{
		{ID: "a", Repo: "org/r", Priority: 1, Title: "A", Prompt: "a"},
		{ID: "b", Repo: "org/r", Priority: 1, DependsOn: []string{"a"}, Title: "B", Prompt: "b"},
		{ID: "c", Repo: "org/r", Priority: 1, DependsOn: []string{"b"}, Title: "C", Prompt: "c"},
		{ID: "d", Repo: "org/r", Priority: 1, Title: "D", Prompt: "d"},
	}

	g, err := BuildGraph(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deps := g.Dependents("a")
	if len(deps) != 2 {
		t.Fatalf("expected 2 transitive dependents of a, got %d: %v", len(deps), deps)
	}

	// d has no dependents
	deps = g.Dependents("d")
	if len(deps) != 0 {
		t.Errorf("expected 0 dependents of d, got %d", len(deps))
	}
}

func TestGraph_Task(t *testing.T) {
	tasks := []Task{
		{ID: "t1", Repo: "org/r", Priority: 1, Title: "Task 1", Prompt: "do stuff"},
	}

	g, err := BuildGraph(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := g.Task("t1")
	if got == nil {
		t.Fatal("expected task, got nil")
	}
	if got.Title != "Task 1" {
		t.Errorf("expected title 'Task 1', got %q", got.Title)
	}

	if g.Task("nonexistent") != nil {
		t.Error("expected nil for nonexistent task")
	}
}

func TestBuildGraph_FanIn(t *testing.T) {
	// Two parents → one child
	tasks := []Task{
		{ID: "p1", Repo: "org/r", Priority: 1, Title: "P1", Prompt: "a"},
		{ID: "p2", Repo: "org/r", Priority: 1, Title: "P2", Prompt: "b"},
		{ID: "child", Repo: "org/r", Priority: 1, DependsOn: []string{"p1", "p2"}, Title: "Child", Prompt: "c"},
	}

	g, err := BuildGraph(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	order := g.Order()
	if len(order) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(order))
	}

	childIdx := indexOf(order, "child")
	p1Idx := indexOf(order, "p1")
	p2Idx := indexOf(order, "p2")

	if p1Idx > childIdx {
		t.Error("p1 must come before child")
	}
	if p2Idx > childIdx {
		t.Error("p2 must come before child")
	}

	// child has 2 deps
	deps := g.Deps("child")
	if len(deps) != 2 {
		t.Errorf("expected 2 deps for child, got %d", len(deps))
	}

	// roots should be p1 and p2
	roots := g.Roots()
	if len(roots) != 2 {
		t.Fatalf("expected 2 roots, got %d: %v", len(roots), roots)
	}
}

func TestBuildGraph_Diamond(t *testing.T) {
	// A → B, A → C, B → D, C → D (diamond shape)
	tasks := []Task{
		{ID: "a", Repo: "org/r", Priority: 1, Title: "A", Prompt: "a"},
		{ID: "b", Repo: "org/r", Priority: 1, DependsOn: []string{"a"}, Title: "B", Prompt: "b"},
		{ID: "c", Repo: "org/r", Priority: 1, DependsOn: []string{"a"}, Title: "C", Prompt: "c"},
		{ID: "d", Repo: "org/r", Priority: 1, DependsOn: []string{"b", "c"}, Title: "D", Prompt: "d"},
	}

	g, err := BuildGraph(tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	order := g.Order()
	if len(order) != 4 {
		t.Fatalf("expected 4 tasks, got %d", len(order))
	}

	aIdx := indexOf(order, "a")
	bIdx := indexOf(order, "b")
	cIdx := indexOf(order, "c")
	dIdx := indexOf(order, "d")

	if aIdx > bIdx || aIdx > cIdx {
		t.Error("a must come before b and c")
	}
	if bIdx > dIdx || cIdx > dIdx {
		t.Error("b and c must come before d")
	}

	// d depends on both b and c
	deps := g.Deps("d")
	if len(deps) != 2 {
		t.Errorf("expected 2 deps for d, got %d", len(deps))
	}

	// a has 2 transitive dependents (b, c, d = 3)
	dependents := g.Dependents("a")
	if len(dependents) != 3 {
		t.Errorf("expected 3 transitive dependents of a, got %d: %v", len(dependents), dependents)
	}
}

func TestBuildGraph_MultiDepCycle(t *testing.T) {
	// a depends on c, b depends on a, c depends on b → cycle
	tasks := []Task{
		{ID: "a", Repo: "org/r", Priority: 1, DependsOn: []string{"c"}, Title: "A", Prompt: "a"},
		{ID: "b", Repo: "org/r", Priority: 1, DependsOn: []string{"a"}, Title: "B", Prompt: "b"},
		{ID: "c", Repo: "org/r", Priority: 1, DependsOn: []string{"b"}, Title: "C", Prompt: "c"},
	}

	_, err := BuildGraph(tasks)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected cycle in error, got: %v", err)
	}
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
