package emutest

import (
	"context"
	"encoding/binary"
	"testing"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/emu"
)

// FindCounter locates a 32-bit little-endian word in domain that the
// running game increments by exactly 1 per frame, by diffing the domain
// across single-frame Steps. It fails the test when there is none.
func FindCounter(t *testing.T, c *emu.Client, domain string) Scratch {
	t.Helper()
	ctx := ctx(t)
	var size uint64
	ds, err := c.ListDomains(ctx)
	must(t, err, "ListDomains")
	for _, d := range ds {
		if d.GetName() == domain {
			size = d.GetSize()
		}
	}
	if size == 0 {
		t.Fatalf("no domain %s", domain)
	}

	const samples = 5
	var snaps [][]byte
	for i := range samples {
		if i > 0 {
			_, err := c.Step(ctx, 1)
			must(t, err, "Step")
		} else {
			must(t, c.Pause(ctx), "Pause")
		}
		b, err := c.ReadOne(ctx, domain, 0, uint32(size))
		must(t, err, "Read")
		snaps = append(snaps, b)
	}
	for a := uint64(0); a+4 <= size; a += 4 {
		ok := true
		for i := 1; i < samples && ok; i++ {
			prev := binary.LittleEndian.Uint32(snaps[i-1][a:])
			cur := binary.LittleEndian.Uint32(snaps[i][a:])
			ok = cur == prev+1
		}
		if ok {
			return Scratch{Domain: domain, Base: a}
		}
	}
	t.Fatalf("no 32-bit word of %s increments by 1 per frame", domain)
	return Scratch{}
}

// RunModes checks the unit modes against counter, a 32-bit little-endian
// word the running game increments once per frame (see FindCounter).
// scanlines tells whether the emulator rewrites SCANLINE units during the
// frame, after the game's increment; the fake has no scanlines and runs
// them like FRAME units.
func RunModes(t *testing.T, c *emu.Client, counter Scratch, scanlines bool) {
	caps := c.Info().GetCapabilities()
	if !caps.GetScanlineUnits() {
		t.Error("capabilities do not report scanline_units")
	}
	read := func(t *testing.T) uint32 {
		t.Helper()
		b, err := c.ReadOne(ctx(t), counter.Domain, counter.Base, 4)
		must(t, err, "Read")
		return binary.LittleEndian.Uint32(b)
	}
	step := func(t *testing.T) {
		t.Helper()
		_, err := c.Step(ctx(t), 1)
		must(t, err, "Step")
	}
	freeze := func(t *testing.T, mode emulatorv1.Mode, off uint64, value []byte) {
		t.Helper()
		must(t, c.ClearUnits(ctx(t)), "ClearUnits")
		t.Cleanup(func() { c.ClearUnits(context.Background()) })
		u := &emulatorv1.Unit{
			Id: 1, Domain: counter.Domain, Address: counter.Base + off, Size: uint32(len(value)),
			Mode: mode, Source: &emulatorv1.Unit_Value{Value: value},
		}
		must(t, c.ApplyUnits(ctx(t), []*emulatorv1.Unit{u}), "ApplyUnits")
	}
	const frozen = 0x5a5a0000

	t.Run("frame loses to the game", func(t *testing.T) {
		freeze(t, emulatorv1.Mode_FRAME, 0, le32(frozen))
		for range 4 {
			step(t)
			if got := read(t); got == frozen {
				t.Errorf("counter = %#x after Step, want the game's value (the unit writes only before the frame)", got)
			}
		}
	})

	t.Run("scanline", func(t *testing.T) {
		freeze(t, emulatorv1.Mode_SCANLINE, 0, le32(frozen))
		for range 4 {
			step(t)
			got := read(t)
			switch {
			case scanlines && got != frozen:
				t.Errorf("counter = %#x at a frame boundary, want the frozen %#x", got, frozen)
			case !scanlines && got == frozen:
				t.Errorf("counter = %#x, want the game's value (SCANLINE runs as FRAME here)", got)
			}
		}
	})

	t.Run("hard", func(t *testing.T) {
		if !caps.GetHardUnits() {
			t.Skip("emulator reports hard_units=false")
		}
		freeze(t, emulatorv1.Mode_HARD, 0, le32(frozen))
		for range 8 {
			step(t)
			if got := read(t); got != frozen {
				t.Errorf("counter = %#x after Step, want the frozen %#x", got, frozen)
			}
		}
		must(t, c.WriteOne(ctx(t), counter.Domain, counter.Base, le32(1234)), "Write")
		if got := read(t); got != frozen {
			t.Errorf("counter = %#x after a client Write, want the frozen %#x", got, frozen)
		}
		must(t, c.ClearUnits(ctx(t)), "ClearUnits")
		step(t)
		step(t)
		if got := read(t); got == frozen {
			t.Errorf("counter = %#x two frames after removing the unit, want the game's value", got)
		}
	})

	t.Run("hard partial word", func(t *testing.T) {
		if !caps.GetHardUnits() {
			t.Skip("emulator reports hard_units=false")
		}
		// The game's 32-bit store covers two frozen bytes: those keep their
		// value, the low half keeps counting.
		freeze(t, emulatorv1.Mode_HARD, 2, []byte{0xef, 0xbe})
		step(t)
		first := read(t)
		for i := range uint32(4) {
			step(t)
			got := read(t)
			if got>>16 != 0xbeef || got&0xffff != (first+i+1)&0xffff {
				t.Errorf("counter = %#x, want 0xbeef in the high half and %#x in the low half", got, (first+i+1)&0xffff)
			}
		}
	})

	t.Run("hard across LoadState", func(t *testing.T) {
		if !caps.GetHardUnits() || !caps.GetSavestates() {
			t.Skip("emulator lacks hard_units or savestates")
		}
		step(t)
		state, err := c.SaveState(ctx(t))
		must(t, err, "SaveState")
		freeze(t, emulatorv1.Mode_HARD, 0, le32(frozen))
		step(t)
		must(t, c.LoadState(ctx(t), state), "LoadState")
		if got := read(t); got != frozen {
			t.Errorf("counter = %#x right after LoadState, want the frozen %#x", got, frozen)
		}
		for range 3 {
			step(t)
			if got := read(t); got != frozen {
				t.Errorf("counter = %#x after LoadState and Step, want the frozen %#x", got, frozen)
			}
		}
	})
}
