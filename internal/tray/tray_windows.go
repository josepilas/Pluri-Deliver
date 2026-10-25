//go:build windows

package tray

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---- Win32 constants ---------------------------------------------------------

const (
	wmClose   = 0x0010
	wmDestroy = 0x0002
	wmNull    = 0x0000
	wmApp     = 0x8000
	// Classic (pre-NOTIFYICON_VERSION_4) tray callback: lParam carries the
	// mouse message, wParam the icon id. Works unchanged from Windows 95 to 11.
	wmLButtonUp = 0x0202
	wmRButtonUp = 0x0205

	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nifInfo    = 0x00000010

	niifInfo         = 0x00000001
	niifRespectQuiet = 0x00000080 // don't break Focus Assist / quiet hours

	tpmBottomAlign = 0x0020
	tpmRightAlign  = 0x0008
	tpmReturnCmd   = 0x0100
	tpmNoNotify    = 0x0080

	mfString    = 0x00000000
	mfSeparator = 0x00000800

	imageIcon      = 1
	lrLoadFromFile = 0x00000010

	trayClass = "PluriDeliverTrayWnd"
	trayUID   = 1
)

// ---- Win32 imports ----------------------------------------------------------

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW  = user32.NewProc("RegisterClassExW")
	procCreateWindowExW   = user32.NewProc("CreateWindowExW")
	procDefWindowProcW    = user32.NewProc("DefWindowProcW")
	procGetMessageW       = user32.NewProc("GetMessageW")
	procTranslateMessage  = user32.NewProc("TranslateMessage")
	procDispatchMessageW  = user32.NewProc("DispatchMessageW")
	procPostQuitMessage   = user32.NewProc("PostQuitMessage")
	procPostMessageW      = user32.NewProc("PostMessageW")
	procDestroyWindow     = user32.NewProc("DestroyWindow")
	procCreatePopupMenu   = user32.NewProc("CreatePopupMenu")
	procDestroyMenu       = user32.NewProc("DestroyMenu")
	procAppendMenuW       = user32.NewProc("AppendMenuW")
	procTrackPopupMenu    = user32.NewProc("TrackPopupMenu")
	procSetForegroundWnd  = user32.NewProc("SetForegroundWindow")
	procGetCursorPos      = user32.NewProc("GetCursorPos")
	procLoadImageW        = user32.NewProc("LoadImageW")
	procRegisterWindowMsg = user32.NewProc("RegisterWindowMessageW")
	procUnregisterClassW  = user32.NewProc("UnregisterClassW")
	procGetModuleHandleW  = kernel32.NewProc("GetModuleHandleW")

	procShellNotifyIcon = shell32.NewProc("Shell_NotifyIconW")
	procSetAppUserModel = shell32.NewProc("SetCurrentProcessExplicitAppUserModelID")
)

// ---- Win32 structures -------------------------------------------------------
//
// Field order and Go's natural alignment reproduce the exact C layouts on
// both amd64 and 386 (verified against the well-known sizeof values:
// NOTIFYICONDATAW = 976 on x64, 960 on x86; MSG = 48 on x64).

type point struct {
	X, Y int32
}

