package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// foraDoCatalogo devolve os itens do outro catálogo que não existem no
// escolhido — são esses, e só esses, que o seed desativa. O que a gestão
// cadastrou pela tela não é de catálogo nenhum e não é tocado.
func foraDoCatalogo(outro, escolhido []string) []string {
	ficam := make(map[string]bool, len(escolhido))
	for _, v := range escolhido {
		ficam[v] = true
	}
	var sai []string
	for _, v := range outro {
		if !ficam[v] {
			sai = append(sai, v)
		}
	}
	return sai
}

func unidades(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	us := st.cat.unidades
	// `active = true` no DO UPDATE: a unidade que o outro catálogo desativou
	// volta a vender quando o catálogo dela é semeado de novo.
	const q = `
		INSERT INTO units (property_id, code, name, sort_order, active)
		SELECT $1, u.code, u.nome, u.ordem, true
		  FROM unnest($2::text[], $3::text[], $4::int[]) AS u(code, nome, ordem)
		ON CONFLICT (property_id, code) DO UPDATE
		   SET name = EXCLUDED.name, sort_order = EXCLUDED.sort_order,
		       active = true, updated_at = now()
		 WHERE (units.name, units.sort_order, units.active)
		       IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.sort_order, true)
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q, st.propriedadeID,
		coluna(us, func(u unidade) string { return u.codigo }),
		coluna(us, func(u unidade) string { return u.nome }),
		coluna(us, func(u unidade) int32 { return u.ordem }),
	)
	c.Previstas = len(us)
	if err != nil {
		return c, err
	}

	// Desativar, nunca apagar: a unidade pode ter histórico de ocupação, e
	// `stay_blocks` aponta para ela.
	c.Desativadas, err = afetadas(ctx, tx, `
		UPDATE units SET active = false, updated_at = now()
		 WHERE property_id = $1 AND code = ANY($2::text[]) AND active`,
		st.propriedadeID, foraDoCatalogo(st.outro.codigosDeUnidade(), st.cat.codigosDeUnidade()))
	return c, err
}

// vendaViva é a mesma definição das guardas do banco (§5 de docs/db.md): os
// três estados em que existe promessa de estadia de pé.
const vendaViva = `r.status IN ('hold','confirmed','checked_in')`

// errCatalogoComVendaViva é o erro que o seed devolve, ANTES de tocar no banco,
// quando trocar o catálogo mudaria o que já foi vendido. Os gatilhos adiados do
// banco recusariam a mesma coisa no COMMIT, mas com uma mensagem sobre
// invariante, não sobre seed — e só depois de o seed ter feito todo o resto.
var errCatalogoComVendaViva = errors.New("o catálogo não pode ser trocado com venda viva")

func produtos(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	ps := st.cat.produtos

	// Guarda: `consumes` é imutável com venda viva (constraint trigger
	// unit_types_consumes_com_venda_viva). Conferir aqui dá a mensagem certa.
	var conflitos []string
	linhas, err := tx.Query(ctx, `
		SELECT ut.code || ' (' || ut.consumes || ' → ' || p.consome || ', reservas: '
		       || string_agg(r.code, ', ' ORDER BY r.code) || ')'
		  FROM unnest($2::text[], $3::text[]) AS p(code, consome)
		  JOIN unit_types ut   ON ut.property_id = $1 AND ut.code = p.code
		  JOIN reservations r  ON r.unit_type_id = ut.id AND `+vendaViva+`
		 WHERE ut.consumes <> p.consome
		 GROUP BY ut.code, ut.consumes, p.consome`,
		st.propriedadeID,
		coluna(ps, func(p produto) string { return p.codigo }),
		coluna(ps, func(p produto) string { return p.consome }))
	if err != nil {
		return contagem{}, err
	}
	conflitos, err = pgx.CollectRows(linhas, pgx.RowTo[string])
	if err != nil {
		return contagem{}, err
	}
	if len(conflitos) > 0 {
		return contagem{}, fmt.Errorf("%w: mudaria consumes de %s — termine ou cancele essas reservas antes",
			errCatalogoComVendaViva, strings.Join(conflitos, "; "))
	}

	const q = `
		INSERT INTO unit_types (property_id, code, name, public_name, capacity, consumes,
		                        cleaning_fee_cents, sort_order, active)
		SELECT $1, p.code, p.nome, p.publico, p.capacidade, p.consome, p.limpeza, p.ordem, true
		  FROM unnest($2::text[], $3::text[], $4::text[], $5::int[], $6::text[], $7::bigint[], $8::int[])
		       AS p(code, nome, publico, capacidade, consome, limpeza, ordem)
		ON CONFLICT (property_id, code) DO UPDATE
		   SET name               = EXCLUDED.name,
		       public_name        = EXCLUDED.public_name,
		       capacity           = EXCLUDED.capacity,
		       consumes           = EXCLUDED.consumes,
		       cleaning_fee_cents = EXCLUDED.cleaning_fee_cents,
		       sort_order         = EXCLUDED.sort_order,
		       active             = true,
		       updated_at         = now()
		 WHERE (unit_types.name, unit_types.public_name, unit_types.capacity, unit_types.consumes,
		        unit_types.cleaning_fee_cents, unit_types.sort_order, unit_types.active)
		       IS DISTINCT FROM
		       (EXCLUDED.name, EXCLUDED.public_name, EXCLUDED.capacity, EXCLUDED.consumes,
		        EXCLUDED.cleaning_fee_cents, EXCLUDED.sort_order, true)
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q, st.propriedadeID,
		coluna(ps, func(p produto) string { return p.codigo }),
		coluna(ps, func(p produto) string { return p.nome }),
		coluna(ps, func(p produto) *string { return p.publico }),
		coluna(ps, func(p produto) int32 { return p.capacidade }),
		coluna(ps, func(p produto) string { return p.consome }),
		coluna(ps, func(p produto) int64 { return reais(p.limpeza) }),
		coluna(ps, func(p produto) int32 { return p.ordem }),
	)
	c.Previstas = len(ps)
	if err != nil {
		return c, err
	}

	c.Desativadas, err = afetadas(ctx, tx, `
		UPDATE unit_types SET active = false, updated_at = now()
		 WHERE property_id = $1 AND code = ANY($2::text[]) AND active`,
		st.propriedadeID, foraDoCatalogo(st.outro.codigosDeProduto(), st.cat.codigosDeProduto()))
	return c, err
}

