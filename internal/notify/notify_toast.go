//go:build !legacywin

package notify

import "github.com/gen2brain/beeep"

// sendOS tries the modern toast first (beeep: WinRT toasts through
// PowerShell on Windows 10/11) and falls back to a tray balloon whenever
// that fails — old Windows, locked-down machines, disabled notification
// platforms — so an update is never silently lost.
func (n *Notifier) sendOS(title, body string) {
	if err := beeep.Notify(title, body, n.iconPath); err != nil {
		if fn := n.balloonFn(); fn != nil {
			fn(title, body)
		}
	}
}
