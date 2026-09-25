package server_test

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

func (e *env) lists() []gen.ListInfo {
	e.t.Helper()
	r, err := e.c.ListListsWithResponse(e.ctx)
	expectStatus(e.t, r, err, http.StatusOK)
	return *r.JSON200
}

func TestLists(t *testing.T) {
	e := newEnv(t, envOptions{noConnect: true})
	if got := e.lists(); len(got) != 0 {
		t.Fatalf("fresh lists = %+v", got)
	}

	lim := e.uploadList("lim.txt", "ABABABAB\n", nil)
	if want := (gen.ListInfo{Name: "lim", Precision: 4, Entries: 1}); lim != want {
		t.Errorf("upload lim = %+v, want %+v", lim, want)
	}
	vals := e.uploadList("whatever.txt", "12121212\r\n34343434\n\n", map[string]string{"name": "vals"})
	if want := (gen.ListInfo{Name: "vals", Precision: 4, Entries: 2}); vals != want {
		t.Errorf("upload vals = %+v, want %+v", vals, want)
	}
	if _, err := os.Stat(filepath.Join(e.dataDir, "lists", "lim.txt")); err != nil {
		t.Errorf("list file: %v", err)
	}
	if got := e.lists(); !slices.Equal(got, []gen.ListInfo{lim, vals}) {
		t.Errorf("lists = %+v", got)
	}

	// Uploading under an existing name replaces the list.
	if got := e.uploadList("lim.txt", "abab\ncdcd\nefef\n", nil); got != (gen.ListInfo{Name: "lim", Precision: 2, Entries: 3}) {
		t.Errorf("replace lim = %+v", got)
	}

	for name, content := range map[string]string{"empty": "", "not hex": "zz\n", "odd": "abc\n"} {
		t.Run("bad "+name, func(t *testing.T) {
			ct, body := multipartBody(t, "bad.txt", []byte(content), nil)
			r, err := e.c.UploadListWithBodyWithResponse(e.ctx, ct, body)
			expectError(t, r, err, http.StatusBadRequest, "INVALID_ARGUMENT")
		})
	}
	t.Run("bad name", func(t *testing.T) {
		ct, body := multipartBody(t, "x.txt", []byte("00\n"), map[string]string{"name": "../escape"})
		r, err := e.c.UploadListWithBodyWithResponse(e.ctx, ct, body)
		expectError(t, r, err, http.StatusBadRequest, "INVALID_ARGUMENT")
	})

	r, err := e.c.DeleteListWithResponse(e.ctx, "lim")
	expectStatus(t, r, err, http.StatusNoContent)
	if got := e.lists(); len(got) != 1 || got[0].Name != "vals" {
		t.Errorf("lists after delete = %+v", got)
	}
	r, err = e.c.DeleteListWithResponse(e.ctx, "lim")
	expectError(t, r, err, http.StatusNotFound, "NOT_FOUND")

	// Lists placed in data/lists by hand are picked up too.
	if err := os.WriteFile(filepath.Join(e.dataDir, "lists", "manual.txt"), []byte("00ff\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := e.lists(); len(got) != 2 || got[0] != (gen.ListInfo{Name: "manual", Precision: 2, Entries: 1}) {
		t.Errorf("lists = %+v", got)
	}
}

// Vector replaces values found in the limiter list with values from the
// value list.
func TestVectorBlast(t *testing.T) {
	e := newEnv(t, envOptions{})
	e.uploadList("lim.txt", "ABABABAB\n", nil)
	e.uploadList("vals.txt", "12121212\n34343434\n", nil)
	e.writeMem(vram, 0, strings.Repeat("ab", vramSize))
	e.selectDomains(vram)
	e.patchSettings(gen.SettingsPatch{
		Engine:    ptr(gen.EngineVector),
		Intensity: ptr(int64(8)),
		Precision: ptr(gen.PrecisionN4),
		Vector:    &gen.VectorSettingsPatch{LimiterList: ptr("lim"), ValueList: ptr("vals")},
	})

	layer := e.blast()
	if len(layer.Units) != 8 {
		t.Fatalf("vector blast: %d units, want 8 (all of VRAM matches the limiter)", len(layer.Units))
	}
	for i, u := range layer.Units {
		if u.Domain != vram || u.Precision != 4 || u.Address%4 != 0 || u.Source != gen.UnitSourceValue ||
			!u.GeneratedUsingValueList || (u.Value != "12121212" && u.Value != "34343434") {
			t.Errorf("unit %d = %+v", i, u)
		}
	}

	// Nothing matches after clearing VRAM.
	e.writeMem(vram, 0, strings.Repeat("00", vramSize))
	if l := e.blast(); len(l.Units) != 0 {
		t.Errorf("blast without matches: %d units", len(l.Units))
	}
}
