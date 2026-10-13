package agenda

import (
	"errors"
	"testing"
	"time"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

var (
	d   = calendar.MustParseClock
	dia = calendar.MustParse
	fz  = calendar.MustLoadZone("America/Fortaleza")

	// O dia D de todos os casos de mesa: 15/11/2026, um domingo.
	hoje = dia("2026-11-15")
)

// provisorios são os horários que o banco semeia, "a confirmar pelo dono"
// (decisão 12): check-in 14:00, check-out 11:00, limpeza das 11:00 às 14:00,
// funcionamento das 08:00 às 20:00 todos os dias, passo de 30 e folga de 15.
func provisorios() Settings {
	s := Settings{
		CheckIn:       d("14:00"),
		CheckOut:      d("11:00"),
		CleaningStart: d("11:00"),
		CleaningEnd:   d("14:00"),
		SlotMinutes:   30,
		BufferMinutes: 15,
	}
	for wd := 1; wd <= 7; wd++ {
		s.BusinessHours = append(s.BusinessHours, Hours{Weekday: wd, Start: d("08:00"), End: d("20:00")})
	}
	return s
}

// em é o instante `hh:mm` do dia `data` na casa.
func em(data, hhmm string) time.Time { return fz.At(dia(data), d(hhmm)) }

func janela(data, de, ate string) Window { return Window{Start: em(data, de), End: em(data, ate)} }

func regra(t *testing.T, err error) *RuleError {
	t.Helper()
	var re *RuleError
	if !errors.As(err, &re) {
		t.Fatalf("esperava RuleError, veio %T: %v", err, err)
	}
	return re
}
