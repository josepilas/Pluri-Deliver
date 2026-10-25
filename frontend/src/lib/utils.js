// lib/utils.js — formatting helpers shared by views.

export function relTime(t, i18n) {
  if (!t) return ''
  const date = typeof t === 'string' ? new Date(t) : t
  const diff = Date.now() - date.getTime()
  if (Number.isNaN(diff)) return ''
  const m = Math.floor(diff / 60_000)
  if (m < 1) return i18n('rel.now')
  if (m < 60) return i18n('rel.m', { n: m })
  const h = Math.floor(m / 60)
  if (h < 24) return i18n('rel.h', { n: h })
  return i18n('rel.d', { n: Math.floor(h / 24) })
}

const pad = (n) => String(n).padStart(2, '0')

export function fmtDateTime(t) {
  if (!t) return ''
  const d = typeof t === 'string' ? new Date(t) : t
  if (Number.isNaN(d.getTime())) return ''
  return `${d.getDate()} ${MONTHS_SHORT[d.getMonth()]} ${d.getFullYear()} · ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function fmtTime(t) {
  if (!t) return ''
  const d = typeof t === 'string' ? new Date(t) : t
  if (Number.isNaN(d.getTime())) return ''
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}

const MONTHS_SHORT = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

export function latestEvent(t) {
  if (!t || !t.events || t.events.length === 0) return null
  return t.events[t.events.length - 1]
}

export function unseenCount(t) {
  if (!t || !t.lastSeen || !t.events) return 0
  const seen = new Date(t.lastSeen).getTime()
  return t.events.filter((e) => new Date(e.date).getTime() > seen).length
}

export function trackingStatus(t) {
  const ev = latestEvent(t)
  return ev ? ev.status : 'pending'
}

const STATUS_ORDER = ['pending', 'info', 'in_transit', 'out_for_delivery', 'delivered']

export function progressOf(t) {
  const s = trackingStatus(t)
  const idx = STATUS_ORDER.indexOf(s)
  if (s === 'exception') return { index: 2, exception: true }
  return { index: idx < 0 ? 0 : idx, exception: false }
}

export function sortTrackings(list) {
  const rank = (t) => (trackingStatus(t) === 'delivered' ? 1 : 0)
  return [...list].sort((a, b) => {
    const ra = rank(a), rb = rank(b)
    if (ra !== rb) return ra - rb
    const da = a.createdAt || '', db = b.createdAt || ''
    return db.localeCompare(da)
  })
}
