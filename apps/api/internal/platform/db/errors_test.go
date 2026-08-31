package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

func pgErro(code, constraint, tabela string) error {
	return fmt.Errorf("consultando: %w", &pgconn.PgError{
		Code:           code,
		ConstraintName: constraint,
		TableName:      tabela,
		Message:        "erro do postgres",
	})
}

// A tradução de 23P01 é o que mantém a promessa do CLAUDE.md: sobreposição de
// datas vira 409 DATE_CONFLICT, nunca 500.
func TestMapErrorTraduzOsCodigosQueImportam(t *testing.T) {
	casos := []struct {
		nome   string
		err    error
		code   string
		status int
	}{
		{"exclusão de datas", pgErro("23P01", "stay_no_overlap", "stay_blocks"), "DATE_CONFLICT", 409},
		{"unicidade", pgErro("23505", "users_email_key", "users"), "VALIDATION_ERROR", 422},
		{"chave estrangeira", pgErro("23503", "users_role_id_fkey", "users"), "VALIDATION_ERROR", 422},
		{"check", pgErro("23514", "reservations_check", "reservations"), "VALIDATION_ERROR", 422},
		{"not null", pgErro("23502", "", "users"), "VALIDATION_ERROR", 422},
		{"uuid malformado", pgErro("22P02", "", ""), "VALIDATION_ERROR", 422},
		// Contenção nunca é 500. O TxManager já repetiu; se o erro chegou aqui,
		// a disputa é real e a resposta honesta é 409 — foi 49 de 49 perdedores
		// vendo "erro interno" numa disputa de data que motivou este bloco.
		{"impasse", pgErro("40P01", "", ""), "DATE_CONFLICT", 409},
		{"falha de serialização", pgErro("40001", "", ""), "DATE_CONFLICT", 409},
		{"lock_timeout", pgErro("55P03", "", ""), "DATE_CONFLICT", 409},
		{"sem linha", fmt.Errorf("busca: %w", pgx.ErrNoRows), "NOT_FOUND", 404},
		{"erro desconhecido", errors.New("conexão caiu"), "INTERNAL", 500},
		{"sqlstate sem tradução", pgErro("42601", "", ""), "INTERNAL", 500},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			e := apperr.From(MapError(c.err))
			if e.Code != c.code {
				t.Fatalf("code = %q, esperado %q", e.Code, c.code)
			}
			if e.Status() != c.status {
				t.Fatalf("status = %d, esperado %d", e.Status(), c.status)
			}
		})
	}
}

func TestMapErrorPreservaErroJaTraduzido(t *testing.T) {
	original := apperr.HoldExpired
	if got := apperr.From(MapError(original)); got.Code != "HOLD_EXPIRED" {
		t.Fatalf("erro de domínio foi reescrito para %q", got.Code)
	}
}

// Cancelamento do cliente não é erro da aplicação: virar 500 encheria o log de
// erro com aba fechada no meio da requisição.
func TestMapErrorNaoTratamCancelamentoComoFalha(t *testing.T) {
	if err := MapError(context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelamento foi traduzido para %v", err)
	}
}

// É por aqui que o repositório de usuários decide entre EMAIL_IN_USE e o
// genérico.
func TestIsUniqueViolationCasaPelaConstraint(t *testing.T) {
	err := pgErro("23505", "users_email_active_idx", "users")

	if !IsUniqueViolation(err) {
		t.Fatal("sem argumento deveria casar qualquer unicidade")
	}
	if !IsUniqueViolation(err, "users_email_key", "users_email_active_idx") {
		t.Fatal("deveria casar a constraint informada")
	}
	if IsUniqueViolation(err, "roles_code_key") {
		t.Fatal("não deveria casar outra constraint")
	}
	if IsUniqueViolation(pgErro("23503", "users_email_key", "users"), "users_email_key") {
		t.Fatal("23503 não é violação de unicidade")
	}
	if IsUniqueViolation(errors.New("qualquer")) {
		t.Fatal("erro comum não é violação de unicidade")
	}
}

