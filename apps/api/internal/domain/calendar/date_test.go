package calendar_test

import (
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

// O intervalo é half-open [check-in, check-out): de 20 a 23 são 3 noites, e o
// dia do check-out fica livre para o próximo hóspede (back-to-back).
func TestNoitesSaoHalfOpen(t *testing.T) {
	in, out := calendar.MustParse("2026-11-20"), calendar.MustParse("2026-11-23")
	if got := in.Nights(out); got != 3 {
		t.Errorf("esperava 3 noites, veio %d", got)
	}
	dias := calendar.Range(in, out)
	if len(dias) != 3 || dias[2].String() != "2026-11-22" {
		t.Errorf("a última noite deveria ser 22, veio %v", dias)
	}
}

func TestViradaDeMesEDeAno(t *testing.T) {
	if got := calendar.MustParse("2026-12-31").AddDays(1).String(); got != "2027-01-01" {
		t.Errorf("virada de ano: veio %s", got)
	}
	if got := calendar.MustParse("2028-02-28").AddDays(1).String(); got != "2028-02-29" {
		t.Errorf("ano bissexto: veio %s", got)
	}
	if got := calendar.MustParse("2026-12-28").Nights(calendar.MustParse("2027-01-02")); got != 5 {
		t.Errorf("réveillon atravessando o ano: esperava 5 noites, veio %d", got)
	}
}
