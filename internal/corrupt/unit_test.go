package corrupt

import (
	"bytes"
	"encoding/json"
	"math/big"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fullUnit() *Unit {
	tilt, _ := new(big.Int).SetString("-1180591620717411303424", 10) // -2^70
	return &Unit{
		Enabled:                 true,
		Locked:                  true,
		BigEndian:               true,
		Domain:                  "MainRAM",
		Address:                 0x1234,
		Precision:               4,
		Source:                  SourceStore,
		Value:                   Hex{0xDE, 0xAD, 0xBE, 0xEF},
		SourceDomain:            "VRAM",
		SourceAddress:           0x40,
		StoreTime:               StorePreExecute,
		StoreType:               StoreContinuous,
		Tilt:                    tilt,
		ExecuteFrame:            3,
		Lifetime:                0,
		Loop:                    true,
		LoopTiming:              7,
		LimiterTime:             LimiterExecute,
		LimiterList:             "_floats",
		InvertLimiter:           true,
		GeneratedUsingValueList: true,
		Note:                    "note",
	}
}

func TestUnitJSONRoundTrip(t *testing.T) {
	l := &Layer{Note: "layer", Units: []*Unit{fullUnit(), NewUnit()}}
	l.Units[1].Domain = "OAM"
	l.Units[1].Value = Hex{0x01}
	l.Units[1].Precision = 1
	data, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{`"value":"deadbeef"`, `"tilt":"-1180591620717411303424"`, `"note":"layer"`, `"storeTime":"preexecute"`, `"limiterList":"_floats"`} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON lacks %s: %s", want, s)
		}
	}
	for _, want := range []string{`"tilt":"0"`, `"loopTiming":null`, `"loopTiming":7`} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON lacks %s: %s", want, s)
		}
	}
	var got Layer
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&got, l) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got.Units[0], l.Units[0])
	}
}

func TestUnitJSONDefaults(t *testing.T) {
	var u Unit
	if err := json.Unmarshal([]byte(`{"domain":"MainRAM","address":16,"value":"aBc","tilt":5,"loopTiming":null}`), &u); err != nil {
		t.Fatal(err)
	}
	want := NewUnit()
	want.Domain, want.Address, want.Value, want.Precision, want.Tilt = "MainRAM", 16, Hex{0x0A, 0xBC}, 2, big.NewInt(5)
	if !reflect.DeepEqual(&u, want) {
		t.Errorf("got %+v, want %+v", u, *want)
	}
	if err := u.Validate(); err != nil {
		t.Error(err)
	}
	if err := json.Unmarshal([]byte(`{"tilt":"x"}`), &u); err == nil {
		t.Error("bad tilt accepted")
	}
	if err := json.Unmarshal([]byte(`{"value":"zz"}`), &u); err == nil {
		t.Error("bad hex accepted")
	}
}

func TestUnitValidate(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Unit)
		ok     bool
	}{
		{"valid store", func(*Unit) {}, true},
		{"no domain", func(u *Unit) { u.Domain = "" }, false},
		{"zero precision", func(u *Unit) { u.Precision = 0 }, false},
		{"huge precision", func(u *Unit) { u.Precision = MaxPrecision + 1 }, false},
		{"16 MiB store", func(u *Unit) { u.Precision = 16 << 20 }, true},
		{"16 MiB value", func(u *Unit) { u.Source = SourceValue; u.Precision = 16 << 20; u.Value = make(Hex, 16<<20) }, true},
		{"16 MiB + 1 value", func(u *Unit) { u.Source = SourceValue; u.Precision = 16<<20 + 1; u.Value = make(Hex, 16<<20+1) }, false},
		{"value length", func(u *Unit) { u.Source = SourceValue; u.Value = Hex{1} }, false},
		{"value ok", func(u *Unit) { u.Source = SourceValue }, true},
		{"no source domain", func(u *Unit) { u.SourceDomain = "" }, false},
		{"bad source", func(u *Unit) { u.Source = "x" }, false},
		{"bad store time", func(u *Unit) { u.StoreTime = "x" }, false},
		{"bad store type", func(u *Unit) { u.StoreType = "x" }, false},
		{"bad limiter", func(u *Unit) { u.LimiterTime = "x" }, false},
		{"negative frame", func(u *Unit) { u.ExecuteFrame = -1 }, false},
		{"negative lifetime", func(u *Unit) { u.Lifetime = -1 }, false},
		{"loop timing", func(u *Unit) { u.LoopTiming = -2 }, false},
	}
	for _, tt := range tests {
		u := fullUnit()
		tt.modify(u)
		if err := u.Validate(); (err == nil) != tt.ok {
			t.Errorf("%s: err = %v", tt.name, err)
		}
	}
}

func TestUnitJSONLargeValue(t *testing.T) {
	v := make([]byte, MaxPrecision)
	for i := range v {
		v[i] = byte(i)
	}
	u := NewUnit()
	u.Domain, u.Precision, u.Value = "A", len(v), v
	data, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	var got Unit
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Value, v) || got.Precision != MaxPrecision {
		t.Fatalf("round trip: precision %d, %d bytes", got.Precision, len(got.Value))
	}
	if err := got.Validate(); err != nil {
		t.Error(err)
	}
}

func TestHexOddLength(t *testing.T) {
	var h Hex
	if err := h.UnmarshalText([]byte(" ABc ")); err != nil || !bytes.Equal(h, []byte{0x0a, 0xbc}) {
		t.Errorf("got %X, %v", h, err)
	}
	if err := h.UnmarshalText([]byte("g12")); err == nil {
		t.Error("bad leading digit accepted")
	}
	if err := h.UnmarshalText(nil); err != nil || len(h) != 0 {
		t.Errorf("empty: %X, %v", h, err)
	}
}

func TestSetPrecision(t *testing.T) {
	u := NewUnit()
	u.Value, u.Precision = Hex{0x11, 0x22}, 2
	u.SetPrecision(4)
	if !reflect.DeepEqual(u.Value, Hex{0, 0, 0x11, 0x22}) {
		t.Errorf("grow: %X", u.Value)
	}
	u.SetPrecision(1)
	if !reflect.DeepEqual(u.Value, Hex{0x22}) || u.Precision != 1 {
		t.Errorf("shrink: %X", u.Value)
	}
	u.SetPrecision(MaxPrecision + 1)
	if u.Precision != MaxPrecision || len(u.Value) != MaxPrecision || u.Value[MaxPrecision-1] != 0x22 || u.Value[0] != 0 {
		t.Errorf("grow to max: precision %d, %d bytes", u.Precision, len(u.Value))
	}
}

func TestLayerLoadSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.bl")
	l := &Layer{Note: "n", Units: []*Unit{fullUnit()}}
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, l) {
		t.Errorf("got %+v", got)
	}
	empty := &Layer{}
	if err := empty.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(empty)
	if string(data) != `{"note":"","units":[]}` {
		t.Errorf("empty layer = %s", data)
	}
}
