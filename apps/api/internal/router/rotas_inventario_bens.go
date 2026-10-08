package router

import (
	"net/http"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/bens"
)

// RotaDeEnvioDeFotoDeBem é o upload da foto de bem: até 15 MB subindo pelo
// celular de dentro de um apartamento, fora do teto de 50 s por requisição
// (rotasDeLongaDuracao, em routes.go).
const RotaDeEnvioDeFotoDeBem = "/inventory/media"

// RotaDaFotoDeBem é a ENTREGA autenticada da foto, também fora do teto de
// 50 s (rotasDeLongaDuracao): os mesmos 15 MB descendo pela mesma conexão. O
// handler estende o próprio prazo de escrita.
const RotaDaFotoDeBem = "/inventory/media/{id}"

// rotasBens declara as rotas do inventário de bens por ambiente (tag `Bens`
// da OpenAPI, 19 paths e 40 operações).
//
// Este arquivo pertence ao módulo `internal/modules/bens`, como
// rotas_inventario.go pertence ao inventário comercial. Sem guarda contra
// handler nulo, de propósito: o teste de contrato monta a tabela com Deps
// zerado e precisa enxergar estas linhas — montar a tabela só cria method
// values, não os chama. A contrapartida é de quem monta as dependências:
// `Deps.Bens` PRECISA ser preenchido com bens.NovoHandler(pool, tx, MEDIA_DIR).
//
// Recurso `inventory.goods` em todas, e o par (recurso, ação) é o `x-rbac` de
// cada operação no openapi.yaml. NENHUMA é pública: a foto mostra o interior da
// casa, e o resto é a planta dela. Sem escopo `own` (cômodo não tem dono).
//
// Cinco coleções expõem os seis verbos (`/rooms`, `/inventory/items`,
// `/inventory/placements`, `/inventory/counts`, `/inventory/issues`). As ações de
// domínio são sub-recursos — `/close`, `/lines/{lineId}`, `/copy`, `/photos` —,
// nunca campo mágico no PATCH.
func rotasBens(d Deps) []Rota {
	const rec = bens.Recurso

	return []Rota{
		// ───────── Ambientes (cômodos) ─────────
		{Metodo: http.MethodGet, Path: "/rooms", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Bens.ListarAmbientes},
		{Metodo: http.MethodPost, Path: "/rooms", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoCriar, Handler: d.Bens.CriarAmbiente},
		{Metodo: http.MethodGet, Path: "/rooms/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Bens.BuscarAmbiente},
		{Metodo: http.MethodPut, Path: "/rooms/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Bens.SubstituirAmbiente},
		{Metodo: http.MethodPatch, Path: "/rooms/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Bens.AtualizarAmbiente},
		{Metodo: http.MethodDelete, Path: "/rooms/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoExcluir, Handler: d.Bens.ApagarAmbiente},

		// ───────── Catálogo de bens ─────────
		{Metodo: http.MethodGet, Path: "/inventory/items", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Bens.ListarBens},
		{Metodo: http.MethodPost, Path: "/inventory/items", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoCriar, Handler: d.Bens.CriarBem},
		{Metodo: http.MethodGet, Path: "/inventory/items/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Bens.BuscarBem},
		{Metodo: http.MethodPut, Path: "/inventory/items/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Bens.SubstituirBem},
		{Metodo: http.MethodPatch, Path: "/inventory/items/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Bens.AtualizarBem},
		{Metodo: http.MethodDelete, Path: "/inventory/items/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoExcluir, Handler: d.Bens.ApagarBem},
		// A galeria é EDITAR: troca a apresentação de um bem que já existe.
		{Metodo: http.MethodGet, Path: "/inventory/items/{id}/photos", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Bens.FotosDoBem},
		{Metodo: http.MethodPut, Path: "/inventory/items/{id}/photos", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Bens.SubstituirFotos},

		// ───────── Colocações (quanto de cada bem em cada ambiente) ─────────
		{Metodo: http.MethodGet, Path: "/inventory/placements", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Bens.ListarColocacoes},
		{Metodo: http.MethodPost, Path: "/inventory/placements", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoCriar, Handler: d.Bens.CriarColocacao},
		{Metodo: http.MethodGet, Path: "/inventory/placements/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Bens.BuscarColocacao},
		{Metodo: http.MethodPut, Path: "/inventory/placements/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Bens.SubstituirColocacao},
		{Metodo: http.MethodPatch, Path: "/inventory/placements/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Bens.AtualizarColocacao},
		{Metodo: http.MethodDelete, Path: "/inventory/placements/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoExcluir, Handler: d.Bens.ApagarColocacao},

		// ───────── Fotos — autenticadas, nunca públicas ─────────
		{Metodo: http.MethodPost, Path: RotaDeEnvioDeFotoDeBem, Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoCriar, Handler: d.Bens.EnviarFoto},
		{Metodo: http.MethodGet, Path: RotaDaFotoDeBem, Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Bens.Foto},

		// ───────── O seletor de unidade e a tela dela ─────────
		// `/inventory/units` existe porque `/units` é do cadastro comercial
		// (`inventory:ver`): quem só tem `inventory.goods` não teria onde
		// escolher a unidade. Devolve id, code, name, active e os números do
		// inventário — nada de tarifa, capacidade, composição ou ocupação.
		{Metodo: http.MethodGet, Path: "/inventory/units", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Bens.ListarUnidades},
		// Só `inventory.goods:ver`: da unidade sai id, code e name, e nada do
		// cadastro comercial (recurso `inventory`).
		{Metodo: http.MethodGet, Path: "/units/{id}/inventory", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Bens.InventarioDaUnidade},
		// CRIAR: a cópia faz nascer cômodos e colocações no destino.
		{Metodo: http.MethodPost, Path: "/units/{id}/inventory/copy", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoCriar, Handler: d.Bens.CopiarInventario},

		// ───────── Conferências ─────────
		{Metodo: http.MethodGet, Path: "/inventory/counts", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Bens.ListarConferencias},
		{Metodo: http.MethodPost, Path: "/inventory/counts", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoCriar, Handler: d.Bens.AbrirConferencia},
		{Metodo: http.MethodGet, Path: "/inventory/counts/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Bens.BuscarConferencia},
		{Metodo: http.MethodPut, Path: "/inventory/counts/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Bens.SubstituirConferencia},
		{Metodo: http.MethodPatch, Path: "/inventory/counts/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Bens.AtualizarConferencia},
		// DELETE cancela (não apaga) — e é EXCLUIR, como no contrato.
		{Metodo: http.MethodDelete, Path: "/inventory/counts/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoExcluir, Handler: d.Bens.CancelarConferencia},
		// O gesto do celular e o fechamento são EDITAR: quem conta não precisa
		// poder apagar nada.
		{Metodo: http.MethodPatch, Path: "/inventory/counts/{id}/lines/{lineId}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Bens.ContarLinha},
		{Metodo: http.MethodPost, Path: "/inventory/counts/{id}/close", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Bens.FecharConferencia},

		// ───────── Avarias ─────────
		{Metodo: http.MethodGet, Path: "/inventory/issues", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Bens.ListarAvarias},
		{Metodo: http.MethodPost, Path: "/inventory/issues", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoCriar, Handler: d.Bens.CriarAvaria},
		{Metodo: http.MethodGet, Path: "/inventory/issues/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Bens.BuscarAvaria},
		{Metodo: http.MethodPut, Path: "/inventory/issues/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Bens.SubstituirAvaria},
		{Metodo: http.MethodPatch, Path: "/inventory/issues/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Bens.AtualizarAvaria},
		{Metodo: http.MethodDelete, Path: "/inventory/issues/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoExcluir, Handler: d.Bens.ApagarAvaria},

		// ───────── Exportação ─────────
		{Metodo: http.MethodGet, Path: "/inventory/export", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Bens.Exportar},
	}
}
