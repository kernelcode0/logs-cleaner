package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	db     *sql.DB
	logger *slog.Logger
}

type RunRecord struct {
	ID                  int64     `json:"id"`
	Timestamp           time.Time `json:"timestamp"`
	ServerID            string    `json:"server_id"`
	LogsCleaned         int       `json:"logs_cleaned"`
	SpaceReclaimedMB    float64   `json:"space_reclaimed_mb"`
	ExecutionDurationMS int64     `json:"execution_duration_ms"`
	ErrorsJSON          string    `json:"errors_json"`
	DryRun              bool      `json:"dry_run"`
}

type ContainerCleanRecord struct {
	ID            int64     `json:"id"`
	RunID         int64     `json:"run_id"`
	ContainerName string    `json:"container_name"`
	Image         string    `json:"image"`
	SizeMB        float64   `json:"size_mb"`
	WasTruncated  bool      `json:"was_truncated"`
	Timestamp     time.Time `json:"timestamp"`
}

type LogSizeRecord struct {
	Timestamp time.Time
	SizeMB    float64
}

type AggregateStats struct {
	TotalRuns             int64     `json:"total_runs"`
	TotalLogsCleaned      int64     `json:"total_logs_cleaned"`
	TotalSpaceReclaimedMB float64   `json:"total_space_reclaimed_mb"`
	AvgDurationMS         float64   `json:"avg_duration_ms"`
	LastRunAt             time.Time `json:"last_run_at"`
	ServerID              string    `json:"server_id"`
}

type ContainerFrequency struct {
	ContainerName string  `json:"container_name"`
	CleanCount    int     `json:"clean_count"`
	TotalSizeMB   float64 `json:"total_size_mb"`
}

const schema = `
CREATE TABLE IF NOT EXISTS cleanup_runs (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    server_id             TEXT NOT NULL,
    logs_cleaned          INTEGER NOT NULL DEFAULT 0,
    space_reclaimed_mb    REAL NOT NULL DEFAULT 0.0,
    execution_duration_ms INTEGER NOT NULL DEFAULT 0,
    errors_json           TEXT NOT NULL DEFAULT '[]',
    dry_run               BOOLEAN NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_cleanup_runs_timestamp ON cleanup_runs(timestamp);
CREATE INDEX IF NOT EXISTS idx_cleanup_runs_server_id ON cleanup_runs(server_id);

CREATE TABLE IF NOT EXISTS container_cleans (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id         INTEGER NOT NULL REFERENCES cleanup_runs(id) ON DELETE CASCADE,
    container_name TEXT NOT NULL,
    image          TEXT NOT NULL DEFAULT '',
    size_mb        REAL NOT NULL DEFAULT 0.0,
    was_truncated  BOOLEAN NOT NULL DEFAULT 1,
    timestamp      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_container_cleans_name      ON container_cleans(container_name);
CREATE INDEX IF NOT EXISTS idx_container_cleans_timestamp ON container_cleans(timestamp);

CREATE TABLE IF NOT EXISTS log_size_snapshots (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    container_id   TEXT NOT NULL,
    container_name TEXT NOT NULL,
    size_mb        REAL NOT NULL,
    timestamp      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_log_snapshots_container ON log_size_snapshots(container_id, timestamp);

CREATE TABLE IF NOT EXISTS schema_version (
    version    INTEGER NOT NULL,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

func New(path string, logger *slog.Logger) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}

	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}

	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}

	return &DB{db: db, logger: logger}, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

// CleanupRunInput is what the cleaner passes when persisting a run.
type CleanupRunInput struct {
	ServerID            string
	LogsCleaned         int
	SpaceReclaimedMB    float64
	ExecutionDurationMS int64
	Errors              []string
	DryRun              bool
	Containers          []ContainerCleanInput
}

type ContainerCleanInput struct {
	ContainerName string
	Image         string
	SizeMB        float64
	WasTruncated  bool
}

func (d *DB) RecordRun(ctx context.Context, input CleanupRunInput) (int64, error) {
	errJSON, err := json.Marshal(input.Errors)
	if err != nil {
		errJSON = []byte("[]")
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx,
		`INSERT INTO cleanup_runs (server_id, logs_cleaned, space_reclaimed_mb, execution_duration_ms, errors_json, dry_run)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		input.ServerID, input.LogsCleaned, input.SpaceReclaimedMB, input.ExecutionDurationMS, string(errJSON), input.DryRun,
	)
	if err != nil {
		return 0, fmt.Errorf("insert run: %w", err)
	}

	runID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}

	for _, c := range input.Containers {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO container_cleans (run_id, container_name, image, size_mb, was_truncated)
			 VALUES (?, ?, ?, ?, ?)`,
			runID, c.ContainerName, c.Image, c.SizeMB, c.WasTruncated,
		); err != nil {
			return 0, fmt.Errorf("insert container clean: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return runID, nil
}

func (d *DB) RecordLogSizeSnapshot(ctx context.Context, containerID, containerName string, sizeMB float64) error {
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO log_size_snapshots (container_id, container_name, size_mb) VALUES (?, ?, ?)`,
		containerID, containerName, sizeMB,
	)
	return err
}

