// Package service implements the tracking lifecycle: refresh via the Lua
// engine, event diffing, persistence and notifications. Correios trackings
// go through the dedicated captcha-verified client (internal/correios).
package service

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"plurideliver/internal/cainiao"
	"plurideliver/internal/correios"
	"plurideliver/internal/engine"
	"plurideliver/internal/i18n"
	"plurideliver/internal/jt"
	"plurideliver/internal/models"
	"plurideliver/internal/spx"
	"plurideliver/internal/store"
)

// Notifier abstracts user-facing notifications (toasts + in-app).
type Notifier interface {
	Notify(kind, title, body string)
}

// Emitter broadcasts Wails runtime events to the frontend.
type Emitter func(name string, data ...any)

// Service coordinates store + engine + notifications.
type Service struct {
	store    *store.Store
	engine   *engine.Engine
	notifier Notifier
	emit     Emitter
	now      func() time.Time

	correios *correios.Client
	cainiao  *cainiao.Client
	spx      *spx.Client
	jt       *jt.Client

	mu        sync.Mutex // serializes refreshes per tracking
	refreshMu map[string]bool
	retries   map[string]bool // QUEUED follow-ups already scheduled
}

// New builds a Service.
func New(st *store.Store, eng *engine.Engine, n Notifier, emit Emitter) *Service {
	return &Service{
		store:     st,
		engine:    eng,
		notifier:  n,
		emit:      emit,
		now:       time.Now,
		correios:  correios.New(),
		cainiao:   cainiao.New(),
		spx:       spx.New(),
		jt:        jt.New(),
		refreshMu: map[string]bool{},
		retries:   map[string]bool{},
	}
}

// List returns all trackings.
func (s *Service) List() []*models.Tracking { return s.store.All() }

// Add creates a tracking and kicks off an async first refresh.
// cpf is the recipient's CPF (J&T Brasil requires it; stored as digits).
func (s *Service) Add(code, name, carrierID string, autoUpdate bool, cpf string) (*models.Tracking, string) {
	code = strings.ToUpper(strings.TrimSpace(code))
	name = strings.TrimSpace(name)
	cpf = jt.Digits(cpf)
	if code == "" {
		return nil, "empty_code"
	}
	c := s.engine.Get(carrierID)
	if c == nil {
		return nil, "unknown_carrier"
	}
	if c.Info.RequiresCPF && cpf != "" && !jt.ValidCPF(cpf) {
		return nil, "invalid_cpf"
	}
	id := models.TrackingID(carrierID, code)
	s.mu.Lock()
	if existing, ok := s.store.Get(id); ok {
		s.mu.Unlock()
		_ = existing
		return nil, "duplicate"
	}
	t := &models.Tracking{
		ID:         id,
		Code:       code,
		Name:       name,
		Carrier:    carrierID,
		AutoUpdate: autoUpdate,
		CreatedAt:  s.now().UTC(),
		// captcha-gated carriers start in "awaiting verification" state
		NeedsCaptcha: c.Info.CaptchaRequired,
		// CPF-gated carriers start in "awaiting CPF" state unless one was given
		RecipientCPF: cpf,
		NeedsCPF:     c.Info.RequiresCPF && cpf == "",
	}
	if err := s.store.Upsert(t); err != nil {
		s.mu.Unlock()
		return nil, "storage_error"
	}
	s.mu.Unlock()

	cp, _ := s.store.Get(id)
	if !t.NeedsCaptcha && !t.NeedsCPF {
		go func() { _ = s.Refresh(cp) }()
	}
	return cp, ""
}

// Remove deletes a tracking.
func (s *Service) Remove(id string) error {
	if err := s.store.Delete(id); err != nil {
		return err
	}
	s.trackingsChanged()
	return nil
}

