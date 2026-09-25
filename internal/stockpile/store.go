package stockpile

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Slots is the number of savestate slots.
const Slots = 50

// Slot is a savestate slot. Key is the savestate key, nil when empty.
type Slot struct {
	Label string    `json:"label"`
	Key   *StashKey `json:"key,omitempty"`
}

// Store holds the stash history, the stockpile, the savestate slots and
// the game protection backups, and the savestate blobs they reference. It
// is not safe for concurrent use.
type Store struct {
	dir       string
	stash     []*StashKey
	stockpile []*StashKey
	slots     [Slots]Slot
	backups   []*StashKey
}

// Open opens the store in dataDir. Savestate slots are read from
// slots.json; savestate blobs no slot references are removed.
func Open(dataDir string) (*Store, error) {
	s := &Store{dir: dataDir}
	if err := os.MkdirAll(s.statesDir(), 0o755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.slotsPath())
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		var slots []Slot
		if err := json.Unmarshal(data, &slots); err != nil {
			return nil, fmt.Errorf("%s: %w", s.slotsPath(), err)
		}
		for i, sl := range slots {
			if i < Slots && (sl.Key == nil || sl.Key.validate() == nil) {
				s.slots[i] = sl
			}
		}
	}
	s.collect()
	return s, nil
}

func (s *Store) statesDir() string { return filepath.Join(s.dir, "states") }
func (s *Store) slotsPath() string { return filepath.Join(s.dir, "slots.json") }

func (s *Store) statePath(key string) string {
	return filepath.Join(s.statesDir(), key+".state")
}

// NewKey returns a random key that is not in use.
func (s *Store) NewKey(rng *rand.Rand) string {
	for {
		k := randomKey(rng)
		if !s.referenced(k) && s.findAny(k) == nil {
			if _, err := os.Stat(s.statePath(k)); errors.Is(err, os.ErrNotExist) {
				return k
			}
		}
	}
}

func (s *Store) WriteState(key string, data []byte) error {
	if !keyPattern.MatchString(key) {
		return fmt.Errorf("%w: key %q", ErrInvalid, key)
	}
	tmp := s.statePath(key) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.statePath(key))
}

func (s *Store) ReadState(key string) ([]byte, error) {
	if !keyPattern.MatchString(key) {
		return nil, fmt.Errorf("%w: savestate %q", ErrNotFound, key)
	}
	data, err := os.ReadFile(s.statePath(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: savestate %s", ErrNotFound, key)
	}
	return data, err
}

func (s *Store) all() []*StashKey {
	out := slices.Concat(s.stash, s.stockpile, s.backups)
	for _, sl := range s.slots {
		if sl.Key != nil {
			out = append(out, sl.Key)
		}
	}
	return out
}

// referenced reports whether a savestate blob is still used.
func (s *Store) referenced(parent string) bool {
	return slices.ContainsFunc(s.all(), func(k *StashKey) bool { return k.ParentKey == parent })
}

// release deletes the blobs of the given savestate keys that nothing
// references any more.
func (s *Store) release(parents ...string) {
	for _, p := range parents {
		if p != "" && !s.referenced(p) {
			os.Remove(s.statePath(p))
		}
	}
}

func parents(keys []*StashKey) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = k.ParentKey
	}
	return out
}

// collect removes unreferenced savestate blobs.
func (s *Store) collect() {
	entries, err := os.ReadDir(s.statesDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		key, ok := strings.CutSuffix(name, ".state")
		if !ok {
			if strings.HasSuffix(name, ".tmp") {
				os.Remove(filepath.Join(s.statesDir(), name))
			}
			continue
		}
		if !s.referenced(key) {
			os.Remove(filepath.Join(s.statesDir(), name))
		}
	}
}

func (s *Store) findAny(key string) *StashKey {
	for _, k := range s.all() {
		if k.Key == key {
			return k
		}
	}
	return nil
}

func find(list []*StashKey, key string) (*StashKey, int, error) {
	i := slices.IndexFunc(list, func(k *StashKey) bool { return k.Key == key })
	if i < 0 {
		return nil, -1, fmt.Errorf("%w: key %s", ErrNotFound, key)
	}
	return list[i], i, nil
}

// ---- Stash history ----

func (s *Store) Stash() []*StashKey { return s.stash }

func (s *Store) AddStash(k *StashKey) { s.stash = append(s.stash, k) }

func (s *Store) FindStash(key string) (*StashKey, error) {
	k, _, err := find(s.stash, key)
	return k, err
}

func (s *Store) RemoveStash(key string) error {
	k, i, err := find(s.stash, key)
	if err != nil {
		return err
	}
	s.stash = slices.Delete(s.stash, i, i+1)
	s.release(k.ParentKey)
	return nil
}

func (s *Store) ClearStash() {
	old := s.stash
	s.stash = nil
	s.release(parents(old)...)
}

// ---- Stockpile ----

