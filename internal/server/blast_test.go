package server_test

import (
	"net/http"
	"reflect"
	"slices"
	"testing"

	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

func TestManualBlast(t *testing.T) {
	cases := []struct {
		name      string
		precision gen.Precision
		alignment int
		ranges    *gen.PrecisionRangesPatch
		wantValue string // every unit's value, when fixed by the range
	}{
		{name: "precision 1", precision: gen.PrecisionN1},
		{name: "precision 2", precision: gen.PrecisionN2},
		{name: "precision 4 aligned", precision: gen.PrecisionN4},
		{name: "precision 4 alignment 2", precision: gen.PrecisionN4, alignment: 2},
		{name: "precision 8 alignment 7", precision: gen.PrecisionN8, alignment: 7},
		{
			name: "fixed range", precision: gen.PrecisionN2,
			ranges:    &gen.PrecisionRangesPatch{P2: &gen.ValueRange{Min: "4660", Max: "4660"}},
			wantValue: "3412", // 0x1234 little-endian
		},
	}
	const intensity = 16
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, envOptions{})
			e.useNightmare(intensity)
			e.patchSettings(gen.SettingsPatch{
				Precision: ptr(tc.precision),
				Alignment: ptr(tc.alignment),
				Nightmare: &gen.NightmareSettingsPatch{Ranges: tc.ranges},
			})
			p := int(tc.precision)

			layer := e.blast()
			if len(layer.Units) != intensity {
				t.Fatalf("blast returned %d units, want %d", len(layer.Units), intensity)
			}
			for i, u := range layer.Units {
				if u.Domain != vram || u.Source != gen.UnitSourceValue || u.Precision != p || !u.Enabled ||
					u.Lifetime != 1 || u.ExecuteFrame != 0 || u.Loop || u.Tilt != "0" {
					t.Errorf("unit %d = %+v, want an enabled VRAM value unit of precision %d, lifetime 1", i, u, p)
				}
				if len(u.Value) != 2*p {
					t.Errorf("unit %d value %q is not %d bytes", i, u.Value, p)
				}
				if tc.wantValue != "" && u.Value != tc.wantValue {
					t.Errorf("unit %d value %q, want %q", i, u.Value, tc.wantValue)
				}
				if u.Address < 0 || u.Address > vramSize-int64(p) || int(u.Address%int64(p)) != tc.alignment {
					t.Errorf("unit %d address %#x: want in range and ≡ %d mod %d", i, u.Address, tc.alignment, p)
				}
			}

			// The fake runs no frames by itself, so the units are still scheduled.
			units := e.units()
			if len(units) != intensity {
				t.Fatalf("scheduled %d units, want %d", len(units), intensity)
			}
			if got, want := unitTargets(units), layerTargets(layer); !slices.Equal(got, want) {
				t.Errorf("scheduled targets %v, want %v", got, want)
			}
			for _, u := range units {
				if u.Size != int64(p) || u.Value == nil || u.Store != nil || u.Lifetime != 1 || u.Delay != 0 {
					t.Errorf("scheduled unit %+v", u)
				}
			}

			// After a frame the lifetime-1 units have run and are gone.
			e.fake.Tick(1)
			if n := len(e.units()); n != 0 {
				t.Errorf("%d units left after one frame", n)
			}
		})
	}
}

func TestManualBlastNoDomains(t *testing.T) {
	e := newEnv(t, envOptions{})
	e.selectDomains()
	r, err := e.c.ManualBlastWithResponse(e.ctx)
	expectError(t, r, err, http.StatusBadRequest, "INVALID_ARGUMENT")
}

// The same seed and the same requests produce the same layers.
func TestBlastReproducible(t *testing.T) {
	run := func(seed int64) []gen.Layer {
		e := newEnv(t, envOptions{seed: seed})
		e.useNightmare(16)
		e.patchSettings(gen.SettingsPatch{Precision: ptr(gen.PrecisionN4)})
		return []gen.Layer{e.blast(), e.blast(), e.blast()}
	}
	a, b := run(7), run(7)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("same seed, different layers:\n%+v\n%+v", a, b)
	}
	if reflect.DeepEqual(a[0], a[1]) {
		t.Error("two consecutive blasts produced the same layer")
	}
	if c := run(8); reflect.DeepEqual(a, c) {
		t.Error("different seeds produced the same layers")
	}
}

