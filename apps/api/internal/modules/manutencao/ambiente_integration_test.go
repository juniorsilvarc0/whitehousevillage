//go:build integration

// Infraestrutura dos testes de integração das ordens de manutenção.
//
// Sobe as rotas do módulo sobre um Postgres real e conversa com elas por HTTP,
// atravessando o autenticador e o middleware de RBAC de verdade. A tabela de
// rotas NÃO é espelhada: vem de `router.Rotas(router.Deps{Manutencao: h})`,
// filtrada pelo recurso — o teste exercita as linhas de
// `internal/router/rotas_manutencao.go`, com o par (recurso, ação) de cada uma.
//
// Cada teste cria as PRÓPRIAS unidades (código `IT-MANUT-…`): o calendário do
// seed fica intocado, e uma data ocupada aqui nunca derruba a venda de outro
// pacote da suíte.
package manutencao_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/manutencao"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/reservas"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/router"
)

const (
	prefixoDaAPI = "/api/v1"
	segredoJWT   = "segredo-de-integracao-da-manutencao-32!"
	senhaDeTeste = "senha-de-integracao-2026"
)

type ambiente struct {
	pool        *pgxpool.Pool
	servidor    *httptest.Server
	emissor     *auth.Emissor
	ctx         context.Context
	propriedade uuid.UUID

	mu       sync.Mutex
	unidades []uuid.UUID
	itens    []uuid.UUID
	usuarios []uuid.UUID
	perfis   []uuid.UUID
}

func subir(t *testing.T) *ambiente {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL ausente: teste de integração pulado")
	}
	ctx := context.Background()
	pool, err := db.New(ctx, url)
	if err != nil {
		t.Fatalf("abrindo pool: %v", err)
	}
	t.Cleanup(pool.Close)

	emissor := auth.NovoEmissor(segredoJWT, 15*time.Minute)
	autenticador := auth.NewAutenticador(emissor, auth.NewRepository(pool))
	h := manutencao.NovoHandler(pool, db.NewTxManager(pool))
	// A rota do calendário que solta bloqueio (`DELETE /blocks/{id}`, de
	// `reservas`) entra também: é ela que não pode soltar o bloqueio da ordem
	// nem o histórico de uma remarcação.
	hr := reservas.NovoHandler(pool, db.NewTxManager(pool))

	r := chi.NewRouter()
	montadas := 0
	r.Route(prefixoDaAPI, func(api chi.Router) {
		api.Use(httpx.RequestID)
		api.Use(audit.Middleware)
		api.Use(autenticador.Middleware)
		for _, rota := range router.Rotas(router.Deps{Manutencao: h, Reservas: hr}) {
			soltarBloqueio := rota.Metodo == http.MethodDelete && rota.Path == "/blocks/{id}"
			if rota.Recurso != manutencao.Recurso && !soltarBloqueio {
				continue
			}
			api.Method(rota.Metodo, rota.Path, auth.Middleware(rota.Recurso, rota.Acao)(rota.Handler))
			if !soltarBloqueio {
				montadas++
			}
		}
	})
	if montadas != 10 {
		t.Fatalf("a tabela de rotas tem %d linhas de %s; o contrato declara 10 operações", montadas, manutencao.Recurso)
	}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	a := &ambiente{pool: pool, servidor: srv, emissor: emissor, ctx: ctx}
	if err := pool.QueryRow(ctx, `SELECT id FROM properties ORDER BY created_at LIMIT 1`).Scan(&a.propriedade); err != nil {
		t.Fatalf("lendo a propriedade do seed (rodou cmd/seed?): %v", err)
	}
	// Uma limpeza só, registrada primeiro e portanto a ÚLTIMA a rodar. A ordem
	// segue as FKs RESTRICT: a ordem segura o bloqueio, a avaria, o cômodo, o
	// bem, a unidade e os usuários (`opened_by`, `closed_by`) — sai primeiro.
	t.Cleanup(func() { a.limpar(t) })
	return a
}

