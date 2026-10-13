package agenda

import (
	"math/rand"
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

func TestProvisoriosSaoCoerentes(t *testing.T) {
	if err := provisorios().Validate(); err != nil {
		t.Fatalf("os horários que o banco semeia não passam na própria regra: %v", err)
	}
}

// Cada recusa, com o campo que a tela marca. Controle negativo de cada regra
// de Validate: remover a linha que a impõe deixa o caso dela verde por engano
// — e este teste vermelho.
func TestValidateMesa(t *testing.T) {
	casos := []struct {
		nome   string
		mexe   func(*Settings)
		campos []string
	}{
		{"limpeza antes da saída", func(s *Settings) { s.CleaningStart = d("10:30") }, []string{"cleaning_start"}},
		{"limpeza sem duração", func(s *Settings) { s.CleaningEnd = d("11:00") }, []string{"cleaning_end"}},
		{"limpeza invertida", func(s *Settings) { s.CleaningEnd = d("10:00"); s.CleaningStart = d("12:00") }, []string{"cleaning_end"}},
		{"entrada antes do fim da limpeza", func(s *Settings) { s.CheckIn = d("13:00") }, []string{"checkin_time"}},
		{"passo zero", func(s *Settings) { s.SlotMinutes = 0 }, []string{"slot_minutes"}},
		{"passo que não divide o dia", func(s *Settings) { s.SlotMinutes = 25 }, []string{"slot_minutes"}},
		{"passo acima do teto", func(s *Settings) { s.SlotMinutes = 480 }, []string{"slot_minutes"}},
		{"folga negativa", func(s *Settings) { s.BufferMinutes = -1 }, []string{"buffer_minutes"}},
		{"folga acima do teto", func(s *Settings) { s.BufferMinutes = 241 }, []string{"buffer_minutes"}},
		{"check-out que não cabe depois da meia-noite", func(s *Settings) {
			s.CheckOut, s.CleaningStart, s.SlotMinutes = d("00:10"), d("00:10"), 30
		}, []string{"checkout_time"}},
		{"check-in que não cabe antes da meia-noite", func(s *Settings) {
			s.CheckIn, s.SlotMinutes = d("23:45"), 30
		}, []string{"checkin_time"}},
		{"entrada às 24:00", func(s *Settings) { s.CheckIn = calendar.EndOfDay }, []string{"checkin_time"}},
		{"hora fora do relógio", func(s *Settings) { s.CheckOut = calendar.Clock(-5) }, []string{"checkout_time"}},
		{"dia da semana zero", func(s *Settings) { s.BusinessHours[0].Weekday = 0 }, []string{"business_hours[0].weekday"}},
		{"dia da semana oito", func(s *Settings) { s.BusinessHours[0].Weekday = 8 }, []string{"business_hours[0].weekday"}},
		{"intervalo invertido", func(s *Settings) { s.BusinessHours[1].End = d("07:00") }, []string{"business_hours[1].end"}},
		{"intervalo que começa às 24:00", func(s *Settings) { s.BusinessHours[1].Start = calendar.EndOfDay }, []string{"business_hours[1].start"}},
		{"intervalos sobrepostos no mesmo dia", func(s *Settings) {
			s.BusinessHours = append(s.BusinessHours, Hours{Weekday: 1, Start: d("19:00"), End: d("22:00")})
		}, []string{"business_hours[7]"}},
		{"intervalos demais num dia", func(s *Settings) {
			s.BusinessHours = []Hours{
				{1, d("06:00"), d("07:00")}, {1, d("08:00"), d("09:00")}, {1, d("10:00"), d("11:00")},
				{1, d("12:00"), d("13:00")}, {1, d("14:00"), d("15:00")},
			}
		}, []string{"business_hours"}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			s := provisorios()
			c.mexe(&s)
			err := s.Validate()
			if err == nil {
				t.Fatal("aceitou")
			}
			re := regra(t, err)
			if re.Code != codeValidation {
				t.Fatalf("code %s", re.Code)
			}
			for _, campo := range c.campos {
				if _, ok := re.Details[campo]; !ok {
					t.Errorf("faltou %q em details: %v", campo, re.Details)
				}
			}
		})
	}
}

