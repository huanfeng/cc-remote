package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var bundled embed.FS

func Files() fs.FS {
	f, err := fs.Sub(bundled, "dist")
	if err != nil {
		panic(err)
	}
	return f
}
