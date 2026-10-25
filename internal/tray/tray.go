// Package tray provides a Windows notification-area (system tray) icon with
// a context menu and balloon notifications, implemented with raw Win32
// syscalls only — no cgo, no third-party libraries — so it runs on every
// Windows version Wails can target (Windows 7 SP1 through Windows 11, x64
// and x86).
//
// Balloon notifications are the compatibility layer for older Windows: they
// render as classic balloons on Windows 7/8/8.1 and as toast notifications
// on Windows 10/11, so Pluri-Deliver can notify users on every supported OS
// without PowerShell/WinRT machinery.
package tray

// Item is one entry of the tray context menu.
type Item struct {
	Label     string // display text (empty when Separator is true)
	Separator bool   // renders a horizontal rule
	Fn        func() // called (on its own goroutine) when the entry is picked
}

// Config wires the tray to the application.
type Config struct {
	IconPath string // absolute path of an .ico file shown in the tray
	Tooltip  string // hover text (max 127 characters)
	Menu     []Item // context-menu entries, in order
}
