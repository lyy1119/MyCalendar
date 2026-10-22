package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lyy1119/MyCalendar/internal/calendar"
	"github.com/lyy1119/MyCalendar/internal/service"
	"github.com/lyy1119/MyCalendar/internal/store"
)

// harness is a fully wired API server backed by a throwaway database.
type harness struct {
	t      *testing.T
	server *httptest.Server
	api    *Server
}

func newHarness(t *testing.T, zone string) *harness {
	t.Helper()

	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", zone, err)
	}
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	svc := service.New(st, loc)
	// Discard logs so test output stays readable.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(svc, logger, "test", Options{
		DefaultLocation:  loc,
		DefaultWeekStart: calendar.Monday,
		AllowAllOrigins:  true,
		// A stub keeps these tests independent of whether the frontend bundle
		// was compiled into the test binary.
		Frontend: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<!doctype html><div id=\"root\"></div>"))
		}),
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	return &harness{t: t, server: ts, api: srv}
}

// do performs a request and returns the status, headers and body.
func (h *harness) do(method, path, body string, headers map[string]string) (int, http.Header, []byte) {
	h.t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(h.t.Context(), method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatalf("NewRequest: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := h.server.Client().Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, resp.Header, raw
}

// decode unmarshals a body, failing the test on malformed JSON.
func (h *harness) decode(raw []byte) map[string]any {
	h.t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		h.t.Fatalf("response is not valid JSON (%v): %s", err, raw)
	}
	return out
}

// create posts an event and returns its decoded representation.
func (h *harness) create(body string) map[string]any {
	h.t.Helper()
	status, _, raw := h.do(http.MethodPost, "/api/v1/events", body, nil)
	if status != http.StatusCreated {
		h.t.Fatalf("create status = %d, want 201; body: %s", status, raw)
	}
	return h.decode(raw)
}

// eventID reads the numeric id from a decoded event.
func eventID(t *testing.T, ev map[string]any) int {
	t.Helper()
	f, ok := ev["id"].(float64)
	if !ok {
		t.Fatalf("event has no numeric id: %v", ev)
	}
	return int(f)
}

// errorCode extracts error.code from a decoded error envelope.
func errorCode(t *testing.T, body map[string]any) string {
	t.Helper()
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error envelope: %v", body)
	}
	code, _ := errObj["code"].(string)
	return code
}

// fields extracts the field names listed in error.details.
func errorFields(t *testing.T, body map[string]any) []string {
	t.Helper()
	errObj, _ := body["error"].(map[string]any)
	details, _ := errObj["details"].([]any)
	out := make([]string, 0, len(details))
	for _, d := range details {
		if m, ok := d.(map[string]any); ok {
			if f, ok := m["field"].(string); ok {
				out = append(out, f)
			}
		}
	}
	return out
}

func TestCreateEvent(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	status, headers, raw := h.do(http.MethodPost, "/api/v1/events?tz=Asia/Shanghai",
		`{"title":"Dentist","start_at":1773205200,"end_at":1773208800,"notes":"Bring the card"}`, nil)

	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", status, raw)
	}
	if got := headers.Get("Location"); got != "/api/v1/events/1" {
		t.Errorf("Location = %q, want /api/v1/events/1", got)
	}
	if ct := headers.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	ev := h.decode(raw)
	if ev["title"] != "Dentist" {
		t.Errorf("title = %v", ev["title"])
	}
	if ev["is_all_day"] != false {
		t.Errorf("is_all_day = %v, want false", ev["is_all_day"])
	}
	if ev["event_date"] != nil {
		t.Errorf("event_date = %v, want null for a timed event", ev["event_date"])
	}
	// Derived, request-scoped renderings.
	if ev["start_at_local"] != "2026-03-11T13:00:00+08:00" {
		t.Errorf("start_at_local = %v", ev["start_at_local"])
	}
	// reminders must be an empty array, never null.
	reminders, ok := ev["reminders"].([]any)
	if !ok || len(reminders) != 0 {
		t.Errorf("reminders = %#v, want []", ev["reminders"])
	}
}

