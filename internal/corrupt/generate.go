package corrupt

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"slices"

	"github.com/puhitaku/rtcv-ish/internal/corrupt/lists"
)

var ErrNoDomains = errors.New("no domains selected")

type generator struct {
	ctx      context.Context
	rng      *rand.Rand
	s        *Settings
	mem      Memory
	selected []Domain
	// usable are the selected domains whose address window (see
	// AddressRange) fits a unit; all selected domains without a range.
	usable  []Domain
	domains map[string]Domain
	calls   int
	layer   *Layer

	limiter *lists.List
	values  *lists.List
}

// Generate builds a blast layer like RTCV's RtcCore.GenerateBlastLayer:
// it picks Intensity (domain, address) pairs from the selected domains
// according to the radius and asks the engine for units at each. Engines
// may return nothing for a pair (limiter miss, address outside the domain),
// so the layer can have fewer units than the intensity.
func Generate(ctx context.Context, rng *rand.Rand, s *Settings, selected []string, mem Memory, reg *lists.Registry) (*Layer, error) {
	g, err := newGenerator(ctx, rng, s, selected, mem, reg)
	if err != nil {
		return nil, err
	}
	if len(g.usable) == 0 {
		slog.InfoContext(ctx, "address range does not intersect any selected domain", "start", s.AddressRange.Start, "end", s.AddressRange.End)
		return g.layer, nil
	}
	intensity := s.Intensity
	if s.infinite() {
		intensity = min(intensity, s.MaxInfiniteUnits)
	}
	if err := g.run(intensity); err != nil {
		return nil, err
	}
	return g.layer, nil
}

func newGenerator(ctx context.Context, rng *rand.Rand, s *Settings, selected []string, mem Memory, reg *lists.Registry) (*generator, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	g := &generator{ctx: ctx, rng: rng, s: s, mem: mem, domains: make(map[string]Domain), layer: &Layer{}}
	for _, d := range mem.Domains() {
		g.domains[d.Name] = d
	}
	if len(selected) == 0 {
		return nil, ErrNoDomains
	}
	for _, name := range selected {
		d, ok := g.domains[name]
		if !ok {
			return nil, fmt.Errorf("%w %q", ErrUnknownDomain, name)
		}
		if !d.Writable {
			return nil, fmt.Errorf("domain %q is read-only", name)
		}
		if d.Size == 0 || d.Size > math.MaxInt64 {
			return nil, fmt.Errorf("domain %q has unusable size %d", name, d.Size)
		}
		g.selected = append(g.selected, d)
	}
	if err := g.resolveLists(reg); err != nil {
		return nil, err
	}
	g.usable = usableDomains(s, g.selected, g.unitSize())
	return g, nil
}

// unitSize is the number of bytes one generated unit (or cluster chunk)
// covers.
func (g *generator) unitSize() int {
	switch {
	case g.s.Engine == EngineVector && !g.s.Vector.UnlockPrecision:
		return g.limiter.Precision()
	case g.s.Engine == EngineCluster:
		return g.limiter.Precision() * g.s.Cluster.ChunkSize
	}
	return g.s.Precision
}

// usableDomains filters the domains to those whose address window holds
// at least n bytes. Without an address range every domain is usable.
func usableDomains(s *Settings, domains []Domain, n int) []Domain {
	if !s.AddressRange.Enabled {
		return domains
	}
	var out []Domain
	for _, d := range domains {
		lo, hi := s.AddressRange.window(d.Size)
		if hi-lo < int64(n) {
			slog.Debug("domain outside the address range", "domain", d.Name, "size", d.Size, "start", s.AddressRange.Start, "end", s.AddressRange.End)
			continue
		}
		out = append(out, d)
	}
	return out
}

// AddressRangeMisses reports whether the address range is enabled and no
// selected domain has room for a unit inside it, in which case Generate
// returns an empty layer.
func AddressRangeMisses(s *Settings, selected []string, mem Memory) bool {
	if !s.AddressRange.Enabled {
		return false
	}
	var doms []Domain
	for _, name := range selected {
		if d, ok := findDomain(mem, name); ok {
			doms = append(doms, d)
		}
	}
	return len(usableDomains(s, doms, s.Precision)) == 0
}

