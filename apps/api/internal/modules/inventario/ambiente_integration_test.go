//go:build integration

// Infraestrutura dos testes de integração do inventário.
//
// Sobe as rotas do módulo sobre um Postgres real e conversa com elas por HTTP,
// atravessando o autenticador e o middleware de RBAC de verdade. Testar
// permissão pelo service seria perguntar ao próprio código sob suspeita se ele
// fez a coisa certa.
//
// O servidor é montado AQUI, e não por `router.New`: o main ainda não injeta
// `Deps.Inventario`, e `internal/router/router.go` não é arquivo deste módulo.
// A tabela abaixo espelha, linha a linha, a de `internal/router/rotas_inventario.go`
// — se as duas divergirem, os testes de permissão passam a testar outra coisa.
package inventario_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/inventario"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

const (
	prefixoDaAPI = "/api/v1"
	segredoJWT   = "segredo-de-integracao-do-inventario-32b"
	senhaDeTeste = "senha-de-integracao-2026"
)

type ambiente struct {
	pool        *pgxpool.Pool
	servidor    *httptest.Server
	emissor     *auth.Emissor
	ctx         context.Context
	propriedade uuid.UUID
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
	h := inventario.NovoHandler(pool, db.NewTxManager(pool))

	r := chi.NewRouter()
	r.Route(prefixoDaAPI, func(api chi.Router) {
		// Espelha o que `internal/router/router.go` monta antes das rotas. Sem
		// eles o módulo audita normalmente, mas `ip`, `user_agent` e
		// `request_id` ficam nulos — e o teste da trilha não teria como provar
		// que as três colunas chegam.
		api.Use(httpx.RequestID)
		api.Use(audit.Middleware)
		api.Use(autenticador.Middleware)
		for _, rota := range rotas(h) {
			api.Method(rota.metodo, rota.path,
				auth.Middleware(inventario.Recurso, rota.acao)(http.HandlerFunc(rota.handler)))
		}
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	var propriedade uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM properties ORDER BY created_at LIMIT 1`).Scan(&propriedade); err != nil {
		t.Fatalf("lendo a propriedade do seed (rodou cmd/seed?): %v", err)
	}

	return &ambiente{pool: pool, servidor: srv, emissor: emissor, ctx: ctx, propriedade: propriedade}
}

type rota struct {
	metodo  string
	path    string
	acao    string
	handler http.HandlerFunc
}

// rotas espelha internal/router/rotas_inventario.go.
func rotas(h *inventario.Handler) []rota {
	return []rota{
		{http.MethodGet, "/properties", auth.AcaoVer, h.ListarPropriedades},
		{http.MethodGet, "/properties/{id}", auth.AcaoVer, h.BuscarPropriedade},
		{http.MethodPatch, "/properties/{id}", auth.AcaoEditar, h.AtualizarPropriedade},

		{http.MethodGet, "/unit-types", auth.AcaoVer, h.ListarProdutos},
		{http.MethodPost, "/unit-types", auth.AcaoCriar, h.CriarProduto},
		{http.MethodGet, "/unit-types/{id}", auth.AcaoVer, h.BuscarProduto},
		{http.MethodPut, "/unit-types/{id}", auth.AcaoEditar, h.SubstituirProduto},
		{http.MethodPatch, "/unit-types/{id}", auth.AcaoEditar, h.AtualizarProduto},
		{http.MethodDelete, "/unit-types/{id}", auth.AcaoExcluir, h.DesativarProduto},
		{http.MethodGet, "/unit-types/{id}/members", auth.AcaoVer, h.Composicao},
		{http.MethodPut, "/unit-types/{id}/members", auth.AcaoEditar, h.SubstituirComposicao},

		{http.MethodGet, "/units", auth.AcaoVer, h.ListarUnidades},
		{http.MethodPost, "/units", auth.AcaoCriar, h.CriarUnidade},
		{http.MethodGet, "/units/{id}", auth.AcaoVer, h.BuscarUnidade},
		{http.MethodPut, "/units/{id}", auth.AcaoEditar, h.SubstituirUnidade},
		{http.MethodPatch, "/units/{id}", auth.AcaoEditar, h.AtualizarUnidade},
		{http.MethodDelete, "/units/{id}", auth.AcaoExcluir, h.DesativarUnidade},
	}
}

// ─────────────────────────── HTTP ───────────────────────────────────

type resposta struct {
	Status int
	Corpo  []byte
}

func (r resposta) codigoDoErro() string {
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

// dado extrai o `data` de uma resposta de recurso único.
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

// lista extrai `data` e `meta` de uma listagem paginada.
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

func (a *ambiente) chamar(t *testing.T, metodo, caminho, token string, corpo any) resposta {
	t.Helper()

	var body io.Reader
	if corpo != nil {
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

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", metodo, caminho, err)
	}
	defer func() { _ = resp.Body.Close() }()

	lido, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("lendo a resposta: %v", err)
	}
	return resposta{Status: resp.StatusCode, Corpo: lido}
}

// ─────────────────────────── Perfis e sessões ───────────────────────

// perfil cria um papel descartável com as células informadas ("recurso:ação").
func (a *ambiente) perfil(t *testing.T, prefixo string, celulas ...string) uuid.UUID {
	t.Helper()

	codigo := fmt.Sprintf("%s_%s", prefixo, uuid.NewString()[:8])
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`INSERT INTO roles (code, name, is_system) VALUES ($1, $1, false) RETURNING id`, codigo).
		Scan(&id); err != nil {
		t.Fatalf("criando perfil %s: %v", codigo, err)
	}
	t.Cleanup(func() { a.executar(t, `DELETE FROM roles WHERE id = $1`, id) })

	for _, celula := range celulas {
		recurso, acao := partesDaCelula(t, celula)
		if _, err := a.pool.Exec(a.ctx,
			`INSERT INTO role_permissions (role_id, resource_code, action, scope) VALUES ($1, $2, $3, 'all')`,
			id, recurso, acao); err != nil {
			t.Fatalf("concedendo %s: %v", celula, err)
		}
	}
	return id
}

func partesDaCelula(t *testing.T, celula string) (recurso, acao string) {
	t.Helper()
	for i := len(celula) - 1; i >= 0; i-- {
		if celula[i] == ':' {
			return celula[:i], celula[i+1:]
		}
	}
	t.Fatalf("célula sem ':' — %q", celula)
	return "", ""
}

// token cria um usuário do perfil e devolve o access token dele. O token é
// assinado direto pelo emissor: o que este pacote testa é autorização, não o
// fluxo de login, que tem suíte própria em internal/auth.
func (a *ambiente) token(t *testing.T, perfilID uuid.UUID) string {
	t.Helper()
	assinado, _ := a.tokenComUsuario(t, perfilID)
	return assinado
}

// tokenComUsuario devolve também o id, para o teste da trilha poder afirmar
// QUEM ficou registrado como autor da escrita.
func (a *ambiente) tokenComUsuario(t *testing.T, perfilID uuid.UUID) (string, uuid.UUID) {
	t.Helper()

	hash, err := auth.Hash(senhaDeTeste)
	if err != nil {
		t.Fatalf("gerando hash: %v", err)
	}

	email := fmt.Sprintf("inv_%s@exemplo.invalid", uuid.NewString()[:8])
	var (
		id     uuid.UUID
		codigo string
	)
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO users (property_id, role_id, name, email, password_hash)
		VALUES ($1, $2, 'Teste Inventário', $3, $4)
		RETURNING id, (SELECT code FROM roles WHERE id = $2)`,
		a.propriedade, perfilID, email, hash).Scan(&id, &codigo); err != nil {
		t.Fatalf("criando usuário: %v", err)
	}
	// A trilha vem ANTES do usuário na limpeza porque `audit_log.actor_id` é
	// uma FK sem ON DELETE: a linha de auditoria sobrevive ao usuário de teste e
	// barraria o DELETE dele. Em produção isso é um ponto a resolver — expurgo
	// de usuário (LGPD) fica bloqueado por rastro guardado —, e está anotado no
	// relatório para o `db-migrations`.
	t.Cleanup(func() {
		a.executar(t, `DELETE FROM audit_log WHERE actor_id = $1`, id)
		a.executar(t, `DELETE FROM users WHERE id = $1`, id)
	})

	assinado, _, err := a.emissor.Issue(id, codigo)
	if err != nil {
		t.Fatalf("assinando token: %v", err)
	}
	return assinado, id
}

// ─────────────────────────── Fixtures ───────────────────────────────

func sufixo() string { return uuid.NewString()[:8] }

// produtoDireto insere um produto pelo banco, sem passar pela API — o teste que
// exercita DELETE não deve depender do POST estar correto.
func (a *ambiente) produtoDireto(t *testing.T, consome string) (uuid.UUID, string) {
	t.Helper()

	codigo := "it-prod-" + sufixo()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO unit_types (property_id, code, name, capacity, consumes, sort_order)
		VALUES ($1, $2, 'Produto de integração', 4, $3, 900)
		RETURNING id`, a.propriedade, codigo, consome).Scan(&id); err != nil {
		t.Fatalf("criando produto: %v", err)
	}
	t.Cleanup(func() { a.executar(t, `DELETE FROM unit_types WHERE id = $1`, id) })
	return id, codigo
}

func (a *ambiente) unidadeDireta(t *testing.T, codigo string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO units (property_id, code, name, sort_order)
		VALUES ($1, $2, $2, 900)
		RETURNING id`, a.propriedade, codigo).Scan(&id); err != nil {
		t.Fatalf("criando unidade %s: %v", codigo, err)
	}
	t.Cleanup(func() { a.executar(t, `DELETE FROM units WHERE id = $1`, id) })
	return id
}

