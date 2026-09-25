// Package corrupt implements RTCV's corruption core: blast units and
// layers, the corruption engines, blast layer generation and the
// translation of blast units into emulator units.
package corrupt

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// MaxPrecision caps Unit.Precision at 16 MiB. RTCV caps it at 16348.
const MaxPrecision = 16 << 20

type Source string

const (
	SourceValue Source = "value"
	SourceStore Source = "store"
)

type StoreTime string

const (
	StoreImmediate  StoreTime = "immediate"
	StorePreExecute StoreTime = "preexecute"
)

type StoreType string

const (
	StoreOnce       StoreType = "once"
	StoreContinuous StoreType = "continuous"
)

type LimiterTime string

const (
	LimiterNone       LimiterTime = "none"
	LimiterGenerate   LimiterTime = "generate"
	LimiterPreExecute LimiterTime = "preexecute"
	LimiterExecute    LimiterTime = "execute"
)

// Hex is a byte string encoded as a lowercase hex string in JSON. Input
// is case-insensitive; an odd number of digits is left-padded with 0.
type Hex []byte

func (h Hex) MarshalText() ([]byte, error) {
	return hex.AppendEncode(nil, h), nil
}

func (h *Hex) UnmarshalText(text []byte) error {
	text = bytes.TrimSpace(text)
	b := make([]byte, (len(text)+1)/2)
	dst, src := b, text
	if len(text)%2 == 1 {
		if _, err := hex.Decode(b[:1], []byte{'0', text[0]}); err != nil {
			return fmt.Errorf("invalid hex value: %w", err)
		}
		dst, src = b[1:], text[1:]
	}
	if _, err := hex.Decode(dst, src); err != nil {
		return fmt.Errorf("invalid hex value: %w", err)
	}
	*h = b
	return nil
}

// Unit mirrors RTCV's BlastUnit.
//
// Value holds the number in little-endian byte order. It is written as-is,
// or byte-reversed when BigEndian is set, after adding Tilt. For STORE
// units Tilt is added to the sampled bytes, interpreted with the unit's
// endianness.
type Unit struct {
	Enabled       bool
	Locked        bool
	BigEndian     bool
	Domain        string
	Address       uint64
	Precision     int
	Source        Source
	Value         Hex
	SourceDomain  string
	SourceAddress uint64
	StoreTime     StoreTime
	StoreType     StoreType
	Tilt          *big.Int
	ExecuteFrame  int
	// Lifetime is the number of frames the unit executes; 0 is forever.
	Lifetime int
	Loop     bool
	// LoopTiming is the delay used when a looping unit is re-applied; -1
	// means ExecuteFrame is used.
	LoopTiming              int
	LimiterTime             LimiterTime
	LimiterList             string
	InvertLimiter           bool
	GeneratedUsingValueList bool
	Note                    string
}

// NewUnit returns a unit with RTCV's defaults.
func NewUnit() *Unit {
	return &Unit{
		Enabled:     true,
		Source:      SourceValue,
		StoreTime:   StoreImmediate,
		StoreType:   StoreOnce,
		Lifetime:    1,
		LoopTiming:  -1,
		LimiterTime: LimiterNone,
	}
}

func newValueUnit(domain string, addr uint64, value []byte, bigEndian bool, lifetime int) *Unit {
	u := NewUnit()
	u.Domain = domain
	u.Address = addr
	u.Precision = len(value)
	u.Value = value
	u.BigEndian = bigEndian
	u.Lifetime = lifetime
	return u
}

func newStoreUnit(typ StoreType, tm StoreTime, domain string, addr uint64, srcDomain string, srcAddr uint64, precision int, bigEndian bool, executeFrame, lifetime int) *Unit {
	u := NewUnit()
	u.Source = SourceStore
	u.StoreType = typ
	u.StoreTime = tm
	u.Domain = domain
	u.Address = addr
	u.SourceDomain = srcDomain
	u.SourceAddress = srcAddr
	u.Precision = precision
	u.BigEndian = bigEndian
	u.ExecuteFrame = executeFrame
	u.Lifetime = lifetime
	return u
}

