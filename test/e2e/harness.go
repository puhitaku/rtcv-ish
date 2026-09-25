// Package e2e tests the core against a real emulator build.
//
// Set RTCVISH_MELONDS to the melonDS executable (or its .app bundle on
// macOS). RTCVISH_ROM_DIR overrides the test ROM directory, which defaults
// to test/roms. Missing default ROMs are built once with
// scripts/build-nds-examples.sh.
package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/puhitaku/rtcv-ish/internal/emu"
)

const (
	dialTimeout = 15 * time.Second
	stopTimeout = 5 * time.Second
)

// melonDSConfig mutes audio. [Instance0] must be declared explicitly: melonDS
// fails to save a config where it is only an implicit parent table.
const melonDSConfig = `LimitFPS = true

[3D]
Renderer = 0

[Screen]
UseGL = false

[Instance0]

[Instance0.Audio]
Volume = 0
`

// Emulator is a melonDS process with a connected client.
type Emulator struct {
	*emu.Client
	Addr   string
	cmd    *exec.Cmd
	output *syncBuffer
	exited chan struct{}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// MelonDSPath returns the melonDS executable from RTCVISH_MELONDS, or skips
// the test.
func MelonDSPath(t testing.TB) string {
	t.Helper()
	p := os.Getenv("RTCVISH_MELONDS")
	if p == "" {
		t.Skip("RTCVISH_MELONDS is not set; set it to a melonDS build with the rtcv-ish API to run e2e tests")
	}
	if strings.HasSuffix(strings.TrimSuffix(p, "/"), ".app") {
		p = filepath.Join(p, "Contents", "MacOS", "melonDS")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("RTCVISH_MELONDS: %v", err)
	}
	return p
}

var (
	buildROMsOnce   sync.Once
	buildROMsOutput []byte
	buildROMsErr    error
)

// ROM returns the path of a test ROM. When the default ROM directory lacks
// it, scripts/build-nds-examples.sh is run once per test binary.
func ROM(t testing.TB, name string) string {
	t.Helper()
	dir := os.Getenv("RTCVISH_ROM_DIR")
	if dir == "" {
		dir = filepath.Join(repoRoot(t), "test", "roms")
	}
	p, err := filepath.Abs(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err == nil {
		return p
	}
	if os.Getenv("RTCVISH_ROM_DIR") != "" {
		t.Fatalf("test ROM missing from RTCVISH_ROM_DIR: %s", p)
	}
	buildROMsOnce.Do(func() {
		t.Logf("test ROM %s missing; running scripts/build-nds-examples.sh", name)
		cmd := exec.Command(filepath.Join(repoRoot(t), "scripts", "build-nds-examples.sh"))
		cmd.Dir = repoRoot(t)
		buildROMsOutput, buildROMsErr = cmd.CombinedOutput()
		t.Logf("build-nds-examples.sh output:\n%s", buildROMsOutput)
	})
	if buildROMsErr != nil {
		t.Fatalf("scripts/build-nds-examples.sh failed: %v\n%s", buildROMsErr, buildROMsOutput)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("test ROM missing after scripts/build-nds-examples.sh: %v", err)
	}
	return p
}

func repoRoot(t testing.TB) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the repository")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func freePort(t testing.TB) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// StartMelonDS launches melonDS with the API enabled and connects to it.
// rom is booted from the command line when not empty. The process is
// killed when the test ends.
func StartMelonDS(t testing.TB, rom string) *Emulator {
	t.Helper()
	exe := MelonDSPath(t)

	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "melonDS.toml"), []byte(melonDSConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	args := []string{"--rtcvish-listen", addr, "--rtcvish-config-dir", configDir}
	if rom != "" {
		args = append(args, rom)
	}

	e := &Emulator{Addr: addr, output: &syncBuffer{}, exited: make(chan struct{})}
	e.cmd = exec.Command(exe, args...)
	e.cmd.Dir = configDir
	e.cmd.Env = append(os.Environ(), "SDL_AUDIODRIVER=dummy")
	if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		e.cmd.Env = append(e.cmd.Env, "QT_QPA_PLATFORM=offscreen")
	}
	e.cmd.Stdout = e.output
	e.cmd.Stderr = e.output
	if err := e.cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", exe, err)
	}
	var waitErr error
	go func() {
		waitErr = e.cmd.Wait()
		close(e.exited)
	}()
	t.Cleanup(func() {
		if e.Client != nil {
			e.Client.Close()
		}
		e.stop()
		if t.Failed() {
			t.Logf("melonDS %v output:\n%s", args, e.output.String())
		}
	})

	deadline := time.Now().Add(dialTimeout)
	var lastErr error
	for {
		select {
		case <-e.exited:
			t.Fatalf("melonDS exited before accepting connections: %v", waitErr)
		default:
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		c, err := emu.Dial(ctx, addr)
		cancel()
		if err == nil {
			e.Client = c
			return e
		}
		lastErr = err
		if time.Now().After(deadline) {
			t.Fatalf("cannot connect to melonDS at %s within %s: %v\n(is this a build of the rtcv-ish fork with the API?)", addr, dialTimeout, lastErr)
		}
		select {
		case <-e.exited:
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// Exited is closed when the process has exited.
func (e *Emulator) Exited() <-chan struct{} { return e.exited }

func (e *Emulator) stop() {
	select {
	case <-e.exited:
		return
	default:
	}
	e.cmd.Process.Kill()
	select {
	case <-e.exited:
	case <-time.After(stopTimeout):
	}
}

// LoadROM loads a test ROM and fails the test on error.
func (e *Emulator) LoadROM(t testing.TB, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := e.LoadRom(ctx, ROM(t, name)); err != nil {
		t.Fatalf("LoadRom(%s): %v", name, err)
	}
}

var errNotExited = errors.New("process did not exit")

// WaitExit waits for the process to exit on its own.
func (e *Emulator) WaitExit(timeout time.Duration) error {
	select {
	case <-e.exited:
		return nil
	case <-time.After(timeout):
		return errNotExited
	}
}
