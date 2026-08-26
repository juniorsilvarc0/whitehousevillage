package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// contagem é o que cada etapa reporta no log.
//
// `Previstas` é quantas linhas a etapa QUER ver no banco; a diferença para
// criadas+atualizadas é o que já estava correto. Numa segunda execução criadas
// e atualizadas ficam em zero e tudo cai em `inalteradas` — é essa a prova de
// idempotência que o seed imprime, sem precisar de ninguém contando tabela.
type contagem struct {
	Previstas   int
	Criadas     int
	Atualizadas int
}

func (c contagem) inalteradas() int { return c.Previstas - c.Criadas - c.Atualizadas }

func (c *contagem) somar(o contagem) {
	c.Previstas += o.Previstas
	c.Criadas += o.Criadas
	c.Atualizadas += o.Atualizadas
}

// upsert executa um INSERT ... ON CONFLICT cuja cláusula RETURNING é
// exatamente `(xmax = 0)`, e separa o que nasceu do que foi atualizado.
//
// `xmax = 0` só é verdadeiro na tupla que ESTA transação acabou de inserir: no
// caminho do DO UPDATE o Postgres deixa em xmax o id da transação que travou a
// versão anterior da linha. É o único jeito de distinguir criação de
// atualização sem uma segunda consulta.
//
// Todo INSERT do seed leva no DO UPDATE uma guarda
// `WHERE (colunas) IS DISTINCT FROM (EXCLUDED.colunas)`: linha que já está
// correta não é reescrita, não sobe `updated_at` e não volta no RETURNING.
func upsert(ctx context.Context, tx pgx.Tx, sql string, args ...any) (contagem, error) {
	var c contagem

	linhas, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return c, err
	}
	defer linhas.Close()

	for linhas.Next() {
		var criada bool
		if err := linhas.Scan(&criada); err != nil {
			return c, err
		}
		if criada {
			c.Criadas++
		} else {
			c.Atualizadas++
		}
	}
	return c, linhas.Err()
}

// coluna extrai um campo de cada item para o array que vira `unnest` no SQL.
//
// Escrever os dados como slice de struct e derivar os arrays daqui garante, por
// construção, que todos tenham o mesmo comprimento: `unnest` de arrays
// desiguais completa o mais curto com NULL, em silêncio.
func coluna[T, R any](itens []T, f func(T) R) []R {
	out := make([]R, len(itens))
	for i, it := range itens {
		out[i] = f(it)
	}
	return out
}

// data converte um literal "AAAA-MM-DD" escrito neste pacote.
//
// Pânico é o comportamento certo: a string é constante do próprio arquivo,
// então erro só pode ser digitação — e precisa estourar na primeira execução,
// não virar data errada no banco.
func data(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(fmt.Sprintf("seed: data inválida %q: %v", s, err))
	}
	return t
}

// reais converte o valor comercial (que a spec escreve em reais) para os
// centavos que o banco guarda. Existe para a conversão acontecer num lugar só:
// gravar 850 num campo `_cents` seria uma diária de R$ 8,50.
func reais(v int64) int64 { return v * 100 }
