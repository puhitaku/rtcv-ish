package stockpile

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// FormatVersion is written to stockpile.json.
const FormatVersion = 1

// maxEntrySize bounds a single file read from an .sks.
const maxEntrySize = 1 << 30

type sksFile struct {
	Version int         `json:"version"`
	Keys    []*StashKey `json:"keys"`
}

var listNamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

// Export writes the stockpile as an .sks zip: stockpile.json, the
// savestate blobs as states/<parentKey>.state and the limiter lists the
// units reference as lists/<name>.txt (read from listsDir).
func (s *Store) Export(w io.Writer, listsDir string) error {
	zw := zip.NewWriter(w)
	keys := s.stockpile
	if keys == nil {
		keys = []*StashKey{}
	}
	data, err := json.MarshalIndent(sksFile{Version: FormatVersion, Keys: keys}, "", "  ")
	if err != nil {
		return err
	}
	if err := writeEntry(zw, "stockpile.json", data); err != nil {
		return err
	}
	var states, lists []string
	for _, k := range keys {
		if !slices.Contains(states, k.ParentKey) {
			states = append(states, k.ParentKey)
		}
		if k.Layer == nil {
			continue
		}
		for _, u := range k.Layer.Units {
			if u.LimiterList != "" && listNamePattern.MatchString(u.LimiterList) && !slices.Contains(lists, u.LimiterList) {
				lists = append(lists, u.LimiterList)
			}
		}
	}
	for _, p := range states {
		data, err := s.ReadState(p)
		if err != nil {
			return err
		}
		if err := writeEntry(zw, "states/"+p+".state", data); err != nil {
			return err
		}
	}
	for _, name := range lists {
		data, err := os.ReadFile(filepath.Join(listsDir, name+".txt"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err := writeEntry(zw, "lists/"+name+".txt", data); err != nil {
			return err
		}
	}
	return zw.Close()
}

func writeEntry(zw *zip.Writer, name string, data []byte) error {
	f, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return err
}

// Import reads an .sks. It replaces the stockpile, or with merge appends
// the keys that are not in it yet. Savestate blobs are stored and lists
// missing from listsDir are written there; their names are returned.
func (s *Store) Import(data []byte, merge bool, listsDir string) ([]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: not an .sks file: %v", ErrInvalid, err)
	}
	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		files[f.Name] = f
	}
	read := func(name string) ([]byte, error) {
		f, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("%w: .sks has no %s", ErrInvalid, name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrInvalid, name, err)
		}
		defer rc.Close()
		b, err := io.ReadAll(io.LimitReader(rc, maxEntrySize))
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrInvalid, name, err)
		}
		return b, nil
	}

	raw, err := read("stockpile.json")
	if err != nil {
		return nil, err
	}
	var sf sksFile
	if err := json.Unmarshal(raw, &sf); err != nil {
		return nil, fmt.Errorf("%w: stockpile.json: %v", ErrInvalid, err)
	}
	if sf.Version > FormatVersion {
		return nil, fmt.Errorf("%w: stockpile.json version %d is newer than %d", ErrInvalid, sf.Version, FormatVersion)
	}
	var keys []*StashKey
	seen := map[string]bool{}
	for _, k := range sf.Keys {
		if k == nil {
			continue
		}
		if err := k.validate(); err != nil {
			return nil, err
		}
		if seen[k.Key] {
			return nil, fmt.Errorf("%w: duplicate key %s", ErrInvalid, k.Key)
		}
		seen[k.Key] = true
		if k.Alias == "" {
			k.Alias = k.Key
		}
		k.SetLayer(k.Layer)
		keys = append(keys, k)
	}
	if merge {
		keys = slices.DeleteFunc(keys, func(k *StashKey) bool {
			_, _, err := find(s.stockpile, k.Key)
			return err == nil
		})
	}

	states := map[string][]byte{}
	for _, k := range keys {
		if _, ok := states[k.ParentKey]; ok {
			continue
		}
		b, err := read("states/" + k.ParentKey + ".state")
		if err != nil {
			return nil, err
		}
		states[k.ParentKey] = b
	}
	lists := map[string][]byte{}
	for name := range files {
		dir, file := filepath.Split(filepath.ToSlash(name))
		base, ok := strings.CutSuffix(file, ".txt")
		if dir != "lists/" || !ok || !listNamePattern.MatchString(base) {
			continue
		}
		if _, err := os.Stat(filepath.Join(listsDir, file)); err == nil {
			continue
		}
		b, err := read(name)
		if err != nil {
			return nil, err
		}
		lists[base] = b
	}

	for p, b := range states {
		if err := s.WriteState(p, b); err != nil {
			return nil, err
		}
	}
	var added []string
	if len(lists) > 0 {
		if err := os.MkdirAll(listsDir, 0o755); err != nil {
			return nil, err
		}
	}
	for name, b := range lists {
		if err := os.WriteFile(filepath.Join(listsDir, name+".txt"), b, 0o644); err != nil {
			return nil, err
		}
		added = append(added, name)
	}
	slices.Sort(added)

	if merge {
		s.stockpile = append(s.stockpile, keys...)
	} else {
		old := s.stockpile
		s.stockpile = keys
		s.release(parents(old)...)
	}
	return added, nil
}
