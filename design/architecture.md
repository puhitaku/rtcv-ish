# Architecture

rtcv-ish is an OS-agnostic reimplementation of RTCV (Real-Time Corruptor
Vanguard). RTCV's Windows-only pieces (WinForms UI, Ceras/.NET IPC,
C++/CLI emulator glue) are replaced by a web frontend, a Go core server
and a small protobuf-over-TCP API implemented inside each emulator.

```
+--------------+   Frontend API    +------------+   Emulator API    +-------------+
| Web frontend | <-- REST + SSE -> |    Core    | <- protobuf/TCP-> | Emulator(s) |
| (Vue, in the |                   | (rtcv-ish, |                   | (melonDS,   |
|  browser)    |                   |  Go)       |                   |  ...)       |
+--------------+                   +------------+                   +-------------+
```

## Components

| Component | Language | Role |
|---|---|---|
| Core (`rtcv-ish` executable) | Go | Serves the frontend, owns all RTCV logic (engines, blast layers, stash/stockpile, settings, auto-corrupt), talks to emulators. |
| Web frontend | Vue 3 + TypeScript | RTCV-equivalent GUI. Built with Vite, embedded into the core executable. |
| Emulator SDK | C++17 | Small library vendored into emulator forks: wire framing, nanopb-generated messages, request dispatch, per-frame unit scheduler. |
| melonDS fork | C++ | `emulators/melonds`, branch `rtcv-ish`. Adds the API server thread and memory domains. |

## Responsibility split

RTCV runs CorruptCore inside the emulator process. rtcv-ish moves
everything that is not latency-critical into the core and keeps the
emulator side minimal so that adding an emulator is cheap.

| Concern | Where |
|---|---|
| Memory domains (peek/poke, sizes, JIT invalidation, renderer dirty flags) | Emulator |
| Per-frame unit execution (freeze, pipe, delayed and looping writes) | Emulator, driven by the core through `ApplyUnits` |
| Savestates (serialize/deserialize) | Emulator; bytes are transferred, the core stores them |
| Blast layer generation (engines, radius, lists, RNG) | Core |
| Auto-corrupt timer (error delay) | Core, using frame events from the emulator |
| Stash history, stockpiles, savestate slots, settings | Core |
| Everything visual | Frontend |

Design notes:

- The emulator is the TCP server. The core connects to it. Users either
  start an emulator by hand and enter its address, or let the core launch
  a bundled emulator with a port argument.
- The core is the single source of truth for settings. Emulators receive
  only commands and units; there is no spec replication.
- Blast units are generated in the core using batched reads, then
  rasterized into emulator `Unit`s. RTCV semantics map as follows:
  - `VALUE` unit: `Unit.value` (tilt pre-applied by the core).
  - `STORE` + `IMMEDIATE`: the core reads the source now and sends a value unit.
  - `STORE` + `PREEXECUTE` + `ONCE`: `Unit.store{continuous:false}`.
  - `STORE` + `CONTINUOUS`: `Unit.store{continuous:true}`.
  - `ExecuteFrame`/`Lifetime`/`Loop`: `delay`/`lifetime`/`loop`.
  - `loop_delay` is filled by the core: `LoopTiming` when set, else `ExecuteFrame`.
  - Limiter checks at `PREEXECUTE`/`EXECUTE` time are not supported; only `GENERATE`.
- Deterministic RNG: the core takes an optional seed (`--seed`) and tests
  always pass one. All randomness goes through one `*rand.Rand` (PCG).
- Savestates and ROM paths: savestate bytes cross the wire, so the core and
  the emulator need not share a filesystem. ROMs are loaded by path on the
  emulator host.

## Repository layout

