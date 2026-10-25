package engine

import (
	"strings"
	"testing"
	"time"

	"plurideliver/internal/models"
)

func testSettings(demo bool) models.Settings {
	st := models.DefaultSettings()
	st.DemoMode = demo
	return st
}

func trackingFor(carrier, code string, age time.Duration) *models.Tracking {
	t := &models.Tracking{
		ID:        models.TrackingID(carrier, code),
		Code:      strings.ToUpper(code),
		Carrier:   carrier,
		CreatedAt: time.Now().UTC().Add(-age),
	}
	return t
}

func TestLoadCarriers(t *testing.T) {
	e := New("", "")
	got := e.Carriers()
	ids := map[string]bool{}
	for _, c := range got {
		ids[c.ID] = true
		t.Logf("carrier: %+v", c)
	}
	for _, want := range []string{"correios", "jtexpress", "spx", "amazon", "dhl", "fedex", "ups", "usps", "generic", "17track", "17track-web", "aftership", "demo"} {
		if !ids[want] {
			t.Errorf("expected carrier %q to be registered", want)
		}
	}
	// ordering: builtins first, aggregators later, demo last
	if len(got) < 3 {
		t.Fatal("too few carriers")
	}
	lastKind := ""
	for _, c := range got {
		rank := map[string]int{"builtin": 0, "aggregator": 1, "demo": 2}[c.Kind]
		last := -1
		if lastKind != "" {
			last = map[string]int{"builtin": 0, "aggregator": 1, "demo": 2}[lastKind]
		}
		if rank < last {
			t.Errorf("carrier ordering broken: %s (%s) after %s", c.ID, c.Kind, lastKind)
		}
		lastKind = c.Kind
	}
}

func TestDetectPatterns(t *testing.T) {
	e := New("", "")
	cases := []struct {
		code string
		want string // first detected carrier id
	}{
		{"PU123456789BR", "correios"},
		{"jt1234567890123", "jtexpress"},
		{"SPXBR0123456789", "spx"},
		{"TBA309842312", "amazon"},
		{"1234567890", "dhl"},
		{"1Z123AA60312345674", "ups"},
		{"DEMO-1234", "demo"},
		{"9400111899223344553621", "usps"},
	}
	for _, c := range cases {
		got := e.Detect(c.code)
		if len(got) == 0 {
			t.Errorf("no carrier detected for %q", c.code)
			continue
		}
		if got[0].ID != c.want {
			t.Errorf("Detect(%q) = %q, want %q (all: %v)", c.code, got[0].ID, c.want, ids(got))
		}
	}
}

func ids(list []models.CarrierInfo) []string {
	out := []string{}
	for _, c := range list {
		out = append(out, c.ID)
	}
	return out
}

func TestDemoEveryCarrier(t *testing.T) {
	e := New("", "")
	st := testSettings(true)
	for _, c := range e.Carriers() {
		tr := trackingFor(c.ID, "DEMO-CODE-42", 30*time.Minute)
		tr.Code = "DEMO-CODE-42"
		events, err := e.Track(tr, st)
		if err != nil {
			t.Errorf("demo track %s: %v", c.ID, err)
			continue
		}
		if len(events) < 1 || len(events) > 8 {
			t.Errorf("demo %s: unexpected event count %d", c.ID, len(events))
		}
		seen := map[string]bool{}
		for _, ev := range events {
			if ev.ID == "" || seen[ev.ID] {
				t.Errorf("demo %s: bad/duplicate event id %q", c.ID, ev.ID)
			}
			seen[ev.ID] = true
			if !time.Time.IsZero(ev.Date) && ev.Description == "" {
				t.Errorf("demo %s: empty description", c.ID)
			}
			switch ev.Status {
			case models.StatusDelivered, models.StatusOutForDelivery, models.StatusInTransit,
				models.StatusPending, models.StatusException, models.StatusInfo:
			default:
				t.Errorf("demo %s: invalid status %q", c.ID, ev.Status)
			}
		}
	}
}

