package corrupt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"os"
	"slices"

	"github.com/puhitaku/rtcv-ish/internal/corrupt/lists"
)

// Layer mirrors RTCV's BlastLayer. Its JSON form is also the .bl file
// format.
type Layer struct {
	Note  string  `json:"note"`
	Units []*Unit `json:"units"`
}

func (l Layer) MarshalJSON() ([]byte, error) {
	type layer Layer
	if l.Units == nil {
		l.Units = []*Unit{}
	}
	return json.Marshal(layer(l))
}

// Load reads a .bl file.
func Load(path string) (*Layer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var l Layer
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	l.Units = slices.DeleteFunc(l.Units, func(u *Unit) bool { return u == nil })
	return &l, nil
}

// Save writes l as an indented .bl file.
func (l *Layer) Save(path string) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func (l *Layer) Clone() *Layer {
	c := &Layer{Note: l.Note, Units: make([]*Unit, len(l.Units))}
	for i, u := range l.Units {
		c.Units[i] = u.Clone()
	}
	return c
}

func (l *Layer) Validate() error {
	var errs []error
	for i, u := range l.Units {
		if err := u.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("unit %d: %w", i, err))
		}
	}
	return errors.Join(errs...)
}

// Merge concatenates copies of the units of all layers.
func Merge(layers ...*Layer) *Layer {
	out := &Layer{}
	for _, l := range layers {
		if l == nil {
			continue
		}
		for _, u := range l.Units {
			out.Units = append(out.Units, u.Clone())
		}
	}
	return out
}

// SanitizeDuplicates keeps only the last unlocked unit for each (domain,
// address). Locked units are always kept.
func (l *Layer) SanitizeDuplicates() {
	type key struct {
		domain string
		addr   uint64
	}
	seen := make(map[key]bool)
	keep := make([]bool, len(l.Units))
	for i := len(l.Units) - 1; i >= 0; i-- {
		u := l.Units[i]
		k := key{u.Domain, u.Address}
		keep[i] = u.Locked || !seen[k]
		if !u.Locked {
			seen[k] = true
		}
	}
	out := l.Units[:0]
	for i, u := range l.Units {
		if keep[i] {
			out = append(out, u)
		}
	}
	clear(l.Units[len(out):])
	l.Units = out
}

func (l *Layer) unlocked() []*Unit {
	var out []*Unit
	for _, u := range l.Units {
		if !u.Locked {
			out = append(out, u)
		}
	}
	return out
}

// Disable50 enables every unlocked unit, then disables a random half.
func (l *Layer) Disable50(rng *rand.Rand) {
	us := l.unlocked()
	for _, u := range us {
		u.Enabled = true
	}
	for _, i := range rng.Perm(len(us))[:len(us)/2] {
		us[i].Enabled = false
	}
}

func (l *Layer) InvertDisabled() {
	for _, u := range l.unlocked() {
		u.Enabled = !u.Enabled
	}
}

// RemoveDisabled removes unlocked disabled units.
func (l *Layer) RemoveDisabled() {
	l.Units = slices.DeleteFunc(l.Units, func(u *Unit) bool { return !u.Locked && !u.Enabled })
}

func (l *Layer) EnableAll() {
	for _, u := range l.unlocked() {
		u.Enabled = true
	}
}

func (l *Layer) DisableAll() {
	for _, u := range l.unlocked() {
		u.Enabled = false
	}
}

// Duplicate appends copies of the unlocked units at the given indices.
func (l *Layer) Duplicate(indices ...int) error {
	for _, i := range indices {
		if i < 0 || i >= len(l.Units) {
			return fmt.Errorf("unit index %d out of range", i)
		}
	}
	for _, i := range indices {
		if u := l.Units[i]; !u.Locked {
			l.Units = append(l.Units, u.Clone())
		}
	}
	return nil
}

type ShiftField string

const (
	ShiftAddress       ShiftField = "address"
	ShiftSourceAddress ShiftField = "sourceAddress"
	ShiftExecuteFrame  ShiftField = "executeFrame"
	ShiftLifetime      ShiftField = "lifetime"
	ShiftLoopTiming    ShiftField = "loopTiming"
	ShiftTilt          ShiftField = "tilt"
	// ShiftValue adds to Value as a little-endian number with wrap-around.
	ShiftValue ShiftField = "value"
)

