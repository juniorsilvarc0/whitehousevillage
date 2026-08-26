package auth

import (
	"context"
	"net/http"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Ações do sistema — as mesmas quatro do CHECK de role_permissions.
const (
	AcaoVer     = "ver"
	AcaoCriar   = "criar"
	AcaoEditar  = "editar"
	AcaoExcluir = "excluir"
)

// Escopos. `all` vê tudo do recurso; `own` restringe ao que é do usuário, e
// vira `AND owner_id = $user` no SQL do repositório — nunca filtro em memória,
// que faria o `total` da paginação mentir.
const (
	EscopoAll = "all"
	EscopoOwn = "own"
)

// AcoesValidas é o catálogo fechado de ações, na ordem em que a tela mostra.
var AcoesValidas = []string{AcaoVer, AcaoCriar, AcaoEditar, AcaoExcluir}

// EscopoValido diz se o escopo é um dos dois conhecidos. Escopo desconhecido no
// banco é dado corrompido, e a decisão é sempre negar: permitir por omissão
// transformaria um typo num vazamento.
func EscopoValido(escopo string) bool { return escopo == EscopoAll || escopo == EscopoOwn }

// AcaoValida diz se a ação é uma das quatro.
func AcaoValida(acao string) bool {
	for _, a := range AcoesValidas {
		if a == acao {
			return true
		}
	}
	return false
}

// Permissao é uma célula da matriz do perfil.
type Permissao struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
	Scope    string `json:"scope"`
}

// Conjunto é a matriz do perfil indexada por "recurso:ação" → escopo. Carregada
// numa consulta só, no início da requisição.
type Conjunto map[string]string

// NovoConjunto monta o índice a partir das linhas de role_permissions,
// descartando o que não faz sentido: linha com escopo desconhecido é ignorada,
// e ignorar significa negar.
func NovoConjunto(perms []Permissao) Conjunto {
	c := make(Conjunto, len(perms))
	for _, p := range perms {
		if !AcaoValida(p.Action) || !EscopoValido(p.Scope) {
			continue
		}
		c[chave(p.Resource, p.Action)] = p.Scope
	}
	return c
}

// Lista devolve a matriz achatada, na forma que /auth/me e /roles/{id} expõem.
func (c Conjunto) Lista() []Permissao {
	out := make([]Permissao, 0, len(c))
	for k, escopo := range c {
		recurso, acao, ok := desmembrar(k)
		if !ok {
			continue
		}
		out = append(out, Permissao{Resource: recurso, Action: acao, Scope: escopo})
	}
	return out
}

// Escopo devolve o escopo concedido e se há concessão. Ausência é negação:
// nunca existe permissão por omissão.
func (c Conjunto) Escopo(recurso, acao string) (string, bool) {
	escopo, ok := c[chave(recurso, acao)]
	if !ok || !EscopoValido(escopo) {
		return "", false
	}
	return escopo, true
}

// Pode é a pergunta binária do middleware.
func (c Conjunto) Pode(recurso, acao string) bool {
	_, ok := c.Escopo(recurso, acao)
	return ok
}

// ScopeOf devolve "all" ou "own" para o repositório aplicar no WHERE, e string
// vazia quando não há permissão. Quem chama trata vazio como negado — é a
// mesma regra do middleware, repetida aqui porque o service pode consultar o
// escopo de um recurso diferente do que a rota protege.
func ScopeOf(ctx context.Context, recurso, acao string) string {
	u, ok := UserFrom(ctx)
	if !ok {
		return ""
	}
	escopo, ok := u.Permissoes.Escopo(recurso, acao)
	if !ok {
		return ""
	}
	return escopo
}

// SomenteProprios é o atalho que o repositório usa para decidir se acrescenta
// `AND owner_id = $user`.
func SomenteProprios(ctx context.Context, recurso, acao string) bool {
	return ScopeOf(ctx, recurso, acao) == EscopoOwn
}

// Middleware autoriza a rota. É a barreira real do sistema: o que o painel
// esconde a partir de /auth/me é conveniência visual, não segurança.
func Middleware(recurso, acao string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := UserFrom(r.Context())
			if !ok {
				httpx.Error(w, r, apperr.Unauthorized)
				return
			}
			if !u.Permissoes.Pode(recurso, acao) {
				httpx.Error(w, r, apperr.Forbidden.WithDetails(map[string]string{
					"resource": recurso,
					"action":   acao,
				}))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func chave(recurso, acao string) string { return recurso + ":" + acao }

func desmembrar(k string) (recurso, acao string, ok bool) {
	// O recurso pode conter ponto ("crm.opportunities"), mas nunca dois-pontos:
	// por isso a quebra é sempre no ÚLTIMO ':'.
	for i := len(k) - 1; i >= 0; i-- {
		if k[i] == ':' {
			return k[:i], k[i+1:], true
		}
	}
	return "", "", false
}
