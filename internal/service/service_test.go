package service

import (
	"strings"
	"testing"
	"time"

	"plurideliver/internal/engine"
	"plurideliver/internal/models"
	"plurideliver/internal/store"
)

// fakeNotifier records every toast the service would have sent.
type fakeNotifier struct {
	sent []string // "kind|title|body" per call
}

func (f *fakeNotifier) Notify(kind, title, body string) {
	f.sent = append(f.sent, kind+"|"+title+"|"+body)
}

func (f *fakeNotifier) count() int { return len(f.sent) }

// last returns the most recent toast body ("" when none).
func (f *fakeNotifier) last() string {
	if len(f.sent) == 0 {
		return ""
	}
	return f.sent[len(f.sent)-1]
}

func newTestService(t *testing.T) (*Service, *fakeNotifier) {
	t.Helper()
	st, err := store.NewAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fn := &fakeNotifier{}
	return New(st, engine.New("", ""), fn, func(name string, data ...any) {}), fn
}

func evAt(offset time.Duration, status, desc, loc string) models.TrackingEvent {
	e := models.TrackingEvent{
		Date:        time.Now().UTC().Add(-offset),
		Status:      status,
		Description: desc,
		Location:    loc,
		Source:      "test",
	}
	e.ID = e.ComputeID()
	return e
}

