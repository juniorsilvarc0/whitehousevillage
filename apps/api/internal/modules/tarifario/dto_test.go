package tarifario

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/booking"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

func TestDataViajaComoTextoISO(t *testing.T) {
	var d Data
	if err := json.Unmarshal([]byte(`"2026-12-31"`), &d); err != nil {
		t.Fatalf("lendo a data: %v", err)
	}
	if got := d.String(); got != "2026-12-31" {
		t.Fatalf("String() = %q, esperado 2026-12-31", got)
	}

	bruto, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("escrevendo a data: %v", err)
	}
	if string(bruto) != `"2026-12-31"` {
		t.Fatalf("MarshalJSON = %s, esperado \"2026-12-31\"", bruto)
	}

	// Meia-noite UTC: a coluna é `date` e não guarda hora. Qualquer outro
	// horário abriria a chance de o driver arredondar para o dia vizinho.
	if h, m, s := d.Tempo().Clock(); h|m|s != 0 {
		t.Fatalf("Tempo() = %v, esperado meia-noite", d.Tempo())
	}
}

func TestDataRecusaTextoQueNaoEData(t *testing.T) {
	for _, bruto := range []string{`"31/12/2026"`, `"2026-13-40"`, `20261231`, `""`} {
		var d Data
		if err := json.Unmarshal([]byte(bruto), &d); err == nil {
			t.Errorf("%s foi aceito como data", bruto)
		}
	}
}

// A grade é a tela inteira numa requisição: o par repetido tem de ser recusado
// ANTES da transação, senão o UNIQUE do banco estoura no meio e a tela recebe um
// erro de constraint em vez de saber qual célula ela duplicou.
func TestGradeRecusaParRepetido(t *testing.T) {
	produto := uuid.New()
	g := GradeEntrada{
		TabelaID: uuid.New(),
		Celulas: []CelulaDaGrade{
			{ProdutoID: produto, TipoDeData: "normal", ValorCents: 85000},
			{ProdutoID: uuid.New(), TipoDeData: "normal", ValorCents: 55000},
			{ProdutoID: produto, TipoDeData: "normal", ValorCents: 90000},
		},
	}

	erros := g.Validar()
	if erros["rates"] == "" {
		t.Fatalf("par repetido passou: %v", erros)
	}
}

func TestGradeRecusaCelulaInvalidaApontandoOIndice(t *testing.T) {
	g := GradeEntrada{
		TabelaID: uuid.New(),
		Celulas: []CelulaDaGrade{
			{ProdutoID: uuid.New(), TipoDeData: "normal", ValorCents: 85000},
			{ProdutoID: uuid.New(), TipoDeData: "domingo", ValorCents: 0},
		},
	}

	erros := g.Validar()
	if erros["rates[1].date_type"] == "" {
		t.Errorf("tipo de data inválido não apontou o índice: %v", erros)
	}
	if erros["rates[1].amount_cents"] == "" {
		t.Errorf("valor não positivo não apontou o índice: %v", erros)
	}
}

// Alçada de aprovação abaixo da automática deixaria um intervalo em que o
// desconto é ao mesmo tempo livre e proibido.
func TestPoliticaComercialExigeAprovacaoAcimaDaGestao(t *testing.T) {
	auto, aprovacao := 10.0, 5.0
	e := PoliticaComercialEntrada{DescontoAutoPct: &auto, DescontoAprovacaoPct: &aprovacao}

	if e.Validar()["discount_approval_pct"] == "" {
		t.Fatal("aprovação menor que a automática foi aceita")
	}

	aprovacao = 10.0
	if erros := e.Validar(); len(erros) > 0 {
		t.Fatalf("alçadas iguais deveriam passar: %v", erros)
	}
}

// ─────────────────── Faixas de cancelamento ───────────────────

func faixa(min, max *int, pct float64, rotulo string, ordem int) FaixaEntrada {
	return FaixaEntrada{DiasMin: min, DiasMax: max, DevolucaoPct: &pct, Rotulo: rotulo, Ordem: &ordem}
}

func inteiro(v int) *int { return &v }

// entradaDoDominio traduz booking.DefaultCancellation() para o corpo do PUT.
//
// Este teste é a costura entre a API e o motor: o que a tela grava aqui é o que
// `booking.CancellationPolicy.Simulate` vai consumir. Se as faixas do domínio
// deixarem de ser aceitas por este endpoint, a política vigente da casa passa a
// ser impossível de cadastrar — e ninguém descobriria até a primeira tentativa
// de salvar.
func entradaDoDominio() PoliticaDeCancelamentoEntrada {
	pol := booking.DefaultCancellation()

	faixas := make([]FaixaEntrada, 0, len(pol.Tiers))
	for i, tier := range pol.Tiers {
		var min, max *int
		if tier.MinDaysBefore >= 0 { // -1 no domínio = NULL no SQL = "sem piso"
			min = inteiro(tier.MinDaysBefore)
		}
		if tier.MaxDaysBefore >= 0 {
			max = inteiro(tier.MaxDaysBefore)
		}
		faixas = append(faixas, faixa(min, max, tier.RefundPct, tier.Label, i+1))
	}

	de := Data{}
	_ = json.Unmarshal([]byte(`"2026-01-01"`), &de)
	return PoliticaDeCancelamentoEntrada{Nome: pol.Name, ValidoDe: &de, Faixas: faixas}
}

func TestFaixasDoDominioSaoAceitasPelaAPI(t *testing.T) {
	e := entradaDoDominio()
	e.Normalizar()

	if erros := e.Validar(); len(erros) > 0 {
		t.Fatalf("a política padrão do domínio foi recusada pela API: %v", erros)
	}
}

