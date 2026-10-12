package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// newEventForReminders creates an event and returns its id.
func newEventForReminders(t *testing.T, h *harness, title string) int {
	t.Helper()
	ev := h.create(fmt.Sprintf(`{"title":%q,"start_at":"2026-10-20T10:00"}`, title))
	return eventID(t, ev)
}

// addReminder posts a reminder and returns the decoded body.
func addReminder(t *testing.T, h *harness, body string) map[string]any {
	t.Helper()
	status, _, raw := h.do(http.MethodPost, "/api/v1/reminders?tz=Asia/Shanghai", body, nil)
	if status != http.StatusCreated {
		t.Fatalf("create reminder status = %d, want 201; body: %s", status, raw)
	}
	return h.decode(raw)
}

func reminderID(t *testing.T, r map[string]any) int {
	t.Helper()
	f, ok := r["id"].(float64)
	if !ok {
		t.Fatalf("reminder has no numeric id: %v", r)
	}
	return int(f)
}

func TestReminderCreateListGet(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")
	eventID := newEventForReminders(t, h, "会议")

	// A reminder time may be sent as a wall clock, which is read in tz.
	status, headers, raw := h.do(http.MethodPost, "/api/v1/reminders?tz=Asia/Shanghai",
		fmt.Sprintf(`{"event_id":%d,"remind_at":"2026-10-20T09:45"}`, eventID), nil)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", status, raw)
	}
	if got := headers.Get("Location"); got != "/api/v1/reminders/1" {
		t.Errorf("Location = %q", got)
	}

	created := h.decode(raw)
	if created["event_id"].(float64) != float64(eventID) {
		t.Errorf("event_id = %v, want %d", created["event_id"], eventID)
	}
	if created["remind_at_local"] != "2026-10-20T09:45:00+08:00" {
		t.Errorf("remind_at_local = %v", created["remind_at_local"])
	}
	// 2026-10-20T09:45:00+08:00
	if created["remind_at"].(float64) != 1792460700 {
		t.Errorf("remind_at = %v, want 1792460700", created["remind_at"])
	}
	id := reminderID(t, created)

	t.Run("fetch one", func(t *testing.T) {
		status, _, raw := h.do(http.MethodGet, fmt.Sprintf("/api/v1/reminders/%d?tz=Asia/Shanghai", id), "", nil)
		if status != http.StatusOK {
			t.Fatalf("status = %d; body: %s", status, raw)
		}
		if got := h.decode(raw); got["remind_at_local"] != "2026-10-20T09:45:00+08:00" {
			t.Errorf("remind_at_local = %v", got["remind_at_local"])
		}
	})

	t.Run("list is ordered and filtered", func(t *testing.T) {
		addReminder(t, h, fmt.Sprintf(`{"event_id":%d,"remind_at":"2026-10-20T09:00"}`, eventID))

		status, _, raw := h.do(http.MethodGet, "/api/v1/reminders?tz=Asia/Shanghai", "", nil)
		if status != http.StatusOK {
			t.Fatalf("status = %d", status)
		}
		body := h.decode(raw)
		if body["count"].(float64) != 2 {
			t.Fatalf("count = %v, want 2", body["count"])
		}
		list := body["reminders"].([]any)
		first := list[0].(map[string]any)["remind_at_local"]
		if first != "2026-10-20T09:00:00+08:00" {
			t.Errorf("reminders are not ordered by remind_at: first = %v", first)
		}

		status, _, raw = h.do(http.MethodGet, fmt.Sprintf("/api/v1/reminders?event_id=%d", eventID), "", nil)
		if status != http.StatusOK {
			t.Fatalf("status = %d", status)
		}
		if got := h.decode(raw); got["count"].(float64) != 2 {
			t.Errorf("event_id filter returned %v", got["count"])
		}

		// An event with no reminders is an empty list, never a 404.
		other := newEventForReminders(t, h, "无提醒")
		status, _, raw = h.do(http.MethodGet, fmt.Sprintf("/api/v1/reminders?event_id=%d", other), "", nil)
		if status != http.StatusOK {
			t.Fatalf("status = %d", status)
		}
		body = h.decode(raw)
		if body["count"].(float64) != 0 {
			t.Errorf("count = %v, want 0", body["count"])
		}
		if reminders, ok := body["reminders"].([]any); !ok || len(reminders) != 0 {
			t.Errorf("reminders = %#v, want []", body["reminders"])
		}
	})

	t.Run("time range filter", func(t *testing.T) {
		status, _, raw := h.do(http.MethodGet,
			"/api/v1/reminders?tz=Asia/Shanghai&from=2026-10-20T09:30&to=2026-10-20T10:00", "", nil)
		if status != http.StatusOK {
			t.Fatalf("status = %d; body: %s", status, raw)
		}
		body := h.decode(raw)
		if body["count"].(float64) != 1 {
			t.Fatalf("count = %v, want 1 (only the 09:45 reminder)", body["count"])
		}
	})
}

