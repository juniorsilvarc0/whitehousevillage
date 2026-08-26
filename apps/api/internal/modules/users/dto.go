// Package users implementa o CRUD de usuários do painel.
package users

import (
	"strings"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Criar é o corpo de POST /users (schema UsuarioCriar).
type Criar struct {
	Nome     string    `json:"name" validate:"required,min=2,max=120"`
	Email    string    `json:"email" validate:"required,email,max=254"`
	Senha    string    `json:"password" validate:"required,min=8,max=200"`
	RoleID   uuid.UUID `json:"role_id" validate:"required"`
	Telefone *string   `json:"phone" validate:"omitempty,max=32"`
	Ativo    *bool     `json:"active"`
}

// Normalizar deixa o e-mail em caixa baixa e limpa telefone vazio: string vazia
// e NULL significam a mesma coisa aqui, e gravar as duas formas faria a coluna
// ter dois jeitos de dizer "sem telefone".
func (c *Criar) Normalizar() {
	c.Nome = strings.TrimSpace(c.Nome)
	c.Email = strings.ToLower(strings.TrimSpace(c.Email))
	if c.Telefone != nil {
		t := strings.TrimSpace(*c.Telefone)
		if t == "" {
			c.Telefone = nil
		} else {
			c.Telefone = &t
		}
	}
}

// AtivoOuPadrao aplica o default do contrato (`active: true`).
func (c Criar) AtivoOuPadrao() bool {
	if c.Ativo == nil {
		return true
	}
	return *c.Ativo
}

// Atualizar é o corpo do PATCH (schema UsuarioAtualizar) e a base do PUT.
//
// Todo campo é Opt porque o PATCH precisa separar três coisas: ausente (não
// mexe), null (limpa) e valor (grava). Ponteiro só separaria duas.
type Atualizar struct {
	Nome     httpx.Opt[string]    `json:"name"`
	Email    httpx.Opt[string]    `json:"email"`
	Senha    httpx.Opt[string]    `json:"password"`
	RoleID   httpx.Opt[uuid.UUID] `json:"role_id"`
	Telefone httpx.Opt[string]    `json:"phone"`
	Ativo    httpx.Opt[bool]      `json:"active"`
}

// Normalizar apara os campos textuais presentes.
func (a *Atualizar) Normalizar() {
	if v, ok := a.Nome.Definido(); ok {
		a.Nome = httpx.De(strings.TrimSpace(v))
	}
	if v, ok := a.Email.Definido(); ok {
		a.Email = httpx.De(strings.ToLower(strings.TrimSpace(v)))
	}
	// Telefone em branco é o mesmo pedido de "limpar" que o null: aceitar os
	// dois evita que o front tenha de saber a diferença.
	if v, ok := a.Telefone.Definido(); ok && strings.TrimSpace(v) == "" {
		a.Telefone = httpx.Nulo[string]()
	} else if ok {
		a.Telefone = httpx.De(strings.TrimSpace(v))
	}
}

// Validar cobre o que as tags não alcançam dentro do Opt.
//
// A regra de fundo: campo que a coluna exige NOT NULL não pode receber `null`.
// Sem esta checagem, `{"name": null}` chegaria ao UPDATE e estouraria 23502 —
// um 422 legível vale mais que uma violação de constraint traduzida.
func (a Atualizar) Validar() map[string]string {
	erros := map[string]string{}

	if a.Nome.DeveLimpar() {
		erros["name"] = "não pode ser nulo."
	} else if v, ok := a.Nome.Definido(); ok && !httpx.ValidarValor(v, "min=2,max=120") {
		erros["name"] = "deve ter entre 2 e 120 caracteres."
	}

	if a.Email.DeveLimpar() {
		erros["email"] = "não pode ser nulo."
	} else if v, ok := a.Email.Definido(); ok && !httpx.ValidarValor(v, "email,max=254") {
		erros["email"] = "e-mail inválido."
	}

	if a.Senha.DeveLimpar() {
		erros["password"] = "não pode ser nulo."
	} else if v, ok := a.Senha.Definido(); ok && !httpx.ValidarValor(v, "min=8,max=200") {
		erros["password"] = "deve ter ao menos 8 caracteres."
	}

	if a.RoleID.DeveLimpar() {
		erros["role_id"] = "não pode ser nulo."
	} else if v, ok := a.RoleID.Definido(); ok && v == uuid.Nil {
		erros["role_id"] = "identificador inválido."
	}

	if a.Ativo.DeveLimpar() {
		erros["active"] = "não pode ser nulo."
	}

	if v, ok := a.Telefone.Definido(); ok && !httpx.ValidarValor(v, "max=32") {
		erros["phone"] = "deve ter no máximo 32 caracteres."
	}

	return erros
}

// ParaSubstituicao transforma o corpo do PUT em substituição integral: o que
// não veio volta ao padrão.
//
// A exceção é `password` — ausente MANTÉM a senha atual. Um PUT sem senha
// zerando a credencial de quem foi editado seria armadilha garantida na tela de
// edição, que raramente reenvia a senha.
func (a Atualizar) ParaSubstituicao() Atualizar {
	sub := a
	if !sub.Telefone.Set {
		sub.Telefone = httpx.Nulo[string]()
	}
	if !sub.Ativo.Set {
		sub.Ativo = httpx.De(true)
	}
	return sub
}

// ObrigatoriosDoPut confere os campos que o allOf do contrato marca como
// required no PUT.
func (a Atualizar) ObrigatoriosDoPut() map[string]string {
	erros := map[string]string{}
	if _, ok := a.Nome.Definido(); !ok {
		erros["name"] = "é obrigatório."
	}
	if _, ok := a.Email.Definido(); !ok {
		erros["email"] = "é obrigatório."
	}
	if _, ok := a.RoleID.Definido(); !ok {
		erros["role_id"] = "é obrigatório."
	}
	return erros
}

// Filtro é a query de GET /users já resolvida.
type Filtro struct {
	Busca     string
	RoleCode  string
	Ativo     *bool
	OrderBy   string
	Pagina    int
	PorPagina int

	// ApenasID é o escopo `own` traduzido para o WHERE. Preenchido pelo
	// service, nunca pelo cliente.
	ApenasID *uuid.UUID
}