// window is the address window of d: [0, size) or its intersection with
// the address range.
func (g *generator) window(d Domain) (lo, hi int64) { return g.s.AddressRange.window(d.Size) }

// align is safeAddress for a unit of p bytes. With an address range the
// result stays inside the domain's window.
func (g *generator) align(d Domain, addr int64, p int) (uint64, bool) {
	if !g.s.AddressRange.Enabled {
		return safeAddress(addr, int64(d.Size), p, g.s.Alignment)
	}
	lo, hi := g.window(d)
	return alignIn(addr, lo, hi, int64(p), int64(g.s.Alignment), int64(p))
}

// alignIn aligns addr like safeAddress (down to p, plus a) and then moves
// it by whole multiples of p until [s, s+n) lies within [lo, hi): up if
// it starts below lo, down if it ends past hi.
func alignIn(addr, lo, hi, p, a, n int64) (uint64, bool) {
	s := addr - addr%p + a
	if s < lo {
		s += (lo - s + p - 1) / p * p
	}
	if s+n > hi {
		s -= (s + n - hi + p - 1) / p * p
	}
	return uint64(max(s, 0)), s >= lo && s+n <= hi
}

func (g *generator) resolveLists(reg *lists.Registry) error {
	get := func(what, name string) (*lists.List, error) {
		if name == "" {
			return nil, fmt.Errorf("%s: no list selected", what)
		}
		l, ok := reg.Get(name)
		if !ok {
			return nil, fmt.Errorf("%s: list %q not found", what, name)
		}
		return l, nil
	}
	var err error
	switch g.s.Engine {
	case EngineVector:
		if g.limiter, err = get("vector limiter", g.s.Vector.LimiterList); err != nil {
			return err
		}
		g.values, err = get("vector value list", g.s.Vector.ValueList)
	case EngineCluster:
		g.limiter, err = get("cluster limiter", g.s.Cluster.LimiterList)
	case EngineCustom:
		c := g.s.Custom
		if c.LimiterTime != LimiterNone {
			if g.limiter, err = get("custom limiter", c.LimiterList); err != nil {
				return err
			}
			if c.LimiterTime != LimiterGenerate {
				slog.DebugContext(g.ctx, "custom engine limiter is checked at generation time", "limiterTime", c.LimiterTime)
			}
		}
		if c.Source == SourceValue && c.ValueSource == ValueFromList {
			g.values, err = get("custom value list", c.ValueList)
		}
	}
	return err
}

