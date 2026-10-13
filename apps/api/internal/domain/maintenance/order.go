// Package maintenance guarda as regras puras da ordem de manutenção (spec §12):
// a máquina de estados da ordem e o destino do bloqueio de calendário que ela
// gera — quando nasce, como se estende ou encurta e o que sobra dele quando a
// ordem encerra.
//
// É código PURO: sem SQL, sem HTTP, sem relógio. "Hoje" chega por parâmetro, já
// resolvido no fuso da PROPRIEDADE por quem chama (o módulo lê
// `properties.timezone` no banco, na mesma transação da escrita) — o domínio
// nunca chama `time.Now`, e por isso a mesma pergunta tem a mesma resposta no
// teste e às 22h de Fortaleza num servidor em UTC.
//
// # O que NÃO mora aqui, e por quê
//
// A garantia contra bloqueio sobreposto não é deste pacote: é a constraint
// `EXCLUDE` de `stay_blocks` (regra 2 do CLAUDE.md). O domínio decide se um
// período é ACEITÁVEL para a ordem (o passado não se reescreve, o teto vale);
// quem decide se ele está LIVRE é o banco, no INSERT/UPDATE, com `23P01`.
// Conferir com SELECT antes seria a corrida que a constraint existe para fechar.
//
// Os erros carregam o `code` do contrato em texto, como `booking.RuleError`: o
// domínio não importa `apperr`, e quem traduz é `apperr.PorCodigo`.
package maintenance

import "fmt"

// Códigos do contrato que este pacote carimba. Os literais são permitidos em
// `internal/domain/` e `apperr/catalogo_test.go` exige que cada um exista no
// catálogo — senão a tradução cairia em 500.
const (
	codeValidation        = "VALIDATION_ERROR"
	codeInvalidTransition = "INVALID_STATE_TRANSITION"
	codeOrderClosed       = "MAINTENANCE_ORDER_CLOSED"
)

// RuleError é a violação de uma regra da ordem. `Code` é estável e vira o
// `error.code` da API; `Details` vai para `error.details`.
//
// Em `VALIDATION_ERROR`, `Details` é o mapa campo → mensagem, com os nomes
// `from` e `to` do período. Quem recebe o período ANINHADO (o `block` do
// `POST /maintenance-orders`) prefixa a chave (`block.from`); o domínio não
// sabe em que corpo o período veio.
type RuleError struct {
	Code    string
	Message string
	Details map[string]any
}

func (e *RuleError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// ─────────────────────────── Estado ─────────────────────────────────

// Status é o estado da ordem. Os quatro valores são, palavra por palavra, o
// `CHECK` de `maintenance_orders.status`.
type Status string

const (
	Open       Status = "aberta"
	InProgress Status = "em_andamento"
	Done       Status = "concluida"
	Cancelled  Status = "cancelada"
)

// Valid diz se o valor é um dos quatro estados.
func (s Status) Valid() bool {
	switch s {
	case Open, InProgress, Done, Cancelled:
		return true
	}
	return false
}

// Closed diz se a ordem está encerrada. Encerrada não reabre: retrabalho é
// ordem NOVA, com outra data e outro autor — reabrir apagaria o fato de que a
// primeira intervenção foi dada por terminada, e o custo dela se misturaria ao
// da segunda.
func (s Status) Closed() bool { return s == Done || s == Cancelled }

// ClosedStatuses lista os estados encerrados, para o repositório ordenar
// "abertas primeiro" e filtrar `open=true` sem repetir a lista num `CASE`.
func ClosedStatuses() []Status { return []Status{Done, Cancelled} }

// ─────────────────────────── Prioridade ─────────────────────────────

// Priority é a urgência da ordem. Os valores são o `CHECK` de
// `maintenance_orders.priority`.
type Priority string

const (
	Low    Priority = "baixa"
	Normal Priority = "normal"
	High   Priority = "alta"
	Urgent Priority = "urgente"
)

// DefaultPriority é a prioridade de quem não informou nenhuma.
const DefaultPriority = Normal

// Valid diz se o valor é uma das quatro prioridades.
func (p Priority) Valid() bool {
	switch p {
	case Low, Normal, High, Urgent:
		return true
	}
	return false
}

// ByUrgency devolve as prioridades da mais urgente para a menos urgente — a
// ordem da lista de trabalho. O repositório a recebe como parâmetro
// (`array_position($1::text[], priority)`) em vez de repetir a escada num
// `CASE`: duas cópias da mesma ordem divergem no dia em que só uma é editada.
func ByUrgency() []Priority { return []Priority{Urgent, High, Normal, Low} }

// ─────────────────────────── Transições ─────────────────────────────

// Action é uma transição nomeada. Estado só muda por ação, nunca por campo
// mágico no PATCH: cada transição tem efeito colateral obrigatório (o
// calendário e a avaria), e um `status` editável o pularia.
type Action string

const (
	// Start — `POST /maintenance-orders/{id}/start`: alguém começou o serviço.
	Start Action = "start"
	// Complete — `POST /maintenance-orders/{id}/complete`.
	Complete Action = "complete"
	// Cancel — `DELETE /maintenance-orders/{id}`: cancelar é o "excluir" de uma
	// ordem, que tem histórico e por isso não se apaga.
	Cancel Action = "cancel"
)

// actions é a ordem em que a tela oferece as ações.
var actions = []Action{Start, Complete, Cancel}

// transitions é a máquina inteira. Concluir direto de `aberta` é permitido de
// propósito: trocar uma lâmpada não pede dois toques no celular, e exigir o
// `start` só produziria `started_at` falso, gravado no mesmo segundo do
// `closed_at`.
var transitions = map[Status]map[Action]Status{
	Open:       {Start: InProgress, Complete: Done, Cancel: Cancelled},
	InProgress: {Complete: Done, Cancel: Cancelled},
}

// Next aplica a ação ao estado atual e devolve o estado seguinte.
//
//   - ordem encerrada → MAINTENANCE_ORDER_CLOSED, qualquer que seja a ação;
//   - ação fora da máquina (`start` em `em_andamento`) →
//     INVALID_STATE_TRANSITION, com `details.allowed`.
//
// Estado ou ação fora do vocabulário é erro de programação (o `CHECK` do banco
// não deixa o primeiro existir; o segundo só nasce de uma constante errada),
// e sai como erro comum — 500, não 409.
func Next(from Status, a Action) (Status, error) {
	if !from.Valid() {
		return "", fmt.Errorf("maintenance: estado desconhecido %q", from)
	}
	if !a.valid() {
		return "", fmt.Errorf("maintenance: ação desconhecida %q", a)
	}
	if from.Closed() {
		return "", closedError(from)
	}
	to, ok := transitions[from][a]
	if !ok {
		return "", &RuleError{
			Code:    codeInvalidTransition,
			Message: "Esta ordem já está em andamento.",
			Details: map[string]any{
				"status":  string(from),
				"action":  string(a),
				"allowed": actionNames(AllowedActions(from)),
			},
		}
	}
	return to, nil
}

// AllowedActions devolve as ações que o estado aceita, na ordem da tela. É o
// `allowed_actions` da resposta: o painel desenha os botões a partir daqui e
// não carrega uma segunda cópia da máquina de estados.
//
// Nunca devolve nil — a resposta é `[]` para ordem encerrada.
func AllowedActions(s Status) []Action {
	out := []Action{}
	for _, a := range actions {
		if _, ok := transitions[s][a]; ok {
			out = append(out, a)
		}
	}
	return out
}

func (a Action) valid() bool {
	for _, x := range actions {
		if a == x {
			return true
		}
	}
	return false
}

func actionNames(as []Action) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = string(a)
	}
	return out
}

