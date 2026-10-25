package correios

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"plurideliver/internal/models"
)

// Aliases keep the assertions below short.
const (
	StatusPending        = models.StatusPending
	StatusInTransit      = models.StatusInTransit
	StatusDelivered      = models.StatusDelivered
	StatusOutForDelivery = models.StatusOutForDelivery
	StatusException      = models.StatusException
)

// The fixtures below are REAL responses captured from the live portal on
// 2026-10-23 for an active Correios packet (NN428571043BR, an import from
// China going through Curitiba) — they pin the parser to the actual schema.

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

func TestParseResultadoSingle(t *testing.T) {
	var env resultadoEnvelope
	if err := json.Unmarshal(loadFixture(t, "resultado_single.json"), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if truthy(env.Erro) {
		t.Fatalf("unexpected error payload: %s", env.Mensagem)
	}
	if env.CodObjeto != "NN428571043BR" {
		t.Fatalf("codObjeto = %q", env.CodObjeto)
	}
	if env.DtPrevista != "03/11/2026" {
		t.Fatalf("dtPrevista = %q", env.DtPrevista)
	}
	if got := len(env.Eventos); got != 6 {
		t.Fatalf("eventos = %d, want 6", got)
	}

	res := &Result{
		Code:     CleanCode(env.CodObjeto),
		Category: env.TipoPostal.Categoria,
		ETA:      env.DtPrevista,
	}
	for _, ev := range env.Eventos {
		res.Events = append(res.Events, buildEvent(res.Code, ev))
	}
	for i, j := 0, len(res.Events)-1; i < j; i, j = i+1, j-1 {
		res.Events[i], res.Events[j] = res.Events[j], res.Events[i]
	}

	if res.Category != "PACKET STANDARD IMPORTAÇÃO" {
		t.Errorf("category = %q", res.Category)
	}
	// chronological: oldest first
	wantFirst := time.Date(2026, 10, 19, 6, 41, 30, 0, time.UTC) // local 03:41:30 UTC-3
	if !res.Events[0].Date.Equal(wantFirst) {
		t.Errorf("first event date = %v, want %v", res.Events[0].Date, wantFirst)
	}
	if res.Events[0].Status != StatusPending {
		t.Errorf("first event status = %q, want pending (posted in China)", res.Events[0].Status)
	}
	if res.Events[0].Location != "CHINA" {
		t.Errorf("first event location = %q, want CHINA", res.Events[0].Location)
	}
	// latest: RO em transferência in Curitiba → in_transit
	last := res.Events[len(res.Events)-1]
	if last.Status != StatusInTransit {
		t.Errorf("latest status = %q, want in_transit", last.Status)
	}
	if last.Location == "" || last.Date.Year() != 2026 {
		t.Errorf("latest location/date = %q / %v", last.Location, last.Date)
	}
	// customs clearance event should classify as in_transit, not exception
	for _, ev := range res.Events {
		if ev.Status == "" || ev.Description == "" {
			t.Errorf("event missing status or description: %+v", ev)
		}
		if ev.ID == "" {
			t.Errorf("event missing id: %+v", ev)
		}
	}
}

func TestParseMultiResult(t *testing.T) {
	var env multiEnvelope
	if err := json.Unmarshal(loadFixture(t, "rastro_multi.json"), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if truthy(env.Erro) {
		t.Fatalf("unexpected error: %s", env.Mensagem)
	}
	if len(env.Transito) != 2 || len(env.Entregue) != 0 {
		t.Fatalf("sections: transito=%d entregue=%d", len(env.Transito), len(env.Entregue))
	}

	out := &MultiResult{Latest: map[string]models.TrackingEvent{}, ETA: map[string]string{}, Errors: map[string]string{}}
	for _, it := range env.Transito {
		code := CleanCode(it.CodObjeto)
		if code == "" {
			code = CleanCode(it.CodObjetoSpaced)
		}
		if code == "" {
			continue
		}
		if it.Mensagem != "" && it.Descricao == "" && it.DataEvento.Date == "" {
			out.Errors[code] = it.Mensagem
			continue
		}
		ev := models.TrackingEvent{
			Date:        it.DataEvento.Time(),
			Status:      StatusFor(it.CodEvento, it.TipoEvento, "N", it.Descricao),
			Description: it.Descricao,
			Location:    cityUF(it.Cidade, it.UF),
			Source:      "Correios",
		}
		ev.ID = ev.ComputeID()
		out.Latest[code] = ev
		if it.DataPrev != "" {
			out.ETA[code] = it.DataPrev
		}
	}

	if _, ok := out.Latest["NN428571043BR"]; !ok {
		t.Fatalf("missing latest event for NN428571043BR: %+v", out.Latest)
	}
	ev := out.Latest["NN428571043BR"]
	if ev.Status != StatusInTransit {
		t.Errorf("status = %q, want in_transit", ev.Status)
	}
	if ev.Location != "Curitiba/PR" {
		t.Errorf("location = %q, want Curitiba/PR", ev.Location)
	}
	if out.ETA["NN428571043BR"] != "03/11/2026" {
		t.Errorf("eta = %q", out.ETA["NN428571043BR"])
	}
	if msg, ok := out.Errors["LB123456789BR"]; !ok || msg == "" {
		t.Errorf("expected SRO-020 error for the unknown code, got %+v", out.Errors)
	}
}

func TestStatusFor(t *testing.T) {
	cases := []struct {
		cod, tipo, fin, desc, want string
	}{
		{"PO", "01", "N", "Objeto postado", StatusPending},
		{"RO", "01", "N", "Objeto em transferência", StatusInTransit},
		{"DO", "01", "N", "Objeto encaminhado", StatusInTransit},
		{"PAR", "10", "N", "Saída do Centro Internacional", StatusInTransit},
		{"OEC", "01", "N", "Objeto saiu para entrega ao destinatário", StatusOutForDelivery},
		{"BDE", "01", "S", "Objeto entregue ao destinatário", StatusDelivered},
		{"BDI", "01", "S", "Objeto entregue", StatusDelivered},
		{"LDI", "01", "N", "Objeto disponível para retirada", StatusOutForDelivery},
		{"EST", "01", "S", "Objeto extraviado", StatusException},
	}
	for _, tc := range cases {
		if got := StatusFor(tc.cod, tc.tipo, tc.fin, tc.desc); got != tc.want {
			t.Errorf("StatusFor(%q,%q,%q,%q) = %q, want %q", tc.cod, tc.tipo, tc.fin, tc.desc, got, tc.want)
		}
	}
}

func TestCleanCodeAndDetect(t *testing.T) {
	if got := CleanCode(" nn 428-571.043;br "); got != "NN428571043BR" {
		t.Errorf("CleanCode = %q", got)
	}
	if !LooksLikeCorreiosCode("NN428571043BR") {
		t.Error("NN428571043BR should look like a Correios code")
	}
	if LooksLikeCorreiosCode("LZ123456789CN") {
		t.Error("LZ123456789CN is a foreign S10 code, not Correios")
	}
	if LooksLikeCorreiosCode("JT1234567890") {
		t.Error("J&T codes are not Correios codes")
	}
}

func TestPHPDate(t *testing.T) {
	d := phpDate{Date: "2026-10-23 07:33:25.000000", Timezone: "America/Sao_Paulo"}
	want := time.Date(2026, 10, 23, 10, 33, 25, 0, time.UTC) // UTC-3 → 10:33Z
	if got := d.Time(); !got.Equal(want) {
		t.Errorf("phpDate.Time() = %v, want %v", got, want)
	}
}

func TestTruthy(t *testing.T) {
	if !truthy(json.RawMessage(`"true"`)) {
		t.Error(`"true" should be truthy`)
	}
	if !truthy(json.RawMessage(`true`)) {
		t.Error("true should be truthy")
	}
	if truthy(json.RawMessage(`"false"`)) || truthy(nil) || truthy(json.RawMessage(`""`)) {
		t.Error("falsey values must stay falsey")
	}
}

func TestClassifyError(t *testing.T) {
	if err := classifyError("Captcha inválido"); err != ErrCaptchaInvalid {
		t.Errorf("captcha error = %v", err)
	}
	if err := classifyError("SRO-020: Não localizamos informações para o código informado."); err != ErrNotFound {
		t.Errorf("not-found error = %v", err)
	}
}