// newTracked stores a bare tracking and returns the store's copy.
func newTracked(t *testing.T, s *Service, id string) *models.Tracking {
	t.Helper()
	tr := &models.Tracking{
		ID:        id,
		Code:      strings.ToUpper(id),
		Carrier:   "dhl",
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.Upsert(tr); err != nil {
		t.Fatal(err)
	}
	out, _ := s.store.Get(id)
	return out
}

// TestFirstPopulationIsSilent pins the intended noise-free onboarding: when a
// freshly added code is fetched for the first time, its whole history merges
// but no toast fires — the watermark just moves to the newest event.
func TestFirstPopulationIsSilent(t *testing.T) {
	s, fn := newTestService(t)
	tr := newTracked(t, s, "firstpop")

	events := []models.TrackingEvent{
		evAt(3*time.Hour, models.StatusPending, "Shipment information received", "Leipzig"),
		evAt(2*time.Hour, models.StatusInTransit, "Processed at facility", "Leipzig"),
	}
	if err := s.applyDirectResult(tr, events, "", ""); err != nil {
		t.Fatal(err)
	}
	if fn.count() != 0 {
		t.Fatalf("first population should not notify, got %d toasts (%v)", fn.count(), fn.sent)
	}
	got, _ := s.store.Get("firstpop")
	if len(got.Events) != 2 {
		t.Fatalf("events were not merged: %+v", got.Events)
	}
	if got.LastNotified == nil || !got.LastNotified.Equal(events[1].Date) {
		t.Fatalf("watermark not initialized to newest event: %+v", got.LastNotified)
	}
}

// TestOnlyGenuinelyNewEventsNotify covers the core bug fix: a second fetch
// that adds one more event notifies exactly once, about that event; refetching
// the same events never notifies again.
func TestOnlyGenuinelyNewEventsNotify(t *testing.T) {
	s, fn := newTestService(t)
	tr := newTracked(t, s, "coreflow")

	e1 := evAt(3*time.Hour, models.StatusPending, "Shipment information received", "Leipzig")
	e2 := evAt(2*time.Hour, models.StatusInTransit, "Processed at facility", "Leipzig")
	if err := s.applyDirectResult(tr, []models.TrackingEvent{e1, e2}, "", ""); err != nil {
		t.Fatal(err)
	}

	// One genuinely new event arrives.
	e3 := evAt(1*time.Hour, models.StatusOutForDelivery, "With delivery courier", "São Paulo")
	tr2, _ := s.store.Get("coreflow")
	if err := s.applyDirectResult(tr2, []models.TrackingEvent{e1, e2, e3}, "", ""); err != nil {
		t.Fatal(err)
	}
	if fn.count() != 1 {
		t.Fatalf("expected exactly 1 toast for the new event, got %d (%v)", fn.count(), fn.sent)
	}
	if !strings.Contains(fn.last(), "With delivery courier") {
		t.Fatalf("toast should describe the new event, got %q", fn.last())
	}

	// Same events again: nothing new, nothing toasted.
	tr3, _ := s.store.Get("coreflow")
	if err := s.applyDirectResult(tr3, []models.TrackingEvent{e1, e2, e3}, "", ""); err != nil {
		t.Fatal(err)
	}
	if fn.count() != 1 {
		t.Fatalf("re-delivering known events must not re-notify, got %d toasts (%v)", fn.count(), fn.sent)
	}
}

// TestNotificationsWorkWithoutLastSeen pins the other half of the old bug:
// the user never opened the tracking (LastSeen nil) and still gets toasts —
// the old code required LastSeen != nil, muting background updates.
func TestNotificationsWorkWithoutLastSeen(t *testing.T) {
	s, fn := newTestService(t)
	tr := newTracked(t, s, "unseen")

	e1 := evAt(2*time.Hour, models.StatusPending, "Posted", "Origin")
	if err := s.applyDirectResult(tr, []models.TrackingEvent{e1}, "", ""); err != nil {
		t.Fatal(err)
	}
	e2 := evAt(30*time.Minute, models.StatusInTransit, "In transit", "Hub")
	tr2, _ := s.store.Get("unseen")
	if tr2.LastSeen != nil {
		t.Fatal("precondition: user never viewed this tracking")
	}
	if err := s.applyDirectResult(tr2, []models.TrackingEvent{e1, e2}, "", ""); err != nil {
		t.Fatal(err)
	}
	if fn.count() != 1 {
		t.Fatalf("background event must notify even when never viewed, got %d (%v)", fn.count(), fn.sent)
	}
}

// TestDeliveredGetsCelebration checks the delivered parcel extra toast.
func TestDeliveredGetsCelebration(t *testing.T) {
	s, fn := newTestService(t)
	tr := newTracked(t, s, "deliv")

	e1 := evAt(2*time.Hour, models.StatusPending, "Posted", "Origin")
	if err := s.applyDirectResult(tr, []models.TrackingEvent{e1}, "", ""); err != nil {
		t.Fatal(err)
	}
	e2 := evAt(10*time.Minute, models.StatusDelivered, "Delivered", "Home")
	tr2, _ := s.store.Get("deliv")
	if err := s.applyDirectResult(tr2, []models.TrackingEvent{e1, e2}, "", ""); err != nil {
		t.Fatal(err)
	}
	if fn.count() != 2 { // update + delivered celebration
		t.Fatalf("expected update + delivered toasts, got %d (%v)", fn.count(), fn.sent)
	}
	if !strings.HasPrefix(fn.sent[0], "update|") || !strings.HasPrefix(fn.sent[1], "delivered|") {
		t.Fatalf("expected [update, delivered] toasts, got %v", fn.sent)
	}
}

// TestLateBackfillIsSilent merges but never toasts events older than the
// watermark (late-arriving history from another source).
func TestLateBackfillIsSilent(t *testing.T) {
	s, fn := newTestService(t)
	tr := newTracked(t, s, "backfill")

	e1 := evAt(3*time.Hour, models.StatusPending, "Posted", "Origin")
	e2 := evAt(2*time.Hour, models.StatusInTransit, "In transit", "A")
	if err := s.applyDirectResult(tr, []models.TrackingEvent{e1, e2}, "", ""); err != nil {
		t.Fatal(err)
	}

	// A new source backfills an event BETWEEN the known ones.
	eMid := evAt(5*time.Hour, models.StatusPending, "Info received", "China")
	tr2, _ := s.store.Get("backfill")
	if err := s.applyDirectResult(tr2, []models.TrackingEvent{e1, e2, eMid}, "", ""); err != nil {
		t.Fatal(err)
	}
	got, _ := s.store.Get("backfill")
	if len(got.Events) != 3 {
		t.Fatalf("backfilled event should merge, got %d events", len(got.Events))
	}
	if fn.count() != 0 {
		t.Fatalf("events older than the watermark must stay silent, got %d toasts (%v)", fn.count(), fn.sent)
	}
}

// TestWatermarkSurvivesRestart ensures notifications are not replayed after an
// app restart (the watermark is persisted with the tracking).
func TestWatermarkSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	fn := &fakeNotifier{}
	s := New(st, engine.New("", ""), fn, func(name string, data ...any) {})

	tr := &models.Tracking{ID: "restart", Code: "RESTART", Carrier: "dhl", CreatedAt: time.Now().UTC()}
	if err := st.Upsert(tr); err != nil {
		t.Fatal(err)
	}
	cur, _ := st.Get("restart")
	e1 := evAt(2*time.Hour, models.StatusPending, "Posted", "Origin")
	e2 := evAt(1*time.Hour, models.StatusInTransit, "In transit", "A")
	if err := s.applyDirectResult(cur, []models.TrackingEvent{e1, e2}, "", ""); err != nil {
		t.Fatal(err)
	}
	if fn.count() != 0 {
		t.Fatalf("first population should be silent, got %d toasts", fn.count())
	}

	// "Restart": fresh Service on a fresh Store over the same directory.
	st2, err := store.NewAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	fn2 := &fakeNotifier{}
	s2 := New(st2, engine.New("", ""), fn2, func(name string, data ...any) {})
	cur2, _ := st2.Get("restart")
	if err := s2.applyDirectResult(cur2, []models.TrackingEvent{e1, e2}, "", ""); err != nil {
		t.Fatal(err)
	}
	if fn2.count() != 0 {
		t.Fatalf("known events must not notify after restart, got %d toasts (%v)", fn2.count(), fn2.sent)
	}

	// A new event after the restart still notifies.
	e3 := evAt(10*time.Minute, models.StatusOutForDelivery, "Out for delivery", "B")
	cur3, _ := st2.Get("restart")
	if err := s2.applyDirectResult(cur3, []models.TrackingEvent{e1, e2, e3}, "", ""); err != nil {
		t.Fatal(err)
	}
	if fn2.count() != 1 {
		t.Fatalf("new event after restart must notify, got %d toasts (%v)", fn2.count(), fn2.sent)
	}
}

