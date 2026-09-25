package corrupt

import (
	"fmt"
	"testing"
)

func TestAddressRangeValidate(t *testing.T) {
	for _, r := range []AddressRange{{Start: 5, End: 5}, {Start: 6, End: 5}, {Start: -1, End: 5}, {Enabled: true, Start: 0x10, End: 0x10}} {
		if err := settings(func(s *Settings) { s.AddressRange = r }).Validate(); err == nil {
			t.Errorf("%+v accepted", r)
		}
	}
	if err := settings(func(s *Settings) { s.AddressRange = AddressRange{Enabled: true, Start: 0x10, End: 0x11} }).Validate(); err != nil {
		t.Error(err)
	}
}

func TestAddressRangeWindow(t *testing.T) {
	tests := []struct {
		r      AddressRange
		size   uint64
		lo, hi int64
	}{
		{AddressRange{Start: 0x10, End: 0x20}, 0x100, 0, 0x100},
		{AddressRange{Enabled: true, Start: 0x10, End: 0x20}, 0x100, 0x10, 0x20},
		{AddressRange{Enabled: true, Start: 0x10, End: 0x200}, 0x100, 0x10, 0x100},
		{AddressRange{Enabled: true, Start: 0x200, End: 0x300}, 0x100, 0x200, 0x200},
	}
	for _, tc := range tests {
		if lo, hi := tc.r.window(tc.size); lo != tc.lo || hi != tc.hi {
			t.Errorf("%+v size %#x: [%#x, %#x), want [%#x, %#x)", tc.r, tc.size, lo, hi, tc.lo, tc.hi)
		}
	}
}

func TestAlignIn(t *testing.T) {
	tests := []struct {
		addr, lo, hi, p, a, n int64
		want                  uint64
		ok                    bool
	}{
		{0x13, 0x10, 0x20, 4, 0, 4, 0x10, true},
		{0x0E, 0x0F, 0x20, 4, 0, 4, 0x10, true},  // below lo: next aligned
		{0x1E, 0x10, 0x20, 4, 1, 4, 0x19, true},  // past hi: last aligned that fits
		{0x19, 0x10, 0x20, 4, 0, 12, 0x14, true}, // chunk of 12
		{0x10, 0x11, 0x14, 4, 0, 4, 0, false},    // no aligned slot inside
	}
	for _, tc := range tests {
		got, ok := alignIn(tc.addr, tc.lo, tc.hi, tc.p, tc.a, tc.n)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("alignIn(%#x, [%#x,%#x), p%d a%d n%d) = %#x %v, want %#x %v", tc.addr, tc.lo, tc.hi, tc.p, tc.a, tc.n, got, ok, tc.want, tc.ok)
		}
	}
}

