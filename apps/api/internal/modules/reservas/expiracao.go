package reservas

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// O job que faz a pré-reserva expirar de verdade.
//
// spec §5: "`hold` grava `expires_at = now() + política`. Um job expira
// automaticamente a cada minuto, libera as unidades e registra uma atividade".
//
// Sem ele o `hold_expires_at` é decoração: a data ficaria presa para sempre num
// hold que ninguém confirmou, e o mapa de ocupação mentiria — a casa apareceria
// cheia com zero reserva paga. É o job que devolve a data ao estoque.

// ChaveDaTravaDeExpiracao é a chave fixa do advisory lock que serializa o job
// entre réplicas do worker. Segue o formato de `users.ChaveDaTravaDeAdministradores`:
// data da decisão + sequência.
//
// POR QUE UM LOCK E NÃO SÓ O `FOR UPDATE SKIP LOCKED`: o SKIP LOCKED já impede
// duas réplicas de expirarem a MESMA reserva, mas as duas ainda varreriam a
// tabela e emitiriam log a cada minuto. O advisory lock deixa uma só trabalhar;
// a outra devolve na hora, sem esperar, e tenta de novo no minuto seguinte.
const ChaveDaTravaDeExpiracao int64 = 2026_0826_0004

// tamanhoDoLoteDeExpiracao limita cada transação. Expirar dez mil holds numa
// transação só seguraria os locks das unidades por segundos e travaria as vendas
// em curso — o lote curto devolve a data em pedaços e libera a fila entre eles.
const tamanhoDoLoteDeExpiracao = 100

// IntervaloPadraoDeExpiracao é o "a cada minuto" da spec §5.
const IntervaloPadraoDeExpiracao = time.Minute

// Expirador roda a varredura. Ele é do módulo, e não do cmd/worker, porque
// expirar reserva é regra deste domínio: o worker só decide QUANDO chamar.
type Expirador struct {
	pool *pgxpool.Pool
	tx   *db.TxManager
	repo *Repository
}

func NovoExpirador(pool *pgxpool.Pool, tx *db.TxManager) *Expirador {
	return &Expirador{pool: pool, tx: tx, repo: NewRepository(pool)}
}

// Rodar expira o que venceu e devolve quantas reservas foram encerradas.
//
// Devolve (0, nil) quando outra réplica está com a trava: não é erro, é a
// resposta correta de "alguém já está fazendo isso".
func (e *Expirador) Rodar(ctx context.Context) (int, error) {
	conexao, err := e.pool.Acquire(ctx)
	if err != nil {
		return 0, db.MapError(err)
	}
	defer conexao.Release()

	// Lock de SESSÃO, não de transação: ele precisa valer durante os vários
	// lotes, que são transações separadas de propósito.
	var peguei bool
	if err := conexao.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, ChaveDaTravaDeExpiracao).Scan(&peguei); err != nil {
		return 0, db.MapError(err)
	}
	if !peguei {
		slog.DebugContext(ctx, "expiração de holds já está rodando em outra réplica")
		return 0, nil
	}
	defer func() {
		// Sem cancelamento: o contexto pode já estar morto (SIGTERM), e o
		// unlock ainda precisa chegar ao banco — senão a trava só cai quando a
		// conexão morrer, e a próxima rodada acha que outra réplica está viva.
		if _, err := conexao.Exec(context.WithoutCancel(ctx),
			`SELECT pg_advisory_unlock($1)`, ChaveDaTravaDeExpiracao); err != nil {
			slog.WarnContext(ctx, "não foi possível soltar a trava da expiração", "err", err)
		}
	}()

	total := 0
	for {
		lote, err := e.rodarLote(ctx)
		if err != nil {
			return total, err
		}
		total += len(lote)
		if len(lote) < tamanhoDoLoteDeExpiracao {
			return total, nil
		}
	}
}

// rodarLote expira um lote numa ÚNICA transação.
//
// É aqui que mora a garantia da White House Completa: as oito linhas de
// `stay_blocks` vão para `expired` junto com a reserva, na mesma transação. Se
// fossem transações separadas, uma falha no meio deixaria a casa meio-livre —
// três unidades vendáveis e cinco presas num hold que já não existe.
func (e *Expirador) rodarLote(ctx context.Context) ([]Expirada, error) {
	var lote []Expirada
	err := e.tx.Do(ctx, func(ctx context.Context) error {
		var err error
		lote, err = e.repo.ExpirarLote(ctx, tamanhoDoLoteDeExpiracao)
		return err
	})
	if err != nil {
		return nil, err
	}
	for _, r := range lote {
		// O "evento emitido" da Fase 1 é o log estruturado mais a linha em
		// `reservation_events`. Não há tabela de outbox no schema desta fase —
		// publicar em fila é do módulo de webhooks (ver relatório).
		slog.InfoContext(ctx, "pré-reserva expirada",
			"reservation_id", r.ID, "code", r.Codigo, "released_blocks", r.Blocos)
	}
	return lote, nil
}

// Loop roda a varredura no intervalo pedido até o contexto morrer.
//
// A primeira execução é IMEDIATA: um worker que acabou de subir depois de uma
// queda pode ter holds vencidos há horas, e esperar o primeiro tique deixaria a
// casa falsamente cheia por mais um minuto.
func (e *Expirador) Loop(ctx context.Context, intervalo time.Duration) {
	if intervalo <= 0 {
		intervalo = IntervaloPadraoDeExpiracao
	}
	relogio := time.NewTicker(intervalo)
	defer relogio.Stop()

	for {
		if n, err := e.Rodar(ctx); err != nil {
			// Falha de uma rodada não derruba o worker: o banco pode ter
			// piscado, e no minuto seguinte a mesma varredura tenta de novo.
			if !errors.Is(err, context.Canceled) {
				slog.ErrorContext(ctx, "expiração de holds falhou", "err", err, "code", codigoDoErro(err))
			}
		} else if n > 0 {
			slog.InfoContext(ctx, "holds expirados", "total", n)
		}

		select {
		case <-ctx.Done():
			return
		case <-relogio.C:
		}
	}
}

func codigoDoErro(err error) string {
	var e *apperr.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
