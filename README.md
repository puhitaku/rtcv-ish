<div align="center">
  <h1>rtcv-ish</h1>
  <p><img width=400px src="./screenshot.avif" alt="rtcv-ish's screenshot"></p>
</div>


rtcv-ish is a multi-OS reimplementation of
[RTCV (Real-Time Corruptor Vanguard)](https://github.com/redscientistlabs/RTCV)
for Windows, macOS and Linux. A Go core server with a web frontend controls
emulators (currently supports only melonDS) over a small TCP API
to corrupt game memory in real time.

rtcv-ish is a spiritual fork of RTCV. Shout out to the RTCV's original authors.


## Install

Download the archive for your OS from the
[releases page](https://github.com/puhitaku/rtcv-ish/releases) and unpack it.
The archive contains the `rtcv-ish` executable and the bundled emulator under
`emulators/`.


## Use

1. Run `rtcv-ish` (double-click it or start it from a terminal).
2. Open the printed URL (default http://127.0.0.1:8420) in a browser.
3. Click Launch to start a bundled emulator.
4. Load a ROM.
5. Corrupt with Manual Blast or Auto-Corrupt. Tune the engine in the Engine
   panel, and use the Glitch Harvester for savestates, stash and stockpiles.

Core flags:

| Flag | Default | Description |
|---|---|---|
| `--listen` | `127.0.0.1:8420` | HTTP listen address |
| `--data-dir` | `data` next to the executable | Data directory (savestates, stockpiles, lists, settings) |
| `--emulator` | none | Emulator API address to connect to at start |
| `--seed` | `0` (time-based) | Random seed |
| `--log-format` | `auto` | Log format: `auto`, `text` or `json` |
| `--melonds` | `$RTCVISH_MELONDS` | melonDS executable or `.app` bundle to launch. Without it, the bundled `emulators/melonds/` next to the executable is used, then the newest `emulators/melonds/build/*/` when run from the repository |


## Build from source

Requires Go 1.26 and Node. Run `make build` to build the core into `bin/`.
The melonDS fork is the `emulators/melonds` submodule (branch `rtcv-ish`);
build it with CMake as described in its `BUILD.md`.
Local builds report their commit (`abc1234`, or `abc1234-dirty` for a modified tree) as the version; tagged release builds report `<VERSION file> <commit>`.

See also: [the GitHub actions pipeline](.github/workflows/release.yml)


## License

rtcv-ish is released under the MIT License. See [LICENSE](LICENSE) for the
referenced projects (RTCV, melonDS, etc.) and their licenses.
