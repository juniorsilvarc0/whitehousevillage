//go:build integration

// Infraestrutura dos testes de integração do inventário de bens.
//
// Sobe as rotas do módulo sobre um Postgres real e conversa com elas por HTTP,
// atravessando o autenticador e o middleware de RBAC de verdade.
//
// A diferença em relação a `internal/modules/inventario`: a tabela de rotas
// NÃO é espelhada aqui. Ela vem de `router.Rotas(router.Deps{Bens: h})`,
// filtrada pelo recurso do módulo — então o teste exercita exatamente as linhas
// de `internal/router/rotas_inventario_bens.go`, com o par (recurso, ação) de
// cada uma, e não uma cópia que poderia divergir dela.
package bens_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
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
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/bens"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/router"
)

const (
	prefixoDaAPI = "/api/v1"
	segredoJWT   = "segredo-de-integracao-dos-bens-32b!"
	senhaDeTeste = "senha-de-integracao-2026"
)

// Células da matriz deste módulo.
const (
	celulaVer     = bens.Recurso + ":ver"
	celulaCriar   = bens.Recurso + ":criar"
	celulaEditar  = bens.Recurso + ":editar"
	celulaExcluir = bens.Recurso + ":excluir"
)

type ambiente struct {
	pool        *pgxpool.Pool
	servidor    *httptest.Server
	emissor     *auth.Emissor
	ctx         context.Context
	propriedade uuid.UUID

	// O que o teste criou, para a limpeza única do fim (ver subir).
	mu           sync.Mutex
	unidades     []uuid.UUID
	itens        []uuid.UUID
	propriedades []uuid.UUID
	usuarios     []uuid.UUID
	perfis       []uuid.UUID
	reservas     []uuid.UUID
	contatos     []uuid.UUID
	produtos     []uuid.UUID
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
	h := bens.NovoHandler(pool, db.NewTxManager(pool), t.TempDir())

	r := chi.NewRouter()
	montadas := 0
	r.Route(prefixoDaAPI, func(api chi.Router) {
		api.Use(httpx.RequestID)
		api.Use(audit.Middleware)
		api.Use(autenticador.Middleware)
		for _, rota := range router.Rotas(router.Deps{Bens: h}) {
			if rota.Recurso != bens.Recurso {
				continue
			}
			api.Method(rota.Metodo, rota.Path, auth.Middleware(rota.Recurso, rota.Acao)(rota.Handler))
			montadas++
		}
	})
	if montadas != 40 {
		t.Fatalf("a tabela de rotas tem %d linhas de %s; o contrato declara 40 operações", montadas, bens.Recurso)
	}

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	a := &ambiente{pool: pool, servidor: srv, emissor: emissor, ctx: ctx}
	if err := pool.QueryRow(ctx, `SELECT id FROM properties ORDER BY created_at LIMIT 1`).Scan(&a.propriedade); err != nil {
		t.Fatalf("lendo a propriedade do seed (rodou cmd/seed?): %v", err)
	}

	// UMA limpeza, registrada antes de tudo e portanto a ÚLTIMA a rodar. A
	// ordem importa por causa das FKs: avaria segura conferência, conferência
	// e linha seguram usuário (`opened_by`, `counted_by`), e `audit_log`
	// segura usuário (`actor_id`). Apagar o usuário antes dos dados falharia.
	t.Cleanup(func() { a.limpar(t) })
	return a
}

func (a *ambiente) limpar(t *testing.T) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, u := range a.unidades {
		a.executar(t, `DELETE FROM inventory_issues WHERE room_id IN (SELECT id FROM unit_rooms WHERE unit_id = $1)
		                  OR count_id IN (SELECT id FROM inventory_counts WHERE unit_id = $1)`, u)
		a.executar(t, `DELETE FROM inventory_counts WHERE unit_id = $1`, u)
		a.executar(t, `DELETE FROM unit_rooms WHERE unit_id = $1`, u)
	}
	for _, i := range a.itens {
		a.executar(t, `DELETE FROM inventory_issues WHERE item_id = $1`, i)
		a.executar(t, `DELETE FROM inventory_count_lines WHERE item_id = $1`, i)
		a.executar(t, `DELETE FROM room_inventory WHERE item_id = $1`, i)
		a.executar(t, `DELETE FROM inventory_items WHERE id = $1`, i)
	}
	for _, r := range a.reservas {
		a.executar(t, `DELETE FROM inventory_issues WHERE reservation_id = $1`, r)
		a.executar(t, `DELETE FROM reservations WHERE id = $1`, r)
	}
	for _, c := range a.contatos {
		a.executar(t, `DELETE FROM contacts WHERE id = $1`, c)
	}
	for _, p := range a.produtos {
		a.executar(t, `DELETE FROM unit_types WHERE id = $1`, p)
	}
	for _, u := range a.unidades {
		a.executar(t, `DELETE FROM units WHERE id = $1`, u)
	}
	for _, uid := range a.usuarios {
		a.executar(t, `DELETE FROM inventory_media WHERE created_by = $1`, uid)
		a.executar(t, `DELETE FROM audit_log WHERE actor_id = $1`, uid)
		a.executar(t, `DELETE FROM users WHERE id = $1`, uid)
	}
	for _, p := range a.perfis {
		a.executar(t, `DELETE FROM roles WHERE id = $1`, p)
	}
	for _, p := range a.propriedades {
		a.executar(t, `DELETE FROM inventory_media WHERE property_id = $1`, p)
		a.executar(t, `DELETE FROM properties WHERE id = $1`, p)
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

// chamar faz a requisição JSON. `corpo` string é enviado cru (para testar
// campo desconhecido); qualquer outro valor é serializado.
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
	return a.enviar(t, req, token)
}