func TestCreateRequiresTitleAndTimeAnchor(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	status, _, raw := h.do(http.MethodPost, "/api/v1/events", `{"title":"   ","end_at":100}`, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	body := h.decode(raw)
	if code := errorCode(t, body); code != "validation_failed" {
		t.Errorf("error code = %q, want validation_failed", code)
	}
	fields := errorFields(t, body)
	if len(fields) != 2 || fields[0] != "title" || fields[1] != "start_at" {
		t.Errorf("details fields = %v, want [title start_at]", fields)
	}
}

// Unknown fields must fail loudly rather than being silently dropped.
func TestUnknownBodyFieldIsRejected(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	status, _, raw := h.do(http.MethodPost, "/api/v1/events",
		`{"title":"x","start_at":1773205200,"titel":"typo"}`, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	body := h.decode(raw)
	if code := errorCode(t, body); code != "invalid_parameter" {
		t.Errorf("error code = %q, want invalid_parameter", code)
	}
	if fields := errorFields(t, body); len(fields) != 1 || fields[0] != "titel" {
		t.Errorf("details fields = %v, want [titel]", fields)
	}
}

func TestCreateRejectsMillisecondTimestamps(t *testing.T) {
	h := newHarness(t, "UTC")

	status, _, raw := h.do(http.MethodPost, "/api/v1/events", `{"title":"x","start_at":1773205200000}`, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if !strings.Contains(string(raw), "milliseconds") {
		t.Errorf("error should hint at milliseconds, got: %s", raw)
	}
}

func TestAllDayEventIsNormalisedToLocalDay(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	// event_date alone implies is_all_day.
	ev := h.create(`{"title":"Renew domain","event_date":"2026-03-31","is_task":true,"reminders":[{"remind_at":1774886400}]}`)

	if ev["is_all_day"] != true {
		t.Errorf("is_all_day = %v, want true (inferred from event_date)", ev["is_all_day"])
	}
	if ev["event_date"] != "2026-03-31" {
		t.Errorf("event_date = %v", ev["event_date"])
	}
	if ev["start_at_local"] != "2026-03-31T00:00:00+08:00" {
		t.Errorf("start_at_local = %v, want local midnight", ev["start_at_local"])
	}
	if ev["end_at_local"] != "2026-04-01T00:00:00+08:00" {
		t.Errorf("end_at_local = %v, want the next local midnight", ev["end_at_local"])
	}
	reminders, _ := ev["reminders"].([]any)
	if len(reminders) != 1 {
		t.Fatalf("reminders = %#v, want one entry", ev["reminders"])
	}
}

func TestAllDayDateCanBeDerivedFromStartAtInRequestTimezone(t *testing.T) {
	h := newHarness(t, "UTC")

	// 1773165600 is 2026-03-10T18:00:00Z, which is already 2026-03-11T02:00:00
	// in Shanghai. The same instant belongs to a different calendar date
	// depending on the timezone, and the tz parameter is what decides.
	const instant = int64(1773165600)

	ev := h.create(fmt.Sprintf(`{"title":"Derived","is_all_day":true,"start_at":%d}`, instant))
	if ev["event_date"] != "2026-03-10" {
		t.Errorf("event_date = %v, want 2026-03-10 in the server timezone UTC", ev["event_date"])
	}

	status, _, raw := h.do(http.MethodPost, "/api/v1/events?tz=Asia/Shanghai",
		fmt.Sprintf(`{"title":"Derived SH","is_all_day":true,"start_at":%d}`, instant), nil)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, body: %s", status, raw)
	}
	if got := h.decode(raw)["event_date"]; got != "2026-03-11" {
		t.Errorf("event_date = %v, want 2026-03-11 when tz=Asia/Shanghai", got)
	}
}

func TestListDayWeekMonthEchoesWindow(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	tests := []struct {
		name      string
		query     string
		kind      string
		startDate string
		endDate   string
		weekStart string
	}{
		{"day", "range=day&date=2026-03-11", "day", "2026-03-11", "2026-03-12", ""},
		{"week monday", "range=week&date=2026-03-11", "week", "2026-03-09", "2026-03-16", "monday"},
		{"week sunday", "range=week&date=2026-03-11&week_start=sunday", "week", "2026-03-08", "2026-03-15", "sunday"},
		{"month", "range=month&date=2026-03-11", "month", "2026-03-01", "2026-04-01", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, _, raw := h.do(http.MethodGet, "/api/v1/events?tz=Asia/Shanghai&"+tc.query, "", nil)
			if status != http.StatusOK {
				t.Fatalf("status = %d, body: %s", status, raw)
			}
			body := h.decode(raw)
			r, ok := body["range"].(map[string]any)
			if !ok {
				t.Fatalf("missing range object: %s", raw)
			}
			if r["kind"] != tc.kind {
				t.Errorf("kind = %v, want %v", r["kind"], tc.kind)
			}
			if r["start_date"] != tc.startDate || r["end_date"] != tc.endDate {
				t.Errorf("window = [%v, %v), want [%v, %v)", r["start_date"], r["end_date"], tc.startDate, tc.endDate)
			}
			if r["interval"] != "[start_at, end_at)" {
				t.Errorf("interval = %v", r["interval"])
			}
			if tc.weekStart != "" && r["week_start"] != tc.weekStart {
				t.Errorf("week_start = %v, want %v", r["week_start"], tc.weekStart)
			}
			events, ok := body["events"].([]any)
			if !ok {
				t.Fatalf("events is not an array: %s", raw)
			}
			if len(events) != 0 {
				t.Errorf("events = %v, want []", events)
			}
			if body["count"].(float64) != 0 {
				t.Errorf("count = %v, want 0", body["count"])
			}
		})
	}
}

// The same stored instant must land on different calendar days depending on
// the requested timezone, without any data being rewritten.
func TestTimezoneOverrideChangesWindowOnly(t *testing.T) {
	h := newHarness(t, "UTC")

	// 1773165600 == 2026-03-10T18:00:00Z == 2026-03-11T02:00:00+08:00, so it is
	// on the 10th in UTC and on the 11th in Shanghai.
	h.create(`{"title":"Late night","start_at":1773165600,"end_at":1773169200}`)

	countFor := func(query string) int {
		status, _, raw := h.do(http.MethodGet, "/api/v1/events?"+query, "", nil)
		if status != http.StatusOK {
			t.Fatalf("status = %d, body: %s", status, raw)
		}
		return int(h.decode(raw)["count"].(float64))
	}

	if got := countFor("range=day&date=2026-03-10&tz=UTC"); got != 1 {
		t.Errorf("UTC 2026-03-10 count = %d, want 1", got)
	}
	if got := countFor("range=day&date=2026-03-11&tz=UTC"); got != 0 {
		t.Errorf("UTC 2026-03-11 count = %d, want 0", got)
	}
	if got := countFor("range=day&date=2026-03-11&tz=Asia/Shanghai"); got != 1 {
		t.Errorf("Shanghai 2026-03-11 count = %d, want 1", got)
	}
	if got := countFor("range=day&date=2026-03-10&tz=Asia/Shanghai"); got != 0 {
		t.Errorf("Shanghai 2026-03-10 count = %d, want 0", got)
	}
}

func TestListOverlapSemantics(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	// Conference spans the 9th to the 11th; Night shift crosses midnight.
	h.create(`{"title":"Conference","start_at":1773021600,"end_at":1773230400}`)  // 03-09 10:00 -> 03-11 18:00 +08
	h.create(`{"title":"Night shift","start_at":1773154800,"end_at":1773162000}`) // 03-10 23:00 -> 03-11 01:00 +08

	titlesOn := func(date string) []string {
		status, _, raw := h.do(http.MethodGet, "/api/v1/events?tz=Asia/Shanghai&range=day&date="+date, "", nil)
		if status != http.StatusOK {
			t.Fatalf("status = %d, body: %s", status, raw)
		}
		body := h.decode(raw)
		events, _ := body["events"].([]any)
		out := make([]string, 0, len(events))
		for _, e := range events {
			out = append(out, e.(map[string]any)["title"].(string))
		}
		return out
	}

	if got := titlesOn("2026-03-10"); len(got) != 2 {
		t.Errorf("03-10 = %v, want both events", got)
	}
	if got := titlesOn("2026-03-09"); len(got) != 1 || got[0] != "Conference" {
		t.Errorf("03-09 = %v, want [Conference]", got)
	}
	if got := titlesOn("2026-03-12"); len(got) != 0 {
		t.Errorf("03-12 = %v, want none", got)
	}
}

func TestCustomWindow(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")
	h.create(`{"title":"Midday","start_at":1773205200,"end_at":1773208800}`) // 03-11 13:00-14:00 +08

	status, _, raw := h.do(http.MethodGet,
		"/api/v1/events?tz=Asia/Shanghai&from=2026-03-11T09:00:00%2B08:00&to=2026-03-11T18:00:00%2B08:00", "", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body: %s", status, raw)
	}
	body := h.decode(raw)
	if body["count"].(float64) != 1 {
		t.Errorf("count = %v, want 1", body["count"])
	}
	r := body["range"].(map[string]any)
	if r["kind"] != "custom" {
		t.Errorf("kind = %v, want custom", r["kind"])
	}
	if r["length_seconds"].(float64) != 9*3600 {
		t.Errorf("length_seconds = %v, want %d", r["length_seconds"], 9*3600)
	}

	// A window before the event must not match.
	status, _, raw = h.do(http.MethodGet,
		"/api/v1/events?tz=Asia/Shanghai&from=2026-03-11T09:00:00%2B08:00&to=2026-03-11T12:00:00%2B08:00", "", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if got := h.decode(raw)["count"].(float64); got != 0 {
		t.Errorf("count = %v, want 0 for a window before the event", got)
	}
}

func TestQueryParameterValidation(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	tests := []struct {
		name  string
		query string
		field string
	}{
		{"unknown timezone", "tz=Mars/Olympus", "tz"},
		{"unknown range", "range=decade", "range"},
		{"impossible date", "range=day&date=2026-02-30", "date"},
		{"malformed date", "range=day&date=11-03-2026", "date"},
		{"range mixed with from", "range=day&from=1&to=2", "range"},
		{"from without to", "from=1773205200", "from"},
		{"unparsable instant", "from=notatime&to=alsonot", "from"},
		{"reversed window", "from=1773208800&to=1773205200", "to"},
		{"bad week start", "range=week&week_start=tuesday", "week_start"},
		{"bad boolean", "is_task=maybe", "is_task"},
		{"limit too large", "limit=99999", "limit"},
		{"negative offset", "offset=-1", "offset"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, _, raw := h.do(http.MethodGet, "/api/v1/events?"+tc.query, "", nil)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", status, raw)
			}
			body := h.decode(raw)
			if code := errorCode(t, body); code != "invalid_parameter" {
				t.Errorf("error code = %q, want invalid_parameter", code)
			}
			if fields := errorFields(t, body); len(fields) == 0 || fields[0] != tc.field {
				t.Errorf("details fields = %v, want first field %q", fields, tc.field)
			}
		})
	}
}

