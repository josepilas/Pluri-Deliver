// scripts.go — the user-scriptable halves of Pluri-Deliver's brain.
//
// Two embedded Lua scripts drive decisions that used to be hard-coded Go:
//
//	polling.lua  decide(info) -> minutes until the next refresh of a parcel
//	notify.lua    build(info)  -> the notifications for a batch of new events
//
// Both ship as curated defaults and can be REPLACED by the user: dropping a
// polling.lua / notify.lua into %APPDATA%\Pluri-Deliver\scripts\ overrides
// the embedded version on the next start. Scripts run in the same sandboxed
// states as carrier adapters (time-budgeted, no os/io/network), so a broken
// or hostile custom script can never wedge the app — the callers fall back
// to the built-in Go heuristics on any error.
package engine

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	lua "github.com/yuin/gopher-lua"

	"plurideliver/internal/models"
)

//go:embed scripts/polling.lua scripts/notify.lua
var scriptsFS embed.FS // embedded defaults, overridable from the user scripts folder

// Script names and their expected entry points.
const (
	scriptPolling = "polling" // decide(info) -> minutes
	scriptNotify  = "notify"  // build(info) -> notifications
)

// scriptTimeout bounds every user-script call.
var scriptTimeout = 5 * time.Second

// PollingInfo is the input table handed to polling.lua's decide().
type PollingInfo struct {
	Code              string
	Carrier           string
	Name              string
	Status            string
	Delivered         bool
	AutoUpdate        bool
	Active            bool
	HasError          bool
	EventCount        int
	LastEventAgeHours float64 // -1 when no events yet
	PollMinutes       int
	IdlePollMinutes   int
}

// NotifyInfo is the input table handed to notify.lua's build().
type NotifyInfo struct {
	Code            string
	Name            string
	Carrier         string
	Language        string
	Status          string
	Delivered       bool
	Events          []models.TrackingEvent // the genuinely-new events, oldest first
	PlusMorePattern string                 // i18n "%s (+%d more events)" for the current language
	DeliveredBody   string                 // localized "Your package has been delivered!"
}

// Notification is one entry returned by notify.lua's build().
type Notification struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// loadScripts resolves the effective source of every user-scriptable hook:
// the user's copy (scripts folder) wins over the embedded default.
func (e *Engine) loadScripts() {
	for _, name := range []string{scriptPolling, scriptNotify} {
		src := ""
		if e.scriptsDir != "" {
			if b, err := os.ReadFile(filepath.Join(e.scriptsDir, name+".lua")); err == nil {
				src = string(b)
			}
		}
		if src == "" {
			if b, err := scriptsFS.ReadFile("scripts/" + name + ".lua"); err == nil {
				src = string(b)
			}
		}
		e.scripts[name] = src
	}
}

// RunPolling asks polling.lua how many minutes until the next refresh.
// Errors (missing/broken/hostile script) propagate; callers fall back to the
// built-in cadence. Results are clamped to [1, 1440].
func (e *Engine) RunPolling(info PollingInfo) (int, error) {
	src := e.scripts[scriptPolling]
	if src == "" {
		return 0, errors.New("polling script unavailable")
	}
	L := newState()
	defer L.Close()
	ctxL, cancel := context.WithTimeout(context.Background(), scriptTimeout)
	defer cancel()
	L.SetContext(ctxL)

	fn := runAndFetch(L, src, "decide")
	if fn == nil {
		return 0, errors.New("polling script: decide() not defined")
	}
	if err := L.CallByParam(lua.P{Fn: fn, NRet: 2, Protect: true}, pollingTable(L, info)); err != nil {
		return 0, fmt.Errorf("polling script: %v", cleanLuaError(err))
	}
	ret := L.Get(-2)
	errv := L.Get(-1)
	if !lua.LVIsFalse(errv) {
		return 0, fmt.Errorf("polling script: %v", errv.String())
	}
	if ret.Type() != lua.LTNumber {
		return 0, errors.New("polling script: decide() must return a number")
	}
	m := int(lua.LVAsNumber(ret))
	if m < 1 {
		m = 1
	}
	if m > 1440 {
		m = 1440
	}
	return m, nil
}

