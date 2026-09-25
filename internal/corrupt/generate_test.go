package corrupt

import (
	"bytes"
	"errors"
	"math/big"
	"reflect"
	"testing"
)

func settings(f func(*Settings)) *Settings {
	s := DefaultSettings()
	f(s)
	return s
}

func TestDefaultSettings(t *testing.T) {
	s := DefaultSettings()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if s.Nightmare.Ranges.Max64 != 1<<64-1 || s.Custom.Ranges != FullRange() || s.Custom.Lifetime != 1 || !s.Cluster.SplitUnits {
		t.Errorf("unexpected defaults %+v", s)
	}
	bad := []func(*Settings){
		func(s *Settings) { s.Engine = "x" },
		func(s *Settings) { s.Intensity = 0 },
		func(s *Settings) { s.ErrorDelay = 0 },
		func(s *Settings) { s.Radius = "x" },
		func(s *Settings) { s.Precision = 3 },
		func(s *Settings) { s.Alignment = 1 },
		func(s *Settings) { s.MaxInfiniteUnits = 0 },
		func(s *Settings) { s.Nightmare.Algo = "x" },
		func(s *Settings) { s.Nightmare.Ranges.Min8 = 5; s.Nightmare.Ranges.Max8 = 4 },
		func(s *Settings) { s.Hellgenie.Ranges.Max8 = 256 },
		func(s *Settings) { s.Distortion.Delay = -1 },
		func(s *Settings) { s.Cluster.ChunkSize = 0 },
		func(s *Settings) { s.Cluster.Method = "x" },
		func(s *Settings) { s.Cluster.Direction = "x" },
		func(s *Settings) { s.Custom.LimiterTime = "x" },
		func(s *Settings) { s.Custom.Lifetime = -1 },
		func(s *Settings) { s.GameProtection.Keep = 0 },
	}
	for i, f := range bad {
		if err := settings(f).Validate(); err == nil {
			t.Errorf("case %d: invalid settings accepted", i)
		}
	}
}

func TestSettingsJSON(t *testing.T) {
	s := DefaultSettings()
	s.Custom.Tilt = big.NewInt(-3)
	data, err := jsonMarshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"8":{"max":"18446744073709551615","min":"0"}`, `"1":{"max":"255","min":"0"}`, `"tilt":"-3"`, `"splitUnits":true`, `"algo":"random"`} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("JSON lacks %s: %s", want, data)
		}
	}
	var got Settings
	if err := jsonUnmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&got, s) {
		t.Errorf("round trip:\n got %+v\nwant %+v", got, s)
	}
	// Partial updates merge into the current values.
	if err := jsonUnmarshal([]byte(`{"custom":{"lifetime":0},"nightmare":{"ranges":{"1":{"max":"16"}}}}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Custom.Lifetime != 0 || got.Custom.Tilt.Int64() != -3 || got.Nightmare.Ranges.Max8 != 16 || got.Nightmare.Ranges.Max16 != 0xFFFF {
		t.Errorf("merge: %+v", got)
	}
}

func TestSafeAddress(t *testing.T) {
	tests := []struct {
		addr, size int64
		p, a       int
		want       uint64
		ok         bool
	}{
		{5, 100, 4, 0, 4, true},
		{5, 100, 4, 3, 7, true},
		{95, 100, 4, 0, 92, true},
		{95, 100, 4, 3, 95, true},
		{96, 100, 4, 3, 95, true}, // 99 > 96: last aligned address + alignment
		{6, 10, 4, 3, 5, true},    // 7 > 6 -> 10-8+3
		{0, 4, 4, 0, 0, true},     // size == precision: no clamp needed
		{0, 4, 4, 1, 1, false},    // size == precision: RTCV does not clamp
		{2, 5, 4, 3, 0, true},     // 3 > 1 -> 5-8+3
		{0, 3, 4, 0, 0, false},    // domain smaller than precision
		{7, 8, 1, 0, 7, true},
	}
	for _, tt := range tests {
		got, ok := safeAddress(tt.addr, tt.size, tt.p, tt.a)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("safeAddress(%d, %d, %d, %d) = %d, %v; want %d, %v", tt.addr, tt.size, tt.p, tt.a, got, ok, tt.want, tt.ok)
		}
	}
}

