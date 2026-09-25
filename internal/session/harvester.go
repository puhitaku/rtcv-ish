package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"time"

	"github.com/puhitaku/rtcv-ish/internal/corrupt"
	"github.com/puhitaku/rtcv-ish/internal/emu"
	"github.com/puhitaku/rtcv-ish/internal/stockpile"
)

type SlotInfo struct {
	Slot  int                 `json:"slot"`
	Key   *string             `json:"key,omitempty"`
	Label string              `json:"label"`
	Game  *stockpile.GameInfo `json:"game,omitempty"`
}

func slotInfo(n int, sl stockpile.Slot) SlotInfo {
	si := SlotInfo{Slot: n, Label: sl.Label}
	if sl.Key != nil {
		key, game := sl.Key.Key, sl.Key.Game
		si.Key, si.Game = &key, &game
	}
	return si
}

func summaries(keys []*stockpile.StashKey) []*stockpile.StashKey {
	out := make([]*stockpile.StashKey, len(keys))
	for i, k := range keys {
		out[i] = k.Summary()
	}
	return out
}

func (s *Session) gameInfoLocked() stockpile.GameInfo {
	gi := stockpile.GameInfo{Title: s.game.Title, Code: s.game.Code, RomPath: s.game.RomPath}
	if s.conn != nil {
		gi.System = s.conn.info.System
	}
	return gi
}

// saveStateLocked saves the emulator state as a new savestate key.
func (s *Session) saveStateLocked(ctx context.Context, c *emu.Client) (*stockpile.StashKey, error) {
	data, err := c.SaveState(ctx)
	if err != nil {
		return nil, err
	}
	key := s.store.NewKey(s.rng)
	if err := s.store.WriteState(key, data); err != nil {
		return nil, err
	}
	return &stockpile.StashKey{
		Key:             key,
		ParentKey:       key,
		Alias:           key,
		Game:            s.gameInfoLocked(),
		SelectedDomains: slices.Clone(s.selected),
		CreatedAt:       time.Now().UTC(),
	}, nil
}

// loadStateLocked clears the scheduled units and loads a savestate blob.
func (s *Session) loadStateLocked(ctx context.Context, c *emu.Client, parentKey string) error {
	data, err := s.store.ReadState(parentKey)
	if err != nil {
		return classify(err)
	}
	if err := s.clearUnitsLocked(ctx, c); err != nil {
		return err
	}
	return c.LoadState(ctx, data)
}

// runLocked loads the key's state and applies its layer with backup.
func (s *Session) runLocked(ctx context.Context, k *stockpile.StashKey) error {
	c, err := s.clientLocked()
	if err != nil {
		return err
	}
	if err := s.loadStateLocked(ctx, c, k.ParentKey); err != nil {
		return err
	}
	if k.Layer == nil {
		return nil
	}
	return s.applyLocked(ctx, k.Layer, true, false)
}

func (s *Session) newKeyLocked(parent *stockpile.StashKey, domains []string, l *corrupt.Layer) *stockpile.StashKey {
	return stockpile.NewKey(s.store.NewKey(s.rng), parent, domains, l, time.Now())
}

// withOp runs f under the session lock with an operation context.
func (s *Session) withOp(ctx context.Context, f func(context.Context) error) error {
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	return classify(f(ctx))
}

// ---- Savestate slots ----

func (s *Session) Savestates() []SlotInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SlotInfo, stockpile.Slots)
	for i, sl := range s.store.SlotList() {
		out[i] = slotInfo(i+1, sl)
	}
	return out
}

func (s *Session) slotInfoLocked(n int) (SlotInfo, error) {
	sl, err := s.store.Slot(n)
	if err != nil {
		return SlotInfo{}, classify(err)
	}
	return slotInfo(n, sl), nil
}

// SaveSlot saves the current state into a slot.
func (s *Session) SaveSlot(ctx context.Context, n int) (SlotInfo, error) {
	var si SlotInfo
	err := s.withOp(ctx, func(ctx context.Context) error {
		if _, err := s.store.Slot(n); err != nil {
			return err
		}
		c, err := s.clientLocked()
		if err != nil {
			return err
		}
		k, err := s.saveStateLocked(ctx, c)
		if err != nil {
			return err
		}
		if err := s.store.SetSlot(n, k); err != nil {
			return err
		}
		s.changed(EventSavestates)
		si, err = s.slotInfoLocked(n)
		return err
	})
	return si, err
}

