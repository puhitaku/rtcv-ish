package emu

import (
	"errors"
	"fmt"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
)

// Error is an Error response sent by the emulator.
type Error struct {
	Code    emulatorv1.Error_Code
	Message string
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("emulator: %s", e.Code)
	}
	return fmt.Sprintf("emulator: %s: %s", e.Code, e.Message)
}

// Is matches another *Error with the same code, so the sentinels below
// work with errors.Is.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// Sentinels for errors.Is.
var (
	ErrInvalidArgument = &Error{Code: emulatorv1.Error_INVALID_ARGUMENT}
	ErrNotFound        = &Error{Code: emulatorv1.Error_NOT_FOUND}
	ErrOutOfRange      = &Error{Code: emulatorv1.Error_OUT_OF_RANGE}
	ErrNoROM           = &Error{Code: emulatorv1.Error_NO_ROM}
	ErrUnsupported     = &Error{Code: emulatorv1.Error_UNSUPPORTED}
	ErrFailed          = &Error{Code: emulatorv1.Error_FAILED}
	ErrBusy            = &Error{Code: emulatorv1.Error_BUSY}
)

// ErrClosed is returned by calls on a client whose connection is gone.
var ErrClosed = errors.New("emu: connection closed")

// ErrProtocol wraps violations of the wire protocol by the emulator.
var ErrProtocol = errors.New("emu: protocol error")

// CodeOf returns the emulator error code carried by err, if any.
func CodeOf(err error) (emulatorv1.Error_Code, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Code, true
	}
	return 0, false
}

func newError(e *emulatorv1.Error) *Error {
	return &Error{Code: e.GetCode(), Message: e.GetMessage()}
}
