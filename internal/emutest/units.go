package emutest

import (
	"bytes"
	"context"
	"encoding/binary"
	"slices"
	"testing"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/emu"
	"google.golang.org/protobuf/proto"
)

// U drives unit scheduler tests on a scratch area with Step.
type U struct {
	t   *testing.T
	ctx context.Context
	c   *emu.Client
	s   Scratch
}

func (u *U) addr(off uint64) uint64 { return u.s.Base + off }

func (u *U) Put(off uint64, b []byte) {
	u.t.Helper()
	must(u.t, u.c.WriteOne(u.ctx, u.s.Domain, u.addr(off), b), "Write")
}

func (u *U) Put32(off uint64, v uint32) { u.t.Helper(); u.Put(off, le32(v)) }

func (u *U) Want(off uint64, want []byte) {
	u.t.Helper()
	got, err := u.c.ReadOne(u.ctx, u.s.Domain, u.addr(off), uint32(len(want)))
	must(u.t, err, "Read")
	if !bytes.Equal(got, want) {
		st, _ := u.c.Status(u.ctx)
		u.t.Errorf("frame %d: %s+0x%x = % x, want % x", st.GetFrame(), u.s, off, got, want)
	}
}

func (u *U) Want32(off uint64, v uint32) { u.t.Helper(); u.Want(off, le32(v)) }

func (u *U) Step(n uint32) {
	u.t.Helper()
	_, err := u.c.Step(u.ctx, n)
	must(u.t, err, "Step")
}

func (u *U) Apply(units ...*emulatorv1.Unit) {
	u.t.Helper()
	must(u.t, u.c.ApplyUnits(u.ctx, units), "ApplyUnits")
}

func (u *U) WantIDs(ids ...uint64) {
	u.t.Helper()
	units, err := u.c.ListUnits(u.ctx)
	must(u.t, err, "ListUnits")
	got := []uint64{}
	for _, x := range units {
		got = append(got, x.GetId())
	}
	if !slices.Equal(got, append([]uint64{}, ids...)) {
		u.t.Errorf("ListUnits ids = %v, want %v", got, ids)
	}
}

// Value returns a value unit writing b at off.
func (u *U) Value(id, off uint64, b []byte) *emulatorv1.Unit {
	return &emulatorv1.Unit{Id: id, Domain: u.s.Domain, Address: u.addr(off), Size: uint32(len(b)), Source: &emulatorv1.Unit_Value{Value: b}}
}

// Store returns a store unit copying size bytes from src to dst.
func (u *U) Store(id, dst, src uint64, size uint32, continuous bool, tilt int64) *emulatorv1.Unit {
	return &emulatorv1.Unit{
		Id: id, Domain: u.s.Domain, Address: u.addr(dst), Size: size, Tilt: tilt,
		Source: &emulatorv1.Unit_Store{Store: &emulatorv1.StoreSource{Domain: u.s.Domain, Address: u.addr(src), Continuous: continuous}},
	}
}

func with(x *emulatorv1.Unit, f func(*emulatorv1.Unit)) *emulatorv1.Unit { f(x); return x }

func withMode(x *emulatorv1.Unit, m emulatorv1.Mode) *emulatorv1.Unit { x.Mode = m; return x }

// hard reports whether the emulator implements HARD units; cases that need
// them return early otherwise.
func (u *U) hard() bool {
	if !u.c.Info().GetCapabilities().GetHardUnits() {
		u.t.Log("emulator reports hard_units=false")
		return false
	}
	return true
}

// UnitCase is one scheduler scenario.
type UnitCase struct {
	Name string
	Run  func(u *U)
}