func TestReminderDuplicateIsConflict(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")
	eventID := newEventForReminders(t, h, "会议")
	body := fmt.Sprintf(`{"event_id":%d,"remind_at":"2026-10-20T09:45"}`, eventID)

	addReminder(t, h, body)

	status, _, raw := h.do(http.MethodPost, "/api/v1/reminders?tz=Asia/Shanghai", body, nil)
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", status, raw)
	}
	got := h.decode(raw)
	if code := errorCode(t, got); code != "reminder_exists" {
		t.Errorf("error code = %q, want reminder_exists", code)
	}
	if msg := got["error"].(map[string]any)["message"].(string); !strings.Contains(msg, "already has a reminder") {
		t.Errorf("message does not explain the clash: %q", msg)
	}

	// The same reminder time on a different event is not a clash.
	other := newEventForReminders(t, h, "另一个会议")
	h.do(http.MethodPost, "/api/v1/reminders?tz=Asia/Shanghai",
		fmt.Sprintf(`{"event_id":%d,"remind_at":"2026-10-20T09:45"}`, other), nil)
}

func TestReminderCreateValidation(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")
	eventID := newEventForReminders(t, h, "会议")

	tests := []struct {
		name  string
		body  string
		field string
	}{
		{"missing event_id", `{"remind_at":1792460700}`, "event_id"},
		{"unknown event", `{"event_id":999999,"remind_at":1792460700}`, "event_id"},
		{"zero event", `{"event_id":0,"remind_at":1792460700}`, "event_id"},
		{"missing remind_at", fmt.Sprintf(`{"event_id":%d}`, eventID), "remind_at"},
		{"null remind_at", fmt.Sprintf(`{"event_id":%d,"remind_at":null}`, eventID), "remind_at"},
		{"unparsable remind_at", fmt.Sprintf(`{"event_id":%d,"remind_at":"soon"}`, eventID), "remind_at"},
		{"milliseconds", fmt.Sprintf(`{"event_id":%d,"remind_at":1792460700000}`, eventID), "remind_at"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, _, raw := h.do(http.MethodPost, "/api/v1/reminders?tz=Asia/Shanghai", tc.body, nil)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", status, raw)
			}
			body := h.decode(raw)
			if code := errorCode(t, body); code != "validation_failed" {
				t.Errorf("error code = %q, want validation_failed", code)
			}
			if fields := errorFields(t, body); len(fields) == 0 || fields[0] != tc.field {
				t.Errorf("details fields = %v, want first %q", fields, tc.field)
			}
		})
	}

	t.Run("unknown field is rejected", func(t *testing.T) {
		status, _, raw := h.do(http.MethodPost, "/api/v1/reminders",
			fmt.Sprintf(`{"event_id":%d,"remind_at":1,"when":2}`, eventID), nil)
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", status)
		}
		if code := errorCode(t, h.decode(raw)); code != "invalid_parameter" {
			t.Errorf("error code = %q", code)
		}
	})
}