type msgStruct struct {
	HWnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       point
	LPrivate uint32
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type notifyIconData struct {
	Size            uint32
	HWnd            uintptr
	UID             uint32
	UFlags          uint32
	CallbackMessage uint32
	HIcon           uintptr
	Tip             [128]uint16
	DwState         uint32
	DwStateMask     uint32
	Info            [256]uint16
	Union           uint32 // uTimeout when setting, uVersion when reading
	InfoTitle       [64]uint16
	DwInfoFlags     uint32
	GuidItem        windows.GUID
	HBalloonIcon    uintptr
}

// ---- Tray -------------------------------------------------------------------

// Tray owns the notification-area icon of the running process. All exported
// methods are safe from any goroutine.
type Tray struct {
	cfg  Config
	proc uintptr // WndProc callback (created once)

	mu              sync.Mutex
	hwnd            uintptr
	hInst           uintptr // module handle (class unregistration)
	hicon           uintptr
	visible         bool // icon currently in the notification area
	started         bool
	wmTaskbar       uint32 // "TaskbarCreated" broadcast (explorer restart)
	taskbarMsgReady bool

	done chan struct{} // closed when the message loop exited
}

// Start loads the icon, creates the hidden message window and shows the tray
// icon. It returns an error when the icon cannot be loaded or the window
// cannot be created (the app keeps working without a tray in that case).
func Start(cfg Config) (*Tray, error) {
	if cfg.IconPath == "" {
		return nil, fmt.Errorf("tray: no icon path")
	}
	t := &Tray{cfg: cfg, done: make(chan struct{})}
	h, err := loadIcon(cfg.IconPath)
	if err != nil {
		return nil, err
	}
	t.hicon = h
	t.proc = syscall.NewCallback(t.wndProc)
	t.started = true
	go t.run()
	// Give the window a moment to exist so early Balloon() calls don't race.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		t.mu.Lock()
		ok := t.hwnd != 0
		t.mu.Unlock()
		if ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return t, nil
}

// run owns the tray's dedicated thread: window creation, the icon and the
// message pump. Everything tray-related that is message-driven happens here.
func (t *Tray) run() {
	// Shell_NotifyIcon delivers callback messages to the window's thread, so
	// the pump must live on the same locked OS thread that created it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	cls, _ := windows.UTF16PtrFromString(trayClass)
	title, _ := windows.UTF16PtrFromString("Pluri-Deliver tray")

	// "TaskbarCreated" is broadcast by Explorer whenever the taskbar is
	// (re)created — the signal to re-add our icon (KB148855).
	if r, _, _ := procRegisterWindowMsg.Call(uintptr(unsafe.Pointer(&taskbarCreatedUTF16[0]))); r != 0 {
		t.mu.Lock()
		t.wmTaskbar = uint32(r)
		t.taskbarMsgReady = true
		t.mu.Unlock()
	}

	hInst, _, _ := procGetModuleHandleW.Call(0)
	t.mu.Lock()
	t.hInst = hInst
	t.mu.Unlock()
	wc := wndClassEx{
		WndProc:   t.proc,
		Instance:  hInst,
		ClassName: cls,
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))) // "already exists" on re-register is fine

	hwnd, _, err := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, // hidden top-level window (style 0, never shown)
		0, 0, hInst, 0)
	if hwnd == 0 {
		t.mu.Lock()
		t.started = false
		t.mu.Unlock()
		close(t.done)
		return
	}
	_ = err // atom=0 details irrelevant; hwnd==0 already covers failure

	t.mu.Lock()
	t.hwnd = hwnd
	t.mu.Unlock()
	t.nimAdd()

	var m msgStruct
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 || r == ^uintptr(0) { // WM_QUIT or error
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}

	t.mu.Lock()
	hwnd = t.hwnd
	t.hwnd = 0
	t.mu.Unlock()
	if hwnd != 0 {
		procDestroyWindow.Call(hwnd)
	}
	t.nimDelete()
	// Free the window class so a later Start() registers a fresh WndProc
	// instead of dispatching through this dead instance.
	if hInst != 0 {
		procUnregisterClassW.Call(uintptr(unsafe.Pointer(trayClassUTF16Ptr)), hInst)
	}
	close(t.done)
}

