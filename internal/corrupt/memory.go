package corrupt

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/sasha-s/go-deadlock"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/emu"
)

// MaxReadRequest is the largest number of bytes read in one request.
const MaxReadRequest = 1 << 20

var (
	ErrUnknownDomain = errors.New("unknown domain")
	ErrOutOfRange    = errors.New("range outside domain")
)

type Domain struct {
	Name      string `json:"name"`
	Size      uint64 `json:"size"`
	WordSize  int    `json:"wordSize"`
	BigEndian bool   `json:"bigEndian"`
	Writable  bool   `json:"writable"`
	Hidden    bool   `json:"hidden"`
}

func (d Domain) contains(addr uint64, size int) bool {
	return size >= 0 && addr <= d.Size && uint64(size) <= d.Size-addr
}

func DomainFromProto(d *emulatorv1.Domain) Domain {
	return Domain{
		Name:      d.GetName(),
		Size:      d.GetSize(),
		WordSize:  int(d.GetWordSize()),
		BigEndian: d.GetBigEndian(),
		Writable:  d.GetWritable(),
		Hidden:    d.GetHidden(),
	}
}

type Range struct {
	Domain  string
	Address uint64
	Size    int
}

// Memory is read access to an emulator's memory domains.
type Memory interface {
	Domains() []Domain
	Read(ctx context.Context, domain string, addr uint64, size int) ([]byte, error)
	// ReadMany returns one byte slice per range, in order.
	ReadMany(ctx context.Context, ranges []Range) ([][]byte, error)
}

func findDomain(mem Memory, name string) (Domain, bool) {
	for _, d := range mem.Domains() {
		if d.Name == name {
			return d, true
		}
	}
	return Domain{}, false
}

type emuMemory struct {
	c       *emu.Client
	domains []Domain
}

// EmuMemory reads memory from an emulator. domains is the emulator's
// domain list (from ListDomains).
func EmuMemory(c *emu.Client, domains []*emulatorv1.Domain) Memory {
	m := &emuMemory{c: c}
	for _, d := range domains {
		m.domains = append(m.domains, DomainFromProto(d))
	}
	return m
}

// LoadEmuMemory lists the emulator's domains and returns EmuMemory.
func LoadEmuMemory(ctx context.Context, c *emu.Client) (Memory, error) {
	ds, err := c.ListDomains(ctx)
	if err != nil {
		return nil, err
	}
	return EmuMemory(c, ds), nil
}

func (m *emuMemory) Domains() []Domain { return m.domains }

func (m *emuMemory) Read(ctx context.Context, domain string, addr uint64, size int) ([]byte, error) {
	out, err := m.ReadMany(ctx, []Range{{Domain: domain, Address: addr, Size: size}})
	if err != nil {
		return nil, err
	}
	return out[0], nil
}

// ReadMany splits the ranges into requests of at most MaxReadRequest bytes.
func (m *emuMemory) ReadMany(ctx context.Context, ranges []Range) ([][]byte, error) {
	return readBatched(ctx, ranges, MaxReadRequest, func(ctx context.Context, rs []Range) ([][]byte, error) {
		req := make([]*emulatorv1.Range, len(rs))
		for i, r := range rs {
			req[i] = &emulatorv1.Range{Domain: r.Domain, Address: r.Address, Size: uint32(r.Size)}
		}
		return m.c.Read(ctx, req)
	})
}

