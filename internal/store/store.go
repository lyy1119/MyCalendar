// Package store owns the SQLite persistence layer.
//
// The schema is exactly the two tables specified for the project; the only
// additions are indexes, which do not change the column layout.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	// Pure-Go SQLite driver: no cgo, so the service cross-compiles to any
	// target architecture the intranet deployment needs.
	_ "modernc.org/sqlite"
)

// schemaVersion is the user_version written after migrations run.
const schemaVersion = 1

// migrations holds one SQL script per schema version, index 0 upgrading
// user_version 0 to 1.
var migrations = []string{
	`
CREATE TABLE IF NOT EXISTS events (
    id           INTEGER PRIMARY KEY,
    title        TEXT    NOT NULL,
    notes        TEXT,
    start_at     INTEGER NOT NULL,
    end_at       INTEGER,
    is_all_day   INTEGER NOT NULL DEFAULT 0,
    event_date   TEXT,
    is_task      INTEGER NOT NULL DEFAULT 0,
    is_completed INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS reminders (
    id         INTEGER PRIMARY KEY,
    event_id   INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    remind_at  INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_start_at   ON events(start_at);
CREATE INDEX IF NOT EXISTS idx_events_end_at     ON events(end_at);
CREATE INDEX IF NOT EXISTS idx_events_event_date ON events(event_date);
CREATE INDEX IF NOT EXISTS idx_events_flags      ON events(is_all_day, is_task, is_completed);
CREATE INDEX IF NOT EXISTS idx_reminders_event   ON reminders(event_id);
CREATE INDEX IF NOT EXISTS idx_reminders_at      ON reminders(remind_at);
`,
}

// Store is a handle on the SQLite database.
type Store struct {
	db   *sql.DB
	path string
}

// Open opens (creating if needed) the database at path and applies migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn, inMemory, err := buildDSN(path)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite allows a single writer at a time. A small pool keeps concurrent
	// readers useful while the busy_timeout pragma absorbs writer contention.
	if inMemory {
		// Every connection to :memory: would otherwise get its own database.
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(4)
	}
	db.SetMaxIdleConns(4)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	s := &Store{db: db, path: path}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// buildDSN turns a filesystem path into a modernc.org/sqlite URI.
func buildDSN(path string) (dsn string, inMemory bool, err error) {
	if strings.TrimSpace(path) == "" {
		return "", false, fmt.Errorf("database path must not be empty")
	}
	if strings.ContainsAny(path, "?#") {
		return "", false, fmt.Errorf("database path must not contain '?' or '#': %q", path)
	}

	pragmas := "?_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)"

	if path == ":memory:" || strings.Contains(path, "mode=memory") {
		return "file::memory:" + pragmas, true, nil
	}
	return "file:" + path + pragmas, false, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the underlying handle for health checks and tests.
func (s *Store) DB() *sql.DB { return s.db }

// Path returns the configured database path.
func (s *Store) Path() string { return s.path }

// migrate applies every pending schema version inside a single transaction.
func (s *Store) migrate(ctx context.Context) error {
	var current int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if current > schemaVersion {
		return fmt.Errorf("database schema version %d is newer than this binary supports (%d)", current, schemaVersion)
	}
	if current == schemaVersion {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for v := current; v < schemaVersion; v++ {
		if _, err := tx.ExecContext(ctx, migrations[v]); err != nil {
			return fmt.Errorf("apply schema version %d: %w", v+1, err)
		}
	}
	// PRAGMA does not accept bound parameters; the value is a compile-time int.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("stamp schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}
