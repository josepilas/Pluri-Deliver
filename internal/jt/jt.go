// Package jt talks to J&T Express Brasil's official tracking API
// (official.jtjms-br.com) exactly like its own web app (jtexpress.com.br):
//
//	POST /official/logisticsTracking/v2/getDetailByWaybillNo
//	     body: {"cpf": "...", "waybillNo": "...", "langType": "PT"}
//	     headers: appId, key, nonce, timestamp, sign (MD5 of
//	              appId+timestamp+nonce+JSON(sorted body)+key, upper-case)
//
//	POST /official/logisticsTracking/trace/planTime  (same body)
//	     -> estimated delivery date shown above the timeline
//
// J&T requires the RECIPIENT'S CPF on every query (their own site asks for
// it too), so the app stores one CPF per tracking. The signing scheme and
// the request shape were extracted from the SPA (chunk 3157/9487 of
// br_official_web) and verified live: signed guest requests return
// {"code":1,"msg":"请求成功","succ":true} with tracking data.
package jt

import (
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"plurideliver/internal/models"
)

const (
	// Credentials the SPA hardcodes on every request (extracted from the
	// axios request interceptor in chunk 3157).
	appID  = "3B29A9C5728BF3E1DB0C4D66B79748B7"
	appKey = "94bbcac67ab47c736d530efe3e1dc358"

	browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

	maxBody = 4 << 20
)

// baseURL is a var so tests can retarget the client at a local server.
var baseURL = "https://official.jtjms-br.com"

// Sentinel errors surfaced to the service layer.
var (
	// ErrNotFound means J&T returned success but no details for the pair
	// (unknown waybill, or CPF does not match the recipient).
	ErrNotFound = errors.New("J&T: no tracking found for this CPF + code")
)

// Client queries the J&T Brasil tracking API. Safe for concurrent use.
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
	ETA    string // estimated delivery ("预计签收时间"), display-only
	Events []models.TrackingEvent
}

type detailEnvelope struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Succ bool   `json:"succ"`
	Data *struct {
		Keyword string       `json:"keyword"`
		Details []detailItem `json:"details"`
	} `json:"data"`
}

type detailItem struct {
	ScanTime         string `json:"scanTime"`
	CustomerTracking string `json:"customerTracking"`
	PicFlag          int    `json:"picFlag"`
}

type planEnvelope struct {
	Code int    `json:"code"`
	Succ bool   `json:"succ"`
	Msg  string `json:"msg"`
	Data string `json:"data"`
}

type trackBody struct {
	CPF       string `json:"cpf"`
	WaybillNo string `json:"waybillNo"`
	LangType  string `json:"langType"`
}

// Track fetches the event timeline of one J&T parcel using the recipient's
// CPF, and (best-effort) the estimated delivery date.
func (c *Client) Track(code, cpf string) (*Result, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	cpf = Digits(cpf)
	if code == "" || cpf == "" {
		return nil, errors.New("jt: code and CPF are required")
	}
	body := trackBody{CPF: cpf, WaybillNo: code, LangType: "PT"}

	raw, err := c.post("/official/logisticsTracking/v2/getDetailByWaybillNo", body)
	if err != nil {
		return nil, err
	}
	var env detailEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("jt: unexpected response")
	}
	if !env.Succ || env.Data == nil {
		return nil, ErrNotFound
	}
	events := make([]models.TrackingEvent, 0, len(env.Data.Details))
	for _, it := range env.Data.Details {
		desc := cleanText(it.CustomerTracking)
		if desc == "" {
			continue
		}
		when, ok := parseScanTime(it.ScanTime)
		if !ok {
			continue
		}
		ev := models.TrackingEvent{
			Date:        when.UTC(),
			Status:      StatusFor(desc),
			Description: desc,
			Source:      "J&T Express",
		}
		ev.ID = ev.ComputeID()
		events = append(events, ev)
	}
	if len(events) == 0 {
		return nil, ErrNotFound
	}
	reverse(events) // details come newest-first; the app wants oldest-first

	out := &Result{Code: code, Events: events}
	// Estimated receipt time is a separate call; failures are non-fatal.
	if res, err := c.post("/official/logisticsTracking/trace/planTime", body); err == nil {
		var pe planEnvelope
		if json.Unmarshal(res, &pe) == nil && pe.Succ && strings.TrimSpace(pe.Data) != "" {
			out.ETA = strings.TrimSpace(pe.Data)
		}
	}
	return out, nil
}

