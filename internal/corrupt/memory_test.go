package corrupt

import (
	"bytes"
	"context"
	"errors"
	"testing"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/emu"
	"github.com/puhitaku/rtcv-ish/internal/emu/fake"
)

func TestSnapshot(t *testing.T) {
	const big = 2*MaxReadRequest + 100
	mem := newFakeMem(dom("Big", big), dom("Small", 16), dom("Other", 8))
	for i := range mem.data["Big"] {
		mem.data["Big"][i] = byte(i * 7)
	}
	mem.put("Small", 0, 1, 2, 3)
	s := NewSnapshot(mem)
	if err := s.Load(t.Context(), "Big", "Small"); err != nil {
		t.Fatal(err)
	}
	if len(mem.calls) != 3 {
		t.Fatalf("%d requests, want 3", len(mem.calls))
	}
	for i, c := range mem.calls {
		total := 0
		for _, r := range c {
			total += r.Size
		}
		if total > MaxReadRequest {
			t.Errorf("request %d reads %d bytes", i, total)
		}
	}
	calls := len(mem.calls)
	got, err := s.ReadMany(t.Context(), []Range{{"Big", MaxReadRequest - 2, 4}, {"Small", 1, 2}, {"Big", big - 1, 1}})
	if err != nil {
		t.Fatal(err)
	}
	b := mem.data["Big"]
	want := [][]byte{b[MaxReadRequest-2 : MaxReadRequest+2], {2, 3}, b[big-1:]}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Errorf("range %d = %X, want %X", i, got[i], want[i])
		}
	}
	if len(mem.calls) != calls {
		t.Error("cached read hit the underlying memory")
	}
	got[1][0] = 99
	if again, _ := s.Read(t.Context(), "Small", 1, 1); again[0] != 2 {
		t.Error("snapshot data aliased")
	}
	if _, err := s.Read(t.Context(), "Other", 4, 2); err != nil || len(mem.calls) != calls+1 {
		t.Errorf("lazy load: err %v, calls %d", err, len(mem.calls))
	}
	if _, err := s.Read(t.Context(), "Small", 15, 2); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("out of range: %v", err)
	}
	if _, err := s.Read(t.Context(), "X", 0, 1); !errors.Is(err, ErrUnknownDomain) {
		t.Errorf("unknown domain: %v", err)
	}
}

func TestReadBatched(t *testing.T) {
	var batches [][]Range
	ranges := []Range{{"A", 0, 5}, {"A", 10, 2}, {"B", 0, 0}, {"A", 20, 7}}
	out, err := readBatched(t.Context(), ranges, 4, func(_ context.Context, rs []Range) ([][]byte, error) {
		batches = append(batches, append([]Range(nil), rs...))
		data := make([][]byte, len(rs))
		for i, r := range rs {
			for j := range r.Size {
				data[i] = append(data[i], byte(r.Address)+byte(j))
			}
		}
		return data, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{{0, 1, 2, 3, 4}, {10, 11}, {}, {20, 21, 22, 23, 24, 25, 26}}
	for i := range want {
		if !bytes.Equal(out[i], want[i]) {
			t.Errorf("range %d = %v", i, out[i])
		}
	}
	for _, b := range batches {
		total := 0
		for _, r := range b {
			total += r.Size
		}
		if total > 4 {
			t.Errorf("batch %v exceeds limit", b)
		}
	}
	if len(batches) != 4 {
		t.Errorf("%d batches, want 4", len(batches))
	}
}

func TestEmuMemory(t *testing.T) {
	srv, err := fake.New(fake.Options{Manual: true, ROM: "/roms/x.nds"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	c, err := emu.Dial(t.Context(), srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if err := c.WriteOne(t.Context(), "VRAM", 0x100, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	mem, err := LoadEmuMemory(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	if ds := mem.Domains(); len(ds) != 3 || ds[0].Name != "MainRAM" || ds[0].Size != 64<<10 || !ds[2].Hidden || !ds[0].Writable {
		t.Fatalf("domains %+v", ds)
	}
	got, err := mem.Read(t.Context(), "VRAM", 0x100, 3)
	if err != nil || !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("Read = %v, %v", got, err)
	}
	snap := NewSnapshot(mem)
	all, err := snap.ReadMany(t.Context(), []Range{{"VRAM", 0x101, 2}, {"MainRAM", 0, 4}})
	if err != nil || !bytes.Equal(all[0], []byte{2, 3}) {
		t.Fatalf("snapshot = %v, %v", all, err)
	}

	l := &Layer{Units: []*Unit{newStoreUnit(StoreOnce, StoreImmediate, "MainRAM", 0x10, "VRAM", 0x100, 3, false, 0, 1)}}
	units, err := Rasterize(t.Context(), l, mem, emulatorv1.Mode_FRAME, func() uint64 { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyUnits(t.Context(), units); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Step(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.ReadOne(t.Context(), "MainRAM", 0x10, 3); !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Errorf("applied unit wrote %v", got)
	}
}