func TestGetEventAndNotFound(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")
	ev := h.create(`{"title":"Solo","start_at":1773205200}`)
	id := eventID(t, ev)

	status, _, raw := h.do(http.MethodGet, fmt.Sprintf("/api/v1/events/%d", id), "", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body: %s", status, raw)
	}
	if got := h.decode(raw)["title"]; got != "Solo" {
		t.Errorf("title = %v", got)
	}
	// end_at was omitted, so it stays null and end_at_local with it.
	body := h.decode(raw)
	if body["end_at"] != nil || body["end_at_local"] != nil {
		t.Errorf("end_at = %v / %v, want null for an open-ended event", body["end_at"], body["end_at_local"])
	}

	for _, path := range []string{"/api/v1/events/999999", "/api/v1/events/0", "/api/v1/events/abc", "/api/v1/events/-3"} {
		t.Run(path, func(t *testing.T) {
			status, _, raw := h.do(http.MethodGet, path, "", nil)
			if status != http.StatusNotFound && status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 404 or 400; body: %s", status, raw)
			}
			body := h.decode(raw)
			code := errorCode(t, body)
			if code != "not_found" && code != "invalid_parameter" {
				t.Errorf("error code = %q", code)
			}
		})
	}
}

func TestPatchSemantics(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	// Every subtest starts from its own event, so no assertion can depend on
	// the order the subtests happen to run in.
	newEvent := func(t *testing.T) string {
		t.Helper()
		ev := h.create(`{"title":"Original","start_at":1773205200,"end_at":1773208800,"notes":"keep me","is_task":true}`)
		return fmt.Sprintf("/api/v1/events/%d?tz=Asia/Shanghai", eventID(t, ev))
	}
	patch := func(t *testing.T, path, body string) map[string]any {
		t.Helper()
		status, _, raw := h.do(http.MethodPatch, path, body, nil)
		if status != http.StatusOK {
			t.Fatalf("PATCH %s status = %d, body: %s", body, status, raw)
		}
		return h.decode(raw)
	}

	t.Run("omitted fields are left alone", func(t *testing.T) {
		got := patch(t, newEvent(t), `{"title":"Renamed"}`)
		if got["title"] != "Renamed" {
			t.Errorf("title = %v", got["title"])
		}
		if got["notes"] != "keep me" {
			t.Errorf("notes = %v, want the original value", got["notes"])
		}
		if got["is_task"] != true {
			t.Errorf("is_task = %v, want true", got["is_task"])
		}
		if got["start_at"].(float64) != 1773205200 {
			t.Errorf("start_at = %v, want it untouched", got["start_at"])
		}
	})

	t.Run("null clears a nullable field", func(t *testing.T) {
		got := patch(t, newEvent(t), `{"notes":null}`)
		if got["notes"] != nil {
			t.Errorf("notes = %v, want null", got["notes"])
		}
	})

	t.Run("null clears the end of an event", func(t *testing.T) {
		got := patch(t, newEvent(t), `{"end_at":null}`)
		if got["end_at"] != nil || got["end_at_local"] != nil {
			t.Errorf("end_at = %v / %v, want null", got["end_at"], got["end_at_local"])
		}
	})

	t.Run("empty object is a valid no-op", func(t *testing.T) {
		got := patch(t, newEvent(t), `{}`)
		if got["title"] != "Original" {
			t.Errorf("title changed on a no-op patch: %v", got["title"])
		}
	})

	t.Run("completing a task", func(t *testing.T) {
		got := patch(t, newEvent(t), `{"is_completed":true}`)
		if got["is_completed"] != true {
			t.Errorf("is_completed = %v, want true", got["is_completed"])
		}
	})

	t.Run("dropping the task flag clears completion", func(t *testing.T) {
		path := newEvent(t)
		patch(t, path, `{"is_completed":true}`)
		got := patch(t, path, `{"is_task":false}`)
		if got["is_completed"] != false {
			t.Errorf("is_completed = %v, want false once is_task is false", got["is_completed"])
		}
	})

	t.Run("event_date converts a timed event to all-day", func(t *testing.T) {
		got := patch(t, newEvent(t), `{"event_date":"2026-03-12"}`)
		if got["is_all_day"] != true {
			t.Errorf("is_all_day = %v, want true", got["is_all_day"])
		}
		if got["event_date"] != "2026-03-12" {
			t.Errorf("event_date = %v", got["event_date"])
		}
		if got["start_at_local"] != "2026-03-12T00:00:00+08:00" {
			t.Errorf("start_at_local = %v, want local midnight of the new date", got["start_at_local"])
		}
	})

	t.Run("a bare start_at moves an all-day event", func(t *testing.T) {
		path := newEvent(t)
		patch(t, path, `{"event_date":"2026-03-12"}`)
		// 2026-03-19T09:00:00+08:00
		got := patch(t, path, `{"start_at":1773882000}`)
		if got["is_all_day"] != true {
			t.Errorf("is_all_day = %v, want the event to stay all-day", got["is_all_day"])
		}
		if got["event_date"] != "2026-03-19" {
			t.Errorf("event_date = %v, want the date re-derived from the new start_at", got["event_date"])
		}
		if got["start_at_local"] != "2026-03-19T00:00:00+08:00" {
			t.Errorf("start_at_local = %v, want local midnight", got["start_at_local"])
		}
	})

	t.Run("nulling event_date converts back to a timed event", func(t *testing.T) {
		path := newEvent(t)
		patch(t, path, `{"event_date":"2026-03-12"}`)
		got := patch(t, path, `{"event_date":null,"start_at":1773212400}`)
		if got["is_all_day"] != false {
			t.Errorf("is_all_day = %v, want false after clearing event_date", got["is_all_day"])
		}
		if got["event_date"] != nil {
			t.Errorf("event_date = %v, want null and not silently re-derived", got["event_date"])
		}
		if got["start_at"].(float64) != 1773212400 {
			t.Errorf("start_at = %v, want the supplied instant to be honoured", got["start_at"])
		}
	})

	t.Run("an explicit is_all_day wins over the event_date inference", func(t *testing.T) {
		got := patch(t, newEvent(t), `{"is_all_day":true,"start_at":1773205200}`)
		if got["is_all_day"] != true || got["event_date"] != "2026-03-11" {
			t.Errorf("is_all_day = %v, event_date = %v; want true and 2026-03-11", got["is_all_day"], got["event_date"])
		}
	})

	t.Run("title cannot be null", func(t *testing.T) {
		status, _, raw := h.do(http.MethodPatch, newEvent(t), `{"title":null}`, nil)
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", status)
		}
		if fields := errorFields(t, h.decode(raw)); len(fields) != 1 || fields[0] != "title" {
			t.Errorf("details fields = %v, want [title]", fields)
		}
	})

	t.Run("end_at before start_at is rejected", func(t *testing.T) {
		status, _, raw := h.do(http.MethodPatch, newEvent(t), `{"start_at":1773208800,"end_at":1773205200}`, nil)
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body: %s", status, raw)
		}
		if fields := errorFields(t, h.decode(raw)); len(fields) != 1 || fields[0] != "end_at" {
			t.Errorf("details fields = %v, want [end_at]", fields)
		}
	})

	t.Run("patching a missing event is 404", func(t *testing.T) {
		status, _, raw := h.do(http.MethodPatch, "/api/v1/events/999999", `{"title":"ghost"}`, nil)
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body: %s", status, raw)
		}
		if code := errorCode(t, h.decode(raw)); code != "not_found" {
			t.Errorf("error code = %q", code)
		}
	})
}

