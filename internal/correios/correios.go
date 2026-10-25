// Package correios talks to the public tracking portal of the Brazilian
// postal service (rastreamento.correios.com.br) the same way its official web
// app does:
//
//  1. GET /app/index.php                     — establishes the PHP session
//  2. GET /core/securimage/securimage_show.php — captcha PNG bound to that session
//  3. GET /app/resultado.php?objeto=CODE&captcha=TYPED&mqs=S
//     -> full JSON timeline for a single object
//  4. GET /app/rastroMulti.php?objeto=CODE1CODE2...&captcha=TYPED
//     -> latest event of up to 20 objects in one call
//
// The portal validates a fresh captcha text on every query, so the UI must
// show the image and let the user type the code. One captcha covers up to 20
// codes when the batch endpoint is used.
package correios

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"plurideliver/internal/models"
)

// Base URLs of the tracking portal.
const (
	baseURL    = "https://rastreamento.correios.com.br"
	appBase    = baseURL + "/app"
	captchaURL = baseURL + "/core/securimage/securimage_show.php"

	browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

	maxBody = 4 << 20 // 4 MiB
)

// Sentinel errors the UI acts on.
var (
	// ErrCaptchaInvalid means the typed text did not match the image —
	// fetch a new captcha and ask the user again.
	ErrCaptchaInvalid = errors.New("captcha invalido")
	// ErrNotFound means Correios has no record for the code (SRO-020).
	ErrNotFound = errors.New("objeto nao localizado")
	// ErrEmptyResult means the code is known but has no events yet.
	ErrEmptyResult = errors.New("no events")
)

// Client keeps the PHP session alive between the captcha image request and
// the query that consumes the typed text. Not safe for concurrent use — the
// Service serializes access.
type Client struct {
	mu  sync.Mutex
	hc  *http.Client
	log io.Writer // optional debug log
}

// New builds a client with its own cookie jar.
func New() *Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		jar = nil // fallback: in-memory default transport without jar
	}
	return &Client{hc: &http.Client{
		Timeout: 30 * time.Second,
		Jar:     jar,
	}}
}

// SetDebugLog wires an optional io.Writer for protocol dumps.
func (c *Client) SetDebugLog(w io.Writer) { c.log = w }

func (c *Client) do(method, rawurl string) (*http.Response, []byte, error) {
	req, err := http.NewRequest(method, rawurl, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", appBase+"/index.php")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return resp, nil, err
	}
	if c.log != nil {
		fmt.Fprintf(c.log, "[correios] %s %s -> %d (%d bytes)\n", method, rawurl, resp.StatusCode, len(body))
	}
	return resp, body, nil
}

// CaptchaImage opens a fresh portal session and returns the captcha PNG the
// user must read. The session cookies stay in the client jar so the next
// Track call consumes the very same captcha.
func (c *Client) CaptchaImage() ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// warm up the session (also rotates Dynatrace/load-balancer cookies)
	if _, _, err := c.do(http.MethodGet, appBase+"/index.php"); err != nil {
		return nil, fmt.Errorf("correios: cannot reach the portal: %w", err)
	}
	resp, body, err := c.do(http.MethodGet, captchaURL)
	if err != nil {
		return nil, fmt.Errorf("correios: cannot load the captcha image: %w", err)
	}
	if resp.StatusCode != http.StatusOK || len(body) < 64 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
		return nil, fmt.Errorf("correios: unexpected captcha response (HTTP %d, %d bytes)", resp.StatusCode, len(body))
	}
	return body, nil
}

// ---- JSON envelopes -----------------------------------------------------------

// phpDate mirrors Correios' {"date":"2026-10-23 07:33:25.000000",
// "timezone_type":3,"timezone":"America/Sao_Paulo"} structure.
type phpDate struct {
	Date     string `json:"date"`
	Timezone string `json:"timezone"`
}