// composicaoDosProdutos deixa a composição de cada produto do catálogo
// EXATAMENTE igual à lista: insere o que falta e remove o membro a mais.
//
// Remover é decisão nova (até 03/10/2026 o seed só acrescentava): sem ela, a
// Completa do catálogo real herdaria a `COB-01` do catálogo de teste — e venderia
// junto uma unidade desativada. Produtos fora do catálogo não são tocados.
func composicaoDosProdutos(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	m := st.cat.composicao
	produtosM := coluna(m, func(m composicao) string { return m.produto })
	unidadesM := coluna(m, func(m composicao) string { return m.unidade })

	// O que sairia. Remover membro de produto com venda viva muda o que foi
	// vendido (e, na Completa, o gatilho adiado reprovaria no COMMIT).
	const sobra = `
		  FROM unit_type_members um
		  JOIN unit_types ut ON ut.id = um.unit_type_id
		  JOIN units      u  ON u.id  = um.unit_id
		 WHERE ut.property_id = $1
		   AND ut.code = ANY($2::text[])
		   AND NOT EXISTS (SELECT 1 FROM unnest($3::text[], $4::text[]) AS k(produto, unidade)
		                    WHERE k.produto = ut.code AND k.unidade = u.code)`

	linhas, err := tx.Query(ctx, `
		SELECT ut.code || ' perderia ' || string_agg(DISTINCT u.code, ', ') || ' (reservas: '
		       || (SELECT string_agg(r.code, ', ' ORDER BY r.code)
		             FROM reservations r WHERE r.unit_type_id = ut.id AND `+vendaViva+`) || ')'
		`+sobra+`
		   AND EXISTS (SELECT 1 FROM reservations r WHERE r.unit_type_id = ut.id AND `+vendaViva+`)
		 GROUP BY ut.id, ut.code`,
		st.propriedadeID, st.cat.codigosDeProduto(), produtosM, unidadesM)
	if err != nil {
		return contagem{}, err
	}
	conflitos, err := pgx.CollectRows(linhas, pgx.RowTo[string])
	if err != nil {
		return contagem{}, err
	}
	if len(conflitos) > 0 {
		return contagem{}, fmt.Errorf("%w: a composição mudaria — %s", errCatalogoComVendaViva, strings.Join(conflitos, "; "))
	}

	// Sem DO UPDATE: a linha é a própria chave primária, não há o que atualizar.
	const q = `
		INSERT INTO unit_type_members (unit_type_id, unit_id)
		SELECT ut.id, u.id
		  FROM unnest($2::text[], $3::text[]) AS m(produto, unidade)
		  JOIN unit_types ut ON ut.property_id = $1 AND ut.code = m.produto
		  JOIN units      u  ON u.property_id  = $1 AND u.code  = m.unidade
		ON CONFLICT DO NOTHING
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q, st.propriedadeID, produtosM, unidadesM)
	c.Previstas = len(m)
	if err != nil {
		return c, err
	}

	c.Removidas, err = afetadas(ctx, tx, `
		DELETE FROM unit_type_members
		 WHERE (unit_type_id, unit_id) IN (SELECT um.unit_type_id, um.unit_id `+sobra+`)`,
		st.propriedadeID, st.cat.codigosDeProduto(), produtosM, unidadesM)
	return c, err
}