func TestIsForeignKeyViolation(t *testing.T) {
	err := pgErro("23503", "role_permissions_resource_code_fkey", "role_permissions")

	if !IsForeignKeyViolation(err, "role_permissions_resource_code_fkey") {
		t.Fatal("deveria casar a FK informada")
	}
	if IsForeignKeyViolation(pgErro("23505", "x", "y")) {
		t.Fatal("23505 não é violação de FK")
	}
}

// Retry só faz sentido onde repetir pode dar certo. 23P01 repetido dá o mesmo
// conflito — e o cliente precisa saber que a data foi ocupada.
//
// 55P03 (estouro do lock_timeout) entra porque é a forma em que a disputa passou
// a chegar depois de db.New definir lock_timeout: é a MESMA espera de antes,
// cortada pelo teto de sessão em vez do detector de impasse. Deixá-lo de fora
// desligaria a repetição inteira sem nenhum teste ficar vermelho.
func TestEhTransienteAceitaSoOsErrosDeContencao(t *testing.T) {
	casos := map[string]bool{
		"40001": true,
		"40P01": true,
		"55P03": true,
		"23P01": false,
		"23505": false,
		// statement_timeout é "consulta lenta demais", não contenção: repetir só
		// gastaria o banco de novo.
		"57014": false,
	}

	for code, esperado := range casos {
		t.Run(code, func(t *testing.T) {
			if got := ehTransiente(pgErro(code, "", "")); got != esperado {
				t.Fatalf("ehTransiente(%s) = %v, esperado %v", code, got, esperado)
			}
		})
	}
	if ehTransiente(errors.New("erro comum")) {
		t.Fatal("erro comum não é transiente")
	}
}

func TestCampoDaConstraint(t *testing.T) {
	casos := []struct {
		pg       *pgconn.PgError
		esperado string
	}{
		{&pgconn.PgError{ConstraintName: "users_email_key", TableName: "users"}, "email"},
		{&pgconn.PgError{ConstraintName: "users_email_active_idx", TableName: "users"}, "email_active"},
		{&pgconn.PgError{ColumnName: "phone"}, "phone"},
		{&pgconn.PgError{}, "campo"},
	}

	for _, c := range casos {
		if got := campoDaConstraint(c.pg); got != c.esperado {
			t.Fatalf("campoDaConstraint(%+v) = %q, esperado %q", c.pg, got, c.esperado)
		}
	}
}

// MapError PRECISA deixar o *pgconn.PgError alcançável por errors.As depois de
// traduzir. É disso que dependem ehTransiente (para decidir a repetição) e o
// teste de concorrência (que confere se o DATE_CONFLICT veio mesmo de um 23P01).
func TestMapErrorPreservaOPgErrorNaCadeia(t *testing.T) {
	for _, code := range []string{"23P01", "40P01", "40001", "55P03"} {
		t.Run(code, func(t *testing.T) {
			traduzido := MapError(pgErro(code, "stay_no_overlap", "stay_blocks"))

			var pg *pgconn.PgError
			if !errors.As(traduzido, &pg) {
				t.Fatalf("PgError se perdeu na tradução de %s: %v", code, traduzido)
			}
			if pg.Code != code {
				t.Fatalf("sqlstate na cadeia = %q, esperado %q", pg.Code, code)
			}
		})
	}
}

// A garantia mais dura do CLAUDE.md §2: sobreposição de datas não faz retry.
func TestEhTransienteNaoRepeteSobreposicaoJaTraduzida(t *testing.T) {
	if ehTransiente(MapError(pgErro("23P01", "stay_no_overlap", "stay_blocks"))) {
		t.Fatal("23P01 traduzido não pode virar transiente: repetir dá o mesmo conflito")
	}
	if !ehTransiente(MapError(pgErro("40P01", "", ""))) {
		t.Fatal("40P01 traduzido continua transiente — é o embrulho que escondia a repetição")
	}
}

// pgCheck monta o 23514 como as constraint triggers do projeto o levantam:
// mensagem de negócio em `Message` e a saída em `Hint`.
func pgCheck(constraint, mensagem, dica string) error {
	return fmt.Errorf("gravando: %w", &pgconn.PgError{
		Code:           "23514",
		ConstraintName: constraint,
		Message:        mensagem,
		Hint:           dica,
	})
}

