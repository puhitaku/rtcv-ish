package server_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

func fullRanges() gen.PrecisionRanges {
	return gen.PrecisionRanges{
		P1: gen.ValueRange{Min: "0", Max: "255"},
		P2: gen.ValueRange{Min: "0", Max: "65535"},
		P4: gen.ValueRange{Min: "0", Max: "4294967295"},
		P8: gen.ValueRange{Min: "0", Max: "18446744073709551615"},
	}
}

// defaultSettings mirrors RTCV's defaults (design/rtcv-reference.md 2.5,
// 2.6 and the reroll defaults of CorruptCore.cs).
func defaultSettings() gen.Settings {
	return gen.Settings{
		Engine:           gen.EngineNightmare,
		Intensity:        1,
		ErrorDelay:       1,
		Radius:           gen.BlastRadiusSpread,
		Precision:        gen.PrecisionN1,
		Alignment:        0,
		AutoCorrupt:      false,
		MaxInfiniteUnits: 50,
		LockUnits:        false,
		Nightmare:        gen.NightmareSettings{Algo: gen.NightmareAlgoRandom, Ranges: fullRanges()},
		Hellgenie:        gen.HellgenieSettings{Ranges: fullRanges()},
		Distortion:       gen.DistortionSettings{Delay: 50},
		Vector:           gen.VectorSettings{},
		Cluster: gen.ClusterSettings{
			ChunkSize:  3,
			Method:     gen.ClusterMethodRandom,
			Modifier:   1,
			Direction:  gen.ClusterDirectionForwards,
			SplitUnits: true,
		},
		Custom: gen.CustomSettings{
			Source:       gen.UnitSourceValue,
			ValueSource:  gen.CustomValueSourceRandom,
			Ranges:       fullRanges(),
			StoreAddress: gen.CustomStoreAddressRandom,
			StoreTime:    gen.StoreTimeImmediate,
			StoreType:    gen.StoreTypeOnce,
			Tilt:         "0",
			Delay:        0,
			Lifetime:     1,
			LimiterTime:  gen.LimiterTimeNone,
		},
		Reroll:         gen.RerollSettings{SourceAddress: true, SourceDomain: true},
		GameProtection: gen.GameProtectionSettings{Enabled: false, IntervalSeconds: 5, Keep: 10},
	}
}

func TestSettingsDefaults(t *testing.T) {
	e := newEnv(t, envOptions{noConnect: true})
	if got, want := e.settings(), defaultSettings(); !reflect.DeepEqual(got, want) {
		t.Errorf("default settings:\n got %+v\nwant %+v", got, want)
	}
}

func TestSettingsPatch(t *testing.T) {
	e := newEnv(t, envOptions{noConnect: true})

	got := e.patchSettings(gen.SettingsPatch{
		Intensity: ptr(int64(5)),
		Nightmare: &gen.NightmareSettingsPatch{Algo: ptr(gen.NightmareAlgoTilt)},
	})
	want := defaultSettings()
	want.Intensity = 5
	want.Nightmare.Algo = gen.NightmareAlgoTilt
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after patch 1:\n got %+v\nwant %+v", got, want)
	}

	// Nested objects merge field by field, down to one precision's range.
	got = e.patchSettings(gen.SettingsPatch{
		Engine:    ptr(gen.EngineCustom),
		Precision: ptr(gen.PrecisionN4),
		Alignment: ptr(3),
		Custom: &gen.CustomSettingsPatch{
			Source:   ptr(gen.UnitSourceStore),
			Tilt:     ptr("-18446744073709551615"),
			Lifetime: ptr(0),
			Ranges:   &gen.PrecisionRangesPatch{P2: &gen.ValueRange{Min: "16", Max: "32"}},
		},
		Reroll: &gen.RerollSettingsPatch{Address: ptr(true)},
	})
	want.Engine = gen.EngineCustom
	want.Precision = gen.PrecisionN4
	want.Alignment = 3
	want.Custom.Source = gen.UnitSourceStore
	want.Custom.Tilt = "-18446744073709551615"
	want.Custom.Lifetime = 0
	want.Custom.Ranges.P2 = gen.ValueRange{Min: "16", Max: "32"}
	want.Reroll.Address = true
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after patch 2:\n got %+v\nwant %+v", got, want)
	}
	if got := e.settings(); !reflect.DeepEqual(got, want) {
		t.Errorf("GET after patch:\n got %+v\nwant %+v", got, want)
	}

	// An empty patch changes nothing.
	if got := e.patchSettings(gen.SettingsPatch{}); !reflect.DeepEqual(got, want) {
		t.Errorf("empty patch changed settings: %+v", got)
	}
}

