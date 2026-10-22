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
const FormatVersion = 3

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
	{
		from:  1,
		to:    2,
		notes: "add categories, and events.category_id",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, categoriesSchema); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, seedCategories); err != nil {
				return err
			}
			// Appended, like every added column: ALTER TABLE cannot insert.
			_, err := tx.ExecContext(ctx,
				`ALTER TABLE events ADD COLUMN category_id INTEGER NOT NULL DEFAULT 0`)
			return err
		},
	},
	{
		from:  2,
		to:    3,
		notes: "canceled and deleted on events; the deleted category becomes the trash",
		apply: func(ctx context.Context, tx *sql.Tx) error {
			steps := []string{
				// Appended in this order; currentSchema lists them the same way.
				`ALTER TABLE events ADD COLUMN canceled INTEGER NOT NULL DEFAULT 0`,
				`ALTER TABLE events ADD COLUMN deleted INTEGER NOT NULL DEFAULT 0`,
				// Format 2 said "deleted" by setting category -1. That state now
				// lives on the event, so those rows move into the trash: off the
				// calendar, still recoverable, and no longer pretending to be a
				// grouping of their own.
				`UPDATE events SET category_id = 0, deleted = 1 WHERE category_id = -1`,
				// -1 stops existing as a category, so a stale reference from a
				// client becomes a validation error instead of a silent state.
				`DELETE FROM categories WHERE id = -1`,
				`CREATE INDEX IF NOT EXISTS idx_events_deleted ON events(deleted)`,
			}
			for _, stmt := range steps {
				if _, err := tx.ExecContext(ctx, stmt); err != nil {
					return err
				}
			}
			return nil
		},
	},
}

// categoriesSchema and seedCategories are used both when creating a database
// from nothing and by the migration that introduced categories, so the two
// paths cannot describe the table differently.
const categoriesSchema = `
CREATE TABLE IF NOT EXISTS categories (
    id      INTEGER PRIMARY KEY,
    name    TEXT NOT NULL,
    color   TEXT NOT NULL,
    display INTEGER NOT NULL DEFAULT 1
);
`

// The name and colour the built-in category starts with. It is not arbitrary:
// 0 is what a new event gets, so its colour is what most of the calendar looks
// like.
const (
	uncategorizedCategoryName = "默认"
	// Google's blue: white text on it reaches 4.5:1, so the default blocks are
	// legible without a per-event decision, and it matches the light theme's
	// accent so the calendar and the UI read as one product.
	uncategorizedCategoryColor = "#1a73e8"
)

// The one row the application depends on: 0 is where everything falls back to.
// Its id is fixed, so later categories auto-increment from 1.
const seedCategories = `
INSERT OR IGNORE INTO categories (id, name, color, display)
 VALUES (0, '` + uncategorizedCategoryName + `', '` + uncategorizedCategoryColor + `', 1);
`

// migrateBuiltInDefaults brings the built-in rows written by earlier builds up
// to the current defaults. It only touches values that are still exactly the
// old defaults, so a name or colour the user chose is never overwritten -- and
// it is the reason the defaults could change without a format bump: the shape
// of the table did not change, only two seeded values.
const migrateBuiltInDefaults = `
UPDATE categories SET name = '` + uncategorizedCategoryName + `' WHERE id = 0 AND name = '未分类';
UPDATE categories SET color = '` + uncategorizedCategoryColor + `'
 WHERE id = 0 AND color IN ('#ffffff', '#4f8cff');
`

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

CREATE TABLE IF NOT EXISTS categories (
    id      INTEGER PRIMARY KEY,
    name    TEXT NOT NULL,
    color   TEXT NOT NULL,
    -- Whether the sidebar shows this category. A presentation choice, stored so
    -- it survives a reload; the deleted row (-1) starts hidden.
    display INTEGER NOT NULL DEFAULT 1
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
    location     TEXT,
    -- Appended by the 1->2 migration.
    category_id  INTEGER NOT NULL DEFAULT 0,
    -- Appended by the 2->3 migration: called off, and in the trash.
    canceled     INTEGER NOT NULL DEFAULT 0,
    deleted      INTEGER NOT NULL DEFAULT 0
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
CREATE INDEX IF NOT EXISTS idx_events_category   ON events(category_id);
CREATE INDEX IF NOT EXISTS idx_events_deleted    ON events(deleted);
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
		// Nothing to migrate, but the two built-in categories are part of the
		// API contract rather than user data: -1 is what deleting writes and 0
		// is what every new event starts in. A database that lost them (an
		// interrupted upgrade, a hand-edited file) would leave every event
		// pointing at an id no lookup can resolve, so repair it. INSERT OR
		// IGNORE keeps any renaming or recolouring the user did to those rows.
		if err := s.ensureCategoryDisplay(ctx); err != nil {
			return err
		}
		return s.ensureSeedCategories(ctx)
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
	// A format-1 database reaches display through the 1 -> 2 step above; this
	// only matters for a format-2 file written by a pre-release build of 2.0.0,
	// and is a no-op otherwise.
	return s.ensureCategoryDisplay(ctx)
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
	// A fresh database needs the seed rows too. Only the migration seeded them
	// at first, which left new databases with an empty categories table and
	// every default category_id pointing at nothing.
	if _, err := tx.ExecContext(ctx, seedCategories); err != nil {
		return fmt.Errorf("seed categories: %w", err)
	}
	if err := setFormatVersion(ctx, tx, FormatVersion); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema creation: %w", err)
	}
	return nil
}

// ensureCategoryDisplay adds the display column to a categories table written
// before the column existed.
//
// The column was folded into the 1 -> 2 step while v2.0.0 was still being
// prepared, so a database from one of that period's pre-release builds claims
// format 2 without having the column and would fail on the first query. This is
// the repair for those files, and a no-op for every database v2.0.0 or later
// wrote.
func (s *Store) ensureCategoryDisplay(ctx context.Context) error {
	has, err := s.columnExists(ctx, "categories", "display")
	if err != nil {
		return err
	}
	if has {
		return nil
	}
	if _, err := s.db.ExecContext(ctx,
		`ALTER TABLE categories ADD COLUMN display INTEGER NOT NULL DEFAULT 1`); err != nil {
		return fmt.Errorf("add categories.display: %w", err)
	}
	return nil
}

// columnExists reports whether a table already has a column.
func (s *Store) columnExists(ctx context.Context, table, column string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, fmt.Errorf("scan %s column: %w", table, err)
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate %s columns: %w", table, err)
	}
	return false, nil
}

// ensureSeedCategories inserts the built-in categories if they are missing and
// normalises the ones written by earlier builds. Idempotent, so it runs on
// every startup.
func (s *Store) ensureSeedCategories(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, seedCategories); err != nil {
		return fmt.Errorf("restore the built-in categories: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, migrateBuiltInDefaults); err != nil {
		return fmt.Errorf("update the built-in category defaults: %w", err)
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
