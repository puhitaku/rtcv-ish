package server_test

import (
	"testing"

	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

func (e *env) domain(name string) gen.Domain {
	e.t.Helper()
	for _, d := range e.domains() {
		if d.Name == name {
			return d
		}
	}
	e.t.Fatalf("no domain %q", name)
	panic("unreachable")
}

// Emulators resize domains when they re-create the console (melonDS: DS vs
// DSi MainRAM) without changing the game. Generation must use the new size
// and the selection must survive.
func TestDomainResize(t *testing.T) {
	const intensity = 50
	e := newEnv(t, envOptions{})
	e.useNightmare(intensity)
	e.selectDomains(mainRAM)
	events := e.openEvents()

	blastBelow := func(size int64) gen.Layer {
		t.Helper()
		l := e.blast()
		if len(l.Units) != intensity {
			t.Fatalf("blast returned %d units, want %d", len(l.Units), intensity)
		}
		for i, u := range l.Units {
			if u.Domain != mainRAM || u.Address < 0 || u.Address >= size {
				t.Errorf("unit %d targets %s 0x%x, want %s below 0x%x", i, u.Domain, u.Address, mainRAM, size)
			}
		}
		return l
	}
	checkDomain := func(size int64) {
		t.Helper()
		d := e.domain(mainRAM)
		if d.Size != size || !d.Selected {
			t.Errorf("%s = %+v, want size 0x%x, selected", mainRAM, d, size)
		}
		if e.domain(vram).Selected {
			t.Errorf("%s became selected", vram)
		}
	}

	// Shrink and blast right away: the blast must not use the cached size.
	if err := e.fake.ResizeDomain(mainRAM, 4<<10); err != nil {
		t.Fatal(err)
	}
	blastBelow(4 << 10)
	checkDomain(4 << 10)
	events.waitFor(t, "domains", nil)

	// Grow and let the status event alone refresh the list.
	if err := e.fake.ResizeDomain(mainRAM, 1<<20); err != nil {
		t.Fatal(err)
	}
	events.waitFor(t, "domains", nil)
	checkDomain(1 << 20)
	l := blastBelow(1 << 20)
	high := false
	for _, u := range l.Units {
		high = high || u.Address >= 64<<10
	}
	if !high {
		t.Errorf("no unit above the old 64 KiB size in %d units", len(l.Units))
	}

	// Several blasts in a row keep working after the shrink.
	if err := e.fake.ResizeDomain(mainRAM, 4<<10); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		blastBelow(4 << 10)
	}
	checkDomain(4 << 10)
}
