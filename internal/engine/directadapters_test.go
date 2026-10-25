package engine

import (
	"embed"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"

	"plurideliver/internal/models"
)

// The direct-adapter fixtures below use the exact response schemas verified
// against the live endpoints / official tracker bundles:
//   - amazon : GET  https://track.amazon.com/api/tracker/{code}   (captured live)
//   - dhl    : GET  https://www.dhl.com/utapi?trackingNumber=...  (scrapfly reference)
//   - ups    : POST https://www.ups.com/track/api/Track/GetStatus  (pacz reference)
//   - loggi  : POST https://www.loggi.com/graphql                 (tracker-web-ui bundle)

//go:embed carriers/amazon.lua
var amazonFS embed.FS

//go:embed carriers/dhl.lua
var dhlFS embed.FS

//go:embed carriers/ups.lua
var upsFS embed.FS

//go:embed carriers/loggi.lua
var loggiFS embed.FS

// loadAdapter runs a carrier script and returns its module table.
func loadAdapter(t *testing.T, fs embed.FS, name string) *lua.LTable {
	t.Helper()
	src, err := fs.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	L := newState()
	defer L.Close()
	if err := L.DoString(string(src)); err != nil {
		t.Fatalf("adapter script failed: %v", err)
	}
	mod, ok := L.Get(-1).(*lua.LTable)
	if !ok {
		t.Fatal("adapter did not return a module table")
	}
	return mod
}

// decodeJSON runs the real bridge json.decode on a sample and returns the
// Lua value.
func decodeJSON(t *testing.T, L *lua.LState, sample string) lua.LValue {
	t.Helper()
	ctxTbl := buildCtxTable(L, &models.Tracking{CreatedAt: time.Now().UTC()}, models.DefaultSettings(), "", nil)
	jsonFn := L.GetField(L.GetField(ctxTbl, "json"), "decode")
	if err := L.CallByParam(lua.P{Fn: jsonFn, NRet: 2, Protect: true}, lua.LString(sample)); err != nil {
		t.Fatalf("json decode bridge failed: %v", err)
	}
	if lua.LVIsFalse(L.Get(-2)) {
		t.Fatalf("sample JSON did not decode: %v", L.Get(-1))
	}
	return L.Get(-1)
}

// callCollect invokes M._collect_events(data) and converts the result to
// normalized Go events.
func callCollect(t *testing.T, L *lua.LState, mod *lua.LTable, data lua.LValue) []models.TrackingEvent {
	t.Helper()
	fn := L.GetField(mod, "_collect_events")
	if !isFunc(fn) {
		t.Fatal("adapter does not expose _collect_events")
	}
	if err := L.CallByParam(lua.P{Fn: fn, NRet: 2, Protect: true}, data); err != nil {
		t.Fatalf("_collect_events failed: %v", err)
	}
	res := L.Get(-2)
	errv := L.Get(-1)
	if !lua.LVIsFalse(errv) {
		t.Fatalf("_collect_events returned error: %v", errv)
	}
	tbl, ok := res.(*lua.LTable)
	if !ok {
		t.Fatal("_collect_events did not return a table")
	}
	return tableToEvents(tbl)
}

// ---------------------------------------------------------------------------
// Amazon (track.amazon.com public tracker)

// amazonNotFound is the LIVE captured response for an unknown number
// (TBA123456789000, captured 2026-10 from the sandbox).
const amazonNotFound = `{"shipmentProfileType":null,"progressTracker":"{\"progressMeter\":null,\"customerRescheduleRequestInfo\":null,\"errors\":[{\"errorCode\":\"TRACKING_ID_NOT_FOUND\",\"errorMessage\":\"INVALID TRACKING_ID\"}],\"summary\":{\"status\":null,\"metadata\":{},\"proofOfDelivery\":null,\"containerStatusTags\":null,\"valueAddedServices\":null,\"trackingDetailCodes\":null,\"signedStatus\":null},\"expectedDeliveryDate\":null,\"legType\":null,\"hasDeliveryDelayed\":false,\"trackerSource\":\"UNKNOWN\"}","eventHistory":null,"addresses":null,"eligibleActions":null,"proofOfDeliveryImage":null,"shipperId":null,"packageMarketplaceDetail":null,"shipperBrandingDetails":null,"geocodeDetails":null,"packageAuthenticationDetails":null,"deliveryLocationDetails":null,"recipientDetails":null,"claimYourPackageEligibility":null,"pinsAndBridgesDetails":null,"otpDetails":null,"shipperDetails":null,"d2CDetails":{"isD2CPackage":false,"counterDetails":"null"},"signatureInfo":null,"recipientEligibleActions":null,"returnDetails":null,"predictiveDeliveryWindowDetails":null}`

