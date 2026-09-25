package stockpile

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/puhitaku/rtcv-ish/internal/corrupt"
)

func newState(t *testing.T, s *Store, rng *rand.Rand, data string) *StashKey {
	t.Helper()
	key := s.NewKey(rng)
	if err := s.WriteState(key, []byte(data)); err != nil {
		t.Fatal(err)
	}
	return &StashKey{Key: key, ParentKey: key, Alias: key, CreatedAt: time.Now()}
}

func exists(s *Store, key string) bool {
	_, err := os.Stat(s.statePath(key))
	return err == nil
}

func TestSlotsPersistAndCollect(t *testing.T) {
	dir := t.TempDir()
	rng := rand.New(rand.NewPCG(1, 0))
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	k := newState(t, s, rng, "slot")
	if err := s.SetSlot(3, k); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSlotLabel(3, "boss"); err != nil {
		t.Fatal(err)
	}
	child := NewKey(s.NewKey(rng), k, []string{"VRAM"}, &corrupt.Layer{}, time.Now())
	s.AddStash(child)
	stray := newState(t, s, rng, "stray")

	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	sl, _ := s2.Slot(3)
	if sl.Label != "boss" || sl.Key == nil || sl.Key.Key != k.Key {
		t.Fatalf("reopened slot = %+v", sl)
	}
	if data, err := s2.ReadState(k.Key); err != nil || string(data) != "slot" {
		t.Errorf("slot state = %q, %v", data, err)
	}
	if exists(s2, stray.Key) {
		t.Error("unreferenced state survived Open")
	}

	// Replacing the slot keeps the blob while the stash references it.
	if err := s.SetSlot(3, newState(t, s, rng, "new")); err != nil {
		t.Fatal(err)
	}
	if !exists(s, k.Key) {
		t.Error("state referenced by a stash key was deleted")
	}
	s.ClearStash()
	if exists(s, k.Key) {
		t.Error("unreferenced state was kept")
	}
	if _, err := s.SlotKey(4); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty slot: %v", err)
	}
	if _, err := s.Slot(51); !errors.Is(err, ErrInvalid) {
		t.Errorf("slot 51: %v", err)
	}
}

func TestBackupRing(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewPCG(1, 0))
	var keys []*StashKey
	for i := range 4 {
		k := newState(t, s, rng, string(rune('a'+i)))
		keys = append(keys, k)
		s.PushBackup(k, 3)
	}
	if n := s.BackupCount(); n != 3 {
		t.Fatalf("%d backups, want 3", n)
	}
	if exists(s, keys[0].Key) {
		t.Error("dropped backup's state was kept")
	}
	if got := s.LatestBackup(); got != keys[3] {
		t.Errorf("latest = %v", got)
	}
	s.DropLatestBackup()
	if exists(s, keys[3].Key) || s.LatestBackup() != keys[2] {
		t.Error("DropLatestBackup")
	}
}

func TestExportImport(t *testing.T) {
	src, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	listsDir := t.TempDir()
	os.WriteFile(filepath.Join(listsDir, "lim.txt"), []byte("ABAB\n"), 0o644)
	rng := rand.New(rand.NewPCG(1, 0))
	parent := newState(t, src, rng, "state")
	u := corrupt.NewUnit()
	u.Domain, u.Precision, u.Value, u.LimiterList = "VRAM", 1, corrupt.Hex{1}, "lim"
	var keys []string
	for range 2 {
		k := NewKey(src.NewKey(rng), parent, nil, &corrupt.Layer{Units: []*corrupt.Unit{u}}, time.Now())
		src.AddStash(k)
		if _, err := src.ToStockpile(k.Key); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k.Key)
	}
	var buf bytes.Buffer
	if err := src.Export(&buf, listsDir); err != nil {
		t.Fatal(err)
	}

	dst, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dstLists := t.TempDir()
	added, err := dst.Import(buf.Bytes(), false, dstLists)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(added, []string{"lim"}) {
		t.Errorf("added lists %v", added)
	}
	var got []string
	for _, k := range dst.Stockpile() {
		got = append(got, k.Key)
		if k.UnitCount != 1 || k.Layer == nil {
			t.Errorf("imported key %+v", k)
		}
	}
	if !slices.Equal(got, keys) {
		t.Errorf("imported %v, want %v", got, keys)
	}
	if data, err := dst.ReadState(parent.Key); err != nil || string(data) != "state" {
		t.Errorf("imported state = %q, %v", data, err)
	}
	if err := dst.RemoveStockpile(keys[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := dst.Import(buf.Bytes(), true, dstLists); err != nil {
		t.Fatal(err)
	}
	got = got[:0]
	for _, k := range dst.Stockpile() {
		got = append(got, k.Key)
	}
	if want := []string{keys[1], keys[0]}; !slices.Equal(got, want) {
		t.Errorf("merged %v, want %v", got, want)
	}
	if _, err := dst.Import([]byte("junk"), false, dstLists); !errors.Is(err, ErrInvalid) {
		t.Errorf("junk import: %v", err)
	}
}