// contato existe porque reservations.contact_id é NOT NULL.
func (a *ambiente) contato(t *testing.T) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO contacts (property_id, name, email)
		VALUES ($1, 'Hóspede de integração', $2)
		RETURNING id`, a.propriedade, "hospede_"+sufixo()+"@exemplo.invalid").Scan(&id); err != nil {
		t.Fatalf("criando contato: %v", err)
	}
	t.Cleanup(func() { a.executar(t, `DELETE FROM contacts WHERE id = $1`, id) })
	return id
}

// reserva insere uma reserva no estado pedido. `code` é omitido de propósito:
// quem o gera é o DEFAULT proximo_codigo_reserva() do banco.
func (a *ambiente) reserva(t *testing.T, produtoID uuid.UUID, status string) uuid.UUID {
	t.Helper()

	contato := a.contato(t)
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO reservations (property_id, unit_type_id, contact_id, status,
		                          check_in, check_out, guests_count)
		VALUES ($1, $2, $3, $4, current_date + 30, current_date + 33, 2)
		RETURNING id`, a.propriedade, produtoID, contato, status).Scan(&id); err != nil {
		t.Fatalf("criando reserva %s: %v", status, err)
	}
	t.Cleanup(func() { a.executar(t, `DELETE FROM reservations WHERE id = $1`, id) })
	return id
}

