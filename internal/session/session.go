// Package session is the rtcv-ish coordinator. A Session owns the
// settings, the emulator connection, the domain selection, the seeded RNG,
// the list registry, the Glitch Harvester store and the event broker.
//
// Two levels of locking keep a slow or hung emulator from blocking the
// core. s.mu guards the session state and is only held briefly; no
// emulator call is ever made with it held. Operations that talk to the
// emulator in several steps are serialized by an operation gate (see
// op.go): an operation reads what it needs under s.mu, calls the emulator
// with only the gate held, and writes the results back under s.mu if the
// connection it started on is still current. Every emulator call has a
// per-call timeout; a call that times out marks the connection
// unresponsive until a ping succeeds again.
package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sasha-s/go-deadlock"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/corrupt"
	"github.com/puhitaku/rtcv-ish/internal/corrupt/lists"
	"github.com/puhitaku/rtcv-ish/internal/emu"
	"github.com/puhitaku/rtcv-ish/internal/stockpile"
)

// opTimeout bounds one API operation's emulator calls.
const opTimeout = 60 * time.Second

// Timeouts tunes how long the session waits for the emulator and for
// other operations. Zero fields use the defaults.
type Timeouts struct {
	// Call and LongCall are the per-call emulator timeouts (emu defaults:
	// 5 s, and 30 s for LoadRom, LoadState and SaveState).
	Call, LongCall time.Duration
	// OpWait is how long an operation waits for a running one before it
	// fails with KindBusy. Default 5 s.
	OpWait time.Duration
	// PingInterval and Ping control how an unresponsive emulator is probed.
	// Default 2 s each.
	PingInterval, Ping time.Duration
	// QuitGrace is how long Quit and Close wait for a launched emulator to
	// exit before killing it. Default 3 s.
	QuitGrace time.Duration
}

func (t Timeouts) withDefaults() Timeouts {
	def := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	def(&t.Call, emu.DefaultCallTimeout)
	def(&t.LongCall, emu.DefaultLongCallTimeout)
	def(&t.OpWait, 5*time.Second)
	def(&t.PingInterval, 2*time.Second)
	def(&t.Ping, 2*time.Second)
	def(&t.QuitGrace, 3*time.Second)
	return t
}

// EmulatorSpec is a bundled emulator the core can launch.
type EmulatorSpec struct {
	Name string
	Path string
	// Args are passed to the executable. "{port}" in an argument is
	// replaced with the emulator API port the core picked.
	Args []string
}

type Config struct {
	DataDir   string
	Seed      int64
	Logger    *slog.Logger
	Emulators []EmulatorSpec
	Version   string
	// VersionInfo is reported in Status.versionInfo.
	VersionInfo VersionInfo
	Timeouts    Timeouts
}

// VersionInfo is the pieces Status.version is composed of.
type VersionInfo struct {
	Release string `json:"release"`
	Commit  string `json:"commit"`
	Dirty   bool   `json:"dirty"`
	Kind    string `json:"kind"`
}

type Session struct {
	cfg    Config
	tm     Timeouts
	log    *slog.Logger
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	once   sync.Once
	broker *broker
	status atomic.Pointer[Status]
	gate   *opGate

	procMu deadlock.Mutex
	procs  map[*process]struct{}

	// rng and nextUnitID are only used by operations holding the gate.
	rng        *rand.Rand
	nextUnitID uint64

	mu           deadlock.Mutex
	settings     *corrupt.Settings
	lists        *lists.Registry
	store        *stockpile.Store
	conn         *conn
	game         GameStatus
	protoDomains []*emulatorv1.Domain
	domains      []corrupt.Domain
	selected     []string
	infinite     []uint64
	blLayer      *corrupt.Layer
	blBackup     *corrupt.Layer
	blOn         bool
	// lastAutoFrame is the emulator frame auto-corrupt counts errorDelay
	// from: the frame it was enabled at, the game started at or it last
	// blasted at.
	lastAutoFrame int64
	lastFrameEv   time.Time
	lastBackup    time.Time
	// lastBusy is the gate state of the last published status event.
	lastBusy gateState
}

