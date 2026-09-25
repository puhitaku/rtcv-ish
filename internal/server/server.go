// Package server serves the Frontend API (api/frontend/openapi.yaml), the
// SSE event stream and the embedded web frontend.
package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/puhitaku/rtcv-ish/internal/server/gen"
	"github.com/puhitaku/rtcv-ish/internal/session"
)

// EmulatorSpec is a bundled emulator the core can launch.
type EmulatorSpec = session.EmulatorSpec

type Config struct {
	DataDir   string
	Seed      int64
	Logger    *slog.Logger
	Emulators []EmulatorSpec
	// Version is reported in Status.version; empty means "dev".
	Version string
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
	sess   *session.Session
	static fs.FS

	ctx    context.Context
	cancel context.CancelFunc
}

func New(ctx context.Context, cfg Config, opts ...Option) (*Server, error) {
	if cfg.DataDir == "" {
		return nil, fmt.Errorf("server: DataDir is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	ctx, cancel := context.WithCancel(ctx)
	sess, err := session.New(ctx, session.Config{
		DataDir:   cfg.DataDir,
		Seed:      cfg.Seed,
		Logger:    cfg.Logger,
		Emulators: cfg.Emulators,
		Version:   cfg.Version,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("server: %w", err)
	}
	s := &Server{
		cfg:    cfg,
		log:    cfg.Logger,
		sess:   sess,
		ctx:    ctx,
		cancel: cancel,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Session is the coordinator behind the API.
func (s *Server) Session() *session.Session { return s.sess }

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

// staticHandler serves the web frontend. Unknown paths get index.html so
// that client-side routes work.
func (s *Server) staticHandler() http.Handler {
	if s.static != nil {
		fsys := s.static
		files := http.FileServerFS(fsys)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
			if name == "" {
				name = "."
			}
			if _, err := fs.Stat(fsys, name); errors.Is(err, fs.ErrNotExist) {
				http.ServeFileFS(w, r, fsys, "index.html")
				return
			}
			files.ServeHTTP(w, r)
		})
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
	return s.sess.Close()
}
