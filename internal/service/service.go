// Package service holds MyCalendar's domain logic: it turns an optional-field
// request body into a fully specified, validated event row.
//
// All "what does this field mean" decisions live here so that the HTTP layer
// stays a thin translation of transport concerns, and the same rules apply no
// matter which client calls the API.
package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lyy1119/MyCalendar/internal/apperr"
	"github.com/lyy1119/MyCalendar/internal/calendar"
	"github.com/lyy1119/MyCalendar/internal/model"
	"github.com/lyy1119/MyCalendar/internal/store"
)

// Input limits enforced on write.
const (
	MaxTitleRunes    = 500
	MaxNotesRunes    = 20000
	MaxLocationRunes = 500
	MaxReminders     = 50
	MaxQueryRunes    = 200
	MaxPageSize      = 2000
	DefaultPageSize  = 0 // 0 means "no limit"
)

// Accepted range for Unix-second timestamps: 1970-01-01 through 2099-12-31.
// The upper bound exists to catch the very common bug of sending milliseconds.
const (
	minTimestamp = int64(0)
	maxTimestamp = int64(4102444800)
)

// Service implements event operations on top of the store.
type Service struct {
	store *store.Store
	loc   *time.Location
	now   func() time.Time
}

// New builds a Service bound to a store and a default location.
func New(st *store.Store, loc *time.Location) *Service {
	return &Service{store: st, loc: loc, now: time.Now}
}

// Store exposes the underlying store (used by health checks).
func (s *Service) Store() *store.Store { return s.store }

// Location returns the server's default timezone.
func (s *Service) Location() *time.Location { return s.loc }

// locOr falls back to the server timezone when no override was supplied.
func (s *Service) locOr(loc *time.Location) *time.Location {
	if loc == nil {
		return s.loc
	}
	return loc
}

// Create validates and inserts a new event.
//
// loc overrides the server timezone for date derivation (nil uses the default).
// It matters because an all-day event created from a bare start_at has its
// event_date derived from that instant, and the caller's timezone decides
// which calendar day that instant belongs to.
func (s *Service) Create(ctx context.Context, in model.EventInput, loc *time.Location) (model.Event, error) {
	ev, reminders, _, err := s.build(ctx, nil, in, s.locOr(loc))
	if err != nil {
		return model.Event{}, err
	}
	now := s.now().Unix()
	ev.CreatedAt = now
	ev.UpdatedAt = now
	return s.store.CreateEvent(ctx, ev, reminders)
}

// Get loads one event by id.
func (s *Service) Get(ctx context.Context, id int64) (model.Event, error) {
	return s.store.GetEvent(ctx, id)
}

// List runs a range query.
func (s *Service) List(ctx context.Context, f model.EventFilter) ([]model.Event, error) {
	return s.store.ListEvents(ctx, f)
}

// Update applies a partial update to an existing event.
func (s *Service) Update(ctx context.Context, id int64, in model.EventInput, loc *time.Location) (model.Event, error) {
	cur, err := s.store.GetEvent(ctx, id)
	if err != nil {
		return model.Event{}, err
	}
	ev, reminders, replaceReminders, err := s.build(ctx, &cur, in, s.locOr(loc))
	if err != nil {
		return model.Event{}, err
	}
	ev.ID = id
	ev.CreatedAt = cur.CreatedAt
	ev.UpdatedAt = s.now().Unix()
	return s.store.UpdateEvent(ctx, ev, reminders, replaceReminders)
}

// Cancel calls an event off without removing it.
//
// The event keeps its category and its place on the calendar, drawn as canceled,
// because "this was called off" is a fact about the event rather than a reason
// to hide it.
func (s *Service) Cancel(ctx context.Context, id int64) error {
	cur, err := s.store.GetEvent(ctx, id)
	if err != nil {
		return err
	}
	if cur.Canceled {
		return nil
	}
	cur.Canceled = true
	cur.UpdatedAt = s.now().Unix()
	_, err = s.store.UpdateEvent(ctx, cur, nil, false)
	return err
}

// Trash moves an event to the trash, which is what DELETE means.
//
// The row stays, so the event can still be read and restored; it simply leaves
// every listing. Destroying the row is DeleteForever, and only works from here.
func (s *Service) Trash(ctx context.Context, id int64) error {
	cur, err := s.store.GetEvent(ctx, id)
	if err != nil {
		return err
	}
	if cur.Deleted {
		return nil
	}
	cur.Deleted = true
	cur.UpdatedAt = s.now().Unix()
	_, err = s.store.UpdateEvent(ctx, cur, nil, false)
	return err
}