```
rtcv-ish/
  AGENTS.md              rules and map for coding agents
  README.md  LICENSE
  design/                design docs (this directory)
  api/
    emulator/v1/         emulator.proto and generated Go code
    frontend/            openapi.yaml, shared by core and web
  cmd/rtcv-ish/          core executable entry point
  internal/
    logging/             slog setup (tint for TTY, JSON otherwise)
    emu/                 emulator API client (framing, requests, events)
    emu/fake/            in-process fake emulator for tests
    corrupt/             blast units/layers, engines, generation, lists
    stockpile/           stash keys, stash history, stockpile files
    session/             coordinator: settings, emulator lifecycle, auto-corrupt, harvester ops
    server/              HTTP server: generated OpenAPI code, handlers, SSE
    webui/               embeds the built frontend (internal/webui/dist) into the executable
  sdk/cpp/               C++ emulator SDK (vendored into emulator forks)
  web/                   Vue frontend (Vite, Pinia, Tailwind, Vitest, Playwright)
  emulators/melonds/     git submodule: melonDS fork, branch rtcv-ish
  references/            git submodules: RTCV, Vanguard melonDS (read-only)
  scripts/               code generation, SDK sync, ROM build helpers
  test/e2e/              end-to-end tests that drive a real emulator
  test/roms/             test ROMs built from devkitPro nds-examples (gitignored)
  .github/workflows/     CI and release
  prompts/               task prompts given to the agents
```

Generated code is committed (`api/emulator/v1/*.pb.go`, nanopb output
inside `sdk/cpp`, `internal/server/gen`, `web/src/api/schema.d.ts`) so
that a plain `go build` works without generators installed.

## Core internals

- Root context comes from `signal.NotifyContext`. Every goroutine takes a
  derived context and exits on cancellation. The HTTP server and the
  emulator connection are shut down gracefully.
- `go-deadlock` replaces `sync.Mutex`/`sync.RWMutex` in the core packages.
- `internal/session.Session` has two levels of locking. `s.mu` guards the
  session state, is held only briefly and never across an emulator call.
  An operation gate (a one-slot semaphore) serializes multi-step
  operations that talk to the emulator (connect/launch, ROM control,
  blast/apply/toggle/reroll, Glitch Harvester, game protection, savestate
  slots): an operation pins the connection, reads what it needs under
  `s.mu`, calls the emulator with only the gate held and writes results
  back under `s.mu` if the connection is still current. API operations
  wait at most 5 s for the gate (at most 4 waiters) and otherwise fail with
  409 `BUSY`; `Status.busy` names the running operation. Background work
  (auto-corrupt on frame events, game protection, status refresh) only
  tries the gate and skips or retries later. Status, domains, settings and
  the stash/stockpile never wait for the gate; memory, words, screenshots
  and the unit list call the emulator without it.
- Every emulator call has a per-call timeout (30 s for LoadRom, LoadState
  and SaveState, 5 s otherwise, plus the frames for Step). A timeout fails
  the call with 504 `EMULATOR_TIMEOUT` and marks the connection
  unresponsive (`Status.unresponsive`): operations then fail with 503
  `EMULATOR_UNRESPONSIVE` until a ping (every 2 s) succeeds. Disconnect,
  quit and close never wait for a stuck call; they close the connection,
  which fails pending calls, and a launched emulator that does not exit
  within 3 s is killed.
- Events (status, frame counter, blast log, stash changes) are fanned out
  to SSE subscribers through a small broker in `internal/session`.
- Persistent data lives in a data directory (`--data-dir`, default `data`
  next to the executable): `states/` (savestate blobs), `stockpiles/`,
  `lists/`, `settings.json`.

## Testing layers

| Layer | Tool | Needs |
|---|---|---|
| Core unit tests | `go test ./...` with the fake emulator | nothing |
| Core vs. real emulator | `go test ./test/e2e` | `RTCVISH_MELONDS` pointing at a built melonDS, ROMs in `test/roms` (built on demand by `scripts/build-nds-examples.sh`) |
| Frontend unit tests | Vitest | Node |
| Frontend E2E | Playwright against `rtcv-ish` + fake emulator | Node, Go |
| Full E2E | Playwright against `rtcv-ish` + melonDS | all of the above |

## Release

A GitHub Actions workflow builds the core for Linux, macOS and Windows,
builds the melonDS fork on each OS, and packages both into one archive per
OS. It runs on tags `v*` and on branches `ci-*`.
