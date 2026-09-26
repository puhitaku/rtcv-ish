// Command rtcv-ish is the rtcv-ish core server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/puhitaku/rtcv-ish/internal/emulators"
	"github.com/puhitaku/rtcv-ish/internal/logging"
	"github.com/puhitaku/rtcv-ish/internal/server"
	"github.com/puhitaku/rtcv-ish/internal/version"
	"github.com/puhitaku/rtcv-ish/internal/webui"
)

const shutdownTimeout = 5 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "rtcv-ish:", err)
		}
		os.Exit(1)
	}
}

type config struct {
	listen    string
	dataDir   string
	emulator  string
	seed      int64
	logFormat string
	melonDS   string
}

// melonDSEnv sets the default of --melonds; the e2e tests use it too.
const melonDSEnv = "RTCVISH_MELONDS"

func parseFlags(args []string, output io.Writer) (*config, error) {
	var cfg config
	fs := flag.NewFlagSet("rtcv-ish", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.StringVar(&cfg.listen, "listen", "127.0.0.1:8420", "HTTP listen address")
	fs.StringVar(&cfg.dataDir, "data-dir", "", "data directory (default: data next to the executable)")
	fs.StringVar(&cfg.emulator, "emulator", "", "emulator API address to connect to at start")
	fs.Int64Var(&cfg.seed, "seed", 0, "random seed (0: time-based)")
	fs.StringVar(&cfg.logFormat, "log-format", logging.FormatAuto, "log format: auto, text or json")
	fs.StringVar(&cfg.melonDS, "melonds", os.Getenv(melonDSEnv), "melonDS executable or .app bundle to launch (env "+melonDSEnv+")")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if err := logging.ValidFormat(cfg.logFormat); err != nil {
		return nil, err
	}
	if cfg.dataDir == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("locate executable: %w", err)
		}
		cfg.dataDir = filepath.Join(filepath.Dir(exe), "data")
	}
	if cfg.seed == 0 {
		cfg.seed = time.Now().UnixNano()
	}
	return &cfg, nil
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cfg, err := parseFlags(args, stderr)
	if err != nil {
		return err
	}
	log := logging.New(stderr, cfg.logFormat)

	if err := os.MkdirAll(cfg.dataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	ver := version.Get()
	log.Info("starting", "version", ver.String(), "data_dir", cfg.dataDir, "seed", cfg.seed)

	specs, err := launchableEmulators(cfg.melonDS, log)
	if err != nil {
		return err
	}

	core, err := server.New(ctx, server.Config{
		DataDir:   cfg.dataDir,
		Seed:      cfg.seed,
		Logger:    log,
		Emulators: specs,
		Version:   ver.String(),
		VersionInfo: server.VersionInfo{
			Release: ver.Release,
			Commit:  ver.Commit,
			Dirty:   ver.Dirty,
			Kind:    ver.Kind,
		},
	}, server.WithStatic(webui.FS()))
	if err != nil {
		return err
	}
	defer core.Close()

	if cfg.emulator != "" {
		if _, err := core.Session().Connect(ctx, cfg.emulator); err != nil {
			log.Error("cannot connect to emulator", "addr", cfg.emulator, "err", err)
		}
	}

	ln, err := net.Listen("tcp", cfg.listen)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           core.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	log.Info(fmt.Sprintf("open %s in a browser", browserURL(ln.Addr())))

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return core.Close()
}

// melonDSArgs makes a launched melonDS listen on the port the core picks.
var melonDSArgs = []string{"--rtcvish-listen", "127.0.0.1:{port}"}

// launchableEmulators lists the emulators the core can launch. override is
// an explicit melonDS path; otherwise the bundle next to the executable
// and then a development build in the repository are used.
func launchableEmulators(override string, log *slog.Logger) ([]server.EmulatorSpec, error) {
	var exeDir string
	if exe, err := os.Executable(); err != nil {
		log.Warn("cannot locate executable; no bundled emulators", "err", err)
	} else {
		if p, err := filepath.EvalSymlinks(exe); err == nil {
			exe = p
		}
		exeDir = filepath.Dir(exe)
	}
	cwd, err := os.Getwd()
	if err != nil {
		log.Debug("cannot get working directory", "err", err)
	}
	path, source, err := emulators.FindMelonDS(runtime.GOOS, cwd, exeDir, override, log)
	if err != nil {
		return nil, fmt.Errorf("--melonds/%s: %w", melonDSEnv, err)
	}
	if source == emulators.SourceNone {
		log.Info("melonDS not found; Launch is disabled", "path", path)
	} else {
		log.Info("using melonDS", "source", source, "path", path)
	}
	if path == "" {
		return nil, nil
	}
	return []server.EmulatorSpec{{Name: "melonDS", Path: path, Args: melonDSArgs}}, nil
}

func browserURL(addr net.Addr) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "http://" + addr.String()
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}
