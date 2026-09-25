package corrupt

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"strconv"
)

type Engine string

const (
	EngineNightmare  Engine = "nightmare"
	EngineHellgenie  Engine = "hellgenie"
	EngineDistortion Engine = "distortion"
	EngineFreeze     Engine = "freeze"
	EnginePipe       Engine = "pipe"
	EngineVector     Engine = "vector"
	EngineCluster    Engine = "cluster"
	EngineCustom     Engine = "custom"
)

// FreezeMode is how infinite units (lifetime 0) are enforced by the
// emulator; see Unit.mode in design/emulator-api.md.
type FreezeMode string

const (
	FreezeFrame    FreezeMode = "frame"
	FreezeScanline FreezeMode = "scanline"
	FreezeHard     FreezeMode = "hard"
)

type Radius string

const (
	RadiusSpread       Radius = "spread"
	RadiusChunk        Radius = "chunk"
	RadiusBurst        Radius = "burst"
	RadiusNormalized   Radius = "normalized"
	RadiusProportional Radius = "proportional"
	RadiusEven         Radius = "even"
)

type NightmareAlgo string

const (
	NightmareRandom     NightmareAlgo = "random"
	NightmareRandomTilt NightmareAlgo = "randomTilt"
	NightmareTilt       NightmareAlgo = "tilt"
)

type ClusterMethod string

const (
	ClusterRandom          ClusterMethod = "random"
	ClusterReverse         ClusterMethod = "reverse"
	ClusterRotateForwards  ClusterMethod = "rotateForwards"
	ClusterRotateBackwards ClusterMethod = "rotateBackwards"
	ClusterOverwrite       ClusterMethod = "overwrite"
)

type ClusterDirection string

const (
	ClusterForwards  ClusterDirection = "forwards"
	ClusterBackwards ClusterDirection = "backwards"
)

type ValueSource string

const (
	ValueRandom    ValueSource = "random"
	ValueFromList  ValueSource = "valueList"
	ValueFromRange ValueSource = "range"
)

type StoreAddress string

const (
	StoreAddressSame   StoreAddress = "same"
	StoreAddressRandom StoreAddress = "random"
)

type StoreLimiterSource string

const (
	LimitAddress       StoreLimiterSource = "address"
	LimitSourceAddress StoreLimiterSource = "sourceAddress"
	LimitBoth          StoreLimiterSource = "both"
)

// ValueRange is RTCV's per-precision inclusive [min, max] for random
// values. In JSON it is a map keyed by precision in bytes with decimal
// string bounds: {"1": {"min": "0", "max": "255"}, "2": ...}. Decoding
// merges into the current values.
type ValueRange struct {
	Min8, Max8   uint64
	Min16, Max16 uint64
	Min32, Max32 uint64
	Min64, Max64 uint64
}

type boundsJSON struct {
	Min *json.Number `json:"min,omitempty"`
	Max *json.Number `json:"max,omitempty"`
}

func (r *ValueRange) fields(precision int) (lo, hi *uint64) {
	switch precision {
	case 1:
		return &r.Min8, &r.Max8
	case 2:
		return &r.Min16, &r.Max16
	case 4:
		return &r.Min32, &r.Max32
	case 8:
		return &r.Min64, &r.Max64
	}
	return nil, nil
}

func (r ValueRange) MarshalJSON() ([]byte, error) {
	m := make(map[string]map[string]string, 4)
	for _, p := range []int{1, 2, 4, 8} {
		lo, hi := r.fields(p)
		m[strconv.Itoa(p)] = map[string]string{"min": strconv.FormatUint(*lo, 10), "max": strconv.FormatUint(*hi, 10)}
	}
	return json.Marshal(m)
}

func (r *ValueRange) UnmarshalJSON(data []byte) error {
	var m map[string]boundsJSON
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	for k, b := range m {
		p, _ := strconv.Atoi(k)
		lo, hi := r.fields(p)
		if lo == nil {
			return fmt.Errorf("invalid precision %q in ranges", k)
		}
		for _, f := range []struct {
			n   *json.Number
			dst *uint64
		}{{b.Min, lo}, {b.Max, hi}} {
			if f.n == nil {
				continue
			}
			v, err := strconv.ParseUint(f.n.String(), 10, 64)
			if err != nil {
				return fmt.Errorf("ranges %s: %w", k, err)
			}
			*f.dst = v
		}
	}
	return nil
}

func FullRange() ValueRange {
	return ValueRange{Max8: math.MaxUint8, Max16: math.MaxUint16, Max32: math.MaxUint32, Max64: math.MaxUint64}
}

