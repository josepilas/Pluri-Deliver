-- Pluri-Deliver carrier adapter: Demo Courier
-- Any code starting with "DEMO" is always tracked by this carrier, regardless
-- of demo mode. Events appear over time (one every 2 minutes) so you can watch
-- notifications, badges and the timeline update live.
local M = {
  id = "demo",
  name = "Demo Courier",
  country = "",
  kind = "demo",
  requires_key = false,
  website = "",
  note = "Codes starting with DEMO always simulate a full delivery journey.",
}

function M.detect(code)
  return code:upper():match("^DEMO") ~= nil
end

function M.track(ctx, code)
  return M.demo(ctx, code)
end

function M.demo(ctx, code)
  local stages = {
    { status = "pending",         description = "Shipment information received", location = "Pluri-Deliver Hub" },
    { status = "in_transit",      description = "Picked up by courier", location = "Pluri-Deliver Hub" },
    { status = "in_transit",      description = "In transit - moving to next facility", location = "Sorting Center" },
    { status = "out_for_delivery", description = "Out for delivery", location = "Your neighborhood" },
    { status = "delivered",       description = "Delivered - enjoy!", location = "Your door" },
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