func TestSettingsValidation(t *testing.T) {
	e := newEnv(t, envOptions{noConnect: true})
	before := e.settings()
	cases := []struct{ name, body string }{
		{"unknown engine", `{"engine":"bogus"}`},
		{"precision 3", `{"precision":3}`},
		{"intensity 0", `{"intensity":0}`},
		{"errorDelay 0", `{"errorDelay":0}`},
		{"unknown radius", `{"radius":"everywhere"}`},
		{"alignment >= precision", `{"alignment":1}`},
		{"alignment >= new precision", `{"precision":2,"alignment":2}`},
		{"maxInfiniteUnits 0", `{"maxInfiniteUnits":0}`},
		{"unknown field", `{"foo":1}`},
		{"wrong type", `{"intensity":"five"}`},
		{"range max too large", `{"nightmare":{"ranges":{"1":{"min":"0","max":"256"}}}}`},
		{"range 64-bit overflow", `{"hellgenie":{"ranges":{"8":{"min":"0","max":"18446744073709551616"}}}}`},
		{"range min > max", `{"nightmare":{"ranges":{"2":{"min":"10","max":"9"}}}}`},
		{"range not decimal", `{"nightmare":{"ranges":{"4":{"min":"0x10","max":"20"}}}}`},
		{"unknown algo", `{"nightmare":{"algo":"chaos"}}`},
		{"custom tilt not integer", `{"custom":{"tilt":"1.5"}}`},
		{"custom unknown limiterTime", `{"custom":{"limiterTime":"execute"}}`},
		{"cluster chunkSize 0", `{"cluster":{"chunkSize":0}}`},
		{"cluster unknown method", `{"cluster":{"method":"shuffle"}}`},
		{"distortion delay 0", `{"distortion":{"delay":0}}`},
		{"protection interval 0", `{"gameProtection":{"intervalSeconds":0}}`},
		{"not JSON", `{`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := e.c.PatchSettingsWithBodyWithResponse(e.ctx, "application/json", strings.NewReader(tc.body))
			expectError(t, r, err, http.StatusBadRequest, "INVALID_ARGUMENT")
		})
	}
	if got := e.settings(); !reflect.DeepEqual(got, before) {
		t.Errorf("rejected patches changed settings:\n got %+v\nwant %+v", got, before)
	}
}

func TestSettingsPersistence(t *testing.T) {
	dir := t.TempDir()
	e := newEnv(t, envOptions{noConnect: true, dataDir: dir})
	want := e.patchSettings(gen.SettingsPatch{
		Engine:      ptr(gen.EngineHellgenie),
		Intensity:   ptr(int64(7)),
		ErrorDelay:  ptr(int64(30)),
		AutoCorrupt: ptr(true),
		Hellgenie:   &gen.HellgenieSettingsPatch{Ranges: &gen.PrecisionRangesPatch{P8: &gen.ValueRange{Min: "1", Max: "18446744073709551614"}}},
		Vector:      &gen.VectorSettingsPatch{LimiterList: ptr("floats")},
	})

	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("settings.json: %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("settings.json is not JSON: %s", b)
	}

	// A new server on the same data directory reads the settings back.
	// autoCorrupt is not persisted.
	e2 := newEnv(t, envOptions{noConnect: true, dataDir: dir})
	want.AutoCorrupt = false
	if got := e2.settings(); !reflect.DeepEqual(got, want) {
		t.Errorf("reloaded settings:\n got %+v\nwant %+v", got, want)
	}
}
