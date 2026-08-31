package router

import (
	"log/slog"
	"net/http"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/contatos"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// rotasContatos declara as rotas do módulo de contatos.
//
// # PARA O INTEGRADOR — duas linhas em routes.go, que NÃO é pasta deste agente
//
//  1. o campo em `Deps`:
//
//     Contatos *contatos.Handler
//
//  2. o grupo na lista de `Rotas`, como adaptador de uma linha:
//
//     func(d Deps) []Rota { return rotasContatos(d.Contatos) },
//
// E, em `internal/router/router.go`, a montagem:
//
//	Contatos: contatos.NovoHandler(o.Pool, tx),
//
// # Por que a assinatura é `*contatos.Handler` e não `Deps`
//
// Os outros arquivos `rotas_<modulo>.go` recebem `Deps` porque o campo deles já
// existe. O de contatos não existe ainda, e escrever `d.Contatos` aqui hoje
// deixaria o PACOTE INTEIRO sem compilar — o router pararia de subir, e não só
// esta rota. Receber o handler direto compila agora, e o adaptador acima é a
// tradução de uma linha no dia em que o campo entrar. Trocar a assinatura para
// `Deps` depois é uma edição de duas linhas neste arquivo.
//
// # Enquanto o main não montar o módulo
//
// As rotas apontam para `naoMontado`, e não para um ponteiro nulo. A diferença
// importa: um method value sobre `*Handler` nulo COMPILA e só estoura quando
// chamado — o processo sobe verde e o primeiro atendente que abrir a agenda
// recebe um 500 de panic sem pista nenhuma. O 503 abaixo diz o nome do campo
// que falta, no corpo e no log, e some sozinho assim que o main montar.
//
// Os pares (recurso, ação) são cópia literal do `x-rbac` de cada operação no
// openapi.yaml, e o código `contacts` vem do catálogo semeado em
// cmd/seed/acesso.go:64 — que NÃO muda nesta rodada.
// O `nolint:unused` que protegia esta função enquanto ela não era chamada foi
// REMOVIDO junto com a ligação em routes.go (grupo `rotasContatos(d.Contatos)`):
// a partir daqui, "rotasContatos não é chamada" volta a ser um defeito de
// verdade — quer dizer que as oito rotas sumiram da API sem nenhum teste
// reclamar.
func rotasContatos(h *contatos.Handler) []Rota {
	naoMontado := func(w http.ResponseWriter, r *http.Request) {
		slog.ErrorContext(r.Context(), "módulo de contatos não montado no router",
			"rota", r.Method+" "+r.URL.Path, "correcao", "Deps.Contatos = contatos.NovoHandler(pool, tx)")
		httpx.Error(w, r, apperr.Internal.
			WithMessage("O módulo de contatos não foi montado neste processo (Deps.Contatos está vazio).").
			WithStatus(http.StatusServiceUnavailable))
	}

	handler := func(metodo func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		if h == nil {
			return naoMontado
		}
		return metodo
	}

	const recurso = contatos.RecursoContatos

	return []Rota{
		{Metodo: http.MethodGet, Path: "/contacts", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoVer, Handler: handler(h.Listar)},
		{Metodo: http.MethodPost, Path: "/contacts", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoCriar, Handler: handler(h.Criar)},
		{Metodo: http.MethodGet, Path: "/contacts/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoVer, Handler: handler(h.Buscar)},
		{Metodo: http.MethodPut, Path: "/contacts/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoEditar, Handler: handler(h.Substituir)},
		{Metodo: http.MethodPatch, Path: "/contacts/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoEditar, Handler: handler(h.Atualizar)},
		{Metodo: http.MethodDelete, Path: "/contacts/{id}", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoExcluir, Handler: handler(h.Excluir)},

		// `/anonymize` é `excluir`, e não `editar`: o ato é a eliminação do dado
		// pessoal, irreversível. Fosse `editar`, o corretor — que tem
		// `contacts:ver` e `contacts:criar` no seed e NÃO tem `editar` — nem
		// assim alcançaria; mas quem ganhasse `editar` para corrigir um nome
		// mal digitado ganharia junto o poder de esvaziar fichas. São coisas de
		// gravidade diferente e a matriz precisa poder separá-las.
		{Metodo: http.MethodPost, Path: "/contacts/{id}/anonymize", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoExcluir, Handler: handler(h.Anonimizar)},

		// `/export` é `ver` — é leitura, ainda que a maior delas. Grava
		// `pii_access_log` como a ficha individual grava.
		{Metodo: http.MethodGet, Path: "/contacts/{id}/export", Acesso: AcessoPermissao, Recurso: recurso, Acao: auth.AcaoVer, Handler: handler(h.Exportar)},
	}
}
