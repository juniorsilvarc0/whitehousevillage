package main

import (
	"fmt"
	"os"
	"strings"
)

// catalogo é tudo que muda entre a casa REAL e o catálogo de TESTE: unidades,
// produtos, composição, tarifas, estadia mínima (geral e por produto), pacotes,
// feriados e períodos especiais. O resto do seed — propriedade, tipos de data,
// políticas, perfis, funil, contas — é igual nos dois e não mora aqui.
//
// São dois catálogos, e não um, por um motivo prático: ~33 arquivos de teste de
// integração dependem dos códigos do catálogo de demonstração (`apto-2s`,
// `cobertura`, `COB-01`…). O catálogo de teste é congelado — é o que o seed
// semeava até 03/10/2026, byte a byte — e o real é o que o dono definiu.
type catalogo struct {
	nome             string
	unidades         []unidade
	produtos         []produto
	composicao       []composicao
	tarifas          []tarifa
	minimoGeral      []minimo
	minimoPorProduto []minimoProduto
	pacotes          []pacote
	feriados         []feriado
	periodos         []periodo
}

const (
	catalogoReal  = "real"
	catalogoTeste = "teste"
)

// escolherCatalogo lê SEED_CATALOGO. O padrão é o REAL: é o que o stack, o
// compose e a VPS semeiam. Só a suíte de integração (`make it-seed`) pede o de
// teste. Valor desconhecido é erro, e não fallback: um erro de digitação no CI
// semearia o catálogo real num banco cujos testes esperam o de teste.
//
// Devolve o catálogo escolhido e o OUTRO — cujos itens que não estiverem no
// escolhido são desativados, para um banco nunca ficar com os dois vendendo.
func escolherCatalogo() (escolhido, outro *catalogo, err error) {
	v := strings.TrimSpace(os.Getenv("SEED_CATALOGO"))
	switch v {
	case "", catalogoReal:
		return &catalogoDaCasa, &catalogoDeTeste, nil
	case catalogoTeste:
		return &catalogoDeTeste, &catalogoDaCasa, nil
	default:
		return nil, nil, fmt.Errorf("SEED_CATALOGO=%q inválido: use %q (padrão) ou %q", v, catalogoReal, catalogoTeste)
	}
}

// codigos extrai os códigos de produto do catálogo.
func (c *catalogo) codigosDeProduto() []string {
	return coluna(c.produtos, func(p produto) string { return p.codigo })
}

func (c *catalogo) codigosDeUnidade() []string {
	return coluna(c.unidades, func(u unidade) string { return u.codigo })
}

// unidade é o que é ocupado e limpo — o que a constraint de sobreposição
// protege. Sem unidade nominal não existe defesa contra overbooking.
type unidade struct {
	codigo string
	nome   string
	ordem  int32
}

// produto é o que se vende. Separado da unidade física de propósito: a Completa
// é um produto que consome todas as unidades, não uma unidade a mais.
type produto struct {
	codigo     string
	nome       string  // nome interno, o do painel
	publico    *string // nome de vitrine (unit_types.public_name); nil = usa nome
	capacidade int32
	consome    string // one_member | all_members
	limpeza    int64  // reais; convertidos em centavos na inserção
	ordem      int32
}

// composicao liga produto → unidade. É daqui que nasce a exclusividade
// bidirecional: vender a Completa insere uma linha em stay_blocks por unidade,
// e qualquer unidade ocupada faz a inserção estourar 23P01 → 409 DATE_CONFLICT.
type composicao struct{ produto, unidade string }

// sobConsulta marca, na matriz de tarifas, o tipo de data SEM diária: o produto
// não tem preço de tabela ali e a linha em `rates` não é inserida. Zero é
// seguro como sentinela porque `rates.amount_cents` tem CHECK (> 0) — nenhuma
// tarifa real pode valer zero.
const sobConsulta int64 = 0

// tarifa é uma linha do tarifário, em REAIS, na ordem de `ordemDosTipos`.
//
// A spec escreve os valores em reais (850 = R$ 850,00 a diária) e o banco guarda
// centavos; a conversão acontece num lugar só, no `reais()`. Gravar 850 direto
// num campo `_cents` seria vender a diária por R$ 8,50.
type tarifa struct {
	produto string
	valores [6]int64
}

// minimo é a estadia mínima GERAL de um tipo de data. Vale o MAIOR mínimo entre
// as noites da estadia; quem aplica a regra é o domínio, aqui é só o dado.
type minimo struct {
	tipo   string
	noites int32
}

// minimoProduto sobrepõe a regra geral para um produto (unit_type_min_nights).
type minimoProduto struct {
	produto string
	tipo    string
	noites  int32
}

// pacote é preço por duração (rate_packages), em REAIS. `tipos` precisa estar
// sempre na mesma ordem: a UNIQUE da tabela compara o array como está escrito.
type pacote struct {
	produto string
	noites  int32
	tipos   []string
	total   int64
}

func (p pacote) chave() string {
	return fmt.Sprintf("%s|%d|%s", p.produto, p.noites, strings.Join(p.tipos, ","))
}
