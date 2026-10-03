package main

// catalogoDaCasa é o catálogo REAL da White House Village, definido pelo dono
// do negócio em 03/10/2026. É o padrão do seed (`make seed`, compose, VPS).
//
// Valores em REAIS; `sobConsulta` marca tipo de data sem diária de tabela.
var catalogoDaCasa = catalogo{
	nome:             catalogoReal,
	unidades:         unidadesReais,
	produtos:         produtosReais,
	composicao:       composicaoReal,
	tarifas:          tarifasReais,
	minimoGeral:      minimoGeralReal,
	minimoPorProduto: minimoPorProdutoReal,
	pacotes:          pacotesReais,
	feriados:         feriadosReais,
	periodos:         periodosReais,
}

func nomePublico(s string) *string { return &s }

// As doze unidades físicas, na ordem do painel.
var unidadesReais = []unidade{
	{"AP-01", "AP 01 — Duplex Aurora", 1},
	{"AP-02", "AP 02 — Duplex Brisa", 2},
	{"AP-03", "AP 03 — Duplex Duna", 3},
	{"AP-04", "AP 04 — Duplex Maré", 4},
	{"AP-05", "AP 05 — Duplex Âmbar", 5},
	{"AP-06", "AP 06 — Duplex Horizonte", 6},
	{"SP-01", "Suíte 01 — Coral", 7},
	{"SP-02", "Suíte 02 — Pérola", 8},
	{"SP-03", "Suíte 03 — Concha", 9},
	{"SP-04", "Suíte 04 — Oceano", 10},
	{"GV-01", "White House Grand Villa (casa principal)", 11},
	{"CV-01", "White House Classic Villa (casa rústica)", 12},
}

// Os produtos. Limpeza R$ 0 em todos, como o dono definiu.
// A capacidade da Completa é DECLARADA (24), não somada.
var produtosReais = []produto{
	{"duplex-aurora", "AP 01 — Duplex Aurora", nomePublico("Duplex Aurora"), 7, "one_member", 0, 1},
	{"duplex-brisa", "AP 02 — Duplex Brisa", nomePublico("Duplex Brisa"), 7, "one_member", 0, 2},
	{"duplex-duna", "AP 03 — Duplex Duna", nomePublico("Duplex Duna"), 7, "one_member", 0, 3},
	{"duplex-mare", "AP 04 — Duplex Maré", nomePublico("Duplex Maré"), 7, "one_member", 0, 4},
	{"duplex-ambar", "AP 05 — Duplex Âmbar", nomePublico("Duplex Âmbar"), 7, "one_member", 0, 5},
	{"duplex-horizonte", "AP 06 — Duplex Horizonte", nomePublico("Duplex Horizonte"), 7, "one_member", 0, 6},
	{"suite-coral", "Suíte 01 — Coral", nomePublico("Pool Suíte Coral"), 2, "one_member", 0, 10},
	{"suite-perola", "Suíte 02 — Pérola", nomePublico("Pool Suíte Pérola"), 2, "one_member", 0, 11},
	{"suite-concha", "Suíte 03 — Concha", nomePublico("Pool Suíte Concha"), 2, "one_member", 0, 12},
	{"suite-oceano", "Suíte 04 — Oceano", nomePublico("Pool Suíte Oceano"), 2, "one_member", 0, 13},
	{"pool-suites", "Pool Suítes — as 4 suítes", nomePublico("Pool Suítes — as 4 suítes"), 8, "all_members", 0, 14},
	{"grand-villa", "White House Grand Villa (casa principal)", nomePublico("White House Grand Villa"), 8, "one_member", 0, 20},
	{"classic-villa", "White House Classic Villa (casa rústica)", nomePublico("White House Classic Villa"), 8, "one_member", 0, 21},
	{"completa", "White House Completa (todas as unidades)", nomePublico("White House Completa"), 24, "all_members", 0, 30},
}

var composicaoReal = func() []composicao {
	m := []composicao{
		{"duplex-aurora", "AP-01"},
		{"duplex-brisa", "AP-02"},
		{"duplex-duna", "AP-03"},
		{"duplex-mare", "AP-04"},
		{"duplex-ambar", "AP-05"},
		{"duplex-horizonte", "AP-06"},
		{"suite-coral", "SP-01"},
		{"suite-perola", "SP-02"},
		{"suite-concha", "SP-03"},
		{"suite-oceano", "SP-04"},
		{"pool-suites", "SP-01"}, {"pool-suites", "SP-02"},
		{"pool-suites", "SP-03"}, {"pool-suites", "SP-04"},
		{"grand-villa", "GV-01"},
		{"classic-villa", "CV-01"},
	}
	// A Completa consome TODAS as unidades do catálogo — derivada da lista.
	for _, u := range unidadesReais {
		m = append(m, composicao{"completa", u.codigo})
	}
	return m
}()

