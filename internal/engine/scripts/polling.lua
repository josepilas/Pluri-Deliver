-- polling.lua — Pluri-Deliver's polling brain.
--
-- decide(info) tells the app how many minutes to wait before refreshing one
-- tracking again. The app asks this question for every parcel on every tick
-- (~30s), so the strategy is fully dynamic: it can react to status changes,
-- errors, focus, staleness...
--
-- COPY THIS FILE to %APPDATA%\Pluri-Deliver\scripts\polling.lua to
-- customize it — your copy replaces this built-in one on the next start.
--
-- info fields:
--   code, carrier, name   identification of the tracking
--   status                "delivered" | "out_for_delivery" | "in_transit"
--                         | "pending" | "exception" | "info"
--   delivered (bool)      the parcel reached its destination
--   auto_update (bool)    background updates enabled for this tracking
--   active (bool)         the main window is visible right now
--   event_count (number)  events already known
--   last_event_age_hours  hours since the newest event (-1 when none yet)
--   has_error (bool)      the last refresh failed
--   poll_minutes          the user's active-interval setting
--   idle_poll_minutes     the user's hidden/minimized-interval setting
--
-- Return: minutes until the next refresh (clamped to 1..1440 by the app).

local function decide(i)
  -- Base cadence: the user's active interval, or the longer idle interval
  -- while the window is hidden/minimized.
  local base = i.poll_minutes
  if not i.active and i.idle_poll_minutes > base then
    base = i.idle_poll_minutes
  end

  -- A carrier misbehaving: back off, but keep an eye on it.
  if i.has_error then
    return math.min(base * 2, 360)
  end

  -- Settled parcels only need to catch returns / post-delivery scans.
  if i.delivered then
    return math.max(base, 720)
  end

  -- The final mile: events move fast, check often (at most every 15 min).
  if i.status == "out_for_delivery" then
    return math.min(base, 15)
  end

  -- Nothing new for over two weeks: relax.
  if i.last_event_age_hours > 336 then
    return math.max(base, 240)
  end

  return base
end

return { decide = decide }
