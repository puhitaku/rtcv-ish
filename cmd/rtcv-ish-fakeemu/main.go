// Command rtcv-ish-fakeemu runs the fake emulator for manual and frontend
// testing.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/emu/fake"
	"github.com/puhitaku/rtcv-ish/internal/logging"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:42069", "emulator API listen address")
	rom := flag.String("rom", "", "ROM path to load at start")
	fps := flag.Float64("fps", 60, "frames per second while running")
	logFormat := flag.String("log-format", logging.FormatAuto, "log format: auto, text or json")
	hangROM := flag.String("hang-rom", "", "never answer LoadRom with this path, nor anything after it (for testing hung emulators)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := logging.New(os.Stderr, *logFormat)

	opts := fake.Options{Addr: *listen, ROM: *rom, FrameRate: *fps, Logger: log}
	if *hangROM != "" {
		opts.Hook = func(req *emulatorv1.Request) {
			if req.GetLoadRom().GetPath() == *hangROM {
				log.Warn("hanging on LoadRom", "path", *hangROM)
				select {}
			}
		}
	}
	s, err := fake.New(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rtcv-ish-fakeemu:", err)
		os.Exit(1)
	}
	log.Info("fake emulator listening", "addr", s.Addr())
	select {
	case <-ctx.Done():
	case <-s.Done():
	}
	s.Close()
}
