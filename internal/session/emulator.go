package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/corrupt"
	"github.com/puhitaku/rtcv-ish/internal/emu"
)

const (
	dialTimeout   = 5 * time.Second
	launchTimeout = 20 * time.Second
	stopTimeout   = 3 * time.Second
	// frameEventInterval limits frame events to 10 per second.
	frameEventInterval = 100 * time.Millisecond
)

type conn struct {
	client *emu.Client
	addr   string
	info   EmulatorInfo
	proc   *process
	// Guarded by Session.mu.
	unresponsive   bool
	refreshPending bool
	// modeFallbackLogged is set once the freeze mode fallback was logged.
	modeFallbackLogged bool
}

type process struct {
	name   string
	cmd    *exec.Cmd
	exited chan struct{}
}

func (s *Session) clientLocked() (*emu.Client, error) {
	if s.conn == nil {
		return nil, errDisconnected
	}
	return s.conn.client, nil
}

// ---- Bundled emulators ----

type BundledEmulator struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Present bool   `json:"present"`
}

func (s *Session) Emulators() []BundledEmulator {
	out := make([]BundledEmulator, 0, len(s.cfg.Emulators))
	for _, e := range s.cfg.Emulators {
		out = append(out, BundledEmulator{Name: e.Name, Path: e.Path, Present: fileExists(e.Path)})
	}
	return out
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// ---- Connection ----

func (s *Session) dial(ctx context.Context, addr string) (*emu.Client, error) {
	return emu.Dial(ctx, addr, emu.WithLogger(s.log),
		emu.WithCallTimeouts(s.tm.Call, s.tm.LongCall),
		emu.WithTimeoutHandler(s.onCallTimeout))
}

func (s *Session) checkDisconnected() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		return errorf(KindAlreadyConnected, "already connected to %s", s.conn.addr)
	}
	return nil
}

func (s *Session) Connect(ctx context.Context, addr string) (Status, error) {
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	release, err := s.beginOp(ctx, "connect", false)
	if err != nil {
		return Status{}, err
	}
	defer release()
	if err := s.checkDisconnected(); err != nil {
		return Status{}, err
	}
	dctx, dcancel := context.WithTimeout(ctx, dialTimeout)
	c, err := s.dial(dctx, addr)
	dcancel()
	if err != nil {
		return Status{}, &Error{Kind: KindConnectFailed, Msg: fmt.Sprintf("connect to %s: %v", addr, err), Err: err}
	}
	if err := s.setupConn(ctx, c, addr, nil); err != nil {
		return Status{}, err
	}
	return s.Status(), nil
}

// setupConn subscribes to frame events, reads the game status, installs
// the connection and starts its event and ping loops. c is closed on
// failure.
func (s *Session) setupConn(ctx context.Context, c *emu.Client, addr string, proc *process) error {
	fail := func(format string, err error) error {
		c.Close()
		return &Error{Kind: KindConnectFailed, Msg: fmt.Sprintf(format, addr, err), Err: err}
	}
	if err := s.ctx.Err(); err != nil {
		c.Close()
		return &Error{Kind: KindConnectFailed, Msg: "session is closed", Err: err}
	}
	if err := c.Subscribe(ctx, 1); err != nil {
		return fail("subscribe to %s: %v", err)
	}
	st, err := c.Status(ctx)
	if err != nil {
		return fail("read status from %s: %v", err)
	}
	game := gameFromProto(st)
	var ds []*emulatorv1.Domain
	if game.State != StateNoRom {
		if ds, err = c.ListDomains(ctx); err != nil {
			return fail("read status from %s: %v", err)
		}
	}
	cn := &conn{client: c, addr: addr, info: infoFromHello(c.Info()), proc: proc}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ctx.Err(); err != nil {
		c.Close()
		return &Error{Kind: KindConnectFailed, Msg: "session is closed", Err: err}
	}
	if s.conn != nil {
		c.Close()
		return errorf(KindAlreadyConnected, "already connected to %s", s.conn.addr)
	}
	s.conn = cn
	s.nextUnitID = 0
	s.resetGameStateLocked(UnitsConnect)
	s.reloadGameLocked(game, ds)
	s.wg.Add(2)
	go s.eventLoop(cn)
	go s.pingLoop(cn)
	s.log.Info("connected to emulator", "addr", addr, "emulator", cn.info.Name, "version", cn.info.Version, "system", cn.info.System)
	s.notify("info", fmt.Sprintf("Connected to %s at %s", cn.info.Name, addr))
	s.publishStatusLocked()
	return nil
}