func (g *generator) run(intensity int) error {
	p := int64(g.s.Precision)
	sel := g.usable
	// size is the window size, the domain size without an address range.
	size := func(d Domain) uint64 {
		lo, hi := g.window(d)
		return uint64(hi - lo)
	}
	blastIn := func(d Domain, n int) error {
		lo, hi := g.window(d)
		for range n {
			if err := g.blast(d, lo+randomAddress(g.rng, hi-lo-p)); err != nil {
				return err
			}
		}
		return nil
	}
	switch g.s.Radius {
	case RadiusSpread:
		for range intensity {
			if err := blastIn(sel[g.rng.IntN(len(sel))], 1); err != nil {
				return err
			}
		}
	case RadiusChunk:
		return blastIn(sel[g.rng.IntN(len(sel))], intensity)
	case RadiusBurst:
		for range 10 {
			if err := blastIn(sel[g.rng.IntN(len(sel))], intensity/10); err != nil {
				return err
			}
		}
	case RadiusNormalized:
		sorted := slices.Clone(sel)
		slices.SortStableFunc(sorted, func(a, b Domain) int { return cmp.Compare(size(a), size(b)) })
		largest := size(sorted[len(sorted)-1])
		for _, d := range sorted {
			if err := blastIn(d, intensity/int(largest/size(d))); err != nil {
				return err
			}
		}
	case RadiusProportional:
		var total float64
		for _, d := range sel {
			total += float64(size(d))
		}
		counts := make([]int, len(sel))
		for i, d := range sel {
			counts[i] = int(math.RoundToEven(float64(intensity) * (float64(size(d)) / total)))
		}
		for i, d := range sel {
			if err := blastIn(d, counts[i]); err != nil {
				return err
			}
		}
	case RadiusEven:
		for _, d := range sel {
			if err := blastIn(d, intensity/len(sel)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *generator) blast(d Domain, addr int64) error {
	if g.calls++; g.calls%256 == 0 {
		if err := g.ctx.Err(); err != nil {
			return err
		}
	}
	var units []*Unit
	var err error
	switch g.s.Engine {
	case EngineNightmare:
		units = g.nightmare(d, addr)
	case EngineHellgenie:
		units = g.hellgenie(d, addr)
	case EngineDistortion:
		units = g.distortion(d, addr)
	case EngineFreeze:
		units = g.freeze(d, addr)
	case EnginePipe:
		units = g.pipe(d, addr)
	case EngineVector:
		units, err = g.vector(d, addr)
	case EngineCluster:
		units, err = g.cluster(d, addr)
	case EngineCustom:
		units, err = g.custom(d, addr)
	}
	g.layer.Units = append(g.layer.Units, units...)
	return err
}

// safeAddress is the alignment rule shared by the engines: align down to
// the precision, add the alignment, and if that runs past the end use the
// last aligned address. ok is false if the result still does not fit
// (RTCV would poke out of range and have the pokes ignored).
func safeAddress(addr, size int64, precision, alignment int) (uint64, bool) {
	p, a := int64(precision), int64(alignment)
	s := addr - addr%p + a
	if s > size-p && size > p {
		s = size - 2*p + a
	}
	return uint64(s), s >= 0 && s+p <= size
}

type target struct {
	d    Domain
	addr int64
}

// blastTarget is RTCV's GetBlastTarget: a random selected domain and a
// random address in it (in its address window).
func (g *generator) blastTarget() target {
	d := g.usable[g.rng.IntN(len(g.usable))]
	lo, hi := g.window(d)
	return target{d, lo + randomAddress(g.rng, hi-lo-1)}
}

func (g *generator) nightmare(d Domain, addr int64) []*Unit {
	const (
		set = iota
		add
		sub
	)
	typ := set
	switch g.s.Nightmare.Algo {
	case NightmareRandomTilt:
		typ = [...]int{add, sub, set}[g.rng.IntN(3)]
	case NightmareTilt:
		typ = [...]int{add, sub}[g.rng.IntN(2)]
	}
	p := g.s.Precision
	safe, ok := g.align(d, addr, p)
	if !ok {
		return nil
	}
	if typ == set {
		return []*Unit{newValueUnit(d.Name, safe, g.s.Nightmare.Ranges.value(g.rng, p), d.BigEndian, 1)}
	}
	u := newStoreUnit(StoreOnce, StorePreExecute, d.Name, safe, d.Name, safe, p, d.BigEndian, 0, 1)
	u.Tilt = bigOne(typ == sub)
	return []*Unit{u}
}

func (g *generator) hellgenie(d Domain, addr int64) []*Unit {
	p := g.s.Precision
	safe, ok := g.align(d, addr, p)
	if !ok {
		return nil
	}
	return []*Unit{newValueUnit(d.Name, safe, g.s.Hellgenie.Ranges.value(g.rng, p), d.BigEndian, 0)}
}

func (g *generator) distortion(d Domain, addr int64) []*Unit {
	p := g.s.Precision
	safe, ok := g.align(d, addr, p)
	if !ok {
		return nil
	}
	return []*Unit{newStoreUnit(StoreOnce, StoreImmediate, d.Name, safe, d.Name, safe, p, d.BigEndian, g.s.Distortion.Delay, 1)}
}

func (g *generator) freeze(d Domain, addr int64) []*Unit {
	p := g.s.Precision
	safe, ok := g.align(d, addr, p)
	if !ok {
		return nil
	}
	return []*Unit{newStoreUnit(StoreOnce, StorePreExecute, d.Name, safe, d.Name, safe, p, d.BigEndian, 0, 0)}
}

func (g *generator) pipe(d Domain, addr int64) []*Unit {
	start := g.blastTarget()
	p := g.s.Precision
	safe, ok := g.align(d, addr, p)
	src, srcOK := g.align(start.d, start.addr, p)
	if !ok || !srcOK {
		return nil
	}
	return []*Unit{newStoreUnit(StoreContinuous, StorePreExecute, d.Name, safe, start.d.Name, src, p, d.BigEndian, 0, 0)}
}

// peekLE reads n bytes at addr in little-endian order, as RTCV compares
// memory against lists. ok is false if the range leaves the domain.
func (g *generator) peekLE(d Domain, addr int64, n int) ([]byte, bool, error) {
	if addr < 0 || !d.contains(uint64(addr), n) {
		return nil, false, nil
	}
	b, err := g.mem.Read(g.ctx, d.Name, uint64(addr), n)
	if err != nil {
		return nil, false, err
	}
	if d.BigEndian {
		slices.Reverse(b)
	}
	return b, true, nil
}

func (g *generator) limited(l *lists.List, d Domain, addr int64, n int) (bool, error) {
	b, ok, err := g.peekLE(d, addr, n)
	return ok && l.Contains(b), err
}

func (g *generator) vector(d Domain, addr int64) ([]*Unit, error) {
	p := g.limiter.Precision()
	if g.s.Vector.UnlockPrecision {
		p = g.s.Precision
	}
	size := int64(d.Size)
	safe := addr - addr%int64(p) + int64(g.s.Alignment)
	if g.s.AddressRange.Enabled {
		lo, hi := g.window(d)
		s, ok := alignIn(addr, lo, hi, int64(p), int64(g.s.Alignment), int64(p))
		if !ok {
			return nil, nil
		}
		safe = int64(s)
	} else if safe >= size-int64(p) {
		safe = size - 2*int64(p) + int64(g.s.Alignment)
	}
	hit, err := g.limited(g.limiter, d, safe, p)
	if err != nil || !hit {
		return nil, err
	}
	u := newValueUnit(d.Name, uint64(safe), g.values.Random(g.rng, p), d.BigEndian, 1)
	u.GeneratedUsingValueList = true
	return []*Unit{u}, nil
}

func (g *generator) cluster(d Domain, addr int64) ([]*Unit, error) {
	c := g.s.Cluster
	p := g.limiter.Precision()
	size := int64(d.Size)
	safe := addr - addr%int64(p) + int64(g.s.Alignment)
	if g.s.AddressRange.Enabled {
		lo, hi := g.window(d)
		s, ok := alignIn(addr, lo, hi, int64(p), int64(g.s.Alignment), int64(c.ChunkSize*p))
		if !ok {
			return nil, nil
		}
		safe = int64(s)
	} else {
		if safe > size-int64(p) {
			safe = size - 2*int64(p) + int64(g.s.Alignment)
		}
		if safe < 0 || safe+int64(c.ChunkSize*p) >= size {
			return nil, nil
		}
	}
	raw, err := g.mem.Read(g.ctx, d.Name, uint64(safe), c.ChunkSize*p)
	if err != nil {
		return nil, err
	}
	segs := make([][]byte, c.ChunkSize)
	for i := range segs {
		segs[i] = raw[i*p : (i+1)*p]
	}
	matches := func(i int) bool {
		b := slices.Clone(segs[i])
		if d.BigEndian {
			slices.Reverse(b)
		}
		return g.limiter.Contains(b)
	}
	src := 0
	if c.Direction == ClusterBackwards {
		src = c.ChunkSize - 1
	}
	if c.FilterAll {
		for i := range segs {
			if !matches(i) {
				return nil, nil
			}
		}
	} else if !matches(src) {
		return nil, nil
	}
	shuffleCluster(g.rng, segs, c.Method, c.Modifier, src)
	if !c.SplitUnits {
		return []*Unit{newValueUnit(d.Name, uint64(safe), slices.Concat(segs...), false, 1)}, nil
	}
	units := make([]*Unit, len(segs))
	for i, s := range segs {
		units[i] = newValueUnit(d.Name, uint64(safe)+uint64(i*p), slices.Clone(s), false, 1)
	}
	return units, nil
}

func shuffleCluster(rng *rand.Rand, segs [][]byte, method ClusterMethod, modifier, src int) {
	n := len(segs)
	switch method {
	case ClusterRotateForwards:
		for range modifier {
			last := segs[n-1]
			copy(segs[1:], segs[:n-1])
			segs[0] = last
		}
	case ClusterRotateBackwards:
		for range modifier {
			first := segs[0]
			copy(segs, segs[1:])
			segs[n-1] = first
		}
	case ClusterReverse:
		slices.Reverse(segs)
	case ClusterOverwrite:
		s := segs[src]
		for i := range segs {
			segs[i] = s
		}
	default:
		for i := n - 1; i > 0; i-- {
			k := rng.IntN(i + 1)
			segs[k], segs[i] = segs[i], segs[k]
		}
	}
}

func (g *generator) custom(d Domain, addr int64) ([]*Unit, error) {
	c := g.s.Custom
	p := g.s.Precision
	safe, ok := g.align(d, addr, p)
	if !ok {
		return nil, nil
	}
	u := NewUnit()
	switch c.Source {
	case SourceValue:
		switch c.ValueSource {
		case ValueFromList:
			u.Value = g.values.Random(g.rng, p)
		case ValueFromRange:
			u.Value = c.Ranges.value(g.rng, p)
		default:
			u.Value = randomBytes(g.rng, p)
		}
	case SourceStore:
		u.StoreType = c.StoreType
		u.StoreTime = c.StoreTime
		if c.StoreAddress == StoreAddressRandom {
			bt := g.blastTarget()
			s, ok := g.align(bt.d, bt.addr, p)
			if !ok {
				return nil, nil
			}
			u.SourceDomain, u.SourceAddress = bt.d.Name, s
		} else {
			u.SourceDomain, u.SourceAddress = d.Name, safe
		}
	}
	u.Precision = p
	u.Address = safe
	u.Domain = d.Name
	u.Source = c.Source
	u.ExecuteFrame = c.Delay
	u.Lifetime = c.Lifetime
	u.LimiterTime = c.LimiterTime
	u.Loop = c.Loop
	u.InvertLimiter = c.LimiterInverted
	if c.Tilt != nil && c.Tilt.Sign() != 0 {
		u.Tilt = cloneBig(c.Tilt)
	}
	u.GeneratedUsingValueList = c.Source == SourceValue && c.ValueSource == ValueFromList
	u.BigEndian = d.BigEndian
	if c.LimiterTime == LimiterNone {
		return []*Unit{u}, nil
	}
	u.LimiterList = c.LimiterList
	// The emulator cannot evaluate lists, so every limiter time is checked
	// here, at generation (see design/architecture.md).
	pass, err := g.limiterCheck(u, c.StoreLimiterSource)
	if err != nil || !pass {
		return nil, err
	}
	return []*Unit{u}, nil
}

// limiterCheck is BlastUnit.LimiterCheck: the unit passes if its target
// (or, for STORE units, the configured side) matches the limiter list,
// inverted by InvertLimiter.
func (g *generator) limiterCheck(u *Unit, src StoreLimiterSource) (bool, error) {
	matched := false
	var err error
	if u.Source != SourceStore || src != LimitSourceAddress {
		matched, err = g.limited(g.limiter, g.domains[u.Domain], int64(u.Address), u.Precision)
		if err != nil {
			return false, err
		}
	}
	if !matched && u.Source == SourceStore && src != LimitAddress {
		sd, ok := g.domains[u.SourceDomain]
		if ok {
			if matched, err = g.limited(g.limiter, sd, int64(u.SourceAddress), u.Precision); err != nil {
				return false, err
			}
		}
	}
	return matched != u.InvertLimiter, nil
}
