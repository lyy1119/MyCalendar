// Package api exposes MyCalendar over HTTP.
//
// The router uses the standard library's pattern matching (method + wildcard),
// so the service has no third-party HTTP dependency. Only the SQLite driver is
// external.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lyy1119/MyCalendar/internal/apperr"
	"github.com/lyy1119/MyCalendar/internal/calendar"
	"github.com/lyy1119/MyCalendar/internal/service"
	"github.com/lyy1119/MyCalendar/internal/webui"
)

// maxBodyBytes caps request bodies at 1 MiB.
const maxBodyBytes = 1 << 20

// Options carries the settings the API layer needs.
type Options struct {
	DefaultLocation  *time.Location
	DefaultWeekStart calendar.WeekStart
	CORSOrigins      []string
	AllowAllOrigins  bool
	LogRequests      bool
	// Frontend serves the single-page app. When nil, the handler from the
	// webui package is used, which is either the embedded bundle or a
	// placeholder page explaining how to build it.
	Frontend http.Handler
}

// Server wires the domain service to HTTP handlers.
type Server struct {
	svc      *service.Service
	log      *slog.Logger
	opts     Options
	version  string
	started  time.Time
	now      func() time.Time
	frontend http.Handler
}

// NewServer builds the API server. version is stamped at build time.
func NewServer(svc *service.Service, log *slog.Logger, version string, opts Options) *Server {
	if opts.DefaultLocation == nil {
		opts.DefaultLocation = time.UTC
	}
	if opts.DefaultWeekStart == "" {
		opts.DefaultWeekStart = calendar.Monday
	}
	frontend := opts.Frontend
	if frontend == nil {
		frontend = webui.Handler()
	}
	return &Server{
		svc:      svc,
		log:      log,
		opts:     opts,
		version:  version,
		started:  time.Now(),
		now:      time.Now,
		frontend: frontend,
	}
}

// Handler returns the fully assembled HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Machine-readable endpoints. The discovery document lives at /api now
	// that / belongs to the frontend. Unknown /api paths are handled by the
	// shared fallback below, not by a pattern here: a method-agnostic "/api/"
	// pattern would collide with the method-agnostic 405 handlers.
	mux.HandleFunc("GET /api", s.handleIndex)
	mux.HandleFunc("GET /help", s.handleHelp)
	mux.HandleFunc("GET /healthz", s.handleHealth)

	// Categories. Ids are 0 (the default) or a positive user category.
	mux.HandleFunc("GET /api/v1/categories", s.handleListCategories)
	mux.HandleFunc("POST /api/v1/categories", s.handleCreateCategory)
	mux.HandleFunc("/api/v1/categories", s.methodNotAllowed("GET, POST"))

	mux.HandleFunc("GET /api/v1/categories/{id}", s.handleGetCategory)
	mux.HandleFunc("PATCH /api/v1/categories/{id}", s.handlePatchCategory)
	mux.HandleFunc("DELETE /api/v1/categories/{id}", s.handleDeleteCategory)
	mux.HandleFunc("/api/v1/categories/{id}", s.methodNotAllowed("GET, PATCH, DELETE"))

	// DELETE moves an event to the trash; these two are the explicit verbs:
	// cancel calls it off in place, delete_forever destroys a trashed event.
	mux.HandleFunc("POST /api/v1/events/{id}/cancel", s.handleCancelEvent)
	mux.HandleFunc("/api/v1/events/{id}/cancel", s.methodNotAllowed("POST"))

	mux.HandleFunc("POST /api/v1/events/{id}/delete_forever", s.handleDeleteForeverEvent)
	mux.HandleFunc("/api/v1/events/{id}/delete_forever", s.methodNotAllowed("POST"))

	// Reminder collection.
	mux.HandleFunc("GET /api/v1/reminders", s.handleListReminders)
	mux.HandleFunc("POST /api/v1/reminders", s.handleCreateReminder)
	mux.HandleFunc("/api/v1/reminders", s.methodNotAllowed("GET, POST"))

	// Single reminder.
	mux.HandleFunc("GET /api/v1/reminders/{id}", s.handleGetReminder)
	mux.HandleFunc("PATCH /api/v1/reminders/{id}", s.handlePatchReminder)
	mux.HandleFunc("DELETE /api/v1/reminders/{id}", s.handleDeleteReminder)
	mux.HandleFunc("/api/v1/reminders/{id}", s.methodNotAllowed("GET, PATCH, DELETE"))

	// Event collection.
	mux.HandleFunc("GET /api/v1/events", s.handleListEvents)
	mux.HandleFunc("POST /api/v1/events", s.handleCreateEvent)
	mux.HandleFunc("/api/v1/events", s.methodNotAllowed("GET, POST"))

	// Single event.
	mux.HandleFunc("GET /api/v1/events/{id}", s.handleGetEvent)
	mux.HandleFunc("PATCH /api/v1/events/{id}", s.handlePatchEvent)
	mux.HandleFunc("DELETE /api/v1/events/{id}", s.handleDeleteEvent)
	mux.HandleFunc("/api/v1/events/{id}", s.methodNotAllowed("GET, PATCH, DELETE"))

	// One fallback owns everything else: unknown API paths, the single-page
	// app and its client-side routes.
	//
	// It is deliberately method-agnostic and registered last. A "GET /"
	// catch-all cannot coexist with the method-agnostic 405 handlers above,
	// because ServeMux rejects overlapping patterns where neither is strictly
	// more specific than the other.
	mux.HandleFunc("/", s.handleFallback)

	var h http.Handler = mux
	h = s.withCORS(h)
	h = s.withRecovery(h)
	if s.opts.LogRequests {
		h = s.withLogging(h)
	}
	return h
}

