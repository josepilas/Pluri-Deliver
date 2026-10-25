package main

import (
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	_ "time/tzdata" // IANA zones (America/Sao_Paulo) on Windows

	"github.com/wailsapp/wails/v2/pkg/options"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"plurideliver/internal/autostart"
	"plurideliver/internal/engine"
	"plurideliver/internal/i18n"
	"plurideliver/internal/models"
	"plurideliver/internal/notify"
	"plurideliver/internal/scheduler"
	"plurideliver/internal/service"
	"plurideliver/internal/store"
	"plurideliver/internal/tray"
)

//go:embed build/appicon.png
var iconPNG []byte

//go:embed build/windows/icon.ico
var iconICO []byte

const appVersion = "1.7.0"

// startHidden reports whether the app was launched with --hidden (set by the
// Windows startup entry): the window is created invisible and the app runs
// quietly in the background until the user opens it from the Start menu.
var startHidden bool

// AddResult is the structured outcome of AddTracking (error codes are
// localized in the frontend).
type AddResult struct {
	Tracking    *models.Tracking `json:"tracking"`
	ErrorCode   string           `json:"errorCode"`
	ErrorDetail string           `json:"errorDetail,omitempty"`
}

// CaptchaImageResult carries a fresh Correios captcha as a data URL.
type CaptchaImageResult struct {
	Image     string `json:"image"`               // data:image/png;base64,...
	ErrorCode string `json:"errorCode,omitempty"` // "network"
}

// VerifyResult is the outcome of a captcha-verified query.
type VerifyResult struct {
	Tracking    *models.Tracking `json:"tracking,omitempty"`
	ErrorCode   string           `json:"errorCode,omitempty"` // captcha_invalid | not_found | no_events | network
	ErrorDetail string           `json:"errorDetail,omitempty"`
}

// SyncResult summarizes the batch captcha sync of Correios trackings.
type SyncResult struct {
	Updated     int               `json:"updated"`
	FailedCodes map[string]string `json:"failedCodes,omitempty"`
	ErrorCode   string            `json:"errorCode,omitempty"`
}

// App is the Wails-bound application core.
type App struct {
	ctx      context.Context
	store    *store.Store
	engine   *engine.Engine
	svc      *service.Service
	sched    *scheduler.Scheduler
	notifier *notify.Notifier
	tray     *tray.Tray

	quitting atomic.Bool // QuitApp forces a full exit even in background mode
	hidToast atomic.Bool // "still running in background" toast shown once
}

// NewApp wires every layer together.
func NewApp() *App {
	// The startup entry (and power users) may pass --hidden so the app
	// opens silently in the background — notifications keep flowing.
	startHidden = hasHiddenFlag(os.Args[1:])

	st, err := store.New()
	if err != nil {
		panic(fmt.Sprintf("Pluri-Deliver: cannot open storage: %v", err))
	}
	eng := engine.New(st.CarriersDir(), st.ScriptsDir())

	app := &App{store: st, engine: eng, notifier: notify.New()}
	app.notifier.SetIcon(iconPNG)
	app.svc = service.New(st, eng, app.notifier, app.emit)
	app.sched = scheduler.New(app.svc, st.Settings)
	return app
}

// hasHiddenFlag reports whether the args contain --hidden / -hidden (the
// quoted registry entry survives os.Args splitting untouched).
func hasHiddenFlag(args []string) bool {
	for _, a := range args {
		if a == "--hidden" || a == "-hidden" {
			return true
		}
	}
	return false
}

func (a *App) emit(name string, data ...any) {
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, name, data...)
	}
}

// ---- lifecycle ----------------------------------------------------------------

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.notifier.SetContext(ctx)
	tray.SetAppUserModelID() // proper identity for Win10/11 toast branding
	a.startTray()
	a.sched.Start()
	a.healAutostartEntry()
}

