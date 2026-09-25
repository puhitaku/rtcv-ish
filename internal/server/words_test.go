package server_test

import (
	"bytes"
	"encoding/hex"
	"net/http"
	"sync"
	"testing"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/emu/fake"
	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

// pattern returns n bytes where byte i is (i*7 + i>>8) & 0xff.
func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i>>8)
	}
	return b
}

// sample is the expected result of the words endpoint over mem.
func sample(mem []byte, addr, size, stride int) []byte {
	var out []byte
	for i := addr; i+2 <= addr+size; i += 2 * stride {
		out = append(out, mem[i], mem[i+1])
	}
	return out
}

func (e *env) fill(domain string, data []byte) {
	for off := 0; off < len(data); off += 64 << 10 {
		end := min(off+64<<10, len(data))
		e.writeMem(domain, int64(off), hex.EncodeToString(data[off:end]))
	}
}

func (e *env) words(domain string, addr, size, stride int64) []byte {
	e.t.Helper()
	p := &gen.ReadMemoryWordsParams{Size: size}
	if addr >= 0 {
		p.Address = &addr
	}
	if stride > 0 {
		p.Stride = &stride
	}
	r, err := e.c.ReadMemoryWordsWithResponse(e.ctx, domain, p)
	expectStatus(e.t, r, err, http.StatusOK)
	if ct := r.HTTPResponse.Header.Get("Content-Type"); ct != "application/octet-stream" {
		e.t.Errorf("Content-Type = %q, want application/octet-stream", ct)
	}
	return r.Body
}

func TestMemoryWords(t *testing.T) {
	e := newEnv(t, envOptions{})
	mem := pattern(vramSize)
	e.fill(vram, mem)

	cases := []struct {
		name               string
		addr, size, stride int64
	}{
		{"whole domain, default stride and address", -1, vramSize, 0},
		{"whole domain, stride 1", 0, vramSize, 1},
		{"stride 2", 0, vramSize, 2},
		{"stride 16", 0, vramSize, 16},
		{"offset and partial last step", 0x102, 0x31, 4},
		{"odd address", 0x11, 8, 1},
		{"one word at the end", vramSize - 2, 2, 8},
		{"stride larger than range", 0x40, 6, 1 << 16},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr, stride := max(tc.addr, 0), max(tc.stride, 1)
			want := sample(mem, int(addr), int(tc.size), int(stride))
			got := e.words(vram, tc.addr, tc.size, tc.stride)
			if !bytes.Equal(got, want) {
				t.Errorf("got %d bytes %x..., want %d bytes %x...", len(got), head(got), len(want), head(want))
			}
		})
	}

	errs := []struct {
		name               string
		domain             string
		addr, size, stride int64
		status             int
		code               string
	}{
		{"past end", vram, vramSize - 2, 4, 1, http.StatusBadRequest, "OUT_OF_RANGE"},
		{"address beyond size", vram, vramSize + 2, 2, 1, http.StatusBadRequest, "OUT_OF_RANGE"},
		{"size too small", vram, 0, 1, 1, http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"zero stride", vram, 0, 16, 0, http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"stride too large", vram, 0, 16, 1<<16 + 1, http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"negative address", vram, -2, 2, 1, http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"unknown domain", "Nope", 0, 2, 1, http.StatusNotFound, "NOT_FOUND"},
	}
	for _, tc := range errs {
		t.Run("error "+tc.name, func(t *testing.T) {
			r, err := e.c.ReadMemoryWordsWithResponse(e.ctx, tc.domain, &gen.ReadMemoryWordsParams{
				Address: &tc.addr, Size: tc.size, Stride: &tc.stride,
			})
			expectError(t, r, err, tc.status, tc.code)
		})
	}

	t.Run("no ROM", func(t *testing.T) {
		e := newEnv(t, envOptions{noROM: true})
		r, err := e.c.ReadMemoryWordsWithResponse(e.ctx, vram, &gen.ReadMemoryWordsParams{Size: 2})
		if err != nil || r.StatusCode() < 400 {
			t.Fatalf("status %d, err %v; want an error without a ROM", r.StatusCode(), err)
		}
	})
}

// Large domains are read in batches of at most 1 MiB and have no 64 KiB cap.
func TestMemoryWordsBatches(t *testing.T) {
	const big = 3<<20 + 6
	var mu sync.Mutex
	var reads []uint32
	e := newEnv(t, envOptions{fake: fake.Options{
		Domains: []fake.Domain{{Name: "Big", Size: big, WordSize: 2}},
		Hook: func(r *emulatorv1.Request) {
			if rd := r.GetRead(); rd != nil {
				mu.Lock()
				for _, rg := range rd.GetRanges() {
					reads = append(reads, rg.GetSize())
				}
				mu.Unlock()
			}
		},
	}})
	mem := pattern(big)
	e.fill("Big", mem)

	for _, stride := range []int64{1, 2, 4, 16} {
		mu.Lock()
		reads = nil
		mu.Unlock()
		got := e.words("Big", 0, big, stride)
		if want := sample(mem, 0, big, int(stride)); !bytes.Equal(got, want) {
			t.Errorf("stride %d: got %d bytes, want %d", stride, len(got), len(want))
		}
		mu.Lock()
		if len(reads) < 4 {
			t.Errorf("stride %d: %d emulator reads, want at least 4 for %d bytes", stride, len(reads), big)
		}
		for _, n := range reads {
			if n > 1<<20 {
				t.Errorf("stride %d: emulator read of %d bytes exceeds 1 MiB", stride, n)
			}
		}
		mu.Unlock()
	}
}

func head(b []byte) []byte { return b[:min(len(b), 16)] }
