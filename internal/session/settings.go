package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/puhitaku/rtcv-ish/internal/corrupt"
)

func (s *Session) settingsPath() string { return filepath.Join(s.cfg.DataDir, "settings.json") }

// loadSettings reads settings.json over the defaults. Invalid files are
// ignored.
func (s *Session) loadSettings() *corrupt.Settings {
	def := corrupt.DefaultSettings()
	data, err := os.ReadFile(s.settingsPath())
	if errors.Is(err, os.ErrNotExist) {
		return def
	}
	st := def.Clone()
	if err == nil {
		err = json.Unmarshal(data, st)
	}
	if err == nil {
		err = st.Validate()
	}
	if err != nil {
		s.log.Warn("ignoring settings file", "path", s.settingsPath(), "err", err)
		return def
	}
	st.AutoCorrupt = false
	return st
}

func (s *Session) saveSettingsLocked() error {
	st := s.settings.Clone()
	st.AutoCorrupt = false
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.settingsPath() + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.settingsPath())
}

func (s *Session) Settings() *corrupt.Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings.Clone()
}

// PatchSettings deep-merges a JSON patch into the settings, validates the
// result and persists it.
func (s *Session) PatchSettings(patch []byte) (*corrupt.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.settings.Clone()
	if err := json.Unmarshal(patch, st); err != nil {
		return nil, errorf(KindInvalid, "settings: %v", err)
	}
	if err := st.Validate(); err != nil {
		return nil, errorf(KindInvalid, "settings: %v", err)
	}
	if st.AutoCorrupt && !s.settings.AutoCorrupt {
		s.lastAutoFrame = s.game.Frame
	}
	s.settings = st
	if err := s.saveSettingsLocked(); err != nil {
		s.log.Error("save settings", "err", err)
	}
	s.changed(EventSettings)
	return st.Clone(), nil
}
