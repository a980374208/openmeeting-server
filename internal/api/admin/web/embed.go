package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html app.css app.js
var embeddedFiles embed.FS

func StaticFS() fs.FS {
	return embeddedFiles
}
