// App.jsx — root component: state, routing, event wiring.
import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { api, events, isDesktop } from './api.js'
import { translate } from './i18n.js'
import { I18nContext } from './lib/i18n-context.jsx'
import { sortTrackings, unseenCount } from './lib/utils.js'
import Titlebar from './components/Titlebar.jsx'
import Sidebar from './components/Sidebar.jsx'
import TrackingList from './components/TrackingList.jsx'
import TrackingDetail from './components/TrackingDetail.jsx'
import SettingsView from './components/SettingsView.jsx'
import AddTrackingModal from './components/AddTrackingModal.jsx'
import ConfirmModal from './components/ConfirmModal.jsx'
import RenameModal from './components/RenameModal.jsx'
import CaptchaModal from './components/CaptchaModal.jsx'
import CpfModal from './components/CpfModal.jsx'
import Toasts from './components/Toasts.jsx'

export default function App() {
  const [booted, setBooted] = useState(false)
  const [settings, setSettings] = useState(null)
  const [trackings, setTrackings] = useState([])
  const [carriers, setCarriers] = useState([])
  const [view, setView] = useState({ name: 'list', id: null })
  const [addOpen, setAddOpen] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(null)
  const [rename, setRename] = useState(null)
  const [captcha, setCaptcha] = useState(null) // null | {mode:'verify', id} | {mode:'sync'}
  const [cpfFor, setCpfFor] = useState(null) // null | tracking id needing a CPF
  const [toasts, setToasts] = useState([])
  const [refreshing, setRefreshing] = useState(false)
  const [appInfo, setAppInfo] = useState({})

  const i18n = useCallback(
    (key, params) => translate(settings?.language || 'en', key, params),
    [settings],
  )

  const pushToast = useCallback((toast) => {
    const id = Math.random().toString(36).slice(2)
    setToasts((prev) => [...prev.slice(-4), { id, ...toast }])
    setTimeout(() => setToasts((prev) => prev.filter((t) => t.id !== id)), 6000)
  }, [])

  const reload = useCallback(async () => {
    try {
      setTrackings(await api.listTrackings())
    } catch { /* ignore */ }
  }, [])

  // boot
  useEffect(() => {
    ;(async () => {
      try {
        const [s, t, c, info] = await Promise.all([
          api.getSettings(), api.listTrackings(), api.getCarriers(), api.appInfo(),
        ])
        setSettings(s)
        setTrackings(t)
        setCarriers(c)
        setAppInfo(info || {})
      } catch (e) {
        console.error('boot failed', e)
      } finally {
        setBooted(true)
      }
    })()
  }, [])

  // backend events
  useEffect(() => {
    const off = []
    off.push(events.on('trackings:changed', () => reload()))
    off.push(events.on('refresh:done', () => setRefreshing(false)))
    off.push(events.on('settings:changed', (s) => s && setSettings(s)))
    off.push(events.on('toast', (t) => t && pushToast(t)))
    return () => off.forEach((fn) => { try { fn && fn.dispose ? fn.dispose() : fn() } catch { } })
  }, [reload, pushToast])

  // window focus -> smart polling
  useEffect(() => {
    const send = (b) => { api.setWindowActive(b).catch(() => { }) }
    send(true)
    const onVis = () => send(document.visibilityState === 'visible')
    const onBlur = () => send(false)
    const onFocus = () => send(true)
    document.addEventListener('visibilitychange', onVis)
    window.addEventListener('blur', onBlur)
    window.addEventListener('focus', onFocus)
    return () => {
      document.removeEventListener('visibilitychange', onVis)
      window.removeEventListener('blur', onBlur)
      window.removeEventListener('focus', onFocus)
    }
  }, [])

  // theme
  useEffect(() => {
    document.documentElement.dataset.theme = settings?.theme === 'light' ? 'light' : 'dark'
    document.documentElement.lang = settings?.language || 'en'
  }, [settings])

  const updateSettings = useCallback(async (partial) => {
    const prev = settings
    const next = { ...settings, ...partial }
    setSettings(next) // optimistic
    try {
      const saved = await api.saveSettings(next)
      setSettings(saved)
    } catch {
      // Save rejected (e.g. the startup registry write failed): roll back to
      // the persisted state so the toggle stays honest.
      setSettings(prev)
      pushToast({ kind: 'update', title: i18n('toast.settingsError') })
    }
  }, [settings, pushToast, i18n])

  const refreshAll = useCallback(() => {
    if (refreshing) return
    setRefreshing(true)
    pushToast({ kind: 'update', title: i18n('toast.refreshing') })
    api.refreshAll().catch(() => setRefreshing(false))
  }, [refreshing, pushToast, i18n])

  const isCaptchaGated = useCallback((tr) => {
    if (!tr) return false
    if (tr.needsCaptcha) return true
    const c = carriers.find((x) => x.id === tr.carrier)
    return !!(c && c.captchaRequired)
  }, [carriers])

  const isCPFGated = useCallback((tr) => {
    if (!tr) return false
    if (tr.needsCPF) return true
    const c = carriers.find((x) => x.id === tr.carrier)
    return !!(c && c.requiresCpf && !tr.recipientCPF)
  }, [carriers])

  const refreshOne = useCallback((id) => {
    const tr = trackings.find((t) => t.id === id)
    if (isCaptchaGated(tr)) {
      setCaptcha({ mode: 'verify', id })
      return
    }
    if (isCPFGated(tr)) {
      setCpfFor(id)
      return
    }
    api.refreshTracking(id).catch(() => { })
  }, [trackings, isCaptchaGated, isCPFGated])

  const openSyncCaptcha = useCallback(() => setCaptcha({ mode: 'sync' }), [])

  const onCaptchaVerified = useCallback((res, mode) => {
    reload()
    if (mode === 'sync') {
      const n = res && res.updated ? res.updated : 0
      pushToast({ kind: 'update', title: i18n('toast.captchaSynced', { n }) })
    } else {
      pushToast({ kind: 'update', title: i18n('toast.captchaVerified') })
    }
  }, [reload, pushToast, i18n])

  const onAdded = useCallback((t) => {
    setView({ name: 'detail', id: t.id })
    if (t && t.needsCaptcha) {
      setCaptcha({ mode: 'verify', id: t.id })
    } else if (t && t.needsCPF) {
      setCpfFor(t.id)
    }
  }, [])

  const doDelete = useCallback(async (id) => {
    setConfirmDelete(null)
    try { await api.removeTracking(id) } catch { }
    setView({ name: 'list', id: null })
    pushToast({ kind: 'update', title: i18n('toast.deleted') })
  }, [pushToast, i18n])

  const doRename = useCallback(async (id, name) => {
    setRename(null)
    try { await api.renameTracking(id, name) } catch { }
  }, [])

  const toggleAuto = useCallback(async (id, on) => {
    try { await api.setAutoUpdate(id, on) } catch { }
    pushToast({ kind: 'update', title: i18n(on ? 'toast.autoOn' : 'toast.autoOff') })
  }, [pushToast, i18n])

  const markSeen = useCallback((id) => { api.markSeen(id).catch(() => { }) }, [])

  const sorted = useMemo(() => sortTrackings(trackings), [trackings])
  const totalUnseen = useMemo(() => trackings.reduce((n, t) => n + unseenCount(t), 0), [trackings])

  if (!booted || !settings) {
    return <div className="boot"><div className="boot-logo" /></div>
  }

  return (
    <I18nContext.Provider value={i18n}>
      <div className="app">
        <Titlebar i18n={i18n} />
        <div className="app-body">
          <Sidebar
            i18n={i18n}
            view={view}
            onNavigate={(v) => setView(v)}
            onAdd={() => setAddOpen(true)}
            onQuit={() => api.quitApp().catch(() => { })}
            unseen={totalUnseen}
            demoMode={settings.demoMode}
          />
          <main className="content">
            {view.name === 'list' && (
              <TrackingList
                trackings={sorted}
                carriers={carriers}
                onOpen={(id) => { setView({ name: 'detail', id }); markSeen(id) }}
                onAdd={() => setAddOpen(true)}
                onRefreshAll={refreshAll}
                refreshing={refreshing}
                onToggleAuto={toggleAuto}
                onVerify={openSyncCaptcha}
                onVerifyOne={(id) => setCaptcha({ mode: 'verify', id })}
                onCpfOne={(id) => setCpfFor(id)}
              />
            )}
            {view.name === 'detail' && (
              <TrackingDetail
                tracking={trackings.find((t) => t.id === view.id)}
                carriers={carriers}
                onBack={() => setView({ name: 'list', id: null })}
                onRefresh={refreshOne}
                onRename={() => setRename(trackings.find((t) => t.id === view.id))}
                onDelete={() => setConfirmDelete(trackings.find((t) => t.id === view.id))}
                onToggleAuto={toggleAuto}
                onSetCPF={() => setCpfFor(view.id)}
              />
            )}
            {view.name === 'settings' && (
              <SettingsView
                settings={settings}
                onChange={updateSettings}
                carriers={carriers}
                appInfo={appInfo}
              />
            )}
          </main>
        </div>

        <AddTrackingModal
          open={addOpen}
          onClose={() => setAddOpen(false)}
          carriers={carriers}
          onAdded={onAdded}
        />
        <ConfirmModal
          open={!!confirmDelete}
          tracking={confirmDelete}
          onClose={() => setConfirmDelete(null)}
          onConfirm={() => confirmDelete && doDelete(confirmDelete.id)}
        />
        <RenameModal
          open={!!rename}
          tracking={rename}
          onClose={() => setRename(null)}
          onConfirm={(name) => rename && doRename(rename.id, name)}
        />
        <CaptchaModal
          open={!!captcha}
          mode={captcha ? captcha.mode : 'verify'}
          tracking={captcha && captcha.mode === 'verify'
            ? trackings.find((t) => t.id === captcha.id)
            : null}
          onClose={() => setCaptcha(null)}
          onVerified={onCaptchaVerified}
        />
        <CpfModal
          open={!!cpfFor}
          tracking={cpfFor ? trackings.find((t) => t.id === cpfFor) : null}
          onClose={() => setCpfFor(null)}
          onSaved={() => {
            reload()
            pushToast({ kind: 'update', title: i18n('toast.cpfSaved') })
          }}
        />
        <Toasts toasts={toasts} onDismiss={(id) => setToasts((p) => p.filter((t) => t.id !== id))} />
      </div>
    </I18nContext.Provider>
  )
}
