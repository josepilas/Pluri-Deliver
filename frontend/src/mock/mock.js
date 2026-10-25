// mock/mock.js — browser-mode implementation of the backend API + runtime.
// Provides realistic sample data so the UI works without the Wails desktop
// shell (vite dev / screenshots).

const bus = { listeners: {} }
export const rt = {
  EventsOn: (name, cb) => {
    bus.listeners[name] = bus.listeners[name] || []
    bus.listeners[name].push(cb)
  },
  EventsOff: (name) => { delete bus.listeners[name] },
  EventsEmit: (name, data) => {
    (bus.listeners[name] || []).forEach((cb) => cb(data))
  },
  WindowMinimise: () => console.log('[mock] WindowMinimise'),
  WindowToggleMaximise: () => console.log('[mock] WindowToggleMaximise'),
  WindowClose: () => console.log('[mock] WindowClose'),
  WindowHide: () => console.log('[mock] WindowHide'),
  BrowserOpenURL: (u) => window.open(u, '_blank'),
}

const emit = rt.EventsEmit
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

const h = (n) => n * 3600_000
const m = (n) => n * 60_000
const iso = (msAgo) => new Date(Date.now() - msAgo).toISOString()

let settings = {
  language: 'en',
  theme: 'dark',
  updateMode: 'smart',
  pollMinutes: 15,
  idlePollMinutes: 60,
  notifications: true,
  startWithWindows: false,
  runInBackground: false,
  hideTrayIcon: false,
  demoMode: false,
  aggregator: { provider: 'none', key: '' },
}

const carriers = [
  { id: 'correios', name: 'Correios', country: 'BR', kind: 'builtin', requiresKey: false, website: 'https://www.correios.com.br/', note: 'Automatic tracking: imports via Cainiao, everything else via the free 17TRACK engine — with the official portal\'s captcha as the final fallback for domestic Sedex/PAC numbers.' },
  { id: 'jtexpress', name: 'J&T Express', country: 'BR', kind: 'builtin', requiresKey: false, requiresCpf: true, website: 'https://www.jtexpress.com.br/', note: "Tracks directly on J&T's official API — asks for the recipient's CPF once (same as J&T's own site)." },
  { id: 'spx', name: 'SPX Express', country: 'BR', kind: 'builtin', requiresKey: false, website: 'https://spx.com.br/', note: "Shopee's logistics arm — tracks directly on spx.com.br, no verification needed." },
  { id: 'amazon', name: 'Amazon Shipping', country: 'US', kind: 'builtin', requiresKey: false, note: "Direct via Amazon's public tracker (track.amazon.com) — no account needed." },
  { id: 'dhl', name: 'DHL', country: 'DE', kind: 'builtin', requiresKey: false, note: "Direct via DHL's public utapi endpoint; falls back to the 17TRACK web engine when DHL blocks the request." },
  { id: 'ups', name: 'UPS', country: 'US', kind: 'builtin', requiresKey: false, note: "Direct via ups.com's tracking API (session handshake); falls back to the 17TRACK web engine." },
  { id: 'loggi', name: 'Loggi', country: 'BR', kind: 'builtin', requiresKey: false, note: "Direct via Loggi's public rastreador API — one event per status change." },
  { id: 'fedex', name: 'FedEx', country: 'US', kind: 'builtin', requiresKey: true, note: 'FedEx requires your aggregator key.' },
  { id: 'usps', name: 'USPS', country: 'US', kind: 'builtin', requiresKey: true, note: 'USPS requires your aggregator key.' },
  { id: 'generic', name: 'Universal (auto)', country: '', kind: 'builtin', requiresKey: true, note: 'Auto-detects the courier via your aggregator key.' },
  { id: '17track-web', name: '17TRACK (web)', country: '', kind: 'aggregator', requiresKey: false, website: 'https://www.17track.net/', note: 'Free universal engine — 2500+ carriers, no API key needed.' },
  { id: '17track', name: '17TRACK', country: '', kind: 'aggregator', requiresKey: true, website: 'https://www.17track.net/', note: 'Covers 2000+ carriers with a free API key.' },
  { id: 'aftership', name: 'AfterShip', country: '', kind: 'aggregator', requiresKey: true, website: 'https://www.aftership.com/', note: 'Covers 1000+ carriers with a free API key.' },
  { id: 'demo', name: 'Demo Courier', country: '', kind: 'demo', requiresKey: false, note: 'Codes starting with DEMO always simulate a full delivery journey.' },
]

