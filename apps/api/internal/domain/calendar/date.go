// Package calendar trata data civil e classificação comercial da diária.
//
// Data de estadia NÃO é instante: "21 de agosto" é o mesmo dia para o hóspede
// em qualquer fuso. Modelar como time.Time e converter entre fusos é a origem
// clássica da reserva que "pula um dia" — por isso Date é um tipo próprio.
package calendar

import (
	"fmt"
	"time"
)

// Date é uma data civil, sem hora e sem fuso.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// Parse lê o formato ISO YYYY-MM-DD.
func Parse(s string) (Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return Date{}, fmt.Errorf("data inválida %q: %w", s, err)
	}
	return Date{t.Year(), t.Month(), t.Day()}, nil
}

// MustParse é para teste e seed — entra em pânico com data inválida.
func MustParse(s string) Date {
	d, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

func (d Date) String() string { return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day) }

func (d Date) time() time.Time { return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC) }

// AddDays devolve a data deslocada em n dias.
func (d Date) AddDays(n int) Date {
	t := d.time().AddDate(0, 0, n)
	return Date{t.Year(), t.Month(), t.Day()}
}

func (d Date) Weekday() time.Weekday { return d.time().Weekday() }

func (d Date) Before(o Date) bool { return d.time().Before(o.time()) }
func (d Date) After(o Date) bool  { return d.time().After(o.time()) }
func (d Date) Equal(o Date) bool  { return d == o }

// Nights conta as noites de [d, out) — o intervalo half-open que o sistema usa
// em todo lugar. De 20 a 23 são 3 noites; o dia do check-out fica livre.
func (d Date) Nights(out Date) int {
	return int(out.time().Sub(d.time()).Hours() / 24)
}

// Range devolve as noites de [in, out).
func Range(in, out Date) []Date {
	n := in.Nights(out)
	if n <= 0 {
		return nil
	}
	days := make([]Date, 0, n)
	for cur := in; cur.Before(out); cur = cur.AddDays(1) {
		days = append(days, cur)
	}
	return days
}
