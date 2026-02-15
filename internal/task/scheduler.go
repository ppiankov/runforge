package task

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ExecFn is the function signature for executing a task.
// Implementations spawn codex exec and return the result.
type ExecFn func(ctx context.Context, t *Task, repoDir string, outputDir string) *TaskResult

// SchedulerConfig holds scheduler parameters.
type SchedulerConfig struct {
	Workers  int
	ReposDir string
	RunDir   string
	ExecFn   ExecFn
	OnUpdate func(id string, result *TaskResult) // called on state changes
}

// Scheduler manages dependency-aware parallel task execution.
type Scheduler struct {
	cfg     SchedulerConfig
	graph   *Graph
	results map[string]*TaskResult
	mu      sync.Mutex
}

// NewScheduler creates a scheduler for the given task graph.
func NewScheduler(graph *Graph, cfg SchedulerConfig) *Scheduler {
	results := make(map[string]*TaskResult, len(graph.Tasks()))
	for id := range graph.Tasks() {
		results[id] = &TaskResult{
			TaskID: id,
			State:  StatePending,
		}
	}

	return &Scheduler{
		cfg:     cfg,
		graph:   graph,
		results: results,
	}
}

// Run executes all tasks respecting dependencies and parallelism limits.
// Returns all results when complete.
func (s *Scheduler) Run(ctx context.Context) map[string]*TaskResult {
	var wg sync.WaitGroup
	work := make(chan string, len(s.graph.Tasks()))

	// start workers
	for i := 0; i < s.cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range work {
				s.execute(ctx, id)
			}
		}()
	}

	// enqueue roots
	roots := s.graph.Roots()
	for _, id := range roots {
		s.setState(id, StateReady)
		work <- id
	}

	// wait for completion using a done channel
	done := make(chan struct{})
	go func() {
		// poll until all tasks are terminal
		for {
			s.mu.Lock()
			allDone := true
			for _, r := range s.results {
				if r.State == StatePending || r.State == StateReady || r.State == StateRunning {
					allDone = false
					break
				}
			}
			s.mu.Unlock()

			if allDone {
				close(done)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	<-done
	close(work)
	wg.Wait()

	return s.results
}

// Results returns the current state of all task results.
func (s *Scheduler) Results() map[string]*TaskResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make(map[string]*TaskResult, len(s.results))
	for k, v := range s.results {
		cpy := *v
		cp[k] = &cpy
	}
	return cp
}

func (s *Scheduler) execute(ctx context.Context, id string) {
	task := s.graph.Task(id)
	if task == nil {
		s.setFailed(id, "task not found in graph")
		return
	}

	s.mu.Lock()
	s.results[id].State = StateRunning
	s.results[id].StartedAt = time.Now()
	s.mu.Unlock()
	s.notify(id)

	repoDir := fmt.Sprintf("%s/%s", s.cfg.ReposDir, repoName(task.Repo))
	outputDir := fmt.Sprintf("%s/%s", s.cfg.RunDir, id)

	result := s.cfg.ExecFn(ctx, task, repoDir, outputDir)

	s.mu.Lock()
	result.TaskID = id
	s.results[id] = result
	s.mu.Unlock()
	s.notify(id)

	// handle dependents
	if result.State == StateCompleted {
		s.unlockChildren(id)
	} else {
		s.skipDependents(id)
	}
}

func (s *Scheduler) unlockChildren(id string) {
	children := s.graph.Children(id)
	for _, childID := range children {
		s.mu.Lock()
		r := s.results[childID]
		if r.State == StatePending {
			// check if all deps are satisfied (for future multi-dep support)
			r.State = StateReady
		}
		s.mu.Unlock()

		if r.State == StateReady {
			s.notify(childID)
			// spawn inline — workers are already running
			go s.execute(context.Background(), childID)
		}
	}
}

func (s *Scheduler) skipDependents(id string) {
	dependents := s.graph.Dependents(id)
	for _, depID := range dependents {
		s.mu.Lock()
		r := s.results[depID]
		if r.State == StatePending || r.State == StateReady {
			r.State = StateSkipped
			r.Error = fmt.Sprintf("dependency %q failed", id)
		}
		s.mu.Unlock()
		s.notify(depID)
	}
}

func (s *Scheduler) setState(id string, state TaskState) {
	s.mu.Lock()
	s.results[id].State = state
	s.mu.Unlock()
}

func (s *Scheduler) setFailed(id string, msg string) {
	s.mu.Lock()
	s.results[id].State = StateFailed
	s.results[id].Error = msg
	s.results[id].EndedAt = time.Now()
	s.mu.Unlock()
	s.notify(id)
	s.skipDependents(id)
}

func (s *Scheduler) notify(id string) {
	if s.cfg.OnUpdate != nil {
		s.mu.Lock()
		cpy := *s.results[id]
		s.mu.Unlock()
		s.cfg.OnUpdate(id, &cpy)
	}
}

// repoName extracts the repo name from "owner/name".
func repoName(repo string) string {
	for i := len(repo) - 1; i >= 0; i-- {
		if repo[i] == '/' {
			return repo[i+1:]
		}
	}
	return repo
}
