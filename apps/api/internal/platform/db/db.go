// Package db abre o pool do Postgres, expõe o executor que os repositórios
// consomem e o gerenciador de transação.
//
// A regra que este pacote existe para sustentar: a transação nasce e morre no
// service; o repositório nunca chama Begin. Ele pega o executor do contexto com
// From — que devolve a transação em curso, se houver, ou o pool. Assim a mesma
// função de repositório serve dentro e fora de transação, sem duplicar SQL nem
// espalhar `*pgx.Tx` pelas assinaturas.
package db

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Parâmetros de sessão aplicados em TODA conexão do pool.
//
// lockTimeout é o que impede que uma disputa de datas vire impasse.
//
// Medido neste repositório (Postgres 16, 50 pedidos simultâneos da mesma
// unidade e do mesmo intervalo): sem lock_timeout, quem perde a corrida fica
// preso em `XactLockTableWait` esperando a transação vencedora enquanto verifica
// a constraint de exclusão. Quando duas dessas esperas se cruzam, o Postgres só
// desfaz o nó depois de `deadlock_timeout` (1 s, padrão) e devolve
// `40P01 deadlock detected` — e as esperas se encadeiam, então o subteste
// inteiro levou de 83 s a 99 s nas execuções em que o nó se formou.
//
// Com lock_timeout ABAIXO de deadlock_timeout, a mesma espera estoura antes
// como `55P03` — um erro traduzível, atribuído a UMA transação, sem detector de
// impasse e sem arrastar a fila inteira. O caminho feliz não sente: a espera
// legítima aqui é o tempo de um INSERT + COMMIT (milissegundos), três ordens de
// grandeza abaixo do teto.
//
// statementTimeout é o teto de consulta desgovernada. Fica MUITO acima do
// lock_timeout de propósito: espera por lock precisa estourar sempre como 55P03
// (que o TxManager repete e o MapError traduz) e nunca como 57014, que é
// "consulta lenta demais" — problema diferente, com resposta diferente. Quem
// precisar de mais tempo numa consulta específica usa `SET LOCAL
// statement_timeout` dentro da própria transação.
const (
	lockTimeout      = "400ms"
	statementTimeout = "30s"
)

// Orçamento de repetição do TxManager. Ver TxManager.Do.
//
// maxTentativas=12 não é chute: com 6, o teste de 50 pedidos simultâneos na
// mesma unidade reprovava 3 vezes em 20. O perdedor esgotava as tentativas
// ainda esperando lock e a resposta saía da tradução do `55P03`, não de um
// `23P01` de verdade — ou seja, o hóspede ouvia "estas datas acabaram de ser
// ocupadas" por um tempo de espera, não por conflito comprovado. Com 12, foram
// 30 execuções verdes seguidas, e o tempo TOTAL das 30 caiu para ~7,5 s (antes,
// cada execução isolada levava ~7 s, justamente por esgotar o orçamento
// esperando). Repetir mais cedo é mais barato do que esperar mais tempo.
const (
	maxTentativas     = 12
	esperaBase        = 5 * time.Millisecond
	esperaMaxima      = 160 * time.Millisecond
	idleInTxTimeout   = "30s"
	tempoDePingNoBoot = 5 * time.Second
)

// DBTX é o mínimo que um repositório precisa. *pgxpool.Pool e pgx.Tx satisfazem
// os dois, e é isso que permite trocar um pelo outro pelo contexto.
type DBTX interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type chaveTx struct{}

// New abre o pool e só devolve depois de provar que o banco responde: falhar no
// boot é barato, falhar na primeira requisição do usuário não é.
func New(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL inválida: %w", err)
	}

	cfg.MaxConns = 20
	cfg.MinConns = 2
	// Reciclar conexão evita acumular estado de sessão e memória no backend do
	// Postgres em processo de vida longa.
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 15 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second

	// Vão no pacote de startup: valem para a sessão inteira, sem custar um
	// round-trip de `SET` a cada conexão nova e sem depender de AfterConnect.
	// Se a DATABASE_URL já trouxer o parâmetro, o operador manda — não
	// sobrescrevemos configuração explícita de ambiente.
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	definirPadrao(cfg.ConnConfig.RuntimeParams, "lock_timeout", lockTimeout)
	definirPadrao(cfg.ConnConfig.RuntimeParams, "statement_timeout", statementTimeout)
	// Transação esquecida aberta segura lock e trava a fila de todo mundo.
	definirPadrao(cfg.ConnConfig.RuntimeParams, "idle_in_transaction_session_timeout", idleInTxTimeout)

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("abrindo pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, tempoDePingNoBoot)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("banco não respondeu: %w", err)
	}
	return pool, nil
}

