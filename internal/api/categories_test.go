package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// listCategories returns the decoded category list.
func listCategories(t *testing.T, h *harness) []map[string]any {
	t.Helper()
	status, _, raw := h.do(http.MethodGet, "/api/v1/categories", "", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", status, raw)
	}
	body := h.decode(raw)
	items, ok := body["categories"].([]any)
	if !ok {
		t.Fatalf("categories is not an array: %s", raw)
	}
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		out = append(out, it.(map[string]any))
	}
	return out
}

// TestSeedCategories pins the two rows the API itself depends on.
func TestSeedCategories(t *testing.T) {
	h := newHarness(t, "UTC")

	categories := listCategories(t, h)
	if len(categories) != 2 {
		t.Fatalf("got %d categories, want the two seed rows: %v", len(categories), categories)
	}

	byID := map[int64]map[string]any{}
	for _, c := range categories {
		byID[int64(c["id"].(float64))] = c
	}

	deleted, ok := byID[-1]
	if !ok {
		t.Fatal("the deleted category (-1) is missing")
	}
	if deleted["name"] != "删除" {
		t.Errorf("category -1 name = %v, want 删除", deleted["name"])
	}
	if deleted["color"] != "#9aa0a6" {
		t.Errorf("category -1 colour = %v, want #9aa0a6", deleted["color"])
	}

	uncategorized, ok := byID[0]
	if !ok {
		t.Fatal("the uncategorized category (0) is missing")
	}
	if uncategorized["name"] != "默认" || uncategorized["color"] != "#1a73e8" {
		t.Errorf("category 0 = %v, want 默认/#1a73e8", uncategorized)
	}
}

func TestCategoryCRUD(t *testing.T) {
	h := newHarness(t, "UTC")

	t.Run("create assigns ids from 1", func(t *testing.T) {
		status, headers, raw := h.do(http.MethodPost, "/api/v1/categories",
			`{"name":"工作","color":"#4A90E2"}`, nil)
		if status != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body: %s", status, raw)
		}
		if got := headers.Get("Location"); got != "/api/v1/categories/1" {
			t.Errorf("Location = %q, want /api/v1/categories/1", got)
		}
		created := h.decode(raw)
		if created["id"].(float64) != 1 {
			t.Errorf("id = %v, want 1", created["id"])
		}
		// Colours are normalised so they compare equal however they were typed.
		if created["color"] != "#4a90e2" {
			t.Errorf("colour = %v, want it lower-cased", created["color"])
		}

		second := h.decode(mustDo(t, h, http.MethodPost, "/api/v1/categories",
			`{"name":"生活","color":"#abc"}`))
		if second["id"].(float64) != 2 {
			t.Errorf("second id = %v, want 2", second["id"])
		}
		if second["color"] != "#aabbcc" {
			t.Errorf("#abc was not expanded: %v", second["color"])
		}
	})

	t.Run("patch renames and recolours independently", func(t *testing.T) {
		status, _, raw := h.do(http.MethodPatch, "/api/v1/categories/1", `{"name":"工作事项"}`, nil)
		if status != http.StatusOK {
			t.Fatalf("status = %d; body: %s", status, raw)
		}
		got := h.decode(raw)
		if got["name"] != "工作事项" {
			t.Errorf("name = %v", got["name"])
		}
		if got["color"] != "#4a90e2" {
			t.Errorf("colour = %v, want it untouched", got["color"])
		}

		status, _, raw = h.do(http.MethodPatch, "/api/v1/categories/1", `{"color":"#111111"}`, nil)
		if status != http.StatusOK {
			t.Fatalf("status = %d", status)
		}
		got = h.decode(raw)
		if got["color"] != "#111111" || got["name"] != "工作事项" {
			t.Errorf("patch changed the wrong field: %v", got)
		}
	})

	t.Run("validation", func(t *testing.T) {
		cases := []struct {
			body  string
			field string
		}{
			{`{"color":"#fff"}`, "name"},
			{`{"name":"  ","color":"#fff"}`, "name"},
			{`{"name":"x"}`, "color"},
			{`{"name":"x","color":"blue"}`, "color"},
			{`{"name":"x","color":"#12345"}`, "color"},
		}
		for _, tc := range cases {
			status, _, raw := h.do(http.MethodPost, "/api/v1/categories", tc.body, nil)
			if status != http.StatusBadRequest {
				t.Fatalf("POST %s status = %d, want 400; body: %s", tc.body, status, raw)
			}
			if fields := errorFields(t, h.decode(raw)); len(fields) == 0 || fields[0] != tc.field {
				t.Errorf("POST %s details = %v, want first field %q", tc.body, fields, tc.field)
			}
		}
	})

	t.Run("unknown category is 404", func(t *testing.T) {
		for _, path := range []string{"/api/v1/categories/999", "/api/v1/categories/-9"} {
			if status, _, _ := h.do(http.MethodGet, path, "", nil); status != http.StatusNotFound {
				t.Errorf("GET %s status = %d, want 404", path, status)
			}
		}
	})
}