func (d *DB) ListRuns(ctx context.Context, serverID string, limit int) ([]RunRecord, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, timestamp, server_id, logs_cleaned, space_reclaimed_mb, execution_duration_ms, errors_json, dry_run
		 FROM cleanup_runs WHERE server_id = ? ORDER BY timestamp DESC LIMIT ?`,
		serverID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []RunRecord
	for rows.Next() {
		var r RunRecord
		var ts string
		if err := rows.Scan(&r.ID, &ts, &r.ServerID, &r.LogsCleaned, &r.SpaceReclaimedMB, &r.ExecutionDurationMS, &r.ErrorsJSON, &r.DryRun); err != nil {
			return nil, err
		}
		r.Timestamp = parseTimestamp(ts)
		records = append(records, r)
	}
	return records, rows.Err()
}

func (d *DB) GetAggregateStats(ctx context.Context, serverID string) (AggregateStats, error) {
	var stats AggregateStats
	stats.ServerID = serverID

	row := d.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(logs_cleaned),0), COALESCE(SUM(space_reclaimed_mb),0),
		        COALESCE(AVG(execution_duration_ms),0), COALESCE(MAX(timestamp),'')
		 FROM cleanup_runs WHERE server_id = ?`,
		serverID,
	)
	var lastRun string
	if err := row.Scan(&stats.TotalRuns, &stats.TotalLogsCleaned, &stats.TotalSpaceReclaimedMB, &stats.AvgDurationMS, &lastRun); err != nil {
		return stats, err
	}
	if lastRun != "" {
		stats.LastRunAt = parseTimestamp(lastRun)
	}
	return stats, nil
}

func (d *DB) GetContainerCleanupCount(ctx context.Context, containerName string, windowDays int) (int, error) {
	var count int
	row := d.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM container_cleans
		 WHERE container_name = ? AND timestamp >= datetime('now', ? || ' days')`,
		containerName, fmt.Sprintf("-%d", windowDays),
	)
	err := row.Scan(&count)
	return count, err
}

func (d *DB) GetLogSizeHistory(ctx context.Context, containerID string, windowDays int) ([]LogSizeRecord, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT timestamp, size_mb FROM log_size_snapshots
		 WHERE container_id = ? AND timestamp >= datetime('now', ? || ' days')
		 ORDER BY timestamp ASC`,
		containerID, fmt.Sprintf("-%d", windowDays),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []LogSizeRecord
	for rows.Next() {
		var r LogSizeRecord
		var ts string
		if err := rows.Scan(&ts, &r.SizeMB); err != nil {
			return nil, err
		}
		r.Timestamp, _ = time.Parse("2006-01-02 15:04:05", ts)
		records = append(records, r)
	}
	return records, rows.Err()
}

func (d *DB) GetTopContainersByCleanupCount(ctx context.Context, serverID string, limit, windowDays int) ([]ContainerFrequency, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT cc.container_name, COUNT(*) AS clean_count, COALESCE(SUM(cc.size_mb),0) AS total_mb
		 FROM container_cleans cc
		 JOIN cleanup_runs cr ON cc.run_id = cr.id
		 WHERE cr.server_id = ? AND cc.timestamp >= datetime('now', ? || ' days')
		 GROUP BY cc.container_name
		 ORDER BY clean_count DESC
		 LIMIT ?`,
		serverID, fmt.Sprintf("-%d", windowDays), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ContainerFrequency
	for rows.Next() {
		var f ContainerFrequency
		if err := rows.Scan(&f.ContainerName, &f.CleanCount, &f.TotalSizeMB); err != nil {
			return nil, err
		}
		result = append(result, f)
	}
	return result, rows.Err()
}

// parseTimestamp handles multiple SQLite timestamp formats gracefully.
func parseTimestamp(s string) time.Time {
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.999999999",
		time.RFC3339,
		time.RFC3339Nano,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
