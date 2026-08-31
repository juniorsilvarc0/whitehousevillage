// Comando api — servidor HTTP do White House Village Manager.
//
// O servidor NÃO aplica migrations: isso é do cmd/migrate, num passo explícito.
// /readyz confere a versão do schema e recusa servir com o banco defasado.
package main

import (
	"context"
	"errors"
	"fmt"
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
	// `-healthcheck` roda ANTES de qualquer coisa: sonda o próprio processo por
	// HTTP e sai 0/1, sem abrir pool, sem ler config de banco e sem tentar
	// escutar porta.
	//
	// Existe porque a imagem é distroless — não há shell nem curl lá dentro, e
	// o healthcheck do Compose só pode invocar o próprio binário. Enquanto a
	// flag não existiu, `["CMD", "/app/api", "-healthcheck"]` caía no caminho
	// normal: o binário SUBIA UMA SEGUNDA API a cada 10 s, ela batia em
	// `address already in use` e saía 1. Resultado medido nesta árvore: o
	// container ficou `unhealthy` desde o primeiro segundo, com 272 falhas
	// seguidas, e cada sonda ainda abria uma conexão de banco antes de morrer.
	// Um `depends_on: service_healthy` apontado para a API nunca teria liberado
	// nada.
	if len(os.Args) > 1 && (os.Args[1] == "-healthcheck" || os.Args[1] == "--healthcheck") {
		if err := sondar(); err != nil {
			// Sem logger estruturado de propósito: quem lê isto é o
			// `docker inspect`, e ele mostra a saída crua.
			os.Stderr.WriteString("healthcheck: " + err.Error() + "\n")
			os.Exit(1)
		}
		os.Exit(0)
	}

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

// sondar bate no /healthz do processo local e devolve erro se ele não estiver
// servindo. É a sonda de VIDA, não de prontidão: um `/readyz` aqui marcaria o
// container como doente sempre que o banco piscasse, e a resposta do
// orquestrador a "doente" é derrubar quem estava de pé — trocar uma
// indisponibilidade de segundos por uma de minutos. Quem tira a instância do
// balanceamento com o banco defasado continua sendo o /readyz, lido pelo
// proxy da frente.
func sondar() error {
	porta := os.Getenv("API_PORT")
	if porta == "" {
		porta = "8080"
	}

	// 127.0.0.1 e não localhost: resolver nome dentro de distroless depende de
	// um /etc/nsswitch.conf que a imagem não tem.
	alvo := "http://127.0.0.1:" + porta + "/healthz"

	ctx, cancelar := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelar()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, alvo, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s respondeu %d", alvo, resp.StatusCode)
	}
	return nil
}
