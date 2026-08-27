package reservas

import (
	"testing"

	"github.com/google/uuid"
)

// candidata monta uma unidade de teste com folgas conhecidas.
func candidata(codigo string, antes, depois int) Candidata {
	return Candidata{ID: uuid.New(), Codigo: codigo, FolgaAntes: antes, FolgaDepois: depois}
}

func codigos(c []Candidata) []string {
	out := make([]string, 0, len(c))
	for _, u := range c {
		out = append(out, u.Codigo)
	}
	return out
}

func iguais(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// O caso que a spec §2 descreve: entre unidades livres, vence a que deixa menos
// sobra entre as reservas vizinhas.
func TestVenceAUnidadeQueEncaixaColada(t *testing.T) {
	ordem := OrdenarPorFragmentacao([]Candidata{
		candidata("AP-01", 5, 5), // sobra 10
		candidata("AP-02", 0, 0), // encaixe perfeito
		candidata("AP-03", 2, 1), // sobra 3
	})
	if got := codigos(ordem); !iguais(got, []string{"AP-02", "AP-03", "AP-01"}) {
		t.Fatalf("ordem = %v, esperado [AP-02 AP-03 AP-01]", got)
	}
}

// Unidade sem vizinho nenhum é a que MAIS fragmenta: ocupar o meio de um
// calendário limpo cria duas sobras onde não havia nenhuma.
func TestUnidadeVirgemFicaPorUltimo(t *testing.T) {
	ordem := OrdenarPorFragmentacao([]Candidata{
		candidata("SP-01", SemVizinho, SemVizinho),
		candidata("SP-02", 30, 45), // 75 dias de sobra ainda ganha da virgem
		candidata("SP-03", SemVizinho, 3),
	})
	if got := codigos(ordem); !iguais(got, []string{"SP-02", "SP-03", "SP-01"}) {
		t.Fatalf("ordem = %v, esperado [SP-02 SP-03 SP-01]", got)
	}
}

// Com o calendário vazio todas empatam — e é justamente aí que o desempate por
// código importa: sem ele, duas requisições idênticas alocariam apartamentos
// diferentes e o mesmo teste passaria e falharia em dias alternados.
func TestEmpateDesempataPorCodigoEEhDeterministico(t *testing.T) {
	entrada := []Candidata{
		candidata("AP-03", SemVizinho, SemVizinho),
		candidata("AP-01", SemVizinho, SemVizinho),
		candidata("AP-02", SemVizinho, SemVizinho),
	}
	esperado := []string{"AP-01", "AP-02", "AP-03"}

	for i := 0; i < 20; i++ {
		if got := codigos(OrdenarPorFragmentacao(entrada)); !iguais(got, esperado) {
			t.Fatalf("execução %d: ordem = %v, esperado %v", i, got, esperado)
		}
	}
}

func TestOrdenarNaoMutaAEntrada(t *testing.T) {
	entrada := []Candidata{candidata("Z", 0, 0), candidata("A", 9, 9)}
	original := codigos(entrada)

	OrdenarPorFragmentacao(entrada)
	if got := codigos(entrada); !iguais(got, original) {
		t.Fatalf("a entrada foi reordenada: %v", got)
	}
}

// A ordem de inserção da Completa é sempre `units.code`. Ordens divergentes
// entre transações causam impasse — e este teste é o que impede alguém de
// "otimizar" a inserção reaproveitando a ordem de fragmentação.
func TestIDsSaemSempreEmOrdemDeCodigo(t *testing.T) {
	a := candidata("COB-01", 0, 0)
	b := candidata("AP-01", 90, 90)
	c := candidata("SP-04", SemVizinho, SemVizinho)

	ids := IDsEmOrdemDeCodigo([]Candidata{a, b, c})
	if len(ids) != 3 || ids[0] != b.ID || ids[1] != a.ID || ids[2] != c.ID {
		t.Fatalf("ordem por código quebrada: %v", ids)
	}

	// A ordem de fragmentação (COB-01 primeiro) NÃO pode vazar para a inserção.
	porFragmentacao := IDsEmOrdemDeCodigo(OrdenarPorFragmentacao([]Candidata{a, b, c}))
	for i := range ids {
		if porFragmentacao[i] != ids[i] {
			t.Fatalf("a ordem de inserção mudou conforme a preferência: %v vs %v", porFragmentacao, ids)
		}
	}
}
