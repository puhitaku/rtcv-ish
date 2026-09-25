// Package emu is a client for the emulator API (api/emulator/v1).
package emu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sasha-s/go-deadlock"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/emu/wire"
)

const (
	ProtocolVersion = 1
	clientName      = "rtcv-ish"
)

// Default per-call timeouts. Calls that transfer or build a whole console
// state get the long timeout; Step gets the short one plus the time the
// frames take at 30 fps.
const (
	DefaultCallTimeout     = 5 * time.Second
	DefaultLongCallTimeout = 30 * time.Second
)

type options struct {
	logger      *slog.Logger
	eventBuffer int
	clientName  string
	callTimeout time.Duration
	longTimeout time.Duration
	onTimeout   func(*Client, *TimeoutError)
}

// Option configures Dial.
type Option func(*options)

func WithLogger(l *slog.Logger) Option { return func(o *options) { o.logger = l } }

// WithEventBuffer sets how many undelivered events are kept (default 256).
func WithEventBuffer(n int) Option { return func(o *options) { o.eventBuffer = max(n, 1) } }

func WithClientName(name string) Option { return func(o *options) { o.clientName = name } }

// WithCallTimeouts overrides DefaultCallTimeout and DefaultLongCallTimeout.
// Zero keeps the default.
func WithCallTimeouts(call, long time.Duration) Option {
	return func(o *options) {
		if call > 0 {
			o.callTimeout = call
		}
		if long > 0 {
			o.longTimeout = long
		}
	}
}

// WithTimeoutHandler calls f, on the calling goroutine, whenever a call
// fails because its per-call timeout expired.
func WithTimeoutHandler(f func(*Client, *TimeoutError)) Option {
	return func(o *options) { o.onTimeout = f }
}

// Client is a connection to one emulator. It is safe for concurrent use.
type Client struct {
	conn net.Conn
	log  *slog.Logger
	info *emulatorv1.HelloResponse
	opts options

	writeMu deadlock.Mutex

	mu      deadlock.Mutex
	nextID  uint32
	pending map[uint32]chan *emulatorv1.Response
	err     error

	// Events are queued here and handed to the events channel by a pump
	// goroutine so that the reader never blocks on a slow consumer. When
	// the queue is full the oldest frame event is dropped; status events
	// are never dropped.
	queueMu   deadlock.Mutex
	queue     []*emulatorv1.Event
	queueMax  int
	queueWake chan struct{}
	events    chan *emulatorv1.Event

	done      chan struct{}
	closeOnce sync.Once
}

// Dial connects to the emulator at addr and performs the Hello handshake.
// ctx bounds the dial and the handshake only; the connection lives until
// Close is called or the emulator disconnects.
func Dial(ctx context.Context, addr string, opts ...Option) (*Client, error) {
	o := options{
		logger:      slog.Default(),
		eventBuffer: 256,
		clientName:  clientName,
		callTimeout: DefaultCallTimeout,
		longTimeout: DefaultLongCallTimeout,
	}
	for _, opt := range opts {
		opt(&o)
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	c := newClient(conn, o)
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_Hello{Hello: &emulatorv1.HelloRequest{
		ProtocolVersion: ProtocolVersion,
		ClientName:      o.clientName,
	}}})
	if err == nil {
		c.info, err = expect(resp, resp.GetHello())
	}
	if err == nil && c.info.GetProtocolVersion() != ProtocolVersion {
		err = fmt.Errorf("%w: emulator speaks protocol version %d, want %d", ErrProtocol, c.info.GetProtocolVersion(), ProtocolVersion)
	}
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("emu: hello: %w", err)
	}
	c.log.Debug("connected to emulator", "addr", addr, "emulator", c.info.GetEmulator(), "version", c.info.GetVersion())
	return c, nil
}

func newClient(conn net.Conn, o options) *Client {
	c := &Client{
		conn:      conn,
		log:       o.logger.With("emulator", conn.RemoteAddr().String()),
		opts:      o,
		pending:   make(map[uint32]chan *emulatorv1.Response),
		queueMax:  o.eventBuffer,
		queueWake: make(chan struct{}, 1),
		events:    make(chan *emulatorv1.Event),
		done:      make(chan struct{}),
	}
	go c.readLoop()
	go c.pumpEvents()
	return c
}

// Info returns the emulator's Hello response.
func (c *Client) Info() *emulatorv1.HelloResponse { return c.info }

// Events delivers events pushed by the emulator. It is closed when the
// connection ends.
func (c *Client) Events() <-chan *emulatorv1.Event { return c.events }

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err returns why the connection ended, or nil while it is open.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close closes the connection. Pending calls fail with ErrClosed.
func (c *Client) Close() error {
	c.shutdown(ErrClosed)
	return nil
}

func (c *Client) shutdown(err error) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.err = err
		c.pending = nil
		c.mu.Unlock()
		close(c.done)
		c.conn.Close()
	})
}

// Do sends a raw request and waits for its response. The request id is
// assigned by the client. Error responses are returned as *Error.
func (c *Client) Do(ctx context.Context, req *emulatorv1.Request) (*emulatorv1.Response, error) {
	return c.call(ctx, req)
}

