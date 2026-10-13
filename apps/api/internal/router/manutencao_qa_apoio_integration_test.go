//go:build integration

// Apoio da bateria de QA das ordens de manutenção (spec §12, tag `Manutenção`
// da OpenAPI, recurso `maintenance`), pela PORTA DA FRENTE: o router completo
// de `router.New`, sessões abertas por `/auth/login` com perfis do seed, e o
// contrato lido de `openapi/openapi.yaml`.
//
// Por que aqui e não em `internal/modules/manutencao`: os testes do módulo
// montam um chi próprio com as linhas de `rotasManutencao` e assinam o token
// direto no emissor. Isso prova o módulo, mas não prova a aplicação montada —
// nem a FRONTEIRA com reservas (a reserva que a ordem recusa, a reserva que a
// ordem libera), com o mapa de ocupação e com as avarias de bens. É ali, entre
// módulos, que um contrato muda sem avisar o vizinho.
//
// A lista das 10 operações e o par (recurso, ação) de cada uma vêm do CONTRATO
// (tag `Manutenção` e `x-rbac`), nunca de `rotas_manutencao.go`.
//
// Reaproveita `bens_qa_apoio` (envelope de erro, faxina, segunda casa, perfis
// do seed) e acrescenta o que a ordem traz de novo: o produto vendável próprio
// do teste (para vender e bloquear a MESMA unidade sem tocar o calendário do
// seed) e a faxina de ordens, bloqueios e reservas.
package router

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

// ─────────────────────────── O contrato ─────────────────────────────────────

// qmOperacoesDeManutencao lê do contrato as operações com `tags: [Manutenção]`
// e o x-rbac de cada uma. Mesmo scanner de indentação de `qbOperacoesDeBens`.
func qmOperacoesDeManutencao(t *testing.T) []qbOperacao {
	t.Helper()

	bruto, err := os.ReadFile(caminhoDaOpenAPI)
	if err != nil {
		t.Fatalf("lendo o contrato: %v", err)
	}
	type acumulada struct {
		ehDaTag         bool
		recurso, acao   string
		metodo, caminho string
	}
	var (
		ordem []*acumulada
		atual *acumulada
		path  string
	)
	for _, linha := range strings.Split(string(bruto), "\n") {
		if linha != "" && linha[0] != ' ' && linha[0] != '#' {
			path, atual = "", nil
			continue
		}
		if m := linhaDePath.FindStringSubmatch(linha); m != nil {
			path, atual = m[1], nil
			continue
		}
		if path == "" {
			continue
		}
		if m := linhaDeVerbo.FindStringSubmatch(linha); m != nil {
			atual = &acumulada{metodo: strings.ToUpper(m[1]), caminho: path}
			ordem = append(ordem, atual)
			continue
		}
		if atual == nil {
			continue
		}
		if strings.HasPrefix(linha, "      tags:") && strings.Contains(linha, "Manutenção") {
			atual.ehDaTag = true
		}
		if m := qbLinhaDeRBAC.FindStringSubmatch(linha); m != nil {
			atual.recurso, atual.acao = m[1], m[2]
		}
	}

	var out []qbOperacao
	for _, a := range ordem {
		if !a.ehDaTag {
			continue
		}
		if a.recurso == "" || a.acao == "" {
			t.Fatalf("%s %s está na tag Manutenção sem x-rbac legível", a.metodo, a.caminho)
		}
		out = append(out, qbOperacao{Metodo: a.metodo, Path: a.caminho, Recurso: a.recurso, Acao: a.acao})
	}
	if len(out) != 10 {
		t.Fatalf("o contrato tem %d operações na tag Manutenção; o pedido desta fatia fala em 10 — o scanner ficou defasado ou o contrato mudou", len(out))
	}
	return out
}

// ─────────────────────────── A resposta ─────────────────────────────────────

type qmBloqueio struct {
	ID     uuid.UUID `json:"id"`
	De     string    `json:"from"`
	Ate    string    `json:"to"`
	Noites int       `json:"nights"`
	Status string    `json:"status"`
	Fase   string    `json:"phase"`
}

type qmAvariaDaOrdem struct {
	ID       uuid.UUID `json:"id"`
	Desfecho *string   `json:"resolution"`
}

type qmOrdem struct {
	ID         uuid.UUID        `json:"id"`
	UnidadeID  uuid.UUID        `json:"unit_id"`
	Codigo     string           `json:"unit_code"`
	ComodoID   *uuid.UUID       `json:"room_id"`
	BemID      *uuid.UUID       `json:"item_id"`
	AvariaID   *uuid.UUID       `json:"issue_id"`
	Avaria     *qmAvariaDaOrdem `json:"issue"`
	Titulo     string           `json:"title"`
	Descricao  *string          `json:"description"`
	Prioridade string           `json:"priority"`
	Status     string           `json:"status"`
	Custo      *int64           `json:"cost_cents"`
	Bloqueio   *qmBloqueio      `json:"block"`
	AbertaEm   time.Time        `json:"opened_at"`
	IniciadaEm *time.Time       `json:"started_at"`
	FechadaEm  *time.Time       `json:"closed_at"`
	Acoes      []string         `json:"allowed_actions"`
	Editavel   string           `json:"editable"`
}

