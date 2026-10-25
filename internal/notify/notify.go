// Package notify delivers system notifications and mirrors them as in-app
// toasts through the Wails event bus.
//
// The OS channel is a compatibility chain: on the standard build it tries a
// WinRT/PowerShell toast (beeep — the pretty, branded route on Windows
// 10/11) and falls back to a tray balloon whenever that fails; on the
// legacy build (Windows 7 SP1/8/8.1, `legacywin` build tag) balloons are the
// only channel — PowerShell toasts do not exist there.
package notify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	runtime "runtime"
	"sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Notifier sends OS notifications and in-app toast events.
type Notifier struct {
	ctx      context.Context
	iconPath string

	mu      sync.Mutex
	balloon func(title, body string) // tray balloon fallback (set by the app)
}

// New creates a notifier. The Wails context is attached in SetContext once
// the app starts.
func New() *Notifier {
	return &Notifier{}
}

// SetContext wires the Wails runtime context.
func (n *Notifier) SetContext(ctx context.Context) { n.ctx = ctx }

// SetIcon writes the embedded icon to disk so toasts can display it.
func (n *Notifier) SetIcon(pngBytes []byte) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return
	}
	dir = filepath.Join(dir, "Pluri-Deliver")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	p := filepath.Join(dir, "icon.png")
	if err := os.WriteFile(p, pngBytes, 0o644); err == nil {
		n.iconPath = p
	}
}

// SetBalloonFn registers the tray balloon used when the toast channel fails
// (or on the legacy build, where toasts don't exist).
func (n *Notifier) SetBalloonFn(fn func(title, body string)) {
	n.mu.Lock()
	n.balloon = fn
	n.mu.Unlock()
}

// balloonFn returns (possibly nil) the balloon fallback.
func (n *Notifier) balloonFn() func(title, body string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.balloon
}

// Notify sends a system notification (best effort) and an in-app toast event.
func (n *Notifier) Notify(kind, title, body string) {
	if title == "" {
		title = "Pluri-Deliver"
	}
	n.sendOS(title, body) // must never break the app flow

	if n.ctx != nil {
		wailsruntime.EventsEmit(n.ctx, "toast", map[string]any{
			"kind":  kind,
			"title": title,
			"body":  body,
		})
	}
}

// OpenPath reveals a folder in the OS file manager (best effort).
func OpenPath(p string) {
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("explorer", p).Start()
	default:
		_ = exec.Command("xdg-open", p).Start()
	}
}
