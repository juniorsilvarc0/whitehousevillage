// Comando migrate — aplica e reverte o schema do White House Village Manager.
//
// Este binário é o ÚNICO caminho para mudar schema e roda num passo EXPLÍCITO
// (`make migrate`, serviço `migrate` do compose, job do CI). A API nunca aplica
// migration no boot: migration silenciosa no startup esconde erro de schema,
// transforma deploy ruim em corrupção descoberta tarde e faz duas réplicas
// competirem pelo mesmo `ALTER TABLE`. Aqui a falha é barulhenta — exit code 1
// e mensagem clara — para o deploy parar antes de subir a aplicação.
//
// Uso:
//
//	migrate up               aplica tudo que falta
//	migrate down N           reverte as N últimas
//	migrate down -all        reverte até zero (usado no CI para provar o ciclo)
//	migrate version          imprime a versão aplicada e o estado dirty
//	migrate force V          marca a versão V como aplicada e limpa o dirty
//	migrate drop             apaga todos os objetos do schema
//
// Ambiente:
//
//	DATABASE_URL      obrigatória
//	MIGRATIONS_PATH   opcional (padrão ./migrations, com fallback /migrations)
//	APP_ENV           production faz o `drop` exigir --yes
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/file"
	"github.com/lib/pq"
)

const (
	// Caminho relativo quando se roda a partir de apps/api; o fallback é onde a
	// imagem da API copia as migrations (a distroless não tem o repositório).
	caminhoPadrao    = "./migrations"
	caminhoContainer = "/migrations"
)

func main() {
	// Log em stderr de propósito: stdout fica limpo para o valor de `version`,
	// que scripts e o healthcheck de schema leem.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	if err := run(os.Args[1:]); err != nil {
		slog.Error("migration falhou", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		imprimirUso()
		return errors.New("subcomando obrigatório")
	}

	dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dbURL == "" {
		return errors.New("DATABASE_URL não definida — configure o ambiente (.env) antes de migrar")
	}

	origem, err := origemMigrations()
	if err != nil {
		return err
	}

	m, err := abrir(origem, dbURL)
	if err != nil {
		return err
	}
	defer func() {
		// Close devolve dois erros (source e database) e ambos importam: um
		// lock não liberado trava a próxima migration.
		if errOrigem, errBanco := m.Close(); errOrigem != nil || errBanco != nil {
			slog.Warn("fechamento imperfeito", "origem", errOrigem, "banco", errBanco)
		}
	}()
	m.Log = logMigrate{}

	// SIGTERM no meio de um `up` (deploy cancelado, `docker stop`) para depois
	// da migration corrente, nunca no meio dela — é o que evita o dirty.
	ctx, pararEscuta := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer pararEscuta()
	go func() {
		<-ctx.Done()
		m.GracefulStop <- true
	}()

	switch args[0] {
	case "up":
		return aplicar(m.Up())
	case "down":
		return descer(m, args[1:])
	case "version":
		return versao(m)
	case "force":
		return forcar(m, args[1:])
	case "drop":
		return dropar(m, args[1:])
	case "help", "-h", "--help":
		imprimirUso()
		return nil
	default:
		imprimirUso()
		return fmt.Errorf("subcomando desconhecido: %q", args[0])
	}
}

// abrir monta o migrate com uma conexão que ENXERGA os avisos do servidor.
//
// `migrate.New(origem, url)` — que era o que estava aqui — abre a conexão por
// dentro da biblioteca, e essa conexão não instala tratador de notice. O efeito
// medido: `RAISE NOTICE` e `RAISE WARNING` de dentro de uma migration são
// ENGOLIDOS no cliente e só existem no log do servidor Postgres, onde ninguém
// que roda `make migrate` está olhando.
//
// Isso não é detalhe cosmético neste projeto. Duas migrations desta rodada
// usam WARNING como a ÚNICA saída de diagnóstico, de propósito, porque o estado
// legado que elas encontram não deve abortar o deploy nem ser reparado às
// escondidas: 20260827100000 (composição da casa inteira) e 20260827140000
// (`unit_types.consumes` trocado com venda viva) avisam "existe dado fora da
// invariante, decida o que fazer" — e o aviso chegava a ninguém. Migration que
// diagnostica em silêncio é migration que passou verde escondendo o problema
// que ela existe para mostrar.
//
// A montagem manual é o preço: o driver `postgres` do golang-migrate aceita um
// *sql.DB pronto (`WithInstance`), e o `lib/pq` — que é o driver que ele usa —
// expõe `ConnectorWithNoticeHandler`. Config vazia mantém EXATAMENTE os padrões
// de `migrate.New` (tabela `schema_migrations`, sem multi-statement).
func abrir(origem, dbURL string) (*migrate.Migrate, error) {
	conector, err := pq.NewConnector(dbURL)
	if err != nil {
		return nil, fmt.Errorf("conectando ao banco: %w", err)
	}

	comAvisos := pq.ConnectorWithNoticeHandler(conector, func(aviso *pq.Error) {
		if aviso == nil {
			return
		}
		// Nível do slog espelha o do Postgres: WARNING é o que a migration usa
		// para dizer "há dado legado fora da invariante" e precisa saltar aos
		// olhos; NOTICE é narrativa de progresso.
		registrar := slog.Info
		if strings.EqualFold(aviso.Severity, "WARNING") {
			registrar = slog.Warn
		}
		registrar("postgres: "+aviso.Message,
			"severidade", aviso.Severity, "detalhe", aviso.Detail, "dica", aviso.Hint)
	})

	banco := sql.OpenDB(comAvisos)

	driver, err := postgres.WithInstance(banco, &postgres.Config{})
	if err != nil {
		banco.Close()
		return nil, fmt.Errorf("abrindo o driver de migration: %w", err)
	}

	fonte, err := (&file.File{}).Open(origem)
	if err != nil {
		banco.Close()
		return nil, fmt.Errorf("abrindo migrations em %s: %w", origem, err)
	}

	m, err := migrate.NewWithInstance("file", fonte, "postgres", driver)
	if err != nil {
		banco.Close()
		return nil, fmt.Errorf("montando o migrate: %w", err)
	}
	return m, nil
}

