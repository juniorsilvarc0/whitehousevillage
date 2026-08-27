//go:build integration

// A jornada que prova a Fase 1, ponta a ponta, pela porta da frente.
//
// Por que este arquivo existe se cada módulo já tem a sua suíte: os testes de
// `internal/modules/*` montam SÓ as rotas do próprio módulo. Nenhum deles
// atravessa a fronteira entre disponibilidade (que precifica) e reservas (que
// vende), e é exatamente ali que o dinheiro passa de um lado para o outro. Aqui
// o servidor é o de verdade — `router.New`, com os quatro módulos montados —,
// e a venda percorre a mesma sequência que um corretor percorre na tela:
//
//	consultar a data → orçar → segurar → tentar vender por cima → receber o
//	sinal → cancelar → ver a data voltar ao estoque.
//
// Se um módulo mudar de contrato sem avisar o vizinho, é aqui que quebra.
package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/booking"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/money"
)

// As datas da jornada são as da spec: 20 a 23 de novembro de 2026 na Cobertura.
// Sexta e sábado são fim de semana (R$ 2.400 a diária), domingo é diária normal
// (R$ 1.900) — na White House o hóspede vai embora no domingo, então domingo não
// é fim de semana. Subtotal R$ 6.700 + limpeza R$ 350 = R$ 7.050, sinal metade.
const (
	jornadaEntrada = "2026-11-20"
	jornadaSaida   = "2026-11-23"

	jornadaSubtotal = int64(670000)
	jornadaLimpeza  = int64(35000)
	jornadaTotal    = int64(705000)
	jornadaSinal    = int64(352500)
)

// ─────────────────────────── Apoio ──────────────────────────────────────────

// exigirSeed recusa rodar a jornada num banco só migrado.
//
// A regra da casa é fixture própria, e ela vale para quase tudo. A exceção é
// aqui, e é deliberada: o que esta jornada mede são os VALORES da Tabela V1 —
// tarifa por tipo de data, taxa de limpeza, percentual de sinal, faixas de
// cancelamento. Uma fixture montada pelo teste provaria que o teste sabe somar,
// não que o tarifário publicado da casa fecha em R$ 7.050.
func exigirSeed(t *testing.T, a *ambiente) uuid.UUID {
	t.Helper()

	var propriedade uuid.UUID
	err := a.pool.QueryRow(a.ctx,
		`SELECT id FROM properties WHERE slug = 'white-house-village'`).Scan(&propriedade)
	if err != nil {
		t.Skipf("banco sem seed: rode `go run ./cmd/seed` antes — sem ele a jornada não tem tarifário para conferir (%v)", err)
	}

	var tarifas int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*) FROM rates r
		  JOIN rate_tables rt ON rt.id = r.rate_table_id
		 WHERE rt.property_id = $1`, propriedade).Scan(&tarifas); err != nil || tarifas == 0 {
		t.Skipf("banco sem tarifário semeado (%d tarifas): rode `go run ./cmd/seed`", tarifas)
	}
	return propriedade
}

// vendedor é o perfil que fecha negócio: vê o calendário, orça e emite reserva.
func (a *ambiente) vendedor(t *testing.T) usuarioDeTeste {
	t.Helper()

	perfil := a.criarPerfil(t, "vendas", []auth.Permissao{
		{Resource: "reservations", Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: "reservations", Action: auth.AcaoCriar, Scope: auth.EscopoAll},
		{Resource: "reservations", Action: auth.AcaoEditar, Scope: auth.EscopoAll},
		{Resource: "reservations", Action: auth.AcaoExcluir, Scope: auth.EscopoAll},
		{Resource: "calendar", Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: "calendar", Action: auth.AcaoCriar, Scope: auth.EscopoAll},
		{Resource: "calendar", Action: auth.AcaoExcluir, Scope: auth.EscopoAll},
		{Resource: "quotes", Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: "quotes", Action: auth.AcaoCriar, Scope: auth.EscopoAll},
	})
	return a.criarUsuario(t, "vendas", perfil)
}

// chamarComChave é o `chamar` das três rotas que exigem Idempotency-Key.
func (a *ambiente) chamarComChave(t *testing.T, metodo, caminho, token string, corpo any, chave string) resposta {
	t.Helper()

	var body io.Reader
	if corpo != nil {
		bruto, err := json.Marshal(corpo)
		if err != nil {
			t.Fatalf("serializando o corpo: %v", err)
		}
		body = bytes.NewReader(bruto)
	}

	req, err := http.NewRequestWithContext(a.ctx, metodo, a.servidor.URL+PrefixoDaAPI+caminho, body)
	if err != nil {
		t.Fatalf("montando a requisição: %v", err)
	}
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", chave)

	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", metodo, caminho, err)
	}
	defer resp.Body.Close()

	lido, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("lendo a resposta: %v", err)
	}
	return resposta{Status: resp.StatusCode, Corpo: lido, Headers: resp.Header}
}

// chaveNova devolve uma Idempotency-Key exclusiva do caso e agenda a limpeza.
func (a *ambiente) chaveNova(t *testing.T, rotulo string) string {
	t.Helper()

	chave := "qa-jornada-" + rotulo + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	t.Cleanup(func() { a.executarQA(t, `DELETE FROM idempotency_keys WHERE key = $1`, chave) })
	return chave
}

func (a *ambiente) executarQA(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := a.pool.Exec(a.ctx, sql, args...); err != nil {
		t.Errorf("limpeza (%s): %v", sql, err)
	}
}

// hospedeDaJornada cria o contato e leva embora, no fim, tudo o que nascer dele.
func (a *ambiente) hospedeDaJornada(t *testing.T, propriedade uuid.UUID) uuid.UUID {
	t.Helper()

	marca := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO contacts (property_id, name, email)
		VALUES ($1, $2, $3) RETURNING id`,
		propriedade, "QA Jornada "+marca, "qa-jornada-"+marca+"@exemplo.invalid").Scan(&id); err != nil {
		t.Fatalf("criando o hóspede da jornada: %v", err)
	}
	t.Cleanup(func() {
		a.executarQA(t, `DELETE FROM reservation_guests WHERE contact_id = $1`, id)
		a.executarQA(t, `DELETE FROM reservations WHERE contact_id = $1`, id)
		a.executarQA(t, `DELETE FROM contacts WHERE id = $1`, id)
	})
	return id
}

