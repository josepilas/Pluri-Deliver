-- Pluri-Deliver carrier adapter: FedEx (aggregator-backed)
local M = {
  id = "fedex",
  name = "FedEx",
  country = "US",
  kind = "builtin",
  requires_key = false,
  website = "https://www.fedex.com/",
  note = "Tracked via the free 17TRACK web engine.",
}

function M.detect(code)
  local c = code:upper():gsub("[ %-]", "")
  if c:match("^%d+$") and (#c == 12 or #c == 15 or #c == 20) then return true end
  if c:match("^%d%d%d%d%d%d%d%d%d%d%d%d%d%d%d%d%d%d%d%d$") then return true end
  return false
end

function M.demo(ctx, code)
  local stages = {
    { status = "pending",    description = "Shipment information sent to FedEx", location = "Memphis, TN" },
    { status = "in_transit", description = "Picked up", location = "Memphis, TN" },
    { status = "in_transit", description = "In transit", location = "Indianapolis, IN" },
    { status = "in_transit", description = "At local FedEx facility", location = "São Paulo, Brazil" },
    { status = "out_for_delivery", description = "On FedEx vehicle for delivery", location = "São Paulo, Brazil" },
    { status = "delivered",  description = "Delivered", location = "São Paulo, Brazil" },
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
