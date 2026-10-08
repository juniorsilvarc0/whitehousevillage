package bens

import (
	"testing"

	"github.com/google/uuid"
)

func ptr[T any](v T) *T { return &v }

// Pendente (NULL) e "contei zero" são coisas diferentes, e o rodapé tem de
// separá-las: zero é contado E divergente; NULL é só pendente.
func TestProgressoSeparaPendenteDeContadoEmZero(t *testing.T) {
	var p Progresso
	p.somar(12, nil)    // pendente
	p.somar(12, ptr(0)) // contei e não achei nenhum
	p.somar(5, ptr(5))  // bate
	p.somar(2, ptr(4))  // sobra 2

	want := Progresso{Linhas: 4, Contadas: 3, Pendentes: 1, Divergentes: 2, Faltas: 12, Sobras: 2}
	if p != want {
		t.Fatalf("progresso = %+v, esperado %+v", p, want)
	}
}

func TestDiferencaNulaEnquantoPendente(t *testing.T) {
	if d := diferenca(12, nil); d != nil {
		t.Fatalf("pendente deveria ter diff nulo, veio %d", *d)
	}
	if d := diferenca(12, ptr(9)); d == nil || *d != -3 {
		t.Fatalf("diff de 9 contra 12 deveria ser -3, veio %v", d)
	}
}

// A perda só existe com FALTA e custo cotado; sobra não é perda, e "não
// cotado" não vira zero.
func TestApurarValoraSoAFaltaCotada(t *testing.T) {
	falta := apurar(linhaApurada{QtdEsperada: 12, QtdContada: 9, Custo: ptr[int64](1890)})
	if falta.Falta != 3 || falta.Divergencia.Diferenca != -3 {
		t.Fatalf("falta = %d, diff = %d; esperado 3 e -3", falta.Falta, falta.Divergencia.Diferenca)
	}
	if falta.Divergencia.PerdaCents == nil || *falta.Divergencia.PerdaCents != 5670 {
		t.Fatalf("perda de 3 × R$ 18,90 deveria ser 5670 centavos, veio %v", falta.Divergencia.PerdaCents)
	}

	semCusto := apurar(linhaApurada{QtdEsperada: 12, QtdContada: 9})
	if semCusto.Divergencia.PerdaCents != nil {
		t.Fatalf("bem sem custo cotado não pode ter perda (nem zero), veio %d", *semCusto.Divergencia.PerdaCents)
	}

	sobra := apurar(linhaApurada{QtdEsperada: 12, QtdContada: 14, Custo: ptr[int64](1890)})
	if sobra.Falta != 0 || sobra.Divergencia.PerdaCents != nil {
		t.Fatalf("sobra não abre avaria nem é perda: falta=%d perda=%v", sobra.Falta, sobra.Divergencia.PerdaCents)
	}
}

func TestCustoTotalEmCentavos(t *testing.T) {
	if v := custoTotal(3, ptr[int64](1890)); v == nil || *v != 5670 {
		t.Fatalf("3 × 1890 = 5670, veio %v", v)
	}
	if v := custoTotal(3, nil); v != nil {
		t.Fatalf("sem custo cotado o total é nulo, veio %d", *v)
	}
}

// Bens DISTINTOS (o mesmo prato em dois cômodos conta uma vez), custo só dos
// cotados e `uncosted_items` dizendo quantos ficaram fora.
func TestTotalizarContaBemDistintoESeparaONaoCotado(t *testing.T) {
	prato, taca, toalha := uuid.New(), uuid.New(), uuid.New()
	ambientes := []AmbienteDoInventario{
		{Ambiente: Ambiente{AvariasAbertas: 1}, Bens: []Colocacao{
			{BemID: prato, QtdEsperada: 12, custo: ptr[int64](1000)},
			{BemID: taca, QtdEsperada: 6},
		}},
		{Ambiente: Ambiente{AvariasAbertas: 2}, Bens: []Colocacao{
			{BemID: prato, QtdEsperada: 4, custo: ptr[int64](1000)},
			{BemID: toalha, QtdEsperada: 3, custo: ptr[int64](2500)},
		}},
	}
	tot := totalizar(ambientes)
	if tot.Ambientes != 2 || tot.Bens != 3 || tot.QtdEsperada != 25 || tot.AvariasAbertas != 3 || tot.BensSemCusto != 1 {
		t.Fatalf("totais = %+v", tot)
	}
	if tot.CustoDeReposicaoCents == nil || *tot.CustoDeReposicaoCents != 16*1000+3*2500 {
		t.Fatalf("custo = %v, esperado %d", tot.CustoDeReposicaoCents, 16*1000+3*2500)
	}

	if vazio := totalizar(nil); vazio.CustoDeReposicaoCents != nil {
		t.Fatalf("sem nada cotado o custo é nulo, veio %d", *vazio.CustoDeReposicaoCents)
	}
}

