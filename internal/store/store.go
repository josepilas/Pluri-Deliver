// Package store persists trackings and settings as JSON files inside the
// user's config directory (%APPDATA%\Pluri-Deliver on Windows).
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"plurideliver/internal/models"
)

// ErrTrackingNotFound is returned by Delete when the id is unknown.
var ErrTrackingNotFound = errors.New("tracking not found")

// Store is a concurrency-safe JSON-backed persistence layer.
// Writes are atomic (temp file + rename) and happen on every mutation.
type Store struct {
	mu          sync.RWMutex
	dir         string
	trackings   map[string]*models.Tracking
	order       []string
	settings    models.Settings
	carriersDir string
	scriptsDir  string
}

// New opens (or creates) the store in the OS config directory.
func New() (*Store, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("cannot resolve config dir: %w", err)
	}
	return NewAt(filepath.Join(base, "Pluri-Deliver"))
}

// NewAt opens (or creates) the store at an explicit directory (used by tests).
func NewAt(dir string) (*Store, error) {
	s := &Store{
		dir:       dir,
		trackings: map[string]*models.Tracking{},
		settings:  models.DefaultSettings(),
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s.carriersDir = filepath.Join(dir, "carriers")
	if err := os.MkdirAll(s.carriersDir, 0o755); err != nil {
		return nil, err
	}
	ensureCarriersReadme(s.carriersDir)
	s.scriptsDir = filepath.Join(dir, "scripts")
	if err := os.MkdirAll(s.scriptsDir, 0o755); err != nil {
		return nil, err
	}
	ensureScriptsReadme(s.scriptsDir)
	s.loadTrackings()
	s.loadSettings()
	return s, nil
}

// Dir returns the storage directory.
func (s *Store) Dir() string { return s.dir }

// CarriersDir returns the folder where user-provided Lua adapters live.
func (s *Store) CarriersDir() string { return s.carriersDir }

// ScriptsDir returns the folder with the user's copies of the Lua policy
// scripts (polling.lua / notify.lua).
func (s *Store) ScriptsDir() string { return s.scriptsDir }

func ensureCarriersReadme(dir string) {
	p := filepath.Join(dir, "README.txt")
	if _, err := os.Stat(p); err == nil {
		return
	}
	content := `Pluri-Deliver - custom carrier adapters
=======================================

Drop .lua files in this folder to add carriers or override built-in ones.
Each script must return a table like:

    local M = {
      id = "mycarrier",
      name = "My Carrier",
      country = "BR",
      kind = "builtin",       -- builtin | aggregator | demo
      requires_key = false,
    }

    function M.detect(code)
      return code:upper():match("^MY%d+$") ~= nil
    end

    -- ctx.http.get(url, headers) -> {status, body}
    -- ctx.http.post(url, body, headers) -> {status, body}
    -- ctx.json.decode(str) / ctx.json.encode(value)
    -- ctx.now() -> unix seconds; ctx.iso(sec) -> RFC3339 string
    -- ctx.hash(str) -> deterministic 0..1 float
    -- ctx.hint -> carrier id hint when routed via aggregator
    function M.track(ctx, code)
      local r = ctx.http.get("https://api.example.com/track?code=" .. code, {})
      if r.status ~= 200 then return nil, "HTTP " .. r.status end
      local data = ctx.json.decode(r.body)
      local events = {}
      for _, e in ipairs(data.events or {}) do
        table.insert(events, {
          date = e.date,            -- RFC3339 or dd/mm/yyyy hh:mm
          status = "in_transit",   -- delivered|out_for_delivery|in_transit|pending|exception|info
          description = e.message,
          location = e.city,
        })
      end
      return events
    end

    -- Optional demo generator (used in Demo Mode):
    -- function M.demo(ctx, code) ... return events end

    return M

Changes are picked up on the next app start. Scripts run in a sandbox
(no os / io / require) and only get the ctx bridges listed above.
Sandbox rules: every script call is time-limited; custom scripts are
UNTRUSTED — they never see the aggregator API key (ctx.settings.aggregator.key
is always empty for them) and cannot reach localhost / link-local addresses.
`
	_ = os.WriteFile(p, []byte(content), 0o644)
}

// ensureScriptsReadme drops the reference for the user-scriptable Lua hooks.
func ensureScriptsReadme(dir string) {
	p := filepath.Join(dir, "README.txt")
	if _, err := os.Stat(p); err == nil {
		return
	}
	content := `Pluri-Deliver - Lua policy scripts
===================================

This folder customizes how Pluri-Deliver THINKS. The app runs two Lua
scripts every cycle; dropping a file with the same name here replaces the
built-in version on the next app start.

polling.lua   HOW OFTEN to refresh each parcel.
              decide(info) -> minutes until the next check.
              info: code, carrier, name, status, delivered, auto_update,
              active (window visible), event_count, last_event_age_hours,
              has_error (last refresh failed), poll_minutes,
              idle_poll_minutes (the user's interval settings).

notify.lua    WHICH notifications to send for new events.
              build(info) -> { {kind, title, body}, ... }
              info: code, name, carrier, language, status, delivered,
              events (the new ones, oldest first: {status, description,
              location, date, source}),
              t.plus_more(body, n)  i18n helper "(+n more events)"
              t.delivered_body     localized "delivered!" message

Example polling.lua that only checks parcels twice a day:

    local function decide(i)
      if i.delivered then return 720 end
      return 720
    end
    return { decide = decide }

Example notify.lua that only celebrates deliveries (silences the rest):

    local function build(i)
      if not i.delivered then return {} end
      return { { kind = "delivered", title = i.name ~= "" and i.name or i.code,
                 body = i.t.delivered_body } }
    end
    return { build = build }

Scripts run in the same sandbox as carrier adapters: time-limited, no
os/io/require, no network. A broken script never crashes the app - the
built-in behavior is used instead until you fix it.
`
	_ = os.WriteFile(p, []byte(content), 0o644)
}

func (s *Store) trackingsPath() string { return filepath.Join(s.dir, "trackings.json") }
func (s *Store) settingsPath() string  { return filepath.Join(s.dir, "settings.json") }

func (s *Store) loadTrackings() {
	b, err := os.ReadFile(s.trackingsPath())
	if err != nil {
		return
	}
	var list []models.Tracking
	if err := json.Unmarshal(b, &list); err != nil {
		return // corrupted file: start fresh rather than crash
	}
	for i := range list {
		t := &list[i]
		migrateTracking(t)
		t.SortEvents()
		// v1.5.0 backfill: trackings written before the notification watermark
		// existed treat their already-known history as notified, so the first
		// genuinely new event after the upgrade still fires a toast.
		if t.LastNotified == nil && len(t.Events) > 0 {
			ts := t.Events[len(t.Events)-1].Date
			t.LastNotified = &ts
		}
		s.trackings[t.ID] = t
		s.order = append(s.order, t.ID)
	}
}

// migrateTracking upgrades trackings written by older versions:
//
//   - v1.4.0 retired the captcha-verified Correios flow (automatic keyless
//     sources replaced it), so the stale "needs verification" flag is cleared
//     and events captured through the old portal client (Source "Correios")
//     are dropped once — their IDs are source-specific and would otherwise
//     duplicate the automatic timeline, which rebuilds itself on the first
//     refresh. Events from other sources (Cainiao, 17TRACK, ...) survive.
func migrateTracking(t *models.Tracking) {
	if t.NeedsCaptcha {
		t.NeedsCaptcha = false
	}
	if t.Carrier == "correios" && len(t.Events) > 0 {
		kept := t.Events[:0]
		for _, e := range t.Events {
			if e.Source != "Correios" {
				kept = append(kept, e)
			}
		}
		t.Events = kept
	}
}

func (s *Store) loadSettings() {
	b, err := os.ReadFile(s.settingsPath())
	if err != nil {
		return
	}
	var st models.Settings
	if err := json.Unmarshal(b, &st); err != nil {
		return
	}
	st.Sanitize()
	s.settings = st
}

func writeFileAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// All returns all trackings sorted by creation time (newest first).
func (s *Store) All() []*models.Tracking {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*models.Tracking, 0, len(s.order))
	for i := len(s.order) - 1; i >= 0; i-- {
		if t, ok := s.trackings[s.order[i]]; ok {
			out = append(out, cloneTracking(t))
		}
	}
	return out
}

