package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lyy1119/MyCalendar/internal/apperr"
	"github.com/lyy1119/MyCalendar/internal/model"
	"github.com/lyy1119/MyCalendar/internal/store"
)

func newTestService(t *testing.T, zone string) *Service {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", zone, err)
	}
	return New(st, loc)
}

// set is a small builder for the Optional-typed request fields.
func set[T any](v T) model.Optional[T] { return model.Optional[T]{Set: true, Value: &v} }

func null[T any]() model.Optional[T] { return model.Optional[T]{Set: true, Value: nil} }

// mustInvalid runs a create and asserts it fails with a field-level error on
// the named field.
func mustInvalid(t *testing.T, svc *Service, in model.EventInput, field string) {
	t.Helper()
	_, err := svc.Create(context.Background(), in, nil)
	if err == nil {
		t.Fatalf("Create succeeded, want a validation error on %q", field)
	}
	var verr *apperr.Validation
	if !asValidation(err, &verr) {
		t.Fatalf("Create error = %v (%T), want *apperr.Validation", err, err)
	}
	for _, d := range verr.Details {
		if d.Field == field {
			return
		}
	}
	t.Fatalf("validation details %v do not mention %q", verr.Details, field)
}

func asValidation(err error, target **apperr.Validation) bool {
	v, ok := err.(*apperr.Validation)
	if ok {
		*target = v
	}
	return ok
}

