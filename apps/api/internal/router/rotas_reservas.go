package router

import (
	"net/http"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/reservas"
)

// rotasReservas declara as rotas do módulo de reservas.
//
// Este arquivo pertence a quem implementa o módulo: mexer só aqui evita disputa
// com os outros módulos escritos em paralelo.
//
// A guarda `if d.Reservas == nil` do esqueleto foi REMOVIDA agora que o módulo
// existe. Com ela, `Rotas(Deps{})` — que é como o teste de contrato monta a
// tabela sem subir banco — devolvia zero rota daqui, e as dezesseis linhas
// abaixo não eram conferidas contra a OpenAPI nem contra a regra dos seis
// verbos. Montar a tabela só cria method values, não os chama, então o ponteiro
// nulo do teste é inofensivo. A contrapartida é que `Deps.Reservas` PRECISA ser
// preenchido pelo main — e é (internal/router/router.go monta os quatro módulos
// da Fase 1).
//
// Os pares (recurso, ação) são cópia literal do `x-rbac` de cada operação no
// openapi.yaml, e os códigos vêm do catálogo semeado em cmd/seed/acesso.go.
func rotasReservas(d Deps) []Rota {
	const (
		rec = reservas.Recurso           // "reservations"
		cal = reservas.RecursoCalendario // "calendar"
	)

	return []Rota{
		// ───────── Reservas ─────────
		{Metodo: http.MethodGet, Path: "/reservations", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Reservas.Listar},
		{Metodo: http.MethodPost, Path: "/reservations", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoCriar, Handler: d.Reservas.Criar},

		// Rota estática antes da paramétrica na leitura humana; o chi resolve a
		// precedência sozinho, mas a ordem aqui evita a dúvida de quem revisa.
		{Metodo: http.MethodGet, Path: "/reservations/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Reservas.Buscar},
		// PUT existe porque a tabela expõe GET+POST em /reservations, e o teste
		// de contrato exige os seis verbos de toda coleção. Ele substitui os
		// campos CADASTRAIS — datas, produto e preço não passam por aqui.
		{Metodo: http.MethodPut, Path: "/reservations/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Reservas.Substituir},
		{Metodo: http.MethodPatch, Path: "/reservations/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Reservas.Atualizar},
		{Metodo: http.MethodDelete, Path: "/reservations/{id}", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoExcluir, Handler: d.Reservas.Descartar},

		{Metodo: http.MethodGet, Path: "/reservations/{id}/full", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoVer, Handler: d.Reservas.Completa},

		// ───────── Ações do ciclo de vida ─────────
		// Todas são `editar`, e nenhuma é `criar`: mudar o estado de uma reserva
		// que já existe é edição. `/reschedule` cria uma reserva nova, mas é
		// consequência de editar a original — quem não pode editar não pode
		// jogar a venda para outra data.
		{Metodo: http.MethodPost, Path: "/reservations/{id}/confirm", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Reservas.Confirmar},
		{Metodo: http.MethodPost, Path: "/reservations/{id}/cancel", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Reservas.Cancelar},
		{Metodo: http.MethodPost, Path: "/reservations/{id}/reschedule", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Reservas.Remarcar},
		{Metodo: http.MethodPost, Path: "/reservations/{id}/check-in", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Reservas.CheckIn},
		{Metodo: http.MethodPost, Path: "/reservations/{id}/check-out", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Reservas.CheckOut},
		{Metodo: http.MethodPost, Path: "/reservations/{id}/reassign-unit", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Reservas.Realocar},
		{Metodo: http.MethodPost, Path: "/reservations/{id}/extend-hold", Acesso: AcessoPermissao, Recurso: rec, Acao: auth.AcaoEditar, Handler: d.Reservas.EstenderHold},

		// ───────── Bloqueio operacional ─────────
		// Recurso `calendar`, e não `reservations`: manutenção e uso do
		// proprietário são operação da casa, não venda. Quem cuida do
		// calendário bloqueia data sem poder emitir reserva.
		//
		// O PAR `criar` ↔ `excluir` É INVARIANTE DA MATRIZ: concede os dois ou
		// nenhum. A revisão mediu o que acontece quando ele se desfaz — o
		// corretor tinha `calendar` em ver+criar, bloqueou as oito unidades por
		// 364 dias (201) e o `DELETE` do bloqueio que ele mesmo tinha acabado de
		// criar respondeu 403. Cadeado sem chave: a casa inteira invendável por
		// um ano, e ninguém com escopo para desfazer a não ser um administrador.
		//
		// A assimetria vivia na MATRIZ (`cmd/seed/acesso.go`), não aqui — as
		// duas linhas abaixo sempre estiveram certas. O que faltava do lado do
		// código era o `own` significar alguma coisa: `stay_blocks` não tinha
		// coluna de dono, então `scope='own'` não tinha por onde virar SQL e
		// degradava em silêncio para `all`. Com a matriz corrigida, `excluir` em
		// `own` SÓ é seguro porque `Repository.LiberarBloqueio` aplica
		// `AND owner_id = $usuario` — sem esse WHERE, conceder `excluir` seria
		// pior que a assimetria original, porque o corretor passaria a apagar
		// bloqueio de qualquer pessoa.
		{Metodo: http.MethodPost, Path: "/blocks", Acesso: AcessoPermissao, Recurso: cal, Acao: auth.AcaoCriar, Handler: d.Reservas.CriarBloqueio},
		{Metodo: http.MethodDelete, Path: "/blocks/{id}", Acesso: AcessoPermissao, Recurso: cal, Acao: auth.AcaoExcluir, Handler: d.Reservas.LiberarBloqueio},
	}
}
