//go:build integration

// Apoio da bateria de QA da agenda operacional (spec §9, tag `Agenda` da
// OpenAPI, F2-19 a F2-21), pela PORTA DA FRENTE: o router completo de
// `router.New`, sessões abertas por `/auth/login` com os perfis REAIS do seed
// (`admin`, `usuario`, `corretor`, `vitrine`), e o contrato lido de
// `openapi/openapi.yaml`.
//
// Por que aqui e não em `internal/modules/agenda`: os testes do módulo montam
// um chi próprio e assinam o token direto no emissor, com perfis feitos à mão.
// Isso prova o módulo; não prova a aplicação montada, nem a matriz que o seed
// entrega de verdade, nem a fronteira com a reserva, o contato, a manutenção e
// o calendário pela mesma porta que o painel atravessa.
//
// O que é esperado sai do CONTRATO, não do código: a lista das 15 operações e o
// x-rbac de cada uma vêm do openapi.yaml; os horários esperados vêm de
// `GET /agenda/settings`; e o relógio da casa é calculado aqui com
// `time.LoadLocation(properties.timezone)` e o `now()` do banco — nunca com
// `calendar.Zone`, que é o código sob teste.
package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

// ─────────────────────────── O contrato ─────────────────────────────────────

// qagOperacoesDaAgenda lê do contrato as operações com `tags: [Agenda]` e o
// x-rbac de cada uma. Mesmo scanner de indentação de `qbOperacoesDeBens`.
func qagOperacoesDaAgenda(t *testing.T) []qbOperacao {
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
		if strings.HasPrefix(linha, "      tags:") && strings.Contains(linha, "[Agenda]") {
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
			t.Fatalf("%s %s está na tag Agenda sem x-rbac legível", a.metodo, a.caminho)
		}
		out = append(out, qbOperacao{Metodo: a.metodo, Path: a.caminho, Recurso: a.recurso, Acao: a.acao})
	}
	if len(out) != 15 {
		t.Fatalf("o contrato tem %d operações na tag Agenda; o pedido desta fatia fala em 15 (6 paths) — o scanner ficou defasado ou o contrato mudou", len(out))
	}
	return out
}

// ─────────────────────────── As formas da resposta ──────────────────────────

type qagUnidade struct {
	ID     uuid.UUID `json:"unit_id"`
	Codigo string    `json:"unit_code"`
}

// qagEvento é o schema `EventoDaAgenda`. Os instantes ficam em TEXTO: o
// contrato promete o offset da casa na própria string, e é a string que se
// confere.
type qagEvento struct {
	ID              uuid.UUID    `json:"id"`
	Tipo            string       `json:"type"`
	Origem          string       `json:"origin"`
	Status          string       `json:"status"`
	Titulo          string       `json:"title"`
	Inicio          string       `json:"starts_at"`
	Fim             string       `json:"ends_at"`
	DiaInteiro      bool         `json:"all_day"`
	Data            string       `json:"date"`
	UnidadeID       *uuid.UUID   `json:"unit_id"`
	Unidades        []qagUnidade `json:"units"`
	ReservaID       *uuid.UUID   `json:"reservation_id"`
	ReservaCodigo   *string      `json:"reservation_code"`
	ContatoID       *uuid.UUID   `json:"contact_id"`
	ContatoNome     *string      `json:"contact_name"`
	ResponsavelID   *uuid.UUID   `json:"assignee_id"`
	ResponsavelNome *string      `json:"assignee_name"`
	Notas           *string      `json:"notes"`
	OrdemID         *uuid.UUID   `json:"maintenance_order_id"`
	BloqueioID      *uuid.UUID   `json:"stay_block_id"`
	ConcluidoEm     *string      `json:"done_at"`
	ConcluidoPor    *uuid.UUID   `json:"done_by"`
	CanceladoEm     *string      `json:"cancelled_at"`
	Conflito        bool         `json:"has_unit_conflict"`
	Acoes           []string     `json:"allowed_actions"`
	Editavel        string       `json:"editable"`
	CriadoPor       *uuid.UUID   `json:"created_by"`
	CriadoEm        *string      `json:"created_at"`
	AtualizadoEm    *string      `json:"updated_at"`
}

func (e qagEvento) acoes() string { return strings.Join(e.Acoes, ",") }

func (e qagEvento) codigos() string {
	cs := make([]string, len(e.Unidades))
	for i, u := range e.Unidades {
		cs[i] = u.Codigo
	}
	sort.Strings(cs)
	return strings.Join(cs, ",")
}

