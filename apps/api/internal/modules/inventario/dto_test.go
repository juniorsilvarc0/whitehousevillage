package inventario

import (
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// A regra que este arquivo protege: composição inválida é recusada ANTES de
// chegar ao banco, com o campo apontado. Produto sem composição é vendável e
// não consome unidade nenhuma — overbooking silencioso, porque a venda nem
// chega a inserir linha em stay_blocks para a constraint recusar.
func TestComposicaoVaziaEhRecusada(t *testing.T) {
	erros := ComposicaoEntrada{UnidadeIDs: nil}.Validar()

	if _, tem := erros["unit_ids"]; !tem {
		t.Fatalf("lista vazia deveria ser recusada; erros = %v", erros)
	}
}

func TestComposicaoRecusaUnidadeRepetida(t *testing.T) {
	id := uuid.New()
	erros := ComposicaoEntrada{UnidadeIDs: []uuid.UUID{id, uuid.New(), id}}.Validar()

	// O índice do repetido é o que a tela precisa para marcar a linha certa.
	if _, tem := erros["unit_ids[2]"]; !tem {
		t.Fatalf("unidade repetida deveria ser apontada por índice; erros = %v", erros)
	}
}

func TestComposicaoValidaNaoAcusaNada(t *testing.T) {
	erros := ComposicaoEntrada{UnidadeIDs: []uuid.UUID{uuid.New(), uuid.New()}}.Validar()

	if len(erros) > 0 {
		t.Fatalf("composição válida não deveria acusar erro; erros = %v", erros)
	}
}

func TestProdutoEntradaRecusaConsumoDesconhecido(t *testing.T) {
	c := ProdutoEntrada{Codigo: "COMPLETA", Nome: "White House Completa",
		Capacidade: novo(24), Consome: "todas"}
	c.Normalizar()

	if _, tem := c.Validar()["consumes"]; !tem {
		t.Fatal("consumes fora do enum deveria ser recusado")
	}
}

// A capacidade da Completa é DECLARADA (24), não somada (daria 40). O DTO só
// exige que seja positiva — não existe, em lugar nenhum do módulo, cálculo que
// a derive da composição.
func TestProdutoEntradaAceitaCapacidadeDeclarada(t *testing.T) {
	c := ProdutoEntrada{Codigo: "completa", Nome: "White House Completa",
		Capacidade: novo(24), Consome: ConsomeTodosMembros}
	c.Normalizar()

	if erros := c.Validar(); len(erros) > 0 {
		t.Fatalf("capacidade declarada deveria passar; erros = %v", erros)
	}
}

func TestProdutoEntradaComPadroesAplicaOsDefaultsDoContrato(t *testing.T) {
	c := ProdutoEntrada{Codigo: "ap2s", Nome: "Apartamento 2 Suítes",
		Capacidade: novo(6), Consome: ConsomeUmMembro}.ComPadroes()

	if *c.TaxaDeLimpezaCents != 0 || *c.Ordem != 0 || !*c.Ativo {
		t.Fatalf("PUT sem opcionais deveria voltar ao padrão do schema; %+v", c)
	}
}

// PATCH tem três estados, e `null` num campo NOT NULL precisa virar 422
// legível — não uma violação 23502 traduzida no meio da transação.
func TestProdutoAtualizarRecusaNuloEmCampoObrigatorio(t *testing.T) {
	a := ProdutoAtualizar{
		Nome:       httpx.Nulo[string](),
		Capacidade: httpx.Nulo[int](),
		Ativo:      httpx.Nulo[bool](),
	}
	a.Normalizar()

	erros := a.Validar()
	for _, campo := range []string{"name", "capacity", "active"} {
		if _, tem := erros[campo]; !tem {
			t.Errorf("%s = null deveria ser recusado; erros = %v", campo, erros)
		}
	}
}

// `description` é anulável na coluna: `null` ali é pedido legítimo de limpar.
func TestProdutoAtualizarAceitaNuloEmCampoAnulavel(t *testing.T) {
	a := ProdutoAtualizar{Descricao: httpx.Nulo[string]()}
	a.Normalizar()

	if erros := a.Validar(); len(erros) > 0 {
		t.Fatalf("description = null deveria ser aceito; erros = %v", erros)
	}
}

// Campo ausente não é campo nulo: o PATCH vazio não muda nada e não acusa nada.
func TestProdutoAtualizarVazioNaoAcusaNada(t *testing.T) {
	var a ProdutoAtualizar
	a.Normalizar()

	if erros := a.Validar(); len(erros) > 0 {
		t.Fatalf("PATCH sem campos não deveria acusar erro; erros = %v", erros)
	}
}

func TestUnidadeEntradaRecusaCodigoComEspaco(t *testing.T) {
	c := UnidadeEntrada{Codigo: "AP 01", Nome: "Apartamento 201"}
	c.Normalizar()

	if _, tem := c.Validar()["code"]; !tem {
		t.Fatal("código com espaço deveria ser recusado: é a chave de ordenação das inserções em lote")
	}
}

// String em branco e NULL significam a mesma coisa numa coluna anulável.
func TestUnidadeAtualizarTrataBrancoComoLimpar(t *testing.T) {
	a := UnidadeAtualizar{Andar: httpx.De("   ")}
	a.Normalizar()

	if !a.Andar.DeveLimpar() {
		t.Fatal("floor em branco deveria virar pedido de limpar")
	}
}

func TestPropriedadeAtualizarRecusaFusoForaDeAmerica(t *testing.T) {
	a := PropriedadeAtualizar{Fuso: httpx.De("Europe/Lisbon")}
	a.Normalizar()

	if _, tem := a.Validar()["timezone"]; !tem {
		t.Fatal("fuso fora da família America/ deveria ser recusado")
	}
}

func TestPropriedadeAtualizarAceitaFusoDaCasa(t *testing.T) {
	a := PropriedadeAtualizar{Fuso: httpx.De("America/Fortaleza")}
	a.Normalizar()

	if erros := a.Validar(); len(erros) > 0 {
		t.Fatalf("America/Fortaleza deveria passar; erros = %v", erros)
	}
}
