// Package webui holds the built web frontend (web/, built by Vite into
// internal/webui/dist). A build without a prior `npm run build` embeds only
// dist/.gitkeep; Present reports false then.
package webui

import (
	"embed"
	"errors"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the frontend files rooted at dist.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

// Present reports whether the frontend was built before this executable.
func Present() bool {
	_, err := fs.Stat(dist, "dist/index.html")
	return !errors.Is(err, fs.ErrNotExist)
}
