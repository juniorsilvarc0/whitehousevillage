package reservas

import "sort"

// Máquina de estados da reserva.
//
//	quote → hold → confirmed → checked_in → checked_out → closed
//	          │        │            │
//	          │        │            └─────────────► no_show
//	          ├────────┴──────────────────────────► cancelled
//	          └── hold vencido (job) ─────────────► expired
//
// POR QUE ESTE ARQUIVO NÃO ESTÁ EM internal/domain: a regra 1 do CLAUDE.md
// manda a regra de negócio morar lá, e esta é regra de negócio. Não escrevi lá
// porque `internal/domain` não é pasta deste agente nesta rodada e uma edição
// paralela do mesmo pacote é exatamente a colisão que a divisão por pasta
// existe para evitar. O arquivo é PURO de propósito — sem SQL, sem HTTP, sem
// relógio — justamente para poder ser movido para `internal/domain/booking`
// depois sem tocar em mais nada. Ver relatório.
const (
	EstadoQuote      = "quote"
	EstadoHold       = "hold"
	EstadoConfirmada = "confirmed"
	EstadoCheckIn    = "checked_in"
	EstadoCheckOut   = "checked_out"
	EstadoFechada    = "closed"
	EstadoCancelada  = "cancelled"
	EstadoExpirada   = "expired"
	EstadoNoShow     = "no_show"
)

// transicoes é a máquina como DADO: origem → destinos legítimos.
//
// `closed` fica sem origem editável na Fase 1 porque é acerto financeiro, e
// `expired` só é alcançado pelo job — nenhuma rota leva a ele, o que é a razão
// de ele aparecer como destino de `hold` mas não ter endpoint.
var transicoes = map[string][]string{
	EstadoQuote:      {EstadoHold, EstadoCancelada},
	EstadoHold:       {EstadoConfirmada, EstadoCancelada, EstadoExpirada},
	EstadoConfirmada: {EstadoCheckIn, EstadoCancelada, EstadoNoShow},
	EstadoCheckIn:    {EstadoCheckOut, EstadoNoShow, EstadoCancelada},
	EstadoCheckOut:   {EstadoFechada},
	EstadoFechada:    nil,
	EstadoCancelada:  nil,
	EstadoExpirada:   nil,
	EstadoNoShow:     nil,
}

// bloqueiam são os estados COMERCIAIS em que a reserva ocupa o calendário.
//
// Atenção ao que este mapa NÃO é: a ocupação de verdade é o `status` das linhas
// de `stay_blocks`, que é o que a constraint `stay_no_overlap` enxerga. Este
// mapa é a visão comercial equivalente, usada para recusar ações que só fazem
// sentido com calendário bloqueado (realocar unidade, por exemplo).
var bloqueiam = map[string]bool{
	EstadoHold:       true,
	EstadoConfirmada: true,
	EstadoCheckIn:    true,
}

// naoCancelaveis são os estados terminais: já encerraram a relação comercial,
// com ou sem estadia. Cancelar por cima reescreveria um fato consumado.
var naoCancelaveis = map[string]bool{
	EstadoCancelada: true,
	EstadoExpirada:  true,
	EstadoNoShow:    true,
	EstadoCheckOut:  true,
	EstadoFechada:   true,
}

// EstadoConhecido diz se o texto é um dos nove estados do contrato.
func EstadoConhecido(e string) bool {
	_, ok := transicoes[e]
	return ok
}

// PodeTransitar responde se `de → para` é uma aresta da máquina.
func PodeTransitar(de, para string) bool {
	for _, d := range transicoes[de] {
		if d == para {
			return true
		}
	}
	return false
}

// DestinosDe devolve os estados alcançáveis a partir de `de`, ordenados — é o
// `details.allowed` do erro INVALID_STATE_TRANSITION.
func DestinosDe(de string) []string {
	out := append([]string(nil), transicoes[de]...)
	sort.Strings(out)
	return out
}

// OrigensDe devolve os estados a partir dos quais `para` é alcançável. É o que
// a mensagem de erro precisa dizer: "só reserva `confirmed` faz check-in".
func OrigensDe(para string) []string {
	var out []string
	for de, destinos := range transicoes {
		for _, d := range destinos {
			if d == para {
				out = append(out, de)
			}
		}
	}
	sort.Strings(out)
	return out
}

// BloqueiaCalendario diz se o estado comercial ocupa data.
func BloqueiaCalendario(e string) bool { return bloqueiam[e] }

// Cancelavel diz se /cancel ainda tem efeito sobre a reserva.
func Cancelavel(e string) bool { return EstadoConhecido(e) && !naoCancelaveis[e] }

// EstadosCancelaveis é a lista para `details.allowed`.
func EstadosCancelaveis() []string {
	var out []string
	for e := range transicoes {
		if Cancelavel(e) {
			out = append(out, e)
		}
	}
	sort.Strings(out)
	return out
}

// StatusDoBlocoPara traduz o estado comercial no status que as linhas de
// `stay_blocks` devem ter.
//
// `checked_in` mapeia para `confirmed`, e não para um status próprio: o que a
// constraint precisa saber é apenas "ocupa ou não ocupa", e hóspede dentro do
// apartamento ocupa exatamente como reserva paga ocupa.
//
// `checked_out` e `closed` mapeiam para `completed`, e NÃO para `cancelled`.
// Os dois liberam a data igual — `completed` está fora do `WHERE` da constraint
// —, mas só um deles diz a verdade sobre o que aconteceu. Com `cancelled`, a
// estadia consumada ficava indistinguível da venda perdida, e nenhuma consulta
// separava receita realizada de cancelamento (ver o comentário de BlocoConcluido).
//
// `no_show` continua em `cancelled` de propósito: quem não apareceu não gerou
// estadia nenhuma. É venda perdida, com retenção — não ocupação.
func StatusDoBlocoPara(estado string) string {
	switch estado {
	case EstadoHold:
		return BlocoHold
	case EstadoConfirmada, EstadoCheckIn:
		return BlocoConfirmado
	case EstadoCheckOut, EstadoFechada:
		return BlocoConcluido
	case EstadoExpirada:
		return BlocoExpirado
	default:
		// cancelled, no_show e quote: a data volta ao estoque vendável e o
		// registro da venda que não aconteceu fica na linha.
		return BlocoCancelado
	}
}
