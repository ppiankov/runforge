package telemetry

import (
	"encoding/json"
	"time"

	"github.com/ppiankov/tokencontrol/internal/task"
)

// Record persists a completed run's data into the telemetry database.
func Record(db *DB, report *task.RunReport, tasks []task.Task, profiles map[string]*task.RunnerProfileConfig) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)

	// Insert run summary
	tasksFilesJSON, _ := json.Marshal(report.TasksFiles)
	totalCost := 0.0

	// First pass: compute total cost
	for _, t := range tasks {
		res := report.Results[t.ID]
		if res == nil {
			continue
		}
		model := modelForRunner(res.RunnerUsed, profiles)
		inputTokens, outputTokens := tokenCounts(res)
		totalCost += EstimateCost(model, inputTokens, outputTokens)
	}

	_, err = tx.Exec(`INSERT OR REPLACE INTO runs
		(run_id, tasks_files, workers, total_tasks, completed, failed, rate_limited, total_duration_ms, total_cost_usd, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		report.RunID, string(tasksFilesJSON), report.Workers,
		report.TotalTasks, report.Completed, report.Failed, report.RateLimited,
		report.TotalDuration.Milliseconds(), totalCost, now)
	if err != nil {
		return err
	}

	// Insert per-task rows
	for _, t := range tasks {
		res := report.Results[t.ID]
		if res == nil {
			continue
		}
		model := modelForRunner(res.RunnerUsed, profiles)
		inputTokens, outputTokens := tokenCounts(res)
		totalTokens := inputTokens + outputTokens
		cost := EstimateCost(model, inputTokens, outputTokens)
		cascadeStep := len(res.Attempts)
		if cascadeStep < 1 {
			cascadeStep = 1
		}

		_, err = tx.Exec(`INSERT OR REPLACE INTO task_executions
			(id, run_id, task_id, runner, model, state, difficulty, cascade_step,
			 input_tokens, output_tokens, total_tokens, cost_usd,
			 duration_ms, started_at, ended_at,
			 false_positive, auto_committed, attempts,
			 repo, task_title, tasks_file, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			report.RunID+"/"+t.ID, report.RunID, t.ID,
			res.RunnerUsed, model, res.State.String(), t.Difficulty, cascadeStep,
			inputTokens, outputTokens, totalTokens, cost,
			res.Duration.Milliseconds(),
			formatTime(res.StartedAt), formatTime(res.EndedAt),
			boolToInt(res.FalsePositive), boolToInt(res.AutoCommitted),
			len(res.Attempts), t.Repo, t.Title, "", now)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func modelForRunner(runner string, profiles map[string]*task.RunnerProfileConfig) string {
	if profiles == nil {
		return ""
	}
	if p := profiles[runner]; p != nil {
		return p.Model
	}
	return ""
}

func tokenCounts(res *task.TaskResult) (input, output int) {
	if res.TokensUsed != nil {
		return res.TokensUsed.InputTokens, res.TokensUsed.OutputTokens
	}
	return 0, 0
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
