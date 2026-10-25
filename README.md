<div align="center">

<img src="build/appicon.png" width="120" alt="Pluri-Deliver" />

# Pluri-Deliver

**Track all your deliveries in one place — keyless, private, free.**

A Windows desktop parcel tracker for Brazil and the world: Correios/Sedex, J&T Express,
SPX Express, Amazon Shipping, DHL, UPS, Loggi, Cainiao and 2500+ more carriers through
the free 17TRACK web engine — with **no accounts, no API keys and no paid services**.

[![License: MPL 2.0](https://img.shields.io/badge/License-MPL%202.0-brightgreen.svg)](./LICENSE)
[![Windows](https://img.shields.io/badge/platform-Windows%207%20SP1%20%E2%86%92%2011-blue)](#downloads)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev)
[![Lua](https://img.shields.io/badge/%E2%9A%99%EF%B8%8F-Lua%20adapters-2C2D72?logo=lua)](#the-lua-adapter-system)

*Made by José Pilas*

</div>

---

## Downloads

Grab the latest release from the [**Releases**](../../releases) page:

| File | For |
|---|---|
| `Pluri-Deliver-Setup-x64.exe` / `Pluri-Deliver.exe` | **Windows 10 / 11** (modern build) |
| `Pluri-Deliver-Legacy-x64.exe` | **Windows 7 SP1 / 8 / 8.1** (x64, needs [WebView2 Runtime 109](https://developer.microsoft.com/en-us/microsoft-edge/webview2/)) |
| `Pluri-Deliver-Legacy-x86.exe` | **Windows 7 SP1 / 8 / 8.1** (32-bit) |

> **Windows Vista is not supported** — the WebView2 rendering engine never existed for it
> and the Go toolchain requires Windows 7 or newer. Windows 7 SP1 is the practical floor.

## Why Pluri-Deliver?

- **No accounts, no API keys.** Every carrier integration talks to the same public,
  keyless endpoints the carriers' own websites use. Nothing to sign up for, nothing to pay.
- **Private by design.** All data lives in a local JSON file
  (`%APPDATA%\Pluri-Deliver\`) — no cloud, no telemetry, no third-party servers.
- **Smart polling.** A Lua-scripted scheduler decides per package when to check next:
  out-for-delivery parcels every ~15 min, delivered ones twice a day, stale ones hourly.
- **Desktop notifications** with a tray icon, background operation, silent auto-start
  with Windows, and balloon fallbacks for older systems.
- **Bilingual** — English and Brazilian Portuguese, switchable in Settings.
- **Scriptable.** Carriers, the polling strategy and the notification rules are plain
  Lua files you can read, tweak and override without recompiling anything.

## Supported carriers

| Carrier | Code shape | How it tracks (all keyless) |
|---|---|---|
| **Correios / Sedex / PAC** | `SS123456789BR` | Cainiao global network → 17TRACK web → official portal with a one-time captcha pill as last resort |
| **Amazon Shipping** | `TBA123456789000` | Direct — Amazon's public tracker (`track.amazon.com`), no account needed |
| **DHL** (Express / eCommerce / GF) | `JVGL…`, `1234567890`, `3S…`, `JJD…`, `LBAA…` | Direct — DHL's own `utapi` endpoint → 17TRACK web fallback |
| **UPS** | `1Z…` (18 chars) | Direct — ups.com tracking API with an XSRF session handshake → 17TRACK web fallback |
| **J&T Express Brasil** | `JT…` | Direct — official API (asks for the recipient's CPF once per parcel) |
| **SPX Express** (Shopee) | `SPXBR…` | Direct — keyless portal query |
| **Loggi** | `LOG…` | Direct — Loggi's public rastreador GraphQL (current status per poll) |
| **Cainiao / AliExpress** | `LP…`, `CN2…` | Direct — Cainiao global API |
| **2500+ more** (FedEx, USPS, China Post…) | any | 17TRACK web engine — the same endpoint the 17track.net site itself uses |

### Test codes

Numbers commonly used to exercise the integrations (some are documentation
examples — not live parcels — but they exercise the full request/response path):

| Carrier | Example code |
|---|---|
| UPS | `1Z12345E0205271688` (classic UPS test number) |
| DHL | `11898773100` (DHL Polska test), `LBAA19526` (DGF sample) |
| Amazon | `TBA123456789000` (Amazon Shipping docs example) |
| Correios/Sedex | `AA123456789BR` / `NX123456789BR` (format examples) |
| Loggi | `LOG123456789` (format example) |
| Demo | any code starting with `DEMO` (built-in demo timeline) |

## The Lua adapter system

Pluri-Deliver's brain is scripted in Lua and runs inside a hardened sandbox:

```
internal/engine/carriers/*.lua   — one file per carrier: detection + tracking + demo data
internal/engine/scripts/polling.lua — decides how many minutes until the next check
internal/engine/scripts/notify.lua  — builds every desktop notification
%APPDATA%\Pluri-Deliver\carriers\*.lua — your overrides (win over the built-ins)
%APPDATA%\Pluri-Deliver\scripts\*.lua  — your policy overrides
```

The sandbox gives each script **no filesystem, no OS access, no `require`** — only
`ctx.http` (rate-limited, scheme-restricted, loopback-blocked for user scripts),
`ctx.json`, time helpers and per-call execution budgets. A hostile or buggy script
cannot hang the app, read your data or exfiltrate anything you didn't put in it.

Want to add a carrier? Drop a `.lua` file in the folder above — see
[`docs/CARRIERS.md`](docs/CARRIERS.md) for the full contract.

## Building from source

Requirements: [Go 1.21+](https://go.dev/dl/), [Node.js 18+](https://nodejs.org), and the
[Wails v2 CLI](https://wails.io):

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# frontend deps (once)
cd frontend && npm install && cd ..

# build (produces build/bin/Pluri-Deliver.exe)
wails build

# run the test suite
go test ./...
```

Legacy (Windows 7 SP1–8.1) binaries are produced by
[`scripts/build-legacy.sh`](scripts/build-legacy.sh), which stages a
dependency-downgraded copy (Go 1.20 + Wails v2.8.1) and cross-compiles
amd64 + 386.

Live endpoint checks for maintainers:

```bash
go run livecarriers.go   # Amazon / DHL / UPS / Loggi through the real engine
go run livecainiao.go    # Cainiao + Correios chain
```

## Architecture

```
┌──────────────────────────── Wails v2 (Go) ────────────────────────────┐
│  React/Vite frontend (EN / pt-BR)  ↔  Go services (store, scheduler, │
│  notifications, tray, autostart)                                      │
│                                                                      │
│  Engine: sandboxed gopher-lua runtime                               │
│   ├── 14 carrier adapters  ── direct keyless endpoints               │
│   ├── polling.lua / notify.lua  ── user-overridable policies        │
│   └── fallback chain: direct → keyed aggregator → 17TRACK web       │
│                                                                      │
│  JSON storage in %APPDATA%\Pluri-Deliver\ (no cloud, no accounts)    │
└──────────────────────────────────────────────────────────────────────┘
```

## Documentation

- [`CHANGELOG.md`](CHANGELOG.md) — release history
- [`docs/CARRIERS.md`](docs/CARRIERS.md) — write your own carrier adapter
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — dev setup, tests, release process
- [`SECURITY.md`](SECURITY.md) — sandbox model and responsible disclosure

## License

[ Mozilla Public License 2.0](./LICENSE) © 2026 **José Pilas**.

Made with care by **José Pilas**.
