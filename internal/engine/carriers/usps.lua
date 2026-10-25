-- Pluri-Deliver carrier adapter: USPS (aggregator-backed)
local M = {
  id = "usps",
  name = "USPS",
  country = "US",
  kind = "builtin",
  requires_key = false,
  website = "https://www.usps.com/",
  note = "Tracked via the free 17TRACK web engine.",
}

function M.detect(code)
  local c = code:upper():gsub("[%s-]", "")
  if c:match("^9[0-4]%d+$") and #c >= 20 then return true end
  if c:match("^%u%u%d%d%d%d%d%d%d%d%d%u%u$") then return true end
  return false
end

function M.demo(ctx, code)
  local stages = {
    { status = "pending",    description = "USPS in possession of item", location = "Los Angeles, CA" },
    { status = "in_transit", description = "Arrived at USPS facility", location = "Los Angeles, CA" },
    { status = "in_transit", description = "In transit to next facility", location = "Denver, CO" },
    { status = "in_transit", description = "Arrived at USPS Regional Facility", location = "New York, NY" },
    { status = "out_for_delivery", description = "Out for delivery", location = "Brooklyn, NY" },
    { status = "delivered",  description = "Delivered - In/At Mailbox", location = "Brooklyn, NY" },
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
