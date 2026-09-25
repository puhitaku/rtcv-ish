package e2e

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// Addresses in the MainRAM domain written by the counter ROM once per
// frame, at the start of VBlank.
const (
	// CounterCPU is incremented by an ARM9 ldr/add/str.
	CounterCPU = 0x100000
	// CounterDMA receives a copy of CounterCPU through DMA3 right after.
	CounterDMA = 0x100004
	// ObservedCPU is what the ARM9 reads back from CounterCPU right after
	// its store, ObservedDMA what it reads from CounterDMA right after the
	// DMA; both well before the next scanline starts.
	ObservedCPU = 0x100008
	ObservedDMA = 0x10000C
)

// counterARM9 is the ARM9 program of the counter ROM, loaded and started at
// 0x02000000:
//
//	    ldr  r0, =0x04000004    @ DISPSTAT
//	    ldr  r1, =0x02100000    @ CounterCPU
//	1:  ldrh r2, [r0]           @ wait for VBlank to end
//	    tst  r2, #1
//	    bne  1b
//	2:  ldrh r2, [r0]           @ wait for VBlank to start
//	    tst  r2, #1
//	    beq  2b
//	    ldr  r3, [r1]
//	    add  r3, r3, #1
//	    str  r3, [r1]
//	    ldr  r3, [r1]
//	    str  r3, [r1, #8]       @ ObservedCPU
//	    ldr  r4, =0x040000D4    @ DMA3: copy CounterCPU to CounterDMA
//	    str  r1, [r4]
//	    add  r5, r1, #4
//	    str  r5, [r4, #4]
//	    ldr  r5, =0x84000001    @ enable, immediate, 32-bit, 1 word
//	    str  r5, [r4, #8]
//	3:  ldr  r5, [r4, #8]       @ wait for the DMA to finish
//	    tst  r5, #0x80000000
//	    bne  3b
//	    ldr  r3, [r1, #4]
//	    str  r3, [r1, #12]      @ ObservedDMA
//	    b    1b
var counterARM9 = []uint32{
	0xE59F005C, 0xE59F105C,
	0xE1D020B0, 0xE3120001, 0x1AFFFFFC,
	0xE1D020B0, 0xE3120001, 0x0AFFFFFC,
	0xE5913000, 0xE2833001, 0xE5813000,
	0xE5913000, 0xE5813008,
	0xE59F4030, 0xE5841000, 0xE2815004, 0xE5845004, 0xE59F5024, 0xE5845008,
	0xE5945008, 0xE3150102, 0x1AFFFFFC,
	0xE5913004, 0xE581300C,
	0xEAFFFFE8,
	0x04000004, 0x02000000 + CounterCPU, 0x040000D4, 0x84000001,
}

// counterARM7 idles: b .
var counterARM7 = []uint32{0xEAFFFFFE}

// CounterROM writes a minimal homebrew ROM without libnds that updates
// the Counter and Observed words every frame, and returns its path. Unlike the
// nds-examples ROMs it also runs under the melonDS JIT.
func CounterROM(t testing.TB) string {
	t.Helper()
	const (
		arm9Off = 0x200
		arm7Off = 0x400
		size    = 0x1000
	)
	rom := make([]byte, size)
	copy(rom[0x00:], "RTCVISHCOUNT")
	copy(rom[0x0C:], "####")
	copy(rom[0x10:], "00")
	le := binary.LittleEndian
	put := func(off int, words []uint32) {
		for i, w := range words {
			le.PutUint32(rom[off+4*i:], w)
		}
	}
	// ARM9/ARM7: ROM offset, entry, RAM address, size.
	put(0x20, []uint32{arm9Off, 0x02000000, 0x02000000, uint32(4 * len(counterARM9))})
	put(0x30, []uint32{arm7Off, 0x02380000, 0x02380000, uint32(4 * len(counterARM7))})
	put(0x80, []uint32{size, 0x4000})
	put(arm9Off, counterARM9)
	put(arm7Off, counterARM7)

	p := filepath.Join(t.TempDir(), "counter.nds")
	if err := os.WriteFile(p, rom, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
