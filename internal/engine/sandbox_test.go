package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"

	"plurideliver/internal/models"
)

// writeUserScript drops a .lua file into a carriers dir and returns the dir.
func writeUserScript(t *testing.T, name, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestRegisterInfiniteLoopIsBounded pins the sandbox budget on script loading:
// a user script whose chunk body never returns must not wedge engine
// construction — it times out and is skipped.
func TestRegisterInfiniteLoopIsBounded(t *testing.T) {
	old := loadTimeout
	loadTimeout = 200 * time.Millisecond
	defer func() { loadTimeout = old }()

	src := `local M = { id = "loopcarrier", name = "Loop", kind = "builtin" }
while true do end
return M`
	dir := writeUserScript(t, "loopcarrier.lua", src)

	start := time.Now()
	e := New(dir, "")
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("register not bounded: took %v", took)
	}
	if e.Get("loopcarrier") != nil {
		t.Fatal("timed-out script must not register")
	}
	// embedded carriers still load fine after the incident
	if e.Get("correios") == nil {
		t.Fatal("embedded carriers must survive a bad user script")
	}
}

// TestDetectInfiniteLoopIsBounded pins the budget on detect(): carrier
// detection must stay responsive even with a hostile script.
func TestDetectInfiniteLoopIsBounded(t *testing.T) {
	old := detectTimeout
	detectTimeout = 200 * time.Millisecond
	defer func() { detectTimeout = old }()

	src := `local M = { id = "loopdetect", name = "LoopDetect", kind = "builtin" }
function M.detect(code)
  while true do end
  return false
end
return M`
	dir := writeUserScript(t, "loopdetect.lua", src)
	e := New(dir, "")
	if e.Get("loopdetect") == nil {
		t.Fatal("script should register (its body is fine)")
	}

	start := time.Now()
	out := e.Detect("ANYCODE")
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("detect not bounded: took %v", took)
	}
	for _, c := range out {
		if c.ID == "loopdetect" {
			t.Fatal("timed-out detect must not match")
		}
	}
}

// TestAggregatorKeyVisibility pins per-adapter permissions on the ctx table:
// only trusted (embedded) adapters that declare requires_key ever see the
// aggregator API key. User scripts and ordinary carriers get an empty string.
func TestAggregatorKeyVisibility(t *testing.T) {
	st := models.DefaultSettings()
	st.Aggregator = models.Aggregator{Provider: "17track", Key: "SUPERSECRET"}

	L := newState()
	defer L.Close()

	trustedKeyed := &Carrier{trusted: true, Info: models.CarrierInfo{ID: "17track", RequiresKey: true}}
	trustedKeyless := &Carrier{trusted: true, Info: models.CarrierInfo{ID: "correios", RequiresKey: false}}
	untrustedKeyed := &Carrier{trusted: false, Info: models.CarrierInfo{ID: "evil", RequiresKey: true}}

	keyOf := func(c *Carrier) string {
		tbl := buildCtxTable(L, &models.Tracking{}, st, "", c)
		agg := L.GetField(tbl, "settings")
		return lua.LVAsString(L.GetField(agg, "aggregator").(*lua.LTable).RawGetString("key"))
	}

	if got := keyOf(trustedKeyed); got != "SUPERSECRET" {
		t.Errorf("trusted requires_key adapter must see the key, got %q", got)
	}
	if got := keyOf(trustedKeyless); got != "" {
		t.Errorf("keyless trusted adapter must not see the key, got %q", got)
	}
	if got := keyOf(untrustedKeyed); got != "" {
		t.Errorf("user-installed adapter must NEVER see the key, got %q", got)
	}
	if got := keyOf(nil); got != "" {
		t.Errorf("unknown adapter must not see the key, got %q", got)
	}
}