func (d phpDate) Time() time.Time {
	tz, err := time.LoadLocation(d.Timezone)
	if err != nil {
		tz = time.FixedZone(d.Timezone, -3*3600) // most BR zones are UTC-3
	}
	for _, layout := range []string{"2006-01-02 15:04:05.000000", "2006-01-02 15:04:05"} {
		if ts, err := time.ParseInLocation(layout, d.Date, tz); err == nil {
			return ts.UTC()
		}
	}
	return time.Time{}
}

type endereco struct {
	Cidade string `json:"cidade"`
	UF     string `json:"uf"`
}

type unidade struct {
	Nome     string   `json:"nome"`
	Tipo     string   `json:"tipo"`
	Endereco endereco `json:"endereco"`
}

type eventoRaw struct {
	Codigo         string   `json:"codigo"`
	Tipo           string   `json:"tipo"`
	DtHrCriado     phpDate  `json:"dtHrCriado"`
	Descricao      string   `json:"descricao"`
	DescricaoFront string   `json:"descricaoFrontEnd"`
	Comentario     string   `json:"comentario"`
	Finalizador    string   `json:"finalizador"`
	Unidade        *unidade `json:"unidade"`
	UnidadeDestino *unidade `json:"unidadeDestino"`
}

type resultadoEnvelope struct {
	CodObjeto  string      `json:"codObjeto"`
	TipoPostal tipoPostal  `json:"tipoPostal"`
	DtPrevista string      `json:"dtPrevista"`
	Eventos    []eventoRaw `json:"eventos"`
	// error payload: {"erro":"true","mensagem":"Captcha inválido"}
	Erro     json.RawMessage `json:"erro"`
	Mensagem string          `json:"mensagem"`
}

type tipoPostal struct {
	Descricao  string `json:"descricao"`
	Categoria  string `json:"categoria"`
	Modalidade string `json:"sigla"`
}

type multiItem struct {
	CodObjeto       string  `json:"cod_objeto_"` // clean code
	CodObjetoSpaced string  `json:"cod_objeto"`  // pretty "NN 428 571 043 BR" (error items only carry this)
	DataPrev        string  `json:"data_prevista"`
	Cidade          string  `json:"cidade"`
	UF              string  `json:"uf"`
	DataEvento      phpDate `json:"data_evento"`
	CodEvento       string  `json:"codEvento"`
	TipoEvento      string  `json:"tipoEvento"`
	Descricao       string  `json:"descricao"`
	Detalhe         string  `json:"detalhe"`
	// error objects carry "mensagem" instead of events
	Mensagem string `json:"mensagem"`
}

type multiEnvelope struct {
	Transito []multiItem     `json:"transito"`
	Entregue []multiItem     `json:"entregue"`
	Erro     json.RawMessage `json:"erro"`
	Mensagem string          `json:"mensagem"`
}

// truthy decodes the portal's inconsistent "erro" field ("true" or true).
func truthy(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.EqualFold(strings.TrimSpace(s), "true")
	}
	return false
}

// ---- result types --------------------------------------------------------------

// Result is the parsed outcome of a single-object query.
type Result struct {
	Code     string
	Category string // tipoPostal.categoria (e.g. "PACKET STANDARD IMPORTAÇÃO")
	ETA      string // dtPrevista "03/11/2026" (dd/mm/yyyy, Brazil time)
	Events   []models.TrackingEvent
}

// MultiResult maps each queried code to its latest event (or error message).
type MultiResult struct {
	Latest map[string]models.TrackingEvent // clean code -> latest event
	ETA    map[string]string
	Errors map[string]string // clean code -> portal message
}

