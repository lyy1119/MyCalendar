package api

import (
	"fmt"
	"net/http"
	"strings"
)

// The help document is the machine-readable contract of this service. It is
// written for automated clients (an Android app, an agent) as much as for a
// human, so every rule the implementation enforces appears here verbatim.

type helpDoc struct {
	Service        string          `json:"service"`
	Description    string          `json:"description"`
	Version        string          `json:"version"`
	APIVersion     string          `json:"api_version"`
	Authentication helpAuth        `json:"authentication"`
	QuickStart     []string        `json:"quick_start"`
	Conventions    helpConventions `json:"conventions"`
	Timezone       helpTimezone    `json:"timezone"`
	DataModel      []helpTable     `json:"data_model"`
	Semantics      helpSemantics   `json:"event_semantics"`
	RangeQueries   helpRanges      `json:"range_queries"`
	Endpoints      []helpEndpoint  `json:"endpoints"`
	BodyFields     []helpField     `json:"request_body_fields"`
	QueryParams    []helpField     `json:"query_parameters"`
	ResponseFields []helpField     `json:"response_fields"`
	ErrorCodes     []helpCode      `json:"error_codes"`
	Limits         helpLimits      `json:"limits"`
	Examples       []helpExample   `json:"examples"`
	NotImplemented []string        `json:"not_implemented"`
}

type helpAuth struct {
	Required bool   `json:"required"`
	Note     string `json:"note"`
}

type helpConventions struct {
	BasePath         string `json:"base_path"`
	ContentType      string `json:"content_type"`
	TimeEncoding     string `json:"time_encoding"`
	IntervalStyle    string `json:"interval_style"`
	UnknownFields    string `json:"unknown_fields"`
	EmptyCollections string `json:"empty_collections"`
	DeleteReturns    string `json:"delete_returns"`
}

type helpTimezone struct {
	ServerDefault    string   `json:"server_default"`
	Resolved         string   `json:"resolved_zone"`
	OverrideParam    string   `json:"override_param"`
	DefaultWeekStart string   `json:"default_week_start"`
	WeekStartParam   string   `json:"week_start_param"`
	Rules            []string `json:"rules"`
}

type helpTable struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Columns     []helpField `json:"columns"`
	Notes       []string    `json:"notes"`
}

type helpSemantics struct {
	TimedEvent  []string `json:"timed_event"`
	Deleting    []string `json:"deleting"`
	AllDayEvent []string `json:"all_day_event"`
	Task        []string `json:"task"`
	Reminders   []string `json:"reminders"`
	Patching    []string `json:"patching"`
}

type helpRanges struct {
	Day    string   `json:"day"`
	Week   string   `json:"week"`
	Month  string   `json:"month"`
	Custom string   `json:"custom"`
	Rules  []string `json:"rules"`
}

type helpEndpoint struct {
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	Summary     string   `json:"summary"`
	Parameters  []string `json:"parameters,omitempty"`
	Body        string   `json:"body,omitempty"`
	Success     string   `json:"success"`
	Errors      []string `json:"errors,omitempty"`
	CurlExample string   `json:"curl_example,omitempty"`
}

type helpField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    string `json:"required,omitempty"`
	Nullable    string `json:"nullable,omitempty"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description"`
	Constraints string `json:"constraints,omitempty"`
}

type helpCode struct {
	Code       string `json:"code"`
	HTTPStatus int    `json:"http_status"`
	Meaning    string `json:"meaning"`
}

type helpLimits struct {
	TitleRunes        int    `json:"title_max_characters"`
	NotesRunes        int    `json:"notes_max_characters"`
	LocationRunes     int    `json:"location_max_characters"`
	CategoryNameRunes int    `json:"category_name_max_characters"`
	RemindersPerEvent int    `json:"reminders_per_event_max"`
	SearchRunes       int    `json:"search_query_max_characters"`
	MaxPageSize       int    `json:"limit_max"`
	BodyBytes         string `json:"request_body_max"`
	TimestampRange    string `json:"timestamp_range"`
}

type helpExample struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Request     string `json:"request"`
	Response    string `json:"response"`
}

// handleHelp serves the API reference as JSON, or as Markdown when the client
// asks for text/plain.
func (s *Server) handleHelp(w http.ResponseWriter, r *http.Request) {
	doc := s.buildHelp()
	if wantsText(r) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(renderHelpText(doc)))
		return
	}
	s.writeJSON(w, http.StatusOK, doc)
}

// wantsText reports whether the client prefers a human-readable document.
func wantsText(r *http.Request) bool {
	accept := strings.ToLower(r.Header.Get("Accept"))
	if strings.Contains(accept, "text/plain") || strings.Contains(accept, "text/markdown") {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format"))) {
	case "text", "markdown", "md":
		return true
	}
	return false
}

