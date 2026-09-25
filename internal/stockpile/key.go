// Package stockpile stores Glitch Harvester data: stash keys, savestate
// blobs, the stash history, the stockpile, savestate slots, game protection
// backups and .sks files.
package stockpile

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"slices"
	"time"

	"github.com/puhitaku/rtcv-ish/internal/corrupt"
)

var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid")
)

// GameInfo identifies the game a key was made on.
type GameInfo struct {
	Title   string `json:"title"`
	Code    string `json:"code"`
	RomPath string `json:"romPath"`
	System  string `json:"system"`
}

// StashKey is one glitch: a savestate reference, a blast layer and the
// game identity (RTCV's StashKey). A savestate key has Key == ParentKey.
type StashKey struct {
	Key             string         `json:"key"`
	ParentKey       string         `json:"parentKey"`
	Alias           string         `json:"alias"`
	Note            string         `json:"note"`
	Game            GameInfo       `json:"game"`
	SelectedDomains []string       `json:"selectedDomains"`
	UnitCount       int            `json:"unitCount"`
	Layer           *corrupt.Layer `json:"layer,omitempty"`
	CreatedAt       time.Time      `json:"createdAt"`
}

// NewKey returns a key based on the savestate key parent, holding layer.
func NewKey(key string, parent *StashKey, domains []string, layer *corrupt.Layer, now time.Time) *StashKey {
	k := &StashKey{
		Key:             key,
		ParentKey:       parent.ParentKey,
		Alias:           key,
		Game:            parent.Game,
		SelectedDomains: slices.Clone(domains),
		CreatedAt:       now.UTC(),
	}
	k.SetLayer(layer)
	return k
}

func (k *StashKey) SetLayer(l *corrupt.Layer) {
	k.Layer = l
	k.UnitCount = 0
	if l != nil {
		k.UnitCount = len(l.Units)
	}
}

func (k *StashKey) Clone() *StashKey {
	c := *k
	c.SelectedDomains = slices.Clone(k.SelectedDomains)
	if k.Layer != nil {
		c.Layer = k.Layer.Clone()
	}
	return &c
}

// Summary is the key without its layer.
func (k *StashKey) Summary() *StashKey {
	c := *k
	c.Layer = nil
	c.SelectedDomains = slices.Clone(k.SelectedDomains)
	return &c
}

func (k *StashKey) MarshalJSON() ([]byte, error) {
	type key StashKey
	c := key(*k)
	if c.SelectedDomains == nil {
		c.SelectedDomains = []string{}
	}
	return json.Marshal(c)
}

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func (k *StashKey) validate() error {
	if !keyPattern.MatchString(k.Key) {
		return fmt.Errorf("%w: key %q", ErrInvalid, k.Key)
	}
	if !keyPattern.MatchString(k.ParentKey) {
		return fmt.Errorf("%w: parentKey %q of key %s", ErrInvalid, k.ParentKey, k.Key)
	}
	if k.Layer != nil {
		if err := k.Layer.Validate(); err != nil {
			return fmt.Errorf("%w: key %s: %v", ErrInvalid, k.Key, err)
		}
	}
	return nil
}

const keyAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

func randomKey(rng *rand.Rand) string {
	b := make([]byte, 10)
	for i := range b {
		b[i] = keyAlphabet[rng.IntN(len(keyAlphabet))]
	}
	return string(b)
}
