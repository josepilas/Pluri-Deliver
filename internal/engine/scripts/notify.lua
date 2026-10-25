-- notify.lua — Pluri-Deliver's notification brain.
--
-- build(info) returns the notifications to send for a batch of genuinely
-- new events of one tracking: a list of { kind, title, body } tables (kind
-- drives the in-app toast style: "update" | "delivered" | "info" | ...).
--
-- COPY THIS FILE to %APPDATA%\Pluri-Deliver\scripts\notify.lua to
-- customize it — your copy replaces this built-in one on the next start.
-- Return an empty list to silence notifications entirely, add rules for
-- your favorite carriers, write your own wording in any language...
--
-- info fields:
--   code, carrier, language, status, delivered (bool)
--   name                  display name of the tracking ("" when unnamed)
--   events                the NEW events, oldest first; each is a table
--                         { status, description, location, date, source }
--   t.plus_more(body, n)  i18n helper: appends "(+n more events)" in the
--                         user's language
--   t.delivered_body      localized "Your package has been delivered!"

local function build(i)
  if #i.events == 0 then
    return {}
  end

  local title = i.name
  if title == nil or title == "" then
    title = i.code
  end

  -- Lead with the newest event; older ones are summarized by count.
  local latest = i.events[#i.events]
  local body = latest.description or ""
  if latest.location and latest.location ~= "" then
    body = body .. " · " .. latest.location
  end
  if #i.events > 1 then
    body = i.t.plus_more(body, #i.events - 1)
  end

  local notes = {
    { kind = "update", title = title, body = body },
  }

  -- Delivered parcels get their own celebratory toast.
  if i.delivered then
    notes[#notes + 1] = {
      kind = "delivered",
      title = title,
      body = i.t.delivered_body,
    }
  end

  return notes
end

return { build = build }
