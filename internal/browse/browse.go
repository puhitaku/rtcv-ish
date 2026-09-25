// Package browse lists directories on the core host for the ROM picker.
package browse

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
)

// Entry is one item of a Listing.
type Entry struct {
	Name string
	Path string
	Dir  bool
	Size int64
}

// Listing is the content of one directory.
type Listing struct {
	Path string
	// Parent is empty at a filesystem root.
	Parent  string
	Entries []Entry
}

var (
	ErrNotDir     = errors.New("not a directory")
	ErrNotFound   = errors.New("no such file or directory")
	ErrPermission = errors.New("permission denied")
)

// Error wraps one of ErrNotDir, ErrNotFound and ErrPermission with the path.
type Error struct {
	Path string
	Err  error
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %v", e.Path, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

// romExtensions are the file names melonDS opens (Window.cpp:
// NdsRomExtensions, ArchiveExtensions and Zstandard-compressed ROMs).
var (
	romExtensions     = []string{".nds", ".srl", ".dsi", ".ids"}
	archiveExtensions = []string{
		".zip", ".7z", ".rar", ".tar",
		".tar.gz", ".tgz", ".tar.xz", ".txz", ".tar.bz2", ".tbz2",
		".tar.lz4", ".tlz4", ".tar.zst", ".tzst", ".tar.z", ".taz",
		".tar.lz", ".tar.lzma", ".tlz", ".tar.lrz", ".tlrz", ".tar.lzo", ".tzo",
	}
)

// IsROM reports whether name has an extension melonDS accepts as a ROM.
func IsROM(name string) bool {
	n := strings.ToLower(name)
	n = strings.TrimSuffix(n, ".zst")
	for _, ext := range romExtensions {
		if strings.HasSuffix(n, ext) {
			return true
		}
	}
	n = strings.ToLower(name)
	for _, ext := range archiveExtensions {
		if strings.HasSuffix(n, ext) {
			return true
		}
	}
	return false
}

// List returns the subdirectories and ROM-like files of dir. Hidden
// (dot-prefixed) entries are omitted. Directories come first, each group
// sorted case-insensitively.
func List(dir string) (*Listing, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, &Error{Path: dir, Err: err}
	}
	abs = filepath.Clean(abs)
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, wrap(abs, err)
	}
	if !fi.IsDir() {
		return nil, &Error{Path: abs, Err: ErrNotDir}
	}
	des, err := os.ReadDir(abs)
	if err != nil {
		return nil, wrap(abs, err)
	}

	var dirs, files []Entry
	for _, de := range des {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		p := filepath.Join(abs, name)
		isDir := de.IsDir()
		var size int64
		if de.Type()&fs.ModeSymlink != 0 || !isDir {
			// Follow symlinks; skip dangling ones.
			info, err := os.Stat(p)
			if err != nil {
				continue
			}
			isDir = info.IsDir()
			size = info.Size()
		}
		switch {
		case isDir:
			dirs = append(dirs, Entry{Name: name, Path: p, Dir: true})
		case IsROM(name):
			files = append(files, Entry{Name: name, Path: p, Size: size})
		}
	}
	sortEntries(dirs)
	sortEntries(files)

	l := &Listing{Path: abs, Entries: append(dirs, files...)}
	if parent := filepath.Dir(abs); parent != abs {
		l.Parent = parent
	} else if runtime.GOOS == "windows" {
		l.Entries = append(drives(abs), l.Entries...)
	}
	return l, nil
}

func sortEntries(es []Entry) {
	slices.SortFunc(es, func(a, b Entry) int {
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
}

// drives lists the other drive roots on Windows so that a drive root can
// navigate to them.
func drives(current string) []Entry {
	var out []Entry
	for c := 'A'; c <= 'Z'; c++ {
		root := string(c) + `:\`
		if strings.EqualFold(root, current) {
			continue
		}
		if fi, err := os.Stat(root); err == nil && fi.IsDir() {
			out = append(out, Entry{Name: root, Path: root, Dir: true})
		}
	}
	return out
}

func wrap(path string, err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &Error{Path: path, Err: ErrNotFound}
	case errors.Is(err, syscall.ENOTDIR):
		// A file used as a directory component.
		return &Error{Path: path, Err: ErrNotDir}
	case errors.Is(err, fs.ErrPermission):
		return &Error{Path: path, Err: ErrPermission}
	default:
		return &Error{Path: path, Err: err}
	}
}
