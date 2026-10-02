package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Vigência da primeira versão de tudo que é comercial: tarifário, política de
// pagamento e política de cancelamento entram juntos.
var vigenciaV1 = data("2026-08-01")

const tarifarioNome = "Tabela Comercial V1"

func tarifario(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	// `valid_to` fica NULL: a tabela vale até que outra a suceda. Fechar a
	// vigência agora criaria um buraco no calendário no dia seguinte.
	const q = `
		INSERT INTO rate_tables (property_id, name, valid_from)
		VALUES ($1, $2, $3)
		ON CONFLICT (property_id, name) DO UPDATE
		   SET valid_from = EXCLUDED.valid_from
		 WHERE rate_tables.valid_from IS DISTINCT FROM EXCLUDED.valid_from
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q, st.propriedadeID, tarifarioNome, vigenciaV1)
	if err != nil {
		return c, err
	}
	c.Previstas = 1

	if err := tx.QueryRow(ctx,
		`SELECT id FROM rate_tables WHERE property_id = $1 AND name = $2`,
		st.propriedadeID, tarifarioNome,
	).Scan(&st.tarifarioID); err != nil {
		return c, fmt.Errorf("lendo o id do tarifário: %w", err)
	}
	return c, nil
}

// ordemDosTipos fixa a leitura da matriz de tarifas abaixo. É a mesma ordem da
// tabela de docs/spec.md §3, para conferir linha a linha com o documento.
var ordemDosTipos = []string{"normal", "fds", "feriado", "alta", "reveillon", "carnaval"}

// tarifa é uma linha da Tabela Comercial V1, em REAIS.
//
// A spec escreve os valores em reais (850 = R$ 850,00 a diária) e o banco guarda
// centavos; a conversão acontece num lugar só, no `reais()`. Gravar 850 direto
// num campo `_cents` seria vender a diária por R$ 8,50.
type tarifa struct {
	produto string
	valores [6]int64
}

var tarifasSeed = []tarifa{
	{"apto-2s", [6]int64{850, 1100, 1400, 1600, 3200, 2600}},
	{"suite-piscina", [6]int64{550, 700, 900, 1050, 2100, 1700}},
	{"cobertura", [6]int64{1900, 2400, 3100, 3600, 7500, 6000}},
	{"completa", [6]int64{5500, 6900, 8900, 10500, 21000, 17000}},
}

func tarifas(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	var (
		produtos []string
		tipos    []string
		valores  []int64
	)
	for _, t := range tarifasSeed {
		for i, tipo := range ordemDosTipos {
			produtos = append(produtos, t.produto)
			tipos = append(tipos, tipo)
			valores = append(valores, reais(t.valores[i]))
		}
	}

	const q = `
		INSERT INTO rates (rate_table_id, unit_type_id, date_type, amount_cents)
		SELECT $1, ut.id, t.tipo, t.valor
		  FROM unnest($3::text[], $4::text[], $5::bigint[]) AS t(produto, tipo, valor)
		  JOIN unit_types ut ON ut.property_id = $2 AND ut.code = t.produto
		ON CONFLICT (rate_table_id, unit_type_id, date_type) DO UPDATE
		   SET amount_cents = EXCLUDED.amount_cents
		 WHERE rates.amount_cents IS DISTINCT FROM EXCLUDED.amount_cents
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q, st.tarifarioID, st.propriedadeID, produtos, tipos, valores)
	c.Previstas = len(produtos)
	return c, err
}

// minimo é a estadia mínima de um tipo de data — spec §3. Vale o MAIOR mínimo
// entre as noites da estadia; quem aplica a regra é o domínio, aqui é só o dado.
type minimo struct {
	tipo   string
	noites int32
}

var estadiaMinimaSeed = []minimo{
	{"normal", 1},
	{"fds", 2},
	{"feriado", 3},
	{"alta", 3},
	{"reveillon", 4},
	{"carnaval", 4},
}