// -1 is a state the API names rather than a grouping the user chose, so its
// name is fixed; its colour -- and everything about category 0 -- stays
// editable.
func TestBuiltInCategoryNames(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	// Renaming -1 is refused, and the refusal names the reason.
	status, _, raw := h.do(http.MethodPatch, "/api/v1/categories/-1", `{"name":"垃圾箱"}`, nil)
	if status != http.StatusConflict {
		t.Fatalf("PATCH -1 name status = %d, want 409; body: %s", status, raw)
	}
	if code := errorCode(t, h.decode(raw)); code != "category_name_fixed" {
		t.Errorf("error code = %q, want category_name_fixed", code)
	}

	// Sending the name it already has is not a rename, so it goes through and
	// leaves the row alone.
	status, _, raw = h.do(http.MethodPatch, "/api/v1/categories/-1", `{"name":"删除"}`, nil)
	if status != http.StatusOK {
		t.Fatalf("PATCH -1 with the same name = %d, want 200; body: %s", status, raw)
	}
	if got := h.decode(raw); got["name"] != "删除" {
		t.Errorf("name = %v", got["name"])
	}

	// The colour of -1 changes freely.
	status, _, raw = h.do(http.MethodPatch, "/api/v1/categories/-1", `{"color":"#cc4444"}`, nil)
	if status != http.StatusOK {
		t.Fatalf("PATCH -1 colour status = %d, want 200; body: %s", status, raw)
	}
	if got := h.decode(raw); got["color"] != "#cc4444" || got["name"] != "删除" {
		t.Errorf("category -1 after recolour = %v, want #cc4444 with the name unchanged", got)
	}

	// Category 0 can be renamed: it is just the default grouping.
	status, _, raw = h.do(http.MethodPatch, "/api/v1/categories/0", `{"name":"个人"}`, nil)
	if status != http.StatusOK {
		t.Fatalf("PATCH 0 name status = %d, want 200; body: %s", status, raw)
	}
	if got := h.decode(raw); got["name"] != "个人" {
		t.Errorf("name = %v, want 个人", got["name"])
	}

	// A user category is unaffected by any of this.
	created := h.decode(mustDo(t, h, http.MethodPost, "/api/v1/categories",
		`{"name":"工作","color":"#4a90e2"}`))
	id := fmt.Sprintf("%v", created["id"])
	if status, _, _ := h.do(http.MethodPatch, "/api/v1/categories/"+id, `{"name":"私事"}`, nil); status != http.StatusOK {
		t.Errorf("renaming a user category status = %d, want 200", status)
	}
}