func (a *ambiente) produtoDoSeed(t *testing.T, propriedade uuid.UUID, codigo string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`SELECT id FROM unit_types WHERE property_id = $1 AND code = $2`, propriedade, codigo).Scan(&id); err != nil {
		t.Fatalf("produto %q do seed: %v", codigo, err)
	}
	return id
}

// ─── Formas de resposta. Só os campos que a jornada afirma alguma coisa sobre ─

type celulaDoProduto struct {
	Data       string `json:"date"`
	Disponivel int    `json:"available"`
	TipoDeData string `json:"date_type"`
	Preco      *int64 `json:"price_cents"`
	MinNoites  int    `json:"min_nights"`
}

type linhaDaDisponibilidade struct {
	UnitTypeID   uuid.UUID         `json:"unit_type_id"`
	UnitTypeCode string            `json:"unit_type_code"`
	Consome      string            `json:"consumes"`
	Dias         []celulaDoProduto `json:"days"`
}

type celulaDoMapa struct {
	Data          string     `json:"date"`
	Status        string     `json:"status"`
	ReservaCodigo *string    `json:"reservation_code"`
	StayBlockID   *uuid.UUID `json:"stay_block_id"`
}

type linhaDoMapaQA struct {
	UnitCode string         `json:"unit_code"`
	Dias     []celulaDoMapa `json:"days"`
}

