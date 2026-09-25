package session

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/puhitaku/rtcv-ish/internal/corrupt/lists"
)

type ListInfo struct {
	Name      string `json:"name"`
	Precision int    `json:"precision"`
	Entries   int    `json:"entries"`
}

func listInfo(l *lists.List) ListInfo {
	return ListInfo{Name: l.Name(), Precision: l.Precision(), Entries: l.Len()}
}

var listNamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

// reloadListsLocked re-reads data/lists.
func (s *Session) reloadListsLocked() {
	reg := lists.NewRegistry()
	if err := reg.Load(s.listsDir()); err != nil {
		s.log.Warn("some lists could not be loaded", "dir", s.listsDir(), "err", err)
	}
	s.lists = reg
}

func (s *Session) Lists() []ListInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadListsLocked()
	out := []ListInfo{}
	for _, l := range s.lists.List() {
		out = append(out, listInfo(l))
	}
	return out
}

// UploadList stores a list file as data/lists/<name>.txt, replacing any
// list with that name.
func (s *Session) UploadList(name string, data []byte) (ListInfo, error) {
	name = strings.TrimSuffix(name, ".txt")
	if !listNamePattern.MatchString(name) {
		return ListInfo{}, errorf(KindInvalid, "invalid list name %q", name)
	}
	if err := checkListLines(data); err != nil {
		return ListInfo{}, err
	}
	l, err := lists.Parse(name, data)
	if err != nil {
		return ListInfo{}, errorf(KindInvalid, "%v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.WriteFile(filepath.Join(s.listsDir(), name+".txt"), data, 0o644); err != nil {
		return ListInfo{}, err
	}
	s.reloadListsLocked()
	s.changed(EventLists)
	return listInfo(l), nil
}

// checkListLines rejects entries with an odd number of hex digits.
func checkListLines(data []byte) error {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimPrefix(strings.TrimSpace(sc.Text()), "\ufeff")
		if len(line)%2 == 1 {
			return errorf(KindInvalid, "line %d: %q has an odd number of digits", n, line)
		}
	}
	return nil
}

func (s *Session) DeleteList(name string) error {
	if !listNamePattern.MatchString(name) {
		return errorf(KindNotFound, "no list %q", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(filepath.Join(s.listsDir(), name+".txt"))
	if errors.Is(err, os.ErrNotExist) {
		return errorf(KindNotFound, "no list %q", name)
	}
	if err != nil {
		return err
	}
	s.reloadListsLocked()
	s.changed(EventLists)
	return nil
}
