package agenda

import (
	"fmt"
	"time"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

// A agenda que se enche sozinha (F2-20): o que a reserva põe na agenda quando
// é confirmada, e o que acontece com isso a cada passo dela.
//
// | Na reserva | Na agenda (eventos de origem `reservation`, e os manuais ligados a ela) |
// |---|---|
// | confirmada (`/confirm`, remarcação que nasce confirmada, preenchimento) | `Plan`: check-in, check-out e limpeza, `agendado` |
// | entrada registrada (`/check-in`) | `OnCheckIn`: o check-in vira `concluido` |
// | saída registrada (`/check-out`) | `OnCheckOut`: o check-out vira `concluido`; saída ANTECIPADA traz check-out e limpeza para o dia em que a unidade esvaziou |
// | cancelada ou no-show sem ter entrado | `OnCancel`: tudo que estava `agendado` vira `cancelado` |
// | cancelada ou no-show com o hóspede dentro | `OnCancel`: o check-out é cancelado; a limpeza vem para hoje; o manual que ainda não começou é cancelado |
// | remarcada | é cancelar a original e confirmar a nova (spec §5): `OnCancel` numa, `Plan` na outra |
// | realocada | nada nos automáticos (a unidade é lida da reserva); `MovesOnReassign` diz que manual muda junto |
// | pré-reserva vencida | nada: `hold` não tem agenda |
//
// Nada é apagado. Cancelado continua na agenda como `cancelado`, porque a
// operação precisa saber que havia um check-in ali — e a trilha é a mesma do
// calendário, onde a data liberada também não some.

// Stay é a reserva como a agenda precisa dela.
type Stay struct {
	CheckIn  calendar.Date
	CheckOut calendar.Date // exclusivo, como em toda estadia
	// CheckedInAt é o instante da entrada, quando a reserva já está
	// `checked_in` — só o preenchimento das reservas anteriores à agenda passa
	// por aqui com ela em curso. Nil é reserva confirmada que não entrou.
	CheckedInAt *time.Time
}

// Planned é um evento automático a gravar.
type Planned struct {
	Type   Type
	Window Window
	Status Status
	DoneAt *time.Time
}

// AutoWindow é a janela do evento automático `t` no dia `day`, pelos horários
// de `s`:
//
//   - check-in: o passo da grade que COMEÇA na hora de entrada;
//   - check-out: o passo que TERMINA na hora de saída;
//   - limpeza: a janela de limpeza.
//
// O check-out termina na hora de saída, e não começa nela, para encostar na
// limpeza sem colidir: o hóspede sai até as 11:00, a limpeza começa às 11:00.
// O segundo retorno é false para tipo que não nasce da reserva.
func AutoWindow(t Type, day calendar.Date, s Settings, z calendar.Zone) (Window, bool) {
	slot := calendar.Clock(s.SlotMinutes)
	switch t {
	case CheckIn:
		return Window{Start: z.At(day, s.CheckIn), End: z.At(day, s.CheckIn+slot)}, true
	case CheckOut:
		return Window{Start: z.At(day, s.CheckOut-slot), End: z.At(day, s.CheckOut)}, true
	case Cleaning:
		return Window{Start: z.At(day, s.CleaningStart), End: z.At(day, s.CleaningEnd)}, true
	}
	return Window{}, false
}

// Plan devolve os três eventos que a reserva confirmada põe na agenda, na
// ordem check-in, check-out, limpeza: o check-in no dia da entrada, o
// check-out e a limpeza no dia da saída.
//
// É a MESMA função para os três caminhos que confirmam — o `/confirm`, a
// remarcação que nasce confirmada e o preenchimento das reservas confirmadas
// antes de a agenda existir. Três cópias da regra divergiriam na primeira
// mudança de horário.
//
// Reserva já em curso (`CheckedInAt`) nasce com o check-in `concluido`: ele
// aconteceu, e um check-in `agendado` no passado seria uma pendência falsa na
// tela de quem recebe hóspedes.
//
// Erro aqui é defeito de quem chamou (estadia invertida, configuração que o
// banco não deveria ter aceitado), não recusa de negócio: o `/confirm` que o
// recebe responde 500 em vez de confirmar sem agenda.
func Plan(st Stay, s Settings, z calendar.Zone) ([]Planned, error) {
	if !st.CheckOut.After(st.CheckIn) {
		return nil, fmt.Errorf("agenda: estadia [%s, %s) sem noite", st.CheckIn, st.CheckOut)
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("agenda: configuração gravada é incoerente: %w", err)
	}
	in, _ := AutoWindow(CheckIn, st.CheckIn, s, z)
	out, _ := AutoWindow(CheckOut, st.CheckOut, s, z)
	clean, _ := AutoWindow(Cleaning, st.CheckOut, s, z)

	checkin := Planned{Type: CheckIn, Window: in, Status: Scheduled}
	if st.CheckedInAt != nil {
		at := *st.CheckedInAt
		checkin.Status, checkin.DoneAt = Done, &at
	}
	return []Planned{
		checkin,
		{Type: CheckOut, Window: out, Status: Scheduled},
		{Type: Cleaning, Window: clean, Status: Scheduled},
	}, nil
}

// ─────────────────────────── O que a reserva faz com a agenda ───────

// Linked é um evento ligado à reserva — automático, ou manual com
// `reservation_id` —, como está gravado.
type Linked struct {
	Type   Type
	Origin Origin
	Status Status
	Window Window
	// UnitRef é a unidade gravada no evento manual (`unit_id`); vazia no
	// automático, cuja unidade é lida da reserva, e no manual que ocupa a
	// reserva inteira.
	UnitRef string
}

// Effect é o que fazer com a linha.
type Effect string

const (
	// Keep — nada.
	Keep Effect = "manter"
	// CancelIt — `status = 'cancelado'`, `cancelled_at = now()`. Nunca DELETE.
	CancelIt Effect = "cancelar"
	// CompleteIt — `status = 'concluido'`, `done_at`, `done_by`, e a janela de
	// `Decision.Window` (que só difere da gravada na saída antecipada).
	CompleteIt Effect = "concluir"
	// MoveIt — a janela passa a ser `Decision.Window`.
	MoveIt Effect = "mover"
)

// Decision é o destino de UMA linha. As funções abaixo devolvem uma por
// evento recebido, na mesma ordem.
type Decision struct {
	Effect Effect
	Window Window
	DoneAt *time.Time
}

func keepAll(evs []Linked) []Decision {
	out := make([]Decision, len(evs))
	for i, e := range evs {
		out[i] = Decision{Effect: Keep, Window: e.Window}
	}
	return out
}

// OnCheckIn: a entrada registrada em `at` conclui o check-in automático que
// estava agendado. A janela não muda — o hóspede que chega às 19:00 chegou
// atrasado para o check-in das 14:00, e é isso que a tela deve mostrar.
func OnCheckIn(evs []Linked, at time.Time) []Decision {
	out := keepAll(evs)
	for i, e := range evs {
		if e.Origin == FromReservation && e.Type == CheckIn && e.Status == Scheduled {
			t := at
			out[i] = Decision{Effect: CompleteIt, Window: e.Window, DoneAt: &t}
		}
	}
	return out
}

// OnCheckOut: a saída registrada em `at` conclui o check-out automático.
//
// Saída ANTECIPADA — `at` cai num dia anterior ao da saída contratada — é o
// caso que importa: o `/check-out` manda os blocos da reserva para
// `completed`, e as noites restantes voltam a ser vendáveis HOJE. Quem pode
// entrar amanhã precisa da unidade limpa hoje. Por isso a limpeza automática
// que ainda estava agendada para o dia contratado vem para o dia em que a
// unidade esvaziou, na janela de limpeza; e o check-out concluído é desenhado
// nesse dia, e não no dia em que não aconteceu.
//
// Os eventos manuais ligados à reserva não são tocados: a faxina pós-estadia
// que alguém marcou para depois continua valendo.
func OnCheckOut(evs []Linked, at time.Time, st Stay, s Settings, z calendar.Zone) []Decision {
	out := keepAll(evs)
	dia := z.DateOf(at)
	antecipada := dia.Before(st.CheckOut)
	for i, e := range evs {
		if e.Origin != FromReservation || e.Status != Scheduled {
			continue
		}
		switch e.Type {
		case CheckOut:
			w := e.Window
			if antecipada {
				w, _ = AutoWindow(CheckOut, dia, s, z)
			}
			t := at
			out[i] = Decision{Effect: CompleteIt, Window: w, DoneAt: &t}
		case Cleaning:
			if antecipada && z.DateOf(e.Window.Start).After(dia) {
				w, _ := AutoWindow(Cleaning, dia, s, z)
				out[i] = Decision{Effect: MoveIt, Window: w}
			}
		}
	}
	return out
}

// OnCancel decide a agenda da reserva cancelada (inclusive por no-show e pela
// remarcação, que cancela a original), no instante `now`.
//
// Sem o hóspede ter entrado, a estadia não vai acontecer: TODO evento
// agendado ligado a ela — os três automáticos e os manuais — é cancelado,
// inclusive o check-in vencido de quem nunca apareceu.
//
// Com o hóspede dentro (`checkedIn`), a estadia acabou agora, e a unidade
// esvazia hoje:
//
//   - o check-out automático é cancelado — não houve saída registrada, houve
//     cancelamento, e a tela não pode afirmar o contrário;
//   - a limpeza automática vem para HOJE, na janela de limpeza, se estava para
//     depois (o calendário devolveu as noites restantes ao estoque, e a
//     unidade vende amanhã); se já era hoje ou antes, fica;
//   - o manual ligado à reserva que ainda não começou é cancelado; o que já
//     começou fica, porque aconteceu.
//
// Concluído e cancelado nunca mudam.
func OnCancel(evs []Linked, checkedIn bool, now time.Time, s Settings, z calendar.Zone) []Decision {
	out := keepAll(evs)
	hoje := z.DateOf(now)
	for i, e := range evs {
		if e.Status != Scheduled {
			continue
		}
		if !checkedIn {
			out[i].Effect = CancelIt
			continue
		}
		switch {
		case e.Origin == FromReservation && e.Type == Cleaning:
			if z.DateOf(e.Window.Start).After(hoje) {
				w, _ := AutoWindow(Cleaning, hoje, s, z)
				out[i] = Decision{Effect: MoveIt, Window: w}
			}
		case e.Origin == FromReservation:
			out[i].Effect = CancelIt
		default:
			if !e.Window.Start.Before(now) {
				out[i].Effect = CancelIt
			}
		}
	}
	return out
}

// MovesOnReassign diz se o evento muda de unidade quando a reserva é realocada
// de `fromUnit` para outra. Só o MANUAL agendado preso à unidade que sai: o
// automático não tem unidade gravada (é lida da reserva, então já está na
// nova), e o que aconteceu — concluído — aconteceu onde aconteceu.
func MovesOnReassign(l Linked, fromUnit string) bool {
	return l.Origin == ByHand && l.Status == Scheduled && l.UnitRef != "" && l.UnitRef == fromUnit
}

// Retime decide a janela de um evento quando a configuração muda de `old`
// para `next` (`PUT /agenda/settings`), no dia `today`.
//
// Muda só o evento automático, agendado, de hoje em diante, e que ainda está
// EXATAMENTE no horário que `old` lhe daria. O que alguém ajustou à mão (o
// hóspede que pediu para sair às 13:00) não é reescrito; o passado não é
// reescrito. É isso que faz a confirmação dos horários provisórios pelo dono
// valer também para tudo o que a agenda já tinha enchido com eles — sem
// precisar de uma coluna "foi ajustado".
//
// O segundo retorno diz se a janela mudou.
func Retime(l Linked, old, next Settings, z calendar.Zone, today calendar.Date) (Window, bool) {
	if l.Origin != FromReservation || l.Status != Scheduled {
		return l.Window, false
	}
	dia := z.DateOf(l.Window.Start)
	if dia.Before(today) {
		return l.Window, false
	}
	antes, ok := AutoWindow(l.Type, dia, old, z)
	if !ok || !antes.Equal(l.Window) {
		return l.Window, false
	}
	depois, _ := AutoWindow(l.Type, dia, next, z)
	return depois, !depois.Equal(l.Window)
}
