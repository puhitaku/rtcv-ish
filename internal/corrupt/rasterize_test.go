package corrupt

import (
	"errors"
	"math/big"
	"slices"
	"testing"

	"google.golang.org/protobuf/proto"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
)

func TestRasterize(t *testing.T) {
	be := dom("BE", 64)
	be.BigEndian = true
	mem := newFakeMem(dom("A", 64), be)
	mem.put("A", 0, 0xFF, 0x00, 0x34, 0x12)
	mem.put("BE", 0, 0x12, 0x34)

	value := func(v []byte, bigEndian bool, tilt int64) *Unit {
		u := newValueUnit("A", 8, v, bigEndian, 1)
		if tilt != 0 {
			u.Tilt = big.NewInt(tilt)
		}
		return u
	}
	store := func(typ StoreType, tm StoreTime, src string, p int, bigEndian bool, tilt *big.Int) *Unit {
		u := newStoreUnit(typ, tm, "A", 8, src, 0, p, bigEndian, 0, 1)
		u.Tilt = tilt
		return u
	}
	huge, _ := new(big.Int).SetString("-100000000000000000000000", 10)
	vu := func(v ...byte) *emulatorv1.Unit {
		return &emulatorv1.Unit{Id: 1, Domain: "A", Address: 8, Size: uint32(len(v)), Lifetime: 1, Source: &emulatorv1.Unit_Value{Value: v}}
	}
	su := func(src string, size uint32, continuous bool, tilt int64) *emulatorv1.Unit {
		return &emulatorv1.Unit{Id: 1, Domain: "A", Address: 8, Size: size, Lifetime: 1, Tilt: tilt,
			Source: &emulatorv1.Unit_Store{Store: &emulatorv1.StoreSource{Domain: src, Address: 0, Continuous: continuous}}}
	}
	timed := value([]byte{1}, false, 0)
	timed.ExecuteFrame, timed.Lifetime, timed.Loop, timed.LoopTiming = 3, 0, true, 7
	timedWant := vu(1)
	timedWant.Delay, timedWant.Lifetime, timedWant.Loop, timedWant.LoopDelay = 3, 0, true, 7
	unsetLoop := value([]byte{1}, false, 0)
	unsetLoop.Loop, unsetLoop.ExecuteFrame = true, 5
	unsetLoopWant := vu(1)
	unsetLoopWant.Loop, unsetLoopWant.Delay, unsetLoopWant.LoopDelay = true, 5, 5
	zeroLoop := value([]byte{1}, false, 0)
	zeroLoop.Loop, zeroLoop.ExecuteFrame, zeroLoop.LoopTiming = true, 5, 0
	zeroLoopWant := vu(1)
	zeroLoopWant.Loop, zeroLoopWant.Delay = true, 5

	tests := []struct {
		name string
		unit *Unit
		want *emulatorv1.Unit
	}{
		{"value", value([]byte{0x01, 0x02}, false, 0), vu(0x01, 0x02)},
		{"value big-endian", value([]byte{0x01, 0x02}, true, 0), vu(0x02, 0x01)},
		{"value tilt", value([]byte{0xFF, 0x00}, false, 1), vu(0x00, 0x01)},
		{"value tilt big-endian", value([]byte{0xFF, 0x00}, true, 1), vu(0x01, 0x00)},
		{"value tilt wraps", value([]byte{0x00}, false, -1), vu(0xFF)},
		{"value tilt clamps", value([]byte{0x05}, false, 300), vu(0x04)},
		{"value tilt 8 bytes", &Unit{Enabled: true, Domain: "A", Address: 8, Precision: 8, Source: SourceValue, Value: Hex{0, 0, 0, 0, 0, 0, 0, 0}, Tilt: huge, Lifetime: 1, LoopTiming: -1, StoreTime: StoreImmediate, StoreType: StoreOnce, LimiterTime: LimiterNone}, vu(1, 0, 0, 0, 0, 0, 0, 0)},
		{"value tilt ignored on 3 bytes", value([]byte{1, 2, 3}, false, 1), vu(1, 2, 3)},
		{"immediate once", store(StoreOnce, StoreImmediate, "A", 2, false, nil), vu(0xFF, 0x00)},
		{"immediate once tilt", store(StoreOnce, StoreImmediate, "A", 2, false, big.NewInt(1)), vu(0x00, 0x01)},
		{"immediate once big-endian tilt", store(StoreOnce, StoreImmediate, "BE", 2, true, big.NewInt(-0x35)), vu(0x11, 0xFF)},
		{"preexecute once", store(StoreOnce, StorePreExecute, "A", 4, false, big.NewInt(-1)), su("A", 4, false, -1)},
		{"preexecute continuous", store(StoreContinuous, StorePreExecute, "BE", 2, true, nil), su("BE", 2, true, 0)},
		{"immediate continuous", store(StoreContinuous, StoreImmediate, "A", 1, false, big.NewInt(2)), su("A", 1, true, 2)},
		{"store tilt clamps", store(StoreOnce, StorePreExecute, "A", 1, false, big.NewInt(-1000)), su("A", 1, false, -255)},
		{"store tilt 8 bytes", store(StoreOnce, StorePreExecute, "A", 8, false, huge), su("A", 8, false, 1)},
		{"store tilt dropped on 3 bytes", store(StoreOnce, StorePreExecute, "A", 3, false, big.NewInt(1)), su("A", 3, false, 0)},
		{"timing", timed, timedWant},
		{"loop timing unset", unsetLoop, unsetLoopWant},
		{"loop timing zero", zeroLoop, zeroLoopWant},
	}
	for _, tt := range tests {
		got, err := Rasterize(t.Context(), &Layer{Units: []*Unit{tt.unit}}, mem, emulatorv1.Mode_FRAME, func() uint64 { return 1 })
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if len(got) != 1 || !proto.Equal(got[0], tt.want) {
			t.Errorf("%s:\n got %v\nwant %v", tt.name, got, tt.want)
		}
	}
}