func (a *ambiente) limpar(t *testing.T) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, u := range a.unidades {
		a.executar(t, `DELETE FROM maintenance_orders WHERE unit_id = $1`, u)
	}
	for _, uid := range a.usuarios {
		a.executar(t, `DELETE FROM maintenance_orders WHERE opened_by = $1 OR closed_by = $1`, uid)
	}
	for _, u := range a.unidades {
		a.executar(t, `DELETE FROM stay_blocks WHERE unit_id = $1`, u)
		a.executar(t, `DELETE FROM inventory_issues WHERE room_id IN (SELECT id FROM unit_rooms WHERE unit_id = $1)`, u)
		a.executar(t, `DELETE FROM unit_rooms WHERE unit_id = $1`, u)
	}
	for _, i := range a.itens {
		a.executar(t, `DELETE FROM inventory_issues WHERE item_id = $1`, i)
		a.executar(t, `DELETE FROM inventory_items WHERE id = $1`, i)
	}
	for _, u := range a.unidades {
		a.executar(t, `DELETE FROM units WHERE id = $1`, u)
	}
	for _, uid := range a.usuarios {
		a.executar(t, `DELETE FROM audit_log WHERE actor_id = $1`, uid)
		a.executar(t, `DELETE FROM users WHERE id = $1`, uid)
	}
	for _, p := range a.perfis {
		a.executar(t, `DELETE FROM roles WHERE id = $1`, p)
	}
}

func (a *ambiente) executar(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := a.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Errorf("limpeza (%s): %v", sql, err)
	}
}

// ─────────────────────────── HTTP ───────────────────────────────────

type resposta struct {
	Status    int
	Corpo     []byte
	Cabecalho http.Header
}

func (r resposta) codigo() string {
	var env struct {
		Erro struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(r.Corpo, &env)
	return env.Erro.Code
}

func (r resposta) detalhes(t *testing.T) map[string]any {
	t.Helper()
	var env struct {
		Erro struct {
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(r.Corpo, &env); err != nil {
		t.Fatalf("lendo details: %v (corpo %s)", err, r.Corpo)
	}
	return env.Erro.Details
}

func dado[T any](t *testing.T, r resposta) T {
	t.Helper()
	var env struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(r.Corpo, &env); err != nil {
		t.Fatalf("lendo data: %v (corpo %s)", err, r.Corpo)
	}
	return env.Data
}

type meta struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

func lista[T any](t *testing.T, r resposta) ([]T, meta) {
	t.Helper()
	var env struct {
		Data []T  `json:"data"`
		Meta meta `json:"meta"`
	}
	if err := json.Unmarshal(r.Corpo, &env); err != nil {
		t.Fatalf("lendo a listagem: %v (corpo %s)", err, r.Corpo)
	}
	return env.Data, env.Meta
}

// chamar faz a requisição JSON. `corpo` string vai cru; outro valor é
// serializado; nil não manda corpo.
func (a *ambiente) chamar(t *testing.T, metodo, caminho, token string, corpo any) resposta {
	t.Helper()
	var body io.Reader
	switch c := corpo.(type) {
	case nil:
	case string:
		body = bytes.NewReader([]byte(c))
	default:
		bruto, err := json.Marshal(corpo)
		if err != nil {
			t.Fatalf("serializando o corpo: %v", err)
		}
		body = bytes.NewReader(bruto)
	}
	req, err := http.NewRequestWithContext(a.ctx, metodo, a.servidor.URL+prefixoDaAPI+caminho, body)
	if err != nil {
		t.Fatalf("montando a requisição: %v", err)
	}
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", metodo, caminho, err)
	}
	defer func() { _ = resp.Body.Close() }()
	lido, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("lendo a resposta: %v", err)
	}
	return resposta{Status: resp.StatusCode, Corpo: lido, Cabecalho: resp.Header}
}

func exigir(t *testing.T, r resposta, status int, contexto string) {
	t.Helper()
	if r.Status != status {
		t.Fatalf("%s: status %d, esperado %d — %s", contexto, r.Status, status, r.Corpo)
	}
}

func exigirErro(t *testing.T, r resposta, status int, code, contexto string) {
	t.Helper()
	if r.Status != status || r.codigo() != code {
		t.Fatalf("%s: %d %s, esperado %d %s — %s", contexto, r.Status, r.codigo(), status, code, r.Corpo)
	}
}

// ─────────────────────────── Perfis e sessões ───────────────────────

func (a *ambiente) perfil(t *testing.T, acoes ...string) uuid.UUID {
	t.Helper()
	return a.perfilDe(t, manutencao.Recurso, acoes...)
}

func (a *ambiente) perfilDe(t *testing.T, recurso string, acoes ...string) uuid.UUID {
	t.Helper()
	codigo := "manut_" + sufixo()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`INSERT INTO roles (code, name, is_system) VALUES ($1, $1, false) RETURNING id`, codigo).Scan(&id); err != nil {
		t.Fatalf("criando perfil: %v", err)
	}
	a.mu.Lock()
	a.perfis = append(a.perfis, id)
	a.mu.Unlock()
	for _, acao := range acoes {
		if _, err := a.pool.Exec(a.ctx,
			`INSERT INTO role_permissions (role_id, resource_code, action, scope) VALUES ($1, $2, $3, 'all')`,
			id, recurso, acao); err != nil {
			t.Fatalf("concedendo %s: %v", acao, err)
		}
	}
	return id
}

