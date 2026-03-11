package telemetry

import (
	"fmt"
	"time"
)

// CostPerSuccess holds efficiency metrics per runner/model/difficulty bucket.
type CostPerSuccess struct {
	Runner      string
	Model       string
	Difficulty  string
	Tasks       int
	Completed   int
	CostUSD     float64
	CostPerTask float64
	SuccessRate float64
	FPRate      float64
	AvgDuration time.Duration
}

// QueryCostPerSuccess returns cost-per-successful-task grouped by runner, model, difficulty.
func QueryCostPerSuccess(db *DB, since string, minTasks int) ([]CostPerSuccess, error) {
	query := `SELECT runner, model, difficulty,
		COUNT(*) as tasks,
		SUM(CASE WHEN state = 'COMPLETED' THEN 1 ELSE 0 END) as completed,
		COALESCE(SUM(cost_usd), 0) as cost,
		COALESCE(AVG(CASE WHEN state = 'COMPLETED' THEN 100.0 ELSE 0.0 END), 0) as success_rate,
		COALESCE(AVG(CASE WHEN false_positive = 1 THEN 100.0 ELSE 0.0 END), 0) as fp_rate,
		COALESCE(AVG(duration_ms), 0) as avg_duration_ms
		FROM task_executions WHERE 1=1`
	var args []any
	if since != "" {
		query += ` AND created_at >= ?`
		args = append(args, since)
	}
	query += ` GROUP BY runner, model, difficulty`
	if minTasks > 0 {
		query += fmt.Sprintf(` HAVING tasks >= %d`, minTasks)
	}
	query += ` ORDER BY cost DESC`

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []CostPerSuccess
	for rows.Next() {
		var s CostPerSuccess
		var avgMs float64
		if err := rows.Scan(&s.Runner, &s.Model, &s.Difficulty,
			&s.Tasks, &s.Completed, &s.CostUSD,
			&s.SuccessRate, &s.FPRate, &avgMs); err != nil {
			return nil, err
		}
		s.AvgDuration = time.Duration(avgMs) * time.Millisecond
		if s.Completed > 0 {
			s.CostPerTask = s.CostUSD / float64(s.Completed)
		}
		results = append(results, s)
	}
	return results, rows.Err()
}

// CascadeStats holds effectiveness metrics per cascade step.
type CascadeStats struct {
	CascadeStep int
	Tasks       int
	Completed   int
	CostUSD     float64
	RescueRate  float64
}

// QueryCascadeEffectiveness returns success rate and cost per cascade step.
func QueryCascadeEffectiveness(db *DB, since string) ([]CascadeStats, error) {
	query := `SELECT cascade_step,
		COUNT(*) as tasks,
		SUM(CASE WHEN state = 'COMPLETED' THEN 1 ELSE 0 END) as completed,
		COALESCE(SUM(cost_usd), 0) as cost
		FROM task_executions WHERE 1=1`
	var args []any
	if since != "" {
		query += ` AND created_at >= ?`
		args = append(args, since)
	}
	query += ` GROUP BY cascade_step ORDER BY cascade_step`

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []CascadeStats
	for rows.Next() {
		var s CascadeStats
		if err := rows.Scan(&s.CascadeStep, &s.Tasks, &s.Completed, &s.CostUSD); err != nil {
			return nil, err
		}
		if s.Tasks > 0 {
			s.RescueRate = float64(s.Completed) / float64(s.Tasks) * 100
		}
		results = append(results, s)
	}
	return results, rows.Err()
}

// FPAnalysis holds false positive metrics per runner/model.
type FPAnalysis struct {
	Runner string
	Model  string
	Tasks  int
	FPs    int
	FPRate float64
	FPCost float64
}

// QueryFalsePositiveAnalysis returns false positive rates and wasted cost per runner.
func QueryFalsePositiveAnalysis(db *DB, since string, minTasks int) ([]FPAnalysis, error) {
	query := `SELECT runner, model,
		COUNT(*) as tasks,
		SUM(false_positive) as fps,
		COALESCE(AVG(CASE WHEN false_positive = 1 THEN 100.0 ELSE 0.0 END), 0) as fp_rate,
		COALESCE(SUM(CASE WHEN false_positive = 1 THEN cost_usd ELSE 0 END), 0) as fp_cost
		FROM task_executions WHERE state = 'COMPLETED'`
	var args []any
	if since != "" {
		query += ` AND created_at >= ?`
		args = append(args, since)
	}
	query += ` GROUP BY runner, model`
	if minTasks > 0 {
		query += fmt.Sprintf(` HAVING tasks >= %d`, minTasks)
	}
	query += ` ORDER BY fp_cost DESC`

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []FPAnalysis
	for rows.Next() {
		var s FPAnalysis
		if err := rows.Scan(&s.Runner, &s.Model, &s.Tasks, &s.FPs, &s.FPRate, &s.FPCost); err != nil {
			return nil, err
		}
		results = append(results, s)
	}
	return results, rows.Err()
}

// TokenEfficiency holds token usage metrics per runner/model.
type TokenEfficiency struct {
	Runner     string
	Model      string
	AvgInput   int
	AvgOutput  int
	InputRatio float64
	CostPer1K  float64
	ReportRate float64
}

// QueryTokenEfficiency returns token usage patterns per runner/model.
func QueryTokenEfficiency(db *DB, since string, minTasks int) ([]TokenEfficiency, error) {
	query := `SELECT runner, model,
		COALESCE(AVG(input_tokens), 0) as avg_input,
		COALESCE(AVG(output_tokens), 0) as avg_output,
		COALESCE(AVG(CASE WHEN total_tokens > 0 THEN input_tokens * 100.0 / total_tokens ELSE 0 END), 0) as input_ratio,
		CASE WHEN SUM(total_tokens) > 0 THEN SUM(cost_usd) / SUM(total_tokens) * 1000 ELSE 0 END as cost_per_1k,
		COALESCE(AVG(tokens_reported) * 100, 0) as report_rate
		FROM task_executions WHERE 1=1`
	var args []any
	if since != "" {
		query += ` AND created_at >= ?`
		args = append(args, since)
	}
	query += ` GROUP BY runner, model`
	if minTasks > 0 {
		query += fmt.Sprintf(` HAVING COUNT(*) >= %d`, minTasks)
	}
	query += ` ORDER BY cost_per_1k DESC`

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []TokenEfficiency
	for rows.Next() {
		var s TokenEfficiency
		var avgIn, avgOut float64
		if err := rows.Scan(&s.Runner, &s.Model, &avgIn, &avgOut,
			&s.InputRatio, &s.CostPer1K, &s.ReportRate); err != nil {
			return nil, err
		}
		s.AvgInput = int(avgIn)
		s.AvgOutput = int(avgOut)
		results = append(results, s)
	}
	return results, rows.Err()
}
