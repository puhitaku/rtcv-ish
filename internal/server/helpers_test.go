package server_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/puhitaku/rtcv-ish/internal/emu/fake"
	"github.com/puhitaku/rtcv-ish/internal/server"
	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

const (
	testSeed = 42
	testROM  = "/roms/hello.nds"
	// testTitle is what the fake emulator derives from testROM.
	testTitle = "HELLO"
	// eventTimeout bounds every wait for an SSE event.
	eventTimeout = 10 * time.Second
)

// Fake emulator domains (fake.DefaultDomains): MainRAM (64 KiB, the
// frame counter lives in its first 4 bytes), VRAM (16 KiB) and ARM7WRAM
// (8 KiB, hidden). Tests probe VRAM because nothing else writes to it.
const (
	mainRAM  = "MainRAM"
	vram     = "VRAM"
	arm7WRAM = "ARM7WRAM"
	vramSize = 16 << 10
)

type envOptions struct {
	dataDir   string
	seed      int64
	auto      bool // run the fake emulator in real time instead of Manual mode
	noConnect bool
	noROM     bool
	fake      fake.Options
	emulators []server.EmulatorSpec
	version   string
	opts      []server.Option
}

// env is one core server with a fake emulator behind it.
type env struct {
	t       *testing.T
	ctx     context.Context
	fake    *fake.Server
	srv     *server.Server
	http    *httptest.Server
	c       *gen.ClientWithResponses
	dataDir string
}

func newEnv(t *testing.T, o envOptions) *env {
	t.Helper()
	if o.dataDir == "" {
		o.dataDir = t.TempDir()
	}
	if o.seed == 0 {
		o.seed = testSeed
	}
	e := &env{t: t, ctx: t.Context(), dataDir: o.dataDir}

	if !o.noConnect {
		fo := o.fake
		fo.Manual = !o.auto
		f, err := fake.New(fo)
		if err != nil {
			t.Fatalf("fake.New: %v", err)
		}
		t.Cleanup(func() { f.Close() })
		e.fake = f
	}

	log := slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv, err := server.New(t.Context(), server.Config{
		DataDir:   o.dataDir,
		Seed:      o.seed,
		Logger:    log,
		Emulators: o.emulators,
		Version:   o.version,
	}, o.opts...)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	e.srv = srv
	e.http = httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		e.http.Close()
		srv.Close()
	})
	e.c, err = gen.NewClientWithResponses(e.http.URL + "/api")
	if err != nil {
		t.Fatal(err)
	}

	if !o.noConnect {
		e.connect(e.fake.Addr())
		if !o.noROM {
			e.loadROM(testROM)
		}
	}
	return e
}

