package router

import (
	"net/http"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/tarifario"
)

// rotasTarifario declara as rotas do módulo de tarifario.
//
// Este arquivo pertence a quem implementa o módulo: mexer só aqui evita
// disputa com os outros módulos que estão sendo escritos em paralelo.
// Devolver nil enquanto o handler não existe mantém a tabela válida — o teste
// de contrato só cobra o que a OpenAPI declara.
//
// Todas as linhas declaram o recurso `settings`: é o `x-rbac` que a OpenAPI
// atribui a cada operação do tarifário. Configuração de preço e de política é
// acesso de gestão, não de quem só vende.
func rotasTarifario(d Deps) []Rota {
	if d.Tarifario == nil {
		return nil
	}

	const recurso = tarifario.RecursoConfiguracoes
	h := d.Tarifario

	return []Rota{
		// ───────── Tabelas de tarifas ─────────
		{Metodo: http.MethodGet, Path: "/rate-tables", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoVer, Handler: h.ListarTabelas},
		{Metodo: http.MethodPost, Path: "/rate-tables", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoCriar, Handler: h.CriarTabela},
		{Metodo: http.MethodGet, Path: "/rate-tables/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoVer, Handler: h.BuscarTabela},
		{Metodo: http.MethodPut, Path: "/rate-tables/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoEditar, Handler: h.SubstituirTabela},
		{Metodo: http.MethodPatch, Path: "/rate-tables/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoEditar, Handler: h.AtualizarTabela},
		{Metodo: http.MethodDelete, Path: "/rate-tables/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoExcluir, Handler: h.DesativarTabela},

		// ───────── Tarifas ─────────
		// A rota estática vem antes da paramétrica na leitura humana; o chi
		// resolve a precedência sozinho, mas a ordem aqui evita a dúvida de quem
		// revisa (`/rates/bulk` não é um `{id}`).
		{Metodo: http.MethodGet, Path: "/rates", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoVer, Handler: h.ListarTarifas},
		{Metodo: http.MethodPost, Path: "/rates", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoCriar, Handler: h.CriarTarifa},
		// `bulk` é `editar`, e não `criar`: ele SUBSTITUI a grade inteira, e
		// quem só pode acrescentar linha não deveria poder apagar as outras 23.
		{Metodo: http.MethodPost, Path: "/rates/bulk", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoEditar, Handler: h.SalvarGrade},
		{Metodo: http.MethodGet, Path: "/rates/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoVer, Handler: h.BuscarTarifa},
		{Metodo: http.MethodPut, Path: "/rates/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoEditar, Handler: h.SubstituirTarifa},
		{Metodo: http.MethodPatch, Path: "/rates/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoEditar, Handler: h.AtualizarTarifa},
		{Metodo: http.MethodDelete, Path: "/rates/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoExcluir, Handler: h.ExcluirTarifa},

		// ───────── Feriados ─────────
		{Metodo: http.MethodGet, Path: "/holidays", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoVer, Handler: h.ListarFeriados},
		{Metodo: http.MethodPost, Path: "/holidays", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoCriar, Handler: h.CriarFeriado},
		{Metodo: http.MethodGet, Path: "/holidays/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoVer, Handler: h.BuscarFeriado},
		{Metodo: http.MethodPut, Path: "/holidays/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoEditar, Handler: h.SubstituirFeriado},
		{Metodo: http.MethodPatch, Path: "/holidays/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoEditar, Handler: h.AtualizarFeriado},
		{Metodo: http.MethodDelete, Path: "/holidays/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoExcluir, Handler: h.ExcluirFeriado},

		// ───────── Períodos especiais ─────────
		{Metodo: http.MethodGet, Path: "/special-periods", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoVer, Handler: h.ListarPeriodos},
		{Metodo: http.MethodPost, Path: "/special-periods", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoCriar, Handler: h.CriarPeriodo},
		{Metodo: http.MethodGet, Path: "/special-periods/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoVer, Handler: h.BuscarPeriodo},
		{Metodo: http.MethodPut, Path: "/special-periods/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoEditar, Handler: h.SubstituirPeriodo},
		{Metodo: http.MethodPatch, Path: "/special-periods/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoEditar, Handler: h.AtualizarPeriodo},
		{Metodo: http.MethodDelete, Path: "/special-periods/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoExcluir, Handler: h.ExcluirPeriodo},

		// ───────── Mínimo de noites ─────────
		{Metodo: http.MethodGet, Path: "/min-nights", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoVer, Handler: h.ListarMinimos},
		{Metodo: http.MethodPost, Path: "/min-nights", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoCriar, Handler: h.CriarMinimo},
		{Metodo: http.MethodGet, Path: "/min-nights/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoVer, Handler: h.BuscarMinimo},
		{Metodo: http.MethodPut, Path: "/min-nights/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoEditar, Handler: h.SubstituirMinimo},
		{Metodo: http.MethodPatch, Path: "/min-nights/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoEditar, Handler: h.AtualizarMinimo},
		{Metodo: http.MethodDelete, Path: "/min-nights/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoExcluir, Handler: h.ExcluirMinimo},

		// ───────── Políticas versionadas ─────────
		// Não há PATCH nem DELETE aqui de propósito: política é histórico, não
		// cadastro. O PUT publica uma versão nova, e voltar atrás é publicar de
		// novo a anterior — o que deixa rastro em vez de apagar o que houve.
		{Metodo: http.MethodGet, Path: "/policies/commercial", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoVer, Handler: h.PoliticaComercial},
		{Metodo: http.MethodPut, Path: "/policies/commercial", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoEditar, Handler: h.PublicarPoliticaComercial},
		{Metodo: http.MethodGet, Path: "/policies/cancellation", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoVer, Handler: h.PoliticaDeCancelamento},
		{Metodo: http.MethodPut, Path: "/policies/cancellation", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoEditar, Handler: h.PublicarPoliticaDeCancelamento},
	}
}
