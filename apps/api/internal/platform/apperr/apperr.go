// Package apperr define o erro da aplicação. O código é estável e faz parte do
// contrato da API: o front reage ao Code, nunca ao texto da mensagem.
//
// O vocabulário inteiro — os 37 códigos do enum de `components.responses.Erro`,
// cada um com o seu status e a sua mensagem padrão — mora em catalogo.go, e só
// lá. Módulo não declara código: importa o erro daqui e, quando o contexto pede,
// troca a frase com WithMessage e acrescenta o porquê com WithDetails.
package apperr

import (
	"errors"
	"fmt"
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

func NotFound(resource string) *Error {
	return naoEncontrado.WithMessage(fmt.Sprintf("%s não encontrado.", resource))
}

func Validation(details any) *Error {
	return validacao.WithDetails(details)
}

// From converte qualquer erro no erro da aplicação, preservando a causa.
func From(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return Internal.WithCause(err)
}