// Rename updates the display name.
func (s *Service) Rename(id, name string) (*models.Tracking, error) {
	t, ok := s.store.Get(id)
	if !ok {
		return nil, fmt.Errorf("tracking not found")
	}
	t.Name = strings.TrimSpace(name)
	if err := s.store.Upsert(t); err != nil {
		return nil, err
	}
	s.trackingsChanged()
	return t, nil
}

// SetAutoUpdate toggles background updates for one tracking.
func (s *Service) SetAutoUpdate(id string, on bool) (*models.Tracking, error) {
	t, ok := s.store.Get(id)
	if !ok {
		return nil, fmt.Errorf("tracking not found")
	}
	t.AutoUpdate = on
	if err := s.store.Upsert(t); err != nil {
		return nil, err
	}
	s.trackingsChanged()
	return t, nil
}

// MarkSeen records the user's last visit time for unseen-badge computation.
func (s *Service) MarkSeen(id string) (*models.Tracking, error) {
	t, ok := s.store.Get(id)
	if !ok {
		return nil, fmt.Errorf("tracking not found")
	}
	now := s.now().UTC()
	t.LastSeen = &now
	if err := s.store.Upsert(t); err != nil {
		return nil, err
	}
	return t, nil
}

// retryScheduled reserves (once) a QUEUED follow-up for a tracking so multiple
// failures cannot stack timers for the same id.
func (s *Service) retryScheduled(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retries[id] {
		return true
	}
	s.retries[id] = true
	return false
}

// clearRetry frees the follow-up slot once events have arrived or the retry ran.
func (s *Service) clearRetry(id string) {
	s.mu.Lock()
	delete(s.retries, id)
	s.mu.Unlock()
}

