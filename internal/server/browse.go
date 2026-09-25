package server

import (
	"context"
	"errors"
	"net/http"
	"os"

	"github.com/puhitaku/rtcv-ish/internal/browse"
	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

// Browse lists a directory on the core host for the ROM picker.
func (a *api) Browse(ctx context.Context, r gen.BrowseRequestObject) (gen.BrowseResponseObject, error) {
	dir := ""
	if r.Params.Path != nil {
		dir = *r.Params.Path
	}
	if dir == "" {
		dir = a.s.cfg.DataDir
		if home, err := os.UserHomeDir(); err == nil {
			dir = home
		}
	}
	l, err := browse.List(dir)
	if err != nil {
		return nil, browseError(err)
	}
	out := gen.DirListing{Path: l.Path, Entries: make([]gen.DirEntry, len(l.Entries))}
	if l.Parent != "" {
		out.Parent = &l.Parent
	}
	for i, e := range l.Entries {
		out.Entries[i] = gen.DirEntry{Name: e.Name, Path: e.Path, Dir: e.Dir, Size: e.Size}
	}
	return gen.Browse200JSONResponse(out), nil
}

func browseError(err error) error {
	switch {
	case errors.Is(err, browse.ErrNotDir):
		return newError(http.StatusBadRequest, CodeInvalidArgument, "%v", err)
	case errors.Is(err, browse.ErrNotFound):
		return newError(http.StatusNotFound, CodeNotFound, "%v", err)
	case errors.Is(err, browse.ErrPermission):
		return newError(http.StatusForbidden, CodePermissionDenied, "%v", err)
	default:
		return err
	}
}