// O que a regra deixa passar de propósito.
func TestValidateAceita(t *testing.T) {
	casos := map[string]func(*Settings){
		"limpeza encostada na saída e na entrada": func(s *Settings) {},
		"limpeza com folga dos dois lados":        func(s *Settings) { s.CleaningStart, s.CleaningEnd = d("11:30"), d("13:30") },
		"funcionamento até 24:00":                 func(s *Settings) { s.BusinessHours[0].End = calendar.EndOfDay },
		"dia fechado (sem intervalo)":             func(s *Settings) { s.BusinessHours = s.BusinessHours[:6] },
		"almoço: dois intervalos que se encostam": func(s *Settings) {
			s.BusinessHours = []Hours{{1, d("08:00"), d("12:00")}, {1, d("12:00"), d("18:00")}}
		},
		"sem folga":  func(s *Settings) { s.BufferMinutes = 0 },
		"passo de 5": func(s *Settings) { s.SlotMinutes = 5 },
	}
	for nome, mexe := range casos {
		s := provisorios()
		mexe(&s)
		if err := s.Validate(); err != nil {
			t.Errorf("%s: recusou — %v", nome, err)
		}
	}
}

// Validate junta TODAS as violações: a tela marca os campos de uma vez.
func TestValidateJuntaTudo(t *testing.T) {
	s := provisorios()
	s.CleaningStart, s.SlotMinutes, s.BufferMinutes = d("10:00"), 7, 999
	re := regra(t, s.Validate())
	for _, campo := range []string{"cleaning_start", "slot_minutes", "buffer_minutes"} {
		if _, ok := re.Details[campo]; !ok {
			t.Errorf("faltou %q: %v", campo, re.Details)
		}
	}
}

// configuracaoSorteada devolve horários quaisquer — coerentes ou não.
func configuracaoSorteada(rng *rand.Rand) Settings {
	slots := []int{5, 10, 15, 20, 30, 45, 60, 90, 120, 7, 25, 0}
	c := func() calendar.Clock { return calendar.Clock(rng.Intn(int(calendar.EndOfDay) + 1)) }
	s := Settings{
		CheckOut:      c(),
		SlotMinutes:   slots[rng.Intn(len(slots))],
		BufferMinutes: rng.Intn(300) - 30,
	}
	// Metade dos casos parte de uma ordem plausível, para a amostra de
	// aceitos não ser vazia.
	if rng.Intn(2) == 0 {
		s.CleaningStart = s.CheckOut + calendar.Clock(rng.Intn(120))
		s.CleaningEnd = s.CleaningStart + calendar.Clock(rng.Intn(300))
		s.CheckIn = s.CleaningEnd + calendar.Clock(rng.Intn(200))
	} else {
		s.CleaningStart, s.CleaningEnd, s.CheckIn = c(), c(), c()
	}
	return s
}

// Propriedade do dia de troca, sobre milhares de configurações sorteadas:
// toda configuração que Validate ACEITA põe o check-out e a limpeza de uma
// estadia e o check-in da seguinte, no mesmo dia e na mesma unidade, sem
// nenhuma sobreposição — e dentro do dia. É a promessa que justifica cada
// desigualdade de Validate. O controle conta os aceitos e os recusados, para
// a propriedade não passar por vacuidade.
func TestTrocaNoMesmoDiaNaoColide(t *testing.T) {
	rng := rand.New(rand.NewSource(20261013))
	troca := dia("2026-11-20")
	var aceitos, recusados int
	for i := 0; i < 50000; i++ {
		s := configuracaoSorteada(rng)
		if s.Validate() != nil {
			recusados++
			continue
		}
		aceitos++
		out, _ := AutoWindow(CheckOut, troca, s, fz)
		limpa, _ := AutoWindow(Cleaning, troca, s, fz)
		in, _ := AutoWindow(CheckIn, troca, s, fz)
		for _, w := range []Window{out, limpa, in} {
			if !w.Valid() {
				t.Fatalf("caso %d (%+v): janela vazia %v", i, s, w)
			}
			if fz.DateOf(w.Start) != troca || w.End.After(fz.At(troca, calendar.EndOfDay)) {
				t.Fatalf("caso %d (%+v): janela %v fora do dia da troca", i, s, w)
			}
		}
		if out.Overlaps(limpa) || limpa.Overlaps(in) || out.Overlaps(in) {
			t.Fatalf("caso %d (%+v): o dia de troca colide consigo mesmo: out %v, limpeza %v, in %v", i, s, out, limpa, in)
		}
	}
	if aceitos < 2000 || recusados < 2000 {
		t.Fatalf("amostra fraca: %d aceitos, %d recusados", aceitos, recusados)
	}
}
