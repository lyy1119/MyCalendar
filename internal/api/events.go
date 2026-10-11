package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/lyy1119/MyCalendar/internal/model"
)

// listRange describes the window that was actually queried.
type listRange struct {
	Kind        string `json:"kind"`
	Timezone    string `json:"timezone"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	StartAt     int64  `json:"start_at"`
	EndAt       int64  `json:"end_at"`
	StartLocal  string `json:"start_at_local"`
	EndLocal    string `json:"end_at_local"`
	Interval    string `json:"interval"`
	WeekStart   string `json:"week_start,omitempty"`
	RangeLength int64  `json:"length_seconds"`
}

// listResponse is the payload of GET /api/v1/events.
type listResponse struct {
	Range   listRange         `json:"range"`
	Filters map[string]any    `json:"filters,omitempty"`
	Count   int               `json:"count"`
	Events  []model.EventView `json:"events"`
}

// handleListEvents serves day, week, month and explicit-window queries.
func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	loc, err := s.resolveLocation(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	ws, err := s.resolveWeekStart(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	window, err := s.resolveWindow(r, loc, ws)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	filter, err := parseFilter(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	filter.Start = window.Start
	filter.End = window.End
	filter.StartDate = window.StartDate
	filter.EndDate = window.EndDate

	events, err := s.svc.List(r.Context(), filter)
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	resp := listResponse{
		Range: listRange{
			Kind:        string(window.Kind),
			Timezone:    window.Timezone,
			StartDate:   window.StartDate,
			EndDate:     window.EndDate,
			StartAt:     window.StartUnix(),
			EndAt:       window.EndUnix(),
			StartLocal:  window.StartRFC3339(),
			EndLocal:    window.EndRFC3339(),
			Interval:    "[start_at, end_at)",
			RangeLength: window.EndUnix() - window.StartUnix(),
		},
		Count:  len(events),
		Events: model.NewEventViews(events, loc),
	}
	if window.WeekStart != "" && window.Kind == "week" {
		resp.Range.WeekStart = string(window.WeekStart)
	}
	if f := filterSummary(filter); len(f) > 0 {
		resp.Filters = f
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func filterSummary(f model.EventFilter) map[string]any {
	out := map[string]any{}
	if f.IsTask != nil {
		out["is_task"] = *f.IsTask
	}
	if f.IsCompleted != nil {
		out["is_completed"] = *f.IsCompleted
	}
	if f.Query != "" {
		out["q"] = f.Query
	}
	if f.Limit > 0 {
		out["limit"] = f.Limit
	}
	if f.Offset > 0 {
		out["offset"] = f.Offset
	}
	return out
}

// handleCreateEvent creates an event.
func (s *Server) handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	loc, err := s.resolveLocation(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var in model.EventInput
	if err := decodeJSON(w, r, &in); err != nil {
		s.writeError(w, r, err)
		return
	}
	ev, err := s.svc.Create(r.Context(), in, loc)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/events/"+strconv.FormatInt(ev.ID, 10))
	s.writeJSON(w, http.StatusCreated, model.NewEventView(ev, loc))
}

// handleGetEvent returns one event.
func (s *Server) handleGetEvent(w http.ResponseWriter, r *http.Request) {
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
	ev, err := s.svc.Get(r.Context(), id)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, model.NewEventView(ev, loc))
}

// handlePatchEvent applies a partial update.
func (s *Server) handlePatchEvent(w http.ResponseWriter, r *http.Request) {
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
	var in model.EventInput
	if err := decodeJSON(w, r, &in); err != nil {
		s.writeError(w, r, err)
		return
	}
	ev, err := s.svc.Update(r.Context(), id, in, loc)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, model.NewEventView(ev, loc))
}

// handleDeleteEvent removes an event and its reminders.
func (s *Server) handleDeleteEvent(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.svc.Delete(r.Context(), id); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAPINotFound keeps unknown API paths on the JSON error contract rather
// than letting them fall through to the single-page app.
func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusNotFound, errorEnvelope{Error: errorBody{
		Code:    "not_found",
		Message: "no such API endpoint; see /help for the API reference",
	}})
}

// handleIndex is the discovery document, served at /api.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{
		"service":     "MyCalendar",
		"description": "A small self-hosted calendar API: events with reminders, queried by day, week or month.",
		"version":     s.version,
		"frontend":    "/",
		"help":        "/help",
		"health":      "/healthz",
		"api_base":    "/api/v1",
		"authentication": map[string]any{
			"required": false,
			"note":     "This service performs no authentication. Keep it on a trusted network.",
		},
		"timezone": map[string]any{
			"server_default": s.opts.DefaultLocation.String(),
			"override_param": "tz",
		},
		"endpoints": []map[string]string{
			{"method": "GET", "path": "/help", "summary": "Full API reference (JSON, or text/plain for Markdown)"},
			{"method": "GET", "path": "/healthz", "summary": "Liveness and database health"},
			{"method": "POST", "path": "/api/v1/events", "summary": "Create an event"},
			{"method": "GET", "path": "/api/v1/events", "summary": "List events in a day, week, month or explicit window"},
			{"method": "GET", "path": "/api/v1/events/{id}", "summary": "Fetch one event with its reminders"},
			{"method": "PATCH", "path": "/api/v1/events/{id}", "summary": "Partially update an event"},
			{"method": "DELETE", "path": "/api/v1/events/{id}", "summary": "Delete an event and its reminders"},
		},
		"server_time": time.Now().In(s.opts.DefaultLocation).Format(time.RFC3339),
	})
}
