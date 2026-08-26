package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// A casa — docs/spec.md §2. O `property_id` acompanha todas as tabelas para não
// impedir uma segunda propriedade no futuro; hoje existe exatamente uma.
const (
	propriedadeNome     = "White House Village"
	propriedadeSlug     = "white-house-village"
	propriedadeFuso     = "America/Fortaleza"
	propriedadeEndereco = "Praia do Coqueiro"
	propriedadeCidade   = "Luís Correia"
	propriedadeUF       = "PI"
)

func propriedade(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	const q = `
		INSERT INTO properties (name, slug, timezone, address, city, state)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (slug) DO UPDATE
		   SET name     = EXCLUDED.name,
		       timezone = EXCLUDED.timezone,
		       address  = EXCLUDED.address,
		       city     = EXCLUDED.city,
		       state    = EXCLUDED.state,
		       updated_at = now()
		 WHERE (properties.name, properties.timezone, properties.address, properties.city, properties.state)
		       IS DISTINCT FROM
		       (EXCLUDED.name, EXCLUDED.timezone, EXCLUDED.address, EXCLUDED.city, EXCLUDED.state)
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q,
		propriedadeNome, propriedadeSlug, propriedadeFuso,
		propriedadeEndereco, propriedadeCidade, propriedadeUF)
	if err != nil {
		return c, err
	}
	c.Previstas = 1

	// O id sai de uma segunda consulta de propósito: com a guarda
	// `WHERE ... IS DISTINCT FROM`, a linha inalterada não aparece no RETURNING
	// — e esse é justamente o caso comum, da segunda execução em diante.
	if err := tx.QueryRow(ctx,
		`SELECT id FROM properties WHERE slug = $1`, propriedadeSlug,
	).Scan(&st.propriedadeID); err != nil {
		return c, fmt.Errorf("lendo o id da propriedade: %w", err)
	}
	return c, nil
}

// unidade é o que é ocupado e limpo — o que a constraint de sobreposição
// protege. Sem unidade nominal não existe defesa contra overbooking.
type unidade struct {
	codigo string
	nome   string
	ordem  int32
}

// As oito unidades físicas (spec §2). `floor` fica NULL: a numeração 201/202/203
// sugere andar, mas a spec não declara nenhum — e seed que adivinha vira dado
// errado que ninguém revisa.
var unidadesSeed = []unidade{
	{"AP-01", "Apartamento 201", 1},
	{"AP-02", "Apartamento 202", 2},
	{"AP-03", "Apartamento 203", 3},
	{"SP-01", "Suíte Piscina 1", 4},
	{"SP-02", "Suíte Piscina 2", 5},
	{"SP-03", "Suíte Piscina 3", 6},
	{"SP-04", "Suíte Piscina 4", 7},
	{"COB-01", "Cobertura", 8},
}

func unidades(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	const q = `
		INSERT INTO units (property_id, code, name, sort_order)
		SELECT $1, u.code, u.nome, u.ordem
		  FROM unnest($2::text[], $3::text[], $4::int[]) AS u(code, nome, ordem)
		ON CONFLICT (property_id, code) DO UPDATE
		   SET name = EXCLUDED.name, sort_order = EXCLUDED.sort_order, updated_at = now()
		 WHERE (units.name, units.sort_order)
		       IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.sort_order)
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q, st.propriedadeID,
		coluna(unidadesSeed, func(u unidade) string { return u.codigo }),
		coluna(unidadesSeed, func(u unidade) string { return u.nome }),
		coluna(unidadesSeed, func(u unidade) int32 { return u.ordem }),
	)
	c.Previstas = len(unidadesSeed)
	return c, err
}

// produto é o que se vende. Separado da unidade física de propósito: a Completa
// é um produto que consome as oito unidades, não uma nona unidade.
type produto struct {
	codigo     string
	nome       string
	capacidade int32
	consome    string // one_member | all_members
	limpeza    int64  // reais; convertidos em centavos na inserção
	ordem      int32
}

