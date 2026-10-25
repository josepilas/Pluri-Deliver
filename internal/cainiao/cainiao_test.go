package cainiao

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"plurideliver/internal/models"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

// TestTrackParsesRealResponse pins the parser to the live response captured
// for a real China→Brazil import (NN428571043BR, 17 combined events).
func TestTrackParsesRealResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mailNos") != "NN428571043BR" {
			t.Errorf("unexpected mailNos %q", r.URL.Query().Get("mailNos"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(loadFixture(t, "detail.json"))
	}))
	defer srv.Close()
	old := baseURL
	baseURL = srv.URL
	defer func() { baseURL = old }()

	res, err := New().Track("nn428571043br", "en")
	if err != nil {
		t.Fatalf("Track: %v", err)
	}
	if res.Code != "NN428571043BR" {
		t.Errorf("Code = %s", res.Code)
	}
	if len(res.Events) != 17 {
		t.Errorf("events = %d, want 17", len(res.Events))
	}
	if res.Origin != "Mainland China" || res.Destination != "Brazil" {
		t.Errorf("origin/dest = %q/%q", res.Origin, res.Destination)
	}
	if res.LastCarrier != "Correios(BR)" {
		t.Errorf("last carrier = %q", res.LastCarrier)
	}

	// chronological order, oldest first
	var prev time.Time
	for i, ev := range res.Events {
		if !prev.IsZero() && ev.Date.Before(prev) {
			t.Errorf("event %d out of order", i)
		}
		prev = ev.Date
	}

	// newest event: "[CURITIBA] Departed from destination country/region
	// sorting center" at 2026-10-23 11:27:37 GMT-3 (epoch 1792726057)
	last := res.Events[len(res.Events)-1]
	want := time.Unix(1792726057, 0).UTC()
	if !last.Date.Equal(want) {
		t.Errorf("latest date = %s, want %s", last.Date, want)
	}
	if last.Location != "CURITIBA" {
		t.Errorf("latest location = %q, want CURITIBA (prefix split)", last.Location)
	}
	if last.Status != models.StatusInTransit {
		t.Errorf("latest status = %s, want in_transit (GTMS_SC_DEPART)", last.Status)
	}
	if last.Source != "Cainiao" {
		t.Errorf("source = %q", last.Source)
	}
	if strings.Contains(last.Description, "[") {
		t.Errorf("city prefix not stripped: %q", last.Description)
	}

	// oldest event: warehouse accept -> pending
	first := res.Events[0]
	if first.Status != models.StatusPending {
		t.Errorf("oldest status = %s, want pending (GWMS_ACCEPT)", first.Status)
	}
}

// TestTrackPortuguese verifies the pt-BR request language is forwarded and
// Correios' own wording comes back.
func TestTrackPortuguese(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("lang"); got != "pt-BR" {
			t.Errorf("lang = %q, want pt-BR", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(loadFixture(t, "detail_pt.json"))
	}))
	defer srv.Close()
	old := baseURL
	baseURL = srv.URL
	defer func() { baseURL = old }()

	res, err := New().Track("NN428571043BR", "pt-BR")
	if err != nil {
		t.Fatalf("Track: %v", err)
	}
	last := res.Events[len(res.Events)-1]
	if !strings.Contains(last.Description, "Objeto em trânsito") {
		t.Errorf("pt-BR wording missing: %q", last.Description)
	}
}

// TestTrackNotFound pins both "unknown number" shapes: the AliExpress
// placeholder (SELLER_PREPARING, empty detailList) and the bare EXTERNAL
// module with no data.
func TestTrackNotFound(t *testing.T) {
	for _, fixture := range []string{"notfound.json", "external.json"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(loadFixture(t, fixture))
		}))
		baseURL = srv.URL
		_, err := New().Track("ZZ123456789BR", "en")
		srv.Close()
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", fixture, err)
		}
	}
	baseURL = "https://global.cainiao.com"
}

