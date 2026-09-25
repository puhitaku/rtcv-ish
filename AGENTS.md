# AGENTS.md

rtcv-ish: OS-agnostic reimplementation of RTCV (Real-Time Corruptor
Vanguard). Go core + Vue web frontend + protobuf/TCP API inside emulators.
Read `design/architecture.md` first; other design docs are in `design/`.

## Map

- `cmd/rtcv-ish` core executable, `cmd/rtcv-ish-fakeemu` fake emulator.
  `internal/*` core packages (see `design/architecture.md`): `emu` client,
  `emu/fake`, `corrupt` engines, `stockpile` storage, `session`
  coordinator, `server` HTTP + generated `gen/`, `webui` embedded frontend
  (build tag `embedweb`).
- `api/emulator/v1/emulator.proto` emulator API (semantics in
  `design/emulator-api.md`). `api/frontend/openapi.yaml` frontend API.
- `sdk/cpp` C++ emulator SDK, copied verbatim into emulator forks.
- `web/` Vue 3 frontend, embedded into the core.
- `emulators/melonds` melonDS fork submodule, branch `rtcv-ish`.
- `references/` read-only submodules (RTCV, Vanguard melonDS, nds-examples).
  Never modify them. Test ROMs: `references/nds-examples/bin/*.nds`
  (build with `scripts/build-nds-examples.sh`).
- `test/e2e` tests that drive a real melonDS (`RTCVISH_MELONDS=/path/to/melonDS`).

## Rules

- New tools are written in Go. Emulator-side code is C++17.
- Generic OSS style. Comments only where the code is not self-explanatory.
- Format everything: `gofmt`/`goimports`, `clang-format` (SDK and emulator
  changes), Prettier for `web/`.
- Tests live in this repository, not in emulator forks. Emulator APIs must
  expose enough for testing and debugging.
- Randomness in the core goes through one seeded `*rand.Rand`; tests pass
  a fixed seed.
- Logging: `log/slog`, tint handler on a TTY, JSON otherwise.
- Contexts derive from the root `signal.NotifyContext`; goroutines stop on
  cancellation. Use `github.com/sasha-s/go-deadlock` mutexes in the core.
- Commits are semantic: one feature/fix/improvement per commit. Emulator
  fork commits go on branch `rtcv-ish` of the submodule, then bump the
  submodule pointer here.
- UI: simple, flat, no gradients, light/dark follows the OS.
- Generated code is committed. Regenerate with `scripts/gen.sh`.
- `make build` builds `web/` then the core with `-tags embedweb`;
  `make build-noweb` skips the frontend. `make test` runs Go tests;
  `make test-e2e` needs `RTCVISH_MELONDS`. Frontend: `cd web && npm test`,
  `npm run test:e2e`.