// Every unit (and store source) of every engine and radius stays inside
// the range intersected with its domain.
func TestAddressRangeGenerate(t *testing.T) {
	mem := newFakeMem(dom("big", 0x1000), dom("small", 0x400))
	sizes := map[string]int64{"big": 0x1000, "small": 0x400}
	reg := registry(t, map[string]string{"zero32": "00000000\n", "zero8": "00\n"})
	engines := map[string]func(*Settings){
		"nightmare":  func(s *Settings) { s.Engine = EngineNightmare },
		"tilt":       func(s *Settings) { s.Engine, s.Nightmare.Algo = EngineNightmare, NightmareTilt },
		"hellgenie":  func(s *Settings) { s.Engine = EngineHellgenie },
		"distortion": func(s *Settings) { s.Engine = EngineDistortion },
		"freeze":     func(s *Settings) { s.Engine = EngineFreeze },
		"pipe":       func(s *Settings) { s.Engine = EnginePipe },
		"vector": func(s *Settings) {
			s.Engine, s.Vector.LimiterList, s.Vector.ValueList = EngineVector, "zero32", "zero32"
		},
		"cluster": func(s *Settings) {
			s.Engine, s.Cluster.LimiterList, s.Cluster.SplitUnits = EngineCluster, "zero8", false
		},
		"custom store": func(s *Settings) { s.Engine, s.Custom.Source = EngineCustom, SourceStore },
		"custom value": func(s *Settings) { s.Engine = EngineCustom },
	}
	ranges := []AddressRange{
		{Enabled: true, Start: 0x301, End: 0x803},  // "small" clipped to [0x301, 0x400)
		{Enabled: true, Start: 0x3FE, End: 0x2000}, // "small" has 2 bytes: no units there
	}
	for _, r := range ranges {
		for name, eng := range engines {
			for _, radius := range []Radius{RadiusSpread, RadiusChunk, RadiusBurst, RadiusNormalized, RadiusProportional, RadiusEven} {
				t.Run(fmt.Sprintf("%#x-%#x/%s/%s", r.Start, r.End, name, radius), func(t *testing.T) {
					s := settings(func(s *Settings) {
						s.Intensity, s.MaxInfiniteUnits, s.Radius, s.Precision, s.Alignment, s.AddressRange = 200, 200, radius, 4, 2, r
						eng(s)
					})
					l := mustGenerate(t, s, []string{"big", "small"}, mem, reg)
					if len(l.Units) == 0 {
						t.Fatal("no units")
					}
					check := func(what, domain string, addr uint64, n int) {
						hi := min(r.End, sizes[domain])
						if int64(addr) < r.Start || int64(addr)+int64(n) > hi {
							t.Fatalf("%s %s:%#x+%d outside [%#x, %#x)", what, domain, addr, n, r.Start, hi)
						}
					}
					for _, u := range l.Units {
						check("unit", u.Domain, u.Address, u.Precision)
						if u.Source == SourceStore {
							check("source", u.SourceDomain, u.SourceAddress, u.Precision)
						}
					}
				})
			}
		}
	}
}

func TestAddressRangeNoDomain(t *testing.T) {
	mem := newFakeMem(dom("big", 0x1000), dom("small", 0x400))
	for _, r := range []AddressRange{{Enabled: true, Start: 0x1000, End: 0x2000}, {Enabled: true, Start: 0xFFE, End: 0x2000}} {
		s := settings(func(s *Settings) { s.Intensity, s.Precision, s.AddressRange = 50, 4, r })
		l := mustGenerate(t, s, []string{"big", "small"}, mem, nil)
		if len(l.Units) != 0 {
			t.Errorf("%+v: %d units, want none", r, len(l.Units))
		}
		if !AddressRangeMisses(s, []string{"big", "small"}, mem) {
			t.Errorf("%+v: AddressRangeMisses = false", r)
		}
	}
	s := settings(func(s *Settings) { s.AddressRange = AddressRange{Enabled: true, Start: 0x10, End: 0x20} })
	if AddressRangeMisses(s, []string{"small"}, mem) {
		t.Error("AddressRangeMisses = true for an intersecting range")
	}
}

func TestAddressRangeReroll(t *testing.T) {
	mem := newFakeMem(dom("big", 0x1000), dom("small", 0x400))
	s := settings(func(s *Settings) {
		s.Reroll = RerollSettings{Address: true, Domain: true, SourceAddress: true, SourceDomain: true}
		s.AddressRange = AddressRange{Enabled: true, Start: 0x3FE, End: 0x500}
	})
	l := &Layer{}
	for range 100 {
		u := NewUnit()
		u.Domain, u.Precision, u.Source, u.SourceDomain = "small", 4, SourceStore, "small"
		l.Units = append(l.Units, u)
	}
	if err := l.Reroll(seeded(), s, []string{"big", "small"}, mem, nil); err != nil {
		t.Fatal(err)
	}
	for _, u := range l.Units {
		if u.Domain != "big" || u.SourceDomain != "big" {
			t.Fatalf("rerolled into %s/%s, want big (small has no room)", u.Domain, u.SourceDomain)
		}
		for _, a := range []uint64{u.Address, u.SourceAddress} {
			if a < 0x3FE || a+4 > 0x500 {
				t.Fatalf("address %#x outside the range", a)
			}
		}
	}
}