func (s *Store) Stockpile() []*StashKey { return s.stockpile }

func (s *Store) FindStockpile(key string) (*StashKey, error) {
	k, _, err := find(s.stockpile, key)
	return k, err
}

// Find looks a key up in the stash history, then in the stockpile.
func (s *Store) Find(key string) (*StashKey, error) {
	if k, err := s.FindStash(key); err == nil {
		return k, nil
	}
	return s.FindStockpile(key)
}

// ToStockpile moves a key from the stash history to the end of the
// stockpile.
func (s *Store) ToStockpile(key string) (*StashKey, error) {
	k, i, err := find(s.stash, key)
	if err != nil {
		return nil, err
	}
	s.stash = slices.Delete(s.stash, i, i+1)
	s.stockpile = append(s.stockpile, k)
	return k, nil
}

func (s *Store) RemoveStockpile(key string) error {
	k, i, err := find(s.stockpile, key)
	if err != nil {
		return err
	}
	s.stockpile = slices.Delete(s.stockpile, i, i+1)
	s.release(k.ParentKey)
	return nil
}

func (s *Store) ClearStockpile() {
	old := s.stockpile
	s.stockpile = nil
	s.release(parents(old)...)
}

// Reorder sets the stockpile order. keys must be a permutation of the
// stockpile's keys.
func (s *Store) Reorder(keys []string) error {
	if len(keys) != len(s.stockpile) {
		return fmt.Errorf("%w: %d keys given, the stockpile has %d", ErrInvalid, len(keys), len(s.stockpile))
	}
	out := make([]*StashKey, 0, len(keys))
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		k, _, err := find(s.stockpile, key)
		if err != nil || seen[key] {
			return fmt.Errorf("%w: keys are not a permutation of the stockpile", ErrInvalid)
		}
		seen[key] = true
		out = append(out, k)
	}
	s.stockpile = out
	return nil
}

// ---- Savestate slots ----

func checkSlot(n int) error {
	if n < 1 || n > Slots {
		return fmt.Errorf("%w: slot %d is not in 1..%d", ErrInvalid, n, Slots)
	}
	return nil
}

// Slot returns slot n (1-based).
func (s *Store) Slot(n int) (Slot, error) {
	if err := checkSlot(n); err != nil {
		return Slot{}, err
	}
	return s.slots[n-1], nil
}

// SlotKey returns the savestate key in slot n or ErrNotFound.
func (s *Store) SlotKey(n int) (*StashKey, error) {
	sl, err := s.Slot(n)
	if err != nil {
		return nil, err
	}
	if sl.Key == nil {
		return nil, fmt.Errorf("%w: slot %d is empty", ErrNotFound, n)
	}
	return sl.Key, nil
}

func (s *Store) SlotList() []Slot { return slices.Clone(s.slots[:]) }

// SetSlot stores a savestate key in slot n, keeping the label.
func (s *Store) SetSlot(n int, k *StashKey) error {
	if err := checkSlot(n); err != nil {
		return err
	}
	old := s.slots[n-1].Key
	s.slots[n-1].Key = k
	if old != nil {
		s.release(old.ParentKey)
	}
	return s.saveSlots()
}

func (s *Store) SetSlotLabel(n int, label string) error {
	if err := checkSlot(n); err != nil {
		return err
	}
	s.slots[n-1].Label = label
	return s.saveSlots()
}

func (s *Store) ClearSlot(n int) error {
	if err := checkSlot(n); err != nil {
		return err
	}
	old := s.slots[n-1].Key
	s.slots[n-1] = Slot{}
	if old != nil {
		s.release(old.ParentKey)
	}
	return s.saveSlots()
}

func (s *Store) saveSlots() error {
	data, err := json.MarshalIndent(s.slots[:], "", "  ")
	if err != nil {
		return err
	}
	tmp := s.slotsPath() + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.slotsPath())
}

// ---- Game protection ----

func (s *Store) BackupCount() int { return len(s.backups) }

// PushBackup adds a backup and drops the oldest ones beyond keep.
func (s *Store) PushBackup(k *StashKey, keep int) {
	s.backups = append(s.backups, k)
	if n := len(s.backups) - max(keep, 1); n > 0 {
		dropped := slices.Clone(s.backups[:n])
		s.backups = slices.Delete(s.backups, 0, n)
		s.release(parents(dropped)...)
	}
}

// LatestBackup returns the most recent backup or nil.
func (s *Store) LatestBackup() *StashKey {
	if len(s.backups) == 0 {
		return nil
	}
	return s.backups[len(s.backups)-1]
}

// DropLatestBackup removes the most recent backup.
func (s *Store) DropLatestBackup() {
	if k := s.LatestBackup(); k != nil {
		s.backups = s.backups[:len(s.backups)-1]
		s.release(k.ParentKey)
	}
}

func (s *Store) ClearBackups() {
	old := s.backups
	s.backups = nil
	s.release(parents(old)...)
}
