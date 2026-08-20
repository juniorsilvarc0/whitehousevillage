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
