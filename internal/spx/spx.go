// Package spx talks to SPX Express Brasil (Shopee's logistics arm, spx.com.br)
// the same way its official tracking page does:
//
//  1. GET /shipment/order/open/order/get_order_info?spx_tn=CODE&language_code=pt-BR
//     -> full order envelope; events live in data.sls_tracking_info.records[]
//     (open endpoint, no session, no captcha)
//  2. GET /api/v2/fleet_order/tracking/search?sls_tracking_number=CODE|<ts><sha256>
//     -> lightweight fleet tracking: data.tracking_list[] {message, timestamp}
//     The query param is signed exactly like the SPA does:
//     param = CODE + "|" + unixSeconds + sha256(CODE + unixSeconds + SALT)
//
// Both endpoints were extracted from the production bundle (module 20351 of
// shopee-spx-live-br) and verified live: no login, no cookies, no captcha.
package spx

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"plurideliver/internal/models"
)

const (
	// querySalt is the literal secret the SPA appends when signing the
	// fleet_order tracking query (extracted from the bundle).
	querySalt = "MGViZmZmZTYzZDJhNDgxY2Y1N2ZlN2Q1ZWJkYzlmZDY="

	browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

	maxBody = 4 << 20 // 4 MiB
)

// baseURL is a var so tests can retarget the client at a local server.
var baseURL = "https://spx.com.br"

// Sentinel errors surfaced to the service layer.
var (
	// ErrNotFound means SPX has no record for the number.
	ErrNotFound = errors.New("SPX: number not found")
	// ErrEmptyResult means the number is known but has no events yet.
	ErrEmptyResult = errors.New("SPX: no events yet")
)

// Client queries the SPX Brasil tracking endpoints. Safe for concurrent use.
type Client struct {
	hc *http.Client
}

// New builds a client.
func New() *Client {
	return &Client{hc: &http.Client{Timeout: 30 * time.Second}}
}

// Result is the parsed outcome of a tracking query.
type Result struct {
	Code   string
	ETA    string // display-only estimate, when the portal provides one
	Events []models.TrackingEvent
}

// ---- response envelopes --------------------------------------------------------

type orderEnvelope struct {
	RetCode int        `json:"retcode"`
	Message string     `json:"message"`
	Data    *orderData `json:"data"`
}

type orderData struct {
	SLSInfo  *slsInfo   `json:"sls_tracking_info"`
	Order    *orderInfo `json:"order_info"`
	Children []child    `json:"children"`
}

type slsInfo struct {
	Records []record `json:"records"`
}

type record struct {
	Description string `json:"description"`
	ActualTime  any    `json:"actual_time"` // unix seconds or string
	DisplayFlag int    `json:"display_flag"`
	Milestone   string `json:"milestone_code"`
	Location    string `json:"location"`
}

type orderInfo struct {
	SPTN     string `json:"spx_tn"`
	SLSTN    string `json:"sls_tn"`
	ParentTN string `json:"parent_tn"`
	Subgroup string `json:"tracking_code_subgroup_name"`
}

type child struct {
	WaybillNo string `json:"waybill_no"`
}

type fleetEnvelope struct {
	RetCode int        `json:"retcode"`
	Message string     `json:"message"`
	Data    *fleetData `json:"data"`
}

type fleetData struct {
	TrackingList  []fleetRecord `json:"tracking_list"`
	CurrentStatus string        `json:"current_status"`
	SLSTN         string        `json:"sls_tracking_number"`
}

type fleetRecord struct {
	Message   string `json:"message"`
	Timestamp any    `json:"timestamp"` // unix seconds or string
}

// Track fetches the event timeline of one SPX parcel: order-info endpoint
// first (richest), fleet search as fallback.
func (c *Client) Track(code string) (*Result, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, errors.New("spx: empty code")
	}
	if res, err := c.trackOrderInfo(code); err == nil {
		return res, nil
	} else if errors.Is(err, ErrEmptyResult) {
		return nil, err // known number, nothing to fall back to
	}
	return c.trackFleet(code)
}

// trackOrderInfo queries the open order endpoint (no signature).
func (c *Client) trackOrderInfo(code string) (*Result, error) {
	u := fmt.Sprintf("%s/shipment/order/open/order/get_order_info?%s",
		baseURL, url.Values{"spx_tn": {code}, "language_code": {"pt-BR"}}.Encode())
	body, err := c.get(u)
	if err != nil {
		return nil, err
	}
	var env orderEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("spx: unexpected response")
	}
	if env.Data == nil || env.RetCode != 0 {
		return nil, ErrNotFound
	}
	var recs []record
	if env.Data.SLSInfo != nil {
		recs = env.Data.SLSInfo.Records
	}
	events := make([]models.TrackingEvent, 0, len(recs))
	for _, r := range recs {
		if r.DisplayFlag == 0 { // SPA hides display_flag==0 records
			continue
		}
		desc := strings.TrimSpace(r.Description)
		if desc == "" {
			continue
		}
		when, ok := parseTime(r.ActualTime)
		if !ok {
			continue
		}
		ev := models.TrackingEvent{
			Date:        when.UTC(),
			Status:      StatusFor(r.Milestone, desc),
			Description: desc,
			Location:    strings.TrimSpace(r.Location),
			Source:      "SPX Express",
		}
		ev.ID = ev.ComputeID()
		events = append(events, ev)
	}
	if len(events) == 0 {
		return nil, ErrEmptyResult
	}
	reverse(events) // records come newest-first; the app wants oldest-first
	out := &Result{Code: code, Events: events}
	if env.Data.Order != nil {
		out.ETA = strings.TrimSpace(env.Data.Order.Subgroup)
	}
	return out, nil
}

