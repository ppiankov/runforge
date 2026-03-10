package telemetry

import (
	"database/sql"
	"fmt"
	"time"
)

// RunnerStats holds aggregated stats for a single runner.
type RunnerStats struct {
	Runner      string
	Tasks       int
	CostUSD     float64
	SuccessRate float64 // 0-100
	AvgDuration time.Duration
}

// PeriodStats holds cost/task counts for a time period.
type PeriodStats struct {
	Label string
	Cost  float64
	Tasks int
}

// ModelStats holds cost/task counts for a model.
type ModelStats struct {
	Model string
	Cost  float64
	Tasks int
}

// Summary holds top-level telemetry summary.
type Summary struct {
	TotalTasks int
	TotalRuns  int
	Since      time.Time
}

// QuerySummary returns high-level telemetry counts.
func QuerySummary(db *DB) (*Summary, error) {
	s := &Summary{}
	row := db.conn.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT run_id), MIN(created_at) FROM task_executions`)
	var since sql.NullString
	if err := row.Scan(&s.TotalTasks, &s.TotalRuns, &since); err != nil {
		return nil, err
	}
	if since.Valid {
		s.Since, _ = time.Parse(time.RFC3339, since.String)
	}
	return s, nil
}

// QueryRunnerStats returns per-runner aggregated stats, optionally filtered by since.
func QueryRunnerStats(db *DB, since string) ([]RunnerStats, error) {
	query := `SELECT runner, COUNT(*) as tasks,
		COALESCE(SUM(cost_usd), 0) as cost,
		COALESCE(AVG(CASE WHEN state = 'COMPLETED' THEN 100.0 ELSE 0.0 END), 0) as success_rate,
		COALESCE(AVG(duration_ms), 0) as avg_duration_ms
		FROM task_executions`
	var args []any
	if since != "" {
		query += ` WHERE created_at >= ?`
		args = append(args, since)
	}
	query += ` GROUP BY runner ORDER BY tasks DESC`

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var stats []RunnerStats
	for rows.Next() {
		var s RunnerStats
		var avgMs float64
		if err := rows.Scan(&s.Runner, &s.Tasks, &s.CostUSD, &s.SuccessRate, &avgMs); err != nil {
			return nil, err
		}
		s.AvgDuration = time.Duration(avgMs) * time.Millisecond
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// QueryCostByPeriod returns cost/tasks for the last 7d, 30d, and all time.
func QueryCostByPeriod(db *DB) ([]PeriodStats, error) {
	now := time.Now().UTC()
	periods := []struct {
		label string
		since string
	}{
		{"Last 7 days", now.AddDate(0, 0, -7).Format(time.RFC3339)},
		{"Last 30 days", now.AddDate(0, 0, -30).Format(time.RFC3339)},
		{"All time", ""},
	}

	var results []PeriodStats
	for _, p := range periods {
		query := `SELECT COALESCE(SUM(cost_usd), 0), COUNT(*) FROM task_executions`
		var args []any
		if p.since != "" {
			query += ` WHERE created_at >= ?`
			args = append(args, p.since)
		}
		var ps PeriodStats
		ps.Label = p.label
		if err := db.conn.QueryRow(query, args...).Scan(&ps.Cost, &ps.Tasks); err != nil {
			return nil, err
		}
		results = append(results, ps)
	}
	return results, nil
}

// QueryTopModels returns models ranked by cost, optionally filtered.
func QueryTopModels(db *DB, since string, limit int) ([]ModelStats, error) {
	query := `SELECT model, COALESCE(SUM(cost_usd), 0) as cost, COUNT(*) as tasks
		FROM task_executions WHERE model != ''`
	var args []any
	if since != "" {
		query += ` AND created_at >= ?`
		args = append(args, since)
	}
	query += ` GROUP BY model ORDER BY cost DESC`
	if limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, limit)
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var stats []ModelStats
	for rows.Next() {
		var s ModelStats
		if err := rows.Scan(&s.Model, &s.Cost, &s.Tasks); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}