func TestRasterizeSkipsAndIDs(t *testing.T) {
	mem := newFakeMem(dom("A", 16))
	disabled := newValueUnit("A", 0, []byte{1}, false, 1)
	disabled.Enabled = false
	l := &Layer{Units: []*Unit{
		newValueUnit("A", 0, []byte{1}, false, 1),
		disabled,
		newValueUnit("X", 0, []byte{1}, false, 1),
		newStoreUnit(StoreOnce, StoreImmediate, "A", 2, "A", 4, 2, false, 0, 1),
		newStoreUnit(StoreContinuous, StorePreExecute, "A", 4, "X", 4, 2, false, 0, 1),
		newValueUnit("A", 1, []byte{2}, false, 1),
	}}
	id := uint64(100)
	got, err := Rasterize(t.Context(), l, mem, emulatorv1.Mode_FRAME, func() uint64 { id++; return id })
	if err != nil {
		t.Fatal(err)
	}
	var addrs, ids []uint64
	for _, u := range got {
		addrs = append(addrs, u.GetAddress())
		ids = append(ids, u.GetId())
	}
	if len(got) != 3 || addrs[0] != 0 || addrs[1] != 2 || addrs[2] != 1 || ids[0] != 101 || ids[2] != 103 {
		t.Errorf("addresses %v ids %v", addrs, ids)
	}
	if len(mem.calls) != 1 {
		t.Errorf("ReadMany calls = %d", len(mem.calls))
	}
	bad := &Layer{Units: []*Unit{{Enabled: true, Domain: "A", Precision: 2, Source: SourceValue, Value: Hex{1}}}}
	if _, err := Rasterize(t.Context(), bad, mem, emulatorv1.Mode_FRAME, func() uint64 { return 1 }); err == nil {
		t.Error("invalid unit accepted")
	}
}

