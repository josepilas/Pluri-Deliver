# Security Policy

## Supported versions

Only the latest release line receives security fixes.

## Reporting a vulnerability

Please open a private security advisory
(GitHub → Security → Advisories → "Report a vulnerability") or email the
maintainer directly. Include reproduction steps and, when relevant, a PoC.
You will hear back within a few days; fixes ship as patch releases.

## Threat model

Pluri-Deliver runs third-party-loadable Lua scripts (carrier adapters and the
policy scripts) inside a sandbox. The security-relevant guarantees are:

1. **No ambient authority.** Script states are created with
   `SkipOpenLibs`; `os`, `io`, `package`, `debug`, `dofile`, `load*` and
   `require` are absent. Scripts cannot touch the filesystem, spawn processes
   or load new code.
2. **Every execution path is time-budgeted** (script load, `detect`, `track`,
   policy calls). A hostile infinite loop is killed by the context deadline,
   not by the user's patience.
3. **Network egress is constrained.** Only `http`/`https` schemes; scripts
   installed by the user (untrusted) are additionally blocked from loopback,
   link-local (including cloud metadata addresses) and unspecified hosts, so
   a malicious adapter cannot probe the local machine. Responses are capped
   at 4 MiB.
4. **Secrets are least-privilege.** The optional aggregator API key is only
   injected into trusted, embedded adapters that declare `requires_key`;
   user-installed scripts always see an empty key.
5. **Data stays local.** Tracking data, settings and events live in
   `%APPDATA%\Pluri-Deliver\` as plain JSON; the app runs no servers and
   phones nothing home beyond the carrier endpoints themselves.

Known limitation (documented, by design): hostname-based blocking is
best-effort against literal addresses; a DNS name that resolves into a
private range is not blocked — self-hosted aggregators on LAN remain
reachable intentionally.

## Carrier endpoints

All integrations use public, keyless endpoints (the same ones the carriers'
own websites call). Treat carrier responses as untrusted input: they are
parsed with bounded allocations and never executed.