// bloqueio ocupa a unidade num intervalo FUTURO, half-open [in, out).
func (a *ambiente) bloqueio(t *testing.T, unidadeID uuid.UUID, status string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO stay_blocks (property_id, unit_id, source, status, period, expires_at)
		VALUES ($1, $2, 'maintenance', $3,
		        daterange(current_date + 40, current_date + 42, '[)'),
		        CASE WHEN $3 = 'hold' THEN now() + interval '1 day' END)
		RETURNING id`, a.propriedade, unidadeID, status).Scan(&id); err != nil {
		t.Fatalf("criando bloqueio: %v", err)
	}
	t.Cleanup(func() { a.executar(t, `DELETE FROM stay_blocks WHERE id = $1`, id) })
	return id
}

func (a *ambiente) executar(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := a.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Errorf("limpeza (%s): %v", sql, err)
	}
}

func (a *ambiente) ativoDaUnidade(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	var ativa bool
	if err := a.pool.QueryRow(a.ctx, `SELECT active FROM units WHERE id = $1`, id).Scan(&ativa); err != nil {
		t.Fatalf("lendo active da unidade: %v", err)
	}
	return ativa
}

// unidadeDiretaAssim insere a unidade já no estado pedido — é como o teste
// reproduz o banco que a Fase 1 deixou: unidade INATIVA ainda dentro da
// composição de um produto exclusivo.
func (a *ambiente) unidadeDiretaAssim(t *testing.T, codigo string, ativa bool) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO units (property_id, code, name, sort_order, active)
		VALUES ($1, $2, $2, 900, $3)
		RETURNING id`, a.propriedade, codigo, ativa).Scan(&id); err != nil {
		t.Fatalf("criando unidade %s: %v", codigo, err)
	}
	t.Cleanup(func() { a.executar(t, `DELETE FROM units WHERE id = $1`, id) })
	return id
}

