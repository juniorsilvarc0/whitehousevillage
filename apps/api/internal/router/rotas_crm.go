package router

import (
	"log/slog"
	"net/http"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/crm"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// rotasCRM declara as rotas do módulo de CRM.
//
// Este arquivo pertence a quem implementa o módulo: mexer só aqui evita disputa
// com os outros módulos escritos em paralelo.
//
// A guarda `if d.CRM == nil { return nil }` do esqueleto foi REMOVIDA agora que
// o módulo existe — pela mesma razão registrada em `rotas_reservas.go`: com
// ela, `Rotas(Deps{})` (que é como o teste de contrato monta a tabela sem subir
// banco) devolveria zero rota daqui, e as 45 linhas abaixo não seriam
// conferidas contra a OpenAPI nem contra a regra dos seis verbos.
//
// A CONTRAPARTIDA: `Deps.CRM` precisa ser preenchido pelo main.
// `internal/router/router.go` NÃO é pasta deste agente — ver "PARA O
// INTEGRADOR" no relatório: falta `CRM: crm.NovoHandler(o.Pool, tx)` na
// montagem das Deps.
//
// Enquanto essa linha não existir, as rotas apontam para `naoMontado`, e não
// para um ponteiro nulo. A diferença importa: um method value sobre `*Handler`
// nulo compila e SÓ estoura quando chamado — o processo sobe verde e o primeiro
// operador que abrir o kanban recebe um 500 de panic sem nenhuma pista do
// porquê. O 503 abaixo diz o nome do campo que falta, no corpo da resposta e no
// log, e some sozinho assim que o main montar o módulo.
//
// Os pares (recurso, ação) são cópia literal do `x-rbac` de cada operação no
// openapi.yaml, e os códigos vêm do catálogo semeado em cmd/seed/acesso.go —
// que NÃO muda nesta rodada.
func rotasCRM(d Deps) []Rota {
	// Placeholder de módulo não montado. NÃO é a guarda `return nil` do
	// esqueleto: a tabela continua inteira, então o teste de contrato varre as
	// 45 linhas contra a OpenAPI mesmo quando `Rotas(Deps{})` é montada sem
	// nenhum handler — que é exatamente como o CI a monta.
	naoMontado := func(w http.ResponseWriter, r *http.Request) {
		slog.ErrorContext(r.Context(), "módulo de CRM não montado no router",
			"rota", r.Method+" "+r.URL.Path, "correcao", "Deps.CRM = crm.NovoHandler(pool, tx)")
		httpx.Error(w, r, apperr.Internal.
			WithMessage("O módulo de CRM não foi montado neste processo (Deps.CRM está vazio).").
			WithStatus(http.StatusServiceUnavailable))
	}

	handler := func(metodo func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		if d.CRM == nil {
			return naoMontado
		}
		return metodo
	}

	const (
		// Etapas e motivos de perda mapeiam em `crm.pipelines`, e não em
		// recursos próprios: os três são CONFIGURAÇÃO DO FUNIL, e é isso que
		// faz o corretor — que tem `crm.pipelines` em somente-leitura no seed —
		// conseguir ler o catálogo de motivos que o `/lose` dele exige. Recurso
		// separado obrigaria uma linha nova na matriz para um dado que ninguém
		// edita sem editar o funil junto.
		funis         = crm.RecursoFunis
		leads         = crm.RecursoLeads
		oportunidades = crm.RecursoOportunidades
		atividades    = crm.RecursoAtividades
	)

	return []Rota{
		// ───────── Funis ─────────
		{Metodo: http.MethodGet, Path: "/crm/pipelines", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoVer, Handler: handler(d.CRM.ListarFunis)},
		{Metodo: http.MethodPost, Path: "/crm/pipelines", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoCriar, Handler: handler(d.CRM.CriarFunil)},
		{Metodo: http.MethodGet, Path: "/crm/pipelines/{id}", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoVer, Handler: handler(d.CRM.BuscarFunil)},
		{Metodo: http.MethodPut, Path: "/crm/pipelines/{id}", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoEditar, Handler: handler(d.CRM.SubstituirFunil)},
		{Metodo: http.MethodPatch, Path: "/crm/pipelines/{id}", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoEditar, Handler: handler(d.CRM.AtualizarFunil)},
		{Metodo: http.MethodDelete, Path: "/crm/pipelines/{id}", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoExcluir, Handler: handler(d.CRM.DesativarFunil)},

		// ───────── Etapas ─────────
		{Metodo: http.MethodGet, Path: "/crm/stages", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoVer, Handler: handler(d.CRM.ListarEtapas)},
		{Metodo: http.MethodPost, Path: "/crm/stages", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoCriar, Handler: handler(d.CRM.CriarEtapa)},
		// A rota estática vem ANTES da paramétrica: o chi resolve a precedência
		// sozinho, mas a tabela é lida por gente na ordem em que está escrita.
		{Metodo: http.MethodPost, Path: "/crm/stages/reorder", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoEditar, Handler: handler(d.CRM.Reordenar)},
		{Metodo: http.MethodGet, Path: "/crm/stages/{id}", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoVer, Handler: handler(d.CRM.BuscarEtapa)},
		{Metodo: http.MethodPut, Path: "/crm/stages/{id}", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoEditar, Handler: handler(d.CRM.SubstituirEtapa)},
		{Metodo: http.MethodPatch, Path: "/crm/stages/{id}", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoEditar, Handler: handler(d.CRM.AtualizarEtapa)},
		{Metodo: http.MethodDelete, Path: "/crm/stages/{id}", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoExcluir, Handler: handler(d.CRM.ExcluirEtapa)},

		// ───────── Motivos de perda ─────────
		{Metodo: http.MethodGet, Path: "/crm/lost-reasons", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoVer, Handler: handler(d.CRM.ListarMotivos)},
		{Metodo: http.MethodPost, Path: "/crm/lost-reasons", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoCriar, Handler: handler(d.CRM.CriarMotivo)},
		{Metodo: http.MethodGet, Path: "/crm/lost-reasons/{id}", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoVer, Handler: handler(d.CRM.BuscarMotivo)},
		{Metodo: http.MethodPut, Path: "/crm/lost-reasons/{id}", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoEditar, Handler: handler(d.CRM.SubstituirMotivo)},
		{Metodo: http.MethodPatch, Path: "/crm/lost-reasons/{id}", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoEditar, Handler: handler(d.CRM.AtualizarMotivo)},
		{Metodo: http.MethodDelete, Path: "/crm/lost-reasons/{id}", Acesso: AcessoPermissao, Recurso: funis, Acao: auth.AcaoExcluir, Handler: handler(d.CRM.DesativarMotivo)},

		// ───────── Leads ─────────
		{Metodo: http.MethodGet, Path: "/crm/leads", Acesso: AcessoPermissao, Recurso: leads, Acao: auth.AcaoVer, Handler: handler(d.CRM.ListarLeads)},
		{Metodo: http.MethodPost, Path: "/crm/leads", Acesso: AcessoPermissao, Recurso: leads, Acao: auth.AcaoCriar, Handler: handler(d.CRM.CriarLead)},
		{Metodo: http.MethodGet, Path: "/crm/leads/{id}", Acesso: AcessoPermissao, Recurso: leads, Acao: auth.AcaoVer, Handler: handler(d.CRM.BuscarLead)},
		{Metodo: http.MethodPut, Path: "/crm/leads/{id}", Acesso: AcessoPermissao, Recurso: leads, Acao: auth.AcaoEditar, Handler: handler(d.CRM.SubstituirLead)},
		{Metodo: http.MethodPatch, Path: "/crm/leads/{id}", Acesso: AcessoPermissao, Recurso: leads, Acao: auth.AcaoEditar, Handler: handler(d.CRM.AtualizarLead)},
		{Metodo: http.MethodDelete, Path: "/crm/leads/{id}", Acesso: AcessoPermissao, Recurso: leads, Acao: auth.AcaoExcluir, Handler: handler(d.CRM.ExcluirLead)},

		// Converter é `editar` do LEAD, e não `criar` de oportunidade: o ato é
		// sobre o lead, e quem pode trabalhar a fila pode promovê-la. Exigir
		// `crm.opportunities:criar` aqui deixaria o corretor com um lead
		// qualificado e sem como abrir o card.
		{Metodo: http.MethodPost, Path: "/crm/leads/{id}/convert", Acesso: AcessoPermissao, Recurso: leads, Acao: auth.AcaoEditar, Handler: handler(d.CRM.Converter)},

		// ───────── Oportunidades ─────────
		{Metodo: http.MethodGet, Path: "/crm/opportunities", Acesso: AcessoPermissao, Recurso: oportunidades, Acao: auth.AcaoVer, Handler: handler(d.CRM.ListarOportunidades)},
		{Metodo: http.MethodPost, Path: "/crm/opportunities", Acesso: AcessoPermissao, Recurso: oportunidades, Acao: auth.AcaoCriar, Handler: handler(d.CRM.CriarOportunidade)},
		{Metodo: http.MethodGet, Path: "/crm/opportunities/kanban", Acesso: AcessoPermissao, Recurso: oportunidades, Acao: auth.AcaoVer, Handler: handler(d.CRM.Kanban)},
		{Metodo: http.MethodGet, Path: "/crm/opportunities/{id}", Acesso: AcessoPermissao, Recurso: oportunidades, Acao: auth.AcaoVer, Handler: handler(d.CRM.BuscarOportunidade)},
		{Metodo: http.MethodPut, Path: "/crm/opportunities/{id}", Acesso: AcessoPermissao, Recurso: oportunidades, Acao: auth.AcaoEditar, Handler: handler(d.CRM.SubstituirOportunidade)},
		{Metodo: http.MethodPatch, Path: "/crm/opportunities/{id}", Acesso: AcessoPermissao, Recurso: oportunidades, Acao: auth.AcaoEditar, Handler: handler(d.CRM.AtualizarOportunidade)},
		{Metodo: http.MethodDelete, Path: "/crm/opportunities/{id}", Acesso: AcessoPermissao, Recurso: oportunidades, Acao: auth.AcaoExcluir, Handler: handler(d.CRM.ExcluirOportunidade)},
		{Metodo: http.MethodGet, Path: "/crm/opportunities/{id}/full", Acesso: AcessoPermissao, Recurso: oportunidades, Acao: auth.AcaoVer, Handler: handler(d.CRM.Completa)},

		// As três ações de estado são `editar`, e nenhuma é `criar`: mudar o
		// desfecho de um card que já existe é edição. `/win` CRIA RESERVA, e
		// mesmo assim continua sendo `crm.opportunities:editar` — é o que o
		// contrato declara em `x-rbac`, e é o que permite ao corretor fechar a
		// própria venda sem receber `reservations:criar`, que lhe daria emitir
		// reserva para qualquer um por fora do funil.
		{Metodo: http.MethodPost, Path: "/crm/opportunities/{id}/stage", Acesso: AcessoPermissao, Recurso: oportunidades, Acao: auth.AcaoEditar, Handler: handler(d.CRM.MudarEtapa)},
		{Metodo: http.MethodPost, Path: "/crm/opportunities/{id}/win", Acesso: AcessoPermissao, Recurso: oportunidades, Acao: auth.AcaoEditar, Handler: handler(d.CRM.Ganhar)},
		{Metodo: http.MethodPost, Path: "/crm/opportunities/{id}/lose", Acesso: AcessoPermissao, Recurso: oportunidades, Acao: auth.AcaoEditar, Handler: handler(d.CRM.Perder)},

		// ───────── Atividades ─────────
		{Metodo: http.MethodGet, Path: "/crm/activities", Acesso: AcessoPermissao, Recurso: atividades, Acao: auth.AcaoVer, Handler: handler(d.CRM.ListarAtividades)},
		{Metodo: http.MethodPost, Path: "/crm/activities", Acesso: AcessoPermissao, Recurso: atividades, Acao: auth.AcaoCriar, Handler: handler(d.CRM.CriarAtividade)},
		{Metodo: http.MethodGet, Path: "/crm/activities/{id}", Acesso: AcessoPermissao, Recurso: atividades, Acao: auth.AcaoVer, Handler: handler(d.CRM.BuscarAtividade)},
		{Metodo: http.MethodPut, Path: "/crm/activities/{id}", Acesso: AcessoPermissao, Recurso: atividades, Acao: auth.AcaoEditar, Handler: handler(d.CRM.SubstituirAtividade)},
		{Metodo: http.MethodPatch, Path: "/crm/activities/{id}", Acesso: AcessoPermissao, Recurso: atividades, Acao: auth.AcaoEditar, Handler: handler(d.CRM.AtualizarAtividade)},
		{Metodo: http.MethodDelete, Path: "/crm/activities/{id}", Acesso: AcessoPermissao, Recurso: atividades, Acao: auth.AcaoExcluir, Handler: handler(d.CRM.ExcluirAtividade)},
		{Metodo: http.MethodPost, Path: "/crm/activities/{id}/complete", Acesso: AcessoPermissao, Recurso: atividades, Acao: auth.AcaoEditar, Handler: handler(d.CRM.Concluir)},
	}
}
