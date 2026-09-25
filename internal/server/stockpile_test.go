package server_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

// stockpileEnv puts three corrupted keys into the stockpile. Slot 1 holds
// probe = 01020304.
func stockpileEnv(t *testing.T) (*env, []gen.StashKey) {
	e, _ := harvesterEnv(t)
	var keys []gen.StashKey
	for range 3 {
		keys = append(keys, e.toStockpile(e.corrupt(1).Key))
	}
	if got := keysOf(e.stockpile()); !slices.Equal(got, keysOf(keys)) {
		t.Fatalf("stockpile = %v, want %v", got, keysOf(keys))
	}
	if n := len(e.stash()); n != 0 {
		t.Fatalf("stash still has %d keys", n)
	}
	return e, keys
}

func TestStockpileManage(t *testing.T) {
	e, keys := stockpileEnv(t)
	k0, k1, k2 := keys[0].Key, keys[1].Key, keys[2].Key

	t.Run("rename", func(t *testing.T) {
		r, err := e.c.PatchStockpileKeyWithResponse(e.ctx, k1, gen.StashKeyPatch{Alias: ptr("second"), Note: ptr("n")})
		expectStatus(t, r, err, http.StatusOK)
		if r.JSON200.Alias != "second" || r.JSON200.Note != "n" {
			t.Errorf("patched = %+v", r.JSON200)
		}
		if sp := e.stockpile(); sp[1].Alias != "second" || sp[1].Note != "n" {
			t.Errorf("stockpile[1] = %+v", sp[1])
		}
	})

	t.Run("reorder", func(t *testing.T) {
		r, err := e.c.ReorderStockpileWithResponse(e.ctx, gen.KeysRequest{Keys: []string{k2, k0, k1}})
		expectStatus(t, r, err, http.StatusOK)
		if got := keysOf(*r.JSON200); !slices.Equal(got, []string{k2, k0, k1}) {
			t.Errorf("reorder returned %v", got)
		}
		if got := keysOf(e.stockpile()); !slices.Equal(got, []string{k2, k0, k1}) {
			t.Errorf("stockpile = %v", got)
		}
		for _, bad := range [][]string{{k2, k0}, {k2, k0, k1, k1}, {k2, k0, "nope"}} {
			r, err := e.c.ReorderStockpileWithResponse(e.ctx, gen.KeysRequest{Keys: bad})
			expectError(t, r, err, http.StatusBadRequest, "INVALID_ARGUMENT")
		}
		if got := keysOf(e.stockpile()); !slices.Equal(got, []string{k2, k0, k1}) {
			t.Errorf("rejected reorder changed the order: %v", got)
		}
	})

	t.Run("layer", func(t *testing.T) {
		l := e.stockpileLayer(k0)
		if len(l.Units) != 4 {
			t.Errorf("layer = %+v, want 4 units", l)
		}
		l.Note = "stockpiled"
		r, err := e.c.PutStockpileLayerWithResponse(e.ctx, k0, l)
		expectStatus(t, r, err, http.StatusOK)
		if got := e.stockpileLayer(k0); !reflect.DeepEqual(got, l) {
			t.Errorf("GET after PUT = %+v", got)
		}
	})

	t.Run("run", func(t *testing.T) {
		e.writeMem(vram, probe, "ffffffff")
		r, err := e.c.RunStockpileKeyWithResponse(e.ctx, k0)
		expectStatus(t, r, err, http.StatusNoContent)
		if got := e.readMem(vram, probe, 4); got != "01020304" {
			t.Errorf("probe = %s, want the key's state", got)
		}
		if n := len(e.units()); n != 4 {
			t.Errorf("%d units scheduled, want 4", n)
		}
	})

	t.Run("inject from stockpile", func(t *testing.T) {
		r, err := e.c.InjectStashWithResponse(e.ctx, gen.InjectRequest{Key: k0, Slot: 1})
		expectStatus(t, r, err, http.StatusOK)
		if r.JSON200.Layer == nil || r.JSON200.Layer.Note != "stockpiled" {
			t.Errorf("injected = %+v", r.JSON200)
		}
		if n := len(e.stash()); n != 1 {
			t.Errorf("inject added %d stash keys, want 1", n)
		}
	})

	t.Run("remove", func(t *testing.T) {
		r, err := e.c.DeleteStockpileKeyWithResponse(e.ctx, k0)
		expectStatus(t, r, err, http.StatusNoContent)
		if got := keysOf(e.stockpile()); !slices.Equal(got, []string{k2, k1}) {
			t.Errorf("stockpile = %v", got)
		}
		r, err = e.c.DeleteStockpileKeyWithResponse(e.ctx, k0)
		expectError(t, r, err, http.StatusNotFound, "NOT_FOUND")
	})

	t.Run("unknown key", func(t *testing.T) {
		rr, err := e.c.RunStockpileKeyWithResponse(e.ctx, "nope")
		expectError(t, rr, err, http.StatusNotFound, "NOT_FOUND")
		rp, err := e.c.PatchStockpileKeyWithResponse(e.ctx, "nope", gen.StashKeyPatch{})
		expectError(t, rp, err, http.StatusNotFound, "NOT_FOUND")
		rl, err := e.c.GetStockpileLayerWithResponse(e.ctx, "nope")
		expectError(t, rl, err, http.StatusNotFound, "NOT_FOUND")
	})

	t.Run("clear", func(t *testing.T) {
		r, err := e.c.ClearStockpileWithResponse(e.ctx)
		expectStatus(t, r, err, http.StatusNoContent)
		if sp := e.stockpile(); len(sp) != 0 {
			t.Errorf("stockpile after clear = %+v", sp)
		}
	})
}

