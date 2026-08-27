//go:build integration

// Infraestrutura dos testes de integração das reservas.
//
// Sobe as rotas do módulo sobre um Postgres real e conversa com elas por HTTP,
// atravessando o autenticador e o middleware de RBAC de verdade. Testar
// concorrência pelo service seria testar tudo menos o que quebra: a corrida
// acontece no banco, e a resposta que o hóspede vê sai do handler.
//
// A tabela abaixo espelha, linha a linha, `internal/router/rotas_reservas.go` —
// se as duas divergirem, os testes de permissão passam a testar outra coisa.
package reservas_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/reservas"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

const (
	prefixoDaAPI = "/api/v1"
	segredoJWT   = "segredo-de-integracao-das-reservas-32b"
	senhaDeTeste = "senha-de-integracao-2026"
)

// Um pool por PACOTE, e não um por teste.
//
// Trinta aberturas de pool contra o mesmo Postgres, cada uma com o ping de boot
// de 5 s, foi o que fez o módulo de tarifário ver "context deadline exceeded" no
// primeiro teste enquanto o pool anterior ainda devolvia as conexões. Um pool
// só resolve e derruba o tempo da suíte.
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
	// hoje é o dia da CASA (fuso da propriedade), não o do processo. Toda data
	// relativa dos testes sai daqui, senão a suíte muda de resultado conforme a
	// hora em que roda.
	hoje calendar.Date
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
	h := reservas.NovoHandler(pool, db.NewTxManager(pool))

	r := chi.NewRouter()
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
		SELECT id, (now() AT TIME ZONE timezone)::text
		  FROM properties ORDER BY created_at LIMIT 1`).Scan(&a.propriedade, &hoje); err != nil {
		t.Fatalf("lendo a propriedade do seed (rodou cmd/seed?): %v", err)
	}
	if a.hoje, err = calendar.Parse(hoje[:10]); err != nil {
		t.Fatalf("data da casa ilegível %q: %v", hoje, err)
	}
	return a
}

// dia devolve a data ISO de hoje + n, no fuso da casa.
func (a *ambiente) dia(n int) string { return a.hoje.AddDays(n).String() }

type rota struct {
	metodo  string
	path    string
	recurso string
	acao    string
	handler http.HandlerFunc
}

// rotas espelha internal/router/rotas_reservas.go.
func rotas(h *reservas.Handler) []rota {
	const rec, cal = reservas.Recurso, reservas.RecursoCalendario
	return []rota{
		{http.MethodGet, "/reservations", rec, auth.AcaoVer, h.Listar},
		{http.MethodPost, "/reservations", rec, auth.AcaoCriar, h.Criar},
		{http.MethodGet, "/reservations/{id}", rec, auth.AcaoVer, h.Buscar},
		{http.MethodPut, "/reservations/{id}", rec, auth.AcaoEditar, h.Substituir},
		{http.MethodPatch, "/reservations/{id}", rec, auth.AcaoEditar, h.Atualizar},
		{http.MethodDelete, "/reservations/{id}", rec, auth.AcaoExcluir, h.Descartar},
		{http.MethodGet, "/reservations/{id}/full", rec, auth.AcaoVer, h.Completa},
		{http.MethodPost, "/reservations/{id}/confirm", rec, auth.AcaoEditar, h.Confirmar},
		{http.MethodPost, "/reservations/{id}/cancel", rec, auth.AcaoEditar, h.Cancelar},
		{http.MethodPost, "/reservations/{id}/reschedule", rec, auth.AcaoEditar, h.Remarcar},
		{http.MethodPost, "/reservations/{id}/check-in", rec, auth.AcaoEditar, h.CheckIn},
		{http.MethodPost, "/reservations/{id}/check-out", rec, auth.AcaoEditar, h.CheckOut},
		{http.MethodPost, "/reservations/{id}/reassign-unit", rec, auth.AcaoEditar, h.Realocar},
		{http.MethodPost, "/reservations/{id}/extend-hold", rec, auth.AcaoEditar, h.EstenderHold},
		{http.MethodPost, "/blocks", cal, auth.AcaoCriar, h.CriarBloqueio},
		{http.MethodDelete, "/blocks/{id}", cal, auth.AcaoExcluir, h.LiberarBloqueio},
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

// chamarIdem é o atalho das três rotas que exigem Idempotency-Key.
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

// perfil cria um papel descartável. Cada célula é "recurso:ação" e o escopo é
// `all`; use perfilComEscopo para o corretor com escopo `own`.
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

// gestor é o perfil usado pela maioria dos testes: pode tudo em reservas,
// calendário e orçamento.
func (a *ambiente) gestor(t *testing.T) string {
	t.Helper()
	return a.token(t, a.perfil(t, "res_gestor",
		"reservations:ver", "reservations:criar", "reservations:editar", "reservations:excluir",
		"calendar:ver", "calendar:criar", "calendar:excluir",
		"quotes:ver", "quotes:criar"))
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

// token cria um usuário do perfil e devolve o access token dele.
func (a *ambiente) token(t *testing.T, perfilID uuid.UUID) string {
	t.Helper()
	_, assinado := a.usuario(t, perfilID)
	return assinado
}

func (a *ambiente) usuario(t *testing.T, perfilID uuid.UUID) (uuid.UUID, string) {
	t.Helper()

	hash, err := auth.Hash(senhaDeTeste)
	if err != nil {
		t.Fatalf("gerando hash: %v", err)
	}

	email := fmt.Sprintf("res_%s@exemplo.invalid", uuid.NewString()[:8])
	var (
		id     uuid.UUID
		codigo string
	)
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO users (property_id, role_id, name, email, password_hash)
		VALUES ($1, $2, 'Teste Reservas', $3, $4)
		RETURNING id, (SELECT code FROM roles WHERE id = $2)`,
		a.propriedade, perfilID, email, hash).Scan(&id, &codigo); err != nil {
		t.Fatalf("criando usuário: %v", err)
	}
	// A ordem da limpeza importa: um usuário criado dentro de um subteste é
	// apagado ANTES do contato criado no teste pai, e `reservations.created_by`
	// o referencia. Soltar as reservas dele primeiro evita o 23503.
	t.Cleanup(func() {
		a.executar(t, `DELETE FROM reservations WHERE created_by = $1 OR owner_id = $1`, id)
		// `stay_blocks` nascidos de bloqueio operacional não caem por cascade de
		// reserva (não têm reserva) e referenciam o usuário por created_by/owner_id.
		a.executar(t, `DELETE FROM stay_blocks WHERE created_by = $1 OR owner_id = $1`, id)
		// A trilha de auditoria referencia o ator e NÃO cascateia de propósito:
		// apagar o usuário não pode apagar a prova do que ele fez. Nos testes o
		// usuário é descartável, então a trilha dele vai junto.
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

// contato cria o hóspede do teste e limpa TUDO o que nascer preso a ele:
// reservas (que levam pricing, noites, unidades, eventos e stay_blocks por
// cascade) e as chaves de idempotência usadas. Sem isso a suíte não roda duas
// vezes seguidas.
func (a *ambiente) contato(t *testing.T) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO contacts (property_id, name, email)
		VALUES ($1, $2, $3) RETURNING id`,
		a.propriedade, "Hóspede "+sufixo(), "hospede_"+sufixo()+"@exemplo.invalid").Scan(&id); err != nil {
		t.Fatalf("criando contato: %v", err)
	}
	t.Cleanup(func() {
		a.executar(t, `DELETE FROM reservation_guests WHERE contact_id = $1`, id)
		a.executar(t, `DELETE FROM reservations WHERE contact_id = $1`, id)
		a.executar(t, `DELETE FROM contacts WHERE id = $1`, id)
	})
	return id
}

// limparChaves apaga as chaves de idempotência de um prefixo no fim do teste.
func (a *ambiente) limparChaves(t *testing.T, prefixo string) {
	t.Helper()
	t.Cleanup(func() { a.executar(t, `DELETE FROM idempotency_keys WHERE key LIKE $1`, prefixo+"%") })
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

// bloqueioDireto ocupa uma unidade pelo banco, sem passar pela API — o teste que
// exercita o conflito não deve depender de POST /blocks estar correto.
func (a *ambiente) bloqueioDireto(t *testing.T, unidade uuid.UUID, de, ate string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO stay_blocks (property_id, unit_id, source, status, period)
		VALUES ($1, $2, 'maintenance', 'confirmed', daterange($3::date, $4::date, '[)'))
		RETURNING id`, a.propriedade, unidade, de, ate).Scan(&id); err != nil {
		t.Fatalf("bloqueando unidade: %v", err)
	}
	t.Cleanup(func() { a.executar(t, `DELETE FROM stay_blocks WHERE id = $1`, id) })
	return id
}

