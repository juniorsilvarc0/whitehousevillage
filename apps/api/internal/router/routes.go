package router

import (
	"fmt"
	"net/http"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/roles"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/users"
)

// Acesso classifica como a rota é protegida. É o eixo que o teste de contrato
// varre: nenhuma rota pode ficar sem classificação, e AcessoAutenticado exige
// justificativa escrita — assim "esqueci de checar permissão" nunca passa por
// distração.
type Acesso string

const (
	// AcessoPublico — sem token. Só sonda e os endpoints que existem
	// justamente para quem ainda não tem sessão.
	AcessoPublico Acesso = "publico"

	// AcessoAutenticado — exige token, mas não consulta a matriz: são os
	// endpoints sobre o PRÓPRIO usuário, onde a identidade já é a autorização.
	AcessoAutenticado Acesso = "autenticado"

	// AcessoPermissao — exige token e uma célula (recurso, ação) da matriz.
	AcessoPermissao Acesso = "permissao"
)

// Rota é uma linha da tabela declarativa. Esta tabela é a fonte da verdade das
// rotas: o teste de contrato a varre e falha se algum recurso não expõe os seis
// verbos, se algum verbo não checa permissão ou se existe rota fora da OpenAPI.
type Rota struct {
	Metodo  string
	Path    string
	Acesso  Acesso
	Recurso string
	Acao    string

	// Motivo é obrigatório em AcessoAutenticado: quem tirar a checagem de
	// permissão de uma rota tem de escrever por quê, e o revisor lê.
	Motivo string

	Handler http.HandlerFunc
}

// Deps carrega os handlers montados. Passar Deps zerado é seguro: montar a
// tabela só cria method values, não os chama — é assim que o teste de contrato
// inspeciona as rotas sem subir banco.
type Deps struct {
	Saude *Saude
	Auth  *auth.Handler
	Users *users.Handler
	Roles *roles.Handler
}

