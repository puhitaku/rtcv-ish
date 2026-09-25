package server_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/sasha-s/go-deadlock"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/emu/fake"
	"github.com/puhitaku/rtcv-ish/internal/server"
	"github.com/puhitaku/rtcv-ish/internal/server/gen"
	"github.com/puhitaku/rtcv-ish/internal/session"
)

// TestMain lowers go-deadlock's timeout far below the 30 s default so that
// a lock held across an emulator call, or a lock waiting on one, fails the
// run with a report instead of passing slowly or hanging.
func TestMain(m *testing.M) {
	deadlock.Opts.DeadlockTimeout = time.Second
	deadlock.Opts.OnPotentialDeadlock = func() {
		fmt.Fprintln(os.Stderr, "FAIL: go-deadlock reported a potential deadlock (see above)")
		os.Exit(2)
	}
	os.Exit(m.Run())
}

const hangROM = "/roms/hang.nds"

// hang makes the fake emulator stop answering at LoadRom of hangROM until
// released. The fake handles a connection's requests one by one, so
// everything after it waits too, as with a hung emulator thread.
type hang struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newHang() *hang {
	return &hang{entered: make(chan struct{}, 16), release: make(chan struct{})}
}

func (h *hang) hook(req *emulatorv1.Request) {
	if req.GetLoadRom().GetPath() == hangROM {
		h.entered <- struct{}{}
		<-h.release
	}
}

func (h *hang) unblock() { h.once.Do(func() { close(h.release) }) }

func (h *hang) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-h.entered:
	case <-time.After(eventTimeout):
		t.Fatal("LoadRom did not reach the fake emulator")
	}
}

// hangEnv is an env whose fake emulator hangs on hangROM.
func hangEnv(t *testing.T, tm session.Timeouts) (*env, *hang) {
	h := newHang()
	e := newEnv(t, envOptions{fake: fake.Options{Hook: h.hook}, timeouts: tm})
	// Registered after newEnv, so it runs before the fake is closed.
	t.Cleanup(h.unblock)
	return e, h
}

// within runs f and fails the test if it takes longer than d.
func within[T any](t *testing.T, d time.Duration, what string, f func() T) T {
	t.Helper()
	start := time.Now()
	v := f()
	if el := time.Since(start); el > d {
		t.Errorf("%s took %s, want at most %s", what, el, d)
	}
	return v
}

type result struct {
	r   response
	err error
}

// startLoadROM loads a ROM in the background.
func (e *env) startLoadROM(path string) <-chan result {
	ch := make(chan result, 1)
	go func() {
		r, err := e.c.LoadRomWithResponse(context.Background(), gen.PathRequest{Path: path})
		ch <- result{r, err}
	}()
	return ch
}

func waitResult(t *testing.T, ch <-chan result, d time.Duration) result {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(d):
		t.Fatalf("request did not finish within %s", d)
		return result{}
	}
}