// TestUserScriptCannotExfiltrateKey runs a hostile user adapter through the
// same execution path track() uses: it declares requires_key and echoes
// ctx.settings.aggregator.key inside its error message — the leaked value
// must be empty, not the real key. (runEvents is called directly so the
// aggregator fallback chain does not touch the network from a unit test.)
func TestUserScriptCannotExfiltrateKey(t *testing.T) {
	src := `local M = {
  id = "leaky", name = "Leaky Carrier", kind = "builtin",
  requires_key = true, -- must NOT grant the key: user scripts are untrusted
}
function M.detect(code) return true end
function M.track(ctx, code)
  return nil, "leaked=[" .. tostring(ctx.settings.aggregator.key) .. "]"
end
return M`
	dir := writeUserScript(t, "leaky.lua", src)
	e := New(dir, "")

	c := e.Get("leaky")
	if c == nil {
		t.Fatal("leaky script should register")
	}
	st := models.DefaultSettings()
	st.Aggregator = models.Aggregator{Provider: "17track", Key: "SUPERSECRET"}
	tr := &models.Tracking{ID: "leak1", Code: "X1", Carrier: "leaky", CreatedAt: time.Now().UTC()}

	_, err := e.runEvents(c, tr, st, "", "track")
	if err == nil {
		t.Fatal("track should have failed (script returns an error)")
	}
	if strings.Contains(err.Error(), "SUPERSECRET") {
		t.Fatalf("user script exfiltrated the aggregator key: %v", err)
	}
	if !strings.Contains(err.Error(), "leaked=[]") {
		t.Fatalf("user script should see an empty key, got: %v", err)
	}

	// The trusted keyed aggregator (embedded 17track.lua) still receives it.
	agg := e.Get("17track")
	if agg == nil {
		t.Fatal("embedded 17track adapter missing")
	}
	L := newState()
	defer L.Close()
	ctxTbl := buildCtxTable(L, tr, st, "", agg)
	keyGot := lua.LVAsString(L.GetField(L.GetField(ctxTbl, "settings"), "aggregator").(*lua.LTable).RawGetString("key"))
	if keyGot != "SUPERSECRET" {
		t.Fatalf("trusted keyed aggregator lost access to the key, got %q", keyGot)
	}
}

// TestUnparseableDateKeepsStableID pins the duplicate-event fix: an event
// whose date string cannot be parsed gets the deterministic epoch fallback,
// so fetching it twice yields the SAME id instead of two "different" events.
func TestUnparseableDateKeepsStableID(t *testing.T) {
	build := func() []models.TrackingEvent {
		L := newState()
		defer L.Close()
		if err := L.DoString(`return {
  { date = "not-a-date", status = "in_transit", description = "Weird format event", location = "X" },
}`); err != nil {
			t.Fatalf("lua chunk failed: %v", err)
		}
		tbl, ok := L.Get(-1).(*lua.LTable)
		if !ok {
			t.Fatal("chunk did not return a table")
		}
		return tableToEvents(tbl)
	}

	first := build()
	if len(first) != 1 {
		t.Fatalf("expected 1 event, got %d", len(first))
	}
	second := build()
	if len(second) != 1 {
		t.Fatalf("expected 1 event, got %d", len(second))
	}
	if first[0].ID != second[0].ID {
		t.Fatalf("unparseable dates must produce stable IDs: %q vs %q", first[0].ID, second[0].ID)
	}
	if !first[0].Date.Equal(time.Unix(0, 0).UTC()) {
		t.Fatalf("unparseable dates must fall back to the epoch, got %+v", first[0].Date)
	}
}

// TestBlockedHost pins the network boundary for untrusted adapters: the local
// machine and cloud-metadata endpoints are off limits; public hosts (and
// private LANs, for self-hosted aggregators) stay reachable.
func TestBlockedHost(t *testing.T) {
	blocked := []string{
		"localhost", "LOCALHOST",
		"127.0.0.1",
		"::1", "[::1]",
		"169.254.169.254", // cloud metadata
		"fe80::1",
		"0.0.0.0",
	}
	for _, h := range blocked {
		if !blockedHost(h) {
			t.Errorf("host %q should be blocked for untrusted adapters", h)
		}
	}
	allowed := []string{
		"global.cainiao.com",
		"t.17track.net",
		"api.17track.net",
		"192.168.0.1",        // private LAN (self-hosted aggregators) stays allowed
		"[2606:4700::1]:443", // public IPv6 literal with brackets
	}
	for _, h := range allowed {
		if blockedHost(h) {
			t.Errorf("host %q must stay reachable", h)
		}
	}
}
