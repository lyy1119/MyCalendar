package store

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyy1119/MyCalendar/internal/apperr"
	"github.com/lyy1119/MyCalendar/internal/calendar"
	"github.com/lyy1119/MyCalendar/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func loc(t *testing.T, name string) *time.Location {
	t.Helper()
	l, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return l
}

// mkTimed builds a plain timed event spanning [start, end) in Unix seconds.
func mkTimed(title string, start, end int64) model.Event {
	return model.Event{
		Title:     title,
		StartAt:   start,
		EndAt:     &end,
		CreatedAt: 1_700_000_000,
		UpdatedAt: 1_700_000_000,
	}
}

// dayFilter builds the filter the API would build for one calendar day.
func dayFilter(t *testing.T, date string, l *time.Location) model.EventFilter {
	t.Helper()
	d, err := calendar.ParseDate(date, l)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", date, err)
	}
	w := calendar.Day(d, l)
	return model.EventFilter{
		Start:     w.Start,
		End:       w.End,
		StartDate: w.StartDate,
		EndDate:   w.EndDate,
	}
}

func titles(events []model.Event) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Title)
	}
	return out
}

func TestOpenIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "again.db")

	first, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := first.CreateEvent(context.Background(), mkTimed("kept", 100, 200), nil); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("second Open (migration must be a no-op): %v", err)
	}
	defer second.Close()

	if _, err := second.GetEvent(context.Background(), 1); err != nil {
		t.Fatalf("event did not survive reopening: %v", err)
	}
}

func TestOpenRejectsUnsafePaths(t *testing.T) {
	for _, bad := range []string{"", "   ", "weird?.db", "hash#.db"} {
		if _, err := Open(context.Background(), bad); err == nil {
			t.Errorf("Open(%q) succeeded, want an error", bad)
		}
	}
}

// SQLite creates the database file but not its directory, so the store has to
// prepare one; otherwise a path like /var/lib/mycalendar/data.db fails on a
// first run with an opaque "unable to open database file".
func TestOpenCreatesMissingParentDirectory(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "data", "mycalendar.db")

	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("precondition failed: %s already exists", filepath.Dir(path))
	}

	st, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open with a missing parent directory: %v", err)
	}
	defer st.Close()

	if info, err := os.Stat(filepath.Dir(path)); err != nil || !info.IsDir() {
		t.Fatalf("parent directory was not created: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database file was not created: %v", err)
	}

	// The new database must be fully usable, not merely present.
	if _, err := st.CreateEvent(context.Background(), mkTimed("first run", 1_000, 2_000), nil); err != nil {
		t.Fatalf("CreateEvent on a freshly created database: %v", err)
	}
}

func TestOpenCreatesSeveralMissingDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c", "deep.db")

	st, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database file was not created: %v", err)
	}
}

func TestOpenRejectsPathsThatAreDirectories(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name string
		path string
	}{
		{"existing directory", dir},
		{"trailing separator", dir + string(os.PathSeparator)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Open(context.Background(), tc.path)
			if err == nil {
				t.Fatalf("Open(%q) succeeded, want an error", tc.path)
			}
			// The message must say what is wrong rather than leaking a bare
			// SQLite errno.
			if !strings.Contains(err.Error(), "directory") {
				t.Errorf("error %q does not mention the directory problem", err)
			}
		})
	}
}

func TestOpenRejectsFileInPlaceOfDirectory(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err := Open(context.Background(), filepath.Join(blocker, "data.db"))
	if err == nil {
		t.Fatal("Open beneath a regular file succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("error %q does not explain the problem", err)
	}
}

func TestOpenInMemory(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:): %v", err)
	}
	defer st.Close()

	if _, err := st.CreateEvent(ctx, mkTimed("in memory", 1_000, 2_000), nil); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	events, err := st.ListEvents(ctx, model.EventFilter{
		Start:     time.Unix(0, 0),
		End:       time.Unix(10_000, 0),
		StartDate: "1970-01-01",
		EndDate:   "1970-01-02",
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("in-memory database returned %d events, want 1", len(events))
	}
}

