# rtcvish C++ SDK

Emulator side of the rtcv-ish emulator API (`api/emulator/v1/emulator.proto`,
semantics in `design/emulator-api.md`). Dependency-free C++17 plus vendored
nanopb (0.4.9.2). Emulator forks copy this directory verbatim
(`scripts/sync-sdk.sh`); do not edit the copies.

## Contents

| File | Purpose |
|---|---|
| `Types.h` | Plain C++ types (status, domains, units, errors). |
| `Backend.h` | Interface the emulator implements. |
| `Scheduler.{h,cpp}` | Per-frame unit scheduler. |
| `Server.{h,cpp}` | TCP server: socket/writer threads, request dispatch, events. |
| `Wire.{h,cpp}` | Framing and nanopb helpers (internal). |
| `emulator.pb.{c,h}`, `emulator.options` | nanopb output; regenerate with `scripts/gen-nanopb.sh`. |
| `nanopb/` | nanopb runtime, built with `PB_ENABLE_MALLOC` and `PB_FIELD_32BIT`. |

## Integrating

1. `add_subdirectory(rtcvish)` and `target_link_libraries(<emu> PRIVATE rtcvish)`.
2. Implement `rtcvish::Backend`. All calls arrive on the thread that calls
   `Server::pollJobs()`, so the backend can touch emulator state directly.
3. Create a `rtcvish::Server(backend)` and `start(host, port, err)`.
4. In the emulation loop:
   - call `pollJobs()` every iteration (running and paused);
   - before emulating a frame, apply the input override and call `runFrame()`;
   - after a frame was emulated, increment the frame counter and call
     `frameCompleted(frame)`;
   - while paused, sleep with `waitForJobs(timeout)` so requests are served
     quickly;
   - when the emulator's own UI resets or loads a game, reset the frame
     counter, `scheduler().clear()` and call `statusChanged()`.
5. `Backend::runFrames(n)` must make the paused emulator emulate `n` frames
   and stay paused; `Step` requests complete from `frameCompleted`.
6. Unit modes (optional): report `Capabilities::scanlineUnits` and call
   `runScanline()` at every scanline of a frame (skipping the call while
   `scheduler().scanlineActive()` is false keeps it free); report
   `Capabilities::hardUnits` and implement `Backend::setFrozen()` by
   dropping guest writes to the given bytes. Missing modes fall back to the
   next weaker one.
7. Destroy the server (or call `stop()`) before tearing down the emulator.
