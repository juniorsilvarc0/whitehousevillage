//go:build integration

// Infraestrutura dos testes de integração do módulo de contatos.
//
// Sobe as rotas sobre um Postgres real e conversa com elas por HTTP,
// atravessando o autenticador e o middleware de RBAC de verdade. Testar pelo
// service pularia justamente o que este módulo tem de mais frágil: a
// deduplicação, que é uma garantia do BANCO, e o escopo, que é uma decisão da
// matriz de permissões resolvida pelo middleware.
//
// A tabela `rotas` abaixo espelha, linha a linha, `internal/router/rotas_contatos.go`
// — se as duas divergirem, os testes de permissão passam a testar outra coisa.
package contatos_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/contatos"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

const (
	prefixoDaAPI = "/api/v1"
	segredoJWT   = "segredo-de-integracao-de-contatos-32b"
	senhaDeTeste = "senha-de-integracao-contatos-2026"
)

// Um pool por PACOTE, e não um por teste: dezenas de aberturas contra o mesmo
// Postgres, cada uma com o ping de boot de 5 s, é o que já fez outra suíte
// deste repositório ver "context deadline exceeded" no primeiro teste.
var abrirPool = sync.OnceValues(func() (*pgxpool.Pool, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return nil, nil
	}
	return db.New(context.Background(), url)
})

func TestMain(m *testing.M) {
	codigo := m.Run()
	if pool, _ := abrirPool(); pool != nil {
		pool.Close()
	}
	os.Exit(codigo)
}

type ambiente struct {
	pool        *pgxpool.Pool
	servidor    *httptest.Server
	emissor     *auth.Emissor
	ctx         context.Context
	propriedade uuid.UUID
	hoje        time.Time
}

func subir(t *testing.T) *ambiente {
	t.Helper()

	pool, err := abrirPool()
	if err != nil {
		t.Fatalf("abrindo pool: %v", err)
	}
	if pool == nil {
		t.Skip("DATABASE_URL ausente: teste de integração pulado")
	}

	ctx := context.Background()
	emissor := auth.NovoEmissor(segredoJWT, 15*time.Minute)
	autenticador := auth.NewAutenticador(emissor, auth.NewRepository(pool))
	h := contatos.NovoHandler(pool, db.NewTxManager(pool))

	r := chi.NewRouter()
	// O middleware de origem entra porque a trilha de auditoria grava IP e
	// user-agent: sem ele, os testes de auditoria passariam com as duas colunas
	// nulas e não provariam nada sobre o caminho real da requisição.
	r.Use(audit.Middleware)
	r.Route(prefixoDaAPI, func(api chi.Router) {
		api.Use(autenticador.Middleware)
		for _, rota := range rotas(h) {
			api.Method(rota.metodo, rota.path,
				auth.Middleware(rota.recurso, rota.acao)(http.HandlerFunc(rota.handler)))
		}
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	a := &ambiente{pool: pool, servidor: srv, emissor: emissor, ctx: ctx}
	var hoje string
	if err := pool.QueryRow(ctx, `
		SELECT id, (now() AT TIME ZONE timezone)::date::text
		  FROM properties ORDER BY created_at LIMIT 1`).Scan(&a.propriedade, &hoje); err != nil {
		t.Fatalf("lendo a propriedade do seed (rodou cmd/seed?): %v", err)
	}
	if a.hoje, err = time.Parse(time.DateOnly, hoje); err != nil {
		t.Fatalf("data da casa ilegível %q: %v", hoje, err)
	}
	return a
}

// dia devolve a data ISO de hoje + n, no fuso da casa.
func (a *ambiente) dia(n int) string { return a.hoje.AddDate(0, 0, n).Format(time.DateOnly) }

type rota struct {
	metodo  string
	path    string
	recurso string
	acao    string
	handler http.HandlerFunc
}

// rotas espelha internal/router/rotas_contatos.go.
func rotas(h *contatos.Handler) []rota {
	const r = contatos.RecursoContatos
	return []rota{
		{http.MethodGet, "/contacts", r, auth.AcaoVer, h.Listar},
		{http.MethodPost, "/contacts", r, auth.AcaoCriar, h.Criar},
		{http.MethodGet, "/contacts/{id}", r, auth.AcaoVer, h.Buscar},
		{http.MethodPut, "/contacts/{id}", r, auth.AcaoEditar, h.Substituir},
		{http.MethodPatch, "/contacts/{id}", r, auth.AcaoEditar, h.Atualizar},
		{http.MethodDelete, "/contacts/{id}", r, auth.AcaoExcluir, h.Excluir},
		{http.MethodPost, "/contacts/{id}/anonymize", r, auth.AcaoExcluir, h.Anonimizar},
		{http.MethodGet, "/contacts/{id}/export", r, auth.AcaoVer, h.Exportar},
	}
}

// ─────────────────────────── HTTP ───────────────────────────────────

type resposta struct {
	Status   int
	Corpo    []byte
	Location string
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

type metaDaLista struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

func lista[T any](t *testing.T, r resposta) ([]T, metaDaLista) {
	t.Helper()
	var env struct {
		Data []T         `json:"data"`
		Meta metaDaLista `json:"meta"`
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

	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", metodo, caminho, err)
	}
	defer func() { _ = resp.Body.Close() }()

	lido, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("lendo a resposta: %v", err)
	}
	return resposta{Status: resp.StatusCode, Corpo: lido, Location: resp.Header.Get("Location")}
}

// chamarBruto manda um corpo JSON literal — é como se testa campo desconhecido
// e tipo errado, que um struct Go não consegue produzir.
func (a *ambiente) chamarBruto(t *testing.T, metodo, caminho, token, corpo string) resposta {
	t.Helper()
	req, err := http.NewRequestWithContext(a.ctx, metodo, a.servidor.URL+prefixoDaAPI+caminho, bytes.NewReader([]byte(corpo)))
	if err != nil {
		t.Fatalf("montando a requisição: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", metodo, caminho, err)
	}
	defer func() { _ = resp.Body.Close() }()
	lido, _ := io.ReadAll(resp.Body)
	return resposta{Status: resp.StatusCode, Corpo: lido}
}

// ─────────────────────────── Perfis e sessões ───────────────────────

func (a *ambiente) perfil(t *testing.T, prefixo string, celulas ...string) uuid.UUID {
	t.Helper()
	return a.perfilComEscopo(t, prefixo, auth.EscopoAll, celulas...)
}

func (a *ambiente) perfilComEscopo(t *testing.T, prefixo, escopo string, celulas ...string) uuid.UUID {
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
			`INSERT INTO role_permissions (role_id, resource_code, action, scope) VALUES ($1,$2,$3,$4)`,
			id, recurso, acao, escopo); err != nil {
			t.Fatalf("concedendo %s: %v", celula, err)
		}
	}
	return id
}

