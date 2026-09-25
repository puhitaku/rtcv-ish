package session

import (
	"context"
	"fmt"
	"slices"
	"time"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/corrupt"
	"github.com/puhitaku/rtcv-ish/internal/emu"
)

func (s *Session) nextID() uint64 {
	s.nextUnitID++
	return s.nextUnitID
}

// usesLists reports whether generation reads memory for list lookups, in
// which case the selected domains are snapshotted first.
func usesLists(st *corrupt.Settings) bool {
	switch st.Engine {
	case corrupt.EngineVector, corrupt.EngineCluster:
		return true
	case corrupt.EngineCustom:
		return st.Custom.LimiterTime != corrupt.LimiterNone
	}
	return false
}

// generated is a layer and the settings and selection it was built with.
type generated struct {
	layer    *corrupt.Layer
	settings *corrupt.Settings
	selected []string
}

// generate builds a layer with the current settings on the selected
// domains. The caller holds the gate.
func (s *Session) generate(ctx context.Context, cn *conn) (generated, error) {
	if err := s.syncDomains(ctx, cn); err != nil {
		return generated{}, err
	}
	s.mu.Lock()
	if s.conn != cn {
		s.mu.Unlock()
		return generated{}, errDisconnected
	}
	if s.game.State == StateNoRom {
		s.mu.Unlock()
		return generated{}, errorf(KindNoROM, "no ROM loaded")
	}
	g := generated{settings: s.settings.Clone(), selected: slices.Clone(s.selected)}
	reg, pd := s.lists, s.protoDomains
	s.mu.Unlock()
	if len(g.selected) == 0 {
		return generated{}, errorf(KindInvalid, "no domains selected")
	}
	var mem corrupt.Memory = corrupt.EmuMemory(cn.client, pd)
	if usesLists(g.settings) {
		mem = corrupt.NewSnapshot(mem)
	}
	l, err := corrupt.Generate(ctx, s.rng, g.settings, g.selected, mem, reg)
	if err != nil {
		return generated{}, invalid(err)
	}
	if len(l.Units) == 0 && corrupt.AddressRangeMisses(g.settings, g.selected, mem) {
		r := g.settings.AddressRange
		s.notify("warn", fmt.Sprintf("Address range 0x%X-0x%X does not fit a unit in any selected domain; nothing was generated", r.Start, r.End))
	}
	g.layer = l
	return g, nil
}

func (s *Session) publishBlast(l *corrupt.Layer, engine corrupt.Engine, start time.Time) {
	s.broker.publish(Event{Type: EventBlast, Data: BlastEvent{
		Count:     len(l.Units),
		Engine:    string(engine),
		ElapsedMs: float64(time.Since(start).Microseconds()) / 1000,
	}})
}

// Blast generates a layer with the current settings and applies it.
func (s *Session) Blast(ctx context.Context) (*corrupt.Layer, error) {
	var l *corrupt.Layer
	err := s.withOp(ctx, "blast", func(ctx context.Context) (err error) {
		l, err = s.blast(ctx, true)
		return err
	})
	return l, err
}

func (s *Session) blast(ctx context.Context, followMax bool) (*corrupt.Layer, error) {
	cn, err := s.currentROM()
	if err != nil {
		return nil, err
	}
	start := time.Now()
	g, err := s.generate(ctx, cn)
	if err != nil {
		return nil, err
	}
	if err := s.apply(ctx, cn, g.layer, false, followMax); err != nil {
		return nil, err
	}
	s.publishBlast(g.layer, g.settings.Engine, start)
	return g.layer, nil
}

