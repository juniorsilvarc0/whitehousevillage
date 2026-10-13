//go:build integration

// Infraestrutura dos testes de integração da agenda operacional.
//
// Sobe as rotas da agenda E as da reserva sobre um Postgres real e conversa com
// elas por HTTP, atravessando o autenticador e o RBAC de verdade. A tabela de
// rotas NÃO é espelhada: vem de `router.Rotas(router.Deps{Agenda, Reservas})`,
// e o teste exercita as linhas de `internal/router/rotas_agenda.go` com o par
// (recurso, ação) de cada uma — inclusive o `settings:editar` do PUT dos
// horários. As da reserva entram porque a agenda SEGUE a reserva: confirmar,
// cancelar, remarcar, registrar estadia e realocar são passos dela.
//
// Unidades próprias (`IT-AGENDA-…`) para o que é só da agenda; as do seed
// (catálogo `teste`) para o que precisa de venda, em datas a ~2 anos de hoje,
// longe das que os outros pacotes usam. Uma limpeza só, registrada primeiro e
// portanto a ÚLTIMA a rodar, na ordem das FKs RESTRICT da agenda.
package agenda_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/agenda"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/contatos"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/reservas"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/router"
)

const (
	prefixoDaAPI = "/api/v1"
	segredoJWT   = "segredo-de-integracao-da-agenda-32-bytes"
	senhaDeTeste = "senha-de-integracao-2026"

	// rotasDaAgenda é o que o contrato declara: 6 paths, 15 operações.
	rotasDaAgenda = 15
)

// Um pool por PACOTE: abrir um por teste custa o ping de boot a cada vez.
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
	// zona e hoje são os da CASA (properties.timezone e o now() do banco):
	// todo instante esperado nos testes sai daqui, e não do relógio do
	// processo.
	zona calendar.Zone
	hoje calendar.Date

	mu       sync.Mutex
	unidades []uuid.UUID
	contatos []uuid.UUID
	usuarios []uuid.UUID
	perfis   []uuid.UUID
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
	ha := agenda.NovoHandler(pool, db.NewTxManager(pool))
	hr := reservas.NovoHandler(pool, db.NewTxManager(pool))
	// O contato entra pelo `DELETE /contacts/{id}`: o cliente de uma visita
	// não se apaga (contatos.Vinculos conta a agenda).
	hc := contatos.NovoHandler(pool, db.NewTxManager(pool))

	r := chi.NewRouter()
	montadas := 0
	r.Route(prefixoDaAPI, func(api chi.Router) {
		api.Use(httpx.RequestID)
		api.Use(audit.Middleware)
		api.Use(autenticador.Middleware)
		for _, rota := range router.Rotas(router.Deps{Agenda: ha, Reservas: hr, Contatos: hc}) {
			daAgenda := strings.HasPrefix(rota.Path, "/agenda/")
			daReserva := strings.HasPrefix(rota.Path, "/reservations") || strings.HasPrefix(rota.Path, "/blocks")
			doContato := rota.Path == "/contacts/{id}"
			if !daAgenda && !daReserva && !doContato {
				continue
			}
			api.Method(rota.Metodo, rota.Path, auth.Middleware(rota.Recurso, rota.Acao)(rota.Handler))
			if daAgenda {
				montadas++
			}
		}
	})
	if montadas != rotasDaAgenda {
		t.Fatalf("a tabela de rotas tem %d linhas da agenda; o contrato declara %d operações", montadas, rotasDaAgenda)
	}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	a := &ambiente{pool: pool, servidor: srv, emissor: emissor, ctx: ctx}
	var (
		fuso  string
		agora time.Time
	)
	if err := pool.QueryRow(ctx, `SELECT id, timezone, now() FROM properties ORDER BY created_at LIMIT 1`).
		Scan(&a.propriedade, &fuso, &agora); err != nil {
		t.Fatalf("lendo a propriedade do seed (rodou cmd/seed?): %v", err)
	}
	a.zona = calendar.MustLoadZone(fuso)
	a.hoje = a.zona.DateOf(agora)
	t.Cleanup(func() { a.limpar(t) })
	return a
}

