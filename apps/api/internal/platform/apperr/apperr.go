// Package apperr define o erro da aplicação. O código é estável e faz parte do
// contrato da API: o front reage ao Code, nunca ao texto da mensagem.
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`

	status int
	cause  error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.cause }
func (e *Error) Status() int   { return e.status }

// WithCause anexa o erro original para o log, sem vazá-lo na resposta.
func (e *Error) WithCause(err error) *Error { c := *e; c.cause = err; return &c }

// WithDetails anexa dados estruturados que o front usa para explicar o erro.
func (e *Error) WithDetails(d any) *Error { c := *e; c.Details = d; return &c }

// WithStatus troca o status HTTP mantendo o code. Existe porque o contrato usa
// TOKEN_INVALID em dois lugares com status diferentes: 401 no /auth/refresh
// (credencial de sessão inválida) e 400 no /auth/password/reset (o token do
// e-mail não é credencial de sessão — responder 401 ali faria o painel achar
// que precisa relogar em vez de pedir outro link).
func (e *Error) WithStatus(s int) *Error { c := *e; c.status = s; return &c }

// WithMessage troca a mensagem legível preservando o code, que é o que o front
// consome.
func (e *Error) WithMessage(m string) *Error { c := *e; c.Message = m; return &c }

func define(code, msg string, status int) *Error {
	return &Error{Code: code, Message: msg, status: status}
}

// Erros de plataforma.
var (
	Internal     = define("INTERNAL", "Erro interno.", http.StatusInternalServerError)
	Unauthorized = define("UNAUTHORIZED", "Autenticação necessária.", http.StatusUnauthorized)
	Forbidden    = define("FORBIDDEN", "Você não tem permissão para isso.", http.StatusForbidden)
	RateLimited  = define("RATE_LIMITED", "Muitas requisições. Tente em instantes.", http.StatusTooManyRequests)
)

// Erros de domínio — o vocabulário do negócio, estável no contrato.
var (
	DateConflict        = define("DATE_CONFLICT", "As datas selecionadas acabaram de ser ocupadas.", http.StatusConflict)
	MinStayNotMet       = define("MIN_STAY_NOT_MET", "Estadia abaixo do mínimo do período.", http.StatusUnprocessableEntity)
	CapacityExceeded    = define("CAPACITY_EXCEEDED", "Número de hóspedes acima da capacidade.", http.StatusUnprocessableEntity)
	DiscountAboveLimit  = define("DISCOUNT_ABOVE_LIMIT", "Desconto acima da alçada.", http.StatusUnprocessableEntity)
	HoldExpired         = define("HOLD_EXPIRED", "A pré-reserva expirou.", http.StatusConflict)
	IdempotencyMismatch = define("IDEMPOTENCY_MISMATCH", "Chave de idempotência reutilizada com corpo diferente.", http.StatusConflict)
)

// Erros de identidade e acesso.
//
// InvalidCredentials é deliberadamente o MESMO erro para e-mail inexistente,
// senha errada e e-mail bloqueado por tentativas. Um código próprio para
// "bloqueado" confirmaria que a conta existe — vira oráculo de enumeração.
var (
	InvalidCredentials = define("INVALID_CREDENTIALS", "E-mail ou senha inválidos.", http.StatusUnauthorized)
	TokenInvalid       = define("TOKEN_INVALID", "Token ausente, expirado ou desconhecido.", http.StatusUnauthorized)
	TokenReused        = define("TOKEN_REUSED", "Sessão comprometida: faça login novamente.", http.StatusUnauthorized)
	EmailInUse         = define("EMAIL_IN_USE", "E-mail já cadastrado.", http.StatusConflict)
	RoleImmutable      = define("ROLE_IMMUTABLE", "Perfil de sistema não pode ser alterado nem excluído.", http.StatusConflict)
	RoleInUse          = define("ROLE_IN_USE", "Ainda há usuários neste perfil.", http.StatusConflict)
)

func NotFound(resource string) *Error {
	return define("NOT_FOUND", fmt.Sprintf("%s não encontrado.", resource), http.StatusNotFound)
}

func Validation(details any) *Error {
	return define("VALIDATION_ERROR", "Dados inválidos.", http.StatusUnprocessableEntity).WithDetails(details)
}

// From converte qualquer erro no erro da aplicação, preservando a causa.
func From(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return Internal.WithCause(err)
}
