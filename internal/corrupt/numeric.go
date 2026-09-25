package corrupt

import (
	"math"
	"math/big"
	"math/rand/v2"
	"slices"
)

func isWordSize(n int) bool { return n == 1 || n == 2 || n == 4 || n == 8 }

func maxForSize(n int) uint64 {
	if n >= 8 {
		return math.MaxUint64
	}
	return 1<<(8*n) - 1
}

func getUint(b []byte, bigEndian bool) uint64 {
	var v uint64
	for i := range b {
		j := i
		if !bigEndian {
			j = len(b) - 1 - i
		}
		v = v<<8 | uint64(b[j])
	}
	return v
}

func putUint(b []byte, v uint64, bigEndian bool) {
	for i := range b {
		j := i
		if bigEndian {
			j = len(b) - 1 - i
		}
		b[j] = byte(v)
		v >>= 8
	}
}

func encodeLE(v uint64, n int) []byte {
	b := make([]byte, n)
	putUint(b, v, false)
	return b
}

// tiltDelta returns the two's-complement delta RTCV adds for tilt on an
// n-byte integer: |tilt| is clamped to the largest n-byte value
// (AddValueToByteArrayUnchecked), then added or subtracted with wrap-around.
func tiltDelta(tilt *big.Int, n int) uint64 {
	if tilt == nil || tilt.Sign() == 0 {
		return 0
	}
	abs := new(big.Int).Abs(tilt)
	d := maxForSize(n)
	if abs.IsUint64() && abs.Uint64() < d {
		d = abs.Uint64()
	}
	if tilt.Sign() < 0 {
		d = -d
	}
	return d
}

// addTilt returns b with tilt added, interpreting b with the given
// endianness. Only 1, 2, 4 and 8-byte values are tilted; others are
// returned unchanged.
func addTilt(b []byte, tilt *big.Int, bigEndian bool) []byte {
	out := slices.Clone(b)
	if !isWordSize(len(b)) || tilt == nil || tilt.Sign() == 0 {
		return out
	}
	putUint(out, getUint(out, bigEndian)+tiltDelta(tilt, len(b)), bigEndian)
	return out
}

// uint64Range draws uniformly from [lo, hi] (inclusive) without bias.
func uint64Range(rng *rand.Rand, lo, hi uint64) uint64 {
	if hi < lo {
		lo, hi = hi, lo
	}
	if lo == 0 && hi == math.MaxUint64 {
		return rng.Uint64()
	}
	return lo + rng.Uint64N(hi-lo+1)
}

func randomBytes(rng *rand.Rand, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Uint32())
	}
	return b
}

// randomAddress draws from [0, n) like RTCV's NextLong(0, n); n <= 0
// yields 0.
func randomAddress(rng *rand.Rand, n int64) int64 {
	if n <= 0 {
		return 0
	}
	return rng.Int64N(n)
}