// limpar desfaz tudo o que os testes criaram, na ordem das FKs RESTRICT: a
// agenda segura reserva, unidade, contato e usuário; a reserva segura o
// contato; o bloqueio segura a unidade e o usuário.
func (a *ambiente) limpar(t *testing.T) {
	a.mu.Lock()
	defer a.mu.Unlock()
	u, c, us := a.unidades, a.contatos, a.usuarios
	a.executar(t, `
		DELETE FROM agenda_events
		 WHERE unit_id = ANY($1::uuid[]) OR contact_id = ANY($2::uuid[])
		    OR created_by = ANY($3::uuid[]) OR assignee_id = ANY($3::uuid[]) OR done_by = ANY($3::uuid[])
		    OR reservation_id IN (SELECT id FROM reservations
		                           WHERE contact_id = ANY($2::uuid[]) OR created_by = ANY($3::uuid[])
		                              OR owner_id = ANY($3::uuid[]))`, u, c, us)
	a.executar(t, `DELETE FROM agenda_blocks WHERE created_by = ANY($1::uuid[])`, us)
	a.executar(t, `UPDATE agenda_settings SET updated_by = NULL WHERE updated_by = ANY($1::uuid[])`, us)
	a.executar(t, `DELETE FROM maintenance_orders WHERE unit_id = ANY($1::uuid[])`, u)
	a.executar(t, `DELETE FROM reservation_guests WHERE contact_id = ANY($1::uuid[])`, c)
	a.executar(t, `DELETE FROM reservations
		WHERE contact_id = ANY($1::uuid[]) OR created_by = ANY($2::uuid[]) OR owner_id = ANY($2::uuid[])`, c, us)
	a.executar(t, `DELETE FROM stay_blocks
		WHERE unit_id = ANY($1::uuid[]) OR created_by = ANY($2::uuid[]) OR owner_id = ANY($2::uuid[])`, u, us)
	a.executar(t, `DELETE FROM units WHERE id = ANY($1::uuid[])`, u)
	a.executar(t, `DELETE FROM pii_access_log WHERE actor_id = ANY($1::uuid[]) OR contact_id = ANY($2::uuid[])`, us, c)
	a.executar(t, `DELETE FROM contacts WHERE id = ANY($1::uuid[])`, c)
	a.executar(t, `DELETE FROM audit_log WHERE actor_id = ANY($1::uuid[])`, us)
	a.executar(t, `DELETE FROM users WHERE id = ANY($1::uuid[])`, us)
	a.executar(t, `DELETE FROM roles WHERE id = ANY($1::uuid[])`, a.perfis)
}

