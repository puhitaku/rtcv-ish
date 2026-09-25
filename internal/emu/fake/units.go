package fake

import (
	"slices"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
)

type unit struct {
	spec     *emulatorv1.Unit
	wait     uint32
	running  bool
	executed uint32
	sample   []byte
}

// applyUnits validates all units before scheduling any. s.mu must be held.
func (s *Server) applyUnits(specs []*emulatorv1.Unit) *emulatorv1.Error {
	ids := make(map[uint64]bool, len(s.units)+len(specs))
	for _, u := range s.units {
		ids[u.spec.GetId()] = true
	}
	for _, u := range specs {
		if ids[u.GetId()] {
			return errorf(emulatorv1.Error_INVALID_ARGUMENT, "duplicate unit id %d", u.GetId())
		}
		ids[u.GetId()] = true
		if u.GetSize() == 0 {
			return errorf(emulatorv1.Error_INVALID_ARGUMENT, "unit %d: size is 0", u.GetId())
		}
		if _, _, e := s.checkRange(u.GetDomain(), u.GetAddress(), uint64(u.GetSize()), true); e != nil {
			return e
		}
		switch src := u.GetSource().(type) {
		case *emulatorv1.Unit_Value:
			if len(src.Value) != int(u.GetSize()) {
				return errorf(emulatorv1.Error_INVALID_ARGUMENT, "unit %d: value has %d bytes, size is %d", u.GetId(), len(src.Value), u.GetSize())
			}
		case *emulatorv1.Unit_Store:
			if _, _, e := s.checkRange(src.Store.GetDomain(), src.Store.GetAddress(), uint64(u.GetSize()), false); e != nil {
				return e
			}
			if u.GetTilt() != 0 && !slices.Contains([]uint32{1, 2, 4, 8}, u.GetSize()) {
				return errorf(emulatorv1.Error_INVALID_ARGUMENT, "unit %d: tilt needs size 1, 2, 4 or 8", u.GetId())
			}
		default:
			return errorf(emulatorv1.Error_INVALID_ARGUMENT, "unit %d: no source", u.GetId())
		}
	}
	for _, spec := range specs {
		s.units = append(s.units, &unit{spec: spec, wait: spec.GetDelay()})
	}
	return nil
}

func (s *Server) removeUnits(ids []uint64) {
	s.units = slices.DeleteFunc(s.units, func(u *unit) bool {
		return slices.Contains(ids, u.spec.GetId())
	})
}

// runUnits executes one frame of the unit scheduler as described in
// design/emulator-api.md. s.mu must be held.
func (s *Server) runUnits() {
	var executing []*unit
	for _, u := range s.units {
		if !u.running {
			if u.wait > 0 {
				u.wait--
				continue
			}
			u.running = true
			u.executed = 0
			if st := u.spec.GetStore(); st != nil && !st.GetContinuous() {
				u.sample = s.sampleUnit(u.spec)
			}
		}
		executing = append(executing, u)
	}

	for _, u := range executing {
		data := u.spec.GetValue()
		if st := u.spec.GetStore(); st != nil {
			if st.GetContinuous() {
				u.sample = s.sampleUnit(u.spec)
			}
			data = u.sample
		}
		copy(s.mem[u.spec.GetDomain()][u.spec.GetAddress():], data)
		u.executed++
	}

	s.units = slices.DeleteFunc(s.units, func(u *unit) bool {
		lifetime := u.spec.GetLifetime()
		if !u.running || lifetime == 0 || u.executed < lifetime {
			return false
		}
		if !u.spec.GetLoop() {
			return true
		}
		u.running = false
		u.wait = u.spec.GetLoopDelay()
		if u.wait == 0 {
			u.wait = u.spec.GetDelay()
		}
		return false
	})
}

func (s *Server) sampleUnit(spec *emulatorv1.Unit) []byte {
	st := spec.GetStore()
	d, m := s.domain(st.GetDomain())
	b := append([]byte(nil), m[st.GetAddress():st.GetAddress()+uint64(spec.GetSize())]...)
	if tilt := spec.GetTilt(); tilt != 0 {
		putUint(b, getUint(b, d.BigEndian)+uint64(tilt), d.BigEndian)
	}
	return b
}