// apply rasterizes and schedules a layer. With backup, the current bytes
// at every unit are stored for Toggle. With followMax, the oldest infinite
// units beyond maxInfiniteUnits are removed unless lockUnits is set.
func (s *Session) apply(ctx context.Context, cn *conn, l *corrupt.Layer, backup, followMax bool) error {
	s.mu.Lock()
	if s.conn != cn {
		s.mu.Unlock()
		return errDisconnected
	}
	pd := s.protoDomains
	lockUnits, maxInfinite := s.settings.LockUnits, s.settings.MaxInfiniteUnits
	want := s.settings.FreezeMode
	caps := cn.info.Capabilities
	mode, fellBack := corrupt.InfiniteMode(want, caps.ScanlineUnits, caps.HardUnits)
	logFallback := fellBack && !cn.modeFallbackLogged
	if logFallback {
		cn.modeFallbackLogged = true
	}
	s.mu.Unlock()
	if logFallback {
		s.log.Info("the emulator lacks the freeze mode; infinite units fall back",
			"freezeMode", want, "using", corrupt.ModeName(mode), "emulator", cn.info.Name)
	}
	mem := corrupt.EmuMemory(cn.client, pd)
	if err := l.CheckRanges(mem.Domains()); err != nil {
		return classify(err)
	}
	var bk *corrupt.Layer
	var err error
	if backup {
		if bk, err = l.Backup(ctx, mem); err != nil {
			return invalid(err)
		}
	}
	units, err := corrupt.Rasterize(ctx, l, mem, mode, s.nextID)
	if err != nil {
		return invalid(err)
	}
	if err := s.schedule(ctx, cn, units); err != nil {
		return err
	}
	if len(units) > 0 {
		s.unitsChanged(UnitsApply, 0)
	}
	if followMax && !lockUnits {
		var drop []uint64
		s.mu.Lock()
		if n := len(s.infinite) - maxInfinite; n > 0 {
			drop = slices.Clone(s.infinite[:n])
		}
		s.mu.Unlock()
		if len(drop) > 0 {
			if err := cn.client.RemoveUnits(ctx, drop); err != nil {
				return err
			}
			s.mu.Lock()
			s.infinite = slices.DeleteFunc(s.infinite, func(id uint64) bool { return slices.Contains(drop, id) })
			s.mu.Unlock()
			s.unitsChanged(UnitsRemove, 0)
		}
	}
	if backup {
		return s.commit(cn, func() {
			s.blLayer, s.blBackup, s.blOn = l.Clone(), bk, true
			s.publishStatusLocked()
		})
	}
	return nil
}

func (s *Session) schedule(ctx context.Context, cn *conn, units []*emulatorv1.Unit) error {
	if len(units) == 0 {
		return nil
	}
	if err := cn.client.ApplyUnits(ctx, units); err != nil {
		return err
	}
	return s.commit(cn, func() {
		for _, u := range units {
			if u.GetLifetime() == 0 {
				s.infinite = append(s.infinite, u.GetId())
			}
		}
	})
}

func (s *Session) clearUnits(ctx context.Context, cn *conn) error {
	if err := cn.client.ClearUnits(ctx); err != nil {
		return err
	}
	return s.commit(cn, func() {
		s.infinite = nil
		s.unitsChanged(UnitsClear, 0)
	})
}

// countUnits returns the number of units scheduled in the emulator.
func (s *Session) countUnits(ctx context.Context, cn *conn) (int, error) {
	us, err := cn.client.ListUnits(ctx)
	return len(us), err
}

// ApplyLayer schedules a layer on the running game. Units must target
// existing domains and fit in them.
func (s *Session) ApplyLayer(ctx context.Context, l *corrupt.Layer, backup bool) error {
	return s.withOp(ctx, "applyLayer", func(ctx context.Context) error {
		cn, err := s.currentROM()
		if err != nil {
			return err
		}
		if err := l.Validate(); err != nil {
			return errorf(KindInvalid, "layer: %v", err)
		}
		if err := s.syncDomains(ctx, cn); err != nil {
			return err
		}
		s.mu.Lock()
		err = s.checkTargetsLocked(l)
		s.mu.Unlock()
		if err != nil {
			return err
		}
		return s.apply(ctx, cn, l, backup, false)
	})
}

// checkTargetsLocked reports enabled units on unknown domains and units
// that do not fit in their domain.
func (s *Session) checkTargetsLocked(l *corrupt.Layer) error {
	known := func(name string) bool {
		return slices.ContainsFunc(s.domains, func(d corrupt.Domain) bool { return d.Name == name })
	}
	for i, u := range l.Units {
		if !u.Enabled {
			continue
		}
		if !known(u.Domain) {
			return errorf(KindNotFound, "unit %d: unknown domain %q", i, u.Domain)
		}
		if u.Source == corrupt.SourceStore && !known(u.SourceDomain) {
			return errorf(KindNotFound, "unit %d: unknown domain %q", i, u.SourceDomain)
		}
	}
	return classify(l.CheckRanges(s.domains))
}

// EmuUnit is a unit scheduled in the emulator.
type EmuUnit struct {
	ID        uint64       `json:"id"`
	Domain    string       `json:"domain"`
	Address   uint64       `json:"address"`
	Size      uint32       `json:"size"`
	Value     corrupt.Hex  `json:"value,omitempty"`
	Store     *StoreSource `json:"store,omitempty"`
	Tilt      int64        `json:"tilt"`
	Delay     uint32       `json:"delay"`
	Lifetime  uint32       `json:"lifetime"`
	Loop      bool         `json:"loop"`
	LoopDelay uint32       `json:"loopDelay"`
	// Mode is frame, scanline or hard (corrupt.FreezeMode values).
	Mode corrupt.FreezeMode `json:"mode"`
}