func engineMem() *fakeMem {
	be := dom("BE", 256)
	be.BigEndian = true
	return newFakeMem(dom("A", 256), be)
}

func TestEngines(t *testing.T) {
	mem := engineMem()
	type expect struct {
		source       Source
		storeTime    StoreTime
		storeType    StoreType
		lifetime     int
		executeFrame int
		sameSource   bool
	}
	tests := []struct {
		engine Engine
		want   expect
	}{
		{EngineNightmare, expect{source: SourceValue, storeTime: StoreImmediate, storeType: StoreOnce, lifetime: 1}},
		{EngineHellgenie, expect{source: SourceValue, storeTime: StoreImmediate, storeType: StoreOnce, lifetime: 0}},
		{EngineDistortion, expect{SourceStore, StoreImmediate, StoreOnce, 1, 50, true}},
		{EngineFreeze, expect{SourceStore, StorePreExecute, StoreOnce, 0, 0, true}},
		{EnginePipe, expect{SourceStore, StorePreExecute, StoreContinuous, 0, 0, false}},
	}
	for _, tt := range tests {
		for _, p := range []int{1, 2, 4, 8} {
			s := settings(func(s *Settings) { s.Engine, s.Intensity, s.Precision, s.Alignment = tt.engine, 40, p, p-1 })
			l := mustGenerate(t, s, []string{"A", "BE"}, mem, nil)
			if len(l.Units) != 40 {
				t.Fatalf("%s/%d: %d units", tt.engine, p, len(l.Units))
			}
			for _, u := range l.Units {
				w := tt.want
				d, _ := findDomain(mem, u.Domain)
				if err := u.Validate(); err != nil {
					t.Fatalf("%s: %v", tt.engine, err)
				}
				if u.Source != w.source || u.StoreTime != w.storeTime || u.StoreType != w.storeType || u.Lifetime != w.lifetime ||
					u.ExecuteFrame != w.executeFrame || u.Precision != p || u.BigEndian != d.BigEndian || !u.Enabled || u.Tilt != nil ||
					!d.contains(u.Address, p) || u.Address%uint64(p) != uint64(p-1) {
					t.Fatalf("%s/%d: unexpected unit %+v", tt.engine, p, *u)
				}
				if u.Source == SourceStore {
					sd, _ := findDomain(mem, u.SourceDomain)
					if !sd.contains(u.SourceAddress, p) || u.SourceAddress%uint64(p) != uint64(p-1) {
						t.Fatalf("%s: bad source %+v", tt.engine, *u)
					}
					if w.sameSource && (u.SourceDomain != u.Domain || u.SourceAddress != u.Address) {
						t.Fatalf("%s: source differs %+v", tt.engine, *u)
					}
				}
			}
		}
	}
}

func TestNightmareAlgos(t *testing.T) {
	mem := engineMem()
	for _, algo := range []NightmareAlgo{NightmareRandom, NightmareRandomTilt, NightmareTilt} {
		s := settings(func(s *Settings) {
			s.Nightmare.Algo, s.Intensity, s.Precision = algo, 300, 2
			s.Nightmare.Ranges.Min16, s.Nightmare.Ranges.Max16 = 0x100, 0x102
		})
		counts := map[string]int{}
		for _, u := range mustGenerate(t, s, []string{"A"}, mem, nil).Units {
			switch {
			case u.Source == SourceValue:
				if v := getUint(u.Value, false); v < 0x100 || v > 0x102 {
					t.Fatalf("value %X outside range", u.Value)
				}
				counts["set"]++
			case u.Tilt.Int64() == 1:
				counts["add"]++
			case u.Tilt.Int64() == -1:
				counts["sub"]++
			default:
				t.Fatalf("unexpected unit %+v", *u)
			}
			if u.Source == SourceStore && (u.StoreTime != StorePreExecute || u.StoreType != StoreOnce || u.Lifetime != 1 || u.SourceAddress != u.Address) {
				t.Fatalf("bad tilt unit %+v", *u)
			}
		}
		want := map[NightmareAlgo][]string{
			NightmareRandom:     {"set"},
			NightmareRandomTilt: {"add", "set", "sub"},
			NightmareTilt:       {"add", "sub"},
		}[algo]
		if len(counts) != len(want) {
			t.Errorf("%s: kinds %v", algo, counts)
		}
		for _, k := range want {
			if counts[k] < 60 {
				t.Errorf("%s: %s only %d times", algo, k, counts[k])
			}
		}
	}
}