type orcamentoQA struct {
	Noites        int     `json:"night_count"`
	Subtotal      int64   `json:"subtotal_cents"`
	Desconto      int64   `json:"discount_cents"`
	Limpeza       int64   `json:"cleaning_cents"`
	Total         int64   `json:"total_cents"`
	Sinal         int64   `json:"deposit_cents"`
	Saldo         int64   `json:"balance_cents"`
	MediaPorNoite int64   `json:"avg_nightly_cents"`
	MinNoites     int     `json:"min_nights"`
	PolicyVersion int     `json:"policy_version"`
	RateTableID   *string `json:"rate_table_id"`
	Diarias       []struct {
		Data  string `json:"date"`
		Tipo  string `json:"date_type"`
		Preco int64  `json:"price_cents"`
	} `json:"nights"`
}

type reservaQA struct {
	ID             uuid.UUID  `json:"id"`
	Codigo         string     `json:"code"`
	Status         string     `json:"status"`
	CheckIn        string     `json:"check_in"`
	CheckOut       string     `json:"check_out"`
	Noites         int        `json:"night_count"`
	Subtotal       int64      `json:"subtotal_cents"`
	Limpeza        int64      `json:"cleaning_cents"`
	Total          int64      `json:"total_cents"`
	Sinal          int64      `json:"deposit_cents"`
	Saldo          int64      `json:"balance_cents"`
	RateTableID    *uuid.UUID `json:"rate_table_id"`
	PolicyVersion  *int       `json:"policy_version"`
	CancelPolicyID *uuid.UUID `json:"cancellation_policy_id"`
	HoldExpiraEm   *time.Time `json:"hold_expires_at"`
	ConfirmadaEm   *time.Time `json:"confirmed_at"`
	Unidades       []struct {
		UnitCode string `json:"unit_code"`
	} `json:"units"`
}

type cancelamentoQA struct {
	Rotulo        string `json:"label"`
	Devolucao     int64  `json:"refund_cents"`
	Retido        int64  `json:"retained_cents"`
	SinalPago     int64  `json:"deposit_paid_cents"`
	Antecedencia  int    `json:"days_before"`
	PolicyVersion int    `json:"policy_version"`
	Simulado      bool   `json:"dry_run"`
	Status        string `json:"status"`
}

// envelopeDe lê o `data` de uma resposta de sucesso e falha com o corpo inteiro
// quando o status não é o esperado — a mensagem tem de dizer o que a API disse.
func envelopeDe[T any](t *testing.T, r resposta, esperado int, oQueEra string) T {
	t.Helper()

	if r.Status != esperado {
		t.Fatalf("%s: status %d, esperado %d — corpo: %s", oQueEra, r.Status, esperado, r.Corpo)
	}
	var env struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(r.Corpo, &env); err != nil {
		t.Fatalf("%s: corpo fora do envelope {data}: %v — %s", oQueEra, err, r.Corpo)
	}
	return env.Data
}

// disponibilidadeDa consulta a visão comercial de um produto na janela da jornada.
func (a *ambiente) disponibilidadeDa(t *testing.T, token string, produto uuid.UUID) linhaDaDisponibilidade {
	t.Helper()

	caminho := fmt.Sprintf("/availability?from=%s&to=%s&unit_type_id=%s", jornadaEntrada, jornadaSaida, produto)
	linhas := envelopeDe[[]linhaDaDisponibilidade](t, a.chamar(t, http.MethodGet, caminho, token, nil),
		http.StatusOK, "GET /availability")

	if len(linhas) != 1 {
		t.Fatalf("GET /availability filtrado por um produto devolveu %d linhas, esperado 1", len(linhas))
	}
	return linhas[0]
}

// ─────────────────────────── A jornada ──────────────────────────────────────

