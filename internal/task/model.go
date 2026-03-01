package task

import (
	"encoding/json"
	"time"
)

// TaskState represents the execution state of a task.
type TaskState int

const (
	StatePending TaskState = iota
	StateReady
	StateRunning
	StateCompleted
	StateFailed
	StateSkipped     // dependency failed
	StateRateLimited // API rate limit reached
)

func (s TaskState) String() string {
	switch s {
	case StatePending:
		return "PENDING"
	case StateReady:
		return "READY"
	case StateRunning:
		return "RUNNING"
	case StateCompleted:
		return "COMPLETED"
	case StateFailed:
		return "FAILED"
	case StateSkipped:
		return "SKIPPED"
	case StateRateLimited:
		return "RATE_LIMITED"
	default:
		return "UNKNOWN"
	}
}

// Task represents a single work order from the tasks file.
type Task struct {
	ID         string   `json:"id"`
	Repo       string   `json:"repo"`
	Priority   int      `json:"priority"`
	DependsOn  []string `json:"depends_on,omitempty"`
	Title      string   `json:"title"`
	Prompt     string   `json:"prompt"`
	Runner     string   `json:"runner,omitempty"`      // default: from TaskFile.DefaultRunner
	Fallbacks  []string `json:"fallbacks,omitempty"`   // runner profiles to try on failure/rate-limit
	SourceFile string   `json:"source_file,omitempty"` // populated during multi-file load
}

