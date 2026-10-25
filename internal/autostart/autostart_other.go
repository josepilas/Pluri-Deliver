//go:build !windows

package autostart

// setEnabled is a no-op outside Windows — the toggle is Windows-specific and
// simply persists its state everywhere else.
func setEnabled(bool, string, string) error { return nil }
