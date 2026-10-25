// Package cainiao talks to Cainiao's global tracking portal
// (global.cainiao.com) the same way its public web page does:
//
//	GET /global/detail.json?mailNos=CODE&lang=<en|pt-BR|...>
//
// The endpoint is keyless (no login, no captcha, no signature) and returns the
// combined origin→destination history of any parcel routed through the
// Cainiao/Alibaba logistics network — which covers essentially every
// AliExpress-style import that lands in Brazil with an S10 ...BR code handled
// by Correios as last-mile carrier. Events come localized per the requested
// language (pt-BR answers use Correios' own wording, e.g. "Objeto em trânsito -
// por favor aguarde").
//
// For numbers Cainiao does not know (domestic-only Correios parcels), the
// answer carries an empty detailList — the caller then falls back to the
// universal 17TRACK web engine.
package cainiao

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"plurideliver/internal/models"
)

// baseURL is a var so tests can retarget the client at a local server.
var baseURL = "https://global.cainiao.com"

const (
	browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

	maxBody = 4 << 20 // 4 MiB
)

// Sentinel errors surfaced to the service layer.
var (
	// ErrNotFound means Cainiao has no event data for the number (empty
	// detailList) — the caller should try the next source.
	ErrNotFound = errors.New("cainiao: no data for this number")
	// ErrEmptyResult means the number is known but has no events yet.
	ErrEmptyResult = errors.New("cainiao: no events yet")
)

// Client queries the Cainiao global tracking endpoint. Safe for concurrent use.
type Client struct {
	hc *http.Client
}

// New builds a client.
func New() *Client {
	return &Client{hc: &http.Client{Timeout: 30 * time.Second}}
}

// Result is the parsed outcome of a tracking query.
type Result struct {
	Code         string
	Events       []models.TrackingEvent
	Origin       string // e.g. "Mainland China"
	Destination  string // e.g. "Brazil"
	LastCarrier  string // last-mile carrier, e.g. "Correios(BR)"
	OverallState string // raw Cainiao status (DELIVERING, SIGN_Y, ...)
}

// ---- response envelope ----------------------------------------------------------

type envelope struct {
	Module  []module `json:"module"`
	Success bool     `json:"success"`
}

type module struct {
	MailNo        string       `json:"mailNo"`
	Origin        string       `json:"originCountry"`
	Dest          string       `json:"destCountry"`
	Status        string       `json:"status"`
	MailNoSource  string       `json:"mailNoSource"`
	NoTracking    string       `json:"noTrackingDataDesc"`
	DestCp        *cpInfo      `json:"destCpInfo"`
	DetailList    []detailItem `json:"detailList"`
	CombinedTrace *detailItem  `json:"globalCombinedLogisticsTraceDTO"`
	LatestTrace   *detailItem  `json:"latestTrace"`
	ProcessInfo   *processInfo `json:"processInfo"`
}

type cpInfo struct {
	Name  string `json:"cpName"`
	URL   string `json:"url"`
	Phone string `json:"phone"`
}

type detailItem struct {
	Time       int64  `json:"time"` // unix milliseconds (authoritative)
	TimeStr    string `json:"timeStr"`
	TimeZone   string `json:"timeZone"`
	ActionCode string `json:"actionCode"`
	Standerd   string `json:"standerdDesc"` // localized description, "[CITY] ..." prefixed
	Desc       string `json:"desc"`         // warehouse-side note, often empty
	Group      *group `json:"group"`
}

type group struct {
	NodeCode string `json:"nodeCode"`
	NodeDesc string `json:"nodeDesc"`
}

type processInfo struct {
	ProgressRate float64 `json:"progressRate"`
}

// Track fetches the full event history of one parcel. lang selects the event
// language ("en", "pt-BR", ...) — pt-BR returns Correios' own wording.
func (c *Client) Track(code, lang string) (*Result, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, errors.New("cainiao: empty code")
	}
	if lang = strings.TrimSpace(lang); lang == "" {
		lang = "en"
	}
	u := fmt.Sprintf("%s/global/detail.json?%s", baseURL,
		url.Values{"mailNos": {code}, "lang": {lang}}.Encode())
	body, err := c.get(u)
	if err != nil {
		return nil, err
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("cainiao: unexpected response")
	}
	if len(env.Module) == 0 {
		return nil, ErrNotFound
	}
	m := env.Module[0]
	if len(m.DetailList) == 0 {
		// Unknown numbers answer with a placeholder ("Awaiting seller
		// dispatch") — treat every empty timeline as not-found.
		return nil, ErrNotFound
	}
	out := &Result{
		Code:         code,
		Origin:       strings.TrimSpace(m.Origin),
		Destination:  strings.TrimSpace(m.Dest),
		OverallState: strings.TrimSpace(m.Status),
	}
	if m.DestCp != nil {
		out.LastCarrier = strings.TrimSpace(m.DestCp.Name)
	}
	for _, it := range m.DetailList {
		ev := buildEvent(code, it)
		if ev.Date.IsZero() {
			continue
		}
		if ev.Description == "" {
			continue
		}
		out.Events = append(out.Events, ev)
	}
	if len(out.Events) == 0 {
		return nil, ErrEmptyResult
	}
	reverse(out.Events) // detailList is newest-first; the app wants oldest-first
	return out, nil
}

