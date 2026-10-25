// Package models defines the core data structures of Pluri-Deliver.
package models

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Normalized event statuses (mapped by each Lua carrier adapter).
const (
	StatusDelivered      = "delivered"
	StatusOutForDelivery = "out_for_delivery"
	StatusInTransit      = "in_transit"
	StatusPending        = "pending"
	StatusException      = "exception"
	StatusInfo           = "info"
)

// AllStatuses is the closed set of statuses understood by the UI.
var AllStatuses = []string{
	StatusDelivered, StatusOutForDelivery, StatusInTransit,
	StatusPending, StatusException, StatusInfo,
}

// TrackingEvent is a single event in a parcel's history.
type TrackingEvent struct {
	ID          string    `json:"id"`
	Date        time.Time `json:"date"`
	Status      string    `json:"status"`
	Description string    `json:"description"`
	Location    string    `json:"location,omitempty"`
	Source      string    `json:"source,omitempty"`
}

// ComputeID derives a deterministic event ID used for diffing across refreshes.
// Carrier identity is deliberately NOT part of the hash so that switching the
// data source (built-in endpoint -> aggregator) does not duplicate events.
func (e TrackingEvent) ComputeID() string {
	h := sha1.Sum([]byte(strings.ToLower(
		fmt.Sprintf("%s|%s|%s|%s",
			e.Date.UTC().Format(time.RFC3339),
			e.Status, e.Description, e.Location))))
	return hex.EncodeToString(h[:])[:12]
}

// Tracking is a saved parcel followed by the user.
type Tracking struct {
	ID           string          `json:"id"`
	Code         string          `json:"code"`
	Name         string          `json:"name"`
	Carrier      string          `json:"carrier"`
	AutoUpdate   bool            `json:"autoUpdate"`
	CreatedAt    time.Time       `json:"createdAt"`
	LastCheck    time.Time       `json:"lastCheck"`
	LastError    string          `json:"lastError,omitempty"`
	Events       []TrackingEvent `json:"events"`
	LastSeen     *time.Time      `json:"lastSeen,omitempty"`     // when the user last VIEWED this tracking (drives the badge)
	LastNotified *time.Time      `json:"lastNotified,omitempty"` // watermark of events already NOTIFIED about (drives toasts)
	NeedsCaptcha bool            `json:"needsCaptcha,omitempty"` // carrier needs a captcha verification to fetch events
	ETA          string          `json:"eta,omitempty"`          // carrier-provided delivery estimate (display only)
	Category     string          `json:"category,omitempty"`     // postal category label (e.g. "PACKET STANDARD IMPORTAÇÃO")
	RecipientCPF string          `json:"recipientCPF,omitempty"` // J&T Brasil asks for the recipient's CPF on every query
	NeedsCPF     bool            `json:"needsCPF,omitempty"`     // carrier needs the recipient CPF before events can be fetched
}

// SortEvents orders events chronologically (oldest first).
func (t *Tracking) SortEvents() {
	sort.SliceStable(t.Events, func(i, j int) bool {
		return t.Events[i].Date.Before(t.Events[j].Date)
	})
}

// LatestEvent returns the most recent event, or nil.
func (t *Tracking) LatestEvent() *TrackingEvent {
	if len(t.Events) == 0 {
		return nil
	}
	return &t.Events[len(t.Events)-1]
}

// NewestEventDate returns the date of the most recent event (nil when empty).
// Used to initialize the notification watermark after a first successful fetch.
func (t *Tracking) NewestEventDate() *time.Time {
	if len(t.Events) == 0 {
		return nil
	}
	ts := t.Events[len(t.Events)-1].Date
	return &ts
}

// Status returns the normalized status derived from the latest event.
func (t *Tracking) Status() string {
	if e := t.LatestEvent(); e != nil {
		return e.Status
	}
	return StatusPending
}

// IsDelivered reports whether the parcel reached its destination.
func (t *Tracking) IsDelivered() bool { return t.Status() == StatusDelivered }