// UnmarshalJSON supports both string and array formats for depends_on.
// String: "depends_on": "task-a" → []string{"task-a"}
// Array:  "depends_on": ["task-a", "task-b"] → []string{"task-a", "task-b"}
func (t *Task) UnmarshalJSON(data []byte) error {
	type Alias Task
	aux := &struct {
		DependsOn json.RawMessage `json:"depends_on,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(t),
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	if len(aux.DependsOn) == 0 || string(aux.DependsOn) == "null" {
		return nil
	}

	// Try string first.
	var s string
	if err := json.Unmarshal(aux.DependsOn, &s); err == nil {
		if s != "" {
			t.DependsOn = []string{s}
		}
		return nil
	}

	// Try array.
	var arr []string
	if err := json.Unmarshal(aux.DependsOn, &arr); err != nil {
		return err
	}
	t.DependsOn = arr
	return nil
}

// RunnerProfileConfig defines a named runner profile with optional env overrides.
// Profiles allow the same runner type (e.g., "claude") to be used with different
// API endpoints or credentials (e.g., Z.ai proxy, direct API).
type RunnerProfileConfig struct {
	Type           string            `json:"type"`                      // "codex", "claude", "gemini", "opencode", "script"
	Model          string            `json:"model,omitempty"`           // model override passed via --model flag
	Profile        string            `json:"profile,omitempty"`         // codex --profile name (references config.toml)
	Env            map[string]string `json:"env,omitempty"`             // env overrides; "env:VAR" = read from OS
	DataCollection bool              `json:"data_collection,omitempty"` // true = prompts may be used for model training
	Free           bool              `json:"free,omitempty"`            // true = free-tier model, excluded from cascade by default
}

// TaskFile is the top-level structure of the tasks JSON file.
type TaskFile struct {
	Description      string                          `json:"description,omitempty"`
	Generated        string                          `json:"generated,omitempty"`
	AllowedRepos     []string                        `json:"allowed_repos,omitempty"`
	DefaultRunner    string                          `json:"default_runner,omitempty"`    // default: "codex"
	DefaultFallbacks []string                        `json:"default_fallbacks,omitempty"` // applied when task has no fallbacks
	Runners          map[string]*RunnerProfileConfig `json:"runners,omitempty"`           // named runner profiles
	Review           *ReviewConfig                   `json:"review,omitempty"`            // auto-review config
	Tasks            []Task                          `json:"tasks"`
}

// ReviewConfig controls automatic review of completed tasks.
type ReviewConfig struct {
	Enabled      bool   `json:"enabled"`
	Runner       string `json:"runner,omitempty"`        // explicit reviewer; if empty, auto-pick
	FallbackOnly bool   `json:"fallback_only,omitempty"` // only review tasks that used a fallback
}

// AttemptInfo records a single runner attempt within a fallback cascade.
type AttemptInfo struct {
	Runner            string        `json:"runner"`
	State             TaskState     `json:"state"`
	Duration          time.Duration `json:"duration"`
	Error             string        `json:"error,omitempty"`
	OutputDir         string        `json:"output_dir,omitempty"`
	ConnectivityError string        `json:"connectivity_error,omitempty"`
}

// TaskResult captures the outcome of executing a single task.
type TaskResult struct {
	TaskID    string        `json:"task_id"`
	State     TaskState     `json:"state"`
	StartedAt time.Time     `json:"started_at,omitempty"`
	EndedAt   time.Time     `json:"ended_at,omitempty"`
	Duration  time.Duration `json:"duration,omitempty"`
	OutputDir string        `json:"output_dir,omitempty"`
	LastMsg   string        `json:"last_message,omitempty"`
	Error     string        `json:"error,omitempty"`
	ResetsAt  time.Time     `json:"resets_at,omitempty"`

	ConnectivityError string `json:"connectivity_error,omitempty"` // TLS/DNS/connection error classification

	FalsePositive bool `json:"false_positive,omitempty"` // completed with 0 events (no real work)
	AutoCommitted bool `json:"auto_committed,omitempty"` // runforge committed changes the agent left unstaged

	RunnerUsed string        `json:"runner_used,omitempty"` // profile that produced the final result
	Attempts   []AttemptInfo `json:"attempts,omitempty"`    // all cascade attempts
	Review     *ReviewResult `json:"review,omitempty"`      // auto-review result
}

// ReviewResult captures the outcome of an automatic code review.
type ReviewResult struct {
	Runner   string        `json:"runner"`
	Passed   bool          `json:"passed"`
	Summary  string        `json:"summary,omitempty"`
	Duration time.Duration `json:"duration,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// RunReport is the final output of a runforge execution.
type RunReport struct {
	RunID          string                 `json:"run_id"`
	ParentRunID    string                 `json:"parent_run_id,omitempty"`
	Timestamp      time.Time              `json:"timestamp"`
	TasksFiles     []string               `json:"tasks_files"`
	Workers        int                    `json:"workers"`
	Filter         string                 `json:"filter,omitempty"`
	ReposDir       string                 `json:"repos_dir"`
	Results        map[string]*TaskResult `json:"results"`
	TotalTasks     int                    `json:"total_tasks"`
	Completed      int                    `json:"completed"`
	Failed         int                    `json:"failed"`
	Skipped        int                    `json:"skipped"`
	RateLimited    int                    `json:"rate_limited"`
	FalsePositives int                    `json:"false_positives,omitempty"`
	AutoCommits    int                    `json:"auto_commits,omitempty"`
	TotalDuration  time.Duration          `json:"total_duration"`
	ResetsAt       time.Time              `json:"resets_at,omitempty"`
}

// UnmarshalJSON supports both old ("tasks_file": "x.json") and new
// ("tasks_files": ["a.json"]) report formats for backward compatibility.
func (r *RunReport) UnmarshalJSON(data []byte) error {
	type Alias RunReport
	aux := &struct {
		TasksFile  string   `json:"tasks_file,omitempty"`
		TasksFiles []string `json:"tasks_files,omitempty"`
		*Alias
	}{Alias: (*Alias)(r)}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	if len(aux.TasksFiles) > 0 {
		r.TasksFiles = aux.TasksFiles
	} else if aux.TasksFile != "" {
		r.TasksFiles = []string{aux.TasksFile}
	}
	return nil
}