func TestReminderWholeSetReplacement(t *testing.T) {
	h := newHarness(t, "UTC")
	ev := h.create(`{"title":"Task","start_at":1773205200,"reminders":[{"remind_at":1773205000},{"remind_at":1773205100}]}`)
	id := eventID(t, ev)

	if reminders := ev["reminders"].([]any); len(reminders) != 2 {
		t.Fatalf("reminders = %d, want 2", len(reminders))
	}
	// The set comes back ordered by remind_at.
	first := ev["reminders"].([]any)[0].(map[string]any)
	if first["remind_at"].(float64) != 1773205000 {
		t.Errorf("reminders not ordered by remind_at: %v", ev["reminders"])
	}

	path := fmt.Sprintf("/api/v1/events/%d", id)

	// Replacing with one entry drops the others.
	_, _, raw := h.do(http.MethodPatch, path, `{"reminders":[{"remind_at":1773205300}]}`, nil)
	if reminders := h.decode(raw)["reminders"].([]any); len(reminders) != 1 {
		t.Errorf("reminders = %d, want 1 after replacement", len(reminders))
	}

	// An empty array clears them.
	_, _, raw = h.do(http.MethodPatch, path, `{"reminders":[]}`, nil)
	if reminders := h.decode(raw)["reminders"].([]any); len(reminders) != 0 {
		t.Errorf("reminders = %d, want 0", len(reminders))
	}

	// Omitting the field leaves the set untouched.
	h.do(http.MethodPatch, path, `{"reminders":[{"remind_at":1773205400}]}`, nil)
	_, _, raw = h.do(http.MethodPatch, path, `{"title":"Task renamed"}`, nil)
	if reminders := h.decode(raw)["reminders"].([]any); len(reminders) != 1 {
		t.Errorf("reminders = %d, want 1 when the field is omitted", len(reminders))
	}

	// Duplicates collapse.
	_, _, raw = h.do(http.MethodPatch, path, `{"reminders":[{"remind_at":7},{"remind_at":7},{"remind_at":9}]}`, nil)
	if reminders := h.decode(raw)["reminders"].([]any); len(reminders) != 2 {
		t.Errorf("reminders = %d, want 2 after de-duplication", len(reminders))
	}
}

