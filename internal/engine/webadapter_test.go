package engine

import (
	"embed"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"

	"plurideliver/internal/models"
)

//go:embed carriers/17track-web.lua
var webAdapterFS embed.FS

// realSample is a trimmed shipment object captured from the live
// t.17track.net restapi response (same shape the SPA itself renders).
const realSample = `{
  "shipping_info": {
    "shipper_address": {"country": "CN"},
    "recipient_address": {"country": "US", "state": "AS", "city": "PAGO PAGO", "postal_code": "96799"}
  },
  "latest_status": {"status": "InTransit", "sub_status": "InTransit_Other"},
  "latest_event": {
    "time_iso": "2022-03-25T19:57:00-05:00",
    "description": "Processed Through Facility",
    "location": "CHINA,WUHAN",
    "key_stage": null
  },
  "tracking": {
    "providers": [
      {
        "provider": {"key": 21051, "name": "USPS", "country": "US"},
        "latest_sync_status": "Success",
        "events": [
          {"time_iso": "2022-03-25T19:57:00-05:00", "description": "Processed Through Facility", "location": "CHINA,WUHAN", "key_stage": null},
          {"time_iso": "2022-03-23T09:17:00-05:00", "description": "Acceptance", "location": "CHINA", "key_stage": "PickedUp"}
        ]
      },
      {
        "provider": {"key": 3011, "name": "China Post", "country": "CN"},
        "latest_sync_status": "Success",
        "events": [
          {"time_iso": "2022-03-25T14:30:00+08:00", "description": "Import Customs Clearance", "location": "WUHAN", "key_stage": null},
          {"time_iso": "2022-03-25T14:30:00+08:00", "description": "Import Customs Clearance", "location": "WUHAN", "key_stage": null},
          {"time_iso": "2022-03-24T07:56:00+08:00", "description": "International flight has departed", "location": "CN", "key_stage": null}
        ]
      }
    ]
  }
}`

// TestWebAdapterParsesRealSample feeds the parser a real captured shipment
// and checks dedupe, chronological order, status mapping and source tagging.
func TestWebAdapterParsesRealSample(t *testing.T) {
	src, err := webAdapterFS.ReadFile("carriers/17track-web.lua")
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

	// Decode the sample JSON through the real bridge (validates its contract too).
	ctxTbl := buildCtxTable(L, &models.Tracking{CreatedAt: time.Now().UTC()}, models.DefaultSettings(), "", nil)
	jsonFn := L.GetField(L.GetField(ctxTbl, "json"), "decode")
	if err := L.CallByParam(lua.P{Fn: jsonFn, NRet: 2, Protect: true}, lua.LString(realSample)); err != nil {
		t.Fatalf("json decode bridge failed: %v", err)
	}
	if lua.LVIsFalse(L.Get(-2)) {
		t.Fatalf("sample JSON did not decode: %v", L.Get(-1))
	}
	shipment := L.Get(-1)

	collect := L.GetField(mod, "_collect_events")
	if collect == nil || collect.Type() != lua.LTFunction {
		t.Fatal("_collect_events test hook missing")
	}
	if err := L.CallByParam(lua.P{Fn: collect.(*lua.LFunction), NRet: 1, Protect: true}, shipment); err != nil {
		t.Fatalf("collect_events failed: %v", err)
	}

	events := tableToEvents(L.Get(-1).(*lua.LTable))
	if len(events) != 4 { // 5 raw events, 1 duplicate removed
		t.Fatalf("expected 4 deduped events, got %d", len(events))
	}
	// chronological: first event must be the oldest (March 23)
	if !events[0].Date.Equal(events[0].Date.UTC()) || events[0].Description != "Acceptance" {
		t.Errorf("events not chronological or wrong first event: %+v", events[0])
	}
	last := events[len(events)-1]
	if last.Description != "Processed Through Facility" {
		t.Errorf("newest event should be 'Processed Through Facility', got %q", last.Description)
	}
	// status mapping: Acceptance (PickedUp stage) -> pending
	if events[0].Status != "pending" {
		t.Errorf("Acceptance should map to pending, got %q", events[0].Status)
	}
	// source tagging
	foundUSPS := false
	for _, e := range events {
		if e.Source == "USPS" || e.Source == "China Post" {
			foundUSPS = true
		}
		if e.ID == "" {
			t.Errorf("event missing id: %+v", e)
		}
	}
	if !foundUSPS {
		t.Errorf("provider name not propagated to event source")
	}
}