func (a *ambiente) enviar(t *testing.T, req *http.Request, token string) resposta {
	t.Helper()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	lido, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("lendo a resposta: %v", err)
	}
	return resposta{Status: resp.StatusCode, Corpo: lido, Cabecalho: resp.Header}
}

// enviarArquivo faz o POST multipart de `/inventory/media`.
func (a *ambiente) enviarArquivo(t *testing.T, token, nome string, conteudo []byte) resposta {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	parte, err := mw.CreateFormFile("file", nome)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parte.Write(conteudo); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(a.ctx, http.MethodPost, a.servidor.URL+prefixoDaAPI+"/inventory/media", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return a.enviar(t, req, token)
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

func (a *ambiente) perfil(t *testing.T, prefixo string, celulas ...string) uuid.UUID {
	t.Helper()
	codigo := fmt.Sprintf("%s_%s", prefixo, uuid.NewString()[:8])
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`INSERT INTO roles (code, name, is_system) VALUES ($1, $1, false) RETURNING id`, codigo).Scan(&id); err != nil {
		t.Fatalf("criando perfil %s: %v", codigo, err)
	}
	a.mu.Lock()
	a.perfis = append(a.perfis, id)
	a.mu.Unlock()
	for _, celula := range celulas {
		var recurso, acao string
		for i := len(celula) - 1; i >= 0; i-- {
			if celula[i] == ':' {
				recurso, acao = celula[:i], celula[i+1:]
				break
			}
		}
		if _, err := a.pool.Exec(a.ctx,
			`INSERT INTO role_permissions (role_id, resource_code, action, scope) VALUES ($1, $2, $3, 'all')`,
			id, recurso, acao); err != nil {
			t.Fatalf("concedendo %s: %v", celula, err)
		}
	}
	return id
}

func (a *ambiente) token(t *testing.T, perfil uuid.UUID) string {
	t.Helper()
	assinado, _ := a.tokenComUsuario(t, perfil, a.propriedade)
	return assinado
}

func (a *ambiente) tokenComUsuario(t *testing.T, perfil, propriedade uuid.UUID) (string, uuid.UUID) {
	t.Helper()
	hash, err := auth.Hash(senhaDeTeste)
	if err != nil {
		t.Fatalf("gerando hash: %v", err)
	}
	email := fmt.Sprintf("bens_%s@exemplo.invalid", uuid.NewString()[:8])
	var (
		id     uuid.UUID
		codigo string
	)
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO users (property_id, role_id, name, email, password_hash)
		VALUES ($1, $2, 'Contadora de Teste', $3, $4)
		RETURNING id, (SELECT code FROM roles WHERE id = $2)`,
		propriedade, perfil, email, hash).Scan(&id, &codigo); err != nil {
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

// gestor é quem tem as quatro ações do módulo.
func (a *ambiente) gestor(t *testing.T) string {
	t.Helper()
	return a.token(t, a.perfil(t, "bens_gestor", celulaVer, celulaCriar, celulaEditar, celulaExcluir))
}

// ─────────────────────────── Fixtures ───────────────────────────────

func sufixo() string { return uuid.NewString()[:8] }

// unidade insere uma unidade física pelo banco — o cadastro comercial não é
// deste módulo.
func (a *ambiente) unidade(t *testing.T) (uuid.UUID, string) {
	t.Helper()
	return a.unidadeEm(t, a.propriedade)
}

func (a *ambiente) unidadeEm(t *testing.T, propriedade uuid.UUID) (uuid.UUID, string) {
	t.Helper()
	codigo := "IT-BENS-" + sufixo()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO units (property_id, code, name, sort_order) VALUES ($1, $2, $2, 990) RETURNING id`,
		propriedade, codigo).Scan(&id); err != nil {
		t.Fatalf("criando unidade: %v", err)
	}
	a.mu.Lock()
	a.unidades = append(a.unidades, id)
	a.mu.Unlock()
	return id, codigo
}

