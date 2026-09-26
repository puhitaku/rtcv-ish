# rtcv-ish web frontend

Vue 3 + TypeScript + Pinia + Tailwind, built with Vite into `../internal/webui/dist`
(embedded into `rtcv-ish` by the next `go build`).

- `npm run dev`: dev server; `/api` is proxied to `http://127.0.0.1:8420` (run `rtcv-ish` there).
- `npm run build`: type-check and build into `../internal/webui/dist`.
- `npm run test`: Vitest unit and component tests. `npm run test:e2e`: Playwright; builds the
  frontend and the Go binaries, then drives `rtcv-ish` with `rtcv-ish-fakeemu`.
- `npm run test:e2e:melonds`: the same Playwright flow against a real melonDS. It sets nothing
  itself; set `RTCVISH_MELONDS` to the melonDS executable or `.app` bundle of the rtcv-ish
  fork (e.g. `RTCVISH_MELONDS=../emulators/melonds/build/local/melonDS.app npm run
test:e2e:melonds`). Any `test:e2e` run uses melonDS whenever `RTCVISH_MELONDS` is set. The ROM
  is `RTCVISH_ROM`, default `../test/roms/hello_world.nds`; run `scripts/build-nds-examples.sh`
  to build it (the run fails when it is missing). melonDS runs muted (volume 0 and
  `SDL_AUDIODRIVER=dummy`) with a temporary config dir and the software renderer, on a free port.
- `npm run lint`, `npm run format`, `npm run typecheck`.
- API types in `src/api/schema.d.ts` are generated from `api/frontend/openapi.yaml` by
  `scripts/gen.sh` (or `npm run gen`); do not edit them by hand.
