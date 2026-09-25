package lists

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		data      string
		precision int
		entries   []string
		hit, miss [][]byte
	}{
		{
			name:      "floats",
			data:      "3F800000\r\n\n40000000\n3f800000\n",
			precision: 4,
			entries:   []string{"3F800000", "40000000"},
			hit:       [][]byte{{0x3F, 0x80, 0x00, 0x00}},
			miss:      [][]byte{{0x00, 0x00, 0x80, 0x3F}, {0x3F, 0x80, 0x00}},
		},
		{
			name:      "_big",
			data:      "3F800000\n0102\n",
			precision: 4,
			entries:   []string{"0000803F", "0201"},
			hit:       [][]byte{{0x00, 0x00, 0x80, 0x3F}, {0x02, 0x01}},
			miss:      [][]byte{{0x3F, 0x80, 0x00, 0x00}},
		},
		{
			name:      "odd",
			data:      "abc\n",
			precision: 2,
			entries:   []string{"0ABC"},
			hit:       [][]byte{{0x0A, 0xBC}},
		},
		{
			name:      "wild",
			data:      "12??\n3?4?\n",
			precision: 2,
			entries:   []string{"12??", "3?4?"},
			hit:       [][]byte{{0x12, 0x00}, {0x12, 0xFF}, {0x35, 0x4A}},
			miss:      [][]byte{{0x13, 0x00}, {0x45, 0x40}, {0x12}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := Parse(tt.name, []byte(tt.data))
			if err != nil {
				t.Fatal(err)
			}
			if l.Precision() != tt.precision {
				t.Errorf("precision = %d, want %d", l.Precision(), tt.precision)
			}
			if got := l.Entries(); !equalStrings(got, tt.entries) {
				t.Errorf("entries = %v, want %v", got, tt.entries)
			}
			for _, b := range tt.hit {
				if !l.Contains(b) {
					t.Errorf("Contains(%X) = false", b)
				}
			}
			for _, b := range tt.miss {
				if l.Contains(b) {
					t.Errorf("Contains(%X) = true", b)
				}
			}
			if l.Hash() == "" {
				t.Error("empty hash")
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseErrors(t *testing.T) {
	for name, data := range map[string]string{
		"bitlogic": "@BitlogicListFilter\n1010\n",
		"empty":    "\n\n",
		"bad":      "12\nzz\n",
	} {
		_, err := Parse(name, []byte(data))
		if err == nil {
			t.Errorf("%s: no error", name)
		}
		if name == "bitlogic" && !errors.Is(err, ErrUnsupported) {
			t.Errorf("bitlogic: err = %v", err)
		}
	}
}

// The hash matches RTCV's Base64(MD5(bytes)): "01" + "02" -> MD5(0x01 0x02).
func TestHash(t *testing.T) {
	l, _ := Parse("x", []byte("01\n02\n01\n"))
	if got, want := l.Hash(), "DLmI0EKn8o3V/itVs/Wseg=="; got != want {
		t.Errorf("hash = %s, want %s", got, want)
	}
	w, _ := Parse("w", []byte("??\n"))
	if got, want := w.Hash(), "Oj6gDPw1Myzt9uXpoy6U2g=="; got != want {
		t.Errorf("wildcard hash = %s, want %s (MD5 of byte 69)", got, want)
	}
}

func TestRandom(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	l, _ := Parse("x", []byte("11223344\n"))
	if got := l.Random(rng, 4); !bytes.Equal(got, []byte{0x11, 0x22, 0x33, 0x44}) {
		t.Errorf("Random(4) = %X", got)
	}
	if got := l.Random(rng, 6); !bytes.Equal(got, []byte{0, 0, 0x11, 0x22, 0x33, 0x44}) {
		t.Errorf("Random(6) = %X", got)
	}
	if got := l.Random(rng, 2); !bytes.Equal(got, []byte{0x33, 0x44}) {
		t.Errorf("Random(2) = %X", got)
	}
	w, _ := Parse("w", []byte("1?\n"))
	for range 50 {
		if got := w.Random(rng, 1); got[0]&0xF0 != 0x10 {
			t.Fatalf("wildcard Random = %X", got)
		}
	}
}

func TestRegistry(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("01\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "_a.txt"), []byte("0102\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "bad.txt"), []byte("@BitlogicListFilter\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "skip.cet"), []byte("01\n"), 0o644)
	r := NewRegistry()
	if err := r.Load(dir); err == nil {
		t.Error("Load: want error for bad.txt")
	}
	all := r.List()
	if len(all) != 2 || all[0].Name() != "_a" || all[1].Name() != "b" {
		t.Fatalf("List = %v", all)
	}
	a, ok := r.Get("_a")
	if !ok || !a.Contains([]byte{0x02, 0x01}) {
		t.Error("Get(_a) not flipped")
	}
	if got, ok := r.ByHash(a.Hash()); !ok || got != a {
		t.Error("ByHash failed")
	}
	r.Remove("_a")
	if _, ok := r.Get("_a"); ok {
		t.Error("Remove failed")
	}
	var nilReg *Registry
	if _, ok := nilReg.Get("x"); ok {
		t.Error("nil registry has lists")
	}
}