type qagAviso struct {
	Tipo     string     `json:"kind"`
	Mensagem string     `json:"message"`
	RefID    *uuid.UUID `json:"ref_id"`
	Inicio   *string    `json:"starts_at"`
	Fim      *string    `json:"ends_at"`
}

// qagEscrita é o schema `EscritaNaAgenda`. `Avisos` nil quer dizer que veio
// `null` ou não veio — o contrato pede `[]` quando não há aviso.
type qagEscrita struct {
	Data qagEvento `json:"data"`
	Meta struct {
		Avisos []qagAviso `json:"warnings"`
	} `json:"meta"`
}

func (e qagEscrita) tipos() string {
	ts := make([]string, len(e.Meta.Avisos))
	for i, a := range e.Meta.Avisos {
		ts[i] = a.Tipo
	}
	sort.Strings(ts)
	return strings.Join(ts, ",")
}

type qagBloqueio struct {
	ID           uuid.UUID  `json:"id"`
	Inicio       string     `json:"starts_at"`
	Fim          string     `json:"ends_at"`
	DiaInteiro   bool       `json:"all_day"`
	Motivo       string     `json:"reason"`
	CriadoPor    *uuid.UUID `json:"created_by"`
	CriadoEm     string     `json:"created_at"`
	AtualizadoEm string     `json:"updated_at"`
}

type qagIntervalo struct {
	Dia    int    `json:"weekday"`
	Inicio string `json:"start"`
	Fim    string `json:"end"`
}

type qagHorarios struct {
	Fuso              string         `json:"timezone"`
	Hoje              string         `json:"today"`
	Entrada           string         `json:"checkin_time"`
	Saida             string         `json:"checkout_time"`
	LimpezaDe         string         `json:"cleaning_start"`
	LimpezaAte        string         `json:"cleaning_end"`
	Funcionamento     []qagIntervalo `json:"business_hours"`
	Passo             int            `json:"slot_minutes"`
	Folga             int            `json:"buffer_minutes"`
	Provisoria        bool           `json:"provisional"`
	AtualizadaEm      string         `json:"updated_at"`
	AtualizadaPor     *uuid.UUID     `json:"updated_by"`
	AtualizadaPorNome *string        `json:"updated_by_name"`
}

// qagReserva é a `Reserva` com o que a agenda precisa dela — inclusive o id
// das unidades alocadas, que `reservaQA` não traz.
type qagReserva struct {
	ID       uuid.UUID `json:"id"`
	Codigo   string    `json:"code"`
	Status   string    `json:"status"`
	CheckIn  string    `json:"check_in"`
	CheckOut string    `json:"check_out"`
	Sinal    int64     `json:"deposit_cents"`
	Unidades []struct {
		ID     uuid.UUID `json:"unit_id"`
		Codigo string    `json:"unit_code"`
	} `json:"units"`
}

// ─────────────────────────── O relógio da casa ──────────────────────────────

// qagCasa é o relógio da propriedade, calculado SEM o código sob teste: o fuso
// é `properties.timezone` carregado pelo `time` da biblioteca padrão, e "hoje"
// é o `now()` do banco convertido pelo próprio Postgres.
type qagCasa struct {
	Prop uuid.UUID
	Fuso string
	Loc  *time.Location
	Hoje calendar.Date
}

func (a *ambiente) qagCasa(t *testing.T, prop uuid.UUID) qagCasa {
	t.Helper()
	var (
		fuso, hoje string
	)
	if err := a.pool.QueryRow(a.ctx,
		`SELECT timezone, (now() AT TIME ZONE timezone)::date::text FROM properties WHERE id = $1`, prop).Scan(&fuso, &hoje); err != nil {
		t.Fatalf("lendo o relógio da casa: %v", err)
	}
	loc, err := time.LoadLocation(fuso)
	if err != nil {
		t.Fatalf("fuso da casa %q: %v", fuso, err)
	}
	return qagCasa{Prop: prop, Fuso: fuso, Loc: loc, Hoje: calendar.MustParse(hoje)}
}

// em é o instante `hh:mm` do dia `d` NA CASA.
func (c qagCasa) em(d calendar.Date, hhmm string) time.Time {
	var h, m int
	if _, err := fmt.Sscanf(hhmm, "%d:%d", &h, &m); err != nil {
		panic("hora de teste malformada: " + hhmm)
	}
	return time.Date(d.Year, d.Month, d.Day, h, m, 0, 0, c.Loc)
}

