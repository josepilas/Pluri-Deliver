package engine

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"plurideliver/internal/models"
)

const maxBodyBytes = 4 << 20 // 4 MiB safety cap for carrier responses

var httpClient = &http.Client{Timeout: 15 * time.Second}

// buildCtxTable creates the `ctx` table handed to a Lua adapter call. The
// carrier's trust level drives the permissions:
//
//   - trusted (embedded) adapters that declare requires_key see
//     ctx.settings.aggregator.key;
//   - untrusted (user-installed) adapters always get an empty key — they can
//     still track public endpoints but can never exfiltrate the API key;
//   - untrusted adapters cannot reach loopback / link-local / cloud-metadata
//     addresses (see doRequest).
func buildCtxTable(L *lua.LState, t *models.Tracking, st models.Settings, hint string, c *Carrier) *lua.LTable {
	trusted := c != nil && c.trusted
	ctx := L.NewTable()

	// ctx.http -----------------------------------------------------------------
	httpT := L.NewTable()
	httpT.RawSetString("get", L.NewFunction(func(L *lua.LState) int { return bridgeHTTPGet(L, trusted) }))
	httpT.RawSetString("post", L.NewFunction(func(L *lua.LState) int { return bridgeHTTPPost(L, trusted) }))
	ctx.RawSetString("http", httpT)

	// ctx.json -----------------------------------------------------------------
	jsonT := L.NewTable()
	jsonT.RawSetString("decode", L.NewFunction(bridgeJSONDecode))
	jsonT.RawSetString("encode", L.NewFunction(bridgeJSONEncode))
	ctx.RawSetString("json", jsonT)

	// ctx.now / ctx.iso / ctx.hash ---------------------------------------------
	ctx.RawSetString("now", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LNumber(time.Now().Unix()))
		return 1
	}))
	ctx.RawSetString("iso", L.NewFunction(func(L *lua.LState) int {
		sec := time.Now().Unix()
		if L.GetTop() >= 1 && L.Get(1).Type() == lua.LTNumber {
			sec = int64(L.CheckNumber(1))
		}
		L.Push(lua.LString(time.Unix(sec, 0).UTC().Format(time.RFC3339)))
		return 1
	}))
	ctx.RawSetString("hash", L.NewFunction(func(L *lua.LState) int {
		h := fnv.New32a()
		_, _ = h.Write([]byte(L.CheckString(1)))
		L.Push(lua.LNumber(float64(h.Sum32()%10000) / 10000.0))
		return 1
	}))

	// ctx.added (creation epoch, for demo pacing) + ctx.hint (aggregator hint)
	if t != nil {
		ctx.RawSetString("added", lua.LNumber(t.CreatedAt.Unix()))
	}
	if hint != "" {
		ctx.RawSetString("hint", lua.LString(hint))
	}

	// ctx.settings ---------------------------------------------------------------
	setT := L.NewTable()
	setT.RawSetString("language", lua.LString(st.Language))
	setT.RawSetString("demo_mode", lua.LBool(st.DemoMode))
	aggT := L.NewTable()
	aggT.RawSetString("provider", lua.LString(st.Aggregator.Provider))
	if trusted && c.Info.RequiresKey {
		aggT.RawSetString("key", lua.LString(st.Aggregator.Key))
	} else {
		// Least privilege: adapters that don't need the aggregator (and any
		// user-installed script) only see an empty key.
		aggT.RawSetString("key", lua.LString(""))
	}
	setT.RawSetString("aggregator", aggT)
	ctx.RawSetString("settings", setT)

	return ctx
}

// bridgeHTTPGet implements ctx.http.get(url [, headers]) -> {status, body}.
func bridgeHTTPGet(L *lua.LState, trusted bool) int {
	url := L.CheckString(1)
	headers := headerTable(L, 2)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		pushResponse(L, -1, err.Error())
		return 1
	}
	doRequest(L, req, headers, trusted)
	return 1
}

// bridgeHTTPPost implements ctx.http.post(url [, body [, headers]]) -> {status, body}.
func bridgeHTTPPost(L *lua.LState, trusted bool) int {
	url := L.CheckString(1)
	body := ""
	if L.GetTop() >= 2 && L.Get(2).Type() != lua.LTNil {
		body = L.CheckString(2)
	}
	headers := headerTable(L, 3)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		pushResponse(L, -1, err.Error())
		return 1
	}
	doRequest(L, req, headers, trusted)
	return 1
}