// With errorDelay=3, nine frames give three blasts.
func TestAutoCorrupt(t *testing.T) {
	e := newEnv(t, envOptions{})
	e.useNightmare(2)
	e.patchSettings(gen.SettingsPatch{ErrorDelay: ptr(int64(3))})
	events := e.openEvents()
	if ev := events.next(t); ev.Type != "status" {
		t.Fatalf("first event %q, want status", ev.Type)
	}

	e.patchSettings(gen.SettingsPatch{AutoCorrupt: ptr(true)})
	e.fake.Tick(9)
	for i := range 3 {
		ev := events.waitFor(t, "blast", nil)
		b := decodeEvent[gen.BlastEvent](t, ev)
		if b.Count != 2 || b.Engine != gen.EngineNightmare || b.ElapsedMs < 0 {
			t.Errorf("blast event %d = %+v", i, b)
		}
	}
	// The fake holds its lock for the whole Tick, so all three layers were
	// applied after frame 9 and none has run yet.
	if n := len(e.units()); n != 6 {
		t.Errorf("%d units scheduled, want 6", n)
	}

	e.patchSettings(gen.SettingsPatch{AutoCorrupt: ptr(false)})
	e.clearUnits()
	e.fake.Tick(9)
	if n := len(e.units()); n != 0 {
		t.Errorf("auto-corrupt off: %d units scheduled", n)
	}
}

