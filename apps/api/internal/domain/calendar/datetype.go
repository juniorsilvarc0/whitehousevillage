package calendar

import "time"

// DateType é a classificação comercial de uma diária.
type DateType string

const (
	Normal     DateType = "normal"
	Weekend    DateType = "fds"
	Holiday    DateType = "feriado"
	HighSeason DateType = "alta"
	NewYear    DateType = "reveillon"
	Carnival   DateType = "carnaval"
)

// Precedence é o peso de cada tipo. Vive como dado — e não como cadeia de
// if/else — porque a ordem de resolução é decisão comercial, não técnica.
var Precedence = map[DateType]int{
	NewYear:    100,
	Carnival:   100,
	Holiday:    80,
	HighSeason: 60,
	Weekend:    40,
	Normal:     0,
}

// Period é um intervalo comercial (réveillon, carnaval, alta temporada).
// Períodos PODEM se sobrepor de propósito — a precedência resolve.
type Period struct {
	Name string
	Type DateType
	From Date
	To   Date // inclusivo
}

func (p Period) Contains(d Date) bool {
	return !d.Before(p.From) && !d.After(p.To)
}

// Classification é o resultado da classificação de uma diária.
type Classification struct {
	Type  DateType
	Label string
}

// Commercial é o calendário comercial: feriados, períodos e quais dias contam
// como fim de semana.
type Commercial struct {
	Holidays    map[string]string // "2026-12-25" → "Natal"
	Periods     []Period
	WeekendDays []time.Weekday // padrão: sexta e sábado
}

// NewCommercial monta o calendário com o fim de semana comercial da casa
// (sexta e sábado — domingo já é diária normal, porque o hóspede vai embora).
func NewCommercial(holidays map[string]string, periods []Period) Commercial {
	return Commercial{
		Holidays:    holidays,
		Periods:     periods,
		WeekendDays: []time.Weekday{time.Friday, time.Saturday},
	}
}

// Classify resolve o tipo de uma diária pela maior precedência.
//
// Réveillon e Carnaval vencem feriado; feriado vence alta temporada; alta vence
// fim de semana. É por isso que 31/12 é réveillon mesmo caindo numa quinta e
// dentro da alta temporada.
func (c Commercial) Classify(d Date) Classification {
	best := Classification{Type: Normal, Label: "Diária normal"}
	bestScore := -1

	consider := func(t DateType, label string) {
		if score := Precedence[t]; score > bestScore {
			best, bestScore = Classification{Type: t, Label: label}, score
		}
	}

	for _, p := range c.Periods {
		if p.Contains(d) {
			consider(p.Type, p.Name)
		}
	}
	if name, ok := c.Holidays[d.String()]; ok {
		consider(Holiday, name)
	}
	for _, wd := range c.WeekendDays {
		if d.Weekday() == wd {
			consider(Weekend, "Fim de semana")
		}
	}
	if bestScore < 0 {
		return Classification{Type: Normal, Label: "Diária normal"}
	}
	return best
}
