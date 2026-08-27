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
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/router"
)

func main() {
	if err := executar(); err != nil {
		slog.Error("api não subiu", "err", err)
		os.Exit(1)
	}
}

func executar() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: nivelDeLog(cfg.LogLevel)})))

	// Boot: o pool só volta depois de o banco responder. Falhar aqui é o
	// orquestrador reiniciando o container; falhar na primeira requisição é o
	// usuário vendo 500.
	ctxBoot, cancelBoot := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelBoot()

	pool, err := db.New(ctxBoot, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Aviso no boot, não bloqueio: subir a API com o schema defasado é decisão
	// do deploy — o /readyz é que mantém a instância fora do balanceamento.
	if versao, sujo, err := db.SchemaVersion(ctxBoot, pool); err != nil {
		slog.Warn("não foi possível ler a versão do schema", "err", err)
	} else if sujo || versao < router.SchemaVersionEsperada {
		slog.Warn("schema fora da versão esperada; /readyz vai responder 503",
			"versao", versao, "esperada", router.SchemaVersionEsperada, "dirty", sujo)
	}

	// Os handlers dos quatro módulos da Fase 1 (inventário, tarifário,
	// disponibilidade e reservas) são montados dentro de router.New, junto com
	// os do núcleo: `Deps` e a tabela de rotas são internos ao pacote router, e
	// o único ponto de composição exportado é este construtor. Montar aqui
	// exigiria exportar a montagem do chi — mais superfície pública para nenhum
	// ganho, e um segundo lugar onde esquecer um módulo.
	handler, err := router.New(router.Opcoes{Config: cfg, Pool: pool})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	erroDoServidor := make(chan error, 1)
	go func() {
		slog.Info("api ouvindo", "port", cfg.Port, "env", cfg.Env, "tz", cfg.Timezone)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			erroDoServidor <- err
		}
	}()

	// Encerramento gracioso: para de aceitar conexão nova e deixa as em curso terminarem.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-erroDoServidor:
		return err
	case <-stop:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("encerramento forçado", "err", err)
	}
	slog.Info("api encerrada")
	return nil
}

func nivelDeLog(nome string) slog.Level {
	var nivel slog.Level
	if err := nivel.UnmarshalText([]byte(nome)); err != nil {
		return slog.LevelInfo
	}
	return nivel
}