func celulasDeContatos() []string {
	out := make([]string, 0, len(auth.AcoesValidas))
	for _, acao := range auth.AcoesValidas {
		out = append(out, contatos.RecursoContatos+":"+acao)
	}
	return out
}

// gestor é o perfil da maioria dos testes: escopo `all` nas quatro ações.
func (a *ambiente) gestor(t *testing.T) (uuid.UUID, string) {
	t.Helper()
	return a.usuario(t, a.perfil(t, "contatos_gestor", celulasDeContatos()...))
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

func (a *ambiente) usuario(t *testing.T, perfilID uuid.UUID) (uuid.UUID, string) {
	t.Helper()

	hash, err := auth.Hash(senhaDeTeste)
	if err != nil {
		t.Fatalf("gerando hash: %v", err)
	}

	email := fmt.Sprintf("contatos_%s@exemplo.invalid", uuid.NewString()[:8])
	var (
		id     uuid.UUID
		codigo string
	)
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO users (property_id, role_id, name, email, password_hash)
		VALUES ($1, $2, 'Teste Contatos', $3, $4)
		RETURNING id, (SELECT code FROM roles WHERE id = $2)`,
		a.propriedade, perfilID, email, hash).Scan(&id, &codigo); err != nil {
		t.Fatalf("criando usuário: %v", err)
	}
	// A ordem da limpeza importa: `pii_access_log.actor_id` e
	// `audit_log.actor_id` referenciam o usuário e NÃO cascateiam — apagar o
	// usuário antes deixaria 23503 na saída do teste. É a tabela nova deste
	// módulo, e por isso ela vem primeiro na lista.
	t.Cleanup(func() {
		a.executar(t, `DELETE FROM pii_access_log WHERE actor_id = $1`, id)
		a.executar(t, `DELETE FROM audit_log WHERE actor_id = $1`, id)
		a.executar(t, `DELETE FROM users WHERE id = $1`, id)
	})

	assinado, _, err := a.emissor.Issue(id, codigo)
	if err != nil {
		t.Fatalf("assinando token: %v", err)
	}
	return id, assinado
}

// ─────────────────────────── Fixtures ───────────────────────────────

func sufixo() string { return uuid.NewString()[:8] }

// telefoneDeTeste devolve um E.164 sorteado na faixa 9 1xxx xxxx.
//
// Sorteado, e não sequencial: o pacote roda com `-count=10` na etapa de
// concorrência do Makefile, e um contador reiniciaria a cada execução e
// colidiria com o telefone que a execução anterior deixou para trás — a falha
// apareceria como "duplicidade" num teste que não fala de duplicidade.
//
// Fora da faixa `9 0000 xxxx` que o seed usa, para nunca disputar telefone com
// a base semeada.
func telefoneDeTeste(t *testing.T) string {
	t.Helper()
	n, err := rand.Int(rand.Reader, big.NewInt(90000000))
	if err != nil {
		t.Fatalf("sorteando telefone: %v", err)
	}
	return fmt.Sprintf("+55859%08d", n.Int64()+10000000)
}

// contatoDireto cria a ficha por SQL — para os testes que precisam de um
// contato PRONTO e não estão testando a criação.
func (a *ambiente) contatoDireto(t *testing.T, nome string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO contacts (property_id, name, email, phone_e164, lgpd_basis)
		VALUES ($1, $2, $3, $4, 'contrato') RETURNING id`,
		a.propriedade, nome, "c_"+sufixo()+"@exemplo.invalid", telefoneDeTeste(t)).Scan(&id); err != nil {
		t.Fatalf("criando contato: %v", err)
	}
	a.limparContato(t, id)
	return id
}

