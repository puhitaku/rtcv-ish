package emutest

import (
	"bytes"
	"testing"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/emu"
)

// RunMemory tests reads and writes. The first scratch area is used for
// single-domain tests; all of them for a multi-chunk write. A ROM must be
// loaded.
func RunMemory(t *testing.T, c *emu.Client, areas ...Scratch) {
	main := areas[0]

	t.Run("roundtrip", func(t *testing.T) {
		ctx := ctx(t)
		_, err := c.Step(ctx, 1)
		must(t, err, "Step")
		want := []byte{0xde, 0xad, 0xbe, 0xef, 0x01, 0x02, 0x03}
		must(t, c.WriteOne(ctx, main.Domain, main.Base+1, want), "Write")
		got, err := c.ReadOne(ctx, main.Domain, main.Base+1, uint32(len(want)))
		must(t, err, "Read")
		if !bytes.Equal(got, want) {
			t.Errorf("read %s = % x, want % x", main, got, want)
		}
	})

	t.Run("multi-chunk", func(t *testing.T) {
		ctx := ctx(t)
		_, err := c.Step(ctx, 1)
		must(t, err, "Step")
		var chunks []*emulatorv1.WriteChunk
		var ranges []*emulatorv1.Range
		for i, a := range areas {
			data := []byte{byte(0x10 + i), byte(0x20 + i), byte(0x30 + i), byte(0x40 + i)}
			chunks = append(chunks, &emulatorv1.WriteChunk{Domain: a.Domain, Address: a.Base + 0x10, Data: data})
			ranges = append(ranges, &emulatorv1.Range{Domain: a.Domain, Address: a.Base + 0x10, Size: 4})
		}
		must(t, c.Write(ctx, chunks), "Write")
		got, err := c.Read(ctx, ranges)
		must(t, err, "Read")
		for i, ch := range chunks {
			if !bytes.Equal(got[i], ch.Data) {
				t.Errorf("%s: read % x, want % x", areas[i], got[i], ch.Data)
			}
		}
	})

	t.Run("later chunks win", func(t *testing.T) {
		ctx := ctx(t)
		must(t, c.Write(ctx, []*emulatorv1.WriteChunk{
			{Domain: main.Domain, Address: main.Base + 0x20, Data: []byte{1, 1, 1, 1}},
			{Domain: main.Domain, Address: main.Base + 0x22, Data: []byte{2, 2}},
		}), "Write")
		got, err := c.ReadOne(ctx, main.Domain, main.Base+0x20, 4)
		must(t, err, "Read")
		if want := []byte{1, 1, 2, 2}; !bytes.Equal(got, want) {
			t.Errorf("read % x, want % x", got, want)
		}
	})

	t.Run("errors", func(t *testing.T) {
		ctx := ctx(t)
		domains, err := c.ListDomains(ctx)
		must(t, err, "ListDomains")
		var size uint64
		for _, d := range domains {
			if d.GetName() == main.Domain {
				size = d.GetSize()
			}
		}
		if size == 0 {
			t.Fatalf("domain %s not listed", main.Domain)
		}
		_, err = c.ReadOne(ctx, "NoSuchDomain", 0, 4)
		WantCode(t, err, emulatorv1.Error_NOT_FOUND, "read unknown domain")
		_, err = c.ReadOne(ctx, main.Domain, size-2, 4)
		WantCode(t, err, emulatorv1.Error_OUT_OF_RANGE, "read across the end")
		_, err = c.ReadOne(ctx, main.Domain, size, 1)
		WantCode(t, err, emulatorv1.Error_OUT_OF_RANGE, "read past the end")
		err = c.WriteOne(ctx, "NoSuchDomain", 0, []byte{1})
		WantCode(t, err, emulatorv1.Error_NOT_FOUND, "write unknown domain")
		err = c.WriteOne(ctx, main.Domain, size-1, []byte{1, 2})
		WantCode(t, err, emulatorv1.Error_OUT_OF_RANGE, "write across the end")
		_, err = c.ReadOne(ctx, main.Domain, size-4, 4)
		must(t, err, "read the last word")
	})
}

// RunSaveState tests a SaveState/LoadState roundtrip. A ROM must be loaded.
func RunSaveState(t *testing.T, c *emu.Client, s Scratch) {
	ctx := ctx(t)
	_, err := c.Step(ctx, 1)
	must(t, err, "Step")
	must(t, c.WriteOne(ctx, s.Domain, s.Base, le32(0x11223344)), "Write")
	state, err := c.SaveState(ctx)
	must(t, err, "SaveState")
	if len(state) == 0 {
		t.Fatal("SaveState returned no data")
	}
	must(t, c.WriteOne(ctx, s.Domain, s.Base, le32(0x55667788)), "Write")
	before, err := c.Step(ctx, 3)
	must(t, err, "Step")

	must(t, c.LoadState(ctx, state), "LoadState")
	got, err := c.ReadOne(ctx, s.Domain, s.Base, 4)
	must(t, err, "Read")
	if !bytes.Equal(got, le32(0x11223344)) {
		t.Errorf("after LoadState %s = % x, want % x", s, got, le32(0x11223344))
	}
	st, err := c.Status(ctx)
	must(t, err, "Status")
	if st.GetFrame() < before {
		t.Errorf("frame counter went back from %d to %d after LoadState", before, st.GetFrame())
	}

	err = c.LoadState(ctx, []byte("not a savestate"))
	WantCode(t, err, emulatorv1.Error_INVALID_ARGUMENT, "LoadState(garbage)")
	_, err = c.Step(ctx, 1)
	must(t, err, "Step after a rejected LoadState")
}