func (s *Server) buildHelp() helpDoc {
	return helpDoc{
		Service:     "MyCalendar",
		Description: "A small self-hosted calendar service: create, query, update and delete events, with per-event reminders stored but not delivered.",
		Version:     s.version,
		APIVersion:  "v1",
		Authentication: helpAuth{
			Required: false,
			Note:     "No authentication, no API keys, no cookies. Every endpoint is open. Bind the service to a trusted network only, or put an authenticating reverse proxy in front of it.",
		},
		QuickStart: []string{
			"GET /help -- read this document",
			"POST /api/v1/events -- create an event",
			"GET /api/v1/events?range=day&date=2026-03-10&tz=Asia/Shanghai -- list one day",
			"PATCH /api/v1/events/1 -- change any subset of fields",
			"DELETE /api/v1/events/1 -- remove an event",
		},
		Conventions: helpConventions{
			BasePath:         "/api/v1",
			ContentType:      "Requests with a body must send Content-Type: application/json. Responses are always application/json; charset=utf-8.",
			TimeEncoding:     "Instants are stored as Unix timestamps in whole seconds, UTC-based, and every response carries them as start_at and end_at. A timestamp may be sent in any of three forms: Unix seconds (1773205200), RFC3339 with an offset (2026-03-11T13:00:00+08:00), or a wall-clock value with no offset (2026-03-11T13:00) which is read in the tz of the request. Responses additionally carry start_at_local and end_at_local, the same instant rendered as RFC3339 in the request timezone.",
			IntervalStyle:    "Every time window is half-open: [start, end). A window contains an instant if start <= instant < end.",
			UnknownFields:    "Rejected with 400 invalid_parameter. A misspelled field never silently disappears.",
			EmptyCollections: "List endpoints return [] rather than null. reminders is always an array.",
			DeleteReturns:    "204 No Content with an empty body.",
		},
		Timezone: helpTimezone{
			ServerDefault:    s.opts.DefaultLocation.String(),
			Resolved:         s.opts.DefaultLocation.String(),
			OverrideParam:    "tz",
			DefaultWeekStart: string(s.opts.DefaultWeekStart),
			WeekStartParam:   "week_start",
			Rules: []string{
				"Stored data is timezone-independent: start_at, end_at, created_at, updated_at and remind_at are absolute Unix seconds; event_date is a plain calendar date.",
				"The tz query parameter (an IANA name such as Asia/Shanghai, Europe/Berlin or UTC) decides how instants are projected onto calendar days, weeks and months for that request only. It never rewrites stored data.",
				"tz applies to GET, POST and PATCH. On POST or PATCH it also decides which calendar day a bare start_at belongs to when an all-day event's date has to be derived.",
				"When tz is absent, the server default is used.",
				"Day, week and month boundaries are built from local wall-clock midnight, so a day is 23, 24 or 25 hours long across a DST transition. The service ships with an embedded copy of the IANA database, so zone rules do not depend on the host's tzdata.",
				"The derived start_at_local and end_at_local fields in responses are rendered in the request's tz and are read-only.",
				"Timestamps may be sent either as Unix seconds or as strings. A string carrying an offset (2026-03-11T13:00:00+08:00) is absolute and tz does not affect it; a string without one (2026-03-11T13:00) is wall-clock time in tz. Either way the stored value is the same absolute instant, so records written by clients in different timezones stay comparable.",
			},
		},
		DataModel: []helpTable{
			{
				Name:        "events",
				Description: "One row per calendar entry. Stored in SQLite.",
				Columns: []helpField{
					{Name: "id", Type: "INTEGER PRIMARY KEY", Description: "Server-assigned identifier. Never sent by clients."},
					{Name: "title", Type: "TEXT NOT NULL", Description: "Event title, trimmed of surrounding whitespace.", Constraints: "1 to 500 characters"},
					{Name: "notes", Type: "TEXT NULL", Description: "Free-form notes, up to 20000 characters. Send null to clear."},
					{Name: "location", Type: "TEXT NULL", Description: "Where the event happens, up to 500 characters. Send null, or an empty string, to clear it."},
					{Name: "start_at", Type: "INTEGER NOT NULL", Description: "Start instant, Unix seconds. For an all-day event this is the local midnight of event_date."},
					{Name: "end_at", Type: "INTEGER NULL", Description: "Exclusive end instant, Unix seconds. Null means the event has no explicit end and is treated as a one-second instant when matching a window. For an all-day event this is the next local midnight."},
					{Name: "is_all_day", Type: "INTEGER NOT NULL DEFAULT 0", Description: "1 for an all-day event, 0 for a timed event."},
					{Name: "event_date", Type: "TEXT NULL", Description: "The calendar date YYYY-MM-DD of an all-day event. Null for timed events."},
					{Name: "is_task", Type: "INTEGER NOT NULL DEFAULT 0", Description: "1 if the event is completable."},
					{Name: "is_completed", Type: "INTEGER NOT NULL DEFAULT 0", Description: "1 if a task is done. Forced to 0 whenever is_task is 0."},
					{Name: "category_id", Type: "INTEGER NOT NULL DEFAULT 0", Description: "The event's single category. 0 means uncategorized; -1 means the event has been deleted, which is what deleting does."},
					{Name: "created_at", Type: "INTEGER NOT NULL", Description: "Insert instant, Unix seconds. Server-managed."},
					{Name: "updated_at", Type: "INTEGER NOT NULL", Description: "Last modification instant, Unix seconds. Server-managed, refreshed on every successful PATCH."},
				},
				Notes: []string{
					"Indexed on start_at, end_at, event_date, (is_all_day, is_task, is_completed) and category_id.",
					"The schema is fixed at the two tables described here; the service only adds indexes.",
				},
			},
			{
				Name:        "categories",
				Description: "Groups events and gives them a colour. Two rows are part of the API's contract.",
				Columns: []helpField{
					{Name: "id", Type: "INTEGER PRIMARY KEY", Description: "Category id. -1 marks an event as deleted and 0 is the default grouping; both are seeded and cannot be removed. User categories are assigned automatically and start at 1."},
					{Name: "name", Type: "TEXT NOT NULL", Description: "Display name, 1 to 100 characters. Category -1 is seeded as \"删除\" and that name is fixed; every other category, including 0 (\"默认\"), can be renamed."},
					{Name: "color", Type: "TEXT NOT NULL", Description: "Hex colour, #rgb or #rrggbb. Stored lower-cased and expanded to six digits, so #ABC becomes #aabbcc."},
					{Name: "display", Type: "INTEGER NOT NULL DEFAULT 1", Description: "Whether the sidebar shows this category. 1 for visible, 0 for hidden; the deleted category (-1) starts at 0. A stored preference, not an access rule: hidden categories keep their events and list queries still return them."},
				},
				Notes: []string{
					"Deleting a category moves its events to the default category (0) in the same transaction; no event is left pointing at a missing category.",
					"The two seeded rows are refused with 409 category_protected, because -1 is how an event is marked as deleted and 0 is the fallback.",
					"Only the deleted category starts hidden (display 0); new categories start visible. A client that wants a filtered calendar either honours display or passes category_id, since the list endpoints themselves do not read the column.",
					"Categories -1 and 0 start as 删除/#9aa0a6 and 默认/#1a73e8. Both colours can be changed. Only -1's name is fixed: renaming it is refused with 409 category_name_fixed, since \"-1 means deleted\" is a statement about the API rather than about a grouping the user chose.",
				},
			},
			{
				Name:        "reminders",
				Description: "Notification times attached to an event. Stored and returned, never delivered.",
				Columns: []helpField{
					{Name: "id", Type: "INTEGER PRIMARY KEY", Description: "Server-assigned identifier."},
					{Name: "event_id", Type: "INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE", Description: "Owning event. Deleting the event removes its reminders."},
					{Name: "remind_at", Type: "INTEGER NOT NULL", Description: "Reminder instant, Unix seconds."},
					{Name: "created_at", Type: "INTEGER NOT NULL", Description: "Insert instant, Unix seconds. Server-managed."},
				},
				Notes: []string{
					"Reminders are returned inside every event payload, sorted by remind_at.",
					"This service does not schedule, push or email reminders. It is a storage and retrieval API only; a client is expected to read remind_at and act on it.",
				},
			},
		},
		Semantics: helpSemantics{
			TimedEvent: []string{
				"Located in time by start_at and end_at and therefore matched against a window by instant.",
				"event_date is always null; sending one converts the event to all-day (see the note on inference below).",
				"start_at is required. end_at is optional; when absent the event behaves as a one-second instant that still lands inside the day it starts on.",
				"end_at must not be earlier than start_at.",
				"A timed event may span several days. It then appears in the result of every day, week and month window it overlaps, exactly once per query.",
			},
			AllDayEvent: []string{
				"The authoritative locator is event_date, the string YYYY-MM-DD. A date is the same date in every timezone, so all-day results stay stable whatever tz is requested.",
				"start_at and end_at are derived and normalised to local midnight and the following local midnight in the timezone used for the write.",
				"Exactly one calendar day per event: the schema stores a single date, so a multi-day all-day span must be expressed as a timed event covering those days.",
				"Supplying a non-null event_date without an explicit is_all_day sets is_all_day to true, which converts a timed event into an all-day one. Supplying null sets it to false and converts it back to a timed event. An explicit is_all_day always wins over this inference.",
				"Supplying is_all_day true without event_date derives the date from start_at in the request timezone, so the same instant can land on different dates for different callers.",
			},
			Task: []string{
				"is_task marks an event as completable; is_completed records whether it is done.",
				"Setting is_completed is meaningless without is_task: whenever is_task ends up false, is_completed is stored as false.",
				"Completing a task does not change its time, and a completed task is still returned by range queries unless is_completed=false is passed as a filter.",
			},
			Reminders: []string{
				"Two ways to write them, both storing the same rows. Inside an event payload, reminders is a whole-set replacement saved atomically: the array you send becomes the complete set. The standalone endpoints below act on one reminder at a time, which suits a client that adds or moves a single reminder without touching the event.",
				"In an event payload, [] and null both clear the set, and omitting the field leaves it untouched. Duplicate remind_at values within one request collapse into one.",
				"POST /api/v1/reminders answers 409 reminder_exists if the event already has a reminder at exactly that time, since two identical rows would be indistinguishable. The same applies to PATCH /api/v1/reminders/{id} when it would land on a time the event already uses. The event-payload form instead collapses duplicates silently.",
				"At most 50 reminders per event, whichever route is used.",
				"Each reminder is returned with id, event_id, remind_at, created_at and the derived remind_at_local.",
				"remind_at is absolute. There is no \"N minutes before the event\" form in the API; compute the instant from the event start if that is what you want to offer in a UI.",
				"Deleting an event deletes its reminders; deleting a reminder does not touch its event.",
			},
			Deleting: []string{
				"DELETE /api/v1/events/{id} does not remove the row: it moves the event to category -1, which is the deleted category. That is exactly equivalent to PATCHing category_id to -1, the endpoint is simply a shortcut for it.",
				"A deleted event keeps its data and can be restored by setting category_id back to 0, or to any other category.",
				"A deleted event stops appearing in range queries, so it is off the calendar. Pass include_deleted=true to see it again, and GET /api/v1/events/{id} keeps returning it either way.",
				"Deleting an event that is already deleted is a 204, not an error: the event exists and is marked.",
				"To destroy the row for good, use POST /api/v1/events/{id}/remove. That is the only operation that deletes data, and it also removes the event's reminders.",
			},
			Patching: []string{
				"PATCH takes the same field set as POST, but every field is optional. Only the fields present in the body are touched.",
				"A field set to null clears the column: notes, end_at and event_date are clearable. title and start_at must not be null.",
				"Omitting a field leaves it as it was; this is the difference between {\"notes\": null} (clear) and {} (leave alone).",
				"updated_at is refreshed on every successful PATCH, including one that changes nothing.",
				"The response is the complete event after normalisation, so a client always sees the final stored state.",
			},
		},
		RangeQueries: helpRanges{
			Day:    "range=day with date=YYYY-MM-DD returns [local midnight, next local midnight).",
			Week:   "range=week returns the seven local days containing date, starting on week_start (default monday).",
			Month:  "range=month returns [first day of the month, first day of the following month).",
			Custom: "from and to describe an explicit half-open window, each given as RFC3339 or Unix seconds.",
			Rules: []string{
				"range defaults to day and date defaults to today in the request timezone, so GET /api/v1/events alone answers \"what is on today\".",
				"range/date and from/to are mutually exclusive; mixing them is a 400.",
				"Overlap, not containment: an event is returned when its occupied interval intersects the window. An event from 23:00 to 01:00 is therefore returned by both days, and a three-day event by every overlapping week.",
				"The response always echoes the resolved window under \"range\", including start_at, end_at, start_at_local, end_at_local and the timezone actually used, so a client never has to guess the boundaries.",
				"Ordering is stable: all-day events first, then by start_at ascending, then by id.",
			},
		},
		Endpoints: []helpEndpoint{
			{
				Method:  "GET",
				Path:    "/help",
				Summary: "This reference. Returns JSON, or Markdown when Accept: text/plain is sent.",
				Parameters: []string{
					"format=text|markdown|md -- alternative way to request the Markdown rendering",
				},
				Success:     "200 with the reference document",
				CurlExample: "curl -s http://localhost:8080/help",
			},
			{
				Method:      "GET",
				Path:        "/healthz",
				Summary:     "Liveness plus a database round-trip. Returns 503 and database=unavailable when the store cannot be reached.",
				Success:     "200 with status, database, version, timezone, time and uptime_seconds",
				CurlExample: "curl -s http://localhost:8080/healthz",
			},
			{
				Method:  "POST",
				Path:    "/api/v1/events",
				Summary: "Create an event. title is required, plus start_at for a timed event or event_date for an all-day event.",
				Parameters: []string{
					"tz -- timezone used to derive an all-day date from start_at, and to render start_at_local",
				},
				Body:    "An object using any subset of the request body fields, subject to the required ones.",
				Success: "201 with the created event, a Location header and the assigned id",
				Errors:  []string{"400 validation_failed", "400 invalid_body", "400 invalid_parameter", "413 body_too_large"},
				CurlExample: `curl -s -X POST http://localhost:8080/api/v1/events \
  -H 'Content-Type: application/json' \
  -d '{"title":"Dentist","start_at":1773205200,"end_at":1773208800,"notes":"Bring the insurance card"}'`,
			},
			{
				Method:  "GET",
				Path:    "/api/v1/events",
				Summary: "List the events overlapping a day, week, month or explicit window.",
				Parameters: []string{
					"range=day|week|month -- window kind, default day",
					"date=YYYY-MM-DD -- anchor day for the window, default today",
					"from, to -- explicit half-open window as Unix seconds, RFC3339, or an offset-free local timestamp; mutually exclusive with range/date",
					"tz -- IANA timezone deciding the window boundaries",
					"week_start=monday|sunday -- overrides the server default for range=week",
					"category_id -- keep only events in this category. Naming a category explicitly wins over the default: category_id=-1 lists the deleted events rather than hiding them",
					"include_deleted=true -- stop hiding events in the deleted category; ignored when category_id is given",
					"is_task=true|false -- keep only tasks or only non-tasks",
					"is_completed=true|false -- keep only completed or only open items",
					"q -- case-insensitive substring match on title",
					"limit -- 0 to 2000, default 0 meaning no limit",
					"offset -- non-negative row offset",
				},
				Success:     "200 with range, optional filters, count and events",
				Errors:      []string{"400 invalid_parameter"},
				CurlExample: `curl -s 'http://localhost:8080/api/v1/events?range=week&date=2026-03-11&tz=Asia/Shanghai&week_start=monday'`,
			},
			{
				Method:  "GET",
				Path:    "/api/v1/events/{id}",
				Summary: "Fetch a single event together with its reminders.",
				Parameters: []string{
					"tz -- timezone used to render start_at_local and end_at_local",
				},
				Success:     "200 with the event",
				Errors:      []string{"400 invalid_parameter", "404 not_found"},
				CurlExample: "curl -s http://localhost:8080/api/v1/events/1",
			},
			{
				Method:  "PATCH",
				Path:    "/api/v1/events/{id}",
				Summary: "Partially update an event. Only the supplied fields change; null clears a nullable field.",
				Parameters: []string{
					"tz -- timezone used when re-deriving an all-day date and when rendering the response",
				},
				Body:    "An object using any subset of the request body fields. {} is valid and only refreshes updated_at.",
				Success: "200 with the complete event after normalisation",
				Errors:  []string{"400 validation_failed", "400 invalid_body", "404 not_found"},
				CurlExample: `curl -s -X PATCH http://localhost:8080/api/v1/events/1 \
  -H 'Content-Type: application/json' \
  -d '{"is_completed":true}'`,
			},
			{
				Method:  "GET",
				Path:    "/api/v1/reminders",
				Summary: "List reminders, optionally for one event and/or a time window. This is the call a client polls to find reminders that are due.",
				Parameters: []string{
					"event_id -- keep only the reminders of this event",
					"from, to -- half-open window on remind_at, same encodings as elsewhere; tz decides how an offset-free value is read",
					"tz -- timezone used for remind_at_local and for offset-free bounds",
					"limit -- 0 to 2000, default 0 meaning no limit",
					"offset -- non-negative row offset",
				},
				Success:     "200 with timezone, optional filters, count and reminders",
				Errors:      []string{"400 invalid_parameter"},
				CurlExample: `curl -s 'http://localhost:8080/api/v1/reminders?from=2026-03-11T00:00&to=2026-03-12T00:00&tz=Asia/Shanghai'`,
			},
			{
				Method:  "POST",
				Path:    "/api/v1/reminders",
				Summary: "Attach a reminder to an existing event.",
				Parameters: []string{
					"tz -- timezone used when remind_at is an offset-free timestamp",
				},
				Body:    `{"event_id": 1, "remind_at": "2026-03-11T12:50"}`,
				Success: "201 with the reminder, a Location header and the assigned id",
				Errors:  []string{"400 validation_failed", "400 invalid_body", "409 reminder_exists"},
				CurlExample: `curl -s -X POST 'http://localhost:8080/api/v1/reminders?tz=Asia/Shanghai' \
  -H 'Content-Type: application/json' \
  -d '{"event_id":1,"remind_at":"2026-03-11T12:50"}'`,
			},
			{
				Method:      "GET",
				Path:        "/api/v1/reminders/{id}",
				Summary:     "Fetch one reminder.",
				Parameters:  []string{"tz -- timezone used to render remind_at_local"},
				Success:     "200 with the reminder",
				Errors:      []string{"400 invalid_parameter", "404 not_found"},
				CurlExample: "curl -s http://localhost:8080/api/v1/reminders/1",
			},
			{
				Method:  "PATCH",
				Path:    "/api/v1/reminders/{id}",
				Summary: "Move a reminder to a new time. The owning event cannot be changed.",
				Parameters: []string{
					"tz -- timezone used when remind_at is an offset-free timestamp",
				},
				Body:    `{"remind_at": "2026-03-11T12:30"}`,
				Success: "200 with the reminder; created_at and event_id are unchanged",
				Errors:  []string{"400 validation_failed", "404 not_found", "409 reminder_exists"},
				CurlExample: `curl -s -X PATCH http://localhost:8080/api/v1/reminders/1 \
  -H 'Content-Type: application/json' -d '{"remind_at":"2026-03-11T12:30"}'`,
			},
			{
				Method:      "DELETE",
				Path:        "/api/v1/reminders/{id}",
				Summary:     "Delete one reminder. The event is untouched.",
				Success:     "204 with an empty body",
				Errors:      []string{"400 invalid_parameter", "404 not_found"},
				CurlExample: "curl -s -X DELETE -o /dev/null -w '%{http_code}\n' http://localhost:8080/api/v1/reminders/1",
			},
			{
				Method:      "DELETE",
				Path:        "/api/v1/events/{id}",
				Summary:     "Delete an event by moving it to the deleted category (-1). The row stays, the event leaves the calendar, and it can be restored. Use POST /api/v1/events/{id}/remove to destroy it.",
				Success:     "204 with an empty body",
				Errors:      []string{"400 invalid_parameter", "404 not_found"},
				CurlExample: "curl -s -X DELETE -o /dev/null -w '%{http_code}\\n' http://localhost:8080/api/v1/events/1",
			},
			{
				Method:  "POST",
				Path:    "/api/v1/events/{id}/remove",
				Summary: "Remove an event permanently. The row is deleted together with its reminders, and the id is never reused. This is the only destructive endpoint.",
				Parameters: []string{
					"tz -- accepted for symmetry; it does not change anything",
				},
				Success:     "204 with an empty body",
				Errors:      []string{"400 invalid_parameter", "404 not_found"},
				CurlExample: "curl -s -X POST -o /dev/null -w '%{http_code}\\n' http://localhost:8080/api/v1/events/1/remove",
			},
			{
				Method:      "GET",
				Path:        "/api/v1/categories",
				Summary:     "List every category, including the built-in deleted (-1) and uncategorized (0) rows. Ordered by id, so the built-ins come first.",
				Success:     "200 with count and categories",
				CurlExample: "curl -s http://localhost:8080/api/v1/categories",
			},
			{
				Method:  "POST",
				Path:    "/api/v1/categories",
				Summary: "Create a category. The id is assigned automatically and starts at 1; -1 and 0 already exist and cannot be recreated.",
				Body:    `{"name": "Work", "color": "#4f8cff"}`,
				Success: "201 with the created category, a Location header and the assigned id",
				Errors:  []string{"400 validation_failed", "400 invalid_body"},
				CurlExample: `curl -s -X POST http://localhost:8080/api/v1/categories \
  -H 'Content-Type: application/json' -d '{"name":"Work","color":"#4f8cff"}'`,
			},
			{
				Method:      "GET",
				Path:        "/api/v1/categories/{id}",
				Summary:     "Fetch one category.",
				Success:     "200 with the category",
				Errors:      []string{"400 invalid_parameter", "404 not_found"},
				CurlExample: "curl -s http://localhost:8080/api/v1/categories/1",
			},
			{
				Method:  "PATCH",
				Path:    "/api/v1/categories/{id}",
				Summary: "Rename a category or change its colour, including the built-in ones. Events pick the change up because they store only the id. Category -1 accepts a colour but not a new name.",
				Body:    `{"color": "#34d399"}`,
				Success: "200 with the complete category after normalisation",
				Errors:  []string{"400 validation_failed", "400 invalid_body", "404 not_found", "409 category_name_fixed"},
				CurlExample: `curl -s -X PATCH http://localhost:8080/api/v1/categories/1 \
  -H 'Content-Type: application/json' -d '{"color":"#34d399"}'`,
			},
			{
				Method:      "DELETE",
				Path:        "/api/v1/categories/{id}",
				Summary:     "Delete a user category. Its events are moved to the default category (0) in the same transaction, so no event is lost. The built-in -1 and 0 categories are protected.",
				Success:     "204 with an empty body",
				Errors:      []string{"400 invalid_parameter", "404 not_found", "409 category_protected"},
				CurlExample: "curl -s -X DELETE -o /dev/null -w '%{http_code}\\n' http://localhost:8080/api/v1/categories/1",
			},
		},
		BodyFields: []helpField{
			{Name: "title", Type: "string", Required: "yes on POST", Nullable: "no", Description: "Event title."},
			{Name: "notes", Type: "string", Required: "no", Nullable: "yes", Description: "Free-form notes. null clears them on PATCH."},
			{Name: "location", Type: "string", Required: "no", Nullable: "yes", Description: "Where the event happens. Trimmed of surrounding whitespace; null or an empty string clears it, so an emptied box reads back as null rather than as an empty location."},
			{Name: "start_at", Type: "integer or string", Required: "yes on POST for a timed event", Nullable: "no", Description: "Start instant, as Unix seconds or as a timestamp string (RFC3339 with an offset, or an offset-free local value read in tz). Stored as an absolute instant, so the timezone you send it in never changes what is stored. On an all-day event it is accepted only to derive the date."},
			{Name: "end_at", Type: "integer or string", Required: "no", Nullable: "yes", Description: "Exclusive end instant, same encodings as start_at. null clears it, and the event then covers a single instant."},
			{Name: "is_all_day", Type: "boolean", Required: "no", Default: "false", Description: "Marks the event as all-day. Inferred as true when a non-null event_date is sent."},
			{Name: "event_date", Type: "string", Required: "yes for an all-day event", Nullable: "yes", Description: "Calendar date YYYY-MM-DD. Sending it also sets is_all_day. null clears it and the event becomes timed."},
			{Name: "is_task", Type: "boolean", Required: "no", Default: "false", Description: "Marks the event as a completable task."},
			{Name: "is_completed", Type: "boolean", Required: "no", Default: "false", Description: "Completion state. Stored as false whenever is_task is false."},
			{Name: "category_id", Type: "integer", Required: "no", Default: "0", Description: "The event's category. Must be an existing category. Setting it to -1 is exactly what DELETE does; setting it back to 0 restores the event."},
			{Name: "name", Type: "string", Required: "yes for POST /api/v1/categories", Description: "A category's display name, 1 to 100 characters after trimming. On PATCH the name of category -1 cannot change: sending a different one returns 409 category_name_fixed."},
			{Name: "color", Type: "string", Required: "yes for POST /api/v1/categories", Description: "A category's colour: #rgb or #rrggbb. Normalised to lower-case six-digit form. Editable on every category, including -1 and 0."},
			{Name: "display", Type: "boolean", Required: "no", Default: "true on POST, unchanged on PATCH", Description: "Whether the sidebar shows this category. Explicitly a boolean: null is refused rather than guessed, and hiding a category never moves or changes its events."},
			{Name: "reminders", Type: "array of objects", Required: "no", Nullable: "yes", Description: "Whole-set replacement of the event's reminders, saved atomically with the event. Each object has one field, remind_at. Equivalent to the standalone reminder endpoints below, but in a single request."},
			{Name: "event_id", Type: "integer", Required: "yes for POST /api/v1/reminders", Nullable: "no", Description: "The event the reminder belongs to. Must already exist. Not accepted inside an event payload, where it is implied."},
			{Name: "remind_at", Type: "integer or string", Required: "yes for the reminder endpoints", Nullable: "no", Description: "The instant to remind at, in the same three encodings as start_at: Unix seconds, RFC3339 with an offset, or an offset-free local timestamp read in tz. There is no relative form such as \"10 minutes before\"; compute it from the event start."},
		},
		QueryParams: []helpField{
			{Name: "tz", Type: "string", Description: "IANA timezone name, e.g. Asia/Shanghai. Defaults to the server timezone.", Constraints: "must be a known zone name"},
			{Name: "range", Type: "string", Description: "Window kind for list queries.", Constraints: "day | week | month"},
			{Name: "date", Type: "string", Description: "Anchor date for the window.", Constraints: "YYYY-MM-DD, must be a real date"},
			{Name: "from", Type: "string", Description: "Inclusive lower bound of an explicit window.", Constraints: "RFC3339 or Unix seconds"},
			{Name: "to", Type: "string", Description: "Exclusive upper bound of an explicit window; must be later than from.", Constraints: "RFC3339 or Unix seconds"},
			{Name: "week_start", Type: "string", Description: "First day of a week window.", Constraints: "monday | sunday"},
			{Name: "category_id", Type: "integer", Description: "Keep only events in this category, including the built-in -1 and 0. Naming a category explicitly wins over the default hiding of deleted events, so category_id=-1 lists them.", Constraints: "must be an existing category id"},
			{Name: "include_deleted", Type: "boolean", Description: "Stop hiding events in the deleted category (-1). Ignored when category_id is given.", Constraints: "true | false, default false"},
			{Name: "is_task", Type: "boolean", Description: "Filter by task flag."},
			{Name: "is_completed", Type: "boolean", Description: "Filter by completion flag."},
			{Name: "q", Type: "string", Description: "Case-insensitive substring match on title.", Constraints: "at most 200 characters"},
			{Name: "limit", Type: "integer", Description: "Maximum number of events to return; 0 means no limit.", Constraints: "0 to 2000"},
			{Name: "offset", Type: "integer", Description: "Number of matching events to skip.", Constraints: ">= 0"},
		},
		ResponseFields: []helpField{
			{Name: "id", Type: "integer", Description: "Event identifier, used in the path of every single-event endpoint."},
			{Name: "title, notes, start_at, end_at, is_all_day, event_date, is_task, is_completed, created_at, updated_at", Type: "see the data model", Description: "The stored row, exactly as persisted."},
			{Name: "category_id", Type: "integer", Description: "The event's category. Look it up in GET /api/v1/categories for its name and colour."},
			{Name: "reminders", Type: "array", Description: "The event's reminders, ordered by remind_at. Each has id, event_id, remind_at, created_at and remind_at_local."},
			{Name: "start_at_local", Type: "string", Description: "Derived: start_at rendered as RFC3339 in the request timezone. Read-only."},
			{Name: "end_at_local", Type: "string or null", Description: "Derived: end_at rendered as RFC3339 in the request timezone. Null when end_at is null. Read-only."},
			{Name: "range", Type: "object", Description: "On list responses: the window actually queried, with kind, timezone, start_date, end_date, start_at, end_at, the local renderings and length_seconds."},
			{Name: "count", Type: "integer", Description: "Number of events in this response."},
		},
		ErrorCodes: []helpCode{
			{Code: "validation_failed", HTTPStatus: 400, Meaning: "A field was present but unusable. The details array names each offending field."},
			{Code: "invalid_parameter", HTTPStatus: 400, Meaning: "A query parameter, the path id, the Content-Type or the body shape was wrong. Unknown body fields also land here."},
			{Code: "invalid_body", HTTPStatus: 400, Meaning: "The body was not valid JSON, or a field had the wrong JSON type."},
			{Code: "body_too_large", HTTPStatus: 413, Meaning: "The body exceeded 1 MiB."},
			{Code: "not_found", HTTPStatus: 404, Meaning: "No such event or reminder, or no such endpoint."},
			{Code: "reminder_exists", HTTPStatus: 409, Meaning: "That event already has a reminder at exactly that time."},
			{Code: "category_protected", HTTPStatus: 409, Meaning: "The deleted (-1) and default (0) categories are part of the API and cannot be removed."},
			{Code: "category_name_fixed", HTTPStatus: 409, Meaning: "Category -1 keeps its name, because -1 is how the API names the deleted state. Its colour can still be changed."},
			{Code: "method_not_allowed", HTTPStatus: 405, Meaning: "The path exists but not for that method; the Allow header lists what is supported."},
			{Code: "request_cancelled", HTTPStatus: 408, Meaning: "The client disconnected before the request completed."},
			{Code: "internal_error", HTTPStatus: 500, Meaning: "An unexpected server-side failure. The body carries no internal detail."},
		},
		Limits: helpLimits{
			TitleRunes:        titleMaxRunes,
			NotesRunes:        notesMaxRunes,
			LocationRunes:     locationMaxRunes,
			CategoryNameRunes: maxCategoryNameRunes,
			RemindersPerEvent: remindersMax,
			SearchRunes:       queryMaxRunes,
			MaxPageSize:       pageSizeMax,
			BodyBytes:         "1 MiB",
			TimestampRange:    "0 to 4102444800 exclusive, that is 1970-01-01 through 2099-12-31; a millisecond value is detected and reported",
		},
		Examples: []helpExample{
			{
				Title:       "Create a timed event",
				Description: "Only title and start_at are required. The response contains the server-assigned id.",
				Request: "POST /api/v1/events?tz=Asia/Shanghai\n" +
					"Content-Type: application/json\n\n" +
					`{"title":"Dentist","start_at":1773205200,"end_at":1773208800,"notes":"Bring the insurance card","location":"Clinic, 2nd floor"}`,
				Response: "201 Created\n" +
					`{"id":1,"title":"Dentist","notes":"Bring the insurance card","location":"Clinic, 2nd floor","start_at":1773205200,"end_at":1773208800,` +
					`"is_all_day":false,"event_date":null,"is_task":false,"is_completed":false,"category_id":0,` +
					`"created_at":1773200000,"updated_at":1773200000,"reminders":[],` +
					`"start_at_local":"2026-03-11T13:00:00+08:00","end_at_local":"2026-03-11T14:00:00+08:00"}`,
			},
			{
				Title:       "Create an all-day task with a reminder",
				Description: "event_date implies is_all_day. start_at and end_at are filled in as local day boundaries.",
				Request: "POST /api/v1/events?tz=Asia/Shanghai\n" +
					"Content-Type: application/json\n\n" +
					`{"title":"Renew the domain","event_date":"2026-03-31","is_task":true,"reminders":[{"remind_at":1774886400}]}`,
				Response: "201 Created\n" +
					`{"id":2,"title":"Renew the domain","notes":null,"location":null,"start_at":1774886400,"end_at":1774972800,` +
					`"is_all_day":true,"event_date":"2026-03-31","is_task":true,"is_completed":false,"category_id":0,` +
					`"created_at":1773200100,"updated_at":1773200100,` +
					`"reminders":[{"id":1,"event_id":2,"remind_at":1774886400,"created_at":1773200100}],` +
					`"start_at_local":"2026-03-31T00:00:00+08:00","end_at_local":"2026-04-01T00:00:00+08:00"}`,
			},
			{
				Title:       "List one day",
				Description: "The window is echoed back so the client can confirm the boundaries and the timezone used.",
				Request:     "GET /api/v1/events?range=day&date=2026-03-31&tz=Asia/Shanghai",
				Response: "200 OK\n" +
					`{"range":{"kind":"day","timezone":"Asia/Shanghai","start_date":"2026-03-31","end_date":"2026-04-01",` +
					`"start_at":1774886400,"end_at":1774972800,"start_at_local":"2026-03-31T00:00:00+08:00",` +
					`"end_at_local":"2026-04-01T00:00:00+08:00","interval":"[start_at, end_at)","length_seconds":86400},` +
					`"count":1,"events":[{"id":2,"title":"Renew the domain","is_all_day":true,"event_date":"2026-03-31", ...}]}`,
			},
			{
				Title:       "Convert a timed event into an all-day event",
				Description: "Sending event_date changes the anchoring; the stored timestamps are normalised to that local day.",
				Request: "PATCH /api/v1/events/1?tz=Asia/Shanghai\n" +
					"Content-Type: application/json\n\n" +
					`{"event_date":"2026-03-12"}`,
				Response: "200 OK with is_all_day true, event_date 2026-03-12 and start_at at local midnight",
			},
			{
				Title:       "Complete a task",
				Description: "PATCH returns the full normalised event, so the new state is immediately visible.",
				Request: "PATCH /api/v1/events/2\n" +
					"Content-Type: application/json\n\n" +
					`{"is_completed":true}`,
				Response: `200 OK with "is_completed":true and a refreshed updated_at`,
			},
			{
				Title:       "Query a window that starts mid-day",
				Description: "from and to give an explicit half-open window in Unix seconds or RFC3339.",
				Request:     "GET /api/v1/events?from=2026-03-11T09:00:00%2B08:00&to=2026-03-11T18:00:00%2B08:00&tz=Asia/Shanghai",
				Response:    "200 OK with every event overlapping 09:00-18:00 on 2026-03-11, all-day events included",
			},
			{
				Title:       "Delete an event and take it back",
				Description: "DELETE is a soft delete: the event joins the deleted category (-1) and leaves every list query. Sending category_id back restores it. Only /remove destroys the row.",
				Request: "DELETE /api/v1/events/1\n\n" +
					"...\n\n" +
					"PATCH /api/v1/events/1\n" +
					"Content-Type: application/json\n\n" +
					`{"category_id":0}`,
				Response: "204 No Content, then 200 OK with \"category_id\":0 and the event back in range queries",
			},
			{
				Title:       "Create a category and colour an event with it",
				Description: "A category is just a name and a colour. Events reference it by id, so renaming or recolouring updates every event at once.",
				Request: "POST /api/v1/categories\n" +
					"Content-Type: application/json\n\n" +
					`{"name":"Work","color":"#4f8cff"}` + "\n\n" +
					"PATCH /api/v1/events/1\n" +
					"Content-Type: application/json\n\n" +
					`{"category_id":1}`,
				Response: "201 Created\n" +
					`{"id":1,"name":"Work","color":"#4f8cff"}` + "\n\nthen 200 OK with the event carrying \"category_id\":1",
			},
			{
				Title:       "A validation failure",
				Description: "Field-level problems are reported together rather than one at a time.",
				Request: "POST /api/v1/events\n" +
					"Content-Type: application/json\n\n" +
					`{"title":"   ","end_at":100}`,
				Response: "400 Bad Request\n" +
					`{"error":{"code":"validation_failed","message":"one or more fields are invalid",` +
					`"details":[{"field":"title","message":"is required and must not be empty"},` +
					`{"field":"start_at","message":"is required for a timed event"}]}}`,
			},
		},
		NotImplemented: []string{
			"Reminder delivery. Reminders are persisted and returned, but nothing is scheduled, pushed or emailed.",
			"Relative reminder offsets. remind_at is always an absolute instant; a \"10 minutes before\" form is a UI affordance, not an API one.",
			"Recurring events. There is no recurrence rule column, so each occurrence is a distinct event.",
			"Multi-day all-day events. The schema holds a single event_date; use a timed event for a multi-day span.",
			"A trash view. Deleted events are still reachable with include_deleted=true, but nothing in the API restores them in bulk.",
			"Authentication and multi-user isolation. The service is single-user by design.",
			"Full-text search. q performs a substring match on title only.",
			"A web frontend.",
		},
	}
}