func (e *env) waitStatus(what string, cond func(gen.Status) bool) gen.Status {
	e.t.Helper()
	deadline := time.Now().Add(eventTimeout)
	for {
		st := e.status()
		if cond(st) {
			return st
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for %s; status = %+v", what, st)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestHungEmulatorKeepsCoreResponsive(t *testing.T) {
	e, h := hangEnv(t, session.Timeouts{
		LongCall:     1500 * time.Millisecond,
		OpWait:       300 * time.Millisecond,
		PingInterval: 100 * time.Millisecond,
		Ping:         100 * time.Millisecond,
	})
	one := 1
	e.patchSettings(gen.SettingsPatch{GameProtection: &gen.GameProtectionSettingsPatch{Enabled: ptr(true), IntervalSeconds: &one}})

	load := e.startLoadROM(hangROM)
	h.waitEntered(t)

	// Read-only endpoints answer while the call hangs, across protection
	// loop ticks.
	for end := time.Now().Add(1100 * time.Millisecond); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		st := within(t, 200*time.Millisecond, "GET /status", e.status)
		if st.Busy == nil || st.Busy.Operation != "loadRom" {
			t.Fatalf("busy = %+v, want loadRom", st.Busy)
		}
		within(t, 200*time.Millisecond, "GET /domains", e.domains)
		within(t, 200*time.Millisecond, "GET /settings", e.settings)
	}

	// A second operation gives up after OpWait instead of queueing.
	rb := within(t, time.Second, "POST /blast while busy", func() result {
		r, err := e.c.ManualBlastWithResponse(e.ctx)
		return result{r, err}
	})
	expectError(t, rb.r, rb.err, http.StatusConflict, "BUSY")

	// The call times out and marks the emulator unresponsive.
	lr := waitResult(t, load, 5*time.Second)
	expectError(t, lr.r, lr.err, http.StatusGatewayTimeout, "EMULATOR_TIMEOUT")
	st := e.status()
	if !st.Connected || !st.Unresponsive || st.Busy != nil {
		t.Fatalf("status after timeout = %+v, want connected, unresponsive, not busy", st)
	}

	// Operations and emulator calls fail fast while unresponsive.
	rb = within(t, 200*time.Millisecond, "POST /blast while unresponsive", func() result {
		r, err := e.c.ManualBlastWithResponse(e.ctx)
		return result{r, err}
	})
	expectError(t, rb.r, rb.err, http.StatusServiceUnavailable, "EMULATOR_UNRESPONSIVE")
	rm := within(t, 200*time.Millisecond, "GET /memory while unresponsive", func() result {
		r, err := e.c.ReadMemoryWithResponse(e.ctx, vram, &gen.ReadMemoryParams{Address: 0, Size: 4})
		return result{r, err}
	})
	expectError(t, rm.r, rm.err, http.StatusServiceUnavailable, "EMULATOR_UNRESPONSIVE")

	// Once the emulator answers again the pinger clears the flag and the
	// protection loop, which kept ticking, takes backups again.
	h.unblock()
	st = e.waitStatus("recovery", func(st gen.Status) bool { return !st.Unresponsive })
	if !st.Connected {
		t.Fatalf("status after recovery = %+v", st)
	}
	e.waitStatus("a protection backup", func(st gen.Status) bool { return st.ProtectionBackups > 0 })
	e.blast()
}

func TestDisconnectDoesNotWaitForHungCall(t *testing.T) {
	e, h := hangEnv(t, session.Timeouts{LongCall: time.Second, PingInterval: 100 * time.Millisecond, Ping: 100 * time.Millisecond})

	// Disconnect while the call is pending: the call fails at once.
	load := e.startLoadROM(hangROM)
	h.waitEntered(t)
	rd := within(t, 200*time.Millisecond, "disconnect during a hung call", func() result {
		r, err := e.c.DisconnectEmulatorWithResponse(e.ctx)
		return result{r, err}
	})
	expectStatus(t, rd.r, rd.err, http.StatusOK)
	lr := waitResult(t, load, 200*time.Millisecond)
	expectError(t, lr.r, lr.err, http.StatusServiceUnavailable, "EMULATOR_DISCONNECTED")

	// Connecting again works (the fake takes one client, and the old one
	// is still stuck); so does disconnecting an unresponsive emulator.
	h2 := newHang()
	f2, err := fake.New(fake.Options{Manual: true, Hook: h2.hook})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f2.Close() })
	t.Cleanup(h2.unblock)
	within(t, time.Second, "reconnect", func() gen.Status { return e.connect(f2.Addr()) })
	load = e.startLoadROM(hangROM)
	h2.waitEntered(t)
	lr = waitResult(t, load, 5*time.Second)
	expectError(t, lr.r, lr.err, http.StatusGatewayTimeout, "EMULATOR_TIMEOUT")
	if !e.status().Unresponsive {
		t.Fatal("not unresponsive after the timeout")
	}
	rd = within(t, 200*time.Millisecond, "disconnect an unresponsive emulator", func() result {
		r, err := e.c.DisconnectEmulatorWithResponse(e.ctx)
		return result{r, err}
	})
	expectStatus(t, rd.r, rd.err, http.StatusOK)
	if st := e.status(); st.Connected || st.Unresponsive {
		t.Errorf("status after disconnect = %+v", st)
	}
}

// hungFakeEmu is a launchable fake emulator that hangs on hangROM.
func hungFakeEmu(t *testing.T) server.EmulatorSpec {
	return server.EmulatorSpec{Name: "fake", Path: buildFakeEmu(t),
		Args: []string{"-listen", "127.0.0.1:{port}", "-log-format", "json", "-hang-rom", hangROM}}
}

func waitRefused(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(eventTimeout)
	for {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return
		}
		c.Close()
		if time.Now().After(deadline) {
			t.Fatalf("emulator at %s still accepts connections", addr)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestQuitKillsHungLaunchedEmulator(t *testing.T) {
	e := newEnv(t, envOptions{noConnect: true, emulators: []server.EmulatorSpec{hungFakeEmu(t)},
		timeouts: session.Timeouts{LongCall: time.Second, QuitGrace: time.Second}})

	// Launch's ROM load times out; the emulator stays connected but
	// unresponsive.
	r, err := e.c.LaunchEmulatorWithResponse(e.ctx, gen.LaunchRequest{Name: "fake", Rom: ptr(hangROM)})
	expectError(t, r, err, http.StatusGatewayTimeout, "EMULATOR_TIMEOUT")
	st := e.status()
	if !st.Connected || !st.Unresponsive {
		t.Fatalf("status after the hung launch = %+v", st)
	}

	// Quit cannot ask it, so it kills it after the grace period.
	rq := within(t, 3*time.Second, "quit a hung emulator", func() result {
		r, err := e.c.QuitEmulatorWithResponse(e.ctx)
		return result{r, err}
	})
	expectStatus(t, rq.r, rq.err, http.StatusNoContent)
	if e.status().Connected {
		t.Error("connected after quit")
	}
	waitRefused(t, st.Address)
}

func TestCloseWithHungEmulator(t *testing.T) {
	e := newEnv(t, envOptions{noConnect: true, emulators: []server.EmulatorSpec{hungFakeEmu(t)},
		timeouts: session.Timeouts{QuitGrace: time.Second}})
	r, err := e.c.LaunchEmulatorWithResponse(e.ctx, gen.LaunchRequest{Name: "fake"})
	expectStatus(t, r, err, http.StatusOK)
	addr := r.JSON200.Address

	// A call that would hang for the full 30 s LoadRom timeout.
	load := e.startLoadROM(hangROM)
	e.waitStatus("the load to start", func(st gen.Status) bool { return st.Busy != nil })
	within(t, 5*time.Second, "Close", e.srv.Close)
	waitResult(t, load, time.Second)
	waitRefused(t, addr)
}