// A path relative to the working directory needs no directory preparation.
func TestOpenWithBareFilename(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	st, err := Open(context.Background(), "relative.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	if _, err := os.Stat(filepath.Join(dir, "relative.db")); err != nil {
		t.Fatalf("database file was not created in the working directory: %v", err)
	}
}

func TestCreateGetUpdateDelete(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	created, err := st.CreateEvent(ctx, mkTimed("Standup", 1_000, 2_000), []model.ReminderInput{{RemindAt: 900}})
	if err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("CreateEvent returned id 0")
	}
	if len(created.Reminders) != 1 || created.Reminders[0].EventID != created.ID {
		t.Fatalf("reminder not linked to event: %+v", created.Reminders)
	}

	got, err := st.GetEvent(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if got.Title != "Standup" || got.StartAt != 1_000 || got.EndAt == nil || *got.EndAt != 2_000 {
		t.Fatalf("unexpected round trip: %+v", got)
	}

	got.Title = "Standup (moved)"
	newEnd := int64(2_500)
	got.EndAt = &newEnd
	got.UpdatedAt = 1_700_000_999
	updated, err := st.UpdateEvent(ctx, got, nil, false)
	if err != nil {
		t.Fatalf("UpdateEvent: %v", err)
	}
	if updated.Title != "Standup (moved)" || *updated.EndAt != 2_500 {
		t.Fatalf("update not persisted: %+v", updated)
	}
	if len(updated.Reminders) != 1 {
		t.Fatalf("reminders must be untouched when replaceReminders is false: %+v", updated.Reminders)
	}

	if err := st.DeleteEvent(ctx, created.ID); err != nil {
		t.Fatalf("DeleteEvent: %v", err)
	}
	if _, err := st.GetEvent(ctx, created.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("GetEvent after delete = %v, want ErrNotFound", err)
	}
	if err := st.DeleteEvent(ctx, created.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("second DeleteEvent = %v, want ErrNotFound", err)
	}
}

func TestDeleteCascadesToReminders(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	ev, err := st.CreateEvent(ctx, mkTimed("With reminders", 1_000, 2_000),
		[]model.ReminderInput{{RemindAt: 500}, {RemindAt: 900}})
	if err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}

	// The cascade relies on the foreign_keys pragma being on for this
	// connection, which the DSN applies to every pooled connection.
	var before int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM reminders WHERE event_id = ?`, ev.ID).Scan(&before); err != nil {
		t.Fatalf("count reminders: %v", err)
	}
	if before != 2 {
		t.Fatalf("reminder count = %d, want 2", before)
	}

	if err := st.DeleteEvent(ctx, ev.ID); err != nil {
		t.Fatalf("DeleteEvent: %v", err)
	}

	var after int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM reminders`).Scan(&after); err != nil {
		t.Fatalf("count reminders: %v", err)
	}
	if after != 0 {
		t.Fatalf("reminders after delete = %d, want 0 (cascade not applied)", after)
	}
}

func TestReplaceReminders(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	ev, err := st.CreateEvent(ctx, mkTimed("Task", 1_000, 2_000), []model.ReminderInput{{RemindAt: 500}})
	if err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}

	replaced, err := st.UpdateEvent(ctx, ev, []model.ReminderInput{{RemindAt: 100}, {RemindAt: 200}}, true)
	if err != nil {
		t.Fatalf("UpdateEvent: %v", err)
	}
	if len(replaced.Reminders) != 2 {
		t.Fatalf("reminders = %d, want 2", len(replaced.Reminders))
	}
	// Ordered by remind_at, and the old reminder is gone.
	if replaced.Reminders[0].RemindAt != 100 || replaced.Reminders[1].RemindAt != 200 {
		t.Fatalf("reminders not replaced or not ordered: %+v", replaced.Reminders)
	}

	cleared, err := st.UpdateEvent(ctx, ev, nil, true)
	if err != nil {
		t.Fatalf("UpdateEvent (clear): %v", err)
	}
	if len(cleared.Reminders) != 0 {
		t.Fatalf("reminders = %d, want 0", len(cleared.Reminders))
	}
}

