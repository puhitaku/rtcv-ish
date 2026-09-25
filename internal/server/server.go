// Package server serves the Frontend API (api/frontend/openapi.yaml), the
// SSE event stream and the embedded web frontend.
package server

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"

	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

// Version is reported in Status.version.
var Version = "dev"

// EmulatorSpec is a bundled emulator the core can launch.
type EmulatorSpec struct {
	Name string
	Path string
	// Args are passed to the executable. "{port}" in an argument is
	// replaced with the emulator API port the core picked.
	Args []string
}

type Config struct {
	DataDir   string
	Seed      int64
	Logger    *slog.Logger
	Emulators []EmulatorSpec
}

// Option configures New.
type Option func(*Server)

// WithStatic serves the web frontend from fsys at "/".
func WithStatic(fsys fs.FS) Option {
	return func(s *Server) { s.static = fsys }
}

type Server struct {
	cfg    Config
	log    *slog.Logger
	rng    *rand.Rand
	static fs.FS

	ctx    context.Context
	cancel context.CancelFunc
}

func New(ctx context.Context, cfg Config, opts ...Option) (*Server, error) {
	if cfg.DataDir == "" {
		return nil, fmt.Errorf("server: DataDir is required")
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("server: create data dir: %w", err)
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Server{
		cfg:    cfg,
		log:    cfg.Logger,
		rng:    rand.New(rand.NewPCG(uint64(cfg.Seed), 0)),
		ctx:    ctx,
		cancel: cancel,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Handler returns the HTTP handler: the API under /api, the SSE stream at
// /api/events and the web frontend at /.
func (s *Server) Handler() http.Handler {
	spec, err := gen.GetSwagger()
	if err != nil {
		panic(fmt.Sprintf("server: embedded OpenAPI spec: %v", err))
	}
	apiMux := http.NewServeMux()
	strict := gen.NewStrictHandlerWithOptions(&api{s: s}, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeError(w, newError(http.StatusBadRequest, CodeInvalidArgument, "%v", err))
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeError(w, err)
		},
	})
	gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{
		BaseURL:    "/api",
		BaseRouter: apiMux,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeError(w, newError(http.StatusBadRequest, CodeInvalidArgument, "%v", err))
		},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events", s.serveEvents)
	mux.Handle("/api/", validateRequests(spec, "/api")(apiMux))
	mux.Handle("/", s.staticHandler())
	return logRequests(s.log, mux)
}

func (s *Server) staticHandler() http.Handler {
	if s.static != nil {
		return http.FileServerFS(s.static)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, "rtcv-ish: web frontend not embedded\n")
	})
}

// Close stops background work and drops the emulator connection.
func (s *Server) Close() error {
	s.cancel()
	return nil
}