// UnseenCount counts events newer than the user's last visit to this tracking.
func (t *Tracking) UnseenCount() int {
	if t.LastSeen == nil {
		return 0
	}
	n := 0
	for _, e := range t.Events {
		if e.Date.After(*t.LastSeen) {
			n++
		}
	}
	return n
}

// TrackingID derives the tracking ID from carrier + code (dedupe key).
func TrackingID(carrier, code string) string {
	h := sha1.Sum([]byte(strings.ToLower(carrier + "|" + strings.ToUpper(strings.TrimSpace(code)))))
	return hex.EncodeToString(h[:])[:12]
}

// CarrierInfo describes a carrier adapter (built-in, aggregator or demo).
type CarrierInfo struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Country         string `json:"country,omitempty"`
	Kind            string `json:"kind"` // "builtin" | "aggregator" | "demo"
	RequiresKey     bool   `json:"requiresKey"`
	CaptchaRequired bool   `json:"captchaRequired,omitempty"` // events only flow after the user types a captcha
	RequiresCPF     bool   `json:"requiresCpf,omitempty"`     // events need the recipient's CPF (J&T Brasil)
	Website         string `json:"website,omitempty"`
	Note            string `json:"note,omitempty"`
}

// Aggregator settings (17TRACK / AfterShip).
type Aggregator struct {
	Provider string `json:"provider"` // "none" | "17track" | "aftership"
	Key      string `json:"key"`
}

// Settings holds all user preferences, persisted as settings.json.
type Settings struct {
	Language         string     `json:"language"`        // "en" | "pt-BR"
	Theme            string     `json:"theme"`           // "dark" | "light"
	UpdateMode       string     `json:"updateMode"`      // "smart" | "manual"
	PollMinutes      int        `json:"pollMinutes"`     // when window is active (default 15)
	IdlePollMinutes  int        `json:"idlePollMinutes"` // when minimized (default 60)
	Notifications    bool       `json:"notifications"`
	StartWithWindows bool       `json:"startWithWindows"` // launch automatically at Windows login
	RunInBackground  bool       `json:"runInBackground"`  // closing the window keeps the app alive (notifications continue)
	HideTrayIcon     bool       `json:"hideTrayIcon"`     // tray icon hidden when true (absent = shown: everyone gets it by default)
	DemoMode         bool       `json:"demoMode"`
	Aggregator       Aggregator `json:"aggregator"`
}

// TrayEnabled reports whether the notification-area icon should be shown.
// The flag is stored inverted (hideTrayIcon) so users upgrading from versions
// without the field get the icon by default instead of silently losing it.
func (s Settings) TrayEnabled() bool { return !s.HideTrayIcon }

// DefaultSettings returns the out-of-the-box configuration.
func DefaultSettings() Settings {
	return Settings{
		Language:         "en",
		Theme:            "dark",
		UpdateMode:       "smart",
		PollMinutes:      15,
		IdlePollMinutes:  60,
		Notifications:    true,
		StartWithWindows: false,
		RunInBackground:  false,
		DemoMode:         false,
		Aggregator:       Aggregator{Provider: "none", Key: ""},
	}
}

// Sanitize clamps settings to valid values.
func (s *Settings) Sanitize() {
	if s.Language != "pt-BR" {
		s.Language = "en"
	}
	if s.Theme != "light" {
		s.Theme = "dark"
	}
	if s.UpdateMode != "manual" {
		s.UpdateMode = "smart"
	}
	if s.PollMinutes < 5 {
		s.PollMinutes = 5
	}
	if s.PollMinutes > 720 {
		s.PollMinutes = 720
	}
	if s.IdlePollMinutes < s.PollMinutes {
		s.IdlePollMinutes = s.PollMinutes
	}
	if s.IdlePollMinutes > 1440 {
		s.IdlePollMinutes = 1440
	}
	switch s.Aggregator.Provider {
	case "17track", "aftership":
		// ok
	default:
		s.Aggregator.Provider = "none"
	}
}

// AggregatorReady reports whether an aggregator is configured.
func (s *Settings) AggregatorReady() bool {
	return s.Aggregator.Provider != "none" && strings.TrimSpace(s.Aggregator.Key) != ""
}
