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

func (s *Session) romLocked() (*emu.Client, error) {
	c, err := s.clientLocked()
	if err != nil {
		return nil, err
	}
	if s.game.State == StateNoRom {
		return nil, errorf(KindNoROM, "no ROM loaded")
	}
	return c, nil
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

func (s *Session) Connect(ctx context.Context, addr string) (Status, error) {
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		return Status{}, errorf(KindAlreadyConnected, "already connected to %s", s.conn.addr)
	}
	dctx, dcancel := context.WithTimeout(ctx, dialTimeout)
	c, err := emu.Dial(dctx, addr, emu.WithLogger(s.log))
	dcancel()
	if err != nil {
		return Status{}, &Error{Kind: KindConnectFailed, Msg: fmt.Sprintf("connect to %s: %v", addr, err), Err: err}
	}
	if err := s.setupConnLocked(ctx, c, addr, nil); err != nil {
		return Status{}, err
	}
	return s.Status(), nil
}

// setupConnLocked subscribes to frame events, reads the game status and
// starts the event loop.
func (s *Session) setupConnLocked(ctx context.Context, c *emu.Client, addr string, proc *process) error {
	cn := &conn{client: c, addr: addr, info: infoFromHello(c.Info()), proc: proc}
	if err := c.Subscribe(ctx, 1); err != nil {
		c.Close()
		return &Error{Kind: KindConnectFailed, Msg: fmt.Sprintf("subscribe to %s: %v", addr, err), Err: err}
	}
	s.conn = cn
	s.nextUnitID = 0
	s.game = GameStatus{State: StateNoRom}
	s.resetGameStateLocked()
	if err := s.refreshGameLocked(ctx, true); err != nil {
		s.dropConnLocked()
		s.publishStatusLocked()
		return &Error{Kind: KindConnectFailed, Msg: fmt.Sprintf("read status from %s: %v", addr, err), Err: err}
	}
	s.wg.Add(1)
	go s.eventLoop(cn)
	s.log.Info("connected to emulator", "addr", addr, "emulator", cn.info.Name, "version", cn.info.Version, "system", cn.info.System)
	s.notify("info", fmt.Sprintf("Connected to %s at %s", cn.info.Name, addr))
	s.publishStatusLocked()
	return nil
}

// dropConnLocked closes the connection and forgets the game.
func (s *Session) dropConnLocked() {
	if s.conn == nil {
		return
	}
	s.conn.client.Close()
	s.conn = nil
	s.game = GameStatus{State: StateNoRom}
	s.resetGameStateLocked()
	s.setDomainsLocked(nil)
}