// amazonFound mirrors the schema the Amazon Shipping tracker bundle renders
// (eventHistory day groups -> events[] with label/eventTimeInMs/city/state/
// country/eventCode; verified against default_chunk_3_main.js).
const amazonFound = `{
  "shipmentProfileType": "REX",
  "progressTracker": "{\"progressMeter\":null,\"errors\":[],\"summary\":{\"status\":\"TRANSIT\"},\"expectedDeliveryDate\":\"2026-10-30\"}",
  "eventHistory": [
    {"time": "2026-10-24T00:00:00.000Z", "events": [
      {"label": "Delivered, handed directly to a resident", "eventTimeInMs": 1792915200000, "city": "São Paulo", "state": "SP", "country": "BR", "eventCode": "DELIVERED"}
    ]},
    {"time": "2026-10-22T00:00:00.000Z", "events": [
      {"label": "Package arrived at delivery station", "eventTimeInMs": 1792742400000, "city": "São Paulo", "state": "SP", "country": "BR", "eventCode": "ARRIVED_AT_FACILITY"},
      {"label": "Package has left the facility", "eventTimeInMs": 1792656000000, "city": "Newark", "state": "NJ", "country": "US", "eventCode": "DEPARTED_FACILITY"}
    ]}
  ]
}`

func TestAmazonAdapterParsesFoundSample(t *testing.T) {
	L := newState()
	defer L.Close()
	mod := loadAdapter(t, amazonFS, "carriers/amazon.lua")
	events := callCollect(t, L, mod, decodeJSON(t, L, amazonFound))
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	// Chronological: the Newark departure comes first.
	if events[0].Location != "Newark, NJ, US" {
		t.Errorf("oldest event location = %q", events[0].Location)
	}
	if !events[0].Date.Equal(time.Unix(1792656000, 0).UTC()) {
		t.Errorf("oldest event date = %v", events[0].Date)
	}
	last := events[len(events)-1]
	if last.Status != models.StatusDelivered {
		t.Errorf("delivered status = %q", last.Status)
	}
	if last.Description != "Delivered, handed directly to a resident" {
		t.Errorf("delivered description = %q", last.Description)
	}
	if last.Source != "Amazon" {
		t.Errorf("source = %q", last.Source)
	}
}

func TestAmazonAdapterReportsNotFound(t *testing.T) {
	L := newState()
	defer L.Close()
	mod := loadAdapter(t, amazonFS, "carriers/amazon.lua")
	fn := L.GetField(mod, "_collect_events")
	if !isFunc(fn) {
		t.Fatal("adapter does not expose _collect_events")
	}
	// The bridge's json.decode is passed as the stringified-progressTracker
	// decoder, exactly like track() does.
	ctxTbl := buildCtxTable(L, &models.Tracking{CreatedAt: time.Now().UTC()}, models.DefaultSettings(), "", nil)
	decodeFn := L.GetField(L.GetField(ctxTbl, "json"), "decode")
	if err := L.CallByParam(lua.P{Fn: fn, NRet: 2, Protect: true}, decodeJSON(t, L, amazonNotFound), decodeFn); err != nil {
		t.Fatalf("_collect_events failed: %v", err)
	}
	if lua.LVIsFalse(L.Get(-1)) {
		t.Fatalf("expected an error return for TRACKING_ID_NOT_FOUND, got %v", L.Get(-1))
	}
	if got := lua.LVAsString(L.Get(-1)); !containsGo(got, "TRACKING_ID_NOT_FOUND") {
		t.Fatalf("error should mention TRACKING_ID_NOT_FOUND, got %q", got)
	}
	// Malformed payloads must not crash the parser.
	if err := L.CallByParam(lua.P{Fn: fn, NRet: 2, Protect: true}, lua.LNil, decodeFn); err != nil {
		t.Fatalf("_collect_events(nil) should not crash: %v", err)
	}
}

