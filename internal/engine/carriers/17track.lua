-- Pluri-Deliver aggregator adapter: 17TRACK (https://www.17track.net)
-- Register a free API key at https://www.17track.net/zh-cn/apicenter (v2.2 API).
-- The parser below is deliberately tolerant: it hunts the response tree for
-- the event arrays 17track returns, so minor shape changes survive.
local M = {
  id = "17track",
  name = "17TRACK",
  country = "",
  kind = "aggregator",
  requires_key = true,
  website = "https://www.17track.net/",
  note = "Optional official API (paid) - the built-in 17TRACK web engine needs no key.",
}

function M.detect(_code)
  return false -- not auto-detected; selected explicitly or via fallback
end

local function is_array(v)
  if type(v) ~= "table" then return false end
  local n = 0
  for k, _ in pairs(v) do
    if type(k) ~= "number" then return false end
    n = n + 1
  end
  return n > 0
end

local function looks_like_event(ev)
  return type(ev) == "table" and ev.a1 ~= nil and type(ev.a1) == "number"
end

local function map_status(ev)
  local d = string.lower((ev.d1 or "") .. " " .. (ev.d2 or "") .. " " .. (ev.z1 or "") .. " " .. (ev.e1 or ""))
  if d:match("deliver") or d:match("signed") or d:match("entreg") then return "delivered" end
  if d:match("out for deliver") or d:match("saiu para entrega") then return "out_for_delivery" end
  if d:match("transit") or d:match("sorting") or d:match("arriv") or d:match("depart") or d:match("tracking") then return "in_transit" end
  if d:match("received") or d:match("created") or d:match("pending") then return "pending" end
  if d:match("fail") or d:match("return") or d:match("alert") or d:match("exception") then return "exception" end
  return "info"
end

-- Depth-first search for arrays of event objects.
local function find_events(node, out)
  if type(node) ~= "table" then return out end
  if is_array(node) then
    local eventish = 0
    for _, v in ipairs(node) do
      if looks_like_event(v) then eventish = eventish + 1 end
    end
    if eventish > 0 and eventish >= #node - 1 then
      for _, ev in ipairs(node) do
        if looks_like_event(ev) then
          out[#out+1] = {
            date = ev.a1,
            status = map_status(ev),
            description = ev.d1 or ev.d2 or ev.d3 or "Event",
            location = ev.l1 or ev.l2 or ev.c3 or "",
          }
        end
      end
      return out
    end
  end
  for _, v in pairs(node) do
    find_events(v, out)
    if #out > 0 then return out end
  end
  return out
end

function M.track(ctx, code)
  local key = ctx.settings.aggregator.key
  if not key or key == "" then
    return nil, "17TRACK API key missing (Settings → Data sources)"
  end
  local headers = {
    ["17token"] = key,
    ["Content-Type"] = "application/json",
  }

  -- Register the number (ignore "already registered" style errors).
  local reg = ctx.http.post(
    "https://api.17track.net/track/v2.2/register",
    ctx.json.encode({ { number = code, auto_detection = true } }),
    headers
  )
  _ = reg

  local r = ctx.http.post(
    "https://api.17track.net/track/v2.2/get",
    ctx.json.encode({ data = { { number = code, auto_detection = true } } }),
    headers
  )
  if r.status ~= 200 then
    return nil, "17TRACK returned HTTP " .. r.status .. " - check your API key"
  end
  local ok, data = ctx.json.decode(r.body)
  if not ok then
    return nil, "invalid 17TRACK response"
  end
  local out = find_events(data, {})
  if #out == 0 then
    return nil, "no events found for this code"
  end
  return out
end

function M.demo(ctx, code)
  local stages = {
    { status = "pending",    description = "Shipment info received", location = "Origin" },
    { status = "in_transit", description = "In transit", location = "Intermediate hub" },
    { status = "out_for_delivery", description = "Out for delivery", location = "Destination city" },
    { status = "delivered",  description = "Delivered", location = "Destination" },
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

return M