type StoreSource struct {
	Domain     string `json:"domain"`
	Address    uint64 `json:"address"`
	Continuous bool   `json:"continuous"`
}

func (s *Session) Units(ctx context.Context) ([]EmuUnit, error) {
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	cn, err := s.currentResponsive()
	if err != nil {
		return nil, err
	}
	us, err := cn.client.ListUnits(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]EmuUnit, 0, len(us))
	for _, u := range us {
		eu := EmuUnit{
			ID: u.GetId(), Domain: u.GetDomain(), Address: u.GetAddress(), Size: u.GetSize(),
			Tilt: u.GetTilt(), Delay: u.GetDelay(), Lifetime: u.GetLifetime(), Loop: u.GetLoop(), LoopDelay: u.GetLoopDelay(),
			Mode: corrupt.ModeName(u.GetMode()),
		}
		if st := u.GetStore(); st != nil {
			eu.Store = &StoreSource{Domain: st.GetDomain(), Address: st.GetAddress(), Continuous: st.GetContinuous()}
		} else {
			eu.Value = u.GetValue()
		}
		out = append(out, eu)
	}
	return out, nil
}

func (s *Session) ClearUnits(ctx context.Context) error {
	return s.withOp(ctx, "clearUnits", func(ctx context.Context) error {
		cn, err := s.current()
		if err != nil {
			return err
		}
		return s.clearUnits(ctx, cn)
	})
}

// RemoveUnit removes one scheduled unit. Removing an unknown or expired
// id is KindNotFound.
func (s *Session) RemoveUnit(ctx context.Context, id uint64) error {
	return s.withOp(ctx, "removeUnit", func(ctx context.Context) error {
		cn, err := s.current()
		if err != nil {
			return err
		}
		us, err := cn.client.ListUnits(ctx)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(us, func(u *emulatorv1.Unit) bool { return u.GetId() == id }) {
			return errorf(KindNotFound, "no scheduled unit %d", id)
		}
		if err := cn.client.RemoveUnits(ctx, []uint64{id}); err != nil {
			return err
		}
		return s.commit(cn, func() {
			s.infinite = slices.DeleteFunc(s.infinite, func(x uint64) bool { return x == id })
			s.unitsChanged(UnitsRemove, 0)
		})
	})
}

// Toggle switches the last layer applied with backup off (clear units,
// apply the uncorrupt backup) or on (clear units, re-apply the layer).
func (s *Session) Toggle(ctx context.Context, on bool) error {
	return s.withOp(ctx, "toggle", func(ctx context.Context) error {
		cn, err := s.current()
		if err != nil {
			return err
		}
		s.mu.Lock()
		l := s.blBackup
		if on {
			l = s.blLayer
		}
		noLayer := s.blLayer == nil
		s.mu.Unlock()
		if noLayer {
			return errorf(KindNoBackup, "no layer was applied with backup")
		}
		if err := s.clearUnits(ctx, cn); err != nil {
			return err
		}
		if err := s.apply(ctx, cn, l, false, false); err != nil {
			return err
		}
		return s.commit(cn, func() {
			s.blOn = on
			s.publishStatusLocked()
		})
	})
}

// Reroll returns a rerolled copy of l following the reroll settings.
func (s *Session) Reroll(ctx context.Context, l *corrupt.Layer) (*corrupt.Layer, error) {
	var out *corrupt.Layer
	err := s.withOp(ctx, "reroll", func(ctx context.Context) (err error) {
		out, err = s.reroll(ctx, l)
		return err
	})
	return out, err
}

func (s *Session) reroll(ctx context.Context, l *corrupt.Layer) (*corrupt.Layer, error) {
	if err := l.Validate(); err != nil {
		return nil, errorf(KindInvalid, "layer: %v", err)
	}
	s.mu.Lock()
	cn := s.conn
	s.mu.Unlock()
	if cn != nil {
		if err := s.syncDomains(ctx, cn); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	st, selected, reg, pd := s.settings.Clone(), slices.Clone(s.selected), s.lists, s.protoDomains
	var c *emu.Client
	if s.conn != nil {
		c = s.conn.client
	}
	s.mu.Unlock()
	out := l.Clone()
	if err := out.Reroll(s.rng, st, selected, corrupt.EmuMemory(c, pd), reg); err != nil {
		return nil, invalid(fmt.Errorf("reroll: %w", err))
	}
	return out, nil
}