// dropConnLocked closes the connection, which fails its pending calls,
// and forgets the game.
func (s *Session) dropConnLocked() {
	if s.conn == nil {
		return
	}
	s.conn.client.Close()
	s.conn = nil
	s.game = GameStatus{State: StateNoRom}
	s.resetGameStateLocked(UnitsDisconnect)
	s.setDomainsLocked(nil)
}

// resetGameStateLocked forgets everything tied to the running game. The
// emulator has no units left (or is gone); reason goes into the units
// event.
func (s *Session) resetGameStateLocked(reason string) {
	s.infinite = nil
	s.unitsChanged(reason, 0)
	s.blLayer, s.blBackup, s.blOn = nil, nil, false
	s.store.ClearBackups()
}

// Disconnect drops the connection without waiting for running operations;
// their emulator calls fail.
func (s *Session) Disconnect() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		s.log.Info("disconnecting from emulator", "addr", s.conn.addr)
		s.dropConnLocked()
		s.publishStatusLocked()
	}
	return s.Status()
}

func (s *Session) Launch(ctx context.Context, name, rom string) (Status, error) {
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	release, err := s.beginOp(ctx, "launch", false)
	if err != nil {
		return Status{}, err
	}
	defer release()
	if err := s.checkDisconnected(); err != nil {
		return Status{}, err
	}
	i := slices.IndexFunc(s.cfg.Emulators, func(e EmulatorSpec) bool { return e.Name == name })
	if i < 0 {
		return Status{}, errorf(KindNotFound, "no bundled emulator %q", name)
	}
	spec := s.cfg.Emulators[i]
	if !fileExists(spec.Path) {
		return Status{}, errorf(KindNotFound, "emulator %q is not present at %s", name, spec.Path)
	}
	port, err := freePort()
	if err != nil {
		return Status{}, err
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	proc, err := s.startProcess(spec, port)
	if err != nil {
		return Status{}, &Error{Kind: KindConnectFailed, Msg: fmt.Sprintf("start %s: %v", name, err), Err: err}
	}
	c, err := s.dialRetry(ctx, addr, proc)
	if err != nil {
		s.kill(proc)
		return Status{}, &Error{Kind: KindConnectFailed, Msg: fmt.Sprintf("connect to %s at %s: %v", name, addr, err), Err: err}
	}
	if err := s.setupConn(ctx, c, addr, proc); err != nil {
		s.kill(proc)
		return Status{}, err
	}
	if rom != "" {
		if _, err := s.loadRom(ctx, rom); err != nil {
			return Status{}, classify(err)
		}
	}
	return s.Status(), nil
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func (s *Session) dialRetry(ctx context.Context, addr string, proc *process) (*emu.Client, error) {
	ctx, cancel := context.WithTimeout(ctx, launchTimeout)
	defer cancel()
	for {
		dctx, dcancel := context.WithTimeout(ctx, dialTimeout)
		c, err := s.dial(dctx, addr)
		dcancel()
		if err == nil {
			return c, nil
		}
		select {
		case <-proc.exited:
			return nil, fmt.Errorf("emulator exited: %s", proc.cmd.ProcessState)
		case <-ctx.Done():
			return nil, fmt.Errorf("%w (last error: %v)", ctx.Err(), err)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// startProcess holds s.mu so that Close, which cancels s.ctx before taking
// s.mu, never misses the process or its goroutine.
func (s *Session) startProcess(spec EmulatorSpec, port int) (*process, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	args := make([]string, len(spec.Args))
	for i, a := range spec.Args {
		args[i] = strings.ReplaceAll(a, "{port}", strconv.Itoa(port))
	}
	cmd := exec.Command(spec.Path, args...)
	log := s.log.With("emulator", spec.Name)
	out := &logWriter{log: log}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	log.Info("launched emulator", "path", spec.Path, "args", args, "pid", cmd.Process.Pid)
	p := &process{name: spec.Name, cmd: cmd, exited: make(chan struct{})}
	s.procMu.Lock()
	s.procs[p] = struct{}{}
	s.procMu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		err := cmd.Wait()
		out.flush()
		log.Info("emulator exited", "pid", cmd.Process.Pid, "status", cmd.ProcessState.String(), "err", err)
		close(p.exited)
		s.procMu.Lock()
		delete(s.procs, p)
		s.procMu.Unlock()
	}()
	return p, nil
}

// kill kills the process without waiting for it to exit. Its wait
// goroutine reaps it and Close waits for that.
func (s *Session) kill(p *process) {
	p.cmd.Process.Kill()
}

// stop asks a launched emulator to exit (SIGTERM, when term is set) and
// kills it if it has not exited by deadline.
func (s *Session) stop(p *process, deadline time.Time, term bool) {
	if term {
		p.cmd.Process.Signal(syscall.SIGTERM)
	}
	t := time.NewTimer(time.Until(deadline))
	defer t.Stop()
	select {
	case <-p.exited:
		return
	case <-t.C:
	}
	s.log.Warn("killing emulator", "emulator", p.name, "pid", p.cmd.Process.Pid)
	s.kill(p)
	select {
	case <-p.exited:
	case <-time.After(time.Second):
	}
}

// stopProcesses waits briefly for launched emulators to exit and kills
// the rest.
func (s *Session) stopProcesses() {
	s.procMu.Lock()
	procs := make([]*process, 0, len(s.procs))
	for p := range s.procs {
		procs = append(procs, p)
	}
	s.procMu.Unlock()
	deadline := time.Now().Add(s.tm.QuitGrace)
	for _, p := range procs {
		s.stop(p, deadline, false)
	}
}

// logWriter logs the output of a launched emulator line by line.
type logWriter struct {
	log *slog.Logger
	buf []byte
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.emit(w.buf[:i])
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > 64<<10 {
		w.flush()
	}
	return len(p), nil
}

func (w *logWriter) flush() {
	if len(w.buf) > 0 {
		w.emit(w.buf)
		w.buf = nil
	}
}

func (w *logWriter) emit(line []byte) {
	if l := strings.TrimRight(string(line), "\r"); l != "" {
		w.log.Debug("emulator output", "line", l)
	}
}

// ---- Event loop ----

// refreshRetryInterval is how often a status refresh skipped because an
// operation was running is retried.
const refreshRetryInterval = 200 * time.Millisecond

// eventLoop handles the connection's events. It never waits for the
// operation gate: work that needs it is skipped or retried later.
func (s *Session) eventLoop(cn *conn) {
	defer s.wg.Done()
	events := cn.client.Events()
	retry := time.NewTicker(refreshRetryInterval)
	defer retry.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-retry.C:
			s.mu.Lock()
			pending := s.conn == cn && cn.refreshPending
			s.mu.Unlock()
			if pending {
				s.onStatusEvent(cn)
			}
		case ev, ok := <-events:
			if !ok {
				s.onDisconnected(cn)
				return
			}
			switch {
			case ev.GetFrame() != nil:
				s.onFrame(cn, ev.GetFrame().GetFrame())
			case ev.GetStatus() != nil:
				s.onStatusEvent(cn)
			}
		}
	}
}

func (s *Session) onDisconnected(cn *conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != cn {
		return
	}
	err := cn.client.Err()
	s.log.Warn("emulator disconnected", "addr", cn.addr, "err", err)
	s.notify("warn", fmt.Sprintf("Emulator at %s disconnected", cn.addr))
	s.dropConnLocked()
	s.publishStatusLocked()
}

func (s *Session) onFrame(cn *conn, frame uint64) {
	f := int64(frame)
	s.mu.Lock()
	if s.conn != cn || s.game.State == StateNoRom {
		s.mu.Unlock()
		return
	}
	if f > s.game.Frame {
		s.game.Frame = f
		s.storeStatusLocked()
	}
	if now := time.Now(); now.Sub(s.lastFrameEv) >= frameEventInterval {
		s.lastFrameEv = now
		s.broker.publish(Event{Type: EventFrame, Data: FrameEvent{Frame: f}})
	}
	// Frame events can be dropped, so count by the frame number. A frame
	// before the marker means the counter went back (reset, savestate).
	due := false
	if s.settings.AutoCorrupt && !cn.unresponsive {
		if f < s.lastAutoFrame {
			s.lastAutoFrame = f
		} else {
			due = f-s.lastAutoFrame >= int64(s.settings.ErrorDelay)
		}
	}
	s.mu.Unlock()
	if !due {
		return
	}
	// Skip this frame while an operation runs; the marker stays, so the
	// next frame event tries again.
	if !s.gate.tryAcquire("autoCorrupt") {
		s.log.Debug("auto-corrupt skipped: an operation is running")
		return
	}
	defer s.gate.release()
	s.mu.Lock()
	due = s.conn == cn && s.settings.AutoCorrupt && f >= s.lastAutoFrame
	if due {
		s.lastAutoFrame = f
	}
	s.mu.Unlock()
	if !due {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, opTimeout)
	defer cancel()
	if _, err := s.blast(ctx, true); err != nil && s.ctx.Err() == nil {
		s.mu.Lock()
		if s.settings.AutoCorrupt {
			s.log.Error("auto-corrupt failed; disabling it", "err", err)
			s.settings.AutoCorrupt = false
			s.changed(EventSettings)
			s.notify("error", fmt.Sprintf("Auto-corrupt disabled: %v", err))
		}
		s.mu.Unlock()
	}
}

// onStatusEvent re-reads the game status. While an operation runs or the
// emulator is unresponsive the refresh is marked pending and retried by
// the event loop.
func (s *Session) onStatusEvent(cn *conn) {
	if !s.gate.tryAcquire("refreshStatus") {
		s.markRefreshPending(cn)
		s.log.Debug("status refresh deferred: an operation is running")
		return
	}
	defer s.gate.release()
	s.mu.Lock()
	if s.conn != cn {
		s.mu.Unlock()
		return
	}
	if cn.unresponsive {
		cn.refreshPending = true
		s.mu.Unlock()
		return
	}
	cn.refreshPending = false
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(s.ctx, opTimeout)
	defer cancel()
	reloaded, err := s.refreshGame(ctx, cn, false)
	if err != nil {
		s.log.Warn("refresh game status", "err", err)
		return
	}
	// A status event also follows a console reset or re-creation, which
	// can change domain sizes without changing the game.
	if !reloaded {
		if err := s.syncDomains(ctx, cn); err != nil {
			s.log.Warn("refresh domains", "err", err)
		}
	}
	s.mu.Lock()
	if s.conn == cn {
		s.publishStatusLocked()
	}
	s.mu.Unlock()
}

func (s *Session) markRefreshPending(cn *conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == cn {
		cn.refreshPending = true
	}
}

// gameChangedLocked reports whether g is another game than the current.
func (s *Session) gameChangedLocked(g GameStatus) bool {
	return s.game.RomPath != g.RomPath || (s.game.State == StateNoRom) != (g.State == StateNoRom)
}

// reloadGameLocked installs a game and its domain list and selects the
// non-hidden domains.
func (s *Session) reloadGameLocked(g GameStatus, ds []*emulatorv1.Domain) {
	s.game = g
	s.lastAutoFrame = g.Frame
	s.setDomainsLocked(ds)
	s.autoSelectLocked()
	if g.State != StateNoRom {
		s.log.Info("game loaded", "rom", g.RomPath, "title", g.Title, "code", g.Code, "domains", len(ds))
	}
}

// refreshGame reads the game status. When the game changed (or force is
// set) the domain list is re-read, the non-hidden domains are selected and
// state tied to the old game is dropped; reloaded reports that. The caller
// holds the gate.
func (s *Session) refreshGame(ctx context.Context, cn *conn, force bool) (reloaded bool, err error) {
	st, err := cn.client.Status(ctx)
	if err != nil {
		return false, err
	}
	g := gameFromProto(st)
	s.mu.Lock()
	if s.conn != cn {
		s.mu.Unlock()
		return false, errDisconnected
	}
	if !force && !s.gameChangedLocked(g) {
		s.game = g
		s.mu.Unlock()
		return false, nil
	}
	s.mu.Unlock()
	var ds []*emulatorv1.Domain
	if g.State != StateNoRom {
		ds, err = cn.client.ListDomains(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != cn {
		return true, errDisconnected
	}
	if !force {
		s.resetGameStateLocked(UnitsGame)
	}
	if err != nil {
		s.game = g
		s.lastAutoFrame = g.Frame
		return true, err
	}
	s.reloadGameLocked(g, ds)
	return true, nil
}

// syncDomains re-reads the domain list of the loaded game. Emulators
// resize domains when they re-create or reset the console (melonDS: DS vs
// DSi MainRAM), so the list is refreshed before every generation instead
// of only on a game change. The selection is kept by name.
func (s *Session) syncDomains(ctx context.Context, cn *conn) error {
	s.mu.Lock()
	if s.conn != cn {
		s.mu.Unlock()
		return errDisconnected
	}
	if s.game.State == StateNoRom {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	ds, err := cn.client.ListDomains(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != cn {
		return errDisconnected
	}
	if slices.EqualFunc(s.protoDomains, ds, func(a, b *emulatorv1.Domain) bool {
		return corrupt.DomainFromProto(a) == corrupt.DomainFromProto(b)
	}) {
		return nil
	}
	wasEmpty := len(s.domains) == 0
	s.setDomainsLocked(ds)
	if wasEmpty {
		s.autoSelectLocked()
	}
	s.log.Info("domain list changed", "domains", s.domains)
	return nil
}

func (s *Session) setDomainsLocked(ds []*emulatorv1.Domain) {
	s.protoDomains = ds
	s.domains = s.domains[:0:0]
	for _, d := range ds {
		s.domains = append(s.domains, corrupt.DomainFromProto(d))
	}
	s.selected = slices.DeleteFunc(s.selected, func(n string) bool {
		return !slices.ContainsFunc(s.domains, func(d corrupt.Domain) bool { return d.Name == n })
	})
	s.changed(EventDomains)
}

// ---- Game control ----

func (s *Session) LoadRom(ctx context.Context, path string) (GameStatus, error) {
	var g GameStatus
	err := s.withOp(ctx, "loadRom", func(ctx context.Context) (err error) {
		g, err = s.loadRom(ctx, path)
		return err
	})
	return g, err
}

func (s *Session) loadRom(ctx context.Context, path string) (GameStatus, error) {
	cn, err := s.current()
	if err != nil {
		return GameStatus{}, err
	}
	if path == "" {
		return GameStatus{}, errorf(KindInvalid, "path is empty")
	}
	if _, err := cn.client.LoadRom(ctx, path); err != nil {
		return GameStatus{}, err
	}
	if err := cn.client.ClearUnits(ctx); err != nil {
		return GameStatus{}, err
	}
	if err := s.commit(cn, func() { s.resetGameStateLocked(UnitsGame) }); err != nil {
		return GameStatus{}, err
	}
	_, err = s.refreshGame(ctx, cn, true)
	return s.publishGame(), err
}

// publishGame publishes the status and returns the game.
func (s *Session) publishGame() GameStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publishStatusLocked()
	return s.game
}

// control runs an emulator call as an operation and returns the refreshed
// game status.
func (s *Session) control(ctx context.Context, name string, call func(context.Context, *conn) error) (GameStatus, error) {
	var g GameStatus
	err := s.withOp(ctx, name, func(ctx context.Context) error {
		cn, err := s.current()
		if err != nil {
			return err
		}
		if err := call(ctx, cn); err != nil {
			return err
		}
		_, err = s.refreshGame(ctx, cn, false)
		g = s.publishGame()
		return err
	})
	return g, err
}

func (s *Session) CloseRom(ctx context.Context) (GameStatus, error) {
	return s.control(ctx, "closeRom", func(ctx context.Context, cn *conn) error { return cn.client.CloseRom(ctx) })
}

func (s *Session) Pause(ctx context.Context) (GameStatus, error) {
	return s.control(ctx, "pause", func(ctx context.Context, cn *conn) error { return cn.client.Pause(ctx) })
}

func (s *Session) Resume(ctx context.Context) (GameStatus, error) {
	return s.control(ctx, "resume", func(ctx context.Context, cn *conn) error { return cn.client.Resume(ctx) })
}

func (s *Session) Reset(ctx context.Context) (GameStatus, error) {
	return s.control(ctx, "reset", func(ctx context.Context, cn *conn) error {
		n, err := s.countUnits(ctx, cn)
		if err != nil {
			return err
		}
		if err := cn.client.Reset(ctx); err != nil {
			return err
		}
		return s.commit(cn, func() {
			s.infinite = nil
			// The frame counter restarts at 0.
			s.lastAutoFrame = 0
			s.unitsChanged(UnitsReset, n)
		})
	})
}

func (s *Session) Step(ctx context.Context, frames int) (GameStatus, error) {
	if frames < 1 {
		return GameStatus{}, errorf(KindInvalid, "frames must be at least 1")
	}
	return s.control(ctx, "step", func(ctx context.Context, cn *conn) error {
		_, err := cn.client.Step(ctx, uint32(frames))
		return err
	})
}

// Quit asks the emulator to exit and disconnects. It does not wait for
// running operations. A launched emulator that has not exited QuitGrace
// after the request is killed.
func (s *Session) Quit(ctx context.Context) error {
	s.mu.Lock()
	cn := s.conn
	if cn == nil {
		s.mu.Unlock()
		return errDisconnected
	}
	unresponsive := cn.unresponsive
	s.mu.Unlock()
	deadline := time.Now().Add(s.tm.QuitGrace)
	var err error
	if unresponsive {
		err = errUnresponsive(cn.addr)
	} else {
		qctx, cancel := context.WithDeadline(ctx, deadline)
		err = cn.client.Quit(qctx)
		cancel()
		if errors.Is(err, emu.ErrClosed) {
			err = nil
		}
	}
	if err != nil && cn.proc == nil {
		return err
	}
	s.log.Info("emulator quit", "addr", cn.addr, "err", err)
	s.mu.Lock()
	if s.conn == cn {
		s.dropConnLocked()
		s.publishStatusLocked()
	}
	s.mu.Unlock()
	if cn.proc != nil {
		s.stop(cn.proc, deadline, err != nil)
	}
	return nil
}

// Screenshot returns all screens stacked vertically as PNG.
func (s *Session) Screenshot(ctx context.Context) ([]byte, error) {
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	cn, err := s.currentResponsive()
	if err != nil {
		return nil, err
	}
	screens, err := cn.client.Screenshot(ctx)
	if err != nil {
		return nil, err
	}
	w, h := 0, 0
	for _, sc := range screens {
		if len(sc.GetRgba()) != 4*int(sc.GetWidth())*int(sc.GetHeight()) {
			return nil, fmt.Errorf("screenshot: %dx%d screen has %d bytes", sc.GetWidth(), sc.GetHeight(), len(sc.GetRgba()))
		}
		w = max(w, int(sc.GetWidth()))
		h += int(sc.GetHeight())
	}
	if w == 0 || h == 0 {
		return nil, errors.New("screenshot: the emulator returned no image")
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	y := 0
	for _, sc := range screens {
		sw, sh := int(sc.GetWidth()), int(sc.GetHeight())
		src := &image.RGBA{Pix: sc.GetRgba(), Stride: 4 * sw, Rect: image.Rect(0, 0, sw, sh)}
		draw.Draw(img, image.Rect(0, y, sw, y+sh), src, image.Point{}, draw.Src)
		y += sh
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---- Memory ----

func (s *Session) ReadMemory(ctx context.Context, domain string, addr uint64, size int) ([]byte, error) {
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	cn, err := s.currentResponsive()
	if err != nil {
		return nil, err
	}
	return cn.client.ReadOne(ctx, domain, addr, uint32(size))
}

// wordBatch is the largest single emulator read ReadWords issues.
const wordBatch = 1 << 20

// ReadWords reads size bytes at addr and returns every stride-th
// little-endian 16-bit word, in memory order. A trailing odd byte is
// ignored. The range is read in batches of at most wordBatch bytes.
func (s *Session) ReadWords(ctx context.Context, domain string, addr uint64, size, stride int) ([]byte, error) {
	cn, err := s.currentResponsive()
	if err != nil {
		return nil, err
	}
	c := cn.client
	s.mu.Lock()
	var dsize uint64
	found := false
	for _, d := range s.domains {
		if d.Name == domain {
			dsize, found = d.Size, true
		}
	}
	s.mu.Unlock()
	if !found {
		return nil, errorf(KindNotFound, "unknown domain %q", domain)
	}
	if addr > dsize || uint64(size) > dsize-addr {
		return nil, errorf(KindOutOfRange, "range %#x+%#x is outside %s (%#x bytes)", addr, size, domain, dsize)
	}
	step := 2 * stride
	batch := wordBatch
	if mp := int(c.Info().GetCapabilities().GetMaxPayload()); mp > 0 && mp < batch {
		batch = mp
	}
	batch = max(batch-batch%step, step)
	words := (size/2 + stride - 1) / stride
	out := make([]byte, 0, 2*words)
	for off := 0; len(out) < 2*words; off += batch {
		// Read only up to the end of the last sampled word in this batch.
		n := min(batch, size-off)
		n = min(n, (n-2)/step*step+2)
		b, err := s.readBatch(ctx, c, domain, addr+uint64(off), n)
		if err != nil {
			return nil, err
		}
		for i := 0; i+2 <= len(b); i += step {
			out = append(out, b[i], b[i+1])
		}
	}
	return out, nil
}

func (s *Session) readBatch(ctx context.Context, c *emu.Client, domain string, addr uint64, n int) ([]byte, error) {
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	return c.ReadOne(ctx, domain, addr, uint32(n))
}

func (s *Session) WriteMemory(ctx context.Context, domain string, addr uint64, data []byte) error {
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	cn, err := s.currentResponsive()
	if err != nil {
		return err
	}
	return cn.client.WriteOne(ctx, domain, addr, data)
}

// ---- Domains ----

type DomainInfo struct {
	corrupt.Domain
	Selected bool `json:"selected"`
}

func (s *Session) domainsLocked() []DomainInfo {
	out := make([]DomainInfo, 0, len(s.domains))
	for _, d := range s.domains {
		out = append(out, DomainInfo{Domain: d, Selected: slices.Contains(s.selected, d.Name)})
	}
	return out
}

func (s *Session) Domains() ([]DomainInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.clientLocked(); err != nil {
		return nil, err
	}
	return s.domainsLocked(), nil
}

// SelectDomains replaces the selection. The selection keeps domain order.
func (s *Session) SelectDomains(names []string) ([]DomainInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.clientLocked(); err != nil {
		return nil, err
	}
	for _, n := range names {
		if !slices.ContainsFunc(s.domains, func(d corrupt.Domain) bool { return d.Name == n }) {
			return nil, errorf(KindInvalid, "unknown domain %q", n)
		}
	}
	s.selected = nil
	for _, d := range s.domains {
		if slices.Contains(names, d.Name) {
			s.selected = append(s.selected, d.Name)
		}
	}
	s.changed(EventDomains)
	return s.domainsLocked(), nil
}

func (s *Session) AutoSelectDomains() ([]DomainInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.clientLocked(); err != nil {
		return nil, err
	}
	s.autoSelectLocked()
	s.changed(EventDomains)
	return s.domainsLocked(), nil
}

func (s *Session) autoSelectLocked() {
	s.selected = nil
	for _, d := range s.domains {
		if !d.Hidden {
			s.selected = append(s.selected, d.Name)
		}
	}
}