func (s *Session) LoadSlot(ctx context.Context, n int) error {
	return s.withOp(ctx, func(ctx context.Context) error {
		k, err := s.store.SlotKey(n)
		if err != nil {
			return err
		}
		c, err := s.clientLocked()
		if err != nil {
			return err
		}
		return s.loadStateLocked(ctx, c, k.ParentKey)
	})
}

func (s *Session) LabelSlot(n int, label string) (SlotInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.SetSlotLabel(n, label); err != nil {
		return SlotInfo{}, classify(err)
	}
	s.changed(EventSavestates)
	return s.slotInfoLocked(n)
}

func (s *Session) ClearSlot(n int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.ClearSlot(n); err != nil {
		return classify(err)
	}
	s.changed(EventSavestates)
	return nil
}

// ---- Stash history ----

func (s *Session) Stash() []*stockpile.StashKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	return summaries(s.store.Stash())
}

// Corrupt generates a layer on the slot's state (loading it first with
// loadBefore), applies it with backup and adds a new key to the stash.
func (s *Session) Corrupt(ctx context.Context, slot int, loadBefore bool) (*stockpile.StashKey, error) {
	var out *stockpile.StashKey
	err := s.withOp(ctx, func(ctx context.Context) error {
		start := time.Now()
		c, err := s.clientLocked()
		if err != nil {
			return err
		}
		sk, err := s.store.SlotKey(slot)
		if err != nil {
			return err
		}
		if err := s.clearUnitsLocked(ctx, c); err != nil {
			return err
		}
		if loadBefore {
			if err := s.loadStateLocked(ctx, c, sk.ParentKey); err != nil {
				return err
			}
		}
		l, err := s.generateLocked(ctx)
		if err != nil {
			return err
		}
		if err := s.applyLocked(ctx, l, true, false); err != nil {
			return err
		}
		k := s.newKeyLocked(sk, s.selected, l)
		s.store.AddStash(k)
		s.changed(EventStash)
		s.publishBlast(l, start)
		out = k.Clone()
		return nil
	})
	return out, err
}

// addAndRunLocked adds a new key to the stash and runs it.
func (s *Session) addAndRunLocked(ctx context.Context, k *stockpile.StashKey) (*stockpile.StashKey, error) {
	if err := s.runLocked(ctx, k); err != nil {
		return nil, err
	}
	s.store.AddStash(k)
	s.changed(EventStash)
	return k.Clone(), nil
}

// Inject applies the layer of a stash or stockpile key on a slot's state
// as a new stash key.
func (s *Session) Inject(ctx context.Context, key string, slot int) (*stockpile.StashKey, error) {
	var out *stockpile.StashKey
	err := s.withOp(ctx, func(ctx context.Context) error {
		src, err := s.store.Find(key)
		if err != nil {
			return err
		}
		sk, err := s.store.SlotKey(slot)
		if err != nil {
			return err
		}
		out, err = s.addAndRunLocked(ctx, s.newKeyLocked(sk, src.SelectedDomains, layerOf(src)))
		return err
	})
	return out, err
}

func layerOf(k *stockpile.StashKey) *corrupt.Layer {
	if k.Layer == nil {
		return &corrupt.Layer{}
	}
	return k.Layer.Clone()
}

func (s *Session) Run(ctx context.Context, inStockpile bool, key string) error {
	return s.withOp(ctx, func(ctx context.Context) error {
		k, err := s.findLocked(inStockpile, key)
		if err != nil {
			return err
		}
		return s.runLocked(ctx, k)
	})
}

// Original loads a stash key's state without its layer.
func (s *Session) Original(ctx context.Context, key string) error {
	return s.withOp(ctx, func(ctx context.Context) error {
		k, err := s.store.FindStash(key)
		if err != nil {
			return err
		}
		c, err := s.clientLocked()
		if err != nil {
			return err
		}
		return s.loadStateLocked(ctx, c, k.ParentKey)
	})
}

