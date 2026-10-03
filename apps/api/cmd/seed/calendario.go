package main

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// mascaraDeFimDeSemana marca sexta e sábado.
//
// O bit é o índice do dia na semana com domingo = 0, que é ao mesmo tempo o
// `time.Weekday` do Go e o `EXTRACT(DOW)` do Postgres — as duas pontas leem a
// mesma máscara sem tabela de conversão.
//
// Domingo NÃO entra: na White House o hóspede vai embora no domingo, então a
// diária de domingo é normal. É o mesmo fim de semana comercial que
// `internal/domain/calendar.NewCommercial` monta (sexta e sábado).
const mascaraDeFimDeSemana = int32(1)<<uint(time.Friday) | int32(1)<<uint(time.Saturday)

// tipoDeData classifica a diária. A precedência é dado e não cadeia de if:
// mudar a ordem de resolução é decisão comercial, não técnica (spec §3).
type tipoDeData struct {
	kind        string
	precedencia int32
	mascara     int32
}

var tiposDeDataSeed = []tipoDeData{
	{"reveillon", 100, 0},
	{"carnaval", 100, 0},
	{"feriado", 80, 0},
	{"alta", 60, 0},
	{"fds", 40, mascaraDeFimDeSemana},
	{"normal", 0, 0},
}

func tiposDeData(ctx context.Context, tx pgx.Tx, _ *estado) (contagem, error) {
	const q = `
		INSERT INTO date_type_rules (kind, precedence, weekday_mask)
		SELECT t.kind, t.precedencia, t.mascara
		  FROM unnest($1::text[], $2::int[], $3::int[]) AS t(kind, precedencia, mascara)
		ON CONFLICT (kind) DO UPDATE
		   SET precedence = EXCLUDED.precedence, weekday_mask = EXCLUDED.weekday_mask
		 WHERE (date_type_rules.precedence, date_type_rules.weekday_mask)
		       IS DISTINCT FROM (EXCLUDED.precedence, EXCLUDED.weekday_mask)
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q,
		coluna(tiposDeDataSeed, func(t tipoDeData) string { return t.kind }),
		coluna(tiposDeDataSeed, func(t tipoDeData) int32 { return t.precedencia }),
		coluna(tiposDeDataSeed, func(t tipoDeData) int32 { return t.mascara }),
	)
	c.Previstas = len(tiposDeDataSeed)
	return c, err
}

type feriado struct {
	dia  time.Time
	nome string
}

func feriados(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	fs := st.cat.feriados
	const q = `
		INSERT INTO holidays (property_id, date, name, active)
		SELECT $1, f.dia, f.nome, true
		  FROM unnest($2::date[], $3::text[]) AS f(dia, nome)
		ON CONFLICT (property_id, date) DO UPDATE
		   SET name = EXCLUDED.name, active = true
		 WHERE (holidays.name, holidays.active) IS DISTINCT FROM (EXCLUDED.name, true)
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q, st.propriedadeID,
		coluna(fs, func(f feriado) time.Time { return f.dia }),
		coluna(fs, func(f feriado) string { return f.nome }),
	)
	c.Previstas = len(fs)
	if err != nil {
		return c, err
	}

	// Feriado do outro catálogo que não é do escolhido sai do calendário
	// comercial — desativado, não apagado.
	c.Desativadas, err = afetadas(ctx, tx, `
		UPDATE holidays SET active = false
		 WHERE property_id = $1 AND date = ANY($2::date[]) AND NOT (date = ANY($3::date[])) AND active`,
		st.propriedadeID,
		coluna(st.outro.feriados, func(f feriado) time.Time { return f.dia }),
		coluna(fs, func(f feriado) time.Time { return f.dia }))
	return c, err
}

// periodo é um intervalo comercial. Períodos PODEM se sobrepor de propósito —
// o Réveillon cai dentro da alta temporada — e a precedência resolve qual vale.
// Por isso não há constraint de exclusão em special_periods.
type periodo struct {
	nome   string
	kind   string
	inicio time.Time
	fim    time.Time // inclusivo: special_periods usa daterange '[]'
}

func periodosEspeciais(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	ps := st.cat.periodos
	const q = `
		INSERT INTO special_periods (property_id, name, kind, starts_on, ends_on, active)
		SELECT $1, p.nome, p.kind, p.inicio, p.fim, true
		  FROM unnest($2::text[], $3::text[], $4::date[], $5::date[])
		       AS p(nome, kind, inicio, fim)
		ON CONFLICT (property_id, name) DO UPDATE
		   SET kind = EXCLUDED.kind, starts_on = EXCLUDED.starts_on, ends_on = EXCLUDED.ends_on,
		       active = true
		 WHERE (special_periods.kind, special_periods.starts_on, special_periods.ends_on, special_periods.active)
		       IS DISTINCT FROM (EXCLUDED.kind, EXCLUDED.starts_on, EXCLUDED.ends_on, true)
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q, st.propriedadeID,
		coluna(ps, func(p periodo) string { return p.nome }),
		coluna(ps, func(p periodo) string { return p.kind }),
		coluna(ps, func(p periodo) time.Time { return p.inicio }),
		coluna(ps, func(p periodo) time.Time { return p.fim }),
	)
	c.Previstas = len(ps)
	if err != nil {
		return c, err
	}

	nome := func(p periodo) string { return p.nome }
	c.Desativadas, err = afetadas(ctx, tx, `
		UPDATE special_periods SET active = false
		 WHERE property_id = $1 AND name = ANY($2::text[]) AND active`,
		st.propriedadeID, foraDoCatalogo(coluna(st.outro.periodos, nome), coluna(ps, nome)))
	return c, err
}