// Os quatro produtos (spec §2). A capacidade da Completa é DECLARADA (24), não
// somada — a soma daria 40; 24 é o limite operacional de evento que os
// proprietários fixaram.
var produtosSeed = []produto{
	{"apto-2s", "Apartamento 2 Suítes", 6, "one_member", 180, 1},
	{"suite-piscina", "Suítes da Piscina", 3, "one_member", 120, 2},
	{"cobertura", "White House Cobertura", 10, "one_member", 350, 3},
	{"completa", "White House Completa", 24, "all_members", 900, 4},
}

func produtos(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	const q = `
		INSERT INTO unit_types (property_id, code, name, capacity, consumes, cleaning_fee_cents, sort_order)
		SELECT $1, p.code, p.nome, p.capacidade, p.consome, p.limpeza, p.ordem
		  FROM unnest($2::text[], $3::text[], $4::int[], $5::text[], $6::bigint[], $7::int[])
		       AS p(code, nome, capacidade, consome, limpeza, ordem)
		ON CONFLICT (property_id, code) DO UPDATE
		   SET name               = EXCLUDED.name,
		       capacity           = EXCLUDED.capacity,
		       consumes           = EXCLUDED.consumes,
		       cleaning_fee_cents = EXCLUDED.cleaning_fee_cents,
		       sort_order         = EXCLUDED.sort_order,
		       updated_at         = now()
		 WHERE (unit_types.name, unit_types.capacity, unit_types.consumes,
		        unit_types.cleaning_fee_cents, unit_types.sort_order)
		       IS DISTINCT FROM
		       (EXCLUDED.name, EXCLUDED.capacity, EXCLUDED.consumes,
		        EXCLUDED.cleaning_fee_cents, EXCLUDED.sort_order)
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q, st.propriedadeID,
		coluna(produtosSeed, func(p produto) string { return p.codigo }),
		coluna(produtosSeed, func(p produto) string { return p.nome }),
		coluna(produtosSeed, func(p produto) int32 { return p.capacidade }),
		coluna(produtosSeed, func(p produto) string { return p.consome }),
		coluna(produtosSeed, func(p produto) int64 { return reais(p.limpeza) }),
		coluna(produtosSeed, func(p produto) int32 { return p.ordem }),
	)
	c.Previstas = len(produtosSeed)
	return c, err
}

// composicao liga produto → unidade. É daqui que nasce a exclusividade
// bidirecional: vender a Completa insere oito linhas em stay_blocks, e qualquer
// unidade ocupada faz a inserção estourar 23P01 → 409 DATE_CONFLICT.
type composicao struct{ produto, unidade string }

var composicaoSeed = func() []composicao {
	m := []composicao{
		{"apto-2s", "AP-01"}, {"apto-2s", "AP-02"}, {"apto-2s", "AP-03"},
		{"suite-piscina", "SP-01"}, {"suite-piscina", "SP-02"},
		{"suite-piscina", "SP-03"}, {"suite-piscina", "SP-04"},
		{"cobertura", "COB-01"},
	}
	// A Completa consome TODAS as unidades — derivar da lista, em vez de
	// redigitar as oito, garante que acrescentar uma unidade nova ao seed não
	// deixe a Completa vendendo por cima dela.
	for _, u := range unidadesSeed {
		m = append(m, composicao{"completa", u.codigo})
	}
	return m
}()

func composicaoDosProdutos(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	// Sem DO UPDATE: a linha é a própria chave primária, não há o que atualizar.
	// E sem DELETE do que não está na lista — a composição da Completa é uma das
	// decisões em aberto da spec, e o seed não pode desfazer o ajuste que a
	// gestão fez na tela.
	const q = `
		INSERT INTO unit_type_members (unit_type_id, unit_id)
		SELECT ut.id, u.id
		  FROM unnest($2::text[], $3::text[]) AS m(produto, unidade)
		  JOIN unit_types ut ON ut.property_id = $1 AND ut.code = m.produto
		  JOIN units      u  ON u.property_id  = $1 AND u.code  = m.unidade
		ON CONFLICT DO NOTHING
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q, st.propriedadeID,
		coluna(composicaoSeed, func(m composicao) string { return m.produto }),
		coluna(composicaoSeed, func(m composicao) string { return m.unidade }),
	)
	c.Previstas = len(composicaoSeed)
	return c, err
}