// outraPropriedade cria uma segunda casa — é o único jeito de provar que a
// API não mistura item de uma com cômodo da outra.
func (a *ambiente) outraPropriedade(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO properties (name, slug, timezone) VALUES ('Casa de teste', $1, 'America/Fortaleza') RETURNING id`,
		"it-bens-"+sufixo()).Scan(&id); err != nil {
		t.Fatalf("criando propriedade: %v", err)
	}
	a.mu.Lock()
	a.propriedades = append(a.propriedades, id)
	a.mu.Unlock()
	return id
}

// reserva insere uma estadia já encerrada (`checked_out`) pelo banco — é a
// pergunta do check-out: "o que quebrou nesta estadia". Produto e contato
// próprios, para não depender do calendário do seed. Devolve id e código.
func (a *ambiente) reserva(t *testing.T) (uuid.UUID, string) {
	t.Helper()
	var produto, contato, id uuid.UUID
	var codigo string
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO unit_types (property_id, code, name, capacity, consumes, sort_order)
		VALUES ($1, $2, 'Produto de teste dos bens', 2, 'one_member', 990) RETURNING id`,
		a.propriedade, "it-bens-"+sufixo()).Scan(&produto); err != nil {
		t.Fatalf("criando produto: %v", err)
	}
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO contacts (property_id, name, email) VALUES ($1, 'Hóspede Sigiloso', $2) RETURNING id`,
		a.propriedade, "hospede_"+sufixo()+"@exemplo.invalid").Scan(&contato); err != nil {
		t.Fatalf("criando contato: %v", err)
	}
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO reservations (property_id, unit_type_id, contact_id, status, check_in, check_out, guests_count)
		VALUES ($1, $2, $3, 'checked_out', current_date - 5, current_date - 2, 2)
		RETURNING id, code`, a.propriedade, produto, contato).Scan(&id, &codigo); err != nil {
		t.Fatalf("criando reserva: %v", err)
	}
	a.mu.Lock()
	a.produtos = append(a.produtos, produto)
	a.contatos = append(a.contatos, contato)
	a.reservas = append(a.reservas, id)
	a.mu.Unlock()
	return id, codigo
}

type ambienteResp struct {
	ID               uuid.UUID `json:"id"`
	UnidadeID        uuid.UUID `json:"unit_id"`
	UnidadeCodigo    string    `json:"unit_code"`
	Nome             string    `json:"name"`
	Tipo             string    `json:"kind"`
	Ordem            int       `json:"sort_order"`
	Ativo            bool      `json:"active"`
	QtdBens          int       `json:"items_count"`
	QtdEsperadaTotal int64     `json:"expected_qty_total"`
	AvariasAbertas   int       `json:"open_issues_count"`
}

func (a *ambiente) comodo(t *testing.T, token string, unidade uuid.UUID, nome, tipo string, ordem int) ambienteResp {
	t.Helper()
	r := a.chamar(t, http.MethodPost, "/rooms", token, map[string]any{
		"unit_id": unidade, "name": nome, "kind": tipo, "sort_order": ordem,
	})
	exigir(t, r, http.StatusCreated, "criando ambiente "+nome)
	return dado[ambienteResp](t, r)
}

type bemResp struct {
	ID    uuid.UUID `json:"id"`
	Nome  string    `json:"name"`
	Custo *int64    `json:"replacement_cost_cents"`
	Ativo bool      `json:"active"`
	Capa  *struct {
		ID           uuid.UUID `json:"id"`
		URL          string    `json:"url"`
		URLMiniatura string    `json:"thumb_url"`
	} `json:"cover"`
	QtdFotos         int   `json:"photos_count"`
	QtdColocacoes    int   `json:"placements_count"`
	QtdEsperadaTotal int64 `json:"expected_qty_total"`
}

func (a *ambiente) bem(t *testing.T, token, nome string, custo *int64) bemResp {
	t.Helper()
	corpo := map[string]any{"name": nome + " " + sufixo(), "category": "louca"}
	if custo != nil {
		corpo["replacement_cost_cents"] = *custo
	}
	r := a.chamar(t, http.MethodPost, "/inventory/items", token, corpo)
	exigir(t, r, http.StatusCreated, "criando bem "+nome)
	b := dado[bemResp](t, r)
	a.mu.Lock()
	a.itens = append(a.itens, b.ID)
	a.mu.Unlock()
	return b
}

