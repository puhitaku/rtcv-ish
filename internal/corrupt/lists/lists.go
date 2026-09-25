// Package lists implements RTCV limiter and value lists.
//
// A list file has one hex value per line. Files whose name starts with "_"
// are stored big-endian and flipped on load, so every list holds values in
// the little-endian order that memory is compared in. A "?" stands for a
// wildcard nibble ("??" for a whole byte).
package lists

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sasha-s/go-deadlock"
)

var ErrUnsupported = errors.New("unsupported list type")

// List is an immutable limiter/value list.
type List struct {
	name     string
	hash     string
	wildcard bool
	values   [][]byte
	// masks[i][j] has a bit set for every bit of values[i][j] that must
	// match. Only used by wildcard lists.
	masks [][]byte
	set   map[string]struct{}
}

// Parse builds a list from the contents of a list file. name is the file
// name without extension.
func Parse(name string, data []byte) (*List, error) {
	flip := strings.HasPrefix(name, "_")
	l := &List{name: name}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	first := true
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if first {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		if line == "" {
			continue
		}
		if first && strings.HasPrefix(line, "@") {
			return nil, fmt.Errorf("list %s: %w: %s", name, ErrUnsupported, line)
		}
		first = false
		v, m, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("list %s line %d: %w", name, n, err)
		}
		if flip {
			slices.Reverse(v)
			slices.Reverse(m)
		}
		l.values = append(l.values, v)
		l.masks = append(l.masks, m)
		if slices.ContainsFunc(m, func(b byte) bool { return b != 0xFF }) {
			l.wildcard = true
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("list %s: %w", name, err)
	}
	if len(l.values) == 0 {
		return nil, fmt.Errorf("list %s is empty", name)
	}
	if l.wildcard {
		l.hash = hashOf(l.values, l.masks)
		return l, nil
	}
	l.masks = nil
	l.set = make(map[string]struct{}, len(l.values))
	distinct := l.values[:0]
	for _, v := range l.values {
		if _, ok := l.set[string(v)]; ok {
			continue
		}
		l.set[string(v)] = struct{}{}
		distinct = append(distinct, v)
	}
	l.values = distinct
	l.hash = hashOf(l.values, nil)
	return l, nil
}

func parseLine(s string) (value, mask []byte, err error) {
	if len(s)%2 == 1 {
		s = "0" + s
	}
	value = make([]byte, len(s)/2)
	mask = make([]byte, len(s)/2)
	for i := range value {
		for j := range 2 {
			c := s[2*i+j]
			shift := 4 * (1 - j)
			if c == '?' {
				continue
			}
			n, ok := nibble(c)
			if !ok {
				return nil, nil, fmt.Errorf("invalid hex value %q", s)
			}
			value[i] |= n << shift
			mask[i] |= 0xF << shift
		}
	}
	return value, mask, nil
}

func nibble(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// hashOf reproduces RTCV's list id: Base64(MD5(all values concatenated)).
// A fully wildcarded byte hashes as 69 like RTCV's NullableByteArrayList;
// partial nibble wildcards (not supported by RTCV) hash with the wildcard
// nibble cleared.
func hashOf(values, masks [][]byte) string {
	h := md5.New()
	for i, v := range values {
		b := v
		if masks != nil {
			b = slices.Clone(v)
			for j := range b {
				if masks[i][j] == 0 {
					b[j] = 69
				}
			}
		}
		h.Write(b)
	}
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func (l *List) Name() string { return l.name }

// Hash is RTCV's list id, used by .sks stockpiles to reference lists.
func (l *List) Hash() string { return l.hash }

// Precision is the length of the first entry, as in RTCV.
func (l *List) Precision() int { return len(l.values[0]) }

func (l *List) Len() int { return len(l.values) }

func (l *List) Wildcard() bool { return l.wildcard }

// Entries returns the values as upper-case hex strings in stored (little-
// endian) order, with "?" for wildcard nibbles.
func (l *List) Entries() []string {
	const digits = "0123456789ABCDEF"
	out := make([]string, len(l.values))
	for i, v := range l.values {
		var sb strings.Builder
		for j, b := range v {
			m := byte(0xFF)
			if l.masks != nil {
				m = l.masks[i][j]
			}
			for _, shift := range []uint{4, 0} {
				if m>>shift&0xF == 0 {
					sb.WriteByte('?')
				} else {
					sb.WriteByte(digits[b>>shift&0xF])
				}
			}
		}
		out[i] = sb.String()
	}
	return out
}

// Contains reports whether b (little-endian order) matches an entry of
// the same length.
func (l *List) Contains(b []byte) bool {
	if !l.wildcard {
		_, ok := l.set[string(b)]
		return ok
	}
	for i, v := range l.values {
		if len(v) != len(b) {
			continue
		}
		m := l.masks[i]
		match := true
		for j := range v {
			if b[j]&m[j] != v[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// Random returns a random entry resized to precision bytes: shorter
// entries are left-padded with zeros, longer ones keep their last bytes,
// as RTCV's GetRandomValue does. Wildcard nibbles are filled randomly.
func (l *List) Random(rng *rand.Rand, precision int) []byte {
	i := rng.IntN(len(l.values))
	v := slices.Clone(l.values[i])
	if l.masks != nil {
		for j, m := range l.masks[i] {
			if m != 0xFF {
				v[j] |= byte(rng.IntN(256)) &^ m
			}
		}
	}
	switch {
	case len(v) < precision:
		v = append(make([]byte, precision-len(v)), v...)
	case len(v) > precision:
		v = v[len(v)-precision:]
	}
	return v
}

// Registry holds lists by name. It is safe for concurrent use.
type Registry struct {
	mu    deadlock.RWMutex
	lists map[string]*List
}

func NewRegistry() *Registry {
	return &Registry{lists: make(map[string]*List)}
}

// Load adds every *.txt file in dir. A missing directory is not an error.
// Files that fail to parse are skipped and reported in the returned error.
func (r *Registry) Load(dir string) error {
	paths, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil {
		return err
	}
	slices.Sort(paths)
	var errs []error
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		name := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		if _, err := r.Add(name, data); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Add parses data and registers it under name, replacing any list with
// the same name.
func (r *Registry) Add(name string, data []byte) (*List, error) {
	l, err := Parse(name, data)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.lists[name] = l
	r.mu.Unlock()
	return l, nil
}

func (r *Registry) Remove(name string) {
	r.mu.Lock()
	delete(r.lists, name)
	r.mu.Unlock()
}

// Get returns the list registered under name. A nil registry has no lists.
func (r *Registry) Get(name string) (*List, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	l, ok := r.lists[name]
	return l, ok
}

// ByHash finds a list by its RTCV hash.
func (r *Registry) ByHash(hash string) (*List, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, name := range r.namesLocked() {
		if l := r.lists[name]; l.hash == hash {
			return l, true
		}
	}
	return nil, false
}

// List returns all lists sorted by name.
func (r *Registry) List() []*List {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*List, 0, len(r.lists))
	for _, name := range r.namesLocked() {
		out = append(out, r.lists[name])
	}
	return out
}

func (r *Registry) namesLocked() []string {
	names := make([]string, 0, len(r.lists))
	for name := range r.lists {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
