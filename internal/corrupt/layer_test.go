package corrupt

import (
	"bytes"
	"math/big"
	"reflect"
	"testing"
)

func vu(domain string, addr uint64, value ...byte) *Unit {
	return newValueUnit(domain, addr, value, false, 1)
}

func addrs(l *Layer) []uint64 {
	var out []uint64
	for _, u := range l.Units {
		out = append(out, u.Address)
	}
	return out
}

func enabled(l *Layer) []bool {
	var out []bool
	for _, u := range l.Units {
		out = append(out, u.Enabled)
	}
	return out
}

func TestSanitizeDuplicates(t *testing.T) {
	l := &Layer{Units: []*Unit{vu("A", 1, 1), vu("A", 1, 2), vu("B", 1, 3), vu("A", 2, 4), vu("A", 1, 5), vu("A", 2, 6)}}
	l.Units[0].Locked = true
	l.SanitizeDuplicates()
	var got []byte
	for _, u := range l.Units {
		got = append(got, u.Value[0])
	}
	if want := []byte{1, 3, 5, 6}; !bytes.Equal(got, want) {
		t.Errorf("kept %v, want %v", got, want)
	}
}

func TestEnableOps(t *testing.T) {
	mk := func() *Layer {
		l := &Layer{}
		for i := range 10 {
			l.Units = append(l.Units, vu("A", uint64(i), 0))
		}
		l.Units[0].Locked, l.Units[0].Enabled = true, false
		l.Units[1].Locked = true
		return l
	}
	l := mk()
	l.Disable50(seeded())
	disabled := 0
	for _, u := range l.Units[2:] {
		if !u.Enabled {
			disabled++
		}
	}
	if disabled != 4 || l.Units[0].Enabled || !l.Units[1].Enabled {
		t.Errorf("Disable50: %v", enabled(l))
	}

	l.InvertDisabled()
	disabled = 0
	for _, u := range l.Units[2:] {
		if !u.Enabled {
			disabled++
		}
	}
	if disabled != 4 || l.Units[0].Enabled || !l.Units[1].Enabled {
		t.Errorf("InvertDisabled: %v", enabled(l))
	}

	l.RemoveDisabled()
	if len(l.Units) != 6 || l.Units[0].Enabled {
		t.Errorf("RemoveDisabled: %v", enabled(l))
	}

	l = mk()
	l.DisableAll()
	if want := []bool{false, true, false, false, false, false, false, false, false, false}; !reflect.DeepEqual(enabled(l), want) {
		t.Errorf("DisableAll: %v", enabled(l))
	}
	l.EnableAll()
	if want := []bool{false, true, true, true, true, true, true, true, true, true}; !reflect.DeepEqual(enabled(l), want) {
		t.Errorf("EnableAll: %v", enabled(l))
	}
}

func TestDuplicateMerge(t *testing.T) {
	l := &Layer{Units: []*Unit{vu("A", 0, 1), vu("A", 1, 2), vu("A", 2, 3)}}
	l.Units[1].Locked = true
	if err := l.Duplicate(0, 1, 2); err != nil {
		t.Fatal(err)
	}
	if want := []uint64{0, 1, 2, 0, 2}; !reflect.DeepEqual(addrs(l), want) {
		t.Errorf("Duplicate: %v", addrs(l))
	}
	l.Units[3].Value[0] = 9
	if l.Units[0].Value[0] != 1 {
		t.Error("Duplicate shares value")
	}
	if err := l.Duplicate(5); err == nil {
		t.Error("out of range index accepted")
	}
	m := Merge(&Layer{Units: []*Unit{vu("A", 7, 0)}}, nil, l)
	if want := []uint64{7, 0, 1, 2, 0, 2}; !reflect.DeepEqual(addrs(m), want) {
		t.Errorf("Merge: %v", addrs(m))
	}
	if m.Units[1] == l.Units[0] {
		t.Error("Merge does not copy")
	}
}