type colocacaoResp struct {
	ID          string    `json:"id"`
	AmbienteID  uuid.UUID `json:"room_id"`
	BemID       uuid.UUID `json:"item_id"`
	QtdEsperada int       `json:"expected_qty"`
	Nota        *string   `json:"note"`
}

func (a *ambiente) colocar(t *testing.T, token string, comodo, item uuid.UUID, qtd int) colocacaoResp {
	t.Helper()
	r := a.chamar(t, http.MethodPost, "/inventory/placements", token, map[string]any{
		"room_id": comodo, "item_id": item, "expected_qty": qtd,
	})
	exigir(t, r, http.StatusCreated, "colocando bem")
	return dado[colocacaoResp](t, r)
}

func ptr[T any](v T) *T { return &v }

// ─────────────────────────── Tipos de leitura ───────────────────────

type progressoResp struct {
	Linhas      int   `json:"lines"`
	Contadas    int   `json:"counted"`
	Pendentes   int   `json:"pending"`
	Divergentes int   `json:"diverging"`
	Faltas      int64 `json:"missing_qty"`
	Sobras      int64 `json:"surplus_qty"`
}

type linhaResp struct {
	ID          uuid.UUID  `json:"id"`
	AmbienteID  uuid.UUID  `json:"room_id"`
	BemID       uuid.UUID  `json:"item_id"`
	QtdEsperada int        `json:"expected_qty"`
	QtdContada  *int       `json:"counted_qty"`
	Diferenca   *int       `json:"diff"`
	Nota        *string    `json:"note"`
	Custo       *int64     `json:"replacement_cost_cents"`
	ContadaPor  *uuid.UUID `json:"counted_by"`
	ContadaNome *string    `json:"counted_by_name"`
	ContadaEm   *time.Time `json:"counted_at"`
}

type conferenciaResp struct {
	ID        uuid.UUID     `json:"id"`
	UnidadeID uuid.UUID     `json:"unit_id"`
	Status    string        `json:"status"`
	Nota      *string       `json:"note"`
	AbertaPor *string       `json:"opened_by_name"`
	Encerrada *time.Time    `json:"closed_at"`
	Progresso progressoResp `json:"progress"`
	Ambientes []struct {
		AmbienteID uuid.UUID     `json:"room_id"`
		Nome       string        `json:"room_name"`
		Progresso  progressoResp `json:"progress"`
		Linhas     []linhaResp   `json:"lines"`
	} `json:"rooms"`
	Resultado *apuracaoResp `json:"result"`
}

type divergenciaResp struct {
	AmbienteID  uuid.UUID  `json:"room_id"`
	BemID       uuid.UUID  `json:"item_id"`
	QtdEsperada int        `json:"expected_qty"`
	QtdContada  int        `json:"counted_qty"`
	Diferenca   int        `json:"diff"`
	Custo       *int64     `json:"replacement_cost_cents"`
	PerdaCents  *int64     `json:"loss_cents"`
	AvariaID    *uuid.UUID `json:"issue_id"`
}

type apuracaoResp struct {
	Divergencias   []divergenciaResp `json:"divergences"`
	AvariasCriadas int               `json:"issues_created"`
}

func (c conferenciaResp) linhas() []linhaResp {
	var out []linhaResp
	for _, a := range c.Ambientes {
		out = append(out, a.Linhas...)
	}
	return out
}

func (c conferenciaResp) linhaDo(t *testing.T, bem uuid.UUID) linhaResp {
	t.Helper()
	for _, l := range c.linhas() {
		if l.BemID == bem {
			return l
		}
	}
	t.Fatalf("conferência sem linha do bem %s", bem)
	return linhaResp{}
}

func (a *ambiente) abrir(t *testing.T, token string, unidade uuid.UUID) conferenciaResp {
	t.Helper()
	r := a.chamar(t, http.MethodPost, "/inventory/counts", token, map[string]any{"unit_id": unidade})
	exigir(t, r, http.StatusCreated, "abrindo conferência")
	return dado[conferenciaResp](t, r)
}

func (a *ambiente) contar(t *testing.T, token string, conferencia, linha uuid.UUID, qtd any) resposta {
	t.Helper()
	return a.chamar(t, http.MethodPatch,
		fmt.Sprintf("/inventory/counts/%s/lines/%s", conferencia, linha), token, map[string]any{"counted_qty": qtd})
}
