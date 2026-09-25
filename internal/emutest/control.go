package emutest

import (
	"testing"
	"time"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/emu"
)

// EventTimeout bounds waits for events from a running emulator.
var EventTimeout = 10 * time.Second

// RunControl tests Step, Pause/Resume with frame events, and Reset. A ROM
// must be loaded. The emulator is left paused.
func RunControl(t *testing.T, c *emu.Client, s Scratch) {
	t.Run("step", func(t *testing.T) {
		ctx := ctx(t)
		f1, err := c.Step(ctx, 1)
		must(t, err, "Step(1)")
		f2, err := c.Step(ctx, 1)
		must(t, err, "Step(1)")
		if f2 != f1+1 {
			t.Errorf("Step(1): frame %d -> %d, want +1", f1, f2)
		}
		f3, err := c.Step(ctx, 10)
		must(t, err, "Step(10)")
		if f3 != f2+10 {
			t.Errorf("Step(10): frame %d -> %d, want +10", f2, f3)
		}
		st, err := c.Status(ctx)
		must(t, err, "Status")
		if st.GetState() != emulatorv1.Status_PAUSED || st.GetFrame() != f3 {
			t.Errorf("status after Step = %s frame %d, want PAUSED frame %d", st.GetState(), st.GetFrame(), f3)
		}
		_, err = c.Step(ctx, 0)
		WantCode(t, err, emulatorv1.Error_INVALID_ARGUMENT, "Step(0)")
	})

	t.Run("pause resume events", func(t *testing.T) {
		ctx := ctx(t)
		_, err := c.Step(ctx, 1)
		must(t, err, "Step")
		must(t, c.Subscribe(ctx, 1), "Subscribe(1)")
		defer c.Subscribe(ctx, 0)
		Drain(c)

		must(t, c.Resume(ctx), "Resume")
		must(t, c.Resume(ctx), "second Resume")
		st, err := c.Status(ctx)
		must(t, err, "Status")
		if st.GetState() != emulatorv1.Status_RUNNING {
			t.Fatalf("state after Resume = %s, want RUNNING", st.GetState())
		}
		NextEvent(t, c, EventTimeout, "StatusEvent RUNNING", isState(emulatorv1.Status_RUNNING))
		var last uint64
		for i := range 3 {
			ev := NextEvent(t, c, EventTimeout, "FrameEvent while running", isFrame)
			if f := ev.GetFrame().GetFrame(); i > 0 && f <= last {
				t.Errorf("frame events not increasing: %d after %d", f, last)
			} else {
				last = f
			}
		}

		must(t, c.Pause(ctx), "Pause")
		must(t, c.Pause(ctx), "second Pause")
		st, err = c.Status(ctx)
		must(t, err, "Status")
		if st.GetState() != emulatorv1.Status_PAUSED {
			t.Fatalf("state after Pause = %s, want PAUSED", st.GetState())
		}
		NextEvent(t, c, EventTimeout, "StatusEvent PAUSED", isState(emulatorv1.Status_PAUSED))
		deadline := time.After(300 * time.Millisecond)
		for {
			select {
			case ev := <-c.Events():
				if f := ev.GetFrame().GetFrame(); f > st.GetFrame() {
					t.Fatalf("FrameEvent %d after pausing at frame %d", f, st.GetFrame())
				}
				continue
			case <-deadline:
			}
			break
		}
		st2, err := c.Status(ctx)
		must(t, err, "Status")
		if st2.GetFrame() != st.GetFrame() {
			t.Errorf("frame advanced while paused: %d -> %d", st.GetFrame(), st2.GetFrame())
		}
	})

	t.Run("reset", func(t *testing.T) {
		ctx := ctx(t)
		before, err := c.Step(ctx, 30)
		must(t, err, "Step(30)")
		must(t, c.ApplyUnits(ctx, []*emulatorv1.Unit{{Id: 900, Domain: s.Domain, Address: s.Base, Size: 1, Source: &emulatorv1.Unit_Value{Value: []byte{0}}, Delay: 1000}}), "ApplyUnits")
		must(t, c.Subscribe(ctx, 1), "Subscribe(1)")
		defer c.Subscribe(ctx, 0)
		must(t, c.Resume(ctx), "Resume")
		Drain(c)

		must(t, c.Reset(ctx), "Reset")
		st, err := c.Status(ctx)
		must(t, err, "Status")
		if st.GetState() != emulatorv1.Status_RUNNING {
			t.Errorf("state after Reset = %s, want RUNNING", st.GetState())
		}
		if st.GetFrame() >= before {
			t.Errorf("frame after Reset = %d, want it reset (was %d)", st.GetFrame(), before)
		}
		units, err := c.ListUnits(ctx)
		must(t, err, "ListUnits")
		if len(units) != 0 {
			t.Errorf("Reset left %d units", len(units))
		}
		NextEvent(t, c, EventTimeout, "StatusEvent after Reset", func(ev *emulatorv1.Event) bool { return ev.GetStatus() != nil })
		ev := NextEvent(t, c, EventTimeout, "FrameEvent after Reset", isFrame)
		if f := ev.GetFrame().GetFrame(); f >= before {
			t.Errorf("FrameEvent %d after Reset, want < %d", f, before)
		}
		must(t, c.Pause(ctx), "Pause")
	})
}
