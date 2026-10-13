package reservas

import (
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/guests"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
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

// ─────────────────────────── Rooming list (F2-22) ───────────────────

// As três garantias do banco sobre `reservation_guests` (docs/db.md §5,
// migration 20261013180000), pelo NOME da restrição — é o nome que separa a
// recusa que o usuário provocou do defeito que só o nosso código provoca.
const (
	// restricaoDaPessoaNaLista é a chave primária `(reservation_id,
	// contact_id)`: a mesma pessoa uma vez só na lista. Sob concorrência é ela
	// quem garante, e o 23505 dela sai com o code da recusa do domínio
	// (CODE_IN_USE).
	restricaoDaPessoaNaLista = "reservation_guests_pkey"
	// restricaoDeUmTitular é o índice único parcial: no máximo um titular por
	// reserva. Não é adiável — por isso o plano da troca apaga o titular
	// anterior ANTES de gravar o novo.
	restricaoDeUmTitular = "reservation_guests_titular_idx"
	// restricaoDoTitularDaReserva é a FK ADIADA `(reservation_id,
	// lead_contact_id) → reservations(id, contact_id)`: a linha de titular é a
	// do `contact_id` da reserva. Trocar um sem o outro falha no COMMIT.
	restricaoDoTitularDaReserva = "reservation_guests_titular_fkey"
)

// mensagemPessoaNaLista é a frase do CODE_IN_USE da pessoa repetida — a mesma
// que `guests.CheckAdd` devolve, para a recusa do domínio e a da chave
// primária (a corrida que o domínio não viu) dizerem a mesma coisa.
const mensagemPessoaNaLista = "Esta pessoa já está na lista de hóspedes desta reserva."

// traduzirRegraDaLista leva a recusa de `internal/domain/guests` ao erro do
// contrato: o code e o status vêm do catálogo (`apperr.PorCodigo`), a frase e
// os `details` do domínio — a mesma tradução da agenda e da manutenção.
//
// O que não é `guests.RuleError` é defeito nosso, e sai 500: é o caso de
// `guests.ErrInvalidInput` (estado fora do vocabulário, número de hóspedes
// menor que 1), entrada que nenhum chamador correto produz — o banco já recusa
// as duas por CHECK, e o DTO, a segunda.
func traduzirRegraDaLista(err error) error {
	if err == nil {
		return nil
	}
	var regra *guests.RuleError
	if !errors.As(err, &regra) {
		return apperr.Internal.WithCause(err)
	}
	base, ok := apperr.PorCodigo(regra.Code)
	if !ok {
		return apperr.Internal.WithCause(err)
	}
	detalhes := make(map[string]any, len(regra.Details))
	for k, v := range regra.Details {
		detalhes[k] = v
	}
	return base.WithMessage(regra.Message).WithDetails(detalhes).WithCause(err)
}

// traduzirEscritaDaLista traduz o erro de uma instrução que grava em
// `reservation_guests`. `contato` é a pessoa que a instrução gravava.
//
//   - 23505 na chave primária → 409 CODE_IN_USE: a pessoa já está na lista. O
//     domínio já recusa com a lista travada em mãos; chegar aqui é a escrita
//     concorrente que passou por fora da trava da reserva, e a resposta é a
//     mesma;
//   - as duas garantias de titular → 500 (ver defeitoDaLista);
//   - o resto segue o caminho comum, `db.MapError`.
func traduzirEscritaDaLista(err error, contato uuid.UUID) error {
	switch {
	case err == nil:
		return nil
	case db.IsUniqueViolation(err, restricaoDaPessoaNaLista):
		return apperr.CodeInUse.
			WithMessage(mensagemPessoaNaLista).
			WithDetails(map[string]any{"contact_id": contato}).
			WithCause(err)
	case violouOTitular(err):
		return apperr.Internal.WithCause(err)
	}
	return db.MapError(err)
}

// defeitoDaLista corrige a resposta de uma TRANSAÇÃO que escreveu na lista ou
// trocou `reservations.contact_id`.
//
// Dois titulares na mesma lista (23505 em `reservation_guests_titular_idx`) e
// uma linha de titular que não é a do `contact_id` da reserva (23503 em
// `reservation_guests_titular_fkey`, que só dispara no COMMIT, por ser adiada)
// não são pedido inválido: nenhum corpo de requisição produz os dois, quem
// grava as duas pontas é o nosso código, na mesma transação. O caminho comum
// (`db.MapError`, chamado pelo TxManager no COMMIT) os devolveria como `422
// VALIDATION_ERROR` — e mandaria o operador procurar erro num formulário que
// estava certo. É 500, como as noites do orçamento que não fecham
// (`quote_nights_fecham_o_orcamento`): a causa fica no log, com a restrição.
//
// Aplicada ao retorno do `tx.Do` — é só ali que o erro do COMMIT aparece.
func defeitoDaLista(err error) error {
	if err != nil && violouOTitular(err) {
		return apperr.Internal.WithCause(err)
	}
	return err
}

func violouOTitular(err error) bool {
	return db.IsUniqueViolation(err, restricaoDeUmTitular) ||
		db.IsForeignKeyViolation(err, restricaoDoTitularDaReserva)
}
