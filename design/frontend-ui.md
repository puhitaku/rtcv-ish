# Frontend UI

An RTCV-equivalent GUI in the browser. Equivalent means the same tools,
controls and workflow, not the same pixels. Hacker-tool aesthetic: flat
panels, one accent color, monospace for addresses and values, no
gradients, no decoration. Light and dark follow the OS
(`prefers-color-scheme`), with a manual override in the top bar.

## Stack

Vue 3 (script setup, TypeScript), Vite, Pinia, Tailwind CSS, Vitest for
unit tests, Playwright for E2E, Prettier + ESLint. Generated API types from
`api/frontend/openapi.yaml` via openapi-typescript and a thin
`openapi-fetch` client. No component library.

## Layout

```
+------------------------------------------------------------------+
| rtcv-ish   [emulator: melonDS  connected]  [game: hello_world]    |
|            [frame 12345]  [Manual Blast] [Auto-Corrupt: OFF]      |
+-----------+------------------------------------------------------+
| Engine    |                                                      |
| Harvester |          active panel                                |
| Blast Ed. |                                                      |
| Memory    |                                                      |
| Hex       |                                                      |
| Settings  |                                                      |
+-----------+------------------------------------------------------+
| log line ... (last few user-facing messages)                      |
+------------------------------------------------------------------+
```

- Top bar: connection status (with a Connect/Launch popover: address
  field, bundled emulator list, ROM path with a Browse… picker that lists
  core-host directories via `/api/browse`), game name and frame counter,
  the two global actions Manual Blast and Auto-Corrupt toggle, theme
  toggle. Game Protection: toggle + Back + Last + Now.
- Stuck emulator: while `Status.busy` is set, a small monospace
  `<operation> <seconds>s` sits next to the connection status (elapsed
  time ticks locally from `sinceMs`). When `Status.unresponsive` is true
  the connection chip turns the warning color, reads "unresponsive" and
  its tooltip says the emulator stopped answering. In the popover,
  Disconnect and Quit stay clickable while connected, even when busy or
  unresponsive (the core never gates them); Quit reads "Quit / kill"
  when unresponsive or busy for more than 5 s, and its tooltip says the
  core force-kills a launched emulator that has not exited after 3 s.
- Left sidebar switches panels. Panels mirror RTCV grids.
- Bottom: a short log strip fed by `log` events.

## Panels

### Engine

Three columns like RTCV's Engine Config grid:

1. General parameters: Intensity (slider + number, non-linear slider
   scale, uncapped number), Error Delay (same), Blast Radius select.
2. Corruption engine: engine select, precision select (8/16/32/64-bit),
   alignment number, and an engine-specific parameter block:
   - Nightmare: algo (Random / Random Tilt / Tilt), min/max for the
     current precision.
   - Hellgenie: min/max, Max ∞ Units, Clear all cheats.
   - Distortion: delay, Resync (clear units).
   - Freeze: Max ∞ Units, Clear all freezes.
   - Pipe: Max ∞ Units, Clear pipes, Lock step units.
   - Vector: limiter list, value list, Unlock precision.
   - Cluster: limiter list, chunk size, method, rotate amount, direction,
     split units, filter all.
   - Custom: the full Custom Engine form inline (unit source, value
     settings, store settings, tilt, delay, lifetime, loop, limiter).
3. Memory domains: multi-select list with size and word size, buttons
   Auto-select / Select all / Unselect all. Hidden domains are shown
   greyed with a "(hidden)" tag but selectable.

### Glitch Harvester

Four areas like RTCV:

- Blast tools: big Corrupt button (label changes to Inject / Original /
  Merge with the mode and selection), Reroll selected, BlastLayer ON/OFF,
  mode menu (Corrupt / Inject / Original), behaviours (Auto-load state,
  Load on select, Stash results).
- Savestate manager: numbered slot boxes with editable labels, SAVE/LOAD
  mode toggle, click saves or loads, paging.
- Stash history: list; click runs it (if Load on select); ▲▼, To
  Stockpile, Clear; context: open in Blast Editor, rename, merge.
- Stockpile: table (name, game, system, note); click runs; Load/Save/
  Save as/Import (.sks upload/download and host paths), Clear, Remove,
  Rename, move up/down; context: open in Blast Editor.

### Blast Editor

