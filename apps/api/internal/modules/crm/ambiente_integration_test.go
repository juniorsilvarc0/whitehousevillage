//go:build integration

// Infraestrutura dos testes de integração do CRM.
//
// Sobe as rotas do módulo sobre um Postgres real e conversa com elas por HTTP,
// atravessando o autenticador e o middleware de RBAC de verdade. Testar escopo
// `own` pelo service seria testar tudo menos o que importa: o recorte é uma
// cláusula de SQL, e quem decide se ela existe é a matriz de permissões
// resolvida pelo middleware.
//
// A tabela `rotas` abaixo espelha, linha a linha, `internal/router/rotas_crm.go`
// — se as duas divergirem, os testes de permissão passam a testar outra coisa.
package crm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/crm"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/disponibilidade"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

const (
	prefixoDaAPI = "/api/v1"
	segredoJWT   = "segredo-de-integracao-do-crm-32-bytes"
	senhaDeTeste = "senha-de-integracao-crm-2026"
)

// Um pool por PACOTE, e não um por teste: trinta aberturas de pool contra o
// mesmo Postgres, cada uma com o ping de boot de 5 s, é o que já fez outra
// suíte deste repositório ver "context deadline exceeded" no primeiro teste.
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
	// hoje é o dia da CASA (fuso da propriedade), não o do processo: toda data
	// relativa sai daqui, senão a suíte muda de resultado conforme a hora em
	// que roda.
	hoje time.Time
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
	h := crm.NovoHandler(pool, db.NewTxManager(pool))

	// `POST /quotes` entra no roteador do teste como FIXTURE, e não como rota do
	// CRM: desde 27/08/2026 é por ele que nasce o orçamento que o `/win`
	// consome, e montá-lo aqui é o que faz a suíte exercitar a jornada INTEIRA
	// pela API. Antes disso o fixture inseria a linha por SQL direto — e um
	// endpoint que só o teste consegue alimentar não está no ar.
	orcamentos := disponibilidade.NovoHandler(pool, db.NewTxManager(pool))

	r := chi.NewRouter()
	r.Route(prefixoDaAPI, func(api chi.Router) {
		api.Use(autenticador.Middleware)
		for _, rota := range rotas(h) {
			api.Method(rota.metodo, rota.path,
				auth.Middleware(rota.recurso, rota.acao)(http.HandlerFunc(rota.handler)))
		}
		api.Method(http.MethodPost, "/quotes",
			auth.Middleware(recursoOrcamentos, auth.AcaoCriar)(http.HandlerFunc(orcamentos.Orcar)))
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

// rotas espelha internal/router/rotas_crm.go.
func rotas(h *crm.Handler) []rota {
	const (
		funis = crm.RecursoFunis
		leads = crm.RecursoLeads
		opps  = crm.RecursoOportunidades
		atvs  = crm.RecursoAtividades
	)
	return []rota{
		{http.MethodGet, "/crm/pipelines", funis, auth.AcaoVer, h.ListarFunis},
		{http.MethodPost, "/crm/pipelines", funis, auth.AcaoCriar, h.CriarFunil},
		{http.MethodGet, "/crm/pipelines/{id}", funis, auth.AcaoVer, h.BuscarFunil},
		{http.MethodPut, "/crm/pipelines/{id}", funis, auth.AcaoEditar, h.SubstituirFunil},
		{http.MethodPatch, "/crm/pipelines/{id}", funis, auth.AcaoEditar, h.AtualizarFunil},
		{http.MethodDelete, "/crm/pipelines/{id}", funis, auth.AcaoExcluir, h.DesativarFunil},

		{http.MethodGet, "/crm/stages", funis, auth.AcaoVer, h.ListarEtapas},
		{http.MethodPost, "/crm/stages", funis, auth.AcaoCriar, h.CriarEtapa},
		{http.MethodPost, "/crm/stages/reorder", funis, auth.AcaoEditar, h.Reordenar},
		{http.MethodGet, "/crm/stages/{id}", funis, auth.AcaoVer, h.BuscarEtapa},
		{http.MethodPut, "/crm/stages/{id}", funis, auth.AcaoEditar, h.SubstituirEtapa},
		{http.MethodPatch, "/crm/stages/{id}", funis, auth.AcaoEditar, h.AtualizarEtapa},
		{http.MethodDelete, "/crm/stages/{id}", funis, auth.AcaoExcluir, h.ExcluirEtapa},

		{http.MethodGet, "/crm/lost-reasons", funis, auth.AcaoVer, h.ListarMotivos},
		{http.MethodPost, "/crm/lost-reasons", funis, auth.AcaoCriar, h.CriarMotivo},
		{http.MethodGet, "/crm/lost-reasons/{id}", funis, auth.AcaoVer, h.BuscarMotivo},
		{http.MethodPut, "/crm/lost-reasons/{id}", funis, auth.AcaoEditar, h.SubstituirMotivo},
		{http.MethodPatch, "/crm/lost-reasons/{id}", funis, auth.AcaoEditar, h.AtualizarMotivo},
		{http.MethodDelete, "/crm/lost-reasons/{id}", funis, auth.AcaoExcluir, h.DesativarMotivo},

		{http.MethodGet, "/crm/leads", leads, auth.AcaoVer, h.ListarLeads},
		{http.MethodPost, "/crm/leads", leads, auth.AcaoCriar, h.CriarLead},
		{http.MethodGet, "/crm/leads/{id}", leads, auth.AcaoVer, h.BuscarLead},
		{http.MethodPut, "/crm/leads/{id}", leads, auth.AcaoEditar, h.SubstituirLead},
		{http.MethodPatch, "/crm/leads/{id}", leads, auth.AcaoEditar, h.AtualizarLead},
		{http.MethodDelete, "/crm/leads/{id}", leads, auth.AcaoExcluir, h.ExcluirLead},
		{http.MethodPost, "/crm/leads/{id}/convert", leads, auth.AcaoEditar, h.Converter},

		{http.MethodGet, "/crm/opportunities", opps, auth.AcaoVer, h.ListarOportunidades},
		{http.MethodPost, "/crm/opportunities", opps, auth.AcaoCriar, h.CriarOportunidade},
		{http.MethodGet, "/crm/opportunities/kanban", opps, auth.AcaoVer, h.Kanban},
		{http.MethodGet, "/crm/opportunities/{id}", opps, auth.AcaoVer, h.BuscarOportunidade},
		{http.MethodPut, "/crm/opportunities/{id}", opps, auth.AcaoEditar, h.SubstituirOportunidade},
		{http.MethodPatch, "/crm/opportunities/{id}", opps, auth.AcaoEditar, h.AtualizarOportunidade},
		{http.MethodDelete, "/crm/opportunities/{id}", opps, auth.AcaoExcluir, h.ExcluirOportunidade},
		{http.MethodGet, "/crm/opportunities/{id}/full", opps, auth.AcaoVer, h.Completa},
		{http.MethodPost, "/crm/opportunities/{id}/stage", opps, auth.AcaoEditar, h.MudarEtapa},
		{http.MethodPost, "/crm/opportunities/{id}/win", opps, auth.AcaoEditar, h.Ganhar},
		{http.MethodPost, "/crm/opportunities/{id}/lose", opps, auth.AcaoEditar, h.Perder},

		{http.MethodGet, "/crm/activities", atvs, auth.AcaoVer, h.ListarAtividades},
		{http.MethodPost, "/crm/activities", atvs, auth.AcaoCriar, h.CriarAtividade},
		{http.MethodGet, "/crm/activities/{id}", atvs, auth.AcaoVer, h.BuscarAtividade},
		{http.MethodPut, "/crm/activities/{id}", atvs, auth.AcaoEditar, h.SubstituirAtividade},
		{http.MethodPatch, "/crm/activities/{id}", atvs, auth.AcaoEditar, h.AtualizarAtividade},
		{http.MethodDelete, "/crm/activities/{id}", atvs, auth.AcaoExcluir, h.ExcluirAtividade},
		{http.MethodPost, "/crm/activities/{id}/complete", atvs, auth.AcaoEditar, h.Concluir},
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
	return a.chamarCom(t, metodo, caminho, token, corpo, nil)
}

func (a *ambiente) chamarIdem(t *testing.T, metodo, caminho, token string, corpo any, chave string) resposta {
	t.Helper()
	return a.chamarCom(t, metodo, caminho, token, corpo, map[string]string{"Idempotency-Key": chave})
}

func (a *ambiente) chamarCom(t *testing.T, metodo, caminho, token string, corpo any, cabecalhos map[string]string) resposta {
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
	for k, v := range cabecalhos {
		req.Header.Set(k, v)
	}

	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", metodo, caminho, err)
	}
	defer resp.Body.Close()

	lido, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("lendo a resposta: %v", err)
	}
	return resposta{Status: resp.StatusCode, Corpo: lido, Location: resp.Header.Get("Location")}
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

// recursoOrcamentos é o recurso RBAC de `POST /quotes` — o mesmo código do
// catálogo semeado por cmd/seed/acesso.go.
const recursoOrcamentos = "quotes"

// celulasDoCRM é o conjunto completo — é o que o gestor recebe.
//
// `quotes` entra junto porque o orçamento é PARTE da jornada do funil: quem
// pode ganhar a oportunidade tem de poder emitir a proposta que ela ganha. É
// também o que o seed concede aos três perfis.
func celulasDoCRM() []string {
	var out []string
	for _, recurso := range []string{
		crm.RecursoFunis, crm.RecursoLeads, crm.RecursoOportunidades, crm.RecursoAtividades,
	} {
		for _, acao := range auth.AcoesValidas {
			out = append(out, recurso+":"+acao)
		}
	}
	return append(out, recursoOrcamentos+":"+auth.AcaoVer, recursoOrcamentos+":"+auth.AcaoCriar)
}

// gestor é o perfil da maioria dos testes: escopo `all` em todo o CRM.
func (a *ambiente) gestor(t *testing.T) (uuid.UUID, string) {
	t.Helper()
	return a.usuario(t, a.perfil(t, "crm_gestor", celulasDoCRM()...))
}

// corretor é o perfil com escopo `own` — o que o §7 promete: "corretor vê no
// kanban apenas os próprios cards".
func (a *ambiente) corretor(t *testing.T) (uuid.UUID, string) {
	t.Helper()
	return a.usuario(t, a.perfilComEscopo(t, "crm_corretor", auth.EscopoOwn, celulasDoCRM()...))
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

	email := fmt.Sprintf("crm_%s@exemplo.invalid", uuid.NewString()[:8])
	var (
		id     uuid.UUID
		codigo string
	)
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO users (property_id, role_id, name, email, password_hash)
		VALUES ($1, $2, 'Teste CRM', $3, $4)
		RETURNING id, (SELECT code FROM roles WHERE id = $2)`,
		a.propriedade, perfilID, email, hash).Scan(&id, &codigo); err != nil {
		t.Fatalf("criando usuário: %v", err)
	}
	// A ordem da limpeza importa: as tabelas do CRM referenciam o usuário por
	// `owner_id`/`created_by` e NÃO cascateiam — apagar o usuário antes deixaria
	// 23503 na saída do teste.
	t.Cleanup(func() {
		a.executar(t, `DELETE FROM crm_activities WHERE owner_id = $1 OR created_by = $1`, id)
		a.executar(t, `DELETE FROM crm_stage_history WHERE user_id = $1`, id)
		a.executar(t, `DELETE FROM crm_opportunities WHERE owner_id = $1 OR created_by = $1`, id)
		a.executar(t, `DELETE FROM crm_leads WHERE owner_id = $1 OR created_by = $1`, id)
		// `quotes` referencia `users` por `owner_id`/`created_by` e não
		// cascateia — e precisa sair ANTES das reservas, senão o DELETE de
		// reservations esbarra em `quotes.reservation_id`.
		a.executar(t, `DELETE FROM quotes WHERE owner_id = $1 OR created_by = $1`, id)
		a.executar(t, `DELETE FROM reservations WHERE created_by = $1 OR owner_id = $1`, id)
		a.executar(t, `DELETE FROM stay_blocks WHERE created_by = $1 OR owner_id = $1`, id)
		a.executar(t, `DELETE FROM idempotency_keys WHERE actor_id = $1`, id)
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

// contato cria o cliente do teste e limpa tudo o que nascer preso a ele.
func (a *ambiente) contato(t *testing.T) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO contacts (property_id, name, email, phone_e164)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		a.propriedade, "Cliente "+sufixo(), "cliente_"+sufixo()+"@exemplo.invalid",
		"+5585"+fmt.Sprintf("%09d", time.Now().UnixNano()%1000000000)).Scan(&id); err != nil {
		t.Fatalf("criando contato: %v", err)
	}
	t.Cleanup(func() {
		a.executar(t, `DELETE FROM crm_activities WHERE contact_id = $1`, id)
		a.executar(t, `DELETE FROM crm_opportunities WHERE contact_id = $1`, id)
		a.executar(t, `DELETE FROM crm_leads WHERE contact_id = $1`, id)
		a.executar(t, `DELETE FROM reservation_guests WHERE contact_id = $1`, id)
		a.executar(t, `DELETE FROM quotes WHERE contact_id = $1`, id)
		a.executar(t, `DELETE FROM reservations WHERE contact_id = $1`, id)
		a.executar(t, `DELETE FROM contacts WHERE id = $1`, id)
	})
	return id
}

func (a *ambiente) produto(t *testing.T, codigo string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`SELECT id FROM unit_types WHERE property_id = $1 AND code = $2`, a.propriedade, codigo).Scan(&id); err != nil {
		t.Fatalf("produto %q do seed: %v", codigo, err)
	}
	return id
}

func (a *ambiente) unidade(t *testing.T, codigo string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`SELECT id FROM units WHERE property_id = $1 AND code = $2`, a.propriedade, codigo).Scan(&id); err != nil {
		t.Fatalf("unidade %q do seed: %v", codigo, err)
	}
	return id
}

// funilDoSeed devolve o funil `is_default` e as oito etapas da spec §7.
func (a *ambiente) funilDoSeed(t *testing.T) (uuid.UUID, map[string]uuid.UUID) {
	t.Helper()

	var funil uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`SELECT id FROM crm_pipelines WHERE property_id = $1 AND is_default`, a.propriedade).Scan(&funil); err != nil {
		t.Fatalf("funil padrão do seed: %v", err)
	}

	linhas, err := a.pool.Query(a.ctx, `SELECT name, id FROM crm_stages WHERE pipeline_id = $1`, funil)
	if err != nil {
		t.Fatalf("etapas do seed: %v", err)
	}
	defer linhas.Close()

	etapas := map[string]uuid.UUID{}
	for linhas.Next() {
		var (
			nome string
			id   uuid.UUID
		)
		if err := linhas.Scan(&nome, &id); err != nil {
			t.Fatalf("etapas do seed: %v", err)
		}
		etapas[nome] = id
	}
	if len(etapas) < 8 {
		t.Fatalf("o seed deveria ter 8 etapas, tem %d", len(etapas))
	}
	return funil, etapas
}

func (a *ambiente) motivoDePerda(t *testing.T) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`SELECT id FROM crm_lost_reasons WHERE property_id = $1 AND active ORDER BY sort_order LIMIT 1`,
		a.propriedade).Scan(&id); err != nil {
		t.Fatalf("motivo de perda do seed: %v", err)
	}
	return id
}

// orcamentoEmitido emite um orçamento PELA API — `POST /quotes` com
// `persist: true`.
//
// Pela API, e não por SQL direto, e a diferença é o assunto desta rodada: até
// 27/08/2026 este fixture inseria uma reserva em `reservations` com status
// `quote`, porque `POST /quotes` era cálculo puro e não persistia. Um `/win` que
// só o fixture conseguia alimentar não estava no ar — e não estava mesmo: contra
// o stack de desenvolvimento, `/win` respondia SEMPRE `422 QUOTE_REQUIRED_TO_WIN`.
//
// `opportunity_id` faz dele o orçamento VIGENTE do card, que é o que o `/win`
// consome sem `quote_id` no corpo.
func (a *ambiente) orcamentoEmitido(t *testing.T, token string, produto, contato uuid.UUID,
	oportunidade *uuid.UUID, checkIn, checkOut string, hospedes int) orcamentoSalvo {
	t.Helper()

	corpo := map[string]any{
		"unit_type_id": produto,
		"check_in":     checkIn,
		"check_out":    checkOut,
		"guests_count": hospedes,
		"contact_id":   contato,
		"persist":      true,
	}
	if oportunidade != nil {
		corpo["opportunity_id"] = *oportunidade
	}

	resp := a.chamar(t, http.MethodPost, "/quotes", token, corpo)
	if resp.Status != http.StatusCreated {
		t.Fatalf("POST /quotes (persist) = %d (%s)", resp.Status, resp.Corpo)
	}
	salvo := dado[orcamentoSalvo](t, resp)
	if salvo.ID == uuid.Nil {
		t.Fatalf("o orçamento emitido voltou sem id: %s", resp.Corpo)
	}
	t.Cleanup(func() { a.executar(t, `DELETE FROM quotes WHERE id = $1`, salvo.ID) })
	return salvo
}

// orcamentoSalvo é o schema `OrcamentoSalvo` visto pelo teste.
type orcamentoSalvo struct {
	ID             uuid.UUID  `json:"id"`
	Total          int64      `json:"total_cents"`
	Subtotal       int64      `json:"subtotal_cents"`
	Sinal          int64      `json:"deposit_cents"`
	Noites         int        `json:"night_count"`
	OportunidadeID *uuid.UUID `json:"opportunity_id"`
	ReservaID      *uuid.UUID `json:"reservation_id"`
	Vencido        bool       `json:"expired"`
	Diarias        []struct {
		Data  string `json:"date"`
		Preco int64  `json:"price_cents"`
	} `json:"nights"`
}

// bloqueioDireto ocupa uma unidade pelo banco — é assim que o teste do `/win`
// que falha monta a data já vendida sem depender de outro endpoint.
func (a *ambiente) bloqueioDireto(t *testing.T, unidade uuid.UUID, de, ate string) {
	t.Helper()

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO stay_blocks (property_id, unit_id, source, status, period)
		VALUES ($1, $2, 'maintenance', 'confirmed', daterange($3::date, $4::date, '[)'))
		RETURNING id`, a.propriedade, unidade, de, ate).Scan(&id); err != nil {
		t.Fatalf("bloqueando unidade: %v", err)
	}
	t.Cleanup(func() { a.executar(t, `DELETE FROM stay_blocks WHERE id = $1`, id) })
}

func (a *ambiente) executar(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := a.pool.Exec(context.Background(), sql, args...); err != nil {
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

func (a *ambiente) texto(t *testing.T, sql string, args ...any) string {
	t.Helper()
	var v string
	if err := a.pool.QueryRow(a.ctx, sql, args...).Scan(&v); err != nil {
		t.Fatalf("lendo (%s): %v", sql, err)
	}
	return v
}

// ─────────────────────────── Projeções lidas ────────────────────────

type oportunidadeVista struct {
	ID            uuid.UUID  `json:"id"`
	ContactID     uuid.UUID  `json:"contact_id"`
	ContatoNome   string     `json:"contact_name"`
	FunilID       uuid.UUID  `json:"pipeline_id"`
	EtapaID       uuid.UUID  `json:"stage_id"`
	EtapaNome     string     `json:"stage_name"`
	EtapaTipo     string     `json:"stage_type"`
	Status        string     `json:"status"`
	DonoID        *uuid.UUID `json:"owner_id"`
	Valor         int64      `json:"amount_cents"`
	Probabilidade int        `json:"probability"`
	OrcamentoID   *uuid.UUID `json:"quote_id"`
	ReservaID     *uuid.UUID `json:"reservation_id"`
	MotivoID      *uuid.UUID `json:"lost_reason_id"`
	SLADias       *int       `json:"sla_days"`
	SLAVenceEm    *string    `json:"sla_due_at"`
	SLAEstourado  bool       `json:"sla_breached"`
	TarefasPend   int        `json:"pending_task_count"`
	CheckIn       *string    `json:"check_in"`
	CheckOut      *string    `json:"check_out"`
}

type atividadeVista struct {
	ID          uuid.UUID  `json:"id"`
	Tipo        string     `json:"type"`
	Assunto     string     `json:"subject"`
	Status      string     `json:"status"`
	Auto        bool       `json:"auto"`
	EtapaID     *uuid.UUID `json:"stage_id"`
	DonoID      uuid.UUID  `json:"owner_id"`
	VenceEm     *string    `json:"due_at"`
	ConcluidaEm *string    `json:"done_at"`
	Vencida     bool       `json:"overdue"`
}

type resultadoDeEtapa struct {
	Oportunidade oportunidadeVista `json:"opportunity"`
	TarefaAuto   *atividadeVista   `json:"auto_task"`
	SLA          struct {
		EtapaID       uuid.UUID `json:"stage_id"`
		SLADias       *int      `json:"sla_days"`
		DiasRestantes *int      `json:"days_left"`
		Estourado     bool      `json:"breached"`
	} `json:"sla"`
}

type resultadoDeGanho struct {
	Oportunidade oportunidadeVista `json:"opportunity"`
	Reserva      struct {
		ID     uuid.UUID `json:"id"`
		Codigo string    `json:"code"`
		Status string    `json:"status"`
	} `json:"reservation"`
	ReservaCriada bool `json:"reservation_created"`
}

// criarOportunidade abre um card e exige 201.
func (a *ambiente) criarOportunidade(t *testing.T, token string, corpo map[string]any) oportunidadeVista {
	t.Helper()

	resp := a.chamar(t, http.MethodPost, "/crm/opportunities", token, corpo)
	if resp.Status != http.StatusCreated {
		t.Fatalf("POST /crm/opportunities = %d (%s)", resp.Status, resp.Corpo)
	}
	return dado[oportunidadeVista](t, resp)
}
