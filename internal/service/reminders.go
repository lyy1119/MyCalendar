package service

import (
	"context"
	"fmt"
	"time"

	"github.com/lyy1119/MyCalendar/internal/apperr"
	"github.com/lyy1119/MyCalendar/internal/model"
	"github.com/lyy1119/MyCalendar/internal/store"
)

// Reminder operations.
//
// Reminders are stored as an absolute instant, exactly like event times, so the
// timezone a caller writes one in never changes what is stored. Delivering a
// reminder is out of scope here: this layer only stores and retrieves them.

// CreateReminder attaches a reminder to an existing event.
func (s *Service) CreateReminder(ctx context.Context, in model.ReminderCreate, loc *time.Location) (model.Reminder, error) {
	loc = s.locOr(loc)
	verr := &apperr.Validation{}

	eventID, ok := s.existingEventID(ctx, verr, in.EventID)
	if !ok {
		return model.Reminder{}, verr
	}

	remindAt, ok := s.remindAt(verr, in.RemindAt, loc)
	if !ok {
		return model.Reminder{}, verr
	}

	// Duplicates carry no meaning: the table has no other attributes, so two
	// identical rows would be indistinguishable.
	if existing, err := s.store.FindReminderAt(ctx, eventID, remindAt); err == nil {
		return model.Reminder{}, &apperr.Conflict{
			Code:    "reminder_exists",
			Message: fmt.Sprintf("event %d already has a reminder at that time (id %d)", eventID, existing.ID),
		}
	} else if err != apperr.ErrNotFound {
		return model.Reminder{}, err
	}

	count, err := s.store.CountRemindersForEvent(ctx, eventID)
	if err != nil {
		return model.Reminder{}, err
	}
	if count >= MaxReminders {
		verr.Add("remind_at", fmt.Sprintf("event %d already has the maximum of %d reminders", eventID, MaxReminders))
		return model.Reminder{}, verr
	}

	return s.store.CreateReminder(ctx, eventID, remindAt, s.now().Unix())
}

// GetReminder loads one reminder by id.
func (s *Service) GetReminder(ctx context.Context, id int64) (model.Reminder, error) {
	return s.store.GetReminder(ctx, id)
}

// ListReminders runs a filtered reminder query.
func (s *Service) ListReminders(ctx context.Context, f store.ReminderFilter) ([]model.Reminder, error) {
	return s.store.ListReminders(ctx, f)
}

// UpdateReminder moves a reminder to a new time.
func (s *Service) UpdateReminder(ctx context.Context, id int64, in model.ReminderPatch, loc *time.Location) (model.Reminder, error) {
	loc = s.locOr(loc)
	current, err := s.store.GetReminder(ctx, id)
	if err != nil {
		return model.Reminder{}, err
	}

	verr := &apperr.Validation{}
	remindAt, ok := s.remindAt(verr, in.RemindAt, loc)
	if !ok {
		return model.Reminder{}, verr
	}

	// Moving a reminder onto a time the same event already uses would create
	// the duplicate that CreateReminder refuses.
	if existing, err := s.store.FindReminderAt(ctx, current.EventID, remindAt); err == nil && existing.ID != id {
		return model.Reminder{}, &apperr.Conflict{
			Code:    "reminder_exists",
			Message: fmt.Sprintf("event %d already has a reminder at that time (id %d)", current.EventID, existing.ID),
		}
	} else if err != nil && err != apperr.ErrNotFound {
		return model.Reminder{}, err
	}

	return s.store.UpdateReminder(ctx, id, remindAt)
}

// DeleteReminder removes one reminder.
func (s *Service) DeleteReminder(ctx context.Context, id int64) error {
	return s.store.DeleteReminder(ctx, id)
}

// existingEventID resolves the event a new reminder belongs to, reporting a
// missing or absent id as a field error rather than a foreign key failure.
func (s *Service) existingEventID(ctx context.Context, verr *apperr.Validation, in model.Optional[int64]) (int64, bool) {
	if !in.Set || in.Value == nil {
		verr.Add("event_id", "is required")
		return 0, false
	}
	id := *in.Value
	if id <= 0 {
		verr.Add("event_id", "must be a positive integer")
		return 0, false
	}
	found, err := s.store.EventExists(ctx, id)
	if err != nil {
		verr.Add("event_id", "could not be verified: "+err.Error())
		return 0, false
	}
	if !found {
		verr.Add("event_id", fmt.Sprintf("event %d does not exist", id))
		return 0, false
	}
	return id, true
}

// remindAt resolves a reminder time and range checks it.
func (s *Service) remindAt(verr *apperr.Validation, in model.FlexTime, loc *time.Location) (int64, bool) {
	if !in.Set {
		verr.Add("remind_at", "is required")
		return 0, false
	}
	if in.IsNull() {
		verr.Add("remind_at", "must not be null")
		return 0, false
	}
	value, ok := resolveFlexTime(verr, "remind_at", in, loc)
	if !ok {
		return 0, false
	}
	validateTimestamp(verr, "remind_at", value)
	if verr.Has() {
		return 0, false
	}
	return value, true
}