// Multi-day and midnight-crossing events must appear in every window they
// overlap, and events that merely touch a boundary must not.
func TestListEventsUsesOverlapNotContainment(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	shanghai := loc(t, "Asia/Shanghai")

	at := func(day string, hour int) int64 {
		d, err := calendar.ParseDate(day, shanghai)
		if err != nil {
			t.Fatalf("ParseDate(%q): %v", day, err)
		}
		return d.Add(time.Duration(hour) * time.Hour).Unix()
	}

	// Spans three days.
	if _, err := st.CreateEvent(ctx, mkTimed("Conference", at("2026-03-09", 10), at("2026-03-11", 18)), nil); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	// Crosses midnight into the next day.
	if _, err := st.CreateEvent(ctx, mkTimed("Night shift", at("2026-03-10", 23), at("2026-03-11", 1)), nil); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	// Ends exactly when 2026-03-11 begins, so it belongs to the 10th only.
	if _, err := st.CreateEvent(ctx, mkTimed("Ends at midnight", at("2026-03-10", 22), at("2026-03-11", 0)), nil); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	// Sits entirely on the 9th.
	if _, err := st.CreateEvent(ctx, mkTimed("Only on the 9th", at("2026-03-09", 9), at("2026-03-09", 10)), nil); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}

	// Expected order is by start_at ascending, which is the documented
	// contract: "Conference" began on the 9th, so it sorts before the events
	// that start on the 10th.
	tests := []struct {
		day  string
		want []string
	}{
		{"2026-03-09", []string{"Only on the 9th", "Conference"}},
		{"2026-03-10", []string{"Conference", "Ends at midnight", "Night shift"}},
		{"2026-03-11", []string{"Conference", "Night shift"}},
		{"2026-03-12", nil},
	}
	for _, tc := range tests {
		t.Run(tc.day, func(t *testing.T) {
			events, err := st.ListEvents(ctx, dayFilter(t, tc.day, shanghai))
			if err != nil {
				t.Fatalf("ListEvents: %v", err)
			}
			got := titles(events)
			if len(got) != len(tc.want) {
				t.Fatalf("day %s = %v, want %v", tc.day, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("day %s = %v, want %v", tc.day, got, tc.want)
				}
			}
		})
	}
}

// An event with no end_at is a single instant and must still be found when it
// sits exactly on the first second of a window.
func TestListEventsFindsNullEndedEventOnWindowEdge(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	shanghai := loc(t, "Asia/Shanghai")

	d, err := calendar.ParseDate("2026-03-11", shanghai)
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}
	point := model.Event{
		Title:     "Instant",
		StartAt:   d.Unix(), // exactly local midnight
		CreatedAt: 1_700_000_000,
		UpdatedAt: 1_700_000_000,
	}
	if _, err := st.CreateEvent(ctx, point, nil); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}

	events, err := st.ListEvents(ctx, dayFilter(t, "2026-03-11", shanghai))
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("null-ended event on the window edge was not found: %v", titles(events))
	}

	// And it must not leak into the previous day.
	events, err = st.ListEvents(ctx, dayFilter(t, "2026-03-10", shanghai))
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("null-ended event leaked into the previous day: %v", titles(events))
	}
}

// An all-day event is located by its date, so it must be found in every
// timezone for which that calendar date is the queried date.
func TestListEventsMatchesAllDayByDateInAnyTimezone(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	shanghai := loc(t, "Asia/Shanghai")

	day, err := calendar.ParseDate("2026-03-31", shanghai)
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}
	dateStr := "2026-03-31"
	dayEnd := calendar.AddDays(day, 1, shanghai).Unix()
	allDay := model.Event{
		Title:     "All day",
		StartAt:   day.Unix(),
		EndAt:     &dayEnd,
		IsAllDay:  true,
		EventDate: &dateStr,
		CreatedAt: 1_700_000_000,
		UpdatedAt: 1_700_000_000,
	}
	if _, err := st.CreateEvent(ctx, allDay, nil); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}

	for _, zone := range []string{"Asia/Shanghai", "UTC", "America/New_York", "Pacific/Kiritimati", "Pacific/Midway"} {
		t.Run(zone, func(t *testing.T) {
			events, err := st.ListEvents(ctx, dayFilter(t, "2026-03-31", loc(t, zone)))
			if err != nil {
				t.Fatalf("ListEvents: %v", err)
			}
			if len(events) != 1 {
				t.Fatalf("all-day event not found in %s: %v", zone, titles(events))
			}
		})
	}
}

