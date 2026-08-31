package router

import (
	"fmt"
	"net/http"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/contatos"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/crm"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/disponibilidade"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/inventario"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/reservas"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/roles"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/stream"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/tarifario"
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

	// NaRaiz tira a rota do prefixo `/api/v1` e a monta em `/`.
	//
	// Serve às SONDAS, e só a elas. Sonda de saúde não é contrato de negócio:
	// quem a chama é o Docker, o Traefik e o orquestrador, com um caminho fixo
	// escrito na configuração de infraestrutura — e no dia em que a API ganhar
	// um `/api/v2`, o healthcheck do Compose não pode ter de ser reescrito
	// junto, nem existir em duas versões.
	//
	// O custo de não ter isto foi medido: `curl localhost:8080/readyz` devolvia
	// 404, contra o que o plano do projeto especifica e contra o que qualquer
	// runbook tenta primeiro.
	//
	// O `Path` da tabela continua sendo `/healthz`, que é o que o teste de
	// contrato compara com a OpenAPI — lá as duas sondas declaram `servers`
	// próprio, apontando a raiz.
	NaRaiz bool

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

	// Fase 1. Cada um é preenchido pelo main quando o módulo existir; a tabela
	// de rotas de cada módulo mora no PRÓPRIO arquivo rotas_<modulo>.go.
	Inventario      *inventario.Handler
	Tarifario       *tarifario.Handler
	Disponibilidade *disponibilidade.Handler
	Reservas        *reservas.Handler
	CRM             *crm.Handler
	Stream          *stream.Handler
	Contatos        *contatos.Handler
}

// Rotas devolve a tabela completa da API v1, concatenando os grupos.
//
// Um grupo por módulo, cada um no seu arquivo: é o que permite dois módulos
// serem escritos ao mesmo tempo sem disputar este arquivo. Acrescentar módulo é
// criar rotas_<modulo>.go e somar uma linha nesta lista.
func Rotas(d Deps) []Rota {
	var todas []Rota
	for _, grupo := range []func(Deps) []Rota{
		rotasNucleo,
		rotasInventario,
		rotasTarifario,
		rotasDisponibilidade,
		rotasReservas,
		rotasCRM,
		rotasStream,
		// Adaptador de uma linha: `rotasContatos` recebe o handler direto, e não
		// `Deps`, porque foi escrito antes de o campo existir (ver o cabeçalho de
		// rotas_contatos.go). Manter a assinatura como está custa esta linha e
		// evita reescrever um arquivo de outro agente.
		func(d Deps) []Rota { return rotasContatos(d.Contatos) },
	} {
		todas = append(todas, grupo(d)...)
	}
	return todas
}

// rotasNucleo são as rotas que já existiam antes da Fase 1: sonda, sessão,
// usuários e perfis.
func rotasNucleo(d Deps) []Rota {
	return []Rota{
		// ───────── Saúde ─────────
		{Metodo: http.MethodGet, Path: "/healthz", Acesso: AcessoPublico, NaRaiz: true, Handler: d.Saude.Vivo},
		{Metodo: http.MethodGet, Path: "/readyz", Acesso: AcessoPublico, NaRaiz: true, Handler: d.Saude.Pronto},

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

// rotasDeLongaDuracao são os paths que NÃO podem passar pelo teto de tempo por
// requisição.
//
// O teto (`middleware.Timeout` no router.go) existe para consulta presa não
// segurar conexão para sempre. Só que "para sempre" é justamente o contrato do
// SSE: `/stream` fica aberto enquanto o operador tiver o mapa na tela, e sob o
// teto ele cairia a cada 50 segundos — todas as vezes, para todo mundo.
//
// A exceção é declarada por PATH, e não descoberta por heurística, porque a
// consequência de errar nos dois sentidos é grave e silenciosa: rota de longa
// duração dentro do teto morre de minuto em minuto sem erro nenhum no log; rota
// comum fora do teto segura uma conexão de banco indefinidamente. `ValidarTabela`
// confere que todo path listado aqui existe de fato na tabela — sem isso,
// renomear `/stream` devolveria o SSE ao teto sem uma linha vermelha em lugar
// nenhum.
var rotasDeLongaDuracao = map[string]bool{
	"/stream": true,
}

// EhDeLongaDuracao diz se a rota fica aberta por tempo indeterminado.
func EhDeLongaDuracao(r Rota) bool { return rotasDeLongaDuracao[r.Path] }

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

	// A exceção ao teto de tempo tem de apontar para rota que existe. Um path
	// listado e ausente da tabela é uma exceção que não protege nada — e o
	// sintoma seria o SSE caindo de 50 em 50 segundos com tudo verde.
	paths := map[string]bool{}
	for _, r := range rotas {
		paths[r.Path] = true
	}
	for path := range rotasDeLongaDuracao {
		if !paths[path] {
			return fmt.Errorf("%s está declarado como rota de longa duração, mas não existe na tabela: "+
				"ou o path foi renomeado (e a rota voltou para o teto de 50s sem ninguém notar), ou a linha aqui sobrou", path)
		}
	}
	return nil
}