// As invariantes de negócio vivem em constraint trigger e já levantam a frase
// certa. Até esta rodada MapError descartava `Message` e `Hint` e devolvia
// "valor fora do permitido pela regra do banco." para todas — inclusive para a
// troca de `consumes` com venda viva, que o contrato documenta como
// 409 RESOURCE_IN_USE.
func TestCheckNomeadoChegaComACodigoEAFraseDoContrato(t *testing.T) {
	const mensagem = `White House Completa tem 1 reserva(s) de pé (WH-2026-0001): ` +
		`mudar de "all_members" para "one_member" mudaria o que já foi vendido a essas pessoas`
	const dica = "espere as estadias terminarem, cancele-as com a gestão, ou cadastre um produto NOVO"

	e := apperr.From(MapError(pgCheck("unit_types_consumes_com_venda_viva", mensagem, dica)))

	if e.Code != "RESOURCE_IN_USE" || e.Status() != 409 {
		t.Fatalf("code/status = %s/%d, esperado RESOURCE_IN_USE/409", e.Code, e.Status())
	}
	if e.Message != mensagem {
		t.Fatalf("a mensagem do banco foi descartada: %q", e.Message)
	}
	d, ok := e.Details.(map[string]any)
	if !ok || d["hint"] != dica || d["constraint"] != "unit_types_consumes_com_venda_viva" {
		t.Fatalf("details = %#v, esperado constraint + hint", e.Details)
	}
}

func TestComposicaoIncompletaDoBancoUsaOMesmoCodigoDaAplicacao(t *testing.T) {
	e := apperr.From(MapError(pgCheck("reservation_units_composicao_completa",
		"reserva WH-2026-0001 do produto completa ocupa 7 de 8 unidades", "")))

	if e.Code != "COMPOSITION_INCOMPLETE" || e.Status() != 422 {
		t.Fatalf("code/status = %s/%d, esperado COMPOSITION_INCOMPLETE/422", e.Code, e.Status())
	}
	if d, _ := e.Details.(map[string]any); d["hint"] != nil {
		t.Fatalf("hint vazio não deve entrar em details: %#v", e.Details)
	}
}

// Orçamento cujas noites não fecham é defeito NOSSO: quem monta quote_nights é
// o nosso código. Responder 422 mandaria o operador procurar erro num
// formulário que estava certo — e a mensagem do banco é instrução para
// programador. Vira 500, e a frase do banco fica só na causa (log).
func TestOrcamentoQueNaoFechaEhErroInternoENaoDeValidacao(t *testing.T) {
	const mensagem = "orçamento de 2026-06-15 a 2026-06-18 precisa de 3 noite(s) detalhada(s) e tem 0: o preço não se explica"

	e := apperr.From(MapError(pgCheck("quote_nights_fecham_o_orcamento", mensagem,
		"grave uma linha em quote_nights para cada noite")))

	if e.Code != "INTERNAL" || e.Status() != 500 {
		t.Fatalf("code/status = %s/%d, esperado INTERNAL/500", e.Code, e.Status())
	}
	if e.Message == mensagem || e.Details != nil {
		t.Fatalf("a instrução para programador vazou na resposta: message=%q details=%#v", e.Message, e.Details)
	}
	// Mas o log precisa dela: é a única pista de qual das três verificações
	// da trigger disparou.
	if !strings.Contains(e.Error(), "o preço não se explica") {
		t.Fatalf("a mensagem do banco sumiu também da causa: %v", e.Error())
	}
}

// Constraint fora da allowlist NÃO ganha tradução: repassar a mensagem de
// qualquer 23514 publicaria texto de banco que ninguém revisou.
func TestCheckDesconhecidoNaoVazaAMensagemDoBanco(t *testing.T) {
	e := apperr.From(MapError(pgCheck("reservations_guests_check", "new row violates check constraint", "")))

	if e.Code != "VALIDATION_ERROR" || e.Status() != 422 {
		t.Fatalf("code/status = %s/%d, esperado VALIDATION_ERROR/422", e.Code, e.Status())
	}
	d, _ := e.Details.(map[string]any)
	if d["message"] != "valor fora do permitido pela regra do banco." {
		t.Fatalf("details = %#v, esperado a frase genérica", e.Details)
	}
}