// TestUpgradeWatermarkBackfill pins the load-time migration: a pre-v1.5
// tracking (events, no watermark) gets one at load, so its next new event
// toasts immediately instead of being swallowed as "first population".
func TestUpgradeWatermarkBackfill(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate v1.4 data: events present, no LastNotified.
	old := &models.Tracking{ID: "legacy", Code: "LEGACY", Carrier: "dhl", CreatedAt: time.Now().UTC()}
	e1 := evAt(2*time.Hour, models.StatusPending, "Posted", "Origin")
	e2 := evAt(1*time.Hour, models.StatusInTransit, "In transit", "A")
	old.Events = []models.TrackingEvent{e1, e2}
	if err := st.Upsert(old); err != nil {
		t.Fatal(err)
	}
	// Strip the watermark to imitate a file written by v1.4.
	cur, _ := st.Get("legacy")
	cur.LastNotified = nil
	if err := st.Upsert(cur); err != nil {
		t.Fatal(err)
	}

	st2, err := store.NewAt(dir) // reload = migration runs
	if err != nil {
		t.Fatal(err)
	}
	got, _ := st2.Get("legacy")
	if got.LastNotified == nil {
		t.Fatal("load migration should initialize the watermark for legacy trackings")
	}
	if !got.LastNotified.Equal(e2.Date) {
		t.Fatalf("watermark should sit at the newest event, got %+v", got.LastNotified)
	}
}

// TestDisabledNotificationsStillAdvanceWatermark ensures enabling toasts
// later does not replay the whole history.
func TestDisabledNotificationsStillAdvanceWatermark(t *testing.T) {
	s, fn := newTestService(t)
	st := s.store
	stSettings := st.Settings()
	stSettings.Notifications = false
	if err := st.SaveSettings(stSettings); err != nil {
		t.Fatal(err)
	}

	tr := newTracked(t, s, "quiet")
	e1 := evAt(2*time.Hour, models.StatusPending, "Posted", "Origin")
	if err := s.applyDirectResult(tr, []models.TrackingEvent{e1}, "", ""); err != nil {
		t.Fatal(err)
	}

	// Notifications come back on; the same events must stay silent.
	stSettings.Notifications = true
	if err := st.SaveSettings(stSettings); err != nil {
		t.Fatal(err)
	}
	tr2, _ := st.Get("quiet")
	if err := s.applyDirectResult(tr2, []models.TrackingEvent{e1}, "", ""); err != nil {
		t.Fatal(err)
	}
	if fn.count() != 0 {
		t.Fatalf("enabling notifications later must not replay history, got %d toasts (%v)", fn.count(), fn.sent)
	}
}

// TestSedexNeverDeadEnds covers the Correios/Sedex captcha rewiring: when a
// domestic Sedex number cannot be resolved by the automatic chain (Cainiao ->
// 17TRACK web), Refresh must land in ONE of the two recoverable states —
// either a scheduled retry (QUEUED: the web engine is still looking) or the
// "needs verification" captcha pill — never a dead-end error.
func TestSedexNeverDeadEnds(t *testing.T) {
	s, _ := newTestService(t)
	tr := &models.Tracking{
		ID:      "sedex1",
		Code:    "SS123456789BR", // well-formed S10 that no network knows
		Carrier: "correios",
	}
	if err := s.store.Upsert(tr); err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(tr); err != nil {
		t.Fatalf("Refresh returned an error: %v", err)
	}
	out, ok := s.store.Get("sedex1")
	if !ok {
		t.Fatal("tracking disappeared from the store")
	}
	if out.NeedsCaptcha {
		return // captcha pill: the expected fallback for unknown numbers
	}
	// Otherwise the web engine must have queued a retry (fresh numbers get a
	// chance to auto-resolve before the pill is offered).
	if out.LastError != "" && !strings.HasPrefix(out.LastError, "QUEUED:") &&
		!strings.Contains(out.LastError, "17TRACK") {
		t.Fatalf("dead-end error for a Sedex number: %q", out.LastError)
	}
}
