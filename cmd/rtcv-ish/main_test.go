package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/puhitaku/rtcv-ish/internal/emu/fake"
)

func TestRun(t *testing.T) {
	defer func(v string) { version = v }(version)
	version = "test-1"

	emulator, err := fake.New(fake.Options{Manual: true})
	if err != nil {
		t.Fatal(err)
	}
	defer emulator.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{
			"--listen", "127.0.0.1:0",
			"--data-dir", t.TempDir(),
			"--emulator", emulator.Addr(),
			"--seed", "1",
			"--log-format", "json",
		}, io.Discard, pw)
		pw.Close()
	}()

	var url string
	var connected bool
	sc := bufio.NewScanner(pr)
	for url == "" && sc.Scan() {
		var rec struct{ Msg string }
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("log line is not JSON: %s", sc.Text())
		}
		connected = connected || rec.Msg == "connected to emulator"
		if u, ok := strings.CutPrefix(rec.Msg, "open "); ok {
			url = strings.TrimSuffix(u, " in a browser")
		}
	}
	go io.Copy(io.Discard, pr)
	if url == "" {
		t.Fatalf("no URL logged: %v", <-done)
	}
	if !connected {
		t.Error("emulator connection not logged")
	}

	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "rtcv-ish") {
		t.Errorf("GET / = %d %q", resp.StatusCode, body)
	}

	resp, err = http.Get(url + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	var st struct{ Version string }
	err = json.NewDecoder(resp.Body).Decode(&st)
	resp.Body.Close()
	if err != nil || st.Version != "test-1" {
		t.Errorf("GET /api/status: version %q (err %v), want %q", st.Version, err, "test-1")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after cancellation")
	}
}

func TestRunBadFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--log-format", "xml"}, {"--nope"}, {"extra"},
		{"--data-dir", t.TempDir(), "--melonds", "/nonexistent/melonDS"},
	} {
		if err := run(t.Context(), args, io.Discard, io.Discard); err == nil {
			t.Errorf("run(%v) succeeded", args)
		}
	}
}