// RerollKey adds a new stash key with a rerolled copy of the key's layer
// and runs it.
func (s *Session) RerollKey(ctx context.Context, key string) (*stockpile.StashKey, error) {
	var out *stockpile.StashKey
	err := s.withOp(ctx, func(ctx context.Context) error {
		k, err := s.store.FindStash(key)
		if err != nil {
			return err
		}
		if _, err := s.clientLocked(); err != nil {
			return err
		}
		l, err := s.rerollLocked(layerOf(k))
		if err != nil {
			return err
		}
		out, err = s.addAndRunLocked(ctx, s.newKeyLocked(k, k.SelectedDomains, l))
		return err
	})
	return out, err
}

// Merge concatenates the layers of 2+ keys (identical units once) on the
// first key's state, runs it and adds it to the stash.
func (s *Session) Merge(ctx context.Context, keys []string) (*stockpile.StashKey, error) {
	var out *stockpile.StashKey
	err := s.withOp(ctx, func(ctx context.Context) error {
		if len(keys) < 2 {
			return errorf(KindInvalid, "merge needs at least 2 keys")
		}
		var ks []*stockpile.StashKey
		var layers []*corrupt.Layer
		for _, key := range keys {
			k, err := s.store.Find(key)
			if err != nil {
				return err
			}
			ks = append(ks, k)
			layers = append(layers, k.Layer)
		}
		l, err := distinct(corrupt.Merge(layers...))
		if err != nil {
			return err
		}
		out, err = s.addAndRunLocked(ctx, s.newKeyLocked(ks[0], ks[0].SelectedDomains, l))
		return err
	})
	return out, err
}

func distinct(l *corrupt.Layer) (*corrupt.Layer, error) {
	var seen [][]byte
	out := &corrupt.Layer{Note: l.Note}
	for _, u := range l.Units {
		b, err := json.Marshal(u)
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(seen, func(o []byte) bool { return bytes.Equal(o, b) }) {
			continue
		}
		seen = append(seen, b)
		out.Units = append(out.Units, u)
	}
	return out, nil
}

// ---- Stash and stockpile keys ----

func (s *Session) findLocked(inStockpile bool, key string) (*stockpile.StashKey, error) {
	if inStockpile {
		return s.store.FindStockpile(key)
	}
	return s.store.FindStash(key)
}

func listEvent(inStockpile bool) string {
	if inStockpile {
		return EventStockpile
	}
	return EventStash
}

func (s *Session) PatchKey(inStockpile bool, key string, alias, note *string) (*stockpile.StashKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, err := s.findLocked(inStockpile, key)
	if err != nil {
		return nil, classify(err)
	}
	if alias != nil {
		k.Alias = *alias
	}
	if note != nil {
		k.Note = *note
	}
	s.changed(listEvent(inStockpile))
	return k.Summary(), nil
}

func (s *Session) DeleteKey(inStockpile bool, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	if inStockpile {
		err = s.store.RemoveStockpile(key)
	} else {
		err = s.store.RemoveStash(key)
	}
	if err != nil {
		return classify(err)
	}
	s.changed(listEvent(inStockpile))
	return nil
}

func (s *Session) ClearKeys(inStockpile bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if inStockpile {
		s.store.ClearStockpile()
	} else {
		s.store.ClearStash()
	}
	s.changed(listEvent(inStockpile))
}

func (s *Session) KeyLayer(inStockpile bool, key string) (*corrupt.Layer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, err := s.findLocked(inStockpile, key)
	if err != nil {
		return nil, classify(err)
	}
	return layerOf(k), nil
}