// TestTrackNoModule covers a fully empty response.
func TestTrackNoModule(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"module":[],"success":true}`))
	}))
	defer srv.Close()
	old := baseURL
	baseURL = srv.URL
	defer func() { baseURL = old }()

	if _, err := New().Track("NN428571043BR", "en"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestStatusFor pins the action/group code mapping.
func TestStatusFor(t *testing.T) {
	cases := []struct {
		action string
		node   string
		desc   string
		want   string
	}{
		{"GTMS_SIGN", "", "Delivered", models.StatusDelivered},
		{"GTMS_DELIVER_SUCCESS", "", "", models.StatusDelivered},
		{"CC_IM_FAILED", "", "Customs issue", models.StatusException},
		{"GWMS_ACCEPT", "AE_GROUP_WH_PROCESSING", "", models.StatusPending},
		{"GWMS_PACKAGE", "", "", models.StatusPending},
		{"GTMS_SC_ALLOCATE", "", "", models.StatusOutForDelivery},
		{"GTMS_SC_DEPART", "AE_GROUP_DES_PROCESSING", "", models.StatusInTransit},
		{"CC_IM_SUCCESS", "AE_GROUP_IM_CLEARING_CUSTOMS", "", models.StatusInTransit},
		{"", "", "Objeto entregue ao destinatário", models.StatusDelivered},
		{"", "", "Objeto saiu para entrega", models.StatusOutForDelivery},
	}
	for _, c := range cases {
		var g *group
		if c.node != "" {
			g = &group{NodeCode: c.node}
		}
		if got := StatusFor(c.action, g, c.desc); got != c.want {
			t.Errorf("StatusFor(%q,%q,%q) = %s, want %s", c.action, c.node, c.desc, got, c.want)
		}
	}
}

// TestBuildEventCitySplit verifies the "[CITY] description" localization
// prefix is split into the location column.
func TestBuildEventCitySplit(t *testing.T) {
	ev := buildEvent("NN428571043BR", detailItem{
		Time:       1792726057000,
		ActionCode: "GTMS_SC_DEPART",
		Standerd:   "[CURITIBA] Objeto em trânsito - por favor aguarde",
	})
	if ev.Location != "CURITIBA" {
		t.Errorf("location = %q", ev.Location)
	}
	if ev.Description != "Objeto em trânsito - por favor aguarde" {
		t.Errorf("description = %q", ev.Description)
	}
	if ev.Date.Unix() != 1792726057 {
		t.Errorf("date = %d", ev.Date.Unix())
	}
	if ev.ID == "" {
		t.Error("missing event id")
	}
}

// TestTimeFallback verifies timeStr parsing when the epoch is absent.
func TestTimeFallback(t *testing.T) {
	ev := buildEvent("X", detailItem{
		TimeStr:  "2026-10-23 11:27:37",
		Standerd: "[CURITIBA] Test event",
	})
	if ev.Date.IsZero() {
		t.Fatal("date not parsed from timeStr")
	}
	// GMT-3 interpretation: 14:27:37Z
	if got := ev.Date.UTC().Format("15:04:05"); got != "14:27:37" {
		t.Errorf("timeStr parsed as %s UTC", got)
	}
}

// TestEnvelopeShape sanity-checks the raw fixture against the struct tags.
func TestEnvelopeShape(t *testing.T) {
	var env envelope
	if err := json.Unmarshal(loadFixture(t, "detail.json"), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.Success || len(env.Module) != 1 {
		t.Fatalf("envelope shape changed: success=%v modules=%d", env.Success, len(env.Module))
	}
	m := env.Module[0]
	if m.Status != "DELIVERING" || m.MailNo != "NN428571043BR" {
		t.Errorf("module = %+v", m)
	}
	if len(m.DetailList) == 0 || m.DestCp == nil || m.DestCp.Name != "Correios(BR)" {
		t.Errorf("module details missing")
	}
}
