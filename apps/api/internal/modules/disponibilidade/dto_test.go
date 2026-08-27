package disponibilidade

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

func TestNovaJanela(t *testing.T) {
	casos := []struct {
		nome       string
		de, ate    string
		queroErro  bool
		queroCampo string
		queroDias  int
	}{
		{nome: "três noites", de: "2026-11-20", ate: "2026-11-23", queroDias: 3},
		{nome: "uma noite", de: "2026-11-20", ate: "2026-11-21", queroDias: 1},
		{nome: "from ausente", ate: "2026-11-23", queroErro: true, queroCampo: "from"},
		{nome: "to ausente", de: "2026-11-20", queroErro: true, queroCampo: "to"},
		{nome: "from ilegível", de: "20/11/2026", ate: "2026-11-23", queroErro: true, queroCampo: "from"},
		{nome: "janela vazia", de: "2026-11-20", ate: "2026-11-20", queroErro: true, queroCampo: "to"},
		{nome: "janela invertida", de: "2026-11-23", ate: "2026-11-20", queroErro: true, queroCampo: "to"},
		{nome: "ano bissexto inteiro cabe", de: "2028-01-01", ate: "2029-01-01", queroDias: 366},
		{nome: "acima do teto", de: "2026-01-01", ate: "2027-01-03", queroErro: true, queroCampo: "to"},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			j, err := NovaJanela(c.de, c.ate)
			if c.queroErro {
				if err == nil {
					t.Fatal("esperava erro")
				}
				e := apperr.From(err)
				if e.Status() != 422 {
					t.Errorf("status = %d, quero 422", e.Status())
				}
				detalhes, ok := e.Details.(map[string]string)
				if !ok || detalhes[c.queroCampo] == "" {
					t.Errorf("details = %v, quero mensagem em %q", e.Details, c.queroCampo)
				}
				return
			}
			if err != nil {
				t.Fatalf("NovaJanela: %v", err)
			}
			if got := len(j.Noites()); got != c.queroDias {
				t.Errorf("noites = %d, quero %d", got, c.queroDias)
			}
		})
	}
}

// A janela é half-open: `to` não entra. É a mesma contagem da estadia — o dia
// do check-out fica livre para outro hóspede.
func TestJanelaNaoIncluiODiaFinal(t *testing.T) {
	j, err := NovaJanela("2026-11-20", "2026-11-23")
	if err != nil {
		t.Fatalf("NovaJanela: %v", err)
	}
	var iso []string
	for _, d := range j.Noites() {
		iso = append(iso, d.String())
	}
	if got := strings.Join(iso, ","); got != "2026-11-20,2026-11-21,2026-11-22" {
		t.Errorf("noites = %s", got)
	}
}

func TestUUIDOpcionalDaQuery(t *testing.T) {
	id := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/availability", nil)
	if got, err := UUIDOpcionalDaQuery(req, "unit_type_id"); err != nil || got != nil {
		t.Errorf("filtro ausente = (%v, %v), quero (nil, nil)", got, err)
	}

	req = httptest.NewRequest(http.MethodGet, "/availability?unit_type_id="+id.String(), nil)
	got, err := UUIDOpcionalDaQuery(req, "unit_type_id")
	if err != nil || got == nil || *got != id {
		t.Errorf("filtro válido = (%v, %v)", got, err)
	}

	// Filtro ilegível NÃO pode virar "sem filtro": a tela acharia que filtrou e
	// mostraria o calendário inteiro.
	req = httptest.NewRequest(http.MethodGet, "/availability?unit_type_id=nao-e-uuid", nil)
	if _, err := UUIDOpcionalDaQuery(req, "unit_type_id"); apperr.From(err).Status() != 422 {
		t.Errorf("filtro ilegível = %v, quero 422", err)
	}
}

func TestPedidoValidaSoOFormatoDasDatas(t *testing.T) {
	base := Pedido{UnitTypeID: uuid.New(), CheckIn: "2026-11-20", CheckOut: "2026-11-23", Hospedes: 4}

	if falhas := base.Validar(); len(falhas) != 0 {
		t.Errorf("pedido válido acusou %v", falhas)
	}

	ruim := base
	ruim.CheckIn = "20-11-2026"
	if falhas := ruim.Validar(); falhas["check_in"] == "" {
		t.Errorf("data ilegível passou: %v", falhas)
	}

	// check_out anterior ao check_in é regra do MOTOR, não do DTO: validar aqui
	// criaria dois lugares para o mesmo "não".
	invertido := base
	invertido.CheckIn, invertido.CheckOut = base.CheckOut, base.CheckIn
	if falhas := invertido.Validar(); len(falhas) != 0 {
		t.Errorf("o DTO não deve julgar a ordem das datas, mas acusou %v", falhas)
	}
}

