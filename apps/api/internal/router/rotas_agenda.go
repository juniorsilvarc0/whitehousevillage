package router

import (
	"net/http"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/agenda"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/tarifario"
)

// rotasAgenda declara as rotas da agenda operacional (tag `Agenda` da OpenAPI,
// 6 paths e 15 operações; spec §9, backlog F2-19 a F2-21).
//
// Escrito pelo tech-lead junto com o contrato, e não pelo módulo: os nomes dos
// métodos abaixo SÃO o contrato entre esta tabela e `internal/modules/agenda`,
// e fixá-los antes do módulo é o que deixa as duas metades nascerem em
// paralelo sem disputa. Sem guarda contra handler nulo, como
// `rotas_manutencao.go`: o teste de contrato monta a tabela com Deps zerado e
// precisa enxergar estas linhas. `Deps.Agenda` PRECISA ser preenchido pelo
// router.go.
//
// Recurso `agenda` em todas, menos o `PUT /agenda/settings` — os pares
// (recurso, ação) são cópia do `x-rbac` de cada operação no openapi.yaml.
// NENHUMA é pública.
//
// O ESCOPO não está nesta tabela, e está no SQL do módulo: `own` na agenda é
// `assignee_id = <ator>` na lista, no detalhe, na edição, na conclusão e no
// cancelamento, com `404` (nunca `403`) fora dele. Em `/agenda/blocks`, que
// não tem responsável, a escrita em `own` é `403`
// (`own_scope_not_applicable`) e a leitura vale em qualquer escopo, como a de
// `/agenda/settings`.
//
// As transições são sub-recurso (`/complete`) e o `DELETE` é desmarcar —
// cancela, não apaga —, e por isso pede `excluir`, como
// `DELETE /maintenance-orders/{id}`. Concluir pede `editar`.
//
// Gravar os horários pede `settings:editar`, e não `agenda:editar`: é o mesmo
// recurso que guarda o tarifário e as políticas (`rotas_tarifario.go`) — o que
// a casa promete ao hóspede é configuração de gestão, e o perfil `usuario`,
// que opera a agenda em `all`, é "operação sem configurações" (spec §1).
func rotasAgenda(d Deps) []Rota {
	const rec = agenda.Recurso // "agenda"
	h := d.Agenda

	return []Rota{
		// ───────── Eventos ─────────
		{Metodo: http.MethodGet, Path: "/agenda/events", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: h.Listar},
		{Metodo: http.MethodPost, Path: "/agenda/events", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoCriar, Handler: h.Criar},
		{Metodo: http.MethodGet, Path: "/agenda/events/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: h.Buscar},
		{Metodo: http.MethodPut, Path: "/agenda/events/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: h.Substituir},
		{Metodo: http.MethodPatch, Path: "/agenda/events/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: h.Atualizar},
		// DELETE desmarca (status `cancelado`), não apaga — EXCLUIR.
		{Metodo: http.MethodDelete, Path: "/agenda/events/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoExcluir, Handler: h.Cancelar},
		{Metodo: http.MethodPost, Path: "/agenda/events/{id}/complete", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: h.Concluir},

		// ───────── Bloqueios de horário da operação ─────────
		{Metodo: http.MethodGet, Path: "/agenda/blocks", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: h.ListarBloqueios},
		{Metodo: http.MethodPost, Path: "/agenda/blocks", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoCriar, Handler: h.CriarBloqueio},
		{Metodo: http.MethodGet, Path: "/agenda/blocks/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: h.BuscarBloqueio},
		{Metodo: http.MethodPut, Path: "/agenda/blocks/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: h.SubstituirBloqueio},
		{Metodo: http.MethodPatch, Path: "/agenda/blocks/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: h.AtualizarBloqueio},
		{Metodo: http.MethodDelete, Path: "/agenda/blocks/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoExcluir, Handler: h.RemoverBloqueio},

		// ───────── Horários da operação ─────────
		{Metodo: http.MethodGet, Path: "/agenda/settings", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: h.Configuracao},
		{Metodo: http.MethodPut, Path: "/agenda/settings", Acesso: AcessoPermissao, Recurso: tarifario.RecursoConfiguracoes, Acao: auth.AcaoEditar, Handler: h.GravarConfiguracao},
	}
}
