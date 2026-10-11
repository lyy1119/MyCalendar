// Package webui serves the compiled single-page frontend.
//
// The bundle is produced by Vite into ./dist and pulled into the binary with
// go:embed, but only when the `webui_embedded` build tag is set. Without the
// tag the package still compiles and serves a short page explaining that the
// frontend was not built into this binary, which keeps plain `go build` and
// `go test` working on a machine with no Node toolchain.
package webui

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// Handler returns the handler for the frontend: the real bundle when it was
// embedded, otherwise a placeholder page.
func Handler() http.Handler {
	if assets, ok := distFS(); ok {
		return NewSPAHandler(assets)
	}
	return placeholderHandler()
}

// Embedded reports whether a real frontend bundle is present in this binary.
func Embedded() bool {
	_, ok := distFS()
	return ok
}

// NewSPAHandler serves a single-page application from fsys.
//
// Requests for a file that exists are served directly; anything else falls
// back to index.html so that client-side routes deep-link correctly. A request
// that names a missing asset with a file extension is a genuine 404 instead,
// because answering it with HTML turns a missing bundle into a confusing MIME
// type error in the browser.
func NewSPAHandler(fsys fs.FS) http.Handler {
	files := http.FileServerFS(fsys)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" || name == "." {
			serveIndex(w, r, fsys)
			return
		}

		info, err := fs.Stat(fsys, name)
		if err == nil && !info.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				// Asset names carry a content hash, so they never change.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			files.ServeHTTP(w, r)
			return
		}

		if ext := path.Ext(name); ext != "" && ext != ".html" {
			http.NotFound(w, r)
			return
		}
		serveIndex(w, r, fsys)
	})
}

// serveIndex writes index.html, letting ServeContent handle HEAD requests and
// range handling.
func serveIndex(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		http.Error(w, "frontend index.html is missing from this build", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(data))
}

const placeholderHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>MyCalendar</title>
<style>
  :root { color-scheme: dark; }
  body { margin: 0; min-height: 100vh; display: flex; align-items: center;
         justify-content: center; background: #131314; color: #e3e3e3;
         font-family: Roboto, "PingFang SC", "Microsoft YaHei", sans-serif; }
  main { max-width: 620px; padding: 32px; line-height: 1.7; }
  h1 { font-size: 20px; font-weight: 500; margin: 0 0 4px; }
  p { margin: 12px 0; color: #9aa0a6; }
  code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  pre { background: #1b1b1b; border: 1px solid #333537; border-radius: 8px;
        padding: 12px 14px; overflow-x: auto; color: #d7e3fd; }
  a { color: #a8c7fa; }
</style>
</head>
<body>
<main>
  <h1>前端未编译进这个二进制</h1>
  <p>后端 API 正常可用：<a href="/help">/help</a>、<a href="/api">/api</a>、<a href="/healthz">/healthz</a>。</p>
  <p>要带上界面，请先构建前端再重新编译（会自动设置 <code>webui_embedded</code> 构建标签）：</p>
  <pre>make build</pre>
  <p>或者手动执行：</p>
  <pre>cd web &amp;&amp; npm ci &amp;&amp; npm run build
go build -tags webui_embedded -o mycalendar ./cmd/mycalendar</pre>
</main>
</body>
</html>
`

func placeholderHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.WriteString(w, placeholderHTML)
	})
}
