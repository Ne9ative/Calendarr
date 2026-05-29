// Package web embeds the front-end assets (HTML, JS, CSS, images, icons) served
// by the server. Assets are organized into css/, js/, img/, and icons/
// subfolders; the embedded tree is rooted at this package's directory and paths
// are served as-is (e.g. "index.html", "css/style.css", "js/app.js").
package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html css js img icons
var assets embed.FS

// FS is the front-end file tree, rooted at this package's directory.
var FS fs.FS = assets
