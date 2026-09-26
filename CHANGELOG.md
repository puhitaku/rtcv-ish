# Changelog

Each release has a section headed `## <tag>` (for example `## v1.0.0` or
`## v1.0.0-rc1`), newest first. The release workflow copies the body of the
section matching the pushed tag into the GitHub release notes and fails if
that section is missing or empty, so add it before tagging. Anything after
the tag on the heading line (such as ` (2026-01-31)`) is ignored.

Write entries for users: what changed and why it matters, not a commit
list. Collect changes under `## Unreleased` and rename that heading to the
tag when releasing.

## Unreleased

## v1.0.0

First release of rtcv-ish, an OS-agnostic reimplementation of RTCV
(Real-Time Corruptor Vanguard) for Windows, macOS and Linux.

- Core server `rtcv-ish` with a web frontend: open the printed URL,
  launch or connect to an emulator, load a ROM and corrupt.
- melonDS (Nintendo DS) bundled in every archive with the rtcv-ish API
  built in: memory domains, savestates, pause/step/reset, input override
  and screenshots.
- RTCV's corruption engines: Nightmare, Hellgenie, Distortion, Freeze,
  Pipe, Vector, Cluster and Custom, with intensity, error delay, blast
  radius, precision, alignment, an address range limit, and limiter and
  value lists.
- Freeze modes per frame, per scanline or hard (guest writes to frozen
  bytes are intercepted, also under the JIT).
- Glitch Harvester: savestate slots, stash history, stockpile with
  `.sks` export/import, corrupt/inject/original/merge/reroll, BlastLayer
  ON/OFF, game protection with Back, Last and Now.
- Blast Editor with RTCV's unit table and operations, `.bl` files, help
  tooltips and hex/decimal inputs.
- Memory viewer with hex editing, freezes and per-domain RGB565 bitmaps.
- Keyboard shortcuts, light and dark theme, and a fail-safe core that
  stays responsive when an emulator hangs.

Changes since v1.0.0-rc1: release archives include the web frontend.

## v1.0.0-rc1

First release candidate of rtcv-ish, an OS-agnostic reimplementation of
RTCV (Real-Time Corruptor Vanguard).

- Core server `rtcv-ish` for Linux, macOS and Windows with a web frontend:
  open the printed URL in a browser, connect to or launch an emulator, load
  a ROM and corrupt.
- melonDS (Nintendo DS) bundled in every archive, with the rtcv-ish API
  built in: memory domains (MainRAM, VRAM, Palette, OAM, WRAMs, TCMs,
  cart ROM, DSi NWRAM), savestates, pause/step/reset, input override and
  screenshots.
- Corruption engines from RTCV: Nightmare, Hellgenie, Distortion, Freeze,
  Pipe, Vector, Cluster and Custom, with intensity, error delay, blast
  radius, precision and alignment, plus limiter and value lists.
- Glitch Harvester: savestate slots, stash history, stockpile with
  `.sks` export/import, corrupt/inject/original/merge/reroll, BlastLayer
  ON/OFF and game protection backups.
- Blast Editor with RTCV's unit table and operations (disable 50%,
  invert, shift, bake, break down, sanitize) and `.bl` files.
- Memory viewer with in-place editing and freezes.
- Light and dark theme following the OS.
