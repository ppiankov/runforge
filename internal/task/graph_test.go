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
		{ID: "t2", Repo: "org/r", Priority: 1, DependsOn: "t1", Title: "T2", Prompt: "b"},
		{ID: "t3", Repo: "org/r", Priority: 1, DependsOn: "t2", Title: "T3", Prompt: "c"},
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
		{ID: "a", Repo: "org/r", Priority: 1, DependsOn: "b", Title: "A", Prompt: "a"},
		{ID: "b", Repo: "org/r", Priority: 1, DependsOn: "a", Title: "B", Prompt: "b"},
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
		{ID: "child", Repo: "org/r", Priority: 1, DependsOn: "root1", Title: "C", Prompt: "c"},
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
		{ID: "c1", Repo: "org/r", Priority: 1, DependsOn: "parent", Title: "C1", Prompt: "b"},
		{ID: "c2", Repo: "org/r", Priority: 1, DependsOn: "parent", Title: "C2", Prompt: "c"},
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
		{ID: "b", Repo: "org/r", Priority: 1, DependsOn: "a", Title: "B", Prompt: "b"},
		{ID: "c", Repo: "org/r", Priority: 1, DependsOn: "b", Title: "C", Prompt: "c"},
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

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
