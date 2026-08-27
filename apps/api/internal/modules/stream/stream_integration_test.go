//go:build integration

// Prova do caminho INTEIRO: escrita no banco → gatilho → pg_notify → LISTEN do
// hub → fan-out → SSE no cliente.
//
// Os testes de unidade publicam direto no hub, o que exercita o fan-out e o
// formato mas NÃO exercita o que mais tem chance de sair errado numa migration:
// o nome do canal, o nome dos campos do payload e o predicado `WHEN` dos
// gatilhos. Um `UPDATE` que deixe de notificar não quebra nenhum teste de
// unidade — quebra a tela da gestão, em produção, em silêncio.
package stream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/realtime"
)

// tempoMaximoDoEvento é o critério de aceite desta rodada: uma alteração no
// banco tem de aparecer na conexão SSE em menos de 2 s.
const tempoMaximoDoEvento = 2 * time.Second

func poolDeTeste(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL ausente: teste de integração pulado (use `make test-integration`)")
	}
	ctx, cancelar := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelar()

	pool, err := db.New(ctx, url)
	if err != nil {
		t.Fatalf("abrindo pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type cenario struct {
	pool        *pgxpool.Pool
	propriedade uuid.UUID
	unidade     uuid.UUID
	produto     uuid.UUID
	contato     uuid.UUID
	eu          uuid.UUID
	outro       uuid.UUID
}

func montarCenario(t *testing.T, pool *pgxpool.Pool) cenario {
	t.Helper()
	ctx := context.Background()

	var c cenario
	c.pool = pool

	if err := pool.QueryRow(ctx, `SELECT id FROM properties ORDER BY created_at LIMIT 1`).Scan(&c.propriedade); err != nil {
		t.Fatalf("a suíte precisa do seed aplicado (make it-seed): %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM units WHERE property_id = $1 ORDER BY code LIMIT 1`,
		c.propriedade).Scan(&c.unidade); err != nil {
		t.Fatalf("lendo uma unidade do seed: %v", err)
	}
	// `one_member`, e não a Completa: a invariante da casa inteira (migration
	// 20260827100000) só perdoa a composição vazia porque a reserva nasce em
	// `quote` — escolher aqui um produto simples tira este teste do caminho
	// daquela regra por inteiro. Ele é sobre o barramento, não sobre composição.
	if err := pool.QueryRow(ctx,
		`SELECT id FROM unit_types WHERE property_id = $1 AND consumes = 'one_member' ORDER BY sort_order LIMIT 1`,
		c.propriedade).Scan(&c.produto); err != nil {
		t.Fatalf("lendo um produto do seed: %v", err)
	}

	ids := []*uuid.UUID{&c.eu, &c.outro}
	linhas, err := pool.Query(ctx, `SELECT id FROM users WHERE property_id = $1 ORDER BY created_at LIMIT 2`, c.propriedade)
	if err != nil {
		t.Fatalf("lendo usuários do seed: %v", err)
	}
	defer linhas.Close()
	i := 0
	for linhas.Next() && i < 2 {
		if err := linhas.Scan(ids[i]); err != nil {
			t.Fatalf("lendo usuário: %v", err)
		}
		i++
	}
	if i < 2 {
		t.Fatalf("a suíte precisa de dois usuários semeados; achei %d", i)
	}

	c.contato = uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO contacts (id, property_id, name) VALUES ($1, $2, 'Contato do teste de tempo real')`,
		c.contato, c.propriedade); err != nil {
		t.Fatalf("criando contato: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM contacts WHERE id = $1`, c.contato)
	})

	return c
}

// criarReserva insere uma reserva em `quote`.
//
// `quote` e não `hold`: orçamento não é venda viva, então a invariante da casa
// inteira (migration 20260827100000) não exige a composição completa — e este
// teste é sobre o barramento, não sobre a regra de composição. O gatilho de
// notificação dispara em qualquer INSERT, que é justamente o que se quer provar.
func (c cenario) criarReserva(t *testing.T, dono *uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := c.pool.Exec(context.Background(), `
		INSERT INTO reservations
		    (id, property_id, unit_type_id, contact_id, status, check_in, check_out, guests_count, owner_id)
		VALUES ($1, $2, $3, $4, 'quote', DATE '2099-01-10', DATE '2099-01-13', 2, $5)`,
		id, c.propriedade, c.produto, c.contato, dono)
	if err != nil {
		t.Fatalf("criando reserva: %v", err)
	}
	t.Cleanup(func() {
		_, _ = c.pool.Exec(context.Background(), `DELETE FROM reservations WHERE id = $1`, id)
	})
	return id
}

