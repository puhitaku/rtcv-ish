package fake_test

import (
	"testing"

	"github.com/puhitaku/rtcv-ish/internal/emu"
	"github.com/puhitaku/rtcv-ish/internal/emu/fake"
	"github.com/puhitaku/rtcv-ish/internal/emutest"
)

// The fake's game stores the frame counter at MainRAM+0 every frame.
func TestUnitModes(t *testing.T) {
	s, err := fake.New(fake.Options{FrameRate: 1000})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	c, err := emu.Dial(t.Context(), s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if _, err := c.LoadRom(t.Context(), "/roms/hello_world.nds"); err != nil {
		t.Fatal(err)
	}
	counter := emutest.FindCounter(t, c, "MainRAM")
	if counter.Base != 0 {
		t.Fatalf("counter found at %s, want MainRAM+0x0", counter)
	}
	emutest.RunModes(t, c, counter, false)
}