// Refresh fetches new events for one tracking, persists them and notifies.
// Direct native clients run first (SPX keyless, J&T CPF-signed); when they
// fail the Lua engine + its fallback chain takes over.
func (s *Service) Refresh(t *models.Tracking) error {
	id := t.ID
	s.mu.Lock()
	if s.refreshMu[id] {
		s.mu.Unlock()
		return nil // a refresh is already in flight
	}
	s.refreshMu[id] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.refreshMu, id)
		s.mu.Unlock()
	}()

	cur, ok := s.store.Get(id)
	if !ok {
		return fmt.Errorf("tracking not found")
	}

	var notifyList []models.TrackingEvent

	settings := s.store.Settings()

	// ---- direct native clients -------------------------------------------
	if c := s.engine.Get(cur.Carrier); c != nil {
		// Correios: fully automatic, no captcha — Cainiao's global network
		// carries the complete China→Brazil import history (localized in
		// Correios' own wording); numbers Cainiao doesn't know fall through
		// to the 17TRACK web engine chain below.
		if c.Info.ID == "correios" && !settings.DemoMode {
			lang := "en"
			if strings.HasPrefix(settings.Language, "pt") {
				lang = "pt-BR"
			}
			if res, err := s.cainiao.Track(cur.Code, lang); err == nil {
				return s.applyDirectResult(cur, res.Events, "", "")
			} else if !errors.Is(err, cainiao.ErrNotFound) {
				cur.LastError = err.Error()
			}
		}
		// SPX Express Brasil: keyless direct portal query.
		if c.Info.ID == "spx" && !settings.DemoMode {
			if res, err := s.spx.Track(cur.Code); err == nil {
				return s.applyDirectResult(cur, res.Events, res.ETA, "")
			} else if !errors.Is(err, spx.ErrEmptyResult) {
				cur.LastError = err.Error()
			}
		}
		// J&T Brasil: the API needs the recipient's CPF on every query.
		if c.Info.RequiresCPF && !settings.DemoMode {
			if strings.TrimSpace(cur.RecipientCPF) == "" {
				if len(cur.Events) == 0 {
					cur.NeedsCPF = true
				}
				if err := s.store.Upsert(cur); err != nil {
					return err
				}
				s.trackingsChanged()
				return nil
			}
			if res, err := s.jt.Track(cur.Code, cur.RecipientCPF); err == nil {
				return s.applyDirectResult(cur, res.Events, res.ETA, "")
			} else if !errors.Is(err, jt.ErrNotFound) {
				cur.LastError = err.Error()
			}
		}
	}

	events, err := s.engine.Track(cur, settings)
	cur.LastCheck = s.now().UTC()

	// Captcha-gated carriers (Correios): not an error — the UI asks the user
	// to type the code once. Only freshly added, still-empty trackings carry
	// the "needs verification" badge.
	if errors.Is(err, engine.ErrCaptchaRequired) {
		if len(cur.Events) == 0 {
			cur.NeedsCaptcha = true
		}
		if upErr := s.store.Upsert(cur); upErr != nil {
			return upErr
		}
		s.trackingsChanged()
		return nil
	}

	// CPF-gated carriers (J&T): the UI asks for the CPF once per tracking.
	if errors.Is(err, engine.ErrCPFRequired) {
		if len(cur.Events) == 0 {
			cur.NeedsCPF = true
		}
		if upErr := s.store.Upsert(cur); upErr != nil {
			return upErr
		}
		s.trackingsChanged()
		return nil
	}

	if err != nil {
		// Sedex/domestic Correios numbers that neither Cainiao nor the
		// 17TRACK web engine could resolve are offered the official
		// portal's captcha flow: the UI shows a "needs verification"
		// pill instead of a dead-end error (v1.2 flow, kept as the
		// last resort of the automatic chain). QUEUED results keep
		// the auto-retry path — fresh numbers deserve the chance to
		// resolve on their own first.
		if cur.Carrier == "correios" && len(cur.Events) == 0 &&
			!strings.HasPrefix(err.Error(), "QUEUED:") {
			cur.NeedsCaptcha = true
			cur.LastError = ""
			cur.LastCheck = s.now().UTC()
			if upErr := s.store.Upsert(cur); upErr != nil {
				return upErr
			}
			s.trackingsChanged()
			return nil
		}
		cur.LastError = err.Error()
	} else {
		cur.LastError = ""
		cur.NeedsCPF = false
	}

	// Freshly added numbers come back as "queued" from the universal web
	// engine (the aggregator is looking the number up on its side). Give it
	// another shot shortly so the first events land without waiting a full
	// scheduler interval.
	if err == nil {
		s.clearRetry(id)
	} else if strings.HasPrefix(err.Error(), "QUEUED:") {
		if len(cur.Events) == 0 && !s.retryScheduled(id) {
			cp := *cur
			time.AfterFunc(90*time.Second, func() {
				s.clearRetry(id)
				_ = s.Refresh(&cp)
			})
		}
	}

	if err == nil {
		s.clearRetry(id)
		added := s.mergeEvents(cur, events)
		notifyList = s.markNotified(cur, added)
	}
	if err := s.store.Upsert(cur); err != nil {
		return err
	}
	s.trackingsChanged()

	if len(notifyList) > 0 {
		settings := s.store.Settings()
		if settings.Notifications {
			s.notifyEvents(cur, notifyList)
		}
	}
	return nil
}

// mergeEvents appends unseen events (by ID) and keeps the timeline sorted.
// It returns the events that were actually added (the truly-new ones).
func (s *Service) mergeEvents(t *models.Tracking, events []models.TrackingEvent) []models.TrackingEvent {
	seen := map[string]bool{}
	for _, e := range t.Events {
		seen[e.ID] = true
	}
	var added []models.TrackingEvent
	for _, e := range events {
		if !seen[e.ID] {
			seen[e.ID] = true
			t.Events = append(t.Events, e)
			added = append(added, e)
		}
	}
	if len(added) > 0 {
		t.SortEvents()
	}
	return added
}

// ---- Correios captcha flows -------------------------------------------------