// startTray puts the Pluri-Deliver icon in the notification area (unless the
// user hid it): one click reopens the window, right-click offers refresh and
// a real exit, and balloons keep notifications alive on every Windows
// version (they render as toasts on 10/11).
func (a *App) startTray() {
	if !a.store.Settings().TrayEnabled() {
		return
	}
	if a.tray != nil {
		return // already running
	}
	icoPath, err := writeCacheFile("icon.ico", iconICO)
	if err != nil {
		log.Printf("tray: cannot stage icon: %v", err)
		return
	}
	lang := a.store.Settings().Language
	tr, err := tray.Start(tray.Config{
		IconPath: icoPath,
		Tooltip:  "Pluri-Deliver",
		Menu: []tray.Item{
			{Label: i18n.T(lang, "tray_open"), Fn: a.trayShow},
			{Label: i18n.T(lang, "tray_refresh"), Fn: func() { go a.svc.RefreshAll() }},
			{Separator: true},
			{Label: i18n.T(lang, "tray_quit"), Fn: a.QuitApp},
		},
	})
	if err != nil {
		log.Printf("tray: %v (continuing without the tray icon)", err)
		return
	}
	a.tray = tr
	a.notifier.SetBalloonFn(tr.Balloon)
}

// trayShow reveals and focuses the main window (tray click / menu Open).
func (a *App) trayShow() {
	if a.ctx == nil {
		return
	}
	wailsruntime.WindowUnminimise(a.ctx)
	wailsruntime.WindowShow(a.ctx)
	a.sched.SetActive(true)
}

// writeCacheFile stages an embedded asset in the user's cache dir and
// returns its path.
func writeCacheFile(name string, content []byte) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "Pluri-Deliver")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, content, 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// healAutostartEntry re-asserts the "start with Windows" registry entry when
// the setting is on, so an executable that was moved or renamed heals its
// startup path on the next launch. Failures are silent best-effort.
func (a *App) healAutostartEntry() {
	if !a.store.Settings().StartWithWindows {
		return
	}
	_ = syncAutostart(true)
}

// syncAutostart registers (or removes) the app in the current user's
// startup entries — HKCU\Software\Microsoft\Windows\CurrentVersion\Run,
// no elevation required. The entry always carries the --hidden switch so a
// login launch runs quietly in the background (per the user's request);
// clicking the app icon again simply reveals the running instance.
func syncAutostart(on bool) error {
	if !on {
		return autostart.SetEnabled(false, "", "")
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate the app executable: %w", err)
	}
	return autostart.SetEnabled(true, exe, autostart.HiddenArg)
}

func (a *App) shutdown(_ context.Context) {
	a.sched.Stop()
	if a.tray != nil {
		a.tray.Stop()
		a.tray = nil
	}
}

// beforeClose decides what the window's native close (Alt+F4, taskbar close)
// does. With "run in the background" enabled, closing hides the window and
// the app keeps watching parcels + sending notifications; the scheduler must
// keep running, so nothing is stopped here. Without it, returning false lets
// Wails close the app (OnShutdown stops the scheduler and the tray).
func (a *App) beforeClose(ctx context.Context) bool {
	if a.quitting.Load() {
		return false // explicit "Close application": allow the exit
	}
	if a.store.Settings().RunInBackground {
		a.hideToBackground(ctx)
		return true // veto the close: keep running in the background
	}
	return false
}

// hideToBackground hides the window and drops the scheduler to the idle
// cadence — the shared behavior of the native close and the titlebar button.
func (a *App) hideToBackground(ctx context.Context) {
	wailsruntime.WindowHide(ctx)
	a.sched.SetActive(false) // idle polling cadence while hidden
	a.notifyHiddenOnce()
}

// notifyHiddenOnce tells the user (once per app run) that closing the window
// did not exit the app, so nobody thinks Pluri-Deliver died.
func (a *App) notifyHiddenOnce() {
	if a.hidToast.Swap(true) {
		return
	}
	lang := a.store.Settings().Language
	a.notifier.Notify("info", i18n.T(lang, "hidden_title"), i18n.T(lang, "hidden_body"))
}