func TestJornadaDaFase1DaConsultaAoCancelamento(t *testing.T) {
	a := subirAPI(t)
	propriedade := exigirSeed(t, a)

	vendedor := a.vendedor(t)
	hospede := a.hospedeDaJornada(t, propriedade)
	cobertura := a.produtoDoSeed(t, propriedade, "cobertura")
	completa := a.produtoDoSeed(t, propriedade, "completa")

	// ── 1. A data está à venda, e por quanto ──────────────────────────────
	//
	// O corretor abre o calendário antes de falar preço. As três noites têm de
	// aparecer vendáveis, cada uma com a diária do seu tipo de data.
	antes := a.disponibilidadeDa(t, vendedor.Token, cobertura)
	if antes.UnitTypeCode != "cobertura" {
		t.Fatalf("o filtro trouxe %q em vez da cobertura", antes.UnitTypeCode)
	}
	if len(antes.Dias) != 3 {
		t.Fatalf("a janela [%s, %s) devolveu %d dias, esperado 3 — `to` não entra",
			jornadaEntrada, jornadaSaida, len(antes.Dias))
	}

	esperadas := []struct {
		data  string
		tipo  string
		preco int64
	}{
		{"2026-11-20", "fds", 240000},
		{"2026-11-21", "fds", 240000},
		{"2026-11-22", "normal", 190000},
	}
	for i, e := range esperadas {
		dia := antes.Dias[i]
		if dia.Data != e.data || dia.TipoDeData != e.tipo {
			t.Errorf("dia %d = %s/%s, esperado %s/%s", i, dia.Data, dia.TipoDeData, e.data, e.tipo)
		}
		if dia.Preco == nil {
			t.Fatalf("dia %s veio sem tarifa: a Cobertura não estaria à venda", dia.Data)
		}
		if *dia.Preco != e.preco {
			t.Errorf("diária de %s = %d, esperado %d (Tabela V1)", dia.Data, *dia.Preco, e.preco)
		}
		if dia.Disponivel != 1 {
			t.Errorf("%s: disponível = %d, esperado 1 — a Cobertura é uma unidade só", dia.Data, dia.Disponivel)
		}
	}

	// ── 2. O orçamento fecha em R$ 7.050 ──────────────────────────────────
	orcamento := envelopeDe[orcamentoQA](t,
		a.chamar(t, http.MethodPost, "/quotes", vendedor.Token, map[string]any{
			"unit_type_id": cobertura,
			"check_in":     jornadaEntrada,
			"check_out":    jornadaSaida,
			"guests_count": 4,
		}), http.StatusOK, "POST /quotes")

	for _, caso := range []struct {
		campo string
		got   int64
		quer  int64
	}{
		{"subtotal_cents", orcamento.Subtotal, jornadaSubtotal},
		{"cleaning_cents", orcamento.Limpeza, jornadaLimpeza},
		{"discount_cents", orcamento.Desconto, 0},
		{"total_cents", orcamento.Total, jornadaTotal},
		{"deposit_cents", orcamento.Sinal, jornadaSinal},
		{"balance_cents", orcamento.Saldo, jornadaTotal - jornadaSinal},
	} {
		if caso.got != caso.quer {
			t.Errorf("orçamento.%s = %d, esperado %d", caso.campo, caso.got, caso.quer)
		}
	}
	if orcamento.Noites != 3 {
		t.Errorf("night_count = %d, esperado 3", orcamento.Noites)
	}
	// `avg_nightly_cents` é `total / noites` — a limpeza ENTRA na conta. É o que
	// o contrato declara e o que a tela mostra como "diária média"; a asserção
	// existe para que ninguém mude a base do cálculo sem mudar o contrato junto,
	// porque é este número que o corretor repete ao hóspede no telefone.
	if quer := jornadaTotal / 3; orcamento.MediaPorNoite != quer {
		t.Errorf("avg_nightly_cents = %d, esperado %d (total ÷ noites, como manda o schema Orcamento)",
			orcamento.MediaPorNoite, quer)
	}
	if orcamento.RateTableID == nil || orcamento.PolicyVersion == 0 {
		t.Errorf("o orçamento não diz de que tabela e política saiu (tabela=%v versão=%d) — sem isso não dá para reproduzi-lo depois",
			orcamento.RateTableID, orcamento.PolicyVersion)
	}

	// O que o mapa mostra e o que o orçamento cobra têm de ser a MESMA tarifa.
	// Duas fontes de preço na mesma tela é como o hóspede descobre um valor na
	// consulta e outro na proposta.
	if len(orcamento.Diarias) != 3 {
		t.Fatalf("o orçamento detalhou %d diárias, esperado 3", len(orcamento.Diarias))
	}
	for i, diaria := range orcamento.Diarias {
		celula := antes.Dias[i]
		if celula.Preco == nil || diaria.Preco != *celula.Preco || diaria.Tipo != celula.TipoDeData {
			t.Errorf("noite %s: o calendário diz %v/%s e o orçamento cobra %d/%s",
				diaria.Data, celula.Preco, celula.TipoDeData, diaria.Preco, diaria.Tipo)
		}
	}

	// ── 3. A pré-reserva tira a data do mercado por 48 h ──────────────────
	chaveDaVenda := a.chaveNova(t, "criar")
	criada := a.chamarComChave(t, http.MethodPost, "/reservations", vendedor.Token, map[string]any{
		"unit_type_id": cobertura,
		"contact_id":   hospede,
		"check_in":     jornadaEntrada,
		"check_out":    jornadaSaida,
		"guests_count": 4,
	}, chaveDaVenda)

	reserva := envelopeDe[reservaQA](t, criada, http.StatusCreated, "POST /reservations")

	if reserva.Status != "hold" {
		t.Fatalf("a reserva nasceu em %q, esperado hold — quem não pagou ainda não confirmou", reserva.Status)
	}
	if reserva.Total != jornadaTotal || reserva.Sinal != jornadaSinal {
		t.Errorf("a venda gravou total %d / sinal %d, mas o orçamento dizia %d / %d",
			reserva.Total, reserva.Sinal, jornadaTotal, jornadaSinal)
	}
	if len(reserva.Codigo) != 12 || !strings.HasPrefix(reserva.Codigo, "WH-") {
		t.Errorf("code = %q, esperado o formato WH-AAAA-NNNN", reserva.Codigo)
	}
	if reserva.RateTableID == nil || reserva.PolicyVersion == nil || reserva.CancelPolicyID == nil {
		t.Fatalf("a venda não congelou o que usou (tabela=%v política=%v cancelamento=%v): mudar o tarifário amanhã reescreveria esta reserva",
			reserva.RateTableID, reserva.PolicyVersion, reserva.CancelPolicyID)
	}
	if got := len(reserva.Unidades); got != 1 || reserva.Unidades[0].UnitCode != "COB-01" {
		t.Fatalf("unidades alocadas = %+v, esperado só a COB-01", reserva.Unidades)
	}

	// O prazo é o `hold_hours` da política vigente: 48 horas.
	if reserva.HoldExpiraEm == nil {
		t.Fatal("a pré-reserva nasceu sem prazo: nenhum job a expiraria e a data ficaria presa para sempre")
	}
	prazo := time.Until(*reserva.HoldExpiraEm)
	if prazo < 47*time.Hour || prazo > 49*time.Hour {
		t.Errorf("a pré-reserva vence em %s, esperado ~48 h (hold_hours da política V1)", prazo.Round(time.Minute))
	}

	// ── 4. A data saiu do estoque — e a casa inteira caiu junto ───────────
	depois := a.disponibilidadeDa(t, vendedor.Token, cobertura)
	for _, dia := range depois.Dias {
		if dia.Disponivel != 0 {
			t.Errorf("%s: a Cobertura ainda aparece com %d disponível depois de vendida", dia.Data, dia.Disponivel)
		}
	}

	// A Completa consome as OITO unidades. Com a COB-01 ocupada, ela é
	// invendável — e quem tentar tem de ouvir "a data acabou de ser ocupada",
	// não "erro interno".
	conflito := a.chamarComChave(t, http.MethodPost, "/reservations", vendedor.Token, map[string]any{
		"unit_type_id": completa,
		"contact_id":   hospede,
		"check_in":     jornadaEntrada,
		"check_out":    jornadaSaida,
		"guests_count": 10,
	}, a.chaveNova(t, "conflito"))

	if conflito.Status != http.StatusConflict {
		t.Fatalf("vender a Casa Completa por cima da Cobertura devolveu %d, esperado 409 — corpo: %s",
			conflito.Status, conflito.Corpo)
	}
	if code := conflito.codigoDeErro(t); code != "DATE_CONFLICT" {
		t.Errorf("code = %q, esperado DATE_CONFLICT", code)
	}

	// O mapa de ocupação mostra QUEM está na unidade, com o código da reserva.
	mapa := envelopeDe[[]linhaDoMapaQA](t,
		a.chamar(t, http.MethodGet,
			fmt.Sprintf("/availability/units?from=%s&to=%s", jornadaEntrada, jornadaSaida), vendedor.Token, nil),
		http.StatusOK, "GET /availability/units")

	achou := false
	for _, linha := range mapa {
		if linha.UnitCode != "COB-01" {
			continue
		}
		achou = true
		for _, dia := range linha.Dias {
			if dia.Status != "hold" {
				t.Errorf("mapa: COB-01 em %s está %q, esperado hold", dia.Data, dia.Status)
			}
			if dia.ReservaCodigo == nil || *dia.ReservaCodigo != reserva.Codigo {
				t.Errorf("mapa: COB-01 em %s não aponta para %s (veio %v)", dia.Data, reserva.Codigo, dia.ReservaCodigo)
			}
		}
	}
	if !achou {
		t.Error("a COB-01 sumiu do mapa de ocupação")
	}

	// ── 5. O mesmo pedido, a mesma chave: uma venda só ────────────────────
	//
	// É o duplo clique do corretor e o retry do celular no elevador. Duas
	// reservas para a mesma data seriam, além de dinheiro errado, um overbooking
	// que a constraint impediria — devolvendo 409 a quem só apertou de novo.
	repetida := a.chamarComChave(t, http.MethodPost, "/reservations", vendedor.Token, map[string]any{
		"unit_type_id": cobertura,
		"contact_id":   hospede,
		"check_in":     jornadaEntrada,
		"check_out":    jornadaSaida,
		"guests_count": 4,
	}, chaveDaVenda)

	segunda := envelopeDe[reservaQA](t, repetida, http.StatusCreated, "POST /reservations repetido")
	if segunda.ID != reserva.ID {
		t.Fatalf("a repetição criou a reserva %s, diferente da original %s — o hóspede pagaria duas vezes",
			segunda.Codigo, reserva.Codigo)
	}

	var quantas int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*) FROM reservations
		 WHERE contact_id = $1 AND check_in = $2 AND status <> 'cancelled'`,
		hospede, jornadaEntrada).Scan(&quantas); err != nil {
		t.Fatalf("contando as reservas do hóspede: %v", err)
	}
	if quantas != 1 {
		t.Fatalf("o hóspede ficou com %d reservas vivas na mesma data, esperado 1", quantas)
	}

	// ── 6. O sinal entra: a venda vira confirmada ─────────────────────────
	confirmada := envelopeDe[reservaQA](t,
		a.chamarComChave(t, http.MethodPost, "/reservations/"+reserva.ID.String()+"/confirm", vendedor.Token,
			map[string]any{"deposit_paid_cents": jornadaSinal, "method": "pix"}, a.chaveNova(t, "confirmar")),
		http.StatusOK, "POST /reservations/{id}/confirm")

	if confirmada.Status != "confirmed" {
		t.Fatalf("status = %q depois do sinal, esperado confirmed", confirmada.Status)
	}
	if confirmada.ConfirmadaEm == nil {
		t.Error("confirmed_at ficou vazio: o BI não sabe quando a venda fechou")
	}
	if confirmada.HoldExpiraEm != nil {
		t.Errorf("hold_expires_at = %v depois do sinal — o job de expiração derrubaria uma reserva PAGA",
			*confirmada.HoldExpiraEm)
	}
	// O preço não é recalculado na confirmação.
	if confirmada.Total != jornadaTotal {
		t.Errorf("o total mudou na confirmação: %d, esperado %d", confirmada.Total, jornadaTotal)
	}

	// A data continua fora do estoque — o que ela perdeu foi o prazo.
	comSinal := a.disponibilidadeDa(t, vendedor.Token, cobertura)
	for _, dia := range comSinal.Dias {
		if dia.Disponivel != 0 {
			t.Errorf("%s: a Cobertura voltou a aparecer disponível com a reserva CONFIRMADA", dia.Data)
		}
	}

	// ── 7. O cancelamento aplica a política CONGELADA ─────────────────────
	//
	// A antecedência é medida contra o dia da casa, então o valor devolvido
	// depende de quando a suíte roda. O que o teste fixa não é o número: é que a
	// API e o motor de domínio digam a MESMA coisa para a mesma antecedência.
	// Divergir aqui é a gestão prometer um valor na tela e o sistema devolver
	// outro na conta.
	caminhoDoCancelamento := "/reservations/" + reserva.ID.String() + "/cancel"

	simulado := envelopeDe[cancelamentoQA](t,
		a.chamar(t, http.MethodPost, caminhoDoCancelamento+"?dry_run=1", vendedor.Token,
			map[string]any{"reason": "desistencia"}), http.StatusOK, "POST /cancel?dry_run=1")

	if !simulado.Simulado {
		t.Error("dry_run = false numa simulação")
	}
	if simulado.SinalPago != jornadaSinal {
		t.Errorf("a devolução foi calculada sobre %d, esperado o sinal efetivamente pago (%d)",
			simulado.SinalPago, jornadaSinal)
	}
	if simulado.PolicyVersion != *reserva.PolicyVersion {
		t.Errorf("o cancelamento usou a política versão %d, mas a venda congelou a %d",
			simulado.PolicyVersion, *reserva.PolicyVersion)
	}

	esperado := booking.DefaultCancellation().Simulate(money.Cents(jornadaSinal), simulado.Antecedencia)
	if int64(esperado.Refund) != simulado.Devolucao || int64(esperado.Retained) != simulado.Retido {
		t.Errorf("com %d dias de antecedência a API devolve %d / retém %d, e a política da casa manda devolver %d / reter %d",
			simulado.Antecedencia, simulado.Devolucao, simulado.Retido, esperado.Refund, esperado.Retained)
	}

	// A simulação não pode ter executado nada.
	aindaConfirmada := envelopeDe[reservaQA](t,
		a.chamar(t, http.MethodGet, "/reservations/"+reserva.ID.String(), vendedor.Token, nil),
		http.StatusOK, "GET /reservations/{id} depois do dry_run")
	if aindaConfirmada.Status != "confirmed" {
		t.Fatalf("a SIMULAÇÃO cancelou a reserva de verdade: status %q", aindaConfirmada.Status)
	}

	executado := envelopeDe[cancelamentoQA](t,
		a.chamar(t, http.MethodPost, caminhoDoCancelamento, vendedor.Token,
			map[string]any{"reason": "desistencia"}), http.StatusOK, "POST /cancel")

	if executado.Simulado {
		t.Error("dry_run = true numa execução")
	}
	if executado.Devolucao != simulado.Devolucao || executado.Retido != simulado.Retido {
		t.Fatalf("o cancelamento executado (devolve %d / retém %d) divergiu do que a tela prometeu (devolve %d / retém %d)",
			executado.Devolucao, executado.Retido, simulado.Devolucao, simulado.Retido)
	}
	if executado.Status != "cancelled" {
		t.Errorf("status = %q, esperado cancelled", executado.Status)
	}

	// ── 8. A data volta ao estoque, e a casa inteira volta junto ──────────
	livre := a.disponibilidadeDa(t, vendedor.Token, cobertura)
	for _, dia := range livre.Dias {
		if dia.Disponivel != 1 {
			t.Errorf("%s: depois do cancelamento a Cobertura continua com %d disponível — a data ficou presa e ninguém pode vendê-la",
				dia.Data, dia.Disponivel)
		}
	}

	casa := a.disponibilidadeDa(t, vendedor.Token, completa)
	for _, dia := range casa.Dias {
		if dia.Disponivel != 1 {
			t.Errorf("%s: a Casa Completa continua invendável depois de a Cobertura ter sido liberada", dia.Data)
		}
	}

	mapaFinal := envelopeDe[[]linhaDoMapaQA](t,
		a.chamar(t, http.MethodGet,
			fmt.Sprintf("/availability/units?from=%s&to=%s", jornadaEntrada, jornadaSaida), vendedor.Token, nil),
		http.StatusOK, "GET /availability/units depois do cancelamento")

	for _, linha := range mapaFinal {
		if linha.UnitCode != "COB-01" {
			continue
		}
		for _, dia := range linha.Dias {
			if dia.Status != "livre" {
				t.Errorf("mapa: COB-01 em %s ficou %q depois do cancelamento, esperado livre", dia.Data, dia.Status)
			}
		}
	}

	// E a venda cancelada não sumiu: o histórico fica.
	var statusNoBanco string
	if err := a.pool.QueryRow(a.ctx,
		`SELECT status FROM reservations WHERE id = $1`, reserva.ID).Scan(&statusNoBanco); err != nil {
		t.Fatalf("a reserva cancelada sumiu do banco: %v", err)
	}
	if statusNoBanco != "cancelled" {
		t.Errorf("no banco o status ficou %q, esperado cancelled", statusNoBanco)
	}
}

// ─────────────────────────── A retenção de 50% ──────────────────────────────

// Dez dias de antecedência retêm metade do sinal — a faixa do meio da política
// da casa (30+ devolve tudo, 7 a 29 retém metade, abaixo de 7 retém tudo).
//
// Vive separado da jornada de propósito: a antecedência é medida contra o dia
// da casa, então provar a faixa exige um check-in RELATIVO a hoje. Amarrar isso
// às datas fixas de novembro faria o resultado mudar conforme o mês em que a
// suíte roda.
func TestCancelarComDezDiasDeAntecedenciaReteMetadeDoSinal(t *testing.T) {
	a := subirAPI(t)
	propriedade := exigirSeed(t, a)

	vendedor := a.vendedor(t)
	hospede := a.hospedeDaJornada(t, propriedade)
	apto := a.produtoDoSeed(t, propriedade, "apto-2s")

	// O dia da CASA, não o do processo: em UTC, depois das 21 h de Fortaleza, o
	// servidor já virou o dia e a antecedência sairia com um dia a menos.
	var hoje time.Time
	if err := a.pool.QueryRow(a.ctx,
		`SELECT (now() AT TIME ZONE timezone)::date FROM properties WHERE id = $1`, propriedade).Scan(&hoje); err != nil {
		t.Fatalf("lendo o dia da casa: %v", err)
	}
	entrada := hoje.AddDate(0, 0, 10).Format("2006-01-02")
	saida := hoje.AddDate(0, 0, 13).Format("2006-01-02")

	const sinalPago = int64(100000)

	reserva := envelopeDe[reservaQA](t,
		a.chamarComChave(t, http.MethodPost, "/reservations", vendedor.Token, map[string]any{
			"unit_type_id": apto,
			"contact_id":   hospede,
			"check_in":     entrada,
			"check_out":    saida,
			"guests_count": 2,
		}, a.chaveNova(t, "dez-dias")), http.StatusCreated, "POST /reservations")

	envelopeDe[reservaQA](t,
		a.chamarComChave(t, http.MethodPost, "/reservations/"+reserva.ID.String()+"/confirm", vendedor.Token,
			map[string]any{"deposit_paid_cents": sinalPago, "method": "pix"}, a.chaveNova(t, "dez-dias-confirmar")),
		http.StatusOK, "POST /confirm")

	r := envelopeDe[cancelamentoQA](t,
		a.chamar(t, http.MethodPost, "/reservations/"+reserva.ID.String()+"/cancel", vendedor.Token,
			map[string]any{"reason": "desistencia"}), http.StatusOK, "POST /cancel")

	if r.Antecedencia != 10 {
		t.Fatalf("days_before = %d, esperado 10 — a antecedência não está sendo medida no fuso da casa", r.Antecedencia)
	}
	if r.Devolucao != sinalPago/2 || r.Retido != sinalPago/2 {
		t.Fatalf("com 10 dias de antecedência a casa devolveu %d e reteve %d, esperado metade e metade (%d / %d)",
			r.Devolucao, r.Retido, sinalPago/2, sinalPago/2)
	}
}
