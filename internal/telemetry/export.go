package telemetry

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
)

// TaskExecution represents a single row from the task_executions table.
type TaskExecution struct {
	ID            string  `json:"id"`
	RunID         string  `json:"run_id"`
	TaskID        string  `json:"task_id"`
	Runner        string  `json:"runner"`
	Model         string  `json:"model"`
	State         string  `json:"state"`
	Difficulty    string  `json:"difficulty"`
	CascadeStep   int     `json:"cascade_step"`
	InputTokens   int     `json:"input_tokens"`
	OutputTokens  int     `json:"output_tokens"`
	TotalTokens   int     `json:"total_tokens"`
	CostUSD       float64 `json:"cost_usd"`
	DurationMs    int64   `json:"duration_ms"`
	StartedAt     string  `json:"started_at"`
	EndedAt       string  `json:"ended_at"`
	FalsePositive bool    `json:"false_positive"`
	AutoCommitted bool    `json:"auto_committed"`
	Attempts      int     `json:"attempts"`
	Repo          string  `json:"repo"`
	TaskTitle     string  `json:"task_title"`
	CreatedAt     string  `json:"created_at"`
}

// QueryExport returns task executions filtered by runner and time range.
func QueryExport(db *DB, runner, since, until string) ([]TaskExecution, error) {
	query := `SELECT id, run_id, task_id, runner, model, state, difficulty, cascade_step,
		input_tokens, output_tokens, total_tokens, cost_usd,
		duration_ms, started_at, ended_at,
		false_positive, auto_committed, attempts,
		repo, task_title, created_at
		FROM task_executions WHERE 1=1`
	var args []any

	if runner != "" {
		query += ` AND runner = ?`
		args = append(args, runner)
	}
	if since != "" {
		query += ` AND created_at >= ?`
		args = append(args, since)
	}
	if until != "" {
		query += ` AND created_at <= ?`
		args = append(args, until)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []TaskExecution
	for rows.Next() {
		var te TaskExecution
		var fp, ac int
		if err := rows.Scan(
			&te.ID, &te.RunID, &te.TaskID, &te.Runner, &te.Model, &te.State,
			&te.Difficulty, &te.CascadeStep,
			&te.InputTokens, &te.OutputTokens, &te.TotalTokens, &te.CostUSD,
			&te.DurationMs, &te.StartedAt, &te.EndedAt,
			&fp, &ac, &te.Attempts,
			&te.Repo, &te.TaskTitle, &te.CreatedAt,
		); err != nil {
			return nil, err
		}
		te.FalsePositive = fp == 1
		te.AutoCommitted = ac == 1
		results = append(results, te)
	}
	return results, rows.Err()
}

// ExportCSV writes task executions as CSV to the writer.
func ExportCSV(w io.Writer, data []TaskExecution) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	header := []string{
		"id", "run_id", "task_id", "runner", "model", "state",
		"difficulty", "cascade_step",
		"input_tokens", "output_tokens", "total_tokens", "cost_usd",
		"duration_ms", "started_at", "ended_at",
		"false_positive", "auto_committed", "attempts",
		"repo", "task_title", "created_at",
	}
	if err := cw.Write(header); err != nil {
		return err
	}

	for _, te := range data {
		row := []string{
			te.ID, te.RunID, te.TaskID, te.Runner, te.Model, te.State,
			te.Difficulty, fmt.Sprintf("%d", te.CascadeStep),
			fmt.Sprintf("%d", te.InputTokens), fmt.Sprintf("%d", te.OutputTokens),
			fmt.Sprintf("%d", te.TotalTokens), fmt.Sprintf("%.6f", te.CostUSD),
			fmt.Sprintf("%d", te.DurationMs), te.StartedAt, te.EndedAt,
			fmt.Sprintf("%t", te.FalsePositive), fmt.Sprintf("%t", te.AutoCommitted),
			fmt.Sprintf("%d", te.Attempts),
			te.Repo, te.TaskTitle, te.CreatedAt,
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	return cw.Error()
}

// ExportJSON writes task executions as JSON to the writer.
func ExportJSON(w io.Writer, data []TaskExecution) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}