func readZip(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name], err = io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func TestStockpileExportImport(t *testing.T) {
	e, keys := stockpileEnv(t)
	e.patchStockpileAlias(keys[0].Key, "first")
	layers := map[string]gen.Layer{}
	for _, k := range keys {
		layers[k.Key] = e.stockpileLayer(k.Key)
	}

	r, err := e.c.ExportStockpileWithResponse(e.ctx)
	expectStatus(t, r, err, http.StatusOK)
	if ct := r.HTTPResponse.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := r.HTTPResponse.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".sks") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	sks := r.Body
	files := readZip(t, sks)
	if !json.Valid(files["stockpile.json"]) {
		t.Errorf("stockpile.json missing or invalid; files: %v", mapKeys(files))
	}
	if _, ok := files["states/"+keys[0].ParentKey+".state"]; !ok {
		t.Errorf("state blob missing; files: %v", mapKeys(files))
	}

	// A fresh core (new data dir, new emulator) restores keys, layers and states.
	e2 := newEnv(t, envOptions{})
	ct, body := multipartBody(t, "export.sks", sks, nil)
	ri, err := e2.c.ImportStockpileWithBodyWithResponse(e2.ctx, nil, ct, body)
	expectStatus(t, ri, err, http.StatusOK)
	if got := keysOf(*ri.JSON200); !slices.Equal(got, keysOf(keys)) {
		t.Fatalf("imported %v, want %v", got, keysOf(keys))
	}
	sp := e2.stockpile()
	if sp[0].Alias != "first" || sp[0].ParentKey != keys[0].ParentKey || sp[0].Game != keys[0].Game {
		t.Errorf("imported key = %+v", sp[0])
	}
	for _, k := range keys {
		if got := e2.stockpileLayer(k.Key); !reflect.DeepEqual(got, layers[k.Key]) {
			t.Errorf("layer of %s = %+v, want %+v", k.Key, got, layers[k.Key])
		}
	}
	rr, err := e2.c.RunStockpileKeyWithResponse(e2.ctx, keys[1].Key)
	expectStatus(t, rr, err, http.StatusNoContent)
	if got := e2.readMem(vram, probe, 4); got != "01020304" {
		t.Errorf("probe after running an imported key = %s", got)
	}
	if _, err := os.Stat(filepath.Join(e2.dataDir, "states", keys[1].ParentKey+".state")); err != nil {
		t.Errorf("imported state not stored: %v", err)
	}

	t.Run("merge", func(t *testing.T) {
		rd, err := e2.c.DeleteStockpileKeyWithResponse(e2.ctx, keys[0].Key)
		expectStatus(t, rd, err, http.StatusNoContent)
		ct, body := multipartBody(t, "export.sks", sks, nil)
		ri, err := e2.c.ImportStockpileWithBodyWithResponse(e2.ctx, &gen.ImportStockpileParams{Merge: ptr(true)}, ct, body)
		expectStatus(t, ri, err, http.StatusOK)
		// Keys already present are skipped; the removed one is appended.
		want := []string{keys[1].Key, keys[2].Key, keys[0].Key}
		if got := keysOf(*ri.JSON200); !slices.Equal(got, want) {
			t.Errorf("merged %v, want %v", got, want)
		}
	})

	t.Run("replace", func(t *testing.T) {
		ct, body := multipartBody(t, "export.sks", sks, nil)
		ri, err := e2.c.ImportStockpileWithBodyWithResponse(e2.ctx, &gen.ImportStockpileParams{Merge: ptr(false)}, ct, body)
		expectStatus(t, ri, err, http.StatusOK)
		if got := keysOf(*ri.JSON200); !slices.Equal(got, keysOf(keys)) {
			t.Errorf("replaced with %v", got)
		}
	})

	t.Run("not a zip", func(t *testing.T) {
		ct, body := multipartBody(t, "junk.sks", []byte("not a zip"), nil)
		ri, err := e2.c.ImportStockpileWithBodyWithResponse(e2.ctx, nil, ct, body)
		expectError(t, ri, err, http.StatusBadRequest, "INVALID_ARGUMENT")
	})
}

func TestStockpileSaveLoad(t *testing.T) {
	e, keys := stockpileEnv(t)
	path := filepath.Join(t.TempDir(), "mine.sks")

	r, err := e.c.SaveStockpileWithResponse(e.ctx, gen.PathRequest{Path: path})
	expectStatus(t, r, err, http.StatusNoContent)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("saved file: %v", err)
	}
	if _, ok := readZip(t, b)["stockpile.json"]; !ok {
		t.Error("saved .sks has no stockpile.json")
	}

	rc, err := e.c.ClearStockpileWithResponse(e.ctx)
	expectStatus(t, rc, err, http.StatusNoContent)

	rl, err := e.c.LoadStockpileWithResponse(e.ctx, gen.PathRequest{Path: path})
	expectStatus(t, rl, err, http.StatusOK)
	if got := keysOf(*rl.JSON200); !slices.Equal(got, keysOf(keys)) {
		t.Errorf("loaded %v, want %v", got, keysOf(keys))
	}
	if got := keysOf(e.stockpile()); !slices.Equal(got, keysOf(keys)) {
		t.Errorf("stockpile = %v", got)
	}

	rl, err = e.c.LoadStockpileWithResponse(e.ctx, gen.PathRequest{Path: filepath.Join(t.TempDir(), "missing.sks")})
	expectError(t, rl, err, http.StatusNotFound, "NOT_FOUND")
}

func (e *env) patchStockpileAlias(key, alias string) {
	e.t.Helper()
	r, err := e.c.PatchStockpileKeyWithResponse(e.ctx, key, gen.StashKeyPatch{Alias: &alias})
	expectStatus(e.t, r, err, http.StatusOK)
}

func mapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
