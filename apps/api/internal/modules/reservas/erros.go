package reservas

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

// Os códigos de erro que o contrato da Fase 1 declara para este módulo e que
// `internal/platform/apperr` ainda não expõe.
//
// Por que montados aqui e não lá: `apperr` é pacote de plataforma e quatro
// módulos foram escritos em paralelo nesta rodada — `inventario` e `tarifario`
// chegaram à mesma solução pelo mesmo motivo. Quatro edições simultâneas do
// mesmo arquivo é a colisão que a divisão por pasta existe para evitar. O que o
// código consome é o SÍMBOLO; quando o tech-lead acrescentar as constantes ao
// `apperr`, estas linhas viram alias e nada mais muda.
//
// O status sai de um erro já definido (as funções `With*` devolvem CÓPIA, então
// nada do pacote apperr é mutado) e só o `Code` é reescrito — é o campo público
// e estável que o painel consome.
var (
	// EstadoInvalido — transição fora da máquina de estados.
	// `details.status` traz o estado atual e `details.allowed` os aceitos.
	EstadoInvalido = conflito("INVALID_STATE_TRANSITION",
		"A reserva não está num estado que permita esta ação.")

	// NaoCancelavel — /cancel sobre reserva já encerrada.
	NaoCancelavel = conflito("RESERVATION_NOT_CANCELLABLE",
		"Esta reserva já foi encerrada e não pode ser cancelada.")

	// UnidadeIndisponivel — /reassign-unit para unidade ocupada. NÃO é
	// DATE_CONFLICT de propósito: a estadia não está em disputa, só a unidade —
	// e a reserva continua exatamente onde estava.
	UnidadeIndisponivel = conflito("UNIT_NOT_AVAILABLE",
		"A unidade de destino está ocupada nesse período.")

	// LimiteDeHold — /extend-hold além do limite da política.
	LimiteDeHold = conflito("HOLD_LIMIT_REACHED",
		"Limite de extensões da pré-reserva atingido.")
)

// ComposicaoIncompleta — o produto não tem, ATIVAS, todas as unidades que a
// composição declara. É o código do CRÍTICO 1.
//
// POR QUE 422 E NÃO 409: no 409 a data está em disputa, e a ação do operador é
// tentar outra data. Aqui não há disputa nenhuma — a White House Completa vende
// oito apartamentos e um deles está em manutenção. NENHUMA data resolve, e
// mandar o operador procurar outra semana seria mandá-lo procurar para sempre.
// O que falta é configuração de inventário, e quem age é a gestão.
//
// Vale também para produto `one_member` sem nenhuma unidade ativa: o motivo é o
// mesmo, e o `details` diz qual é.
var ComposicaoIncompleta = invalido("COMPOSITION_INCOMPLETE",
	"O produto não tem todas as unidades da composição ativas.")

// composicaoIncompleta monta o 422 com o que a tela precisa listar: qual
// produto, quantas unidades ele promete, quantas estão de pé e QUAIS faltam —
// sem os códigos, o operador não sabe o que reativar.
func composicaoIncompleta(codigo string, declaradas, ativas int, faltando []string) error {
	if faltando == nil {
		faltando = []string{}
	}
	return ComposicaoIncompleta.WithDetails(map[string]any{
		"unit_type_code":     codigo,
		"expected_units":     declaradas,
		"active_units":       ativas,
		"missing_unit_codes": faltando,
	})
}

func conflito(code, mensagem string) *apperr.Error {
	e := apperr.DateConflict.WithMessage(mensagem)
	e.Code = code
	return e
}

// invalido monta um 422 com Code próprio. `apperr.Validation` fixa
// VALIDATION_ERROR, e é o Code que o painel consome para escolher a tela: um
// erro de CAMPO ele grude no input, e COMPOSITION_INCOMPLETE não é erro de
// campo nenhum — é a venda inteira que não pode sair, e a ação é da gestão de
// inventário.
func invalido(code, mensagem string) *apperr.Error {
	e := apperr.Validation(nil).WithMessage(mensagem)
	e.Code = code
	return e
}

// transicaoInvalida monta o 409 com o contexto que a tela precisa para explicar
// o "não" ao operador: em que estado a reserva está e para onde ela poderia ir.
func transicaoInvalida(atual string, permitidos []string) error {
	return EstadoInvalido.WithDetails(map[string]any{
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
