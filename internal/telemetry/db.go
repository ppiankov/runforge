package telemetry

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const (
	dbDriver        = "sqlite"
	dbSchemaVersion = 1
)

// DB wraps a SQLite connection for telemetry storage.
type DB struct {
	conn *sql.DB
	path string
}

// DefaultPath returns ~/.tokencontrol/telemetry.db.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".tokencontrol", "telemetry.db")
}

// OpenDB opens (or creates) the telemetry database and runs migrations.
func OpenDB(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create telemetry dir: %w", err)
	}
	conn, err := sql.Open(dbDriver, path+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open telemetry db: %w", err)
	}
	db := &DB{conn: conn, path: path}
	if err := db.migrate(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("migrate telemetry db: %w", err)
	}
	return db, nil
}

// Close closes the database connection.
func (db *DB) Close() error { return db.conn.Close() }

// Path returns the database file path.
func (db *DB) Path() string { return db.path }

func (db *DB) migrate() error {
	// Create schema_version table
	if _, err := db.conn.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}

	var version int
	row := db.conn.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`)
	if err := row.Scan(&version); err != nil {
		return err
	}

	if version < 1 {
		if err := db.migrateV1(); err != nil {
			return fmt.Errorf("migrate v1: %w", err)
		}
	}

	return nil
}

func (db *DB) migrateV1() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS runs (
			run_id            TEXT PRIMARY KEY,
			tasks_files       TEXT NOT NULL DEFAULT '[]',
			workers           INTEGER NOT NULL DEFAULT 0,
			total_tasks       INTEGER NOT NULL DEFAULT 0,
			completed         INTEGER NOT NULL DEFAULT 0,
			failed            INTEGER NOT NULL DEFAULT 0,
			rate_limited      INTEGER NOT NULL DEFAULT 0,
			total_duration_ms INTEGER NOT NULL DEFAULT 0,
			total_cost_usd    REAL NOT NULL DEFAULT 0,
			created_at        TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS task_executions (
			id              TEXT PRIMARY KEY,
			run_id          TEXT NOT NULL,
			task_id         TEXT NOT NULL,
			runner          TEXT NOT NULL,
			model           TEXT NOT NULL DEFAULT '',
			state           TEXT NOT NULL,
			difficulty      TEXT NOT NULL DEFAULT '',
			cascade_step    INTEGER NOT NULL DEFAULT 1,
			input_tokens    INTEGER NOT NULL DEFAULT 0,
			output_tokens   INTEGER NOT NULL DEFAULT 0,
			total_tokens    INTEGER NOT NULL DEFAULT 0,
			cost_usd        REAL NOT NULL DEFAULT 0,
			duration_ms     INTEGER NOT NULL DEFAULT 0,
			started_at      TEXT NOT NULL DEFAULT '',
			ended_at        TEXT NOT NULL DEFAULT '',
			false_positive  INTEGER NOT NULL DEFAULT 0,
			auto_committed  INTEGER NOT NULL DEFAULT 0,
			attempts        INTEGER NOT NULL DEFAULT 1,
			repo            TEXT NOT NULL DEFAULT '',
			task_title      TEXT NOT NULL DEFAULT '',
			tasks_file      TEXT NOT NULL DEFAULT '',
			created_at      TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_te_run_id ON task_executions(run_id)`,
		`CREATE INDEX IF NOT EXISTS idx_te_runner ON task_executions(runner)`,
		`CREATE INDEX IF NOT EXISTS idx_te_model ON task_executions(model)`,
		`CREATE INDEX IF NOT EXISTS idx_te_state ON task_executions(state)`,
		`CREATE INDEX IF NOT EXISTS idx_te_created_at ON task_executions(created_at)`,
		`INSERT INTO schema_version (version) VALUES (1)`,
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:40], err)
		}
	}

	return tx.Commit()
}
