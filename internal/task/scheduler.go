package task

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
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
	FailFast bool                                // stop spawning on first failure
}

// Scheduler manages dependency-aware parallel task execution.
type Scheduler struct {
	cfg         SchedulerConfig
	graph       *Graph
	results     map[string]*TaskResult
	mu          sync.Mutex
	stopping    atomic.Bool  // set when fail-fast triggered
	rateLimited atomic.Bool  // set when rate limit detected
	inflight    atomic.Int64 // tracks tasks enqueued or executing
	doneCh      chan struct{}
	doneOnce    sync.Once

	// Per-task cancel support for interactive control.
	work       chan string
	taskCancel map[string]context.CancelFunc
	finished   atomic.Bool // true after Run() exits
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
		cfg:        cfg,
		graph:      graph,
		results:    results,
		taskCancel: make(map[string]context.CancelFunc),
	}
}

// Run executes all tasks respecting dependencies and parallelism limits.
// Returns all results when complete.
func (s *Scheduler) Run(ctx context.Context) map[string]*TaskResult {
	var wg sync.WaitGroup
	work := make(chan string, len(s.graph.Tasks()))
	s.work = work
	s.doneCh = make(chan struct{})

	// start workers
	for i := 0; i < s.cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range work {
				if s.stopping.Load() {
					s.mu.Lock()
					r := s.results[id]
					if r.State == StateReady {
						if s.rateLimited.Load() {
							r.State = StateRateLimited
							r.Error = "rate limit reached"
						} else {
							r.State = StateSkipped
							r.Error = "fail-fast: stopped after failure"
						}
					}
					s.mu.Unlock()
					s.notify(id)
					s.decInflight()
					continue
				}
				s.execute(ctx, id, work)
				s.decInflight()
			}
		}()
	}

	// enqueue roots
	roots := s.graph.Roots()
	for _, id := range roots {
		s.setState(id, StateReady)
		s.inflight.Add(1)
		work <- id
	}

	// if no roots (empty graph), signal done immediately
	if len(roots) == 0 {
		s.doneOnce.Do(func() { close(s.doneCh) })
	}

	// wait for all tasks to reach terminal state
	<-s.doneCh
	s.finished.Store(true)
	close(work)
	wg.Wait()

	return s.results
}