// Bounds returns the range for a precision of 1, 2, 4 or 8 bytes.
func (r ValueRange) Bounds(precision int) (lo, hi uint64, ok bool) {
	switch precision {
	case 1:
		return r.Min8, r.Max8, true
	case 2:
		return r.Min16, r.Max16, true
	case 4:
		return r.Min32, r.Max32, true
	case 8:
		return r.Min64, r.Max64, true
	}
	return 0, 0, false
}

func (r ValueRange) validate() error {
	var errs []error
	for _, p := range []int{1, 2, 4, 8} {
		lo, hi, _ := r.Bounds(p)
		if lo > hi {
			errs = append(errs, fmt.Errorf("min%d > max%d", 8*p, 8*p))
		}
		if hi > maxForSize(p) {
			errs = append(errs, fmt.Errorf("max%d exceeds %d bits", 8*p, 8*p))
		}
	}
	return errors.Join(errs...)
}

// value draws a little-endian random value of precision bytes: from the
// range for 1/2/4/8 bytes, random bytes otherwise.
func (r ValueRange) value(rng *rand.Rand, precision int) []byte {
	lo, hi, ok := r.Bounds(precision)
	if !ok {
		return randomBytes(rng, precision)
	}
	return encodeLE(uint64Range(rng, lo, hi), precision)
}

type NightmareSettings struct {
	Algo   NightmareAlgo `json:"algo"`
	Ranges ValueRange    `json:"ranges"`
}

type HellgenieSettings struct {
	Ranges ValueRange `json:"ranges"`
}

type DistortionSettings struct {
	Delay int `json:"delay"`
}

type VectorSettings struct {
	LimiterList     string `json:"limiterList"`
	ValueList       string `json:"valueList"`
	UnlockPrecision bool   `json:"unlockPrecision"`
}

type ClusterSettings struct {
	LimiterList string           `json:"limiterList"`
	ChunkSize   int              `json:"chunkSize"`
	Method      ClusterMethod    `json:"method"`
	Modifier    int              `json:"modifier"`
	Direction   ClusterDirection `json:"direction"`
	// SplitUnits is RTCV's CLUSTER_MULTIOUT: one unit per element instead
	// of one unit for the whole chunk.
	SplitUnits bool `json:"splitUnits"`
	FilterAll  bool `json:"filterAll"`
}

// CustomSettings are RTCV's CUSTOM_* parameters. Precision and alignment
// come from Settings.
type CustomSettings struct {
	Source       Source
	ValueSource  ValueSource
	Ranges       ValueRange
	ValueList    string
	StoreAddress StoreAddress
	StoreTime    StoreTime
	StoreType    StoreType
	Tilt         *big.Int
	Delay        int
	Lifetime     int
	Loop         bool
	LimiterList  string
	// LimiterTime preexecute and execute are checked at generation time
	// because the emulator cannot evaluate lists.
	LimiterTime        LimiterTime
	LimiterInverted    bool
	StoreLimiterSource StoreLimiterSource
}

type customJSON struct {
	Source             Source             `json:"source"`
	ValueSource        ValueSource        `json:"valueSource"`
	ValueList          string             `json:"valueList"`
	StoreAddress       StoreAddress       `json:"storeAddress"`
	StoreTime          StoreTime          `json:"storeTime"`
	StoreType          StoreType          `json:"storeType"`
	Tilt               json.RawMessage    `json:"tilt"`
	Delay              int                `json:"delay"`
	Lifetime           int                `json:"lifetime"`
	Loop               bool               `json:"loop"`
	LimiterList        string             `json:"limiterList"`
	LimiterTime        LimiterTime        `json:"limiterTime"`
	LimiterInverted    bool               `json:"limiterInverted"`
	StoreLimiterSource StoreLimiterSource `json:"storeLimiterSource"`
	Ranges             ValueRange         `json:"ranges"`
}

func (c CustomSettings) toJSON() customJSON {
	tilt := marshalDecimal(c.Tilt)
	return customJSON{
		Source: c.Source, ValueSource: c.ValueSource, ValueList: c.ValueList,
		StoreAddress: c.StoreAddress, StoreTime: c.StoreTime, StoreType: c.StoreType,
		Tilt: tilt, Delay: c.Delay, Lifetime: c.Lifetime, Loop: c.Loop,
		LimiterList: c.LimiterList, LimiterTime: c.LimiterTime, LimiterInverted: c.LimiterInverted,
		StoreLimiterSource: c.StoreLimiterSource, Ranges: c.Ranges,
	}
}

