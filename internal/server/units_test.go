package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

func infinite(u gen.Unit) gen.Unit {
	u.Lifetime = 0
	return u
}

// waitUnits waits for a units event with the reason and returns it.
func waitUnits(t *testing.T, s *sseStream, reason gen.UnitsReason) gen.UnitsEvent {
	t.Helper()
	ev := s.waitFor(t, "units", func(d json.RawMessage) bool {
		var u gen.UnitsEvent
		return json.Unmarshal(d, &u) == nil && u.Reason == reason
	})
	return decodeEvent[gen.UnitsEvent](t, ev)
}

func TestUnitsEvents(t *testing.T) {
	e := newEnv(t, envOptions{})
	events := e.openEvents()
	events.waitFor(t, "status", nil)
	freeze := gen.Layer{Units: []gen.Unit{infinite(valueUnit(vram, 0x10, "aa")), infinite(valueUnit(vram, 0x20, "bb"))}}

	e.applyLayer(freeze, false)
	waitUnits(t, events, gen.UnitsReasonApply)

	e.clearUnits()
	waitUnits(t, events, gen.UnitsReasonClear)

	// Savestate loads clear the units and say how many.
	e.saveSlot(1)
	e.applyLayer(freeze, false)
	waitUnits(t, events, gen.UnitsReasonApply)
	r, err := e.c.LoadSavestateWithResponse(e.ctx, 1)
	expectStatus(t, r, err, http.StatusNoContent)
	if ev := waitUnits(t, events, gen.UnitsReasonLoad); ev.Cleared != 2 {
		t.Errorf("load cleared = %d, want 2", ev.Cleared)
	}
	if us := e.units(); len(us) != 0 {
		t.Errorf("%d units after load", len(us))
	}

	e.applyLayer(freeze, false)
	waitUnits(t, events, gen.UnitsReasonApply)
	rr, err := e.c.ResetEmulatorWithResponse(e.ctx)
	expectStatus(t, rr, err, http.StatusOK)
	if ev := waitUnits(t, events, gen.UnitsReasonReset); ev.Cleared != 2 {
		t.Errorf("reset cleared = %d, want 2", ev.Cleared)
	}

	e.useNightmare(4)
	e.blast()
	waitUnits(t, events, gen.UnitsReasonApply)
	events.waitFor(t, "blast", nil)

	// Max infinite units evicts the oldest (intensity is capped at it).
	e.patchSettings(gen.SettingsPatch{MaxInfiniteUnits: ptr(1), Engine: ptr(gen.EngineFreeze)})
	e.blast()
	e.blast()
	waitUnits(t, events, gen.UnitsReasonRemove)

	e.loadROM(testROM)
	waitUnits(t, events, gen.UnitsReasonGame)

	dr, err := e.c.DisconnectEmulatorWithResponse(e.ctx)
	expectStatus(t, dr, err, http.StatusOK)
	waitUnits(t, events, gen.UnitsReasonDisconnect)
}

func TestUnitsEventToggle(t *testing.T) {
	e := newEnv(t, envOptions{})
	events := e.openEvents()
	e.applyLayer(gen.Layer{Units: []gen.Unit{infinite(valueUnit(vram, 0, "01"))}}, true)
	waitUnits(t, events, gen.UnitsReasonApply)
	r, err := e.c.ToggleLayerWithResponse(e.ctx, gen.ToggleRequest{On: false})
	expectStatus(t, r, err, http.StatusNoContent)
	waitUnits(t, events, gen.UnitsReasonClear)
	waitUnits(t, events, gen.UnitsReasonApply)
}

func TestRemoveUnit(t *testing.T) {
	e := newEnv(t, envOptions{})
	e.applyLayer(gen.Layer{Units: []gen.Unit{infinite(valueUnit(vram, 0x10, "aa")), infinite(valueUnit(vram, 0x20, "bb"))}}, false)
	us := e.units()
	if len(us) != 2 {
		t.Fatalf("%d units, want 2", len(us))
	}
	events := e.openEvents()
	r, err := e.c.RemoveUnitWithResponse(e.ctx, us[0].Id)
	expectStatus(t, r, err, http.StatusNoContent)
	waitUnits(t, events, gen.UnitsReasonRemove)
	left := e.units()
	if len(left) != 1 || left[0].Id != us[1].Id {
		t.Fatalf("units after remove = %+v, want only %d", left, us[1].Id)
	}

	r, err = e.c.RemoveUnitWithResponse(e.ctx, us[0].Id)
	expectError(t, r, err, http.StatusNotFound, "NOT_FOUND")
	r, err = e.c.RemoveUnitWithResponse(e.ctx, -1)
	expectError(t, r, err, http.StatusBadRequest, "INVALID_ARGUMENT")
}

// The freeze mode goes to every infinite unit, falling back to what the
// emulator supports; store units get scanline at most, finite units frame.
func TestUnitFreezeMode(t *testing.T) {
	e := newEnv(t, envOptions{})
	caps := e.status().Emulator.Capabilities
	effective := func(m gen.FreezeMode) gen.FreezeMode {
		if m == gen.FreezeModeHard && !caps.HardUnits {
			m = gen.FreezeModeScanline
		}
		if m == gen.FreezeModeScanline && !caps.ScanlineUnits {
			m = gen.FreezeModeFrame
		}
		return m
	}
	atMostScanline := func(m gen.FreezeMode) gen.FreezeMode {
		if m == gen.FreezeModeHard {
			return gen.FreezeModeScanline
		}
		return m
	}
	layer := gen.Layer{Units: []gen.Unit{
		infinite(valueUnit(vram, 0x10, "aa")),
		valueUnit(vram, 0x20, "bb"),
		infinite(storeUnit(vram, 0x30, 1, vram, 0x40, gen.StoreTimePreexecute, gen.StoreTypeContinuous)),
	}}
	for _, m := range []gen.FreezeMode{gen.FreezeModeFrame, gen.FreezeModeScanline, gen.FreezeModeHard} {
		t.Run(string(m), func(t *testing.T) {
			e.clearUnits()
			e.patchSettings(gen.SettingsPatch{FreezeMode: ptr(m)})
			e.applyLayer(layer, false)
			want := map[int64]gen.FreezeMode{
				0x10: effective(m),
				0x20: gen.FreezeModeFrame,
				0x30: atMostScanline(effective(m)),
			}
			us := e.units()
			if len(us) != 3 {
				t.Fatalf("%d units, want 3", len(us))
			}
			for _, u := range us {
				if u.Mode != want[u.Address] {
					t.Errorf("unit at %#x: mode %s, want %s", u.Address, u.Mode, want[u.Address])
				}
			}
		})
	}
}
