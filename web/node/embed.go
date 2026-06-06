// Package nodeui embeds the static node console served by hopper-agent on its
// local (loopback) address. Keeping the embed directive in the same directory as
// the assets lets the agent serve the GUI with no build or copy step.
package nodeui

import (
	"embed"
	"io/fs"
)

//go:embed index.html app.js styles.css
var files embed.FS

// FS returns the embedded node-console assets as a filesystem suitable for
// http.FileServerFS.
func FS() fs.FS { return files }