// buildEvent converts one Cainiao trace item into an app event. The epoch
// milliseconds field is authoritative (timeStr/timeZone vary per leg country).
func buildEvent(code string, it detailItem) models.TrackingEvent {
	desc := strings.TrimSpace(it.Standerd)
	if desc == "" {
		desc = strings.TrimSpace(it.Desc)
	}
	location := ""
	// Cainiao localizes as "[CITY] description" — split the prefix so it lands
	// in the timeline's location column instead of the text.
	if after, ok := strings.CutPrefix(desc, "["); ok {
		if city, rest, ok2 := strings.Cut(after, "]"); ok2 && city != "" {
			location = strings.TrimSpace(city)
			desc = strings.TrimSpace(rest)
		}
	}
	if it.Desc != "" && !strings.EqualFold(it.Desc, desc) {
		// warehouse note ("Leave the warehouse") adds detail on origin events
		desc = desc + " — " + strings.TrimSpace(it.Desc)
	}
	status := StatusFor(it.ActionCode, it.Group, desc)
	out := models.TrackingEvent{
		Status:      status,
		Description: desc,
		Location:    location,
		Source:      "Cainiao",
	}
	switch {
	case it.Time > 0: // epoch milliseconds is authoritative
		out.Date = time.UnixMilli(it.Time).UTC()
	case it.TimeStr != "": // defensive: parse timeStr as Brazil time first
		if ts, ok := parseTimeStr(it.TimeStr); ok {
			out.Date = ts
		}
	}
	if out.Description == "" {
		out.Description = out.Status
	}
	out.ID = out.ComputeID()
	return out
}

// StatusFor maps Cainiao action/group codes to normalized app statuses.
func StatusFor(action string, g *group, description string) string {
	a := strings.ToUpper(strings.TrimSpace(action))
	node := ""
	if g != nil {
		node = strings.ToUpper(strings.TrimSpace(g.NodeCode))
	}

	// delivery family first
	switch {
	case strings.Contains(a, "SIGN"), strings.Contains(a, "DELIVER_SUCCESS"),
		strings.Contains(a, "SUCCESS_SIGN"):
		return models.StatusDelivered
	case strings.Contains(a, "FAIL"), strings.Contains(a, "REJECT"),
		strings.Contains(a, "EXCEPTION"), strings.Contains(a, "RETURN"):
		return models.StatusException
	}

	// origin warehouse legs are pre-transit
	if strings.HasPrefix(node, "AE_GROUP_WH") || strings.HasPrefix(a, "GWMS_ACCEPT") ||
		strings.HasPrefix(a, "GWMS_PACKAGE") {
		return models.StatusPending
	}

	// known out-for-delivery markers
	if strings.Contains(a, "ALLOCATE") || strings.Contains(node, "DELIVERING") {
		return models.StatusOutForDelivery
	}

	// everything else on the Cainiao network is movement
	if a != "" || node != "" {
		return models.StatusInTransit
	}

	// no codes at all: infer from the localized text
	d := strings.ToLower(description)
	switch {
	case contains(d, "entregue", "delivered", "signed"):
		return models.StatusDelivered
	case contains(d, "saiu para entrega", "out for delivery", "arriving"):
		return models.StatusOutForDelivery
	case contains(d, "devolvido", "returned", "extraviado", "lost"):
		return models.StatusException
	case contains(d, "recebido", "received", "postado", "posted"):
		return models.StatusPending
	}
	return models.StatusInTransit
}

func contains(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// parseTimeStr parses "2006-01-02 15:04:05" in the most common Cainiao zones
// (GMT-3 Brazil legs, GMT+8 China legs).
func parseTimeStr(s string) (time.Time, bool) {
	for _, tz := range []*time.Location{
		time.FixedZone("BRT", -3*3600),
		time.FixedZone("CST", 8*3600),
	} {
		if ts, err := time.ParseInLocation("2006-01-02 15:04:05", s, tz); err == nil {
			return ts.UTC(), true
		}
	}
	return time.Time{}, false
}

func (c *Client) get(rawurl string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawurl, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", baseURL+"/")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cainiao: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBody))
}

func reverse(evs []models.TrackingEvent) {
	for i, j := 0, len(evs)-1; i < j; i, j = i+1, j-1 {
		evs[i], evs[j] = evs[j], evs[i]
	}
}