func qmPeriodo(de, ate calendar.Date) map[string]string {
	return map[string]string{"from": de.String(), "to": ate.String()}
}

// qmHoje é o dia D no fuso da casa — a mesma fonte do módulo, lida do banco.
func (a *ambiente) qmHoje(t *testing.T, propriedade uuid.UUID) calendar.Date {
	t.Helper()
	var d string
	if err := a.pool.QueryRow(a.ctx,
		`SELECT (now() AT TIME ZONE timezone)::date::text FROM properties WHERE id = $1`, propriedade).Scan(&d); err != nil {
		t.Fatal(err)
	}
	return calendar.MustParse(d)
}

// qmAbrir cria a ordem pela API e exige o 201.
func (a *ambiente) qmAbrir(t *testing.T, token string, corpo map[string]any) qmOrdem {
	t.Helper()
	return qbDado[qmOrdem](t, a.chamar(t, http.MethodPost, "/maintenance-orders", token, corpo), http.StatusCreated, "POST /maintenance-orders")
}

func (a *ambiente) qmLer(t *testing.T, token string, id uuid.UUID) qmOrdem {
	t.Helper()
	return qbDado[qmOrdem](t, a.chamar(t, http.MethodGet, "/maintenance-orders/"+id.String(), token, nil), http.StatusOK, "GET /maintenance-orders/{id}")
}

// qmMoverBloqueio reescreve o período da linha pelo banco: é o RELÓGIO andando.
// A API não aceita bloqueio que começa no passado; um bloqueio "em curso" é um
// que foi pedido antes e cujo período o tempo alcançou.
func (a *ambiente) qmMoverBloqueio(t *testing.T, bloco uuid.UUID, de, ate calendar.Date) {
	t.Helper()
	if _, err := a.pool.Exec(a.ctx, `UPDATE stay_blocks SET period = daterange($2::date, $3::date, '[)') WHERE id = $1`,
		bloco, de.String(), ate.String()); err != nil {
		t.Fatalf("movendo o bloqueio no tempo: %v", err)
	}
}

// ─────────────────────────── Faxina ─────────────────────────────────────────

// qmFaxina complementa a qbFaxina com o que a ordem cria: a própria ordem, o
// bloqueio dela, as reservas e o produto vendável do teste. Registrada DEPOIS
// da qbFaxina (e dos usuários), roda ANTES dela: a ordem segura o bloqueio, a
// avaria, o cômodo, o bem, a unidade e os usuários por FK RESTRICT.
type qmFaxina struct {
	f        *qbFaxina
	mu       sync.Mutex
	produtos []uuid.UUID
	contatos []uuid.UUID
}

func (a *ambiente) qmFaxina(t *testing.T, f *qbFaxina) *qmFaxina {
	t.Helper()
	m := &qmFaxina{f: f}
	t.Cleanup(func() { m.limpar(t) })
	return m
}

func (m *qmFaxina) limpar(t *testing.T) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.f.mu.Lock()
	unidades := append([]uuid.UUID(nil), m.f.unidades...)
	itens := append([]uuid.UUID(nil), m.f.itens...)
	usuarios := append([]uuid.UUID(nil), m.f.usuarios...)
	m.f.mu.Unlock()

	a := m.f.a
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exec := func(sql string, args ...any) {
		if _, err := a.pool.Exec(ctx, sql, args...); err != nil {
			t.Logf("LIMPEZA INCOMPLETA (%s): %v", sql, err)
		}
	}
	exec(`DELETE FROM maintenance_orders WHERE unit_id = ANY($1) OR item_id = ANY($2) OR opened_by = ANY($3) OR closed_by = ANY($3)`,
		unidades, itens, usuarios)
	for _, p := range m.produtos {
		exec(`DELETE FROM reservation_guests WHERE reservation_id IN (SELECT id FROM reservations WHERE unit_type_id = $1)`, p)
		exec(`DELETE FROM reservations WHERE unit_type_id = $1`, p)
	}
	exec(`DELETE FROM stay_blocks WHERE unit_id = ANY($1)`, unidades)
	exec(`DELETE FROM stay_blocks WHERE created_by = ANY($1) OR owner_id = ANY($1)`, usuarios)
	for _, c := range m.contatos {
		exec(`DELETE FROM pii_access_log WHERE contact_id = $1`, c)
		exec(`DELETE FROM contacts WHERE id = $1`, c)
	}
	for _, p := range m.produtos {
		exec(`DELETE FROM unit_types WHERE id = $1`, p)
	}
}

// qmProduto é um produto vendável do teste, `one_member`, com UMA unidade
// nova: vender e bloquear essa unidade não toca o calendário do seed. Tarifa
// de R$ 100,00 em todo tipo de data da tabela vigente e estadia mínima de uma
// noite, para a venda caber em qualquer dia que o relógio cair.
type qmProduto struct {
	ID      uuid.UUID
	Unidade uuid.UUID
	Codigo  string
}