func TestReminderPerEventCap(t *testing.T) {
	h := newHarness(t, "UTC")
	eventID := newEventForReminders(t, h, "会议")

	base := 1792460000
	for i := 0; i < 50; i++ {
		addReminder(t, h, fmt.Sprintf(`{"event_id":%d,"remind_at":%d}`, eventID, base+i*60))
	}

	status, _, raw := h.do(http.MethodPost, "/api/v1/reminders?tz=UTC",
		fmt.Sprintf(`{"event_id":%d,"remind_at":%d}`, eventID, base+9999), nil)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 once the cap is reached; body: %s", status, raw)
	}
	if fields := errorFields(t, h.decode(raw)); len(fields) == 0 || fields[0] != "remind_at" {
		t.Errorf("details fields = %v", fields)
	}
}

func TestReminderPatch(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")
	eventID := newEventForReminders(t, h, "会议")
	first := addReminder(t, h, fmt.Sprintf(`{"event_id":%d,"remind_at":"2026-10-20T09:00"}`, eventID))
	second := addReminder(t, h, fmt.Sprintf(`{"event_id":%d,"remind_at":"2026-10-20T09:45"}`, eventID))
	firstID, secondID := reminderID(t, first), reminderID(t, second)

	t.Run("moves the time and keeps created_at", func(t *testing.T) {
		status, _, raw := h.do(http.MethodPatch, fmt.Sprintf("/api/v1/reminders/%d?tz=Asia/Shanghai", firstID),
			`{"remind_at":"2026-10-20T08:30"}`, nil)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", status, raw)
		}
		got := h.decode(raw)
		if got["remind_at_local"] != "2026-10-20T08:30:00+08:00" {
			t.Errorf("remind_at_local = %v", got["remind_at_local"])
		}
		if got["created_at"] != first["created_at"] {
			t.Errorf("created_at changed: %v -> %v", first["created_at"], got["created_at"])
		}
		if int(got["event_id"].(float64)) != eventID {
			t.Errorf("event_id changed to %v", got["event_id"])
		}
	})

	t.Run("moving onto another reminder is a conflict", func(t *testing.T) {
		status, _, raw := h.do(http.MethodPatch, fmt.Sprintf("/api/v1/reminders/%d", firstID),
			fmt.Sprintf(`{"remind_at":%d}`, int(second["remind_at"].(float64))), nil)
		if status != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body: %s", status, raw)
		}
		if code := errorCode(t, h.decode(raw)); code != "reminder_exists" {
			t.Errorf("error code = %q", code)
		}
	})

	t.Run("patching an unknown reminder is 404", func(t *testing.T) {
		status, _, _ := h.do(http.MethodPatch, "/api/v1/reminders/999999", `{"remind_at":1}`, nil)
		if status != http.StatusNotFound {
			t.Errorf("status = %d, want 404", status)
		}
	})

	t.Run("remind_at is required", func(t *testing.T) {
		status, _, raw := h.do(http.MethodPatch, fmt.Sprintf("/api/v1/reminders/%d", secondID), `{}`, nil)
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body: %s", status, raw)
		}
		if fields := errorFields(t, h.decode(raw)); len(fields) == 0 || fields[0] != "remind_at" {
			t.Errorf("details fields = %v", fields)
		}
	})
}

func TestReminderDelete(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")
	eventID := newEventForReminders(t, h, "会议")
	created := addReminder(t, h, fmt.Sprintf(`{"event_id":%d,"remind_at":1792460700}`, eventID))
	id := reminderID(t, created)

	status, _, raw := h.do(http.MethodDelete, fmt.Sprintf("/api/v1/reminders/%d", id), "", nil)
	if status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", status, raw)
	}
	if len(raw) != 0 {
		t.Errorf("204 must have an empty body, got %s", raw)
	}
	status, _, _ = h.do(http.MethodDelete, fmt.Sprintf("/api/v1/reminders/%d", id), "", nil)
	if status != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", status)
	}
	// The event survives its reminder.
	status, _, _ = h.do(http.MethodGet, fmt.Sprintf("/api/v1/events/%d", eventID), "", nil)
	if status != http.StatusOK {
		t.Errorf("event was removed with its reminder: status %d", status)
	}
}

