-- Pluri-Deliver carrier adapter: Amazon Shipping / Amazon Logistics
-- ---------------------------------------------------------------------------
-- Keyless direct integration with Amazon's PUBLIC package tracker — the
-- same service that powers https://track.amazon.com/tracking/<TBA> (which
-- anyone can open without an Amazon account). Verified live:
--   GET https://track.amazon.com/api/tracker/{code}
--   -> {"progressTracker":"<stringified JSON: summary, expectedDeliveryDate,
--        errors[].errorCode=TRACKING_ID_NOT_FOUND...>",
--       "eventHistory":[{"time":"...","events":[{"label":"...",
--         "eventTimeInMs":1718971234567,"city":"...","state":"...",
--         "country":"...","eventCode":"..."}]}] | null}
-- TBA (US/BR), TBM and TBC codes are all accepted. When Amazon does not
-- know the number the engine falls back to the 17TRACK web engine chain.
-- Tip: parcels shipped by Amazon may also carry an underlying carrier code
-- (Correios, J&T...) on the order page — track that one for richer events.
local M = {
  id = "amazon",
  name = "Amazon Shipping",
  country = "US",
  kind = "builtin",
  requires_key = false,
  website = "https://track.amazon.com/",
  note = "Direct via Amazon's public tracker (track.amazon.com) — no account needed.",
}

function M.detect(code)
  local c = code:upper():gsub("%s", "")
  return c:match("^TB[AMBC]%d+$") ~= nil
end

local API = "https://track.amazon.com/api/tracker/"

local HEADERS = {
  ["Accept"] = "application/json, text/plain, */*",
  ["Accept-Language"] = "en-US,en;q=0.9",
  ["Referer"] = "https://track.amazon.com/",
  ["User-Agent"] = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
}

-- Map Amazon eventCode enums to Pluri-Deliver statuses (nil = let Go derive
-- from the English label text, which works well for Amazon's wording).
local function map_code(code)
  local c = string.lower(tostring(code or ""))
  if c == "delivered" then return "delivered" end
  if c == "out_for_delivery" or c == "outfordelivery" then return "out_for_delivery" end
  if c == "in_transit" or c == "transit" or c == "departed" or c == "arrived"
     or c == "arrived_at_facility" or c == "departed_facility" then return "in_transit" end
  if c == "picked_up" or c == "pickup" or c == "pending" or c == "label_created"
     or c == "info_received" then return "pending" end
  if c == "delivery_attempted" or c == "exception" or c == "returned" then return "exception" end
  return nil
end

-- collect_events turns a decoded /api/tracker payload into the normalized
-- event list. decode is an optional json.decode function used to unpack the
-- STRINGIFIED progressTracker member (track() passes ctx.json.decode).
-- Exposed as M._collect_events so Go tests can feed it real captured samples.
local function collect_events(data, decode)
  local out = {}
  if type(data) ~= "table" then return out end

  -- progressTracker arrives stringified: unpack it once, then inspect errors.
  local pt = nil
  if type(data.progressTracker) == "table" then
    pt = data.progressTracker
  elseif type(data.progressTracker) == "string" and data.progressTracker ~= "" and decode then
    local pok, pv = decode(data.progressTracker)
    if pok and type(pv) == "table" then
      pt = pv
    end
  end
  if pt and type(pt.errors) == "table" and #pt.errors > 0 then
    local msgs = {}
    for _, e in ipairs(pt.errors) do
      if type(e) == "table" and e.errorCode then
        msgs[#msgs+1] = tostring(e.errorCode)
      end
    end
    if #msgs > 0 then
      return out, table.concat(msgs, ", ")
    end
  end

  local history = data.eventHistory
  if type(history) ~= "table" then return out end

  for _, day in ipairs(history) do
    if type(day) == "table" and type(day.events) == "table" then
      for _, ev in ipairs(day.events) do
        if type(ev) == "table" and ev.label then
          local loc = {}
          if ev.city and ev.city ~= "" then loc[#loc+1] = tostring(ev.city) end
          if ev.state and ev.state ~= "" and ev.state ~= ev.city then loc[#loc+1] = tostring(ev.state) end
          if ev.country and ev.country ~= "" then loc[#loc+1] = tostring(ev.country) end
          local when
          if type(ev.eventTimeInMs) == "number" then
            when = math.floor(ev.eventTimeInMs / 1000)
          elseif type(day.time) == "string" then
            when = day.time
          end
          out[#out+1] = {
            date = when,
            status = map_code(ev.eventCode),
            description = tostring(ev.label),
            location = table.concat(loc, ", "),
            source = "Amazon",
          }
        end
      end
    end
  end
  -- Oldest first (Amazon lists newest first inside each day group).
  table.sort(out, function(a, b) return tostring(a.date) < tostring(b.date) end)
  return out
end

function M.track(ctx, code)
  code = tostring(code or ""):upper():gsub("%s", "")
  if code == "" then
    return nil, "empty tracking code"
  end

  local r = ctx.http.get(API .. code, HEADERS)
  if r.status ~= 200 then
    return nil, "Amazon tracker returned HTTP " .. tostring(r.status)
  end
  local ok, data = ctx.json.decode(r.body)
  if not ok or type(data) ~= "table" then
    return nil, "invalid Amazon tracker response"
  end

  local events, err = collect_events(data, ctx.json.decode)
  if err then
    return nil, "Amazon does not know this tracking ID (" .. err .. ")"
  end
  if #events == 0 then
    return nil, "Amazon tracker has no events for this number yet"
  end
  return events
end

-- Demo timeline (same contract as every builtin adapter).
function M.demo(ctx, code)
  local stages = {
    { status = "pending",    description = "Shipment created", location = "Amazon Fulfillment Center" },
    { status = "in_transit", description = "Package has left the carrier facility", location = "Newark, NJ" },
    { status = "in_transit", description = "Package arrived at carrier facility", location = "Hebron, KY" },
    { status = "out_for_delivery", description = "Out for delivery", location = "Local delivery station" },
    { status = "delivered",  description = "Delivered, handed directly to a resident", location = "Front door" },
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
M._map_code = map_code

return M