// compor grava a composição direto no banco, sem passar pela API — o teste da
// REATIVAÇÃO precisa montar um estado que a API (agora) recusa criar.
func (a *ambiente) compor(t *testing.T, produtoID uuid.UUID, unidades ...uuid.UUID) {
	t.Helper()
	for _, unidade := range unidades {
		if _, err := a.pool.Exec(a.ctx,
			`INSERT INTO unit_type_members (unit_type_id, unit_id) VALUES ($1, $2)`,
			produtoID, unidade); err != nil {
			t.Fatalf("compondo produto: %v", err)
		}
	}
	t.Cleanup(func() {
		a.executar(t, `DELETE FROM unit_type_members WHERE unit_type_id = $1`, produtoID)
	})
}

// bloqueioDaReserva ocupa a unidade PELA reserva, nas mesmas datas que o helper
// `reserva` usa. É o bloco que a venda da casa inteira insere por unidade.
func (a *ambiente) bloqueioDaReserva(t *testing.T, unidadeID, reservaID uuid.UUID) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO stay_blocks (property_id, unit_id, reservation_id, source, status, period)
		VALUES ($1, $2, $3, 'reservation', 'confirmed',
		        daterange(current_date + 30, current_date + 33, '[)'))
		RETURNING id`, a.propriedade, unidadeID, reservaID).Scan(&id); err != nil {
		t.Fatalf("criando bloco da reserva: %v", err)
	}
	t.Cleanup(func() { a.executar(t, `DELETE FROM stay_blocks WHERE id = $1`, id) })
	return id
}

// codigoDaReserva lê o código gerado pelo DEFAULT do banco.
func (a *ambiente) codigoDaReserva(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var codigo string
	if err := a.pool.QueryRow(a.ctx, `SELECT code FROM reservations WHERE id = $1`, id).Scan(&codigo); err != nil {
		t.Fatalf("lendo o código da reserva: %v", err)
	}
	return codigo
}

// linhaDaTrilha é uma linha de audit_log como o teste a inspeciona.
type linhaDaTrilha struct {
	Acao      string
	Entidade  string
	Ator      uuid.UUID
	Antes     []byte
	Depois    []byte
	IP        *string
	UserAgent *string
	RequestID *string
}

// trilhaDaEntidade devolve o que foi gravado sobre uma entidade, em ordem.
func (a *ambiente) trilhaDaEntidade(t *testing.T, entidade string, id uuid.UUID) []linhaDaTrilha {
	t.Helper()

	linhas, err := a.pool.Query(a.ctx, `
		SELECT action, entity, actor_id, before, after, host(ip), user_agent, request_id
		  FROM audit_log
		 WHERE entity = $1 AND entity_id = $2
		 ORDER BY at ASC`, entidade, id)
	if err != nil {
		t.Fatalf("lendo audit_log: %v", err)
	}
	defer linhas.Close()

	out := []linhaDaTrilha{}
	for linhas.Next() {
		var l linhaDaTrilha
		if err := linhas.Scan(&l.Acao, &l.Entidade, &l.Ator, &l.Antes, &l.Depois,
			&l.IP, &l.UserAgent, &l.RequestID); err != nil {
			t.Fatalf("lendo linha de audit_log: %v", err)
		}
		out = append(out, l)
	}
	if err := linhas.Err(); err != nil {
		t.Fatalf("lendo audit_log: %v", err)
	}
	return out
}
