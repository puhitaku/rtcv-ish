# Emulator API

The contract between the core and an emulator. The canonical definition
is `api/emulator/v1/emulator.proto`; this document explains the semantics.

## Choice of RPC

Requirements: OS-agnostic, fast, explicit and stable signatures, minimal
dependencies inside the emulator build.

- gRPC/C++ was rejected: it is a large dependency to build for three OSes
  inside every emulator fork, and Qt's gRPC module has no server side.
- REST/JSON was rejected for parse cost and payload size (savestates,
  memory reads).
- Chosen: protobuf messages with a 4-byte length prefix over TCP.
  Serialization is protobuf (nanopb in C++, google.golang.org/protobuf in
  Go). Request/response matching is a single `id` field. This is the
  smallest thing that keeps explicit schemas and a stable ABI.

## Transport

- One TCP connection per client. The emulator listens (default
  `127.0.0.1:42069`, set with `--rtcvish-listen HOST:PORT`).
- Framing: `uint32 little-endian length` + `Message`. Maximum message size
  is 64 MiB.
- The emulator serves one client at a time. Further connections are
  refused until the active one closes.
- Requests carry a client-chosen `id`. The emulator sends exactly one
  `Response` with the same `id` for each request, in completion order,
  which may differ from arrival order for long operations (`Step`,
  `LoadRom`).
- `Event`s are pushed by the emulator at any time and carry no `id`.
- The first request must be `Hello`. `protocol_version` is 1. The emulator
  rejects other versions with `UNSUPPORTED` and any other request sent
  before `Hello` with `INVALID_ARGUMENT`.
- `Capabilities.max_payload` is at most the 64 MiB frame cap and must be
  large enough for a savestate (melonDS reports 64 MiB).
- Any request that violates the schema or the state machine yields an
  `Error` response; the connection stays open. A malformed frame closes
  the connection.

## Threading model in the emulator

- A server thread owns the socket: it accepts, reads and decodes requests,
  encodes and writes responses and events.
- Every request that touches emulator state becomes a job on a queue
  drained by the emulation thread at a frame boundary (before a frame
  runs, or in the paused loop). The emulation thread runs the job and
  hands the result back to the server thread. The server thread never
  touches emulator memory.
- `Hello`, `Ping`, `Subscribe` and `GetStatus` may be answered on the
  server thread from cached state.
- The per-frame unit scheduler runs on the emulation thread, right before
  the frame is emulated, after the job queue is drained.

## Operations