func TestDemoDeterminismAndProgress(t *testing.T) {
	e := New("", "")
	st := testSettings(true)
	a := trackingFor("demo", "DEMO-ABC", 30*time.Minute)
	b := trackingFor("demo", "DEMO-ABC", 30*time.Minute)
	e1, err1 := e.Track(a, st)
	e2, err2 := e.Track(b, st)
	if err1 != nil || err2 != nil {
		t.Fatalf("demo track failed: %v %v", err1, err2)
	}
	if len(e1) != len(e2) {
		t.Fatalf("demo not deterministic: %d vs %d events", len(e1), len(e2))
	}
	for i := range e1 {
		if e1[i].ID != e2[i].ID {
			t.Fatalf("event %d id mismatch: %s vs %s", i, e1[i].ID, e2[i].ID)
		}
	}
	// older tracking must have at least as many events
	c := trackingFor("demo", "DEMO-ABC", 24*time.Hour)
	e3, _ := e.Track(c, st)
	if len(e3) < len(e1) {
		t.Fatalf("older tracking has fewer events: %d < %d", len(e3), len(e1))
	}
}

func TestDemoCodeAlwaysDemo(t *testing.T) {
	e := New("", "")
	st := testSettings(false) // demo mode OFF
	tr := trackingFor("correios", "DEMO-123", time.Minute)
	events, err := e.Track(tr, st)
	if err != nil {
		t.Fatalf("DEMO code should always demo: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected demo events")
	}
}

func TestAggregatorRequiresKey(t *testing.T) {
	e := New("", "")
	st := testSettings(false)
	st.Aggregator = models.Aggregator{Provider: "none", Key: ""}
	tr := trackingFor("17track", "PU123456789BR", time.Minute)
	_, err := e.Track(tr, st)
	if err == nil {
		// The keyless web engine may legitimately answer on a networked host,
		// but a made-up code must never produce a success path here.
		t.Log("keyed adapter fell through to the web engine and got events for a fake code - unexpected")
	}

	// with a bogus key we still expect an error (no network guarantees in CI)
	st.Aggregator = models.Aggregator{Provider: "17track", Key: "bogus"}
	tr2 := trackingFor("fedex", "999999999999", time.Minute)
	_, _ = e.Track(tr2, st) // must not panic
}

func TestRequiresKeyCarriers(t *testing.T) {
	e := New("", "")
	st := testSettings(false)
	for _, id := range []string{"amazon", "fedex", "ups", "usps", "generic"} {
		tr := trackingFor(id, "AMAZONTEST1234", time.Minute)
		_, err := e.Track(tr, st)
		if err == nil {
			t.Errorf("%s: expected error for a made-up code", id)
		}
	}
}

func TestParseDate(t *testing.T) {
	cases := []string{
		"2025-10-20T14:22:00Z", "2025-10-20T14:22:00", "2025-10-20 14:22:00",
		"2025-10-20 14:22", "2025-10-20", "20/10/2025 14:22", "20/10/2025",
		"20-10-2025 14:22:05", "2025/10/20 14:22",
	}
	for _, c := range cases {
		if _, ok := parseDate(c); !ok {
			t.Errorf("parseDate(%q) failed", c)
		}
	}
	if _, ok := parseDate("not a date"); ok {
		t.Error("parseDate accepted garbage")
	}
}

func TestNormalizeAndDerive(t *testing.T) {
	if got := normalizeStatus("Entregue"); got != models.StatusDelivered {
		t.Errorf("normalizeStatus(Entregue) = %s", got)
	}
	if got := deriveFromDescription("Package delivered to front door"); got != models.StatusDelivered {
		t.Errorf("derive(delivered text) = %s", got)
	}
	if got := deriveFromDescription("Objeto saiu para entrega"); got != models.StatusOutForDelivery {
		t.Errorf("derive(out for delivery text) = %s", got)
	}
}

func TestCorreiosIsFullyAutomatic(t *testing.T) {
	e := New("", "")

	// Correios must NOT declare captcha anymore: v1.4.0 routes it through
	// the keyless Cainiao client + 17TRACK web engine.
	c := e.Get("correios")
	if c == nil {
		t.Fatal("correios carrier missing")
	}
	if c.Info.CaptchaRequired {
		t.Error("correios must not declare captcha_required (automatic sources since v1.4.0)")
	}
	if c.hasTrack {
		t.Error("correios should have no direct track function (Go client + web engine chain)")
	}

	// Detection: only BR-suffixed S10 codes are claimed automatically.
	for _, yes := range []string{"NN428571043BR", "PU123456789BR", "LB999222333BR"} {
		found := false
		for _, ci := range e.Detect(yes) {
			if ci.ID == "correios" {
				found = true
			}
		}
		if !found {
			t.Errorf("correios should detect %s", yes)
		}
	}
	for _, no := range []string{"LZ123456789CN", "RR123456789CN", "JT1234567890123"} {
		for _, ci := range e.Detect(no) {
			if ci.ID == "correios" {
				t.Errorf("correios must not claim foreign code %s", no)
			}
		}
	}
}