// ---------------------------------------------------------------------------
// DHL (www.dhl.com/utapi)

// dhlSample mirrors the utapi response shape (shipments[].events[] with
// timestamp/statusCode/description/location.address.addressLocality — the
// fields scrapfly's maintained DHL scraper reads; sample data adapted from
// their captured tracking.json for LBAA19526).
const dhlSample = `{
  "shipments": [
    {
      "id": "LBAA19526",
      "status": {"statusCode": "transit", "description": "Actual Vessel Departure"},
      "details": {"dgf:routes": [{"dgf:estimatedArrivalDate": "2026-08-23"}]},
      "origin": {"address": {"addressLocality": "Purfleet"}},
      "destination": {"address": {"addressLocality": "Shanghai"}},
      "events": [
        {"timestamp": "2026-08-23T00:00:00+08:00", "statusCode": "transit", "description": "Estimated Vessel Arrival (Last Updated)", "location": {"address": {"addressLocality": "Shanghai"}}},
        {"timestamp": "2026-06-27T10:54:00+01:00", "statusCode": "transit", "description": "Actual Vessel Departure", "location": {"address": {"addressLocality": "Port of Felixstowe"}}},
        {"timestamp": "2026-06-15T12:12:00+01:00", "statusCode": "pre-transit", "description": "CFS/CY Cut-Off Date", "location": {"address": {"addressLocality": "Purfleet"}}}
      ]
    }
  ]
}`

const dhlBlocked = `<!DOCTYPE html><html><head><title>Request blocked</title></head></html>`

func TestDHLAdapterParsesSample(t *testing.T) {
	L := newState()
	defer L.Close()
	mod := loadAdapter(t, dhlFS, "carriers/dhl.lua")
	events := callCollect(t, L, mod, decodeJSON(t, L, dhlSample))
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Location != "Purfleet" {
		t.Errorf("chronological order broken: first location = %q", events[0].Location)
	}
	if events[0].Status != models.StatusPending {
		t.Errorf("pre-transit should map to pending, got %q", events[0].Status)
	}
	last := events[len(events)-1]
	if last.Location != "Shanghai" {
		t.Errorf("newest location = %q", last.Location)
	}
	if last.Source != "DHL" {
		t.Errorf("source = %q", last.Source)
	}

	// A response without shipments (e.g. the Akamai block page, which fails
	// json decoding earlier) surfaces as an error here.
	L.Push(L.NewTable())
	if err := L.CallByParam(lua.P{Fn: L.GetField(mod, "_collect_events"), NRet: 2, Protect: true}, L.Get(-1)); err != nil {
		t.Fatalf("_collect_events(empty) failed: %v", err)
	}
	if lua.LVIsFalse(L.Get(-1)) {
		t.Fatalf("empty response should error, got %v", L.Get(-1))
	}
}

func TestDHLDetect(t *testing.T) {
	L := newState()
	defer L.Close()
	mod := loadAdapter(t, dhlFS, "carriers/dhl.lua")
	detectFn := L.GetField(mod, "detect")
	positive := []string{"JVGL066953000011391733", "11898773100", "1234567890", "3SABCD1234567", "JJD000390003602115001", "LBAA19526", "GM605859449200317621"}
	negative := []string{"SS123456789BR", "1Z12345E0205271688", "SPXBR12345678", "123456789", "123456789012", "LOG123456789"}
	for _, c := range positive {
		if err := L.CallByParam(lua.P{Fn: detectFn, NRet: 1, Protect: true}, lua.LString(c)); err != nil {
			t.Fatalf("detect(%s): %v", c, err)
		}
		if !lua.LVAsBool(L.Get(-1)) {
			t.Errorf("detect(%q) should accept", c)
		}
	}
	for _, c := range negative {
		if err := L.CallByParam(lua.P{Fn: detectFn, NRet: 1, Protect: true}, lua.LString(c)); err != nil {
			t.Fatalf("detect(%s): %v", c, err)
		}
		if lua.LVAsBool(L.Get(-1)) {
			t.Errorf("detect(%q) should reject", c)
		}
	}
}

// ---------------------------------------------------------------------------
// UPS (ups.com/track/api/Track/GetStatus)