// Shift adds amount to a field of the units at indices (all units if
// none are given), clamping integer fields at 0 like RTCV's Blast Editor.
func (l *Layer) Shift(field ShiftField, amount int64, indices ...int) error {
	if !oneOf(field, ShiftAddress, ShiftSourceAddress, ShiftExecuteFrame, ShiftLifetime, ShiftLoopTiming, ShiftTilt, ShiftValue) {
		return fmt.Errorf("cannot shift field %q", field)
	}
	units := l.Units
	if len(indices) > 0 {
		units = make([]*Unit, len(indices))
		for j, i := range indices {
			if i < 0 || i >= len(l.Units) {
				return fmt.Errorf("unit index %d out of range", i)
			}
			units[j] = l.Units[i]
		}
	}
	for _, u := range units {
		switch field {
		case ShiftAddress:
			u.Address = shiftUint(u.Address, amount)
		case ShiftSourceAddress:
			u.SourceAddress = shiftUint(u.SourceAddress, amount)
		case ShiftExecuteFrame:
			u.ExecuteFrame = shiftInt(u.ExecuteFrame, amount)
		case ShiftLifetime:
			u.Lifetime = shiftInt(u.Lifetime, amount)
		case ShiftLoopTiming:
			u.LoopTiming = shiftInt(u.LoopTiming, amount)
		case ShiftTilt:
			t := big.NewInt(amount)
			if u.Tilt != nil {
				t.Add(t, u.Tilt)
			}
			u.Tilt = t
			if t.Sign() == 0 {
				u.Tilt = nil
			}
		case ShiftValue:
			u.Value = addLE(u.Value, amount)
		}
	}
	return nil
}

func shiftUint(v uint64, amount int64) uint64 {
	if amount < 0 {
		d := uint64(-(amount + 1)) + 1
		if d > v {
			return 0
		}
		return v - d
	}
	if uint64(amount) > math.MaxUint64-v {
		return math.MaxUint64
	}
	return v + uint64(amount)
}

func shiftInt(v int, amount int64) int {
	r := int64(v) + amount
	return int(min(max(r, 0), math.MaxInt32))
}

// addLE adds amount to a little-endian number of any length, wrapping
// around.
func addLE(b []byte, amount int64) []byte {
	be := slices.Clone(b)
	slices.Reverse(be)
	v := new(big.Int).SetBytes(be)
	v.Add(v, big.NewInt(amount))
	v.Mod(v, new(big.Int).Lsh(big.NewInt(1), uint(8*len(be))))
	v.FillBytes(be)
	slices.Reverse(be)
	return be
}

// Bake returns a layer of VALUE units (lifetime 1) holding the current
// memory at every enabled unit. Units outside their domain are skipped.
func (l *Layer) Bake(ctx context.Context, mem Memory) (*Layer, error) {
	var ranges []Range
	var src []*Unit
	for _, u := range l.Units {
		if !u.Enabled || u.Precision < 1 {
			continue
		}
		d, ok := findDomain(mem, u.Domain)
		if !ok || !d.contains(u.Address, u.Precision) {
			continue
		}
		ranges = append(ranges, Range{Domain: u.Domain, Address: u.Address, Size: u.Precision})
		src = append(src, u)
	}
	out := &Layer{Note: l.Note}
	if len(ranges) == 0 {
		return out, nil
	}
	data, err := mem.ReadMany(ctx, ranges)
	if err != nil {
		return nil, err
	}
	for i, u := range src {
		b := newValueUnit(u.Domain, u.Address, data[i], false, 1)
		b.Note = u.Note
		b.Locked = u.Locked
		out.Units = append(out.Units, b)
	}
	return out, nil
}

// Backup is the uncorrupt backup of l: its baked layer, taken before l is
// applied.
func (l *Layer) Backup(ctx context.Context, mem Memory) (*Layer, error) {
	return l.Bake(ctx, mem)
}