func TestDeleteEvent(t *testing.T) {
	h := newHarness(t, "UTC")
	ev := h.create(`{"title":"Doomed","start_at":1773205200,"reminders":[{"remind_at":1773205000}]}`)
	id := eventID(t, ev)
	path := fmt.Sprintf("/api/v1/events/%d", id)

	status, _, raw := h.do(http.MethodDelete, path, "", nil)
	if status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", status, raw)
	}
	if len(raw) != 0 {
		t.Errorf("204 response must have an empty body, got: %s", raw)
	}

	// Deleting only marks the event: it moves to the deleted category and the
	// row stays, so it can still be read and brought back.
	status, _, raw = h.do(http.MethodGet, path, "", nil)
	if status != http.StatusOK {
		t.Fatalf("GET after delete status = %d, want 200 with the event still present", status)
	}
	if got := h.decode(raw); got["category_id"].(float64) != -1 {
		t.Errorf("category_id = %v, want -1 after delete", got["category_id"])
	}

	// It stops appearing in range queries.
	status, _, raw = h.do(http.MethodGet, "/api/v1/events?range=month&date=2026-03-11&tz=UTC", "", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if got := h.decode(raw); got["count"].(float64) != 0 {
		t.Errorf("a deleted event still appeared in a range query: %v", got["count"])
	}

	// Deleting again is harmless rather than a 404: the event still exists.
	if status, _, _ = h.do(http.MethodDelete, path, "", nil); status != http.StatusNoContent {
		t.Errorf("second DELETE status = %d, want 204", status)
	}

	// Remove is the destructive one, and only then is the event gone.
	if status, _, _ = h.do(http.MethodPost, path+"/remove", "", nil); status != http.StatusNoContent {
		t.Fatalf("remove status = %d, want 204", status)
	}
	status, _, raw = h.do(http.MethodGet, path, "", nil)
	if status != http.StatusNotFound {
		t.Fatalf("GET after remove status = %d, want 404", status)
	}
	if code := errorCode(t, h.decode(raw)); code != "not_found" {
		t.Errorf("error code = %q", code)
	}

	// Removing something gone is a 404.
	if status, _, _ = h.do(http.MethodPost, path+"/remove", "", nil); status != http.StatusNotFound {
		t.Errorf("second remove status = %d, want 404", status)
	}
}