// upsSample mirrors the GetStatus trackDetails shape (date DD/MM/YYYY +
// time HH:MM + activityScan + location — the en_GB locale contract used by
// the public pacz tracker and sauladam/shipment-tracker).
const upsSample = `{
  "statusCode": 200,
  "statusText": "OK",
  "trackDetails": [
    {
      "trackingNumber": "1Z12345E0205271688",
      "deliveredDate": "24/07/2017",
      "deliveredTime": "10:30",
      "receivedBy": "M. SILVA",
      "shipmentProgressActivities": [
        {"date": "24/07/2017", "time": "10:30", "location": "SAO PAULO, BR", "activityScan": "Delivered", "milestone": {"name": "delivered"}},
        {"date": "24/07/2017", "time": "07:55", "location": "SAO PAULO, BR", "activityScan": "Out For Delivery", "milestone": {}},
        {"date": "22/07/2017", "time": "22:41", "location": "LOUISVILLE, KY, US", "activityScan": "Departed from Facility", "milestone": {}},
        {"date": "22/07/2017", "time": "14:10", "location": "LOUISVILLE, KY, US", "activityScan": "Origin Scan", "milestone": {}},
        {"date": "21/07/2017", "time": "18:02", "location": "US", "activityScan": "Order Processed: Ready for UPS", "milestone": {}}
      ]
    }
  ]
}`

func TestUPSAdapterParsesSample(t *testing.T) {
	L := newState()
	defer L.Close()
	mod := loadAdapter(t, upsFS, "carriers/ups.lua")
	data := decodeJSON(t, L, upsSample)
	details := L.GetField(data, "trackDetails").(*lua.LTable)
	events := callCollect(t, L, mod, details.RawGetInt(1))
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}
	first, last := events[0], events[len(events)-1]
	if first.Description != "Order Processed: Ready for UPS" {
		t.Errorf("chronological order broken: first = %q", first.Description)
	}
	if first.Status != models.StatusPending {
		t.Errorf("order processed should map to pending, got %q", first.Status)
	}
	if last.Status != models.StatusDelivered {
		t.Errorf("delivered scan should map to delivered, got %q", last.Status)
	}
	if last.Source != "UPS" {
		t.Errorf("source = %q", last.Source)
	}
}

func TestUPSDetect(t *testing.T) {
	L := newState()
	defer L.Close()
	mod := loadAdapter(t, upsFS, "carriers/ups.lua")
	detectFn := L.GetField(mod, "detect")
	positive := []string{"1Z12345E0205271688", "1ZWX0692YP40636269", "1Z9999999999999999", "1Z12345E6690"}
	negative := []string{"JVGL066953000011391733", "SS123456789BR", "LOG123456789", "TBA123456789000"}
	for _, c := range positive {
		if err := L.CallByParam(lua.P{Fn: detectFn, NRet: 1, Protect: true}, lua.LString(c)); err != nil {
			t.Fatalf("detect(%s): %v", c, err)
		}
		if !lua.LVAsBool(L.Get(-1)) {
			t.Errorf("detect(%q) should accept", c)
		}
	}
	for _, c := range negative {
		if err := L.CallByParam(lua.P{Fn: detectFn, NRet: 1, Protect: true}, lua.LString(c)); err != nil {
			t.Fatalf("detect(%s): %v", c, err)
		}
		if lua.LVAsBool(L.Get(-1)) {
			t.Errorf("detect(%q) should reject", c)
		}
	}
}

// ---------------------------------------------------------------------------
// Loggi (www.loggi.com/graphql — statusQuery)

// loggiSample mirrors the statusQuery packageTracker payload (field names
// verified against Loggi's tracker-web-ui bundle prop-types).
const loggiSample = `{
  "data": {
    "packageTracker": {
      "eta": 1793000000,
      "packageInfo": {"packageId": 420311, "trackingKey": "LOG123456789", "companyName": "Loja Exemplo"},
      "redispatchInfo": {"trackingCode": null, "maxPickupTime": null},
      "currentStatus": {"error": false, "inProgress": true, "destination": "São Paulo", "type": 3},
      "shipperStatus": {"code": "OUT_FOR_DELIVERY", "label": "Saiu para entrega", "description": "Seu pedido sau para entrega", "actionRequired": null},
      "destinationInfo": {"firstLine": "Rua das Flores, 123 - São Paulo/SP"},
      "canAuthenticate": false
    }
  }
}`