func definirPadrao(params map[string]string, chave, valor string) {
	if _, jaVeio := params[chave]; !jaVeio {
		params[chave] = valor
	}
}

// From devolve o executor a usar: a transação do contexto, quando existe, ou o
// pool. É a única forma que o repositório tem de obter um executor.
func From(ctx context.Context, pool DBTX) DBTX {
	if tx, ok := ctx.Value(chaveTx{}).(pgx.Tx); ok && tx != nil {
		return tx
	}
	return pool
}

// EmTransacao diz se já existe transação aberta no contexto. Serve para o
// service evitar aninhar Do sem perceber.
func EmTransacao(ctx context.Context) bool {
	tx, ok := ctx.Value(chaveTx{}).(pgx.Tx)
	return ok && tx != nil
}

// TxManager abre a transação e injeta no contexto.
type TxManager struct {
	pool *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) *TxManager { return &TxManager{pool: pool} }

// Do executa fn dentro de uma transação. Commit se fn devolver nil, rollback em
// qualquer outro caso — inclusive em panic, que é repropagado depois de
// desfazer o trabalho.
//
// # Repetição
//
// 40001 (falha de serialização), 40P01 (impasse) e 55P03 (lock_timeout) são
// estados TRANSIENTES: o banco está dizendo "não consegui decidir agora", não
// "seu pedido é inválido". Repetir é o que transforma isso na resposta
// definitiva — assim que a transação vencedora comita, a tentativa seguinte lê
// a linha comitada e recebe 23P01 na hora, sem espera nenhuma.
//
// Duas coisas que a versão anterior não tinha e sem as quais a repetição não
// funciona sob disputa real:
//
//   - orçamento maior que uma tentativa. Com 50 pedidos na mesma data, a
//     segunda tentativa ainda encontra dezenas de transações em voo;
//   - espera com jitter ANTES de voltar. Sem ela as N goroutines que colidiram
//     voltam juntas e colidem de novo, em lockstep — foi exatamente isso que
//     mediu 49 de 49 perdedores recebendo 500.
//
// 23P01 NUNCA faz retry (regra 2 do CLAUDE.md): repetir a mesma inserção dá o
// mesmo conflito, e o cliente precisa saber que a data foi ocupada.
//
// Esgotado o orçamento, o erro sai por MapError — que traduz contenção para
// 409 DATE_CONFLICT. Disputa de data jamais vira 500, com ou sem orçamento
// sobrando.
func (m *TxManager) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return m.repetir(ctx, pgx.TxOptions{}, fn)
}

// opcoesDaFotografia são as da transação de Fotografia: um snapshot só, e
// nenhuma escrita.
var opcoesDaFotografia = pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}