// QuitApp exits Pluri-Deliver completely — even with background mode on.
// runtime.Quit consults OnBeforeClose, so the intent is flagged first; the
// flag also makes the plain X button quit when a race lands both paths.
func (a *App) QuitApp() {
	if a.ctx == nil {
		return
	}
	a.quitting.Store(true)
	wailsruntime.Quit(a.ctx)
}

// RequestClose is what the custom titlebar's X button calls — the Wails JS
// runtime exposes no WindowClose (only Hide/Show/Minimise/Maximise), so the
// close request has to come through Go anyway. It applies exactly the same
// rules as the native window close: background mode hides the window, a
// plain close really exits.
func (a *App) RequestClose() {
	if a.ctx == nil || a.quitting.Load() {
		return
	}
	if a.store.Settings().RunInBackground {
		a.hideToBackground(a.ctx)
		return
	}
	a.QuitApp()
}

func (a *App) onSecondInstance(_ options.SecondInstanceData) {
	// The app is already running (possibly hidden in the background):
	// reveal and focus it instead of spawning a duplicate.
	if a.ctx != nil {
		wailsruntime.WindowUnminimise(a.ctx)
		wailsruntime.WindowShow(a.ctx)
		a.sched.SetActive(true)
	}
}

// ---- bound methods (frontend API) -----------------------------------------------

// ListTrackings returns all saved trackings.
func (a *App) ListTrackings() []*models.Tracking { return a.svc.List() }

// AddTracking saves a new tracking and triggers its first refresh.
// cpf carries the recipient's CPF for carriers that need it (J&T Brasil).
func (a *App) AddTracking(code, name, carrier string, autoUpdate bool, cpf string) AddResult {
	t, code2 := a.svc.Add(code, name, carrier, autoUpdate, cpf)
	if code2 != "" {
		return AddResult{ErrorCode: code2}
	}
	return AddResult{Tracking: t}
}

// RemoveTracking deletes a tracking.
func (a *App) RemoveTracking(id string) error { return a.svc.Remove(id) }

// RenameTracking changes the display name.
func (a *App) RenameTracking(id, name string) (*models.Tracking, error) {
	return a.svc.Rename(id, name)
}

// SetAutoUpdate toggles background updates for one tracking.
func (a *App) SetAutoUpdate(id string, on bool) (*models.Tracking, error) {
	return a.svc.SetAutoUpdate(id, on)
}

// MarkSeen clears the "new events" badge for a tracking.
func (a *App) MarkSeen(id string) (*models.Tracking, error) { return a.svc.MarkSeen(id) }

// RefreshTracking synchronously refreshes one tracking (manual updates).
func (a *App) RefreshTracking(id string) (*models.Tracking, error) {
	t, ok := a.store.Get(id)
	if !ok {
		return nil, fmt.Errorf("tracking not found")
	}
	if err := a.svc.Refresh(t); err != nil {
		return nil, err
	}
	out, _ := a.store.Get(id)
	return out, nil
}

// RefreshAll triggers a refresh of every tracking (async).
func (a *App) RefreshAll() { go a.svc.RefreshAll() }

// GetCorreiosCaptcha fetches a fresh Correios captcha image (data URL).
func (a *App) GetCorreiosCaptcha() CaptchaImageResult {
	img, err := a.svc.CaptchaImage()
	if err != nil {
		return CaptchaImageResult{ErrorCode: "network"}
	}
	return CaptchaImageResult{Image: "data:image/png;base64," + base64.StdEncoding.EncodeToString(img)}
}

// VerifyCorreiosCaptcha types the user's answer and pulls the full timeline
// of one tracking.
func (a *App) VerifyCorreiosCaptcha(id, captcha string) VerifyResult {
	t, code := a.svc.VerifyCaptcha(id, captcha)
	res := VerifyResult{Tracking: t, ErrorCode: code}
	if code == "network" || code == "not_found" || code == "no_events" {
		res.ErrorDetail = "Correios"
	}
	return res
}

