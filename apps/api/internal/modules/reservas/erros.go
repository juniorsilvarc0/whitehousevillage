package reservas

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

// Os códigos de erro deste módulo (INVALID_STATE_TRANSITION,
// RESERVATION_NOT_CANCELLABLE, UNIT_NOT_AVAILABLE, HOLD_LIMIT_REACHED,
// COMPOSITION_INCOMPLETE) moram em `internal/platform/apperr`, com o status e a
// frase padrão. Aqui ficam só os montadores que acrescentam o contexto que a
// tela precisa — o code e o status vêm de lá, sempre.

// mensagemTransicaoDaReserva é a frase do 409 de estado nas ações da reserva.
// Mais precisa que a padrão do apperr ("Transição de estado não permitida."),
// que serve também ao CRM; é a mesma frase que o módulo devolvia antes do F2-03.
const mensagemTransicaoDaReserva = "A reserva não está num estado que permita esta ação."

// composicaoIncompleta monta o 422 com o que a tela precisa listar: qual
// produto, quantas unidades ele promete, quantas estão de pé e QUAIS faltam —
// sem os códigos, o operador não sabe o que reativar.
func composicaoIncompleta(codigo string, declaradas, ativas int, faltando []string) error {
	if faltando == nil {
		faltando = []string{}
	}
	return apperr.CompositionIncomplete.WithDetails(map[string]any{
		"unit_type_code":     codigo,
		"expected_units":     declaradas,
		"active_units":       ativas,
		"missing_unit_codes": faltando,
	})
}

// transicaoInvalida monta o 409 com o contexto que a tela precisa para explicar
// o "não" ao operador: em que estado a reserva está e para onde ela poderia ir.
func transicaoInvalida(atual string, permitidos []string) error {
	return apperr.InvalidStateTransition.
		WithMessage(mensagemTransicaoDaReserva).
		WithDetails(map[string]any{
			"status":  atual,
			"allowed": permitidos,
		})
}

// sqlstateExclusao é o `exclusion_violation`: a constraint stay_no_overlap
// recusou a gravação porque a unidade já está ocupada no período.
const sqlstateExclusao = "23P01"

// ehSobreposicao diz se o erro é a recusa da constraint de exclusão.
//
// Existe separado de db.MapError porque o MESMO 23P01 tem duas leituras
// comerciais: em POST /reservations e POST /blocks ele é DATE_CONFLICT ("as
// datas acabaram de ser ocupadas"), e em /reassign-unit é UNIT_NOT_AVAILABLE
// ("a unidade de destino está ocupada"). Quem sabe a diferença é a operação,
// não a plataforma.
func ehSobreposicao(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == sqlstateExclusao
}

// conflitoDeData traduz o 23P01 preservando o que a constraint contou sobre o
// choque — é o `details.unit_code`/`details.period` que o contrato promete.
func conflitoDeData(err error, unidade, periodo string) error {
	detalhes := map[string]any{}
	if unidade != "" {
		detalhes["unit_code"] = unidade
	}
	if periodo != "" {
		detalhes["period"] = periodo
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		detalhes["constraint"] = pg.ConstraintName
	}
	return apperr.DateConflict.WithDetails(detalhes).WithCause(err)
}

// POR QUE NÃO HÁ TRADUÇÃO DE booking.RuleError AQUI: os erros do motor
// comercial (VALIDATION_ERROR, CAPACITY_EXCEEDED, MIN_STAY_NOT_MET,
// DISCOUNT_ABOVE_LIMIT, RATE_NOT_FOUND) já chegam traduzidos, porque quem chama
// o motor é o módulo `disponibilidade` — este módulo consome o orçamento pronto.
// Uma segunda tabela de tradução aqui divergiria da primeira no dia em que só
// uma fosse ajustada, e o front reage ao Code.
