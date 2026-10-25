-- Pluri-Deliver carrier adapter: UPS (keyless, cookie handshake)
-- ---------------------------------------------------------------------------
-- Direct integration with the same API the ups.com tracking page uses.
-- Verified protocol (public knowledge, used by open-source trackers):
--   1. GET  https://www.ups.com/track?tracknum=<code>&requester=ST/trackdetails
--      -> Set-Cookie: X-XSRF-TOKEN-ST=<token>; ... (session cookies)
--   2. POST https://www.ups.com/track/api/Track/GetStatus?loc=en_GB
--      Headers: Content-Type: application/json, X-XSRF-TOKEN: <token>,
--               Cookie: <all cookies from step 1>
--      Body: {"Locale":"en_GB","TrackingNumber":["<code>"],
--             "Requester":"","returnToValue":""}
--   -> {"statusCode":200,"trackDetails":[{"deliveredDate":"DD/MM/YYYY",
--        "shipmentProgressActivities":[{"date":"DD/MM/YYYY","time":"HH:MM",
--         "location":"...","activityScan":"...","milestone":{...}}]}]}
-- The en_GB locale gives 24h times and DD/MM/YYYY dates, which the engine's
-- date parser understands natively. ups.com refuses datacenter IPs; from
-- residential connections it answers like the website. On failure the
-- engine falls back to the 17TRACK web engine chain.
local M = {
  id = "ups",
  name = "UPS",
  country = "US",
  kind = "builtin",
  requires_key = false,
  website = "https://www.ups.com/",
  note = "Direct via ups.com's tracking API (session handshake); falls back to the 17TRACK web engine.",
}

function M.detect(code)
  local c = code:upper():gsub("[ %-]", "")
  if c:match("^1Z[%u%d]+$") then
    return #c == 18 or #c == 12 -- 1Z + 16 (ground/express) or 1Z + 10 (SurePost)
  end
  -- UPS Mail Innovations / freight fallbacks stay on the generic chain.
  return false
end

local UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"
local PAGE_URL = "https://www.ups.com/track?tracknum="
local STATUS_URL = "https://www.ups.com/track/api/Track/GetStatus?loc=en_GB"

-- map_scan classifies UPS activity descriptions; nil = let Go derive from
-- the text (UPS wording is covered by the Go keyword table).
local function map_scan(text)
  local s = string.lower(tostring(text or ""))
  if s:match("delivered") then return "delivered" end
  if s:match("out for delivery") then return "out_for_delivery" end
  if s:match("returned to sender") or s:match("exception")
     or s:match("adverse weather") or s:match("emergency") then return "exception" end
  if s:match("origin scan") or s:match("order processed")
     or s:match("ready for ups") or s:match("label created") then return "pending" end
  return nil
end

-- collect_events turns a decoded trackDetails entry into the normalized
-- event list (exposed as M._collect_events for the Go test suite).
local function collect_events(details)
  local out = {}
  if type(details) ~= "table" then return out end
  local acts = type(details.shipmentProgressActivities) == "table"
    and details.shipmentProgressActivities or {}
  for _, a in ipairs(acts) do
    if type(a) == "table" and (a.activityScan or a.milestone) then
      local desc = tostring(a.activityScan or "")
      if desc ~= "" then
        local when = ""
        if a.date and a.time then
          when = tostring(a.date) .. " " .. tostring(a.time) -- DD/MM/YYYY HH:MM
        elseif a.date then
          when = tostring(a.date)
        end
        out[#out+1] = {
          date = when,
          status = map_scan(desc),
          description = desc,
          location = tostring(a.location or ""),
          source = "UPS",
        }
      end
    end
  end
  -- UPS lists newest first; our timeline is chronological.
  table.sort(out, function(a, b) return tostring(a.date) < tostring(b.date) end)
  return out
end

function M.track(ctx, code)
  code = tostring(code or ""):gsub("[ %-]", "")
  if code == "" then
    return nil, "empty tracking code"
  end

  -- Step 1: load the tracking page to collect the session cookies.
  local page = ctx.http.get(PAGE_URL .. code .. "&requester=ST/trackdetails", {
    ["Accept"] = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
    ["Accept-Language"] = "en-GB,en;q=0.9",
    ["Referer"] = "https://www.ups.com/track",
    ["User-Agent"] = UA,
  })
  if page.status ~= 200 then
    return nil, "UPS page returned HTTP " .. tostring(page.status)
  end
  local cookies = page.cookies()
  local token = cookies["X-XSRF-TOKEN-ST"]
  if not token or token == "" then
    return nil, "UPS did not issue a session token for this connection"
  end
  local pairs = {}
  for name, value in pairs(cookies) do
    pairs[#pairs+1] = name .. "=" .. value
  end

  -- Step 2: query the status API with the cookie + XSRF handshake.
  local body = '{"Locale":"en_GB","TrackingNumber":["' .. code .. '"],"Requester":"","returnToValue":""}'
  local r = ctx.http.post(STATUS_URL, body, {
    ["Content-Type"] = "application/json",
    ["Accept"] = "application/json, text/plain, */*",
    ["Accept-Language"] = "en-GB,en;q=0.9",
    ["Cookie"] = table.concat(pairs, "; "),
    ["Referer"] = PAGE_URL .. code .. "&requester=ST/trackdetails",
    ["User-Agent"] = UA,
    ["X-XSRF-TOKEN"] = tostring(token),
  })
  if r.status ~= 200 then
    return nil, "UPS API returned HTTP " .. tostring(r.status)
  end
  local ok, data = ctx.json.decode(r.body)
  if not ok or type(data) ~= "table" then
    return nil, "invalid UPS API response"
  end
  if data.statusCode and data.statusCode ~= 200 then
    return nil, "UPS API status " .. tostring(data.statusCode) .. " (" .. tostring(data.statusText or "?") .. ")"
  end
  if type(data.trackDetails) ~= "table" or #data.trackDetails == 0 then
    return nil, "UPS returned no track details"
  end

  local events = collect_events(data.trackDetails[1])
  if #events == 0 then
    return nil, "UPS has no events for this number yet"
  end
  return events
end

-- Demo timeline (same contract as every builtin adapter).
function M.demo(ctx, code)
  local stages = {
    { status = "pending",    description = "Order processed - ready for UPS", location = "Louisville, KY" },
    { status = "in_transit", description = "Origin scan", location = "Louisville, KY" },
    { status = "in_transit", description = "Departed from facility", location = "Chicago, IL" },
    { status = "out_for_delivery", description = "Out for delivery", location = "Curitiba, Brazil" },
    { status = "delivered",  description = "Delivered - Left at front door", location = "Curitiba, Brazil" },
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
M._map_scan = map_scan

return M
