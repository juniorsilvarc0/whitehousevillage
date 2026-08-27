package reservas

import (
	"sort"

	"github.com/google/uuid"
)

// Alocação de unidade física — spec §2: "o sistema aloca automaticamente a
// unidade que MENOS FRAGMENTA o calendário (menor sobra entre reservas
// vizinhas), com desempate por código".
//
// Por que isso importa comercialmente: três Apartamentos 2 Suítes livres e uma
// estadia de 20 a 23. Se o sistema pegar sempre o AP-01, o calendário vira um
// queijo suíço — cada unidade com dois dias livres aqui e três ali, e nenhuma
// com uma janela grande o bastante para a próxima estadia de uma semana.
// Encaixar a estadia coladinha na ocupação vizinha preserva os blocos contínuos.
//
// ESTE ARQUIVO É PURO. Ele NÃO decide disponibilidade: a ordem que ele produz é
// uma PREFERÊNCIA, e quem recusa a data continua sendo a constraint
// `stay_no_overlap`. O service tenta inserir na ordem daqui e deixa o banco
// dizer não — nunca "consulta e depois insere achando que ainda está livre",
// que é o TOCTOU que a regra 2 do CLAUDE.md proíbe.

// SemVizinho marca o lado em que a unidade não tem ocupação nenhuma.
const SemVizinho = -1

// pesoDeQuemNaoTemVizinho é a folga atribuída a um lado sem vizinho.
//
// Grande de propósito, e maior que qualquer folga real (o calendário comercial
// não passa de alguns anos): unidade vazia dos dois lados é a que MAIS fragmenta
// — ocupar o meio de um calendário limpo cria duas sobras onde não havia
// nenhuma. Entre encaixar ao lado de uma reserva existente e abrir uma unidade
// virgem, a primeira vence sempre.
const pesoDeQuemNaoTemVizinho = 1_000_000

// Candidata é uma unidade que compõe o produto, com a distância até a ocupação
// vizinha em dias. O repositório calcula as folgas; a escolha acontece aqui.
type Candidata struct {
	ID     uuid.UUID
	Codigo string
	// FolgaAntes é `check_in − fim da ocupação anterior mais próxima`, em dias.
	// 0 significa encaixe perfeito (a estadia começa no dia em que a anterior
	// terminou — o back-to-back que o intervalo half-open permite).
	FolgaAntes int
	// FolgaDepois é `início da próxima ocupação − check_out`, em dias.
	FolgaDepois int
}

// fragmentacao é a sobra total que a estadia deixaria nesta unidade.
func (c Candidata) fragmentacao() int {
	return lado(c.FolgaAntes) + lado(c.FolgaDepois)
}

func lado(folga int) int {
	if folga < 0 {
		return pesoDeQuemNaoTemVizinho
	}
	return folga
}

// OrdenarPorFragmentacao devolve as candidatas na ordem de tentativa: menor
// sobra primeiro, desempate por `code`.
//
// O desempate por código não é enfeite. Ele torna a escolha DETERMINÍSTICA: com
// o calendário vazio (o caso mais comum no início da temporada) todas as
// candidatas empatam, e sem um critério estável duas requisições idênticas
// alocariam apartamentos diferentes — e o mesmo teste passaria e falharia em
// dias alternados.
func OrdenarPorFragmentacao(candidatas []Candidata) []Candidata {
	out := append([]Candidata(nil), candidatas...)
	sort.SliceStable(out, func(i, j int) bool {
		fi, fj := out[i].fragmentacao(), out[j].fragmentacao()
		if fi != fj {
			return fi < fj
		}
		return out[i].Codigo < out[j].Codigo
	})
	return out
}

// IDsEmOrdemDeCodigo devolve os ids ordenados por `units.code`.
//
// É a ordem de inserção da White House Completa, e não é preferência de estilo:
// duas transações inserindo subconjuntos das mesmas unidades em ordens opostas
// se travam mutuamente, e o Postgres só desfaz o nó depois de `deadlock_timeout`
// (docs/db.md §14, e o comentário da constraint na migration).
func IDsEmOrdemDeCodigo(candidatas []Candidata) []uuid.UUID {
	out := append([]Candidata(nil), candidatas...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Codigo < out[j].Codigo })

	ids := make([]uuid.UUID, 0, len(out))
	for _, c := range out {
		ids = append(ids, c.ID)
	}
	return ids
}
