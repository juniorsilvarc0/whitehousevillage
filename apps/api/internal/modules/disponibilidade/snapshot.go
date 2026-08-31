package disponibilidade

import (
	"context"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

// GANHAR TRANSCREVE O ORÇAMENTO; NÃO O REFAZ.
//
// `POST /crm/opportunities/{id}/win` cria a reserva pelo módulo `reservas`, e o
// caminho de criação dele pede o preço a ESTE módulo (`Servico.Orcar`) — que
// hoje roda o motor sobre a tabela de tarifas de HOJE. Para um ganho isso está
// errado por construção: a proposta é de três semanas atrás, e um `PUT /rates`
// no meio da negociação cobraria do hóspede um valor que ele nunca ouviu. O
// contrato é explícito — "o motor **não** roda de novo".
//
// FIXAR é o mecanismo que faz isso valer sem que o módulo vizinho mude: quem
// vai ganhar prende o snapshot no contexto e, na chamada que a criação da
// reserva faz logo em seguida, `Orcar` DEVOLVE o snapshot em vez de calcular.
// Os valores congelados seguem daí para `reservation_pricing`, as noites para
// `reservation_nights` e `rate_table_id`/`policy_version` para as colunas que os
// congelam — sem que uma linha de `reservas` precise saber que orçamento
// persistido existe.
//
// O contexto é o canal certo, e não um truque: é por ele que estes dois módulos
// já se coordenam hoje — a transação do ganho viaja no contexto (`db.From`), e é
// isso que faz "a reserva falhou, a oportunidade não fecha" ser uma propriedade
// da transação e não uma sequência de `if`.
//
// PARA O INTEGRADOR: no dia em que `internal/modules/reservas` puder ser
// editado, o lugar definitivo disto é um `ReservaCriar.QuoteID` (ou um
// `reservas.Servico.EmitirDoOrcamento`) que receba o snapshot como ARGUMENTO. A
// troca é local: apagar este arquivo, o ramo de três linhas no topo de `Orcar` e
// a chamada em `crm.ganhar`. Ver o relatório.

type chaveOrcamentoFixado struct{}

// ComOrcamentoFixado devolve um contexto em que `Orcar` deixa de calcular e
// passa a devolver este orçamento emitido.
//
// O escopo é a chamada: o contexto vive dentro de uma execução de `/win`, e a
// única chamada a `Orcar` que acontece lá dentro é a da criação da reserva.
func ComOrcamentoFixado(ctx context.Context, o OrcamentoSalvo) context.Context {
	return context.WithValue(ctx, chaveOrcamentoFixado{}, o)
}

func orcamentoFixado(ctx context.Context) (OrcamentoSalvo, bool) {
	o, ok := ctx.Value(chaveOrcamentoFixado{}).(OrcamentoSalvo)
	return o, ok
}

// conferirOFixado recusa usar o snapshot para um pedido que não é o dele.
//
// O snapshot só vale para a estadia que ele orçou. Devolver os valores de um
// orçamento de 3 noites para um pedido de 5 seria vender cinco noites pelo preço
// de três, em silêncio — e um erro interno alto e nomeado é infinitamente melhor
// que um preço errado que fecha a venda. Não é validação de negócio (nenhum
// cliente consegue provocar isto pela API): é a trava do mecanismo.
func conferirOFixado(fixo OrcamentoSalvo, e Entrada) error {
	divergencias := map[string]any{}
	if fixo.UnitTypeID != e.UnitTypeID {
		divergencias["unit_type_id"] = e.UnitTypeID.String()
	}
	if fixo.CheckIn != e.CheckIn.String() {
		divergencias["check_in"] = e.CheckIn.String()
	}
	if fixo.CheckOut != e.CheckOut.String() {
		divergencias["check_out"] = e.CheckOut.String()
	}
	if fixo.Hospedes != e.Hospedes {
		divergencias["guests_count"] = e.Hospedes
	}
	if fixo.DescontoPct != e.DescontoPct {
		divergencias["discount_pct"] = e.DescontoPct
	}
	if fixo.IsEvento != e.IsEvento {
		divergencias["is_event"] = e.IsEvento
	}
	if len(divergencias) == 0 {
		return nil
	}
	return apperr.Internal.
		WithMessage("O orçamento congelado não corresponde ao que está sendo vendido.").
		WithDetails(map[string]any{"quote_id": fixo.ID.String(), "divergencias": divergencias})
}
