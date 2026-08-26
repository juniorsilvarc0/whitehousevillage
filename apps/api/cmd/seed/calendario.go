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

// Feriados nacionais de 2026–2027 que caem na janela comercial coberta pelo
// tarifário V1. Não inclui feriado municipal de Luís Correia — falta a lista
// oficial, e feriado inventado vira diária cobrada a mais.
var feriadosSeed = []feriado{
	{data("2026-09-07"), "Independência do Brasil"},
	{data("2026-10-12"), "Nossa Senhora Aparecida"},
	{data("2026-11-02"), "Finados"},
	{data("2026-11-15"), "Proclamação da República"},
	{data("2026-12-25"), "Natal"},
	{data("2027-01-01"), "Confraternização Universal"},
	{data("2027-04-21"), "Tiradentes"},
}

func feriados(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	const q = `
		INSERT INTO holidays (property_id, date, name)
		SELECT $1, f.dia, f.nome
		  FROM unnest($2::date[], $3::text[]) AS f(dia, nome)
		ON CONFLICT (property_id, date) DO UPDATE
		   SET name = EXCLUDED.name
		 WHERE holidays.name IS DISTINCT FROM EXCLUDED.name
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q, st.propriedadeID,
		coluna(feriadosSeed, func(f feriado) time.Time { return f.dia }),
		coluna(feriadosSeed, func(f feriado) string { return f.nome }),
	)
	c.Previstas = len(feriadosSeed)
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

// O nome carrega o ano de propósito: ele é a chave natural do seed
// (`UNIQUE (property_id, name)`), e o Réveillon do ano que vem precisa ser uma
// LINHA NOVA, não uma edição desta.
var periodosSeed = []periodo{
	{"Réveillon 2026/2027", "reveillon", data("2026-12-27"), data("2027-01-02")},
	{"Carnaval 2027", "carnaval", data("2027-02-05"), data("2027-02-10")},
	{"Alta temporada 2026/2027", "alta", data("2026-12-15"), data("2027-01-31")},
	{"Férias de julho 2027", "alta", data("2027-07-01"), data("2027-07-31")},
}

func periodosEspeciais(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	const q = `
		INSERT INTO special_periods (property_id, name, kind, starts_on, ends_on)
		SELECT $1, p.nome, p.kind, p.inicio, p.fim
		  FROM unnest($2::text[], $3::text[], $4::date[], $5::date[])
		       AS p(nome, kind, inicio, fim)
		ON CONFLICT (property_id, name) DO UPDATE
		   SET kind = EXCLUDED.kind, starts_on = EXCLUDED.starts_on, ends_on = EXCLUDED.ends_on
		 WHERE (special_periods.kind, special_periods.starts_on, special_periods.ends_on)
		       IS DISTINCT FROM (EXCLUDED.kind, EXCLUDED.starts_on, EXCLUDED.ends_on)
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q, st.propriedadeID,
		coluna(periodosSeed, func(p periodo) string { return p.nome }),
		coluna(periodosSeed, func(p periodo) string { return p.kind }),
		coluna(periodosSeed, func(p periodo) time.Time { return p.inicio }),
		coluna(periodosSeed, func(p periodo) time.Time { return p.fim }),
	)
	c.Previstas = len(periodosSeed)
	return c, err
}