// Breakdown splits every unit into 1-byte units. VALUE units get the
// bytes they would write (tilt and endianness applied); STORE units keep
// the tilt on their least significant byte only, as RTCV does.
func (l *Layer) Breakdown() *Layer {
	out := &Layer{Note: l.Note}
	for _, u := range l.Units {
		if u.Precision <= 1 {
			out.Units = append(out.Units, u.Clone())
			continue
		}
		var written []byte
		if u.Source == SourceValue {
			written = u.writtenValue()
		}
		lsb := 0
		if u.BigEndian {
			lsb = u.Precision - 1
		}
		for i := range u.Precision {
			s := u.Clone()
			s.Precision = 1
			s.Address = u.Address + uint64(i)
			if u.Source == SourceValue {
				s.Value = Hex{written[i]}
				s.BigEndian = false
				s.Tilt = nil
			} else {
				s.Value = nil
				s.SourceAddress = u.SourceAddress + uint64(i)
				if i != lsb {
					s.Tilt = nil
				}
			}
			out.Units = append(out.Units, s)
		}
	}
	return out
}

// writtenValue is the byte sequence a VALUE unit pokes: Value plus tilt
// (little-endian arithmetic), reversed if BigEndian.
func (u *Unit) writtenValue() []byte {
	v := addTilt(resize(u.Value, u.Precision), u.Tilt, false)
	if u.BigEndian {
		slices.Reverse(v)
	}
	return v
}

// Reroll re-randomizes unlocked units like BlastUnit.Reroll: VALUE units
// get a new value (from the vector or custom value list if they were
// generated from one), STORE units get new domains/addresses according to
// s.Reroll. New addresses always leave room for the unit's precision.
func (l *Layer) Reroll(rng *rand.Rand, s *Settings, selected []string, mem Memory, reg *lists.Registry) error {
	for i, u := range l.Units {
		if u.Locked {
			continue
		}
		if err := u.reroll(rng, s, selected, mem, reg); err != nil {
			return fmt.Errorf("unit %d: %w", i, err)
		}
	}
	return nil
}

func (u *Unit) reroll(rng *rand.Rand, s *Settings, selected []string, mem Memory, reg *lists.Registry) error {
	p := max(u.Precision, 1)
	if u.Source == SourceValue {
		listName := s.Vector.ValueList
		if s.Reroll.FollowCustomEngine {
			listName = s.Custom.ValueList
		}
		switch {
		case u.GeneratedUsingValueList:
			vl, ok := reg.Get(listName)
			if !ok {
				return fmt.Errorf("value list %q not found", listName)
			}
			u.Value = vl.Random(rng, p)
		case s.Reroll.FollowCustomEngine && s.Custom.ValueSource == ValueFromRange:
			u.Value = s.Custom.Ranges.value(rng, p)
		default:
			u.Value = FullRange().value(rng, p)
		}
		return nil
	}
	pick := func() (string, error) {
		if len(selected) == 0 {
			return "", ErrNoDomains
		}
		return selected[rng.IntN(len(selected))], nil
	}
	addr := func(domain string) (uint64, error) {
		d, ok := findDomain(mem, domain)
		if !ok {
			return 0, fmt.Errorf("%w %q", ErrUnknownDomain, domain)
		}
		return uint64(randomAddress(rng, int64(d.Size)-int64(p)+1)), nil
	}
	var err error
	if s.Reroll.SourceDomain {
		if u.SourceDomain, err = pick(); err != nil {
			return err
		}
	}
	if s.Reroll.SourceAddress {
		if u.SourceAddress, err = addr(u.SourceDomain); err != nil {
			return err
		}
	}
	if s.Reroll.Domain {
		if u.Domain, err = pick(); err != nil {
			return err
		}
	}
	if s.Reroll.Address {
		if u.Address, err = addr(u.Domain); err != nil {
			return err
		}
	}
	return nil
}

func bigOne(negative bool) *big.Int {
	if negative {
		return big.NewInt(-1)
	}
	return big.NewInt(1)
}

func cloneBig(v *big.Int) *big.Int { return new(big.Int).Set(v) }
