package disponibilidade

import (
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

// validadePadraoEmDias é quanto tempo um orçamento emitido fica de pé quando
// quem emite não diz até quando.
//
// DÍVIDA NOMEADA, e está escrita no contrato (PedidoDeOrcamento.valid_until):
// "tudo que é regra comercial é dado versionado", e o lugar deste número é
// `commercial_policies.quote_validity_days`, ao lado de `hold_hours` e
// `balance_due_days`. A coluna não existe — e não pode ser criada por este
// agente (regra 3: migrations só pelo `db-migrations`), com o agravante de que
// `PublicarPoliticaComercial` copia coluna a coluna e uma coluna nova voltaria
// ao DEFAULT a cada versão publicada. Fica aqui, com nome, para não virar
// número mágico perdido no meio de uma expressão.
const validadePadraoEmDias = 7

// OrcamentoSalvo é o orçamento EMITIDO — schema `OrcamentoSalvo` do contrato.
//
// `Orcamento` embutido e SEM tag: os campos dele saem inline no JSON, que é
// exatamente o que o `allOf` do contrato descreve. Nenhum número aqui é
// recalculado na leitura — todos vêm de `quotes` e `quote_nights`.
type OrcamentoSalvo struct {
	Orcamento

	ID                     uuid.UUID  `json:"id"`
	ContactID              *uuid.UUID `json:"contact_id"`
	OportunidadeID         *uuid.UUID `json:"opportunity_id"`
	DonoID                 *uuid.UUID `json:"owner_id"`
	PoliticaCancelamentoID *uuid.UUID `json:"cancellation_policy_id"`
	ValidoAte              time.Time  `json:"valid_until"`

	// Vencido é DERIVADO — `valid_until` contra o "agora" do banco, que já vem
	// no fuso da casa. Não é coluna: um booleano gravado ficaria errado no
	// minuto seguinte, e quem o corrigisse seria um job que não precisa existir.
	Vencido bool `json:"expired"`

	ReservaID *uuid.UUID `json:"reservation_id"`
	CriadoEm  time.Time  `json:"created_at"`

	// ─── O PEDIDO, que o contrato não declara em `OrcamentoSalvo` ───
	//
	// `json:"-"` porque o schema do contrato não tem estes campos. Eles existem
	// aqui porque `/win` precisa deles para emitir a reserva a partir do
	// snapshot: sem `unit_type_id` e `guests_count` não há o que vender, e
	// derivá-los da resposta é impossível (as datas até saem de `nights`, o
	// produto e o número de hóspedes não saem de lugar nenhum).
	//
	// PARA O INTEGRADOR: o schema `OrcamentoSalvo` deveria declarar
	// `unit_type_id`, `check_in`, `check_out`, `guests_count` e `is_event` —
	// hoje o painel que reabre um orçamento não consegue dizer o que foi
	// orçado. Ver o relatório.
	UnitTypeID uuid.UUID `json:"-"`
	CheckIn    string    `json:"-"`
	CheckOut   string    `json:"-"`
	Hospedes   int       `json:"-"`
	IsEvento   bool      `json:"-"`
}

// rotuloDoTipo traduz o tipo de data para o texto que a tela mostra ao reabrir
// um orçamento emitido.
//
// O rótulo ORIGINAL ("Natal", "Réveillon 2026/2027") vive no calendário e NÃO é
// gravado em `quote_nights` — a coluna guarda o TIPO, não o nome do feriado, do
// mesmo jeito que `reservation_nights`. Rótulo genérico é honesto; buscar o nome
// do feriado no calendário de hoje seria recontar o passado com o calendário de
// agora, e um feriado revogado em janeiro mudaria o texto de um orçamento de
// dezembro.
//
// É gêmeo de `reservas.rotuloDoTipo` e não uma importação porque `reservas`
// importa ESTE pacote: trazê-lo de volta fecha um ciclo de compilação.
func rotuloDoTipo(t calendar.DateType) string {
	switch t {
	case calendar.Normal:
		return "Diária normal"
	case calendar.Weekend:
		return "Fim de semana"
	case calendar.Holiday:
		return "Feriado"
	case calendar.HighSeason:
		return "Alta temporada"
	case calendar.NewYear:
		return "Réveillon"
	case calendar.Carnival:
		return "Carnaval"
	default:
		return string(t)
	}
}

// agruparEmLinhas monta o "Fim de semana 2× R$ 4.800" a partir do snapshot.
//
// Agrupa o que FOI gravado e não recalcula nada: o valor unitário é o preço da
// primeira noite do grupo, que é o mesmo de todas porque a tarifa é por
// (produto, tipo de data). A ordem é a de aparição — a ordem em que o hóspede
// vive a estadia —, a mesma que `booking.Build` produz.
func agruparEmLinhas(noites []NoiteDoOrcamento) []LinhaDoOrcamento {
	out := make([]LinhaDoOrcamento, 0, 4)
	indice := map[calendar.DateType]int{}
	for _, n := range noites {
		if i, visto := indice[n.Tipo]; visto {
			out[i].Noites++
			out[i].Subtotal += n.Preco
			continue
		}
		indice[n.Tipo] = len(out)
		out = append(out, LinhaDoOrcamento{
			Tipo: n.Tipo, Rotulo: n.Rotulo, Noites: 1,
			Unitario: n.Preco, Subtotal: n.Preco,
		})
	}
	return out
}
