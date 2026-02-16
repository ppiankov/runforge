package task

import "time"

// TaskState represents the execution state of a task.
type TaskState int

const (
	StatePending TaskState = iota
	StateReady
	StateRunning
	StateCompleted
	StateFailed
	StateSkipped // dependency failed
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
	default:
		return "UNKNOWN"
	}
}

// Task represents a single work order from the tasks file.
type Task struct {
	ID        string `json:"id"`
	Repo      string `json:"repo"`
	Priority  int    `json:"priority"`
	DependsOn string `json:"depends_on,omitempty"`
	Title     string `json:"title"`
	Prompt    string `json:"prompt"`
	Runner    string `json:"runner,omitempty"` // default: "codex"
}

// TaskFile is the top-level structure of the tasks JSON file.
type TaskFile struct {
	Description  string   `json:"description,omitempty"`
	Generated    string   `json:"generated,omitempty"`
	AllowedRepos []string `json:"allowed_repos,omitempty"`
	Tasks        []Task   `json:"tasks"`
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
}

// RunReport is the final output of a codexrun execution.
type RunReport struct {
	Timestamp     time.Time              `json:"timestamp"`
	TasksFile     string                 `json:"tasks_file"`
	Workers       int                    `json:"workers"`
	Filter        string                 `json:"filter,omitempty"`
	ReposDir      string                 `json:"repos_dir"`
	Results       map[string]*TaskResult `json:"results"`
	TotalTasks    int                    `json:"total_tasks"`
	Completed     int                    `json:"completed"`
	Failed        int                    `json:"failed"`
	Skipped       int                    `json:"skipped"`
	TotalDuration time.Duration          `json:"total_duration"`
}