func estadiaMinima(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	const q = `
		INSERT INTO min_nights_rules (rate_table_id, date_type, nights)
		SELECT $1, m.tipo, m.noites
		  FROM unnest($2::text[], $3::int[]) AS m(tipo, noites)
		ON CONFLICT (rate_table_id, date_type) DO UPDATE
		   SET nights = EXCLUDED.nights
		 WHERE min_nights_rules.nights IS DISTINCT FROM EXCLUDED.nights
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, q, st.tarifarioID,
		coluna(estadiaMinimaSeed, func(m minimo) string { return m.tipo }),
		coluna(estadiaMinimaSeed, func(m minimo) int32 { return m.noites }),
	)
	c.Previstas = len(estadiaMinimaSeed)
	return c, err
}

// Política comercial v1 — spec §3.
//
// Os percentuais viajam como texto e são convertidos com `::numeric` no SQL:
// `numeric(5,2)` guarda o valor exato, e passar por float64 no caminho seria
// reintroduzir binário de ponto flutuante numa coluna que existe justamente
// para não ter isso.
const (
	sinalPct                = "50.00" // sinal para confirmar a reserva
	saldoDiasAntes          = 7       // saldo até 7 dias antes do check-in
	preReservaHoras         = 48      // pré-reserva segura a data sem pagamento
	descontoGestaoPct       = "5.00"  // até aqui a gestão fecha sozinha
	descontoProprietarioPct = "10.00" // de 6 a 10% exige o proprietário; acima, ninguém
	caucaoDeEventoReais     = 2000    // cobrada como recebível reembolsável

	// Limite da extensão de pré-reserva (spec §5: "com limite configurável").
	// A spec não fixa números — estes são ponto de partida operacional, e a
	// gestão os ajusta pela tela de política sem migration.
	extensaoDeHoldHoras = 24 // quanto cada `extend-hold` adiciona
	extensoesDeHoldMax  = 1  // quantas vezes a pré-reserva pode ser esticada

	// Validade do orçamento emitido sem `valid_until` explícito. Era a constante
	// `validadePadraoEmDias` da aplicação (dívida D7) e virou
	// `commercial_policies.quote_validity_days` em 20260831100000; 7 é o mesmo
	// número que a API já usava, de modo que semear isto não muda nenhum
	// orçamento existente — `quotes.valid_until` é gravada na emissão.
	validadeDeOrcamentoDias = 7
)

// sqlPoliticaComercialV1 é constante de PACOTE, e não local da função, porque
// `politica_comercial_integration_test.go` a compara com
// `information_schema.columns`: é essa comparação que impede a repetição da D7
// do lado do seed — coluna nova em `commercial_policies` que não entre nesta
// lista voltaria ao DEFAULT a cada `make seed`, em silêncio.
const sqlPoliticaComercialV1 = `
		INSERT INTO commercial_policies (
			property_id, version, deposit_pct, balance_due_days, hold_hours,
			discount_auto_pct, discount_approval_pct, event_deposit_cents, valid_from,
			hold_extension_hours, hold_max_extensions, quote_validity_days)
		VALUES ($1, 1, $2::numeric, $3, $4, $5::numeric, $6::numeric, $7, $8, $9, $10, $11)
		ON CONFLICT (property_id, version) DO UPDATE
		   SET deposit_pct           = EXCLUDED.deposit_pct,
		       balance_due_days      = EXCLUDED.balance_due_days,
		       hold_hours            = EXCLUDED.hold_hours,
		       discount_auto_pct     = EXCLUDED.discount_auto_pct,
		       discount_approval_pct = EXCLUDED.discount_approval_pct,
		       event_deposit_cents   = EXCLUDED.event_deposit_cents,
		       valid_from            = EXCLUDED.valid_from,
		       hold_extension_hours  = EXCLUDED.hold_extension_hours,
		       hold_max_extensions   = EXCLUDED.hold_max_extensions,
		       quote_validity_days   = EXCLUDED.quote_validity_days
		 WHERE (commercial_policies.deposit_pct, commercial_policies.balance_due_days,
		        commercial_policies.hold_hours, commercial_policies.discount_auto_pct,
		        commercial_policies.discount_approval_pct, commercial_policies.event_deposit_cents,
		        commercial_policies.valid_from, commercial_policies.hold_extension_hours,
		        commercial_policies.hold_max_extensions, commercial_policies.quote_validity_days)
		       IS DISTINCT FROM
		       (EXCLUDED.deposit_pct, EXCLUDED.balance_due_days, EXCLUDED.hold_hours,
		        EXCLUDED.discount_auto_pct, EXCLUDED.discount_approval_pct,
		        EXCLUDED.event_deposit_cents, EXCLUDED.valid_from,
		        EXCLUDED.hold_extension_hours, EXCLUDED.hold_max_extensions,
		        EXCLUDED.quote_validity_days)
		RETURNING (xmax = 0)`

func politicaComercial(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	c, err := upsert(ctx, tx, sqlPoliticaComercialV1, st.propriedadeID,
		sinalPct, int32(saldoDiasAntes), int32(preReservaHoras),
		descontoGestaoPct, descontoProprietarioPct,
		reais(caucaoDeEventoReais), vigenciaV1,
		int32(extensaoDeHoldHoras), int32(extensoesDeHoldMax),
		int32(validadeDeOrcamentoDias))
	c.Previstas = 1
	return c, err
}

const politicaCancelamentoNome = "Padrão White House"

// faixa é uma faixa de cancelamento. `min`/`max` nulos significam sem piso e
// sem teto — é a tradução do `-1` que `internal/domain/booking.Tier` usa como
// sentinela em Go, e a razão de as colunas serem anuláveis.
type faixa struct {
	min       *int32
	max       *int32
	devolucao string // percentual do sinal, como texto → ::numeric
	rotulo    string
	ordem     int32
}

func i32(v int32) *int32 { return &v }

// Espelha booking.DefaultCancellation(): ≥30 dias devolve tudo, 7 a 29 retém
// metade, abaixo de 7 retém tudo. Se estes números divergirem do domínio, a
// simulação do `?dry_run=1` mente sobre o que a política guardada faria.
var faixasSeed = []faixa{
	{i32(30), nil, "100.00", "Devolução integral do sinal", 1},
	{i32(7), i32(29), "50.00", "Retenção de 50% do sinal", 2},
	{nil, i32(6), "0.00", "Retenção integral do sinal", 3},
}

func politicaDeCancelamento(ctx context.Context, tx pgx.Tx, st *estado) (contagem, error) {
	const qPolitica = `
		INSERT INTO cancellation_policies (property_id, version, name, valid_from)
		VALUES ($1, 1, $2, $3)
		ON CONFLICT (property_id, version) DO UPDATE
		   SET name = EXCLUDED.name, valid_from = EXCLUDED.valid_from
		 WHERE (cancellation_policies.name, cancellation_policies.valid_from)
		       IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.valid_from)
		RETURNING (xmax = 0)`

	c, err := upsert(ctx, tx, qPolitica, st.propriedadeID, politicaCancelamentoNome, vigenciaV1)
	if err != nil {
		return c, err
	}
	c.Previstas = 1 + len(faixasSeed)

	const qFaixas = `
		INSERT INTO cancellation_tiers (policy_id, days_before_min, days_before_max, refund_pct, label, sort_order)
		SELECT p.id, f.minimo, f.maximo, f.devolucao::numeric, f.rotulo, f.ordem
		  FROM unnest($2::int[], $3::int[], $4::text[], $5::text[], $6::int[])
		       AS f(minimo, maximo, devolucao, rotulo, ordem)
		  JOIN cancellation_policies p ON p.property_id = $1 AND p.version = 1
		ON CONFLICT (policy_id, sort_order) DO UPDATE
		   SET days_before_min = EXCLUDED.days_before_min,
		       days_before_max = EXCLUDED.days_before_max,
		       refund_pct      = EXCLUDED.refund_pct,
		       label           = EXCLUDED.label
		 WHERE (cancellation_tiers.days_before_min, cancellation_tiers.days_before_max,
		        cancellation_tiers.refund_pct, cancellation_tiers.label)
		       IS DISTINCT FROM
		       (EXCLUDED.days_before_min, EXCLUDED.days_before_max,
		        EXCLUDED.refund_pct, EXCLUDED.label)
		RETURNING (xmax = 0)`

	cf, err := upsert(ctx, tx, qFaixas, st.propriedadeID,
		coluna(faixasSeed, func(f faixa) *int32 { return f.min }),
		coluna(faixasSeed, func(f faixa) *int32 { return f.max }),
		coluna(faixasSeed, func(f faixa) string { return f.devolucao }),
		coluna(faixasSeed, func(f faixa) string { return f.rotulo }),
		coluna(faixasSeed, func(f faixa) int32 { return f.ordem }),
	)
	c.Criadas += cf.Criadas
	c.Atualizadas += cf.Atualizadas
	return c, err
}
