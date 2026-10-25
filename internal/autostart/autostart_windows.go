//go:build windows

package autostart

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// runKey is the per-user startup location Explorer processes on login.
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// setEnabled writes or deletes the Run entry. The command line is built as
// "exePath" [args] — the path is quoted so spaces survive the round-trip.
func setEnabled(on bool, exePath, args string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("cannot open startup key: %w", err)
	}
	defer k.Close()

	if !on {
		if err := k.DeleteValue(AppName); err != nil && err != registry.ErrNotExist {
			return fmt.Errorf("cannot remove startup entry: %w", err)
		}
		return nil
	}
	cmd := `"` + exePath + `"`
	if a := strings.TrimSpace(args); a != "" {
		cmd += " " + a
	}
	if err := k.SetStringValue(AppName, cmd); err != nil {
		return fmt.Errorf("cannot write startup entry: %w", err)
	}
	return nil
}
