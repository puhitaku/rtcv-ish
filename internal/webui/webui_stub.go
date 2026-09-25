//go:build !embedweb

// Package webui holds the built web frontend. This build has no frontend
// embedded; build with -tags embedweb after `make web`.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed stub
var stub embed.FS

// FS returns a placeholder index.html.
func FS() fs.FS {
	sub, err := fs.Sub(stub, "stub")
	if err != nil {
		panic(err)
	}
	return sub
}
