package corrupt

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/puhitaku/rtcv-ish/internal/corrupt/lists"
)

func seeded() *rand.Rand { return rand.New(rand.NewPCG(1, 2)) }

// fakeMem is an in-memory Memory that records its ReadMany calls.
type fakeMem struct {
	domains []Domain
	data    map[string][]byte
	calls   [][]Range
}

func newFakeMem(domains ...Domain) *fakeMem {
	m := &fakeMem{domains: domains, data: make(map[string][]byte)}
	for _, d := range domains {
		m.data[d.Name] = make([]byte, d.Size)
	}
	return m
}

func (m *fakeMem) Domains() []Domain { return m.domains }

func (m *fakeMem) Read(ctx context.Context, domain string, addr uint64, size int) ([]byte, error) {
	out, err := m.ReadMany(ctx, []Range{{domain, addr, size}})
	if err != nil {
		return nil, err
	}
	return out[0], nil
}

func (m *fakeMem) ReadMany(_ context.Context, ranges []Range) ([][]byte, error) {
	m.calls = append(m.calls, append([]Range(nil), ranges...))
	out := make([][]byte, len(ranges))
	for i, r := range ranges {
		b, ok := m.data[r.Domain]
		if !ok {
			return nil, fmt.Errorf("%w %q", ErrUnknownDomain, r.Domain)
		}
		if r.Address+uint64(r.Size) > uint64(len(b)) {
			return nil, ErrOutOfRange
		}
		out[i] = append([]byte(nil), b[r.Address:r.Address+uint64(r.Size)]...)
	}
	return out, nil
}

func (m *fakeMem) put(domain string, addr uint64, b ...byte) {
	copy(m.data[domain][addr:], b)
}

func dom(name string, size uint64) Domain {
	return Domain{Name: name, Size: size, WordSize: 4, Writable: true}
}

func registry(t *testing.T, files map[string]string) *lists.Registry {
	t.Helper()
	r := lists.NewRegistry()
	for name, data := range files {
		if _, err := r.Add(name, []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func mustGenerate(t *testing.T, s *Settings, selected []string, mem Memory, reg *lists.Registry) *Layer {
	t.Helper()
	l, err := Generate(t.Context(), seeded(), s, selected, mem, reg)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func randPCG(seed uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, 2)) }

func jsonMarshal(v any) ([]byte, error)      { return json.Marshal(v) }
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
