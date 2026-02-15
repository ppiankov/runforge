package task

import "fmt"

// Graph represents a directed acyclic graph of tasks.
type Graph struct {
	tasks    map[string]*Task
	deps     map[string]string   // child → parent (depends_on)
	children map[string][]string // parent → children
	order    []string            // topological order
}

// BuildGraph creates a dependency graph from a list of tasks.
// Returns an error if the graph contains cycles.
func BuildGraph(tasks []Task) (*Graph, error) {
	g := &Graph{
		tasks:    make(map[string]*Task, len(tasks)),
		deps:     make(map[string]string),
		children: make(map[string][]string),
	}

	for i := range tasks {
		t := &tasks[i]
		g.tasks[t.ID] = t
		if t.DependsOn != "" {
			g.deps[t.ID] = t.DependsOn
			g.children[t.DependsOn] = append(g.children[t.DependsOn], t.ID)
		}
	}

	order, err := g.topoSort()
	if err != nil {
		return nil, err
	}
	g.order = order

	return g, nil
}

// Order returns tasks in topological order (dependencies first).
// Within the same dependency level, sorted by priority ascending then ID.
func (g *Graph) Order() []string {
	return g.order
}

// Roots returns task IDs with no dependencies.
func (g *Graph) Roots() []string {
	var roots []string
	for id := range g.tasks {
		if _, hasDep := g.deps[id]; !hasDep {
			roots = append(roots, id)
		}
	}
	sortIDs(roots, g.tasks)
	return roots
}

// Children returns IDs that depend on the given task.
func (g *Graph) Children(id string) []string {
	return g.children[id]
}

// Task returns the task for an ID.
func (g *Graph) Task(id string) *Task {
	return g.tasks[id]
}

// Tasks returns all tasks in the graph.
func (g *Graph) Tasks() map[string]*Task {
	return g.tasks
}

// Dependents returns all transitive dependents of a task.
func (g *Graph) Dependents(id string) []string {
	var result []string
	visited := make(map[string]struct{})
	g.collectDependents(id, visited, &result)
	return result
}

func (g *Graph) collectDependents(id string, visited map[string]struct{}, result *[]string) {
	for _, child := range g.children[id] {
		if _, seen := visited[child]; seen {
			continue
		}
		visited[child] = struct{}{}
		*result = append(*result, child)
		g.collectDependents(child, visited, result)
	}
}

// topoSort performs Kahn's algorithm for topological sorting.
func (g *Graph) topoSort() ([]string, error) {
	inDegree := make(map[string]int, len(g.tasks))
	for id := range g.tasks {
		inDegree[id] = 0
	}
	for id, dep := range g.deps {
		_ = id
		_ = dep
		inDegree[id]++
	}

	// seed queue with zero in-degree nodes
	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	sortIDs(queue, g.tasks)

	var order []string
	for len(queue) > 0 {
		// pick first (deterministic — sorted by priority, then ID)
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)

		for _, child := range g.children[id] {
			inDegree[child]--
			if inDegree[child] == 0 {
				queue = append(queue, child)
				sortIDs(queue, g.tasks)
			}
		}
	}

	if len(order) != len(g.tasks) {
		return nil, fmt.Errorf("dependency cycle detected: processed %d of %d tasks", len(order), len(g.tasks))
	}

	return order, nil
}

// sortIDs sorts task IDs by priority ascending, then ID lexicographic.
func sortIDs(ids []string, tasks map[string]*Task) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0; j-- {
			a, b := tasks[ids[j]], tasks[ids[j-1]]
			if a.Priority < b.Priority || (a.Priority == b.Priority && ids[j] < ids[j-1]) {
				ids[j], ids[j-1] = ids[j-1], ids[j]
			}
		}
	}
}
