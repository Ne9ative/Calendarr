// Package web embeds the front-end assets (HTML, JS, CSS, icons) served by the
// server. The files live directly in this directory, so the embedded tree is
// rooted here — paths are served as-is (e.g. "index.html", "icons/favicon.svg").
package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html *.css *.js logo.png favicon.ico icons
var assets embed.FS

// FS is the front-end file tree, rooted at this package's directory.
var FS fs.FS = assets
