package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ppiankov/runforge/internal/runner"
	"github.com/ppiankov/runforge/internal/task"
)

const (
	reviewPoolBuffer = 64
	maxOutputRead    = 32 * 1024 // 32KB max for review prompt
)

// ReviewPool runs async reviews of completed tasks.
type ReviewPool struct {
	runners    map[string]runner.Runner
	blacklist  *runner.RunnerBlacklist
	config     *task.ReviewConfig
	maxRuntime time.Duration
	jobs       chan reviewJob
	wg         sync.WaitGroup
	mu         sync.Mutex
	results    map[string]*task.ReviewResult
	ctx        context.Context
}

type reviewJob struct {
	taskID string
	task   *task.Task
	result *task.TaskResult
	runDir string
}

// NewReviewPool creates a review pool with the given configuration.
func NewReviewPool(
	config *task.ReviewConfig,
	runners map[string]runner.Runner,
	blacklist *runner.RunnerBlacklist,
	maxRuntime time.Duration,
	workers int,
) *ReviewPool {
	return &ReviewPool{
		runners:    runners,
		blacklist:  blacklist,
		config:     config,
		maxRuntime: maxRuntime,
		jobs:       make(chan reviewJob, reviewPoolBuffer),
		results:    make(map[string]*task.ReviewResult),
	}
}

// Start spawns worker goroutines that consume review jobs.
func (p *ReviewPool) Start(ctx context.Context, workers int) {
	p.ctx = ctx
	for range workers {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for job := range p.jobs {
				p.runReview(job)
			}
		}()
	}
}

// Submit sends a review job. The pool decides whether the task needs review.
func (p *ReviewPool) Submit(job reviewJob) {
	if p.config.FallbackOnly && len(job.result.Attempts) <= 1 {
		return
	}
	select {
	case p.jobs <- job:
	default:
		slog.Warn("review pool full, dropping review", "task", job.taskID)
	}
}

// Wait closes the jobs channel and waits for all reviews to finish.
func (p *ReviewPool) Wait() {
	close(p.jobs)
	p.wg.Wait()
}

// ApplyResults attaches review results to the corresponding task results.
func (p *ReviewPool) ApplyResults(results map[string]*task.TaskResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, review := range p.results {
		if r, ok := results[id]; ok {
			r.Review = review
		}
	}
}

func (p *ReviewPool) runReview(job reviewJob) {
	reviewer := p.pickReviewer(job.result.RunnerUsed)
	if reviewer == "" {
		slog.Warn("no reviewer available", "task", job.taskID)
		p.storeResult(job.taskID, &task.ReviewResult{
			Error: "no reviewer available",
		})
		return
	}

	r, ok := p.runners[reviewer]
	if !ok {
		p.storeResult(job.taskID, &task.ReviewResult{
			Runner: reviewer,
			Error:  fmt.Sprintf("reviewer %q not in registry", reviewer),
		})
		return
	}

	// read the output from the successful attempt
	output := p.readTaskOutput(job.result)
	if output == "" {
		slog.Debug("no output to review", "task", job.taskID)
		p.storeResult(job.taskID, &task.ReviewResult{
			Runner: reviewer,
			Error:  "no output to review",
		})
		return
	}

	reviewDir := filepath.Join(job.runDir, job.taskID, "review")
	if err := os.MkdirAll(reviewDir, 0o755); err != nil {
		p.storeResult(job.taskID, &task.ReviewResult{
			Runner: reviewer,
			Error:  fmt.Sprintf("create review dir: %v", err),
		})
		return
	}

	prompt := buildReviewPrompt(job.task.Title, output)
	reviewTask := &task.Task{
		ID:     job.taskID + "-review",
		Repo:   job.task.Repo,
		Prompt: prompt,
	}

	repoDir := filepath.Join(filepath.Dir(job.runDir), filepath.Base(job.task.Repo))

	ctx, cancel := context.WithTimeout(p.ctx, p.maxRuntime)
	start := time.Now()
	result := r.Run(ctx, reviewTask, repoDir, reviewDir)
	cancel()
	elapsed := time.Since(start)

	passed := parseReviewVerdict(result.LastMsg)

	p.storeResult(job.taskID, &task.ReviewResult{
		Runner:   reviewer,
		Passed:   passed,
		Summary:  truncate(result.LastMsg, 500),
		Duration: elapsed,
	})

	slog.Info("review complete", "task", job.taskID, "reviewer", reviewer, "passed", passed)
}

func (p *ReviewPool) pickReviewer(runnerUsed string) string {
	// prefer explicit reviewer from config
	if p.config.Runner != "" {
		if !p.blacklist.IsBlocked(p.config.Runner) {
			return p.config.Runner
		}
	}

	// auto-pick: try built-in trusted runners that differ from the one that did the work
	preferred := []string{"codex", "claude"}
	for _, name := range preferred {
		if name == runnerUsed {
			continue
		}
		if _, ok := p.runners[name]; !ok {
			continue
		}
		if p.blacklist.IsBlocked(name) {
			continue
		}
		return name
	}
	return ""
}

func (p *ReviewPool) readTaskOutput(result *task.TaskResult) string {
	// try LastMsg first (always available for claude runner)
	if result.LastMsg != "" {
		if len(result.LastMsg) > maxOutputRead {
			return result.LastMsg[:maxOutputRead]
		}
		return result.LastMsg
	}

	// try output.md from the successful attempt's output dir
	outputDir := result.OutputDir
	// check if last successful attempt has a different dir
	for i := len(result.Attempts) - 1; i >= 0; i-- {
		if result.Attempts[i].State == task.StateCompleted && result.Attempts[i].OutputDir != "" {
			outputDir = result.Attempts[i].OutputDir
			break
		}
	}

	if outputDir == "" {
		return ""
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "output.md"))
	if err != nil {
		return ""
	}
	if len(data) > maxOutputRead {
		data = data[:maxOutputRead]
	}
	return string(data)
}

func (p *ReviewPool) storeResult(taskID string, review *task.ReviewResult) {
	p.mu.Lock()
	p.results[taskID] = review
	p.mu.Unlock()
}

func buildReviewPrompt(title, output string) string {
	return fmt.Sprintf(
		"Review this code change for correctness, security issues, and test coverage. "+
			"The task was: %q. Output PASS or FAIL as the first word, followed by a one-paragraph summary.\n\n"+
			"--- Output to review ---\n%s",
		title, output,
	)
}

// parseReviewVerdict checks if the review output starts with PASS.
func parseReviewVerdict(msg string) bool {
	msg = strings.TrimSpace(msg)
	upper := strings.ToUpper(msg)
	return strings.HasPrefix(upper, "PASS")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
