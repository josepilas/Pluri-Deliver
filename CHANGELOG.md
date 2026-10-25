# Changelog

All notable changes to Pluri-Deliver are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);

## [1.7.0] — 2026-10-25

### Added
- **Amazon Shipping (keyless)** — direct integration with Amazon's public tracker
  (`track.amazon.com/api/tracker`): TBA/TBM/TBC codes track without an Amazon account.
- **DHL (keyless)** — direct integration with DHL's own `utapi` endpoint
  (Express / eCommerce / Global Forwarding), with a much broader code detector
  (JVGL, JJD, JD, 3S, 10–11 digit waybills, GM/LX, LB* freight references);
  falls back to the 17TRACK web engine whenever DHL's edge refuses a connection.
- **UPS (keyless)** — direct integration with ups.com's tracking API via the
  XSRF session handshake (cookie bridge added to the Lua sandbox); falls back
  to the 17TRACK web engine.
- **Loggi (new carrier)** — direct integration with Loggi's public rastreador
  GraphQL API (`statusQuery`); current status + ETA per poll, one event per
  status change, pt-BR demo timeline.
- **Sedex / domestic Correios captcha fallback restored** — numbers the
  automatic chain (Cainiao → 17TRACK) cannot resolve now surface a
  "needs verification" pill (the v1.2 official-portal flow) instead of a
  dead-end error; the batch "Sync all with captcha" works again (selection
  fixed to the correios carrier).
- Lua HTTP bridge now exposes response **cookies** (`resp.cookies()`), enabling
  stateful carrier handshakes (UPS) without a hidden global cookie jar.
- New app icon.
- Repository prepared for public distribution: MPL 2.0 license, README,
  CHANGELOG, CONTRIBUTING, SECURITY, CI + release workflows.

### Changed
- License changed from MIT to **MPL 2.0**.
- `amazon` carrier renamed "Amazon Shipping" (tracks TBA/TBM/TBC directly).

## [1.6.0]

- X button fixed for real (Wails v2 has no JS WindowClose — new `RequestClose`
  Go binding applies native-close semantics).
- Tray icon (pure syscall, Shell_NotifyIconW) with click-to-reopen and
  right-click menu; balloon notifications fallback for older Windows.
- **More Lua**: polling strategy and notification rules are now user-scriptable
  (`polling.lua`, `notify.lua`) with embedded defaults; user copies in
  `%APPDATA%\Pluri-Deliver\scripts` win.
- Windows 7 SP1 → 11 support matrix: legacy builds (Go 1.20 + Wails v2.8.1,
  x64 + x86, `legacywin` build tag, balloon-only notifications); manifest
  supportedOS GUIDs; Vista documented as impossible (no WebView2, Go floor is 7).

## [1.5.0]

- Background mode: closing the window keeps the app running (Settings toggle),
  notifications keep flowing; "Close application" button for a real exit;
  Windows-login silent start.
- Friend-review fixes: notifications fire exactly once per event (watermark
  separate from viewed-state), Lua sandbox time budgets on every execution
  path, per-adapter key permissions, network restrictions for user scripts,
  unparseable dates keep stable IDs (no phantom duplicates), staged store
  writes (no memory/disk drift on failure).

## [1.4.0]

- Captcha-free Correios via Cainiao's global network (full China→Brazil
  import history in Correios' own wording).
- Start-with-Windows (registry Run key) and refresh-on-launch.

## [1.3.0]

- SPX Express direct client (keyless portal).
- J&T Brasil CPF flow (UI collects the CPF once per tracking).

## [1.2.0]

- Correios direct engine with the captcha flow (official portal + batch sync).

## [1.1.0]

- Free web-endpoint interception replaces paid APIs: 17TRACK web engine
  (register → primer → track handshake, keyless).

## [1.0.0]

- Initial release: Wails + React desktop app, JSON storage, smart polling,
  desktop notifications, EN/pt-BR, demo mode, carrier auto-detection.
