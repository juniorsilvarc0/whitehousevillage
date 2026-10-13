// Package agenda guarda as regras puras da agenda operacional (spec §9): o que
// a operação faz, onde e quando — check-in, check-out, limpeza, visita, evento —
// e o que ela não pode fazer porque a unidade está ocupada, em manutenção ou
// bloqueada.
//
// É código PURO: sem SQL, sem HTTP, sem relógio. "Agora" e "hoje" chegam por
// parâmetro, já resolvidos no fuso da PROPRIEDADE por `calendar.Zone` — que é
// a única conversão entre relógio de parede e instante no código Go —, a partir
// do `now()` do banco lido na mesma transação da escrita. Por isso a mesma
// pergunta tem a mesma resposta no teste e às 22h de Fortaleza num servidor em
// UTC.
//
// # Três decisões que organizam o pacote
//
//  1. **A agenda segue a reserva, nunca o contrário.** Confirmar cria o
//     check-in, o check-out e a limpeza (`Plan`); cancelar os cancela;
//     registrar a entrada e a saída os conclui (`OnCancel`, `OnCheckIn`,
//     `OnCheckOut`). O evento automático nunca é RECUSADO por conflito: a
//     verdade do calendário é `stay_blocks` com a constraint `EXCLUDE`, e uma
//     venda não pode cair porque alguém marcou uma visita. Se a venda cria um
//     conflito com o que já estava na agenda, ele APARECE na leitura
//     (`UnitConflicts` → `has_unit_conflict`), não some.
//  2. **Conflito de unidade bloqueia; conflito de horário avisa** (spec §9, de
//     propósito). Unidade: duas coisas na mesma unidade no mesmo instante — a
//     visita no apartamento que está sendo limpo, ou com hóspede dentro
//     (`CheckUnit` → `409 UNIT_NOT_AVAILABLE`). Horário: fora do funcionamento,
//     dentro de um bloqueio de horário da operação, ou a mesma pessoa em dois
//     lugares sem a folga configurada (`TimeWarnings` → `201` com avisos). A
//     operação sabe o que está fazendo com o próprio tempo; não sabe fazer duas
//     coisas no mesmo apartamento.
//  3. **O evento automático guarda a reserva, e só.** A unidade e o titular
//     são LIDOS da reserva (`reservation_units`, `reservations.contact_id`) a
//     cada leitura. Copiados, seriam uma segunda fonte: realocar a unidade ou
//     trocar o titular deixaria a agenda apontando para o apartamento e a
//     pessoa de antes, e o conserto dependeria de um passo que alguém
//     esqueceria. O que é DA AGENDA — o horário combinado, o responsável, a
//     anotação, o estado — é gravado.
//
// # O que NÃO mora aqui
//
// Que a reserva tenha no máximo um check-in, um check-out e uma limpeza
// automáticos é do índice único `(reservation_id, type)` das linhas de origem
// `reservation`; que dois eventos manuais não ocupem a mesma unidade ao mesmo
// tempo é da constraint `EXCLUDE` sobre a unidade. Este pacote decide o que é
// ACEITÁVEL; quem decide o que é ÚNICO é o banco, no INSERT — conferir com
// SELECT antes seria a corrida que a constraint existe para fechar.
//
// Os erros carregam o `code` do contrato em texto, como `maintenance.RuleError`:
// o domínio não importa `apperr`, e quem traduz é `apperr.PorCodigo`.
package agenda

import (
	"fmt"
	"time"
)

// Códigos do contrato que este pacote carimba. Os literais são permitidos em
// `internal/domain/`, e `apperr/catalogo_test.go` exige que cada um exista no
// catálogo — senão a tradução cairia em 500.
const (
	codeValidation        = "VALIDATION_ERROR"
	codeForbidden         = "FORBIDDEN"
	codeInvalidTransition = "INVALID_STATE_TRANSITION"
	codeEventClosed       = "AGENDA_EVENT_CLOSED"
	codeUnitNotAvailable  = "UNIT_NOT_AVAILABLE"
)

// RuleError é a violação de uma regra da agenda. `Code` é estável e vira o
// `error.code` da API; `Details` vai para `error.details`.
//
// Em `VALIDATION_ERROR`, `Details` é o mapa campo → mensagem, com os nomes do
// contrato (`starts_at`, `checkin_time`, `business_hours[2].end`).
type RuleError struct {
	Code    string
	Message string
	Details map[string]any
}