func TestValueRanges(t *testing.T) {
	mem := engineMem()
	for _, p := range []int{1, 2, 4, 8} {
		for _, engine := range []Engine{EngineNightmare, EngineHellgenie} {
			s := settings(func(s *Settings) {
				s.Engine, s.Intensity, s.Precision = engine, 200, p
				r := ValueRange{Min8: 10, Max8: 11, Min16: 1000, Max16: 1001, Min32: 1 << 31, Max32: 1<<31 + 1, Min64: 1<<64 - 2, Max64: 1<<64 - 1}
				s.Nightmare.Ranges, s.Hellgenie.Ranges = r, r
			})
			lo, hi, _ := s.Nightmare.Ranges.Bounds(p)
			seen := map[uint64]bool{}
			for _, u := range mustGenerate(t, s, []string{"A"}, mem, nil).Units {
				v := getUint(u.Value, false)
				if v < lo || v > hi {
					t.Fatalf("%s/%d: %d outside [%d, %d]", engine, p, v, lo, hi)
				}
				seen[v] = true
			}
			if len(seen) != 2 {
				t.Errorf("%s/%d: range not inclusive: %v", engine, p, seen)
			}
		}
	}
}

func TestIntensityCap(t *testing.T) {
	mem := engineMem()
	for _, tt := range []struct {
		engine   Engine
		lifetime int
		want     int
	}{
		{EngineHellgenie, 1, 7}, {EngineFreeze, 1, 7}, {EnginePipe, 1, 7},
		{EngineCustom, 0, 7}, {EngineCustom, 1, 20}, {EngineNightmare, 1, 20}, {EngineDistortion, 1, 20},
	} {
		s := settings(func(s *Settings) {
			s.Engine, s.Intensity, s.MaxInfiniteUnits, s.Custom.Lifetime = tt.engine, 20, 7, tt.lifetime
		})
		if n := len(mustGenerate(t, s, []string{"A"}, mem, nil).Units); n != tt.want {
			t.Errorf("%s lifetime %d: %d units, want %d", tt.engine, tt.lifetime, n, tt.want)
		}
	}
}

func TestRadius(t *testing.T) {
	mem := newFakeMem(dom("S", 1000), dom("M", 3000), dom("L", 6000))
	sel := []string{"S", "M", "L"}
	count := func(r Radius, intensity int) map[string]int {
		s := settings(func(s *Settings) { s.Radius, s.Intensity = r, intensity })
		out := map[string]int{}
		for _, u := range mustGenerate(t, s, sel, mem, nil).Units {
			out[u.Domain]++
		}
		return out
	}
	exact := []struct {
		radius    Radius
		intensity int
		want      map[string]int
	}{
		{RadiusEven, 100, map[string]int{"S": 33, "M": 33, "L": 33}},
		{RadiusEven, 2, map[string]int{}},
		{RadiusProportional, 100, map[string]int{"S": 10, "M": 30, "L": 60}},
		{RadiusProportional, 5, map[string]int{"S": 0, "M": 2, "L": 3}}, // 0.5 and 1.5, 3.0 rounded to even
		{RadiusNormalized, 100, map[string]int{"S": 16, "M": 50, "L": 100}},
	}
	for _, tt := range exact {
		got := count(tt.radius, tt.intensity)
		for k, v := range tt.want {
			if got[k] != v {
				t.Errorf("%s %d: %v, want %v", tt.radius, tt.intensity, got, tt.want)
				break
			}
		}
	}
	if got := count(RadiusChunk, 50); len(got) != 1 {
		t.Errorf("chunk hit %v", got)
	}
	burst := count(RadiusBurst, 105)
	total := 0
	for _, n := range burst {
		total += n
		if n%10 != 0 {
			t.Errorf("burst counts %v not multiples of 10", burst)
		}
	}
	if total != 100 {
		t.Errorf("burst total %d, want 100", total)
	}
	spread := count(RadiusSpread, 3000)
	for _, k := range sel {
		if spread[k] < 900 || spread[k] > 1100 {
			t.Errorf("spread %v not uniform over domains", spread)
		}
	}
}

