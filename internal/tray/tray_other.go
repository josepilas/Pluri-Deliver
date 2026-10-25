//go:build !windows

package tray

// Off-Windows the tray is inert: the app is only shipped for Windows, but
// the package must compile everywhere so tests and vet keep running.

// Tray is a no-op stub.
type Tray struct{}

// Start never starts anything off-Windows.
func Start(cfg Config) (*Tray, error) { return &Tray{}, nil }

// SetVisible is a no-op.
func (t *Tray) SetVisible(bool) {}

// SetTooltip is a no-op.
func (t *Tray) SetTooltip(string) {}

// Balloon is a no-op.
func (t *Tray) Balloon(title, text string) {}

// Stop is a no-op.
func (t *Tray) Stop() {}

// SetAppUserModelID is a no-op.
func SetAppUserModelID() {}