func TestLoggiAdapterParsesSample(t *testing.T) {
	L := newState()
	defer L.Close()
	mod := loadAdapter(t, loggiFS, "carriers/loggi.lua")
	data := decodeJSON(t, L, loggiSample)
	pt := L.GetField(L.GetField(data, "data"), "packageTracker")
	L.Push(pt)
	events := callCollect(t, L, mod, L.Get(-1))
	if len(events) != 1 {
		t.Fatalf("status-only API: expected exactly 1 event, got %d", len(events))
	}
	ev := events[0]
	if ev.Status != models.StatusOutForDelivery {
		t.Errorf("status = %q (want out_for_delivery)", ev.Status)
	}
	if ev.Location != "Rua das Flores, 123 - São Paulo/SP" {
		t.Errorf("location = %q", ev.Location)
	}
	if ev.Source != "Loggi" {
		t.Errorf("source = %q", ev.Source)
	}
}

func TestLoggiDetect(t *testing.T) {
	L := newState()
	defer L.Close()
	mod := loadAdapter(t, loggiFS, "carriers/loggi.lua")
	detectFn := L.GetField(mod, "detect")
	positive := []string{"LOG123456789", "LGP12345678", "log123456789", "LGCAB12CD34E"}
	negative := []string{"LG", "LOG123", "SS123456789BR", "1Z12345E0205271688", "LOG12345678901234567"}
	for _, c := range positive {
		if err := L.CallByParam(lua.P{Fn: detectFn, NRet: 1, Protect: true}, lua.LString(c)); err != nil {
			t.Fatalf("detect(%s): %v", c, err)
		}
		if !lua.LVAsBool(L.Get(-1)) {
			t.Errorf("detect(%q) should accept", c)
		}
	}
	for _, c := range negative {
		if err := L.CallByParam(lua.P{Fn: detectFn, NRet: 1, Protect: true}, lua.LString(c)); err != nil {
			t.Fatalf("detect(%s): %v", c, err)
		}
		if lua.LVAsBool(L.Get(-1)) {
			t.Errorf("detect(%q) should reject", c)
		}
	}
}

// ---------------------------------------------------------------------------
// Bridge cookie support (UPS handshake requirement)

func TestBridgeExposesSetCookies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "X-XSRF-TOKEN-ST=abc-123; Path=/; Secure; HttpOnly")
		w.Header().Add("Set-Cookie", "session=xyz; Path=/")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	L := newState()
	defer L.Close()
	// Trusted (embedded) carrier so the sandbox may dial the loopback test
	// server (untrusted adapters are blocked from local addresses).
	ctx := buildCtxTable(L, &models.Tracking{CreatedAt: time.Now().UTC()}, models.DefaultSettings(), "", &Carrier{trusted: true, Info: models.CarrierInfo{ID: "test"}})
	getFn := L.GetField(L.GetField(ctx, "http"), "get")
	if err := L.CallByParam(lua.P{Fn: getFn, NRet: 1, Protect: true}, lua.LString(srv.URL)); err != nil {
		t.Fatalf("http.get failed: %v", err)
	}
	resp := L.Get(-1).(*lua.LTable)
	if n := L.GetField(resp, "status"); n != lua.LNumber(200) {
		t.Fatalf("status = %v", n)
	}
	cookiesFn := L.GetField(resp, "cookies")
	if !isFunc(cookiesFn) {
		t.Fatal("response.cookies is not callable")
	}
	if err := L.CallByParam(lua.P{Fn: cookiesFn, NRet: 1, Protect: true}); err != nil {
		t.Fatalf("cookies() failed: %v", err)
	}
	jar, ok := L.Get(-1).(*lua.LTable)
	if !ok {
		t.Fatal("cookies() did not return a table")
	}
	if v := L.GetField(jar, "X-XSRF-TOKEN-ST"); v != lua.LString("abc-123") {
		t.Errorf("X-XSRF-TOKEN-ST = %v", v)
	}
	if v := L.GetField(jar, "session"); v != lua.LString("xyz") {
		t.Errorf("session = %v", v)
	}
}

func containsGo(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && contains(s, sub))
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