// wndProc is the window procedure of the hidden tray window. It runs on the
// pump goroutine's OS thread only.
func (t *Tray) wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	t.mu.Lock()
	taskbar, tbOK := t.wmTaskbar, t.taskbarMsgReady
	visible := t.visible
	t.mu.Unlock()

	switch {
	case tbOK && msg == taskbar:
		if visible {
			t.nimAdd() // explorer restarted: the icon is gone, put it back
		}
		return 0

	case msg == wmApp+trayUID:
		switch lParam {
		case wmLButtonUp:
			// A plain left click runs the first menu action ("Open").
			if fn := t.firstAction(); fn != nil {
				go fn()
			}
		case wmRButtonUp:
			t.showMenu(hwnd)
		}
		return 0

	case msg == wmDestroy:
		procPostQuitMessage.Call(0)
		return 0

	case msg == wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

// firstAction returns the Fn of the first non-separator menu entry — the
// natural target of a plain left click (Open).
func (t *Tray) firstAction() func() {
	for _, it := range t.cfg.Menu {
		if !it.Separator && it.Fn != nil {
			return it.Fn
		}
	}
	return nil
}

// showMenu opens the context menu and runs the picked entry. Runs on the
// pump thread (inside wndProc).
func (t *Tray) showMenu(hwnd uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	nextID := uintptr(100)
	var ids []uintptr
	var fns []func()
	for _, it := range t.cfg.Menu {
		if it.Separator {
			procAppendMenuW.Call(menu, mfSeparator, 0, 0)
			continue
		}
		lbl, err := windows.UTF16PtrFromString(it.Label)
		if err != nil {
			continue
		}
		procAppendMenuW.Call(menu, mfString, nextID, uintptr(unsafe.Pointer(lbl)))
		ids = append(ids, nextID)
		fns = append(fns, it.Fn)
		nextID++
	}

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	// Classic tray-menu quirk (KB Q135788): without SetForegroundWindow the
	// menu refuses to dismiss when the user clicks elsewhere; without the
	// trailing WM_NULL the pump thread never regains foreground logic.
	procSetForegroundWnd.Call(hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(
		menu,
		tpmRightAlign|tpmBottomAlign|tpmReturnCmd|tpmNoNotify,
		uintptr(int64(pt.X)), uintptr(int64(pt.Y)),
		0, hwnd, 0)
	procPostMessageW.Call(hwnd, wmNull, 0, 0)

	if cmd == 0 {
		return // dismissed
	}
	for i, id := range ids {
		if id == cmd && fns[i] != nil {
			go fns[i]()
			break
		}
	}
}

// ---- icon management --------------------------------------------------------

func (t *Tray) baseNID(flags uint32) notifyIconData {
	t.mu.Lock()
	hwnd, hicon, tip := t.hwnd, t.hicon, t.cfg.Tooltip
	t.mu.Unlock()
	var nid notifyIconData
	nid.Size = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwnd
	nid.UID = trayUID
	nid.UFlags = flags
	nid.CallbackMessage = wmApp + trayUID
	nid.HIcon = hicon
	copyUTF16(nid.Tip[:], tip)
	return nid
}

func (t *Tray) nimAdd() {
	nid := t.baseNID(nifMessage | nifIcon | nifTip)
	if r, _, _ := procShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); r != 0 {
		t.mu.Lock()
		t.visible = true
		t.mu.Unlock()
	}
}

func (t *Tray) nimDelete() {
	t.mu.Lock()
	hwnd := t.hwnd
	t.visible = false
	t.mu.Unlock()
	if hwnd == 0 {
		return
	}
	nid := t.baseNID(0)
	nid.HWnd = hwnd
	procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
}

// SetVisible adds or removes the notification-area icon.
func (t *Tray) SetVisible(on bool) {
	t.mu.Lock()
	prev, started := t.visible, t.started
	t.mu.Unlock()
	if !started || prev == on {
		return
	}
	if on {
		t.nimAdd()
	} else {
		t.nimDelete()
	}
}

// SetTooltip changes the hover text of the icon.
func (t *Tray) SetTooltip(s string) {
	t.mu.Lock()
	t.cfg.Tooltip = s
	t.mu.Unlock()
	nid := t.baseNID(nifMessage | nifIcon | nifTip)
	procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

// Balloon shows a classic notification balloon (rendered as a toast on
// Windows 10/11). Best effort: invisible without a tray icon.
func (t *Tray) Balloon(title, text string) {
	t.mu.Lock()
	visible, hwnd := t.visible, t.hwnd
	t.mu.Unlock()
	if !visible || hwnd == 0 {
		return
	}
	nid := t.baseNID(nifMessage | nifIcon | nifTip | nifInfo)
	copyUTF16(nid.InfoTitle[:], title)
	copyUTF16(nid.Info[:], text)
	nid.DwInfoFlags = niifInfo | niifRespectQuiet
	nid.Union = 10 // seconds the balloon lingers (advisory on Win10+)
	procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

// Stop removes the icon and ends the pump thread.
func (t *Tray) Stop() {
	t.mu.Lock()
	started, hwnd := t.started, t.hwnd
	t.started = false
	t.mu.Unlock()
	if !started {
		return
	}
	if hwnd != 0 {
		procPostMessageW.Call(hwnd, wmClose, 0, 0)
	}
	select {
	case <-t.done:
	case <-time.After(2 * time.Second):
		// The pump didn't exit in time — still drop the icon so the user
		// is not left with a ghost.
		t.nimDelete()
	}
}

// ---- helpers ----------------------------------------------------------------

var taskbarCreatedUTF16 = func() []uint16 {
	u, _ := windows.UTF16FromString("TaskbarCreated")
	return u
}()

// trayClassUTF16Ptr is the pinned class-name string shared by
// RegisterClassExW and UnregisterClassW.
var trayClassUTF16Ptr = func() *uint16 {
	p, _ := windows.UTF16PtrFromString(trayClass)
	return p
}()

func copyUTF16(dst []uint16, s string) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return
	}
	n := len(dst) - 1
	if len(u) < n {
		n = len(u)
	}
	copy(dst[:n], u[:n])
}

func loadIcon(path string) (uintptr, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	h, _, callErr := procLoadImageW.Call(
		0, uintptr(unsafe.Pointer(p)),
		imageIcon, 32, 32, lrLoadFromFile)
	if h == 0 {
		return 0, fmt.Errorf("tray: LoadImage(%s): %v", path, callErr)
	}
	return h, nil
}

// SetAppUserModelID brands the process so Windows 10/11 shows balloon tips
// and the window under the proper app identity (no-op where unavailable).
func SetAppUserModelID() {
	if err := procSetAppUserModel.Find(); err != nil {
		return // pre-Vista: not available, not needed
	}
	p, err := windows.UTF16PtrFromString("PluriDeliver.app")
	if err != nil {
		return
	}
	procSetAppUserModel.Call(uintptr(unsafe.Pointer(p)))
}
