package session

import (
	"bytes"
	"errors"
	"sync/atomic"
	"testing"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/emu"
	"github.com/puhitaku/rtcv-ish/internal/emu/fake"
)

// A failed ApplyUnits batch after earlier ones succeeded must leave no
// unit of the call scheduled.
func TestScheduleBatchFailureIsAllOrNothing(t *testing.T) {
	const size = 16 << 20
	var applies atomic.Int32
	f, err := fake.New(fake.Options{
		Manual:  true,
		Domains: []fake.Domain{{Name: "Big", Size: 100 << 20, WordSize: 4}},
		Hook: func(req *emulatorv1.Request) {
			if req.GetApplyUnits() != nil && applies.Add(1) == 3 {
				req.GetApplyUnits().GetUnits()[0].Size = 0
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	s, err := New(t.Context(), Config{DataDir: t.TempDir(), Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err := s.Connect(t.Context(), f.Addr()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadRom(t.Context(), "/roms/hello.nds"); err != nil {
		t.Fatal(err)
	}
	cn, err := s.current()
	if err != nil {
		t.Fatal(err)
	}

	units := make([]*emulatorv1.Unit, 5)
	for i := range units {
		units[i] = &emulatorv1.Unit{
			Id: s.nextID(), Domain: "Big", Address: uint64(i) * size, Size: size,
			Source: &emulatorv1.Unit_Value{Value: bytes.Repeat([]byte{0xAA}, size)},
		}
	}
	err = s.schedule(t.Context(), cn, units)
	if !errors.Is(err, emu.ErrInvalidArgument) {
		t.Fatalf("schedule = %v, want INVALID_ARGUMENT", err)
	}
	var ae *emu.ApplyError
	if errors.As(err, &ae) {
		t.Errorf("schedule leaked the partial-apply error %v", err)
	}
	if n := applies.Load(); n != 3 {
		t.Errorf("sent %d ApplyUnits batches, want 3", n)
	}
	us, err := cn.client.ListUnits(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(us) != 0 {
		t.Errorf("%d units stayed scheduled after the failed apply", len(us))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.infinite) != 0 {
		t.Errorf("infinite = %v, want none", s.infinite)
	}
}