func TestShift(t *testing.T) {
	u := fullUnit()
	u.Source, u.Value = SourceValue, Hex{0xFF, 0x00, 0x00, 0x00}
	l := &Layer{Units: []*Unit{u, vu("A", 5, 0xFF)}}
	steps := []struct {
		field  ShiftField
		amount int64
		idx    []int
		check  func() bool
	}{
		{ShiftAddress, 16, nil, func() bool { return l.Units[0].Address == 0x1244 && l.Units[1].Address == 21 }},
		{ShiftAddress, -100, []int{1}, func() bool { return l.Units[1].Address == 0 && l.Units[0].Address == 0x1244 }},
		{ShiftSourceAddress, -1, []int{0}, func() bool { return u.SourceAddress == 0x3F }},
		{ShiftExecuteFrame, -10, []int{0}, func() bool { return u.ExecuteFrame == 0 }},
		{ShiftLifetime, 2, []int{0}, func() bool { return u.Lifetime == 2 }},
		{ShiftLoopTiming, 1, []int{0}, func() bool { return u.LoopTiming == 8 }},
		{ShiftTilt, 1, []int{1}, func() bool { return l.Units[1].Tilt.Int64() == 1 }},
		{ShiftTilt, -1, []int{1}, func() bool { return l.Units[1].Tilt == nil }},
		{ShiftValue, 1, nil, func() bool {
			return bytes.Equal(u.Value, []byte{0x00, 0x01, 0, 0}) && bytes.Equal(l.Units[1].Value, []byte{0x00})
		}},
		{ShiftValue, -2, []int{1}, func() bool { return bytes.Equal(l.Units[1].Value, []byte{0xFE}) }},
	}
	for i, s := range steps {
		if err := l.Shift(s.field, s.amount, s.idx...); err != nil {
			t.Fatal(err)
		}
		if !s.check() {
			t.Errorf("step %d (%s %d): unit0 %+v unit1 %+v", i, s.field, s.amount, *l.Units[0], *l.Units[1])
		}
	}
	if err := l.Shift("domain", 1); err == nil {
		t.Error("bad field accepted")
	}
	if err := l.Shift(ShiftAddress, 1, 2); err == nil {
		t.Error("bad index accepted")
	}
}

func TestBake(t *testing.T) {
	mem := newFakeMem(dom("A", 16))
	mem.put("A", 4, 0x11, 0x22, 0x33, 0x44)
	store := newStoreUnit(StoreOnce, StorePreExecute, "A", 4, "A", 0, 4, true, 5, 0)
	store.Note, store.Locked = "n", true
	disabled := vu("A", 0, 1)
	disabled.Enabled = false
	l := &Layer{Note: "x", Units: []*Unit{store, disabled, vu("A", 15, 0, 0), vu("B", 0, 0), vu("A", 5, 0)}}
	b, err := l.Backup(t.Context(), mem)
	if err != nil {
		t.Fatal(err)
	}
	want0 := newValueUnit("A", 4, []byte{0x11, 0x22, 0x33, 0x44}, false, 1)
	want0.Note, want0.Locked = "n", true
	want := &Layer{Note: "x", Units: []*Unit{want0, newValueUnit("A", 5, []byte{0x22}, false, 1)}}
	if !reflect.DeepEqual(b, want) {
		t.Errorf("got %+v %+v", b.Units[0], b.Units[1:])
	}
	if len(mem.calls) != 1 {
		t.Errorf("ReadMany calls = %d, want 1", len(mem.calls))
	}
}