func (c CustomSettings) MarshalJSON() ([]byte, error) { return json.Marshal(c.toJSON()) }

// UnmarshalJSON merges into the current values, so a partial object only
// changes the fields it contains.
func (c *CustomSettings) UnmarshalJSON(data []byte) error {
	j := c.toJSON()
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	tilt, err := unmarshalBigInt(j.Tilt)
	if err != nil {
		return fmt.Errorf("custom tilt: %w", err)
	}
	*c = CustomSettings{
		Source: j.Source, ValueSource: j.ValueSource, ValueList: j.ValueList,
		StoreAddress: j.StoreAddress, StoreTime: j.StoreTime, StoreType: j.StoreType,
		Tilt: tilt, Delay: j.Delay, Lifetime: j.Lifetime, Loop: j.Loop,
		LimiterList: j.LimiterList, LimiterTime: j.LimiterTime, LimiterInverted: j.LimiterInverted,
		StoreLimiterSource: j.StoreLimiterSource, Ranges: j.Ranges,
	}
	return nil
}

type RerollSettings struct {
	Address            bool `json:"address"`
	SourceAddress      bool `json:"sourceAddress"`
	Domain             bool `json:"domain"`
	SourceDomain       bool `json:"sourceDomain"`
	FollowCustomEngine bool `json:"followCustomEngine"`
}

type GameProtectionSettings struct {
	Enabled         bool `json:"enabled"`
	IntervalSeconds int  `json:"intervalSeconds"`
	Keep            int  `json:"keep"`
}

// Settings is the engine configuration (RTCV's CorruptCoreSpec subset).
type Settings struct {
	Engine    Engine `json:"engine"`
	Intensity int    `json:"intensity"`
	// ErrorDelay is the number of frames between auto-corrupt blasts.
	ErrorDelay       int                    `json:"errorDelay"`
	Radius           Radius                 `json:"radius"`
	Precision        int                    `json:"precision"`
	Alignment        int                    `json:"alignment"`
	AutoCorrupt      bool                   `json:"autoCorrupt"`
	MaxInfiniteUnits int                    `json:"maxInfiniteUnits"`
	LockUnits        bool                   `json:"lockUnits"`
	FreezeMode       FreezeMode             `json:"freezeMode"`
	Nightmare        NightmareSettings      `json:"nightmare"`
	Hellgenie        HellgenieSettings      `json:"hellgenie"`
	Distortion       DistortionSettings     `json:"distortion"`
	Vector           VectorSettings         `json:"vector"`
	Cluster          ClusterSettings        `json:"cluster"`
	Custom           CustomSettings         `json:"custom"`
	Reroll           RerollSettings         `json:"reroll"`
	GameProtection   GameProtectionSettings `json:"gameProtection"`
}

// NightmareTemplate is RTCV's "Nightmare Engine" custom template, the
// default custom engine configuration.
func NightmareTemplate() CustomSettings {
	return CustomSettings{
		Source:             SourceValue,
		ValueSource:        ValueRandom,
		Ranges:             FullRange(),
		StoreAddress:       StoreAddressRandom,
		StoreTime:          StoreImmediate,
		StoreType:          StoreOnce,
		Lifetime:           1,
		LimiterTime:        LimiterNone,
		StoreLimiterSource: LimitAddress,
	}
}

func DefaultSettings() *Settings {
	return &Settings{
		Engine:           EngineNightmare,
		Intensity:        1,
		ErrorDelay:       1,
		Radius:           RadiusSpread,
		Precision:        1,
		MaxInfiniteUnits: 50,
		FreezeMode:       FreezeHard,
		Nightmare:        NightmareSettings{Algo: NightmareRandom, Ranges: FullRange()},
		Hellgenie:        HellgenieSettings{Ranges: FullRange()},
		Distortion:       DistortionSettings{Delay: 50},
		Cluster: ClusterSettings{
			ChunkSize:  3,
			Method:     ClusterRandom,
			Modifier:   1,
			Direction:  ClusterForwards,
			SplitUnits: true,
		},
		Custom:         NightmareTemplate(),
		Reroll:         RerollSettings{SourceAddress: true, SourceDomain: true},
		GameProtection: GameProtectionSettings{IntervalSeconds: 5, Keep: 10},
	}
}

func (s *Settings) Clone() *Settings {
	c := *s
	if s.Custom.Tilt != nil {
		c.Custom.Tilt = new(big.Int).Set(s.Custom.Tilt)
	}
	return &c
}

