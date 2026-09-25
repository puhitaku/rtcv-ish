package server_test

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

func TestBrowse(t *testing.T) {
	e := newEnv(t, envOptions{noConnect: true})
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	rom := filepath.Join(root, "test.nds")
	for _, f := range []string{rom, filepath.Join(root, "readme.txt")} {
		if err := os.WriteFile(f, []byte("rom"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	r, err := e.c.BrowseWithResponse(e.ctx, &gen.BrowseParams{Path: ptr(filepath.Join(root, "sub", ".."))})
	expectStatus(t, r, err, http.StatusOK)
	l := *r.JSON200
	if l.Path != root || l.Parent == nil || *l.Parent != filepath.Dir(root) {
		t.Errorf("path = %q, parent = %v", l.Path, l.Parent)
	}
	want := []gen.DirEntry{
		{Name: "sub", Path: filepath.Join(root, "sub"), Dir: true},
		{Name: "test.nds", Path: rom, Size: 3},
	}
	if len(l.Entries) != len(want) || l.Entries[0] != want[0] || l.Entries[1] != want[1] {
		t.Errorf("entries = %+v, want %+v", l.Entries, want)
	}

	// Default: the home directory (or the data dir).
	r, err = e.c.BrowseWithResponse(e.ctx, &gen.BrowseParams{})
	expectStatus(t, r, err, http.StatusOK)
	wantDefault := e.dataDir
	if home, err := os.UserHomeDir(); err == nil {
		wantDefault = home
	}
	if r.JSON200.Path != filepath.Clean(wantDefault) {
		t.Errorf("default path = %q, want %q", r.JSON200.Path, wantDefault)
	}

	// The filesystem root has no parent.
	top := string(filepath.Separator)
	if runtime.GOOS == "windows" {
		top = filepath.VolumeName(root) + `\`
	}
	r, err = e.c.BrowseWithResponse(e.ctx, &gen.BrowseParams{Path: &top})
	expectStatus(t, r, err, http.StatusOK)
	if r.JSON200.Parent != nil {
		t.Errorf("root parent = %q, want null", *r.JSON200.Parent)
	}
	if body := string(r.Body); !strings.Contains(body, `"parent":null`) {
		t.Errorf("root parent is not JSON null: %s", body)
	}

	r, err = e.c.BrowseWithResponse(e.ctx, &gen.BrowseParams{Path: &rom})
	expectError(t, r, err, http.StatusBadRequest, "INVALID_ARGUMENT")
	r, err = e.c.BrowseWithResponse(e.ctx, &gen.BrowseParams{Path: ptr(filepath.Join(root, "missing"))})
	expectError(t, r, err, http.StatusNotFound, "NOT_FOUND")

	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		locked := filepath.Join(root, "locked")
		if err := os.Mkdir(locked, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(locked, 0o755) })
		r, err = e.c.BrowseWithResponse(e.ctx, &gen.BrowseParams{Path: &locked})
		expectError(t, r, err, http.StatusForbidden, "PERMISSION_DENIED")
	}
}
