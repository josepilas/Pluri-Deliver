-- Pluri-Deliver carrier adapter: SPX Express (Brazil)
-- ---------------------------------------------------------------------------
-- SPX Express is Shopee's own logistics arm. Its public tracking portal
-- (spx.com.br/track?NUMBER) is fully keyless: no login, no captcha. The app
-- ships a dedicated Go client (internal/spx) that speaks to the portal
-- exactly like the SPA does — the open order-info endpoint first, then the
-- signed fleet_order quick-track. This adapter provides detection + demo
-- data; the service routes real tracking to the Go client.
-- Contract: return M. M.detect(code) -> bool; M.track is intentionally absent.
local M = {
  id = "spx",
  name = "SPX Express",
  country = "BR",
  kind = "builtin",
  requires_key = false,
  website = "https://spx.com.br/",
  note = "Shopee's logistics arm — tracks directly on spx.com.br, no verification needed.",
}

function M.detect(code)
  local c = code:upper():gsub("%s", "")
  -- SPXBR + 8..15 digits: the family SPX Brasil itself accepts.
  return c:match("^SPXBR%d%d%d%d%d%d%d%d%d?%d?%d?%d?%d?%d?$") ~= nil
end

-- Demo timeline with authentic SPX wording (pt-BR).
function M.demo(ctx, code)
  local stages = {
    { status = "pending",          description = "Pedido recebido", location = "Hub SPX · Cajamar/SP" },
    { status = "in_transit",       description = "Pacote coletado", location = "Cajamar/SP" },
    { status = "in_transit",       description = "Em trânsito para o centro de distribuição", location = "CD SPX · Extrema/MG" },
    { status = "in_transit",       description = "Chegou ao centro de distribuição", location = "CD SPX · São Paulo/SP" },
    { status = "out_for_delivery", description = "Saiu para entrega", location = "São Paulo/SP" },
    { status = "delivered",        description = "Encomenda entregue", location = "São Paulo/SP" },
  }
  return demo_timeline(ctx, code, stages)
end

-- Shared demo engine (same contract as the other builtin adapters).
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