const seen = (minsAgo) => new Date(Date.now() - minsAgo * 60_000).toISOString()

const ev = (msAgo, status, description, location, id) => ({
  id,
  date: iso(msAgo),
  status,
  description,
  location,
})

let trackings = [
  {
    id: 't1', code: 'PU123456789BR', name: 'Headset bluetooth', carrier: 'correios',
    autoUpdate: true, createdAt: iso(h(96)), lastCheck: iso(9 * 60_000), lastError: '',
    lastSeen: seen(30),
    events: [
      ev(h(96), 'pending', 'Objeto postado', 'Agência Central · São Paulo/SP', 'a1'),
      ev(h(78), 'in_transit', 'Objeto encaminhado', 'CTE São Paulo/SP', 'a2'),
      ev(h(47), 'in_transit', 'Objeto em trânsito - por favor aguarde', 'CTE Belo Horizonte/MG', 'a3'),
      ev(h(26), 'in_transit', 'Objeto em trânsito - por favor aguarde', 'CTE Curitiba/PR', 'a4'),
      ev(h(21), 'out_for_delivery', 'Objeto saiu para entrega ao destinatário', 'Curitiba/PR', 'a5'),
      ev(h(20.5), 'delivered', 'Objeto entregue ao destinatário', 'Curitiba/PR', 'a6'),
    ],
  },
  {
    id: 't2', code: 'JT9821473605821', name: 'Capa de celular', carrier: 'jtexpress',
    autoUpdate: true, createdAt: iso(h(50)), lastCheck: iso(4 * 60_000), lastError: '',
    lastSeen: seen(180), recipientCPF: '11144477735',
    events: [
      ev(h(50), 'pending', 'Pedido criado', 'Hub J&T · Cajamar/SP', 'b1'),
      ev(h(44), 'in_transit', 'Pacote coletado', 'Cajamar/SP', 'b2'),
      ev(h(31), 'in_transit', 'Em trânsito para o centro de distribuição', 'CTE · Guarulhos/SP', 'b3'),
      ev(h(3.2), 'out_for_delivery', 'Saiu para entrega', 'CD · São Paulo/SP', 'b4'),
    ],
  },
  {
    id: 't7', code: 'SPXBR320458190744', name: 'Carregador Shopee', carrier: 'spx',
    autoUpdate: true, createdAt: iso(h(18)), lastCheck: iso(3 * 60_000), lastError: '', eta: 'Standard',
    lastSeen: seen(45),
    events: [
      ev(h(18), 'pending', 'Pedido recebido', 'Hub SPX · Cajamar/SP', 'e1'),
      ev(h(16), 'in_transit', 'Pacote coletado', 'Cajamar/SP', 'e2'),
      ev(h(9), 'in_transit', 'Em trânsito para o centro de distribuição', 'CD SPX · Extrema/MG', 'e3'),
      ev(h(1.4), 'out_for_delivery', 'Saiu para entrega', 'São Paulo/SP', 'e4'),
    ],
  },
  {
    id: 't8', code: 'JT6012398472103', name: 'Fone Bluetooth', carrier: 'jtexpress',
    autoUpdate: true, createdAt: iso(h(2)), lastCheck: iso(30_000), lastError: '',
    lastSeen: null, needsCPF: true, events: [],
  },
  {
    id: 't3', code: 'DEMO-8231', name: 'Café especial 1kg', carrier: 'demo',
    autoUpdate: true, createdAt: iso(h(7)), lastCheck: iso(2 * 60_000), lastError: '',
    lastSeen: seen(15),
    events: [
      ev(h(7), 'pending', 'Shipment information received', 'Pluri-Deliver Hub', 'c1'),
      ev(h(5.8), 'in_transit', 'Picked up by courier', 'Pluri-Deliver Hub', 'c2'),
      ev(h(4.4), 'in_transit', 'In transit - moving to next facility', 'Sorting Center', 'c3'),
    ],
  },
  {
    id: 't4', code: 'TBA318264915', name: 'Kindle Paperwhite', carrier: 'amazon',
    autoUpdate: true, createdAt: iso(h(30)), lastCheck: iso(11 * 60_000),
    lastError: 'amazon: Amazon tracker has no events for this number yet',
    lastSeen: seen(60),
    events: [],
  },
  {
    id: 't6', code: 'NN428571043BR', name: 'Encomenda da China', carrier: 'correios',
    autoUpdate: true, createdAt: iso(h(6)), lastCheck: iso(20 * 60_000), lastError: '',
    lastSeen: null, eta: '03/11/2026', category: 'PACKET STANDARD IMPORTAÇÃO',
    events: [
      ev(h(126), 'pending', 'Shipment information received', 'China', 'n1'),
      ev(h(122), 'in_transit', 'Left the origin warehouse', 'China', 'n2'),
      ev(h(96), 'in_transit', 'Arrived in Brazil — cleared customs', 'Curitiba, Brazil', 'n3'),
      ev(h(72), 'in_transit', 'Released by customs', 'Curitiba, Brazil', 'n4'),
      ev(h(30), 'in_transit', 'In transit - please wait', 'Curitiba, Brazil', 'n5'),
    ],
  },
  {
    id: 't5', code: '1234567890', name: 'Camera lens', carrier: 'dhl',
    autoUpdate: false, createdAt: iso(h(120)), lastCheck: iso(30 * 60_000), lastError: '',
    lastSeen: null,
    events: [
      ev(h(120), 'pending', 'Shipment information received', 'Leipzig, Germany', 'd1'),
      ev(h(110), 'in_transit', 'Processed at facility', 'Leipzig, Germany', 'd2'),
      ev(h(90), 'exception', 'Delivery attempted - recipient not home', 'São Paulo, Brazil', 'd3'),
      ev(h(88), 'out_for_delivery', 'With delivery courier', 'São Paulo, Brazil', 'd4'),
    ],
  },
]

