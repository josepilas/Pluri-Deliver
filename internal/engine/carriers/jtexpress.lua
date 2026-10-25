-- Pluri-Deliver carrier adapter: J&T Express (Brazil)
-- ---------------------------------------------------------------------------
-- J&T Brasil's official tracking API (official.jtjms-br.com, the backend of
-- jtexpress.com.br) requires the RECIPIENT'S CPF on every query — their own
-- site asks for it too. Pluri-Deliver ships a dedicated Go client
-- (internal/jt) that signs requests exactly like the SPA and asks the user
-- for the CPF once per tracking. This adapter provides detection + demo
-- data; the service routes real tracking to the Go client via the CPF flow.
-- Contract: return M. M.detect(code) -> bool; M.track is intentionally absent.
local M = {
  id = "jtexpress",
  name = "J&T Express",
  country = "BR",
  kind = "builtin",
  requires_key = false,
  requires_cpf = true,
  website = "https://www.jtexpress.com.br/",
  note = "Tracks directly on J&T's official API — asks for the recipient's CPF once (same as J&T's own site).",
}

function M.detect(code)
  local c = code:upper():gsub("%s", "")
  -- JT + 9..13 digits (Shopee marketplace waybills) or pure 12-15 digit keys.
  if c:match("^JT%d%d%d%d%d%d%d%d%d%d?%d?%d?%d?$") then return true end
  if c:match("^%d+$") and #c >= 12 and #c <= 15 then return true end
  return false
end

-- Demo timeline with authentic J&T wording (pt-BR).
function M.demo(ctx, code)
  local stages = {
    { status = "pending",          description = "Pedido criado", location = "Hub J&T · Cajamar/SP" },
    { status = "in_transit",       description = "Pacote coletado", location = "Cajamar/SP" },
    { status = "in_transit",       description = "Em trânsito para o centro de distribuição", location = "CTE · Guarulhos/SP" },
    { status = "out_for_delivery", description = "Saiu para entrega", location = "CD · São Paulo/SP" },
    { status = "delivered",        description = "Entrega efetuada", location = "São Paulo/SP" },
  }
  return demo_timeline(ctx, code, stages)
end

-- Shared demo engine: reveals one new stage every 2 minutes since the
-- tracking was added, deterministically jittered by the code hash.
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