// readBatched splits ranges into pieces and batches of at most limit bytes,
// calls read for each batch and reassembles the results.
func readBatched(ctx context.Context, ranges []Range, limit int, read func(context.Context, []Range) ([][]byte, error)) ([][]byte, error) {
	out := make([][]byte, len(ranges))
	type piece struct{ index, offset int }
	var batch []Range
	var pieces []piece
	total := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		data, err := read(ctx, batch)
		if err != nil {
			return err
		}
		if len(data) != len(batch) {
			return fmt.Errorf("read returned %d ranges, want %d", len(data), len(batch))
		}
		for i, p := range pieces {
			if len(data[i]) != batch[i].Size {
				return fmt.Errorf("read returned %d bytes, want %d", len(data[i]), batch[i].Size)
			}
			copy(out[p.index][p.offset:], data[i])
		}
		batch, pieces, total = batch[:0], pieces[:0], 0
		return nil
	}
	for i, r := range ranges {
		if r.Size < 0 {
			return nil, fmt.Errorf("%w: negative size", ErrOutOfRange)
		}
		out[i] = make([]byte, r.Size)
		for off := 0; off < r.Size; {
			if total == limit {
				if err := flush(); err != nil {
					return nil, err
				}
			}
			n := min(r.Size-off, limit-total)
			batch = append(batch, Range{Domain: r.Domain, Address: r.Address + uint64(off), Size: n})
			pieces = append(pieces, piece{i, off})
			total += n
			off += n
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return out, nil
}

// Snapshot caches whole domains of an underlying Memory. A domain is read
// once, in requests of at most MaxReadRequest bytes, the first time it is
// accessed (or by Load), and every later read is served from the copy.
// It is meant for generating many units against a paused emulator.
type Snapshot struct {
	mem  Memory
	mu   deadlock.Mutex
	data map[string][]byte
}

func NewSnapshot(mem Memory) *Snapshot {
	return &Snapshot{mem: mem, data: make(map[string][]byte)}
}

func (s *Snapshot) Domains() []Domain { return s.mem.Domains() }

// Load reads the named domains now.
func (s *Snapshot) Load(ctx context.Context, names ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(ctx, names)
}

func (s *Snapshot) loadLocked(ctx context.Context, names []string) error {
	var ranges []Range
	var todo []string
	for _, name := range names {
		if _, ok := s.data[name]; ok || slices.Contains(todo, name) {
			continue
		}
		d, ok := findDomain(s.mem, name)
		if !ok {
			return fmt.Errorf("%w %q", ErrUnknownDomain, name)
		}
		for off := uint64(0); off < d.Size; off += MaxReadRequest {
			ranges = append(ranges, Range{Domain: name, Address: off, Size: int(min(MaxReadRequest, d.Size-off))})
		}
		todo = append(todo, name)
	}
	if len(ranges) == 0 {
		for _, name := range todo {
			s.data[name] = []byte{}
		}
		return nil
	}
	data, err := readBatched(ctx, ranges, MaxReadRequest, s.mem.ReadMany)
	if err != nil {
		return err
	}
	for i, r := range ranges {
		s.data[r.Domain] = append(s.data[r.Domain], data[i]...)
	}
	for _, name := range todo {
		if s.data[name] == nil {
			s.data[name] = []byte{}
		}
	}
	return nil
}

func (s *Snapshot) Read(ctx context.Context, domain string, addr uint64, size int) ([]byte, error) {
	out, err := s.ReadMany(ctx, []Range{{Domain: domain, Address: addr, Size: size}})
	if err != nil {
		return nil, err
	}
	return out[0], nil
}

func (s *Snapshot) ReadMany(ctx context.Context, ranges []Range) ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, 4)
	for _, r := range ranges {
		names = append(names, r.Domain)
	}
	if err := s.loadLocked(ctx, names); err != nil {
		return nil, err
	}
	out := make([][]byte, len(ranges))
	for i, r := range ranges {
		b := s.data[r.Domain]
		if r.Size < 0 || r.Address > uint64(len(b)) || uint64(r.Size) > uint64(len(b))-r.Address {
			return nil, fmt.Errorf("%w: %s+0x%x size %d", ErrOutOfRange, r.Domain, r.Address, r.Size)
		}
		out[i] = append([]byte(nil), b[r.Address:r.Address+uint64(r.Size)]...)
	}
	return out, nil
}