func (e *RuleError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// ─────────────────────────── Tipo ───────────────────────────────────

// Type é o tipo do evento — os sete da spec §9.
type Type string

const (
	CheckIn     Type = "checkin"
	CheckOut    Type = "checkout"
	Cleaning    Type = "limpeza"
	Maintenance Type = "manutencao"
	Visit       Type = "visita"
	Event       Type = "evento"
	Block       Type = "bloqueio"
)

// Types devolve os sete tipos, na ordem em que a tela os lista.
func Types() []Type {
	return []Type{CheckIn, CheckOut, Cleaning, Maintenance, Visit, Event, Block}
}

// Valid diz se o valor é um dos sete tipos.
func (t Type) Valid() bool {
	for _, x := range Types() {
		if t == x {
			return true
		}
	}
	return false
}

// Stored diz se o tipo é gravado em `agenda_events`. `manutencao` e `bloqueio`
// NÃO são: aparecem na agenda lidos de `stay_blocks` (e da ordem de
// manutenção), sem cópia — uma segunda linha para o mesmo bloqueio seria uma
// segunda fonte, e as duas divergiriam no dia em que a ordem fosse estendida
// por um caminho que não soubesse da agenda. É, palavra por palavra, o `CHECK`
// de `agenda_events.type`.
func (t Type) Stored() bool {
	switch t {
	case CheckIn, CheckOut, Cleaning, Visit, Event:
		return true
	}
	return false
}

// Automatic diz se o tipo nasce da reserva confirmada.
func (t Type) Automatic() bool { return t == CheckIn || t == CheckOut || t == Cleaning }

// Manual diz se o tipo pode ser criado à mão (`POST /agenda/events`).
// `limpeza` é dos dois: a da reserva nasce sozinha, e a faxina avulsa (a do
// meio da estadia, a geral antes da temporada) é marcada por alguém. Check-in e
// check-out só existem como consequência de uma reserva; manutenção é a ordem
// de manutenção, que tem módulo próprio; bloqueio de unidade é `POST /blocks`.
func (t Type) Manual() bool { return t == Visit || t == Event || t == Cleaning }

// ManualTypes lista os tipos que `POST /agenda/events` aceita.
func ManualTypes() []Type { return []Type{Visit, Event, Cleaning} }

// Label é o nome do tipo na tela e no título composto.
func (t Type) Label() string {
	switch t {
	case CheckIn:
		return "Check-in"
	case CheckOut:
		return "Check-out"
	case Cleaning:
		return "Limpeza"
	case Maintenance:
		return "Manutenção"
	case Visit:
		return "Visita"
	case Event:
		return "Evento"
	case Block:
		return "Bloqueio"
	}
	return string(t)
}

// AutoTitle é o título do evento automático. Não é gravado (`title` é nulo
// nas linhas de origem `reservation`): o código da reserva é imutável, mas a
// regra de composição pode mudar sem migration.
func AutoTitle(t Type, reservationCode string) string {
	if reservationCode == "" {
		return t.Label()
	}
	return t.Label() + " · " + reservationCode
}

// ─────────────────────────── Origem ─────────────────────────────────

// Origin diz de onde o evento vem. As duas primeiras são o `CHECK` de
// `agenda_events.origin`; as duas últimas só existem na leitura, para os
// eventos derivados de `stay_blocks`.
type Origin string

const (
	// FromReservation — nasceu do `/confirm` (ou da remarcação que nasce
	// confirmada, ou do preenchimento das reservas anteriores à agenda). Segue a
	// reserva: não se cancela, não muda de tipo nem de unidade por aqui.
	FromReservation Origin = "reservation"
	// ByHand — marcado por alguém em `POST /agenda/events`.
	ByHand Origin = "manual"
	// FromMaintenance — linha de `stay_blocks` com `source = 'maintenance'`,
	// lida na hora; mexe-se pela ordem de manutenção.
	FromMaintenance Origin = "maintenance"
	// FromStayBlock — linha de `stay_blocks` com `source` `owner_hold` ou
	// `ota`, lida na hora; mexe-se por `/blocks` (e, na Fase 4, pelo canal).
	FromStayBlock Origin = "stay_block"
)

// Stored diz se a origem é de uma linha de `agenda_events`.
func (o Origin) Stored() bool { return o == FromReservation || o == ByHand }

// ─────────────────────────── Estado ─────────────────────────────────

// Status é o estado do evento — o `CHECK` de `agenda_events.status`.
type Status string

const (
	Scheduled Status = "agendado"
	Done      Status = "concluido"
	Cancelled Status = "cancelado"
)

// Valid diz se o valor é um dos três estados.
func (s Status) Valid() bool { return s == Scheduled || s == Done || s == Cancelled }

// Closed diz se o evento terminou — concluído ou cancelado. Fechado não
// reabre: visita desmarcada que volta a acontecer é visita nova, e a trilha de
// "foi desmarcada" não some.
func (s Status) Closed() bool { return s == Done || s == Cancelled }

// ─────────────────────────── Janela ─────────────────────────────────

// Window é o intervalo half-open [Start, End) do evento, em instantes — como a
// estadia é [entrada, saída) em datas. Half-open é o que deixa a limpeza das
// 11:00 às 14:00 e o check-in das 14:00 encostarem sem colidir.
type Window struct {
	Start time.Time
	End   time.Time
}

// Valid diz se a janela tem duração positiva.
func (w Window) Valid() bool { return w.End.After(w.Start) }

// Overlaps diz se as duas janelas têm algum instante em comum. Janela vazia ou
// invertida não sobrepõe nada; janelas que só se encostam também não.
func (w Window) Overlaps(o Window) bool {
	return w.Valid() && o.Valid() && w.Start.Before(o.End) && o.Start.Before(w.End)
}

// Equal compara os dois instantes (e não a representação, que muda com o fuso
// em que o `time.Time` foi lido).
func (w Window) Equal(o Window) bool { return w.Start.Equal(o.Start) && w.End.Equal(o.End) }

// Duration é a duração da janela.
func (w Window) Duration() time.Duration { return w.End.Sub(w.Start) }
