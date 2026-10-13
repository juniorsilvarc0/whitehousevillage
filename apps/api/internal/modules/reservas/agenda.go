package reservas

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/agenda"
)

// A reserva move a agenda operacional (F2-20): confirmar põe na agenda o
// check-in, o check-out e a limpeza; cancelar os cancela; registrar a entrada
// e a saída os conclui; remarcar é cancelar a original e confirmar a nova;
// realocar leva junto o evento manual preso à unidade que saiu.
//
// As interfaces moram AQUI, do lado de quem chama: a reserva diz o que precisa
// que aconteça a cada passo, e a agenda (`agenda.Ligacao`) implementa. Toda
// chamada acontece NA MESMA TRANSAÇÃO do passo da reserva — o contexto a
// carrega —, de modo que a reserva confirmada sem agenda, ou a agenda de uma
// confirmação que não aconteceu, não existem.
//
// Nenhuma venda, cancelamento, estadia ou realocação é recusada por causa da
// agenda: os efeitos não conferem conflito, e o conflito que o passo cria
// aparece na leitura da agenda (`has_unit_conflict`) — inclusive o do evento
// manual que, na realocação, não cabe na unidade nova e segue a reserva sem
// unidade própria. O erro que voltar daqui é defeito (500) ou contenção que o
// TxManager repete.

// EfeitoDaConfirmacao é o que acontece quando a reserva é confirmada, na mesma
// transação do `/confirm`. A agenda é o primeiro; o financeiro (recebíveis e
// comissão, F2-12 e F2-13) acrescenta os seus com ComEfeitosDaConfirmacao,
// sem reabrir o `/confirm`.
type EfeitoDaConfirmacao interface {
	AoConfirmar(ctx context.Context, reserva uuid.UUID) error
}

// AgendaDaReserva é a agenda vista pela reserva — um método por passo.
type AgendaDaReserva interface {
	EfeitoDaConfirmacao
	// AoCancelar: `estavaHospedado` é a reserva cancelada (ou no-show) com o
	// hóspede dentro (`checked_in`).
	AoCancelar(ctx context.Context, reserva uuid.UUID, estavaHospedado bool) error
	// AoRegistrarEntrada e AoRegistrarSaida recebem o instante informado no
	// registro ou, sem ele, o `now()` do banco.
	AoRegistrarEntrada(ctx context.Context, reserva uuid.UUID, em time.Time) error
	AoRegistrarSaida(ctx context.Context, reserva uuid.UUID, em time.Time) error
	AoRealocar(ctx context.Context, reserva, de, para uuid.UUID) error
}

var _ AgendaDaReserva = (*agenda.Ligacao)(nil)

// ComEfeitosDaConfirmacao acrescenta efeitos ao `/confirm`, DEPOIS dos que já
// estão (a agenda é sempre o primeiro). É a porta do financeiro.
func (s *Servico) ComEfeitosDaConfirmacao(efeitos ...EfeitoDaConfirmacao) *Servico {
	s.efeitosDaConfirmacao = append(s.efeitosDaConfirmacao, efeitos...)
	return s
}

// confirmarEfeitos roda os efeitos da confirmação, em ordem, na transação do
// contexto. O primeiro erro derruba a confirmação inteira.
func (s *Servico) confirmarEfeitos(ctx context.Context, reserva uuid.UUID) error {
	for _, efeito := range s.efeitosDaConfirmacao {
		if err := efeito.AoConfirmar(ctx, reserva); err != nil {
			return err
		}
	}
	return nil
}