// The two system categories are part of the API's contract, so they cannot be
// deleted; user categories can, and their events fall back to the default one.
func TestCategoryDelete(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	for _, id := range []string{"-1", "0"} {
		status, _, raw := h.do(http.MethodDelete, "/api/v1/categories/"+id, "", nil)
		if status != http.StatusConflict {
			t.Fatalf("DELETE category %s status = %d, want 409; body: %s", id, status, raw)
		}
		if code := errorCode(t, h.decode(raw)); code != "category_protected" {
			t.Errorf("error code = %q, want category_protected", code)
		}
	}

	// A user category, with an event in it.
	created := h.decode(mustDo(t, h, http.MethodPost, "/api/v1/categories",
		`{"name":"临时","color":"#ff0000"}`))
	categoryID := int(created["id"].(float64))
	event := h.create(fmt.Sprintf(`{"title":"带分类","start_at":"2026-10-20T10:00","category_id":%d}`, categoryID))
	eventID := eventID(t, event)

	if status, _, _ := h.do(http.MethodDelete, fmt.Sprintf("/api/v1/categories/%d", categoryID), "", nil); status != http.StatusNoContent {
		t.Fatalf("DELETE user category status = %d, want 204", status)
	}

	// The event survives, moved to uncategorized.
	status, _, raw := h.do(http.MethodGet, fmt.Sprintf("/api/v1/events/%d", eventID), "", nil)
	if status != http.StatusOK {
		t.Fatalf("the event was lost with its category: status %d", status)
	}
	if got := h.decode(raw); got["category_id"].(float64) != 0 {
		t.Errorf("category_id = %v, want 0 after its category was deleted", got["category_id"])
	}
}

func TestEventCategoryField(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	created := h.decode(mustDo(t, h, http.MethodPost, "/api/v1/categories",
		`{"name":"工作","color":"#4a90e2"}`))
	categoryID := int(created["id"].(float64))

	t.Run("defaults to uncategorized", func(t *testing.T) {
		ev := h.create(`{"title":"默认分类","start_at":"2026-10-20T10:00"}`)
		if ev["category_id"].(float64) != 0 {
			t.Errorf("category_id = %v, want 0", ev["category_id"])
		}
	})

	t.Run("can be set on create and changed later", func(t *testing.T) {
		ev := h.create(fmt.Sprintf(`{"title":"有分类","start_at":"2026-10-20T11:00","category_id":%d}`, categoryID))
		if int(ev["category_id"].(float64)) != categoryID {
			t.Fatalf("category_id = %v, want %d", ev["category_id"], categoryID)
		}
		path := fmt.Sprintf("/api/v1/events/%d", eventID(t, ev))

		status, _, raw := h.do(http.MethodPatch, path, `{"category_id":0}`, nil)
		if status != http.StatusOK {
			t.Fatalf("status = %d; body: %s", status, raw)
		}
		if got := h.decode(raw)["category_id"].(float64); got != 0 {
			t.Errorf("category_id = %v, want 0", got)
		}
	})

	t.Run("a category that does not exist is rejected", func(t *testing.T) {
		status, _, raw := h.do(http.MethodPost, "/api/v1/events",
			`{"title":"坏分类","start_at":"2026-10-20T12:00","category_id":999}`, nil)
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body: %s", status, raw)
		}
		if fields := errorFields(t, h.decode(raw)); len(fields) == 0 || fields[0] != "category_id" {
			t.Errorf("details fields = %v, want [category_id]", fields)
		}
	})

	t.Run("null is rejected", func(t *testing.T) {
		status, _, _ := h.do(http.MethodPost, "/api/v1/events",
			`{"title":"空分类","start_at":"2026-10-20T13:00","category_id":null}`, nil)
		if status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", status)
		}
	})
}

