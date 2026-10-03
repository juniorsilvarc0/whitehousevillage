package main

// catalogoDeTeste é o catálogo de DEMONSTRAÇÃO — congelado no que o seed
// semeava até 03/10/2026. Os testes de integração (~33 arquivos) dependem destes
// códigos, nomes e valores; mudar qualquer número aqui é mudar o que a suíte
// espera. `make it-seed` semeia este com SEED_CATALOGO=teste.
//
// Fonte original: docs/spec.md §2 (inventário) e §3 (tarifário).
var catalogoDeTeste = catalogo{
	nome:             catalogoTeste,
	unidades:         unidadesTeste,
	produtos:         produtosTeste,
	composicao:       composicaoTeste,
	tarifas:          tarifasTeste,
	minimoGeral:      minimoGeralTeste,
	minimoPorProduto: nil,
	pacotes:          nil,
	feriados:         feriadosTeste,
	periodos:         periodosTeste,
}

// As oito unidades físicas (spec §2). `floor` fica NULL: a numeração 201/202/203
// sugere andar, mas a spec não declara nenhum — e seed que adivinha vira dado
// errado que ninguém revisa.
var unidadesTeste = []unidade{
	{"AP-01", "Apartamento 201", 1},
	{"AP-02", "Apartamento 202", 2},
	{"AP-03", "Apartamento 203", 3},
	{"SP-01", "Suíte Piscina 1", 4},
	{"SP-02", "Suíte Piscina 2", 5},
	{"SP-03", "Suíte Piscina 3", 6},
	{"SP-04", "Suíte Piscina 4", 7},
	{"COB-01", "Cobertura", 8},
}

// Os quatro produtos (spec §2). A capacidade da Completa é DECLARADA (24), não
// somada — a soma daria 40; 24 é o limite operacional de evento que os
// proprietários fixaram. Sem nome de vitrine: `public_name` fica NULL.
var produtosTeste = []produto{
	{"apto-2s", "Apartamento 2 Suítes", nil, 6, "one_member", 180, 1},
	{"suite-piscina", "Suítes da Piscina", nil, 3, "one_member", 120, 2},
	{"cobertura", "White House Cobertura", nil, 10, "one_member", 350, 3},
	{"completa", "White House Completa", nil, 24, "all_members", 900, 4},
}

var composicaoTeste = func() []composicao {
	m := []composicao{
		{"apto-2s", "AP-01"}, {"apto-2s", "AP-02"}, {"apto-2s", "AP-03"},
		{"suite-piscina", "SP-01"}, {"suite-piscina", "SP-02"},
		{"suite-piscina", "SP-03"}, {"suite-piscina", "SP-04"},
		{"cobertura", "COB-01"},
	}
	// A Completa consome TODAS as unidades do catálogo — derivar da lista, em
	// vez de redigitar, garante que acrescentar uma unidade não deixe a
	// Completa vendendo por cima dela.
	for _, u := range unidadesTeste {
		m = append(m, composicao{"completa", u.codigo})
	}
	return m
}()

// Tabela Comercial V1 da spec §3, em reais, na ordem de `ordemDosTipos`.
var tarifasTeste = []tarifa{
	{"apto-2s", [6]int64{850, 1100, 1400, 1600, 3200, 2600}},
	{"suite-piscina", [6]int64{550, 700, 900, 1050, 2100, 1700}},
	{"cobertura", [6]int64{1900, 2400, 3100, 3600, 7500, 6000}},
	{"completa", [6]int64{5500, 6900, 8900, 10500, 21000, 17000}},
}

// Estadia mínima geral — spec §3.
var minimoGeralTeste = []minimo{
	{"normal", 1},
	{"fds", 2},
	{"feriado", 3},
	{"alta", 3},
	{"reveillon", 4},
	{"carnaval", 4},
}

// Feriados nacionais de 2026–2027 que caem na janela comercial coberta pelo
// tarifário V1. Não inclui feriado municipal de Luís Correia — falta a lista
// oficial, e feriado inventado vira diária cobrada a mais.
var feriadosTeste = []feriado{
	{data("2026-09-07"), "Independência do Brasil"},
	{data("2026-10-12"), "Nossa Senhora Aparecida"},
	{data("2026-11-02"), "Finados"},
	{data("2026-11-15"), "Proclamação da República"},
	{data("2026-12-25"), "Natal"},
	{data("2027-01-01"), "Confraternização Universal"},
	{data("2027-04-21"), "Tiradentes"},
}

// O nome carrega o ano de propósito: ele é a chave natural do seed
// (`UNIQUE (property_id, name)`), e o Réveillon do ano que vem precisa ser uma
// LINHA NOVA, não uma edição desta.
var periodosTeste = []periodo{
	{"Réveillon 2026/2027", "reveillon", data("2026-12-27"), data("2027-01-02")},
	{"Carnaval 2027", "carnaval", data("2027-02-05"), data("2027-02-10")},
	{"Alta temporada 2026/2027", "alta", data("2026-12-15"), data("2027-01-31")},
	{"Férias de julho 2027", "alta", data("2027-07-01"), data("2027-07-31")},
}
