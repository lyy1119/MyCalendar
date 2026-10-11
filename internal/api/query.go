package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lyy1119/MyCalendar/internal/apperr"
	"github.com/lyy1119/MyCalendar/internal/calendar"
	"github.com/lyy1119/MyCalendar/internal/model"
	"github.com/lyy1119/MyCalendar/internal/service"
)

// resolveLocation returns the timezone for this request.
//
// The tz parameter is what lets one stored row be interpreted as the right
// calendar day for a client that is travelling, without changing the data.
func (s *Server) resolveLocation(r *http.Request) (*time.Location, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("tz"))
	if raw == "" {
		return s.opts.DefaultLocation, nil
	}
	loc, err := time.LoadLocation(raw)
	if err != nil {
		return nil, apperr.Param("tz", "is not a known IANA timezone name, for example Asia/Shanghai or UTC")
	}
	return loc, nil
}

// resolveWeekStart returns the first day of the week for this request.
func (s *Server) resolveWeekStart(r *http.Request) (calendar.WeekStart, error) {
	q := r.URL.Query()
	if !q.Has("week_start") {
		return s.opts.DefaultWeekStart, nil
	}
	return calendar.ParseWeekStart(q.Get("week_start"))
}

// resolveWindow turns the range parameters into a concrete interval.
//
// Two styles are accepted and must not be mixed:
//   - range=day|week|month with an optional date=YYYY-MM-DD
//   - from and to, an explicit half-open window
func (s *Server) resolveWindow(r *http.Request, loc *time.Location, ws calendar.WeekStart) (calendar.Window, error) {
	q := r.URL.Query()
	fromRaw := strings.TrimSpace(q.Get("from"))
	toRaw := strings.TrimSpace(q.Get("to"))
	rangeRaw := strings.TrimSpace(q.Get("range"))
	dateRaw := strings.TrimSpace(q.Get("date"))

	if fromRaw != "" || toRaw != "" {
		if rangeRaw != "" || dateRaw != "" {
			return calendar.Window{}, apperr.Param("range",
				"cannot be combined with from/to; use either a named range or an explicit window")
		}
		if fromRaw == "" || toRaw == "" {
			return calendar.Window{}, apperr.Param("from", "both from and to are required for an explicit window")
		}
		start, err := parseInstant("from", fromRaw, loc)
		if err != nil {
			return calendar.Window{}, err
		}
		end, err := parseInstant("to", toRaw, loc)
		if err != nil {
			return calendar.Window{}, err
		}
		return calendar.Custom(start, end, loc, ws)
	}

	kind := calendar.KindDay
	if rangeRaw != "" {
		k, err := calendar.ParseKind(rangeRaw)
		if err != nil {
			return calendar.Window{}, err
		}
		kind = k
	}

	day := s.now().In(loc)
	if dateRaw != "" {
		d, err := calendar.ParseDate(dateRaw, loc)
		if err != nil {
			return calendar.Window{}, err
		}
		day = d
	}

	switch kind {
	case calendar.KindDay:
		return calendar.Day(day, loc), nil
	case calendar.KindWeek:
		return calendar.Week(day, loc, ws), nil
	default:
		return calendar.Month(day, loc), nil
	}
}

// parseInstant accepts either Unix seconds or an RFC3339 timestamp.
func parseInstant(param, raw string, loc *time.Location) (time.Time, error) {
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return time.Unix(n, 0).In(loc), nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.In(loc), nil
	}
	return time.Time{}, apperr.Param(param, "must be RFC3339 (2026-03-10T09:00:00+08:00) or Unix seconds")
}

// parseFilter reads the optional list filters and pagination.
func parseFilter(r *http.Request) (model.EventFilter, error) {
	q := r.URL.Query()
	var f model.EventFilter

	if raw := strings.TrimSpace(q.Get("is_task")); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return f, apperr.Param("is_task", "must be true or false")
		}
		f.IsTask = &v
	}
	if raw := strings.TrimSpace(q.Get("is_completed")); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return f, apperr.Param("is_completed", "must be true or false")
		}
		f.IsCompleted = &v
	}
	if raw := strings.TrimSpace(q.Get("q")); raw != "" {
		if utf8.RuneCountInString(raw) > service.MaxQueryRunes {
			return f, apperr.Param("q", "must be at most 200 characters")
		}
		f.Query = raw
	}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > service.MaxPageSize {
			return f, apperr.Param("limit", "must be an integer between 0 and 2000 (0 means no limit)")
		}
		f.Limit = n
	}
	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return f, apperr.Param("offset", "must be a non-negative integer")
		}
		f.Offset = n
	}
	return f, nil
}
