package agenda

import (
	"fmt"
	"time"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

// O evento gravado: o que se pode fazer com ele (ações nomeadas), o que se
// pode mudar nele (edição), a janela aceitável e quem pode ser o responsável.

// Facts é o evento como as regras deste arquivo precisam dele.
type Facts struct {
	Type   Type
	Origin Origin
	Status Status
	// Date é o dia, na propriedade, em que o evento começa.
	Date calendar.Date
}

func (f Facts) check() error {
	if !f.Type.Stored() || !f.Origin.Stored() || !f.Status.Valid() {
		return fmt.Errorf("agenda: evento fora do vocabulário gravado (%q, %q, %q)", f.Type, f.Origin, f.Status)
	}
	return nil
}

// ─────────────────────────── Ações ──────────────────────────────────

// Action é uma transição nomeada. Estado só muda por ação, nunca por `status`
// no PATCH — como na ordem de manutenção.
type Action string

const (
	// Complete — `POST /agenda/events/{id}/complete`: a limpeza foi feita, a
	// visita aconteceu.
	Complete Action = "complete"
	// Cancel — `DELETE /agenda/events/{id}`: desmarcar. Cancela, não apaga.
	Cancel Action = "cancel"
)

func (a Action) valid() bool { return a == Complete || a == Cancel }

// Reason é o porquê estável de uma transição recusada, em `details.reason`.
type Reason string

const (
	// ReasonFollowsReservation — o evento automático segue a reserva: o
	// check-in conclui com `/reservations/{id}/check-in`, o check-out com
	// `/check-out`, e nenhum dos três sai da agenda sem a reserva ser
	// cancelada.
	ReasonFollowsReservation Reason = "segue_a_reserva"
	// ReasonNotStarted — concluir o que ainda não chegou ao dia.
	ReasonNotStarted Reason = "nao_comecou"
	// ReasonAssigneeNotActor — em escopo `own`, o responsável pedido não é o
	// próprio ator.
	ReasonAssigneeNotActor Reason = "assignee_not_actor"
	// ReasonOwnScopeNotApplicable — escrita em `/agenda/blocks` com escopo
	// `own`. O bloqueio de horário é da operação inteira e não tem
	// responsável: `own` não tem por onde se aplicar, e a resposta é negar
	// (`403`), como o módulo de contatos faz com o recurso sem dono — nunca
	// degradar para `all`. Quem recusa é o módulo, com este `details.reason`.
	ReasonOwnScopeNotApplicable Reason = "own_scope_not_applicable"
)

// allowed é a máquina sem o relógio: o que cada (origem, tipo) aceita quando
// agendado. Check-in e check-out automáticos não aceitam nada — quem os move é
// a reserva. A limpeza automática conclui (quem limpa marca no celular) e não
// cancela.
func allowed(f Facts) []Action {
	if f.Status.Closed() {
		return []Action{}
	}
	if f.Origin == FromReservation {
		if f.Type == Cleaning {
			return []Action{Complete}
		}
		return []Action{}
	}
	return []Action{Complete, Cancel}
}

// AllowedActions devolve as ações que o evento aceita HOJE, na ordem da tela —
// é o `allowed_actions` da resposta, e o painel desenha os botões a partir
// daqui, sem segunda cópia da máquina. Nunca devolve nil.
//
// `complete` só aparece do dia do evento em diante: concluir a limpeza de
// amanhã é marcar como feito o que não foi.
func AllowedActions(f Facts, today calendar.Date) []Action {
	out := []Action{}
	for _, a := range allowed(f) {
		if a == Complete && f.Date.After(today) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// Next aplica a ação ao evento no dia `today` e devolve o estado seguinte.
//
//   - fechado (concluído ou cancelado) → AGENDA_EVENT_CLOSED, qualquer ação;
//   - automático fora do que aceita → INVALID_STATE_TRANSITION com
//     `details.reason: segue_a_reserva`;
//   - `complete` antes do dia → INVALID_STATE_TRANSITION com
//     `details.reason: nao_comecou`.
//
// Vocabulário desconhecido é defeito de programação e sai como erro comum
// (500), não 409 — mandaria o operador tentar de novo o que nunca vai passar.
func Next(f Facts, a Action, today calendar.Date) (Status, error) {
	if err := f.check(); err != nil {
		return "", err
	}
	if !a.valid() {
		return "", fmt.Errorf("agenda: ação desconhecida %q", a)
	}
	if f.Status.Closed() {
		return "", closedError(f)
	}
	ok := false
	for _, x := range allowed(f) {
		if x == a {
			ok = true
		}
	}
	if !ok {
		return "", &RuleError{
			Code:    codeInvalidTransition,
			Message: followsMessage(f.Type),
			Details: map[string]any{
				"status":  string(f.Status),
				"action":  string(a),
				"allowed": actionNames(AllowedActions(f, today)),
				"reason":  string(ReasonFollowsReservation),
			},
		}
	}
	if a == Complete && f.Date.After(today) {
		return "", &RuleError{
			Code:    codeInvalidTransition,
			Message: fmt.Sprintf("Este evento é em %s: só se conclui o que já chegou ao dia.", f.Date),
			Details: map[string]any{
				"status":  string(f.Status),
				"action":  string(a),
				"allowed": actionNames(AllowedActions(f, today)),
				"reason":  string(ReasonNotStarted),
			},
		}
	}
	if a == Complete {
		return Done, nil
	}
	return Cancelled, nil
}

func followsMessage(t Type) string {
	switch t {
	case CheckIn:
		return "O check-in segue a reserva: registre a entrada na reserva."
	case CheckOut:
		return "O check-out segue a reserva: registre a saída na reserva."
	default:
		return "A limpeza da reserva só sai da agenda quando a reserva é cancelada."
	}
}

func actionNames(as []Action) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = string(a)
	}
	return out
}

// ─────────────────────────── Edição ─────────────────────────────────

// Editable diz o que o evento aceita de edição — o `editable` da resposta.
type Editable string

const (
	// EditAll — manual agendado: tudo que a rota aceita.
	EditAll Editable = "tudo"
	// EditFollowsReservation — automático agendado: o horário, o responsável
	// e a anotação. Tipo, unidade, reserva e titular são da reserva.
	EditFollowsReservation Editable = "segue_a_reserva"
	// EditNotesOnly — concluído: só a anotação ("faltou toalha no AP-02").
	EditNotesOnly Editable = "so_notas"
	// EditNone — cancelado: nada.
	EditNone Editable = "nada"
)

// EditableIn é a regra de edição do evento.
func EditableIn(f Facts) Editable {
	switch {
	case f.Status == Cancelled:
		return EditNone
	case f.Status == Done:
		return EditNotesOnly
	case f.Origin == FromReservation:
		return EditFollowsReservation
	default:
		return EditAll
	}
}

// Edit descreve o que uma escrita MUDA (comparado ao gravado — reenviar o
// valor atual não é mudança, para o formulário que devolve o evento inteiro
// não falhar pelo que não tocou).
type Edit struct {
	// Identity são os campos de identidade que mudam, pelo nome do contrato:
	// `type`, `title`, `all_day`, `unit_id`, `reservation_id`, `contact_id`.
	Identity []string
	// Window é a janela nova, quando muda.
	Window   *Window
	Assignee bool
	Notes    bool
}

func (e Edit) empty() bool {
	return len(e.Identity) == 0 && e.Window == nil && !e.Assignee && !e.Notes
}

// AutoBounds é o que a edição de um evento automático precisa saber da reserva.
type AutoBounds struct {
	// CheckoutEnd é o fim do check-out da reserva — o gravado, ou o da
	// configuração. A limpeza não começa antes dele.
	CheckoutEnd time.Time
}

// CheckEdit recusa a edição que o evento não aceita.
//
//   - cancelado: nada (AGENDA_EVENT_CLOSED, `details.editable: nada`);
//   - concluído: só `notes` (AGENDA_EVENT_CLOSED, `details.editable: so_notas`);
//   - automático agendado: identidade não muda (VALIDATION_ERROR no campo —
//     "segue a reserva"); check-in e check-out mudam de HORA, não de dia (o dia
//     é o da reserva); a limpeza pode ir para depois — inclusive outro dia,
//     quando a equipe só vem amanhã —, nunca para antes do fim do check-out;
//   - manual agendado: tudo, e as outras regras (janela, unidade) são de
//     CheckWindow e CheckUnit.
//
// Corpo que não muda nada passa em qualquer estado.
func CheckEdit(f Facts, e Edit, b AutoBounds, z calendar.Zone) error {
	if err := f.check(); err != nil {
		return err
	}
	if e.empty() {
		return nil
	}
	switch EditableIn(f) {
	case EditNone:
		return closedError(f)
	case EditNotesOnly:
		if len(e.Identity) > 0 || e.Window != nil || e.Assignee {
			return closedError(f)
		}
		return nil
	case EditAll:
		return nil
	}

	erros := map[string]any{}
	for _, campo := range e.Identity {
		erros[campo] = "segue a reserva: não muda por aqui."
	}
	if e.Window != nil {
		w := *e.Window
		switch f.Type {
		case CheckIn, CheckOut:
			fimDoDia := z.At(f.Date, calendar.EndOfDay)
			if z.DateOf(w.Start) != f.Date {
				erros["starts_at"] = fmt.Sprintf(
					"o %s é no dia da reserva (%s): muda a hora, não o dia.", f.Type.Label(), f.Date)
			} else if w.End.After(fimDoDia) {
				erros["ends_at"] = fmt.Sprintf("o %s termina no mesmo dia (%s).", f.Type.Label(), f.Date)
			}
		case Cleaning:
			if w.Start.Before(b.CheckoutEnd) {
				erros["starts_at"] = fmt.Sprintf(
					"a limpeza começa depois que o hóspede sai (%s).", z.In(b.CheckoutEnd).Format(time.RFC3339))
			}
		}
	}
	if len(erros) == 0 {
		return nil
	}
	return &RuleError{Code: codeValidation, Message: "Este evento segue a reserva.", Details: erros}
}

func closedError(f Facts) *RuleError {
	msg := "Este evento foi cancelado: para voltar a acontecer, marque outro."
	if f.Status == Done {
		msg = "Este evento já foi concluído: só a anotação ainda pode mudar."
	}
	return &RuleError{
		Code:    codeEventClosed,
		Message: msg,
		Details: map[string]any{"status": string(f.Status), "editable": string(EditableIn(f))},
	}
}

// ─────────────────────────── Janela aceitável ───────────────────────

// Tetos da janela. Guarda de digitação, como os de `calendar`: um zero a mais
// no ano não pode virar um evento de uma década.
const (
	// MaxEventDays — duração máxima de um evento manual. Um evento de casamento
	// que ocupa o fim de semana cabe; um mês inteiro é bloqueio, não evento.
	MaxEventDays = 31
	// MaxOperationBlockDays — duração máxima de um bloqueio de horário da
	// operação (`/agenda/blocks`): férias coletivas cabem, a década não.
	MaxOperationBlockDays = 366
	// PastDays — quanto para trás um evento pode ser lançado: registrar a
	// visita que aconteceu ontem é legítimo; a de três anos atrás é dedo.
	PastDays = 365
)

// CheckWindow recusa a janela inaceitável, com VALIDATION_ERROR em
// `starts_at`/`ends_at`:
//
//   - fim não posterior ao início;
//   - mais longa que `maxDays`;
//   - dia inteiro que não começa e termina à meia-noite DA CASA — o painel
//     manda `2026-11-20T00:00:00-03:00`, e não a meia-noite do navegador;
//   - começo antes de hoje − PastDays ou depois de hoje +
//     `calendar.BlockHorizonDays` (o mesmo horizonte do calendário).
func CheckWindow(w Window, allDay bool, z calendar.Zone, today calendar.Date, maxDays int) error {
	erros := map[string]any{}
	switch {
	case !w.Valid():
		erros["ends_at"] = "deve ser posterior a `starts_at` (o intervalo é half-open: `ends_at` não entra)."
	case w.Duration() > time.Duration(maxDays)*24*time.Hour:
		erros["ends_at"] = fmt.Sprintf("no máximo %d dias.", maxDays)
	}
	if allDay {
		if z.ClockOf(w.Start) != calendar.Midnight || z.In(w.Start).Second() != 0 {
			erros["starts_at"] = "evento de dia inteiro começa à meia-noite da casa (00:00 no fuso da propriedade)."
		}
		if z.ClockOf(w.End) != calendar.Midnight || z.In(w.End).Second() != 0 {
			erros["ends_at"] = "evento de dia inteiro termina à meia-noite da casa do dia seguinte ao último."
		}
	}
	if _, ja := erros["starts_at"]; !ja {
		if w.Start.Before(z.StartOfDay(today.AddDays(-PastDays))) {
			erros["starts_at"] = fmt.Sprintf("mais de %d dias no passado.", PastDays)
		} else if w.Start.After(z.StartOfDay(today.AddDays(calendar.BlockHorizonDays))) {
			erros["starts_at"] = fmt.Sprintf("além do horizonte de %d dias.", calendar.BlockHorizonDays)
		}
	}
	if len(erros) == 0 {
		return nil
	}
	return &RuleError{Code: codeValidation, Message: "Horário inválido.", Details: erros}
}

// ─────────────────────────── Responsável ────────────────────────────

// Scope é o alcance do ator NA AÇÃO, lido da matriz de RBAC — nunca o nome do
// perfil (regra 8).
type Scope string

const (
	ScopeAll Scope = "all"
	ScopeOwn Scope = "own"
)

// Write é o verbo da escrita: os três tratam o campo AUSENTE de jeito
// diferente.
type Write int

const (
	Create  Write = iota + 1 // POST: ausente assume o padrão do escopo
	Replace                  // PUT: ausente volta ao padrão do escopo
	Patch                    // PATCH: ausente não muda
)

// Field é o `assignee_id` como veio no corpo: ausente, `null` ou valor — a
// distinção do `Opt[T]` do httpx, redeclarada porque o domínio não importa a
// camada HTTP.
type Field[ID comparable] struct {
	Present bool
	Value   *ID
}

// AssigneeWrite é tudo que a decisão do responsável precisa.
type AssigneeWrite[ID comparable] struct {
	Write     Write
	Scope     Scope
	Actor     ID
	Current   *ID // ignorado em Create
	Requested Field[ID]
}

// ResolveAssignee decide o `assignee_id` gravado, ou recusa.
//
// Em `all`, grava o pedido; ausente é ninguém no POST e no PUT, e não muda no
// PATCH. A existência do usuário é da FK, não deste código.
//
// Em `own` — `own` na agenda é `assignee_id = ator`, no SQL —, o valor final
// tem de ser o próprio ator: ausente no POST e no PUT vira o ator; `null` ou
// outra pessoa é `403 FORBIDDEN` com `details.reason: assignee_not_actor`. A
// recusa não diz se a pessoa existe: dizer ensinaria, por tentativa, quais ids
// existem. E entregar o evento a ninguém o tiraria da vista de quem o criou.
func ResolveAssignee[ID comparable](w AssigneeWrite[ID]) (*ID, error) {
	if w.Write != Create && w.Write != Replace && w.Write != Patch {
		return nil, fmt.Errorf("agenda: verbo %d", w.Write)
	}
	if w.Scope != ScopeAll && w.Scope != ScopeOwn {
		// Falha FECHADA: escopo desconhecido tratado como `all` seria conceder
		// em silêncio.
		return nil, fmt.Errorf("agenda: escopo %q", w.Scope)
	}
	var final *ID
	switch {
	case w.Requested.Present:
		final = w.Requested.Value
	case w.Write == Patch:
		final = w.Current
	case w.Scope == ScopeOwn:
		a := w.Actor
		final = &a
	}
	if w.Scope == ScopeOwn && (final == nil || *final != w.Actor) {
		return nil, &RuleError{
			Code:    codeForbidden,
			Message: "No seu escopo, o responsável pelo evento é você.",
			Details: map[string]any{
				"field":  "assignee_id",
				"scope":  string(ScopeOwn),
				"reason": string(ReasonAssigneeNotActor),
			},
		}
	}
	return final, nil
}