// New opens the data directory and starts background work. The session
// stops when ctx is cancelled or Close is called.
func New(ctx context.Context, cfg Config) (*Session, error) {
	if cfg.DataDir == "" {
		return nil, errors.New("session: DataDir is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "lists"), 0o755); err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}
	store, err := stockpile.Open(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Session{
		cfg:    cfg,
		tm:     cfg.Timeouts.withDefaults(),
		log:    cfg.Logger,
		ctx:    ctx,
		cancel: cancel,
		broker: newBroker(),
		gate:   newOpGate(),
		procs:  make(map[*process]struct{}),
		rng:    rand.New(rand.NewPCG(uint64(cfg.Seed), 0)),
		store:  store,
		game:   GameStatus{State: StateNoRom},
	}
	s.settings = s.loadSettings()
	s.reloadListsLocked()
	s.mu.Lock()
	s.publishStatusLocked()
	s.mu.Unlock()
	s.wg.Add(2)
	go s.protectionLoop()
	go s.busyLoop()
	return s, nil
}

// Close drops the emulator connection, stops launched emulators and waits
// for background work. It does not wait for running operations: their
// calls fail once the connection is closed.
func (s *Session) Close() error {
	s.once.Do(func() {
		s.cancel()
		s.mu.Lock()
		cn := s.conn
		quit := cn != nil && cn.proc != nil && !cn.unresponsive
		s.mu.Unlock()
		if quit {
			qctx, cancel := context.WithTimeout(context.Background(), time.Second)
			cn.client.Quit(qctx)
			cancel()
		}
		s.mu.Lock()
		s.dropConnLocked()
		s.mu.Unlock()
		s.stopProcesses()
		s.broker.close()
		s.wg.Wait()
	})
	return nil
}

func (s *Session) DataDir() string { return s.cfg.DataDir }

func (s *Session) listsDir() string { return filepath.Join(s.cfg.DataDir, "lists") }

// opCtx bounds an API operation and ties it to the session.
func (s *Session) opCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	stop := context.AfterFunc(s.ctx, cancel)
	return ctx, func() { stop(); cancel() }
}

// ---- Errors ----

type Kind int

const (
	KindInvalid Kind = iota + 1
	KindOutOfRange
	KindNotFound
	KindNoROM
	KindAlreadyConnected
	KindNoBackup
	KindConnectFailed
	KindDisconnected
	// KindBusy: another operation is running and did not finish in time.
	KindBusy
	// KindUnresponsive: the emulator stopped answering calls.
	KindUnresponsive
)

// Error is a session error the API maps to a status and code.
type Error struct {
	Kind Kind
	Msg  string
	Err  error
}

func (e *Error) Error() string { return e.Msg }
func (e *Error) Unwrap() error { return e.Err }

func errorf(kind Kind, format string, args ...any) *Error {
	return &Error{Kind: kind, Msg: fmt.Sprintf(format, args...)}
}

var errDisconnected = errorf(KindDisconnected, "no emulator connected")

func passthrough(err error) bool {
	var se *Error
	var ee *emu.Error
	return errors.As(err, &se) || errors.As(err, &ee) || errors.Is(err, emu.ErrClosed) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// classify turns errors from corrupt, stockpile and the file system into
// session errors. Emulator and context errors are returned unchanged.
func classify(err error) error {
	switch {
	case err == nil || passthrough(err):
		return err
	case errors.As(err, new(*corrupt.RangeError)):
		return &Error{Kind: KindOutOfRange, Msg: err.Error(), Err: err}
	case errors.Is(err, stockpile.ErrNotFound), errors.Is(err, os.ErrNotExist):
		return &Error{Kind: KindNotFound, Msg: err.Error(), Err: err}
	case errors.Is(err, stockpile.ErrInvalid), errors.Is(err, corrupt.ErrNoDomains),
		errors.Is(err, corrupt.ErrUnknownDomain), errors.Is(err, corrupt.ErrOutOfRange):
		return &Error{Kind: KindInvalid, Msg: err.Error(), Err: err}
	}
	return err
}

// invalid is classify, with every other error an invalid argument.
func invalid(err error) error {
	if err = classify(err); err == nil || passthrough(err) {
		return err
	}
	return &Error{Kind: KindInvalid, Msg: err.Error(), Err: err}
}

// ---- Status ----

const (
	StateNoRom   = "noRom"
	StateRunning = "running"
	StatePaused  = "paused"
)

type Status struct {
	Version     string        `json:"version"`
	VersionInfo VersionInfo   `json:"versionInfo"`
	DataDir     string        `json:"dataDir"`
	Connected   bool          `json:"connected"`
	Address     string        `json:"address"`
	Emulator    *EmulatorInfo `json:"emulator,omitempty"`
	Game        *GameStatus   `json:"game,omitempty"`
	// Unresponsive is set while the connected emulator does not answer.
	Unresponsive      bool            `json:"unresponsive"`
	ProtectionBackups int             `json:"protectionBackups"`
	BlastLayer        BlastLayerState `json:"blastLayer"`
	// Busy is the running operation, if any.
	Busy *BusyStatus `json:"busy,omitempty"`
}

type BusyStatus struct {
	Operation string `json:"operation"`
	SinceMs   int64  `json:"sinceMs"`
}

type BlastLayerState struct {
	Available bool `json:"available"`
	On        bool `json:"on"`
}

type EmulatorInfo struct {
	Name            string       `json:"name"`
	Version         string       `json:"version"`
	System          string       `json:"system"`
	ProtocolVersion int          `json:"protocolVersion"`
	Capabilities    Capabilities `json:"capabilities"`
}

type Capabilities struct {
	Savestates bool  `json:"savestates"`
	Screenshot bool  `json:"screenshot"`
	Input      bool  `json:"input"`
	LoadRom    bool  `json:"loadRom"`
	Reset      bool  `json:"reset"`
	MaxPayload int64 `json:"maxPayload"`
	// Unit modes beyond frame the emulator implements.
	ScanlineUnits bool `json:"scanlineUnits"`
	HardUnits     bool `json:"hardUnits"`
}

type GameStatus struct {
	State   string `json:"state"`
	Frame   int64  `json:"frame"`
	RomPath string `json:"romPath"`
	Title   string `json:"title"`
	Code    string `json:"code"`
	Console string `json:"console"`
}

func gameFromProto(st *emulatorv1.Status) GameStatus {
	state := StateNoRom
	switch st.GetState() {
	case emulatorv1.Status_RUNNING:
		state = StateRunning
	case emulatorv1.Status_PAUSED:
		state = StatePaused
	}
	return GameStatus{
		State:   state,
		Frame:   int64(st.GetFrame()),
		RomPath: st.GetRomPath(),
		Title:   st.GetGameTitle(),
		Code:    st.GetGameCode(),
		Console: st.GetConsole(),
	}
}

func infoFromHello(h *emulatorv1.HelloResponse) EmulatorInfo {
	c := h.GetCapabilities()
	return EmulatorInfo{
		Name:            h.GetEmulator(),
		Version:         h.GetVersion(),
		System:          h.GetSystem(),
		ProtocolVersion: int(h.GetProtocolVersion()),
		Capabilities: Capabilities{
			Savestates:    c.GetSavestates(),
			Screenshot:    c.GetScreenshot(),
			Input:         c.GetInput(),
			LoadRom:       c.GetLoadRom(),
			Reset:         c.GetReset_(),
			MaxPayload:    int64(c.GetMaxPayload()),
			ScanlineUnits: c.GetScanlineUnits(),
			HardUnits:     c.GetHardUnits(),
		},
	}
}

// Status returns the current status without waiting for running
// operations.
func (s *Session) Status() Status {
	st := *s.status.Load()
	st.Busy = s.gate.status()
	return st
}

func (s *Session) statusLocked() *Status {
	st := &Status{
		Version:           s.cfg.Version,
		VersionInfo:       s.cfg.VersionInfo,
		DataDir:           s.cfg.DataDir,
		ProtectionBackups: s.store.BackupCount(),
		BlastLayer:        BlastLayerState{Available: s.blLayer != nil, On: s.blLayer != nil && s.blOn},
	}
	if s.conn != nil {
		info := s.conn.info
		game := s.game
		st.Connected = true
		st.Unresponsive = s.conn.unresponsive
		st.Address = s.conn.addr
		st.Emulator = &info
		st.Game = &game
	}
	return st
}

// storeStatusLocked updates the status snapshot without an event.
func (s *Session) storeStatusLocked() { s.status.Store(s.statusLocked()) }

func (s *Session) publishStatusLocked() {
	st := s.statusLocked()
	s.status.Store(st)
	ev := *st
	s.lastBusy = s.gate.state()
	ev.Busy = s.lastBusy.busy()
	s.broker.publish(Event{Type: EventStatus, Data: &ev})
}
