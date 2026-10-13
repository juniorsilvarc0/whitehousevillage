package router

import (
	"net/http"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/manutencao"
)

// rotasManutencao declara as rotas das ordens de manutenção (tag `Manutenção`
// da OpenAPI, 5 paths e 10 operações).
//
// Escrito pelo tech-lead junto com o contrato, e não pelo módulo: os nomes dos
// métodos abaixo SÃO o contrato entre esta tabela e `internal/modules/
// manutencao`, e fixá-los aqui antes do módulo é o que deixa as duas metades
// nascerem em paralelo sem disputa. Sem guarda contra handler nulo, como
// `rotas_inventario_bens.go`: o teste de contrato monta a tabela com Deps
// zerado e precisa enxergar estas linhas. `Deps.Manutencao` PRECISA ser
// preenchido pelo router.go.
//
// Recurso `maintenance` em todas — o par (recurso, ação) é o `x-rbac` de cada
// operação no openapi.yaml. NENHUMA é pública. Sem escopo `own`: a ordem é da
// casa, não de quem a abriu.
//
// As transições são sub-recursos (`/start`, `/complete`), nunca `status`
// editável no PATCH: cada uma carrega efeito obrigatório no calendário e na
// avaria. Cancelar é o `DELETE` — o "excluir" de um registro com histórico —, e
// por isso pede `excluir`, não `editar`.
//
// O bloqueio da ordem pede só `maintenance:editar`, e não `calendar:*`: ele é
// consequência da ordem (spec §12). Conceder `maintenance` é conceder bloquear
// a unidade DA ordem — está escrito na tag do contrato.
func rotasManutencao(d Deps) []Rota {
	const rec = manutencao.Recurso // "maintenance"

	return []Rota{
		{Metodo: http.MethodGet, Path: "/maintenance-orders", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Manutencao.Listar},
		{Metodo: http.MethodPost, Path: "/maintenance-orders", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoCriar, Handler: d.Manutencao.Criar},
		{Metodo: http.MethodGet, Path: "/maintenance-orders/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Manutencao.Buscar},
		{Metodo: http.MethodPut, Path: "/maintenance-orders/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Manutencao.Substituir},
		{Metodo: http.MethodPatch, Path: "/maintenance-orders/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Manutencao.Atualizar},
		// DELETE cancela (não apaga), e libera o calendário — EXCLUIR, como
		// `DELETE /inventory/counts/{id}`.
		{Metodo: http.MethodDelete, Path: "/maintenance-orders/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoExcluir, Handler: d.Manutencao.Cancelar},

		// ───────── Transições ─────────
		{Metodo: http.MethodPost, Path: "/maintenance-orders/{id}/start", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Manutencao.Iniciar},
		{Metodo: http.MethodPost, Path: "/maintenance-orders/{id}/complete", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Manutencao.Concluir},

		// ───────── O bloqueio de calendário da ordem ─────────
		{Metodo: http.MethodPut, Path: "/maintenance-orders/{id}/block", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Manutencao.Bloquear},
		{Metodo: http.MethodDelete, Path: "/maintenance-orders/{id}/block", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Manutencao.SoltarBloqueio},
	}
}
