package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	middleware "github.com/oapi-codegen/nethttp-middleware"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach Flush on the real writer (SSE).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// logRequests logs every request at debug level and recovers from panics.
func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Error("panic in handler", "method", r.Method, "path", r.URL.Path, "panic", v, "stack", string(debug.Stack()))
				if rec.status == 0 {
					writeError(rec, newError(http.StatusInternalServerError, CodeInternal, "internal error: %v", v))
				}
			}
			log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration", time.Since(start))
		}()
		next.ServeHTTP(rec, r)
	})
}

// validateRequests rejects requests that do not match the OpenAPI spec with
// 400 INVALID_ARGUMENT (404 NOT_FOUND for unknown routes).
func validateRequests(spec *openapi3.T, prefix string) func(http.Handler) http.Handler {
	return middleware.OapiRequestValidatorWithOptions(spec, &middleware.Options{
		DoNotValidateServers: true,
		Prefix:               prefix,
		Options: openapi3filter.Options{
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
		},
		ErrorHandlerWithOpts: func(_ context.Context, err error, w http.ResponseWriter, _ *http.Request, opts middleware.ErrorHandlerOpts) {
			switch {
			case opts.StatusCode == http.StatusNotFound:
				writeError(w, newError(http.StatusNotFound, CodeNotFound, "%v", err))
			case opts.StatusCode == http.StatusMethodNotAllowed:
				writeError(w, newError(http.StatusMethodNotAllowed, CodeInvalidArgument, "%v", err))
			default:
				writeError(w, newError(http.StatusBadRequest, CodeInvalidArgument, "%s", validationMessage(err)))
			}
		},
	})
}

// validationMessage turns a validation error into one line without the
// schema dump kin-openapi adds.
func validationMessage(err error) string {
	msg := err.Error()
	var se *openapi3.SchemaError
	if errors.As(err, &se) {
		msg = se.Reason
		if p := se.JSONPointer(); len(p) > 0 {
			msg = "/" + strings.Join(p, "/") + ": " + msg
		}
	}
	var re *openapi3filter.RequestError
	if errors.As(err, &re) {
		if re.Parameter != nil {
			return fmt.Sprintf("parameter %q: %s", re.Parameter.Name, msg)
		}
		if se == nil && re.Err != nil {
			msg = re.Err.Error()
		}
		return "request body: " + msg
	}
	return msg
}