func (a *ambiente) executar(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := a.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Errorf("limpeza (%s): %v", strings.Join(strings.Fields(sql), " "), err)
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

type paginacao struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

func lista[T any](t *testing.T, r resposta) ([]T, paginacao) {
	t.Helper()
	var env struct {
		Data []T       `json:"data"`
		Meta paginacao `json:"meta"`
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
	return a.chamarCom(t, metodo, caminho, token, corpo, nil)
}

func (a *ambiente) chamarCom(t *testing.T, metodo, caminho, token string, corpo any, cabecalhos map[string]string) resposta {
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
	for k, v := range cabecalhos {
		req.Header.Set(k, v)
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

func sufixo() string { return uuid.NewString()[:8] }

// perfil cria um papel descartável; cada célula é "recurso:ação", no escopo dado.
func (a *ambiente) perfil(t *testing.T, escopo string, celulas ...string) uuid.UUID {
	t.Helper()
	codigo := "agenda_" + sufixo()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`INSERT INTO roles (code, name, is_system) VALUES ($1, $1, false) RETURNING id`, codigo).Scan(&id); err != nil {
		t.Fatalf("criando perfil: %v", err)
	}
	a.mu.Lock()
	a.perfis = append(a.perfis, id)
	a.mu.Unlock()
	for _, celula := range celulas {
		i := strings.LastIndex(celula, ":")
		if _, err := a.pool.Exec(a.ctx,
			`INSERT INTO role_permissions (role_id, resource_code, action, scope) VALUES ($1, $2, $3, $4)`,
			id, celula[:i], celula[i+1:], escopo); err != nil {
			t.Fatalf("concedendo %s: %v", celula, err)
		}
	}
	return id
}

func (a *ambiente) usuario(t *testing.T, perfil uuid.UUID, nome string) (uuid.UUID, string) {
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
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, (SELECT code FROM roles WHERE id = $2)`,
		a.propriedade, perfil, nome, "agenda_"+sufixo()+"@exemplo.invalid", hash).Scan(&id, &codigo); err != nil {
		t.Fatalf("criando usuário: %v", err)
	}
	a.mu.Lock()
	a.usuarios = append(a.usuarios, id)
	a.mu.Unlock()
	assinado, _, err := a.emissor.Issue(id, codigo)
	if err != nil {
		t.Fatalf("assinando token: %v", err)
	}
	return id, assinado
}

// celulasDaAgenda são as quatro ações de `agenda`.
var celulasDaAgenda = []string{"agenda:ver", "agenda:criar", "agenda:editar", "agenda:excluir"}

// gestor opera a casa: a agenda e a reserva em `all`, e grava os horários
// (`settings:editar`).
func (a *ambiente) gestor(t *testing.T) (uuid.UUID, string) {
	t.Helper()
	celulas := append([]string{
		"reservations:ver", "reservations:criar", "reservations:editar", "reservations:excluir",
		"calendar:ver", "calendar:criar", "calendar:excluir", "quotes:ver", "quotes:criar",
		"settings:ver", "settings:editar",
	}, celulasDaAgenda...)
	return a.usuario(t, a.perfil(t, auth.EscopoAll, celulas...), "Gestora da Agenda")
}

// emOwn tem as quatro ações de `agenda` em `own` — o corretor do seed.
func (a *ambiente) emOwn(t *testing.T, nome string) (uuid.UUID, string) {
	t.Helper()
	return a.usuario(t, a.perfil(t, auth.EscopoOwn, celulasDaAgenda...), nome)
}

// ─────────────────────────── Fixtures ───────────────────────────────

// unidade cria uma unidade própria, sem produto: o calendário do seed fica
// intocado.
func (a *ambiente) unidade(t *testing.T) (uuid.UUID, string) {
	t.Helper()
	codigo := "IT-AGENDA-" + sufixo()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO units (property_id, code, name, sort_order) VALUES ($1, $2, $2, 991) RETURNING id`,
		a.propriedade, codigo).Scan(&id); err != nil {
		t.Fatalf("criando unidade: %v", err)
	}
	a.mu.Lock()
	a.unidades = append(a.unidades, id)
	a.mu.Unlock()
	return id, codigo
}

func (a *ambiente) contato(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO contacts (property_id, name, email) VALUES ($1, $2, $3) RETURNING id`,
		a.propriedade, "Hóspede da Agenda "+sufixo(), "agenda_"+sufixo()+"@exemplo.invalid").Scan(&id); err != nil {
		t.Fatalf("criando contato: %v", err)
	}
	a.mu.Lock()
	a.contatos = append(a.contatos, id)
	a.mu.Unlock()
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

func (a *ambiente) unidadeDoSeed(t *testing.T, codigo string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`SELECT id FROM units WHERE property_id = $1 AND code = $2`, a.propriedade, codigo).Scan(&id); err != nil {
		t.Fatalf("unidade %q do seed: %v", codigo, err)
	}
	return id
}

// bloqueio ocupa a unidade pelo banco (`stay_blocks`), como a ordem de
// manutenção ou o `POST /blocks` fariam.
func (a *ambiente) bloqueio(t *testing.T, unidade uuid.UUID, origem string, de, ate calendar.Date, nota *string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO stay_blocks (property_id, unit_id, source, status, period, note)
		VALUES ($1, $2, $3, 'confirmed', daterange($4::date, $5::date, '[)'), $6)
		RETURNING id`, a.propriedade, unidade, origem, de.String(), ate.String(), nota).Scan(&id); err != nil {
		t.Fatalf("bloqueando unidade: %v", err)
	}
	return id
}

// em é o instante `hh:mm` do dia `d` NA CASA — pela mesma porta do módulo.
func (a *ambiente) em(d calendar.Date, hhmm string) time.Time {
	return a.zona.At(d, calendar.MustParseClock(hhmm))
}

// rfc devolve o instante no formato do contrato (com o offset da casa).
func (a *ambiente) rfc(d calendar.Date, hhmm string) string {
	return a.zona.In(a.em(d, hhmm)).Format(time.RFC3339)
}

func chave() string { return "it-agenda-" + sufixo() + "-" + sufixo() }

// ─────────────────────────── A reserva ──────────────────────────────

type unidadeVista struct {
	ID     uuid.UUID `json:"unit_id"`
	Codigo string    `json:"unit_code"`
}

type reservaVista struct {
	ID       uuid.UUID      `json:"id"`
	Codigo   string         `json:"code"`
	Status   string         `json:"status"`
	CheckIn  string         `json:"check_in"`
	CheckOut string         `json:"check_out"`
	Unidades []unidadeVista `json:"units"`
}

// criarReserva emite a pré-reserva e exige 201.
func (a *ambiente) criarReserva(t *testing.T, token string, produto, contato uuid.UUID, de, ate calendar.Date) reservaVista {
	t.Helper()
	r := a.chamarCom(t, http.MethodPost, "/reservations", token, map[string]any{
		"unit_type_id": produto, "contact_id": contato,
		"check_in": de.String(), "check_out": ate.String(), "guests_count": 2,
	}, map[string]string{"Idempotency-Key": chave()})
	exigir(t, r, http.StatusCreated, "POST /reservations")
	return dado[reservaVista](t, r)
}

// confirmar registra o sinal com a chave dada.
func (a *ambiente) confirmar(t *testing.T, token string, reserva uuid.UUID, k string) resposta {
	t.Helper()
	return a.chamarCom(t, http.MethodPost, "/reservations/"+reserva.String()+"/confirm", token,
		map[string]any{"method": "pix"}, map[string]string{"Idempotency-Key": k})
}

// reservaConfirmada vende e confirma.
func (a *ambiente) reservaConfirmada(t *testing.T, token, produto string, contato uuid.UUID, de, ate calendar.Date) reservaVista {
	t.Helper()
	r := a.criarReserva(t, token, a.produto(t, produto), contato, de, ate)
	exigir(t, a.confirmar(t, token, r.ID, chave()), http.StatusOK, "confirmando "+r.Codigo)
	return r
}

// ─────────────────────────── O evento ───────────────────────────────

type eventoVisto struct {
	ID                uuid.UUID      `json:"id"`
	Tipo              string         `json:"type"`
	Origem            string         `json:"origin"`
	Status            string         `json:"status"`
	Titulo            string         `json:"title"`
	Inicio            time.Time      `json:"starts_at"`
	Fim               time.Time      `json:"ends_at"`
	DiaInteiro        bool           `json:"all_day"`
	Data              string         `json:"date"`
	Unidades          []unidadeVista `json:"units"`
	UnidadeID         *uuid.UUID     `json:"unit_id"`
	ReservaID         *uuid.UUID     `json:"reservation_id"`
	ReservaCodigo     *string        `json:"reservation_code"`
	ContatoID         *uuid.UUID     `json:"contact_id"`
	ContatoNome       *string        `json:"contact_name"`
	ResponsavelID     *uuid.UUID     `json:"assignee_id"`
	ResponsavelNome   *string        `json:"assignee_name"`
	Notas             *string        `json:"notes"`
	OrdemID           *uuid.UUID     `json:"maintenance_order_id"`
	BloqueioID        *uuid.UUID     `json:"stay_block_id"`
	ConcluidoEm       *time.Time     `json:"done_at"`
	ConcluidoPor      *uuid.UUID     `json:"done_by"`
	CanceladoEm       *time.Time     `json:"cancelled_at"`
	ConflitoDeUnidade bool           `json:"has_unit_conflict"`
	Acoes             []string       `json:"allowed_actions"`
	Editavel          string         `json:"editable"`
	CriadoPor         *uuid.UUID     `json:"created_by"`
	CriadoEm          *time.Time     `json:"created_at"`
}

type avisoVisto struct {
	Tipo  string     `json:"kind"`
	RefID *uuid.UUID `json:"ref_id"`
}

type escritaVista struct {
	Data eventoVisto `json:"data"`
	Meta struct {
		Avisos []avisoVisto `json:"warnings"`
	} `json:"meta"`
}

func escrita(t *testing.T, r resposta) escritaVista {
	t.Helper()
	var e escritaVista
	if err := json.Unmarshal(r.Corpo, &e); err != nil {
		t.Fatalf("lendo a escrita: %v (corpo %s)", err, r.Corpo)
	}
	return e
}

// marcar cria o evento à mão e exige 201.
func (a *ambiente) marcar(t *testing.T, token string, corpo map[string]any) escritaVista {
	t.Helper()
	r := a.chamar(t, http.MethodPost, "/agenda/events", token, corpo)
	exigir(t, r, http.StatusCreated, "POST /agenda/events")
	return escrita(t, r)
}

// eventosDaReserva lista os eventos ligados à reserva, em qualquer estado.
func (a *ambiente) eventosDaReserva(t *testing.T, token string, reserva uuid.UUID) []eventoVisto {
	t.Helper()
	r := a.chamar(t, http.MethodGet, "/agenda/events?per_page=100&status=agendado,concluido,cancelado&reservation_id="+reserva.String(), token, nil)
	exigir(t, r, http.StatusOK, "GET /agenda/events?reservation_id")
	evs, _ := lista[eventoVisto](t, r)
	return evs
}

// porTipo indexa os eventos automáticos pelo tipo.
func porTipo(t *testing.T, evs []eventoVisto) map[string]eventoVisto {
	t.Helper()
	out := map[string]eventoVisto{}
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

func (a *ambiente) exigirJanela(t *testing.T, e eventoVisto, d calendar.Date, de, ate, contexto string) {
	t.Helper()
	if !e.Inicio.Equal(a.em(d, de)) || !e.Fim.Equal(a.em(d, ate)) {
		t.Fatalf("%s: janela %s–%s, esperada %s %s–%s", contexto,
			e.Inicio.Format(time.RFC3339), e.Fim.Format(time.RFC3339), d, de, ate)
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

func janelaDaQuery(de, ate calendar.Date) string {
	return fmt.Sprintf("from=%s&to=%s", de, ate)
}