func TestApplyLayerUnits(t *testing.T) {
	e := newEnv(t, envOptions{})
	e.writeMem(vram, 0x30, "beef")

	type want struct {
		size      int64
		value     string // "" = store unit
		store     *gen.EmuStoreSource
		tilt      int64
		delay     int64
		lifetime  int64
		loop      bool
		loopDelay int64
	}
	loopUnit := valueUnit(vram, 0x10, "1234")
	loopUnit.ExecuteFrame, loopUnit.Lifetime, loopUnit.Loop, loopUnit.LoopTiming = 3, 5, true, ptr(7)
	tiltUp := valueUnit(vram, 0x10, "00")
	tiltUp.Tilt = "1"
	tiltDown := valueUnit(vram, 0x10, "00")
	tiltDown.Tilt = "-1"
	bigEndian := valueUnit(vram, 0x10, "1234")
	bigEndian.BigEndian = true
	infinite := valueUnit(vram, 0x10, "aa")
	infinite.Lifetime = 0
	continuous := storeUnit(vram, 0x10, 2, vram, 0x20, gen.StoreTimePreexecute, gen.StoreTypeContinuous)
	continuous.Tilt = "2"
	continuous.Lifetime = 0

	cases := []struct {
		name string
		unit gen.Unit
		want want
	}{
		{"value", valueUnit(vram, 0x10, "12345678"), want{size: 4, value: "12345678", lifetime: 1}},
		{"timing", loopUnit, want{size: 2, value: "1234", delay: 3, lifetime: 5, loop: true, loopDelay: 7}},
		{"tilt +1", tiltUp, want{size: 1, value: "01", lifetime: 1}},
		{"tilt -1", tiltDown, want{size: 1, value: "ff", lifetime: 1}},
		{"big endian", bigEndian, want{size: 2, value: "3412", lifetime: 1}},
		{"infinite", infinite, want{size: 1, value: "aa", lifetime: 0}},
		{
			"store preexecute once", storeUnit(vram, 0x10, 2, vram, 0x20, gen.StoreTimePreexecute, gen.StoreTypeOnce),
			want{size: 2, store: &gen.EmuStoreSource{Domain: vram, Address: 0x20}, lifetime: 1},
		},
		{"store continuous", continuous, want{size: 2, store: &gen.EmuStoreSource{Domain: vram, Address: 0x20, Continuous: true}, tilt: 2, lifetime: 0}},
		// IMMEDIATE samples the source when the layer is applied.
		{"store immediate", storeUnit(vram, 0x10, 2, vram, 0x30, gen.StoreTimeImmediate, gen.StoreTypeOnce), want{size: 2, value: "beef", lifetime: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e.clearUnits()
			e.applyLayer(gen.Layer{Units: []gen.Unit{tc.unit}}, false)
			us := e.units()
			if len(us) != 1 {
				t.Fatalf("scheduled %d units, want 1: %+v", len(us), us)
			}
			u := us[0]
			if u.Domain != vram || u.Address != 0x10 || u.Size != tc.want.size || u.Tilt != tc.want.tilt ||
				u.Delay != tc.want.delay || u.Lifetime != tc.want.lifetime || u.Loop != tc.want.loop || u.LoopDelay != tc.want.loopDelay {
				t.Errorf("scheduled %+v, want %+v", u, tc.want)
			}
			if tc.want.value != "" && (u.Value == nil || *u.Value != tc.want.value || u.Store != nil) {
				t.Errorf("value/store = %v/%+v, want value %q", u.Value, u.Store, tc.want.value)
			}
			if tc.want.store != nil && (u.Store == nil || *u.Store != *tc.want.store || u.Value != nil) {
				t.Errorf("value/store = %v/%+v, want store %+v", u.Value, u.Store, *tc.want.store)
			}
		})
	}

	t.Run("disabled units are skipped", func(t *testing.T) {
		e.clearUnits()
		off := valueUnit(vram, 0x40, "01")
		off.Enabled = false
		e.applyLayer(gen.Layer{Units: []gen.Unit{off, valueUnit(vram, 0x41, "02")}}, false)
		if us := e.units(); len(us) != 1 || us[0].Address != 0x41 {
			t.Errorf("scheduled %+v, want only the enabled unit", us)
		}
	})

	errs := []struct {
		name   string
		unit   gen.Unit
		status int
		code   string
	}{
		{"unknown domain", valueUnit("Nope", 0, "00"), http.StatusNotFound, "NOT_FOUND"},
		{"out of range", valueUnit(vram, vramSize-1, "0000"), http.StatusBadRequest, "OUT_OF_RANGE"},
		{"value length != precision", func() gen.Unit { u := valueUnit(vram, 0, "0000"); u.Precision = 4; return u }(), http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"bad tilt", func() gen.Unit { u := valueUnit(vram, 0, "00"); u.Tilt = "x"; return u }(), http.StatusBadRequest, "INVALID_ARGUMENT"},
	}
	for _, tc := range errs {
		t.Run(tc.name, func(t *testing.T) {
			e.clearUnits()
			r, err := e.c.ApplyLayerWithResponse(e.ctx, gen.ApplyRequest{Layer: gen.Layer{Units: []gen.Unit{valueUnit(vram, 0x50, "01"), tc.unit}}})
			expectError(t, r, err, tc.status, tc.code)
			if n := len(e.units()); n != 0 {
				t.Errorf("a rejected layer scheduled %d units", n)
			}
		})
	}
}

func TestClearUnits(t *testing.T) {
	e := newEnv(t, envOptions{})
	u := valueUnit(vram, 0x100, "aa")
	u.Lifetime = 0
	e.applyLayer(gen.Layer{Units: []gen.Unit{u, valueUnit(vram, 0x101, "bb")}}, false)
	if n := len(e.units()); n != 2 {
		t.Fatalf("%d units, want 2", n)
	}
	e.fake.Tick(1)
	if n := len(e.units()); n != 1 {
		t.Fatalf("after a frame: %d units, want the infinite one", n)
	}
	e.clearUnits()
	if n := len(e.units()); n != 0 {
		t.Errorf("after clear: %d units", n)
	}
}