// post signs the request like the SPA and returns the raw response body.
func (c *Client) post(path string, body trackBody) ([]byte, error) {
	payload, err := json.Marshal(sortedBody(body))
	if err != nil {
		return nil, err
	}
	nonce := jsRandom()
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	signStr := appID + ts + nonce + string(payload) + appKey
	sign := strings.ToUpper(fmt.Sprintf("%x", md5.Sum([]byte(signStr))))

	req, err := http.NewRequest(http.MethodPost, baseURL+path, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	h := req.Header
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "max-age=2, must-revalidate")
	h.Set("langType", "pt-br")
	h.Set("clientSource", "web")
	h.Set("timezone", "GMT-0300")
	h.Set("timeZone", "GMT-0300")
	h.Set("countryId", "1")
	h.Set("token", "")
	h.Set("appId", appID)
	h.Set("key", appKey)
	h.Set("nonce", nonce)
	h.Set("timestamp", ts)
	h.Set("sign", sign)
	h.Set("User-Agent", browserUA)
	h.Set("Origin", "https://www.jtexpress.com.br")
	h.Set("Referer", "https://www.jtexpress.com.br/")

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jt: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBody))
}

// sortedBody rebuilds the request with alphabetically sorted keys and no
// null values — JSON.stringify of the SPA's sorted/cleaned object.
func sortedBody(b trackBody) map[string]string {
	out := map[string]string{}
	for k, v := range map[string]string{
		"cpf":       b.CPF,
		"waybillNo": b.WaybillNo,
		"langType":  b.LangType,
	} {
		if v != "" {
			out[k] = v
		}
	}
	return out
}

// jsRandom mirrors JS Math.random().toString() (e.g. "0.4829384726384927").
func jsRandom() string {
	return strconv.FormatFloat(rand.Float64(), 'f', -1, 64)
}

// cleanText strips the HTML markup the API sometimes embeds in descriptions.
func cleanText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "<br>", " ")
	s = strings.ReplaceAll(s, "</br>", " ")
	for _, tag := range []string{"<b>", "</b>", "<i>", "</i>", "<span>", "</span>", "<em>", "</em>"} {
		s = strings.ReplaceAll(s, tag, "")
	}
	return strings.Join(strings.Fields(s), " ")
}

// parseScanTime understands the scanTime layouts J&T uses.
func parseScanTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		"2006-01-02 15:04:05", "2006-01-02 15:04",
		"2006/01/02 15:04:05", "2006-01-02T15:04:05",
		time.RFC3339, "02/01/2006 15:04:05",
	}
	for _, l := range layouts {
		if ts, err := time.ParseInLocation(l, s, time.FixedZone("BRT", -3*3600)); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}

// StatusFor maps J&T's customer-facing event text to normalized app statuses.
func StatusFor(description string) string {
	d := strings.ToLower(description)
	switch {
	case contains(d, "entrega efetuada", "entregue", "delivered", "signed", "assinado", "recebido pelo"):
		return models.StatusDelivered
	case contains(d, "saiu para entrega", "out for delivery", "rota de entrega", "em rota"):
		return models.StatusOutForDelivery
	case contains(d, "falha na entrega", "failed", "não foi possível", "nao foi possível", "devolução", "devolvido", "returned", "extraviado"):
		return models.StatusException
	case contains(d, "criado", "created", "coletado", "picked up", "recebido", "received"):
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

func reverse(evs []models.TrackingEvent) {
	for i, j := 0, len(evs)-1; i < j; i, j = i+1, j-1 {
		evs[i], evs[j] = evs[j], evs[i]
	}
}

// Digits strips anything but numbers (CPF masks like 123.456.789-01 become
// 12345678901) — the API expects the raw 11-digit form.
func Digits(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ValidCPF checks the 11-digit Brazilian CPF check digits. J&T rejects
// syntactically invalid CPFs, so the UI uses this to validate input early.
func ValidCPF(s string) bool {
	c := Digits(s)
	if len(c) != 11 || strings.Repeat(string(c[0]), 11) == c {
		return false
	}
	digit := func(i int) int {
		n, _ := strconv.Atoi(string(c[i]))
		return n
	}
	check := func(n int) int {
		r := n % 11
		if r < 2 {
			return 0
		}
		return 11 - r
	}
	sum1 := 0
	for i := 0; i < 9; i++ {
		sum1 += digit(i) * (10 - i)
	}
	sum2 := 0
	for i := 0; i < 10; i++ {
		sum2 += digit(i) * (11 - i)
	}
	return check(sum1) == digit(9) && check(sum2) == digit(10)
}

// LooksLikeJTCode reports whether a code matches J&T Brasil waybills:
// "JT" + 9..13 digits is the common Shopee-marketplace format; J&T also
// accepts pure-numeric keys (12-15 digits), which are matched conservatively.
func LooksLikeJTCode(code string) bool {
	c := strings.ToUpper(strings.TrimSpace(code))
	if strings.HasPrefix(c, "JT") {
		d := c[2:]
		if len(d) >= 9 && len(d) <= 13 {
			allDigits := true
			for i := 0; i < len(d); i++ {
				if d[i] < '0' || d[i] > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				return true
			}
		}
	}
	if len(c) >= 12 && len(c) <= 15 {
		allDigits := true
		for i := 0; i < len(c); i++ {
			if c[i] < '0' || c[i] > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return true
		}
	}
	return false
}
