package db

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

// Códigos SQLSTATE que interessam à tradução. Estão aqui nomeados porque
// "23P01" espalhado pelo código não diz nada a quem lê depois.
const (
	sqlstateExclusao      = "23P01" // exclusion_violation — sobreposição de datas
	sqlstateUnico         = "23505" // unique_violation
	sqlstateChaveEstrang  = "23503" // foreign_key_violation
	sqlstateCheck         = "23514" // check_violation
	sqlstateNotNull       = "23502" // not_null_violation
	sqlstateSerializacao  = "40001" // serialization_failure
	sqlstateDeadlock      = "40P01" // deadlock_detected
	sqlstateLockIndisponi = "55P03" // lock_not_available — estouro do lock_timeout
	sqlstateEntradaTexto  = "22P02" // invalid_text_representation (uuid malformado, p.ex.)
	sqlstateNumeroForaInt = "22003" // numeric_value_out_of_range
)

// MapError traduz o erro do Postgres para o vocabulário da API. Todo repositório
// devolve o erro por aqui — é o único lugar do sistema que sabe o que "23P01"
// significa.
//
// 23P01 vira DATE_CONFLICT porque a constraint de exclusão do stay_blocks é a
// defesa contra overbooking: quando ela dispara, o cliente precisa saber que a
// data foi ocupada, não ver um 500.
//
// 23505 sem tratamento específico vira VALIDATION_ERROR com o campo em details.
// O enum de erros do contrato não tem código genérico de conflito de unicidade —
// quem tem código próprio (EMAIL_IN_USE) decide pela constraint, com
// IsUniqueViolation, antes de chamar MapError.
func MapError(err error) error {
	if err == nil {
		return nil
	}

	// Cancelamento do cliente não é erro da aplicação: virar 500 polui o log de
	// erro com aba fechada no meio da requisição.
	if errors.Is(err, context.Canceled) {
		return err
	}
	var jaTraduzido *apperr.Error
	if errors.As(err, &jaTraduzido) {
		return jaTraduzido
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("Registro").WithCause(err)
	}

	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return apperr.Internal.WithCause(err)
	}

	switch pg.Code {
	case sqlstateExclusao:
		return apperr.DateConflict.WithCause(err).WithDetails(map[string]string{
			"constraint": pg.ConstraintName,
			"detail":     pg.Detail,
		})

	case sqlstateUnico:
		campo := campoDaConstraint(pg)
		return apperr.Validation(map[string]string{campo: "já está em uso."}).
			WithCause(err).
			WithDetails(map[string]any{
				campo:        "já está em uso.",
				"constraint": pg.ConstraintName,
			})

	case sqlstateChaveEstrang:
		campo := campoDaConstraint(pg)
		return apperr.Validation(map[string]string{campo: "referência inexistente."}).WithCause(err)

	case sqlstateCheck:
		return apperr.Validation(map[string]any{
			"constraint": pg.ConstraintName,
			"message":    "valor fora do permitido pela regra do banco.",
		}).WithCause(err)

	case sqlstateNotNull:
		return apperr.Validation(map[string]string{colunaOu(pg.ColumnName, "campo"): "é obrigatório."}).WithCause(err)

	// Contenção. O banco não disse "seu pedido é inválido", disse "não
	// consegui decidir agora porque outra transação está mexendo no mesmo
	// lugar". O TxManager já repetiu — chegar aqui significa que a disputa
	// sobreviveu ao orçamento de repetição, ou que quem chamou não usou
	// transação gerenciada.
	//
	// Vira DATE_CONFLICT, nunca INTERNAL. A razão é a regra 2 do CLAUDE.md: a
	// única contenção real deste sistema é a briga pela mesma data na
	// constraint de exclusão de stay_blocks, e o 40P01 nasce DENTRO da
	// verificação dessa constraint (medido: `where` do erro traz "while
	// checking exclusion constraint ... in relation "stay_blocks""). Dizer
	// "erro interno" a quem perdeu a corrida manda o hóspede embora e acende
	// alarme de incidente numa situação que é rotina de alta temporada.
	//
	// O preço assumido: uma contenção em OUTRA tabela também sairia como 409
	// DATE_CONFLICT em vez de 500. É a troca certa — 409 é retentável e honesto
	// ("o recurso está disputado agora"), 500 é mentira e vira chamado.
	case sqlstateSerializacao, sqlstateDeadlock, sqlstateLockIndisponi:
		return apperr.DateConflict.WithCause(err).WithDetails(map[string]string{
			"constraint": pg.ConstraintName,
			"detail":     primeiroNaoVazio(pg.Detail, pg.Where, pg.Message),
			"sqlstate":   pg.Code,
		})

	case sqlstateEntradaTexto, sqlstateNumeroForaInt:
		return apperr.Validation(map[string]string{"payload": "valor em formato inválido."}).WithCause(err)

	default:
		return apperr.Internal.WithCause(err)
	}
}