func (a *ambiente) perfilDoSeed(t *testing.T, codigo string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `SELECT id FROM roles WHERE code = $1`, codigo).Scan(&id); err != nil {
		t.Fatalf("perfil %s do seed: %v", codigo, err)
	}
	return id
}

func (a *ambiente) sessao(t *testing.T, perfil uuid.UUID) (string, uuid.UUID) {
	t.Helper()
	hash, err := auth.Hash(senhaDeTeste)
	if err != nil {
		t.Fatalf("gerando hash: %v", err)
	}
	var (
		id     uuid.UUID
		codigo string
	)
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO users (property_id, role_id, name, email, password_hash)
		VALUES ($1, $2, 'Encarregada de Teste', $3, $4)
		RETURNING id, (SELECT code FROM roles WHERE id = $2)`,
		a.propriedade, perfil, "manut_"+sufixo()+"@exemplo.invalid", hash).Scan(&id, &codigo); err != nil {
		t.Fatalf("criando usuário: %v", err)
	}
	a.mu.Lock()
	a.usuarios = append(a.usuarios, id)
	a.mu.Unlock()
	assinado, _, err := a.emissor.Issue(id, codigo)
	if err != nil {
		t.Fatalf("assinando token: %v", err)
	}
	return assinado, id
}

// gestor tem as quatro ações de `maintenance`.
func (a *ambiente) gestor(t *testing.T) (string, uuid.UUID) {
	t.Helper()
	return a.sessao(t, a.perfil(t, auth.AcaoVer, auth.AcaoCriar, auth.AcaoEditar, auth.AcaoExcluir))
}

// ─────────────────────────── Fixtures ───────────────────────────────

func sufixo() string { return uuid.NewString()[:8] }

func (a *ambiente) unidade(t *testing.T) (uuid.UUID, string) {
	t.Helper()
	codigo := "IT-MANUT-" + sufixo()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO units (property_id, code, name, sort_order) VALUES ($1, $2, $2, 990) RETURNING id`,
		a.propriedade, codigo).Scan(&id); err != nil {
		t.Fatalf("criando unidade: %v", err)
	}
	a.mu.Lock()
	a.unidades = append(a.unidades, id)
	a.mu.Unlock()
	return id, codigo
}

func (a *ambiente) comodo(t *testing.T, unidade uuid.UUID, nome string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO unit_rooms (property_id, unit_id, code, name, kind) VALUES ($1, $2, $3, $4, 'quarto') RETURNING id`,
		a.propriedade, unidade, "it-"+sufixo(), nome).Scan(&id); err != nil {
		t.Fatalf("criando cômodo: %v", err)
	}
	return id
}

func (a *ambiente) bem(t *testing.T, nome string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO inventory_items (property_id, name, category) VALUES ($1, $2, 'eletro') RETURNING id`,
		a.propriedade, nome+" "+sufixo()).Scan(&id); err != nil {
		t.Fatalf("criando bem: %v", err)
	}
	a.mu.Lock()
	a.itens = append(a.itens, id)
	a.mu.Unlock()
	return id
}