// RunNotify asks notify.lua which notifications a batch of new events should
// produce. Errors propagate; callers fall back to the built-in formatting.
func (e *Engine) RunNotify(info NotifyInfo) ([]Notification, error) {
	src := e.scripts[scriptNotify]
	if src == "" {
		return nil, errors.New("notify script unavailable")
	}
	L := newState()
	defer L.Close()
	ctxL, cancel := context.WithTimeout(context.Background(), scriptTimeout)
	defer cancel()
	L.SetContext(ctxL)

	fn := runAndFetch(L, src, "build")
	if fn == nil {
		return nil, errors.New("notify script: build() not defined")
	}
	if err := L.CallByParam(lua.P{Fn: fn, NRet: 2, Protect: true}, notifyTable(L, info)); err != nil {
		return nil, fmt.Errorf("notify script: %v", cleanLuaError(err))
	}
	ret := L.Get(-2)
	errv := L.Get(-1)
	if !lua.LVIsFalse(errv) {
		return nil, fmt.Errorf("notify script: %v", errv.String())
	}
	tbl, ok := ret.(*lua.LTable)
	if !ok {
		return nil, errors.New("notify script: build() must return a table")
	}
	out := []Notification{}
	tbl.ForEach(func(_, v lua.LValue) {
		nt, ok := v.(*lua.LTable)
		if !ok {
			return
		}
		n := Notification{
			Kind:  lua.LVAsString(nt.RawGetString("kind")),
			Title: lua.LVAsString(nt.RawGetString("title")),
			Body:  lua.LVAsString(nt.RawGetString("body")),
		}
		if n.Kind == "" {
			n.Kind = "update"
		}
		if n.Title == "" {
			n.Title = "Pluri-Deliver"
		}
		out = append(out, n)
	})
	return out, nil
}

// ---- input tables -------------------------------------------------------------

func pollingTable(L *lua.LState, info PollingInfo) *lua.LTable {
	t := L.NewTable()
	t.RawSetString("code", lua.LString(info.Code))
	t.RawSetString("carrier", lua.LString(info.Carrier))
	t.RawSetString("name", lua.LString(info.Name))
	t.RawSetString("status", lua.LString(info.Status))
	t.RawSetString("delivered", lua.LBool(info.Delivered))
	t.RawSetString("auto_update", lua.LBool(info.AutoUpdate))
	t.RawSetString("active", lua.LBool(info.Active))
	t.RawSetString("has_error", lua.LBool(info.HasError))
	t.RawSetString("event_count", lua.LNumber(info.EventCount))
	t.RawSetString("last_event_age_hours", lua.LNumber(info.LastEventAgeHours))
	t.RawSetString("poll_minutes", lua.LNumber(info.PollMinutes))
	t.RawSetString("idle_poll_minutes", lua.LNumber(info.IdlePollMinutes))
	return t
}

func notifyTable(L *lua.LState, info NotifyInfo) *lua.LTable {
	t := L.NewTable()
	t.RawSetString("code", lua.LString(info.Code))
	t.RawSetString("name", lua.LString(info.Name))
	t.RawSetString("carrier", lua.LString(info.Carrier))
	t.RawSetString("language", lua.LString(info.Language))
	t.RawSetString("status", lua.LString(info.Status))
	t.RawSetString("delivered", lua.LBool(info.Delivered))

	evs := L.NewTable()
	for i, ev := range info.Events {
		et := L.NewTable()
		et.RawSetString("status", lua.LString(ev.Status))
		et.RawSetString("description", lua.LString(ev.Description))
		et.RawSetString("location", lua.LString(ev.Location))
		et.RawSetString("date", lua.LString(ev.Date.UTC().Format(time.RFC3339)))
		et.RawSetString("source", lua.LString(ev.Source))
		evs.RawSetInt(i+1, et)
	}
	t.RawSetString("events", evs)

	// The i18n helper table: default scripts keep the app's translations,
	// custom scripts can ignore it and write their own wording.
	h := L.NewTable()
	h.RawSetString("plus_more", L.NewFunction(func(L *lua.LState) int {
		body := L.CheckString(1)
		n := int(L.CheckNumber(2))
		L.Push(lua.LString(fmt.Sprintf(info.PlusMorePattern, body, n)))
		return 1
	}))
	h.RawSetString("delivered_body", lua.LString(info.DeliveredBody))
	t.RawSetString("t", h)
	return t
}
