package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"plurideliver/internal/models"
)

func TestStoreRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	tr := &models.Tracking{
		ID:         models.TrackingID("correios", "PU123456789BR"),
		Code:       "PU123456789BR",
		Name:       "Fone bluetooth",
		Carrier:    "correios",
		AutoUpdate: true,
		CreatedAt:  time.Now().UTC(),
		Events: []models.TrackingEvent{
			{ID: "abc123", Date: time.Now().UTC().Add(-2 * time.Hour), Status: models.StatusPending, Description: "Objeto postado"},
			{ID: "def456", Date: time.Now().UTC().Add(-1 * time.Hour), Status: models.StatusInTransit, Description: "Em trânsito"},
		},
	}
	if err := s.Upsert(tr); err != nil {
		t.Fatal(err)
	}

	// reopen: persistence check
	s2, err := NewAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.Get(tr.ID)
	if !ok {
		t.Fatal("tracking lost after reopen")
	}
	if got.Name != "Fone bluetooth" || len(got.Events) != 2 {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
	if got.Events[0].Description != "Objeto postado" {
		t.Errorf("events not sorted chronologically")
	}

	if err := s2.Delete(tr.ID); err != nil {
		t.Errorf("Delete returned error: %v", err)
	}
	if err := s2.Delete(tr.ID); !errors.Is(err, ErrTrackingNotFound) {
		t.Errorf("double delete should report ErrTrackingNotFound, got %v", err)
	}
	s3, _ := NewAt(dir)
	if _, ok := s3.Get(tr.ID); ok {
		t.Error("deleted tracking persisted")
	}
}

func TestSettingsPersistence(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewAt(dir)
	st := models.DefaultSettings()
	st.Language = "pt-BR"
	st.Theme = "light"
	st.UpdateMode = "manual"
	st.PollMinutes = 30
	st.Aggregator = models.Aggregator{Provider: "17track", Key: "abc"}
	if err := s.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	s2, _ := NewAt(dir)
	got := s2.Settings()
	if got.Language != "pt-BR" || got.Theme != "light" || got.UpdateMode != "manual" || got.PollMinutes != 30 {
		t.Errorf("settings lost: %+v", got)
	}
	if got.Aggregator.Key != "abc" || got.Aggregator.Provider != "17track" {
		t.Errorf("aggregator settings lost: %+v", got.Aggregator)
	}
}

func TestSettingsSanitize(t *testing.T) {
	st := models.DefaultSettings()
	st.Language = "fr"
	st.Theme = "hotdog"
	st.UpdateMode = "banana"
	st.PollMinutes = 1
	st.IdlePollMinutes = 0
	st.Aggregator.Provider = "nope"
	st.Sanitize()
	if st.Language != "en" || st.Theme != "dark" || st.UpdateMode != "smart" {
		t.Errorf("sanitize failed: %+v", st)
	}
	if st.PollMinutes != 5 || st.IdlePollMinutes != 5 {
		t.Errorf("poll clamping failed: %+v", st)
	}
	if st.Aggregator.Provider != "none" {
		t.Errorf("aggregator sanitize failed")
	}
}

func TestAtomicWriteLeavesNoTmp(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewAt(dir)
	tr := &models.Tracking{ID: models.TrackingID("dhl", "1234567890"), Code: "1234567890", Carrier: "dhl", CreatedAt: time.Now()}
	_ = s.Upsert(tr)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover tmp file: %s", e.Name())
		}
	}
}

func TestCarriersReadmeCreated(t *testing.T) {
	dir := t.TempDir()
	_, err := NewAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "carriers", "README.txt")); err != nil {
		t.Error("carriers README not created")
	}
}

// breakWrites makes the persistence target unwritable: trackings.json (or
// settings.json) becomes a directory, so the atomic tmp-write succeeds but
// the rename fails — exactly the failure mode staged writes must survive.
func breakWrites(t *testing.T, dir, name string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.Remove(p); err != nil {
		t.Fatalf("remove %s: %v", name, err)
	}
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", name, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(p) })
}