// Track queries the full timeline of one object with the captcha text typed
// by the user.
func (c *Client) Track(code, captcha string) (*Result, error) {
	code = CleanCode(code)
	if code == "" {
		return nil, errors.New("correios: empty code")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	u := fmt.Sprintf("%s/resultado.php?objeto=%s&captcha=%s&mqs=S",
		appBase, url.QueryEscape(code), url.QueryEscape(captcha))
	resp, body, err := c.do(http.MethodGet, u)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("correios: HTTP %d", resp.StatusCode)
	}
	var env resultadoEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("correios: unexpected response")
	}
	if truthy(env.Erro) {
		return nil, classifyError(env.Mensagem)
	}
	if len(env.Eventos) == 0 {
		return nil, ErrEmptyResult
	}
	out := &Result{
		Code:     code,
		Category: env.TipoPostal.Categoria,
		ETA:      env.DtPrevista,
		Events:   make([]models.TrackingEvent, 0, len(env.Eventos)),
	}
	for _, ev := range env.Eventos {
		out.Events = append(out.Events, buildEvent(code, ev))
	}
	// chronological order (oldest first), matching the app timeline
	for i, j := 0, len(out.Events)-1; i < j; i, j = i+1, j-1 {
		out.Events[i], out.Events[j] = out.Events[j], out.Events[i]
	}
	return out, nil
}

// TrackMulti queries the latest event of up to 20 objects with a single
// captcha. Codes are concatenated exactly like the portal web app does.
func (c *Client) TrackMulti(codes []string, captcha string) (*MultiResult, error) {
	clean := make([]string, 0, len(codes))
	seen := map[string]bool{}
	for _, code := range codes {
		if cc := CleanCode(code); cc != "" && !seen[cc] {
			seen[cc] = true
			clean = append(clean, cc)
		}
	}
	if len(clean) == 0 {
		return &MultiResult{Latest: map[string]models.TrackingEvent{}, ETA: map[string]string{}, Errors: map[string]string{}}, nil
	}
	if len(clean) > 20 {
		clean = clean[:20] // portal hard limit
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	u := fmt.Sprintf("%s/rastroMulti.php?objeto=%s&captcha=%s",
		appBase, url.QueryEscape(strings.Join(clean, "")), url.QueryEscape(captcha))
	resp, body, err := c.do(http.MethodGet, u)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("correios: HTTP %d", resp.StatusCode)
	}
	var env multiEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("correios: unexpected response")
	}
	if truthy(env.Erro) {
		return nil, classifyError(env.Mensagem)
	}
	out := &MultiResult{Latest: map[string]models.TrackingEvent{}, ETA: map[string]string{}, Errors: map[string]string{}}
	add := func(items []multiItem, forceStatus string) {
		for _, it := range items {
			code := CleanCode(it.CodObjeto)
			if code == "" {
				continue
			}
			if it.Mensagem != "" && it.Descricao == "" && it.DataEvento.Date == "" {
				out.Errors[code] = it.Mensagem
				continue
			}
			ev := models.TrackingEvent{
				Date:        it.DataEvento.Time(),
				Status:      forceStatus,
				Description: it.Descricao,
				Location:    cityUF(it.Cidade, it.UF),
				Source:      "Correios",
			}
			if it.Detalhe != "" {
				ev.Description += " — " + it.Detalhe
			}
			if forceStatus == "" {
				ev.Status = StatusFor(it.CodEvento, it.TipoEvento, "N", it.Descricao)
			}
			if ev.Date.IsZero() {
				continue
			}
			ev.ID = ev.ComputeID()
			out.Latest[code] = ev
			if it.DataPrev != "" {
				out.ETA[code] = it.DataPrev
			}
		}
	}
	add(env.Transito, "")
	add(env.Entregue, models.StatusDelivered)
	return out, nil
}

// ---- event building -------------------------------------------------------------