func TestFaixasRecusamBuraco(t *testing.T) {
	// 30+ devolve tudo, 7 a 20 retém metade, 0 a 6 retém tudo:
	// de 21 a 29 dias não há faixa, e o motor reteria tudo em silêncio.
	e := PoliticaDeCancelamentoEntrada{
		Faixas: []FaixaEntrada{
			faixa(inteiro(30), nil, 100, "Devolução integral", 1),
			faixa(inteiro(7), inteiro(20), 50, "Retenção de 50%", 2),
			faixa(nil, inteiro(6), 0, "Retenção integral", 3),
		},
	}

	msg := e.Validar()["tiers"]
	if msg == "" {
		t.Fatal("buraco entre faixas foi aceito")
	}
	if want := "de 21 a 29"; !contem(msg, want) {
		t.Fatalf("a mensagem não diz onde está o buraco: %q", msg)
	}
}

func TestFaixasRecusamSobreposicao(t *testing.T) {
	e := PoliticaDeCancelamentoEntrada{
		Faixas: []FaixaEntrada{
			faixa(inteiro(25), nil, 100, "Devolução integral", 1),
			faixa(inteiro(7), inteiro(29), 50, "Retenção de 50%", 2),
			faixa(nil, inteiro(6), 0, "Retenção integral", 3),
		},
	}

	if e.Validar()["tiers"] == "" {
		t.Fatal("faixas sobrepostas foram aceitas")
	}
}

// A faixa mais generosa precisa ser aberta: sem teto, ninguém que cancela com
// muita antecedência cai fora da política.
func TestFaixasExigemFaixaAbertaNoTopo(t *testing.T) {
	e := PoliticaDeCancelamentoEntrada{
		Faixas: []FaixaEntrada{
			faixa(inteiro(30), inteiro(365), 100, "Devolução integral", 1),
			faixa(inteiro(7), inteiro(29), 50, "Retenção de 50%", 2),
			faixa(nil, inteiro(6), 0, "Retenção integral", 3),
		},
	}

	if e.Validar()["tiers"] == "" {
		t.Fatal("política que para em 365 dias foi aceita")
	}
}

func TestFaixasExigemCoberturaDesdeZero(t *testing.T) {
	e := PoliticaDeCancelamentoEntrada{
		Faixas: []FaixaEntrada{
			faixa(inteiro(30), nil, 100, "Devolução integral", 1),
			faixa(inteiro(3), inteiro(29), 50, "Retenção de 50%", 2),
		},
	}

	if e.Validar()["tiers"] == "" {
		t.Fatal("política que não cobre de 0 a 2 dias foi aceita")
	}
}

func TestFaixasRecusamOrdemRepetida(t *testing.T) {
	e := PoliticaDeCancelamentoEntrada{
		Faixas: []FaixaEntrada{
			faixa(inteiro(30), nil, 100, "Devolução integral", 1),
			faixa(inteiro(7), inteiro(29), 50, "Retenção de 50%", 1),
			faixa(nil, inteiro(6), 0, "Retenção integral", 3),
		},
	}

	if e.Validar()["tiers[1].sort_order"] == "" {
		t.Fatal("sort_order repetido foi aceito — a chave natural é (policy_id, sort_order)")
	}
}

// Sem `sort_order`, vale a posição no array: é a ordem em que a tela desenhou as
// faixas, e é ela que o motor percorre.
func TestOrdemAusenteVemDaPosicao(t *testing.T) {
	e := entradaDoDominio()
	for i := range e.Faixas {
		e.Faixas[i].Ordem = nil
	}
	e.Normalizar()

	for i, f := range e.Faixas {
		if f.Ordem == nil || *f.Ordem != i+1 {
			t.Fatalf("faixa %d ficou com ordem %v, esperado %d", i, f.Ordem, i+1)
		}
	}
}

// PATCH distingue "não mandei" de "mandei null". Sem isso, limpar o fim da
// vigência ("esta tabela não tem mais data para acabar") seria inexprimível.
func TestPatchDistingueAusenteDeNulo(t *testing.T) {
	var ausente TabelaAtualizar
	if err := json.Unmarshal([]byte(`{}`), &ausente); err != nil {
		t.Fatalf("lendo o patch vazio: %v", err)
	}
	if ausente.ValidoAte.Set {
		t.Fatal("campo ausente foi marcado como enviado")
	}

	var nulo TabelaAtualizar
	if err := json.Unmarshal([]byte(`{"valid_to":null}`), &nulo); err != nil {
		t.Fatalf("lendo o patch com null: %v", err)
	}
	if !nulo.ValidoAte.DeveLimpar() {
		t.Fatal("`valid_to: null` não foi entendido como pedido de limpeza")
	}

	// E `name: null` é erro: o nome é a chave natural da tabela.
	nome := TabelaAtualizar{Nome: httpx.Nulo[string]()}
	if nome.Validar()["name"] == "" {
		t.Fatal("`name: null` foi aceito")
	}
}

func contem(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// quote_validity_days: piso do CHECK do banco (> 0) e teto de digitação do
// domínio (booking.QuoteValidityMaxDays). As bordas aceitas e as recusadas.
func TestPoliticaComercialLimitaValidadeDoOrcamento(t *testing.T) {
	for _, caso := range []struct {
		dias   int
		aceita bool
	}{{0, false}, {1, true}, {15, true}, {365, true}, {366, false}, {-3, false}} {
		e := politicaValida(t, "2026-01-01")
		e.ValidadeOrcamentoDia = optDe(caso.dias)
		_, recusou := e.Validar()["quote_validity_days"]
		if recusou == caso.aceita {
			t.Errorf("quote_validity_days=%d: recusou=%v, esperado aceitar=%v", caso.dias, recusou, caso.aceita)
		}
	}
}