Opened for a stash or stockpile key (or a fresh layer). Table of units
with RTCV's columns (togglable), filter bar, side property editor for the
selected rows (multi-edit), buttons: Disable 50%, Invert Disabled, Remove
Disabled, Enable/Disable everything, Remove selected, Duplicate, Add row,
Shift selected (field + amount), Load + Corrupt, Apply Corruption, Send
to Stash, To Stockpile, Bake to VALUE, Break down, Sanitize duplicates,
Load/Save `.bl`. Layer size label.

### Memory / Hex

Domain select, address input, a hex view (16 bytes per row, 1/2/4-byte
grouping), editing by typing, Freeze/Unfreeze (adds or removes an
infinite value unit at the address), Refresh, and a screenshot box.

Below them, a Bitmaps box with one pane per memory domain (all domains,
hidden ones included), stacked vertically at the full panel width. The
pane header shows the name, size, "(hidden)", a stride selector and the
native image size; clicking the name collapses or expands the pane
(hidden domains start collapsed; the state and stride overrides persist
in localStorage under `rtcvish.memoryBitmaps`). The pane draws the domain
into a `<canvas>` at its native pixel size, scaled by CSS to the pane
width (`image-rendering: pixelated`): each little-endian 16-bit word is
one RGB565 pixel (bits 15..11 R, 10..5 G, 4..0 B, expanded to 8 bits).
Data comes from `/api/memory/{domain}/words`.

- Width: a power of two from the stride-1 pixel count `px = size/2`:
  `2^floor((ceil(log2 px) + 2) / 2)`, capped at 4096, so the image is
  landscape (4:1 or 2:1). The width is fixed per domain; the stride only
  changes the height. E.g. 4 MiB → 2048×1024, 8 MiB → 4096×1024 (512
  rows at stride 2), 16 MiB → 4096×2048 (512 at stride 4), 64 KiB →
  256×128, 2 KiB → 64×16.
- Stride: every stride-th word is fetched, so a pixel stands for
  2·stride bytes. Auto is the smallest power of two that keeps one fetch
  ≤ 4 MiB (4 MiB → 1, 8 MiB → 2, 16 MiB → 4); the selector offers
  1/2/4/8/16 (and the auto value).
- Hover shows a popup at the cursor with the domain-relative address
  (hex, padded to the domain's width) and the 16-bit value from the last
  fetch; it never fetches. Click jumps the hex view to that domain and
  address (row-aligned, cursor on the byte), scrolls it into view and
  flashes the pixel.

Refresh and "auto (2 Hz)" drive the hex view and every expanded bitmap
from one scheduler (500 ms). Collapsed panes do not fetch; a tick is
skipped while the previous one is still running, while the page is
hidden or no ROM is loaded, and the timer stops when the panel is left.

### Settings

Reroll settings, StepActions settings (max infinite units, lock units),
game protection interval, lists manager (upload/delete `.txt`), data
directory, log level, About.

## Behaviour

- One Pinia store per resource (`status`, `settings`, `domains`, `stash`,
  `stockpile`, `savestates`, `lists`); the SSE stream triggers refetches.
- Buttons are disabled (with a tooltip reason) when the emulator is
  disconnected, no ROM is loaded, or it is busy or unresponsive
  (`needEmu` / `needRom` in the status store include both).
- Status events are not sent when an operation starts, so while a
  tracked API call is pending or `busy` is set, the frontend polls
  `/status` once a second. A missing `unresponsive` counts as false and a
  missing `busy` as null.
- Errors from the API show in the log strip and as a transient toast.
  `BUSY`, `EMULATOR_TIMEOUT` and `EMULATOR_UNRESPONSIVE` read "Emulator is
  busy: <operation>", "Emulator did not answer in time" and "Emulator is
  unresponsive; disconnect or quit it", and refresh the status.
- Everything is keyboard-usable. Global shortcuts (plain single keys, no
  modifiers): `m` Manual Blast, `a` toggle Auto-Corrupt, `p` toggle Game
  Protection, `b` / `l` / `n` Game Protection Back / Last / Now. They run
  the same actions as the top bar buttons (same enabled conditions, same
  log lines), and are ignored while focus is in an input, textarea,
  select or contenteditable, or while a dialog, prompt or context menu is
  open. The key map is one table in `web/src/lib/shortcuts.ts`; button
  tooltips show the key and Settings lists all shortcuts.

## Testing

- Vitest: stores (event handling, optimistic updates), unit formatting
  helpers (hex, tilt), the hex view model.
- Playwright: against `rtcv-ish` with the fake emulator: connect, load a
  ROM, manual blast, create a savestate slot, corrupt, stash appears,
  send to stockpile, open in Blast Editor, disable 50%, apply, export
  stockpile, theme toggle.
