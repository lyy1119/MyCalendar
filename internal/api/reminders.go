package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/lyy1119/MyCalendar/internal/apperr"
	"github.com/lyy1119/MyCalendar/internal/model"
	"github.com/lyy1119/MyCalendar/internal/service"
	"github.com/lyy1119/MyCalendar/internal/store"
)

// listRemindersResponse is the payload of GET /api/v1/reminders.
type listRemindersResponse struct {
	Timezone  string               `json:"timezone"`
	Filters   map[string]any       `json:"filters,omitempty"`
	Count     int                  `json:"count"`
	Reminders []model.ReminderView `json:"reminders"`
}

// handleListReminders serves the reminder collection.
func (s *Server) handleListReminders(w http.ResponseWriter, r *http.Request) {
	loc, err := s.resolveLocation(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	filter, summary, err := parseReminderFilter(r, loc)
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	reminders, err := s.svc.ListReminders(r.Context(), filter)
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	resp := listRemindersResponse{
		Timezone:  loc.String(),
		Count:     len(reminders),
		Reminders: model.NewReminderViews(reminders, loc),
	}
	if len(summary) > 0 {
		resp.Filters = summary
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// handleCreateReminder attaches a reminder to an event.
func (s *Server) handleCreateReminder(w http.ResponseWriter, r *http.Request) {
	loc, err := s.resolveLocation(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var in model.ReminderCreate
	if err := decodeJSON(w, r, &in); err != nil {
		s.writeError(w, r, err)
		return
	}

	reminder, err := s.svc.CreateReminder(r.Context(), in, loc)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/reminders/"+formatID(reminder.ID))
	s.writeJSON(w, http.StatusCreated, model.NewReminderView(reminder, loc))
}

// handleGetReminder returns one reminder.
func (s *Server) handleGetReminder(w http.ResponseWriter, r *http.Request) {
	loc, err := s.resolveLocation(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	id, err := parseID(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	reminder, err := s.svc.GetReminder(r.Context(), id)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, model.NewReminderView(reminder, loc))
}

// handlePatchReminder moves a reminder to a new time.
func (s *Server) handlePatchReminder(w http.ResponseWriter, r *http.Request) {
	loc, err := s.resolveLocation(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	id, err := parseID(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var in model.ReminderPatch
	if err := decodeJSON(w, r, &in); err != nil {
		s.writeError(w, r, err)
		return
	}
	reminder, err := s.svc.UpdateReminder(r.Context(), id, in, loc)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, model.NewReminderView(reminder, loc))
}

// handleDeleteReminder removes one reminder without touching its event.
func (s *Server) handleDeleteReminder(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.svc.DeleteReminder(r.Context(), id); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseReminderFilter reads the optional filters and pagination.
func parseReminderFilter(r *http.Request, loc *time.Location) (store.ReminderFilter, map[string]any, error) {
	q := r.URL.Query()
	var (
		filter  store.ReminderFilter
		summary = map[string]any{}
	)

	if raw := strings.TrimSpace(q.Get("event_id")); raw != "" {
		id, err := parsePositiveInt("event_id", raw)
		if err != nil {
			return filter, nil, err
		}
		filter.EventID = &id
		summary["event_id"] = id
	}
	if raw := strings.TrimSpace(q.Get("from")); raw != "" {
		t, err := parseInstant("from", raw, loc)
		if err != nil {
			return filter, nil, err
		}
		seconds := t.Unix()
		filter.From = &seconds
		summary["from"] = t.Format(time.RFC3339)
	}
	if raw := strings.TrimSpace(q.Get("to")); raw != "" {
		t, err := parseInstant("to", raw, loc)
		if err != nil {
			return filter, nil, err
		}
		seconds := t.Unix()
		filter.To = &seconds
		summary["to"] = t.Format(time.RFC3339)
	}
	if filter.From != nil && filter.To != nil && *filter.To <= *filter.From {
		return filter, nil, apperr.Param("to", "must be later than from")
	}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := parsePositiveInt("limit", raw)
		if err != nil {
			return filter, nil, err
		}
		if n > service.MaxPageSize {
			return filter, nil, apperr.Param("limit", "must be an integer between 0 and 2000 (0 means no limit)")
		}
		filter.Limit = int(n)
		summary["limit"] = filter.Limit
	}
	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		n, err := parsePositiveInt("offset", raw)
		if err != nil {
			return filter, nil, err
		}
		filter.Offset = int(n)
		summary["offset"] = filter.Offset
	}
	return filter, summary, nil
}