// UnitCases are the scheduler scenarios from design/emulator-api.md.
var UnitCases = []UnitCase{
	{"value lifetime 1", func(u *U) {
		u.Apply(with(u.Value(1, 0, le32(0xaabbccdd)), func(x *emulatorv1.Unit) { x.Lifetime = 1 }))
		u.Step(1)
		u.Want32(0, 0xaabbccdd)
		u.WantIDs()
		u.Put32(0, 0)
		u.Step(1)
		u.Want32(0, 0)
	}},
	{"value lifetime 3", func(u *U) {
		u.Apply(with(u.Value(1, 0, le32(7)), func(x *emulatorv1.Unit) { x.Lifetime = 3 }))
		for range 3 {
			u.Step(1)
			u.Want32(0, 7)
			u.Put32(0, 0)
		}
		u.WantIDs()
		u.Step(1)
		u.Want32(0, 0)
	}},
	{"value infinite", func(u *U) {
		u.Apply(u.Value(1, 0, le32(0x12345678)))
		u.Step(1)
		u.Want32(0, 0x12345678)
		u.Put32(0, 0)
		u.Step(1)
		u.Want32(0, 0x12345678)
		u.Put32(0, 0)
		u.Step(5)
		u.Want32(0, 0x12345678)
		u.WantIDs(1)
		must(u.t, u.c.RemoveUnits(u.ctx, []uint64{1}), "RemoveUnits")
		u.Put32(0, 0)
		u.Step(1)
		u.Want32(0, 0)
	}},
	{"store once with tilt", func(u *U) {
		u.Put32(4, 41)
		u.Apply(u.Store(1, 0, 4, 4, false, 1))
		u.Step(1)
		u.Want32(0, 42)
		u.Put32(4, 100)
		u.Step(1)
		u.Want32(0, 42)
		u.Put32(0, 0)
		u.Step(1)
		u.Want32(0, 42)
	}},
	{"tilt wraps around", func(u *U) {
		u.Put(0x40, []byte{0xff})
		u.Put(0x48, []byte{0x00, 0x00})
		u.Put(0x50, le32(0xffffffff))
		u.Put(0x58, binary.LittleEndian.AppendUint64(nil, 0x7fffffffffffffff))
		u.Put(0x61, []byte{0xee})
		u.Put(0x62, []byte{0xee, 0xee})
		u.Put(0x64, []byte{0xee})
		u.Put(0x78, []byte{0xee})
		u.Put(0x68, []byte{0xee, 0xee, 0xee, 0xee})
		units := []*emulatorv1.Unit{
			u.Store(1, 0x60, 0x40, 1, false, 2),
			u.Store(2, 0x62, 0x48, 2, false, -1),
			u.Store(3, 0x68, 0x50, 4, false, 1),
			u.Store(4, 0x70, 0x58, 8, true, 1),
		}
		for _, x := range units {
			x.Lifetime = 1
		}
		u.Apply(units...)
		u.Step(1)
		u.Want(0x60, []byte{0x01, 0xee})
		u.Want(0x62, []byte{0xff, 0xff, 0xee})
		u.Want(0x68, []byte{0, 0, 0, 0})
		u.Want(0x70, append(binary.LittleEndian.AppendUint64(nil, 0x8000000000000000), 0xee))
	}},
	{"store continuous pipe", func(u *U) {
		u.Put32(4, 1)
		u.Apply(u.Store(1, 0, 4, 4, true, 0))
		u.Step(1)
		u.Want32(0, 1)
		u.Put32(4, 2)
		u.Step(1)
		u.Want32(0, 2)
		u.Put32(4, 0xcafe)
		u.Step(3)
		u.Want32(0, 0xcafe)
	}},
	{"store continuous with tilt", func(u *U) {
		u.Put32(4, 10)
		u.Apply(u.Store(1, 0, 4, 4, true, -1))
		u.Step(1)
		u.Want32(0, 9)
		u.Put32(4, 0)
		u.Step(1)
		u.Want32(0, 0xffffffff)
	}},
	{"delay", func(u *U) {
		u.Apply(with(u.Value(1, 0, le32(5)), func(x *emulatorv1.Unit) { x.Delay = 5 }))
		u.Step(5)
		u.Want32(0, 0)
		u.Step(1)
		u.Want32(0, 5)
	}},
	{"delay with lifetime", func(u *U) {
		u.Apply(with(u.Value(1, 0, le32(9)), func(x *emulatorv1.Unit) { x.Delay = 2; x.Lifetime = 2 }))
		u.Step(2)
		u.Want32(0, 0)
		u.Step(1)
		u.Want32(0, 9)
		u.Put32(0, 0)
		u.Step(1)
		u.Want32(0, 9)
		u.Put32(0, 0)
		u.WantIDs()
		u.Step(1)
		u.Want32(0, 0)
	}},
	{"loop delay 0 re-executes next frame", func(u *U) {
		u.Apply(with(u.Value(1, 0, le32(3)), func(x *emulatorv1.Unit) { x.Delay = 2; x.Lifetime = 1; x.Loop = true }))
		u.Step(2)
		u.Want32(0, 0)
		for range 3 {
			u.Step(1)
			u.Want32(0, 3)
			u.Put32(0, 0)
		}
		u.WantIDs(1)
	}},
	{"loop delay", func(u *U) {
		u.Apply(with(u.Value(1, 0, le32(4)), func(x *emulatorv1.Unit) { x.Lifetime = 1; x.Loop = true; x.LoopDelay = 3 }))
		u.Step(1)
		u.Want32(0, 4)
		u.Put32(0, 0)
		for range 2 {
			u.Step(3)
			u.Want32(0, 0)
			u.Step(1)
			u.Want32(0, 4)
			u.Put32(0, 0)
		}
	}},
	{"apply order", func(u *U) {
		u.Apply(u.Value(1, 0, le32(1)), u.Value(2, 0, le32(2)))
		u.Apply(u.Value(3, 2, []byte{3}))
		u.Step(1)
		u.Want(0, []byte{2, 0, 3, 0})
	}},
	{"list remove clear", func(u *U) {
		u.Apply(u.Value(1, 0, le32(1)), u.Value(2, 4, le32(2)), with(u.Value(3, 8, le32(3)), func(x *emulatorv1.Unit) { x.Delay = 100 }))
		u.WantIDs(1, 2, 3)
		units, err := u.c.ListUnitsFull(u.ctx)
		must(u.t, err, "ListUnitsFull")
		if got := units[2]; got.GetDelay() != 100 || !bytes.Equal(got.GetValue(), le32(3)) || got.GetAddress() != u.addr(8) {
			u.t.Errorf("listed unit = %v", got)
		}
		must(u.t, u.c.RemoveUnits(u.ctx, []uint64{2, 12345}), "RemoveUnits")
		u.WantIDs(1, 3)
		u.Step(1)
		u.Want(0, append(le32(1), le32(0)...))
		must(u.t, u.c.ClearUnits(u.ctx), "ClearUnits")
		u.WantIDs()
		u.Put32(0, 0)
		u.Step(1)
		u.Want32(0, 0)
	}},
	{"list values elided", func(u *U) {
		value := with(u.Value(1, 0, le32(0x11223344)), func(x *emulatorv1.Unit) { x.Delay = 100 })
		store := with(u.Store(2, 4, 8, 4, true, -2), func(x *emulatorv1.Unit) { x.Delay = 100 })
		u.Apply(value, store)
		full, err := u.c.ListUnitsFull(u.ctx)
		must(u.t, err, "ListUnitsFull")
		if len(full) != 2 || !proto.Equal(full[0], value) || !proto.Equal(full[1], store) {
			u.t.Errorf("ListUnitsFull = %v, want [%v %v]", full, value, store)
		}
		elided, err := u.c.ListUnits(u.ctx)
		must(u.t, err, "ListUnits")
		want := proto.Clone(value).(*emulatorv1.Unit)
		want.Source = &emulatorv1.Unit_Value{Value: []byte{}}
		if len(elided) != 2 || !proto.Equal(elided[0], want) || !proto.Equal(elided[1], store) {
			u.t.Errorf("ListUnits = %v, want [%v %v]", elided, want, store)
		}
	}},
	{"modes accepted", func(u *U) {
		u.Apply(withMode(u.Value(1, 0, le32(1)), emulatorv1.Mode_SCANLINE), withMode(u.Value(2, 4, le32(2)), emulatorv1.Mode_HARD))
		u.Apply(withMode(u.Store(3, 8, 0, 4, true, 0), emulatorv1.Mode_SCANLINE), withMode(u.Store(4, 12, 4, 4, false, 1), emulatorv1.Mode_SCANLINE))
		units, err := u.c.ListUnits(u.ctx)
		must(u.t, err, "ListUnits")
		var modes []emulatorv1.Mode
		for _, x := range units {
			modes = append(modes, x.GetMode())
		}
		want := []emulatorv1.Mode{emulatorv1.Mode_SCANLINE, emulatorv1.Mode_HARD, emulatorv1.Mode_SCANLINE, emulatorv1.Mode_SCANLINE}
		if !slices.Equal(modes, want) {
			u.t.Errorf("listed modes = %v, want %v", modes, want)
		}
		u.Step(1)
		u.Want(0, slices.Concat(le32(1), le32(2), le32(1), le32(3)))
	}},
	{"mode errors", func(u *U) {
		err := u.c.ApplyUnits(u.ctx, []*emulatorv1.Unit{withMode(u.Store(1, 0, 4, 4, true, 0), emulatorv1.Mode_HARD)})
		WantCode(u.t, err, emulatorv1.Error_INVALID_ARGUMENT, "HARD continuous store unit")
		err = u.c.ApplyUnits(u.ctx, []*emulatorv1.Unit{withMode(u.Store(1, 0, 4, 4, false, 0), emulatorv1.Mode_HARD)})
		WantCode(u.t, err, emulatorv1.Error_INVALID_ARGUMENT, "HARD once store unit")
		err = u.c.ApplyUnits(u.ctx, []*emulatorv1.Unit{withMode(u.Value(1, 0, le32(1)), emulatorv1.Mode(7))})
		WantCode(u.t, err, emulatorv1.Error_INVALID_ARGUMENT, "unknown mode")
		err = u.c.ApplyUnits(u.ctx, []*emulatorv1.Unit{u.Value(1, 0, le32(1)), withMode(u.Store(2, 0, 4, 4, true, 0), emulatorv1.Mode_HARD)})
		WantCode(u.t, err, emulatorv1.Error_INVALID_ARGUMENT, "batch with a HARD store unit")
		u.WantIDs()
	}},
	{"hard masks client writes", func(u *U) {
		if !u.hard() {
			return
		}
		u.Apply(withMode(u.Value(1, 2, []byte{0xaa, 0xbb}), emulatorv1.Mode_HARD))
		// Not executing yet: nothing is frozen.
		u.Put32(0, 0x11223344)
		u.Want32(0, 0x11223344)
		u.Step(1)
		u.Want(0, []byte{0x44, 0x33, 0xaa, 0xbb})
		u.Put32(0, 0x55667788)
		u.Want(0, []byte{0x88, 0x77, 0xaa, 0xbb})
		u.Put(3, []byte{0x01, 0x02})
		u.Want(2, []byte{0xaa, 0xbb, 0x02})
		u.Step(2)
		u.Want(0, []byte{0x88, 0x77, 0xaa, 0xbb, 0x02})
		must(u.t, u.c.RemoveUnits(u.ctx, []uint64{1}), "RemoveUnits")
		u.Put32(0, 0)
		u.Want32(0, 0)
	}},
	{"hard lifetime", func(u *U) {
		if !u.hard() {
			return
		}
		u.Apply(withMode(with(u.Value(1, 0, le32(0xfeedface)), func(x *emulatorv1.Unit) { x.Lifetime = 2 }), emulatorv1.Mode_HARD))
		u.Step(1)
		u.Put32(0, 0)
		u.Want32(0, 0xfeedface)
		u.Step(1)
		u.WantIDs()
		u.Put32(0, 0)
		u.Want32(0, 0)
	}},
	{"errors", func(u *U) {
		u.Apply(u.Value(1, 0, le32(1)))
		err := u.c.ApplyUnits(u.ctx, []*emulatorv1.Unit{u.Value(1, 4, le32(1))})
		WantCode(u.t, err, emulatorv1.Error_INVALID_ARGUMENT, "duplicate id")
		err = u.c.ApplyUnits(u.ctx, []*emulatorv1.Unit{u.Value(2, 4, le32(1)), u.Value(2, 8, le32(1))})
		WantCode(u.t, err, emulatorv1.Error_INVALID_ARGUMENT, "duplicate id in one request")
		x := u.Value(3, 0, le32(1))
		x.Domain = "NoSuchDomain"
		err = u.c.ApplyUnits(u.ctx, []*emulatorv1.Unit{x})
		WantCode(u.t, err, emulatorv1.Error_NOT_FOUND, "unknown domain")
		x = u.Value(3, 0, le32(1))
		x.Address = 1 << 40
		err = u.c.ApplyUnits(u.ctx, []*emulatorv1.Unit{x})
		WantCode(u.t, err, emulatorv1.Error_OUT_OF_RANGE, "address out of range")
		x = u.Store(3, 0, 4, 4, false, 0)
		x.GetStore().Address = 1 << 40
		err = u.c.ApplyUnits(u.ctx, []*emulatorv1.Unit{x})
		WantCode(u.t, err, emulatorv1.Error_OUT_OF_RANGE, "store source out of range")
		err = u.c.ApplyUnits(u.ctx, []*emulatorv1.Unit{u.Store(3, 0, 4, 3, false, 1)})
		WantCode(u.t, err, emulatorv1.Error_INVALID_ARGUMENT, "tilt on 3 bytes")
		u.WantIDs(1)
	}},
}

// RunUnits runs UnitCases on a scratch area. A ROM must be loaded.
func RunUnits(t *testing.T, c *emu.Client, s Scratch) {
	for _, tc := range UnitCases {
		t.Run(tc.Name, func(t *testing.T) {
			u := &U{t: t, ctx: ctx(t), c: c, s: s}
			_, err := c.Step(u.ctx, 1)
			must(t, err, "Step")
			must(t, c.ClearUnits(u.ctx), "ClearUnits")
			u.Put(0, make([]byte, scratchSize))
			tc.Run(u)
			must(t, c.ClearUnits(u.ctx), "ClearUnits")
		})
	}
}
