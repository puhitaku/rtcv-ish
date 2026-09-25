package server_test

import (
	"encoding/hex"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/puhitaku/rtcv-ish/internal/emu/fake"
	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

const (
	bigRAM     = "BigRAM"
	bigRAMSize = 20 << 20
)

func bigEnv(t *testing.T) *env {
	return newEnv(t, envOptions{fake: fake.Options{
		Domains: append(fake.DefaultDomains(), fake.Domain{Name: bigRAM, Size: bigRAMSize, WordSize: 4}),
	}})
}

// patternHex is n bytes 0, 1, 2, ... as hex.
func patternHex(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return hex.EncodeToString(b)
}

// A unit far above RTCV's 16348-byte cap applies and writes all its bytes.
func TestApplyLargeUnit(t *testing.T) {
	e := bigEnv(t)
	for _, size := range []int{1 << 20, 16 << 20} {
		value := patternHex(size)
		const addr = 0x10
		e.applyLayer(gen.Layer{Units: []gen.Unit{valueUnit(bigRAM, addr, value)}}, false)
		us := e.units()
		if len(us) != 1 || us[0].Size != int64(size) {
			t.Fatalf("size %d: units = %d", size, len(us))
		}
		e.fake.Tick(1)
		for _, off := range []int{0, size/2 - 8, size - 16} {
			if got, want := e.readMem(bigRAM, int64(addr+off), 16), value[2*off:2*off+32]; got != want {
				t.Errorf("size %d: bytes at +0x%x = %s, want %s", size, off, got, want)
			}
		}
		e.clearUnits()
	}
}

// Listing units omits values, so several maximum-size units, together
// far above the emulator API message cap, still list.
func TestListLargeUnits(t *testing.T) {
	const (
		hugeRAM = "HugeRAM"
		size    = 16 << 20
		count   = 5
	)
	e := newEnv(t, envOptions{fake: fake.Options{
		Domains: append(fake.DefaultDomains(), fake.Domain{Name: hugeRAM, Size: 100 << 20, WordSize: 4}),
	}})
	value := patternHex(size)
	for i := range count {
		e.applyLayer(gen.Layer{Units: []gen.Unit{valueUnit(hugeRAM, int64(i*size), value)}}, false)
	}
	us := e.units()
	if len(us) != count {
		t.Fatalf("listed %d units, want %d", len(us), count)
	}
	for i, u := range us {
		if u.Domain != hugeRAM || u.Address != int64(i*size) || u.Size != size || u.Value == nil || *u.Value != "" || u.Store != nil {
			t.Errorf("unit %d = %+v, want a %d-byte value unit at 0x%x without its value", i, u, size, i*size)
		}
	}

	r, err := e.c.RemoveUnitWithResponse(e.ctx, us[1].Id)
	expectStatus(t, r, err, http.StatusNoContent)
	if n := len(e.units()); n != count-1 {
		t.Errorf("listed %d units after removing one, want %d", n, count-1)
	}
	e.fake.Tick(1)
	if got, want := e.readMem(hugeRAM, 4*size+size-16, 16), value[2*(size-16):]; got != want {
		t.Errorf("last unit's tail = %s, want %s", got, want)
	}
}

func TestApplyPrecisionLimits(t *testing.T) {
	e := bigEnv(t)
	u := valueUnit(bigRAM, 0, "00")
	u.Precision = 16<<20 + 1
	r, err := e.c.ApplyLayerWithResponse(e.ctx, gen.ApplyRequest{Layer: gen.Layer{Units: []gen.Unit{u}}})
	expectError(t, r, err, http.StatusBadRequest, "INVALID_ARGUMENT")
	if !strings.Contains(string(r.Bytes()), "precision") {
		t.Errorf("message does not name precision: %s", r.Bytes())
	}
}

func TestLayerOutOfRange(t *testing.T) {
	e := bigEnv(t)
	e.saveSlot(1)
	k := e.corrupt(1)
	ok := valueUnit(vram, 0, "01")
	large := valueUnit(bigRAM, 0x100, patternHex(1<<20))

	t.Run("put large", func(t *testing.T) {
		l := gen.Layer{Note: "large", Units: []gen.Unit{ok, large}}
		r, err := e.c.PutStashLayerWithResponse(e.ctx, k.Key, l)
		expectStatus(t, r, err, http.StatusOK)
		if got := e.stashLayer(k.Key); len(got.Units) != 2 || got.Units[1].Value != large.Value || got.Units[1].Precision != 1<<20 {
			t.Errorf("GET after PUT: %d units", len(got.Units))
		}
	})

	store := valueUnit(vram, 0, "")
	store.Source = gen.UnitSourceStore
	store.Precision = 0x20
	store.SourceDomain = bigRAM
	store.SourceAddress = bigRAMSize - 0x10

	disabled := valueUnit(vram, vramSize, "01")
	disabled.Enabled = false

	cases := []struct {
		name string
		unit gen.Unit
		want string
	}{
		{"large past end", valueUnit(bigRAM, bigRAMSize-(1<<20)+1, patternHex(1<<20)), "unit 1: BigRAM 0x1300001+0x100000 exceeds size 0x1400000"},
		{"one byte past end", valueUnit(vram, vramSize-1, "0000"), "unit 1: VRAM 0x3fff+0x2 exceeds size 0x4000"},
		{"store source", store, "unit 1: BigRAM 0x13ffff0+0x20 exceeds size 0x1400000"},
		{"disabled", disabled, "unit 1: VRAM 0x4000+0x1 exceeds size 0x4000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := gen.Layer{Units: []gen.Unit{ok, tc.unit}}
			calls := map[string]func() (response, error){
				"put": func() (response, error) { return e.c.PutStashLayerWithResponse(e.ctx, k.Key, l) },
				"apply": func() (response, error) {
					return e.c.ApplyLayerWithResponse(e.ctx, gen.ApplyRequest{Layer: l, Backup: true})
				},
			}
			for _, name := range slices.Sorted(maps.Keys(calls)) {
				e.clearUnits()
				r, err := calls[name]()
				expectError(t, r, err, http.StatusBadRequest, "OUT_OF_RANGE")
				if !strings.Contains(string(r.Bytes()), tc.want) {
					t.Errorf("%s: body %s does not contain %q", name, r.Bytes(), tc.want)
				}
				if n := len(e.units()); n != 0 {
					t.Errorf("%s: a rejected layer scheduled %d units", name, n)
				}
			}
		})
	}
}
