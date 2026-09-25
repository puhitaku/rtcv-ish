// Package emulators locates emulator executables the core can launch.
package emulators

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Sources of a melonDS path, in priority order.
const (
	SourceOverride = "override"
	SourceBundled  = "bundled"
	SourceDevBuild = "development build"
	SourceNone     = "none"
)

// FindMelonDS returns the melonDS executable to launch and where it came
// from. goos selects the platform layout (runtime.GOOS in production).
//
// The first hit wins:
//  1. override, an explicit executable or .app bundle; if it is set but
//     missing, FindMelonDS returns an error.
//  2. The bundle in <exeDir>/emulators/melonds.
//  3. The most recently modified build in emulators/melonds/build/*/ of
//     the repository containing cwd or exeDir.
//
// When nothing is found it returns the preferred bundled path with
// SourceNone, so the emulator is reported as not present.
func FindMelonDS(goos, cwd, exeDir, override string, log *slog.Logger) (path, source string, err error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if override != "" {
		p, err := resolveOverride(override)
		if err != nil {
			return "", "", err
		}
		return p, SourceOverride, nil
	}

	var bundled []string
	if exeDir != "" {
		bundled = bundleCandidates(goos, filepath.Join(exeDir, "emulators", "melonds"))
		for _, c := range bundled {
			log.Debug("looking for melonDS", "source", SourceBundled, "path", c)
			if isFile(c) {
				return c, SourceBundled, nil
			}
		}
	}

	for _, root := range repoRoots(cwd, exeDir) {
		if p := newestBuild(goos, filepath.Join(root, "emulators", "melonds", "build"), log); p != "" {
			return p, SourceDevBuild, nil
		}
	}

	if len(bundled) == 0 {
		return "", SourceNone, nil
	}
	return bundled[0], SourceNone, nil
}

func resolveOverride(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("melonDS path %s: %w", p, err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("melonDS path %s: %w", p, err)
	}
	if !st.IsDir() {
		return abs, nil
	}
	inner := filepath.Join(abs, "Contents", "MacOS", "melonDS")
	if isFile(inner) {
		return inner, nil
	}
	return "", fmt.Errorf("melonDS path %s is a directory without Contents/MacOS/melonDS", p)
}

// bundleCandidates lists the release-layout executables in dir, preferred
// first.
func bundleCandidates(goos, dir string) []string {
	var cs []string
	switch goos {
	case "darwin":
		cs = []string{filepath.Join(dir, "melonDS.app", "Contents", "MacOS", "melonDS")}
	case "windows":
		cs = []string{filepath.Join(dir, "melonDS.exe")}
	case "linux":
		cs, _ = filepath.Glob(filepath.Join(dir, "melonDS*.AppImage"))
	}
	return append(cs, filepath.Join(dir, "melonDS"))
}

func buildExecutable(goos, dir string) string {
	switch goos {
	case "darwin":
		return filepath.Join(dir, "melonDS.app", "Contents", "MacOS", "melonDS")
	case "windows":
		return filepath.Join(dir, "melonDS.exe")
	default:
		return filepath.Join(dir, "melonDS")
	}
}

// repoRoots returns the distinct repository roots above cwd and exeDir.
func repoRoots(dirs ...string) []string {
	var roots []string
	seen := map[string]bool{}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		root, ok := findRepoRoot(d)
		if ok && !seen[root] {
			seen[root] = true
			roots = append(roots, root)
		}
	}
	return roots
}

func findRepoRoot(dir string) (string, bool) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for {
		if isFile(filepath.Join(dir, "go.mod")) && isDir(filepath.Join(dir, "emulators", "melonds")) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func newestBuild(goos, buildDir string, log *slog.Logger) string {
	entries, err := os.ReadDir(buildDir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Debug("cannot read melonDS build directory", "path", buildDir, "err", err)
		}
		return ""
	}
	var best string
	var bestMod int64
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		c := buildExecutable(goos, filepath.Join(buildDir, e.Name()))
		log.Debug("looking for melonDS", "source", SourceDevBuild, "path", c)
		st, err := os.Stat(c)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		if m := st.ModTime().UnixNano(); best == "" || m > bestMod {
			best, bestMod = c, m
		}
	}
	return best
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