// Fotografia executa fn numa transação REPEATABLE READ e READ ONLY: todas as
// leituras de fn enxergam o banco no MESMO instante, e nenhuma delas escreve.
//
// Existe para a tela montada com várias consultas. Em READ COMMITTED — o padrão
// do pool e de Do — cada instrução enxerga o que estiver comitado no instante
// DELA, e uma escrita que comita entre duas leituras da mesma tela mistura dois
// estados na mesma resposta. Medido pelo QA da rooming list (13/10/2026): com
// 40 trocas de titular e 120 aberturas do `/full` em paralelo, de 2 a 9
// leituras saíam com o cabeçalho dizendo "Ana" e a lista de hóspedes marcando
// "Beto" como titular — a troca tinha comitado entre a leitura da reserva e a
// da lista. Em REPEATABLE READ o snapshot é tirado na primeira instrução da
// transação e vale até o fim dela: a tela inteira é de antes da troca, ou
// inteira de depois.
//
// READ ONLY porque fotografia não escreve: o banco recusa (25006) qualquer
// INSERT, UPDATE, DELETE ou `SELECT ... FOR UPDATE` que entre por engano numa
// leitura. A consequência para quem chama: o que a leitura precisa GRAVAR — o
// registro de acesso a dado pessoal (`pii.Registrar`) — vai DEPOIS, fora dela,
// e antes de a resposta sair (ver o `/full` em internal/modules/reservas). E
// uma transação só de leitura em REPEATABLE READ nunca é abortada por conflito
// de serialização: a escrita concorrente não espera por ela, e ela não espera
// pela escrita.
//
// A repetição é a mesma de Do — só alcança a espera por trava de tabela (DDL),
// já que leitura não trava linha.
//
// Dentro de uma transação que já existe, fn roda nela, como em Do: o
// isolamento é o de quem a abriu, e um snapshot não se troca no meio.
func (m *TxManager) Fotografia(ctx context.Context, fn func(ctx context.Context) error) error {
	return m.repetir(ctx, opcoesDaFotografia, fn)
}

// repetir é o laço de Do e de Fotografia: abre a transação com `opcoes`, roda
// fn e repete o que for contenção transiente, até o orçamento.
func (m *TxManager) repetir(ctx context.Context, opcoes pgx.TxOptions, fn func(ctx context.Context) error) error {
	if EmTransacao(ctx) {
		// Transação aninhada reusa a de fora: commit interno não pode publicar
		// metade do trabalho do service que chamou.
		return fn(ctx)
	}

	var err error
	for tentativa := 1; ; tentativa++ {
		err = m.executar(ctx, opcoes, fn)
		if err == nil || !ehTransiente(err) {
			return err
		}
		if tentativa >= maxTentativas || !esperarAntesDeRepetir(ctx, tentativa) {
			break
		}
	}
	return MapError(err)
}

// esperarAntesDeRepetir dorme um tempo curto e aleatório antes da próxima
// tentativa. Devolve false quando o contexto morreu no meio da espera — aí não
// há próxima tentativa, e quem chama devolve o erro do BANCO, não o do relógio:
// o hóspede precisa saber que a data foi disputada, não que "o contexto
// expirou".
func esperarAntesDeRepetir(ctx context.Context, tentativa int) bool {
	teto := esperaBase << (tentativa - 1)
	if teto > esperaMaxima {
		teto = esperaMaxima
	}
	// Metade fixa + metade sorteada: garante progresso e desalinha quem colidiu.
	espera := teto/2 + rand.N(teto/2+1)

	temporizador := time.NewTimer(espera)
	defer temporizador.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-temporizador.C:
		return true
	}
}

func (m *TxManager) executar(ctx context.Context, opcoes pgx.TxOptions, fn func(ctx context.Context) error) (err error) {
	// `pgx.TxOptions{}` é o BEGIN seco, o mesmo de `pool.Begin`: o isolamento e
	// o modo de acesso padrão do servidor.
	tx, err := m.pool.BeginTx(ctx, opcoes)
	if err != nil {
		return MapError(err)
	}

	defer func() {
		if p := recover(); p != nil {
			desfazer(ctx, tx)
			panic(p)
		}
		if err != nil {
			desfazer(ctx, tx)
		}
	}()

	if err = fn(context.WithValue(ctx, chaveTx{}, tx)); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return MapError(err)
	}
	return nil
}

// desfazer roda o rollback e engole o erro dele de propósito.
//
// O erro que interessa a quem chamou é o que ABORTOU a transação — 23P01,
// 40P01, o que for. O rollback de uma transação já abortada pelo servidor
// devolve rotineiramente "tx is closed" ou o próprio erro anterior; deixar isso
// substituir o original faria a perdedora da corrida receber 500 no lugar do
// 409, que é uma das formas de o defeito voltar.
//
// O contexto vai sem cancelamento porque ele pode já estar morto (cliente
// desconectou, deadline estourou) e ainda assim o rollback precisa chegar ao
// banco — senão a conexão volta ao pool com transação aberta segurando lock.
func desfazer(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(context.WithoutCancel(ctx))
}
