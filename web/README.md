# rtcv-ish web frontend

Vue 3 + TypeScript + Pinia + Tailwind, built with Vite into `../internal/webui/dist`
(embedded into `rtcv-ish` when built with `-tags embedweb`).

- `npm run dev`: dev server; `/api` is proxied to `http://127.0.0.1:8420` (run `rtcv-ish` there).
- `npm run build`: type-check and build into `../internal/webui/dist`.
- `npm run test`: Vitest unit and component tests. `npm run test:e2e`: Playwright; builds the
  frontend and the Go binaries, then drives `rtcv-ish` with `rtcv-ish-fakeemu`.
- `npm run lint`, `npm run format`, `npm run typecheck`.
- API types in `src/api/schema.d.ts` are generated from `api/frontend/openapi.yaml` by
  `scripts/gen.sh` (or `npm run gen`); do not edit them by hand.