// decInflight decrements the inflight counter and signals done when it reaches zero.
func (s *Scheduler) decInflight() {
	if s.inflight.Add(-1) <= 0 {
		s.doneOnce.Do(func() { close(s.doneCh) })
	}
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

// SetRunnerUsed updates the runner name on a running task's result.
// Called from the cascade callback to make the active runner visible to the TUI.
func (s *Scheduler) SetRunnerUsed(id, runner string) {
	s.mu.Lock()
	if r, ok := s.results[id]; ok {
		r.RunnerUsed = runner
	}
	s.mu.Unlock()
}

// SetRunning transitions a task from Waiting to Running and sets StartedAt.
// Called by ExecFn after acquiring the repo lock so the TUI shows actual
// execution time, not lock wait time.
func (s *Scheduler) SetRunning(id string) {
	s.mu.Lock()
	if r, ok := s.results[id]; ok {
		r.State = StateRunning
		r.StartedAt = time.Now()
	}
	s.mu.Unlock()
	s.notify(id)
}

// CancelTask cancels a running task by invoking its per-task context cancel.
// No-op if the task is not running.
func (s *Scheduler) CancelTask(id string) {
	s.mu.Lock()
	cancel, ok := s.taskCancel[id]
	r := s.results[id]
	isRunning := r != nil && (r.State == StateRunning || r.State == StateWaiting)
	s.mu.Unlock()

	if ok && isRunning {
		cancel()
	}
}

// RequeueTask resets a failed/skipped/rate-limited task to pending and
// re-enqueues it for execution. Optionally overrides the runner.
// No-op if the task is running, completed, or the scheduler has finished.
func (s *Scheduler) RequeueTask(id, runner string) {
	if s.finished.Load() {
		return
	}

	s.mu.Lock()
	r, ok := s.results[id]
	if !ok {
		s.mu.Unlock()
		return
	}
	// Only requeue terminal non-success states.
	switch r.State {
	case StateFailed, StateSkipped, StateRateLimited:
		// ok to requeue
	default:
		s.mu.Unlock()
		return
	}

	// Reset result state.
	r.State = StateReady
	r.Error = ""
	r.StartedAt = time.Time{}
	r.EndedAt = time.Time{}
	r.Duration = 0
	r.Attempts = nil
	r.FalsePositive = false
	r.RunnerUsed = ""

	// Apply runner override to the graph task.
	if runner != "" {
		if t := s.graph.Task(id); t != nil {
			t.Runner = runner
		}
	}
	s.mu.Unlock()
	s.notify(id)

	// Re-enqueue via goroutine to avoid blocking TUI if channel is full.
	s.inflight.Add(1)
	go func() { s.work <- id }()
}

func (s *Scheduler) execute(ctx context.Context, id string, work chan<- string) {
	task := s.graph.Task(id)
	if task == nil {
		s.setFailed(id, "task not found in graph")
		return
	}

	// Create per-task cancellable context for interactive control.
	taskCtx, taskCancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.taskCancel[id] = taskCancel
	s.results[id].State = StateWaiting
	s.mu.Unlock()
	s.notify(id)

	var repoDir string
	if filepath.IsAbs(task.Repo) {
		repoDir = task.Repo
	} else {
		name := repoName(task.Repo)
		if filepath.Base(s.cfg.ReposDir) == name {
			repoDir = s.cfg.ReposDir
		} else {
			repoDir = fmt.Sprintf("%s/%s", s.cfg.ReposDir, name)
		}
	}
	outputDir := fmt.Sprintf("%s/%s", s.cfg.RunDir, id)

	result := s.cfg.ExecFn(taskCtx, task, repoDir, outputDir)
	taskCancel() // release resources

	s.mu.Lock()
	result.TaskID = id
	s.results[id] = result
	delete(s.taskCancel, id)
	s.mu.Unlock()
	s.notify(id)

	// handle dependents
	switch result.State {
	case StateCompleted:
		s.unlockChildren(id, work)
	case StateRateLimited:
		s.rateLimited.Store(true)
		s.stopping.Store(true)
		s.rateLimitRemaining(result.ResetsAt)
	default:
		if s.cfg.FailFast {
			s.stopping.Store(true)
		}
		s.skipDependents(id)
	}
}

func (s *Scheduler) unlockChildren(id string, work chan<- string) {
	children := s.graph.Children(id)
	for _, childID := range children {
		// fail-fast or rate-limit: skip new tasks when stopping
		if s.stopping.Load() {
			s.mu.Lock()
			r := s.results[childID]
			if r.State == StatePending || r.State == StateReady {
				if s.rateLimited.Load() {
					r.State = StateRateLimited
					r.Error = "rate limit reached"
				} else {
					r.State = StateSkipped
					r.Error = "fail-fast: stopped after failure"
				}
			}
			s.mu.Unlock()
			s.notify(childID)
			continue
		}

		s.mu.Lock()
		r := s.results[childID]
		shouldStart := false
		if r.State == StatePending {
			allDone := true
			for _, parentID := range s.graph.Deps(childID) {
				if s.results[parentID].State != StateCompleted {
					allDone = false
					break
				}
			}
			if allDone {
				r.State = StateReady
				shouldStart = true
			}
		}
		s.mu.Unlock()

		if shouldStart {
			s.notify(childID)
			s.inflight.Add(1)
			work <- childID
		}
	}
}

// rateLimitRemaining marks all non-terminal tasks as rate-limited.
func (s *Scheduler) rateLimitRemaining(resetsAt time.Time) {
	s.mu.Lock()
	for _, r := range s.results {
		if r.State == StatePending || r.State == StateReady {
			r.State = StateRateLimited
			r.ResetsAt = resetsAt
			r.Error = "rate limit reached"
		}
	}
	s.mu.Unlock()
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
