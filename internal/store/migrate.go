package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Database format versioning.
//
// A database carries a format version that equals the major version of the
// release that wrote it, so "which format is this file" and "which release can
// read it" are the same question. The rule the project follows:
//
//   - Changing the database format always means a new major version.
//   - A major version may also be cut for other reasons (an API change, say),
//     and then it needs no migration: its slot below is left empty.
//
// So bump FormatVersion together with the release major, and add one entry to
// migrations for the transition. An entry with a nil apply is a deliberate
// no-op, not an oversight.
//
// Upgrades are one major version at a time. A database two or more majors
// behind is refused rather than chain-migrated, because each step is only ever
// developed and tested against the release immediately before it. The README
// says so in bold, and the error message repeats it.

// FormatVersion is the database format this build reads and writes.
//
// Keep it equal to the major version of the release. Migrating a database
// happens on startup and brings it up to exactly this number.
const FormatVersion = 1

// formatVersionKey is the row in the meta table holding the stamp.
const formatVersionKey = "format_version"

// step upgrades a database from one major version to the next.
type step struct {
	from  int
	to    int
	notes string
	// apply runs inside a transaction. nil means the major version changed
	// without touching the format, which is a normal outcome.
	apply func(ctx context.Context, tx *sql.Tx) error
}

// migrations[i] moves a database from format i to format i+1.
//
// Add one entry each time the major version moves; leave apply nil when the
// format did not change. Never reorder or remove an entry: a released entry is
// already applied in the field.
var migrations = []step{
	{
		from:  0,
		to:    1,
		notes: "add events.location",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `ALTER TABLE events ADD COLUMN location TEXT`)
			return err
		},
	},
	// Next major version goes here, for example:
	//
	//	{from: 1, to: 2, notes: "no format change", apply: nil},
}

// currentSchema creates the schema of FormatVersion from nothing.
//
// It must produce exactly what running every migration in order produces, and
// the tests compare the two column by column so they cannot drift apart. That
// is why location sits at the end of the table rather than beside notes where
// it reads better: ALTER TABLE can only append, so a migrated database has it
// last, and both routes have to agree. Add future columns the same way.
const currentSchema = `
CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

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
    updated_at   INTEGER NOT NULL,
    -- Appended by the 0->1 migration; see the note above on ordering.
    location     TEXT
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
`

// migrate brings the database up to FormatVersion.
func (s *Store) migrate(ctx context.Context) error {
	if err := s.ensureMeta(ctx); err != nil {
		return err
	}

	stamp, err := s.readFormatVersion(ctx)
	if err != nil {
		return err
	}

	if stamp == nil {
		// No stamp. Either a database written before versioning existed, or a
		// brand new file: the presence of the events table tells them apart.
		existing, err := s.tableExists(ctx, "events")
		if err != nil {
			return err
		}
		if !existing {
			return s.createCurrent(ctx)
		}
		legacy := 0
		stamp = &legacy
	}

	if *stamp == FormatVersion {
		return nil
	}
	if *stamp > FormatVersion {
		return fmt.Errorf(
			"database format %d was written by a newer release than this one (format %d); upgrade the server instead of downgrading it",
			*stamp, FormatVersion)
	}

	plan, err := planSteps(*stamp, FormatVersion, migrations)
	if err != nil {
		return err
	}
	if len(plan) == 0 {
		// A format version with no step defined: the schema is already right,
		// only the stamp needs moving.
		return s.writeFormatVersion(ctx, FormatVersion)
	}

	for _, st := range plan {
		if err := s.applyStep(ctx, st); err != nil {
			return err
		}
	}
	return nil
}

// planSteps chooses the steps to run, refusing to jump more than one major
// version at a time.
//
// Kept separate from the database so the rule can be tested directly, including
// the case that only exists once there are two or more migrations.
func planSteps(from, to int, steps []step) ([]step, error) {
	if from > to {
		return nil, fmt.Errorf("database format %d is newer than format %d", from, to)
	}
	if to-from > 1 {
		return nil, fmt.Errorf(
			"database format %d cannot be upgraded straight to %d: this project upgrades one major version at a time. "+
				"Run the release for each intermediate major version, in order, before starting this one",
			from, to)
	}
	if from == to {
		return nil, nil
	}

	for _, st := range steps {
		if st.from == from && st.to == to {
			return []step{st}, nil
		}
	}
	// A major version cut without a format change has no step; the caller
	// handles that by moving the stamp.
	return nil, nil
}

// applyStep runs one migration and moves the stamp in the same transaction, so
// an interrupted upgrade leaves the previous version intact.
func (s *Store) applyStep(ctx context.Context, st step) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration to format %d: %w", st.to, err)
	}
	defer func() { _ = tx.Rollback() }()

	if st.apply != nil {
		if err := st.apply(ctx, tx); err != nil {
			return fmt.Errorf("migrate to format %d (%s): %w", st.to, st.notes, err)
		}
	}
	if err := setFormatVersion(ctx, tx, st.to); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration to format %d: %w", st.to, err)
	}
	return nil
}

// createCurrent builds the current schema from nothing and stamps it.
func (s *Store) createCurrent(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema creation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, currentSchema); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	if err := setFormatVersion(ctx, tx, FormatVersion); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema creation: %w", err)
	}
	return nil
}

func (s *Store) ensureMeta(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`)
	if err != nil {
		return fmt.Errorf("create meta table: %w", err)
	}
	return nil
}

// readFormatVersion returns the stamp, or nil when the database has none.
func (s *Store) readFormatVersion(ctx context.Context) (*int, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, formatVersionKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read format version: %w", err)
	}
	var version int
	if _, err := fmt.Sscanf(raw, "%d", &version); err != nil {
		return nil, fmt.Errorf("format version %q is not a number: %w", raw, err)
	}
	return &version, nil
}

func (s *Store) writeFormatVersion(ctx context.Context, version int) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		formatVersionKey, fmt.Sprint(version))
	if err != nil {
		return fmt.Errorf("write format version: %w", err)
	}
	return nil
}

func setFormatVersion(ctx context.Context, tx *sql.Tx, version int) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		formatVersionKey, fmt.Sprint(version))
	if err != nil {
		return fmt.Errorf("stamp format version %d: %w", version, err)
	}
	return nil
}

func (s *Store) tableExists(ctx context.Context, name string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("look up table %s: %w", name, err)
	}
	return true, nil
}

// FormatVersionOf reports the format version stamped in the database.
//
// Used by the health endpoint and by tests; opening the store already migrated
// it, so this is the version the running build writes.
func (s *Store) FormatVersionOf(ctx context.Context) (int, error) {
	stamp, err := s.readFormatVersion(ctx)
	if err != nil {
		return 0, err
	}
	if stamp == nil {
		return 0, nil
	}
	return *stamp, nil
}