func (c cenario) criarBloco(t *testing.T, dono *uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := c.pool.Exec(context.Background(), `
		INSERT INTO stay_blocks (id, property_id, unit_id, source, status, period, owner_id)
		VALUES ($1, $2, $3, 'maintenance', 'cancelled', daterange(DATE '2099-03-01', DATE '2099-03-04', '[)'), $4)`,
		id, c.propriedade, c.unidade, dono)
	if err != nil {
		t.Fatalf("criando bloco: %v", err)
	}
	t.Cleanup(func() {
		_, _ = c.pool.Exec(context.Background(), `DELETE FROM stay_blocks WHERE id = $1`, id)
	})
	return id
}

// ambienteReal monta hub + handler sobre o banco de verdade.
func ambienteReal(t *testing.T, pool *pgxpool.Pool, u *auth.Usuario) *ambiente {
	t.Helper()

	hub := realtime.NovoHub(realtime.Config{
		Abrir: realtime.AbridorDePool(pool),
		Log:   semLog(),
	})
	hub.Iniciar(context.Background())
	t.Cleanup(hub.Parar)

	prazo := time.Now().Add(10 * time.Second)
	for !hub.Conectado() && time.Now().Before(prazo) {
		time.Sleep(5 * time.Millisecond)
	}
	if !hub.Conectado() {
		t.Fatal("o hub não conseguiu abrir o LISTEN no Postgres")
	}

	h := NovoHandlerCom(hub, NovoFiltro(pool), Opcoes{Batimento: time.Second, Log: semLog()})

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.WithUser(req.Context(), u)))
		})
	})
	r.Get("/stream", h.Assinar)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &ambiente{hub: hub, handler: h, srv: srv, usuario: u}
}

func usuarioReal(id, propriedade uuid.UUID, escopos map[string]string) *auth.Usuario {
	perms := make([]auth.Permissao, 0, len(escopos))
	for recurso, escopo := range escopos {
		perms = append(perms, auth.Permissao{Resource: recurso, Action: auth.AcaoVer, Scope: escopo})
	}
	return &auth.Usuario{ID: id, PropertyID: propriedade, Permissoes: auth.NovoConjunto(perms)}
}

// O critério de aceite da rodada.
func TestAlteracaoNoBancoChegaNoStreamEmMenosDeDoisSegundos(t *testing.T) {
	pool := poolDeTeste(t)
	c := montarCenario(t, pool)
	u := usuarioReal(c.eu, c.propriedade, map[string]string{recursoCalendario: auth.EscopoAll})

	a := ambienteReal(t, pool, u)

	_, leitor, cancelar := a.abrir(t, "topics=calendar", nil)
	defer cancelar()
	if pronto := leitor.proximoDeDados(t, 5*time.Second); pronto.Nome != "ready" {
		t.Fatalf("esperava ready, veio %q", pronto.Nome)
	}

	inicio := time.Now()
	reserva := c.criarReserva(t, &c.eu)

	recebido := leitor.proximoDeDados(t, tempoMaximoDoEvento)
	decorrido := time.Since(inicio)

	if recebido.Nome != realtime.TopicoCalendario {
		t.Fatalf("event: = %q, esperado calendar", recebido.Nome)
	}
	var dados eventoDoStream
	if err := json.Unmarshal([]byte(recebido.Dados), &dados); err != nil {
		t.Fatalf("data ilegível: %v", err)
	}
	if dados.Entity != "reservation" || dados.ID != reserva {
		t.Fatalf("evento errado: %+v (esperava reservation %s)", dados, reserva)
	}
	if dados.V <= 0 {
		t.Fatalf("v = %d; o cliente usa v para descartar reentrega, então ele precisa vir do banco", dados.V)
	}
	if recebido.ID == "" {
		t.Fatal("evento de dado sem id: — o cliente não teria o que mandar em Last-Event-ID")
	}
	t.Logf("INSERT em reservations → evento SSE em %s (teto do aceite: %s)", decorrido.Round(time.Millisecond), tempoMaximoDoEvento)
}

