// Package scheduler implements "smart" background polling. Every tick each
// tracking's next-check interval is decided by the Lua polling strategy
// (polling.lua — user-overridable in %APPDATA%\Pluri-Deliver\scripts\),
// which by default uses the user's PollMinutes while the window is active,
// IdlePollMinutes once it is minimized/hidden, and adapts per parcel state.
//   - manual mode: no background polling at all; refreshes happen on launch
//     and on explicit user action only.
package scheduler

import (
	"sync"
	"sync/atomic"
	"time"

	"plurideliver/internal/models"
	"plurideliver/internal/service"
)

// SettingsFn reads current settings at tick time.
type SettingsFn func() models.Settings

// Scheduler runs the background refresh loop.
type Scheduler struct {
	svc      *service.Service
	settings SettingsFn
	active   atomic.Bool
	stopped  chan struct{}
	once     sync.Once
}

// New builds a scheduler.
func New(svc *service.Service, settings SettingsFn) *Scheduler {
	return &Scheduler{svc: svc, settings: settings, stopped: make(chan struct{})}
}

// Start launches the loop and the on-launch refresh.
func (s *Scheduler) Start() {
	s.active.Store(true)
	go s.loop()
	go func() {
		time.Sleep(2 * time.Second)
		s.svc.RefreshAll()
	}()
}

// SetActive reports whether the window is focused/visible.
func (s *Scheduler) SetActive(active bool) { s.active.Store(active) }

// Stop terminates the loop.
func (s *Scheduler) Stop() {
	s.once.Do(func() { close(s.stopped) })
}

func (s *Scheduler) loop() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.stopped:
			return
		case <-t.C:
			s.tick()
		}
	}
}

func (s *Scheduler) tick() {
	if s.settings().UpdateMode != "smart" {
		return // manual mode: nothing to do
	}
	now := time.Now()
	active := s.active.Load()
	for _, tr := range s.svc.List() {
		if !tr.AutoUpdate {
			continue
		}
		if tr.NeedsCaptcha {
			continue // captcha-gated carriers refresh via the UI flow
		}
		if tr.NeedsCPF {
			continue // CPF-gated carriers refresh once the user provides the CPF
		}
		// The interval is decided per tracking by the Lua polling strategy
		// (polling.lua — user-overridable): delivered parcels relax, the
		// final mile tightens, errors back off...
		interval := time.Duration(s.svc.NextPollMinutes(tr, active)) * time.Minute
		if now.Sub(tr.LastCheck) >= interval {
			go s.svc.Refresh(tr)
		}
	}
}