func TestBreakdown(t *testing.T) {
	le := newValueUnit("A", 8, []byte{0xFF, 0x00}, false, 1)
	le.Tilt = big.NewInt(1)
	be := newValueUnit("A", 8, []byte{0x01, 0x02}, true, 1)
	st := newStoreUnit(StoreOnce, StorePreExecute, "A", 8, "B", 0, 2, true, 0, 1)
	st.Tilt = big.NewInt(-1)
	one := vu("A", 3, 7)
	l := &Layer{Units: []*Unit{le, be, st, one}}
	b := l.Breakdown()
	type row struct {
		addr, src uint64
		value     string
		tilt      int64
	}
	var got []row
	for _, u := range b.Units {
		if u.Precision != 1 {
			t.Fatalf("precision %d", u.Precision)
		}
		var tilt int64
		if u.Tilt != nil {
			tilt = u.Tilt.Int64()
		}
		v, _ := u.Value.MarshalText()
		got = append(got, row{u.Address, u.SourceAddress, string(v), tilt})
	}
	want := []row{
		{8, 0, "00", 0}, {9, 0, "01", 0},
		{8, 0, "02", 0}, {9, 0, "01", 0},
		{8, 0, "", 0}, {9, 1, "", -1},
		{3, 0, "07", 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestReroll(t *testing.T) {
	mem := newFakeMem(dom("A", 64), dom("B", 16))
	reg := registry(t, map[string]string{"vals": "AAAA\n", "cvals": "BBBB\n"})
	mk := func() *Layer {
		v := vu("A", 0, 0, 0)
		listed := vu("A", 2, 0, 0)
		listed.GeneratedUsingValueList = true
		locked := vu("A", 4, 0, 0)
		locked.Locked = true
		st := newStoreUnit(StoreOnce, StorePreExecute, "A", 10, "A", 20, 4, false, 0, 1)
		return &Layer{Units: []*Unit{v, listed, locked, st}}
	}
	s := DefaultSettings()
	s.Vector.ValueList = "vals"
	s.Custom.ValueList = "cvals"
	s.Custom.ValueSource = ValueFromRange
	s.Custom.Ranges.Min16, s.Custom.Ranges.Max16 = 0x1234, 0x1234

	tests := []struct {
		name   string
		reroll RerollSettings
		check  func(*Layer) bool
	}{
		{"default", RerollSettings{SourceAddress: true, SourceDomain: true}, func(l *Layer) bool {
			st := l.Units[3]
			d, _ := findDomain(mem, st.SourceDomain)
			return bytes.Equal(l.Units[1].Value, []byte{0xAA, 0xAA}) && bytes.Equal(l.Units[2].Value, []byte{0, 0}) &&
				st.Domain == "A" && st.Address == 10 && st.SourceAddress <= d.Size-4
		}},
		{"destination", RerollSettings{Domain: true, Address: true}, func(l *Layer) bool {
			st := l.Units[3]
			d, _ := findDomain(mem, st.Domain)
			return st.SourceDomain == "A" && st.SourceAddress == 20 && st.Address <= d.Size-4
		}},
		{"nothing", RerollSettings{}, func(l *Layer) bool {
			st := l.Units[3]
			return st.Domain == "A" && st.Address == 10 && st.SourceDomain == "A" && st.SourceAddress == 20
		}},
		{"custom", RerollSettings{FollowCustomEngine: true}, func(l *Layer) bool {
			return bytes.Equal(l.Units[0].Value, []byte{0x34, 0x12}) && bytes.Equal(l.Units[1].Value, []byte{0xBB, 0xBB})
		}},
	}
	for _, tt := range tests {
		s.Reroll = tt.reroll
		l := mk()
		if err := l.Reroll(seeded(), s, []string{"A", "B"}, mem, reg); err != nil {
			t.Fatal(err)
		}
		if !tt.check(l) {
			t.Errorf("%s: %+v", tt.name, l.Units)
		}
	}
	s.Reroll = RerollSettings{Domain: true}
	if err := mk().Reroll(seeded(), s, nil, mem, reg); err == nil {
		t.Error("reroll without selected domains succeeded")
	}
	s.Vector.ValueList = "missing"
	s.Reroll = RerollSettings{}
	if err := mk().Reroll(seeded(), s, nil, mem, reg); err == nil {
		t.Error("reroll with missing value list succeeded")
	}
}