// Get returns a deep copy of a tracking.
func (s *Store) Get(id string) (*models.Tracking, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.trackings[id]
	if !ok {
		return nil, false
	}
	return cloneTracking(t), true
}

// Upsert stores a tracking (id must already be set) and persists it.
// The change is prepared on a staged copy and written to disk BEFORE the
// in-memory state is touched: if the write fails, the Store keeps its
// previous contents instead of drifting out of sync with the JSON file.
func (s *Store) Upsert(t *models.Tracking) error {
	cp := cloneTracking(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	nextTrackings := make(map[string]*models.Tracking, len(s.trackings)+1)
	for k, v := range s.trackings {
		nextTrackings[k] = v
	}
	nextOrder := append([]string(nil), s.order...)
	if _, exists := nextTrackings[cp.ID]; !exists {
		nextOrder = append(nextOrder, cp.ID)
	}
	nextTrackings[cp.ID] = cp
	if err := writeFileAtomic(s.trackingsPath(), snapshotFrom(nextOrder, nextTrackings)); err != nil {
		return err // memory unchanged: disk and state stay consistent
	}
	s.trackings = nextTrackings
	s.order = nextOrder
	return nil
}

// Delete removes a tracking and persists the change. It reports both a
// missing id (ErrTrackingNotFound) and any persistence failure, and leaves
// the in-memory state untouched when the write fails.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.trackings[id]; !ok {
		return ErrTrackingNotFound
	}
	nextTrackings := make(map[string]*models.Tracking, len(s.trackings))
	for k, v := range s.trackings {
		if k != id {
			nextTrackings[k] = v
		}
	}
	nextOrder := make([]string, 0, len(s.order))
	for _, x := range s.order {
		if x != id {
			nextOrder = append(nextOrder, x)
		}
	}
	if err := writeFileAtomic(s.trackingsPath(), snapshotFrom(nextOrder, nextTrackings)); err != nil {
		return err // memory unchanged: the tracking stays on disk and in state
	}
	s.trackings = nextTrackings
	s.order = nextOrder
	return nil
}

// snapshotFrom serializes the given order/map pair (helper for the staged
// writes above; the receiver's own state is deliberately ignored).
func snapshotFrom(order []string, trackings map[string]*models.Tracking) []*models.Tracking {
	out := make([]*models.Tracking, 0, len(order))
	for _, id := range order {
		if t, ok := trackings[id]; ok {
			out = append(out, t)
		}
	}
	return out
}

// Settings returns the current settings.
func (s *Store) Settings() models.Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

// SaveSettings persists new settings. Like Upsert, it stages the value and
// only swaps the in-memory copy after a successful disk write.
func (s *Store) SaveSettings(st models.Settings) error {
	st.Sanitize()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeFileAtomic(s.settingsPath(), st); err != nil {
		return err // keep the previously saved settings
	}
	s.settings = st
	return nil
}

func cloneTracking(t *models.Tracking) *models.Tracking {
	cp := *t
	cp.Events = append([]models.TrackingEvent(nil), t.Events...)
	if t.LastSeen != nil {
		ls := *t.LastSeen
		cp.LastSeen = &ls
	}
	if t.LastNotified != nil {
		ln := *t.LastNotified
		cp.LastNotified = &ln
	}
	return &cp
}

// Now is exposed for tests that need to fake time.
var Now = time.Now