func (u *Unit) Clone() *Unit {
	c := *u
	c.Value = bytes.Clone(u.Value)
	if u.Tilt != nil {
		c.Tilt = new(big.Int).Set(u.Tilt)
	}
	return &c
}

func (u *Unit) hasTilt() bool { return u.Tilt != nil && u.Tilt.Sign() != 0 }

// SetPrecision changes the precision and resizes Value like RTCV: a longer
// value is left-padded with zeros, a shorter one keeps its last bytes.
func (u *Unit) SetPrecision(p int) {
	p = min(max(p, 1), MaxPrecision)
	u.Precision = p
	if u.Source == SourceValue || len(u.Value) > 0 {
		u.Value = resize(u.Value, p)
	}
}

func resize(b []byte, n int) []byte {
	switch {
	case len(b) < n:
		out := make([]byte, n)
		copy(out[n-len(b):], b)
		return out
	case len(b) > n:
		return bytes.Clone(b[len(b)-n:])
	}
	return b
}

func (u *Unit) Validate() error {
	var errs []error
	if u.Domain == "" {
		errs = append(errs, errors.New("domain is empty"))
	}
	if u.Precision < 1 || u.Precision > MaxPrecision {
		errs = append(errs, fmt.Errorf("precision %d out of range 1..%d", u.Precision, MaxPrecision))
	}
	switch u.Source {
	case SourceValue:
		if len(u.Value) != u.Precision {
			errs = append(errs, fmt.Errorf("value has %d bytes, precision is %d", len(u.Value), u.Precision))
		}
	case SourceStore:
		if u.SourceDomain == "" {
			errs = append(errs, errors.New("source domain is empty"))
		}
	default:
		errs = append(errs, fmt.Errorf("invalid source %q", u.Source))
	}
	if u.StoreTime != StoreImmediate && u.StoreTime != StorePreExecute {
		errs = append(errs, fmt.Errorf("invalid storeTime %q", u.StoreTime))
	}
	if u.StoreType != StoreOnce && u.StoreType != StoreContinuous {
		errs = append(errs, fmt.Errorf("invalid storeType %q", u.StoreType))
	}
	switch u.LimiterTime {
	case LimiterNone, LimiterGenerate, LimiterPreExecute, LimiterExecute:
	default:
		errs = append(errs, fmt.Errorf("invalid limiterTime %q", u.LimiterTime))
	}
	if u.ExecuteFrame < 0 {
		errs = append(errs, fmt.Errorf("executeFrame %d is negative", u.ExecuteFrame))
	}
	if u.Lifetime < 0 {
		errs = append(errs, fmt.Errorf("lifetime %d is negative", u.Lifetime))
	}
	if u.LoopTiming < -1 {
		errs = append(errs, fmt.Errorf("loopTiming %d is below -1", u.LoopTiming))
	}
	return errors.Join(errs...)
}

type unitJSON struct {
	Enabled                 bool            `json:"enabled"`
	Locked                  bool            `json:"locked"`
	BigEndian               bool            `json:"bigEndian"`
	Domain                  string          `json:"domain"`
	Address                 uint64          `json:"address"`
	Precision               int             `json:"precision"`
	Source                  Source          `json:"source"`
	Value                   Hex             `json:"value"`
	SourceDomain            string          `json:"sourceDomain"`
	SourceAddress           uint64          `json:"sourceAddress"`
	StoreTime               StoreTime       `json:"storeTime"`
	StoreType               StoreType       `json:"storeType"`
	Tilt                    json.RawMessage `json:"tilt"`
	ExecuteFrame            int             `json:"executeFrame"`
	Lifetime                int             `json:"lifetime"`
	Loop                    bool            `json:"loop"`
	LoopTiming              *int            `json:"loopTiming"`
	LimiterTime             LimiterTime     `json:"limiterTime"`
	LimiterList             string          `json:"limiterList"`
	InvertLimiter           bool            `json:"invertLimiter"`
	GeneratedUsingValueList bool            `json:"generatedUsingValueList"`
	Note                    string          `json:"note"`
}