// ---- middleware --------------------------------------------------------

// withRecovery converts a panic into a 500 instead of killing the connection.
func (s *Server) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				s.log.Error("panic recovered", "error", fmt.Sprint(rec), "path", r.URL.Path)
				s.writeError(w, r, fmt.Errorf("internal error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// withLogging emits one structured line per request.
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

// withCORS answers preflight requests and echoes the allowed origin.
func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && s.originAllowed(origin) {
			if s.opts.AllowAllOrigins {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(origin string) bool {
	if s.opts.AllowAllOrigins {
		return true
	}
	for _, o := range s.opts.CORSOrigins {
		if strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// ---- responses ---------------------------------------------------------

// errorEnvelope is the single error shape used by every endpoint.
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Details []apperr.FieldError `json:"details,omitempty"`
}

// writeJSON serialises v with the given status code.
func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	buf, err := json.Marshal(v)
	if err != nil {
		s.log.Error("encode response", "error", err)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"internal_error","message":"failed to encode response"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf)
}

// writeError maps a domain error onto the documented envelope.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, body := classify(err)
	if status >= http.StatusInternalServerError {
		if !errors.Is(err, context.Canceled) {
			s.log.Error("request failed", "error", err, "method", r.Method, "path", r.URL.Path)
		}
		if body.Message == "" {
			body.Message = "an unexpected internal error occurred"
		}
	}
	s.writeJSON(w, status, errorEnvelope{Error: body})
}

// classify translates domain errors into HTTP status codes.
func classify(err error) (int, errorBody) {
	var (
		validation *apperr.Validation
		badParam   *apperr.BadParam
		conflict   *apperr.Conflict
		unmarshal  *json.UnmarshalTypeError
		syntax     *json.SyntaxError
		maxBytes   *http.MaxBytesError
	)

	switch {
	case errors.As(err, &validation):
		return http.StatusBadRequest, errorBody{
			Code:    "validation_failed",
			Message: "one or more fields are invalid",
			Details: validation.Details,
		}
	case errors.As(err, &badParam):
		details := []apperr.FieldError{{Field: badParam.Param, Message: badParam.Message}}
		return http.StatusBadRequest, errorBody{
			Code:    "invalid_parameter",
			Message: "one or more parameters are invalid",
			Details: details,
		}
	case errors.As(err, &unmarshal):
		return http.StatusBadRequest, errorBody{
			Code:    "invalid_body",
			Message: "request body has a field with the wrong type",
			Details: []apperr.FieldError{{Field: unmarshal.Field, Message: fmt.Sprintf("must be of type %s", unmarshal.Type)}},
		}
	case errors.As(err, &syntax):
		return http.StatusBadRequest, errorBody{
			Code:    "invalid_body",
			Message: "request body is not valid JSON: " + syntax.Error(),
		}
	case errors.Is(err, io.ErrUnexpectedEOF):
		// A truncated object such as `{"title":` decodes to this, which is a
		// client error rather than a server fault.
		return http.StatusBadRequest, errorBody{
			Code:    "invalid_body",
			Message: "request body is not valid JSON: unexpected end of input",
		}
	case errors.As(err, &maxBytes):
		return http.StatusRequestEntityTooLarge, errorBody{
			Code:    "body_too_large",
			Message: "request body must be at most 1 MiB",
		}
	case errors.As(err, &conflict):
		return http.StatusConflict, errorBody{
			Code:    conflict.Code,
			Message: conflict.Message,
		}
	case errors.Is(err, apperr.ErrNotFound):
		return http.StatusNotFound, errorBody{
			Code:    "not_found",
			Message: "the requested record does not exist",
		}
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// The client went away; the status is never seen, but keep it coherent.
		return http.StatusRequestTimeout, errorBody{
			Code:    "request_cancelled",
			Message: "the request was cancelled before it completed",
		}
	default:
		return http.StatusInternalServerError, errorBody{Code: "internal_error"}
	}
}

// decodeJSON strictly decodes the request body into dst.
//
// Unknown fields are rejected so that a misspelled field fails loudly instead
// of being silently ignored, which matters for an API driven by automated
// clients.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if ct := r.Header.Get("Content-Type"); ct != "" {
		mt, _, err := mime.ParseMediaType(ct)
		if err != nil || (mt != "application/json" && !strings.HasSuffix(mt, "+json")) {
			return apperr.Param("Content-Type", "must be application/json")
		}
	}
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return apperr.Param("body", "must not be empty; send a JSON object")
		}
		const unknown = "json: unknown field "
		if msg := err.Error(); strings.HasPrefix(msg, unknown) {
			name := strings.Trim(strings.TrimPrefix(msg, unknown), `"`)
			return apperr.Param(name, "is not a recognised field; see /help")
		}
		return err
	}
	if dec.More() {
		return apperr.Param("body", "must contain exactly one JSON object")
	}
	return nil
}

