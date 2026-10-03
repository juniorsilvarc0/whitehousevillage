// Package commission guarda as regras puras da comissão do corretor.
//
// Nesta rodada, só a ATRIBUIÇÃO: quem pode gravar `reservations.broker_id`, o
// campo que, a partir do F2-13, decide para quem vai a comissão. O cálculo
// (base, percentual, estorno no razão) entra com o F2-08, aqui mesmo.
//
// # Por que a atribuição é domínio, e não um `if` no service
//
// Medido em 31/08/2026, com a API no ar: `corretor@wh.local`, com
// `reservations:criar` em escopo `own`, gravou uma venda no `broker_id` de OUTRO
// corretor e recebeu 201 (`WH-2026-0009`). O campo ia do corpo direto para o
// INSERT. A FK do F2-09 fecha o UUID inventado; não fecha o id de um colega que
// existe. O que fecha é esta regra — e ela decide dinheiro, então mora num lugar
// só, com teste de mesa e teste de propriedade, em vez de espalhada entre o POST,
// o PUT e o PATCH de um service.
//
// É código PURO: sem SQL, sem HTTP, sem relógio. O ID é genérico para o módulo
// passar `uuid.UUID` sem o domínio importar biblioteca de identificador.
package commission

import (
	"errors"
	"fmt"
)

// Scope é o alcance da permissão do ator NA AÇÃO — `reservations:criar` no POST,
// `reservations:editar` no PUT/PATCH —, lido da matriz de RBAC. Nunca o nome do
// perfil (regra 8 do CLAUDE.md): um perfil novo com `own` segue a mesma regra do
// corretor sem uma linha de código.
type Scope string

const (
	ScopeAll Scope = "all"
	ScopeOwn Scope = "own"
)

// Write é o verbo da escrita. Os três tratam o campo AUSENTE de jeito diferente,
// e é exatamente aí que um service escrito à mão erraria um dos três.
type Write int

const (
	// Create é o POST: ausente assume o padrão do escopo.
	Create Write = iota + 1
	// Replace é o PUT: ausente volta ao padrão do escopo — substituição
	// integral, como todo PUT do contrato.
	Replace
	// Patch é o PATCH: ausente não muda.
	Patch
)

// Field é o `broker_id` como veio no corpo: ausente, `null` ou valor. É a
// distinção do `Opt[T]` do httpx, redeclarada aqui porque o domínio não importa
// a camada HTTP.
type Field[ID comparable] struct {
	Present bool // o campo veio no corpo
	Value   *ID  // nil com Present = `null` explícito
}

// Absent é o campo que não veio.
func Absent[ID comparable]() Field[ID] { return Field[ID]{} }

// Null é o `"broker_id": null`.
func Null[ID comparable]() Field[ID] { return Field[ID]{Present: true} }

// Set é o `"broker_id": "<id>"`.
func Set[ID comparable](id ID) Field[ID] { return Field[ID]{Present: true, Value: &id} }

// BrokerWrite é tudo que a decisão precisa. O service monta; o domínio decide.
type BrokerWrite[ID comparable] struct {
	Write Write
	Scope Scope

	// ActorBroker é o corretor da conta de quem escreve (`users.broker_id`);
	// nil quando a conta não tem cadastro de corretor. Em escopo `own`, é o
	// ÚNICO valor diferente de nil que o ator pode gravar.
	ActorBroker *ID

	// Current é o corretor gravado hoje. Ignorado em Create.
	Current *ID

	// BeyondHold é true quando a reserva já saiu de `quote`/`hold` —
	// `confirmed` em diante, inclusive os estados finais. Daí em diante a venda
	// gera (ou gerou) comissão, e trocar o corretor é transferir dinheiro entre
	// pessoas. Ignorado em Create.
	BeyondHold bool

	Requested Field[ID]
}

// Decision é o que gravar.
type Decision[ID comparable] struct {
	// Broker é o valor final da coluna; nil é venda direta.
	Broker *ID
	// Changed diz se Broker difere do gravado (em Create, de "nada"). É o que
	// manda o service auditar a troca e, a partir do F2-13, estornar a comissão
	// anterior quando a reserva já passou de `hold`.
	Changed bool
}

// Reason é o porquê estável da recusa. Vai em `details.reason` do
// `403 FORBIDDEN`; a tela reage a ele, nunca ao texto.
type Reason string

const (
	// ReasonNotActorBroker — em escopo `own`, o valor pedido não é `null` nem o
	// corretor da própria conta. É o WH-2026-0009.
	ReasonNotActorBroker Reason = "not_actor_broker"
	// ReasonReplacesOtherBroker — em escopo `own`, a venda está atribuída a
	// outro corretor (gravado por quem tem `all`), e trocar tiraria a comissão
	// dele.
	ReasonReplacesOtherBroker Reason = "replaces_other_broker"
	// ReasonReservationConfirmed — em escopo `own`, a reserva já saiu de `hold`.
	ReasonReservationConfirmed Reason = "reservation_confirmed"
)

