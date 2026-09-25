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
  field, bundled emulator list, ROM path), game name and frame counter,
  the two global actions Manual Blast and Auto-Corrupt toggle, theme
  toggle. Game Protection: toggle + Back + Now.
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
infinite value unit at the address), Refresh, follows the frame counter
when "auto refresh" is on (1 Hz).

### Settings

Reroll settings, StepActions settings (max infinite units, lock units),
game protection interval, lists manager (upload/delete `.txt`), data
directory, log level, About.

## Behaviour

- One Pinia store per resource (`status`, `settings`, `domains`, `stash`,
  `stockpile`, `savestates`, `lists`); the SSE stream triggers refetches.
- Buttons are disabled (with a tooltip reason) when the emulator is
  disconnected or no ROM is loaded.
- Errors from the API show in the log strip and as a transient toast.
- Everything is keyboard-usable; hotkeys are a later feature.

## Testing

- Vitest: stores (event handling, optimistic updates), unit formatting
  helpers (hex, tilt), the hex view model.
- Playwright: against `rtcv-ish` with the fake emulator: connect, load a
  ROM, manual blast, create a savestate slot, corrupt, stash appears,
  send to stockpile, open in Blast Editor, disable 50%, apply, export
  stockpile, theme toggle.
