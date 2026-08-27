package router

import (
	"net/http"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/inventario"
)

// rotasInventario declara as rotas do módulo de inventário.
//
// Este arquivo pertence a quem implementa o módulo: mexer só aqui evita
// disputa com os outros módulos que estão sendo escritos em paralelo.
// A guarda `if d.Inventario == nil` do esqueleto foi REMOVIDA agora que o
// módulo existe, e isso é deliberado: com ela, `Rotas(Deps{})` — que é como o
// teste de contrato monta a tabela sem subir banco — devolvia zero rota daqui, e
// as dezessete linhas abaixo não eram conferidas contra a OpenAPI nem contra a
// regra dos seis verbos. Montar a tabela só cria method values, não os chama,
// então o ponteiro nulo do teste é inofensivo.
//
// A contrapartida é para quem montar o main: `Deps.Inventario` PRECISA ser
// preenchido com inventario.NovoHandler(pool, tx). Sem isso as rotas sobem
// apontando para um handler nulo.
//
// O par (recurso, ação) de cada linha é o `x-rbac` da operação correspondente
// no openapi.yaml, e o recurso é o código do catálogo semeado por
// cmd/seed/acesso.go. Não há escopo `own` aqui: apartamento não tem dono no
// sentido do RBAC, e `resources.supports_own` do `inventory` é false.
func rotasInventario(d Deps) []Rota {
	const rec = inventario.Recurso

	return []Rota{
		// ───────── Propriedades ─────────
		// Sem POST e sem DELETE de propósito: criar a casa é ato de instalação,
		// feito por migration/seed. Como a coleção não expõe POST, ela não entra
		// na conta dos "seis verbos" do teste de contrato — e é por isso que a
		// ausência aqui é legítima, e não uma omissão.
		{Metodo: http.MethodGet, Path: "/properties", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Inventario.ListarPropriedades},
		{Metodo: http.MethodGet, Path: "/properties/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Inventario.BuscarPropriedade},
		{Metodo: http.MethodPatch, Path: "/properties/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Inventario.AtualizarPropriedade},

		// ───────── Produtos (o que se vende) ─────────
		{Metodo: http.MethodGet, Path: "/unit-types", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Inventario.ListarProdutos},
		{Metodo: http.MethodPost, Path: "/unit-types", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoCriar, Handler: d.Inventario.CriarProduto},
		{Metodo: http.MethodGet, Path: "/unit-types/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Inventario.BuscarProduto},
		{Metodo: http.MethodPut, Path: "/unit-types/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Inventario.SubstituirProduto},
		{Metodo: http.MethodPatch, Path: "/unit-types/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Inventario.AtualizarProduto},
		{Metodo: http.MethodDelete, Path: "/unit-types/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoExcluir, Handler: d.Inventario.DesativarProduto},

		// A composição é EDITAR, não CRIAR: ela troca a definição de um produto
		// que já existe. Quem pode cadastrar produto sem poder editá-lo não pode
		// redefinir o que ele consome.
		{Metodo: http.MethodGet, Path: "/unit-types/{id}/members", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Inventario.Composicao},
		{Metodo: http.MethodPut, Path: "/unit-types/{id}/members", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Inventario.SubstituirComposicao},

		// ───────── Unidades físicas (o que se ocupa) ─────────
		{Metodo: http.MethodGet, Path: "/units", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Inventario.ListarUnidades},
		{Metodo: http.MethodPost, Path: "/units", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoCriar, Handler: d.Inventario.CriarUnidade},
		{Metodo: http.MethodGet, Path: "/units/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Inventario.BuscarUnidade},
		{Metodo: http.MethodPut, Path: "/units/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Inventario.SubstituirUnidade},
		{Metodo: http.MethodPatch, Path: "/units/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Inventario.AtualizarUnidade},
		{Metodo: http.MethodDelete, Path: "/units/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoExcluir, Handler: d.Inventario.DesativarUnidade},
	}
}