// Limits mirrored from the service layer, kept as plain ints so this document
// cannot drift into a compile error if a constant is renamed.
const (
	titleMaxRunes    = 500
	notesMaxRunes    = 20000
	locationMaxRunes = 500
	remindersMax     = 50
	queryMaxRunes    = 200
	pageSizeMax      = 2000

	maxCategoryNameRunes = 100
)

// renderHelpText writes the same reference as Markdown for humans and for
// agents that prefer to read a document rather than parse JSON.
func renderHelpText(d helpDoc) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	line("# %s API reference", d.Service)
	line("")
	line("%s", d.Description)
	line("")
	line("- API version: `%s`", d.APIVersion)
	line("- Build version: `%s`", d.Version)
	line("- Authentication: none. %s", d.Authentication.Note)
	line("")

	line("## Quick start")
	line("")
	for _, q := range d.QuickStart {
		line("- %s", q)
	}
	line("")

	line("## Conventions")
	line("")
	line("- Base path: `%s`", d.Conventions.BasePath)
	line("- Content type: %s", d.Conventions.ContentType)
	line("- Time encoding: %s", d.Conventions.TimeEncoding)
	line("- Interval style: %s", d.Conventions.IntervalStyle)
	line("- Unknown fields: %s", d.Conventions.UnknownFields)
	line("- Empty collections: %s", d.Conventions.EmptyCollections)
	line("- Delete: %s", d.Conventions.DeleteReturns)
	line("")

	line("## Timezones")
	line("")
	line("- Server default: `%s`", d.Timezone.ServerDefault)
	line("- Per-request override: `%s` query parameter", d.Timezone.OverrideParam)
	line("- Default first day of week: `%s` (override with `%s`)", d.Timezone.DefaultWeekStart, d.Timezone.WeekStartParam)
	line("")
	for _, r := range d.Timezone.Rules {
		line("- %s", r)
	}
	line("")

	line("## Data model")
	line("")
	for _, t := range d.DataModel {
		line("### `%s`", t.Name)
		line("")
		line("%s", t.Description)
		line("")
		line("| column | type | description |")
		line("|---|---|---|")
		for _, c := range t.Columns {
			desc := c.Description
			if c.Constraints != "" {
				desc += " (" + c.Constraints + ")"
			}
			line("| `%s` | `%s` | %s |", c.Name, c.Type, desc)
		}
		line("")
		for _, n := range t.Notes {
			line("- %s", n)
		}
		line("")
	}

	line("## Event semantics")
	line("")
	for _, section := range []struct {
		heading string
		items   []string
	}{
		{"Timed events", d.Semantics.TimedEvent},
		{"All-day events", d.Semantics.AllDayEvent},
		{"Tasks", d.Semantics.Task},
		{"Deleting and restoring", d.Semantics.Deleting},
		{"Reminders", d.Semantics.Reminders},
		{"Patching", d.Semantics.Patching},
	} {
		line("### %s", section.heading)
		line("")
		for _, s := range section.items {
			line("- %s", s)
		}
		line("")
	}

	line("## Range queries")
	line("")
	line("- Day: %s", d.RangeQueries.Day)
	line("- Week: %s", d.RangeQueries.Week)
	line("- Month: %s", d.RangeQueries.Month)
	line("- Custom: %s", d.RangeQueries.Custom)
	line("")
	for _, r := range d.RangeQueries.Rules {
		line("- %s", r)
	}
	line("")

	line("## Endpoints")
	line("")
	for _, e := range d.Endpoints {
		line("### `%s %s`", e.Method, e.Path)
		line("")
		line("%s", e.Summary)
		line("")
		if len(e.Parameters) > 0 {
			line("Parameters:")
			line("")
			for _, p := range e.Parameters {
				line("- %s", p)
			}
			line("")
		}
		if e.Body != "" {
			line("Body: %s", e.Body)
			line("")
		}
		line("Success: %s", e.Success)
		if len(e.Errors) > 0 {
			line("")
			line("Errors: %s", strings.Join(e.Errors, ", "))
		}
		if e.CurlExample != "" {
			line("")
			line("```sh")
			line("%s", e.CurlExample)
			line("```")
		}
		line("")
	}

	line("## Request body fields")
	line("")
	line("| field | type | required | nullable | default | description |")
	line("|---|---|---|---|---|---|")
	for _, f := range d.BodyFields {
		line("| `%s` | %s | %s | %s | %s | %s |",
			f.Name, f.Type, orDash(f.Required), orDash(f.Nullable), orDash(f.Default), f.Description)
	}
	line("")

	line("## Query parameters")
	line("")
	line("| parameter | type | description | constraints |")
	line("|---|---|---|---|")
	for _, f := range d.QueryParams {
		line("| `%s` | %s | %s | %s |", f.Name, f.Type, f.Description, orDash(f.Constraints))
	}
	line("")

	line("## Response fields")
	line("")
	for _, f := range d.ResponseFields {
		line("- `%s` (%s): %s", f.Name, f.Type, f.Description)
	}
	line("")

	line("## Error envelope")
	line("")
	line("Every failure returns `{\"error\":{\"code\":...,\"message\":...,\"details\":[...]}}`.")
	line("")
	line("| code | HTTP | meaning |")
	line("|---|---|---|")
	for _, c := range d.ErrorCodes {
		line("| `%s` | %d | %s |", c.Code, c.HTTPStatus, c.Meaning)
	}
	line("")

	line("## Limits")
	line("")
	line("- title: at most %d characters", d.Limits.TitleRunes)
	line("- notes: at most %d characters", d.Limits.NotesRunes)
	line("- location: at most %d characters", d.Limits.LocationRunes)
	line("- category name: at most %d characters", d.Limits.CategoryNameRunes)
	line("- reminders per event: at most %d", d.Limits.RemindersPerEvent)
	line("- `q`: at most %d characters", d.Limits.SearchRunes)
	line("- `limit`: 0 to %d", d.Limits.MaxPageSize)
	line("- request body: at most %s", d.Limits.BodyBytes)
	line("- timestamps: %s", d.Limits.TimestampRange)
	line("")

	line("## Examples")
	line("")
	for _, e := range d.Examples {
		line("### %s", e.Title)
		line("")
		line("%s", e.Description)
		line("")
		line("```")
		line("%s", e.Request)
		line("```")
		line("")
		line("```")
		line("%s", e.Response)
		line("```")
		line("")
	}

	line("## Deliberately not implemented")
	line("")
	for _, n := range d.NotImplemented {
		line("- %s", n)
	}
	line("")

	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
