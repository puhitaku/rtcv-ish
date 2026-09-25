package e2e

import (
	"context"
	"testing"
	"time"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/emu"
	"github.com/puhitaku/rtcv-ish/internal/emutest"
)

const helloWorld = "hello_world.nds"

// Scratch areas that hello_world.nds does not use.
var (
	mainScratch    = emutest.Scratch{Domain: "MainRAM", Base: 0x3f0000}
	unitScratch    = emutest.Scratch{Domain: "MainRAM", Base: 0x3f1000}
	vramScratch    = emutest.Scratch{Domain: "VRAM", Base: 0x208000}
	paletteScratch = emutest.Scratch{Domain: "Palette", Base: 0x700}
	oamScratch     = emutest.Scratch{Domain: "OAM", Base: 0x700}
)

func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func started(t *testing.T) (*Emulator, context.Context) {
	e := StartMelonDS(t, "")
	e.LoadROM(t, helloWorld)
	return e, testCtx(t)
}

func TestHello(t *testing.T) {
	e := StartMelonDS(t, "")
	info := e.Info()
	if info.GetEmulator() != "melonDS" {
		t.Errorf("emulator = %q, want melonDS", info.GetEmulator())
	}
	if info.GetProtocolVersion() != 1 || info.GetSystem() != "nds" || info.GetVersion() == "" {
		t.Errorf("hello = %v, want protocol 1, system nds and a version", info)
	}
	caps := info.GetCapabilities()
	if !caps.GetSavestates() || !caps.GetScreenshot() || !caps.GetInput() || !caps.GetLoadRom() || !caps.GetReset_() {
		t.Errorf("capabilities = %v, want all features", caps)
	}
	if caps.GetMaxPayload() < 16<<20 {
		t.Errorf("max_payload = %d, want at least 16 MiB for MainRAM and savestates", caps.GetMaxPayload())
	}

	ctx := testCtx(t)
	st, err := e.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.GetState() != emulatorv1.Status_NO_ROM {
		t.Errorf("state without ROM = %s, want NO_ROM", st.GetState())
	}
	ds, err := e.ListDomains(ctx)
	if err != nil || len(ds) != 0 {
		t.Errorf("ListDomains without ROM = %v, %v; want empty", ds, err)
	}
	if err := e.Ping(ctx); err != nil {
		t.Error(err)
	}
}