// A cópia só ACRESCENTA: cômodo casado (pelo `code`, ou pelo `name` sem `code`
// igual) é reaproveitado, colocação que já existe é mantida com a quantidade do
// destino e vai para `kept`, e o cômodo novo herda o `code` da origem.
func TestPlanoDaCopiaSoAcrescenta(t *testing.T) {
	prato, taca := uuid.New(), uuid.New()
	origemAmb := []ambienteGravado{
		{Codigo: "cozinha", Nome: "Cozinha", Tipo: "cozinha"},
		{Codigo: "quarto-1", Nome: "Quarto 1", Tipo: "quarto", Ordem: 2},
	}
	destinoAmb := []ambienteGravado{{Codigo: "cozinha", Nome: "Cozinha", Tipo: "sala"}}
	origemCol := []colocacaoDaCopia{
		{AmbienteCodigo: "cozinha", BemID: prato, BemNome: "Prato", Qtd: 12},
		{AmbienteCodigo: "cozinha", BemID: taca, BemNome: "Taça", Qtd: 6},
		{AmbienteCodigo: "quarto-1", BemID: taca, BemNome: "Taça", Qtd: 2},
	}
	destinoCol := []colocacaoDaCopia{{AmbienteCodigo: "cozinha", BemID: prato, BemNome: "Prato", Qtd: 8}}

	p := planejarCopia(origemAmb, destinoAmb, origemCol, destinoCol, false)

	if len(p.Ambientes) != 1 || p.Ambientes[0].Nome != "Quarto 1" || p.Ambientes[0].Codigo != "quarto-1" || p.Ambientes[0].Ordem != 2 {
		t.Fatalf("só o Quarto 1 nasce, com o code e a ordem da origem: %+v", p.Ambientes)
	}
	if len(p.Colocacoes) != 2 || p.Colocacoes[1].AmbienteCodigo != "quarto-1" {
		t.Fatalf("nascem a taça da cozinha e a do quarto: %+v", p.Colocacoes)
	}
	if len(p.Mantidas) != 1 || p.Mantidas[0].QtdAtual != 8 || p.Mantidas[0].QtdOrigem != 12 {
		t.Fatalf("os 8 pratos do destino ficam, lado a lado com os 12 da origem: %+v", p.Mantidas)
	}

	// Rodar de novo, com o destino já copiado, não cria nada.
	segunda := planejarCopia(origemAmb,
		append(destinoAmb, ambienteGravado{Codigo: "quarto-1", Nome: "Quarto 1"}), origemCol,
		append(destinoCol,
			colocacaoDaCopia{AmbienteCodigo: "cozinha", BemID: taca, Qtd: 6},
			colocacaoDaCopia{AmbienteCodigo: "quarto-1", BemID: taca, Qtd: 2}), false)
	if len(segunda.Ambientes) != 0 || len(segunda.Colocacoes) != 0 || len(segunda.Mantidas) != 3 {
		t.Fatalf("a segunda passada deveria só manter: %+v", segunda)
	}

	soComodos := planejarCopia(origemAmb, destinoAmb, origemCol, destinoCol, true)
	if len(soComodos.Colocacoes) != 0 || len(soComodos.Mantidas) != 0 || len(soComodos.Ambientes) != 1 {
		t.Fatalf("rooms_only copia só a planta: %+v", soComodos)
	}
}

// O cômodo renomeado no destino continua sendo o mesmo: a cópia o reencontra
// pelo `code` e não cria um fantasma com o nome antigo.
func TestPlanoDaCopiaCasaPeloCodigoDepoisDoRename(t *testing.T) {
	cama := uuid.New()
	origem := []ambienteGravado{{Codigo: "quarto-grande", Nome: "Quarto grande", Tipo: "quarto"}}
	destino := []ambienteGravado{{Codigo: "quarto-grande", Nome: "Suíte Master", Tipo: "quarto"}}
	p := planejarCopia(origem, destino,
		[]colocacaoDaCopia{{AmbienteCodigo: "quarto-grande", BemID: cama, BemNome: "Cama", Qtd: 1}}, nil, false)
	if len(p.Ambientes) != 0 {
		t.Fatalf("casado pelo code, nenhum cômodo nasce: %+v", p.Ambientes)
	}
	if len(p.Colocacoes) != 1 || p.Colocacoes[0].AmbienteNome != "Suíte Master" {
		t.Fatalf("a colocação vai para o cômodo de destino, com o nome de lá: %+v", p.Colocacoes)
	}

	// Sem code igual, casa pelo nome — e nunca cria o segundo de mesmo nome.
	destinoAntigo := []ambienteGravado{{Codigo: "quarto-grande-2", Nome: "Quarto grande"}}
	if q := planejarCopia(origem, destinoAntigo, nil, nil, true); len(q.Ambientes) != 0 {
		t.Fatalf("sem code igual, casa pelo name: %+v", q.Ambientes)
	}
}

func TestChaveDaColocacaoIdaEVolta(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	ra, rb, ok := LerChaveDaColocacao(ChaveDaColocacao(a, b))
	if !ok || ra != a || rb != b {
		t.Fatalf("a chave não volta: %v %v %v", ra, rb, ok)
	}
	for _, ruim := range []string{"", a.String(), a.String() + "-" + b.String(), "x_" + b.String(), a.String() + "_"} {
		if _, _, ok := LerChaveDaColocacao(ruim); ok {
			t.Errorf("chave %q deveria ser recusada", ruim)
		}
	}
}
