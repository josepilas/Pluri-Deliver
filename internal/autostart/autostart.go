// Package autostart manages the "start with Windows" behaviour of
// Pluri-Deliver through the per-user Run registry key
// (HKCU\Software\Microsoft\Windows\CurrentVersion\Run). No elevation is
// required. On non-Windows platforms every call is a no-op so the settings
// toggle stays portable.
package autostart

// AppName is the registry value under the Run key.
const AppName = "Pluri-Deliver"

// HiddenArg is the command-line switch appended to the startup entry so the
// app launches quietly in the background at Windows login (no visible
// window; it can be reopened from the Start menu at any time).
const HiddenArg = "--hidden"

// SetEnabled registers (or removes) the app in the current user's startup
// entries. exePath is the running executable's full path; args carries extra
// command-line switches ("" for none).
func SetEnabled(on bool, exePath, args string) error {
	return setEnabled(on, exePath, args)
}
