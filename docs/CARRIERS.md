# Writing a carrier adapter

Pluri-Deliver carriers are single Lua files. Drop one in
`%APPDATA%\Pluri-Deliver\carriers\` and it is picked up at the next start —
no recompile, no install. The same contract powers the built-in adapters in
`internal/engine/carriers/`.

## The module table

```lua
local M = {
  id = "mycarrier",           -- unique, lowercase
  name = "My Carrier",       -- shown in the UI
  country = "BR",            -- ISO code or ""
  kind = "builtin",          -- builtin | aggregator | demo
  requires_key = false,      -- true only for keyed aggregators
  website = "https://…/",
  note = "How this carrier tracks.",
}

function M.detect(code)          -- -> boolean
  return code:upper():match("^MY[%u%d]+$") ~= nil
end

function M.track(ctx, code)       -- -> events[] | nil, errString
  local r = ctx.http.get("https://api.example.com/track/" .. code, {
    ["Accept"] = "application/json",
    ["User-Agent"] = "Mozilla/5.0 …",
  })
  if r.status ~= 200 then return nil, "HTTP " .. r.status end
  local ok, data = ctx.json.decode(r.body)
  if not ok then return nil, "bad json" end
  local out = {}
  for _, ev in ipairs(data.events or {}) do
    out[#out+1] = {
      date = ev.timestamp,            -- ISO string or epoch seconds (number)
      status = "in_transit",          -- pending | in_transit | out_for_delivery
                                        -- | delivered | exception | info
      description = ev.description,  -- free text (required)
      location = ev.city,            -- optional
      source = "MyCarrier",          -- who reported it
    }
  end
  return out                          -- empty list -> treated as an error
end

return M
```

## The `ctx` sandbox

You get — and only get:

| Helper | What it does |
|---|---|
| `ctx.http.get(url, headers)` | GET → `{status, body, cookies()}` |
| `ctx.http.post(url, body, headers)` | POST → same shape |
| `ctx.json.decode(str)` / `ctx.json.encode(v)` | JSON bridge |
| `ctx.now()`, `ctx.iso(epoch)` | time helpers |
| `ctx.hash(s)` | deterministic [0,1) hash (demo pacing) |
| `ctx.added` | tracking creation epoch (demo pacing) |
| `ctx.hint` | carrier id that spawned a fallback query (aggregators) |
| `ctx.settings.language` | `"en"` or `"pt-BR"` |

`cookies()` returns the `Set-Cookie` names/values of the response — use them to
build stateful handshakes (see `ups.lua` for a full XSRF example) by passing
them back as a `Cookie` header explicitly.

**Not available:** `os`, `io`, `require`, `load`, `dofile`, filesystem,
environment, subprocesses. User-installed scripts additionally cannot reach
loopback/link-local hosts, and never see aggregator API keys.

## Time budgets

Every call is deadline-bounded: script load 10 s, `detect` 5 s, `track` 30 s.
Do your work and return — polling is the scheduler's job, not yours.

## Fallback chain

If `track` returns `nil, err`, the engine (for builtin carriers) retries the
number through the configured aggregator and then the keyless 17TRACK web
engine before giving up — your adapter just needs to fail loudly.

## Demo mode

Add a `M.demo(ctx, code)` generator (see any built-in adapter) so Demo Mode
works when your carrier is selected. Reveal one stage every 2 minutes using
`ctx.now() - ctx.added` and `ctx.hash(code)` for deterministic jitter.

## Testing

Follow `internal/engine/directadapters_test.go`: embed your script, decode a
**real captured response** through the bridge, and assert the parsed events.
Expose internal parsers as `M._collect_events` test hooks.