func TestGenerateErrors(t *testing.T) {
	mem := newFakeMem(dom("A", 16), Domain{Name: "RO", Size: 16})
	s := DefaultSettings()
	for _, sel := range [][]string{nil, {"X"}, {"RO"}} {
		if _, err := Generate(t.Context(), seeded(), s, sel, mem, nil); err == nil {
			t.Errorf("%v: no error", sel)
		}
	}
	if _, err := Generate(t.Context(), seeded(), s, nil, mem, nil); !errors.Is(err, ErrNoDomains) {
		t.Errorf("err = %v", err)
	}
	for _, e := range []Engine{EngineVector, EngineCluster} {
		if _, err := Generate(t.Context(), seeded(), settings(func(s *Settings) { s.Engine = e }), []string{"A"}, mem, nil); err == nil {
			t.Errorf("%s without lists: no error", e)
		}
	}
	bad := settings(func(s *Settings) { s.Precision = 3 })
	if _, err := Generate(t.Context(), seeded(), bad, []string{"A"}, mem, nil); err == nil {
		t.Error("invalid settings accepted")
	}
}

func TestDeterminism(t *testing.T) {
	mem := engineMem()
	for _, e := range []Engine{EngineNightmare, EnginePipe, EngineCustom} {
		s := settings(func(s *Settings) { s.Engine, s.Intensity, s.Nightmare.Algo = e, 100, NightmareRandomTilt })
		a := mustGenerate(t, s, []string{"A", "BE"}, mem, nil)
		b := mustGenerate(t, s, []string{"A", "BE"}, mem, nil)
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s: same seed, different layers", e)
		}
	}
}

func TestVector(t *testing.T) {
	be := dom("BE", 32)
	be.BigEndian = true
	mem := newFakeMem(dom("A", 32), be)
	one := []byte{0x00, 0x00, 0x80, 0x3F} // 1.0f little-endian
	for addr := uint64(0); addr < 32; addr += 4 {
		mem.put("A", addr, 1, 2, 3, 4)
		mem.put("BE", addr, 1, 2, 3, 4)
	}
	mem.put("A", 8, one...)
	mem.put("BE", 12, 0x3F, 0x80, 0x00, 0x00)
	reg := registry(t, map[string]string{"_one": "3F800000\n", "_two": "40000000\n", "short": "07\n"})
	s := settings(func(s *Settings) {
		s.Engine, s.Intensity, s.Vector.LimiterList, s.Vector.ValueList = EngineVector, 200, "_one", "_two"
	})
	l := mustGenerate(t, s, []string{"A", "BE"}, mem, reg)
	hits := map[string]bool{}
	for _, u := range l.Units {
		want := map[string]uint64{"A": 8, "BE": 12}[u.Domain]
		d, _ := findDomain(mem, u.Domain)
		if u.Address != want || u.Precision != 4 || !bytes.Equal(u.Value, []byte{0, 0, 0, 0x40}) ||
			!u.GeneratedUsingValueList || u.Lifetime != 1 || u.BigEndian != d.BigEndian {
			t.Fatalf("unexpected unit %+v", *u)
		}
		hits[u.Domain] = true
	}
	if !hits["A"] || !hits["BE"] || len(l.Units) >= 200 {
		t.Errorf("hits %v, %d units", hits, len(l.Units))
	}

	// Unlocked precision: the value list entry is resized to the precision.
	s.Vector.UnlockPrecision, s.Precision, s.Vector.ValueList = true, 1, "short"
	s.Vector.LimiterList = "short"
	mem.put("A", 30, 7)
	l = mustGenerate(t, s, []string{"A"}, mem, reg)
	if len(l.Units) == 0 {
		t.Fatal("no hits with unlocked precision")
	}
	for _, u := range l.Units {
		if u.Address != 30 || u.Precision != 1 || !bytes.Equal(u.Value, []byte{7}) {
			t.Fatalf("unexpected unit %+v", *u)
		}
	}
}

