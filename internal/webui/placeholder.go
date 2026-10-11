//go:build !webui_embedded

package webui

import "io/fs"

// distFS reports that no bundle was compiled in.
//
// A plain `go build` lands here, which keeps the backend buildable and testable
// on a machine with no Node toolchain; the HTTP layer then serves the
// placeholder page instead of the app.
func distFS() (fs.FS, bool) { return nil, false }