// rfc é o instante no formato que o contrato promete: RFC 3339 com o offset
// da casa (`2026-11-20T14:00:00-03:00`).
func (c qagCasa) rfc(d calendar.Date, hhmm string) string {
	return c.em(d, hhmm).Format(time.RFC3339)
}

// somar devolve `hh:mm` + minutos, no mesmo formato (só para horas do dia).
func qagSomar(hhmm string, minutos int) string {
	var h, m int
	if _, err := fmt.Sscanf(hhmm, "%d:%d", &h, &m); err != nil {
		panic("hora de teste malformada: " + hhmm)
	}
	total := h*60 + m + minutos
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}

// ─────────────────────────── Faxina ─────────────────────────────────────────

// qagFaxina apaga o que a bateria criou, na ordem das FKs RESTRICT da agenda
// (o evento segura reserva, unidade, contato e usuário; o horário gravado
// segura quem gravou). Tem de ser criada DEPOIS dos usuários: t.Cleanup roda em
// ordem inversa, e o usuário só sai depois que os eventos dele saíram.
type qagFaxina struct {
	a        *ambiente
	mu       sync.Mutex
	unidades []uuid.UUID
	produtos []uuid.UUID
	contatos []uuid.UUID
	usuarios []uuid.UUID
	ordens   []uuid.UUID
}

func (a *ambiente) qagFaxina(t *testing.T, usuarios ...usuarioDeTeste) *qagFaxina {
	t.Helper()
	f := &qagFaxina{a: a}
	for _, u := range usuarios {
		f.usuarios = append(f.usuarios, u.ID)
	}
	t.Cleanup(func() { f.limpar(t) })
	return f
}

func (f *qagFaxina) limpar(t *testing.T) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exec := func(sql string, args ...any) {
		if _, err := f.a.pool.Exec(ctx, sql, args...); err != nil {
			t.Logf("LIMPEZA INCOMPLETA (%s): %v", strings.Join(strings.Fields(sql), " "), err)
		}
	}
	u, p, c, us := f.unidades, f.produtos, f.contatos, f.usuarios
	exec(`
		DELETE FROM agenda_events
		 WHERE unit_id = ANY($1::uuid[]) OR contact_id = ANY($3::uuid[])
		    OR created_by = ANY($4::uuid[]) OR assignee_id = ANY($4::uuid[]) OR done_by = ANY($4::uuid[])
		    OR reservation_id IN (SELECT id FROM reservations
		                           WHERE unit_type_id = ANY($2::uuid[]) OR contact_id = ANY($3::uuid[])
		                              OR created_by = ANY($4::uuid[]) OR owner_id = ANY($4::uuid[]))`, u, p, c, us)
	exec(`DELETE FROM agenda_blocks WHERE created_by = ANY($1::uuid[])`, us)
	// Os horários que a bateria gravou voltam aos provisórios — os outros
	// testes contam com eles, e o seed nunca os reescreve.
	exec(`
		UPDATE agenda_settings
		   SET checkin_time = DEFAULT, checkout_time = DEFAULT, cleaning_start = DEFAULT, cleaning_end = DEFAULT,
		       business_hours = DEFAULT, slot_minutes = DEFAULT, buffer_minutes = DEFAULT,
		       updated_by = NULL, updated_at = now()
		 WHERE updated_by = ANY($1::uuid[])`, us)
	exec(`DELETE FROM maintenance_orders WHERE unit_id = ANY($1::uuid[]) OR id = ANY($2::uuid[])
	         OR opened_by = ANY($3::uuid[]) OR closed_by = ANY($3::uuid[])`, u, f.ordens, us)
	exec(`DELETE FROM reservation_guests WHERE reservation_id IN (SELECT id FROM reservations
	         WHERE unit_type_id = ANY($1::uuid[]) OR contact_id = ANY($2::uuid[]))`, p, c)
	exec(`DELETE FROM reservations WHERE unit_type_id = ANY($1::uuid[]) OR contact_id = ANY($2::uuid[])
	         OR created_by = ANY($3::uuid[]) OR owner_id = ANY($3::uuid[])`, p, c, us)
	exec(`DELETE FROM stay_blocks WHERE unit_id = ANY($1::uuid[]) OR created_by = ANY($2::uuid[]) OR owner_id = ANY($2::uuid[])`, u, us)
	exec(`DELETE FROM unit_types WHERE id = ANY($1::uuid[])`, p)
	exec(`DELETE FROM units WHERE id = ANY($1::uuid[])`, u)
	exec(`DELETE FROM pii_access_log WHERE contact_id = ANY($1::uuid[]) OR actor_id = ANY($2::uuid[])`, c, us)
	exec(`DELETE FROM contacts WHERE id = ANY($1::uuid[])`, c)
}

