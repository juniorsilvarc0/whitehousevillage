package calendar_test

import (
	"math/rand"
	"testing"
	"time"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

func TestParseClockMesa(t *testing.T) {
	validos := map[string]int{
		"00:00": 0, "09:05": 545, "11:00": 660, "14:00": 840, "23:59": 1439, "24:00": 1440,
	}
	for s, min := range validos {
		c, err := calendar.ParseClock(s)
		if err != nil || c.Minutes() != min {
			t.Errorf("ParseClock(%q) = (%d, %v), esperava %d", s, c, err, min)
		}
		if c.String() != s {
			t.Errorf("ida e volta de %q devolveu %q", s, c.String())
		}
	}
	for _, s := range []string{"", "9:00", "14h", "14:00:00", "24:01", "25:00", "12:60", "ab:cd", "-1:00", "1400"} {
		if _, err := calendar.ParseClock(s); err == nil {
			t.Errorf("ParseClock(%q) aceitou forma fora do contrato", s)
		}
	}
}

// Fortaleza é UTC−3 o ano inteiro: 14:00 na casa é 17:00 em UTC, no dia que
// for — inclusive na virada do ano, que é quando o dia UTC e o dia da casa
// discordam.
func TestAtEmFortaleza(t *testing.T) {
	z := calendar.MustLoadZone("America/Fortaleza")
	got := z.At(calendar.MustParse("2026-12-31"), calendar.MustParseClock("22:30"))
	if want := time.Date(2027, 1, 1, 1, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("31/12 22:30 em Fortaleza = %s, esperava %s", got.UTC(), want)
	}
	// Às 22:30 de 31/12 na casa, o dia UTC já é 2027 — e "hoje" continua 31/12.
	if d := z.DateOf(got); d.String() != "2026-12-31" {
		t.Fatalf("DateOf devolveu %s: o dia da casa não pode seguir o dia UTC", d)
	}
	if c := z.ClockOf(got); c.String() != "22:30" {
		t.Fatalf("ClockOf devolveu %s", c)
	}
	if s := z.In(got).Format(time.RFC3339); s != "2026-12-31T22:30:00-03:00" {
		t.Fatalf("In(...) serializa %s: a string tem de trazer a hora da casa", s)
	}
}

// 24:00 fecha o dia: é a meia-noite do dia seguinte, e não um instante
// inválido.
func TestFimDoDiaEhAMeiaNoiteSeguinte(t *testing.T) {
	z := calendar.MustLoadZone("America/Fortaleza")
	d := calendar.MustParse("2026-02-28")
	if !z.At(d, calendar.EndOfDay).Equal(z.StartOfDay(d.AddDays(1))) {
		t.Fatal("At(d, 24:00) deveria ser o começo de d+1")
	}
}

// Propriedade da conversão, sobre instantes sorteados em dois fusos:
//
//  1. ida e volta — At(DateOf(t), ClockOf(t)) é t truncado no minuto;
//  2. DateOf(At(d, c)) = d e ClockOf(At(d, c)) = c para toda hora < 24:00;
//
// em Fortaleza sempre; em Nova York (que tem horário de verão) fora das duas
// horas que o relógio pula ou repete. O controle conta quantos casos de Nova
// York caíram nelas, para a exceção não esconder a propriedade inteira.
func TestConversaoIdaEVolta(t *testing.T) {
	rng := rand.New(rand.NewSource(20261013))
	for _, nome := range []string{"America/Fortaleza", "America/New_York"} {
		z := calendar.MustLoadZone(nome)
		base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		pulados := 0
		for i := 0; i < 20000; i++ {
			inst := base.Add(time.Duration(rng.Int63n(int64(5 * 365 * 24 * time.Hour)))).Truncate(time.Minute)
			if back := z.At(z.DateOf(inst), z.ClockOf(inst)); !back.Equal(inst) {
				// A hora repetida do outono: 01:20 acontece duas vezes, e
				// `At` só sabe devolver uma delas.
				if nome == "America/Fortaleza" {
					t.Fatalf("%s: ida e volta de %s deu %s", nome, inst, back)
				}
				pulados++
			}

			d := calendar.Date{Year: 2024 + rng.Intn(5), Month: time.Month(1 + rng.Intn(12)), Day: 1 + rng.Intn(28)}
			c := calendar.Clock(rng.Intn(int(calendar.EndOfDay)))
			at := z.At(d, c)
			if z.DateOf(at) != d || z.ClockOf(at) != c {
				if nome == "America/Fortaleza" {
					t.Fatalf("%s: At(%s, %s) = %s voltou como (%s, %s)", nome, d, c, at, z.DateOf(at), z.ClockOf(at))
				}
				pulados++
			}
		}
		if pulados > 20 {
			t.Fatalf("%s: %d casos fora da volta — mais que a hora pulada explica", nome, pulados)
		}
	}
}

// O valor zero não é UTC: é defeito de quem esqueceu de carregar o fuso.
func TestZoneZeroEntraEmPanico(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Zone{} converteu sem fuso — deveria entrar em pânico")
		}
	}()
	var z calendar.Zone
	if !z.IsZero() {
		t.Fatal("Zone{} deveria ser zero")
	}
	z.At(calendar.MustParse("2026-11-20"), calendar.MustParseClock("14:00"))
}

func TestLoadZoneRecusaNomeDesconhecido(t *testing.T) {
	for _, nome := range []string{"", "America/Atlantida", "Fortaleza"} {
		if _, err := calendar.LoadZone(nome); err == nil {
			t.Errorf("LoadZone(%q) aceitou", nome)
		}
	}
}
