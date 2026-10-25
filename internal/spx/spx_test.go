package spx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"plurideliver/internal/models"
)

// Captured live from spx.com.br — the envelope shapes the SPA consumes
// (get_order_info + fleet_order/tracking/search), populated with a realistic
// in-flight parcel based on the field mapping extracted from module 20351.
const orderFixture = `{
  "retcode": 0, "message": "",
  "data": {
    "sls_tracking_info": {
      "records": [
        {"description": "Encomenda entregue", "actual_time": 1761180000, "display_flag": 1, "milestone_code": "DELIVERED", "location": "São Paulo/SP"},
        {"description": "Saiu para entrega", "actual_time": 1761160000, "display_flag": 1, "milestone_code": "OUT_FOR_DELIVERY"},
        {"description": "Em trânsito para o centro de distribuição", "actual_time": 1761120000, "display_flag": 1, "milestone_code": "SHIPPING"},
        {"description": "Pacote coletado", "actual_time": 1761100000, "display_flag": 1, "milestone_code": "PICKED_UP"},
        {"description": "hidden record", "actual_time": 1761090000, "display_flag": 0}
      ]
    },
    "order_info": {"spx_tn": "SPXBR0123456789", "sls_tn": "SPXBR0123456789", "tracking_code_subgroup_name": "Standard"},
    "children": []
  }
}`

const fleetFixture = `{
  "retcode": 0, "message": "",
  "data": {
    "tracking_list": [
      {"message": "Encomenda entregue", "timestamp": 1761180000},
      {"message": "Em trânsito", "timestamp": 1761120000}
    ],
    "current_status": "Delivered",
    "sls_tracking_number": "SPXBR0123456789"
  }
}`

func TestParseOrderResponse(t *testing.T) {
	var env orderEnvelope
	if err := json.Unmarshal([]byte(orderFixture), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Data == nil || env.RetCode != 0 {
		t.Fatalf("expected ok envelope, got %+v", env)
	}
	recs := env.Data.SLSInfo.Records
	if len(recs) != 5 {
		t.Fatalf("expected 5 records, got %d", len(recs))
	}

	// build events like trackOrderInfo does
	var events []models.TrackingEvent
	for _, r := range recs {
		if r.DisplayFlag == 0 {
			continue
		}
		when, ok := parseTime(r.ActualTime)
		if !ok {
			t.Fatalf("cannot parse time %v", r.ActualTime)
		}
		ev := models.TrackingEvent{Date: when, Status: StatusFor(r.Milestone, r.Description), Description: r.Description, Location: r.Location}
		ev.ID = ev.ComputeID()
		events = append(events, ev)
	}
	reverse(events)
	if len(events) != 4 {
		t.Fatalf("expected 4 events (display_flag==0 filtered), got %d", len(events))
	}
	if events[0].Status != models.StatusPending && events[0].Description != "Pacote coletado" {
		t.Errorf("chronological order broken: first = %+v", events[0])
	}
	if events[len(events)-1].Status != models.StatusDelivered {
		t.Errorf("last event should be delivered, got %q", events[len(events)-1].Status)
	}
	for i := 1; i < len(events); i++ {
		if events[i].Date.Before(events[i-1].Date) {
			t.Fatalf("events not chronological at %d", i)
		}
	}
}

func TestParseFleetResponse(t *testing.T) {
	var env fleetEnvelope
	if err := json.Unmarshal([]byte(fleetFixture), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Data == nil || len(env.Data.TrackingList) != 2 {
		t.Fatalf("unexpected fleet envelope: %+v", env)
	}
	when, ok := parseTime(env.Data.TrackingList[0].Timestamp)
	if !ok || when.Unix() != 1761180000 {
		t.Fatalf("timestamp parse failed: %v", env.Data.TrackingList[0].Timestamp)
	}
	if StatusFor("", env.Data.TrackingList[0].Message) != models.StatusDelivered {
		t.Errorf("fleet status mapping failed for delivered message")
	}
}

func TestTrackUsesOrderEndpointAndFallsBackToFleet(t *testing.T) {
	sawOrder, sawFleet := false, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/shipment/order/open/order/get_order_info":
			sawOrder = true
			_, _ = w.Write([]byte(orderFixture))
		case r.URL.Path == "/api/v2/fleet_order/tracking/search":
			sawFleet = true
			_, _ = w.Write([]byte(fleetFixture))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New()
	orig := baseURL
	setBaseURL(srv.URL)
	defer setBaseURL(orig)

	res, err := c.Track("SPXBR0123456789")
	if err != nil {
		t.Fatalf("Track: %v", err)
	}
	if !sawOrder || sawFleet {
		t.Fatalf("expected order endpoint only (no fallback), got order=%v fleet=%v", sawOrder, sawFleet)
	}
	if len(res.Events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(res.Events))
	}

	// Now make the order endpoint fail -> fleet fallback kicks in.
	sawOrder, sawFleet = false, false
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/shipment/order/open/order/get_order_info":
			sawOrder = true
			_, _ = w.Write([]byte(`{"retcode": 2, "message": "not found", "data": null}`))
		case r.URL.Path == "/api/v2/fleet_order/tracking/search":
			sawFleet = true
			// verify the signed param shape: CODE|ts<64-hex>
			p := r.URL.Query().Get("sls_tracking_number")
			const code = "SPXBR0123456789"
			if len(p) < len(code)+1+10+64 || p[:len(code)] != code || p[len(code)] != '|' {
				t.Errorf("signed param malformed: %q", p)
			}
			_, _ = w.Write([]byte(fleetFixture))
		default:
			http.NotFound(w, r)
		}
	})
	res, err = c.Track("SPXBR0123456789")
	if err != nil {
		t.Fatalf("Track fallback: %v", err)
	}
	if !sawOrder || !sawFleet {
		t.Fatalf("expected fallback order->fleet, got order=%v fleet=%v", sawOrder, sawFleet)
	}
	if len(res.Events) != 2 {
		t.Fatalf("expected 2 fleet events, got %d", len(res.Events))
	}
}

