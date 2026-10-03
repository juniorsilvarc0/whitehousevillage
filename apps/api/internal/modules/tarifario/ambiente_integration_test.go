//go:build integration

// Infraestrutura dos testes de integração do tarifário.
//
// Cada execução cria uma PROPRIEDADE só sua e injeta um usuário dessa
// propriedade no contexto. Duas consequências que valem o custo:
//
//   - os testes não encostam no que o seed criou (a Tabela Comercial V1, os 7
//     feriados, as duas políticas v1), então "esta é a única tabela vigente" é
//     uma afirmação verificável em vez de depender do que mais existe no banco;
//   - o escopo por propriedade — que é o que impede uma sessão de reprecificar a
//     casa do vizinho — passa a ser exercitado em todo teste, e não só no que
//     lembrar de checá-lo.
package tarifario_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/tarifario"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/router"
)

type ambiente struct {
	ctx      context.Context
	pool     *pgxpool.Pool
	tx       *db.TxManager
	repo     *tarifario.Repository
	svc      *tarifario.Service
	servidor *httptest.Server

	propriedade uuid.UUID
	produtoA    uuid.UUID
	produtoB    uuid.UUID
	contato     uuid.UUID
	usuario     uuid.UUID
}

// abrirPool devolve UM pool para o pacote inteiro.
//
// Um pool por teste custava 30 aberturas contra o mesmo Postgres em cada
// execução — e o `db.New` faz ping com 5 s de teto. Medido nesta árvore: em duas
// execuções seguidas o primeiro teste reprovou com "banco não respondeu:
// context deadline exceeded" enquanto o pool da execução anterior ainda
// devolvia as 20 conexões. Um pool por processo é, além de mais barato, o que a
// produção faz de qualquer forma.
var abrirPool = sync.OnceValues(func() (*pgxpool.Pool, error) {
	return db.New(context.Background(), os.Getenv("DATABASE_URL"))
})

// TestMain fecha o pool compartilhado depois da última cleanup de teste.
func TestMain(m *testing.M) {
	codigo := m.Run()
	if os.Getenv("DATABASE_URL") != "" {
		if pool, err := abrirPool(); err == nil {
			pool.Close()
		}
	}
	os.Exit(codigo)
}

func subir(t *testing.T) *ambiente {
	t.Helper()

	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL ausente: teste de integração pulado")
	}

	ctx := context.Background()
	pool, err := abrirPool()
	if err != nil {
		t.Fatalf("abrindo pool: %v", err)
	}

	repo := tarifario.NovoRepository(pool)
	tx := db.NewTxManager(pool)
	svc := tarifario.NovoService(repo, tx)

	a := &ambiente{ctx: ctx, pool: pool, tx: tx, repo: repo, svc: svc}
	a.criarPropriedade(t)
	a.montarServidor(t, tarifario.NovoHandlerComService(svc))
	return a
}

// criarPropriedade monta a casa de teste com dois produtos e um contato.
func (a *ambiente) criarPropriedade(t *testing.T) {
	t.Helper()

	sufixo := uuid.NewString()[:8]
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO properties (name, slug, timezone, city, state)
		VALUES ($1, $2, 'America/Fortaleza', 'Luís Correia', 'PI')
		RETURNING id`,
		"Casa de teste "+sufixo, "casa-de-teste-"+sufixo).Scan(&a.propriedade); err != nil {
		t.Fatalf("criando a propriedade de teste: %v", err)
	}

	// O usuário de teste tem de EXISTIR em `users`: `audit_log.actor_id` tem
	// chave estrangeira para lá, e um ator inventado faria a trilha derrubar a
	// transação do negócio com 23503 em toda escrita. Reaproveita o perfil que o
	// seed criou — o teste não exercita RBAC, exercita o endpoint.
	a.usuario = a.criarUsuario(t, sufixo)

	a.produtoA = a.criarProduto(t, "AP2S-"+sufixo, "Apartamento 2 Suítes", 4, 1)
	a.produtoB = a.criarProduto(t, "COB-"+sufixo, "Cobertura", 6, 2)

	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO contacts (property_id, name, lgpd_basis)
		VALUES ($1, $2, 'legitimo_interesse')
		RETURNING id`, a.propriedade, "Hóspede de teste "+sufixo).Scan(&a.contato); err != nil {
		t.Fatalf("criando o contato de teste: %v", err)
	}

	// Limpeza em ordem de dependência: as FKs para `properties` não são em
	// cascata, e deixar lixo faria a próxima execução no MESMO container medir
	// um banco diferente.
	t.Cleanup(func() { a.limpar(t) })
}