func TestListEventsFiltersAndPaging(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	shanghai := loc(t, "Asia/Shanghai")

	d, err := calendar.ParseDate("2026-03-11", shanghai)
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}
	base := d.Unix()

	mk := func(title string, offset int64, isTask, done bool) model.Event {
		end := base + offset + 60
		return model.Event{
			Title: title, StartAt: base + offset, EndAt: &end,
			IsTask: isTask, IsCompleted: done,
			CreatedAt: 1_700_000_000, UpdatedAt: 1_700_000_000,
		}
	}
	for _, ev := range []model.Event{
		mk("Alpha meeting", 3600, true, false),
		mk("Beta meeting", 7200, true, true),
		mk("Gamma lunch", 10800, false, false),
	} {
		if _, err := st.CreateEvent(ctx, ev, nil); err != nil {
			t.Fatalf("CreateEvent: %v", err)
		}
	}

	base_ := dayFilter(t, "2026-03-11", shanghai)

	t.Run("is_task", func(t *testing.T) {
		f := base_
		yes := true
		f.IsTask = &yes
		events, err := st.ListEvents(ctx, f)
		if err != nil {
			t.Fatalf("ListEvents: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("is_task=true returned %v, want 2 events", titles(events))
		}
	})

	t.Run("is_completed", func(t *testing.T) {
		f := base_
		done := true
		f.IsCompleted = &done
		events, err := st.ListEvents(ctx, f)
		if err != nil {
			t.Fatalf("ListEvents: %v", err)
		}
		if len(events) != 1 || events[0].Title != "Beta meeting" {
			t.Fatalf("is_completed=true returned %v, want Beta meeting", titles(events))
		}
	})

	t.Run("title search", func(t *testing.T) {
		f := base_
		f.Query = "meeting"
		events, err := st.ListEvents(ctx, f)
		if err != nil {
			t.Fatalf("ListEvents: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("q=meeting returned %v, want 2 events", titles(events))
		}
	})

	t.Run("like wildcards are literal", func(t *testing.T) {
		f := base_
		f.Query = "%"
		events, err := st.ListEvents(ctx, f)
		if err != nil {
			t.Fatalf("ListEvents: %v", err)
		}
		if len(events) != 0 {
			t.Fatalf("q=%% matched everything, escaping is broken: %v", titles(events))
		}
	})

	t.Run("limit and offset", func(t *testing.T) {
		f := base_
		f.Limit = 2
		events, err := st.ListEvents(ctx, f)
		if err != nil {
			t.Fatalf("ListEvents: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("limit=2 returned %d events", len(events))
		}
		f.Offset = 2
		events, err = st.ListEvents(ctx, f)
		if err != nil {
			t.Fatalf("ListEvents: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("limit=2 offset=2 returned %d events, want 1", len(events))
		}
	})

	t.Run("offset without limit", func(t *testing.T) {
		f := base_
		f.Offset = 1
		events, err := st.ListEvents(ctx, f)
		if err != nil {
			t.Fatalf("ListEvents: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("offset=1 without limit returned %d events, want 2", len(events))
		}
	})
}

// Concurrent writers must not surface "database is locked": the pool size and
// busy_timeout pragma have to absorb the contention.
func TestConcurrentCreates(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	const writers = 24
	var wg sync.WaitGroup
	errs := make(chan error, writers)

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ev := mkTimed("concurrent", int64(1_000+i), int64(2_000+i))
			if _, err := st.CreateEvent(ctx, ev, []model.ReminderInput{{RemindAt: int64(500 + i)}}); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent CreateEvent failed: %v", err)
	}

	var count int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != writers {
		t.Fatalf("stored %d events, want %d", count, writers)
	}
}

func TestListEventsEmptyResultIsNil(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	shanghai := loc(t, "Asia/Shanghai")

	events, err := st.ListEvents(ctx, dayFilter(t, "2026-03-11", shanghai))
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no events, got %v", titles(events))
	}
}
