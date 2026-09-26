package server_test

import (
	"bytes"
	"context"
	"image/png"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/puhitaku/rtcv-ish/internal/emu/fake"
	"github.com/puhitaku/rtcv-ish/internal/server"
	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

func TestStatusDisconnected(t *testing.T) {
	e := newEnv(t, envOptions{noConnect: true})
	st := e.status()
	if st.Connected || st.Address != "" || st.Emulator != nil || st.Game != nil {
		t.Errorf("status before connect = %+v, want disconnected without emulator/game", st)
	}
	if st.DataDir != e.dataDir {
		t.Errorf("dataDir = %q, want %q", st.DataDir, e.dataDir)
	}
	if st.Version == "" {
		t.Error("version is empty")
	}
	if st.BlastLayer.Available || st.ProtectionBackups != 0 {
		t.Errorf("fresh status = %+v", st)
	}
}

func TestStatusVersion(t *testing.T) {
	for _, tc := range []struct{ configured, want string }{
		{"", "dev"},
		{"v1.2.3", "v1.2.3"},
	} {
		e := newEnv(t, envOptions{noConnect: true, version: tc.configured})
		if got := e.status().Version; got != tc.want {
			t.Errorf("Config.Version %q: status version = %q, want %q", tc.configured, got, tc.want)
		}
	}
}

func TestStatusVersionInfo(t *testing.T) {
	if got := newEnv(t, envOptions{noConnect: true}).status().VersionInfo; got.Kind != gen.VersionInfoKindDev {
		t.Errorf("default versionInfo = %+v, want kind dev", got)
	}
	in := server.VersionInfo{Release: "v1.2.3", Commit: "abc1234", Dirty: true, Kind: "release"}
	got := newEnv(t, envOptions{noConnect: true, verInfo: in}).status().VersionInfo
	want := gen.VersionInfo{Release: "v1.2.3", Commit: "abc1234", Dirty: true, Kind: gen.VersionInfoKindRelease}
	if got != want {
		t.Errorf("versionInfo = %+v, want %+v", got, want)
	}
}

// Operations that need an emulator fail with 503 EMULATOR_DISCONNECTED
// while none is connected; core-only resources keep working.
func TestDisconnectedErrors(t *testing.T) {
	e := newEnv(t, envOptions{noConnect: true})
	ctx := e.ctx
	c := e.c
	needEmu := map[string]func() (response, error){
		"loadRom":    func() (response, error) { return c.LoadRomWithResponse(ctx, gen.PathRequest{Path: testROM}) },
		"closeRom":   func() (response, error) { return c.CloseRomWithResponse(ctx) },
		"pause":      func() (response, error) { return c.PauseEmulatorWithResponse(ctx) },
		"resume":     func() (response, error) { return c.ResumeEmulatorWithResponse(ctx) },
		"reset":      func() (response, error) { return c.ResetEmulatorWithResponse(ctx) },
		"step":       func() (response, error) { return c.StepEmulatorWithResponse(ctx, gen.StepRequest{Frames: 1}) },
		"quit":       func() (response, error) { return c.QuitEmulatorWithResponse(ctx) },
		"screenshot": func() (response, error) { return c.GetScreenshotWithResponse(ctx) },
		"domains":    func() (response, error) { return c.ListDomainsWithResponse(ctx) },
		"autoSelect": func() (response, error) { return c.AutoSelectDomainsWithResponse(ctx) },
		"readMemory": func() (response, error) {
			return c.ReadMemoryWithResponse(ctx, vram, &gen.ReadMemoryParams{Address: 0, Size: 4})
		},
		"writeMemory": func() (response, error) {
			return c.WriteMemoryWithResponse(ctx, vram, gen.MemoryWrite{Address: 0, Data: "00"})
		},
		"blast": func() (response, error) { return c.ManualBlastWithResponse(ctx) },
		"apply": func() (response, error) {
			return c.ApplyLayerWithResponse(ctx, gen.ApplyRequest{Layer: gen.Layer{Units: []gen.Unit{}}})
		},
		"listUnits":     func() (response, error) { return c.ListUnitsWithResponse(ctx) },
		"clearUnits":    func() (response, error) { return c.ClearUnitsWithResponse(ctx) },
		"saveSavestate": func() (response, error) { return c.SaveSavestateWithResponse(ctx, 1) },
		"corrupt": func() (response, error) {
			return c.CorruptStashWithResponse(ctx, gen.CorruptRequest{Slot: 1, LoadBefore: true})
		},
		"backup": func() (response, error) { return c.ProtectionBackupWithResponse(ctx) },
	}
	for name, call := range needEmu {
		t.Run(name, func(t *testing.T) {
			r, err := call()
			expectError(t, r, err, http.StatusServiceUnavailable, "EMULATOR_DISCONNECTED")
		})
	}
	coreOnly := map[string]func() (response, error){
		"status":     func() (response, error) { return c.GetStatusWithResponse(ctx) },
		"settings":   func() (response, error) { return c.GetSettingsWithResponse(ctx) },
		"emulators":  func() (response, error) { return c.ListEmulatorsWithResponse(ctx) },
		"savestates": func() (response, error) { return c.ListSavestatesWithResponse(ctx) },
		"stash":      func() (response, error) { return c.ListStashWithResponse(ctx) },
		"stockpile":  func() (response, error) { return c.ListStockpileWithResponse(ctx) },
		"lists":      func() (response, error) { return c.ListListsWithResponse(ctx) },
		"disconnect": func() (response, error) { return c.DisconnectEmulatorWithResponse(ctx) },
	}
	for name, call := range coreOnly {
		t.Run(name, func(t *testing.T) {
			r, err := call()
			expectStatus(t, r, err, http.StatusOK)
		})
	}
}

func TestConnectDisconnect(t *testing.T) {
	e := newEnv(t, envOptions{noConnect: true})
	f := newFake(t)
	st := e.connect(f.Addr())
	if !st.Connected || st.Address != f.Addr() {
		t.Fatalf("connect: status = %+v", st)
	}
	want := gen.EmulatorInfo{
		Name: "fake", Version: "0", System: "nds", ProtocolVersion: 1,
		Capabilities: gen.Capabilities{Savestates: true, Screenshot: true, Input: true, LoadRom: true, Reset: true, MaxPayload: fake.MaxPayload, ScanlineUnits: true, HardUnits: true},
	}
	if st.Emulator == nil || *st.Emulator != want {
		t.Errorf("emulator = %+v, want %+v", st.Emulator, want)
	}
	if st.Game == nil || st.Game.State != gen.GameStateNoRom {
		t.Errorf("game = %+v, want state noRom", st.Game)
	}
	if got := e.status(); !got.Connected || got.Address != f.Addr() {
		t.Errorf("GET status after connect = %+v", got)
	}

	t.Run("already connected", func(t *testing.T) {
		r, err := e.c.ConnectEmulatorWithResponse(e.ctx, gen.ConnectRequest{Address: f.Addr()})
		expectError(t, r, err, http.StatusConflict, "ALREADY_CONNECTED")
	})

	r, err := e.c.DisconnectEmulatorWithResponse(e.ctx)
	expectStatus(t, r, err, http.StatusOK)
	if r.JSON200.Connected || r.JSON200.Emulator != nil {
		t.Errorf("disconnect: status = %+v", r.JSON200)
	}
	if e.status().Connected {
		t.Error("still connected after disconnect")
	}
	rp, err := e.c.PauseEmulatorWithResponse(e.ctx)
	expectError(t, rp, err, http.StatusServiceUnavailable, "EMULATOR_DISCONNECTED")

	// The fake accepts one client at a time and releases it asynchronously,
	// so reconnect to a second instance.
	f2 := newFake(t)
	if st := e.connect(f2.Addr()); !st.Connected || st.Address != f2.Addr() {
		t.Errorf("reconnect: status = %+v", st)
	}
}

func TestConnectFailed(t *testing.T) {
	e := newEnv(t, envOptions{noConnect: true})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	r, err := e.c.ConnectEmulatorWithResponse(e.ctx, gen.ConnectRequest{Address: addr})
	expectError(t, r, err, http.StatusBadGateway, "CONNECT_FAILED")
	if e.status().Connected {
		t.Error("connected after a failed connect")
	}
}

func TestGameLifecycle(t *testing.T) {
	e := newEnv(t, envOptions{noROM: true, fake: fake.Options{
		ROMExists: func(p string) bool { return p != "/roms/missing.nds" },
	}})
	ctx := e.ctx

	t.Run("rom not found", func(t *testing.T) {
		r, err := e.c.LoadRomWithResponse(ctx, gen.PathRequest{Path: "/roms/missing.nds"})
		expectError(t, r, err, http.StatusNotFound, "NOT_FOUND")
	})
	t.Run("empty path", func(t *testing.T) {
		r, err := e.c.LoadRomWithResponse(ctx, gen.PathRequest{Path: ""})
		expectError(t, r, err, http.StatusBadRequest, "INVALID_ARGUMENT")
	})

	gs := e.loadROM(testROM)
	want := gen.GameStatus{State: gen.GameStateRunning, Frame: 0, RomPath: testROM, Title: testTitle, Code: "FAKE", Console: "DS"}
	if gs != want {
		t.Fatalf("loadRom = %+v, want %+v", gs, want)
	}
	if st := e.status(); st.Game == nil || *st.Game != want {
		t.Errorf("status.game = %+v, want %+v", st.Game, want)
	}

	rp, err := e.c.PauseEmulatorWithResponse(ctx)
	expectStatus(t, rp, err, http.StatusOK)
	if rp.JSON200.State != gen.GameStatePaused || e.fake.State().String() != "PAUSED" {
		t.Errorf("pause: %+v, fake %v", rp.JSON200, e.fake.State())
	}

	rr, err := e.c.ResumeEmulatorWithResponse(ctx)
	expectStatus(t, rr, err, http.StatusOK)
	if rr.JSON200.State != gen.GameStateRunning || e.fake.State().String() != "RUNNING" {
		t.Errorf("resume: %+v, fake %v", rr.JSON200, e.fake.State())
	}

	rs, err := e.c.StepEmulatorWithResponse(ctx, gen.StepRequest{Frames: 5})
	expectStatus(t, rs, err, http.StatusOK)
	if rs.JSON200.State != gen.GameStatePaused || rs.JSON200.Frame != 5 {
		t.Errorf("step 5: %+v, want paused at frame 5", rs.JSON200)
	}
	if got := e.fake.Frame(); got != 5 {
		t.Errorf("fake frame = %d, want 5", got)
	}
	if st := e.status(); st.Game == nil || st.Game.Frame != 5 || st.Game.State != gen.GameStatePaused {
		t.Errorf("status.game after step = %+v", st.Game)
	}

	t.Run("step 0", func(t *testing.T) {
		r, err := e.c.StepEmulatorWithResponse(ctx, gen.StepRequest{Frames: 0})
		expectError(t, r, err, http.StatusBadRequest, "INVALID_ARGUMENT")
	})

	rreset, err := e.c.ResetEmulatorWithResponse(ctx)
	expectStatus(t, rreset, err, http.StatusOK)
	if rreset.JSON200.Frame != 0 || e.fake.Frame() != 0 {
		t.Errorf("reset: %+v, fake frame %d", rreset.JSON200, e.fake.Frame())
	}

	rc, err := e.c.CloseRomWithResponse(ctx)
	expectStatus(t, rc, err, http.StatusOK)
	if rc.JSON200.State != gen.GameStateNoRom || rc.JSON200.RomPath != "" {
		t.Errorf("closeRom: %+v", rc.JSON200)
	}
	if st := e.status(); st.Game == nil || st.Game.State != gen.GameStateNoRom {
		t.Errorf("status.game after close = %+v", st.Game)
	}

	t.Run("no rom", func(t *testing.T) {
		calls := map[string]func() (response, error){
			"pause": func() (response, error) { return e.c.PauseEmulatorWithResponse(ctx) },
			"step":  func() (response, error) { return e.c.StepEmulatorWithResponse(ctx, gen.StepRequest{Frames: 1}) },
			"readMemory": func() (response, error) {
				return e.c.ReadMemoryWithResponse(ctx, vram, &gen.ReadMemoryParams{Address: 0, Size: 1})
			},
			"blast":      func() (response, error) { return e.c.ManualBlastWithResponse(ctx) },
			"save":       func() (response, error) { return e.c.SaveSavestateWithResponse(ctx, 1) },
			"screenshot": func() (response, error) { return e.c.GetScreenshotWithResponse(ctx) },
		}
		for name, call := range calls {
			t.Run(name, func(t *testing.T) {
				r, err := call()
				expectError(t, r, err, http.StatusConflict, "NO_ROM")
			})
		}
		if d := e.domains(); len(d) != 0 {
			t.Errorf("domains without ROM = %+v, want none", d)
		}
	})
}

func TestScreenshot(t *testing.T) {
	e := newEnv(t, envOptions{})
	r, err := e.c.GetScreenshotWithResponse(e.ctx)
	expectStatus(t, r, err, http.StatusOK)
	if ct := r.HTTPResponse.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	img, err := png.Decode(bytes.NewReader(r.Body))
	if err != nil {
		t.Fatalf("decode PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != fake.ScreenWidth || b.Dy() != 2*fake.ScreenHeight {
		t.Errorf("screenshot is %v, want two %dx%d screens stacked vertically", b, fake.ScreenWidth, fake.ScreenHeight)
	}
}

func TestQuit(t *testing.T) {
	e := newEnv(t, envOptions{})
	r, err := e.c.QuitEmulatorWithResponse(e.ctx)
	expectStatus(t, r, err, http.StatusNoContent)
	select {
	case <-e.fake.Done():
	case <-time.After(eventTimeout):
		t.Fatal("fake emulator did not quit")
	}
	if st := e.status(); st.Connected {
		t.Errorf("status after quit = %+v, want disconnected", st)
	}
}

func buildFakeEmu(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds cmd/rtcv-ish-fakeemu")
	}
	bin := filepath.Join(t.TempDir(), "rtcv-ish-fakeemu")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", bin, "github.com/puhitaku/rtcv-ish/cmd/rtcv-ish-fakeemu")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func TestLaunch(t *testing.T) {
	bin := buildFakeEmu(t)
	missing := filepath.Join(t.TempDir(), "nope")
	e := newEnv(t, envOptions{noConnect: true, emulators: []server.EmulatorSpec{
		{Name: "fake", Path: bin, Args: []string{"-listen", "127.0.0.1:{port}", "-log-format", "json"}},
		{Name: "missing", Path: missing},
	}})

	rl, err := e.c.ListEmulatorsWithResponse(e.ctx)
	expectStatus(t, rl, err, http.StatusOK)
	wantList := []gen.BundledEmulator{
		{Name: "fake", Path: bin, Present: true},
		{Name: "missing", Path: missing, Present: false},
	}
	if got := *rl.JSON200; !slices.Equal(got, wantList) {
		t.Errorf("emulators = %+v, want %+v", got, wantList)
	}

	for _, tc := range []struct{ name, code string }{{"unknown", "NOT_FOUND"}, {"missing", "NOT_FOUND"}} {
		r, err := e.c.LaunchEmulatorWithResponse(e.ctx, gen.LaunchRequest{Name: tc.name})
		expectError(t, r, err, http.StatusNotFound, tc.code)
	}

	r, err := e.c.LaunchEmulatorWithResponse(e.ctx, gen.LaunchRequest{Name: "fake", Rom: ptr("/roms/launched.nds")})
	expectStatus(t, r, err, http.StatusOK)
	st := *r.JSON200
	if !st.Connected || st.Emulator == nil || st.Emulator.Name != "fake" {
		t.Fatalf("launch: status = %+v", st)
	}
	if !strings.HasPrefix(st.Address, "127.0.0.1:") {
		t.Errorf("launch address = %q", st.Address)
	}
	if st.Game == nil || st.Game.State != gen.GameStateRunning || st.Game.Title != "LAUNCHED" {
		t.Errorf("launch game = %+v", st.Game)
	}

	rq, err := e.c.QuitEmulatorWithResponse(e.ctx)
	expectStatus(t, rq, err, http.StatusNoContent)
	if e.status().Connected {
		t.Error("connected after quit")
	}
}

func TestStaticAndUnknownRoutes(t *testing.T) {
	e := newEnv(t, envOptions{noConnect: true, opts: []server.Option{
		server.WithStatic(fstest.MapFS{"index.html": {Data: []byte("<!doctype html>hi")}}),
	}})
	resp, err := e.http.Client().Get(e.http.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Errorf("GET / = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, e.http.URL+"/api/nope", nil)
	resp, err = e.http.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("GET /api/nope = %d %q, want 404 JSON", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}
