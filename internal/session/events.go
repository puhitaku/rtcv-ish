package session

import (
	"github.com/sasha-s/go-deadlock"
)

// Event types streamed to the frontend (api/frontend/openapi.yaml).
const (
	EventStatus     = "status"
	EventFrame      = "frame"
	EventBlast      = "blast"
	EventStash      = "stash"
	EventStockpile  = "stockpile"
	EventSavestates = "savestates"
	EventSettings   = "settings"
	EventDomains    = "domains"
	EventLists      = "lists"
	EventUnits      = "units"
	EventLog        = "log"
)

// Event is one SSE event; Data is marshaled as JSON.
type Event struct {
	Type string
	Data any
}

type FrameEvent struct {
	Frame int64 `json:"frame"`
}

type BlastEvent struct {
	Count     int     `json:"count"`
	Engine    string  `json:"engine"`
	ElapsedMs float64 `json:"elapsedMs"`
}

// Reasons of a units event.
const (
	UnitsApply      = "apply"
	UnitsRemove     = "remove"
	UnitsClear      = "clear"
	UnitsLoad       = "load"
	UnitsReset      = "reset"
	UnitsGame       = "game"
	UnitsConnect    = "connect"
	UnitsDisconnect = "disconnect"
)

// UnitsEvent says the set of scheduled units changed. Cleared is the
// number of units a savestate load or reset removed.
type UnitsEvent struct {
	Reason  string `json:"reason"`
	Cleared int    `json:"cleared"`
}

type LogEvent struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
}

// subscriberBuffer is how many events a slow subscriber may lag behind
// before events are dropped for it.
const subscriberBuffer = 256

type broker struct {
	mu     deadlock.Mutex
	subs   map[chan Event]struct{}
	closed bool
}

func newBroker() *broker { return &broker{subs: make(map[chan Event]struct{})} }

func (b *broker) subscribe() (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuffer)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		close(ch)
		return ch, func() {}
	}
	b.subs[ch] = struct{}{}
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
	}
}

func (b *broker) publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (b *broker) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for ch := range b.subs {
		delete(b.subs, ch)
		close(ch)
	}
}

// Subscribe returns a channel of events and a function that ends the
// subscription. The channel is closed when the session closes.
func (s *Session) Subscribe() (<-chan Event, func()) { return s.broker.subscribe() }

func (s *Session) changed(typ string) {
	s.broker.publish(Event{Type: typ, Data: struct{}{}})
}

func (s *Session) unitsChanged(reason string, cleared int) {
	s.broker.publish(Event{Type: EventUnits, Data: UnitsEvent{Reason: reason, Cleared: cleared}})
}

// notify sends a user-facing log message.
func (s *Session) notify(level, msg string) {
	s.broker.publish(Event{Type: EventLog, Data: LogEvent{Level: level, Msg: msg}})
}