func (s *Session) SetKeyLayer(inStockpile bool, key string, l *corrupt.Layer) (*corrupt.Layer, error) {
	if err := l.Validate(); err != nil {
		return nil, errorf(KindInvalid, "layer: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k, err := s.findLocked(inStockpile, key)
	if err != nil {
		return nil, classify(err)
	}
	k.SetLayer(l.Clone())
	s.changed(listEvent(inStockpile))
	return layerOf(k), nil
}

// ToStockpile moves a stash key to the end of the stockpile.
func (s *Session) ToStockpile(key string, alias *string) (*stockpile.StashKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, err := s.store.ToStockpile(key)
	if err != nil {
		return nil, classify(err)
	}
	if alias != nil {
		k.Alias = *alias
	}
	s.changed(EventStash)
	s.changed(EventStockpile)
	return k.Summary(), nil
}

// ---- Stockpile ----

func (s *Session) Stockpile() []*stockpile.StashKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	return summaries(s.store.Stockpile())
}

func (s *Session) Reorder(keys []string) ([]*stockpile.StashKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.Reorder(keys); err != nil {
		return nil, classify(err)
	}
	s.changed(EventStockpile)
	return summaries(s.store.Stockpile()), nil
}

// ExportStockpile returns the stockpile as an .sks file.
func (s *Session) ExportStockpile() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var buf bytes.Buffer
	if err := s.store.Export(&buf, s.listsDir()); err != nil {
		return nil, classify(err)
	}
	return buf.Bytes(), nil
}

// ImportStockpile reads an .sks, replacing the stockpile unless merge is
// set.
func (s *Session) ImportStockpile(data []byte, merge bool) ([]*stockpile.StashKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	added, err := s.store.Import(data, merge, s.listsDir())
	if err != nil {
		return nil, classify(err)
	}
	if len(added) > 0 {
		s.reloadListsLocked()
		s.changed(EventLists)
	}
	s.changed(EventStockpile)
	return summaries(s.store.Stockpile()), nil
}

// SaveStockpile writes the stockpile as .sks to a path on the core host.
func (s *Session) SaveStockpile(path string) error {
	data, err := s.ExportStockpile()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return classify(err)
	}
	return nil
}

// LoadStockpile replaces the stockpile with an .sks at a path on the core
// host.
func (s *Session) LoadStockpile(path string) ([]*stockpile.StashKey, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errorf(KindNotFound, "%s does not exist", path)
	}
	if err != nil {
		return nil, classify(err)
	}
	return s.ImportStockpile(data, false)
}

// ---- Game protection ----

func (s *Session) ProtectionBackup(ctx context.Context) error {
	return s.withOp(ctx, s.backupLocked)
}

func (s *Session) backupLocked(ctx context.Context) error {
	c, err := s.clientLocked()
	if err != nil {
		return err
	}
	k, err := s.saveStateLocked(ctx, c)
	if err != nil {
		return err
	}
	s.store.PushBackup(k, s.settings.GameProtection.Keep)
	s.lastBackup = time.Now()
	s.publishStatusLocked()
	return nil
}

// ProtectionBack loads the most recent backup and drops it.
func (s *Session) ProtectionBack(ctx context.Context) error {
	return s.withOp(ctx, func(ctx context.Context) error {
		c, err := s.clientLocked()
		if err != nil {
			return err
		}
		k := s.store.LatestBackup()
		if k == nil {
			return errorf(KindNoBackup, "no game protection backup")
		}
		if err := s.loadStateLocked(ctx, c, k.ParentKey); err != nil {
			return err
		}
		s.store.DropLatestBackup()
		s.lastBackup = time.Now()
		s.publishStatusLocked()
		return nil
	})
}

// protectionLoop takes a backup every gameProtection.intervalSeconds while
// game protection is enabled and a game is running.
func (s *Session) protectionLoop() {
	defer s.wg.Done()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		}
		s.mu.Lock()
		gp := s.settings.GameProtection
		due := gp.Enabled && s.conn != nil && s.game.State == StateRunning &&
			time.Since(s.lastBackup) >= time.Duration(gp.IntervalSeconds)*time.Second
		if due {
			ctx, cancel := context.WithTimeout(s.ctx, opTimeout)
			if err := s.backupLocked(ctx); err != nil && s.ctx.Err() == nil {
				s.log.Warn("game protection backup failed", "err", err)
				s.lastBackup = time.Now()
			}
			cancel()
		}
		s.mu.Unlock()
	}
}
