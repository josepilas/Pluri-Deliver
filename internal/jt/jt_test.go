package jt

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"plurideliver/internal/models"
)

// Captured live from official.jtjms-br.com (fake number): the envelope is
// {"code":1,"msg":"...","succ":true,"data":{"keyword":null,"details":null}}.
// The populated shape below mirrors chunk 9487's consumption of
// details[] {scanTime, customerTracking, picFlag} (newest first).
const detailFixture = `{
  "code": 1, "msg": "请求成功", "succ": true,
  "data": {
    "keyword": "JT9821473605821",
    "details": [
      {"scanTime": "2025-10-23 09:41:12", "customerTracking": "Entrega efetuada", "picFlag": 0},
      {"scanTime": "2025-10-23 07:12:03", "customerTracking": "Saiu para entrega", "picFlag": 0},
      {"scanTime": "2025-10-21 22:45:10", "customerTracking": "Em trânsito para o centro de distribuição", "picFlag": 0},
      {"scanTime": "2025-10-20 18:02:44", "customerTracking": "Pacote coletado", "picFlag": 1}
    ]
  }
}`

const planFixture = `{"code": 1, "msg": "请求成功", "succ": true, "data": "2025-10-24"}`

func TestParseDetailResponse(t *testing.T) {
	var env detailEnvelope
	if err := json.Unmarshal([]byte(detailFixture), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.Succ || env.Data == nil || len(env.Data.Details) != 4 {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	if env.Data.Details[0].ScanTime == "" || env.Data.Details[0].CustomerTracking == "" {
		t.Fatal("detail fields missing")
	}
	when, ok := parseScanTime(env.Data.Details[0].ScanTime)
	if !ok || when.Year() != 2025 || when.Month() != 10 || when.Day() != 23 {
		t.Fatalf("scanTime parse failed: %v", when)
	}
}

func TestTrackSignsAndParses(t *testing.T) {
	var gotSign, gotBody string
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		gotSign = r.Header.Get("sign")
		for _, h := range []string{"appId", "key", "nonce", "timestamp", "sign", "langType", "countryId", "clientSource"} {
			if r.Header.Get(h) == "" {
				t.Errorf("missing header %s", h)
			}
		}
		if r.Header.Get("appId") != appID || r.Header.Get("key") != appKey {
			t.Errorf("app credentials wrong")
		}
		switch r.URL.Path {
		case "/official/logisticsTracking/v2/getDetailByWaybillNo":
			_, _ = w.Write([]byte(detailFixture))
		case "/official/logisticsTracking/trace/planTime":
			_, _ = w.Write([]byte(planFixture))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New()
	orig := baseURL
	setBaseURL(srv.URL)
	defer setBaseURL(orig)

	res, err := c.Track("JT9821473605821", "123.456.789-09")
	if err != nil {
		t.Fatalf("Track: %v", err)
	}
	if gotPath := paths[len(paths)-2]; gotPath != "/official/logisticsTracking/v2/getDetailByWaybillNo" {
		t.Errorf("wrong path: %s (all: %v)", gotPath, paths)
	}
	// body must be the signed, key-sorted JSON: cpf, langType, waybillNo
	if !strings.Contains(gotBody, `"cpf":"12345678909"`) ||
		!strings.Contains(gotBody, `"langType":"PT"`) ||
		!strings.Contains(gotBody, `"waybillNo":"JT9821473605821"`) {
		t.Errorf("body wrong: %s", gotBody)
	}
	if strings.Index(gotBody, `"cpf"`) > strings.Index(gotBody, `"langType"`) {
		t.Errorf("body keys not sorted (sign depends on it): %s", gotBody)
	}
	if len(gotSign) != 32 {
		t.Errorf("sign should be a 32-char MD5 hex, got %q", gotSign)
	}
	if res.ETA != "2025-10-24" {
		t.Errorf("ETA not parsed: %q", res.ETA)
	}
	if len(res.Events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(res.Events))
	}
	// chronological (oldest first), statuses mapped
	if res.Events[0].Description != "Pacote coletado" || res.Events[0].Status != models.StatusPending {
		t.Errorf("first event wrong: %+v", res.Events[0])
	}
	last := res.Events[len(res.Events)-1]
	if last.Status != models.StatusDelivered {
		t.Errorf("last event should be delivered, got %q", last.Status)
	}
	if res.Events[0].Source != "J&T Express" {
		t.Errorf("source not tagged: %+v", res.Events[0])
	}
}

func TestTrackNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// the live-verified shape for an unknown waybill / mismatched CPF
		_, _ = w.Write([]byte(`{"code":1,"msg":"请求成功","data":{"keyword":null,"details":null},"succ":true}`))
	}))
	defer srv.Close()
	c := New()
	orig := baseURL
	setBaseURL(srv.URL)
	defer setBaseURL(orig)
	if _, err := c.Track("JT9999999999", "12345678909"); err == nil || !strings.Contains(err.Error(), "no tracking found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestDigits(t *testing.T) {
	if got := Digits("123.456.789-09"); got != "12345678909" {
		t.Errorf("Digits = %q", got)
	}
	if got := Digits(" 12345678909 "); got != "12345678909" {
		t.Errorf("Digits = %q", got)
	}
}

func TestValidCPF(t *testing.T) {
	// 111.444.777-35 is a publicly known valid test CPF.
	if !ValidCPF("111.444.777-35") || !ValidCPF("11144477735") {
		t.Error("known-valid CPF rejected")
	}
	for _, bad := range []string{"11144477734", "1114447773", "11111111111", "12345678900", ""} {
		if ValidCPF(bad) {
			t.Errorf("invalid CPF accepted: %q", bad)
		}
	}
}

func TestCleanText(t *testing.T) {
	if got := cleanText("  Encomenda <b>entregue</b><br>no local "); got != "Encomenda entregue no local" {
		t.Errorf("cleanText = %q", got)
	}
}

func TestLooksLikeJTCode(t *testing.T) {
	yes := []string{"JT9821473605821", "jt123456789", "123456789012"}
	no := []string{"JT12345678", "SPXBR0123456789", "PU123456789BR", "DEMO-1", ""}
	for _, c := range yes {
		if !LooksLikeJTCode(c) {
			t.Errorf("LooksLikeJTCode(%q) = false, want true", c)
		}
	}
	for _, c := range no {
		if LooksLikeJTCode(c) {
			t.Errorf("LooksLikeJTCode(%q) = true, want false", c)
		}
	}
}

func TestStatusFor(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Entrega efetuada", models.StatusDelivered},
		{"Saiu para entrega", models.StatusOutForDelivery},
		{"Falha na entrega - destinatário ausente", models.StatusException},
		{"Pacote coletado", models.StatusPending},
		{"Em trânsito", models.StatusInTransit},
	}
	for _, c := range cases {
		if got := StatusFor(c.in); got != c.want {
			t.Errorf("StatusFor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// setBaseURL is the test hook that retargets the client at a local server.
func setBaseURL(u string) { baseURL = u }
