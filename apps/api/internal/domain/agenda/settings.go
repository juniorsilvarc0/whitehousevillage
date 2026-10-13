package agenda

import (
	"fmt"
	"sort"
	"time"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

// Os horários da operação são DADO (`agenda_settings`, uma linha por
// propriedade), não constante — é o que destrava a decisão 12 do roadmap. Os
// valores que nascem no banco são provisórios (check-in 14:00, check-out
// 11:00, limpeza das 11:00 às 14:00) e estão marcados "a confirmar pelo dono";
// quem os confirma é ele, em Configurações → Agenda (`PUT /agenda/settings`).
//
// Este arquivo não carrega valor nenhum: carrega a COERÊNCIA entre eles.

// Settings é a configuração da agenda de uma propriedade.
type Settings struct {
	// CheckIn é a hora em que a unidade é entregue ao hóspede.
	CheckIn calendar.Clock
	// CheckOut é a hora até a qual o hóspede devolve a unidade.
	CheckOut calendar.Clock
	// CleaningStart e CleaningEnd são a janela de limpeza do dia da saída.
	CleaningStart calendar.Clock
	CleaningEnd   calendar.Clock
	// BusinessHours é o horário de funcionamento da operação, por dia da
	// semana. Dia sem intervalo é dia fechado. Vale só para AVISO: evento
	// marcado fora dele responde `201` com `fora_do_horario`.
	BusinessHours []Hours
	// SlotMinutes é o passo da grade das visões de dia e semana e a duração do
	// check-in e do check-out.
	SlotMinutes int
	// BufferMinutes é a folga mínima entre dois compromissos da MESMA pessoa.
	// Sem ela, quem sai de uma visita às 11:00 "pode" estar noutra às 11:00 do
	// outro lado da casa.
	BufferMinutes int
}

// Hours é um intervalo de funcionamento: num dia da semana ISO (1 = segunda …
// 7 = domingo), de Start a End. End pode ser 24:00.
type Hours struct {
	Weekday int
	Start   calendar.Clock
	End     calendar.Clock
}

// Tetos da configuração. Não são regra comercial: são guarda de digitação,
// como `booking.QuoteValidityMaxDays`.
const (
	MinSlotMinutes     = 5
	MaxSlotMinutes     = 240
	MaxBufferMinutes   = 240
	MaxIntervalsPerDay = 4
)

// Slot é a duração do passo da grade.
func (s Settings) Slot() time.Duration { return time.Duration(s.SlotMinutes) * time.Minute }

// Buffer é a folga entre compromissos da mesma pessoa.
func (s Settings) Buffer() time.Duration { return time.Duration(s.BufferMinutes) * time.Minute }

// Validate recusa a configuração incoerente, com VALIDATION_ERROR e TODAS as
// violações em `details` (campo → mensagem) — a tela marca os campos de uma
// vez, em vez de um por tentativa.
//
// A regra que organiza as outras é o dia de troca (back-to-back): um hóspede
// sai, a unidade é limpa e o seguinte entra, no mesmo dia. Para isso caber sem
// a agenda colidir consigo mesma,
//
//	check-out  ≤  início da limpeza  <  fim da limpeza  ≤  check-in
//
// e o check-out (que termina na hora de saída) e o check-in (que começa na de
// entrada) têm de caber, cada um, num passo da grade dentro do dia. Com isso,
// os três eventos de um dia de troca nunca se sobrepõem na mesma unidade — é a
// propriedade que `TestTrocaNoMesmoDiaNaoColide` confere.
//
// O banco repete as desigualdades de horário em `CHECK` (a linha é escrita por
// dois caminhos, a API e o seed); a validação do horário de funcionamento é
// só daqui, porque é `jsonb`.
func (s Settings) Validate() error {
	erros := map[string]any{}
	relogio := func(campo string, c calendar.Clock, comeco bool) bool {
		if !c.Valid() || (comeco && c == calendar.EndOfDay) {
			erros[campo] = "hora inválida: use HH:MM, de 00:00 a 23:59."
			return false
		}
		return true
	}
	okIn := relogio("checkin_time", s.CheckIn, true)
	okOut := relogio("checkout_time", s.CheckOut, false)
	okCS := relogio("cleaning_start", s.CleaningStart, true)
	okCE := relogio("cleaning_end", s.CleaningEnd, false)

	if okOut && okCS && s.CleaningStart < s.CheckOut {
		erros["cleaning_start"] = fmt.Sprintf(
			"a limpeza começa depois que o hóspede sai: o check-out é às %s.", s.CheckOut)
	}
	if okCS && okCE && s.CleaningEnd <= s.CleaningStart {
		erros["cleaning_end"] = "o fim da limpeza tem de ser depois do início."
	}
	if okCE && okIn && s.CheckIn < s.CleaningEnd {
		erros["checkin_time"] = fmt.Sprintf(
			"o próximo hóspede entra depois que a limpeza termina: a limpeza vai até %s.", s.CleaningEnd)
	}

	okSlot := s.SlotMinutes >= MinSlotMinutes && s.SlotMinutes <= MaxSlotMinutes &&
		int(calendar.EndOfDay)%s.SlotMinutes == 0
	if !okSlot {
		erros["slot_minutes"] = fmt.Sprintf(
			"de %d a %d minutos, e o dia tem de se dividir em passos inteiros (5, 10, 15, 20, 30, 60…).",
			MinSlotMinutes, MaxSlotMinutes)
	}
	if s.BufferMinutes < 0 || s.BufferMinutes > MaxBufferMinutes {
		erros["buffer_minutes"] = fmt.Sprintf("de 0 a %d minutos.", MaxBufferMinutes)
	}
	if okSlot && okOut && s.CheckOut.Minutes() < s.SlotMinutes {
		if _, ja := erros["checkout_time"]; !ja {
			erros["checkout_time"] = fmt.Sprintf(
				"o check-out ocupa os %d minutos que terminam nele, e eles têm de caber no dia.", s.SlotMinutes)
		}
	}
	if okSlot && okIn && s.CheckIn.Minutes()+s.SlotMinutes > calendar.EndOfDay.Minutes() {
		if _, ja := erros["checkin_time"]; !ja {
			erros["checkin_time"] = fmt.Sprintf(
				"o check-in ocupa os %d minutos que começam nele, e eles têm de caber no dia.", s.SlotMinutes)
		}
	}

	validarHorario(s.BusinessHours, erros)

	if len(erros) == 0 {
		return nil
	}
	return &RuleError{Code: codeValidation, Message: "Configuração da agenda inválida.", Details: erros}
}

// validarHorario confere cada intervalo e a coerência entre os do mesmo dia.
func validarHorario(hs []Hours, erros map[string]any) {
	porDia := map[int][]int{}
	for i, h := range hs {
		campo := fmt.Sprintf("business_hours[%d]", i)
		ok := true
		if h.Weekday < 1 || h.Weekday > 7 {
			erros[campo+".weekday"] = "dia da semana ISO: 1 (segunda) a 7 (domingo)."
			ok = false
		}
		if !h.Start.Valid() || h.Start == calendar.EndOfDay {
			erros[campo+".start"] = "hora inválida: use HH:MM, de 00:00 a 23:59."
			ok = false
		}
		if !h.End.Valid() {
			erros[campo+".end"] = "hora inválida: use HH:MM, de 00:00 a 24:00."
			ok = false
		}
		if ok && h.End <= h.Start {
			erros[campo+".end"] = "o fim tem de ser depois do início — intervalo que atravessa a meia-noite vira dois, um em cada dia."
			ok = false
		}
		if ok {
			porDia[h.Weekday] = append(porDia[h.Weekday], i)
		}
	}
	for dia, idx := range porDia {
		if len(idx) > MaxIntervalsPerDay {
			erros["business_hours"] = fmt.Sprintf(
				"no máximo %d intervalos por dia; o dia %d tem %d.", MaxIntervalsPerDay, dia, len(idx))
		}
		sort.Slice(idx, func(a, b int) bool { return hs[idx[a]].Start < hs[idx[b]].Start })
		for k := 1; k < len(idx); k++ {
			if hs[idx[k]].Start < hs[idx[k-1]].End {
				erros[fmt.Sprintf("business_hours[%d]", idx[k])] = "sobrepõe outro intervalo do mesmo dia."
			}
		}
	}
}

// isoWeekday é o dia da semana ISO da data: 1 = segunda … 7 = domingo.
func isoWeekday(d calendar.Date) int {
	if w := int(d.Weekday()); w != 0 {
		return w
	}
	return 7
}

// withinHours diz se a janela cabe INTEIRA num intervalo de funcionamento do
// dia em que começa. Janela que atravessa a meia-noite só cabe se o intervalo
// for até 24:00 e ela terminar exatamente aí.
func withinHours(w Window, s Settings, z calendar.Zone) bool {
	dia := z.DateOf(w.Start)
	wd := isoWeekday(dia)
	for _, h := range s.BusinessHours {
		if h.Weekday != wd {
			continue
		}
		if !w.Start.Before(z.At(dia, h.Start)) && !w.End.After(z.At(dia, h.End)) {
			return true
		}
	}
	return false
}