// Deleting through the category field and through DELETE must be the same
// thing, and both must be reversible.
func TestDeleteEqualsDeletingCategory(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")

	viaEndpoint := h.create(`{"title":"用 DELETE","start_at":"2026-10-20T10:00"}`)
	viaCategory := h.create(`{"title":"用分类","start_at":"2026-10-20T11:00"}`)

	h.do(http.MethodDelete, fmt.Sprintf("/api/v1/events/%d", eventID(t, viaEndpoint)), "", nil)
	h.do(http.MethodPatch, fmt.Sprintf("/api/v1/events/%d", eventID(t, viaCategory)), `{"category_id":-1}`, nil)

	for name, ev := range map[string]map[string]any{"viaEndpoint": viaEndpoint, "viaCategory": viaCategory} {
		status, _, raw := h.do(http.MethodGet, fmt.Sprintf("/api/v1/events/%d", eventID(t, ev)), "", nil)
		if status != http.StatusOK {
			t.Fatalf("%s: status = %d", name, status)
		}
		if got := h.decode(raw)["category_id"].(float64); got != -1 {
			t.Errorf("%s: category_id = %v, want -1", name, got)
		}
	}

	t.Run("both disappear from range queries", func(t *testing.T) {
		status, _, raw := h.do(http.MethodGet, "/api/v1/events?range=month&date=2026-10-20&range=month&tz=Asia/Shanghai", "", nil)
		if status != http.StatusBadRequest {
			// range twice is still fine; the server takes the last one
			_ = raw
		}
		status, _, raw = h.do(http.MethodGet, "/api/v1/events?range=month&date=2026-10-20&tz=Asia/Shanghai", "", nil)
		if status != http.StatusOK {
			t.Fatalf("status = %d", status)
		}
		body := h.decode(raw)
		if body["count"].(float64) != 0 {
			t.Errorf("deleted events are still on the calendar: %v", body["count"])
		}

		// Unless asked for.
		status, _, raw = h.do(http.MethodGet, "/api/v1/events?range=month&date=2026-10-20&tz=Asia/Shanghai&include_deleted=true", "", nil)
		if status != http.StatusOK {
			t.Fatalf("status = %d", status)
		}
		if got := h.decode(raw)["count"].(float64); got != 2 {
			t.Errorf("include_deleted returned %v, want 2", got)
		}
	})

	t.Run("restoring is setting the category back", func(t *testing.T) {
		h.do(http.MethodPatch, fmt.Sprintf("/api/v1/events/%d", eventID(t, viaEndpoint)), `{"category_id":0}`, nil)
		status, _, raw := h.do(http.MethodGet, "/api/v1/events?range=month&date=2026-10-20&tz=Asia/Shanghai", "", nil)
		if status != http.StatusOK {
			t.Fatalf("status = %d", status)
		}
		if got := h.decode(raw)["count"].(float64); got != 1 {
			t.Errorf("restored event count = %v, want 1", got)
		}
	})
}

func TestListByCategory(t *testing.T) {
	h := newHarness(t, "Asia/Shanghai")
	created := h.decode(mustDo(t, h, http.MethodPost, "/api/v1/categories", `{"name":"工作","color":"#4a90e2"}`))
	categoryID := int(created["id"].(float64))

	h.create(fmt.Sprintf(`{"title":"在工作里","start_at":"2026-10-20T10:00","category_id":%d}`, categoryID))
	h.create(`{"title":"未分类","start_at":"2026-10-20T11:00"}`)

	status, _, raw := h.do(http.MethodGet,
		fmt.Sprintf("/api/v1/events?range=month&date=2026-10-20&tz=Asia/Shanghai&category_id=%d", categoryID), "", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d; body: %s", status, raw)
	}
	body := h.decode(raw)
	if body["count"].(float64) != 1 {
		t.Fatalf("count = %v, want 1", body["count"])
	}
	events := body["events"].([]any)
	if title := events[0].(map[string]any)["title"]; title != "在工作里" {
		t.Errorf("title = %v", title)
	}
}

// mustDo performs a request and fails unless it succeeded.
func mustDo(t *testing.T, h *harness, method, path, body string) []byte {
	t.Helper()
	status, _, raw := h.do(method, path, body, nil)
	if status < 200 || status > 299 {
		t.Fatalf("%s %s status = %d; body: %s", method, path, status, raw)
	}
	return raw
}

// The help document must describe every category endpoint that exists.
func TestHelpListsCategoryEndpoints(t *testing.T) {
	h := newHarness(t, "UTC")
	status, _, raw := h.do(http.MethodGet, "/help", "", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	doc := string(raw)
	for _, want := range []string{"/api/v1/categories", "/api/v1/events/{id}/remove"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the help document does not mention %s", want)
		}
	}
}