// SetTrackingCPF stores (or updates) the recipient CPF of a tracking and
// triggers a refresh — the gateway to J&T Brasil's CPF-gated API.
// Error codes: "invalid_cpf", "not_found_tracking".
func (s *Service) SetTrackingCPF(id, cpf string) (*models.Tracking, string) {
	cur, ok := s.store.Get(id)
	if !ok {
		return nil, "not_found_tracking"
	}
	digits := jt.Digits(cpf)
	if digits == "" || !jt.ValidCPF(digits) {
		return nil, "invalid_cpf"
	}
	cur.RecipientCPF = digits
	cur.NeedsCPF = false
	if err := s.store.Upsert(cur); err != nil {
		return nil, "storage_error"
	}
	s.trackingsChanged()
	out, _ := s.store.Get(id)
	if out != nil {
		cp := *out
		go func() { _ = s.Refresh(&cp) }()
		return out, ""
	}
	return cur, ""
}

// applyDirectResult merges events fetched by a native Go client into a
// tracking and persists it (Cainiao-Correios / SPX / J&T path). Events are
// merged FIRST; only the ones that are genuinely new (and newer than the
// notification watermark) trigger a toast — after the state is persisted.
func (s *Service) applyDirectResult(t *models.Tracking, events []models.TrackingEvent, eta, category string) error {
	t.LastCheck = s.now().UTC()
	t.LastError = ""
	t.NeedsCPF = false
	if eta != "" {
		t.ETA = eta
	}
	if category != "" {
		t.Category = category
	}
	added := s.mergeEvents(t, events)
	notifyList := s.markNotified(t, added)
	if err := s.store.Upsert(t); err != nil {
		return err
	}
	s.trackingsChanged()
	if len(notifyList) > 0 && s.store.Settings().Notifications {
		s.notifyEvents(t, notifyList)
	}
	return nil
}

// CaptchaImage returns a fresh captcha PNG from the Correios portal. The
// session cookies stay inside the client, so the next Verify/Sync call
// consumes exactly this image.
func (s *Service) CaptchaImage() ([]byte, error) {
	return s.correios.CaptchaImage()
}

// VerifyCaptcha types the user's answer on the portal and, when accepted,
// pulls the FULL timeline for one tracking.
// Error codes: "captcha_invalid", "not_found", "no_events", "network".
func (s *Service) VerifyCaptcha(id, captcha string) (*models.Tracking, string) {
	cur, ok := s.store.Get(id)
	if !ok {
		return nil, "not_found_tracking"
	}
	res, err := s.correios.Track(cur.Code, captcha)
	if err != nil {
		return nil, captchaErrorCode(err)
	}
	return s.applyCorreiosResult(cur, res.Events, res.ETA, res.Category), ""
}

// SyncAllCorreios uses ONE captcha to refresh up to 20 Correios trackings at
// once via the portal's batch endpoint (latest event per code).
// Error codes: "captcha_invalid", "network".
func (s *Service) SyncAllCorreios(captcha string) (updated int, failedCodes map[string]string, errorCode string) {
	failedCodes = map[string]string{}
	var codes []string
	for _, t := range s.store.All() {
		// The correios carrier keeps captcha_required=false since the
		// Cainiao integration (v1.4) — select by carrier id instead.
		if t.Carrier == "correios" || s.needsCaptchaCarrier(t.Carrier) {
			codes = append(codes, t.Code)
		}
	}
	if len(codes) == 0 {
		return 0, failedCodes, ""
	}
	res, err := s.correios.TrackMulti(codes, captcha)
	if err != nil {
		return 0, failedCodes, captchaErrorCode(err)
	}
	for _, t := range s.store.All() {
		if t.Carrier != "correios" && !s.needsCaptchaCarrier(t.Carrier) {
			continue
		}
		ev, ok := res.Latest[correios.CleanCode(t.Code)]
		if !ok {
			if msg, hasErr := res.Errors[correios.CleanCode(t.Code)]; hasErr {
				failedCodes[t.Code] = msg
			}
			continue
		}
		eta := res.ETA[correios.CleanCode(t.Code)]
		s.applyCorreiosResult(t, []models.TrackingEvent{ev}, eta, "")
		updated++
	}
	return updated, failedCodes, ""
}

