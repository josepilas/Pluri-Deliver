-- Pluri-Deliver carrier adapter: Correios / Sedex / PAC (Brazil)
-- ---------------------------------------------------------------------------
-- Fully automatic chain, in order:
--   1. Cainiao's global network (Go client) — carries the complete
--      China→Brazil history of every AliExpress-style import, localized in
--      Correios' own wording.
--   2. The free 17TRACK web engine — numbers Cainiao does not know.
--   3. The official portal's captcha flow — domestic Sedex/PAC numbers the
--      automatic chain cannot resolve: the app shows a "needs verification"
--      pill, the user types the captcha once and the full timeline loads.
--
-- Contract: return M. M.detect(code) -> bool; M.track is intentionally absent
-- (the engine routes to the Go client + universal web engine chain).
local M = {
  id = "correios",
  name = "Correios",
  country = "BR",
  kind = "builtin",
  requires_key = false,
  website = "https://www.correios.com.br/",
  note = "Automatic tracking: imports via Cainiao, everything else via the 17TRACK engine — with the official portal's captcha as the final fallback for domestic Sedex/PAC numbers.",
}

function M.detect(code)
  -- S10 format with the BR suffix: the family Correios auto-detects reliably.
  return code:upper():gsub("%s", ""):match("^%u%u%d%d%d%d%d%d%d%d%dBR$") ~= nil
end

-- Demo timeline with authentic Correios event wording (pt-BR).
function M.demo(ctx, code)
  local stages = {
    { status = "pending",         description = "Objeto postado", location = "Agência dos Correios · São Paulo/SP" },
    { status = "in_transit",      description = "Objeto encaminhado", location = "CTE São Paulo/SP" },
    { status = "in_transit",      description = "Objeto em trânsito - por favor aguarde", location = "CTE Belo Horizonte/MG" },
    { status = "in_transit",      description = "Objeto em trânsito - por favor aguarde", location = "CTE Curitiba/PR" },
    { status = "out_for_delivery", description = "Objeto saiu para entrega ao destinatário", location = "Curitiba/PR" },
    { status = "delivered",       description = "Objeto entregue ao destinatário", location = "Curitiba/PR" },
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
