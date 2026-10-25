# Contributing to Pluri-Deliver

Thanks for your interest! This guide covers everything you need to hack on
Pluri-Deliver productively — and safely, since this app embeds a scripting
runtime and talks to live carrier endpoints.

## Development setup

```
Requirements
├── Go 1.21+          (1.25 for release builds)
├── Node.js 18+       (frontend build)
└── Wails CLI         go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

```bash
git clone https://github.com/josepilas/pluri-deliver.git
cd pluri-deliver
(cd frontend && npm install)

wails dev      # hot-reload development window
```

## Project layout

```
main.go / app.go            Wails wiring, window/tray lifecycle, bindings
internal/engine/            Lua sandbox, carrier registry, HTTP bridge
internal/engine/carriers/   one .lua per carrier (embedded, user-overridable)
internal/engine/scripts/    polling.lua + notify.lua (policy scripts)
internal/service/           refresh orchestration, notifications, captcha/CPF flows
internal/store/             JSON persistence (%APPDATA%\Pluri-Deliver\)
internal/scheduler/         per-tracking poll scheduling
internal/{correios,jt,spx,cainiao}/  native Go clients for gated carriers
internal/{tray,notify,autostart}/    Windows integration (syscall-based)
frontend/                   React + Vite UI (EN/pt-BR)
scripts/build-legacy.sh     Windows 7/8.1 build (Go 1.20 + Wails 2.8.1)
```

## Testing

```bash
go test ./...              # full suite (fast, mostly offline)
go vet ./...

# live endpoint checks (real network — maintainers only)
go run livecarriers.go     # Amazon / DHL / UPS / Loggi
go run livecainiao.go      # Cainiao → Correios chain
go run livetest.go         # 17TRACK web engine
go run livecpt.go          # Correios captcha image fetch
```

When you touch an adapter, add a fixture test in
`internal/engine/directadapters_test.go` / `webadapter_test.go` — capture a
real response from the endpoint, trim it, and assert the parser output.

## Adding a carrier adapter

See [`docs/CARRIERS.md`](docs/CARRIERS.md) for the full Lua contract. The short
version: one file per carrier in `internal/engine/carriers/` returning a module
table with `id`, `name`, `detect(code)` and optionally `track(ctx, code)`.
The sandbox hands you `ctx.http.get/post`, `ctx.json`, `ctx.now/iso/hash` — and
nothing else (no filesystem, no os, no require).

## Hard rules

1. **No API keys, no paid services.** Integrations must work against public,
   keyless endpoints. If a carrier only offers a keyed API, route it through
   the 17TRACK web engine fallback instead.
2. **The sandbox stays sealed.** Never expose the filesystem, os or require to
   adapter scripts; user-installed scripts must never see aggregator keys and
   must stay blocked from loopback/link-local addresses.
3. **Time-budget every Lua path** (load, detect, track) so a hostile script
   cannot wedge the app.
4. **gofmt + go vet + go test ./... green** before every commit. Frontend
   changes must build (`cd frontend && npm run build`).
5. **Bump `appVersion`, `wails.json`, `frontend/package.json` together**, and
   document user-visible changes in `CHANGELOG.md`.

## Release process

1. Update the three version fields + `CHANGELOG.md`.
2. `go test ./...`, `wails build`, `scripts/build-legacy.sh` (modern x64/x86 +
   legacy x64/x86).
3. Package `Pluri-Deliver-<version>-windows-all.zip` (4 exes + README).
4. Tag `vX.Y.Z` and push — GitHub Actions builds and publishes the release.

## License

By contributing, you agree that your contributions are licensed under the
[MPL 2.0](./LICENSE). Add the header below to every new source file:

```
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.
```