// needsCaptchaCarrier reports whether a carrier's adapter explicitly
// declares captcha_required (the correios carrier itself is always included
// by id — see SyncAllCorreios).
func (s *Service) needsCaptchaCarrier(carrier string) bool {
	c := s.engine.Get(carrier)
	return c != nil && c.Info.CaptchaRequired
}

// applyCorreiosResult merges fetched events into a tracking and persists it.
func (s *Service) applyCorreiosResult(t *models.Tracking, events []models.TrackingEvent, eta, category string) *models.Tracking {
	t.LastCheck = s.now().UTC()
	t.LastError = ""
	t.NeedsCaptcha = false
	if eta != "" {
		t.ETA = eta
	}
	if category != "" {
		t.Category = category
	}
	added := s.mergeEvents(t, events)
	notifyList := s.markNotified(t, added)
	if err := s.store.Upsert(t); err != nil {
		log.Printf("service: persisting Correios result for %s failed: %v", t.Code, err)
	}
	s.trackingsChanged()
	out, _ := s.store.Get(t.ID)
	if out == nil {
		return t
	}
	if len(notifyList) > 0 && s.store.Settings().Notifications {
		s.notifyEvents(out, notifyList)
	}
	return out
}

func captchaErrorCode(err error) string {
	switch {
	case errors.Is(err, correios.ErrCaptchaInvalid):
		return "captcha_invalid"
	case errors.Is(err, correios.ErrNotFound):
		return "not_found"
	case errors.Is(err, correios.ErrEmptyResult):
		return "no_events"
	default:
		return "network"
	}
}

// RefreshAll refreshes every tracking concurrently (bounded). Captcha-gated
// carriers are skipped — they refresh through the captcha flow. CPF-gated
// carriers without a stored CPF are skipped too — they refresh once the
// user provides it.
func (s *Service) RefreshAll() {
	list := s.store.All()
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, t := range list {
		wg.Add(1)
		go func(t *models.Tracking) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if t.NeedsCaptcha || t.NeedsCPF {
				return // awaits the user's verification
			}
			_ = s.Refresh(t)
		}(t)
	}
	wg.Wait()
	s.emit("refresh:done", map[string]any{"count": len(list)})
}

// CaptchaGatedCodes lists codes that need a captcha verification right now
// (either never verified or the user asked to sync them all).
func (s *Service) CaptchaGatedCodes() []string {
	var out []string
	for _, t := range s.store.All() {
		if c := s.engine.Get(t.Carrier); c != nil && c.Info.CaptchaRequired {
			out = append(out, t.Code)
		}
	}
	sort.Strings(out)
	return out
}

