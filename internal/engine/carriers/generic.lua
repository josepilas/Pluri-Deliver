-- Pluri-Deliver carrier adapter: Universal (auto-detect)
-- Matches any plausible tracking code and routes it through the configured
-- aggregator, which performs courier auto-detection on its side.
local M = {
  id = "generic",
  name = "Universal (auto)",
  country = "",
  kind = "builtin",
  requires_key = false,
  website = "",
  note = "Auto-detects the courier via the free 17TRACK web engine.",
}

function M.detect(code)
  local c = code:upper():gsub("[%s-]", "")
  return #c >= 8 and #c <= 30 and c:match("^[%u%d]+$") ~= nil
end

function M.demo(ctx, code)
  local stages = {
    { status = "pending",    description = "Shipment information received", location = "Origin facility" },
    { status = "in_transit", description = "In transit", location = "In network" },
    { status = "in_transit", description = "Arrived at destination country", location = "Customs" },
    { status = "out_for_delivery", description = "Out for delivery", location = "Local facility" },
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
