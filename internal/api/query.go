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

// parseCategoryQueryID reads a category id from a query parameter. Ids are 0 or
// positive: the -1 that once meant "deleted" is now a column on the event.
func parseCategoryQueryID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0, apperr.Param("category_id", "must be a non-negative integer")
	}
	return id, nil
}

// parseInstant reads a window bound, reporting problems against the parameter
// that carried it.
//
// The accepted forms are shared with the request body: Unix seconds, RFC3339
// with an offset, or a wall-clock value read in the request timezone.
func parseInstant(param, raw string, loc *time.Location) (time.Time, error) {
	t, err := calendar.ParseInstant(raw, loc)
	if err != nil {
		return time.Time{}, apperr.Param(param, err.Error())
	}
	return t, nil
}

// parseFilter reads the optional list filters and pagination.
func parseFilter(r *http.Request, loc *time.Location) (model.EventFilter, error) {
	q := r.URL.Query()
	var f model.EventFilter

	if raw := strings.TrimSpace(q.Get("deleted")); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return f, apperr.Param("deleted", "must be true or false")
		}
		f.Deleted = &v
	}
	if raw := strings.TrimSpace(q.Get("canceled")); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return f, apperr.Param("canceled", "must be true or false")
		}
		f.Canceled = &v
	}
	if raw := strings.TrimSpace(q.Get("category_id")); raw != "" {
		id, err := parseCategoryQueryID(raw)
		if err != nil {
			return f, err
		}
		f.CategoryID = &id
	}
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
	// q_field decides which column q is matched against. Date is the only one
	// that needs resolving: a typed date becomes that day's bounds in tz.
	if raw := strings.TrimSpace(q.Get("q_field")); raw != "" {
		switch raw {
		case "title", "location", "notes", "date":
			f.SearchField = raw
		default:
			return f, apperr.Param("q_field", "must be one of title, location, notes, date")
		}
		if raw == "date" && f.Query != "" {
			day, err := calendar.ParseDate(f.Query, loc)
			if err != nil {
				return f, apperr.Param("q", "with q_field=date, q must be a date formatted as YYYY-MM-DD")
			}
			from := day.Unix()
			to := calendar.AddDays(day, 1, loc).Unix()
			f.SearchFrom, f.SearchTo = &from, &to
			f.Query = day.Format(model.DateLayout)
		}
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
