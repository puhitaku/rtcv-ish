//go:build embedweb

// Package webui holds the built web frontend (web/, built by Vite into
// internal/webui/dist).
package webui

import (
	"embed"
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