func doRequest(L *lua.LState, req *http.Request, headers map[string]string, trusted bool) {
	// Only plain web protocols are ever dialed from the sandbox.
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		pushResponse(L, -1, "unsupported URL scheme")
		return
	}
	// Untrusted (user-installed) adapters must not reach the machine itself:
	// loopback, link-local (incl. cloud metadata 169.254.169.254) and
	// wildcard addresses are refused. Best-effort against literal hosts —
	// a DNS name that resolves to a private range is still allowed.
	if !trusted && blockedHost(req.URL.Hostname()) {
		pushResponse(L, -1, "host not allowed")
		return
	}
	req.Header.Set("User-Agent", "Pluri-Deliver/1.7 (+https://github.com/josepilas/pluri-deliver)")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		pushResponse(L, -1, err.Error())
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	pushResponse(L, resp.StatusCode, string(b), resp.Header.Values("Set-Cookie"))
}

// parseSetCookie extracts the "name=value" prefix of a Set-Cookie line.
func parseSetCookie(line string) (string, string) {
	if i := strings.IndexByte(line, ';'); i >= 0 {
		line = line[:i]
	}
	i := strings.IndexByte(line, '=')
	if i <= 0 {
		return "", ""
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:])
}

// blockedHost reports whether an untrusted adapter tried to dial a local or
// link-local address.
func blockedHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "localhost" || h == "" {
		return h == "localhost"
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
	}
	// IPv6 literals arrive bracketed.
	if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		if ip := net.ParseIP(strings.Trim(h, "[]")); ip != nil {
			return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
		}
	}
	return false
}

// pushResponse builds the Lua response table handed back to adapters:
// {status, body, cookies}. The cookies table maps every Set-Cookie name to
// its (first) value, so adapters can implement stateful handshakes (UPS's
// XSRF token flow) by re-sending them as a Cookie header explicitly — no
// hidden global cookie jar, no cross-carrier contamination.
func pushResponse(L *lua.LState, status int, body string, setCookies ...[]string) {
	t := L.NewTable()
	t.RawSetString("status", lua.LNumber(status))
	t.RawSetString("body", lua.LString(body))
	t.RawSetString("cookies", L.NewFunction(func(L *lua.LState) int {
		// Lazy: cookies are parsed on first access so adapters that never
		// look at them pay nothing.
		jar := L.NewTable()
		var lines []string
		if len(setCookies) > 0 {
			lines = setCookies[0]
		}
		for _, line := range lines {
			name, value := parseSetCookie(line)
			if name == "" {
				continue
			}
			if jar.RawGetString(name) == lua.LNil {
				jar.RawSetString(name, lua.LString(value))
			}
		}
		L.Push(jar)
		return 1
	}))
	L.Push(t)
}

func headerTable(L *lua.LState, idx int) map[string]string {
	out := map[string]string{}
	if L.GetTop() < idx {
		return out
	}
	if t, ok := L.Get(idx).(*lua.LTable); ok {
		t.ForEach(func(k, v lua.LValue) {
			if k.Type() == lua.LTString {
				out[string(k.(lua.LString))] = v.String()
			}
		})
	}
	return out
}

// bridgeJSONDecode implements ctx.json.decode(str) -> ok, value|err.
// Returns (true, table) on success and (false, message) on failure, so Lua
// adapters can write `local ok, data = ctx.json.decode(body)`.
func bridgeJSONDecode(L *lua.LState) int {
	s := L.CheckString(1)
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		L.Push(lua.LBool(false))
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LBool(true))
	L.Push(toLua(L, v))
	return 2
}

// bridgeJSONEncode implements ctx.json.encode(value) -> str, err.
func bridgeJSONEncode(L *lua.LState) int {
	v := fromLua(L.Get(1))
	b, err := json.Marshal(v)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LString(b))
	return 1
}

// ---- conversions -------------------------------------------------------------

func toLua(L *lua.LState, v any) lua.LValue {
	switch x := v.(type) {
	case nil:
		return lua.LNil
	case bool:
		return lua.LBool(x)
	case string:
		return lua.LString(x)
	case float64:
		return lua.LNumber(x)
	case int:
		return lua.LNumber(x)
	case []any:
		t := L.NewTable()
		for i, e := range x {
			t.RawSetInt(i+1, toLua(L, e))
		}
		return t
	case map[string]any:
		t := L.NewTable()
		for k, e := range x {
			t.RawSetString(k, toLua(L, e))
		}
		return t
	default:
		return lua.LString(fmt.Sprint(x))
	}
}