// limparContato registra a faxina de tudo que pode nascer preso à ficha.
func (a *ambiente) limparContato(t *testing.T, id uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		a.executar(t, `DELETE FROM pii_access_log WHERE contact_id = $1`, id)
		a.executar(t, `DELETE FROM audit_log WHERE entity = 'contacts' AND entity_id = $1`, id)
		a.executar(t, `DELETE FROM crm_activities WHERE contact_id = $1`, id)
		a.executar(t, `DELETE FROM crm_notes WHERE contact_id = $1`, id)
		a.executar(t, `DELETE FROM crm_opportunities WHERE contact_id = $1`, id)
		a.executar(t, `DELETE FROM crm_leads WHERE contact_id = $1`, id)
		a.executar(t, `DELETE FROM quotes WHERE contact_id = $1`, id)
		a.executar(t, `DELETE FROM reservation_guests WHERE contact_id = $1`, id)
		a.executar(t, `DELETE FROM reservation_units WHERE reservation_id IN (SELECT id FROM reservations WHERE contact_id = $1)`, id)
		a.executar(t, `DELETE FROM stay_blocks WHERE reservation_id IN (SELECT id FROM reservations WHERE contact_id = $1)`, id)
		a.executar(t, `DELETE FROM reservations WHERE contact_id = $1`, id)
		a.executar(t, `DELETE FROM contacts WHERE id = $1`, id)
	})
}

// produtoDeUmaUnidade devolve um `unit_type` com `consumes = 'one_member'`.
//
// A escolha não é arbitrária: produto `all_members` (a Completa) tem constraint
// trigger adiado exigindo uma linha de `reservation_units` para CADA unidade da
// composição (20260827100000). Uma reserva de apartamento não passa por essa
// invariante, e este módulo não tem nada a dizer sobre composição de casa.
func (a *ambiente) produtoDeUmaUnidade(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		SELECT id FROM unit_types
		 WHERE property_id = $1 AND consumes = 'one_member' AND active
		 ORDER BY code LIMIT 1`, a.propriedade).Scan(&id); err != nil {
		t.Fatalf("produto de uma unidade no seed: %v", err)
	}
	return id
}

// reserva cria uma reserva COMPLETA — cabeçalho, preço congelado e noites — no
// status pedido, para os testes de anonimização terem o que preservar.
type reservaDeTeste struct {
	ID    uuid.UUID
	Code  string
	Total int64
}

func (a *ambiente) reserva(t *testing.T, contato uuid.UUID, status string, primeiroDia int, noites int) reservaDeTeste {
	t.Helper()

	produto := a.produtoDeUmaUnidade(t)
	checkIn := a.dia(primeiroDia)
	checkOut := a.dia(primeiroDia + noites)

	const porNoite int64 = 45000
	subtotal := porNoite * int64(noites)

	var r reservaDeTeste
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO reservations (property_id, unit_type_id, contact_id, status,
		                          check_in, check_out, guests_count)
		VALUES ($1, $2, $3, $4, $5::date, $6::date, 2)
		RETURNING id, code`,
		a.propriedade, produto, contato, status, checkIn, checkOut).Scan(&r.ID, &r.Code); err != nil {
		t.Fatalf("criando reserva: %v", err)
	}

	if _, err := a.pool.Exec(a.ctx, `
		INSERT INTO reservation_pricing (reservation_id, subtotal_cents, cleaning_cents, total_cents, deposit_cents)
		VALUES ($1, $2, 15000, $3, 0)`, r.ID, subtotal, subtotal+15000); err != nil {
		t.Fatalf("gravando o preço congelado: %v", err)
	}
	r.Total = subtotal + 15000

	var tipoDeData string
	if err := a.pool.QueryRow(a.ctx, `SELECT kind FROM date_type_rules ORDER BY kind LIMIT 1`).Scan(&tipoDeData); err != nil {
		t.Fatalf("lendo date_type_rules do seed: %v", err)
	}
	for i := 0; i < noites; i++ {
		if _, err := a.pool.Exec(a.ctx, `
			INSERT INTO reservation_nights (reservation_id, night, date_type, unit_type_id, price_cents)
			VALUES ($1, $2::date, $3, $4, $5)`,
			r.ID, a.dia(primeiroDia+i), tipoDeData, produto, porNoite); err != nil {
			t.Fatalf("gravando a noite %d: %v", i, err)
		}
	}
	return r
}

func (a *ambiente) executar(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := a.pool.Exec(a.ctx, sql, args...); err != nil {
		t.Errorf("limpeza (%s): %v", sql, err)
	}
}

func (a *ambiente) contar(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(a.ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("contando (%s): %v", sql, err)
	}
	return n
}