func (c *Client) call(ctx context.Context, req *emulatorv1.Request) (*emulatorv1.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d := c.timeoutFor(req)
	terr := &TimeoutError{Request: requestName(req), After: d}
	ctx, cancel := context.WithTimeoutCause(ctx, d, terr)
	defer cancel()
	resp, err := c.roundTrip(ctx, req)
	if err != nil && errors.Is(err, context.DeadlineExceeded) && context.Cause(ctx) == terr {
		c.log.Warn("emulator call timed out", "request", terr.Request, "after", d)
		if c.opts.onTimeout != nil {
			c.opts.onTimeout(c, terr)
		}
		return nil, terr
	}
	return resp, err
}

func (c *Client) roundTrip(ctx context.Context, req *emulatorv1.Request) (*emulatorv1.Response, error) {
	ch := make(chan *emulatorv1.Response, 1)
	c.mu.Lock()
	if c.pending == nil {
		err := c.err
		c.mu.Unlock()
		return nil, err
	}
	id := c.allocID()
	c.pending[id] = ch
	c.mu.Unlock()

	req.Id = id
	if err := c.send(ctx, &emulatorv1.Message{Body: &emulatorv1.Message_Request{Request: req}}); err != nil {
		c.forget(id)
		return nil, err
	}

	select {
	case resp := <-ch:
		return result(resp)
	case <-ctx.Done():
		c.forget(id)
		return nil, ctx.Err()
	case <-c.done:
		select {
		case resp := <-ch:
			return result(resp)
		default:
			return nil, c.Err()
		}
	}
}

// timeoutFor is the per-call timeout of a request.
func (c *Client) timeoutFor(req *emulatorv1.Request) time.Duration {
	switch b := req.GetBody().(type) {
	case *emulatorv1.Request_LoadRom, *emulatorv1.Request_LoadState, *emulatorv1.Request_SaveState:
		return c.opts.longTimeout
	case *emulatorv1.Request_Step:
		return c.opts.callTimeout + time.Duration(b.Step.GetFrames())*time.Second/30
	}
	return c.opts.callTimeout
}

func requestName(req *emulatorv1.Request) string {
	return strings.TrimPrefix(fmt.Sprintf("%T", req.GetBody()), "*emulatorv1.Request_")
}

func result(resp *emulatorv1.Response) (*emulatorv1.Response, error) {
	if e := resp.GetError(); e != nil {
		return nil, newError(e)
	}
	return resp, nil
}

// allocID must be called with c.mu held.
func (c *Client) allocID() uint32 {
	for {
		c.nextID++
		if _, busy := c.pending[c.nextID]; c.nextID != 0 && !busy {
			return c.nextID
		}
	}
}

func (c *Client) forget(id uint32) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// send writes one frame. A failed or partial write leaves the stream in an
// unknown state, so it closes the connection.
func (c *Client) send(ctx context.Context, m *emulatorv1.Message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.done:
		return c.Err()
	default:
	}
	if dl, ok := ctx.Deadline(); ok {
		c.conn.SetWriteDeadline(dl)
		defer c.conn.SetWriteDeadline(time.Time{})
	}
	if err := wire.Write(c.conn, m); err != nil {
		if errors.Is(err, wire.ErrTooLarge) {
			return err
		}
		c.shutdown(fmt.Errorf("emu: write: %w", err))
		return c.Err()
	}
	return nil
}

func (c *Client) readLoop() {
	for {
		m, err := wire.Read(c.conn)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				err = ErrClosed
			} else {
				err = fmt.Errorf("emu: read: %w", err)
			}
			c.shutdown(err)
			return
		}
		switch b := m.GetBody().(type) {
		case *emulatorv1.Message_Response:
			c.dispatch(b.Response)
		case *emulatorv1.Message_Event:
			c.enqueue(b.Event)
		default:
			c.log.Warn("unexpected message from emulator", "message", m)
		}
	}
}

func (c *Client) dispatch(resp *emulatorv1.Response) {
	c.mu.Lock()
	ch, ok := c.pending[resp.GetId()]
	delete(c.pending, resp.GetId())
	c.mu.Unlock()
	if !ok {
		c.log.Debug("discarding response without waiter", "id", resp.GetId())
		return
	}
	ch <- resp
}

func (c *Client) enqueue(ev *emulatorv1.Event) {
	c.queueMu.Lock()
	if len(c.queue) >= c.queueMax && ev.GetFrame() != nil {
		i := slices.IndexFunc(c.queue, func(e *emulatorv1.Event) bool { return e.GetFrame() != nil })
		if i < 0 {
			c.queueMu.Unlock()
			return
		}
		c.queue = append(c.queue[:i], c.queue[i+1:]...)
	}
	c.queue = append(c.queue, ev)
	c.queueMu.Unlock()
	select {
	case c.queueWake <- struct{}{}:
	default:
	}
}

func (c *Client) pumpEvents() {
	defer close(c.events)
	for {
		c.queueMu.Lock()
		var ev *emulatorv1.Event
		if len(c.queue) > 0 {
			ev = c.queue[0]
			c.queue = c.queue[1:]
		}
		c.queueMu.Unlock()
		if ev == nil {
			select {
			case <-c.queueWake:
				continue
			case <-c.done:
				return
			}
		}
		select {
		case c.events <- ev:
		case <-c.done:
			return
		}
	}
}

func expect[T any](resp *emulatorv1.Response, v *T) (*T, error) {
	if v == nil {
		return nil, fmt.Errorf("%w: unexpected response %T to request %d", ErrProtocol, resp.GetBody(), resp.GetId())
	}
	return v, nil
}
