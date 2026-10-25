package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"plurideliver/internal/models"
)

// ---- polling.lua -------------------------------------------------------------

func TestPollingDefaults(t *testing.T) {
	e := New("", "")
	base := PollingInfo{
		Code: "X", Carrier: "c", Status: models.StatusInTransit,
		Active: true, PollMinutes: 15, IdlePollMinutes: 60,
		LastEventAgeHours: 2,
	}
	cases := []struct {
		name string
		mut  func(*PollingInfo)
		want int
	}{
		{"plain active", func(p *PollingInfo) {}, 15},
		{"idle uses idle interval", func(p *PollingInfo) { p.Active = false }, 60},
		{"idle below active uses active", func(p *PollingInfo) { p.IdlePollMinutes = 10; p.Active = false }, 15},
		{"delivered relaxes to 12h", func(p *PollingInfo) { p.Delivered = true; p.Status = models.StatusDelivered }, 720},
		{"delivered never slower than base when base bigger", func(p *PollingInfo) {
			p.Delivered = true
			p.Status = models.StatusDelivered
			p.PollMinutes = 1200 // sanitized max
		}, 1200},
		{"final mile tightens to 15", func(p *PollingInfo) {
			p.Status = models.StatusOutForDelivery
			p.PollMinutes = 120
		}, 15},
		{"final mile respects smaller base", func(p *PollingInfo) { p.Status = models.StatusOutForDelivery }, 15},
		{"error backs off", func(p *PollingInfo) { p.HasError = true }, 30},
		{"error backs off capped", func(p *PollingInfo) { p.HasError = true; p.PollMinutes = 600 }, 360},
		{"stale relaxes", func(p *PollingInfo) { p.LastEventAgeHours = 400 }, 240},
		{"fresh stays base", func(p *PollingInfo) { p.LastEventAgeHours = 335 }, 15},
		{"no events yet stays base", func(p *PollingInfo) { p.LastEventAgeHours = -1 }, 15},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := base
			tc.mut(&info)
			got, err := e.RunPolling(info)
			if err != nil {
				t.Fatalf("RunPolling: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestPollingUserOverrideWins(t *testing.T) {
	dir := t.TempDir()
	src := `
local function decide(i)
  if i.status == "delivered" then return 9999 end
  return 42
end
return { decide = decide }
`
	if err := os.WriteFile(filepath.Join(dir, "polling.lua"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	e := New("", dir)
	got, err := e.RunPolling(PollingInfo{Status: models.StatusInTransit, PollMinutes: 15})
	if err != nil {
		t.Fatal(err)
	}
	if got != 42 {
		t.Fatalf("user override ignored: got %d, want 42", got)
	}
	// Out-of-range values are clamped, never trusted blindly.
	got, _ = e.RunPolling(PollingInfo{Status: models.StatusDelivered, PollMinutes: 15})
	if got != 1440 {
		t.Fatalf("clamp failed: got %d, want 1440", got)
	}
}

func TestPollingHostileScriptIsBounded(t *testing.T) {
	dir := t.TempDir()
	src := `
local function decide(i)
  while true do end
end
return { decide = decide }
`
	if err := os.WriteFile(filepath.Join(dir, "polling.lua"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := scriptTimeout
	scriptTimeout = 2 * time.Second
	defer func() { scriptTimeout = orig }()
	e := New("", dir)
	if _, err := e.RunPolling(PollingInfo{PollMinutes: 15}); err == nil {
		t.Fatal("expected the infinite loop to be time-bounded and fail, got success")
	}
}

func TestPollingBrokenScriptFailsClosed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "polling.lua"), []byte("this is not lua"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := New("", dir)
	if _, err := e.RunPolling(PollingInfo{}); err == nil {
		t.Fatal("expected error for a broken script")
	}
}

// ---- notify.lua ---------------------------------------------------------------

func notifyFixtures() (NotifyInfo, []models.TrackingEvent) {
	events := []models.TrackingEvent{
		{ID: "a", Date: time.Now().Add(-2 * time.Hour), Status: models.StatusInTransit, Description: "Objeto em trânsito", Location: "Curitiba / PR"},
		{ID: "b", Date: time.Now().Add(-1 * time.Hour), Status: models.StatusOutForDelivery, Description: "Saiu para entrega", Location: "São Paulo / SP"},
	}
	info := NotifyInfo{
		Code: "NN428571043BR", Name: "Fone de ouvido", Carrier: "correios",
		Language: "pt-BR", Status: models.StatusOutForDelivery, Events: events,
		PlusMorePattern: "%s (+%d outros eventos)",
		DeliveredBody:   "Sua encomenda foi entregue!",
	}
	return info, events
}

func TestNotifyDefaultFormatting(t *testing.T) {
	e := New("", "")
	info, events := notifyFixtures()
	notes, err := e.RunNotify(info)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 {
		t.Fatalf("got %d notes, want 1 (not delivered)", len(notes))
	}
	n := notes[0]
	if n.Kind != "update" || n.Title != "Fone de ouvido" {
		t.Fatalf("bad note header: %+v", n)
	}
	want := "Saiu para entrega · São Paulo / SP (+1 outros eventos)"
	if n.Body != want {
		t.Fatalf("body:\n got %q\nwant %q", n.Body, want)
	}
	_ = events
}

func TestNotifyDefaultDeliveredCelebration(t *testing.T) {
	e := New("", "")
	info, _ := notifyFixtures()
	info.Delivered = true
	info.Status = models.StatusDelivered
	notes, err := e.RunNotify(info)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 {
		t.Fatalf("got %d notes, want 2 (update + delivered)", len(notes))
	}
	if notes[1].Kind != "delivered" || notes[1].Body != "Sua encomenda foi entregue!" {
		t.Fatalf("bad delivered note: %+v", notes[1])
	}
}

func TestNotifyUnnamedTrackingFallsBackToCode(t *testing.T) {
	e := New("", "")
	info, _ := notifyFixtures()
	info.Name = ""
	notes, err := e.RunNotify(info)
	if err != nil {
		t.Fatal(err)
	}
	if notes[0].Title != "NN428571043BR" {
		t.Fatalf("want code as title, got %q", notes[0].Title)
	}
}

func TestNotifyUserOverrideSilences(t *testing.T) {
	dir := t.TempDir()
	src := `
local function build(i)
  if not i.delivered then return {} end
  return { { kind = "delivered", title = "PRONTO", body = "chegou!" } }
end
return { build = build }
`
	if err := os.WriteFile(filepath.Join(dir, "notify.lua"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	e := New("", dir)

	notes, err := e.RunNotify(mustFixtures(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Fatalf("custom script should have silenced non-delivered updates, got %+v", notes)
	}

	info, _ := notifyFixtures()
	info.Delivered = true
	info.Status = models.StatusDelivered
	notes, err = e.RunNotify(info)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Title != "PRONTO" || notes[0].Body != "chegou!" {
		t.Fatalf("custom delivered note wrong: %+v", notes)
	}
}

func mustFixtures(t *testing.T) NotifyInfo {
	t.Helper()
	info, _ := notifyFixtures()
	return info
}