// trackFleet queries the signed fleet endpoint (the SPA's quick-track).
func (c *Client) trackFleet(code string) (*Result, error) {
	param := signParam(code)
	u := fmt.Sprintf("%s/api/v2/fleet_order/tracking/search?sls_tracking_number=%s",
		baseURL, url.QueryEscape(param))
	body, err := c.get(u)
	if err != nil {
		return nil, err
	}
	var env fleetEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("spx: unexpected response")
	}
	if env.Data == nil || env.RetCode != 0 || len(env.Data.TrackingList) == 0 {
		return nil, ErrNotFound
	}
	events := make([]models.TrackingEvent, 0, len(env.Data.TrackingList))
	for _, r := range env.Data.TrackingList {
		desc := strings.TrimSpace(r.Message)
		if desc == "" {
			continue
		}
		when, ok := parseTime(r.Timestamp)
		if !ok {
			continue
		}
		ev := models.TrackingEvent{
			Date:        when.UTC(),
			Status:      StatusFor("", desc),
			Description: desc,
			Source:      "SPX Express",
		}
		ev.ID = ev.ComputeID()
		events = append(events, ev)
	}
	if len(events) == 0 {
		return nil, ErrEmptyResult
	}
	reverse(events)
	return &Result{Code: code, Events: events}, nil
}

// signParam builds CODE|<unixSeconds><sha256(CODE+unixSeconds+SALT)>.
func signParam(code string) string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sum := sha256.Sum256([]byte(code + ts + querySalt))
	return code + "|" + ts + hex.EncodeToString(sum[:])
}

func (c *Client) get(rawurl string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawurl, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("x-language", "pt-BR")
	req.Header.Set("Referer", baseURL+"/track")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("spx: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBody))
}

// StatusFor maps SPX milestone codes / event text to normalized app statuses.
func StatusFor(milestone, description string) string {
	m := strings.ToUpper(strings.TrimSpace(milestone))
	d := strings.ToLower(description)

	switch m {
	case "DELIVERED", "SIGNED":
		return models.StatusDelivered
	case "OUT_FOR_DELIVERY", "DELIVERING":
		return models.StatusOutForDelivery
	case "PICKED_UP", "SHIPPING":
		return models.StatusInTransit
	case "PENDING", "CREATED":
		return models.StatusPending
	}

	// The fleet endpoint carries no milestone code: infer from the text.
	switch {
	case contains(d, "entregue", "delivered", "signed for", "received by"):
		return models.StatusDelivered
	case contains(d, "saiu para entrega", "out for delivery", "rota de entrega", "with delivery courier"):
		return models.StatusOutForDelivery
	case contains(d, "falha", "failed", "não foi possível", "nao foi possível", "devolvido", "returned", "extraviado", "lost"):
		return models.StatusException
	case contains(d, "coletado", "picked up", "recebido", "received", "criado", "created"):
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

// parseTime accepts unix seconds (number) or the common string layouts.
func parseTime(v any) (time.Time, bool) {
	switch x := v.(type) {
	case float64:
		if x <= 0 {
			return time.Time{}, false
		}
		return time.Unix(int64(x), 0), true
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return time.Time{}, false
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 1_000_000_000 {
			return time.Unix(n, 0), true
		}
		for _, layout := range []string{
			"2006-01-02 15:04:05", "2006-01-02T15:04:05", time.RFC3339, "02/01/2006 15:04:05",
		} {
			if ts, err := time.ParseInLocation(layout, s, time.FixedZone("BRT", -3*3600)); err == nil {
				return ts, true
			}
		}
	}
	return time.Time{}, false
}

func reverse(evs []models.TrackingEvent) {
	for i, j := 0, len(evs)-1; i < j; i, j = i+1, j-1 {
		evs[i], evs[j] = evs[j], evs[i]
	}
}

// LooksLikeSPXCode reports whether a code matches the SPX Brasil family
// (SPXBR + 8..15 digits — the format SPX itself accepts in its search box).
func LooksLikeSPXCode(code string) bool {
	c := strings.ToUpper(strings.TrimSpace(code))
	if !strings.HasPrefix(c, "SPXBR") {
		return false
	}
	d := c[len("SPXBR"):]
	if len(d) < 8 || len(d) > 15 {
		return false
	}
	for i := 0; i < len(d); i++ {
		if d[i] < '0' || d[i] > '9' {
			return false
		}
	}
	return true
}