// desativarUnidade tira a unidade do ar (manutenção) e a devolve no fim do
// teste. É o caminho que o painel usa: `PATCH /units/{id} {"active": false}`.
func (a *ambiente) desativarUnidade(t *testing.T, codigo string) {
	t.Helper()

	if _, err := a.pool.Exec(a.ctx,
		`UPDATE units SET active = false WHERE property_id = $1 AND code = $2`, a.propriedade, codigo); err != nil {
		t.Fatalf("desativando %s: %v", codigo, err)
	}
	t.Cleanup(func() {
		a.executar(t, `UPDATE units SET active = true WHERE property_id = $1 AND code = $2`, a.propriedade, codigo)
	})
}

// apagarReserva existe para o teste do CRÍTICO poder falhar SEM deixar a venda
// indevida travando o calendário dos testes seguintes.
func (a *ambiente) apagarReserva(t *testing.T, id uuid.UUID) {
	t.Helper()
	a.executar(t, `DELETE FROM reservations WHERE id = $1`, id)
}

func (a *ambiente) executar(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := a.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Errorf("limpeza (%s): %v", sql, err)
	}
}

// ─────────────────────────── Corpos e leituras ──────────────────────

// reservaVista é a projeção do schema `Reserva` que os testes conferem.
type reservaVista struct {
	ID           uuid.UUID `json:"id"`
	Codigo       string    `json:"code"`
	Status       string    `json:"status"`
	UnitTypeID   uuid.UUID `json:"unit_type_id"`
	UnitTypeNome string    `json:"unit_type_name"`
	CheckIn      string    `json:"check_in"`
	CheckOut     string    `json:"check_out"`
	Noites       int       `json:"night_count"`
	Subtotal     int64     `json:"subtotal_cents"`
	Desconto     int64     `json:"discount_cents"`
	Limpeza      int64     `json:"cleaning_cents"`
	Total        int64     `json:"total_cents"`
	Sinal        int64     `json:"deposit_cents"`
	Saldo        int64     `json:"balance_cents"`
	PolicyVer    *int      `json:"policy_version"`
	RateTableID  *string   `json:"rate_table_id"`
	CancelPolicy *string   `json:"cancellation_policy_id"`
	HoldExpiraEm *string   `json:"hold_expires_at"`
	ConfirmadaEm *string   `json:"confirmed_at"`
	MotivoCanc   *string   `json:"cancel_reason"`
	RemarcadaDe  *string   `json:"rebooked_from_id"`
	Unidades     []struct {
		UnitID   uuid.UUID `json:"unit_id"`
		UnitCode string    `json:"unit_code"`
		Bloco    *string   `json:"stay_block_id"`
		Travada  bool      `json:"locked"`
	} `json:"units"`
}

