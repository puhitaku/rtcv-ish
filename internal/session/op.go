package session

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/sasha-s/go-deadlock"

	"github.com/puhitaku/rtcv-ish/internal/emu"
)

// maxWaitingOps is how many operations may wait for a running one. More
// fail with KindBusy at once instead of piling up.
const maxWaitingOps = 4

// opGate serializes operations that talk to the emulator. It is a
// semaphore rather than a mutex so that waiting is bounded: API operations
// wait at most Timeouts.OpWait, background work only tries.
type opGate struct {
	sem     chan struct{}
	waiting atomic.Int32
	// changed is signalled when the gate is acquired or released.
	changed chan struct{}

	mu    deadlock.Mutex
	name  string
	since time.Time
}

func newOpGate() *opGate {
	return &opGate{sem: make(chan struct{}, 1), changed: make(chan struct{}, 1)}
}

func (g *opGate) tryAcquire(name string) bool {
	select {
	case g.sem <- struct{}{}:
		g.set(name)
		return true
	default:
		return false
	}
}

func (g *opGate) acquire(ctx context.Context, name string, wait time.Duration) error {
	if g.tryAcquire(name) {
		return nil
	}
	if g.waiting.Add(1) > maxWaitingOps {
		g.waiting.Add(-1)
		return g.busy()
	}
	defer g.waiting.Add(-1)
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case g.sem <- struct{}{}:
		g.set(name)
		return nil
	case <-t.C:
		return g.busy()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *opGate) release() {
	g.mu.Lock()
	g.name = ""
	g.mu.Unlock()
	<-g.sem
	g.signal()
}

func (g *opGate) set(name string) {
	g.mu.Lock()
	g.name, g.since = name, time.Now()
	g.mu.Unlock()
	g.signal()
}

func (g *opGate) signal() {
	select {
	case g.changed <- struct{}{}:
	default:
	}
}

// gateState identifies the running operation; the zero value means idle.
type gateState struct {
	name  string
	since time.Time
}

func (g *opGate) state() gateState {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.name == "" {
		return gateState{}
	}
	return gateState{g.name, g.since}
}

func (g *opGate) status() *BusyStatus { return g.state().busy() }

func (st gateState) busy() *BusyStatus {
	if st.name == "" {
		return nil
	}
	return &BusyStatus{Operation: st.name, SinceMs: time.Since(st.since).Milliseconds()}
}

// busyEventInterval is the minimum time between status events published
// for gate changes. Changes in between are coalesced, so an operation
// yields at most one busy and one idle event.
const busyEventInterval = 100 * time.Millisecond

// busyLoop publishes a status event when the running operation changes.
func (s *Session) busyLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.gate.changed:
		}
		s.mu.Lock()
		if s.gate.state() != s.lastBusy {
			s.publishStatusLocked()
		}
		s.mu.Unlock()
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(busyEventInterval):
		}
	}
}

func (g *opGate) busy() error {
	name := "unknown"
	if st := g.status(); st != nil {
		name = st.Operation
	}
	return errorf(KindBusy, "another operation is running: %s", name)
}

// beginOp acquires the gate for an API operation. With checkConn, an
// unresponsive emulator fails the operation at once.
func (s *Session) beginOp(ctx context.Context, name string, checkConn bool) (release func(), err error) {
	if checkConn {
		if err := s.checkResponsive(); err != nil {
			return nil, err
		}
	}
	if err := s.gate.acquire(ctx, name, s.tm.OpWait); err != nil {
		return nil, err
	}
	if checkConn {
		if err := s.checkResponsive(); err != nil {
			s.gate.release()
			return nil, err
		}
	}
	return s.gate.release, nil
}

// withOp runs f as the operation name with an operation context.
func (s *Session) withOp(ctx context.Context, name string, f func(context.Context) error) error {
	ctx, cancel := s.opCtx(ctx)
	defer cancel()
	release, err := s.beginOp(ctx, name, true)
	if err != nil {
		return err
	}
	defer release()
	return classify(f(ctx))
}

func (s *Session) checkResponsive() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil && s.conn.unresponsive {
		return errUnresponsive(s.conn.addr)
	}
	return nil
}

func errUnresponsive(addr string) error {
	return errorf(KindUnresponsive, "the emulator at %s is not responding; disconnect or quit it", addr)
}

// current returns the connection an operation works on. Operations pass
// it on and check it is still current before writing results back.
func (s *Session) current() (*conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return nil, errDisconnected
	}
	return s.conn, nil
}

func (s *Session) currentROM() (*conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return nil, errDisconnected
	}
	if s.game.State == StateNoRom {
		return nil, errorf(KindNoROM, "no ROM loaded")
	}
	return s.conn, nil
}

// currentResponsive is current for calls made without the gate (memory,
// screenshots), which fail fast while the emulator is unresponsive.
func (s *Session) currentResponsive() (*conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return nil, errDisconnected
	}
	if s.conn.unresponsive {
		return nil, errUnresponsive(s.conn.addr)
	}
	return s.conn, nil
}

// commit runs f under s.mu if cn is still the connection.
func (s *Session) commit(cn *conn, f func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != cn {
		return errDisconnected
	}
	f()
	return nil
}

// ---- Unresponsive emulators ----

// onCallTimeout is the emu client's timeout handler. It runs on the
// calling goroutine, which never holds s.mu.
func (s *Session) onCallTimeout(c *emu.Client, e *emu.TimeoutError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cn := s.conn
	if cn == nil || cn.client != c || cn.unresponsive {
		return
	}
	cn.unresponsive = true
	s.log.Warn("emulator is not responding", "addr", cn.addr, "request", e.Request, "after", e.After)
	s.notify("error", fmt.Sprintf("The emulator is not responding (%s did not answer within %s)", e.Request, e.After))
	s.publishStatusLocked()
}

// pingLoop probes an unresponsive emulator until it answers again.
func (s *Session) pingLoop(cn *conn) {
	defer s.wg.Done()
	t := time.NewTicker(s.tm.PingInterval)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-cn.client.Done():
			return
		case <-t.C:
		}
		s.mu.Lock()
		flagged := s.conn == cn && cn.unresponsive
		s.mu.Unlock()
		if !flagged {
			continue
		}
		ctx, cancel := context.WithTimeout(s.ctx, s.tm.Ping)
		err := cn.client.Ping(ctx)
		cancel()
		if err != nil {
			s.log.Debug("emulator still not responding", "addr", cn.addr, "err", err)
			continue
		}
		s.mu.Lock()
		if s.conn == cn && cn.unresponsive {
			cn.unresponsive = false
			// The stuck call may have changed the game meanwhile.
			cn.refreshPending = true
			s.log.Info("emulator is responding again", "addr", cn.addr)
			s.notify("info", fmt.Sprintf("The emulator at %s is responding again", cn.addr))
			s.publishStatusLocked()
		}
		s.mu.Unlock()
	}
}