func fromLua(v lua.LValue) any {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case lua.LBool:
		return bool(x)
	case lua.LNumber:
		return float64(x)
	case lua.LString:
		return string(x)
	case *lua.LTable:
		isArray := true
		n := 0
		x.ForEach(func(k, _ lua.LValue) {
			n++
			if k.Type() != lua.LTNumber || int(k.(lua.LNumber)) != n {
				isArray = false
			}
		})
		if isArray {
			out := make([]any, 0, n)
			x.ForEach(func(_, val lua.LValue) {
				out = append(out, fromLua(val))
			})
			return out
		}
		out := map[string]any{}
		x.ForEach(func(k, val lua.LValue) {
			out[k.String()] = fromLua(val)
		})
		return out
	default:
		return v.String()
	}
}

// unknownDate is the deterministic date assigned to events whose date string
// could not be parsed. Using "now" here would give every fetch a different
// event ID (the ID is derived from the date), so the same occurrence would be
// re-detected as "new" over and over. A fixed epoch keeps the ID stable:
// fetched twice = same event, no duplicates, no repeated notifications.
var unknownDate = time.Unix(0, 0).UTC()

// tableToEvents converts the Lua event list into normalized Go events.
func tableToEvents(t *lua.LTable) []models.TrackingEvent {
	out := []models.TrackingEvent{}
	t.ForEach(func(_, v lua.LValue) {
		et, ok := v.(*lua.LTable)
		if !ok {
			return
		}
		ev := models.TrackingEvent{
			Description: strings.TrimSpace(lua.LVAsString(et.RawGetString("description"))),
			Location:    strings.TrimSpace(lua.LVAsString(et.RawGetString("location"))),
			Source:      strings.TrimSpace(lua.LVAsString(et.RawGetString("source"))),
			Status:      normalizeStatus(lua.LVAsString(et.RawGetString("status"))),
		}
		if ev.Status == models.StatusInfo {
			ev.Status = deriveFromDescription(ev.Description)
		}
		switch d := et.RawGetString("date").(type) {
		case lua.LNumber:
			ev.Date = time.Unix(int64(d), 0).UTC()
		case lua.LString:
			if ts, ok := parseDate(string(d)); ok {
				ev.Date = ts
			} else {
				log.Printf("engine: unparseable event date %q (source %q) — using epoch fallback", string(d), ev.Source)
				ev.Date = unknownDate
			}
		default:
			ev.Date = unknownDate
		}
		if ev.Description == "" {
			ev.Description = ev.Status
		}
		ev.ID = ev.ComputeID()
		out = append(out, ev)
	})
	return out
}

func normalizeStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case models.StatusDelivered, "entregue", "entregue ao destinatário", "delivered to":
		return models.StatusDelivered
	case models.StatusOutForDelivery, "saiu para entrega",
		"objeto saiu para entrega ao destinatário", "outfordelivery":
		return models.StatusOutForDelivery
	case models.StatusInTransit, "em transito", "em trânsito", "objeto em trânsito":
		return models.StatusInTransit
	case models.StatusPending, "pendente", "aguardando", "awaiting", "pre_transit", "info received":
		return models.StatusPending
	case models.StatusException, "exceção", "failed attempt", "tentativa de entrega",
		"delivery failed", "alerta", "returned":
		return models.StatusException
	default:
		return models.StatusInfo
	}
}

// deriveFromDescription guesses a status from the event text when the adapter
// could not classify it (aggregator responses often carry free-form text).
func deriveFromDescription(desc string) string {
	d := strings.ToLower(desc)
	switch {
	case containsAny(d, "delivered", "entregue", "signed for", "received by"):
		return models.StatusDelivered
	case containsAny(d, "out for delivery", "saiu para entrega", "with delivery courier", "on vehicle for delivery"):
		return models.StatusOutForDelivery
	case containsAny(d, "in transit", "em trânsito", "transit", "sorting", "encaminhado", "arrived at", "departed"):
		return models.StatusInTransit
	case containsAny(d, "posted", "postado", "picked up", "collected", "received", "created"):
		return models.StatusPending
	case containsAny(d, "failed", "attempt", "tentativa", "returned", "extraviado", "exception", "alerta"):
		return models.StatusException
	}
	return models.StatusInfo
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// parseDate understands the date formats carriers commonly use.
func parseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339, time.RFC3339Nano,
		"2006-01-02T15:04:05", "2006-01-02T15:04",
		"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02",
		"02/01/2006 15:04:05", "02/01/2006 15:04", "02/01/2006",
		"02-01-2006 15:04:05", "02-01-2006 15:04", "02-01-2006",
		"2006/01/02 15:04:05", "2006/01/02 15:04", "2006/01/02",
		time.RFC1123Z, time.RFC1123,
	}
	for _, l := range layouts {
		if ts, err := time.Parse(l, s); err == nil {
			return ts.UTC(), true
		}
	}
	return time.Time{}, false
}