func (a *ambiente) avaria(t *testing.T, comodo, bem uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO inventory_issues (property_id, room_id, item_id, kind, qty, note)
		VALUES ($1, $2, $3, 'avariado', 1, 'parou de gelar') RETURNING id`,
		a.propriedade, comodo, bem).Scan(&id); err != nil {
		t.Fatalf("criando avaria: %v", err)
	}
	return id
}

// hoje é o dia D no fuso da propriedade — a mesma fonte do módulo.
func (a *ambiente) hoje(t *testing.T) calendar.Date {
	t.Helper()
	var d string
	if err := a.pool.QueryRow(a.ctx,
		`SELECT (now() AT TIME ZONE timezone)::date::text FROM properties WHERE id = $1`, a.propriedade).Scan(&d); err != nil {
		t.Fatal(err)
	}
	return calendar.MustParse(d)
}

// moverBloqueio reescreve o período da linha pelo banco: é o RELÓGIO andando.
// A API não aceita bloqueio que começa no passado; um bloqueio "em curso" ou
// "terminado" é um que foi pedido antes e cujo período o tempo alcançou.
func (a *ambiente) moverBloqueio(t *testing.T, bloco uuid.UUID, de, ate calendar.Date) {
	t.Helper()
	if _, err := a.pool.Exec(a.ctx, `UPDATE stay_blocks SET period = daterange($2::date, $3::date, '[)') WHERE id = $1`,
		bloco, de.String(), ate.String()); err != nil {
		t.Fatalf("movendo o bloqueio no tempo: %v", err)
	}
}

// vendavel diz se as noites [de, ate) da unidade ainda podem ser ocupadas:
// tenta ocupá-las de verdade (a constraint decide) e desfaz.
func (a *ambiente) vendavel(t *testing.T, unidade uuid.UUID, de, ate calendar.Date) bool {
	t.Helper()
	var id uuid.UUID
	err := a.pool.QueryRow(a.ctx, `
		INSERT INTO stay_blocks (property_id, unit_id, source, status, period, note)
		VALUES ($1, $2, 'owner_hold', 'confirmed', daterange($3::date, $4::date, '[)'), 'sonda de venda')
		RETURNING id`, a.propriedade, unidade, de.String(), ate.String()).Scan(&id)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23P01" {
		return false
	}
	if err != nil {
		t.Fatalf("sondando a venda de [%s, %s): %v", de, ate, err)
	}
	if _, err := a.pool.Exec(a.ctx, `DELETE FROM stay_blocks WHERE id = $1`, id); err != nil {
		t.Fatalf("desfazendo a sonda: %v", err)
	}
	return true
}

// ─────────────────────────── A resposta ─────────────────────────────

type bloqueioResp struct {
	ID     uuid.UUID `json:"id"`
	De     string    `json:"from"`
	Ate    string    `json:"to"`
	Noites int       `json:"nights"`
	Status string    `json:"status"`
	Fase   string    `json:"phase"`
}

type avariaResp struct {
	ID       uuid.UUID `json:"id"`
	Tipo     string    `json:"kind"`
	Qtd      int       `json:"qty"`
	Desfecho *string   `json:"resolution"`
}

type ordemResp struct {
	ID            uuid.UUID     `json:"id"`
	UnidadeID     uuid.UUID     `json:"unit_id"`
	UnidadeCodigo string        `json:"unit_code"`
	UnidadeNome   string        `json:"unit_name"`
	ComodoID      *uuid.UUID    `json:"room_id"`
	ComodoNome    *string       `json:"room_name"`
	BemID         *uuid.UUID    `json:"item_id"`
	BemNome       *string       `json:"item_name"`
	AvariaID      *uuid.UUID    `json:"issue_id"`
	Avaria        *avariaResp   `json:"issue"`
	Titulo        string        `json:"title"`
	Descricao     *string       `json:"description"`
	Prioridade    string        `json:"priority"`
	Status        string        `json:"status"`
	CustoCents    *int64        `json:"cost_cents"`
	Bloqueio      *bloqueioResp `json:"block"`
	AbertaEm      time.Time     `json:"opened_at"`
	AbertaPor     *uuid.UUID    `json:"opened_by"`
	AbertaPorNome *string       `json:"opened_by_name"`
	IniciadaEm    *time.Time    `json:"started_at"`
	FechadaEm     *time.Time    `json:"closed_at"`
	FechadaPor    *uuid.UUID    `json:"closed_by"`
	AtualizadaEm  time.Time     `json:"updated_at"`
	Acoes         []string      `json:"allowed_actions"`
	Editavel      string        `json:"editable"`
}

// abrir cria a ordem e exige o 201.
func (a *ambiente) abrir(t *testing.T, token string, corpo map[string]any) ordemResp {
	t.Helper()
	r := a.chamar(t, http.MethodPost, "/maintenance-orders", token, corpo)
	exigir(t, r, http.StatusCreated, "abrindo a ordem")
	return dado[ordemResp](t, r)
}

func periodo(de, ate calendar.Date) map[string]string {
	return map[string]string{"from": de.String(), "to": ate.String()}
}

func (a *ambiente) contarTrilha(t *testing.T, acao string, entidade uuid.UUID) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(a.ctx,
		`SELECT count(*) FROM audit_log WHERE action = $1 AND entity_id = $2`, acao, entidade).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func textoDe(v *string) string {
	if v == nil {
		return "<nil>"
	}
	return *v
}

func idTexto(v *uuid.UUID) string {
	if v == nil {
		return "<nil>"
	}
	return v.String()
}
