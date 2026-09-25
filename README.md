# rtcv-ish

rtcv-ish is an OS-agnostic reimplementation of
[RTCV (Real-Time Corruptor Vanguard)](https://github.com/redscientistlabs/RTCV)
for Windows, macOS and Linux. A Go core server with a web frontend controls
emulators (currently melonDS, a Nintendo DS emulator) over a small TCP API
to corrupt game memory in real time. It is a hacker's tool and is not
affiliated with the RTCV authors.

## Install

Download the archive for your OS from the
[releases page](https://github.com/puhitaku/rtcv-ish/releases) and unpack it.
The archive contains the `rtcv-ish` executable and the bundled emulator under
`emulators/`.

You must provide your own ROMs. BIOS/firmware files are optional for homebrew
(melonDS ships a free BIOS) but needed for commercial games; configure them in
melonDS as usual.

## Use

1. Run `rtcv-ish` (double-click it or start it from a terminal).
2. Open the printed URL (default http://127.0.0.1:8420) in a browser.
3. Click Launch to start the bundled melonDS. Alternatively, start melonDS
   yourself with `--rtcvish-listen 127.0.0.1:42069` and click Connect.
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

## Build from source

Requires Go 1.26 and Node. Run `make build` to build the core into `bin/`.
The melonDS fork is the `emulators/melonds` submodule (branch `rtcv-ish`);
build it with CMake as described in its `BUILD.md`.

## License

rtcv-ish is released under the MIT License. See [LICENSE](LICENSE) for the
referenced projects (RTCV, melonDS, nanopb) and their licenses.