func TestCreateTrimsTitleAndRequiresIt(t *testing.T) {
	svc := newTestService(t, "UTC")

	ev, err := svc.Create(context.Background(), model.EventInput{
		Title:   set("  Padded title  "),
		StartAt: set(int64(1_000_000)),
	}, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ev.Title != "Padded title" {
		t.Errorf("title = %q, want it trimmed", ev.Title)
	}

	mustInvalid(t, svc, model.EventInput{Title: set("   "), StartAt: set(int64(1))}, "title")
	mustInvalid(t, svc, model.EventInput{StartAt: set(int64(1))}, "title")
	mustInvalid(t, svc, model.EventInput{Title: null[string](), StartAt: set(int64(1))}, "title")
}

func TestLengthLimits(t *testing.T) {
	svc := newTestService(t, "UTC")
	ctx := context.Background()

	atLimit := strings.Repeat("a", MaxTitleRunes)
	if _, err := svc.Create(ctx, model.EventInput{Title: set(atLimit), StartAt: set(int64(1))}, nil); err != nil {
		t.Errorf("title at the limit was rejected: %v", err)
	}

	overLimit := strings.Repeat("a", MaxTitleRunes+1)
	mustInvalid(t, svc, model.EventInput{Title: set(overLimit), StartAt: set(int64(1))}, "title")

	notesOver := strings.Repeat("n", MaxNotesRunes+1)
	mustInvalid(t, svc, model.EventInput{
		Title:   set("ok"),
		StartAt: set(int64(1)),
		Notes:   set(notesOver),
	}, "notes")

	// Counting characters rather than bytes matters for non-ASCII titles.
	cjk := strings.Repeat("日", MaxTitleRunes)
	if _, err := svc.Create(ctx, model.EventInput{Title: set(cjk), StartAt: set(int64(1))}, nil); err != nil {
		t.Errorf("a %d-character CJK title was rejected: %v", MaxTitleRunes, err)
	}
	mustInvalid(t, svc, model.EventInput{Title: set(cjk + "日"), StartAt: set(int64(1))}, "title")
}

func TestReminderLimitsAndNormalisation(t *testing.T) {
	svc := newTestService(t, "UTC")

	tooMany := make([]model.ReminderInput, 0, MaxReminders+1)
	for i := 0; i <= MaxReminders; i++ {
		tooMany = append(tooMany, model.ReminderInput{RemindAt: int64(1_000 + i)})
	}
	mustInvalid(t, svc, model.EventInput{
		Title:     set("too many"),
		StartAt:   set(int64(1)),
		Reminders: set(tooMany),
	}, "reminders")

	ev, err := svc.Create(context.Background(), model.EventInput{
		Title:   set("dedupe"),
		StartAt: set(int64(1)),
		Reminders: set([]model.ReminderInput{
			{RemindAt: 300},
			{RemindAt: 100},
			{RemindAt: 300},
			{RemindAt: 200},
		}),
	}, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(ev.Reminders) != 3 {
		t.Fatalf("reminders = %d, want 3 after de-duplication", len(ev.Reminders))
	}
	for i, want := range []int64{100, 200, 300} {
		if ev.Reminders[i].RemindAt != want {
			t.Errorf("reminder[%d] = %d, want %d (sorted)", i, ev.Reminders[i].RemindAt, want)
		}
	}
}

func TestTimestampBounds(t *testing.T) {
	svc := newTestService(t, "UTC")

	// The accepted window is [0, maxTimestamp).
	if _, err := svc.Create(context.Background(), model.EventInput{
		Title:   set("epoch"),
		StartAt: set(int64(0)),
	}, nil); err != nil {
		t.Errorf("timestamp 0 was rejected: %v", err)
	}

	mustInvalid(t, svc, model.EventInput{Title: set("negative"), StartAt: set(int64(-1))}, "start_at")
	mustInvalid(t, svc, model.EventInput{Title: set("too far"), StartAt: set(maxTimestamp)}, "start_at")
	mustInvalid(t, svc, model.EventInput{Title: set("ms"), StartAt: set(maxTimestamp * 2)}, "start_at")
	mustInvalid(t, svc, model.EventInput{Title: set("no start"), StartAt: null[int64]()}, "start_at")
}

func TestAllDayNormalisation(t *testing.T) {
	svc := newTestService(t, "Asia/Shanghai")
	ctx := context.Background()

	ev, err := svc.Create(ctx, model.EventInput{
		Title:     set("Holiday"),
		EventDate: set("2026-03-31"),
	}, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !ev.IsAllDay {
		t.Error("is_all_day was not inferred from event_date")
	}
	if ev.EventDate == nil || *ev.EventDate != "2026-03-31" {
		t.Fatalf("event_date = %v", ev.EventDate)
	}

	wantStart := time.Date(2026, 3, 31, 0, 0, 0, 0, svc.Location()).Unix()
	wantEnd := time.Date(2026, 4, 1, 0, 0, 0, 0, svc.Location()).Unix()
	if ev.StartAt != wantStart {
		t.Errorf("start_at = %d, want local midnight %d", ev.StartAt, wantStart)
	}
	if ev.EndAt == nil || *ev.EndAt != wantEnd {
		t.Errorf("end_at = %v, want the next local midnight %d", ev.EndAt, wantEnd)
	}

	// An all-day event without a date anchor is rejected.
	mustInvalid(t, svc, model.EventInput{Title: set("no anchor"), IsAllDay: set(true)}, "event_date")

	// A bogus date is rejected rather than silently shifted.
	mustInvalid(t, svc, model.EventInput{Title: set("bad"), EventDate: set("2026-02-30")}, "event_date")
	mustInvalid(t, svc, model.EventInput{Title: set("bad"), EventDate: set("31/03/2026")}, "event_date")
}

// An all-day date derived from a bare start_at depends on the timezone the
// caller is thinking in.
func TestAllDayDerivedFromStartAtUsesProvidedLocation(t *testing.T) {
	svc := newTestService(t, "UTC")
	ctx := context.Background()
	const instant = int64(1773165600) // 2026-03-10T18:00Z == 2026-03-11T02:00+08:00

	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	utcEvent, err := svc.Create(ctx, model.EventInput{
		Title:    set("utc"),
		IsAllDay: set(true),
		StartAt:  set(instant),
	}, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if utcEvent.EventDate == nil || *utcEvent.EventDate != "2026-03-10" {
		t.Errorf("UTC event_date = %v, want 2026-03-10", utcEvent.EventDate)
	}

	shEvent, err := svc.Create(ctx, model.EventInput{
		Title:    set("shanghai"),
		IsAllDay: set(true),
		StartAt:  set(instant),
	}, shanghai)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if shEvent.EventDate == nil || *shEvent.EventDate != "2026-03-11" {
		t.Errorf("Shanghai event_date = %v, want 2026-03-11", shEvent.EventDate)
	}
}

func TestCompletionRequiresTask(t *testing.T) {
	svc := newTestService(t, "UTC")

	ev, err := svc.Create(context.Background(), model.EventInput{
		Title:       set("not a task"),
		StartAt:     set(int64(1_000)),
		IsCompleted: set(true),
	}, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ev.IsCompleted {
		t.Error("is_completed was kept true even though is_task is false")
	}
}

func TestTimedEventClearsEventDate(t *testing.T) {
	svc := newTestService(t, "UTC")

	ev, err := svc.Create(context.Background(), model.EventInput{
		Title:     set("timed"),
		StartAt:   set(int64(1_000)),
		EventDate: null[string](),
	}, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ev.EventDate != nil {
		t.Errorf("event_date = %v, want null for a timed event", ev.EventDate)
	}
}

// PATCH semantics at the service level: absent leaves the value alone, null
// clears it, and converting between anchorings re-derives the timestamps.
func TestUpdatePatchSemantics(t *testing.T) {
	svc := newTestService(t, "Asia/Shanghai")
	ctx := context.Background()

	created, err := svc.Create(ctx, model.EventInput{
		Title:   set("Original"),
		StartAt: set(int64(1773205200)),
		EndAt:   set(int64(1773208800)),
		Notes:   set("keep me"),
		IsTask:  set(true),
	}, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := created.ID

	t.Run("absent fields are untouched", func(t *testing.T) {
		got, err := svc.Update(ctx, id, model.EventInput{Title: set("Renamed")}, nil)
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got.Title != "Renamed" {
			t.Errorf("title = %q", got.Title)
		}
		if got.Notes == nil || *got.Notes != "keep me" {
			t.Errorf("notes = %v, want it preserved", got.Notes)
		}
		if got.StartAt != 1773205200 {
			t.Errorf("start_at = %d, want it preserved", got.StartAt)
		}
		if !got.IsTask {
			t.Error("is_task was cleared by an unrelated patch")
		}
	})

	t.Run("null clears notes", func(t *testing.T) {
		got, err := svc.Update(ctx, id, model.EventInput{Notes: null[string]()}, nil)
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got.Notes != nil {
			t.Errorf("notes = %v, want nil", got.Notes)
		}
	})

	t.Run("updated_at is refreshed but created_at is not", func(t *testing.T) {
		got, err := svc.Update(ctx, id, model.EventInput{Title: set("Touched")}, nil)
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got.CreatedAt != created.CreatedAt {
			t.Errorf("created_at = %d, want %d", got.CreatedAt, created.CreatedAt)
		}
		if got.UpdatedAt < created.UpdatedAt {
			t.Errorf("updated_at went backwards: %d < %d", got.UpdatedAt, created.UpdatedAt)
		}
	})

	t.Run("converting to all-day re-derives the timestamps", func(t *testing.T) {
		got, err := svc.Update(ctx, id, model.EventInput{EventDate: set("2026-06-01")}, nil)
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if !got.IsAllDay {
			t.Error("is_all_day = false, want true")
		}
		want := time.Date(2026, 6, 1, 0, 0, 0, 0, svc.Location()).Unix()
		if got.StartAt != want {
			t.Errorf("start_at = %d, want %d", got.StartAt, want)
		}
	})

	t.Run("clearing the date converts back to timed", func(t *testing.T) {
		got, err := svc.Update(ctx, id, model.EventInput{
			EventDate: null[string](),
			StartAt:   set(int64(1773212400)),
		}, nil)
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got.IsAllDay {
			t.Error("is_all_day = true, want false after clearing event_date")
		}
		if got.EventDate != nil {
			t.Errorf("event_date = %v, want nil and not re-derived", got.EventDate)
		}
	})

	t.Run("updating a missing event reports not found", func(t *testing.T) {
		_, err := svc.Update(ctx, 999_999, model.EventInput{Title: set("ghost")}, nil)
		if err == nil || !isNotFound(err) {
			t.Fatalf("Update error = %v, want apperr.ErrNotFound", err)
		}
	})
}

func isNotFound(err error) bool { return err == apperr.ErrNotFound }

func TestDeleteReportsMissing(t *testing.T) {
	svc := newTestService(t, "UTC")
	if err := svc.Delete(context.Background(), 4242); !isNotFound(err) {
		t.Fatalf("Delete error = %v, want apperr.ErrNotFound", err)
	}
}
