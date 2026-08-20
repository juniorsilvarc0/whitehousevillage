// Comando api — servidor HTTP do White House Village Manager.
//
// O servidor NÃO aplica migrations: isso é do cmd/migrate, num passo explícito.
// /readyz confere a versão do schema e recusa servir com o banco defasado.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/config"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuração inválida", "err", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		// TODO(backend-go): ping no pool + comparação da versão da migration.
		httpx.JSON(w, http.StatusOK, map[string]any{"status": "ok", "database": "pendente", "schema": "pendente"})
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		slog.Info("api ouvindo", "port", cfg.Port, "env", cfg.Env, "tz", cfg.Timezone)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("servidor caiu", "err", err)
			os.Exit(1)
		}
	}()

	// Encerramento gracioso: para de aceitar conexão nova e deixa as em curso terminarem.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("encerramento forçado", "err", err)
	}
	slog.Info("api encerrada")
}
