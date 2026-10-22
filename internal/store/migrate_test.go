package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lyy1119/MyCalendar/internal/model"
)

// legacySchema is the exact schema the v0.1.0 release created: no meta table,
// no location column, and PRAGMA user_version stamped by the migrator of the
// day. Written by hand here because migrations must keep working against files
// that already exist in the field, and the old code that produced them is gone.
const legacySchema = `
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

CREATE INDEX IF NOT EXISTS idx_events_start_at ON events(start_at);
PRAGMA user_version = 1;
`

// v1Schema is the schema v1.1.2 left behind: the meta stamp exists and location
// has been appended, but there are no categories and no category_id.
const v1Schema = `
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
    location     TEXT
);

CREATE TABLE IF NOT EXISTS reminders (
    id         INTEGER PRIMARY KEY,
    event_id   INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    remind_at  INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_start_at ON events(start_at);
INSERT INTO meta (key, value) VALUES ('format_version', '1');
`

// makeV1Database writes a database shaped like the v1 major version.
func makeV1Database(t *testing.T, withEvent bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "v1.db")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open v1 database: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(v1Schema); err != nil {
		t.Fatalf("create v1 schema: %v", err)
	}
	if withEvent {
		_, err = db.Exec(
			`INSERT INTO events (title, notes, location, start_at, end_at, is_task, is_completed, created_at, updated_at)
			 VALUES ('来自 v1', '旧备注', '旧地点', 1773205200, 1773208800, 1, 0, 1000, 1000)`)
		if err != nil {
			t.Fatalf("seed v1 event: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO reminders (event_id, remind_at, created_at) VALUES (1, 1773205000, 1000)`); err != nil {
			t.Fatalf("seed v1 reminder: %v", err)
		}
	}
	return path
}

// makeLegacyDatabase writes a v0.1.0-shaped database and returns its path.
func makeLegacyDatabase(t *testing.T, withEvent bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.db")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	if withEvent {
		_, err = db.Exec(
			`INSERT INTO events (title, notes, start_at, end_at, is_task, is_completed, created_at, updated_at)
			 VALUES ('来自旧版本', '旧备注', 1773205200, 1773208800, 1, 0, 1000, 1000)`)
		if err != nil {
			t.Fatalf("seed legacy event: %v", err)
		}
		_, err = db.Exec(`INSERT INTO reminders (event_id, remind_at, created_at) VALUES (1, 1773205000, 1000)`)
		if err != nil {
			t.Fatalf("seed legacy reminder: %v", err)
		}
	}
	return path
}

func TestFreshDatabaseIsStampedWithTheCurrentFormat(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	version, err := st.FormatVersionOf(ctx)
	if err != nil {
		t.Fatalf("FormatVersionOf: %v", err)
	}
	if version != FormatVersion {
		t.Errorf("format version = %d, want %d", version, FormatVersion)
	}

	// A fresh database must already have the location column, without needing
	// the migration: creating and migrating have to agree.
	ev, err := st.CreateEvent(ctx, modelEventWithLocation("有地点", "会议室 A"), nil)
	if err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if ev.Location == nil || *ev.Location != "会议室 A" {
		t.Fatalf("location = %v, want 会议室 A", ev.Location)
	}
}

// The v0 shape is two major versions behind, so the one-major-at-a-time rule
// refuses it: the v1 release has to be run in between.
func TestUpgradeFromV0IsRefused(t *testing.T) {
	ctx := context.Background()
	path := makeLegacyDatabase(t, true)

	_, err := Open(ctx, path)
	if err == nil {
		t.Fatal("Open upgraded a v0 database straight to the current format")
	}
	if !strings.Contains(err.Error(), "one major version at a time") {
		t.Errorf("error does not explain the rule: %v", err)
	}
}

func TestUpgradeFromV1Database(t *testing.T) {
	ctx := context.Background()
	path := makeV1Database(t, true)

	st, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open legacy database: %v", err)
	}
	defer st.Close()

	version, err := st.FormatVersionOf(ctx)
	if err != nil {
		t.Fatalf("FormatVersionOf: %v", err)
	}
	if version != FormatVersion {
		t.Errorf("format version after upgrade = %d, want %d", version, FormatVersion)
	}

	// The column exists and existing rows kept their data.
	ev, err := st.GetEvent(ctx, 1)
	if err != nil {
		t.Fatalf("GetEvent after upgrade: %v", err)
	}
	if ev.Title != "来自 v1" || ev.Notes == nil || *ev.Notes != "旧备注" {
		t.Errorf("existing data changed during upgrade: %+v", ev)
	}
	if ev.Location == nil || *ev.Location != "旧地点" {
		t.Errorf("location = %v, want the migrated row to keep 旧地点", ev.Location)
	}
	if ev.CategoryID != CategoryUncategorized {
		t.Errorf("category_id = %d, want the migrated row to land in uncategorized", ev.CategoryID)
	}
	if len(ev.Reminders) != 1 {
		t.Errorf("reminders = %d, want the migrated row's reminder", len(ev.Reminders))
	}

	// And the new column is usable afterwards.
	location := "新地点"
	ev.Location = &location
	updated, err := st.UpdateEvent(ctx, ev, nil, false)
	if err != nil {
		t.Fatalf("UpdateEvent after upgrade: %v", err)
	}
	if updated.Location == nil || *updated.Location != "新地点" {
		t.Errorf("location after update = %v", updated.Location)
	}
}