// Diárias por tipo de data, em reais, na ordem de `ordemDosTipos`
// (normal, fds, feriado, alta, reveillon, carnaval). A Completa não tem linha:
// é vendida sob consulta.
var tarifasReais = func() []tarifa {
	duplex := [6]int64{500, 600, 600, 750, 1000, 1000}
	suite := [6]int64{320, 400, 400, 480, 600, 600}
	t := []tarifa{}
	for _, p := range []string{"duplex-aurora", "duplex-brisa", "duplex-duna", "duplex-mare", "duplex-ambar", "duplex-horizonte"} {
		t = append(t, tarifa{p, duplex})
	}
	for _, p := range []string{"suite-coral", "suite-perola", "suite-concha", "suite-oceano"} {
		t = append(t, tarifa{p, suite})
	}
	return append(t,
		tarifa{"pool-suites", [6]int64{2500, 3200, 3200, 4800, 6000, 6000}},
		tarifa{"grand-villa", [6]int64{3500, 3500, sobConsulta, 6500, sobConsulta, sobConsulta}},
		tarifa{"classic-villa", [6]int64{2500, 3000, 4000, 3500, 4500, 4500}},
	)
}()

var minimoGeralReal = []minimo{
	{"normal", 1},
	{"fds", 2},
	{"feriado", 2},
	{"alta", 3},
	{"reveillon", 4},
	{"carnaval", 4},
}

var minimoPorProdutoReal = func() []minimoProduto {
	var m []minimoProduto
	porTipo := func(produto string, tipos []string, noites []int32) {
		for i, t := range tipos {
			m = append(m, minimoProduto{produto, t, noites[i]})
		}
	}
	for _, p := range []string{"suite-coral", "suite-perola", "suite-concha", "suite-oceano", "pool-suites"} {
		porTipo(p, ordemDosTipos, []int32{2, 2, 2, 4, 4, 4})
	}
	porTipo("grand-villa", []string{"normal", "fds", "alta"}, []int32{2, 2, 2})
	porTipo("classic-villa", ordemDosTipos, []int32{2, 2, 2, 2, 2, 2})
	return m
}()

// Pacotes da Grand Villa: noites consecutivas de tipos listados pelo total.
var pacotesReais = []pacote{
	{"grand-villa", 2, []string{"normal", "fds"}, 6500},
	{"grand-villa", 4, []string{"normal", "fds"}, 12000},
	{"grand-villa", 2, []string{"alta"}, 10500},
	{"grand-villa", 4, []string{"alta"}, 20000},
}

// Feriados — uma linha por NOITE cobrada como feriado (o feriado prolongado
// cobre a véspera e o dia, ou o sábado e o domingo).
var feriadosReais = []feriado{
	{data("2026-09-05"), "Independência do Brasil"},
	{data("2026-09-06"), "Independência do Brasil"},
	{data("2026-10-10"), "Nossa Senhora Aparecida"},
	{data("2026-10-11"), "Nossa Senhora Aparecida"},
	{data("2026-10-17"), "Dia do Piauí"},
	{data("2026-10-18"), "Dia do Piauí"},
	{data("2026-10-31"), "Finados"},
	{data("2026-11-01"), "Finados"},
	{data("2026-11-20"), "Consciência Negra"},
	{data("2026-11-21"), "Consciência Negra"},
}

// Períodos especiais; `fim` é a ÚLTIMA NOITE (inclusivo). Os nomes são os que
// o dono deu, sem ano — o período de 2027/2028 vai precisar de nome distinto,
// porque o nome é a chave natural de special_periods.
var periodosReais = []periodo{
	{"Natal", "alta", data("2026-12-24"), data("2026-12-26")},
	{"Réveillon", "reveillon", data("2026-12-28"), data("2027-01-01")},
	{"Férias de Janeiro", "alta", data("2027-01-01"), data("2027-01-31")},
	{"Carnaval", "carnaval", data("2027-02-06"), data("2027-02-09")},
	{"Semana Santa", "alta", data("2027-03-25"), data("2027-03-27")},
	{"Corpus Christi", "alta", data("2027-05-27"), data("2027-05-29")},
	{"Férias de Julho", "alta", data("2027-07-01"), data("2027-07-31")},
}