func TestCluster(t *testing.T) {
	mem := newFakeMem(dom("A", 64))
	// Elements 0x10..0x15 at 0, 2, ..., 10; the limiter matches 0x10 and 0x11.
	for i := range 6 {
		mem.put("A", uint64(2*i), byte(0x10+i), 0xAA)
	}
	reg := registry(t, map[string]string{"lim": "10AA\n11AA\n", "_lim": "10AA\n11AA\n"})
	e := func(b byte) []byte { return []byte{b, 0xAA} }
	tests := []struct {
		name string
		addr int64
		f    func(*ClusterSettings)
		want [][]byte
	}{
		{"reverse", 0, func(c *ClusterSettings) { c.Method = ClusterReverse }, [][]byte{e(0x12), e(0x11), e(0x10)}},
		{"unaligned address", 1, func(c *ClusterSettings) { c.Method = ClusterReverse }, [][]byte{e(0x12), e(0x11), e(0x10)}},
		{"rotate forwards", 0, func(c *ClusterSettings) { c.Method = ClusterRotateForwards }, [][]byte{e(0x12), e(0x10), e(0x11)}},
		{"rotate forwards x2", 0, func(c *ClusterSettings) { c.Method, c.Modifier = ClusterRotateForwards, 2 }, [][]byte{e(0x11), e(0x12), e(0x10)}},
		{"rotate backwards", 0, func(c *ClusterSettings) { c.Method = ClusterRotateBackwards }, [][]byte{e(0x11), e(0x12), e(0x10)}},
		{"overwrite", 0, func(c *ClusterSettings) { c.Method = ClusterOverwrite }, [][]byte{e(0x10), e(0x10), e(0x10)}},
		{"random", 0, func(c *ClusterSettings) {}, [][]byte{e(0x11), e(0x10), e(0x12)}},
		{"forwards miss", 4, func(c *ClusterSettings) {}, nil},
		{"backwards miss", 0, func(c *ClusterSettings) { c.Method, c.Direction = ClusterOverwrite, ClusterBackwards }, nil},
		{"backwards hit", 0, func(c *ClusterSettings) { c.Method, c.Direction, c.ChunkSize = ClusterOverwrite, ClusterBackwards, 2 }, [][]byte{e(0x11), e(0x11)}},
		{"filter all miss", 0, func(c *ClusterSettings) { c.Method, c.FilterAll = ClusterReverse, true }, nil},
		{"filter all hit", 0, func(c *ClusterSettings) { c.Method, c.FilterAll, c.ChunkSize = ClusterReverse, true, 2 }, [][]byte{e(0x11), e(0x10)}},
		{"chunk past end", 60, func(c *ClusterSettings) {}, nil},
		{"big-endian list", 0, func(c *ClusterSettings) { c.LimiterList = "_lim" }, nil},
	}
	for _, tt := range tests {
		s := settings(func(s *Settings) {
			s.Engine, s.Cluster.LimiterList = EngineCluster, "lim"
			tt.f(&s.Cluster)
		})
		g, err := newGenerator(t.Context(), seeded(), s, []string{"A"}, mem, reg)
		if err != nil {
			t.Fatal(err)
		}
		units, err := g.cluster(g.selected[0], tt.addr)
		if err != nil {
			t.Fatal(err)
		}
		var got [][]byte
		for i, u := range units {
			if u.BigEndian || u.Precision != 2 || u.Address != uint64(2*i) || u.Lifetime != 1 || u.Source != SourceValue {
				t.Fatalf("%s: unit %+v", tt.name, *u)
			}
			got = append(got, u.Value)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %X, want %X", tt.name, got, tt.want)
		}
	}

	s := settings(func(s *Settings) {
		s.Engine, s.Cluster.LimiterList, s.Cluster.SplitUnits, s.Cluster.Method = EngineCluster, "lim", false, ClusterReverse
	})
	g, _ := newGenerator(t.Context(), seeded(), s, []string{"A"}, mem, reg)
	units, _ := g.cluster(g.selected[0], 0)
	if len(units) != 1 || units[0].Precision != 6 || !bytes.Equal(units[0].Value, []byte{0x12, 0xAA, 0x11, 0xAA, 0x10, 0xAA}) {
		t.Errorf("joined: %+v", units)
	}
	s.Intensity = 500
	if l := mustGenerate(t, s, []string{"A"}, mem, reg); len(l.Units) == 0 || len(l.Units) > 60 {
		t.Errorf("generate: %d units", len(l.Units))
	}
}