// SyncCorreios uses one captcha to refresh every Correios tracking (batch).
func (a *App) SyncCorreios(captcha string) SyncResult {
	updated, failed, code := a.svc.SyncAllCorreios(captcha)
	return SyncResult{Updated: updated, FailedCodes: failed, ErrorCode: code}
}

// SetTrackingCPF stores the recipient CPF of a tracking (J&T Brasil asks
// for it on every query) and refreshes right away.
func (a *App) SetTrackingCPF(id, cpf string) VerifyResult {
	t, code := a.svc.SetTrackingCPF(id, cpf)
	return VerifyResult{Tracking: t, ErrorCode: code}
}

// CaptchaGatedCount reports how many saved trackings need a Correios
// verification (drives the list badge).
func (a *App) CaptchaGatedCount() int {
	return len(a.svc.CaptchaGatedCodes())
}

// GetCarriers returns every registered carrier adapter.
func (a *App) GetCarriers() []models.CarrierInfo { return a.engine.Carriers() }

// DetectCarriers returns carriers matching a tracking code.
func (a *App) DetectCarriers(code string) []models.CarrierInfo { return a.engine.Detect(code) }

// GetSettings returns the current settings.
func (a *App) GetSettings() models.Settings { return a.store.Settings() }

// SaveSettings persists settings and broadcasts the change. The "start with
// Windows" flag also drives the user's Run registry entry: when the registry
// write fails, the previous flag value is kept and the error surfaced so the
// UI can roll the toggle back — every other setting still saves normally.
// The tray-icon toggle is applied live (icon added/removed without a
// restart).
func (a *App) SaveSettings(s models.Settings) (models.Settings, error) {
	prev := a.store.Settings()
	var regErr error
	if s.StartWithWindows != prev.StartWithWindows {
		if regErr = syncAutostart(s.StartWithWindows); regErr != nil {
			s.StartWithWindows = prev.StartWithWindows
		}
	}
	trayChanged := s.HideTrayIcon != prev.HideTrayIcon
	if err := a.store.SaveSettings(s); err != nil {
		return a.store.Settings(), err
	}
	out := a.store.Settings()
	if trayChanged {
		if out.HideTrayIcon {
			if a.tray != nil {
				a.tray.SetVisible(false)
			}
		} else {
			a.startTray()
			if a.tray != nil {
				a.tray.SetVisible(true)
			}
		}
	}
	a.emit("settings:changed", out)
	if regErr != nil {
		return out, regErr
	}
	return out, nil
}

// TestNotification fires a sample toast.
func (a *App) TestNotification() {
	lang := a.store.Settings().Language
	a.notifier.Notify("test", i18n.T(lang, "test_title"), i18n.T(lang, "test_body"))
}

// SetWindowActive reports focus state (drives smart polling intervals).
func (a *App) SetWindowActive(active bool) { a.sched.SetActive(active) }

// OpenDataFolder reveals the storage folder in Explorer.
func (a *App) OpenDataFolder() { notify.OpenPath(a.store.Dir()) }

// OpenCarriersFolder reveals the user carrier adapters folder.
func (a *App) OpenCarriersFolder() { notify.OpenPath(a.store.CarriersDir()) }

// OpenScriptsFolder reveals the user Lua policy-scripts folder
// (polling.lua / notify.lua overrides).
func (a *App) OpenScriptsFolder() { notify.OpenPath(a.store.ScriptsDir()) }

// AppInfo returns build/runtime details.
func (a *App) AppInfo() map[string]string {
	return map[string]string{
		"version":  appVersion,
		"go":       runtime.Version(),
		"storage":  a.store.Dir(),
		"carriers": a.store.CarriersDir(),
		"scripts":  a.store.ScriptsDir(),
	}
}