func (a *ambiente) criarUsuario(t *testing.T, sufixo string) uuid.UUID {
	t.Helper()

	var perfil uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`SELECT id FROM roles ORDER BY created_at LIMIT 1`).Scan(&perfil); err != nil {
		t.Fatalf("lendo um perfil (o seed rodou?): %v", err)
	}

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO users (property_id, role_id, name, email, password_hash)
		VALUES ($1, $2, $3, $4, 'nao-usado-neste-teste')
		RETURNING id`,
		a.propriedade, perfil, "Gestor de teste "+sufixo, "gestor-"+sufixo+"@teste.local").Scan(&id); err != nil {
		t.Fatalf("criando o usuário de teste: %v", err)
	}
	return id
}

func (a *ambiente) criarProduto(t *testing.T, codigo, nome string, capacidade, ordem int) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO unit_types (property_id, code, name, capacity, consumes, cleaning_fee_cents, sort_order)
		VALUES ($1, $2, $3, $4, 'one_member', 18000, $5)
		RETURNING id`, a.propriedade, codigo, nome, capacidade, ordem).Scan(&id); err != nil {
		t.Fatalf("criando o produto %s: %v", codigo, err)
	}
	return id
}

func (a *ambiente) limpar(t *testing.T) {
	t.Helper()

	limpezas := []string{
		`DELETE FROM audit_log WHERE property_id = $1`,
		`DELETE FROM reservation_nights WHERE reservation_id IN (SELECT id FROM reservations WHERE property_id = $1)`,
		`DELETE FROM reservation_pricing WHERE reservation_id IN (SELECT id FROM reservations WHERE property_id = $1)`,
		`DELETE FROM reservations WHERE property_id = $1`,
		`DELETE FROM contacts WHERE property_id = $1`,
		`DELETE FROM rates WHERE rate_table_id IN (SELECT id FROM rate_tables WHERE property_id = $1)`,
		`DELETE FROM min_nights_rules WHERE rate_table_id IN (SELECT id FROM rate_tables WHERE property_id = $1)`,
		`DELETE FROM rate_tables WHERE property_id = $1`,
		`DELETE FROM holidays WHERE property_id = $1`,
		`DELETE FROM special_periods WHERE property_id = $1`,
		`DELETE FROM cancellation_tiers WHERE policy_id IN (SELECT id FROM cancellation_policies WHERE property_id = $1)`,
		`DELETE FROM cancellation_policies WHERE property_id = $1`,
		`DELETE FROM commercial_policies WHERE property_id = $1`,
		`DELETE FROM unit_types WHERE property_id = $1`,
		`DELETE FROM users WHERE property_id = $1`,
		`DELETE FROM properties WHERE id = $1`,
	}
	for _, q := range limpezas {
		if _, err := a.pool.Exec(context.Background(), q, a.propriedade); err != nil {
			t.Logf("limpeza (%s): %v", q, err)
		}
	}
}

// montarServidor sobe SÓ as rotas deste módulo, com a identidade já injetada.
//
// Não usa `router.New` porque o `main` ainda não constrói o handler do
// tarifário: a tabela real devolveria zero rotas nossas. A autorização por
// matriz é coberta pelo teste de contrato de rotas; o que se prova aqui é o
// comportamento do endpoint — status, envelope e o que sobra gravado.
func (a *ambiente) montarServidor(t *testing.T, h *tarifario.Handler) {
	t.Helper()

	usuario := &auth.Usuario{ID: a.usuario, PropertyID: a.propriedade, Nome: "Gestor de teste"}

	mux := chi.NewRouter()
	mux.Use(func(proximo http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			proximo.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), usuario)))
		})
	})

	semModulo := map[string]bool{}
	for _, r := range router.Rotas(router.Deps{}) {
		semModulo[r.Metodo+" "+r.Path] = true
	}
	registradas := 0
	mux.Route(router.PrefixoDaAPI, func(api chi.Router) {
		for _, rota := range router.Rotas(router.Deps{Tarifario: h}) {
			if semModulo[rota.Metodo+" "+rota.Path] {
				continue
			}
			api.Method(rota.Metodo, rota.Path, rota.Handler)
			registradas++
		}
	})
	if registradas == 0 {
		t.Fatal("nenhuma rota do tarifário registrada — rotasTarifario voltou a devolver nil?")
	}

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	a.servidor = srv
}