func (m *qmFaxina) qmProdutoVendavel(t *testing.T, propriedade uuid.UUID) qmProduto {
	t.Helper()
	a := m.f.a
	var p qmProduto
	p.Unidade, p.Codigo = m.f.qbUnidade(t, propriedade, "QA-MANUT-"+strings.ToUpper(qbSufixo()))
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO unit_types (property_id, code, name, capacity, consumes, sort_order)
		VALUES ($1, $2, 'Produto do QA de manutenção', 4, 'one_member', 992) RETURNING id`,
		propriedade, "qa-manut-"+qbSufixo()).Scan(&p.ID); err != nil {
		t.Fatalf("criando o produto: %v", err)
	}
	m.mu.Lock()
	m.produtos = append(m.produtos, p.ID)
	m.mu.Unlock()
	if _, err := a.pool.Exec(a.ctx, `INSERT INTO unit_type_members (unit_type_id, unit_id) VALUES ($1, $2)`, p.ID, p.Unidade); err != nil {
		t.Fatalf("composição do produto: %v", err)
	}
	var tabela uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		SELECT id FROM rate_tables WHERE property_id = $1 AND active ORDER BY valid_from DESC LIMIT 1`, propriedade).Scan(&tabela); err != nil {
		t.Skipf("sem tabela de tarifas vigente: rode o seed (%v)", err)
	}
	if _, err := a.pool.Exec(a.ctx, `
		INSERT INTO rates (rate_table_id, unit_type_id, date_type, amount_cents)
		SELECT $1, $2, t, 10000 FROM (SELECT DISTINCT date_type FROM rates WHERE rate_table_id = $1) AS d(t)`, tabela, p.ID); err != nil {
		t.Fatalf("tarifas do produto: %v", err)
	}
	if _, err := a.pool.Exec(a.ctx, `
		INSERT INTO unit_type_min_nights (rate_table_id, unit_type_id, date_type, nights)
		SELECT $1, $2, t, 1 FROM (SELECT DISTINCT date_type FROM rates WHERE rate_table_id = $1) AS d(t)`, tabela, p.ID); err != nil {
		t.Fatalf("estadia mínima do produto: %v", err)
	}
	return p
}

// qmHospede é o contato da reserva do teste — sem dado pessoal além do nome.
func (m *qmFaxina) qmHospede(t *testing.T, propriedade uuid.UUID) uuid.UUID {
	t.Helper()
	a := m.f.a
	var id uuid.UUID
	s := qbSufixo()
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO contacts (property_id, name, email) VALUES ($1, $2, $3) RETURNING id`,
		propriedade, "QA Manutenção "+s, "qa-manut-"+s+"@exemplo.invalid").Scan(&id); err != nil {
		t.Fatalf("criando o hóspede: %v", err)
	}
	m.mu.Lock()
	m.contatos = append(m.contatos, id)
	m.mu.Unlock()
	return id
}

// qmReservar faz a pré-reserva pela porta da frente, com Idempotency-Key.
func (a *ambiente) qmReservar(t *testing.T, token string, produto, contato uuid.UUID, de, ate calendar.Date) resposta {
	t.Helper()
	return a.chamarComChave(t, http.MethodPost, "/reservations", token, map[string]any{
		"unit_type_id": produto,
		"contact_id":   contato,
		"check_in":     de.String(),
		"check_out":    ate.String(),
		"guests_count": 2,
	}, a.chaveNova(t, "manut"))
}

// qmVendida segura e CONFIRMA a venda (sinal pago): é a "reserva confirmada".
func (a *ambiente) qmVendida(t *testing.T, token string, produto, contato uuid.UUID, de, ate calendar.Date) reservaQA {
	t.Helper()
	r := qbDado[reservaQA](t, a.qmReservar(t, token, produto, contato, de, ate), http.StatusCreated, "POST /reservations")
	exigirStatusQB(t, a.chamarComChave(t, http.MethodPost, "/reservations/"+r.ID.String()+"/confirm", token,
		map[string]any{"deposit_paid_cents": r.Sinal}, a.chaveNova(t, "confirm")), http.StatusOK, "POST /confirm")
	return r
}

// qmCelulas lê o mapa de ocupação (a leitura que a tela do mapa usa) de UMA
// unidade, como dia → célula.
func (a *ambiente) qmCelulas(t *testing.T, token string, unidade uuid.UUID, de, ate calendar.Date) map[string]celulaDoMapa {
	t.Helper()
	caminho := fmt.Sprintf("/availability/units?from=%s&to=%s&unit_id=%s", de, ate, unidade)
	linhas := envelopeDe[[]linhaDoMapaQA](t, a.chamar(t, http.MethodGet, caminho, token, nil), http.StatusOK, "GET /availability/units")
	if len(linhas) != 1 {
		t.Fatalf("o mapa filtrado por uma unidade devolveu %d linhas", len(linhas))
	}
	out := map[string]celulaDoMapa{}
	for _, c := range linhas[0].Dias {
		out[c.Data] = c
	}
	return out
}

// qmContar conta linhas numa consulta de verificação.
func (a *ambiente) qmContar(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(a.ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("contando (%s): %v", sql, err)
	}
	return n
}