// BlastLayer ON/OFF: off restores the bytes saved when the layer was
// applied with backup, on re-applies the layer.
func TestToggle(t *testing.T) {
	e := newEnv(t, envOptions{})

	r, err := e.c.ToggleLayerWithResponse(e.ctx, gen.ToggleRequest{On: false})
	expectError(t, r, err, http.StatusConflict, "NO_BACKUP")

	e.writeMem(vram, 0x100, "11223344")
	e.writeMem(vram, 0x200, "55")
	layer := gen.Layer{Note: "toggle", Units: []gen.Unit{valueUnit(vram, 0x100, "aabbccdd"), valueUnit(vram, 0x200, "66")}}
	e.applyLayer(layer, true)
	if st := e.status(); !st.BlastLayer.Available || !st.BlastLayer.On {
		t.Errorf("after apply with backup: blastLayer = %+v", st.BlastLayer)
	}
	e.fake.Tick(1)
	if got := e.readMem(vram, 0x100, 4) + e.readMem(vram, 0x200, 1); got != "aabbccdd66" {
		t.Fatalf("after apply: %s", got)
	}

	for i, tc := range []struct {
		on   bool
		want string
	}{{false, "1122334455"}, {true, "aabbccdd66"}, {false, "1122334455"}} {
		r, err := e.c.ToggleLayerWithResponse(e.ctx, gen.ToggleRequest{On: tc.on})
		expectStatus(t, r, err, http.StatusNoContent)
		e.fake.Tick(1)
		if got := e.readMem(vram, 0x100, 4) + e.readMem(vram, 0x200, 1); got != tc.want {
			t.Errorf("step %d toggle on=%v: memory %s, want %s", i, tc.on, got, tc.want)
		}
		if st := e.status(); !st.BlastLayer.Available || st.BlastLayer.On != tc.on {
			t.Errorf("step %d: blastLayer = %+v", i, st.BlastLayer)
		}
	}

	// A layer applied without backup does not replace the toggle pair.
	e.applyLayer(gen.Layer{Units: []gen.Unit{valueUnit(vram, 0x300, "77")}}, false)
	e.fake.Tick(1)
	r, err = e.c.ToggleLayerWithResponse(e.ctx, gen.ToggleRequest{On: true})
	expectStatus(t, r, err, http.StatusNoContent)
	e.fake.Tick(1)
	if got := e.readMem(vram, 0x100, 4); got != "aabbccdd" {
		t.Errorf("toggle on after an unbacked apply: %s", got)
	}
}

func TestRerollLayer(t *testing.T) {
	e := newEnv(t, envOptions{})
	var units []gen.Unit
	for i := range 8 {
		units = append(units, valueUnit(vram, int64(0x100+4*i), "00000000"))
	}
	locked := valueUnit(vram, 0x200, "00000000")
	locked.Locked = true
	units = append(units, locked)
	in := gen.Layer{Note: "n", Units: units}

	reroll := func() gen.Layer {
		t.Helper()
		r, err := e.c.RerollLayerWithResponse(e.ctx, gen.LayerRequest{Layer: in})
		expectStatus(t, r, err, http.StatusOK)
		return *r.JSON200
	}

	t.Run("values only", func(t *testing.T) {
		out := reroll()
		if len(out.Units) != len(in.Units) || out.Note != in.Note {
			t.Fatalf("rerolled layer = %+v", out)
		}
		changed := 0
		for i, u := range out.Units {
			if u.Domain != in.Units[i].Domain || u.Address != in.Units[i].Address || u.Precision != 4 || len(u.Value) != 8 {
				t.Errorf("unit %d: %+v, want same target and precision", i, u)
			}
			if u.Value != in.Units[i].Value {
				changed++
			}
		}
		if changed == 0 {
			t.Error("no value changed")
		}
		if last := out.Units[len(out.Units)-1]; !reflect.DeepEqual(last, locked) {
			t.Errorf("locked unit changed: %+v", last)
		}
	})

	t.Run("addresses", func(t *testing.T) {
		e.patchSettings(gen.SettingsPatch{Reroll: &gen.RerollSettingsPatch{Address: ptr(true)}})
		out := reroll()
		moved := 0
		for i, u := range out.Units[:8] {
			if u.Domain != vram || u.Address < 0 || u.Address > vramSize-4 {
				t.Errorf("unit %d: %+v", i, u)
			}
			if u.Address != in.Units[i].Address {
				moved++
			}
		}
		if moved == 0 {
			t.Error("reroll.address=true moved no unit")
		}
		if last := out.Units[8]; !reflect.DeepEqual(last, locked) {
			t.Errorf("locked unit changed: %+v", last)
		}
	})

	t.Run("does not apply", func(t *testing.T) {
		if n := len(e.units()); n != 0 {
			t.Errorf("reroll scheduled %d units", n)
		}
	})
}
