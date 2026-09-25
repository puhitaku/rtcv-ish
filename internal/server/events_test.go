package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

func TestEvents(t *testing.T) {
	e := newEnv(t, envOptions{noConnect: true})
	events := e.openEvents()

	first := events.next(t)
	if first.Type != "status" {
		t.Fatalf("first event %q, want status", first.Type)
	}
	if st := decodeEvent[gen.Status](t, first); st.Connected || st.DataDir != e.dataDir {
		t.Errorf("first status = %+v", st)
	}

	changed := func(typ string) {
		t.Helper()
		ev := events.waitFor(t, typ, nil)
		var obj map[string]any
		if err := json.Unmarshal(ev.Data, &obj); err != nil {
			t.Errorf("%s event data is not a JSON object: %s", typ, ev.Data)
		}
	}

	e.patchSettings(gen.SettingsPatch{Intensity: ptr(int64(3))})
	changed("settings")

	e.uploadList("l.txt", "00\n", nil)
	changed("lists")

	fk := newFake(t)
	e.connect(fk.Addr())
	events.waitFor(t, "status", func(d json.RawMessage) bool {
		var st gen.Status
		return json.Unmarshal(d, &st) == nil && st.Connected
	})

	e.loadROM(testROM)
	events.waitFor(t, "status", func(d json.RawMessage) bool {
		var st gen.Status
		return json.Unmarshal(d, &st) == nil && st.Game != nil && st.Game.State == gen.GameStateRunning
	})

	e.selectDomains(vram)
	changed("domains")

	layer := e.blast()
	b := decodeEvent[gen.BlastEvent](t, events.waitFor(t, "blast", nil))
	if b.Count != len(layer.Units) || b.Engine != gen.EngineNightmare {
		t.Errorf("blast event = %+v, layer has %d units", b, len(layer.Units))
	}

	e.saveSlot(1)
	changed("savestates")
	k := e.corrupt(1)
	changed("stash")
	e.toStockpile(k.Key)
	changed("stockpile")

	r, err := e.c.PauseEmulatorWithResponse(e.ctx)
	expectStatus(t, r, err, http.StatusOK)
	events.waitFor(t, "status", func(d json.RawMessage) bool {
		var st gen.Status
		return json.Unmarshal(d, &st) == nil && st.Game != nil && st.Game.State == gen.GameStatePaused
	})
}

// A running emulator produces frame events.
func TestFrameEvents(t *testing.T) {
	e := newEnv(t, envOptions{auto: true})
	events := e.openEvents()
	var last int64
	for range 3 {
		fe := decodeEvent[gen.FrameEvent](t, events.waitFor(t, "frame", nil))
		if fe.Frame <= last {
			t.Errorf("frame %d after %d", fe.Frame, last)
		}
		last = fe.Frame
	}
}

// Every subscriber gets its own stream.
func TestEventsTwoSubscribers(t *testing.T) {
	e := newEnv(t, envOptions{noConnect: true})
	a, b := e.openEvents(), e.openEvents()
	for _, s := range []*sseStream{a, b} {
		if ev := s.next(t); ev.Type != "status" {
			t.Fatalf("first event %q", ev.Type)
		}
	}
	e.patchSettings(gen.SettingsPatch{Intensity: ptr(int64(2))})
	a.waitFor(t, "settings", nil)
	b.waitFor(t, "settings", nil)
}
