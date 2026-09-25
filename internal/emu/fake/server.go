// Package fake is an in-process emulator that speaks the emulator API.
// It has no CPU: its "game" only stores the frame counter as a 32-bit
// word at offset 0 of the first domain after every frame.
package fake

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sasha-s/go-deadlock"
	"google.golang.org/protobuf/proto"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/emu/wire"
)

// MaxPayload is advertised in Capabilities.max_payload.
const MaxPayload = 32 << 20

const (
	ScreenWidth  = 256
	ScreenHeight = 192
)

type Domain struct {
	Name      string
	Size      uint64
	WordSize  uint32
	BigEndian bool
	ReadOnly  bool
	Hidden    bool
}

func DefaultDomains() []Domain {
	return []Domain{
		{Name: "MainRAM", Size: 64 << 10, WordSize: 4},
		{Name: "VRAM", Size: 16 << 10, WordSize: 2},
		{Name: "ARM7WRAM", Size: 8 << 10, WordSize: 4, Hidden: true},
	}
}

type Options struct {
	// Addr to listen on. Default 127.0.0.1:0.
	Addr    string
	Domains []Domain
	// FrameRate in Hz while running. Default 60.
	FrameRate float64
	// Manual disables the frame goroutine; frames advance only through
	// Step requests and Tick.
	Manual bool
	// ROM is loaded at start when set.
	ROM string
	// ROMExists decides whether LoadRom finds a path. Default: always.
	ROMExists func(path string) bool
	// Hook, when set, is called before each request is handled. Tests use it
	// to delay responses.
	Hook   func(*emulatorv1.Request)
	Logger *slog.Logger
}

type Server struct {
	opts Options
	ln   net.Listener
	log  *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	done   chan struct{}
	once   sync.Once

	mu      deadlock.Mutex
	conn    *conn
	state   emulatorv1.Status_State
	frame   uint64
	romPath string
	mem     map[string][]byte
	units   []*unit
	input   *emulatorv1.SetInputRequest
}

type conn struct {
	net.Conn
	writeMu  deadlock.Mutex
	hello    bool
	interval uint32
}

// New starts a fake emulator listening on opts.Addr.
func New(opts Options) (*Server, error) {
	if opts.Addr == "" {
		opts.Addr = "127.0.0.1:0"
	}
	if opts.Domains == nil {
		opts.Domains = DefaultDomains()
	}
	if opts.FrameRate <= 0 {
		opts.FrameRate = 60
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	ln, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		opts:   opts,
		ln:     ln,
		log:    opts.Logger,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	if opts.ROM != "" {
		s.mu.Lock()
		s.loadROM(opts.ROM)
		s.mu.Unlock()
	}
	s.wg.Add(1)
	go s.acceptLoop()
	if !opts.Manual {
		s.wg.Add(1)
		go s.frameLoop()
	}
	return s, nil
}

func (s *Server) Addr() string { return s.ln.Addr().String() }

// Done is closed when the server has been closed, e.g. by a Quit request.
func (s *Server) Done() <-chan struct{} { return s.done }

// Close stops the server and drops the active connection. It is idempotent.
func (s *Server) Close() error {
	s.once.Do(func() {
		s.cancel()
		s.ln.Close()
		s.mu.Lock()
		if s.conn != nil {
			s.conn.Close()
		}
		s.mu.Unlock()
		s.wg.Wait()
		close(s.done)
	})
	return nil
}

// Tick emulates n frames if the emulator is running.
func (s *Server) Tick(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for range n {
		if s.state != emulatorv1.Status_RUNNING {
			return
		}
		s.runFrame()
	}
}

func (s *Server) Frame() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frame
}

func (s *Server) State() emulatorv1.Status_State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *Server) ROMPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.romPath
}

// Input returns the current input override, or nil.
func (s *Server) Input() *emulatorv1.SetInputRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.input == nil {
		return nil
	}
	return proto.Clone(s.input).(*emulatorv1.SetInputRequest)
}

// Memory returns a copy of a domain's contents, or nil.
func (s *Server) Memory(domain string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.mem[domain]; ok {
		return append([]byte(nil), m...)
	}
	return nil
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				s.log.Error("accept", "err", err)
			}
			return
		}
		s.mu.Lock()
		if s.conn != nil {
			s.mu.Unlock()
			s.log.Warn("refusing second client", "remote", nc.RemoteAddr())
			nc.Close()
			continue
		}
		c := &conn{Conn: nc}
		s.conn = c
		s.mu.Unlock()
		s.wg.Add(1)
		go s.serve(c)
	}
}

func (s *Server) frameLoop() {
	defer s.wg.Done()
	t := time.NewTicker(time.Duration(float64(time.Second) / s.opts.FrameRate))
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			s.Tick(1)
		}
	}
}

