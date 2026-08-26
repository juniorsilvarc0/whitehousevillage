// Package roles implementa o CRUD de perfis e a matriz de permissões.
package roles

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// padraoDoCodigo é o mesmo do contrato. O código é a chave estável por onde
// seed, filtro de /users e teste referenciam o perfil — por isso o formato é
// restrito, e não texto livre.
var padraoDoCodigo = regexp.MustCompile(`^[a-z][a-z0-9_]{1,30}$`)

// Perfil é o schema `Perfil` do contrato.
type Perfil struct {
	ID       uuid.UUID `json:"id"`
	Codigo   string    `json:"code"`
	Nome     string    `json:"name"`
	IsSystem bool      `json:"is_system"`
}

// PerfilDetalhe é o GET /roles/{id}: o perfil já com a matriz aplicada, para a
// tela de edição montar em uma chamada.
type PerfilDetalhe struct {
	Perfil
	Permissoes []auth.Permissao `json:"permissions"`
}

// Recurso é uma linha de GET /roles/resources.
type Recurso struct {
	Codigo  string   `json:"code"`
	Label   string   `json:"label"`
	Grupo   string   `json:"group"`
	Acoes   []string `json:"actions"`
	Escopos []string `json:"scopes"`
}

// MetaRecurso é o que o catálogo diz sobre um recurso. NÃO vai para a resposta:
// é o que o service consulta para recusar ação que o recurso não oferece e
// escopo `own` onde não há dono. Vem de `resources`, sempre — o catálogo é dado
// (regra 8), e a validação tem de olhar para a MESMA fonte que a grade da tela.
type MetaRecurso struct {
	Acoes      []string
	SuportaOwn bool
}

// Oferece diz se o recurso aceita a ação.
func (m MetaRecurso) Oferece(acao string) bool {
	for _, a := range m.Acoes {
		if a == acao {
			return true
		}
	}
	return false
}

// Criar é o corpo de POST /roles. `is_system` NÃO entra: só o seed marca perfil
// de sistema, senão qualquer um se tornaria imutável de propósito.
type Criar struct {
	Codigo string `json:"code" validate:"required"`
	Nome   string `json:"name" validate:"required,min=2,max=80"`
}

func (c *Criar) Normalizar() {
	c.Codigo = strings.ToLower(strings.TrimSpace(c.Codigo))
	c.Nome = strings.TrimSpace(c.Nome)
}

func (c Criar) Validar() map[string]string {
	erros := map[string]string{}
	if c.Codigo != "" && !padraoDoCodigo.MatchString(c.Codigo) {
		erros["code"] = "use minúsculas, números e _ , começando por letra (2 a 31 caracteres)."
	}
	return erros
}

// Atualizar é o corpo do PATCH /roles/{id} — só rótulo e código. A matriz tem
// endpoint próprio, para não misturar "renomear" com "mudar acesso".
type Atualizar struct {
	Codigo httpx.Opt[string] `json:"code"`
	Nome   httpx.Opt[string] `json:"name"`
}

func (a *Atualizar) Normalizar() {
	if v, ok := a.Codigo.Definido(); ok {
		a.Codigo = httpx.De(strings.ToLower(strings.TrimSpace(v)))
	}
	if v, ok := a.Nome.Definido(); ok {
		a.Nome = httpx.De(strings.TrimSpace(v))
	}
}

func (a Atualizar) Validar() map[string]string {
	erros := map[string]string{}

	if a.Codigo.DeveLimpar() {
		erros["code"] = "não pode ser nulo."
	} else if v, ok := a.Codigo.Definido(); ok && !padraoDoCodigo.MatchString(v) {
		erros["code"] = "use minúsculas, números e _ , começando por letra (2 a 31 caracteres)."
	}

	if a.Nome.DeveLimpar() {
		erros["name"] = "não pode ser nulo."
	} else if v, ok := a.Nome.Definido(); ok && !httpx.ValidarValor(v, "min=2,max=80") {
		erros["name"] = "deve ter entre 2 e 80 caracteres."
	}

	return erros
}

// ObrigatoriosDoPut confere o que o `required: [code, name]` do PUT exige.
// A checagem é explícita aqui, e não em tag, porque o mesmo DTO serve ao PATCH,
// onde os dois campos são opcionais.
//
// A matriz NÃO vem no PUT: quem troca permissão é PUT /roles/{id}/permissions.
// Um PUT de cadastro que zerasse a matriz por omissão tiraria o acesso de todos
// os usuários do perfil sem ninguém ter pedido isso.
func (a Atualizar) ObrigatoriosDoPut() map[string]string {
	erros := map[string]string{}
	if _, ok := a.Codigo.Definido(); !ok {
		erros["code"] = "é obrigatório."
	}
	if _, ok := a.Nome.Definido(); !ok {
		erros["name"] = "é obrigatório."
	}
	return erros
}

// Matriz é o corpo de PUT /roles/{id}/permissions: um array no TOPO do JSON.
// Precisa ser um tipo nomeado para carregar o Validar() — o validator só
// enxerga struct, e o array cru não tem onde pendurar regra.
type Matriz []auth.Permissao

// Validar recusa ação e escopo desconhecidos e, principalmente, o par
// (resource, action) repetido: a PK é (role_id, resource_code, action), então
// dois escopos para o mesmo par não têm como coexistir — deixar passar faria o
// INSERT estourar 23505 e virar 500 no meio de uma transação.
func (m Matriz) Validar() map[string]string {
	erros := map[string]string{}
	vistos := map[string]int{}

	for i, p := range m {
		if strings.TrimSpace(p.Resource) == "" {
			erros[fmt.Sprintf("permissions[%d].resource", i)] = "é obrigatório."
		}
		if !auth.AcaoValida(p.Action) {
			erros[fmt.Sprintf("permissions[%d].action", i)] = "deve ser ver, criar, editar ou excluir."
		}
		// Escopo ausente vale como `all`, que é o default do contrato.
		if p.Scope != "" && !auth.EscopoValido(p.Scope) {
			erros[fmt.Sprintf("permissions[%d].scope", i)] = "deve ser all ou own."
		}

		chave := p.Resource + ":" + p.Action
		if anterior, repetido := vistos[chave]; repetido {
			erros[fmt.Sprintf("permissions[%d]", i)] = fmt.Sprintf(
				"par (%s, %s) repetido — já declarado no índice %d.", p.Resource, p.Action, anterior)
			continue
		}
		vistos[chave] = i
	}
	return erros
}

// ComEscopoPadrao devolve a matriz com o escopo preenchido onde veio vazio.
func (m Matriz) ComEscopoPadrao() Matriz {
	out := make(Matriz, len(m))
	for i, p := range m {
		if p.Scope == "" {
			p.Scope = auth.EscopoAll
		}
		out[i] = p
	}
	return out
}
