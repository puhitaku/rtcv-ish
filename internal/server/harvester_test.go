package server_test

import (
	"net/http"
	"reflect"
	"slices"
	"testing"

	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

const probe = 0x3000 // VRAM address the harvester tests watch

func TestSavestates(t *testing.T) {
	e := newEnv(t, envOptions{})

	slots := e.savestates()
	if len(slots) != 50 {
		t.Fatalf("%d slots, want 50", len(slots))
	}
	for i, s := range slots {
		if s.Slot != i+1 || s.Key != nil || s.Label != "" || s.Game != nil {
			t.Errorf("fresh slot %d = %+v", i, s)
		}
	}

	e.writeMem(vram, probe, "01020304")
	s1 := e.saveSlot(1)
	if s1.Slot != 1 || s1.Key == nil || *s1.Key == "" {
		t.Fatalf("save slot 1 = %+v", s1)
	}
	wantGame := gen.GameInfo{Title: testTitle, Code: "FAKE", RomPath: testROM, System: "nds"}
	if s1.Game == nil || *s1.Game != wantGame {
		t.Errorf("slot game = %+v, want %+v", s1.Game, wantGame)
	}
	if got := e.savestates()[0]; !reflect.DeepEqual(got, s1) {
		t.Errorf("GET slot 1 = %+v, want %+v", got, s1)
	}
	if n := len(e.stash()); n != 0 {
		t.Errorf("saving a state added %d stash entries", n)
	}

	r, err := e.c.PatchSavestateWithResponse(e.ctx, 1, gen.LabelRequest{Label: "boss fight"})
	expectStatus(t, r, err, http.StatusOK)
	if r.JSON200.Label != "boss fight" || r.JSON200.Key == nil || *r.JSON200.Key != *s1.Key {
		t.Errorf("patch label = %+v", r.JSON200)
	}

	e.writeMem(vram, probe, "ffffffff")
	e.applyLayer(gen.Layer{Units: []gen.Unit{valueUnit(vram, 0x10, "01")}}, false)
	rl, err := e.c.LoadSavestateWithResponse(e.ctx, 1)
	expectStatus(t, rl, err, http.StatusNoContent)
	if got := e.readMem(vram, probe, 4); got != "01020304" {
		t.Errorf("after load: %s, want 01020304", got)
	}
	if n := len(e.units()); n != 0 {
		t.Errorf("load left %d units scheduled", n)
	}

	// Saving again replaces the state but keeps the label.
	e.writeMem(vram, probe, "0a0b0c0d")
	s1b := e.saveSlot(1)
	if s1b.Key == nil || *s1b.Key == *s1.Key || s1b.Label != "boss fight" {
		t.Errorf("re-save = %+v", s1b)
	}

	rd, err := e.c.DeleteSavestateWithResponse(e.ctx, 1)
	expectStatus(t, rd, err, http.StatusNoContent)
	if got := e.savestates()[0]; got.Key != nil || got.Label != "" {
		t.Errorf("after delete: %+v", got)
	}

	t.Run("errors", func(t *testing.T) {
		r, err := e.c.LoadSavestateWithResponse(e.ctx, 1)
		expectError(t, r, err, http.StatusNotFound, "NOT_FOUND")
		for _, slot := range []int{0, 51} {
			r, err := e.c.SaveSavestateWithResponse(e.ctx, slot)
			expectError(t, r, err, http.StatusBadRequest, "INVALID_ARGUMENT")
		}
	})
}

// harvesterEnv has slot 1 saved with probe = 01020304 and Nightmare set
// to 4 units on VRAM.
func harvesterEnv(t *testing.T) (*env, gen.SavestateSlot) {
	e := newEnv(t, envOptions{})
	e.useNightmare(4)
	e.writeMem(vram, probe, "01020304")
	return e, e.saveSlot(1)
}

func TestStashCorrupt(t *testing.T) {
	e, s1 := harvesterEnv(t)
	e.writeMem(vram, probe, "ffffffff")

	k := e.corrupt(1)
	if k.Key == "" || k.Key == *s1.Key || k.ParentKey != *s1.Key {
		t.Errorf("key %q parent %q, want a new key with parent %q", k.Key, k.ParentKey, *s1.Key)
	}
	if k.Alias != k.Key || k.Note != "" || k.CreatedAt.IsZero() {
		t.Errorf("new key = %+v", k)
	}
	if k.Game != *s1.Game || !reflect.DeepEqual(k.SelectedDomains, []string{vram}) {
		t.Errorf("game %+v domains %v", k.Game, k.SelectedDomains)
	}
	if k.Layer == nil || len(k.Layer.Units) != 4 || k.UnitCount != 4 {
		t.Fatalf("layer = %+v, unitCount %d; want 4 units", k.Layer, k.UnitCount)
	}
	// loadBefore restored the slot's state before the layer was applied.
	if got := e.readMem(vram, probe, 4); got != "01020304" {
		t.Errorf("probe after corrupt = %s, want the slot's state", got)
	}
	if n := len(e.units()); n != 4 {
		t.Errorf("%d units scheduled, want 4", n)
	}
	if st := e.status(); !st.BlastLayer.Available || !st.BlastLayer.On {
		t.Errorf("corrupt applies with backup: blastLayer = %+v", st.BlastLayer)
	}

	hist := e.stash()
	if len(hist) != 1 || hist[0].Key != k.Key || hist[0].Layer != nil || hist[0].UnitCount != 4 {
		t.Errorf("stash = %+v", hist)
	}
	if got := e.stashLayer(k.Key); !reflect.DeepEqual(got, *k.Layer) {
		t.Errorf("GET layer = %+v, want %+v", got, *k.Layer)
	}

	t.Run("without loadBefore", func(t *testing.T) {
		e.clearUnits()
		e.writeMem(vram, probe, "eeeeeeee")
		r, err := e.c.CorruptStashWithResponse(e.ctx, gen.CorruptRequest{Slot: 1, LoadBefore: false})
		expectStatus(t, r, err, http.StatusOK)
		if got := e.readMem(vram, probe, 4); got != "eeeeeeee" {
			t.Errorf("probe = %s, want untouched", got)
		}
		if r.JSON200.ParentKey != *s1.Key {
			t.Errorf("parent = %q", r.JSON200.ParentKey)
		}
		if n := len(e.stash()); n != 2 {
			t.Errorf("stash has %d keys, want 2", n)
		}
	})

	t.Run("empty slot", func(t *testing.T) {
		r, err := e.c.CorruptStashWithResponse(e.ctx, gen.CorruptRequest{Slot: 2, LoadBefore: true})
		expectError(t, r, err, http.StatusNotFound, "NOT_FOUND")
	})
}

func TestStashOperations(t *testing.T) {
	e, s1 := harvesterEnv(t)
	k1 := e.corrupt(1)
	l1 := *k1.Layer
	e.fake.Tick(1)

	t.Run("run", func(t *testing.T) {
		e.writeMem(vram, probe, "ffffffff")
		r, err := e.c.RunStashKeyWithResponse(e.ctx, k1.Key)
		expectStatus(t, r, err, http.StatusNoContent)
		if got := e.readMem(vram, probe, 4); got != "01020304" {
			t.Errorf("probe = %s, want the key's state", got)
		}
		if got := e.units(); len(got) != 4 || !slices.Equal(unitTargets(got), layerTargets(l1)) {
			t.Errorf("scheduled %+v, want the key's layer", got)
		}
	})

	t.Run("original", func(t *testing.T) {
		e.writeMem(vram, probe, "ffffffff")
		r, err := e.c.OriginalStashKeyWithResponse(e.ctx, k1.Key)
		expectStatus(t, r, err, http.StatusNoContent)
		if got := e.readMem(vram, probe, 4); got != "01020304" {
			t.Errorf("probe = %s, want the key's state", got)
		}
		if n := len(e.units()); n != 0 {
			t.Errorf("original scheduled %d units", n)
		}
	})

	var k2 gen.StashKey
	t.Run("reroll", func(t *testing.T) {
		r, err := e.c.RerollStashKeyWithResponse(e.ctx, k1.Key)
		expectStatus(t, r, err, http.StatusOK)
		k2 = *r.JSON200
		if k2.Key == k1.Key || k2.ParentKey != k1.ParentKey || k2.Layer == nil || len(k2.Layer.Units) != 4 {
			t.Fatalf("rerolled key = %+v", k2)
		}
		if !slices.Equal(layerTargets(*k2.Layer), layerTargets(l1)) {
			t.Errorf("reroll moved units (reroll.address is false by default)")
		}
		if reflect.DeepEqual(*k2.Layer, l1) {
			t.Errorf("reroll did not change any value")
		}
		if n := len(e.units()); n != 4 {
			t.Errorf("reroll runs the new key: %d units scheduled", n)
		}
		if got := keysOf(e.stash()); !reflect.DeepEqual(got, []string{k1.Key, k2.Key}) {
			t.Errorf("stash = %v", got)
		}
	})

	t.Run("inject", func(t *testing.T) {
		e.writeMem(vram, probe, "0a0a0a0a")
		s2 := e.saveSlot(2)
		e.writeMem(vram, probe, "ffffffff")
		r, err := e.c.InjectStashWithResponse(e.ctx, gen.InjectRequest{Key: k1.Key, Slot: 2})
		expectStatus(t, r, err, http.StatusOK)
		k := *r.JSON200
		if k.Key == k1.Key || k.ParentKey != *s2.Key || k.Layer == nil || !reflect.DeepEqual(k.Layer.Units, l1.Units) {
			t.Errorf("injected key = %+v, want k1's layer on slot 2", k)
		}
		if got := e.readMem(vram, probe, 4); got != "0a0a0a0a" {
			t.Errorf("probe = %s, want slot 2's state", got)
		}
		if n := len(e.units()); n != 4 {
			t.Errorf("%d units scheduled, want 4", n)
		}
	})

	t.Run("merge", func(t *testing.T) {
		r, err := e.c.MergeStashWithResponse(e.ctx, gen.KeysRequest{Keys: []string{k1.Key, k2.Key}})
		expectStatus(t, r, err, http.StatusOK)
		k := *r.JSON200
		want := distinctUnits(append(append([]gen.Unit{}, l1.Units...), k2.Layer.Units...))
		if k.ParentKey != k1.ParentKey || k.Layer == nil || !reflect.DeepEqual(k.Layer.Units, want) {
			t.Errorf("merged key = %+v, want parent %q and %d units", k, k1.ParentKey, len(want))
		}
		if n := len(e.units()); n != len(want) {
			t.Errorf("%d units scheduled, want %d", n, len(want))
		}

		r, err = e.c.MergeStashWithResponse(e.ctx, gen.KeysRequest{Keys: []string{k1.Key}})
		expectError(t, r, err, http.StatusBadRequest, "INVALID_ARGUMENT")
		r, err = e.c.MergeStashWithResponse(e.ctx, gen.KeysRequest{Keys: []string{k1.Key, "nope"}})
		expectError(t, r, err, http.StatusNotFound, "NOT_FOUND")
	})

	t.Run("layer round trip", func(t *testing.T) {
		edited := l1
		edited.Note = "edited"
		edited.Units = append([]gen.Unit{}, l1.Units...)
		edited.Units[0].Enabled = false
		edited.Units = append(edited.Units, valueUnit(vram, 0x20, "abcd"))
		r, err := e.c.PutStashLayerWithResponse(e.ctx, k1.Key, edited)
		expectStatus(t, r, err, http.StatusOK)
		if !reflect.DeepEqual(*r.JSON200, edited) {
			t.Errorf("PUT returned %+v", *r.JSON200)
		}
		if got := e.stashLayer(k1.Key); !reflect.DeepEqual(got, edited) {
			t.Errorf("GET after PUT = %+v", got)
		}
		for _, k := range e.stash() {
			if k.Key == k1.Key && k.UnitCount != 5 {
				t.Errorf("unitCount = %d, want 5", k.UnitCount)
			}
		}

		bad := edited
		bad.Units = []gen.Unit{valueUnit(vram, 0, "00")}
		bad.Units[0].Precision = 0
		rb, err := e.c.PutStashLayerWithResponse(e.ctx, k1.Key, bad)
		expectError(t, rb, err, http.StatusBadRequest, "INVALID_ARGUMENT")
	})

	t.Run("patch", func(t *testing.T) {
		r, err := e.c.PatchStashKeyWithResponse(e.ctx, k1.Key, gen.StashKeyPatch{Alias: ptr("wobbly"), Note: ptr("sprites")})
		expectStatus(t, r, err, http.StatusOK)
		if r.JSON200.Alias != "wobbly" || r.JSON200.Note != "sprites" || r.JSON200.Key != k1.Key {
			t.Errorf("patched = %+v", r.JSON200)
		}
		r, err = e.c.PatchStashKeyWithResponse(e.ctx, k1.Key, gen.StashKeyPatch{Note: ptr("")})
		expectStatus(t, r, err, http.StatusOK)
		if r.JSON200.Alias != "wobbly" || r.JSON200.Note != "" {
			t.Errorf("partial patch = %+v", r.JSON200)
		}
	})

	t.Run("to stockpile", func(t *testing.T) {
		r, err := e.c.StashKeyToStockpileWithResponse(e.ctx, k1.Key, gen.ToStockpileRequest{Alias: ptr("keeper")})
		expectStatus(t, r, err, http.StatusOK)
		if r.JSON200.Key != k1.Key || r.JSON200.Alias != "keeper" {
			t.Errorf("moved = %+v", r.JSON200)
		}
		for _, k := range e.stash() {
			if k.Key == k1.Key {
				t.Error("key still in stash")
			}
		}
		sp := e.stockpile()
		if len(sp) != 1 || sp[0].Key != k1.Key || sp[0].Alias != "keeper" || sp[0].ParentKey != *s1.Key {
			t.Errorf("stockpile = %+v", sp)
		}
		if got := e.stockpileLayer(k1.Key); got.Note != "edited" {
			t.Errorf("stockpile layer = %+v", got)
		}
	})

	t.Run("delete and clear", func(t *testing.T) {
		n := len(e.stash())
		rd, err := e.c.DeleteStashKeyWithResponse(e.ctx, k2.Key)
		expectStatus(t, rd, err, http.StatusNoContent)
		if got := len(e.stash()); got != n-1 {
			t.Errorf("stash has %d keys, want %d", got, n-1)
		}
		rc, err := e.c.ClearStashWithResponse(e.ctx)
		expectStatus(t, rc, err, http.StatusNoContent)
		if got := e.stash(); len(got) != 0 {
			t.Errorf("stash after clear = %+v", got)
		}
		if n := len(e.stockpile()); n != 1 {
			t.Errorf("clearing the stash touched the stockpile (%d keys)", n)
		}
	})

	t.Run("unknown key", func(t *testing.T) {
		calls := map[string]func() (response, error){
			"run":      func() (response, error) { return e.c.RunStashKeyWithResponse(e.ctx, "nope") },
			"original": func() (response, error) { return e.c.OriginalStashKeyWithResponse(e.ctx, "nope") },
			"reroll":   func() (response, error) { return e.c.RerollStashKeyWithResponse(e.ctx, "nope") },
			"patch": func() (response, error) {
				return e.c.PatchStashKeyWithResponse(e.ctx, "nope", gen.StashKeyPatch{Note: ptr("x")})
			},
			"delete":   func() (response, error) { return e.c.DeleteStashKeyWithResponse(e.ctx, "nope") },
			"getLayer": func() (response, error) { return e.c.GetStashLayerWithResponse(e.ctx, "nope") },
			"putLayer": func() (response, error) { return e.c.PutStashLayerWithResponse(e.ctx, "nope", l1) },
			"toStockpile": func() (response, error) {
				return e.c.StashKeyToStockpileWithResponse(e.ctx, "nope", gen.ToStockpileRequest{})
			},
			"inject": func() (response, error) {
				return e.c.InjectStashWithResponse(e.ctx, gen.InjectRequest{Key: "nope", Slot: 1})
			},
			// k1 is in the stockpile now, not in the stash.
			"moved": func() (response, error) { return e.c.DeleteStashKeyWithResponse(e.ctx, k1.Key) },
		}
		for name, call := range calls {
			t.Run(name, func(t *testing.T) {
				r, err := call()
				expectError(t, r, err, http.StatusNotFound, "NOT_FOUND")
			})
		}
	})
}

func TestProtection(t *testing.T) {
	e := newEnv(t, envOptions{})

	r, err := e.c.ProtectionBackWithResponse(e.ctx)
	expectError(t, r, err, http.StatusConflict, "NO_BACKUP")

	e.writeMem(vram, probe, "aaaaaaaa")
	rb, err := e.c.ProtectionBackupWithResponse(e.ctx)
	expectStatus(t, rb, err, http.StatusNoContent)
	e.writeMem(vram, probe, "bbbbbbbb")
	rb, err = e.c.ProtectionBackupWithResponse(e.ctx)
	expectStatus(t, rb, err, http.StatusNoContent)
	if n := e.status().ProtectionBackups; n != 2 {
		t.Errorf("protectionBackups = %d, want 2", n)
	}

	e.writeMem(vram, probe, "cccccccc")
	for _, want := range []string{"bbbbbbbb", "aaaaaaaa"} {
		r, err := e.c.ProtectionBackWithResponse(e.ctx)
		expectStatus(t, r, err, http.StatusNoContent)
		if got := e.readMem(vram, probe, 4); got != want {
			t.Errorf("after back: %s, want %s", got, want)
		}
	}
	if n := e.status().ProtectionBackups; n != 0 {
		t.Errorf("protectionBackups = %d, want 0", n)
	}
	r, err = e.c.ProtectionBackWithResponse(e.ctx)
	expectError(t, r, err, http.StatusConflict, "NO_BACKUP")
}

func TestProtectionLast(t *testing.T) {
	e := newEnv(t, envOptions{})

	r, err := e.c.ProtectionLastWithResponse(e.ctx)
	expectError(t, r, err, http.StatusConflict, "NO_BACKUP")

	e.writeMem(vram, probe, "aaaaaaaa")
	rb, err := e.c.ProtectionBackupWithResponse(e.ctx)
	expectStatus(t, rb, err, http.StatusNoContent)

	for i, dirty := range []string{"bbbbbbbb", "cccccccc"} {
		e.writeMem(vram, probe, dirty)
		r, err := e.c.ProtectionLastWithResponse(e.ctx)
		expectStatus(t, r, err, http.StatusNoContent)
		if got := e.readMem(vram, probe, 4); got != "aaaaaaaa" {
			t.Errorf("after last #%d: %s, want aaaaaaaa", i+1, got)
		}
		if n := e.status().ProtectionBackups; n != 1 {
			t.Errorf("after last #%d: protectionBackups = %d, want 1", i+1, n)
		}
	}

	e.writeMem(vram, probe, "dddddddd")
	rback, err := e.c.ProtectionBackWithResponse(e.ctx)
	expectStatus(t, rback, err, http.StatusNoContent)
	if got := e.readMem(vram, probe, 4); got != "aaaaaaaa" {
		t.Errorf("after back: %s, want aaaaaaaa", got)
	}
	if n := e.status().ProtectionBackups; n != 0 {
		t.Errorf("after back: protectionBackups = %d, want 0", n)
	}
	r, err = e.c.ProtectionLastWithResponse(e.ctx)
	expectError(t, r, err, http.StatusConflict, "NO_BACKUP")
}

func distinctUnits(us []gen.Unit) []gen.Unit {
	var out []gen.Unit
	for _, u := range us {
		dup := false
		for _, o := range out {
			if reflect.DeepEqual(u, o) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, u)
		}
	}
	return out
}
