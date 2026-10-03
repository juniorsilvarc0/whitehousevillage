// Comando seed — popula o banco com o estado inicial da White House Village.
//
// É idempotente por contrato: rodar dez vezes tem o mesmo efeito de rodar uma.
// Toda escrita é um INSERT ... ON CONFLICT sobre a chave NATURAL da tabela
// (`slug`, `code`, `(property_id, code)`, `(rate_table_id, unit_type_id,
// date_type)`…), nunca sobre id gerado — id novo a cada execução é exatamente o
// que duplicaria tudo na segunda rodada.
//
// Tudo corre numa transação única: ou o estado inicial inteiro entra, ou o
// banco fica como estava. Meio seed aplicado é pior que nenhum, porque a
// execução seguinte parte de um banco que ninguém sabe descrever.
//
// Dois catálogos, escolhidos por SEED_CATALOGO (`real`, o padrão, ou `teste`,
// o da suíte de integração) — ver catalogo.go.
//
// Fonte dos dados: catalogo_real.go (definido pelo dono), docs/spec.md §2
// (inventário de teste), §3 (tarifário e políticas) e §1/§7 (perfis). Nada aqui é inventado; o que precisou de decisão está
// comentado no ponto onde a decisão foi tomada.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/config"
)

// estado carrega o que uma etapa descobre e as seguintes precisam. Os ids são
// `pgtype.UUID` — e não string — porque pgx não sabe codificar string num
// parâmetro `uuid`, e converter com `$1::text::uuid` sujaria todas as consultas.
type estado struct {
	propriedadeID pgtype.UUID
	tarifarioID   pgtype.UUID
	funilID       pgtype.UUID
	producao      bool

	// cat é o catálogo escolhido por SEED_CATALOGO; outro é o que fica de
	// fora, e cujos itens ausentes de `cat` são desativados.
	cat   *catalogo
	outro *catalogo
}

type etapa struct {
	nome  string
	rodar func(context.Context, pgx.Tx, *estado) (contagem, error)
}

// etapas é a ordem de aplicação, e a ordem importa: cada bloco resolve por
// chave natural o que o anterior gravou (a composição dos produtos faz JOIN nos
// códigos das unidades, as tarifas fazem JOIN no código do produto).
func etapas() []etapa {
	return []etapa{
		{"propriedade", propriedade},
		{"unidades", unidades},
		{"produtos", produtos},
		{"composicao_dos_produtos", composicaoDosProdutos},
		{"tipos_de_data", tiposDeData},
		{"feriados", feriados},
		{"periodos_especiais", periodosEspeciais},
		{"tarifario", tarifario},
		{"tarifas", tarifas},
		{"estadia_minima", estadiaMinima},
		{"estadia_minima_por_produto", estadiaMinimaPorProduto},
		{"pacotes", pacotes},
		{"politica_comercial", politicaComercial},
		{"politica_de_cancelamento", politicaDeCancelamento},
		{"contatos_de_demonstracao", contatosDeDemonstracao},
		{"catalogo_de_recursos", catalogoDeRecursos},
		{"perfis", perfis},
		{"permissoes", permissoes},
		{"perfil_da_vitrine", perfilDaVitrine},
		{"conta_da_vitrine", contaDaVitrine},
		{"usuarios_de_desenvolvimento", usuariosDeDesenvolvimento},
		{"corretores_de_desenvolvimento", corretoresDeDesenvolvimento},
		{"funil_padrao", funilPadrao},
		{"etapas_do_funil", etapasDoFunil},
		{"motivos_de_perda", motivosDePerda},
	}
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("seed falhou", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuração inválida: %w", err)
	}

	// Ctrl-C durante o seed cancela o contexto, a transação morre e o banco
	// volta ao estado anterior — o oposto de deixar metade aplicada.
	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer parar()
	ctx, cancelar := context.WithTimeout(ctx, 3*time.Minute)
	defer cancelar()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("abrindo o pool: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("banco inacessível: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("abrindo transação: %w", err)
	}
	// Rollback depois de um commit bem-sucedido é no-op; o defer aqui garante
	// que qualquer saída pelo meio do caminho não deixe seed pela metade.
	// `WithoutCancel` porque o contexto já pode estar cancelado justamente no
	// caso em que o rollback é indispensável.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	cat, outro, err := escolherCatalogo()
	if err != nil {
		return err
	}
	slog.Info("catálogo escolhido", "catalogo", cat.nome, "desativa_o_de", outro.nome)

	st := &estado{producao: cfg.IsProduction(), cat: cat, outro: outro}
	var total contagem

	for _, e := range etapas() {
		inicio := time.Now()

		c, err := e.rodar(ctx, tx, st)
		if err != nil {
			return fmt.Errorf("etapa %s: %w", e.nome, err)
		}
		total.somar(c)

		slog.Info("etapa concluída",
			"etapa", e.nome,
			"previstas", c.Previstas,
			"criadas", c.Criadas,
			"atualizadas", c.Atualizadas,
			"inalteradas", c.inalteradas(),
			"desativadas", c.Desativadas,
			"removidas", c.Removidas,
			"ms", time.Since(inicio).Milliseconds(),
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("confirmando transação: %w", err)
	}

	slog.Info("seed concluído",
		"catalogo", cat.nome,
		"previstas", total.Previstas,
		"criadas", total.Criadas,
		"atualizadas", total.Atualizadas,
		"inalteradas", total.inalteradas(),
		"desativadas", total.Desativadas,
		"removidas", total.Removidas,
	)
	if !total.mudou() {
		slog.Info("nada mudou — o banco já estava no estado do seed")
	}
	return nil
}