// markNotified advances the notification watermark of a tracking and returns
// the events that should be toasted. Notification state is tracked SEPARATELY
// from the user's "viewed" marker (LastSeen):
//
//   - the first fetch that populates a tracking only sets the watermark (the
//     user just added the code — replaying its whole history as toasts would
//     be noise);
//   - afterwards, only events newer than the watermark are returned, and the
//     watermark moves to the newest event so a notification is never sent
//     twice for the same occurrence — even across restarts;
//   - events older than the watermark (late backfills) are merged but stay
//     silent.
func (s *Service) markNotified(t *models.Tracking, added []models.TrackingEvent) []models.TrackingEvent {
	if len(added) == 0 {
		return nil
	}
	if t.LastNotified == nil {
		// First population: swallow the initial history silently but
		// remember it, so enabling notifications later never replays it.
		newest := t.NewestEventDate()
		if newest != nil {
			t.LastNotified = newest
		}
		return nil
	}
	var fresh []models.TrackingEvent
	for _, e := range added {
		if e.Date.After(*t.LastNotified) {
			fresh = append(fresh, e)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	sort.Slice(fresh, func(i, j int) bool { return fresh[i].Date.Before(fresh[j].Date) })
	newest := fresh[len(fresh)-1].Date
	if newest.Before(*t.LastNotified) {
		newest = *t.LastNotified
	}
	t.LastNotified = &newest
	return fresh
}

// notifyEvents sends the system notifications (+ in-app mirror) for
// genuinely new events. fresh is expected chronologically sorted (last =
// most recent). Formatting decisions live in the Lua notification script
// (notify.lua — user-overridable); if that script fails, the built-in Go
// formatting takes over so a broken user script can never silence updates.
func (s *Service) notifyEvents(t *models.Tracking, fresh []models.TrackingEvent) {
	if len(fresh) == 0 {
		return
	}
	lang := s.store.Settings().Language
	notes, err := s.engine.RunNotify(engine.NotifyInfo{
		Code:            t.Code,
		Name:            t.Name,
		Carrier:         t.Carrier,
		Language:        lang,
		Status:          t.Status(),
		Delivered:       t.Status() == models.StatusDelivered,
		Events:          fresh,
		PlusMorePattern: i18n.T(lang, "plus_more_events"),
		DeliveredBody:   i18n.T(lang, "delivered_body"),
	})
	if err != nil {
		notes = fallbackNotifications(t, fresh, lang)
	}
	for _, n := range notes {
		s.notifier.Notify(n.Kind, n.Title, n.Body)
	}
}

// fallbackNotifications is the deterministic Go formatting used when the
// Lua notification script is broken or hostile (mirrors notify.lua's default).
func fallbackNotifications(t *models.Tracking, fresh []models.TrackingEvent, lang string) []engine.Notification {
	title := t.Name
	if title == "" {
		title = t.Code
	}
	latest := fresh[len(fresh)-1]
	body := latest.Description
	if latest.Location != "" {
		body += " · " + latest.Location
	}
	if len(fresh) > 1 {
		body = fmt.Sprintf(i18n.T(lang, "plus_more_events"), body, len(fresh)-1)
	}
	notes := []engine.Notification{{Kind: "update", Title: title, Body: body}}
	if t.Status() == models.StatusDelivered {
		notes = append(notes, engine.Notification{
			Kind:  "delivered",
			Title: title,
			Body:  i18n.T(lang, "delivered_body"),
		})
	}
	return notes
}

// NextPollMinutes returns how many minutes should pass before this
// tracking is refreshed again, decided by the Lua polling strategy
// (polling.lua — user-overridable). Any script failure falls back to the
// built-in cadence (the user's interval settings).
func (s *Service) NextPollMinutes(t *models.Tracking, active bool) int {
	st := s.store.Settings()
	fallback := func() int {
		m := st.PollMinutes
		if !active && st.IdlePollMinutes > st.PollMinutes {
			m = st.IdlePollMinutes
		}
		return m
	}
	info := engine.PollingInfo{
		Code:            t.Code,
		Carrier:         t.Carrier,
		Name:            t.Name,
		Status:          t.Status(),
		Delivered:       t.Status() == models.StatusDelivered,
		AutoUpdate:      t.AutoUpdate,
		Active:          active,
		HasError:        t.LastError != "",
		EventCount:      len(t.Events),
		PollMinutes:     st.PollMinutes,
		IdlePollMinutes: st.IdlePollMinutes,
	}
	if e := t.LatestEvent(); e != nil {
		info.LastEventAgeHours = s.now().Sub(e.Date).Hours()
	} else {
		info.LastEventAgeHours = -1
	}
	mins, err := s.engine.RunPolling(info)
	if err != nil {
		log.Printf("service: polling strategy failed (%v) — using default interval", err)
		return fallback()
	}
	return mins
}

func (s *Service) trackingsChanged() {
	s.emit("trackings:changed", nil)
}
