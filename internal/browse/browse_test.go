package browse_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/puhitaku/rtcv-ish/internal/browse"
)

func mkfile(t *testing.T, p string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestList(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"b-dir", "A-dir", ".hidden-dir", "c-dir"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mkfile(t, filepath.Join(root, "zelda.nds"), 3)
	mkfile(t, filepath.Join(root, "Alpha.NDS"), 1)
	mkfile(t, filepath.Join(root, "game.nds.zst"), 1)
	mkfile(t, filepath.Join(root, "pack.7z"), 1)
	mkfile(t, filepath.Join(root, "dsi.srl"), 1)
	mkfile(t, filepath.Join(root, "notes.txt"), 1)
	mkfile(t, filepath.Join(root, "save.sav"), 1)
	mkfile(t, filepath.Join(root, "other.zst"), 1)
	mkfile(t, filepath.Join(root, ".hidden.nds"), 1)

	// Relative and unclean paths are made absolute and cleaned.
	l, err := browse.List(filepath.Join(root, "A-dir", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != root {
		t.Errorf("Path = %q, want %q", l.Path, root)
	}
	if l.Parent != filepath.Dir(root) {
		t.Errorf("Parent = %q, want %q", l.Parent, filepath.Dir(root))
	}
	want := []struct {
		name string
		dir  bool
		size int64
	}{
		{"A-dir", true, 0},
		{"b-dir", true, 0},
		{"c-dir", true, 0},
		{"Alpha.NDS", false, 1},
		{"dsi.srl", false, 1},
		{"game.nds.zst", false, 1},
		{"pack.7z", false, 1},
		{"zelda.nds", false, 3},
	}
	if len(l.Entries) != len(want) {
		t.Fatalf("entries = %+v, want %d entries", l.Entries, len(want))
	}
	for i, w := range want {
		e := l.Entries[i]
		if e.Name != w.name || e.Dir != w.dir || e.Size != w.size || e.Path != filepath.Join(root, w.name) {
			t.Errorf("entry %d = %+v, want %+v", i, e, w)
		}
	}
}

func TestListSymlink(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	_ = os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "dangling.nds"))
	l, err := browse.List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 1 || !l.Entries[0].Dir || l.Entries[0].Name != "link" {
		t.Errorf("entries = %+v, want the link as a directory", l.Entries)
	}
}

func TestListRoot(t *testing.T) {
	root := string(filepath.Separator)
	if runtime.GOOS == "windows" {
		root = filepath.VolumeName(os.TempDir()) + `\`
	}
	l, err := browse.List(root)
	if err != nil {
		t.Fatal(err)
	}
	if l.Parent != "" {
		t.Errorf("Parent = %q, want empty at the root", l.Parent)
	}
	if l.Path != root {
		t.Errorf("Path = %q, want %q", l.Path, root)
	}
}

func TestListErrors(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "rom.nds")
	mkfile(t, file, 1)

	for _, tc := range []struct {
		path string
		want error
	}{
		{filepath.Join(root, "missing"), browse.ErrNotFound},
		{file, browse.ErrNotDir},
		{filepath.Join(file, "sub"), browse.ErrNotDir},
	} {
		_, err := browse.List(tc.path)
		if !errors.Is(err, tc.want) {
			t.Errorf("List(%q) = %v, want %v", tc.path, err, tc.want)
		}
	}

	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		locked := filepath.Join(root, "locked")
		if err := os.Mkdir(locked, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(locked, 0o755) })
		if _, err := browse.List(locked); !errors.Is(err, browse.ErrPermission) {
			t.Errorf("List(locked) = %v, want %v", err, browse.ErrPermission)
		}
	}
}

func TestIsROM(t *testing.T) {
	for name, want := range map[string]bool{
		"a.nds": true, "a.DSI": true, "a.ids": true, "a.srl": true,
		"a.nds.zst": true, "a.zip": true, "a.rar": true, "a.tar.gz": true,
		"a.zst": false, "a.gba": false, "a.txt": false, "nds": false,
	} {
		if got := browse.IsROM(name); got != want {
			t.Errorf("IsROM(%q) = %v, want %v", name, got, want)
		}
	}
}