const nextStage = {
  demo: { status: 'out_for_delivery', description: 'Out for delivery', location: 'Your neighborhood' },
  default: { status: 'in_transit', description: 'Moving to next facility', location: 'In network' },
}

export const api = {
  ListTrackings: async () => JSON.parse(JSON.stringify(trackings)),

  AddTracking: async (code, name, carrier, autoUpdate, cpf = '') => {
    await sleep(600)
    const clean = String(code || '').trim().toUpperCase()
    const digits = String(cpf || '').replace(/\D/g, '')
    if (!clean) return { tracking: null, errorCode: 'empty_code', errorDetail: '' }
    if (trackings.some((t) => t.code === clean && t.carrier === carrier)) {
      return { tracking: null, errorCode: 'duplicate', errorDetail: '' }
    }
    const info = carriers.find((c) => c.id === carrier)
    if (!info) {
      return { tracking: null, errorCode: 'unknown_carrier', errorDetail: '' }
    }
    if (info.requiresCpf && digits && digits.length !== 11) {
      return { tracking: null, errorCode: 'invalid_cpf', errorDetail: '' }
    }
    const t = {
      id: 'm' + (trackings.length + 1), code: clean, name: String(name || '').trim(), carrier,
      autoUpdate: autoUpdate !== false, createdAt: new Date().toISOString(),
      lastCheck: new Date().toISOString(), lastError: '', lastSeen: null, events: [],
      recipientCPF: info.requiresCpf ? digits : '',
      needsCPF: !!(info.requiresCpf && !digits),
    }
    trackings.unshift(t)
    setTimeout(() => {
      if (t.needsCPF) return
      t.events = [
        { id: 'n1', date: new Date().toISOString(), status: 'pending', description: 'Shipment information received', location: 'Origin facility' },
      ]
      t.lastCheck = new Date().toISOString()
      emit('trackings:changed', null)
      emit('toast', { kind: 'update', title: t.name || t.code, body: 'Shipment information received · Origin facility' })
    }, 900)
    emit('trackings:changed', null)
    return { tracking: JSON.parse(JSON.stringify(t)), errorCode: '', errorDetail: '' }
  },

  SetTrackingCPF: async (id, cpf) => {
    await sleep(500)
    const t = trackings.find((x) => x.id === id)
    const digits = String(cpf || '').replace(/\D/g, '')
    if (!t) return { tracking: null, errorCode: 'not_found_tracking' }
    if (digits.length !== 11) return { tracking: null, errorCode: 'invalid_cpf' }
    t.recipientCPF = digits
    t.needsCPF = false
    setTimeout(() => {
      t.events = [
        { id: 'p1', date: new Date().toISOString(), status: 'pending', description: 'Pedido criado', location: 'Hub J&T · Cajamar/SP' },
      ]
      t.lastCheck = new Date().toISOString()
      emit('trackings:changed', null)
      emit('toast', { kind: 'update', title: t.name || t.code, body: 'Pedido criado · Hub J&T · Cajamar/SP' })
    }, 800)
    emit('trackings:changed', null)
    return { tracking: JSON.parse(JSON.stringify(t)), errorCode: '' }
  },

  RemoveTracking: async (id) => {
    trackings = trackings.filter((t) => t.id !== id)
    emit('trackings:changed', null)
    return null
  },

  RenameTracking: async (id, name) => {
    const t = trackings.find((x) => x.id === id)
    if (!t) throw new Error('tracking not found')
    t.name = name
    emit('trackings:changed', null)
    return JSON.parse(JSON.stringify(t))
  },

  SetAutoUpdate: async (id, on) => {
    const t = trackings.find((x) => x.id === id)
    if (!t) throw new Error('tracking not found')
    t.autoUpdate = on
    emit('trackings:changed', null)
    return JSON.parse(JSON.stringify(t))
  },

  RefreshTracking: async (id) => {
    await sleep(900)
    const t = trackings.find((x) => x.id === id)
    if (!t) throw new Error('tracking not found')
    if (t.lastError) {
      t.lastError = ''
      emit('trackings:changed', null)
      return JSON.parse(JSON.stringify(t))
    }
    if (t.events.length === 0) {
      t.events.push({ id: 'g' + Math.random().toString(36).slice(2, 8), date: new Date().toISOString(), status: 'pending', description: 'Shipment information received', location: 'Origin' })
      t.lastCheck = new Date().toISOString()
      emit('trackings:changed', null)
      return JSON.parse(JSON.stringify(t))
    }
    const stage = nextStage[t.carrier] || nextStage.default
    t.events.push({
      id: 'f' + Math.random().toString(36).slice(2, 10),
      date: new Date().toISOString(),
      status: stage.status,
      description: stage.description,
      location: stage.location,
    })
    t.lastCheck = new Date().toISOString()
    emit('trackings:changed', null)
    return JSON.parse(JSON.stringify(t))
  },

  RefreshAll: async () => {
    await sleep(400)
    for (const t of trackings) {
      if (t.lastError) continue
      if (t.events.length > 0 && t.events.length < 6) {
        const stage = nextStage[t.carrier] || nextStage.default
        t.events.push({
          id: 'z' + Math.random().toString(36).slice(2, 10),
          date: new Date().toISOString(),
          status: stage.status,
          description: stage.description,
          location: stage.location,
        })
        t.lastCheck = new Date().toISOString()
      }
    }
    emit('trackings:changed', null)
    emit('refresh:done', { count: trackings.length })
    return null
  },

  GetCarriers: async () => JSON.parse(JSON.stringify(carriers)),

  DetectCarriers: async (code) => {
    const c = String(code || '').trim().toUpperCase()
    if (!c) return []
    const rx = [
      [/^DEMO/, ['demo']],
      [/^[A-Z]{2}\d{9}[A-Z]{2}$/, ['correios', 'generic']],
      [/^JT\d{10,16}$/, ['jtexpress', 'generic']],
      [/^TB[AMBC]\d+$/, ['amazon', 'generic']],
      [/^JVGL\d+$|^JJD\d+$|^3S[A-Z0-9]+$|^LB[A-Z]*\d+$|^\d{10,11}$/, ['dhl', 'fedex', 'usps', 'generic']],
      [/^1Z[A-Z0-9]{10,16}$/, ['ups', 'generic']],
      [/^LOG[A-Z0-9]+$|^LG[A-Z0-9]{6,}$/, ['loggi', 'generic']],
      [/^9[0-4]\d{18,}$/, ['usps', 'generic']],
    ]
    for (const [re, ids] of rx) {
      if (re.test(c)) {
        return ids.map((id) => carriers.find((x) => x.id === id)).filter(Boolean)
      }
    }
    return [carriers.find((x) => x.id === 'generic')]
  },

  GetSettings: async () => JSON.parse(JSON.stringify(settings)),

  SaveSettings: async (s) => {
    settings = JSON.parse(JSON.stringify(s))
    emit('settings:changed', settings)
    return JSON.parse(JSON.stringify(settings))
  },

  TestNotification: async () => {
    emit('toast', { kind: 'test', title: 'Pluri-Deliver', body: 'Notifications are working! This is how update alerts will look.' })
    return null
  },

  MarkSeen: async (id) => {
    const t = trackings.find((x) => x.id === id)
    if (!t) throw new Error('tracking not found')
    t.lastSeen = new Date().toISOString()
    return JSON.parse(JSON.stringify(t))
  },

  SetWindowActive: async () => null,
  OpenDataFolder: async () => null,
  OpenCarriersFolder: async () => null,
  OpenScriptsFolder: async () => null,
  QuitApp: async () => { console.log('[mock] QuitApp — would exit the app'); return null },
  RequestClose: async () => { console.log('[mock] RequestClose - would hide or close the window'); return null },

  // Correios captcha flow (mock): a tiny SVG stands in for the real PNG.
  GetCorreiosCaptcha: async () => {
    await sleep(350)
    const n = Math.random().toString(36).slice(2, 7)
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="210" height="62"><rect width="100%" height="100%" fill="#0d1420"/><text x="50%" y="57%" font-family="monospace" font-size="26" fill="#8fe3c4" text-anchor="middle" letter-spacing="6">${n}</text><line x1="8" y1="18" x2="200" y2="46" stroke="#2c3a52"/><line x1="4" y1="40" x2="204" y2="26" stroke="#2c3a52"/></svg>`
    return { image: 'data:image/svg+xml;base64,' + btoa(svg), errorCode: '' }
  },

  VerifyCorreiosCaptcha: async (id, captcha) => {
    await sleep(700)
    const t = trackings.find((x) => x.id === id)
    if (!t) return { tracking: null, errorCode: 'not_found_tracking', errorDetail: '' }
    if (!captcha || captcha.length < 3) {
      return { tracking: null, errorCode: 'captcha_invalid', errorDetail: '' }
    }
    t.needsCaptcha = false
    t.lastCheck = new Date().toISOString()
    t.eta = '03/11/2026'
    t.events = [
      { id: 'nn1', date: iso(h(96)), status: 'pending', description: 'Objeto postado', location: 'CHINA' },
      { id: 'nn2', date: iso(h(70)), status: 'in_transit', description: 'Informações enviadas para análise da autoridade aduaneira', location: 'Curitiba/PR' },
      { id: 'nn3', date: iso(h(48)), status: 'in_transit', description: 'Análise concluída - importação autorizada', location: 'Curitiba/PR' },
      { id: 'nn4', date: iso(h(24)), status: 'in_transit', description: 'Saída do Centro Internacional', location: 'Curitiba/PR' },
      { id: 'nn5', date: iso(h(2)), status: 'in_transit', description: 'Objeto em transferência - por favor aguarde', location: 'Curitiba/PR' },
    ]
    emit('trackings:changed', null)
    return { tracking: JSON.parse(JSON.stringify(t)), errorCode: '', errorDetail: '' }
  },

  SyncCorreios: async (captcha) => {
    await sleep(800)
    if (!captcha || captcha.length < 3) {
      return { updated: 0, failedCodes: {}, errorCode: 'captcha_invalid' }
    }
    let updated = 0
    for (const t of trackings) {
      if (t.carrier !== 'correios') continue
      t.needsCaptcha = false
      t.lastCheck = new Date().toISOString()
      if (t.events.length === 0) {
        t.events.push({ id: 's' + Math.random().toString(36).slice(2, 8), date: new Date().toISOString(), status: 'in_transit', description: 'Objeto em transferência - por favor aguarde', location: 'Curitiba/PR' })
      }
      updated++
    }
    emit('trackings:changed', null)
    return { updated, failedCodes: {}, errorCode: '' }
  },
  AppInfo: async () => ({
    version: '1.0.0-mock',
    go: 'mock',
    storage: '(mock)',
    carriers: '(mock)',
  }),
}
