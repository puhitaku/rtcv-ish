package corrupt

import (
	"context"
	"fmt"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
)

// Rasterize turns the enabled units of a layer into emulator units:
//
//   - VALUE: a value unit with tilt and endianness applied.
//   - STORE + IMMEDIATE + ONCE: the source is read now (tilt added with the
//     unit's endianness) and sent as a value unit. A looping unit therefore
//     replays this sample instead of re-sampling on every loop.
//   - STORE + PREEXECUTE + ONCE: store{continuous: false}.
//   - STORE + CONTINUOUS: store{continuous: true}. With IMMEDIATE, RTCV
//     starts queueing samples at apply time and replays them ExecuteFrame
//     frames late; the emulator samples live instead.
//
// ExecuteFrame, Lifetime and Loop map to delay, lifetime and loop.
// loop_delay is LoopTiming when set, else ExecuteFrame, like RTCV's
// re-apply. Tilt on sizes other than 1, 2, 4 and 8 bytes is dropped.
// Units whose domain is unknown or whose range leaves the domain are
// skipped, like RTCV ignores out-of-range pokes. Invalid units are an
// error.
//
// Units with lifetime 0 get mode infinite, store units at most SCANLINE
// (HARD is value-only); every other unit is FRAME.
func Rasterize(ctx context.Context, layer *Layer, mem Memory, infinite emulatorv1.Mode, nextID func() uint64) ([]*emulatorv1.Unit, error) {
	domains := make(map[string]Domain)
	for _, d := range mem.Domains() {
		domains[d.Name] = d
	}
	fits := func(name string, addr uint64, size int) bool {
		d, ok := domains[name]
		return ok && d.contains(addr, size)
	}
	var out []*emulatorv1.Unit
	var reads []Range
	var readers []int
	var readUnits []*Unit
	for i, u := range layer.Units {
		if !u.Enabled {
			continue
		}
		if err := u.Validate(); err != nil {
			return nil, fmt.Errorf("unit %d: %w", i, err)
		}
		if !fits(u.Domain, u.Address, u.Precision) {
			continue
		}
		eu := &emulatorv1.Unit{
			Domain:   u.Domain,
			Address:  u.Address,
			Size:     uint32(u.Precision),
			Delay:    uint32(u.ExecuteFrame),
			Lifetime: uint32(u.Lifetime),
			Loop:     u.Loop,
		}
		if u.Loop {
			eu.LoopDelay = uint32(u.ExecuteFrame)
			if u.LoopTiming >= 0 {
				eu.LoopDelay = uint32(u.LoopTiming)
			}
		}
		switch {
		case u.Source == SourceValue:
			eu.Source = &emulatorv1.Unit_Value{Value: u.writtenValue()}
		case !fits(u.SourceDomain, u.SourceAddress, u.Precision):
			continue
		case u.StoreTime == StoreImmediate && u.StoreType == StoreOnce:
			reads = append(reads, Range{Domain: u.SourceDomain, Address: u.SourceAddress, Size: u.Precision})
			readers = append(readers, len(out))
			readUnits = append(readUnits, u)
		default:
			eu.Source = &emulatorv1.Unit_Store{Store: &emulatorv1.StoreSource{
				Domain:     u.SourceDomain,
				Address:    u.SourceAddress,
				Continuous: u.StoreType == StoreContinuous,
			}}
			if isWordSize(u.Precision) {
				eu.Tilt = int64(tiltDelta(u.Tilt, u.Precision))
			}
		}
		out = append(out, eu)
	}
	if len(reads) > 0 {
		data, err := mem.ReadMany(ctx, reads)
		if err != nil {
			return nil, err
		}
		for j, i := range readers {
			u := readUnits[j]
			out[i].Source = &emulatorv1.Unit_Value{Value: addTilt(data[j], u.Tilt, u.BigEndian)}
		}
	}
	for _, eu := range out {
		eu.Id = nextID()
		if eu.GetLifetime() == 0 {
			eu.Mode = infinite
			if eu.GetStore() != nil {
				eu.Mode = min(eu.Mode, emulatorv1.Mode_SCANLINE)
			}
		}
	}
	return out, nil
}

// InfiniteMode is the unit mode for infinite units under a freeze mode
// setting, falling back from HARD to SCANLINE to FRAME when the emulator
// lacks a mode. fellBack reports that the setting could not be honoured.
func InfiniteMode(want FreezeMode, scanline, hard bool) (mode emulatorv1.Mode, fellBack bool) {
	switch want {
	case FreezeHard:
		mode = emulatorv1.Mode_HARD
	case FreezeScanline:
		mode = emulatorv1.Mode_SCANLINE
	}
	if mode == emulatorv1.Mode_HARD && !hard {
		mode, fellBack = emulatorv1.Mode_SCANLINE, true
	}
	if mode == emulatorv1.Mode_SCANLINE && !scanline {
		mode, fellBack = emulatorv1.Mode_FRAME, true
	}
	return mode, fellBack
}

// ModeName is the frontend API name of a unit mode.
func ModeName(m emulatorv1.Mode) FreezeMode {
	switch m {
	case emulatorv1.Mode_SCANLINE:
		return FreezeScanline
	case emulatorv1.Mode_HARD:
		return FreezeHard
	}
	return FreezeFrame
}
