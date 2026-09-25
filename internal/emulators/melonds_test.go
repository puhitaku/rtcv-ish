package emulators

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func touch(t *testing.T, p string, mod time.Time) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
	return p
}

// newRepo creates a fake repository root with go.mod and emulators/melonds.
func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	touch(t, filepath.Join(root, "go.mod"), time.Now())
	if err := os.MkdirAll(filepath.Join(root, "emulators", "melonds"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func find(t *testing.T, goos, cwd, exeDir, override string) (string, string) {
	t.Helper()
	p, src, err := FindMelonDS(goos, cwd, exeDir, override, nil)
	if err != nil {
		t.Fatalf("FindMelonDS: %v", err)
	}
	return p, src
}

func TestFindMelonDSOverride(t *testing.T) {
	dir := t.TempDir()
	exe := touch(t, filepath.Join(dir, "melonDS"), time.Now())
	app := filepath.Join(dir, "melonDS.app")
	inner := touch(t, filepath.Join(app, "Contents", "MacOS", "melonDS"), time.Now())

	// The override wins over a bundled executable.
	exeDir := t.TempDir()
	touch(t, filepath.Join(exeDir, "emulators", "melonds", "melonDS"), time.Now())

	for _, tc := range []struct{ in, want string }{
		{exe, exe},
		{app, inner},
		{app + "/", inner},
	} {
		p, src := find(t, "linux", "", exeDir, tc.in)
		if p != tc.want || src != SourceOverride {
			t.Errorf("override %s = %s (%s), want %s", tc.in, p, src, tc.want)
		}
	}

	for _, bad := range []string{filepath.Join(dir, "missing"), t.TempDir()} {
		if _, _, err := FindMelonDS("darwin", "", "", bad, nil); err == nil {
			t.Errorf("override %s: no error", bad)
		}
	}
}

func TestFindMelonDSBundled(t *testing.T) {
	for goos, rel := range map[string]string{
		"darwin":  "melonDS.app/Contents/MacOS/melonDS",
		"windows": "melonDS.exe",
		"linux":   "melonDS-x86_64.AppImage",
	} {
		exeDir := t.TempDir()
		want := touch(t, filepath.Join(exeDir, "emulators", "melonds", filepath.FromSlash(rel)), time.Now())
		// A development build is ignored when a bundle exists.
		repo := newRepo(t)
		touch(t, filepath.Join(repo, "emulators", "melonds", "build", "local", "melonDS"), time.Now())
		p, src := find(t, goos, repo, exeDir, "")
		if p != want || src != SourceBundled {
			t.Errorf("%s: got %s (%s), want %s", goos, p, src, want)
		}
	}
}

func TestFindMelonDSDevBuild(t *testing.T) {
	old := time.Now().Add(-time.Hour)
	for goos, rel := range map[string]string{
		"darwin":  "melonDS.app/Contents/MacOS/melonDS",
		"windows": "melonDS.exe",
		"linux":   "melonDS",
	} {
		repo := newRepo(t)
		build := filepath.Join(repo, "emulators", "melonds", "build")
		touch(t, filepath.Join(build, "release", filepath.FromSlash(rel)), old)
		want := touch(t, filepath.Join(build, "local", filepath.FromSlash(rel)), time.Now())
		touch(t, filepath.Join(build, "release-mac-arm64", filepath.FromSlash(rel)), old.Add(-time.Hour))
		sub := filepath.Join(repo, "cmd", "rtcv-ish")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}

		// Found from cwd below the root, with an unrelated exe dir.
		p, src := find(t, goos, sub, t.TempDir(), "")
		if p != want || src != SourceDevBuild {
			t.Errorf("%s via cwd: got %s (%s), want %s", goos, p, src, want)
		}
		// Found from the exe dir (bin/) when cwd is elsewhere.
		p, src = find(t, goos, t.TempDir(), filepath.Join(repo, "bin"), "")
		if p != want || src != SourceDevBuild {
			t.Errorf("%s via exe dir: got %s (%s), want %s", goos, p, src, want)
		}
	}
}

func TestFindMelonDSNone(t *testing.T) {
	exeDir := t.TempDir()
	// A go.mod without emulators/melonds is not the repository root.
	touch(t, filepath.Join(exeDir, "go.mod"), time.Now())
	p, src := find(t, "darwin", exeDir, exeDir, "")
	want := filepath.Join(exeDir, "emulators", "melonds", "melonDS.app", "Contents", "MacOS", "melonDS")
	if p != want || src != SourceNone {
		t.Errorf("got %s (%s), want %s", p, src, want)
	}
	if p, src := find(t, "linux", "", "", ""); p != "" || src != SourceNone {
		t.Errorf("no dirs: got %s (%s)", p, src)
	}
}
