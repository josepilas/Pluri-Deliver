// api.js — single access point to the Wails backend with a full browser mock.
// In a browser (vite dev), every method routes to the mock implementation so
// the UI can be developed and screenshotted without a desktop build.
import * as WailsApp from '../wailsjs/go/main/App'
import * as WailsRT from '../wailsjs/runtime/runtime'
import * as mock from './mock/mock.js'

const underWails =
  typeof window !== 'undefined' && !!(window.go && window.go.main && window.go.main.App)

export const isDesktop = underWails
const impl = underWails ? WailsApp : mock.api
const rt = underWails ? WailsRT : mock.rt

export const api = {
  listTrackings: () => impl.ListTrackings(),
  addTracking: (code, name, carrier, autoUpdate, cpf = '') => impl.AddTracking(code, name, carrier, autoUpdate, cpf),
  removeTracking: (id) => impl.RemoveTracking(id),
  renameTracking: (id, name) => impl.RenameTracking(id, name),
  setAutoUpdate: (id, on) => impl.SetAutoUpdate(id, on),
  refreshTracking: (id) => impl.RefreshTracking(id),
  refreshAll: () => impl.RefreshAll(),
  getCarriers: () => impl.GetCarriers(),
  detectCarriers: (code) => impl.DetectCarriers(code),
  getSettings: () => impl.GetSettings(),
  saveSettings: (s) => impl.SaveSettings(s),
  testNotification: () => impl.TestNotification(),
  markSeen: (id) => impl.MarkSeen(id),
  setWindowActive: (b) => impl.SetWindowActive(b),
  getCorreiosCaptcha: () => impl.GetCorreiosCaptcha(),
  verifyCorreiosCaptcha: (id, captcha) => impl.VerifyCorreiosCaptcha(id, captcha),
  syncCorreios: (captcha) => impl.SyncCorreios(captcha),
  setTrackingCPF: (id, cpf) => impl.SetTrackingCPF(id, cpf),
  openDataFolder: () => impl.OpenDataFolder(),
  openCarriersFolder: () => impl.OpenCarriersFolder(),
  openScriptsFolder: () => impl.OpenScriptsFolder(),
  appInfo: () => impl.AppInfo(),
  quitApp: () => impl.QuitApp(),
  requestClose: () => impl.RequestClose(),
}

export const events = {
  on: (name, cb) => rt.EventsOn(name, cb),
}

export const windowControls = {
  minimize: () => rt.WindowMinimise(),
  toggleMaximize: () => rt.WindowToggleMaximise(),
  // NOTE: the Wails JS runtime exposes NO WindowClose — only Go can close
  // the window (the old rt.WindowClose() call was a silent no-op: the X
  // button did nothing). The titlebar now asks the Go side, which applies
  // the background-mode rules: hide when "keep running" is on, real exit
  // otherwise.
  close: () => impl.RequestClose(),
}

export const browser = {
  open: (url) => (underWails ? rt.BrowserOpenURL(url) : window.open(url, '_blank')),
}