// ---- small handlers ----------------------------------------------------

func (s *Server) methodNotAllowed(allow string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", allow)
		s.writeJSON(w, http.StatusMethodNotAllowed, errorEnvelope{Error: errorBody{
			Code:    "method_not_allowed",
			Message: fmt.Sprintf("method %s is not supported here; allowed: %s", r.Method, allow),
		}})
	}
}

// handleFallback decides what happens for anything no explicit route matched.
//
// Three cases, in order: an unknown path under /api stays on the JSON error
// contract; a read of anything else is handed to the single-page app, which
// also makes client-side routes deep-linkable; and a write to anything else is
// refused rather than being answered with a page.
func (s *Server) handleFallback(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		s.handleAPINotFound(w, r)
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		s.writeJSON(w, http.StatusMethodNotAllowed, errorEnvelope{Error: errorBody{
			Code:    "method_not_allowed",
			Message: fmt.Sprintf("method %s is not supported on this path; allowed: GET, HEAD", r.Method),
		}})
		return
	}

	s.frontend.ServeHTTP(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	dbState := "ok"
	status := http.StatusOK
	if err := s.svc.Store().DB().PingContext(ctx); err != nil {
		dbState = "unavailable"
		status = http.StatusServiceUnavailable
		s.log.Error("database health check failed", "error", err)
	}
	s.writeJSON(w, status, map[string]any{
		"status":         map[bool]string{true: "ok", false: "degraded"}[status == http.StatusOK],
		"database":       dbState,
		"version":        s.version,
		"timezone":       s.opts.DefaultLocation.String(),
		"time":           time.Now().In(s.opts.DefaultLocation).Format(time.RFC3339),
		"uptime_seconds": int64(time.Since(s.started).Seconds()),
	})
}

// parseID extracts the {id} path segment.
func parseID(r *http.Request) (int64, error) {
	return parsePositiveInt("id", r.PathValue("id"))
}

// parsePositiveInt reads a positive integer from a path or query value.
func parsePositiveInt(param, raw string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0, apperr.Param(param, "must be a positive integer")
	}
	return value, nil
}

// parseSignedID reads an integer that may be negative, used for category ids:
// -1 is the deleted marker and 0 the uncategorized fallback.
func parseSignedID(param, raw string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, apperr.Param(param, "must be an integer")
	}
	return value, nil
}

// formatID renders an identifier for a Location header.
func formatID(id int64) string { return strconv.FormatInt(id, 10) }
