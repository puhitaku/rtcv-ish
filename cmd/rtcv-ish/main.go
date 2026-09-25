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
	"syscall"
	"time"

	"github.com/puhitaku/rtcv-ish/internal/emu"
	"github.com/puhitaku/rtcv-ish/internal/logging"
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
}

func parseFlags(args []string, output io.Writer) (*config, error) {
	var cfg config
	fs := flag.NewFlagSet("rtcv-ish", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.StringVar(&cfg.listen, "listen", "127.0.0.1:8420", "HTTP listen address")
	fs.StringVar(&cfg.dataDir, "data-dir", "", "data directory (default: data next to the executable)")
	fs.StringVar(&cfg.emulator, "emulator", "", "emulator API address to connect to at start")
	fs.Int64Var(&cfg.seed, "seed", 0, "random seed (0: time-based)")
	fs.StringVar(&cfg.logFormat, "log-format", logging.FormatAuto, "log format: auto, text or json")
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
	log.Info("starting", "data_dir", cfg.dataDir, "seed", cfg.seed)

	if cfg.emulator != "" {
		c, err := emu.Dial(ctx, cfg.emulator, emu.WithLogger(log))
		if err != nil {
			log.Error("cannot connect to emulator", "addr", cfg.emulator, "err", err)
		} else {
			defer c.Close()
			info := c.Info()
			log.Info("connected to emulator", "addr", cfg.emulator, "emulator", info.GetEmulator(), "version", info.GetVersion(), "system", info.GetSystem())
		}
	}

	ln, err := net.Listen("tcp", cfg.listen)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, "rtcv-ish\n")
	})
	srv := &http.Server{
		Handler:           mux,
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
	return nil
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