// Rotas devolve a tabela completa da API v1.
func Rotas(d Deps) []Rota {
	return []Rota{
		// ───────── Saúde ─────────
		{Metodo: http.MethodGet, Path: "/healthz", Acesso: AcessoPublico, Handler: d.Saude.Vivo},
		{Metodo: http.MethodGet, Path: "/readyz", Acesso: AcessoPublico, Handler: d.Saude.Pronto},

		// ───────── Autenticação ─────────
		{Metodo: http.MethodPost, Path: "/auth/login", Acesso: AcessoPublico, Handler: d.Auth.Login},
		{Metodo: http.MethodPost, Path: "/auth/refresh", Acesso: AcessoPublico, Handler: d.Auth.Refresh},
		{Metodo: http.MethodPost, Path: "/auth/password/forgot", Acesso: AcessoPublico, Handler: d.Auth.EsqueciSenha},
		{Metodo: http.MethodPost, Path: "/auth/password/reset", Acesso: AcessoPublico, Handler: d.Auth.TrocarSenha},
		{
			Metodo: http.MethodPost, Path: "/auth/logout", Acesso: AcessoAutenticado,
			Motivo:  "encerra a própria sessão; exigir permissão deixaria um usuário sem acesso preso dentro da sessão",
			Handler: d.Auth.Logout,
		},
		{
			Metodo: http.MethodGet, Path: "/auth/me", Acesso: AcessoAutenticado,
			Motivo:  "devolve a identidade e a matriz do próprio requisitante; a identidade já é a autorização",
			Handler: d.Auth.Me,
		},

		// ───────── Usuários ─────────
		{Metodo: http.MethodGet, Path: "/users", Acesso: AcessoPermissao, Recurso: auth.RecursoUsuarios, Acao: auth.AcaoVer, Handler: d.Users.Listar},
		{Metodo: http.MethodPost, Path: "/users", Acesso: AcessoPermissao, Recurso: auth.RecursoUsuarios, Acao: auth.AcaoCriar, Handler: d.Users.Criar},
		{Metodo: http.MethodGet, Path: "/users/{id}", Acesso: AcessoPermissao, Recurso: auth.RecursoUsuarios, Acao: auth.AcaoVer, Handler: d.Users.Buscar},
		{Metodo: http.MethodPut, Path: "/users/{id}", Acesso: AcessoPermissao, Recurso: auth.RecursoUsuarios, Acao: auth.AcaoEditar, Handler: d.Users.Substituir},
		{Metodo: http.MethodPatch, Path: "/users/{id}", Acesso: AcessoPermissao, Recurso: auth.RecursoUsuarios, Acao: auth.AcaoEditar, Handler: d.Users.Atualizar},
		{Metodo: http.MethodDelete, Path: "/users/{id}", Acesso: AcessoPermissao, Recurso: auth.RecursoUsuarios, Acao: auth.AcaoExcluir, Handler: d.Users.Desativar},

		// ───────── Perfis ─────────
		{Metodo: http.MethodGet, Path: "/roles", Acesso: AcessoPermissao, Recurso: auth.RecursoPerfis, Acao: auth.AcaoVer, Handler: d.Roles.Listar},
		{Metodo: http.MethodPost, Path: "/roles", Acesso: AcessoPermissao, Recurso: auth.RecursoPerfis, Acao: auth.AcaoCriar, Handler: d.Roles.Criar},
		// Rota estática antes da paramétrica na leitura humana; o chi resolve a
		// precedência sozinho, mas a ordem aqui evita a dúvida de quem revisa.
		{Metodo: http.MethodGet, Path: "/roles/resources", Acesso: AcessoPermissao, Recurso: auth.RecursoPerfis, Acao: auth.AcaoVer, Handler: d.Roles.Recursos},
		{Metodo: http.MethodGet, Path: "/roles/{id}", Acesso: AcessoPermissao, Recurso: auth.RecursoPerfis, Acao: auth.AcaoVer, Handler: d.Roles.Buscar},
		{Metodo: http.MethodPut, Path: "/roles/{id}", Acesso: AcessoPermissao, Recurso: auth.RecursoPerfis, Acao: auth.AcaoEditar, Handler: d.Roles.Substituir},
		{Metodo: http.MethodPatch, Path: "/roles/{id}", Acesso: AcessoPermissao, Recurso: auth.RecursoPerfis, Acao: auth.AcaoEditar, Handler: d.Roles.Atualizar},
		{Metodo: http.MethodDelete, Path: "/roles/{id}", Acesso: AcessoPermissao, Recurso: auth.RecursoPerfis, Acao: auth.AcaoExcluir, Handler: d.Roles.Excluir},
		{Metodo: http.MethodPut, Path: "/roles/{id}/permissions", Acesso: AcessoPermissao, Recurso: auth.RecursoPerfis, Acao: auth.AcaoEditar, Handler: d.Roles.SubstituirPermissoes},
	}
}

// ValidarTabela confere as invariantes da tabela. É chamada no boot: uma rota
// mal declarada derruba o processo na subida, e não silenciosamente em produção
// com um endpoint aberto.
func ValidarTabela(rotas []Rota) error {
	vistas := map[string]bool{}

	for _, r := range rotas {
		chave := r.Metodo + " " + r.Path
		if vistas[chave] {
			return fmt.Errorf("rota duplicada: %s", chave)
		}
		vistas[chave] = true

		switch r.Acesso {
		case AcessoPublico:
			if r.Recurso != "" || r.Acao != "" {
				return fmt.Errorf("%s: rota pública não deve declarar recurso/ação", chave)
			}
		case AcessoAutenticado:
			if r.Motivo == "" {
				return fmt.Errorf("%s: rota autenticada sem checagem de permissão precisa de Motivo", chave)
			}
		case AcessoPermissao:
			if r.Recurso == "" || r.Acao == "" {
				return fmt.Errorf("%s: rota protegida sem recurso/ação", chave)
			}
			if !auth.AcaoValida(r.Acao) {
				return fmt.Errorf("%s: ação %q não existe", chave, r.Acao)
			}
		default:
			return fmt.Errorf("%s: acesso não classificado", chave)
		}
	}
	return nil
}
