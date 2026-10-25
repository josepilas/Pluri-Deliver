// Package engine embeds a sandboxed Lua VM (gopher-lua) and runs carrier
// adapters. Each carrier is a standalone .lua script — hot-pluggable without
// recompiling the app. Built-in scripts are embedded via go:embed and can be
// overridden or extended by dropping scripts into the user carriers folder.
package engine

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"

	"plurideliver/internal/models"
)

//go:embed carriers
var carriersFS embed.FS

// demoCodesPrefix marks tracking codes that always use the demo engine.
const demoCodesPrefix = "DEMO"

// Carrier is a registered Lua adapter.
type Carrier struct {
	Info     models.CarrierInfo
	Src      string
	hasTrack bool
	hasDemo  bool
	trusted  bool // embedded (curated) adapters only; user scripts never are
}

// Sandbox time budgets. Every Lua execution path is bounded so a malicious
// or buggy script can never wedge initialization, carrier detection or a
// tracking refresh. They are vars so tests can tighten them.
var (
	loadTimeout   = 10 * time.Second // script load (register) + chunk body
	detectTimeout = 5 * time.Second  // detect() calls
	trackTimeout  = 30 * time.Second // track()/demo() calls
)

// Engine owns the carrier registry and executes adapter calls on fresh,
// sandboxed Lua states (one state per call: no cross-script contamination).
// It also runs the two user-scriptable policy hooks — polling.lua and
// notify.lua (see scripts.go).
type Engine struct {
	mu         sync.RWMutex
	carriers   map[string]*Carrier
	order      []string
	scriptsDir string            // user overrides for the policy scripts
	scripts    map[string]string // resolved sources (embedded default or user copy)
}