func TestCustom(t *testing.T) {
	mem := engineMem()
	for a := range uint64(256) {
		mem.put("A", a, byte(a))
	}
	reg := registry(t, map[string]string{"vals": "BEEF\n", "lim": "10\n20\n"})
	tilt := big.NewInt(-5)
	tests := []struct {
		name  string
		f     func(*CustomSettings)
		check func(*Unit) bool
		min   int
	}{
		{"random", func(*CustomSettings) {}, func(u *Unit) bool {
			return u.Source == SourceValue && len(u.Value) == 1 && u.Lifetime == 1 && !u.GeneratedUsingValueList
		}, 50},
		{"range", func(c *CustomSettings) { c.ValueSource = ValueFromRange; c.Ranges.Min8, c.Ranges.Max8 = 3, 3 }, func(u *Unit) bool {
			return bytes.Equal(u.Value, []byte{3})
		}, 50},
		{"list", func(c *CustomSettings) { c.ValueSource, c.ValueList = ValueFromList, "vals" }, func(u *Unit) bool {
			return bytes.Equal(u.Value, []byte{0xEF}) && u.GeneratedUsingValueList
		}, 50},
		{"store same", func(c *CustomSettings) {
			c.Source, c.StoreAddress, c.StoreTime, c.StoreType = SourceStore, StoreAddressSame, StorePreExecute, StoreContinuous
			c.Delay, c.Lifetime, c.Loop, c.Tilt = 4, 9, true, tilt
		}, func(u *Unit) bool {
			return u.Source == SourceStore && u.SourceDomain == u.Domain && u.SourceAddress == u.Address &&
				u.StoreTime == StorePreExecute && u.StoreType == StoreContinuous && u.ExecuteFrame == 4 &&
				u.Lifetime == 9 && u.Loop && u.Tilt.Cmp(tilt) == 0 && u.Tilt != tilt && u.Value == nil
		}, 50},
		{"store random", func(c *CustomSettings) { c.Source = SourceStore }, func(u *Unit) bool {
			return u.Source == SourceStore && u.StoreTime == StoreImmediate && u.StoreType == StoreOnce
		}, 50},
		{"limiter", func(c *CustomSettings) { c.LimiterTime, c.LimiterList = LimiterGenerate, "lim" }, func(u *Unit) bool {
			return u.Domain == "A" && (u.Address == 0x10 || u.Address == 0x20) && u.LimiterList == "lim" && u.LimiterTime == LimiterGenerate
		}, 1},
		{"limiter execute", func(c *CustomSettings) { c.LimiterTime, c.LimiterList = LimiterExecute, "lim" }, func(u *Unit) bool {
			return u.Domain == "A" && (u.Address == 0x10 || u.Address == 0x20) && u.LimiterTime == LimiterExecute
		}, 1},
		{"limiter inverted", func(c *CustomSettings) {
			c.LimiterTime, c.LimiterList, c.LimiterInverted = LimiterGenerate, "lim", true
		}, func(u *Unit) bool {
			return u.Domain == "BE" || (u.Address != 0x10 && u.Address != 0x20)
		}, 300},
		{"limiter source", func(c *CustomSettings) {
			c.Source, c.LimiterTime, c.LimiterList, c.StoreLimiterSource = SourceStore, LimiterGenerate, "lim", LimitSourceAddress
		}, func(u *Unit) bool {
			return u.SourceDomain == "A" && (u.SourceAddress == 0x10 || u.SourceAddress == 0x20)
		}, 1},
	}
	for _, tt := range tests {
		s := settings(func(s *Settings) { s.Engine, s.Intensity = EngineCustom, 400; tt.f(&s.Custom) })
		l := mustGenerate(t, s, []string{"A", "BE"}, mem, reg)
		if len(l.Units) < tt.min {
			t.Errorf("%s: %d units", tt.name, len(l.Units))
		}
		for _, u := range l.Units {
			d, _ := findDomain(mem, u.Domain)
			if !tt.check(u) || u.Validate() != nil || u.BigEndian != d.BigEndian {
				t.Fatalf("%s: unexpected unit %+v", tt.name, *u)
			}
		}
	}
}