// aplicar traduz o ErrNoChange — que não é erro — em sucesso silencioso.
func aplicar(err error) error {
	if errors.Is(err, migrate.ErrNoChange) {
		slog.Info("schema já está atualizado, nada a aplicar")
		return nil
	}
	if err != nil {
		return err
	}
	slog.Info("migrations aplicadas")
	return nil
}

func descer(m *migrate.Migrate, args []string) error {
	// `down` sem argumento é ambíguo: numa CLI de schema, ambiguidade custa
	// banco. Ou se diz quantos passos, ou se diz -all explicitamente.
	if len(args) == 0 {
		return errors.New("uso: migrate down N | migrate down -all")
	}
	if args[0] == "-all" || args[0] == "--all" {
		if err := m.Down(); err != nil {
			if errors.Is(err, migrate.ErrNoChange) {
				slog.Info("schema já está vazio")
				return nil
			}
			return err
		}
		slog.Info("todas as migrations revertidas")
		return nil
	}

	passos, err := strconv.Atoi(args[0])
	if err != nil || passos <= 0 {
		return fmt.Errorf("número de passos inválido: %q (esperado inteiro positivo ou -all)", args[0])
	}
	if err := m.Steps(-passos); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			slog.Info("nada a reverter")
			return nil
		}
		return err
	}
	slog.Info("migrations revertidas", "passos", passos)
	return nil
}

func versao(m *migrate.Migrate) error {
	v, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		fmt.Println("nil\tfalse")
		slog.Info("nenhuma migration aplicada")
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Printf("%d\t%t\n", v, dirty)
	slog.Info("versão do schema", "version", v, "dirty", dirty)
	if dirty {
		// Dirty significa migration interrompida no meio: o schema não é o que
		// o código espera. Sai 1 para o deploy travar e alguém olhar.
		return fmt.Errorf("schema sujo na versão %d — investigue e resolva com `migrate force %d`", v, v)
	}
	return nil
}

func forcar(m *migrate.Migrate, args []string) error {
	if len(args) == 0 {
		return errors.New("uso: migrate force V (use -1 para zerar a versão)")
	}
	v, err := strconv.Atoi(args[0])
	if err != nil || v < -1 {
		return fmt.Errorf("versão inválida: %q", args[0])
	}
	if err := m.Force(v); err != nil {
		return err
	}
	slog.Warn("versão forçada — confira o schema à mão, o force não roda SQL nenhum", "version", v)
	return nil
}

func dropar(m *migrate.Migrate, args []string) error {
	// drop apaga TODOS os objetos, inclusive o histórico de migrations. Em
	// produção só passa com confirmação explícita — errar o .env apontando para
	// o banco errado é o acidente mais barato de cometer aqui.
	if strings.EqualFold(os.Getenv("APP_ENV"), "production") && !contem(args, "--yes") {
		return errors.New("drop em APP_ENV=production exige --yes explícito")
	}
	if err := m.Drop(); err != nil {
		return err
	}
	slog.Warn("schema derrubado por completo")
	return nil
}

// origemMigrations resolve o diretório das migrations e devolve a URL file://
// já absoluta — caminho relativo depende do working dir e some no container.
func origemMigrations() (string, error) {
	caminho := strings.TrimSpace(os.Getenv("MIGRATIONS_PATH"))
	if caminho == "" {
		caminho = caminhoPadrao
		if _, err := os.Stat(caminho); err != nil {
			caminho = caminhoContainer
		}
	}

	abs, err := filepath.Abs(caminho)
	if err != nil {
		return "", fmt.Errorf("caminho de migrations inválido (%s): %w", caminho, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("migrations não encontradas em %s — defina MIGRATIONS_PATH: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s não é um diretório", abs)
	}
	return "file://" + filepath.ToSlash(abs), nil
}

func contem(args []string, alvo string) bool {
	for _, a := range args {
		if a == alvo {
			return true
		}
	}
	return false
}

// logMigrate liga o log interno da biblioteca ao slog, para a saída do
// container sair toda em JSON estruturado como a do resto do sistema.
type logMigrate struct{}

func (logMigrate) Printf(formato string, v ...any) {
	slog.Info(strings.TrimSpace(fmt.Sprintf(formato, v...)))
}

func (logMigrate) Verbose() bool { return false }

func imprimirUso() {
	fmt.Fprint(os.Stderr, `migrate — schema do White House Village Manager

  up             aplica todas as migrations pendentes
  down N         reverte as N últimas migrations
  down -all      reverte tudo até zero
  version        imprime "<versao>\t<dirty>" em stdout
  force V        marca a versão V como aplicada (limpa o dirty; não roda SQL)
  drop           apaga todos os objetos do schema

Ambiente: DATABASE_URL (obrigatória), MIGRATIONS_PATH (padrão ./migrations).
`)
}