// New loads all embedded carriers, any user-provided overrides, and the
// policy scripts (polling.lua / notify.lua — embedded defaults, user's
// copies in scriptsDir win when present).
func New(userDir, scriptsDir string) *Engine {
	e := &Engine{
		carriers:   map[string]*Carrier{},
		scriptsDir: scriptsDir,
		scripts:    map[string]string{},
	}
	entries, err := carriersFS.ReadDir("carriers")
	if err == nil {
		names := make([]string, 0, len(entries))
		for _, en := range entries {
			if !en.IsDir() && strings.HasSuffix(en.Name(), ".lua") {
				names = append(names, en.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			if src, err := carriersFS.ReadFile("carriers/" + n); err == nil {
				e.register(string(src), true) // embedded scripts are trusted
			}
		}
	}
	if userDir != "" {
		e.LoadUserDir(userDir)
	}
	e.loadScripts()
	return e
}

// LoadUserDir registers or overrides carriers from user .lua scripts.
func (e *Engine) LoadUserDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	for _, en := range entries {
		if !en.IsDir() && strings.HasSuffix(en.Name(), ".lua") {
			names = append(names, en.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		if src, err := os.ReadFile(filepath.Join(dir, n)); err == nil {
			// User-provided scripts are UNTRUSTED: they never receive
			// the aggregator API key and their HTTP calls are
			// restricted (see buildCtxTable / doRequest), so a
			// third-party adapter cannot exfiltrate the key or probe
			// the local machine.
			e.register(string(src), false)
		}
	}
}

// register parses and indexes a carrier script. trusted marks scripts that
// ship with the app (embedded); user-dir scripts are untrusted. The chunk
// body runs under a timeout so a hostile script cannot stall startup with an
// infinite loop before it even returns its module table.
func (e *Engine) register(src string, trusted bool) {
	L := newState()
	defer L.Close()
	ctxL, cancel := context.WithTimeout(context.Background(), loadTimeout)
	defer cancel()
	L.SetContext(ctxL)
	if err := runChunk(L, src); err != nil {
		return // invalid script: skip silently, keep app resilient
	}
	mod := chunkResult(L)
	if mod == nil {
		return
	}
	info, err := readInfo(mod)
	if err != nil || info.ID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.carriers[info.ID]; !exists {
		e.order = append(e.order, info.ID)
	}
	e.carriers[info.ID] = &Carrier{
		Info:     info,
		Src:      src,
		hasTrack: isFunc(L.GetField(mod, "track")),
		hasDemo:  isFunc(L.GetField(mod, "demo")),
		trusted:  trusted,
	}
}

// Carriers returns all carriers ordered builtin -> aggregator -> demo.
func (e *Engine) Carriers() []models.CarrierInfo {
	e.mu.RLock()
	defer e.mu.RUnlock()
	list := make([]models.CarrierInfo, 0, len(e.order))
	for _, id := range sortedByKind(e.order, e.carriers) {
		list = append(list, e.carriers[id].Info)
	}
	return list
}

// Get returns a carrier by id.
func (e *Engine) Get(id string) *Carrier {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if c, ok := e.carriers[id]; ok {
		return c
	}
	return nil
}

// Detect returns carriers whose detect() accepts the code, ordered
// builtin -> aggregator -> demo.
func (e *Engine) Detect(code string) []models.CarrierInfo {
	e.mu.RLock()
	ids := append([]string(nil), sortedByKind(e.order, e.carriers)...)
	carriers := make(map[string]*Carrier, len(e.carriers))
	for k, v := range e.carriers {
		carriers[k] = v
	}
	e.mu.RUnlock()

	out := []models.CarrierInfo{}
	for _, id := range ids {
		c := carriers[id]
		if c == nil {
			continue
		}
		L := newState()
		ctxL, cancel := context.WithTimeout(context.Background(), detectTimeout)
		L.SetContext(ctxL)
		fn := runAndFetch(L, c.Src, "detect")
		if fn == nil {
			cancel()
			L.Close()
			continue
		}
		ok := false
		if err := L.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}, lua.LString(code)); err == nil {
			ok = lua.LVAsBool(L.Get(-1))
		}
		cancel()
		L.Close()
		if ok {
			out = append(out, c.Info)
		}
	}
	// Codes starting with DEMO always suggest the Demo Courier first.
	if isDemoCode(code) {
		for i, c := range out {
			if c.ID == "demo" {
				out = append([]models.CarrierInfo{c}, append(out[:i:i], out[i+1:]...)...)
				break
			}
		}
	}
	return out
}

// webEngineID is the keyless universal fallback adapter.
const webEngineID = "17track-web"

// ErrCaptchaRequired signals that the carrier's portal needs a captcha typed
// by the user before any event can be fetched (Correios does this on every
// query). The service turns it into a "needs verification" state instead of an
// error, and the UI opens the captcha flow.
var ErrCaptchaRequired = errors.New("captcha verification required")

// ErrCPFRequired signals that the carrier's API needs the recipient's CPF
// (J&T Brasil does, on every query). The service turns it into a "needs CPF"
// state instead of an error, and the UI collects the CPF once per tracking.
var ErrCPFRequired = errors.New("recipient CPF required")

// Track fetches events for a tracking, honoring demo mode and falling back to
// the configured aggregator (keyed) and then to the free 17TRACK web engine
// when a built-in endpoint fails.
func (e *Engine) Track(t *models.Tracking, st models.Settings) ([]models.TrackingEvent, error) {
	c := e.Get(t.Carrier)
	if c == nil {
		return nil, fmt.Errorf("unknown carrier %q", t.Carrier)
	}

	demo := st.DemoMode || c.Info.Kind == "demo" || isDemoCode(t.Code)
	if demo {
		target := c
		if !c.hasDemo {
			if g := e.Get("demo"); g != nil && g.hasDemo {
				target = g
			}
		}
		if target.hasDemo {
			return e.runEvents(target, t, st, "", "demo")
		}
		return nil, fmt.Errorf("demo mode is on but carrier %q has no demo generator", t.Carrier)
	}

	if c.hasTrack {
		evs, err := e.runEvents(c, t, st, "", "track")
		if err == nil {
			return evs, nil
		}
		// Smart mix: user-configured keyed aggregator first (most reliable)...
		if st.AggregatorReady() && st.Aggregator.Provider != t.Carrier {
			if agg := e.Get(st.Aggregator.Provider); agg != nil && agg.hasTrack {
				if evs2, err2 := e.runEvents(agg, t, st, t.Carrier, "track"); err2 == nil {
					return evs2, nil
				}
			}
		}
		// ...then the keyless 17TRACK web engine (always available).
		if web := e.Get(webEngineID); web != nil && web.hasTrack && t.Carrier != webEngineID {
			if evs2, err2 := e.runEvents(web, t, st, t.Carrier, "track"); err2 == nil {
				return evs2, nil
			} else {
				return nil, err2
			}
		}
		return nil, err
	}

	// Carrier without a direct endpoint (captcha-walled, login-walled...).
	if st.AggregatorReady() && st.Aggregator.Provider != t.Carrier {
		if agg := e.Get(st.Aggregator.Provider); agg != nil && agg.hasTrack {
			if evs, err := e.runEvents(agg, t, st, t.Carrier, "track"); err == nil {
				return evs, nil
			}
		}
	}
	// Captcha-gated carriers (Correios) are handled by the dedicated Go
	// client with a UI-driven captcha; the keyless web engine cannot see
	// those numbers, so signal the state instead of a dead-end error.
	if c.Info.CaptchaRequired {
		return nil, ErrCaptchaRequired
	}
	// CPF-gated carriers (J&T Brasil) run through their dedicated Go client
	// with a UI-collected CPF; signal the state when it is still missing.
	if c.Info.RequiresCPF {
		return nil, ErrCPFRequired
	}
	if web := e.Get(webEngineID); web != nil && web.hasTrack && t.Carrier != webEngineID {
		return e.runEvents(web, t, st, t.Carrier, "track")
	}
	return nil, fmt.Errorf("carrier %q has no tracking endpoint", c.Info.Name)
}

func isDemoCode(code string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(code)), demoCodesPrefix)
}

