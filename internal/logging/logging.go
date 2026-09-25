// Package logging builds the slog loggers used by rtcv-ish.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/lmittmann/tint"
	"golang.org/x/term"
)

// Formats accepted by New.
const (
	FormatAuto = "auto"
	FormatText = "text"
	FormatJSON = "json"
)

// New returns a logger writing to w. Format "auto" selects colored text
// when w is a terminal and JSON otherwise.
func New(w io.Writer, format string) *slog.Logger {
	return NewLevel(w, format, slog.LevelInfo)
}

// NewLevel is New with a minimum level.
func NewLevel(w io.Writer, format string, level slog.Leveler) *slog.Logger {
	tty := isTerminal(w)
	switch format {
	case FormatText:
		return slog.New(tint.NewHandler(w, &tint.Options{Level: level, TimeFormat: time.TimeOnly, NoColor: !tty}))
	case FormatJSON:
		return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
	default:
		if tty {
			return slog.New(tint.NewHandler(w, &tint.Options{Level: level, TimeFormat: time.TimeOnly}))
		}
		return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
	}
}

// ValidFormat reports an error for unknown format names.
func ValidFormat(format string) error {
	switch format {
	case FormatAuto, FormatText, FormatJSON:
		return nil
	}
	return fmt.Errorf("unknown log format %q (want auto, text or json)", format)
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}
