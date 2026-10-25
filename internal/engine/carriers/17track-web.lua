-- Pluri-Deliver universal adapter: 17TRACK web engine (no API key)
-- ---------------------------------------------------------------------------
-- Talks to the same public endpoint the 17TRACK website itself uses
-- (https://t.17track.net/restapi/track), so every carrier in their
-- 2500+ catalogue works out of the box - Correios, J&T, DHL, FedEx, UPS,
-- USPS, China Post... - without any signup or paid API key.
--
-- Protocol (verified against the live endpoint):
--   1. POST /restapi/register  {"guid":G,"data":[{num,fc,sc}]}   (warm-up; answers 302 - ignored)
--   2. POST /restapi/track     {"guid":G,"data":[{num,fc,sc}]}   (primer; answers notN - ignored)
--   3. POST /restapi/track     {"guid":G,"data":[{num,fc,sc,param:null}]}
--      -> {"shipments":[{"code":200,"number":...,"carrier_final":<int>,
--          "shipment":{"latest_status":{...},"tracking":{"providers":[
--            {"provider":{"name":"USPS",...},"events":[{"time_iso":"...",
--             "description":"...","location":"CHINA,WUHAN","key_stage":"PickedUp"}]}]}},
--          "state":"Success"}],"meta":{"code":200,"message":"Ok"}}
--
-- Fresh numbers come back as state="ConcurrencyLimit" (queued on their
-- side). The adapter retries a few times in-call; anything still pending is
-- reported as QUEUED so the app schedules a follow-up automatically, and
-- the next scheduler pass picks it up for good. Numbers already known to
-- 17TRACK (most real-world parcels) answer with events on the first call.
-- ---------------------------------------------------------------------------
local M = {
  id = "17track-web",
  name = "17TRACK (web)",
  country = "",
  kind = "aggregator",
  requires_key = false,
  website = "https://www.17track.net/",
  note = "Free universal engine - tracks 2500+ carriers, no API key needed.",
}

function M.detect(_code)
  return false -- never auto-selected; used as the global fallback
end

local API = "https://t.17track.net/restapi/"

local HEADERS = {
  ["Content-Type"] = "application/json",
  ["Accept"] = "application/json, text/plain, */*",
  ["Origin"] = "https://t.17track.net",
  ["Referer"] = "https://t.17track.net/en",
  ["User-Agent"] = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
}

-- Map 17TRACK key stages to Pluri-Deliver statuses.
local function map_stage(stage)
  if stage == "Delivered" then return "delivered" end
  if stage == "OutForDelivery" or stage == "AvailableForPickup" then return "out_for_delivery" end
  if stage == "InfoReceived" or stage == "PickedUp" then return "pending" end
  if stage == "Returning" or stage == "Returned" then return "exception" end
  return nil -- Departure/Arrival/etc: let Go derive from the description
end

-- Deterministic 32-hex guid derived from the code, so the guest session is
-- stable per number across refreshes (17TRACK keeps per-session state and
-- caches previously queried numbers against the guid).
local function session_guid(ctx, code)
  local h1 = math.floor(ctx.hash(code) * 4294967295)
  local h2 = math.floor(ctx.hash(code .. "#pluri") * 4294967295)
  local h3 = (h1 * 31 + h2) % 4294967295
  return string.format("%08x%08x%08x%08x", h1, h2, h3, (h1 + h2 + h3) % 4294967295)
end

-- One full handshake: register -> primer -> query. Returns the decoded
-- query response table, or nil + error.
local function handshake(ctx, guid, code)
  local base = '{"guid":"' .. guid .. '","data":[{"num":"' .. code .. '","fc":0,"sc":0'
  ctx.http.post(API .. "register", base .. "}]}", HEADERS)   -- warm-up (302 is fine)
  ctx.http.post(API .. "track", base .. "}]}", HEADERS)      -- primer
  local r = ctx.http.post(API .. "track", base .. ',"param":null}]}', HEADERS)
  if r.status ~= 200 then
    return nil, "17TRACK web returned HTTP " .. tostring(r.status)
  end
  local ok, data = ctx.json.decode(r.body)
  if not ok or type(data) ~= "table" then
    return nil, "invalid 17TRACK web response"
  end
  return data
end

-- Collect events from every provider (each may carry a different slice of
-- the journey; the final-mile one usually has the richest history) and
-- dedupe by (time_iso | description).
local function collect_events(shipment)
  local out = {}
  local seen = {}
  local tracking = shipment.tracking
  if type(tracking) ~= "table" or type(tracking.providers) ~= "table" then
    return out
  end
  for _, prov in ipairs(tracking.providers) do
    local pname = "17TRACK"
    if type(prov.provider) == "table" and prov.provider.name then
      pname = prov.provider.name
    end
    local evs = type(prov.events) == "table" and prov.events or {}
    for _, ev in ipairs(evs) do
      if type(ev) == "table" and ev.description then
        local key = tostring(ev.time_iso or "") .. "|" .. tostring(ev.description)
        if not seen[key] then
          seen[key] = true
          out[#out+1] = {
            date = ev.time_iso or "",
            status = map_stage(ev.key_stage) or "info",
            description = ev.description,
            location = ev.location or "",
            source = pname,
          }
        end
      end
    end
  end
  -- 17TRACK lists newest first; our timeline is chronological (oldest first).
  table.sort(out, function(a, b) return tostring(a.date) < tostring(b.date) end)
  return out
end

function M.track(ctx, code)
  code = string.gsub(string.upper(tostring(code or "")), "[^%w]", "")
  if code == "" then
    return nil, "empty tracking code"
  end

  local guid = session_guid(ctx, code)
  local last_state = nil

  -- Two handshakes: numbers already known to 17TRACK answer with events on
  -- the first call; brand-new ones get a live carrier query queued on their
  -- side - the QUEUED error we then report makes the app retry in ~90s
  -- (see service.Refresh), which is usually enough for events to land.
  for _ = 1, 2 do
    local data, err = handshake(ctx, guid, code)
    if not data then
      return nil, err or "17TRACK web request failed"
    end
    local meta = data.meta or {}
    local shipments = data.shipments
    if type(shipments) == "table" and #shipments > 0 then
      local sh = shipments[1]
      last_state = sh.state or meta.message
      if type(sh.shipment) == "table" then
        local events = collect_events(sh.shipment)
        if #events > 0 then
          return events
        end
        -- shipment object present but no events yet -> keep trying
      end
    elseif meta.code == -4 then
      last_state = "notN"
    else
      last_state = meta.message or "empty response"
    end
  end

  if last_state == "ConcurrencyLimit" or last_state == "Ok" then
    return nil, "QUEUED:17TRACK is looking up this number - it usually takes a minute or two. Events will appear automatically; no action needed."
  end
  if last_state == "notN" then
    return nil, "17TRACK could not recognize this tracking number. Double-check the code - or turn on Demo mode to explore the app."
  end
  return nil, "17TRACK web: " .. tostring(last_state)
end

-- Demo timeline (used when demo mode is on while this adapter is selected).
function M.demo(ctx, code)
  local stages = {
    { status = "pending",           description = "Shipment information received", location = "Origin" },
    { status = "in_transit",        description = "In transit", location = "International hub" },
    { status = "in_transit",        description = "Arrived in destination country", location = "Customs" },
    { status = "out_for_delivery",  description = "Out for delivery", location = "Destination city" },
    { status = "delivered",         description = "Delivered", location = "Destination" },
  }
  return demo_timeline(ctx, code, stages)
end

function demo_timeline(ctx, code, stages)
  local step = 120
  local done = math.min(#stages, math.floor((ctx.now() - ctx.added) / step) + 1)
  local h = ctx.hash(code)
  local out = {}
  for i = 1, done do
    local s = stages[i]
    local when = ctx.added + (i - 1) * step - math.floor(h * 3600)
    out[#out+1] = {
      date = ctx.iso(when),
      status = s.status,
      description = s.description,
      location = s.location,
    }
  end
  return out
end

-- Test hook: exposes the internal parser so Go tests can feed it real
-- response samples captured from the endpoint.
M._collect_events = collect_events
M._map_stage = map_stage

return M
