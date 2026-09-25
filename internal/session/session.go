// Package session is the rtcv-ish coordinator. A Session owns the
// settings, the emulator connection, the domain selection, the seeded RNG,
// the list registry, the Glitch Harvester store and the event broker, and
// serializes every state change with one mutex.
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
}

type Session struct {
	cfg    Config
	log    *slog.Logger
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	once   sync.Once
	broker *broker
	status atomic.Pointer[Status]

	procMu deadlock.Mutex
	procs  map[*process]struct{}

	mu           deadlock.Mutex
	rng          *rand.Rand
	settings     *corrupt.Settings
	lists        *lists.Registry
	store        *stockpile.Store
	conn         *conn
	game         GameStatus
	protoDomains []*emulatorv1.Domain
	domains      []corrupt.Domain
	selected     []string
	nextUnitID   uint64
	infinite     []uint64
	blLayer      *corrupt.Layer
	blBackup     *corrupt.Layer
	blOn         bool
	autoCount    int
	lastFrameEv  time.Time
	lastBackup   time.Time
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
		log:    cfg.Logger,
		ctx:    ctx,
		cancel: cancel,
		broker: newBroker(),
		procs:  make(map[*process]struct{}),
		rng:    rand.New(rand.NewPCG(uint64(cfg.Seed), 0)),
		store:  store,
		game:   GameStatus{State: StateNoRom},
	}
	s.settings = s.loadSettings()
	s.reloadListsLocked()
	s.publishStatusLocked()
	s.wg.Add(1)
	go s.protectionLoop()
	return s, nil
}

// Close drops the emulator connection, stops launched emulators and waits
// for background work.
func (s *Session) Close() error {
	s.once.Do(func() {
		s.cancel()
		s.mu.Lock()
		if s.conn != nil {
			if s.conn.proc != nil {
				qctx, cancel := context.WithTimeout(context.Background(), time.Second)
				s.conn.client.Quit(qctx)
				cancel()
			}
			s.dropConnLocked()
		}
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
	Version           string          `json:"version"`
	DataDir           string          `json:"dataDir"`
	Connected         bool            `json:"connected"`
	Address           string          `json:"address"`
	Emulator          *EmulatorInfo   `json:"emulator,omitempty"`
	Game              *GameStatus     `json:"game,omitempty"`
	ProtectionBackups int             `json:"protectionBackups"`
	BlastLayer        BlastLayerState `json:"blastLayer"`
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
			Savestates: c.GetSavestates(),
			Screenshot: c.GetScreenshot(),
			Input:      c.GetInput(),
			LoadRom:    c.GetLoadRom(),
			Reset:      c.GetReset_(),
			MaxPayload: int64(c.GetMaxPayload()),
		},
	}
}

// Status returns the current status without waiting for running
// operations.
func (s *Session) Status() Status { return *s.status.Load() }

func (s *Session) statusLocked() *Status {
	st := &Status{
		Version:           s.cfg.Version,
		DataDir:           s.cfg.DataDir,
		ProtectionBackups: s.store.BackupCount(),
		BlastLayer:        BlastLayerState{Available: s.blLayer != nil, On: s.blLayer != nil && s.blOn},
	}
	if s.conn != nil {
		info := s.conn.info
		game := s.game
		st.Connected = true
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
	s.broker.publish(Event{Type: EventStatus, Data: st})
}