func (r reservaVista) codigosDasUnidades() []string {
	out := make([]string, 0, len(r.Unidades))
	for _, u := range r.Unidades {
		out = append(out, u.UnitCode)
	}
	return out
}

// pedido monta o corpo do POST /reservations.
func pedido(produto, contato uuid.UUID, checkIn, checkOut string, hospedes int) map[string]any {
	return map[string]any{
		"unit_type_id": produto,
		"contact_id":   contato,
		"check_in":     checkIn,
		"check_out":    checkOut,
		"guests_count": hospedes,
	}
}

// criarReserva emite uma pré-reserva e exige 201.
func (a *ambiente) criarReserva(t *testing.T, token string, corpo map[string]any) reservaVista {
	t.Helper()

	chave := "it-" + sufixo() + "-" + sufixo()
	a.limparChaves(t, chave)
	resp := a.chamarIdem(t, http.MethodPost, "/reservations", token, corpo, chave)
	if resp.Status != http.StatusCreated {
		t.Fatalf("POST /reservations = %d (%s)", resp.Status, resp.Corpo)
	}
	return dado[reservaVista](t, resp)
}

// statusNoBanco lê o estado das linhas de stay_blocks da reserva, agrupado.
func (a *ambiente) statusDosBlocos(t *testing.T, reserva uuid.UUID) map[string]int {
	t.Helper()

	linhas, err := a.pool.Query(a.ctx,
		`SELECT status, count(*) FROM stay_blocks WHERE reservation_id = $1 GROUP BY status`, reserva)
	if err != nil {
		t.Fatalf("lendo blocos: %v", err)
	}
	defer linhas.Close()

	out := map[string]int{}
	for linhas.Next() {
		var (
			status string
			n      int
		)
		if err := linhas.Scan(&status, &n); err != nil {
			t.Fatalf("lendo blocos: %v", err)
		}
		out[status] = n
	}
	return out
}

// decodificar lê o envelope inteiro (data + meta) numa struct do teste.
func decodificar(t *testing.T, r resposta, alvo any) {
	t.Helper()
	if err := json.Unmarshal(r.Corpo, alvo); err != nil {
		t.Fatalf("lendo a resposta: %v (corpo %s)", err, r.Corpo)
	}
}

func valorOuVazio(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// mesmoJSON compara dois corpos por CONTEÚDO.
//
// A resposta repetida de uma rota idempotente volta do `jsonb` de
// `idempotency_keys`, que normaliza ordem de chaves e espaçamento. Comparar
// bytes reprovaria uma repetição perfeitamente correta.
func mesmoJSON(t *testing.T, a, b []byte) bool {
	t.Helper()

	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatalf("corpo A ilegível: %v", err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatalf("corpo B ilegível: %v", err)
	}
	return reflect.DeepEqual(x, y)
}

func (a *ambiente) statusDaReserva(t *testing.T, reserva uuid.UUID) string {
	t.Helper()

	var status string
	if err := a.pool.QueryRow(a.ctx, `SELECT status FROM reservations WHERE id = $1`, reserva).Scan(&status); err != nil {
		t.Fatalf("lendo status: %v", err)
	}
	return status
}