// ─────────────────────────── HTTP ───────────────────────────────────────────

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

// dados desembrulha o `data` do envelope no destino informado.
func (r resposta) dados(t *testing.T, destino any) {
	t.Helper()

	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(r.Corpo, &env); err != nil {
		t.Fatalf("lendo o envelope: %v (corpo: %s)", err, r.Corpo)
	}
	if err := json.Unmarshal(env.Data, destino); err != nil {
		t.Fatalf("lendo o data: %v (corpo: %s)", err, r.Corpo)
	}
}

func (a *ambiente) chamar(t *testing.T, metodo, caminho string, corpo any) resposta {
	t.Helper()

	var body io.Reader
	if corpo != nil {
		bruto, err := json.Marshal(corpo)
		if err != nil {
			t.Fatalf("serializando o corpo: %v", err)
		}
		body = bytes.NewReader(bruto)
	}

	req, err := http.NewRequestWithContext(a.ctx, metodo, a.servidor.URL+router.PrefixoDaAPI+caminho, body)
	if err != nil {
		t.Fatalf("montando a requisição: %v", err)
	}
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
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

func (a *ambiente) exigir(t *testing.T, esperado int, metodo, caminho string, corpo any) resposta {
	t.Helper()

	r := a.chamar(t, metodo, caminho, corpo)
	if r.Status != esperado {
		t.Fatalf("%s %s: status %d, esperado %d — corpo: %s", metodo, caminho, r.Status, esperado, r.Corpo)
	}
	return r
}

// ─────────────────────────── Fixtures de tarifário ──────────────────────────

func (a *ambiente) criarTabela(t *testing.T, nome, de string, ate *string) uuid.UUID {
	t.Helper()

	corpo := map[string]any{"name": nome, "valid_from": de}
	if ate != nil {
		corpo["valid_to"] = *ate
	}

	var tabela struct {
		ID uuid.UUID `json:"id"`
	}
	a.exigir(t, http.StatusCreated, http.MethodPost, "/rate-tables", corpo).dados(t, &tabela)
	return tabela.ID
}

// gradeCompleta é a grade mínima que faz um orçamento sair: os seis tipos de
// data para um produto.
func gradeCompleta(produto uuid.UUID, base int64) []map[string]any {
	tipos := []string{"normal", "fds", "feriado", "alta", "reveillon", "carnaval"}
	out := make([]map[string]any, 0, len(tipos))
	for i, tipo := range tipos {
		out = append(out, map[string]any{
			"unit_type_id": produto,
			"date_type":    tipo,
			"amount_cents": base + int64(i)*10000,
		})
	}
	return out
}

func (a *ambiente) tarifasDaTabela(t *testing.T, tabela uuid.UUID) map[string]int64 {
	t.Helper()

	linhas, err := a.pool.Query(a.ctx,
		`SELECT unit_type_id::text || ':' || date_type, amount_cents FROM rates WHERE rate_table_id = $1`, tabela)
	if err != nil {
		t.Fatalf("lendo a grade: %v", err)
	}
	defer linhas.Close()

	out := map[string]int64{}
	for linhas.Next() {
		var (
			chave string
			valor int64
		)
		if err := linhas.Scan(&chave, &valor); err != nil {
			t.Fatalf("lendo a grade: %v", err)
		}
		out[chave] = valor
	}
	return out
}

func hojeMais(dias int) string {
	return time.Now().AddDate(0, 0, dias).Format("2006-01-02")
}

func texto(s string) *string { return &s }

func jsonUnmarshal(bruto []byte, destino any) error { return json.Unmarshal(bruto, destino) }

// reservaEmitida grava uma venda JÁ FECHADA, com o snapshot financeiro
// completo: a reserva, o satélite `reservation_pricing` e uma linha por noite em
// `reservation_nights`.
//
// É o fixture do teste que mais importa neste módulo — o que prova que mexer no
// tarifário não reescreve o passado. Ele é montado por SQL direto, e não pelo
// módulo de reservas, porque aquele módulo está sendo escrito em paralelo: o que
// se quer congelar aqui são as COLUNAS, e elas já existem.
func (a *ambiente) reservaEmitida(t *testing.T, tabela uuid.UUID, precoPorNoite int64, noites int) uuid.UUID {
	t.Helper()

	entrada := time.Now().AddDate(0, 0, 20)
	saida := entrada.AddDate(0, 0, noites)

	var reserva uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO reservations
			(property_id, unit_type_id, contact_id, status, check_in, check_out, guests_count, confirmed_at)
		VALUES ($1, $2, $3, 'confirmed', $4, $5, 2, now())
		RETURNING id`,
		a.propriedade, a.produtoA, a.contato, entrada, saida).Scan(&reserva); err != nil {
		t.Fatalf("criando a reserva de teste: %v", err)
	}

	subtotal := precoPorNoite * int64(noites)
	const limpeza int64 = 18000
	total := subtotal + limpeza

	if _, err := a.pool.Exec(a.ctx, `
		INSERT INTO reservation_pricing
			(reservation_id, subtotal_cents, cleaning_cents, total_cents, deposit_cents, rate_table_id, policy_version)
		VALUES ($1, $2, $3, $4, $5, $6, 1)`,
		reserva, subtotal, limpeza, total, total/2, tabela); err != nil {
		t.Fatalf("gravando o preço congelado: %v", err)
	}

	for i := 0; i < noites; i++ {
		if _, err := a.pool.Exec(a.ctx, `
			INSERT INTO reservation_nights (reservation_id, night, date_type, unit_type_id, price_cents)
			VALUES ($1, $2, 'normal', $3, $4)`,
			reserva, entrada.AddDate(0, 0, i), a.produtoA, precoPorNoite); err != nil {
			t.Fatalf("gravando a noite %d: %v", i, err)
		}
	}
	return reserva
}

// ─────────────────────────── Trilha de auditoria ────────────────────────────

// linhasDeAuditoria conta as linhas de `audit_log` de uma ação sobre uma
// entidade, DESTA propriedade — o filtro é o que impede o teste de contar o que
// outro pacote da execução escreveu no mesmo Postgres.
func (a *ambiente) linhasDeAuditoria(t *testing.T, acao string, entidade uuid.UUID) int {
	t.Helper()

	var n int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*) FROM audit_log
		 WHERE property_id = $1 AND action = $2 AND entity_id = $3`,
		a.propriedade, acao, entidade).Scan(&n); err != nil {
		t.Fatalf("contando a trilha de %s: %v", acao, err)
	}
	return n
}

