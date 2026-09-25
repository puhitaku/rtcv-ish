// Package emutest holds conformance tests for the emulator API. They run
// against the fake emulator and against real emulators in test/e2e.
package emutest

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"time"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/emu"
)

// Scratch is a little-endian memory area the running game does not touch.
// Tests use up to 0x100 bytes from Base.
type Scratch struct {
	Domain string
	Base   uint64
}

const scratchSize = 0x100

func (s Scratch) String() string { return fmt.Sprintf("%s+0x%x", s.Domain, s.Base) }

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

func le32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

func must(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

// WantCode fails unless err carries the emulator error code.
func WantCode(t *testing.T, err error, code emulatorv1.Error_Code, what string) {
	t.Helper()
	var e *emu.Error
	if !errors.As(err, &e) {
		t.Errorf("%s: err = %v, want emulator error %s", what, err, code)
		return
	}
	if e.Code != code {
		t.Errorf("%s: code = %s (%q), want %s", what, e.Code, e.Message, code)
	}
}

// Drain discards buffered events.
func Drain(c *emu.Client) {
	for {
		select {
		case _, ok := <-c.Events():
			if !ok {
				return
			}
		default:
			return
		}
	}
}

// NextEvent waits for an event matching f.
func NextEvent(t *testing.T, c *emu.Client, timeout time.Duration, what string, f func(*emulatorv1.Event) bool) *emulatorv1.Event {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-c.Events():
			if !ok {
				t.Fatalf("waiting for %s: events channel closed: %v", what, c.Err())
			}
			if f(ev) {
				return ev
			}
		case <-timer.C:
			t.Fatalf("no %s within %s", what, timeout)
		}
	}
}

func isFrame(ev *emulatorv1.Event) bool { return ev.GetFrame() != nil }

func isState(state emulatorv1.Status_State) func(*emulatorv1.Event) bool {
	return func(ev *emulatorv1.Event) bool {
		return ev.GetStatus() != nil && ev.GetStatus().GetStatus().GetState() == state
	}
}