func oneOf[T comparable](v T, allowed ...T) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func (s *Settings) Validate() error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	if !oneOf(s.Engine, EngineNightmare, EngineHellgenie, EngineDistortion, EngineFreeze, EnginePipe, EngineVector, EngineCluster, EngineCustom) {
		add("invalid engine %q", s.Engine)
	}
	if s.Intensity < 1 {
		add("intensity must be at least 1")
	}
	if s.ErrorDelay < 1 {
		add("errorDelay must be at least 1")
	}
	if !oneOf(s.Radius, RadiusSpread, RadiusChunk, RadiusBurst, RadiusNormalized, RadiusProportional, RadiusEven) {
		add("invalid radius %q", s.Radius)
	}
	if !oneOf(s.Precision, 1, 2, 4, 8) {
		add("precision must be 1, 2, 4 or 8")
	}
	if s.Alignment < 0 || s.Alignment >= max(s.Precision, 1) {
		add("alignment must be in 0..precision-1")
	}
	if s.MaxInfiniteUnits < 1 {
		add("maxInfiniteUnits must be at least 1")
	}
	if !oneOf(s.FreezeMode, FreezeFrame, FreezeScanline, FreezeHard) {
		add("invalid freezeMode %q", s.FreezeMode)
	}
	if !oneOf(s.Nightmare.Algo, NightmareRandom, NightmareRandomTilt, NightmareTilt) {
		add("invalid nightmare algo %q", s.Nightmare.Algo)
	}
	if err := s.Nightmare.Ranges.validate(); err != nil {
		add("nightmare: %w", err)
	}
	if err := s.Hellgenie.Ranges.validate(); err != nil {
		add("hellgenie: %w", err)
	}
	if s.Distortion.Delay < 0 {
		add("distortion delay must not be negative")
	}
	c := s.Cluster
	if c.ChunkSize < 1 {
		add("cluster chunkSize must be at least 1")
	}
	if c.Modifier < 0 {
		add("cluster modifier must not be negative")
	}
	if !oneOf(c.Method, ClusterRandom, ClusterReverse, ClusterRotateForwards, ClusterRotateBackwards, ClusterOverwrite) {
		add("invalid cluster method %q", c.Method)
	}
	if !oneOf(c.Direction, ClusterForwards, ClusterBackwards) {
		add("invalid cluster direction %q", c.Direction)
	}
	if err := s.Custom.validate(); err != nil {
		add("custom: %w", err)
	}
	if s.GameProtection.IntervalSeconds < 1 {
		add("gameProtection intervalSeconds must be at least 1")
	}
	if s.GameProtection.Keep < 1 {
		add("gameProtection keep must be at least 1")
	}
	return errors.Join(errs...)
}

func (c *CustomSettings) validate() error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	if !oneOf(c.Source, SourceValue, SourceStore) {
		add("invalid source %q", c.Source)
	}
	if !oneOf(c.ValueSource, ValueRandom, ValueFromList, ValueFromRange) {
		add("invalid valueSource %q", c.ValueSource)
	}
	if err := c.Ranges.validate(); err != nil {
		errs = append(errs, err)
	}
	if !oneOf(c.StoreAddress, StoreAddressSame, StoreAddressRandom) {
		add("invalid storeAddress %q", c.StoreAddress)
	}
	if !oneOf(c.StoreTime, StoreImmediate, StorePreExecute) {
		add("invalid storeTime %q", c.StoreTime)
	}
	if !oneOf(c.StoreType, StoreOnce, StoreContinuous) {
		add("invalid storeType %q", c.StoreType)
	}
	if !oneOf(c.LimiterTime, LimiterNone, LimiterGenerate, LimiterPreExecute, LimiterExecute) {
		add("invalid limiterTime %q", c.LimiterTime)
	}
	if !oneOf(c.StoreLimiterSource, LimitAddress, LimitSourceAddress, LimitBoth) {
		add("invalid storeLimiterSource %q", c.StoreLimiterSource)
	}
	if c.Delay < 0 {
		add("delay must not be negative")
	}
	if c.Lifetime < 0 {
		add("lifetime must not be negative")
	}
	return errors.Join(errs...)
}

// infinite reports whether the engine produces units that never expire,
// for which RTCV caps intensity at MaxInfiniteUnits.
func (s *Settings) infinite() bool {
	switch s.Engine {
	case EngineHellgenie, EngineFreeze, EnginePipe:
		return true
	case EngineCustom:
		return s.Custom.Lifetime == 0
	}
	return false
}
