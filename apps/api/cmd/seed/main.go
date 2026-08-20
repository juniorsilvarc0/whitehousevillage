// Comando seed — popula o banco com o estado inicial da White House Village.
//
// É idempotente por contrato: rodar dez vezes tem o mesmo efeito de rodar uma.
// Popula propriedade, as 8 unidades físicas, os 4 produtos com a composição da
// Completa, o calendário comercial de 2026–2027, a Tabela Comercial V1, as
// políticas, o funil padrão, o catálogo de recursos e os 3 perfis.
package main

import (
	"log/slog"
	"os"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/config"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuração inválida", "err", err)
		os.Exit(1)
	}
	slog.Info("seed pendente de implementação", "env", cfg.Env)

	// TODO(db-migrations): implementar o seed idempotente.
	// Referência dos dados: docs/spec.md §2 (inventário) e §3 (tarifário).
}