// A `UPDATE` que muda o que o mapa desenha tem de notificar; a que só encosta
// numa coluna decorativa, não. É o predicado `WHEN` dos gatilhos, e é o que
// impede um acerto de `note` de acordar todas as abas da gestão.
func TestUpdateRelevanteNotificaEOIrrelevanteNao(t *testing.T) {
	pool := poolDeTeste(t)
	c := montarCenario(t, pool)
	u := usuarioReal(c.eu, c.propriedade, map[string]string{recursoCalendario: auth.EscopoAll})
	a := ambienteReal(t, pool, u)

	bloco := c.criarBloco(t, &c.eu)

	_, leitor, cancelar := a.abrir(t, "topics=calendar", nil)
	defer cancelar()
	leitor.proximoDeDados(t, 5*time.Second) // ready

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE stay_blocks SET note = 'só um bilhete' WHERE id = $1`, bloco); err != nil {
		t.Fatalf("UPDATE da nota: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE stay_blocks SET status = 'confirmed' WHERE id = $1`, bloco); err != nil {
		t.Fatalf("UPDATE do status: %v", err)
	}

	recebido := leitor.proximoDeDados(t, tempoMaximoDoEvento)
	var dados eventoDoStream
	if err := json.Unmarshal([]byte(recebido.Dados), &dados); err != nil {
		t.Fatalf("data ilegível: %v", err)
	}
	if dados.Entity != "stay_block" || dados.ID != bloco {
		t.Fatalf("evento errado: %+v", dados)
	}
	if dados.UnitID == nil || *dados.UnitID != c.unidade {
		t.Fatalf("unit_id = %v, esperado %v — é o que deixa o mapa repintar uma coluna só", dados.UnitID, c.unidade)
	}

	// Se a nota tivesse notificado, teria chegado ANTES do status — e o que
	// acabou de ser lido seria o evento da nota, não o do status. O `id:`
	// monotônico confirma que só um evento passou por aqui.
	select {
	case extra := <-leitor.eventos:
		if extra.Nome == realtime.TopicoCalendario {
			t.Fatalf("chegou evento a mais: o UPDATE da nota notificou (%s)", extra.Dados)
		}
	case <-time.After(500 * time.Millisecond):
	}
}

// A promessa do contrato: "o corretor não recebe aviso de oportunidade alheia —
// nem o id dela". Aqui, com o calendário: escopo `own` só vê o que é dele.
func TestEscopoOwnNaoRecebeEventoDeLinhaAlheia(t *testing.T) {
	pool := poolDeTeste(t)
	c := montarCenario(t, pool)
	u := usuarioReal(c.eu, c.propriedade, map[string]string{recursoCalendario: auth.EscopoOwn})
	a := ambienteReal(t, pool, u)

	_, leitor, cancelar := a.abrir(t, "topics=calendar", nil)
	defer cancelar()
	leitor.proximoDeDados(t, 5*time.Second) // ready

	// A do outro corretor não pode nem ser mencionada.
	c.criarReserva(t, &c.outro)
	// A minha, sim.
	minha := c.criarReserva(t, &c.eu)

	recebido := leitor.proximoDeDados(t, tempoMaximoDoEvento)
	var dados eventoDoStream
	if err := json.Unmarshal([]byte(recebido.Dados), &dados); err != nil {
		t.Fatalf("data ilegível: %v", err)
	}
	if dados.ID != minha {
		t.Fatalf("o primeiro evento entregue foi %s; a reserva alheia vazou pelo barramento", dados.ID)
	}
}

// Uma mudança atômica que emite vários eventos compartilha o `v`: é assim que o
// cliente sabe que aquilo foi UMA venda e faz UM refetch.
func TestEventosDaMesmaTransacaoCompartilhamAVersao(t *testing.T) {
	pool := poolDeTeste(t)
	c := montarCenario(t, pool)
	u := usuarioReal(c.eu, c.propriedade, map[string]string{recursoCalendario: auth.EscopoAll})
	a := ambienteReal(t, pool, u)

	_, leitor, cancelar := a.abrir(t, "topics=calendar", nil)
	defer cancelar()
	leitor.proximoDeDados(t, 5*time.Second)

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	var ids []uuid.UUID
	for range 2 {
		id := uuid.New()
		ids = append(ids, id)
		if _, err := tx.Exec(ctx, `
			INSERT INTO reservations
			    (id, property_id, unit_type_id, contact_id, status, check_in, check_out, guests_count, owner_id)
			VALUES ($1, $2, $3, $4, 'quote', DATE '2099-05-10', DATE '2099-05-13', 2, $5)`,
			id, c.propriedade, c.produto, c.contato, c.eu); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("INSERT na transação: %v", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	t.Cleanup(func() {
		for _, id := range ids {
			_, _ = pool.Exec(context.Background(), `DELETE FROM reservations WHERE id = $1`, id)
		}
	})

	var versoes []int64
	var cursores []string
	for range 2 {
		ev := leitor.proximoDeDados(t, tempoMaximoDoEvento)
		var dados eventoDoStream
		if err := json.Unmarshal([]byte(ev.Dados), &dados); err != nil {
			t.Fatalf("data ilegível: %v", err)
		}
		versoes = append(versoes, dados.V)
		cursores = append(cursores, ev.ID)
	}

	if versoes[0] != versoes[1] {
		t.Fatalf("v = %v; eventos da MESMA transação têm de compartilhar v, senão o cliente faz N refetches de uma venda só", versoes)
	}
	// E os cursores, ao contrário, são distintos: um é a posição no barramento,
	// o outro é a idade do dado.
	if cursores[0] == cursores[1] {
		t.Fatalf("id: repetido (%s); o cursor do barramento é por EVENTO", cursores[0])
	}
}