func TestReminderCascadeOnEventDelete(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")
	eventID := newEventForReminders(t, h, "会议")
	addReminder(t, h, fmt.Sprintf(`{"event_id":%d,"remind_at":1792460700}`, eventID))

	h.do(http.MethodDelete, fmt.Sprintf("/api/v1/events/%d", eventID), "", nil)

	status, _, raw := h.do(http.MethodGet, "/api/v1/reminders", "", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if got := h.decode(raw); got["count"].(float64) != 0 {
		t.Errorf("reminders survived the event: %v", got["count"])
	}
}

func TestReminderMethodNotAllowed(t *testing.T) {
	h := newHarness(t, "UTC")

	tests := []struct {
		method string
		path   string
		allow  string
	}{
		{http.MethodPut, "/api/v1/reminders", "GET, POST"},
		{http.MethodDelete, "/api/v1/reminders", "GET, POST"},
		{http.MethodPost, "/api/v1/reminders/1", "GET, PATCH, DELETE"},
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
		})
	}
}

// The event payload keeps working as a whole-set write, and its times are
// resolved in the request timezone like any other timestamp.
func TestEventPayloadRemindersStillWork(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	ev := h.create(`{"title":"会议","start_at":"2026-10-20T10:00","reminders":[{"remind_at":"2026-10-20T09:45"},{"remind_at":"2026-10-20T09:00"}]}`)
	eventID := eventID(t, ev)

	reminders, ok := ev["reminders"].([]any)
	if !ok || len(reminders) != 2 {
		t.Fatalf("reminders = %#v, want 2", ev["reminders"])
	}
	first := reminders[0].(map[string]any)
	if first["remind_at"].(float64) != 1792458000 { // 2026-10-20T09:00:00+08:00
		t.Errorf("reminder was not resolved in tz: %v", first["remind_at"])
	}

	// The standalone list agrees with what the event payload returned.
	status, _, raw := h.do(http.MethodGet, fmt.Sprintf("/api/v1/reminders?event_id=%d&tz=Asia/Shanghai", eventID), "", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if got := h.decode(raw); got["count"].(float64) != 2 {
		t.Errorf("standalone list count = %v, want 2", got["count"])
	}
}

// A reminder nested in an event payload must be rendered exactly like one from
// the reminder endpoints, otherwise clients need two code paths.
func TestNestedRemindersCarryLocalTime(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	ev := h.create(`{"title":"会议","start_at":"2026-10-20T10:00","reminders":[{"remind_at":"2026-10-20T09:45"}]}`)
	eventID := eventID(t, ev)

	reminders, ok := ev["reminders"].([]any)
	if !ok || len(reminders) != 1 {
		t.Fatalf("reminders = %#v", ev["reminders"])
	}
	nested := reminders[0].(map[string]any)
	if nested["remind_at_local"] != "2026-10-20T09:45:00+08:00" {
		t.Errorf("nested remind_at_local = %v, want 2026-10-20T09:45:00+08:00", nested["remind_at_local"])
	}

	// The standalone read of the same row agrees field for field.
	status, _, raw := h.do(http.MethodGet, fmt.Sprintf("/api/v1/reminders?event_id=%d&tz=Asia/Shanghai", eventID), "", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	standalone := h.decode(raw)["reminders"].([]any)[0].(map[string]any)
	for _, field := range []string{"id", "event_id", "remind_at", "created_at", "remind_at_local"} {
		if nested[field] != standalone[field] {
			t.Errorf("field %q differs: nested %v vs standalone %v", field, nested[field], standalone[field])
		}
	}
}
