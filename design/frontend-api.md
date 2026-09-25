# Frontend API

REST + JSON between the browser and the core, plus one Server-Sent Events
stream. The canonical definition is `api/frontend/openapi.yaml` (OpenAPI
3.1). The Go server code is generated with oapi-codegen (strict server,
std `net/http`), the TypeScript client types with openapi-typescript.
This document lists the resources and the rules; field-level detail lives
in the spec.

## Conventions

- Base path `/api`. JSON bodies. Errors are `{ "error": string, "code": string }`
  with 4xx/5xx. Emulator errors map to `code` = the emulator error code
  (`NO_ROM`, `OUT_OF_RANGE`, ...) with 409 or 400 as appropriate; a
  disconnected emulator yields 503 `EMULATOR_DISCONNECTED`.
- Addresses, values and sizes are JSON numbers unless they can exceed
  2^53: byte strings (`value`, savestate ids) are hex strings.
- All state-changing calls are serialized by the core; the client never
  needs to lock anything.
- The client learns about changes through `/api/events` and re-fetches
  the affected resource. Responses of mutations return the updated
  resource when it is small.

## Resources

### Status and emulator

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/status` | Core version, data dir, emulator connection state, emulator info (name, version, system, capabilities), game status (state, frame, ROM path, title, code, console). |
| GET | `/api/emulators` | Bundled emulators the core can launch (name, executable path, present?). |
| POST | `/api/emulator/connect` | `{address}`: connect to a running emulator. |
| POST | `/api/emulator/launch` | `{name, rom?}`: launch a bundled emulator on a free port and connect. |
| POST | `/api/emulator/disconnect` | Drop the connection (emulator keeps running). |
| POST | `/api/emulator/rom` | `{path}`: load a ROM. |
| POST | `/api/emulator/close-rom` | |
| POST | `/api/emulator/pause` / `resume` / `reset` | |
| POST | `/api/emulator/step` | `{frames}` |
| POST | `/api/emulator/quit` | |
| GET | `/api/emulator/screenshot` | PNG of all screens stacked vertically. |
| GET | `/api/browse?path=` | ROM picker: `{path, parent (null at a root), entries: [{name, path, dir, size}]}` for a core-host directory; directories then ROM-like files (melonDS extensions), hidden entries omitted. Default: home dir, else data dir. 400 not a directory, 404 missing, 403 `PERMISSION_DENIED`. On Windows a drive root also lists the other drives. |

### Memory domains and memory

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/domains` | `[{name, size, wordSize, bigEndian, writable, hidden, selected}]` |
| PUT | `/api/domains/selected` | `{names: []}`; the selected domains used by generation. |
| POST | `/api/domains/auto-select` | Select all non-hidden domains. |
| GET | `/api/memory/{domain}?address=&size=` | Hex-encoded bytes (size ≤ 64 KiB). |
| PUT | `/api/memory/{domain}` | `{address, data(hex)}` |

