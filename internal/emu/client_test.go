package emu_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/emu"
	"github.com/puhitaku/rtcv-ish/internal/emu/fake"
	"github.com/puhitaku/rtcv-ish/internal/emutest"
)

func startFake(t *testing.T, opts fake.Options) *fake.Server {
	t.Helper()
	s, err := fake.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func dial(t *testing.T, s *fake.Server, opts ...emu.Option) *emu.Client {
	t.Helper()
	c, err := emu.Dial(t.Context(), s.Addr(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func loaded(t *testing.T, opts fake.Options) (*fake.Server, *emu.Client) {
	t.Helper()
	s := startFake(t, opts)
	c := dial(t, s)
	if _, err := c.LoadRom(t.Context(), "/roms/hello_world.nds"); err != nil {
		t.Fatal(err)
	}
	return s, c
}

func TestDial(t *testing.T) {
	s := startFake(t, fake.Options{Manual: true})
	c := dial(t, s)
	info := c.Info()
	if info.GetEmulator() != "fake" || info.GetProtocolVersion() != 1 || !info.GetCapabilities().GetSavestates() || info.GetCapabilities().GetMaxPayload() == 0 {
		t.Errorf("Info() = %v", info)
	}
	if err := c.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestDialRefused(t *testing.T) {
	s := startFake(t, fake.Options{Manual: true})
	addr := s.Addr()
	s.Close()
	if _, err := emu.Dial(t.Context(), addr); err == nil {
		t.Fatal("Dial to a closed server succeeded")
	}
}

func TestSecondClientRefused(t *testing.T) {
	s := startFake(t, fake.Options{Manual: true})
	dial(t, s)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if c, err := emu.Dial(ctx, s.Addr()); err == nil {
		c.Close()
		t.Fatal("second client was accepted")
	}
}

func TestNoROM(t *testing.T) {
	s := startFake(t, fake.Options{Manual: true, ROMExists: func(p string) bool { return p != "/missing.nds" }})
	c := dial(t, s)
	ctx := t.Context()

	st, err := c.Status(ctx)
	if err != nil || st.GetState() != emulatorv1.Status_NO_ROM {
		t.Fatalf("Status = %v, %v", st, err)
	}
	ds, err := c.ListDomains(ctx)
	if err != nil || len(ds) != 0 {
		t.Errorf("ListDomains = %v, %v; want empty", ds, err)
	}
	_, err = c.ReadOne(ctx, "MainRAM", 0, 4)
	emutest.WantCode(t, err, emulatorv1.Error_NO_ROM, "Read")
	emutest.WantCode(t, c.WriteOne(ctx, "MainRAM", 0, []byte{1}), emulatorv1.Error_NO_ROM, "Write")
	_, err = c.SaveState(ctx)
	emutest.WantCode(t, err, emulatorv1.Error_NO_ROM, "SaveState")
	emutest.WantCode(t, c.LoadState(ctx, []byte{1}), emulatorv1.Error_NO_ROM, "LoadState")
	emutest.WantCode(t, c.Reset(ctx), emulatorv1.Error_NO_ROM, "Reset")
	emutest.WantCode(t, c.Pause(ctx), emulatorv1.Error_NO_ROM, "Pause")
	emutest.WantCode(t, c.Resume(ctx), emulatorv1.Error_NO_ROM, "Resume")
	_, err = c.Step(ctx, 1)
	emutest.WantCode(t, err, emulatorv1.Error_NO_ROM, "Step")
	_, err = c.Screenshot(ctx)
	emutest.WantCode(t, err, emulatorv1.Error_NO_ROM, "Screenshot")
	if err := c.CloseRom(ctx); err != nil {
		t.Errorf("CloseRom without ROM: %v", err)
	}
	_, err = c.LoadRom(ctx, "/missing.nds")
	emutest.WantCode(t, err, emulatorv1.Error_NOT_FOUND, "LoadRom(missing)")
	_, err = c.LoadRom(ctx, "")
	emutest.WantCode(t, err, emulatorv1.Error_NOT_FOUND, "LoadRom(\"\")")
}

func TestLoadRomAndClose(t *testing.T) {
	s := startFake(t, fake.Options{Manual: true})
	c := dial(t, s)
	ctx := t.Context()
	if err := c.Subscribe(ctx, 1); err != nil {
		t.Fatal(err)
	}
	st, err := c.LoadRom(ctx, "/roms/hello_world.nds")
	if err != nil {
		t.Fatal(err)
	}
	if st.GetState() != emulatorv1.Status_RUNNING || st.GetGameTitle() != "HELLO_WORLD" || st.GetGameCode() == "" ||
		st.GetConsole() != "DS" || st.GetRomPath() != "/roms/hello_world.nds" || st.GetFrame() != 0 {
		t.Errorf("LoadRom status = %v", st)
	}
	if s.ROMPath() != "/roms/hello_world.nds" {
		t.Errorf("fake ROM path = %q", s.ROMPath())
	}
	ev := emutest.NextEvent(t, c, time.Second, "StatusEvent", func(ev *emulatorv1.Event) bool { return ev.GetStatus() != nil })
	if ev.GetStatus().GetStatus().GetState() != emulatorv1.Status_RUNNING {
		t.Errorf("status event = %v", ev)
	}
	ds, err := c.ListDomains(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range ds {
		names = append(names, fmt.Sprintf("%s/%d/%d/%v/%v", d.GetName(), d.GetSize(), d.GetWordSize(), d.GetWritable(), d.GetHidden()))
	}
	if got, want := fmt.Sprint(names), "[MainRAM/65536/4/true/false VRAM/16384/2/true/false ARM7WRAM/8192/4/true/true]"; got != want {
		t.Errorf("domains = %s, want %s", got, want)
	}

	if err := c.CloseRom(ctx); err != nil {
		t.Fatal(err)
	}
	emutest.NextEvent(t, c, time.Second, "StatusEvent NO_ROM", func(ev *emulatorv1.Event) bool {
		return ev.GetStatus().GetStatus().GetState() == emulatorv1.Status_NO_ROM && ev.GetStatus() != nil
	})
	if ds, _ := c.ListDomains(ctx); len(ds) != 0 {
		t.Errorf("domains after CloseRom = %v", ds)
	}
}

func TestErrorMapping(t *testing.T) {
	_, c := loaded(t, fake.Options{Manual: true})
	_, err := c.ReadOne(t.Context(), "MainRAM", 1<<20, 4)
	var e *emu.Error
	if !errors.As(err, &e) || e.Code != emulatorv1.Error_OUT_OF_RANGE || e.Message == "" {
		t.Fatalf("err = %#v", err)
	}
	if !errors.Is(err, emu.ErrOutOfRange) || errors.Is(err, emu.ErrNotFound) {
		t.Errorf("errors.Is mismatch for %v", err)
	}
	if code, ok := emu.CodeOf(fmt.Errorf("wrapped: %w", err)); !ok || code != emulatorv1.Error_OUT_OF_RANGE {
		t.Errorf("CodeOf = %v, %v", code, ok)
	}
	if _, ok := emu.CodeOf(errors.New("x")); ok {
		t.Error("CodeOf matched a plain error")
	}
}

func TestReadOnlyDomain(t *testing.T) {
	_, c := loaded(t, fake.Options{Manual: true, Domains: []fake.Domain{{Name: "ROM", Size: 16, WordSize: 4, ReadOnly: true}}})
	emutest.WantCode(t, c.WriteOne(t.Context(), "ROM", 0, []byte{1}), emulatorv1.Error_INVALID_ARGUMENT, "write to read-only domain")
}

func TestConformance(t *testing.T) {
	_, c := loaded(t, fake.Options{FrameRate: 1000})
	main := emutest.Scratch{Domain: "MainRAM", Base: 0x8000}
	t.Run("memory", func(t *testing.T) {
		emutest.RunMemory(t, c, main, emutest.Scratch{Domain: "VRAM", Base: 0x100}, emutest.Scratch{Domain: "ARM7WRAM", Base: 0x100})
	})
	t.Run("savestate", func(t *testing.T) { emutest.RunSaveState(t, c, main) })
	t.Run("control", func(t *testing.T) { emutest.RunControl(t, c, main) })
	t.Run("units", func(t *testing.T) { emutest.RunUnits(t, c, main) })
}

func TestManualTick(t *testing.T) {
	s, c := loaded(t, fake.Options{Manual: true})
	ctx := t.Context()
	s.Tick(5)
	st, _ := c.Status(ctx)
	if st.GetFrame() != 5 {
		t.Errorf("frame = %d, want 5", st.GetFrame())
	}
	got, _ := c.ReadOne(ctx, "MainRAM", 0, 4)
	if !bytes.Equal(got, []byte{5, 0, 0, 0}) {
		t.Errorf("fake game counter = % x", got)
	}
	if err := c.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	s.Tick(5)
	if f := s.Frame(); f != 5 {
		t.Errorf("frame advanced while paused: %d", f)
	}
}

func TestUnitsBigEndian(t *testing.T) {
	_, c := loaded(t, fake.Options{Manual: true, Domains: []fake.Domain{{Name: "RAM", Size: 64, WordSize: 2, BigEndian: true}}})
	ctx := t.Context()
	if err := c.WriteOne(ctx, "RAM", 8, []byte{0x00, 0xff}); err != nil {
		t.Fatal(err)
	}
	err := c.ApplyUnits(ctx, []*emulatorv1.Unit{{
		Id: 1, Domain: "RAM", Address: 16, Size: 2, Tilt: 1, Lifetime: 1,
		Source: &emulatorv1.Unit_Store{Store: &emulatorv1.StoreSource{Domain: "RAM", Address: 8}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Step(ctx, 1); err != nil {
		t.Fatal(err)
	}
	got, _ := c.ReadOne(ctx, "RAM", 16, 2)
	if !bytes.Equal(got, []byte{0x01, 0x00}) {
		t.Errorf("big-endian tilt = % x, want 01 00", got)
	}
}

func TestLoadStateKeepsUnits(t *testing.T) {
	_, c := loaded(t, fake.Options{Manual: true})
	ctx := t.Context()
	state, err := c.SaveState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyUnits(ctx, []*emulatorv1.Unit{{Id: 1, Domain: "MainRAM", Address: 0x100, Size: 1, Source: &emulatorv1.Unit_Value{Value: []byte{9}}}}); err != nil {
		t.Fatal(err)
	}
	if err := c.LoadState(ctx, state); err != nil {
		t.Fatal(err)
	}
	if units, _ := c.ListUnits(ctx); len(units) != 1 {
		t.Errorf("units after LoadState = %v", units)
	}
}

func TestScreenshotAndInput(t *testing.T) {
	s, c := loaded(t, fake.Options{Manual: true})
	ctx := t.Context()
	imgs, err := c.Screenshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 2 {
		t.Fatalf("got %d screens", len(imgs))
	}
	for _, img := range imgs {
		if img.GetWidth() != 256 || img.GetHeight() != 192 || len(img.GetRgba()) != 256*192*4 {
			t.Errorf("screen %dx%d with %d bytes", img.GetWidth(), img.GetHeight(), len(img.GetRgba()))
		}
	}
	if bytes.Equal(imgs[0].GetRgba(), imgs[1].GetRgba()) {
		t.Error("both screens are identical")
	}

	if err := c.SetInput(ctx, &emulatorv1.SetInputRequest{Buttons: 1 << 3, Touch: true, TouchX: 10, TouchY: 20}); err != nil {
		t.Fatal(err)
	}
	if in := s.Input(); in.GetButtons() != 1<<3 || !in.GetTouch() || in.GetTouchX() != 10 || in.GetTouchY() != 20 {
		t.Errorf("input = %v", in)
	}
	if err := c.SetInput(ctx, &emulatorv1.SetInputRequest{Clear: true}); err != nil {
		t.Fatal(err)
	}
	if in := s.Input(); in != nil {
		t.Errorf("input after clear = %v", in)
	}
}

func TestConcurrentCalls(t *testing.T) {
	_, c := loaded(t, fake.Options{FrameRate: 1000})
	ctx := t.Context()
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := range 32 {
		wg.Go(func() {
			addr := uint64(0x1000 + 16*i)
			want := []byte{byte(i), byte(i >> 8), 0xa5, byte(i)}
			for range 20 {
				if err := c.WriteOne(ctx, "MainRAM", addr, want); err != nil {
					errs <- err
					return
				}
				got, err := c.ReadOne(ctx, "MainRAM", addr, 4)
				if err != nil {
					errs <- err
					return
				}
				if !bytes.Equal(got, want) {
					errs <- fmt.Errorf("goroutine %d read % x, want % x", i, got, want)
					return
				}
				if err := c.Ping(ctx); err != nil {
					errs <- err
					return
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// gate blocks Ping requests on the fake until released.
type gate struct {
	entered chan struct{}
	release chan struct{}
}

func newGate() *gate { return &gate{entered: make(chan struct{}, 16), release: make(chan struct{})} }

func (g *gate) hook(req *emulatorv1.Request) {
	if req.GetPing() != nil {
		g.entered <- struct{}{}
		<-g.release
	}
}

func TestContextCancel(t *testing.T) {
	g := newGate()
	s := startFake(t, fake.Options{Manual: true, Hook: g.hook})
	c := dial(t, s)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := c.Ping(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Ping = %v, want DeadlineExceeded", err)
	}
	<-g.entered
	close(g.release)

	st, err := c.Status(t.Context())
	if err != nil || st.GetState() != emulatorv1.Status_NO_ROM {
		t.Fatalf("Status after a cancelled call = %v, %v", st, err)
	}

	cancelled, cancel2 := context.WithCancel(t.Context())
	cancel2()
	if err := c.Ping(cancelled); !errors.Is(err, context.Canceled) {
		t.Errorf("Ping with cancelled ctx = %v", err)
	}
}

func TestEventsDropFrames(t *testing.T) {
	s := startFake(t, fake.Options{Manual: true})
	c := dial(t, s, emu.WithEventBuffer(4))
	ctx := t.Context()
	if err := c.Subscribe(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := c.LoadRom(ctx, "a.nds"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Step(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if err := c.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	var states []emulatorv1.Status_State
	var frames []uint64
	for len(states) < 3 {
		ev := emutest.NextEvent(t, c, time.Second, "event", func(*emulatorv1.Event) bool { return true })
		if st := ev.GetStatus(); st != nil {
			states = append(states, st.GetStatus().GetState())
		} else {
			frames = append(frames, ev.GetFrame().GetFrame())
		}
	}
	want := []emulatorv1.Status_State{emulatorv1.Status_RUNNING, emulatorv1.Status_PAUSED, emulatorv1.Status_RUNNING}
	if fmt.Sprint(states) != fmt.Sprint(want) {
		t.Errorf("status events = %v, want %v", states, want)
	}
	if len(frames) == 0 || len(frames) > 4 || frames[len(frames)-1] != 100 {
		t.Errorf("frame events = %v, want at most 4 ending with 100", frames)
	}
}

func TestConnectionLoss(t *testing.T) {
	g := newGate()
	s := startFake(t, fake.Options{Manual: true, Hook: g.hook})
	c := dial(t, s)

	errc := make(chan error, 1)
	go func() { errc <- c.Ping(t.Context()) }()
	<-g.entered
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()

	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done not closed after connection loss")
	}
	if err := <-errc; !errors.Is(err, emu.ErrClosed) {
		t.Errorf("pending call = %v, want ErrClosed", err)
	}
	if _, ok := <-c.Events(); ok {
		t.Error("events channel not closed")
	}
	if err := c.Ping(t.Context()); !errors.Is(err, emu.ErrClosed) {
		t.Errorf("call after loss = %v", err)
	}
	close(g.release)
	<-closed
}

func TestClose(t *testing.T) {
	s := startFake(t, fake.Options{Manual: true})
	c := dial(t, s)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Ping(t.Context()); !errors.Is(err, emu.ErrClosed) {
		t.Errorf("Ping after Close = %v", err)
	}
	if !errors.Is(c.Err(), emu.ErrClosed) {
		t.Errorf("Err() = %v", c.Err())
	}
	// The fake accepts a new client once the old one is gone.
	deadline := time.Now().Add(5 * time.Second)
	for {
		c2, err := emu.Dial(t.Context(), s.Addr())
		if err == nil {
			c2.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("reconnect: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestQuit(t *testing.T) {
	s := startFake(t, fake.Options{Manual: true})
	c := dial(t, s)
	if err := c.Quit(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []<-chan struct{}{c.Done(), s.Done()} {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatal("not closed after Quit")
		}
	}
}