func TestLoadRom(t *testing.T) {
	e := StartMelonDS(t, "")
	ctx := testCtx(t)
	path := ROM(t, helloWorld)
	st, err := e.LoadRom(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	check := func(what string, st *emulatorv1.Status) {
		t.Helper()
		if st.GetState() != emulatorv1.Status_RUNNING || st.GetGameTitle() == "" || st.GetGameCode() == "" ||
			st.GetConsole() != "DS" || st.GetRomPath() != path {
			t.Errorf("%s = %v, want RUNNING, console DS, rom_path %s, game title and code", what, st, path)
		}
	}
	check("LoadRom status", st)
	st, err = e.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	check("Status", st)

	_, err = e.LoadRom(ctx, path+".missing")
	emutest.WantCode(t, err, emulatorv1.Error_NOT_FOUND, "LoadRom(missing)")
}

func TestBootFromCommandLine(t *testing.T) {
	MelonDSPath(t) // skip before requiring the ROM
	e := StartMelonDS(t, ROM(t, helloWorld))
	ctx := testCtx(t)
	deadline := time.Now().Add(15 * time.Second)
	for {
		st, err := e.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st.GetState() == emulatorv1.Status_RUNNING && st.GetRomPath() != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ROM given on the command line not running: %v", st)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestListDomains(t *testing.T) {
	e, ctx := started(t)
	ds, err := e.ListDomains(ctx)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		size   uint64
		word   uint32
		hidden bool
	}
	want := map[string]row{
		"MainRAM":    {4 << 20, 4, false},
		"VRAM":       {8 << 20, 4, false},
		"Palette":    {2 << 10, 2, false},
		"OAM":        {2 << 10, 2, false},
		"SharedWRAM": {32 << 10, 4, true},
		"ARM7WRAM":   {64 << 10, 4, true},
		"ITCM":       {32 << 10, 4, true},
		"DTCM":       {16 << 10, 4, true},
		"CartROM":    {0, 4, true},
	}
	seen := map[string]bool{}
	for _, d := range ds {
		seen[d.GetName()] = true
		w, ok := want[d.GetName()]
		if !ok {
			t.Errorf("unexpected domain %v", d)
			continue
		}
		if d.GetName() == "CartROM" {
			if d.GetSize() == 0 {
				t.Errorf("CartROM size is 0")
			}
			w.size = d.GetSize()
		}
		got := row{d.GetSize(), d.GetWordSize(), d.GetHidden()}
		if got != w || d.GetBigEndian() || !d.GetWritable() {
			t.Errorf("domain %s = %+v big_endian=%v writable=%v, want %+v little-endian writable",
				d.GetName(), got, d.GetBigEndian(), d.GetWritable(), w)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("domain %s missing", name)
		}
	}
	if t.Failed() {
		t.Logf("domains: %v", ds)
	}
}

func TestMemory(t *testing.T) {
	e, _ := started(t)
	emutest.RunMemory(t, e.Client, mainScratch, vramScratch, paletteScratch, oamScratch)
}

func TestControl(t *testing.T) {
	e, _ := started(t)
	emutest.RunControl(t, e.Client, unitScratch)
}

func TestSaveState(t *testing.T) {
	e, ctx := started(t)
	emutest.RunSaveState(t, e.Client, mainScratch)
	state, err := e.SaveState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if max := e.Info().GetCapabilities().GetMaxPayload(); uint32(len(state)) > max {
		t.Errorf("savestate is %d bytes, above max_payload %d", len(state), max)
	}
}

func TestUnits(t *testing.T) {
	e, _ := started(t)
	emutest.RunUnits(t, e.Client, unitScratch)
}

func TestScreenshotAndInput(t *testing.T) {
	e, ctx := started(t)
	if _, err := e.Step(ctx, 30); err != nil {
		t.Fatal(err)
	}
	screens, err := e.Screenshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(screens) != 2 {
		t.Fatalf("got %d screens, want 2", len(screens))
	}
	for i, s := range screens {
		if s.GetWidth() != 256 || s.GetHeight() != 192 || len(s.GetRgba()) != 256*192*4 {
			t.Errorf("screen %d: %dx%d with %d bytes, want 256x192 RGBA", i, s.GetWidth(), s.GetHeight(), len(s.GetRgba()))
		}
	}

	// Start makes hello_world leave its main loop, so this test comes last
	// on its emulator.
	const start = 1 << 3
	if err := e.SetInput(ctx, &emulatorv1.SetInputRequest{Buttons: start}); err != nil {
		t.Fatalf("SetInput(Start): %v", err)
	}
	if _, err := e.Step(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if err := e.SetInput(ctx, &emulatorv1.SetInputRequest{Clear: true}); err != nil {
		t.Fatalf("SetInput(clear): %v", err)
	}
	if err := e.SetInput(ctx, &emulatorv1.SetInputRequest{Touch: true, TouchX: 128, TouchY: 96}); err != nil {
		t.Fatalf("SetInput(touch): %v", err)
	}
	if err := e.SetInput(ctx, &emulatorv1.SetInputRequest{Clear: true}); err != nil {
		t.Fatalf("SetInput(clear): %v", err)
	}
}

func TestCloseRom(t *testing.T) {
	e, ctx := started(t)
	if err := e.CloseRom(ctx); err != nil {
		t.Fatal(err)
	}
	st, err := e.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.GetState() != emulatorv1.Status_NO_ROM {
		t.Errorf("state after CloseRom = %s, want NO_ROM", st.GetState())
	}
	ds, err := e.ListDomains(ctx)
	if err != nil || len(ds) != 0 {
		t.Errorf("ListDomains after CloseRom = %v, %v; want empty", ds, err)
	}
	_, err = e.ReadOne(ctx, "MainRAM", 0, 4)
	emutest.WantCode(t, err, emulatorv1.Error_NO_ROM, "Read after CloseRom")
	e.LoadROM(t, helloWorld)
}

func TestQuit(t *testing.T) {
	e, ctx := started(t)
	if err := e.Quit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-e.Done():
	case <-time.After(10 * time.Second):
		t.Error("connection still open 10s after Quit")
	}
	if err := e.WaitExit(10 * time.Second); err != nil {
		t.Errorf("melonDS still running 10s after Quit: %v", err)
	}
}

func TestSecondClientRefused(t *testing.T) {
	e := StartMelonDS(t, "")
	ctx, cancel := context.WithTimeout(testCtx(t), 5*time.Second)
	defer cancel()
	c, err := emu.Dial(ctx, e.Addr)
	if err == nil {
		c.Close()
		t.Fatal("second client was accepted")
	}
	if err := e.Ping(ctx); err != nil {
		t.Errorf("first client broken after a refused second client: %v", err)
	}
}