func TestListFilters(t *testing.T) {
	h := newHarness(t, "UTC")
	h.create(`{"title":"Alpha meeting","start_at":1773205200,"is_task":true}`)
	h.create(`{"title":"Beta meeting","start_at":1773208800,"is_task":true,"is_completed":true}`)
	h.create(`{"title":"Gamma lunch","start_at":1773212400}`)

	count := func(query string) int {
		t.Helper()
		status, _, raw := h.do(http.MethodGet, "/api/v1/events?range=day&date=2026-03-11&tz=UTC&"+query, "", nil)
		if status != http.StatusOK {
			t.Fatalf("status = %d, body: %s", status, raw)
		}
		body := h.decode(raw)
		if f, ok := body["filters"].(map[string]any); ok && len(f) > 0 {
			_ = f
		}
		return int(body["count"].(float64))
	}

	if got := count("is_task=true"); got != 2 {
		t.Errorf("is_task=true count = %d, want 2", got)
	}
	if got := count("is_task=false"); got != 1 {
		t.Errorf("is_task=false count = %d, want 1", got)
	}
	if got := count("is_completed=true"); got != 1 {
		t.Errorf("is_completed=true count = %d, want 1", got)
	}
	if got := count("q=meeting"); got != 2 {
		t.Errorf("q=meeting count = %d, want 2", got)
	}
	if got := count("limit=1"); got != 1 {
		t.Errorf("limit=1 count = %d, want 1", got)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := newHarness(t, "UTC")

	tests := []struct {
		method string
		path   string
		allow  string
	}{
		{http.MethodPut, "/api/v1/events", "GET, POST"},
		{http.MethodDelete, "/api/v1/events", "GET, POST"},
		{http.MethodPut, "/api/v1/events/1", "GET, PATCH, DELETE"},
		{http.MethodPost, "/api/v1/events/1", "GET, PATCH, DELETE"},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			status, headers, raw := h.do(tc.method, tc.path, "", nil)
			if status != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want 405; body: %s", status, raw)
			}
			if got := headers.Get("Allow"); got != tc.allow {
				t.Errorf("Allow = %q, want %q", got, tc.allow)
			}
			if code := errorCode(t, h.decode(raw)); code != "method_not_allowed" {
				t.Errorf("error code = %q, want method_not_allowed", code)
			}
		})
	}
}

// Anything that is not an API path is handed to the app shell, which is what
// makes a client-side route survive a page reload.
func TestUnknownNonAPIPathServesAppShell(t *testing.T) {
	h := newHarness(t, "UTC")

	status, headers, raw := h.do(http.MethodGet, "/does/not/exist", "", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", status, raw)
	}
	if ct := headers.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(string(raw), "<!doctype html>") {
		t.Errorf("body is not the app shell: %s", raw)
	}
}

func TestContentTypeIsEnforced(t *testing.T) {
	h := newHarness(t, "UTC")

	status, _, raw := h.do(http.MethodPost, "/api/v1/events", `{"title":"x","start_at":1}`,
		map[string]string{"Content-Type": "text/plain"})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", status, raw)
	}
	if code := errorCode(t, h.decode(raw)); code != "invalid_parameter" {
		t.Errorf("error code = %q", code)
	}
}

func TestMalformedBodies(t *testing.T) {
	h := newHarness(t, "UTC")

	tests := []struct {
		name string
		body string
		want string
	}{
		// A malformed body is a transport problem.
		{"not json", `{"title":`, "invalid_body"},
		{"array instead of object", `[]`, "invalid_body"},
		// A timestamp field accepts a number or a string, so an array is the
		// wrong JSON type while an unparsable string is a bad value.
		{"wrong type for a timestamp", `{"title":"x","start_at":[1,2]}`, "invalid_body"},
		{"unparsable timestamp", `{"title":"x","start_at":"soon"}`, "validation_failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, _, raw := h.do(http.MethodPost, "/api/v1/events", tc.body, nil)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", status, raw)
			}
			if code := errorCode(t, h.decode(raw)); code != tc.want {
				t.Errorf("error code = %q, want %q", code, tc.want)
			}
		})
	}
}

func TestHealthAndIndex(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	status, _, raw := h.do(http.MethodGet, "/healthz", "", nil)
	if status != http.StatusOK {
		t.Fatalf("healthz status = %d, body: %s", status, raw)
	}
	body := h.decode(raw)
	if body["status"] != "ok" || body["database"] != "ok" {
		t.Errorf("health body = %v", body)
	}

	status, _, raw = h.do(http.MethodGet, "/api", "", nil)
	if status != http.StatusOK {
		t.Fatalf("discovery status = %d", status)
	}
	body = h.decode(raw)
	if body["service"] != "MyCalendar" {
		t.Errorf("service = %v", body["service"])
	}
	endpoints, ok := body["endpoints"].([]any)
	if !ok || len(endpoints) == 0 {
		t.Fatalf("discovery document has no endpoint list: %s", raw)
	}
}

// The root path belongs to the frontend; the JSON discovery document moved to
// /api so that both a browser and an agent have a sensible entry point.
func TestFrontendOwnsRootPath(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	for _, path := range []string{"/", "/some/route"} {
		t.Run(path, func(t *testing.T) {
			status, headers, raw := h.do(http.MethodGet, path, "", nil)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200", status)
			}
			if ct := headers.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Errorf("Content-Type = %q, want text/html", ct)
			}
			if !strings.Contains(string(raw), "<!doctype html>") {
				t.Errorf("body is not the app shell: %s", raw)
			}
		})
	}

	t.Run("non-GET on the root is rejected", func(t *testing.T) {
		status, headers, raw := h.do(http.MethodPost, "/", "{}", nil)
		if status != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", status)
		}
		if got := headers.Get("Allow"); got != "GET, HEAD" {
			t.Errorf("Allow = %q, want \"GET, HEAD\"", got)
		}
		if code := errorCode(t, h.decode(raw)); code != "method_not_allowed" {
			t.Errorf("error code = %q", code)
		}
	})
}

// An unknown API path must stay on the JSON contract instead of being answered
// with the single-page app.
func TestUnknownAPIPathIsJSON(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	status, headers, raw := h.do(http.MethodGet, "/api/v1/nope", "", nil)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
	if ct := headers.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if code := errorCode(t, h.decode(raw)); code != "not_found" {
		t.Errorf("error code = %q", code)
	}
}