| Request | Effect | Errors |
|---|---|---|
| `Hello` | Handshake, returns emulator name, version, system and capabilities. | `UNSUPPORTED` |
| `GetStatus` | Current `Status` (state, frame, ROM info). | |
| `ListDomains` | Memory domains available for the loaded ROM. Empty when no ROM. | |
| `Read` | Read a batch of ranges. Each range must lie inside its domain. | `NOT_FOUND`, `OUT_OF_RANGE`, `NO_ROM` |
| `Write` | Write a batch of chunks. Applied in order, at a frame boundary. | `NOT_FOUND`, `OUT_OF_RANGE`, `NO_ROM`, `INVALID_ARGUMENT` (read-only domain) |
| `SaveState` | Serialize the console to bytes. | `NO_ROM`, `UNSUPPORTED` |
| `LoadState` | Restore from bytes. Scheduled units are left untouched. | `NO_ROM`, `INVALID_ARGUMENT` (bad blob) |
| `LoadRom` | Load a ROM by path and start running. Replies when the game is running. Clears scheduled units, resets the frame counter. | `NOT_FOUND`, `FAILED` |
| `CloseRom` | Stop emulation, unload the ROM. Clears units. | |
| `Reset` | Hard reset the console. Clears units, resets the frame counter, keeps the running/paused state. | `NO_ROM` |
| `Pause` / `Resume` | Pause or resume emulation. Idempotent. | `NO_ROM` |
| `Step` | Pause, emulate N frames, reply with the frame counter. | `NO_ROM`, `INVALID_ARGUMENT` (N == 0) |
| `ApplyUnits` | Schedule units. Ids must be unique among live units. The whole batch is validated before any unit is scheduled. | `NO_ROM`, `NOT_FOUND`, `OUT_OF_RANGE`, `INVALID_ARGUMENT` |
| `RemoveUnits` | Remove units by id. Unknown ids are ignored. | |
| `ClearUnits` | Remove all units. | |
| `ListUnits` | Units that are queued or executing. | |
| `SetInput` | Override input (OR-ed with the user's) until `clear`. | `UNSUPPORTED` |
| `Screenshot` | RGBA images of all screens. | `NO_ROM`, `UNSUPPORTED` |
| `Subscribe` | Frame events every N frames (0 disables). | |
| `Ping` | Liveness. | |
| `Quit` | Exit the emulator process after replying. | |

Events:

- `FrameEvent{frame}` after every `frame_interval`-th frame.
- `StatusEvent{status}` whenever the state changes (ROM loaded/closed,
  pause/resume, reset).

## Frame counter

`Status.frame` counts frames emulated since the last `LoadRom`/`Reset`.
It is monotonic: loading a savestate does not rewind it. Unit timing
(`delay`, `lifetime`, `loop_delay`) is expressed in these frames.

## Unit scheduler

Units implement RTCV's StepActions with a smaller model. For each frame
`f`, before emulation:

1. Units whose `delay` has elapsed enter execution. A store unit with
   `continuous=false` samples its source now (tilt applied, wrap-around
   arithmetic on `size` bytes, domain endianness).
2. Every executing unit writes: value units write `value`; store units
   write the sampled bytes (`continuous=true` re-samples every frame).
   Units execute in the order they were applied.
3. Units whose lifetime is over are removed. If `loop` is set they are
   re-queued with `loop_delay` (or `delay` if `loop_delay` is 0).

A unit with `lifetime=0` never expires. The core enforces RTCV's
"max infinite units" limit by removing the oldest ids itself.

Timing, precisely: a unit applied with `delay=N` is skipped for the next
N frames and first writes right before frame N+1. So after `Step(N)` the
write is not visible yet, after `Step(N+1)` it is. A store unit reads its
source at the moment it writes (continuous) or when it first executes
(once), so writes by earlier units in the same frame are visible to it.

## Memory domains for melonDS

Little-endian throughout. `hidden` marks domains RTCV blacklisted.

| Name | Size | Word | Writable | Hidden | Backing |
|---|---|---|---|---|---|
| `MainRAM` | 4 MiB (DS) / 16 MiB (DSi) | 4 | yes | no | `NDS::MainRAM`, JIT invalidation per write |
| `VRAM` | 8 MiB | 4 | yes | no | ARM9 view at `0x06000000` through `GPU::ReadVRAM_*`/`WriteVRAM_*` |
| `Palette` | 2 KiB | 2 | yes | no | `GPU::Palette` via `WritePalette` |
| `OAM` | 2 KiB | 2 | yes | no | `GPU::OAM` via `WriteOAM` |
| `SharedWRAM` | 32 KiB | 4 | yes | yes | `NDS::SharedWRAM` |
| `ARM7WRAM` | 64 KiB | 4 | yes | yes | `NDS::ARM7WRAM` |
| `ITCM` | 32 KiB | 4 | yes | yes | `ARM9.ITCM` |
| `DTCM` | 16 KiB | 4 | yes | yes | `ARM9.DTCM` |
| `CartROM` | ROM size | 4 | yes | yes | cart ROM image |
| `NWRAM_A/B/C` (DSi) | 256 KiB each | 4 | yes | yes | `DSi::NWRAM_*` |

Input bits for `SetInput.buttons` on melonDS: A=0, B=1, Select=2, Start=3,
Right=4, Left=5, Up=6, Down=7, R=8, L=9, X=10, Y=11 (1 = pressed).

## melonDS command line

- `--rtcvish-listen HOST:PORT` starts the API server. Without it the
  emulator behaves like upstream.
- `--rtcvish-config-dir DIR` uses DIR as the config/emu directory instead
  of the default, so tests can run with a private `melonDS.toml`.
- The existing positional ROM argument may be used to boot a ROM at start.

## C++ SDK (`sdk/cpp`)

Dependency-free C++17 (plus nanopb, vendored). It provides:

- `rtcvish::Server`: socket thread, framing, decode/encode, request
  dispatch to a `Backend` interface, event sending.
- `rtcvish::Backend`: the interface an emulator implements (domains,
  read/write, savestate, control, screenshot, input). Calls arrive on the
  thread the emulator chooses by draining `Server::pollJobs()`.
- `rtcvish::Scheduler`: the unit scheduler, called once per frame with a
  `Backend`.

Emulator forks copy `sdk/cpp/rtcvish` verbatim (`scripts/sync-sdk.sh`).
The generated nanopb sources are committed so the emulator build needs no
protoc.
