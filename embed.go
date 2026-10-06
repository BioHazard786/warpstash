package warpstash

import (
	"embed"
	"io/fs"
)

//go:embed all:web/dist
var webDist embed.FS

// StaticFS returns the embedded filesystem rooted at the compiled Astro web/dist directory.
func StaticFS() (fs.FS, error) {
	return fs.Sub(webDist, "web/dist")
}