// Refusal é a recusa de AUTORIDADE: o valor é bem formado e a reserva é do ator;
// falta o poder de atribuir a venda a outra pessoa. Vira `403 FORBIDDEN` com
// `details: {field: "broker_id", scope: "own", reason}`.
//
// A mensagem não carrega nome nem id de ninguém: nome de pessoa não entra em
// mensagem de erro, e o id do colega na mensagem seria a enumeração que o 403
// existe para não fazer.
type Refusal struct {
	Reason Reason
}

func (r *Refusal) Error() string {
	return "commission: corretor inválido para o seu escopo (" + string(r.Reason) + ")"
}

// ErrInvalidInput é entrada que nenhum chamador correto produz — escopo ou verbo
// fora do vocabulário. Não é recusa de negócio: é defeito de quem chamou, e o
// service deve tratá-lo como erro interno. Falha FECHADA de propósito: escopo
// desconhecido tratado como `all` seria conceder em silêncio.
var ErrInvalidInput = errors.New("commission: entrada inválida")

// ResolveBroker decide o `broker_id` que a escrita grava, ou recusa.
//
// Escopo `all`: grava o pedido; ausente é nil no Create e no Replace, e não muda
// no Patch. Nunca recusa — a existência do corretor é da FK
// `reservations.broker_id → brokers(id)`, e não deste código (conferir aqui e
// gravar depois seria TOCTOU).
//
// Escopo `own`, na ordem em que as recusas são conferidas:
//
//  1. Reenviar o valor gravado não é troca e passa, em qualquer estado — o
//     formulário que devolve a reserva inteira não pode falhar por carregar o
//     corretor que já estava lá.
//  2. O valor final tem de ser nil ou ActorBroker (ausente no Create e no
//     Replace assume ActorBroker). Senão, ReasonNotActorBroker — exista o
//     corretor ou não: responder "não existe" a quem não pode gravar nenhum
//     outro ensinaria, por tentativa, quais ids existem.
//  3. Na edição, o valor gravado também tem de ser nil ou ActorBroker. Senão,
//     ReasonReplacesOtherBroker: a venda que alguém com `all` deu a um colega
//     só se reatribui com `all`.
//  4. Na edição, só com a reserva em `quote` ou `hold`. Senão,
//     ReasonReservationConfirmed.
func ResolveBroker[ID comparable](w BrokerWrite[ID]) (Decision[ID], error) {
	if w.Write != Create && w.Write != Replace && w.Write != Patch {
		return Decision[ID]{}, fmt.Errorf("%w: verbo %d", ErrInvalidInput, w.Write)
	}
	if w.Scope != ScopeAll && w.Scope != ScopeOwn {
		return Decision[ID]{}, fmt.Errorf("%w: escopo %q", ErrInvalidInput, w.Scope)
	}

	// Em Create não há valor gravado: comparar contra nil é o que faz "criar
	// com corretor" contar como mudança (de nada para alguém).
	current := w.Current
	if w.Write == Create {
		current = nil
	}

	final := finalValue(w, current)
	d := Decision[ID]{Broker: final, Changed: !same(final, current)}

	if w.Scope == ScopeAll {
		return d, nil
	}

	// Daqui para baixo, escopo `own`.
	if !d.Changed && w.Write != Create {
		return d, nil // regra 1
	}
	if !ownOrNil(final, w.ActorBroker) {
		return Decision[ID]{}, &Refusal{Reason: ReasonNotActorBroker} // regra 2
	}
	if w.Write == Create {
		return d, nil
	}
	if !ownOrNil(current, w.ActorBroker) {
		return Decision[ID]{}, &Refusal{Reason: ReasonReplacesOtherBroker} // regra 3
	}
	if w.BeyondHold {
		return Decision[ID]{}, &Refusal{Reason: ReasonReservationConfirmed} // regra 4
	}
	return d, nil
}

// finalValue aplica a semântica do verbo ao campo ausente.
func finalValue[ID comparable](w BrokerWrite[ID], current *ID) *ID {
	if w.Requested.Present {
		return w.Requested.Value
	}
	if w.Write == Patch {
		return current
	}
	// Create e Replace: o padrão do escopo. Em `own`, o corretor da própria
	// conta — quem vende pela própria conta não precisa lembrar de se atribuir
	// a venda. Em `all`, venda direta.
	if w.Scope == ScopeOwn {
		return w.ActorBroker
	}
	return nil
}

// ownOrNil diz se v é um dos dois valores que o escopo `own` conhece.
func ownOrNil[ID comparable](v, actor *ID) bool {
	return v == nil || (actor != nil && *v == *actor)
}

// same compara dois ponteiros pelo valor, com nil igual só a nil.
func same[ID comparable](a, b *ID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