// DeleteForever destroys the row, reminders included.
//
// Only an event that is already in the trash can be destroyed: the two-step is
// deliberate, so nothing loses data by one careless call.
func (s *Service) DeleteForever(ctx context.Context, id int64) error {
	cur, err := s.store.GetEvent(ctx, id)
	if err != nil {
		return err
	}
	if !cur.Deleted {
		return apperr.NewConflict("not_in_trash",
			"this event is not in the trash; move it there first, or it is not what you meant to destroy")
	}
	return s.store.DeleteEvent(ctx, id)
}

// build merges a request body onto cur (nil when creating) and normalises the
// result. It returns the event to persist, the reminder times to apply as
// Unix seconds, and
// whether that reminder set should replace the stored one.
func (s *Service) build(ctx context.Context, cur *model.Event, in model.EventInput, loc *time.Location) (model.Event, []int64, bool, error) {
	verr := &apperr.Validation{}
	creating := cur == nil

	var ev model.Event
	if cur != nil {
		ev = *cur
	}

	// ---- scalar fields -------------------------------------------------

	if in.Title.Set {
		if in.Title.Value == nil {
			verr.Add("title", "must not be null")
		} else {
			ev.Title = strings.TrimSpace(*in.Title.Value)
		}
	}
	switch {
	case ev.Title == "":
		verr.Add("title", "is required and must not be empty")
	case utf8.RuneCountInString(ev.Title) > MaxTitleRunes:
		verr.Add("title", fmt.Sprintf("must be at most %d characters", MaxTitleRunes))
	}

	if in.Notes.Set {
		if in.Notes.Value == nil {
			ev.Notes = nil
		} else {
			n := *in.Notes.Value
			if utf8.RuneCountInString(n) > MaxNotesRunes {
				verr.Add("notes", fmt.Sprintf("must be at most %d characters", MaxNotesRunes))
			}
			ev.Notes = &n
		}
	}

	if in.Location.Set {
		if in.Location.Value == nil {
			ev.Location = nil
		} else {
			l := strings.TrimSpace(*in.Location.Value)
			if utf8.RuneCountInString(l) > MaxLocationRunes {
				verr.Add("location", fmt.Sprintf("must be at most %d characters", MaxLocationRunes))
			}
			if l == "" {
				// An emptied box means "no location", stored as NULL rather
				// than as an empty string that would read back as present.
				ev.Location = nil
			} else {
				ev.Location = &l
			}
		}
	}

	// Every event belongs to exactly one category. New ones start uncategorized
	// unless told otherwise.
	if creating {
		ev.CategoryID = store.CategoryUncategorized
	}
	if in.CategoryID.Set {
		if in.CategoryID.Value == nil {
			verr.Add("category_id", "must not be null")
		} else if s.resolveCategory(ctx, verr, *in.CategoryID.Value) {
			ev.CategoryID = *in.CategoryID.Value
		}
	}

	if in.IsTask.Set && in.IsTask.Value != nil {
		ev.IsTask = *in.IsTask.Value
	}
	if in.IsCompleted.Set && in.IsCompleted.Value != nil {
		ev.IsCompleted = *in.IsCompleted.Value
	}
	// canceled and deleted are ordinary fields on PATCH: setting deleted=false
	// is how an event comes back out of the trash.
	if in.Canceled.Set && in.Canceled.Value != nil {
		ev.Canceled = *in.Canceled.Value
	}
	if in.Deleted.Set && in.Deleted.Value != nil {
		ev.Deleted = *in.Deleted.Value
	}
	if in.IsAllDay.Set && in.IsAllDay.Value != nil {
		ev.IsAllDay = *in.IsAllDay.Value
	} else if in.EventDate.Set {
		// event_date and the anchoring move together: a concrete date means an
		// all-day event, and null means a timed one. Treating null as "clear the
		// column but stay all-day" would re-derive the very date the caller just
		// removed, so the field would appear to ignore the request.
		ev.IsAllDay = in.EventDate.Value != nil
	}

	// ---- time anchoring ------------------------------------------------

	startSet, startVal := false, int64(0)
	if in.StartAt.Set {
		if in.StartAt.IsNull() {
			verr.Add("start_at", "must not be null")
		} else if v, ok := resolveFlexTime(verr, "start_at", in.StartAt, loc); ok {
			startSet, startVal = true, v
		}
	}
	endSet, endVal := false, int64(0)
	if in.EndAt.Set {
		if in.EndAt.IsNull() {
			ev.EndAt = nil
		} else if v, ok := resolveFlexTime(verr, "end_at", in.EndAt, loc); ok {
			endSet, endVal = true, v
		}
	}
	dateSet := in.EventDate.Set && in.EventDate.Value != nil
	dateCleared := in.EventDate.Set && in.EventDate.Value == nil

	if ev.IsAllDay {
		// event_date is authoritative for all-day events. start_at and end_at
		// are derived from it so that the stored row always describes exactly
		// one local day; a supplied start_at is used only to derive the date.
		dateStr := ""
		switch {
		case dateSet:
			dateStr = *in.EventDate.Value
		case startSet:
			dateStr = time.Unix(startVal, 0).In(loc).Format(model.DateLayout)
		case cur != nil && !dateCleared && cur.EventDate != nil:
			dateStr = *cur.EventDate
		case cur != nil:
			dateStr = time.Unix(cur.StartAt, 0).In(loc).Format(model.DateLayout)
		default:
			verr.Add("event_date", "is required for an all-day event, or provide start_at to derive it")
		}

		if dateStr != "" {
			day, err := calendar.ParseDate(dateStr, loc)
			if err != nil {
				verr.Add("event_date", "must be a real date formatted as YYYY-MM-DD")
			} else {
				dateStr = day.Format(model.DateLayout)
				ev.EventDate = &dateStr
				ev.StartAt = day.Unix()
				ev.EndAt = ptr(calendar.AddDays(day, 1, loc).Unix())
			}
		}
	} else {
		// A timed event is located purely by absolute instants, so event_date
		// carries no meaning and is cleared.
		ev.EventDate = nil

		switch {
		case startSet:
			ev.StartAt = startVal
		case creating:
			verr.Add("start_at", "is required for a timed event")
		}

		if endSet {
			ev.EndAt = ptr(endVal)
		}

		validateTimestamp(verr, "start_at", ev.StartAt)
		if ev.EndAt != nil {
			validateTimestamp(verr, "end_at", *ev.EndAt)
			if *ev.EndAt < ev.StartAt {
				verr.Add("end_at", "must not be earlier than start_at")
			}
		}
	}

	// Completion only means something for a task.
	if !ev.IsTask {
		ev.IsCompleted = false
	}

	// ---- reminders -----------------------------------------------------

	replaceReminders := in.Reminders.Set
	var reminders []int64
	if replaceReminders && in.Reminders.Value != nil {
		list := *in.Reminders.Value
		if len(list) > MaxReminders {
			verr.Add("reminders", fmt.Sprintf("must contain at most %d entries", MaxReminders))
		}
		seen := make(map[int64]bool, len(list))
		for i, r := range list {
			field := fmt.Sprintf("reminders[%d].remind_at", i)
			if !r.RemindAt.Set || r.RemindAt.IsNull() {
				verr.Add(field, "is required")
				continue
			}
			// Each entry is resolved in the request timezone, exactly like a
			// standalone reminder.
			value, ok := resolveFlexTime(verr, field, r.RemindAt, loc)
			if !ok {
				continue
			}
			validateTimestamp(verr, field, value)
			if seen[value] {
				continue // duplicate reminder times collapse into one
			}
			seen[value] = true
			reminders = append(reminders, value)
		}
		slices.Sort(reminders)
	}

	if verr.Has() {
		return model.Event{}, nil, false, verr
	}
	return ev, reminders, replaceReminders, nil
}

// resolveFlexTime turns a timestamp that may have arrived as seconds or as a
// string into Unix seconds.
//
// A string without an offset is read in loc, so a client can send its own wall
// clock; the value that reaches the database is always an absolute instant.
func resolveFlexTime(verr *apperr.Validation, field string, in model.FlexTime, loc *time.Location) (int64, bool) {
	if in.Number != nil {
		return *in.Number, true
	}
	if in.Text != nil {
		t, err := calendar.ParseInstant(*in.Text, loc)
		if err != nil {
			verr.Add(field, err.Error())
			return 0, false
		}
		return t.Unix(), true
	}
	return 0, false
}

func validateTimestamp(verr *apperr.Validation, field string, ts int64) {
	if ts >= minTimestamp && ts < maxTimestamp {
		return
	}
	msg := fmt.Sprintf("must be a Unix timestamp in seconds between %d (1970-01-01) and %d (2099-12-31)", minTimestamp, maxTimestamp)
	if ts >= maxTimestamp && ts < maxTimestamp*1000 {
		msg += "; the value looks like milliseconds, but this API uses seconds"
	}
	verr.Add(field, msg)
}

func ptr[T any](v T) *T { return &v }
