package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/emu"
	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

// Error codes returned in gen.Error.Code. Emulator error codes are passed
// through by name (see emuCodes).
const (
	CodeInvalidArgument      = "INVALID_ARGUMENT"
	CodeOutOfRange           = "OUT_OF_RANGE"
	CodeNotFound             = "NOT_FOUND"
	CodeNoROM                = "NO_ROM"
	CodeBusy                 = "BUSY"
	CodeUnsupported          = "UNSUPPORTED"
	CodeAlreadyConnected     = "ALREADY_CONNECTED"
	CodeNoBackup             = "NO_BACKUP"
	CodeConnectFailed        = "CONNECT_FAILED"
	CodeFailed               = "FAILED"
	CodeEmulatorDisconnected = "EMULATOR_DISCONNECTED"
	CodeNotImplemented       = "NOT_IMPLEMENTED"
	CodeInternal             = "INTERNAL"
)

// apiError is an error with an HTTP status and an API error code.
type apiError struct {
	Status int
	Code   string
	Msg    string
}

func (e *apiError) Error() string { return e.Msg }

func newError(status int, code, format string, args ...any) *apiError {
	return &apiError{Status: status, Code: code, Msg: fmt.Sprintf(format, args...)}
}

var (
	errNotImplemented = newError(http.StatusNotImplemented, CodeNotImplemented, "not implemented")
	errDisconnected   = newError(http.StatusServiceUnavailable, CodeEmulatorDisconnected, "no emulator connected")
)

var emuCodes = map[emulatorv1.Error_Code]struct {
	status int
	code   string
}{
	emulatorv1.Error_INVALID_ARGUMENT: {http.StatusBadRequest, CodeInvalidArgument},
	emulatorv1.Error_OUT_OF_RANGE:     {http.StatusBadRequest, CodeOutOfRange},
	emulatorv1.Error_NOT_FOUND:        {http.StatusNotFound, CodeNotFound},
	emulatorv1.Error_NO_ROM:           {http.StatusConflict, CodeNoROM},
	emulatorv1.Error_BUSY:             {http.StatusConflict, CodeBusy},
	emulatorv1.Error_UNSUPPORTED:      {http.StatusConflict, CodeUnsupported},
	emulatorv1.Error_FAILED:           {http.StatusBadGateway, CodeFailed},
	emulatorv1.Error_UNKNOWN:          {http.StatusBadGateway, CodeFailed},
}

// toAPIError maps any error to an apiError.
func toAPIError(err error) *apiError {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae
	}
	var ee *emu.Error
	if errors.As(err, &ee) {
		if m, ok := emuCodes[ee.Code]; ok {
			return &apiError{Status: m.status, Code: m.code, Msg: ee.Error()}
		}
		return &apiError{Status: http.StatusBadGateway, Code: CodeFailed, Msg: ee.Error()}
	}
	if errors.Is(err, emu.ErrClosed) {
		return &apiError{Status: errDisconnected.Status, Code: errDisconnected.Code, Msg: err.Error()}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &apiError{Status: http.StatusGatewayTimeout, Code: CodeInternal, Msg: err.Error()}
	}
	return &apiError{Status: http.StatusInternalServerError, Code: CodeInternal, Msg: err.Error()}
}

// writeError writes err as Error JSON.
func writeError(w http.ResponseWriter, err error) {
	ae := toAPIError(err)
	writeJSON(w, ae.Status, gen.Error{Error: ae.Msg, Code: ae.Code})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