// ─────────────────────────── Edição ─────────────────────────────────

// Editable diz o que a ordem aceita de edição no estado em que está. É o
// `editable` da resposta.
type Editable string

const (
	// EditAll — aberta ou em andamento: tudo que a rota aceita.
	EditAll Editable = "tudo"
	// EditCostOnly — concluída: só o custo. A nota do encanador chega dias
	// depois do conserto, e uma ordem concluída sem custo deixaria a margem
	// da estadia (spec §12) sem o número que existe. Título, descrição,
	// cômodo e bem são o registro do que foi feito, e isso não muda depois.
	EditCostOnly Editable = "so_custo"
	// EditNone — cancelada: nada. Não houve serviço a custear.
	EditNone Editable = "nada"
)

// EditableIn é a regra de edição de cada estado.
func EditableIn(s Status) Editable {
	switch s {
	case Done:
		return EditCostOnly
	case Cancelled:
		return EditNone
	default:
		return EditAll
	}
}

// Edit descreve o que uma escrita mexe. `Cost` é o `cost_cents`; `Other` é
// qualquer outra coisa — título, descrição, prioridade, cômodo, bem e o
// período do bloqueio.
//
// O `PUT` é sempre `Other: true` (substitui o corpo inteiro). O `PATCH` marca
// `Other` quando algum campo além do custo veio no corpo — presente, ainda que
// `null`. Corpo vazio não mexe em nada e passa em qualquer estado.
type Edit struct {
	Cost  bool
	Other bool
}

// CheckEdit recusa a edição que o estado não aceita, com
// MAINTENANCE_ORDER_CLOSED e `details.editable` dizendo o que ainda dá para
// mudar — a tela usa para explicar por que o campo travou.
func CheckEdit(s Status, e Edit) error {
	if !s.Valid() {
		return fmt.Errorf("maintenance: estado desconhecido %q", s)
	}
	switch EditableIn(s) {
	case EditAll:
		return nil
	case EditCostOnly:
		if e.Other {
			return closedError(s)
		}
		return nil
	default:
		if e.Cost || e.Other {
			return closedError(s)
		}
		return nil
	}
}

func closedError(s Status) *RuleError {
	msg := "Esta ordem já foi encerrada: retrabalho é uma ordem nova."
	if s == Done {
		msg = "Esta ordem já foi concluída: só o custo ainda pode ser lançado. Retrabalho é uma ordem nova."
	}
	return &RuleError{
		Code:    codeOrderClosed,
		Message: msg,
		Details: map[string]any{
			"status":   string(s),
			"editable": string(EditableIn(s)),
		},
	}
}

// ─────────────────────────── Avaria ─────────────────────────────────

// RepairedResolution é o desfecho que a avaria de origem recebe quando a ordem
// é CONCLUÍDA — um dos valores de `inventory_issues.resolution`.
const RepairedResolution = "consertado"

// IssueOnClose diz o que encerrar a ordem faz com a avaria que a originou.
//
// Concluir conserta: a avaria AINDA ABERTA (`resolution IS NULL`) ganha
// `consertado`, na mesma transação do encerramento. Avaria que alguém já
// resolveu de outro jeito (reposta, cobrada) não é tocada — quem chama aplica
// o desfecho só sobre `resolution IS NULL`, e o desfecho escolhido por uma
// pessoa vence o inferido pela ordem.
//
// Cancelar não mexe: a ordem cancelada não consertou nada, e a avaria continua
// pendente para outra ordem ou outro desfecho.
func IssueOnClose(to Status) (resolution string, resolves bool) {
	if to == Done {
		return RepairedResolution, true
	}
	return "", false
}
