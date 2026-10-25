-- Pluri-Deliver carrier adapter: DHL (Express, eCommerce, Global Forwarding)
-- ---------------------------------------------------------------------------
-- Keyless direct integration with the background API DHL's own Track & Trace
-- SPA uses (https://www.dhl.com/us-en/home/tracking.html). Verified protocol:
--   GET https://www.dhl.com/utapi?trackingNumber=<code>
--   -> {"shipments":[{"id":"...","status":{"statusCode":"...","description":...},
--        "details":{"dgf:routes":[{"dgf:estimatedArrivalDate":"..."}]},
--        "events":[{"timestamp":"2026-08-27T17:05:00+08:00","statusCode":"...",
--          "description":"...","location":{"address":{"addressLocality":"..."}}}],
--        "origin":{"address":{...}},"destination":{"address":{...}}}]}
-- DHL's edge (Akamai) refuses obvious bot traffic — datacenter IPs get an
-- explicit block page, and the request then fails here with a non-JSON body.
-- From residential connections it answers exactly like the website. When the
-- direct call fails, the engine falls back to the 17TRACK web engine chain.
local M = {
  id = "dhl",
  name = "DHL",
  country = "DE",
  kind = "builtin",
  requires_key = false,
  website = "https://www.dhl.com/",
  note = "Direct via DHL's public utapi endpoint; falls back to the 17TRACK web engine when DHL blocks the request.",
}

function M.detect(code)
  local c = code:upper():gsub("[ %-]", "")
  if #c < 7 then return false end
  -- DHL Express waybill: 10-11 digits.
  if c:match("^%d%d%d%d%d%d%d%d%d%d%d?$") then return true end
  -- DHL eCommerce / Paket: JVGL, JJD, JD + digits.
  if c:match("^JVGL%d+$") then return true end
  if c:match("^JJD%d+$") then return true end
  if c:match("^JD%d%d%d+$") then return true end
  -- DHL Parcel (NL/PL/...): 3S + 9 alphanumerics.
  if c:match("^3S[%u%d]+$") then return true end
  -- DHL eCommerce US: GM/LX/GG + 15+ digits (S10-ish "GM...").
  if c:match("^GM%d+$") and #c >= 15 then return true end
  if c:match("^LX%d+$") and #c >= 15 then return true end
  -- DHL Global Forwarding: LB + letter(s) + digits (ocean/air freight refs,
  -- e.g. LBAA19526).
  if c:match("^LB[%u]*%d+$") then return true end
  return false
end

local API = "https://www.dhl.com/utapi?trackingNumber="

local HEADERS = {
  ["Accept"] = "application/json, text/plain, */*",
  ["Accept-Language"] = "en-US,en;q=0.9",
  ["Referer"] = "https://www.dhl.com/us-en/home/tracking.html",
  ["User-Agent"] = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
}

-- Map DHL utapi statusCode values to Pluri-Deliver statuses; unknown codes
-- return nil so Go derives from the description text instead.
local function map_status(code)
  local c = string.lower(tostring(code or ""))
  if c == "del" or c == "ok-d" or c == "delivered" then return "delivered" end
  if c == "od" or c == "ok-wdh" or c == "out for delivery" then return "out_for_delivery" end
  if c == "ok" or c == "transit" or c == "pl" or c == "dep"
     or c == "arr" or c == "hbt" or c == "acc" then return "in_transit" end
  if c == "pu" or c == "pickup" or c == "oh" or c == "fc" or c == "pre-transit"
     or c == "pretransit" then return "pending" end
  if c == "undel" or c == "except" or c == "exception" or c == "rt"
     or c == "returned" then return "exception" end
  return nil
end

-- collect_shipment extracts the first shipment table from a decoded utapi
-- payload (exposed as M._collect_events for the Go test suite).
local function collect_events(data)
  local out = {}
  if type(data) ~= "table" or type(data.shipments) ~= "table" then
    return out, "no shipments in response"
  end
  local sh = data.shipments[1]
  if type(sh) ~= "table" then
    return out, "empty shipments list"
  end
  local evs = type(sh.events) == "table" and sh.events or {}
  for _, ev in ipairs(evs) do
    if type(ev) == "table" and (ev.description or ev.status) then
      local loc = ""
      if type(ev.location) == "table" and type(ev.location.address) == "table" then
        loc = tostring(ev.location.address.addressLocality or "")
      end
      out[#out+1] = {
        date = tostring(ev.timestamp or ""),
        status = map_status(ev.statusCode or ev.status),
        description = tostring(ev.description or ev.status or "DHL scan"),
        location = loc,
        source = "DHL",
      }
    end
  end
  -- utapi lists newest first; our timeline is chronological.
  table.sort(out, function(a, b) return tostring(a.date) < tostring(b.date) end)
  return out
end

function M.track(ctx, code)
  code = tostring(code or ""):gsub("[ %-]", "")
  if code == "" then
    return nil, "empty tracking code"
  end

  local r = ctx.http.get(API .. code, HEADERS)
  if r.status ~= 200 then
    -- Akamai bot block page / rate limit: fall through to the 17TRACK chain.
    return nil, "DHL returned HTTP " .. tostring(r.status) .. " for this connection"
  end
  local ok, data = ctx.json.decode(r.body)
  if not ok or type(data) ~= "table" then
    return nil, "DHL refused this request (bot protection) or sent an invalid body"
  end
  local events, err = collect_events(data)
  if err then
    return nil, "DHL: " .. err
  end
  if #events == 0 then
    return nil, "DHL has no events for this number yet"
  end
  return events
end

-- Demo timeline (same contract as every builtin adapter).
function M.demo(ctx, code)
  local stages = {
    { status = "pending",         description = "Shipment information received", location = "Leipzig, Germany" },
    { status = "in_transit",      description = "Processed at facility", location = "Leipzig, Germany" },
    { status = "in_transit",      description = "Departed facility", location = "Cincinnati, USA" },
    { status = "out_for_delivery", description = "With delivery courier", location = "São Paulo, Brazil" },
    { status = "delivered",        description = "Delivered - Signed for by", location = "São Paulo, Brazil" },
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

-- Test hooks for the Go suite (real captured samples).
M._collect_events = collect_events
M._map_status = map_status

return M