func (s *Server) serve(c *conn) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		if s.conn == c {
			s.conn = nil
		}
		s.mu.Unlock()
		c.Close()
	}()
	s.log.Info("client connected", "remote", c.RemoteAddr())
	for {
		m, err := wire.Read(c)
		if err != nil {
			s.log.Info("client disconnected", "remote", c.RemoteAddr(), "err", err)
			return
		}
		req := m.GetRequest()
		if req == nil {
			s.log.Warn("closing connection: expected a request", "message", m)
			return
		}
		if s.opts.Hook != nil {
			s.opts.Hook(req)
		}
		resp := s.handle(c, req)
		resp.Id = req.GetId()
		if err := s.send(c, &emulatorv1.Message{Body: &emulatorv1.Message_Response{Response: resp}}); err != nil {
			s.log.Warn("write response", "err", err)
			return
		}
		if req.GetQuit() != nil {
			go s.Close()
			return
		}
	}
}

func (s *Server) send(c *conn, m *emulatorv1.Message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return wire.Write(c, m)
}

// emit sends an event to the active client. s.mu must be held.
func (s *Server) emit(ev *emulatorv1.Event) {
	c := s.conn
	if c == nil || !c.hello {
		return
	}
	if err := s.send(c, &emulatorv1.Message{Body: &emulatorv1.Message_Event{Event: ev}}); err != nil {
		s.log.Warn("write event", "err", err)
		c.Close()
	}
}

func (s *Server) emitStatus() {
	s.emit(&emulatorv1.Event{Body: &emulatorv1.Event_Status{Status: &emulatorv1.StatusEvent{Status: s.status()}}})
}

// status must be called with s.mu held.
func (s *Server) status() *emulatorv1.Status {
	st := &emulatorv1.Status{State: s.state, Frame: s.frame}
	if s.state != emulatorv1.Status_NO_ROM {
		st.RomPath = s.romPath
		st.GameTitle = gameTitle(s.romPath)
		st.GameCode = "FAKE"
		st.Console = "DS"
	}
	return st
}

func gameTitle(path string) string {
	base := filepath.Base(path)
	title := strings.ToUpper(strings.TrimSuffix(base, filepath.Ext(base)))
	if len(title) > 12 {
		title = title[:12]
	}
	return title
}

// runFrame runs the unit scheduler and emulates one frame. s.mu must be held.
func (s *Server) runFrame() {
	s.runUnits()
	s.frame++
	if len(s.opts.Domains) > 0 {
		d := s.opts.Domains[0]
		if m := s.mem[d.Name]; len(m) >= 4 {
			putUint(m[:4], s.frame&0xffffffff, d.BigEndian)
		}
	}
	if c := s.conn; c != nil && c.interval > 0 && s.frame%uint64(c.interval) == 0 {
		s.emit(&emulatorv1.Event{Body: &emulatorv1.Event_Frame{Frame: &emulatorv1.FrameEvent{Frame: s.frame}}})
	}
}

// loadROM must be called with s.mu held.
func (s *Server) loadROM(path string) {
	s.romPath = path
	s.mem = make(map[string][]byte, len(s.opts.Domains))
	for _, d := range s.opts.Domains {
		s.mem[d.Name] = make([]byte, d.Size)
	}
	s.frame = 0
	s.units = nil
	s.state = emulatorv1.Status_RUNNING
}

func (s *Server) domain(name string) (*Domain, []byte) {
	for i := range s.opts.Domains {
		if s.opts.Domains[i].Name == name {
			return &s.opts.Domains[i], s.mem[name]
		}
	}
	return nil, nil
}

// checkRange validates a domain access. s.mu must be held.
func (s *Server) checkRange(domain string, addr uint64, size uint64, write bool) (*Domain, []byte, *emulatorv1.Error) {
	if s.state == emulatorv1.Status_NO_ROM {
		return nil, nil, errorf(emulatorv1.Error_NO_ROM, "no ROM loaded")
	}
	d, m := s.domain(domain)
	if d == nil {
		return nil, nil, errorf(emulatorv1.Error_NOT_FOUND, "unknown domain %q", domain)
	}
	if addr > d.Size || size > d.Size-addr {
		return nil, nil, errorf(emulatorv1.Error_OUT_OF_RANGE, "%s: 0x%x+0x%x exceeds size 0x%x", domain, addr, size, d.Size)
	}
	if write && d.ReadOnly {
		return nil, nil, errorf(emulatorv1.Error_INVALID_ARGUMENT, "%s is read-only", domain)
	}
	return d, m, nil
}

func errorf(code emulatorv1.Error_Code, format string, args ...any) *emulatorv1.Error {
	return &emulatorv1.Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

func putUint(b []byte, v uint64, bigEndian bool) {
	for i := range b {
		shift := 8 * i
		if bigEndian {
			shift = 8 * (len(b) - 1 - i)
		}
		b[i] = byte(v >> shift)
	}
}

func getUint(b []byte, bigEndian bool) uint64 {
	var v uint64
	for i := range b {
		shift := 8 * i
		if bigEndian {
			shift = 8 * (len(b) - 1 - i)
		}
		v |= uint64(b[i]) << shift
	}
	return v
}