// newFake starts a Manual-mode fake emulator for the rest of the test.
func newFake(t *testing.T) *fake.Server {
	t.Helper()
	f, err := fake.New(fake.Options{Manual: true})
	if err != nil {
		t.Fatalf("fake.New: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

type response interface {
	StatusCode() int
	Bytes() []byte
}

func expectStatus(t testing.TB, r response, err error, want int) {
	t.Helper()
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if got := r.StatusCode(); got != want {
		t.Fatalf("HTTP %d, want %d; body: %s", got, want, r.Bytes())
	}
}

// expectError checks for an Error JSON response with the status and code.
func expectError(t testing.TB, r response, err error, wantStatus int, wantCode string) {
	t.Helper()
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	var e gen.Error
	if jerr := json.Unmarshal(r.Bytes(), &e); jerr != nil {
		t.Fatalf("HTTP %d, want %d %s; body is not Error JSON (%v): %s", r.StatusCode(), wantStatus, wantCode, jerr, r.Bytes())
	}
	if r.StatusCode() != wantStatus || e.Code != wantCode {
		t.Fatalf("HTTP %d %s, want %d %s; body: %s", r.StatusCode(), e.Code, wantStatus, wantCode, r.Bytes())
	}
	if e.Error == "" {
		t.Errorf("error message is empty: %s", r.Bytes())
	}
}

func (e *env) connect(addr string) gen.Status {
	e.t.Helper()
	r, err := e.c.ConnectEmulatorWithResponse(e.ctx, gen.ConnectRequest{Address: addr})
	expectStatus(e.t, r, err, http.StatusOK)
	return *r.JSON200
}

func (e *env) loadROM(path string) gen.GameStatus {
	e.t.Helper()
	r, err := e.c.LoadRomWithResponse(e.ctx, gen.PathRequest{Path: path})
	expectStatus(e.t, r, err, http.StatusOK)
	return *r.JSON200
}

func (e *env) status() gen.Status {
	e.t.Helper()
	r, err := e.c.GetStatusWithResponse(e.ctx)
	expectStatus(e.t, r, err, http.StatusOK)
	return *r.JSON200
}

func (e *env) settings() gen.Settings {
	e.t.Helper()
	r, err := e.c.GetSettingsWithResponse(e.ctx)
	expectStatus(e.t, r, err, http.StatusOK)
	return *r.JSON200
}

func (e *env) patchSettings(p gen.SettingsPatch) gen.Settings {
	e.t.Helper()
	r, err := e.c.PatchSettingsWithResponse(e.ctx, p)
	expectStatus(e.t, r, err, http.StatusOK)
	return *r.JSON200
}

func (e *env) domains() []gen.Domain {
	e.t.Helper()
	r, err := e.c.ListDomainsWithResponse(e.ctx)
	expectStatus(e.t, r, err, http.StatusOK)
	return *r.JSON200
}

func (e *env) selectDomains(names ...string) {
	e.t.Helper()
	if names == nil {
		names = []string{} // the spec requires an array, not null
	}
	r, err := e.c.SetSelectedDomainsWithResponse(e.ctx, gen.NamesRequest{Names: names})
	expectStatus(e.t, r, err, http.StatusOK)
}

func (e *env) readMem(domain string, addr, size int64) string {
	e.t.Helper()
	r, err := e.c.ReadMemoryWithResponse(e.ctx, domain, &gen.ReadMemoryParams{Address: addr, Size: size})
	expectStatus(e.t, r, err, http.StatusOK)
	return r.JSON200.Data
}

func (e *env) writeMem(domain string, addr int64, data string) {
	e.t.Helper()
	r, err := e.c.WriteMemoryWithResponse(e.ctx, domain, gen.MemoryWrite{Address: addr, Data: data})
	expectStatus(e.t, r, err, http.StatusNoContent)
}

func (e *env) blast() gen.Layer {
	e.t.Helper()
	r, err := e.c.ManualBlastWithResponse(e.ctx)
	expectStatus(e.t, r, err, http.StatusOK)
	return *r.JSON200
}

func (e *env) applyLayer(l gen.Layer, backup bool) {
	e.t.Helper()
	r, err := e.c.ApplyLayerWithResponse(e.ctx, gen.ApplyRequest{Layer: l, Backup: backup})
	expectStatus(e.t, r, err, http.StatusNoContent)
}

func (e *env) units() []gen.EmuUnit {
	e.t.Helper()
	r, err := e.c.ListUnitsWithResponse(e.ctx)
	expectStatus(e.t, r, err, http.StatusOK)
	return *r.JSON200
}

func (e *env) clearUnits() {
	e.t.Helper()
	r, err := e.c.ClearUnitsWithResponse(e.ctx)
	expectStatus(e.t, r, err, http.StatusNoContent)
}

func (e *env) savestates() []gen.SavestateSlot {
	e.t.Helper()
	r, err := e.c.ListSavestatesWithResponse(e.ctx)
	expectStatus(e.t, r, err, http.StatusOK)
	return *r.JSON200
}

func (e *env) saveSlot(slot int) gen.SavestateSlot {
	e.t.Helper()
	r, err := e.c.SaveSavestateWithResponse(e.ctx, slot)
	expectStatus(e.t, r, err, http.StatusOK)
	return *r.JSON200
}

func (e *env) corrupt(slot int) gen.StashKey {
	e.t.Helper()
	r, err := e.c.CorruptStashWithResponse(e.ctx, gen.CorruptRequest{Slot: slot, LoadBefore: true})
	expectStatus(e.t, r, err, http.StatusOK)
	return *r.JSON200
}

func (e *env) stash() []gen.StashKey {
	e.t.Helper()
	r, err := e.c.ListStashWithResponse(e.ctx)
	expectStatus(e.t, r, err, http.StatusOK)
	return *r.JSON200
}

func (e *env) stockpile() []gen.StashKey {
	e.t.Helper()
	r, err := e.c.ListStockpileWithResponse(e.ctx)
	expectStatus(e.t, r, err, http.StatusOK)
	return *r.JSON200
}

func (e *env) stashLayer(key string) gen.Layer {
	e.t.Helper()
	r, err := e.c.GetStashLayerWithResponse(e.ctx, key)
	expectStatus(e.t, r, err, http.StatusOK)
	return *r.JSON200
}

func (e *env) stockpileLayer(key string) gen.Layer {
	e.t.Helper()
	r, err := e.c.GetStockpileLayerWithResponse(e.ctx, key)
	expectStatus(e.t, r, err, http.StatusOK)
	return *r.JSON200
}

func (e *env) toStockpile(key string) gen.StashKey {
	e.t.Helper()
	r, err := e.c.StashKeyToStockpileWithResponse(e.ctx, key, gen.ToStockpileRequest{})
	expectStatus(e.t, r, err, http.StatusOK)
	return *r.JSON200
}

// useNightmare selects plain random Nightmare with n units of precision 1
// on VRAM only.
func (e *env) useNightmare(n int64) {
	e.t.Helper()
	e.selectDomains(vram)
	e.patchSettings(gen.SettingsPatch{
		Engine:    ptr(gen.EngineNightmare),
		Intensity: ptr(n),
		Precision: ptr(gen.PrecisionN1),
		Alignment: ptr(0),
		Radius:    ptr(gen.BlastRadiusSpread),
		Nightmare: &gen.NightmareSettingsPatch{Algo: ptr(gen.NightmareAlgoRandom)},
	})
}

func ptr[T any](v T) *T { return &v }

func keysOf(ks []gen.StashKey) []string {
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = k.Key
	}
	return out
}

func valueUnit(domain string, addr int64, value string) gen.Unit {
	return gen.Unit{
		Enabled:     true,
		Domain:      domain,
		Address:     addr,
		Precision:   len(value) / 2,
		Source:      gen.UnitSourceValue,
		Value:       value,
		StoreTime:   gen.StoreTimeImmediate,
		StoreType:   gen.StoreTypeOnce,
		Tilt:        "0",
		Lifetime:    1,
		LimiterTime: gen.LimiterTimeNone,
	}
}

func storeUnit(domain string, addr int64, precision int, srcDomain string, srcAddr int64, st gen.StoreTime, typ gen.StoreType) gen.Unit {
	return gen.Unit{
		Enabled:       true,
		Domain:        domain,
		Address:       addr,
		Precision:     precision,
		Source:        gen.UnitSourceStore,
		SourceDomain:  srcDomain,
		SourceAddress: srcAddr,
		StoreTime:     st,
		StoreType:     typ,
		Tilt:          "0",
		Lifetime:      1,
		LimiterTime:   gen.LimiterTimeNone,
	}
}

// multipartBody builds a multipart/form-data body with one file field
// "file" and optional extra fields.
func multipartBody(t testing.TB, filename string, content []byte, fields map[string]string) (string, io.Reader) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(content)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return w.FormDataContentType(), &buf
}

func (e *env) uploadList(filename, content string, fields map[string]string) gen.ListInfo {
	e.t.Helper()
	ct, body := multipartBody(e.t, filename, []byte(content), fields)
	r, err := e.c.UploadListWithBodyWithResponse(e.ctx, ct, body)
	expectStatus(e.t, r, err, http.StatusOK)
	return *r.JSON200
}

// ---- SSE ----

type sseEvent struct {
	Type string
	Data json.RawMessage
}

type sseStream struct {
	events chan sseEvent
	errc   chan error
}

// openEvents subscribes to /api/events for the rest of the test.
func (e *env) openEvents() *sseStream {
	e.t.Helper()
	req, err := http.NewRequestWithContext(e.ctx, http.MethodGet, e.http.URL+"/api/events", nil)
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := e.http.Client().Do(req)
	if err != nil {
		e.t.Fatalf("GET /api/events: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		e.t.Fatalf("GET /api/events: HTTP %d: %s", resp.StatusCode, b)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		resp.Body.Close()
		e.t.Fatalf("GET /api/events: Content-Type %q", ct)
	}
	s := &sseStream{events: make(chan sseEvent, 1024), errc: make(chan error, 1)}
	e.t.Cleanup(func() { resp.Body.Close() })
	go s.read(resp.Body)
	return s
}

func (s *sseStream) read(r io.Reader) {
	defer close(s.events)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	var typ string
	var data []string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if typ != "" || len(data) > 0 {
				if typ == "" {
					typ = "message"
				}
				s.events <- sseEvent{Type: typ, Data: json.RawMessage(strings.Join(data, "\n"))}
			}
			typ, data = "", nil
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "event:"):
			typ = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, context.Canceled) {
		s.errc <- err
	}
}

