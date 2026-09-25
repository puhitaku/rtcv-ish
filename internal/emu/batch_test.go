package emu_test

import (
	"bytes"
	"errors"
	"sync/atomic"
	"testing"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/emu"
	"github.com/puhitaku/rtcv-ish/internal/emu/fake"
)

const bigUnit = 16 << 20

func bigDomain() []fake.Domain {
	return []fake.Domain{{Name: "Big", Size: 100 << 20, WordSize: 4}}
}

// bigUnits returns n VALUE units of 16 MiB at consecutive addresses, each
// filled with its index + 1.
func bigUnits(n int) []*emulatorv1.Unit {
	us := make([]*emulatorv1.Unit, n)
	for i := range us {
		us[i] = &emulatorv1.Unit{
			Id: uint64(i + 1), Domain: "Big", Address: uint64(i) * bigUnit, Size: bigUnit,
			Source: &emulatorv1.Unit_Value{Value: bytes.Repeat([]byte{byte(i + 1)}, bigUnit)},
		}
	}
	return us
}

// countRequests returns a fake hook counting requests of one kind; fail, when
// set, is called with the 1-based count and may modify the request.
func countRequests(n *atomic.Int32, match func(*emulatorv1.Request) bool, fail func(int32, *emulatorv1.Request)) func(*emulatorv1.Request) {
	return func(req *emulatorv1.Request) {
		if !match(req) {
			return
		}
		k := n.Add(1)
		if fail != nil {
			fail(k, req)
		}
	}
}

func isApply(req *emulatorv1.Request) bool { return req.GetApplyUnits() != nil }

func TestApplyUnitsBatches(t *testing.T) {
	var n atomic.Int32
	s, c := loaded(t, fake.Options{Manual: true, Domains: bigDomain(), Hook: countRequests(&n, isApply, nil)})
	if err := c.ApplyUnits(t.Context(), bigUnits(5)); err != nil {
		t.Fatal(err)
	}
	if got := n.Load(); got < 3 {
		t.Errorf("ApplyUnits sent %d requests, want the 80 MiB split into at least 3", got)
	}
	// The listing itself would exceed the wire cap, so check that every unit
	// ran instead.
	s.Tick(1)
	mem := s.Memory("Big")
	for i := range 5 {
		off := i * bigUnit
		if mem[off] != byte(i+1) || mem[off+bigUnit-1] != byte(i+1) {
			t.Errorf("unit %d did not run: %#x..%#x", i+1, mem[off], mem[off+bigUnit-1])
		}
	}
	if err := c.RemoveUnits(t.Context(), []uint64{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	us, err := c.ListUnits(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(us) != 2 || us[0].GetId() != 4 || us[1].GetId() != 5 {
		t.Errorf("ListUnits after removing 1-3 = %d units", len(us))
	}
}

func TestApplyUnitsBatchFailure(t *testing.T) {
	var n atomic.Int32
	breakThird := func(k int32, req *emulatorv1.Request) {
		if k == 3 {
			req.GetApplyUnits().GetUnits()[0].Size = 0
		}
	}
	_, c := loaded(t, fake.Options{Manual: true, Domains: bigDomain(), Hook: countRequests(&n, isApply, breakThird)})
	err := c.ApplyUnits(t.Context(), bigUnits(5))
	var ae *emu.ApplyError
	if !errors.As(err, &ae) || !errors.Is(err, emu.ErrInvalidArgument) {
		t.Fatalf("ApplyUnits = %v, want an *ApplyError wrapping INVALID_ARGUMENT", err)
	}
	us, err := c.ListUnits(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(us) != ae.Applied || ae.Applied == 0 {
		t.Fatalf("Applied = %d, ListUnits has %d units", ae.Applied, len(us))
	}
	for i, u := range us {
		if u.GetId() != uint64(i+1) {
			t.Errorf("unit %d has id %d", i, u.GetId())
		}
	}
}

func TestWriteBatches(t *testing.T) {
	var n atomic.Int32
	isWrite := func(req *emulatorv1.Request) bool { return req.GetWrite() != nil }
	s, c := loaded(t, fake.Options{Manual: true, Domains: bigDomain(), Hook: countRequests(&n, isWrite, nil)})
	data := make([]byte, 40<<20)
	for i := range data {
		data[i] = byte(i * 7)
	}
	small := []byte{1, 2, 3, 4}
	if err := c.Write(t.Context(), []*emulatorv1.WriteChunk{
		{Domain: "Big", Address: 0x10, Data: data},
		{Domain: "Big", Address: 90 << 20, Data: small},
	}); err != nil {
		t.Fatal(err)
	}
	if got := n.Load(); got < 2 {
		t.Errorf("Write sent %d requests, want at least 2", got)
	}
	mem := s.Memory("Big")
	if !bytes.Equal(mem[0x10:0x10+len(data)], data) || !bytes.Equal(mem[90<<20:90<<20+4], small) {
		t.Error("memory does not match what was written")
	}
}
