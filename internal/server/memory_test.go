package server_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

func selectedNames(ds []gen.Domain) []string {
	var out []string
	for _, d := range ds {
		if d.Selected {
			out = append(out, d.Name)
		}
	}
	return out
}

func TestDomains(t *testing.T) {
	e := newEnv(t, envOptions{})

	// Loading a ROM auto-selects the non-hidden domains.
	want := []gen.Domain{
		{Name: mainRAM, Size: 64 << 10, WordSize: 4, Writable: true, Selected: true},
		{Name: vram, Size: vramSize, WordSize: 2, Writable: true, Selected: true},
		{Name: arm7WRAM, Size: 8 << 10, WordSize: 4, Writable: true, Hidden: true, Selected: false},
	}
	if got := e.domains(); !slices.Equal(got, want) {
		t.Fatalf("domains = %+v\nwant %+v", got, want)
	}

	r, err := e.c.SetSelectedDomainsWithResponse(e.ctx, gen.NamesRequest{Names: []string{arm7WRAM, vram}})
	expectStatus(t, r, err, http.StatusOK)
	if got := selectedNames(*r.JSON200); !slices.Equal(got, []string{vram, arm7WRAM}) {
		t.Errorf("PUT selected: selected = %v, want [VRAM ARM7WRAM] (domain order)", got)
	}
	if got := selectedNames(e.domains()); !slices.Equal(got, []string{vram, arm7WRAM}) {
		t.Errorf("GET after PUT: selected = %v", got)
	}

	t.Run("unknown domain", func(t *testing.T) {
		r, err := e.c.SetSelectedDomainsWithResponse(e.ctx, gen.NamesRequest{Names: []string{"Nope"}})
		expectError(t, r, err, http.StatusBadRequest, "INVALID_ARGUMENT")
		if got := selectedNames(e.domains()); !slices.Equal(got, []string{vram, arm7WRAM}) {
			t.Errorf("selection changed by a rejected PUT: %v", got)
		}
	})

	t.Run("none", func(t *testing.T) {
		r, err := e.c.SetSelectedDomainsWithResponse(e.ctx, gen.NamesRequest{Names: []string{}})
		expectStatus(t, r, err, http.StatusOK)
		if got := selectedNames(*r.JSON200); len(got) != 0 {
			t.Errorf("selected = %v, want none", got)
		}
	})

	ra, err := e.c.AutoSelectDomainsWithResponse(e.ctx)
	expectStatus(t, ra, err, http.StatusOK)
	if got := selectedNames(*ra.JSON200); !slices.Equal(got, []string{mainRAM, vram}) {
		t.Errorf("auto-select: selected = %v, want [MainRAM VRAM]", got)
	}
}

func TestMemory(t *testing.T) {
	e := newEnv(t, envOptions{})

	if got := e.readMem(vram, 0x10, 4); got != "00000000" {
		t.Errorf("fresh VRAM = %q, want zeros", got)
	}
	e.writeMem(vram, 0x10, "DEADbeef")
	if got := e.readMem(vram, 0x10, 4); got != "deadbeef" {
		t.Errorf("read after write = %q, want deadbeef (lowercase)", got)
	}
	if got := e.fake.Memory(vram)[0x10:0x14]; string(got) != "\xde\xad\xbe\xef" {
		t.Errorf("fake VRAM = %x", got)
	}
	r, err := e.c.ReadMemoryWithResponse(e.ctx, vram, &gen.ReadMemoryParams{Address: 0x12, Size: 2})
	expectStatus(t, r, err, http.StatusOK)
	if want := (gen.MemoryChunk{Domain: vram, Address: 0x12, Data: "beef"}); *r.JSON200 != want {
		t.Errorf("read = %+v, want %+v", *r.JSON200, want)
	}

	// The largest read is 64 KiB.
	if got := e.readMem(mainRAM, 0, 64<<10); len(got) != 2*(64<<10) {
		t.Errorf("64 KiB read returned %d hex chars", len(got))
	}
	// The last byte of a domain is readable and writable.
	e.writeMem(vram, vramSize-1, "7f")
	if got := e.readMem(vram, vramSize-1, 1); got != "7f" {
		t.Errorf("last byte = %q", got)
	}

	reads := []struct {
		name          string
		domain        string
		address, size int64
		status        int
		code          string
	}{
		{"past end", vram, vramSize - 2, 4, http.StatusBadRequest, "OUT_OF_RANGE"},
		{"address beyond size", vram, vramSize + 1, 1, http.StatusBadRequest, "OUT_OF_RANGE"},
		{"too large", mainRAM, 0, 64<<10 + 1, http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"zero size", vram, 0, 0, http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"negative address", vram, -1, 1, http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"unknown domain", "Nope", 0, 1, http.StatusNotFound, "NOT_FOUND"},
	}
	for _, tc := range reads {
		t.Run("read "+tc.name, func(t *testing.T) {
			r, err := e.c.ReadMemoryWithResponse(e.ctx, tc.domain, &gen.ReadMemoryParams{Address: tc.address, Size: tc.size})
			expectError(t, r, err, tc.status, tc.code)
		})
	}

	writes := []struct {
		name    string
		domain  string
		address int64
		data    string
		status  int
		code    string
	}{
		{"past end", vram, vramSize - 1, "0102", http.StatusBadRequest, "OUT_OF_RANGE"},
		{"odd hex", vram, 0, "123", http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"not hex", vram, 0, "zz", http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"empty", vram, 0, "", http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"too large", mainRAM, 0, strings.Repeat("00", 64<<10+1), http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"unknown domain", "Nope", 0, "00", http.StatusNotFound, "NOT_FOUND"},
	}
	for _, tc := range writes {
		t.Run("write "+tc.name, func(t *testing.T) {
			r, err := e.c.WriteMemoryWithResponse(e.ctx, tc.domain, gen.MemoryWrite{Address: tc.address, Data: tc.data})
			expectError(t, r, err, tc.status, tc.code)
		})
	}
	if got := e.readMem(vram, vramSize-1, 1); got != "7f" {
		t.Errorf("a rejected write changed memory: %q", got)
	}
}
