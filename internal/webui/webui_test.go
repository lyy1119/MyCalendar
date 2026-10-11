package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// testFS mimics a Vite build: a hashed asset directory plus index.html.
func testFS() fs.FS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<!doctype html><title>MyCalendar</title>"),
		},
		"favicon.svg": &fstest.MapFile{Data: []byte("<svg/>")},
		"assets/index-abc123.js": &fstest.MapFile{
			Data: []byte("console.log('app')"),
		},
		"assets/index-abc123.css": &fstest.MapFile{
			Data: []byte("body{}"),
		},
	}
}

func TestSPAServesIndexAtRoot(t *testing.T) {
	srv := httptest.NewServer(NewSPAHandler(testFS()))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache for index.html", cc)
	}
}

func TestSPAServesHashedAssetsWithLongCache(t *testing.T) {
	srv := httptest.NewServer(NewSPAHandler(testFS()))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/assets/index-abc123.js")
	if err != nil {
		t.Fatalf("GET asset: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want an immutable directive for hashed assets", cc)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type = %q, want a javascript type", ct)
	}
}

// A deep link must reach the app rather than a 404, since the router lives in
// the browser.
func TestSPAFallsBackToIndexForRoutes(t *testing.T) {
	srv := httptest.NewServer(NewSPAHandler(testFS()))
	defer srv.Close()

	for _, path := range []string{"/some/route", "/settings", "/day/2026-03-11"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body := make([]byte, 64)
		n, _ := resp.Body.Read(body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, resp.StatusCode)
		}
		if !strings.Contains(string(body[:n]), "<!doctype html>") {
			t.Errorf("GET %s did not return the app shell: %q", path, string(body[:n]))
		}
	}
}

// Answering a missing script with HTML turns a packaging mistake into a
// confusing MIME type error, so those requests must 404.
func TestSPAReturns404ForMissingAssets(t *testing.T) {
	srv := httptest.NewServer(NewSPAHandler(testFS()))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/assets/index-missing.js")
	if err != nil {
		t.Fatalf("GET missing asset: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a missing asset", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "text/html") {
		t.Errorf("missing asset was answered with HTML (%q)", ct)
	}
}

func TestSPAServesOtherStaticFiles(t *testing.T) {
	srv := httptest.NewServer(NewSPAHandler(testFS()))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/favicon.svg")
	if err != nil {
		t.Fatalf("GET favicon: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	// A non-asset file is still versioned by the build, so it must not be
	// cached as aggressively as a hashed asset.
	if cc := resp.Header.Get("Cache-Control"); strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want no immutable directive", cc)
	}
}

func TestSPAHandlesHeadRequests(t *testing.T) {
	srv := httptest.NewServer(NewSPAHandler(testFS()))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodHead, srv.URL+"/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HEAD /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.ContentLength <= 0 {
		t.Errorf("Content-Length = %d, want the size of index.html", resp.ContentLength)
	}
}

func TestSPAReportsMissingIndex(t *testing.T) {
	srv := httptest.NewServer(NewSPAHandler(fstest.MapFS{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when index.html is absent", resp.StatusCode)
	}
}

func TestPlaceholderExplainsHowToBuild(t *testing.T) {
	srv := httptest.NewServer(placeholderHandler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	// The page has to point at the working machine endpoints and at the build
	// command, otherwise it is just a dead end.
	for _, want := range []string{"/help", "/api", "make build", "webui_embedded"} {
		if !strings.Contains(body, want) {
			t.Errorf("placeholder page does not mention %q", want)
		}
	}
}

func TestPlaceholderRejectsNonReadMethods(t *testing.T) {
	srv := httptest.NewServer(placeholderHandler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("POST /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

// Handler always returns something usable, whichever build produced it.
func TestHandlerIsAlwaysUsable(t *testing.T) {
	handler := Handler()
	if handler == nil {
		t.Fatal("Handler() returned nil")
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}
