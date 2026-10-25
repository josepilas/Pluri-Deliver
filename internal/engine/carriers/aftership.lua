-- Pluri-Deliver aggregator adapter: AfterShip (https://www.aftership.com)
-- Register a free API key at https://admin.aftership.com/profile/api-keys
local M = {
  id = "aftership",
  name = "AfterShip",
  country = "",
  kind = "aggregator",
  requires_key = true,
  website = "https://www.aftership.com/",
  note = "Covers 1000+ carriers with a free API key.",
}

function M.detect(_code)
  return false
end

-- Known carrier id -> AfterShip slug map. Unknown ids fall back to
-- AfterShip's courier auto-detection endpoint.
local SLUGS = {
  correios = "brazil-correios",
  jtexpress = "jt-express",
  dhl = "dhl",
  fedex = "fedex",
  ups = "ups",
  usps = "usps",
  amazon = "amazon-logistics",
}

local function tag_map(tag)
  tag = (tag or ""):lower()
  if tag == "delivered" then return "delivered" end
  if tag == "outfordelivery" then return "out_for_delivery" end
  if tag == "intransit" then return "in_transit" end
  if tag == "pending" or tag == "inforeceived" then return "pending" end
  if tag == "attemptfail" or tag == "exception" then return "exception" end
  return "info"
end

local function headers_for(key)
  return {
    ["aftership-api-key"] = key,
    ["Content-Type"] = "application/json",
  }
end

function M.track(ctx, code)
  local key = ctx.settings.aggregator.key
  if not key or key == "" then
    return nil, "AfterShip API key missing (Settings → Data sources)"
  end
  local H = headers_for(key)

  -- Resolve slug: hint -> known map -> detect endpoint.
  local slug = nil
  if ctx.hint then slug = SLUGS[ctx.hint] end
  if not slug then
    local r = ctx.http.post(
      "https://api.aftership.com/v4/couriers/detect",
      ctx.json.encode({ tracking_number = code }),
      H
    )
    if r.status == 200 then
      local ok, data = ctx.json.decode(r.body)
      if ok and data.data and data.data.couriers and data.data.couriers[1] then
        slug = data.data.couriers[1].slug
      end
    end
  end
  if not slug then
    return nil, "AfterShip could not detect the courier for this code"
  end

  -- Register tracking (409 = already tracked, fine).
  ctx.http.post(
    "https://api.aftership.com/v4/trackings",
    ctx.json.encode({ tracking = { tracking_number = code, slug = slug } }),
    H
  )

  local r = ctx.http.get("https://api.aftership.com/v4/trackings/" .. slug .. "/" .. code, H)
  if r.status ~= 200 then
    return nil, "AfterShip returned HTTP " .. r.status
  end
  local ok, data = ctx.json.decode(r.body)
  if not ok or not data.data or not data.data.tracking then
    return nil, "invalid AfterShip response"
  end
  local cps = data.data.tracking.checkpoints or {}
  if #cps == 0 then
    return nil, "no events found for this code"
  end

  local out = {}
  for _, cp in ipairs(cps) do
    local loc = table.concat({ cp.city, cp.state, cp.country_name }, ", ")
    loc = loc:gsub("^, ", ""):gsub(", $", ""):gsub("^(.-),$", "%1")
    out[#out+1] = {
      date = cp.checkpoint_time or cp.created_at or "",
      status = tag_map(cp.tag),
      description = cp.message or cp.tag or "Checkpoint",
      location = cp.location or loc,
    }
  end
  return out
end

function M.demo(ctx, code)
  local stages = {
    { status = "pending",    description = "Shipment information received", location = "Origin" },
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