func TestRasterizeOutOfRange(t *testing.T) {
	mem := newFakeMem(dom("A", 16))
	ok := newValueUnit("A", 0, []byte{1}, false, 1)
	disabled := newValueUnit("A", 16, []byte{1}, false, 1)
	disabled.Enabled = false
	for _, tt := range []struct {
		name string
		u    *Unit
		want string
	}{
		{"target", newValueUnit("A", 15, []byte{1, 2}, false, 1), "unit 1: A 0xf+0x2 exceeds size 0x10"},
		{"store source", newStoreUnit(StoreOnce, StoreImmediate, "A", 0, "A", 15, 2, false, 0, 1), "unit 1: A 0xf+0x2 exceeds size 0x10"},
		{"address past end", newValueUnit("A", 1<<40, []byte{1}, false, 1), "unit 1: A 0x10000000000+0x1 exceeds size 0x10"},
		{"disabled", disabled, "unit 1: A 0x10+0x1 exceeds size 0x10"},
	} {
		l := &Layer{Units: []*Unit{ok, tt.u}}
		_, err := Rasterize(t.Context(), l, mem, emulatorv1.Mode_FRAME, func() uint64 { return 1 })
		var re *RangeError
		if !errors.As(err, &re) || !errors.Is(err, ErrOutOfRange) || err.Error() != tt.want {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
}

func TestRasterizeModes(t *testing.T) {
	mem := newFakeMem(dom("A", 64))
	l := &Layer{Units: []*Unit{
		newValueUnit("A", 0, []byte{1}, false, 0),
		newValueUnit("A", 1, []byte{1}, false, 5),
		newStoreUnit(StoreContinuous, StorePreExecute, "A", 2, "A", 8, 1, false, 0, 0),
		newStoreUnit(StoreOnce, StorePreExecute, "A", 3, "A", 8, 1, false, 0, 3),
		// Sampled now and sent as a value unit, so HARD applies.
		newStoreUnit(StoreOnce, StoreImmediate, "A", 4, "A", 8, 1, false, 0, 0),
	}}
	F, S, H := emulatorv1.Mode_FRAME, emulatorv1.Mode_SCANLINE, emulatorv1.Mode_HARD
	tests := []struct {
		infinite emulatorv1.Mode
		want     []emulatorv1.Mode
	}{
		{F, []emulatorv1.Mode{F, F, F, F, F}},
		{S, []emulatorv1.Mode{S, F, S, F, S}},
		{H, []emulatorv1.Mode{H, F, S, F, H}},
	}
	for _, tt := range tests {
		got, err := Rasterize(t.Context(), l, mem, tt.infinite, func() uint64 { return 1 })
		if err != nil {
			t.Fatal(err)
		}
		var modes []emulatorv1.Mode
		for _, u := range got {
			modes = append(modes, u.GetMode())
		}
		if !slices.Equal(modes, tt.want) {
			t.Errorf("infinite %v: modes %v, want %v", tt.infinite, modes, tt.want)
		}
	}
}

func TestInfiniteMode(t *testing.T) {
	F, S, H := emulatorv1.Mode_FRAME, emulatorv1.Mode_SCANLINE, emulatorv1.Mode_HARD
	tests := []struct {
		want           FreezeMode
		scanline, hard bool
		mode           emulatorv1.Mode
		fellBack       bool
	}{
		{FreezeHard, true, true, H, false},
		{FreezeHard, true, false, S, true},
		{FreezeHard, false, false, F, true},
		// An emulator may implement HARD without SCANLINE.
		{FreezeHard, false, true, H, false},
		{FreezeScanline, true, true, S, false},
		{FreezeScanline, false, true, F, true},
		{FreezeFrame, false, false, F, false},
		{FreezeFrame, true, true, F, false},
	}
	for _, tt := range tests {
		mode, fellBack := InfiniteMode(tt.want, tt.scanline, tt.hard)
		if mode != tt.mode || fellBack != tt.fellBack {
			t.Errorf("InfiniteMode(%s, scanline=%v, hard=%v) = %v, %v; want %v, %v",
				tt.want, tt.scanline, tt.hard, mode, fellBack, tt.mode, tt.fellBack)
		}
	}
}

func TestFreezeModeSetting(t *testing.T) {
	if m := DefaultSettings().FreezeMode; m != FreezeHard {
		t.Errorf("default freezeMode %q", m)
	}
	st := DefaultSettings()
	st.FreezeMode = "sometimes"
	if err := st.Validate(); err == nil {
		t.Error("invalid freezeMode accepted")
	}
	for _, m := range []FreezeMode{FreezeFrame, FreezeScanline, FreezeHard} {
		st.FreezeMode = m
		if err := st.Validate(); err != nil {
			t.Errorf("%s: %v", m, err)
		}
	}
}