func (a *ambiente) totalDeAuditoria(t *testing.T) int {
	t.Helper()

	var n int
	if err := a.pool.QueryRow(a.ctx,
		`SELECT count(*) FROM audit_log WHERE property_id = $1`, a.propriedade).Scan(&n); err != nil {
		t.Fatalf("contando a trilha: %v", err)
	}
	return n
}

// ultimaTrilha devolve os dois documentos e o ator da linha mais recente.
func (a *ambiente) ultimaTrilha(t *testing.T, acao string, entidade uuid.UUID) (map[string]any, map[string]any, uuid.UUID) {
	t.Helper()

	var (
		bruAntes, bruDepois []byte
		ator                *uuid.UUID
	)
	if err := a.pool.QueryRow(a.ctx, `
		SELECT before, after, actor_id FROM audit_log
		 WHERE property_id = $1 AND action = $2 AND entity_id = $3
		 ORDER BY at DESC LIMIT 1`,
		a.propriedade, acao, entidade).Scan(&bruAntes, &bruDepois, &ator); err != nil {
		t.Fatalf("lendo a trilha de %s: %v", acao, err)
	}

	ler := func(bruto []byte) map[string]any {
		if len(bruto) == 0 {
			return map[string]any{}
		}
		out := map[string]any{}
		if err := json.Unmarshal(bruto, &out); err != nil {
			t.Fatalf("lendo o documento da trilha: %v", err)
		}
		return out
	}

	id := uuid.Nil
	if ator != nil {
		id = *ator
	}
	return ler(bruAntes), ler(bruDepois), id
}

func (a *ambiente) codigoDoProduto(t *testing.T, produto uuid.UUID) string {
	t.Helper()

	var codigo string
	if err := a.pool.QueryRow(a.ctx, `SELECT code FROM unit_types WHERE id = $1`, produto).Scan(&codigo); err != nil {
		t.Fatalf("lendo o código do produto: %v", err)
	}
	return codigo
}