// IsUniqueViolation diz se o erro é 23505 e, quando constraints são informadas,
// se a violação foi em uma delas. É o gancho para o service escolher o código de
// negócio (EMAIL_IN_USE) em vez do genérico. Sem argumento, casa qualquer
// unicidade.
func IsUniqueViolation(err error, constraints ...string) bool {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != sqlstateUnico {
		return false
	}
	if len(constraints) == 0 {
		return true
	}
	for _, c := range constraints {
		if pg.ConstraintName == c {
			return true
		}
	}
	return false
}

// IsForeignKeyViolation, mesma ideia do IsUniqueViolation para 23503 — serve
// para transformar "role_id que não existe" em erro de campo, e não em 500.
func IsForeignKeyViolation(err error, constraints ...string) bool {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != sqlstateChaveEstrang {
		return false
	}
	if len(constraints) == 0 {
		return true
	}
	for _, c := range constraints {
		if pg.ConstraintName == c {
			return true
		}
	}
	return false
}

// ehTransiente marca os erros que justificam repetir a transação: o banco
// recusou por CONTENÇÃO, não por conteúdo, e a próxima tentativa pode ter outra
// resposta.
//
//   - 40001 falha de serialização;
//   - 40P01 impasse — sob disputa, a verificação da constraint de exclusão faz
//     uma transação esperar pela outra, e duas esperas cruzadas viram nó;
//   - 55P03 estouro do lock_timeout — a mesma espera, cortada antes pelo teto de
//     sessão. É o formato em que a disputa chega depois de db.New passar a
//     definir lock_timeout, e ficar de fora aqui derrubaria a repetição inteira.
//
// Sobreposição de datas (23P01) NÃO entra: repetir dá exatamente o mesmo
// conflito, e o cliente precisa saber que a data foi ocupada (regra 2).
//
// errors.As percorre a cadeia inteira, então funciona tanto com o erro cru do
// pgx quanto com ele já embrulhado em *apperr.Error por MapError — que é como
// o erro sobe quando o repositório traduz antes de devolver ao service.
func ehTransiente(err error) bool {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return false
	}
	switch pg.Code {
	case sqlstateSerializacao, sqlstateDeadlock, sqlstateLockIndisponi:
		return true
	default:
		return false
	}
}

// campoDaConstraint tira o nome do campo do nome da constraint, que é o que o
// Postgres entrega em 23505/23503 (ColumnName vem vazio nesses casos).
// "users_email_key" → "email"; "users_email_active_idx" → "email_active".
func campoDaConstraint(pg *pgconn.PgError) string {
	if pg.ColumnName != "" {
		return pg.ColumnName
	}
	nome := pg.ConstraintName
	if nome == "" {
		return "campo"
	}
	for _, sufixo := range []string{"_key", "_idx", "_unique", "_fkey", "_pkey"} {
		nome = strings.TrimSuffix(nome, sufixo)
	}
	if pg.TableName != "" {
		nome = strings.TrimPrefix(nome, pg.TableName+"_")
	}
	if nome == "" {
		return "campo"
	}
	return nome
}

func colunaOu(coluna, padrao string) string {
	if coluna != "" {
		return coluna
	}
	return padrao
}

func primeiroNaoVazio(valores ...string) string {
	for _, v := range valores {
		if v != "" {
			return v
		}
	}
	return ""
}