### Settings (engine config)

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/settings` | The whole settings object. |
| PATCH | `/api/settings` | Partial update; returns the whole object. |

Settings object (persisted in `settings.json`):

- `engine`: `nightmare | hellgenie | distortion | freeze | pipe | vector | cluster | custom`
- `intensity` (1..), `errorDelay` (frames, 1..), `radius`: `spread | chunk | burst | normalized | proportional | even`
- `precision`: 1 | 2 | 4 | 8, `alignment` (0..precision-1)
- `autoCorrupt` (bool), `maxInfiniteUnits` (default 50), `lockUnits` (bool)
- `nightmare`: `{algo: random|randomTilt|tilt, min, max}` (min/max per precision, as in RTCV: `min8,max8,min16,...` or a map keyed by precision)
- `hellgenie`: `{min, max}` per precision
- `distortion`: `{delay}`
- `vector`: `{limiterList, valueList, unlockPrecision}` (list names)
- `cluster`: `{limiterList, chunkSize, method: random|reverse|rotateForwards|rotateBackwards|overwrite, modifier, direction: forwards|backwards, splitUnits, filterAll}`
- `custom`: every `CUSTOM_*` parameter from RTCV (see `design/rtcv-reference.md` 2.6): `source`, `valueSource`, `min/max`, `valueList`, `storeAddress`, `storeTime`, `storeType`, `tilt`, `delay`, `lifetime`, `loop`, `limiterList`, `limiterTime` (`none | generate` supported), `limiterInverted`
- `reroll`: `{address, sourceAddress, domain, sourceDomain, followCustomEngine}`
- `gameProtection`: `{enabled, intervalSeconds, keep}`

### Blasting

| Method | Path | Purpose |
|---|---|---|
| POST | `/api/blast` | Manual blast: generate with current settings and apply. Returns the generated layer. |
| POST | `/api/blast/apply` | `{layer, backup: bool}`: apply a given layer. |
| GET | `/api/blast/units` | Units currently scheduled in the emulator. |
| DELETE | `/api/blast/units` | Clear all scheduled units. |
| POST | `/api/blast/toggle` | `{on: bool}` BlastLayer ON/OFF using the uncorrupt backup. |
| POST | `/api/blast/reroll` | `{layer}` → rerolled layer. |

### Blast layer JSON

`{ "note": string, "units": [Unit] }` where `Unit` mirrors RTCV's BlastUnit:
`enabled, locked, bigEndian, domain, address, precision, source (value|store),
value (hex), sourceDomain, sourceAddress, storeTime (immediate|preexecute),
storeType (once|continuous), tilt (string decimal, may exceed int64), executeFrame, lifetime, loop, loopTiming,
limiterTime, limiterList, invertLimiter, generatedUsingValueList, note`.
`.bl` files are this object.

### Glitch Harvester

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/savestates` | Savestate slots `[{slot, key?, label, game}]` (fixed number of slots, e.g. 10 pages of 5). |
| POST | `/api/savestates/{slot}` | Save the current state into the slot (creates a stash key with `parentKey == key`). |
| POST | `/api/savestates/{slot}/load` | Load the slot's state. |
| PATCH | `/api/savestates/{slot}` | `{label}` |
| DELETE | `/api/savestates/{slot}` | |
| GET | `/api/stash` | Stash history `[StashKey]` |
| POST | `/api/stash/corrupt` | `{slot, loadBefore: bool}`: corrupt on the slot's state, add to history. Returns the new key. |
| POST | `/api/stash/inject` | `{key, slot}`: apply key's layer on the slot's state. |
| POST | `/api/stash/{key}/run` | Load state + apply layer. |
| POST | `/api/stash/{key}/original` | Load state only. |
| POST | `/api/stash/{key}/reroll` | New key with a rerolled layer, run it. |
| POST | `/api/stash/merge` | `{keys: []}` |
| PATCH | `/api/stash/{key}` | `{alias, note}` |
| DELETE | `/api/stash/{key}` / `/api/stash` | Remove one / clear. |
| POST | `/api/stash/{key}/to-stockpile` | Move into the stockpile. |
| GET | `/api/stash/{key}/layer` / PUT | Read / replace the key's layer (Blast Editor). |
| GET | `/api/stockpile` | `[StashKey]` |
| POST | `/api/stockpile/{key}/run` | |
| PATCH / DELETE | `/api/stockpile/{key}` | rename, note / remove |
| POST | `/api/stockpile/reorder` | `{keys: []}` |
| DELETE | `/api/stockpile` | clear |
| GET | `/api/stockpile/export` | Download `.sks` (zip: `stockpile.json` + state blobs + lists). |
| POST | `/api/stockpile/import` | Upload `.sks` (multipart); `?merge=true` merges. |
| POST | `/api/stockpile/save` / `load` | `{path}` on the core host. |

StashKey JSON: `{key, parentKey, alias, note, game: {title, code, romPath, system}, selectedDomains, layer?, createdAt}`.
Savestate blobs are stored as files in `data/states/<key>.state`.

### Game protection

| Method | Path | Purpose |
|---|---|---|
| POST | `/api/protection/backup` | Take a backup now. |
| POST | `/api/protection/back` | Load the most recent backup and drop it. |

### Lists

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/lists` | `[{name, precision, entries}]` from `data/lists/*.txt`. |
| POST | `/api/lists` | Upload a list file. |
| DELETE | `/api/lists/{name}` | |

### Events (SSE)

`GET /api/events` streams `event: <type>` + `data: <json>`:

- `status`: the same object as `/api/status` (sent on connect and every change).
- `frame`: `{frame}` at most 10 times per second.
- `blast`: `{count, engine, elapsedMs}` after every blast.
- `stash`, `stockpile`, `savestates`, `settings`, `domains`: "changed, refetch".
- `log`: `{level, msg}` for user-facing messages.

## Not in the first release

Virtual memory domains, Blast Generator, plugins, rendering, hotkeys,
Simple Mode, multiplayer. The API is designed so they can be added.