func TestHelpDocument(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	status, headers, raw := h.do(http.MethodGet, "/help", "", nil)
	if status != http.StatusOK {
		t.Fatalf("help status = %d", status)
	}
	if ct := headers.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	doc := h.decode(raw)

	// Every documented endpoint must actually exist, which keeps the reference
	// honest as the router changes.
	endpoints, ok := doc["endpoints"].([]any)
	if !ok || len(endpoints) == 0 {
		t.Fatalf("help has no endpoints: %s", raw)
	}
	for _, rawEndpoint := range endpoints {
		e := rawEndpoint.(map[string]any)
		method, _ := e["method"].(string)
		path, _ := e["path"].(string)

		// Substitute a concrete id for the wildcard so the route resolves.
		probe := strings.ReplaceAll(path, "{id}", "1")
		if method == http.MethodDelete {
			// Delete a non-existent id: 404 still proves the route exists.
			status, _, body := h.do(method, probe, "", nil)
			if status == http.StatusMethodNotAllowed || status == http.StatusNotFound && errorCode(t, h.decode(body)) == "not_found" && strings.Contains(string(body), "no such endpoint") {
				t.Errorf("documented endpoint %s %s is not routed", method, path)
			}
			continue
		}
		status, _, body := h.do(method, probe, "", nil)
		if status == http.StatusMethodNotAllowed {
			t.Errorf("documented endpoint %s %s is not routed: %s", method, path, body)
		}
	}

	// The document must state the limits the code actually enforces.
	limits, _ := doc["limits"].(map[string]any)
	if limits == nil {
		t.Fatal("help has no limits section")
	}
	if limits["title_max_characters"].(float64) != service.MaxTitleRunes {
		t.Errorf("documented title limit = %v, code enforces %d", limits["title_max_characters"], service.MaxTitleRunes)
	}
	if limits["notes_max_characters"].(float64) != service.MaxNotesRunes {
		t.Errorf("documented notes limit = %v, code enforces %d", limits["notes_max_characters"], service.MaxNotesRunes)
	}
	if limits["reminders_per_event_max"].(float64) != service.MaxReminders {
		t.Errorf("documented reminder limit = %v, code enforces %d", limits["reminders_per_event_max"], service.MaxReminders)
	}
	if limits["limit_max"].(float64) != service.MaxPageSize {
		t.Errorf("documented page size = %v, code enforces %d", limits["limit_max"], service.MaxPageSize)
	}

	// Authentication must be documented as absent, since that is a deliberate
	// design decision rather than an oversight.
	auth, _ := doc["authentication"].(map[string]any)
	if auth == nil || auth["required"] != false {
		t.Errorf("authentication section = %v, want required=false", doc["authentication"])
	}
}

// The discovery document at /api is a second, hand-written index. It is easier
// to forget than /help, so it gets the same route check: everything it lists
// has to be something the router really serves.
func TestIndexDocument(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	status, _, raw := h.do(http.MethodGet, "/api", "", nil)
	if status != http.StatusOK {
		t.Fatalf("index status = %d", status)
	}
	doc := h.decode(raw)

	endpoints, ok := doc["endpoints"].([]any)
	if !ok || len(endpoints) == 0 {
		t.Fatalf("index has no endpoints: %s", raw)
	}

	// Every resource has to appear, not just the event ones it started with.
	listed := make(map[string]bool, len(endpoints))
	for _, rawEndpoint := range endpoints {
		e := rawEndpoint.(map[string]any)
		method, _ := e["method"].(string)
		path, _ := e["path"].(string)
		listed[method+" "+path] = true

		probe := strings.ReplaceAll(path, "{id}", "1")
		status, _, body := h.do(method, probe, "", nil)
		if status == http.StatusMethodNotAllowed {
			t.Errorf("indexed endpoint %s %s is not routed", method, path)
			continue
		}
		if status == http.StatusNotFound && strings.Contains(string(body), "no such endpoint") {
			t.Errorf("indexed endpoint %s %s is not routed: %s", method, path, body)
		}
	}

	for _, want := range []string{
		"GET /api/v1/events",
		"POST /api/v1/events/{id}/remove",
		"GET /api/v1/reminders",
		"PATCH /api/v1/reminders/{id}",
		"GET /api/v1/categories",
		"POST /api/v1/categories",
		"DELETE /api/v1/categories/{id}",
	} {
		if !listed[want] {
			t.Errorf("index does not list %q", want)
		}
	}

	// The two list endpoints hide the deleted category by default, which is the
	// detail a polling client most needs from a one-line summary.
	summaries := map[string]string{}
	for _, rawEndpoint := range endpoints {
		e := rawEndpoint.(map[string]any)
		summaries[e["method"].(string)+" "+e["path"].(string)], _ = e["summary"].(string)
	}
	for _, path := range []string{"GET /api/v1/events", "GET /api/v1/reminders"} {
		if !strings.Contains(summaries[path], "include_deleted") {
			t.Errorf("index summary for %s does not mention include_deleted: %q", path, summaries[path])
		}
	}
}

