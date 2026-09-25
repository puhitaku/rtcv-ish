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

// memLocked is the emulator memory for generation and rasterization. c
// may be nil when only the domain list is needed.
func (s *Session) memLocked(c *emu.Client) corrupt.Memory {
	return corrupt.EmuMemory(c, s.protoDomains)
}

func (s *Session) nextIDLocked() uint64 {
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

// generateLocked builds a layer with the current settings on the selected
// domains.
func (s *Session) generateLocked(ctx context.Context) (*corrupt.Layer, error) {
	c, err := s.romLocked()
	if err != nil {
		return nil, err
	}
	if err := s.syncDomainsLocked(ctx); err != nil {
		return nil, err
	}
	if len(s.selected) == 0 {
		return nil, errorf(KindInvalid, "no domains selected")
	}
	var mem corrupt.Memory = s.memLocked(c)
	if usesLists(s.settings) {
		mem = corrupt.NewSnapshot(mem)
	}
	l, err := corrupt.Generate(ctx, s.rng, s.settings, s.selected, mem, s.lists)
	if err != nil {
		return nil, invalid(err)
	}
	return l, nil
}

func (s *Session) publishBlast(l *corrupt.Layer, start time.Time) {
	s.broker.publish(Event{Type: EventBlast, Data: BlastEvent{
		Count:     len(l.Units),
		Engine:    string(s.settings.Engine),
		ElapsedMs: float64(time.Since(start).Microseconds()) / 1000,
	}})
}

// Blast generates a layer with the current settings and applies it.
func (s *Session) Blast(ctx context.Context) (*corrupt.Layer, error) {
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.blastLocked(ctx, true)
}

func (s *Session) blastLocked(ctx context.Context, followMax bool) (*corrupt.Layer, error) {
	start := time.Now()
	l, err := s.generateLocked(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.applyLocked(ctx, l, false, followMax); err != nil {
		return nil, err
	}
	s.publishBlast(l, start)
	return l, nil
}

// applyLocked rasterizes and schedules a layer. With backup, the current
// bytes at every unit are stored for Toggle. With followMax, the oldest
// infinite units beyond maxInfiniteUnits are removed unless lockUnits is
// set.
func (s *Session) applyLocked(ctx context.Context, l *corrupt.Layer, backup, followMax bool) error {
	c, err := s.clientLocked()
	if err != nil {
		return err
	}
	mem := s.memLocked(c)
	var bk *corrupt.Layer
	if backup {
		if bk, err = l.Backup(ctx, mem); err != nil {
			return invalid(err)
		}
	}
	units, err := corrupt.Rasterize(ctx, l, mem, s.nextIDLocked)
	if err != nil {
		return invalid(err)
	}
	if err := s.scheduleLocked(ctx, c, units); err != nil {
		return err
	}
	if followMax && !s.settings.LockUnits {
		if n := len(s.infinite) - s.settings.MaxInfiniteUnits; n > 0 {
			if err := c.RemoveUnits(ctx, s.infinite[:n]); err != nil {
				return err
			}
			s.infinite = slices.Delete(s.infinite, 0, n)
		}
	}
	if backup {
		s.blLayer, s.blBackup, s.blOn = l.Clone(), bk, true
		s.publishStatusLocked()
	}
	return nil
}

func (s *Session) scheduleLocked(ctx context.Context, c *emu.Client, units []*emulatorv1.Unit) error {
	if len(units) == 0 {
		return nil
	}
	if err := c.ApplyUnits(ctx, units); err != nil {
		return err
	}
	for _, u := range units {
		if u.GetLifetime() == 0 {
			s.infinite = append(s.infinite, u.GetId())
		}
	}
	return nil
}

func (s *Session) clearUnitsLocked(ctx context.Context, c *emu.Client) error {
	if err := c.ClearUnits(ctx); err != nil {
		return err
	}
	s.infinite = nil
	return nil
}

// ApplyLayer schedules a layer on the running game. Units must target
// existing domains and fit in them.
func (s *Session) ApplyLayer(ctx context.Context, l *corrupt.Layer, backup bool) error {
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.romLocked(); err != nil {
		return err
	}
	if err := l.Validate(); err != nil {
		return errorf(KindInvalid, "layer: %v", err)
	}
	if err := s.syncDomainsLocked(ctx); err != nil {
		return err
	}
	if err := s.checkTargetsLocked(l); err != nil {
		return err
	}
	return s.applyLocked(ctx, l, backup, false)
}

func (s *Session) checkTargetsLocked(l *corrupt.Layer) error {
	check := func(i int, name string, addr uint64, size int) error {
		j := slices.IndexFunc(s.domains, func(d corrupt.Domain) bool { return d.Name == name })
		if j < 0 {
			return errorf(KindNotFound, "unit %d: unknown domain %q", i, name)
		}
		if d := s.domains[j]; addr > d.Size || uint64(size) > d.Size-addr {
			return errorf(KindOutOfRange, "unit %d: %s 0x%x+%d exceeds size 0x%x", i, name, addr, size, d.Size)
		}
		return nil
	}
	for i, u := range l.Units {
		if !u.Enabled {
			continue
		}
		if err := check(i, u.Domain, u.Address, u.Precision); err != nil {
			return err
		}
		if u.Source == corrupt.SourceStore {
			if err := check(i, u.SourceDomain, u.SourceAddress, u.Precision); err != nil {
				return err
			}
		}
	}
	return nil
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
}

type StoreSource struct {
	Domain     string `json:"domain"`
	Address    uint64 `json:"address"`
	Continuous bool   `json:"continuous"`
}

func (s *Session) Units(ctx context.Context) ([]EmuUnit, error) {
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	s.mu.Lock()
	c, err := s.clientLocked()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	us, err := c.ListUnits(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]EmuUnit, 0, len(us))
	for _, u := range us {
		eu := EmuUnit{
			ID: u.GetId(), Domain: u.GetDomain(), Address: u.GetAddress(), Size: u.GetSize(),
			Tilt: u.GetTilt(), Delay: u.GetDelay(), Lifetime: u.GetLifetime(), Loop: u.GetLoop(), LoopDelay: u.GetLoopDelay(),
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
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.clientLocked()
	if err != nil {
		return err
	}
	return s.clearUnitsLocked(ctx, c)
}

// Toggle switches the last layer applied with backup off (clear units,
// apply the uncorrupt backup) or on (clear units, re-apply the layer).
func (s *Session) Toggle(ctx context.Context, on bool) error {
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.clientLocked()
	if err != nil {
		return err
	}
	if s.blLayer == nil {
		return errorf(KindNoBackup, "no layer was applied with backup")
	}
	if err := s.clearUnitsLocked(ctx, c); err != nil {
		return err
	}
	l := s.blBackup
	if on {
		l = s.blLayer
	}
	if err := s.applyLocked(ctx, l, false, false); err != nil {
		return err
	}
	s.blOn = on
	s.publishStatusLocked()
	return nil
}

// Reroll returns a rerolled copy of l following the reroll settings.
func (s *Session) Reroll(l *corrupt.Layer) (*corrupt.Layer, error) {
	ctx, cancel := s.opCtx(context.Background())
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rerollLocked(ctx, l)
}

func (s *Session) rerollLocked(ctx context.Context, l *corrupt.Layer) (*corrupt.Layer, error) {
	if err := l.Validate(); err != nil {
		return nil, errorf(KindInvalid, "layer: %v", err)
	}
	if err := s.syncDomainsLocked(ctx); err != nil {
		return nil, err
	}
	out := l.Clone()
	var c *emu.Client
	if s.conn != nil {
		c = s.conn.client
	}
	if err := out.Reroll(s.rng, s.settings, s.selected, s.memLocked(c), s.lists); err != nil {
		return nil, invalid(fmt.Errorf("reroll: %w", err))
	}
	return out, nil
}
