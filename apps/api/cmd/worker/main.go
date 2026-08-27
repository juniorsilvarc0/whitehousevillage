// Comando worker — jobs de fundo do White House Village Manager.
//
// Fase 1 entrega UM job, o que a spec §5 exige: expirar a pré-reserva vencida,
// liberar as unidades e registrar o fato. Sem ele o `hold_expires_at` seria
// decoração — a data ficaria presa para sempre num hold que ninguém confirmou, e
// o mapa mostraria a casa cheia com zero reserva paga.
//
// Alertas de saldo a vencer, reprocessamento de webhooks e o pull de iCal
// (Fase 4) entram depois, cada um como um Loop ao lado deste.
//
// Todo job trava com pg_try_advisory_lock antes de rodar, para duas réplicas do
// worker nunca executarem a mesma apuração duas vezes.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/reservas"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/config"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

func main() {
	if err := executar(); err != nil {
		slog.Error("worker não subiu", "err", err)
		os.Exit(1)
	}
}

func executar() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: nivelDeLog(cfg.LogLevel)})))

	// O worker é encerrado pelo sinal, e é o cancelamento deste contexto que
	// interrompe o job no meio de uma espera — sem ele, o SIGTERM esperaria até
	// um minuto pelo próximo tique.
	ctx, encerrar := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer encerrar()

	// Boot: o pool só volta depois de o banco responder. Falhar aqui é o
	// orquestrador reiniciando o container; falhar no primeiro job é uma data
	// que não libera e ninguém percebe.
	ctxBoot, cancelBoot := context.WithTimeout(ctx, 15*time.Second)
	defer cancelBoot()

	pool, err := db.New(ctxBoot, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	slog.Info("worker iniciado", "env", cfg.Env, "tz", cfg.Timezone,
		"job", "holds.expire", "intervalo", reservas.IntervaloPadraoDeExpiracao.String())

	var jobs sync.WaitGroup
	jobs.Add(1)
	go func() {
		defer jobs.Done()
		reservas.NovoExpirador(pool, db.NewTxManager(pool)).
			Loop(ctx, reservas.IntervaloPadraoDeExpiracao)
	}()

	<-ctx.Done()
	// Espera o job em curso terminar antes de fechar o pool: cortar no meio
	// deixaria a trava de sessão presa até o Postgres derrubar a conexão, e a
	// réplica seguinte acharia que outra instância ainda está trabalhando.
	jobs.Wait()

	slog.Info("worker encerrado")
	return nil
}

func nivelDeLog(nome string) slog.Level {
	var nivel slog.Level
	if err := nivel.UnmarshalText([]byte(nome)); err != nil {
		return slog.LevelInfo
	}
	return nivel
}
