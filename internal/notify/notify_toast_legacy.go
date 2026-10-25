//go:build legacywin

package notify

// sendOS is the legacy-build notification channel: a tray balloon only.
// beeep (PowerShell/WinRT toasts) requires Go 1.21+ toolchains, which do
// not support the Windows versions this build targets; balloons work
// everywhere and render as toasts on modern Windows anyway.
func (n *Notifier) sendOS(title, body string) {
	if fn := n.balloonFn(); fn != nil {
		fn(title, body)
	}
}
