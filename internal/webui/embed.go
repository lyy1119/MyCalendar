//go:build webui_embedded

package webui

import (
	"embed"
	"io/fs"
)

// dist is produced by `npm run build` in web/ and emitted into this package.
//
// The build tag keeps this file out of ordinary builds so that `go build ./...`
// and `go test ./...` work without a Node toolchain and without a dist
// directory present. `make build` builds the frontend first and then compiles
// with -tags webui_embedded.
//
//go:embed all:dist
var dist embed.FS

func distFS() (fs.FS, bool) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, false
	}
	return sub, true
}
