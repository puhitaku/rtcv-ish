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
	domains  map[string]Domain
	calls    int
	layer    *Layer

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
	return g, nil
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
	sel := g.selected
	blastIn := func(d Domain, n int) error {
		for range n {
			if err := g.blast(d, randomAddress(g.rng, int64(d.Size)-p)); err != nil {
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
		slices.SortStableFunc(sorted, func(a, b Domain) int { return cmp.Compare(a.Size, b.Size) })
		largest := sorted[len(sorted)-1].Size
		for _, d := range sorted {
			if err := blastIn(d, intensity/int(largest/d.Size)); err != nil {
				return err
			}
		}
	case RadiusProportional:
		var total float64
		for _, d := range sel {
			total += float64(d.Size)
		}
		counts := make([]int, len(sel))
		for i, d := range sel {
			counts[i] = int(math.RoundToEven(float64(intensity) * (float64(d.Size) / total)))
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
// random address in it.
func (g *generator) blastTarget() target {
	d := g.selected[g.rng.IntN(len(g.selected))]
	return target{d, randomAddress(g.rng, int64(d.Size)-1)}
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
	safe, ok := safeAddress(addr, int64(d.Size), p, g.s.Alignment)
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
	safe, ok := safeAddress(addr, int64(d.Size), p, g.s.Alignment)
	if !ok {
		return nil
	}
	return []*Unit{newValueUnit(d.Name, safe, g.s.Hellgenie.Ranges.value(g.rng, p), d.BigEndian, 0)}
}

func (g *generator) distortion(d Domain, addr int64) []*Unit {
	p := g.s.Precision
	safe, ok := safeAddress(addr, int64(d.Size), p, g.s.Alignment)
	if !ok {
		return nil
	}
	return []*Unit{newStoreUnit(StoreOnce, StoreImmediate, d.Name, safe, d.Name, safe, p, d.BigEndian, g.s.Distortion.Delay, 1)}
}

func (g *generator) freeze(d Domain, addr int64) []*Unit {
	p := g.s.Precision
	safe, ok := safeAddress(addr, int64(d.Size), p, g.s.Alignment)
	if !ok {
		return nil
	}
	return []*Unit{newStoreUnit(StoreOnce, StorePreExecute, d.Name, safe, d.Name, safe, p, d.BigEndian, 0, 0)}
}

func (g *generator) pipe(d Domain, addr int64) []*Unit {
	start := g.blastTarget()
	p := g.s.Precision
	safe, ok := safeAddress(addr, int64(d.Size), p, g.s.Alignment)
	src, srcOK := safeAddress(start.addr, int64(start.d.Size), p, g.s.Alignment)
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
	if safe >= size-int64(p) {
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
	if safe > size-int64(p) {
		safe = size - 2*int64(p) + int64(g.s.Alignment)
	}
	if safe < 0 || safe+int64(c.ChunkSize*p) >= size {
		return nil, nil
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
	safe, ok := safeAddress(addr, int64(d.Size), p, g.s.Alignment)
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
			bs := int64(bt.d.Size)
			pp, a := int64(p), int64(g.s.Alignment)
			s := bt.addr - bt.addr%pp + a
			if s > bs-pp {
				s = bs - 2*pp + a
			}
			if s < 0 || s+pp > bs {
				return nil, nil
			}
			u.SourceDomain, u.SourceAddress = bt.d.Name, uint64(s)
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