// resetGameStateLocked forgets everything tied to the running game.
func (s *Session) resetGameStateLocked() {
	s.infinite = nil
	s.blLayer, s.blBackup, s.blOn = nil, nil, false
	s.autoCount = 0
	s.store.ClearBackups()
}

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
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		return Status{}, errorf(KindAlreadyConnected, "already connected to %s", s.conn.addr)
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
	c, err := dialRetry(ctx, addr, proc, s.log)
	if err != nil {
		s.kill(proc)
		return Status{}, &Error{Kind: KindConnectFailed, Msg: fmt.Sprintf("connect to %s at %s: %v", name, addr, err), Err: err}
	}
	if err := s.setupConnLocked(ctx, c, addr, proc); err != nil {
		s.kill(proc)
		return Status{}, err
	}
	if rom != "" {
		if _, err := s.loadRomLocked(ctx, rom); err != nil {
			return Status{}, err
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

func dialRetry(ctx context.Context, addr string, proc *process, log *slog.Logger) (*emu.Client, error) {
	ctx, cancel := context.WithTimeout(ctx, launchTimeout)
	defer cancel()
	for {
		dctx, dcancel := context.WithTimeout(ctx, dialTimeout)
		c, err := emu.Dial(dctx, addr, emu.WithLogger(log))
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

func (s *Session) startProcess(spec EmulatorSpec, port int) (*process, error) {
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
	go func() {
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

func (s *Session) kill(p *process) {
	p.cmd.Process.Kill()
	<-p.exited
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
	deadline := time.After(stopTimeout)
	for _, p := range procs {
		select {
		case <-p.exited:
			continue
		case <-deadline:
		}
		s.log.Warn("killing emulator", "emulator", p.name, "pid", p.cmd.Process.Pid)
		s.kill(p)
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

func (s *Session) eventLoop(cn *conn) {
	defer s.wg.Done()
	events := cn.client.Events()
	for {
		select {
		case <-s.ctx.Done():
			return
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != cn || s.game.State == StateNoRom {
		return
	}
	if f := int64(frame); f > s.game.Frame {
		s.game.Frame = f
		s.storeStatusLocked()
	}
	if now := time.Now(); now.Sub(s.lastFrameEv) >= frameEventInterval {
		s.lastFrameEv = now
		s.broker.publish(Event{Type: EventFrame, Data: FrameEvent{Frame: int64(frame)}})
	}
	if !s.settings.AutoCorrupt {
		return
	}
	if s.autoCount++; s.autoCount < s.settings.ErrorDelay {
		return
	}
	s.autoCount = 0
	ctx, cancel := context.WithTimeout(s.ctx, opTimeout)
	defer cancel()
	if _, err := s.blastLocked(ctx, true); err != nil && s.ctx.Err() == nil {
		s.log.Error("auto-corrupt failed; disabling it", "err", err)
		s.settings.AutoCorrupt = false
		s.changed(EventSettings)
		s.notify("error", fmt.Sprintf("Auto-corrupt disabled: %v", err))
	}
}

func (s *Session) onStatusEvent(cn *conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != cn {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, opTimeout)
	defer cancel()
	if err := s.refreshGameLocked(ctx, false); err != nil {
		s.log.Warn("refresh game status", "err", err)
		return
	}
	s.publishStatusLocked()
}

// refreshGameLocked reads the game status. When the game changed (or
// force is set) the domain list is re-read, the non-hidden domains are
// selected and state tied to the old game is dropped.
func (s *Session) refreshGameLocked(ctx context.Context, force bool) error {
	c, err := s.clientLocked()
	if err != nil {
		return err
	}
	st, err := c.Status(ctx)
	if err != nil {
		return err
	}
	old := s.game
	s.game = gameFromProto(st)
	if !force && old.RomPath == s.game.RomPath && (old.State == StateNoRom) == (s.game.State == StateNoRom) {
		return nil
	}
	if !force {
		s.resetGameStateLocked()
	}
	var ds []*emulatorv1.Domain
	if s.game.State != StateNoRom {
		if ds, err = c.ListDomains(ctx); err != nil {
			return err
		}
	}
	s.setDomainsLocked(ds)
	s.autoSelectLocked()
	if s.game.State != StateNoRom {
		s.log.Info("game loaded", "rom", s.game.RomPath, "title", s.game.Title, "code", s.game.Code, "domains", len(ds))
	}
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
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadRomLocked(ctx, path)
}

func (s *Session) loadRomLocked(ctx context.Context, path string) (GameStatus, error) {
	c, err := s.clientLocked()
	if err != nil {
		return GameStatus{}, err
	}
	if path == "" {
		return GameStatus{}, errorf(KindInvalid, "path is empty")
	}
	if _, err := c.LoadRom(ctx, path); err != nil {
		return GameStatus{}, err
	}
	if err := c.ClearUnits(ctx); err != nil {
		return GameStatus{}, err
	}
	s.resetGameStateLocked()
	err = s.refreshGameLocked(ctx, true)
	s.publishStatusLocked()
	return s.game, err
}

// control runs an emulator call and returns the refreshed game status.
func (s *Session) control(ctx context.Context, call func(context.Context, *emu.Client) error) (GameStatus, error) {
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.clientLocked()
	if err != nil {
		return GameStatus{}, err
	}
	if err := call(ctx, c); err != nil {
		return GameStatus{}, err
	}
	err = s.refreshGameLocked(ctx, false)
	s.publishStatusLocked()
	return s.game, err
}

func (s *Session) CloseRom(ctx context.Context) (GameStatus, error) {
	return s.control(ctx, func(ctx context.Context, c *emu.Client) error { return c.CloseRom(ctx) })
}

func (s *Session) Pause(ctx context.Context) (GameStatus, error) {
	return s.control(ctx, func(ctx context.Context, c *emu.Client) error { return c.Pause(ctx) })
}

func (s *Session) Resume(ctx context.Context) (GameStatus, error) {
	return s.control(ctx, func(ctx context.Context, c *emu.Client) error { return c.Resume(ctx) })
}

func (s *Session) Reset(ctx context.Context) (GameStatus, error) {
	return s.control(ctx, func(ctx context.Context, c *emu.Client) error {
		if err := c.Reset(ctx); err != nil {
			return err
		}
		s.infinite = nil
		s.autoCount = 0
		return nil
	})
}

func (s *Session) Step(ctx context.Context, frames int) (GameStatus, error) {
	if frames < 1 {
		return GameStatus{}, errorf(KindInvalid, "frames must be at least 1")
	}
	return s.control(ctx, func(ctx context.Context, c *emu.Client) error {
		_, err := c.Step(ctx, uint32(frames))
		return err
	})
}

// Quit asks the emulator to exit and disconnects.
func (s *Session) Quit(ctx context.Context) error {
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.clientLocked()
	if err != nil {
		return err
	}
	if err := c.Quit(ctx); err != nil && !errors.Is(err, emu.ErrClosed) {
		return err
	}
	s.log.Info("emulator quit", "addr", s.conn.addr)
	s.dropConnLocked()
	s.publishStatusLocked()
	return nil
}

// Screenshot returns all screens stacked vertically as PNG.
func (s *Session) Screenshot(ctx context.Context) ([]byte, error) {
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	s.mu.Lock()
	c, err := s.clientLocked()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	screens, err := c.Screenshot(ctx)
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
	s.mu.Lock()
	c, err := s.clientLocked()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return c.ReadOne(ctx, domain, addr, uint32(size))
}

func (s *Session) WriteMemory(ctx context.Context, domain string, addr uint64, data []byte) error {
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	s.mu.Lock()
	c, err := s.clientLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return c.WriteOne(ctx, domain, addr, data)
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