// StatusFor maps the SRO event code to a normalized app status.
// Reference: https://www.correios.com.br/precisa-de-ajuda/glossario
func StatusFor(codigo, tipo, finalizador, descricao string) string {
	c := strings.ToUpper(strings.TrimSpace(codigo))
	t := strings.TrimSpace(tipo)
	d := strings.ToLower(descricao)

	// delivery family: BDE/BDI/BDOR/BDP... with tipo 01 is "entregue"
	if strings.HasPrefix(c, "BD") {
		if t == "01" || strings.EqualFold(finalizador, "S") ||
			strings.Contains(d, "entregue") && !strings.Contains(d, "nao") {
			return models.StatusDelivered
		}
		return models.StatusException
	}
	switch c {
	case "OEC": // saiu para entrega
		return models.StatusOutForDelivery
	case "LDI", "LDIAC": // disponível para retirada
		if strings.Contains(d, "retirada") {
			return models.StatusOutForDelivery
		}
		return models.StatusInfo
	case "PO": // postado
		return models.StatusPending
	case "EST", "ESTR": // extraviado
		return models.StatusException
	case "RO", "DO", "CF", "CTR", "TRS", "CET", "PAR", "EPP", "EPPR", "APP", "MI":
		return models.StatusInTransit
	}
	// fallback: infer from the description
	switch {
	case strings.Contains(d, "entregue"):
		return models.StatusDelivered
	case strings.Contains(d, "saiu para entrega") || strings.Contains(d, "retirada"):
		return models.StatusOutForDelivery
	case strings.Contains(d, "postado") || strings.Contains(d, "posted"):
		return models.StatusPending
	case strings.Contains(d, "extraviado") || strings.Contains(d, "devolvido"):
		return models.StatusException
	}
	return models.StatusInTransit
}

func buildEvent(code string, ev eventoRaw) models.TrackingEvent {
	desc := strings.TrimSpace(ev.Descricao)
	if desc == "" {
		desc = strings.TrimSpace(ev.DescricaoFront)
	}
	if cm := strings.TrimSpace(ev.Comentario); cm != "" {
		desc += " — " + cm
	}
	loc := unitLocation(ev.Unidade)
	if dest := unitLocation(ev.UnidadeDestino); dest != "" && dest != loc {
		if loc == "" {
			loc = dest
		} else {
			loc += " → " + dest
		}
	}
	out := models.TrackingEvent{
		Date:        ev.DtHrCriado.Time(),
		Status:      StatusFor(ev.Codigo, ev.Tipo, ev.Finalizador, desc),
		Description: desc,
		Location:    loc,
		Source:      "Correios",
	}
	if out.Date.IsZero() {
		out.Date = time.Now().UTC()
	}
	if out.Description == "" {
		out.Description = out.Status
	}
	out.ID = out.ComputeID()
	return out
}

func unitLocation(u *unidade) string {
	if u == nil {
		return ""
	}
	if u.Endereco.Cidade != "" {
		return cityUF(u.Endereco.Cidade, u.Endereco.UF)
	}
	if u.Nome != "" { // e.g. "CHINA"
		return u.Nome
	}
	return ""
}

func cityUF(city, uf string) string {
	city = strings.TrimSpace(city)
	uf = strings.ToUpper(strings.TrimSpace(uf))
	switch {
	case city != "" && uf != "":
		return city + "/" + uf
	case city != "":
		return city
	case uf != "":
		return uf
	}
	return ""
}

func classifyError(msg string) error {
	m := strings.ToLower(strings.TrimSpace(msg))
	switch {
	case strings.Contains(m, "captcha"):
		return ErrCaptchaInvalid
	case strings.Contains(m, "sro-020"), strings.Contains(m, "nao localizamos"), strings.Contains(m, "não localizamos"):
		return ErrNotFound
	}
	if msg == "" {
		msg = "erro desconhecido"
	}
	return errors.New(msg)
}

// CleanCode strips anything the portal input would strip (spaces, dashes,
// dots, semicolons...) and uppercases the result.
func CleanCode(code string) string {
	return strings.ToUpper(strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '.', ',', ';', ':', '|', '/', '\n', '\r', '\t':
			return -1
		}
		return r
	}, strings.TrimSpace(code)))
}

// LooksLikeCorreiosCode reports whether a code matches the S10 format with
// the BR country suffix (the only family Correios auto-detects reliably).
func LooksLikeCorreiosCode(code string) bool {
	c := CleanCode(code)
	if len(c) != 13 {
		return false
	}
	for i := 0; i < 2; i++ {
		if !isLetter(c[i]) {
			return false
		}
	}
	for i := 2; i < 11; i++ {
		if c[i] < '0' || c[i] > '9' {
			return false
		}
	}
	return strings.HasSuffix(c, "BR")
}

func isLetter(b byte) bool { return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') }