// TestUpsertWriteFailureKeepsState pins the consistency contract: when the
// disk write fails, the in-memory state must NOT gain the tracking.
func TestUpsertWriteFailureKeepsState(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewAt(dir)
	first := &models.Tracking{ID: "first", Code: "A", Carrier: "dhl", CreatedAt: time.Now()}
	if err := s.Upsert(first); err != nil {
		t.Fatal(err)
	}

	breakWrites(t, dir, "trackings.json")
	second := &models.Tracking{ID: "second", Code: "B", Carrier: "dhl", CreatedAt: time.Now()}
	if err := s.Upsert(second); err == nil {
		t.Fatal("Upsert should fail when the write fails")
	}
	if _, ok := s.Get("second"); ok {
		t.Error("memory gained a tracking whose write failed — state diverged from disk")
	}
	if _, ok := s.Get("first"); !ok {
		t.Error("pre-existing tracking lost")
	}
	// once the disk is healthy again, the same upsert succeeds
	_ = os.RemoveAll(filepath.Join(dir, "trackings.json"))
	if err := s.Upsert(second); err != nil {
		t.Fatalf("Upsert should succeed after recovery: %v", err)
	}
}

// TestDeleteWriteFailureKeepsState pins the other half of the contract: a
// failed delete must leave the tracking in memory AND on disk.
func TestDeleteWriteFailureKeepsState(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewAt(dir)
	tr := &models.Tracking{ID: "keep", Code: "A", Carrier: "dhl", CreatedAt: time.Now()}
	if err := s.Upsert(tr); err != nil {
		t.Fatal(err)
	}

	breakWrites(t, dir, "trackings.json")
	if err := s.Delete("keep"); err == nil {
		t.Fatal("Delete should report the persistence failure")
	}
	if _, ok := s.Get("keep"); !ok {
		t.Error("tracking vanished from memory although the write failed")
	}
	_ = os.RemoveAll(filepath.Join(dir, "trackings.json"))
}

// TestSaveSettingsWriteFailureKeepsState pins SaveSettings to the same
// write-before-commit contract.
func TestSaveSettingsWriteFailureKeepsState(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewAt(dir)
	st := models.DefaultSettings()
	st.Language = "en"
	if err := s.SaveSettings(st); err != nil {
		t.Fatal(err)
	}

	breakWrites(t, dir, "settings.json")
	st.Language = "pt-BR"
	if err := s.SaveSettings(st); err == nil {
		t.Fatal("SaveSettings should fail when the write fails")
	}
	if got := s.Settings().Language; got != "en" {
		t.Errorf("memory settings changed to %q although the write failed", got)
	}
	_ = os.RemoveAll(filepath.Join(dir, "settings.json"))
}

// TestCorreiosMigrationIsOneTime pins the v1.4.0 upgrade: portal-captured
// events (Source "Correios") are dropped once on load, automatic-source
// events survive, and the stale captcha flag is cleared.
func TestCorreiosMigrationIsOneTime(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewAt(dir)
	tr := &models.Tracking{
		ID:           models.TrackingID("correios", "NN428571043BR"),
		Code:         "NN428571043BR",
		Carrier:      "correios",
		NeedsCaptcha: true,
		Events: []models.TrackingEvent{
			{ID: "p1", Date: time.Now().UTC().Add(-3 * time.Hour), Status: models.StatusInTransit, Description: "portal event", Source: "Correios"},
			{ID: "c1", Date: time.Now().UTC().Add(-2 * time.Hour), Status: models.StatusInTransit, Description: "cainiao event", Source: "Cainiao"},
		},
	}
	if err := s.Upsert(tr); err != nil {
		t.Fatal(err)
	}

	s2, _ := NewAt(dir)
	got, ok := s2.Get(tr.ID)
	if !ok {
		t.Fatal("tracking lost")
	}
	if got.NeedsCaptcha {
		t.Error("stale needsCaptcha flag not cleared")
	}
	if len(got.Events) != 1 || got.Events[0].Source != "Cainiao" {
		t.Fatalf("portal events not filtered: %+v", got.Events)
	}

	// Reload again: the surviving Cainiao event must NOT be touched.
	s3, _ := NewAt(dir)
	got3, _ := s3.Get(tr.ID)
	if len(got3.Events) != 1 || got3.Events[0].Source != "Cainiao" {
		t.Fatalf("migration re-ran on automatic events: %+v", got3.Events)
	}

	// Non-correios carriers keep their events untouched.
	other := &models.Tracking{
		ID: models.TrackingID("spx", "SPXBR123456789"), Code: "SPXBR123456789", Carrier: "spx",
		Events: []models.TrackingEvent{
			{ID: "s1", Date: time.Now().UTC(), Status: models.StatusPending, Description: "x", Source: "SPX"},
		},
	}
	if err := s2.Upsert(other); err != nil {
		t.Fatal(err)
	}
	s4, _ := NewAt(dir)
	got4, _ := s4.Get(other.ID)
	if len(got4.Events) != 1 {
		t.Fatalf("non-correios events must survive migration: %+v", got4.Events)
	}
}
