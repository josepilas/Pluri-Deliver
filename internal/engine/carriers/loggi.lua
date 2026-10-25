-- Pluri-Deliver carrier adapter: Loggi (Brazil)
-- ---------------------------------------------------------------------------
-- Keyless direct integration with the GraphQL API Loggi's public tracker
-- (https://www.loggi.com/rastreador/) calls — extracted from their tracker
-- bundle (tracker-web-ui). One query, no login, no captcha:
--   POST https://www.loggi.com/graphql
--   {"query":"query statusQuery($trackingKey: String!...{
--      packageTracker(trackingKey: $trackingKey ...) {
--        eta, currentStatus {error inProgress destination type},
--        shipperStatus {code label description}, destinationInfo {firstLine},
--        packageInfo {packageId trackingKey companyName}}}",
--    "variables":{"trackingKey":"<code>"}}
-- The public API exposes the CURRENT status (+ ETA + description) rather than
-- a full scan history, so the adapter emits one event per observed status:
-- every status change lands as a new event on the timeline, exactly what the
-- official rastreador page shows. Loggi's CloudFront edge refuses datacenter
-- IPs; residential connections answer normally. On failure the engine falls
-- back to the 17TRACK web engine chain.
local M = {
  id = "loggi",
  name = "Loggi",
  country = "BR",
  kind = "builtin",
  requires_key = false,
  website = "https://www.loggi.com/",
  note = "Direct via Loggi's public rastreador API — one event per status change.",
}

function M.detect(code)
  local c = code:upper():gsub("%s", "")
  -- Loggi tracking keys: LOG + alphanumerics (e.g. LOG123456789), plus
  -- the LGP/LGC families seen on their widgets and labels.
  return (c:match("^LOG[%u%d]+$") or c:match("^LG[%u%d]+$")) ~= nil
    and #c >= 8 and #c <= 16
end

local GRAPHQL = "https://www.loggi.com/graphql"

local HEADERS = {
  ["Content-Type"] = "application/json",
  ["Accept"] = "application/json, text/plain, */*",
  ["Accept-Language"] = "pt-BR,pt;q=0.9,en;q=0.8",
  ["Origin"] = "https://www.loggi.com",
  ["Referer"] = "https://www.loggi.com/rastreador/",
  ["User-Agent"] = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
}

-- The exact statusQuery Loggi's tracker bundle ships (tracker-web-ui).
local QUERY = [[query statusQuery(
  $trackingKey: String!
  $postalCode: String
  $companySecretId: String
) {
  packageTracker(trackingKey: $trackingKey, postalCode: $postalCode, companySecretId: $companySecretId) {
    eta
    packageInfo {
      packageId
      trackingKey
      companyName
    }
    redispatchInfo {
      trackingCode
      maxPickupTime
    }
    currentStatus {
      error
      inProgress
      destination
      type
    }
    shipperStatus {
      code
      label
      description
      actionRequired {
        reasonLabel
        reasonDescription
      }
    }
    destinationInfo {
      firstLine
    }
    canAuthenticate
  }
}]]

-- map_status classifies Loggi shipperStatus labels (pt-BR or en-US);
-- nil = let Go derive from the text.
local function map_status(text)
  local s = string.lower(tostring(text or ""))
  if s:match("entregue") or s:match("delivered") then return "delivered" end
  if s:match("saiu para entrega") or s:match("out for delivery")
     or s:match("rota de entrega") then return "out_for_delivery" end
  if s:match("em trânsito") or s:match("em transito") or s:match("in transit")
     or s:match("roterizad") or s:match("a caminho") then return "in_transit" end
  if s:match("coletad") or s:match("pickup") or s:match("picked up")
     or s:match("criad") or s:match("created") or s:match("postad") then return "pending" end
  if s:match("endereço incorreto") or s:match("destinatário ausente")
     or s:match("recusad") or s:match("falha") or s:match("problema")
     or s:match("exception") or s:match("atras") then return "exception" end
  return nil
end

-- collect_events turns a decoded packageTracker object into the event list
-- (exposed as M._collect_events for the Go test suite). now is the fetch
-- epoch so tests stay deterministic.
local function collect_events(pt, now)
  local out = {}
  if type(pt) ~= "table" then return out, "no packageTracker in response" end

  -- GraphQL error envelope.
  if pt.errors and type(pt.errors) == "table" and #pt.errors > 0 then
    return out, "GraphQL error"
  end

  local shipper = type(pt.shipperStatus) == "table" and pt.shipperStatus or nil
  local label = shipper and tostring(shipper.label or "") or ""
  local desc = shipper and tostring(shipper.description or "") or ""
  local statusText = label ~= "" and label or desc
  if statusText == "" then
    return out, "Loggi returned no status for this code"
  end

  local location = ""
  if type(pt.destinationInfo) == "table" and pt.destinationInfo.firstLine then
    location = tostring(pt.destinationInfo.firstLine)
  end
  local full = label
  if desc ~= "" and desc ~= label then
    full = full ~= "" and (full .. " — " .. desc) or desc
  end

  out[#out+1] = {
    date = now,
    status = map_status(statusText),
    description = full ~= "" and full or "Loggi status update",
    location = location,
    source = "Loggi",
  }
  return out
end

function M.track(ctx, code)
  code = tostring(code or ""):gsub("%s", "")
  if code == "" then
    return nil, "empty tracking code"
  end

  local payload = '{"query":' .. ctx.json.encode(QUERY)
    .. ',"variables":{"trackingKey":"' .. code .. '"}}'
  local r = ctx.http.post(GRAPHQL, payload, HEADERS)
  if r.status ~= 200 then
    return nil, "Loggi returned HTTP " .. tostring(r.status) .. " for this connection"
  end
  local ok, data = ctx.json.decode(r.body)
  if not ok or type(data) ~= "table" then
    return nil, "invalid Loggi response"
  end
  local pt = data.packageTracker
  if pt == nil and type(data.errors) == "table" and #data.errors > 0 then
    -- Unknown codes surface as GraphQL errors ("package not found").
    return nil, "Loggi does not know this tracking code"
  end

  local events, err = collect_events(pt, ctx.now())
  if err then
    return nil, "Loggi: " .. err
  end
  if #events == 0 then
    return nil, "Loggi has no status for this number yet"
  end
  return events
end

-- Demo timeline with authentic Loggi wording (pt-BR).
function M.demo(ctx, code)
  local stages = {
    { status = "pending",         description = "Pedido recebido", location = "Hub Loggi · São Paulo/SP" },
    { status = "in_transit",       description = "Pedido coletado", location = "São Paulo/SP" },
    { status = "in_transit",       description = "Em trânsito para o centro de distribuição", location = "CD Loggi · Barueri/SP" },
    { status = "out_for_delivery", description = "Saiu para entrega", location = "São Paulo/SP" },
    { status = "delivered",        description = "Entrega realizada", location = "São Paulo/SP" },
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
M._map_status = map_status

return M
