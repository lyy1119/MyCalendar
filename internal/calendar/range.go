// Package calendar computes timezone-aware calendar windows.
//
// Every window is a half-open interval [Start, End) of absolute instants. All
// boundaries are derived from the wall clock of a specific location, which
// keeps day, week and month edges correct across DST transitions and in
// timezones whose offset is not a whole number of hours.
package calendar

import (
	"fmt"
	"strings"
	"time"

	"github.com/lyy1119/MyCalendar/internal/apperr"
	"github.com/lyy1119/MyCalendar/internal/model"
)

// Kind identifies which calendar window was requested.
type Kind string

// Supported window kinds.
const (
	KindDay    Kind = "day"
	KindWeek   Kind = "week"
	KindMonth  Kind = "month"
	KindCustom Kind = "custom"
)

// WeekStart selects the first day of a week window.
type WeekStart string

// Supported week starts.
const (
	Monday WeekStart = "monday"
	Sunday WeekStart = "sunday"
)

// ParseWeekStart validates a week_start parameter.
func ParseWeekStart(s string) (WeekStart, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(Monday), "":
		return Monday, nil
	case string(Sunday):
		return Sunday, nil
	default:
		return "", apperr.Param("week_start", `must be "monday" or "sunday"`)
	}
}

// ParseKind validates a range parameter.
func ParseKind(s string) (Kind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(KindDay):
		return KindDay, nil
	case string(KindWeek):
		return KindWeek, nil
	case string(KindMonth):
		return KindMonth, nil
	default:
		return "", apperr.Param("range", `must be "day", "week" or "month"`)
	}
}

// Window is a resolved calendar interval.
type Window struct {
	Kind      Kind      `json:"kind"`
	Start     time.Time `json:"-"`
	End       time.Time `json:"-"`
	StartDate string    `json:"start_date"`
	EndDate   string    `json:"end_date"`
	Timezone  string    `json:"timezone"`
	WeekStart WeekStart `json:"week_start,omitempty"`
}

// StartUnix returns the inclusive lower bound as a Unix timestamp.
func (w Window) StartUnix() int64 { return w.Start.Unix() }

// EndUnix returns the exclusive upper bound as a Unix timestamp.
func (w Window) EndUnix() int64 { return w.End.Unix() }

// StartRFC3339 renders the inclusive lower bound in the window timezone.
func (w Window) StartRFC3339() string { return w.Start.Format(time.RFC3339) }

// EndRFC3339 renders the exclusive upper bound in the window timezone.
func (w Window) EndRFC3339() string { return w.End.Format(time.RFC3339) }

// Midnight returns the local start of day containing t.
//
// Rebuilding the instant from the calendar date (instead of truncating a
// duration) is what makes this DST-safe: on a spring-forward day the local day
// genuinely starts at the first existing wall-clock instant.
func Midnight(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// AddDays returns the local start of day n days after t.
func AddDays(t time.Time, n int, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d+n, 0, 0, 0, 0, loc)
}

// ParseDate parses a strict YYYY-MM-DD calendar date in loc.
func ParseDate(s string, loc *time.Location) (time.Time, error) {
	t, err := time.ParseInLocation(model.DateLayout, strings.TrimSpace(s), loc)
	if err != nil {
		return time.Time{}, apperr.Param("date", "must be formatted as YYYY-MM-DD")
	}
	// Reject impossible dates such as 2026-02-30, which time.Parse normalises.
	y, m, d := t.Date()
	if fmt.Sprintf("%04d-%02d-%02d", y, int(m), d) != strings.TrimSpace(s) {
		return time.Time{}, apperr.Param("date", "is not a real calendar date")
	}
	return t, nil
}

// Day returns the window covering the whole of the day containing date.
func Day(date time.Time, loc *time.Location) Window {
	start := Midnight(date, loc)
	end := AddDays(start, 1, loc)
	return newWindow(KindDay, start, end, loc, Monday)
}

// Week returns the window covering the week containing date.
//
// The window starts at local midnight on the configured first day of the week
// and ends exactly 7 local days later, so it spans 167 or 169 absolute hours
// across a DST transition rather than being a fixed 168 hours.
func Week(date time.Time, loc *time.Location, ws WeekStart) Window {
	start := Midnight(date, loc)
	offset := weekdayOffset(start.In(loc).Weekday(), ws)
	start = AddDays(start, -offset, loc)
	end := AddDays(start, 7, loc)
	return newWindow(KindWeek, start, end, loc, ws)
}

// Month returns the window covering the calendar month containing date.
func Month(date time.Time, loc *time.Location) Window {
	y, m, _ := date.In(loc).Date()
	start := time.Date(y, m, 1, 0, 0, 0, 0, loc)
	end := time.Date(y, m+1, 1, 0, 0, 0, 0, loc)
	return newWindow(KindMonth, start, end, loc, Monday)
}

// Custom builds a window from explicit absolute bounds.
func Custom(start, end time.Time, loc *time.Location, ws WeekStart) (Window, error) {
	if !end.After(start) {
		return Window{}, apperr.Param("to", "must be later than from")
	}
	return newWindow(KindCustom, start.In(loc), end.In(loc), loc, ws), nil
}

func newWindow(kind Kind, start, end time.Time, loc *time.Location, ws WeekStart) Window {
	return Window{
		Kind:      kind,
		Start:     start,
		End:       end,
		StartDate: start.In(loc).Format(model.DateLayout),
		EndDate:   LastTouchedDate(start, end, loc),
		Timezone:  loc.String(),
		WeekStart: ws,
	}
}

// LastTouchedDate returns the exclusive end date of the calendar days the
// half-open interval [start, end) covers.
//
// For whole-day windows this is simply the day after the last day, but using
// the last *touched* instant also makes partial-day windows behave sensibly:
// a window of 09:00-17:00 on the 10th still covers the calendar day of the
// 10th, so an all-day event on the 10th is included.
func LastTouchedDate(start, end time.Time, loc *time.Location) string {
	last := end.Add(-time.Nanosecond)
	if !last.After(start) {
		last = start
	}
	y, m, d := last.In(loc).Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, loc).Format(model.DateLayout)
}

func weekdayOffset(wd time.Weekday, ws WeekStart) int {
	if ws == Sunday {
		return int(wd)
	}
	return (int(wd) + 6) % 7
}

// Intersects reports whether the half-open instant interval [start, end)
// overlaps [w.Start, w.End). A null-ended event is treated as a one-second
// instant by the caller before this check.
func (w Window) Intersects(start, end int64) bool {
	return start < w.EndUnix() && end > w.StartUnix()
}
