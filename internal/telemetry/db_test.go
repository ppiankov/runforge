package telemetry

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/tokencontrol/internal/task"
)

func tempDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpenDB_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("database file should exist")
	}
}

func TestMigration_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	db1, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = db1.Close()

	// Open again — migration should be idempotent
	db2, err := OpenDB(path)
	if err != nil {
		t.Fatal("second open should succeed:", err)
	}
	_ = db2.Close()
}

func TestRecord_FullReport(t *testing.T) {
	db := tempDB(t)

	now := time.Now()
	tasks := []task.Task{
		{ID: "t1", Repo: "org/repo", Title: "Fix bug", Difficulty: "complex"},
		{ID: "t2", Repo: "org/repo", Title: "Add test", Difficulty: "simple"},
	}
	report := &task.RunReport{
		RunID:         "abc123",
		TasksFiles:    []string{"tasks.json"},
		Workers:       4,
		TotalTasks:    2,
		Completed:     1,
		Failed:        1,
		TotalDuration: 5 * time.Minute,
		Results: map[string]*task.TaskResult{
			"t1": {
				TaskID:     "t1",
				State:      task.StateCompleted,
				StartedAt:  now.Add(-3 * time.Minute),
				EndedAt:    now,
				Duration:   3 * time.Minute,
				RunnerUsed: "codex",
				TokensUsed: &task.TokenUsage{
					InputTokens:  5000,
					OutputTokens: 2000,
					TotalTokens:  7000,
				},
				AutoCommitted: true,
				Attempts:      []task.AttemptInfo{{Runner: "codex"}},
			},
			"t2": {
				TaskID:     "t2",
				State:      task.StateFailed,
				StartedAt:  now.Add(-2 * time.Minute),
				EndedAt:    now,
				Duration:   2 * time.Minute,
				RunnerUsed: "gemini",
				Attempts:   []task.AttemptInfo{{Runner: "codex"}, {Runner: "gemini"}},
			},
		},
	}
	profiles := map[string]*task.RunnerProfileConfig{
		"codex":  {Model: "gpt-4.1"},
		"gemini": {Model: "gemini-2.5-pro"},
	}

	if err := Record(db, report, tasks, profiles); err != nil {
		t.Fatal(err)
	}

	// Verify run row
	var runID string
	var totalTasks int
	err := db.conn.QueryRow(`SELECT run_id, total_tasks FROM runs WHERE run_id = ?`, "abc123").Scan(&runID, &totalTasks)
	if err != nil {
		t.Fatal(err)
	}
	if totalTasks != 2 {
		t.Errorf("expected total_tasks=2, got %d", totalTasks)
	}

	// Verify task execution rows
	var count int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM task_executions WHERE run_id = ?`, "abc123").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected 2 task_executions, got %d", count)
	}

	// Verify cost was computed for t1
	var cost float64
	if err := db.conn.QueryRow(`SELECT cost_usd FROM task_executions WHERE id = ?`, "abc123/t1").Scan(&cost); err != nil {
		t.Fatal(err)
	}
	expected := EstimateCost("gpt-4.1", 5000, 2000)
	if cost != expected {
		t.Errorf("expected cost %.6f, got %.6f", expected, cost)
	}
}

func TestEstimateCost_KnownModel(t *testing.T) {
	cost := EstimateCost("gpt-4.1", 1_000_000, 1_000_000)
	// 1M input * $2/1M + 1M output * $8/1M = $10
	if cost != 10.0 {
		t.Errorf("expected $10.00, got $%.2f", cost)
	}
}

func TestEstimateCost_UnknownModel(t *testing.T) {
	cost := EstimateCost("unknown-model", 1000, 1000)
	if cost != 0 {
		t.Errorf("expected $0 for unknown model, got $%.6f", cost)
	}
}

func TestQueryRunnerStats(t *testing.T) {
	db := tempDB(t)
	insertTestData(t, db)

	stats, err := QueryRunnerStats(db, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) == 0 {
		t.Fatal("expected runner stats")
	}

	found := false
	for _, s := range stats {
		if s.Runner == "codex" {
			found = true
			if s.Tasks != 2 {
				t.Errorf("expected 2 codex tasks, got %d", s.Tasks)
			}
		}
	}
	if !found {
		t.Error("expected codex in stats")
	}
}

func TestQueryCostByPeriod(t *testing.T) {
	db := tempDB(t)
	insertTestData(t, db)

	periods, err := QueryCostByPeriod(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(periods) != 3 {
		t.Fatalf("expected 3 periods, got %d", len(periods))
	}
	// All time should have tasks
	if periods[2].Tasks == 0 {
		t.Error("all-time period should have tasks")
	}
}

func TestQueryTopModels(t *testing.T) {
	db := tempDB(t)
	insertTestData(t, db)

	models, err := QueryTopModels(db, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 {
		t.Fatal("expected model stats")
	}
}

func TestExport_CSV(t *testing.T) {
	db := tempDB(t)
	insertTestData(t, db)

	data, err := QueryExport(db, "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := ExportCSV(&buf, data); err != nil {
		t.Fatal(err)
	}

	csv := buf.String()
	if !strings.Contains(csv, "run_id") {
		t.Error("CSV should contain header")
	}
	if !strings.Contains(csv, "codex") {
		t.Error("CSV should contain codex data")
	}
}

func TestExport_JSON(t *testing.T) {
	db := tempDB(t)
	insertTestData(t, db)

	data, err := QueryExport(db, "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := ExportJSON(&buf, data); err != nil {
		t.Fatal(err)
	}

	var parsed []TaskExecution
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatal("JSON should be valid:", err)
	}
	if len(parsed) == 0 {
		t.Error("JSON should contain data")
	}
}

func TestExport_FilterByRunner(t *testing.T) {
	db := tempDB(t)
	insertTestData(t, db)

	data, err := QueryExport(db, "gemini", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, te := range data {
		if te.Runner != "gemini" {
			t.Errorf("expected only gemini, got %s", te.Runner)
		}
	}
}

func insertTestData(t *testing.T, db *DB) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.conn.Begin()
	if err != nil {
		t.Fatal(err)
	}

	_, err = tx.Exec(`INSERT INTO runs (run_id, workers, total_tasks, completed, failed, total_duration_ms, total_cost_usd, created_at)
		VALUES ('run1', 4, 3, 2, 1, 300000, 0.5, ?)`, now)
	if err != nil {
		t.Fatal(err)
	}

	rows := []struct {
		id, taskID, runner, model, state string
		input, output                    int
		cost                             float64
	}{
		{"run1/t1", "t1", "codex", "gpt-4.1", "COMPLETED", 5000, 2000, 0.026},
		{"run1/t2", "t2", "codex", "gpt-4.1", "COMPLETED", 3000, 1000, 0.014},
		{"run1/t3", "t3", "gemini", "gemini-2.5-pro", "FAILED", 1000, 500, 0.006},
	}
	for _, r := range rows {
		_, err = tx.Exec(`INSERT INTO task_executions
			(id, run_id, task_id, runner, model, state, input_tokens, output_tokens, total_tokens, cost_usd, duration_ms, repo, task_title, created_at)
			VALUES (?, 'run1', ?, ?, ?, ?, ?, ?, ?, ?, 60000, 'org/repo', 'task', ?)`,
			r.id, r.taskID, r.runner, r.model, r.state, r.input, r.output, r.input+r.output, r.cost, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
