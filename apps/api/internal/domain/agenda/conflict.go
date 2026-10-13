package agenda

import (
	"sort"
	"time"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

// Os dois conflitos da spec §9, separados de propósito:
//
//   - de UNIDADE é bloqueio: duas coisas no mesmo apartamento ao mesmo tempo.
//     A visita marcada para as 12:00 no AP-01 que está sendo limpo das 11:00
//     às 14:00, ou com hóspede dentro, não acontece — e marcar é `409`;
//   - de HORÁRIO é aviso: fora do funcionamento, dentro de um bloqueio de
//     horário da operação, ou a mesma pessoa em dois compromissos sem a folga.
//     A operação pode ter motivo para isso, e marcar é `201` com o aviso.
//
// # Quem ocupa uma unidade
//
//   - a ESTADIA — da hora de entrada do dia do check-in até a hora de saída do
//     dia do check-out, pelos horários do evento da reserva quando ele existe
//     (o hóspede que combinou sair às 13:00 está lá até as 13:00) e pela
//     configuração quando não existe (a pré-reserva ainda não tem agenda). É
//     lida de `stay_blocks` (`hold`, `confirmed`) — a mesma ocupação que
//     impede a venda;
//   - a MANUTENÇÃO e o BLOQUEIO de unidade (`stay_blocks` de origem
//     `maintenance`, `owner_hold`, `ota`), o dia inteiro, de `from` a `to`
//     exclusivo;
//   - todo EVENTO agendado com unidade: os automáticos (na unidade da reserva)
//     e os manuais (na unidade gravada ou, sem ela, nas da reserva ligada).
//
// O evento ligado a uma reserva não colide com a ESTADIA dela — a faxina do
// meio da estadia e o casamento do grupo que alugou a casa acontecem com o
// hóspede dentro, de propósito. Com os outros EVENTOS da mesma reserva colide:
// duas limpezas no mesmo apartamento na mesma hora é erro de quem marcou.

// OccupancyKind é o que ocupa a unidade. Os de evento são o próprio `Type`.
type OccupancyKind string

// ByStay é a ocupação pela estadia de um hóspede.
const ByStay OccupancyKind = "estadia"

// Occupancy é uma ocupação de unidade numa janela.
type Occupancy struct {
	Kind   OccupancyKind
	Unit   string // a unidade ocupada (o id, em texto)
	Window Window
	// Reservation é a reserva dona da ocupação, quando há; Event é o evento,
	// quando a ocupação é um evento. Os dois servem às exceções: o evento não
	// colide consigo mesmo, nem com a estadia da própria reserva.
	Reservation string
	Event       string
}

// StayOccupancy é a ocupação da unidade pela estadia [in, out).
// `checkInStart` e `checkOutEnd` são o começo do check-in e o fim do check-out
// GRAVADOS para a reserva, quando existem; nil cai nos horários de `s`.
func StayOccupancy(unit, reservation string, in, out calendar.Date, checkInStart, checkOutEnd *time.Time, s Settings, z calendar.Zone) Occupancy {
	w := Window{Start: z.At(in, s.CheckIn), End: z.At(out, s.CheckOut)}
	if checkInStart != nil {
		w.Start = *checkInStart
	}
	if checkOutEnd != nil {
		w.End = *checkOutEnd
	}
	return Occupancy{Kind: ByStay, Unit: unit, Window: w, Reservation: reservation}
}

// BlockOccupancy é a ocupação por um bloqueio de calendário de noites
// [from, to) — manutenção ou bloqueio de unidade —, o dia inteiro: da
// meia-noite de `from` à meia-noite de `to`.
func BlockOccupancy(kind Type, unit, ref string, from, to calendar.Date, z calendar.Zone) Occupancy {
	return Occupancy{
		Kind:   OccupancyKind(kind),
		Unit:   unit,
		Window: Window{Start: z.StartOfDay(from), End: z.StartOfDay(to)},
		Event:  ref,
	}
}

// EventOccupancy é a ocupação da unidade por um evento agendado.
func EventOccupancy(t Type, unit, event, reservation string, w Window) Occupancy {
	return Occupancy{Kind: OccupancyKind(t), Unit: unit, Window: w, Reservation: reservation, Event: event}
}

// Candidate é o evento que se quer gravar (ou que se está lendo, para saber se
// está em conflito).
type Candidate struct {
	// Event é o próprio evento, na edição; vazio na criação.
	Event string
	// Reservation é a reserva ligada, quando há.
	Reservation string
	// Units são as unidades que o evento ocupa: a gravada, ou as da reserva
	// ligada, ou nenhuma (reunião sem apartamento não ocupa unidade).
	Units  []string
	Window Window
	AllDay bool
	// Assignee é o responsável, quando há.
	Assignee string
}

// UnitConflicts devolve as ocupações que colidem com o candidato numa das
// unidades dele, em ordem de início. Vazia quando não há conflito.
//
// Serve à escrita (via CheckUnit) e à LEITURA: o evento que já estava na
// agenda quando a venda chegou não é recusado nem apagado — ele passa a
// colidir, e a lista mostra (`has_unit_conflict`).
func UnitConflicts(c Candidate, occ []Occupancy) []Occupancy {
	unidades := make(map[string]bool, len(c.Units))
	for _, u := range c.Units {
		unidades[u] = true
	}
	var out []Occupancy
	for _, o := range occ {
		if !unidades[o.Unit] {
			continue
		}
		if c.Event != "" && o.Event == c.Event {
			continue // o próprio evento
		}
		if o.Kind == ByStay && c.Reservation != "" && o.Reservation == c.Reservation {
			continue // a estadia da reserva a que o evento pertence
		}
		if c.Window.Overlaps(o.Window) {
			out = append(out, o)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Window.Start.Equal(out[j].Window.Start) {
			return out[i].Window.Start.Before(out[j].Window.Start)
		}
		return out[i].Unit < out[j].Unit
	})
	return out
}

// CheckUnit recusa o evento que colide numa unidade, com UNIT_NOT_AVAILABLE e
// `details.conflicts`: para cada colisão, a unidade, o que a ocupa e a janela
// — sem id, título nem pessoa. O ator pode não ter escopo para ver o evento
// alheio, e "a unidade está em limpeza das 11:00 às 14:00" basta para escolher
// outro horário.
func CheckUnit(c Candidate, occ []Occupancy, z calendar.Zone) error {
	conflitos := UnitConflicts(c, occ)
	if len(conflitos) == 0 {
		return nil
	}
	lista := make([]map[string]any, len(conflitos))
	for i, o := range conflitos {
		lista[i] = map[string]any{
			"unit_id":   o.Unit,
			"kind":      string(o.Kind),
			"starts_at": z.In(o.Window.Start).Format(time.RFC3339),
			"ends_at":   z.In(o.Window.End).Format(time.RFC3339),
		}
	}
	return &RuleError{
		Code:    codeUnitNotAvailable,
		Message: "A unidade está ocupada nesse horário.",
		Details: map[string]any{"conflicts": lista},
	}
}

// ─────────────────────────── Horário: aviso ─────────────────────────

// WarningKind é o tipo do aviso de horário.
type WarningKind string

const (
	// OutsideHours — o evento não cabe num intervalo de funcionamento do dia.
	OutsideHours WarningKind = "fora_do_horario"
	// OperationBlocked — o evento cruza um bloqueio de horário da operação.
	OperationBlocked WarningKind = "bloqueio_de_horario"
	// AssigneeBusy — o responsável tem outro compromisso que cruza este, já
	// contada a folga (`buffer_minutes`) dos dois lados.
	AssigneeBusy WarningKind = "responsavel_ocupado"
)

// Busy é um compromisso que pode gerar aviso: um bloqueio de horário, ou um
// evento agendado do mesmo responsável.
type Busy struct {
	Ref    string
	Window Window
}

// Warning é um aviso de horário. `Ref` e `Window` são do compromisso com que
// o candidato cruza; vazios em OutsideHours.
type Warning struct {
	Kind    WarningKind
	Message string
	Ref     string
	Window  Window
}

// TimeWarnings devolve os avisos de horário do candidato — nunca recusa.
//
//   - Evento de dia inteiro não recebe `fora_do_horario`: ele não tem hora.
//   - O compromisso do responsável conta com a folga dos dois lados: com 15
//     minutos de folga, visita até 11:00 e outra a partir das 11:00 avisam.
//   - Sem responsável, não há a quem faltar folga.
func TimeWarnings(c Candidate, s Settings, z calendar.Zone, blocks []Busy, assigneeAgenda []Busy) []Warning {
	var out []Warning
	if !c.AllDay && !withinHours(c.Window, s, z) {
		out = append(out, Warning{
			Kind:    OutsideHours,
			Message: "Fora do horário de funcionamento da operação.",
		})
	}
	for _, b := range ordenados(blocks) {
		if c.Window.Overlaps(b.Window) {
			out = append(out, Warning{
				Kind:    OperationBlocked,
				Message: "Cruza um bloqueio de horário da operação.",
				Ref:     b.Ref,
				Window:  b.Window,
			})
		}
	}
	if c.Assignee != "" {
		folga := s.Buffer()
		for _, b := range ordenados(assigneeAgenda) {
			if c.Event != "" && b.Ref == c.Event {
				continue
			}
			alargada := Window{Start: b.Window.Start.Add(-folga), End: b.Window.End.Add(folga)}
			if c.Window.Overlaps(alargada) {
				out = append(out, Warning{
					Kind:    AssigneeBusy,
					Message: "O responsável tem outro compromisso nesse horário (contada a folga entre compromissos).",
					Ref:     b.Ref,
					Window:  b.Window,
				})
			}
		}
	}
	return out
}

func ordenados(bs []Busy) []Busy {
	out := append([]Busy(nil), bs...)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Window.Start.Equal(out[j].Window.Start) {
			return out[i].Window.Start.Before(out[j].Window.Start)
		}
		return out[i].Ref < out[j].Ref
	})
	return out
}