// next returns the next event of any type.
func (s *sseStream) next(t testing.TB) sseEvent {
	t.Helper()
	select {
	case ev, ok := <-s.events:
		if !ok {
			select {
			case err := <-s.errc:
				t.Fatalf("event stream ended: %v", err)
			default:
				t.Fatalf("event stream ended")
			}
		}
		return ev
	case <-time.After(eventTimeout):
		t.Fatalf("no event within %v", eventTimeout)
	}
	panic("unreachable")
}

// waitFor skips events until one of type typ satisfies match (nil matches
// any) and returns it.
func (s *sseStream) waitFor(t testing.TB, typ string, match func(json.RawMessage) bool) sseEvent {
	t.Helper()
	deadline := time.After(eventTimeout)
	var seen []string
	for {
		select {
		case ev, ok := <-s.events:
			if !ok {
				t.Fatalf("event stream ended while waiting for %q (seen %v)", typ, seen)
			}
			if ev.Type == typ && (match == nil || match(ev.Data)) {
				return ev
			}
			seen = append(seen, ev.Type)
		case <-deadline:
			t.Fatalf("no %q event within %v (seen %v)", typ, eventTimeout, seen)
		}
	}
}

func decodeEvent[T any](t testing.TB, ev sseEvent) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(ev.Data, &v); err != nil {
		t.Fatalf("%s event: %v: %s", ev.Type, err, ev.Data)
	}
	return v
}

func sortedAddrs(domainAddr func(i int) string, n int) []string {
	out := make([]string, n)
	for i := range n {
		out[i] = domainAddr(i)
	}
	slices.Sort(out)
	return out
}

func layerTargets(l gen.Layer) []string {
	return sortedAddrs(func(i int) string { return fmt.Sprintf("%s:%d", l.Units[i].Domain, l.Units[i].Address) }, len(l.Units))
}

func unitTargets(us []gen.EmuUnit) []string {
	return sortedAddrs(func(i int) string { return fmt.Sprintf("%s:%d", us[i].Domain, us[i].Address) }, len(us))
}