func (f *qagFaxina) ordem(id uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ordens = append(f.ordens, id)
}

// qagOutraCasa cria a segunda propriedade, com a remoção registrada AGORA —
// antes dos usuários dela — para rodar por último. Ela não passa pela etapa
// `agenda_settings` do seed: a linha dela nasce na primeira leitura.
func (a *ambiente) qagOutraCasa(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO properties (name, slug, timezone) VALUES ('Casa B do QA da agenda', $1, 'America/Fortaleza') RETURNING id`,
		"qa-agenda-b-"+qbSufixo()).Scan(&id); err != nil {
		t.Fatalf("criando a segunda propriedade: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{
			`DELETE FROM agenda_events WHERE property_id = $1`,
			`DELETE FROM agenda_blocks WHERE property_id = $1`,
			`DELETE FROM agenda_settings WHERE property_id = $1`,
			`DELETE FROM stay_blocks WHERE property_id = $1`,
			`DELETE FROM units WHERE property_id = $1`,
			`DELETE FROM pii_access_log WHERE contact_id IN (SELECT id FROM contacts WHERE property_id = $1)`,
			`DELETE FROM contacts WHERE property_id = $1`,
			`DELETE FROM properties WHERE id = $1`,
		} {
			if _, err := a.pool.Exec(ctx, q, id); err != nil {
				t.Logf("LIMPEZA INCOMPLETA da segunda casa (%s): %v", q, err)
			}
		}
	})
	return id
}

// ─────────────────────────── Fixtures ───────────────────────────────────────

// unidade cria a unidade física pelo banco, numa propriedade qualquer.
func (f *qagFaxina) unidade(t *testing.T, prop uuid.UUID) (uuid.UUID, string) {
	t.Helper()
	codigo := "QA-AG-" + strings.ToUpper(qbSufixo())
	var id uuid.UUID
	if err := f.a.pool.QueryRow(f.a.ctx, `
		INSERT INTO units (property_id, code, name, sort_order) VALUES ($1, $2, $3, 993) RETURNING id`,
		prop, codigo, "Unidade "+codigo).Scan(&id); err != nil {
		t.Fatalf("criando unidade %s: %v", codigo, err)
	}
	f.mu.Lock()
	f.unidades = append(f.unidades, id)
	f.mu.Unlock()
	return id, codigo
}

// qagProduto é um produto vendável da bateria, com unidades próprias: vender
// e bloquear essas unidades não toca o calendário do seed.
type qagProduto struct {
	ID       uuid.UUID
	Unidades []uuid.UUID
	Codigos  []string
}

// produto cria o produto com `n` unidades novas e o `consumes` pedido
// (`one_member` é o apartamento; `all_members` é a Completa — ocupa todas).
// Tarifa de R$ 100,00 em todo tipo de data da tabela vigente e estadia mínima
// de uma noite, para a venda caber em qualquer dia em que o relógio cair.
func (f *qagFaxina) produto(t *testing.T, prop uuid.UUID, consome string, n int) qagProduto {
	t.Helper()
	a := f.a
	var p qagProduto
	for i := 0; i < n; i++ {
		id, codigo := f.unidade(t, prop)
		p.Unidades = append(p.Unidades, id)
		p.Codigos = append(p.Codigos, codigo)
	}
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO unit_types (property_id, code, name, capacity, consumes, sort_order)
		VALUES ($1, $2, 'Produto do QA da agenda', 6, $3, 993) RETURNING id`,
		prop, "qa-agenda-"+qbSufixo(), consome).Scan(&p.ID); err != nil {
		t.Fatalf("criando o produto: %v", err)
	}
	f.mu.Lock()
	f.produtos = append(f.produtos, p.ID)
	f.mu.Unlock()
	for _, u := range p.Unidades {
		if _, err := a.pool.Exec(a.ctx, `INSERT INTO unit_type_members (unit_type_id, unit_id) VALUES ($1, $2)`, p.ID, u); err != nil {
			t.Fatalf("composição do produto: %v", err)
		}
	}
	var tabela uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		SELECT id FROM rate_tables WHERE property_id = $1 AND active ORDER BY valid_from DESC LIMIT 1`, prop).Scan(&tabela); err != nil {
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

// qagContato é um contato com TODO dado pessoal preenchido — é o que nenhuma
// resposta da agenda pode carregar além do id e do nome.
type qagContato struct {
	ID        uuid.UUID
	Nome      string
	Email     string
	Telefone  string
	Documento string
}

func (f *qagFaxina) contato(t *testing.T, prop uuid.UUID) qagContato {
	t.Helper()
	s := qbSufixo()
	c := qagContato{
		Nome:      "Cliente da Agenda " + s,
		Email:     "qa-agenda-" + s + "@exemplo.invalid",
		Telefone:  qbTelefone(),
		Documento: fmt.Sprintf("7%010d", int(uuid.New()[1])*1000003%1000000000),
	}
	if err := f.a.pool.QueryRow(f.a.ctx, `
		INSERT INTO contacts (property_id, name, email, phone_e164, doc_type, doc_number)
		VALUES ($1, $2, $3, $4, 'cpf', $5) RETURNING id`,
		prop, c.Nome, c.Email, c.Telefone, c.Documento).Scan(&c.ID); err != nil {
		t.Fatalf("criando contato: %v", err)
	}
	f.mu.Lock()
	f.contatos = append(f.contatos, c.ID)
	f.mu.Unlock()
	return c
}

// ─────────────────────────── A reserva, pela porta da frente ────────────────

func (a *ambiente) qagReservar(t *testing.T, token string, produto, contato uuid.UUID, de, ate calendar.Date) qagReserva {
	t.Helper()
	r := a.chamarComChave(t, http.MethodPost, "/reservations", token, map[string]any{
		"unit_type_id": produto, "contact_id": contato,
		"check_in": de.String(), "check_out": ate.String(), "guests_count": 2,
	}, a.chaveNova(t, "agenda-res"))
	return qbDado[qagReserva](t, r, http.StatusCreated, "POST /reservations")
}

func (a *ambiente) qagConfirmar(t *testing.T, token string, r qagReserva, chave string) resposta {
	t.Helper()
	return a.chamarComChave(t, http.MethodPost, "/reservations/"+r.ID.String()+"/confirm", token,
		map[string]any{"deposit_paid_cents": r.Sinal, "method": "pix"}, chave)
}

// qagVendida segura e CONFIRMA a venda: é a reserva que põe a agenda de pé.
func (a *ambiente) qagVendida(t *testing.T, token string, produto, contato uuid.UUID, de, ate calendar.Date) qagReserva {
	t.Helper()
	r := a.qagReservar(t, token, produto, contato, de, ate)
	exigirStatusQB(t, a.qagConfirmar(t, token, r, a.chaveNova(t, "agenda-conf")), http.StatusOK, "POST /confirm")
	return r
}

// ─────────────────────────── A agenda, pela porta da frente ─────────────────

// qagMarcar faz o POST e exige o 201.
func (a *ambiente) qagMarcar(t *testing.T, token string, corpo map[string]any) qagEscrita {
	t.Helper()
	r := a.chamar(t, http.MethodPost, "/agenda/events", token, corpo)
	if r.Status != http.StatusCreated {
		t.Fatalf("POST /agenda/events: status %d, esperado 201 — %s", r.Status, r.Corpo)
	}
	return qagEscritaDe(t, r)
}

func qagEscritaDe(t *testing.T, r resposta) qagEscrita {
	t.Helper()
	var e qagEscrita
	if err := json.Unmarshal(r.Corpo, &e); err != nil {
		t.Fatalf("lendo EscritaNaAgenda: %v — %s", err, r.Corpo)
	}
	return e
}

func (a *ambiente) qagLer(t *testing.T, token string, id uuid.UUID) qagEvento {
	t.Helper()
	return qbDado[qagEvento](t, a.chamar(t, http.MethodGet, "/agenda/events/"+id.String(), token, nil), http.StatusOK,
		"GET /agenda/events/{id}")
}

// qagDaReserva lista os eventos ligados à reserva, em QUALQUER estado.
func (a *ambiente) qagDaReserva(t *testing.T, token string, reserva uuid.UUID) []qagEvento {
	t.Helper()
	evs, _ := qbLista[qagEvento](t, a.chamar(t, http.MethodGet,
		"/agenda/events?per_page=100&status=agendado,concluido,cancelado&reservation_id="+reserva.String(), token, nil),
		"GET /agenda/events?reservation_id")
	return evs
}

// qagJanela lista a janela inteira de dias da casa, seguindo a paginação.
func (a *ambiente) qagJanela(t *testing.T, token string, de, ate calendar.Date, filtros string) []qagEvento {
	t.Helper()
	var out []qagEvento
	for pagina := 1; ; pagina++ {
		caminho := fmt.Sprintf("/agenda/events?from=%s&to=%s&per_page=100&page=%d", de, ate, pagina)
		if filtros != "" {
			caminho += "&" + filtros
		}
		evs, meta := qbLista[qagEvento](t, a.chamar(t, http.MethodGet, caminho, token, nil), "GET "+caminho)
		out = append(out, evs...)
		if pagina >= meta.TotalPages {
			return out
		}
	}
}

// qagPorTipo indexa os automáticos da reserva pelo tipo — e reprova o segundo.
func qagPorTipo(t *testing.T, evs []qagEvento) map[string]qagEvento {
	t.Helper()
	out := map[string]qagEvento{}
	for _, e := range evs {
		if e.Origem != "reservation" {
			continue
		}
		if _, repetido := out[e.Tipo]; repetido {
			t.Fatalf("dois %s automáticos na mesma reserva: %+v", e.Tipo, evs)
		}
		out[e.Tipo] = e
	}
	return out
}

func qagAchar(evs []qagEvento, id uuid.UUID) (qagEvento, bool) {
	for _, e := range evs {
		if e.ID == id {
			return e, true
		}
	}
	return qagEvento{}, false
}

// qagHorariosDaCasa lê os horários pela API — é deles que sai o esperado.
func (a *ambiente) qagHorariosDaCasa(t *testing.T, token string) qagHorarios {
	t.Helper()
	return qbDado[qagHorarios](t, a.chamar(t, http.MethodGet, "/agenda/settings", token, nil), http.StatusOK, "GET /agenda/settings")
}

// qagCorpoDosHorarios monta o `ConfiguracaoDaAgendaEntrada` com o
// funcionamento de 08:00 às 20:00 nos sete dias.
func qagCorpoDosHorarios(entrada, saida, limpezaDe, limpezaAte string, passo, folga int) map[string]any {
	horario := make([]map[string]any, 0, 7)
	for dia := 1; dia <= 7; dia++ {
		horario = append(horario, map[string]any{"weekday": dia, "start": "08:00", "end": "20:00"})
	}
	return map[string]any{
		"checkin_time": entrada, "checkout_time": saida,
		"cleaning_start": limpezaDe, "cleaning_end": limpezaAte,
		"business_hours": horario, "slot_minutes": passo, "buffer_minutes": folga,
	}
}

// qagExigirJanela confere a janela do evento, em TEXTO: o instante e o offset
// da casa na mesma string.
func qagExigirJanela(t *testing.T, e qagEvento, inicio, fim, contexto string) {
	t.Helper()
	if e.Inicio != inicio || e.Fim != fim {
		t.Errorf("%s: janela %s → %s, esperada %s → %s", contexto, e.Inicio, e.Fim, inicio, fim)
	}
}

// qagContarEventos conta eventos gravados numa consulta de verificação.
func (a *ambiente) qagContar(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(a.ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("contando (%s): %v", sql, err)
	}
	return n
}

// qagDetalhes devolve `error.details` do 4xx.
func qagDetalhes(t *testing.T, r resposta) map[string]any {
	t.Helper()
	var env struct {
		Erro struct {
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(r.Corpo, &env); err != nil {
		t.Fatalf("lendo error.details: %v — %s", err, r.Corpo)
	}
	return env.Erro.Details
}

// qagConflitos devolve `details.conflicts` do 409 UNIT_NOT_AVAILABLE.
func qagConflitos(t *testing.T, r resposta) []map[string]any {
	t.Helper()
	bruto, ok := qagDetalhes(t, r)["conflicts"].([]any)
	if !ok {
		t.Fatalf("409 sem details.conflicts em lista: %s", r.Corpo)
	}
	out := make([]map[string]any, 0, len(bruto))
	for _, c := range bruto {
		m, ok := c.(map[string]any)
		if !ok {
			t.Fatalf("details.conflicts com item que não é objeto: %s", r.Corpo)
		}
		out = append(out, m)
	}
	return out
}
