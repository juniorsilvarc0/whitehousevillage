// Comando worker — jobs de fundo do White House Village Manager.
//
// Fase 1: expirar pré-reserva vencida, alertar saldo a vencer e reprocessar o
// outbox de webhooks. Fase 4 acrescenta o pull dos calendários iCal.
//
// Todo job trava com pg_try_advisory_lock antes de rodar, para duas réplicas do
// worker nunca executarem a mesma apuração duas vezes.
package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/config"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuração inválida", "err", err)
		os.Exit(1)
	}
	slog.Info("worker iniciado", "env", cfg.Env, "tz", cfg.Timezone)

	// TODO(backend-go): registrar os jobs no River —
	//   holds.expire (1 min) · reminders.balance (diário) · webhooks.dispatch (fila)
	//   crm.sla (horário) · finance.overdue (diário) · bi.refresh (horário)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	slog.Info("worker encerrado")
}
