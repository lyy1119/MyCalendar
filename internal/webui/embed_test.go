//go:build webui_embedded

package webui

import (
	"io/fs"
	"strings"
	"testing"
)

// Verifies that a real Vite build was compiled into this binary, which is what
// `make build` and `go test -tags webui_embedded ./...` are for. Without this
// check the embedded bundle could silently go stale or missing.
func TestEmbeddedBundleIsPresent(t *testing.T) {
	if !Embedded() {
		t.Fatal("Embedded() = false with the webui_embedded tag set")
	}

	assets, ok := distFS()
	if !ok {
		t.Fatal("distFS() reported no assets")
	}

	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		t.Fatalf("reading embedded index.html: %v", err)
	}
	html := string(index)
	if !strings.Contains(html, "<div id=\"root\">") {
		t.Errorf("embedded index.html does not look like the app shell: %s", html)
	}
	// The bundle must reference its built assets, not the dev entry point.
	if strings.Contains(html, "/src/main.tsx") {
		t.Error("embedded index.html still points at the development entry point")
	}
	if !strings.Contains(html, "assets/") {
		t.Errorf("embedded index.html does not reference any built asset: %s", html)
	}
}