func TestHelpMarkdown(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	for _, tc := range []struct {
		name    string
		headers map[string]string
		path    string
	}{
		{"accept text/plain", map[string]string{"Accept": "text/plain"}, "/help"},
		{"format query", nil, "/help?format=markdown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, headers, raw := h.do(http.MethodGet, tc.path, "", tc.headers)
			if status != http.StatusOK {
				t.Fatalf("status = %d", status)
			}
			if ct := headers.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
				t.Errorf("Content-Type = %q, want text/markdown", ct)
			}
			text := string(raw)
			if !strings.HasPrefix(text, "# MyCalendar API reference") {
				t.Errorf("markdown does not start with a heading: %.60q", text)
			}
			for _, want := range []string{"## Endpoints", "## Timezones", "## Range queries", "## Data model", "## Examples"} {
				if !strings.Contains(text, want) {
					t.Errorf("markdown is missing %q", want)
				}
			}
			if bytes.Contains(raw, []byte("\x00")) {
				t.Error("markdown contains a NUL byte")
			}
		})
	}
}

func TestCORSPreflight(t *testing.T) {
	h := newHarness(t, "UTC")

	status, headers, _ := h.do(http.MethodOptions, "/api/v1/events", "",
		map[string]string{"Origin": "http://example.test", "Access-Control-Request-Method": "POST"})
	if status != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", status)
	}
	if got := headers.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
	}
	if got := headers.Get("Access-Control-Allow-Methods"); !strings.Contains(got, "PATCH") {
		t.Errorf("Access-Control-Allow-Methods = %q, want PATCH included", got)
	}
}

// With no parameters at all, a list request answers "what is on today".
func TestDefaultRangeIsToday(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	fixed := time.Date(2026, 3, 11, 15, 30, 0, 0, time.UTC)
	h.api.now = func() time.Time { return fixed }

	// 2026-03-11 in Shanghai.
	h.create(`{"title":"Today","start_at":1773205200,"end_at":1773208800}`)

	status, _, raw := h.do(http.MethodGet, "/api/v1/events?tz=Asia/Shanghai", "", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body: %s", status, raw)
	}
	body := h.decode(raw)
	r := body["range"].(map[string]any)
	if r["kind"] != "day" || r["start_date"] != "2026-03-11" {
		t.Errorf("default window = %v %v, want day 2026-03-11", r["kind"], r["start_date"])
	}
	if body["count"].(float64) != 1 {
		t.Errorf("count = %v, want 1", body["count"])
	}
}

// A limit of 2000 is the documented ceiling and is accepted.
func TestLimitBoundaries(t *testing.T) {
	h := newHarness(t, "UTC")

	for _, query := range []string{"limit=0", "limit=2000"} {
		status, _, raw := h.do(http.MethodGet, "/api/v1/events?"+query, "", nil)
		if status != http.StatusOK {
			t.Errorf("%s status = %d, want 200; body: %s", query, status, raw)
		}
	}
	for _, query := range []string{"limit=2001", "limit=-1"} {
		status, _, _ := h.do(http.MethodGet, "/api/v1/events?"+query, "", nil)
		if status != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400", query, status)
		}
	}
}

// Location travels through the whole stack: create, read back, clear.
func TestEventLocation(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	t.Run("round trips and is trimmed", func(t *testing.T) {
		ev := h.create(`{"title":"会议","start_at":"2026-10-20T10:00","location":"  3 楼会议室  "}`)
		if ev["location"] != "3 楼会议室" {
			t.Errorf("location = %v, want it trimmed", ev["location"])
		}
		id := eventID(t, ev)

		status, _, raw := h.do(http.MethodGet, fmt.Sprintf("/api/v1/events/%d", id), "", nil)
		if status != http.StatusOK {
			t.Fatalf("status = %d", status)
		}
		if got := h.decode(raw)["location"]; got != "3 楼会议室" {
			t.Errorf("location after reread = %v", got)
		}
	})

	t.Run("absent on create means no location", func(t *testing.T) {
		ev := h.create(`{"title":"无地点","start_at":"2026-10-20T10:00"}`)
		if ev["location"] != nil {
			t.Errorf("location = %v, want null", ev["location"])
		}
	})

	t.Run("null and an empty string both clear it", func(t *testing.T) {
		ev := h.create(`{"title":"可清空","start_at":"2026-10-20T10:00","location":"旧地点"}`)
		path := fmt.Sprintf("/api/v1/events/%d", eventID(t, ev))

		for _, body := range []string{`{"location":null}`, `{"location":"   "}`} {
			status, _, raw := h.do(http.MethodPatch, path, body, nil)
			if status != http.StatusOK {
				t.Fatalf("PATCH %s status = %d; body: %s", body, status, raw)
			}
			if got := h.decode(raw)["location"]; got != nil {
				t.Errorf("PATCH %s left location = %v, want null", body, got)
			}
		}
	})

	t.Run("an omitted field leaves it alone", func(t *testing.T) {
		ev := h.create(`{"title":"保留","start_at":"2026-10-20T10:00","location":"保留我"}`)
		path := fmt.Sprintf("/api/v1/events/%d", eventID(t, ev))

		status, _, raw := h.do(http.MethodPatch, path, `{"title":"改了标题"}`, nil)
		if status != http.StatusOK {
			t.Fatalf("status = %d", status)
		}
		if got := h.decode(raw)["location"]; got != "保留我" {
			t.Errorf("location = %v, want it untouched", got)
		}
	})

	t.Run("too long is rejected", func(t *testing.T) {
		long := strings.Repeat("地", 501)
		status, _, raw := h.do(http.MethodPost, "/api/v1/events",
			fmt.Sprintf(`{"title":"太长","start_at":"2026-10-20T10:00","location":%q}`, long), nil)
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body: %s", status, raw)
		}
		if fields := errorFields(t, h.decode(raw)); len(fields) == 0 || fields[0] != "location" {
			t.Errorf("details fields = %v, want [location]", fields)
		}
	})
}