func TestPedidoNormalizaParaOsTiposDoDominio(t *testing.T) {
	versao := 3
	tabela := uuid.New()
	p := Pedido{
		UnitTypeID: uuid.New(), CheckIn: "2026-12-28", CheckOut: "2027-01-03",
		Hospedes: 24, DescontoPct: 7.5, IsEvento: true,
		RateTableID: &tabela, PolicyVersion: &versao,
	}

	e, err := p.Normalizar()
	if err != nil {
		t.Fatalf("Normalizar: %v", err)
	}
	if e.CheckIn != (calendar.Date{Year: 2026, Month: time.December, Day: 28}) {
		t.Errorf("check_in = %v", e.CheckIn)
	}
	if e.CheckOut.String() != "2027-01-03" {
		t.Errorf("check_out = %v", e.CheckOut)
	}
	if e.DescontoPct != 7.5 || !e.IsEvento || *e.PolicyVersion != 3 || *e.RateTableID != tabela {
		t.Errorf("normalização perdeu campo: %+v", e)
	}
}

// O contrato exige `guests_count >= 1` e `unit_type_id`; quem cobra isso é a tag
// do DTO, e o erro tem de chegar com o campo que o painel usa no JSON.
//
// REGRESSÃO do defeito medido em 26/08/2026: o DTO lia `json:"guests"` enquanto
// o contrato e o painel já mandavam `guests_count`, e a tela de orçamento
// respondia `422 {"guests":"é obrigatório."}` com o campo preenchido. Não era
// divergência de documento — era a Fase 1 quebrada ponta a ponta, e derrubava
// junto três testes de `internal/router`.
//
// A asserção é contra o CONTRATO, não contra a implementação: era este teste,
// escrito com o nome antigo, que dava cobertura verde à assimetria.
func TestDecodeDoPedidoAcusaOsCamposObrigatorios(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/quotes",
		strings.NewReader(`{"check_in":"2026-11-20","check_out":"2026-11-23","guests_count":0}`))
	req.Header.Set("Content-Type", "application/json")

	_, err := httpx.Decode[Pedido](req)
	e := apperr.From(err)
	if e.Code != "VALIDATION_ERROR" {
		t.Fatalf("code = %q", e.Code)
	}
	detalhes, ok := e.Details.(map[string]string)
	if !ok {
		t.Fatalf("details = %T", e.Details)
	}
	for _, campo := range []string{"unit_type_id", "guests_count"} {
		if detalhes[campo] == "" {
			t.Errorf("faltou acusar %q em %v", campo, detalhes)
		}
	}
	if detalhes["guests"] != "" {
		t.Errorf("o erro ainda fala do nome antigo `guests`: %v", detalhes)
	}
}

// O CONTROLE do rename: o nome antigo tem de ser RECUSADO, e recusado como campo
// desconhecido. Aceitar os dois nomes para sempre é exatamente como a
// ambiguidade nasceu — `guests` na criação, `guests_count` na edição, e um
// PATCH que respondia 200 sem mudar coisa nenhuma.
func TestDecodeDoPedidoRecusaONomeAntigoDeHospedes(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/quotes",
		strings.NewReader(`{"unit_type_id":"`+uuid.New().String()+
			`","check_in":"2026-11-20","check_out":"2026-11-23","guests":4}`))
	req.Header.Set("Content-Type", "application/json")

	if _, err := httpx.Decode[Pedido](req); err == nil {
		t.Fatal("POST /quotes aceitou o nome antigo `guests` — dois nomes para o mesmo dado")
	} else if e := apperr.From(err); e.Status() != 422 {
		t.Errorf("status = %d, quero 422 (campo desconhecido)", e.Status())
	}
}

// A saída é o contrato: `date` é string ISO, não `{"Year":2026,...}`. É a razão
// de o Orcamento ser DTO próprio em vez de booking.Quote serializado.
func TestOrcamentoSerializaDataComoISO(t *testing.T) {
	o := Orcamento{
		Diarias: []NoiteDoOrcamento{{Data: "2026-11-20", Tipo: calendar.Weekend, Rotulo: "Fim de semana", Preco: 240000}},
		Linhas:  []LinhaDoOrcamento{},
	}
	bruto, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(bruto), `"date":"2026-11-20"`) {
		t.Errorf("saída sem data ISO: %s", bruto)
	}
	for _, chave := range []string{`"night_count"`, `"total_cents"`, `"deposit_cents"`, `"avg_nightly_cents"`, `"rate_table_id"`} {
		if !strings.Contains(string(bruto), chave) {
			t.Errorf("faltou %s na saída", chave)
		}
	}
}

func TestDiaDaSemanaLeAMascaraDoBanco(t *testing.T) {
	// Sexta (5) e sábado (6) — a máscara semeada em cmd/seed/calendario.go.
	mascara := int32(1)<<uint(time.Friday) | int32(1)<<uint(time.Saturday)
	dias := diaDaSemana(mascara)
	if len(dias) != 2 || dias[0] != time.Friday || dias[1] != time.Saturday {
		t.Errorf("diaDaSemana(%d) = %v", mascara, dias)
	}
	if got := diaDaSemana(0); got != nil {
		t.Errorf("máscara zerada deveria dar nenhum dia, deu %v", got)
	}
}