// Migrating twice must be a no-op: every start runs the check.
func TestUpgradeIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := makeV1Database(t, true)

	for attempt := 1; attempt <= 3; attempt++ {
		st, err := Open(ctx, path)
		if err != nil {
			t.Fatalf("Open attempt %d: %v", attempt, err)
		}
		version, err := st.FormatVersionOf(ctx)
		if err != nil {
			t.Fatalf("FormatVersionOf on attempt %d: %v", attempt, err)
		}
		if version != FormatVersion {
			t.Errorf("attempt %d: format version = %d", attempt, version)
		}
		st.Close()
	}
}

// The built-in categories are API contract, not user data, so a database that
// lost them is repaired on the next start instead of leaving every event
// pointing at an id that no lookup can resolve.
func TestBuiltInCategoriesAreRestored(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	path := st.Path()
	st.Close()

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM categories`); err != nil {
		t.Fatalf("empty the categories: %v", err)
	}
	db.Close()

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reopened.Close()

	categories, err := reopened.ListCategories(ctx)
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}
	if len(categories) != 2 {
		t.Fatalf("categories = %d, want the two built-in rows back", len(categories))
	}
	if categories[0].ID != CategoryDeleted || categories[1].ID != CategoryUncategorized {
		t.Errorf("restored ids = %d, %d", categories[0].ID, categories[1].ID)
	}
}

// Format 2 is unreleased, so the display column was added by editing the 1 -> 2
// step rather than by adding a 2 -> 3 one. A database written by an
// intermediate build of that same unreleased version reports format 2 without
// the column; startup has to add it, or every category query fails.
func TestUnreleasedFormat2GainsDisplayColumn(t *testing.T) {
	ctx := context.Background()
	path := makeV1Database(t, true)

	// Simulate the intermediate build: migrate, then strip the column the way
	// that build's schema would have looked.
	st, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	st.Close()

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	for _, stmt := range []string{
		// The same table without the column, keeping the primary key so the seed
		// rows stay identifiable.
		`CREATE TABLE categories_old (
		     id INTEGER PRIMARY KEY, name TEXT NOT NULL, color TEXT NOT NULL)`,
		`INSERT INTO categories_old (id, name, color) SELECT id, name, color FROM categories`,
		`DROP TABLE categories`,
		`ALTER TABLE categories_old RENAME TO categories`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	db.Close()

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open after downgrade: %v", err)
	}
	defer reopened.Close()

	categories, err := reopened.ListCategories(ctx)
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}
	if len(categories) != 2 {
		t.Fatalf("categories = %d, want the two built-ins", len(categories))
	}
	byID := map[int64]model.Category{}
	for _, c := range categories {
		byID[c.ID] = c
	}
	if !byID[CategoryUncategorized].Display {
		t.Errorf("category 0 display = false, want the default true")
	}
	if byID[CategoryDeleted].Display {
		t.Errorf("category -1 display = true, want it hidden")
	}
}

// A database written by an earlier build of this release still has the old
// built-in defaults (未分类 in white), so startup moves them to the current
// ones -- but only while they are untouched.
func TestBuiltInDefaultsAreMigrated(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	path := st.Path()
	st.Close()

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if _, err := db.Exec(`UPDATE categories SET name = '未分类', color = '#ffffff' WHERE id = 0`); err != nil {
		t.Fatalf("write the old defaults: %v", err)
	}
	db.Close()

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reopened.Close()

	category, err := reopened.GetCategory(ctx, CategoryUncategorized)
	if err != nil {
		t.Fatalf("GetCategory: %v", err)
	}
	if category.Name != "默认" || category.Color != "#1a73e8" {
		t.Errorf("category 0 = %q/%q, want the new defaults 默认/#1a73e8", category.Name, category.Color)
	}
}

// ...and a colour the user picked is not "the old default", so it stays.
func TestBuiltInDefaultsAreNotImposedOnAChosenColour(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	path := st.Path()

	if _, err := st.UpdateCategory(ctx, CategoryUncategorized, "默认", "#22aa66", true); err != nil {
		t.Fatalf("UpdateCategory: %v", err)
	}
	st.Close()

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reopened.Close()

	category, err := reopened.GetCategory(ctx, CategoryUncategorized)
	if err != nil {
		t.Fatalf("GetCategory: %v", err)
	}
	if category.Color != "#22aa66" {
		t.Errorf("colour = %q, want the chosen #22aa66 to survive", category.Color)
	}
}

// A rename of a built-in category is user intent and must survive the repair.
func TestRestoringBuiltInCategoriesKeepsRenames(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	path := st.Path()

	renamed, err := st.UpdateCategory(ctx, CategoryDeleted, "已删除", "#9aa0a6", false)
	if err != nil {
		t.Fatalf("UpdateCategory: %v", err)
	}
	if renamed.Name != "已删除" {
		t.Fatalf("name = %q", renamed.Name)
	}
	st.Close()

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reopened.Close()

	category, err := reopened.GetCategory(ctx, CategoryDeleted)
	if err != nil {
		t.Fatalf("GetCategory: %v", err)
	}
	if category.Name != "已删除" {
		t.Errorf("name after restart = %q, want the rename to survive", category.Name)
	}
}

// A database written by a newer release must not be opened: an older binary
// does not know what changed.
func TestNewerDatabaseIsRefused(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "future.db")

	st, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := st.writeFormatVersion(ctx, FormatVersion+1); err != nil {
		t.Fatalf("writeFormatVersion: %v", err)
	}
	st.Close()

	_, err = Open(ctx, path)
	if err == nil {
		t.Fatal("Open accepted a database from a newer release")
	}
	if !strings.Contains(err.Error(), "newer release") {
		t.Errorf("error does not explain the problem: %v", err)
	}
}

// Two or more majors behind is refused rather than chained: each step is only
// tested against the release immediately before it.
func TestJumpingMoreThanOneMajorIsRefused(t *testing.T) {
	tests := []struct {
		name    string
		from    int
		to      int
		wantErr string
	}{
		{"adjacent is allowed", 0, 1, ""},
		{"two majors is refused", 0, 2, "one major version at a time"},
		{"three majors is refused", 1, 4, "one major version at a time"},
		{"downgrade is refused", 2, 1, "newer than format"},
		{"same version needs no step", 3, 3, ""},
	}
	steps := []step{{from: 0, to: 1, notes: "test"}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := planSteps(tc.from, tc.to, steps)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("planSteps(%d, %d) = %v, want no error", tc.from, tc.to, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("planSteps(%d, %d) succeeded, want an error", tc.from, tc.to)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to mention %q", err, tc.wantErr)
			}
			if plan != nil {
				t.Errorf("plan = %v, want nil alongside the error", plan)
			}
		})
	}
}

// Every transition needs a step, even if the step does nothing: the registry is
// what documents that the major version was considered.
func TestMigrationRegistryCoversEveryTransition(t *testing.T) {
	if len(migrations) != FormatVersion {
		t.Fatalf("registry has %d entries but the format is %d: each major version needs one slot, empty if nothing changed",
			len(migrations), FormatVersion)
	}
	for i, st := range migrations {
		if st.from != i || st.to != i+1 {
			t.Errorf("migrations[%d] covers %d->%d, want %d->%d", i, st.from, st.to, i, i+1)
		}
		if st.notes == "" {
			t.Errorf("migrations[%d] has no notes describing what it does", i)
		}
	}
}

// Building the current schema from nothing and migrating an old database to it
// must land on the same thing, otherwise one of the two paths is wrong.
func TestMigratedSchemaMatchesFreshSchema(t *testing.T) {
	ctx := context.Background()

	fresh, err := Open(ctx, filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("Open fresh: %v", err)
	}
	defer fresh.Close()

	migrated, err := Open(ctx, makeV1Database(t, false))
	if err != nil {
		t.Fatalf("Open legacy: %v", err)
	}
	defer migrated.Close()

	freshCols, err := columnNames(ctx, fresh)
	if err != nil {
		t.Fatalf("columns of fresh database: %v", err)
	}
	migratedCols, err := columnNames(ctx, migrated)
	if err != nil {
		t.Fatalf("columns of migrated database: %v", err)
	}

	if len(freshCols) != len(migratedCols) {
		t.Fatalf("column counts differ:\n fresh    %v\n migrated %v", freshCols, migratedCols)
	}
	for i := range freshCols {
		if freshCols[i] != migratedCols[i] {
			t.Errorf("column %d differs: fresh %q, migrated %q", i, freshCols[i], migratedCols[i])
		}
	}
}

// columnNames lists every column as "table.name", in a stable order.
func columnNames(ctx context.Context, st *Store) ([]string, error) {
	rows, err := st.DB().QueryContext(ctx,
		`SELECT m.name || '.' || p.name
		   FROM sqlite_master AS m
		   JOIN pragma_table_info(m.name) AS p
		  WHERE m.type = 'table' AND m.name IN ('events', 'reminders', 'meta')
		  ORDER BY m.name, p.cid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// A file that is not a database at all must be reported clearly rather than
// producing a confusing migration error.
func TestUnreadableDatabaseIsReported(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "garbage.db")
	if err := writeFile(path, "this is not a sqlite database"); err != nil {
		t.Fatalf("write garbage file: %v", err)
	}

	_, err := Open(ctx, path)
	if err == nil {
		t.Fatal("Open accepted a file that is not a database")
	}
	if errors.Is(err, sql.ErrNoRows) {
		t.Errorf("error is not descriptive: %v", err)
	}
}

// modelEventWithLocation builds a minimal event carrying a location.
func modelEventWithLocation(title, location string) model.Event {
	end := int64(1773208800)
	return model.Event{
		Title:     title,
		Location:  &location,
		StartAt:   1773205200,
		EndAt:     &end,
		CreatedAt: 1000,
		UpdatedAt: 1000,
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