func (u *Unit) MarshalJSON() ([]byte, error) {
	j := unitJSON{
		Enabled:                 u.Enabled,
		Locked:                  u.Locked,
		BigEndian:               u.BigEndian,
		Domain:                  u.Domain,
		Address:                 u.Address,
		Precision:               u.Precision,
		Source:                  u.Source,
		Value:                   u.Value,
		SourceDomain:            u.SourceDomain,
		SourceAddress:           u.SourceAddress,
		StoreTime:               u.StoreTime,
		StoreType:               u.StoreType,
		Tilt:                    marshalDecimal(u.Tilt),
		ExecuteFrame:            u.ExecuteFrame,
		Lifetime:                u.Lifetime,
		Loop:                    u.Loop,
		LoopTiming:              loopTiming(u.LoopTiming),
		LimiterTime:             u.LimiterTime,
		LimiterList:             u.LimiterList,
		InvertLimiter:           u.InvertLimiter,
		GeneratedUsingValueList: u.GeneratedUsingValueList,
		Note:                    u.Note,
	}
	return json.Marshal(j)
}

// UnmarshalJSON fills missing fields with RTCV's defaults. A missing or
// zero precision is taken from the value length; a null or negative
// loopTiming is unset (-1).
func (u *Unit) UnmarshalJSON(data []byte) error {
	d := NewUnit()
	j := unitJSON{
		Enabled:     d.Enabled,
		Source:      d.Source,
		StoreTime:   d.StoreTime,
		StoreType:   d.StoreType,
		Lifetime:    d.Lifetime,
		LimiterTime: d.LimiterTime,
	}
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	tilt, err := unmarshalBigInt(j.Tilt)
	if err != nil {
		return fmt.Errorf("tilt: %w", err)
	}
	*u = Unit{
		Enabled:                 j.Enabled,
		Locked:                  j.Locked,
		BigEndian:               j.BigEndian,
		Domain:                  j.Domain,
		Address:                 j.Address,
		Precision:               j.Precision,
		Source:                  j.Source,
		Value:                   j.Value,
		SourceDomain:            j.SourceDomain,
		SourceAddress:           j.SourceAddress,
		StoreTime:               j.StoreTime,
		StoreType:               j.StoreType,
		Tilt:                    tilt,
		ExecuteFrame:            j.ExecuteFrame,
		Lifetime:                j.Lifetime,
		Loop:                    j.Loop,
		LoopTiming:              -1,
		LimiterTime:             j.LimiterTime,
		LimiterList:             j.LimiterList,
		InvertLimiter:           j.InvertLimiter,
		GeneratedUsingValueList: j.GeneratedUsingValueList,
		Note:                    j.Note,
	}
	if j.LoopTiming != nil && *j.LoopTiming >= 0 {
		u.LoopTiming = *j.LoopTiming
	}
	if u.Source == "" {
		u.Source = SourceValue
	}
	if u.StoreTime == "" {
		u.StoreTime = StoreImmediate
	}
	if u.StoreType == "" {
		u.StoreType = StoreOnce
	}
	if u.LimiterTime == "" {
		u.LimiterTime = LimiterNone
	}
	if u.Precision == 0 {
		u.Precision = len(u.Value)
	}
	return nil
}

// marshalDecimal encodes v as a decimal JSON string; nil is "0".
func marshalDecimal(v *big.Int) json.RawMessage {
	if v == nil {
		return json.RawMessage(`"0"`)
	}
	return json.RawMessage(strconv.Quote(v.String()))
}

func loopTiming(v int) *int {
	if v < 0 {
		return nil
	}
	return &v
}

// unmarshalBigInt accepts a decimal string or a JSON number.
func unmarshalBigInt(raw json.RawMessage) (*big.Int, error) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return nil, nil
	}
	if strings.HasPrefix(s, `"`) {
		var err error
		if s, err = strconv.Unquote(s); err != nil {
			return nil, err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, nil
		}
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("invalid integer %q", s)
	}
	if v.Sign() == 0 {
		return nil, nil
	}
	return v, nil
}