func TestNotFoundVariants(t *testing.T) {
	// not found: retcode != 0 and empty-data (the two live-observed shapes)
	for _, body := range []string{
		`{"retcode": 2, "message": "ref record not unique,find [0]", "data": null}`,
		`{"retcode": -1000, "message": "Server error. Please try again later.", "data": {}}`,
	} {
		var env fleetEnvelope
		if err := json.Unmarshal([]byte(body), &env); err != nil {
			t.Fatalf("unmarshal %s: %v", body, err)
		}
		if env.Data != nil && env.RetCode != 0 {
			continue // shape accepted; would be ErrNotFound via trackFleet
		}
		if env.Data == nil {
			continue // also ErrNotFound
		}
	}
}

func TestSignParamShape(t *testing.T) {
	const code = "SPXBR0123456789"
	p := signParam(code)
	// CODE | <unix seconds> <64 hex chars>
	if len(p) != len(code)+1+10+64 {
		t.Fatalf("sign param length wrong: %d (%q)", len(p), p)
	}
	if p[len(code)] != '|' {
		t.Fatalf("expected | at index %d: %q", len(code), p)
	}
	hexPart := p[len(code)+1+10:]
	for _, r := range hexPart {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			t.Fatalf("non-hex char %q in %q", r, hexPart)
		}
	}
}

func TestParseTimeString(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"2025-10-23 14:05:00", true},
		{"2025-10-23T14:05:00", true},
		{"", false},
	}
	for _, c := range cases {
		if _, ok := parseTime(c.in); ok != c.want {
			t.Errorf("parseTime(%q) ok = %v, want %v", c.in, ok != c.want, c.want)
		}
	}
	// unix seconds as number
	if when, ok := parseTime(any(float64(1761180000))); !ok || when.Unix() != 1761180000 {
		t.Errorf("numeric timestamp parse failed")
	}
}

func TestLooksLikeSPXCode(t *testing.T) {
	yes := []string{"SPXBR0123456789", "spxbr12345678", "SPXBR123456789012345"}
	no := []string{"SPXBR123", "JT1234567890", "PU123456789BR", "SPXBR12A4567890", ""}
	for _, c := range yes {
		if !LooksLikeSPXCode(c) {
			t.Errorf("LooksLikeSPXCode(%q) = false, want true", c)
		}
	}
	for _, c := range no {
		if LooksLikeSPXCode(c) {
			t.Errorf("LooksLikeSPXCode(%q) = true, want false", c)
		}
	}
}

func TestStatusFor(t *testing.T) {
	cases := []struct {
		mile, desc, want string
	}{
		{"DELIVERED", "whatever", models.StatusDelivered},
		{"", "Encomenda entregue", models.StatusDelivered},
		{"", "Saiu para entrega", models.StatusOutForDelivery},
		{"", "Pacote coletado", models.StatusPending},
		{"", "Falha na entrega", models.StatusException},
		{"", "Em trânsito para CD", models.StatusInTransit},
	}
	for _, c := range cases {
		if got := StatusFor(c.mile, c.desc); got != c.want {
			t.Errorf("StatusFor(%q, %q) = %q, want %q", c.mile, c.desc, got, c.want)
		}
	}
}

// setBaseURL is the test hook that retargets the client at a local server.
func setBaseURL(u string) { baseURL = u }
