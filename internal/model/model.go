// Package model defines the wire and storage types of MyCalendar.
//
// The struct tags mirror the SQLite schema one-to-one so that the JSON API and
// the database stay easy to reason about together.
package model

import (
	"encoding/json"
	"time"
)

// DateLayout is the calendar-date layout used for all-day events (event_date).
const DateLayout = "2006-01-02"

// Optional distinguishes an absent JSON field from an explicit JSON null.
//
// It is what makes PATCH semantics unambiguous: Set == false means "leave the
// column alone", Set == true with Value == nil means "clear the column", and
// Set == true with a non-nil Value means "assign this value".
type Optional[T any] struct {
	Set   bool
	Value *T
}

// UnmarshalJSON records that the field was present in the payload.
func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

// MarshalJSON renders the wrapped value, or null when it is unset.
func (o Optional[T]) MarshalJSON() ([]byte, error) {
	if o.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(o.Value)
}

// Event is a stored calendar entry.
type Event struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	Notes       *string    `json:"notes"`
	StartAt     int64      `json:"start_at"`
	EndAt       *int64     `json:"end_at"`
	IsAllDay    bool       `json:"is_all_day"`
	EventDate   *string    `json:"event_date"`
	IsTask      bool       `json:"is_task"`
	IsCompleted bool       `json:"is_completed"`
	CreatedAt   int64      `json:"created_at"`
	UpdatedAt   int64      `json:"updated_at"`
	Reminders   []Reminder `json:"reminders"`
}

// Reminder is a scheduled notification time attached to an event.
//
// Only storage and retrieval are implemented; no notification is ever
// delivered by this service.
type Reminder struct {
	ID        int64 `json:"id"`
	EventID   int64 `json:"event_id"`
	RemindAt  int64 `json:"remind_at"`
	CreatedAt int64 `json:"created_at"`
}

// FlexTime is a timestamp input that accepts either Unix seconds or a
// timestamp string.
//
// Strings are resolved against the request timezone, which is what lets a
// client send "2026-03-11T13:00" meaning 13:00 where it is, while the stored
// value stays an absolute UTC-based instant.
type FlexTime struct {
	Set bool
	// Number is the value when given as Unix seconds.
	Number *int64
	// Text is the value when given as a string, resolved later against tz.
	Text *string
}

// UnmarshalJSON accepts a number, a string, or null.
func (f *FlexTime) UnmarshalJSON(data []byte) error {
	f.Set = true
	f.Number, f.Text = nil, nil

	if string(data) == "null" {
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		f.Text = &text
		return nil
	}
	var number int64
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	f.Number = &number
	return nil
}

// IsNull reports an explicit JSON null.
func (f FlexTime) IsNull() bool { return f.Set && f.Number == nil && f.Text == nil }

// ReminderInput is the request shape for attaching a reminder.
type ReminderInput struct {
	RemindAt int64 `json:"remind_at"`
}

// EventInput is the request body for both POST (create) and PATCH (update).
//
// For POST, title plus a time anchor (start_at, or event_date for all-day) are
// required. For PATCH every field is optional.
type EventInput struct {
	Title       Optional[string]          `json:"title"`
	Notes       Optional[string]          `json:"notes"`
	StartAt     FlexTime                  `json:"start_at"`
	EndAt       FlexTime                  `json:"end_at"`
	IsAllDay    Optional[bool]            `json:"is_all_day"`
	EventDate   Optional[string]          `json:"event_date"`
	IsTask      Optional[bool]            `json:"is_task"`
	IsCompleted Optional[bool]            `json:"is_completed"`
	Reminders   Optional[[]ReminderInput] `json:"reminders"`
}

// EventView decorates an Event with request-scoped derived timestamps.
//
// The *_local fields are read-only conveniences rendered in the timezone that
// was used to resolve the request. They are never persisted.
type EventView struct {
	Event
	StartAtLocal string  `json:"start_at_local"`
	EndAtLocal   *string `json:"end_at_local"`
}

// EventFilter describes a range query against the events table.
//
// Start/End are absolute instants and form a half-open interval [Start, End).
// StartDate/EndDate are the same window expressed as calendar dates in the
// query timezone and are used to match all-day events, whose authoritative
// locator is event_date rather than a timestamp.
type EventFilter struct {
	Start       time.Time
	End         time.Time
	StartDate   string
	EndDate     string
	IsTask      *bool
	IsCompleted *bool
	Query       string
	Limit       int
	Offset      int
}

// NewEventView renders an Event in loc, adding derived local timestamps.
func NewEventView(ev Event, loc *time.Location) EventView {
	v := EventView{
		Event:        ev,
		StartAtLocal: time.Unix(ev.StartAt, 0).In(loc).Format(time.RFC3339),
	}
	if ev.EndAt != nil {
		s := time.Unix(*ev.EndAt, 0).In(loc).Format(time.RFC3339)
		v.EndAtLocal = &s
	}
	if v.Reminders == nil {
		v.Reminders = []Reminder{}
	}
	return v
}

// NewEventViews renders a slice of events, always returning a non-nil slice so
// that the JSON payload contains [] instead of null.
func NewEventViews(events []Event, loc *time.Location) []EventView {
	views := make([]EventView, 0, len(events))
	for _, ev := range events {
		views = append(views, NewEventView(ev, loc))
	}
	return views
}