// runEvents executes the named function (track|demo) of a carrier in a fresh
// sandboxed state and converts the returned event table.
func (e *Engine) runEvents(c *Carrier, t *models.Tracking, st models.Settings, hint, fnName string) ([]models.TrackingEvent, error) {
	L := newState()
	defer L.Close()

	ctxL, cancel := context.WithTimeout(context.Background(), trackTimeout)
	defer cancel()
	L.SetContext(ctxL)

	ctx := buildCtxTable(L, t, st, hint, c)
	fn := runAndFetch(L, c.Src, fnName)
	if fn == nil {
		return nil, fmt.Errorf("%s: function %q not defined", c.Info.ID, fnName)
	}

	if err := L.CallByParam(lua.P{Fn: fn, NRet: 2, Protect: true}, ctx, lua.LString(t.Code)); err != nil {
		return nil, fmt.Errorf("%s: %v", c.Info.ID, cleanLuaError(err))
	}

	ret := L.Get(-2) // first return: events table
	errv := L.Get(-1)
	if !lua.LVIsFalse(errv) {
		if s, ok := errv.(lua.LString); ok {
			return nil, fmt.Errorf("%s: %s", c.Info.ID, string(s))
		}
		return nil, fmt.Errorf("%s: unknown error", c.Info.ID)
	}
	tbl, ok := ret.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("%s: expected event list", c.Info.ID)
	}
	events := tableToEvents(tbl)
	if len(events) == 0 {
		return nil, fmt.Errorf("%s: no events found", c.Info.ID)
	}
	return events, nil
}

func isFunc(v lua.LValue) bool { return v != nil && v.Type() == lua.LTFunction }

func sortedByKind(order []string, carriers map[string]*Carrier) []string {
	kindRank := map[string]int{"builtin": 0, "aggregator": 1, "demo": 2}
	rank := func(id string) (int, int) {
		k, generic := 3, 0
		if c := carriers[id]; c != nil {
			k = kindRank[c.Info.Kind]
			if c.Info.ID == "generic" {
				generic = 1 // the universal fallback always sorts last within its kind
			}
		}
		return k, generic
	}
	out := append([]string(nil), order...)
	sort.SliceStable(out, func(i, j int) bool {
		ki, gi := rank(out[i])
		kj, gj := rank(out[j])
		if ki != kj {
			return ki < kj
		}
		return gi < gj
	})
	return out
}

// ---- Lua state helpers ----------------------------------------------------

// newState creates a sandboxed Lua state: only base/string/table/math are
// opened; os, io, package and debug never touch the script environment.
func newState() *lua.LState {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	lua.OpenBase(L)
	lua.OpenString(L)
	lua.OpenTable(L)
	lua.OpenMath(L)
	// Base library convenience functions that touch the filesystem or load
	// arbitrary chunks are removed.
	for _, g := range []string{"dofile", "loadfile", "load", "loadstring", "require"} {
		L.SetGlobal(g, lua.LNil)
	}
	L.SetGlobal("print", L.NewFunction(func(L *lua.LState) int {
		n := L.GetTop()
		parts := make([]string, 0, n)
		for i := 1; i <= n; i++ {
			parts = append(parts, L.Get(i).String())
		}
		return 0
	}))
	return L
}

// runChunk loads a script source; the script must end with `return M`.
func runChunk(L *lua.LState, src string) error {
	return L.DoString(src)
}

// chunkResult returns the value left on the stack by the chunk.
func chunkResult(L *lua.LState) *lua.LTable {
	if L.GetTop() == 0 {
		return nil
	}
	if t, ok := L.Get(-1).(*lua.LTable); ok {
		return t
	}
	return nil
}

// runAndFetch runs a script and returns the named function from its module
// table (or nil).
func runAndFetch(L *lua.LState, src, fn string) *lua.LFunction {
	if err := runChunk(L, src); err != nil {
		return nil
	}
	mod := chunkResult(L)
	if mod == nil {
		return nil
	}
	v := L.GetField(mod, fn)
	if f, ok := v.(*lua.LFunction); ok {
		return f
	}
	return nil
}

func readInfo(mod *lua.LTable) (models.CarrierInfo, error) {
	var info models.CarrierInfo
	if mod == nil {
		return info, fmt.Errorf("nil module")
	}
	info.ID = lua.LVAsString(mod.RawGetString("id"))
	info.Name = lua.LVAsString(mod.RawGetString("name"))
	info.Country = lua.LVAsString(mod.RawGetString("country"))
	info.Kind = lua.LVAsString(mod.RawGetString("kind"))
	info.Website = lua.LVAsString(mod.RawGetString("website"))
	info.Note = lua.LVAsString(mod.RawGetString("note"))
	info.RequiresKey = lua.LVAsBool(mod.RawGetString("requires_key"))
	info.CaptchaRequired = lua.LVAsBool(mod.RawGetString("captcha_required"))
	info.RequiresCPF = lua.LVAsBool(mod.RawGetString("requires_cpf"))
	if info.ID == "" || info.Name == "" {
		return info, fmt.Errorf("missing id or name")
	}
	switch info.Kind {
	case "builtin", "aggregator", "demo":
	default:
		info.Kind = "builtin"
	}
	return info, nil
}

func cleanLuaError(err error) string {
	s := err.Error()
	s = strings.ReplaceAll(s, "<string>:", "")
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
