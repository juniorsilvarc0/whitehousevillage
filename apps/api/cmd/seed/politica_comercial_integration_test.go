//go:build integration

package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Guardas da dívida D7 do lado do SEED.
//
// A D7 nasceu de um INSERT que lista as colunas nome a nome: coluna nova que não
// entra na lista volta ao DEFAULT em silêncio a cada gravação. O
// `PublicarPoliticaComercial` do tarifário tem o mesmo formato e é consertado no
// F2-05; aqui fica a metade que mora nesta pasta, porque o seed reescreve a v1 a
// cada `make seed` e o esquecimento seria igualmente mudo.
//
// Os dois testes abaixo pegam metades diferentes: o primeiro, a lista do INSERT;
// o segundo, as listas do `DO UPDATE SET` e da guarda `IS DISTINCT FROM`.

func abrirPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL ausente: teste de integração pulado (use `make test-integration`)")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("abrindo o pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// colunasGeradasPeloBanco são as únicas que o seed tem o direito de omitir:
// `id` sai de `gen_random_uuid()` e `created_at` de `now()`. Qualquer outra
// coluna de `commercial_policies` é decisão comercial, e decisão comercial que o
// seed não escreve é decisão que ninguém tomou.
var colunasGeradasPeloBanco = map[string]bool{"id": true, "created_at": true}

func TestSeedDaPoliticaComercialEscreveTodaColunaDaTabela(t *testing.T) {
	pool := abrirPool(t)
	ctx := context.Background()

	linhas, err := pool.Query(ctx, `
		SELECT column_name
		  FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = 'commercial_policies'
		 ORDER BY ordinal_position`)
	if err != nil {
		t.Fatalf("lendo information_schema: %v", err)
	}
	defer linhas.Close()

	// A lista de colunas do INSERT é o trecho entre os parênteses que seguem o
	// nome da tabela. Comparar contra o SQL inteiro casaria também com o
	// `DO UPDATE SET`, e aí uma coluna presente só lá passaria por presente aqui.
	const marca = "INSERT INTO commercial_policies ("
	i := strings.Index(sqlPoliticaComercialV1, marca)
	if i < 0 {
		t.Fatal("o INSERT do seed mudou de forma: o teste não encontrou a lista de colunas")
	}
	resto := sqlPoliticaComercialV1[i+len(marca):]
	fim := strings.Index(resto, ")")
	if fim < 0 {
		t.Fatal("lista de colunas do INSERT sem parêntese de fechamento")
	}
	listadas := map[string]bool{}
	for _, c := range strings.Split(resto[:fim], ",") {
		listadas[strings.TrimSpace(c)] = true
	}

	var conferidas int
	for linhas.Next() {
		var coluna string
		if err := linhas.Scan(&coluna); err != nil {
			t.Fatalf("lendo nome de coluna: %v", err)
		}
		if colunasGeradasPeloBanco[coluna] {
			continue
		}
		conferidas++
		if !listadas[coluna] {
			t.Errorf("`commercial_policies.%s` existe no banco e NÃO está no INSERT do seed.\n"+
				"Efeito: `make seed` grava a v1 com o DEFAULT desta coluna, em silêncio — "+
				"a regra comercial que ela carrega nunca chega ao banco e ninguém vê erro. "+
				"É a dívida D7 se repetindo; some a coluna às três listas de `sqlPoliticaComercialV1`.", coluna)
		}
	}
	if err := linhas.Err(); err != nil {
		t.Fatalf("varrendo information_schema: %v", err)
	}
	if conferidas == 0 {
		t.Fatal("nenhuma coluna conferida — a tabela sumiu ou o filtro do teste ficou defasado; " +
			"teste que não confere nada passa verde por engano")
	}
}

// Este é o teste que fica vermelho se `quote_validity_days` sair do
// `DO UPDATE SET` ou da guarda `IS DISTINCT FROM`: ele sujeita a v1 semeada a um
// valor divergente e exige que a rodada seguinte do seed a traga de volta.
//
// Tudo numa transação com ROLLBACK: o banco de integração é compartilhado pelos
// outros pacotes (`-p 1` serializa, mas não isola), e deixar a política v1 com
// validade 15 estragaria quem lê a política depois.
func TestSeedRepoePorCimaAValidadeDoOrcamentoDivergente(t *testing.T) {
	pool := abrirPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("abrindo transação: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	st := &estado{}
	if _, err := propriedade(ctx, tx, st); err != nil {
		t.Fatalf("resolvendo a propriedade do seed: %v", err)
	}

	const divergente = 15
	etiqueta, err := tx.Exec(ctx, `
		UPDATE commercial_policies
		   SET quote_validity_days = $2
		 WHERE property_id = $1 AND version = 1`, st.propriedadeID, divergente)
	if err != nil {
		t.Fatalf("desviando a validade da v1: %v", err)
	}
	if etiqueta.RowsAffected() != 1 {
		t.Fatalf("v1 da política comercial não encontrada (%d linhas) — o seed rodou antes da suíte?",
			etiqueta.RowsAffected())
	}

	if _, err := politicaComercial(ctx, tx, st); err != nil {
		t.Fatalf("reexecutando a etapa do seed: %v", err)
	}

	var gravado int
	if err := tx.QueryRow(ctx, `
		SELECT quote_validity_days
		  FROM commercial_policies
		 WHERE property_id = $1 AND version = 1`, st.propriedadeID).Scan(&gravado); err != nil {
		t.Fatalf("relendo a validade: %v", err)
	}
	if gravado != validadeDeOrcamentoDias {
		t.Fatalf("quote_validity_days = %d depois do seed, esperado %d.\n"+
			"O seed não reescreve esta coluna: ou ela saiu do `DO UPDATE SET`, ou saiu da guarda "+
			"`IS DISTINCT FROM` (e aí a linha divergente é dada como já correta). Nos dois casos a "+
			"validade do orçamento no banco deixa de ser a que este pacote declara.",
			gravado, validadeDeOrcamentoDias)
	}
}
