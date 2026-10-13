package agenda

import (
	"math/rand"
	"testing"
	"time"
)

// O AP-01 no dia 20/11: hóspede A sai (estadia até 11:00), limpeza das 11:00
// às 14:00, hóspede B entra às 14:00; o AP-02 em manutenção nos dias 20 e 21.
func ocupacaoDoDia20() []Occupancy {
	s := provisorios()
	return []Occupancy{
		StayOccupancy("AP-01", "RA", dia("2026-11-17"), dia("2026-11-20"), nil, nil, s, fz),
		EventOccupancy(Cleaning, "AP-01", "limpeza-RA", "RA", janela("2026-11-20", "11:00", "14:00")),
		StayOccupancy("AP-01", "RB", dia("2026-11-20"), dia("2026-11-23"), nil, nil, s, fz),
		BlockOccupancy(Maintenance, "AP-02", "bloco-1", dia("2026-11-20"), dia("2026-11-22"), fz),
	}
}

func visita(unidade string, w Window) Candidate {
	return Candidate{Units: []string{unidade}, Window: w}
}

func TestUnitConflictsMesa(t *testing.T) {
	occ := ocupacaoDoDia20()
	casos := []struct {
		nome  string
		c     Candidate
		kinds []OccupancyKind
	}{
		{"visita com o hóspede A dentro", visita("AP-01", janela("2026-11-20", "10:00", "10:30")), []OccupancyKind{ByStay}},
		{"visita durante a limpeza", visita("AP-01", janela("2026-11-20", "12:00", "12:30")), []OccupancyKind{OccupancyKind(Cleaning)}},
		{"visita encostada na limpeza, antes de B entrar", visita("AP-01", janela("2026-11-20", "11:00", "11:00")), nil},
		{"visita que atravessa a limpeza e a entrada de B", visita("AP-01", janela("2026-11-20", "13:30", "14:30")), []OccupancyKind{OccupancyKind(Cleaning), ByStay}},
		{"visita no AP-02 em manutenção", visita("AP-02", janela("2026-11-21", "09:00", "10:00")), []OccupancyKind{OccupancyKind(Maintenance)}},
		{"visita no AP-02 no dia em que a manutenção acaba", visita("AP-02", janela("2026-11-22", "09:00", "10:00")), nil},
		{"visita no AP-03, livre", visita("AP-03", janela("2026-11-20", "12:00", "12:30")), nil},
		{"evento sem unidade", Candidate{Window: janela("2026-11-20", "12:00", "13:00")}, nil},
		{"a faxina do meio da estadia de B não colide com a estadia de B",
			Candidate{Reservation: "RB", Units: []string{"AP-01"}, Window: janela("2026-11-21", "10:00", "11:00")}, nil},
		{"mas colide com a estadia de A, se fosse no dia 18",
			Candidate{Reservation: "RB", Units: []string{"AP-01"}, Window: janela("2026-11-18", "10:00", "11:00")}, []OccupancyKind{ByStay}},
		{"a limpeza de A editada não colide consigo mesma",
			Candidate{Event: "limpeza-RA", Reservation: "RA", Units: []string{"AP-01"}, Window: janela("2026-11-20", "11:00", "13:30")}, nil},
		{"evento da Completa sem unidade gravada ocupa as unidades da reserva",
			Candidate{Reservation: "RC", Units: []string{"AP-01", "AP-02"}, Window: janela("2026-11-21", "18:00", "23:00")},
			[]OccupancyKind{ByStay, OccupancyKind(Maintenance)}},
	}
	for _, c := range casos {
		got := UnitConflicts(c.c, occ)
		if len(got) != len(c.kinds) {
			t.Errorf("%s: %d conflitos (%+v), esperava %v", c.nome, len(got), got, c.kinds)
			continue
		}
		vistos := map[OccupancyKind]bool{}
		for _, o := range got {
			vistos[o.Kind] = true
		}
		for _, k := range c.kinds {
			if !vistos[k] {
				t.Errorf("%s: faltou %s em %+v", c.nome, k, got)
			}
		}
	}
}

// A estadia lida pelos horários GRAVADOS: o hóspede que combinou sair às 13:00
// está no apartamento até as 13:00.
func TestEstadiaUsaOHorarioCombinado(t *testing.T) {
	saida := em("2026-11-20", "13:00")
	occ := []Occupancy{StayOccupancy("AP-01", "RA", dia("2026-11-17"), dia("2026-11-20"), nil, &saida, provisorios(), fz)}
	if len(UnitConflicts(visita("AP-01", janela("2026-11-20", "12:00", "12:30")), occ)) != 1 {
		t.Fatal("visita às 12:00 com o hóspede ficando até as 13:00 passou")
	}
}

func TestCheckUnitDetalhaSemIdentificar(t *testing.T) {
	err := CheckUnit(visita("AP-01", janela("2026-11-20", "12:00", "12:30")), ocupacaoDoDia20(), fz)
	re := regra(t, err)
	if re.Code != codeUnitNotAvailable {
		t.Fatalf("code %s", re.Code)
	}
	lista, ok := re.Details["conflicts"].([]map[string]any)
	if !ok || len(lista) != 1 {
		t.Fatalf("details.conflicts: %#v", re.Details)
	}
	c := lista[0]
	if c["unit_id"] != "AP-01" || c["kind"] != "limpeza" ||
		c["starts_at"] != "2026-11-20T11:00:00-03:00" || c["ends_at"] != "2026-11-20T14:00:00-03:00" {
		t.Fatalf("conflito: %#v", c)
	}
	for _, proibido := range []string{"event", "reservation", "title", "contact_id", "contact_name"} {
		if _, ok := c[proibido]; ok {
			t.Errorf("o conflito expõe %q — o ator pode não ter escopo para ver o evento alheio", proibido)
		}
	}
	if CheckUnit(visita("AP-03", janela("2026-11-20", "12:00", "12:30")), ocupacaoDoDia20(), fz) != nil {
		t.Fatal("recusou unidade livre")
	}
}

// Propriedade: conflito de unidade é exatamente "mesma unidade, janelas que se
// cruzam, e não é o próprio evento nem a estadia da própria reserva". Janelas
// que só se encostam (half-open) nunca colidem. O controle conta conflitos e
// encostos, para nenhum dos dois lados passar por vacuidade.
func TestUnitConflictsPropriedade(t *testing.T) {
	rng := rand.New(rand.NewSource(77))
	unidades := []string{"AP-01", "AP-02", "AP-03"}
	base := em("2026-11-20", "00:00")
	var conflitos, encostos int
	for caso := 0; caso < 20000; caso++ {
		ini := base.Add(time.Duration(rng.Intn(48)*30) * time.Minute)
		c := Candidate{
			Event:       []string{"", "E1"}[rng.Intn(2)],
			Reservation: []string{"", "R1", "R2"}[rng.Intn(3)],
			Units:       []string{unidades[rng.Intn(3)]},
			Window:      Window{Start: ini, End: ini.Add(time.Duration(1+rng.Intn(8)) * 30 * time.Minute)},
		}
		var occ []Occupancy
		for k := 0; k < 4; k++ {
			oi := base.Add(time.Duration(rng.Intn(48)*30) * time.Minute)
			if rng.Intn(3) == 0 {
				oi = c.Window.End // encosta no fim
			}
			o := Occupancy{
				Kind:        []OccupancyKind{ByStay, OccupancyKind(Cleaning), OccupancyKind(Visit)}[rng.Intn(3)],
				Unit:        unidades[rng.Intn(3)],
				Window:      Window{Start: oi, End: oi.Add(time.Duration(1+rng.Intn(8)) * 30 * time.Minute)},
				Reservation: []string{"", "R1", "R2"}[rng.Intn(3)],
				Event:       []string{"", "E1", "E2"}[rng.Intn(3)],
			}
			occ = append(occ, o)
		}
		got := UnitConflicts(c, occ)
		esperado := 0
		for _, o := range occ {
			if o.Window.Start.Equal(c.Window.End) || o.Window.End.Equal(c.Window.Start) {
				encostos++
			}
			mesma := o.Unit == c.Units[0]
			cruza := o.Window.Start.Before(c.Window.End) && c.Window.Start.Before(o.Window.End)
			proprio := c.Event != "" && o.Event == c.Event
			estadiaPropria := o.Kind == ByStay && c.Reservation != "" && o.Reservation == c.Reservation
			if mesma && cruza && !proprio && !estadiaPropria {
				esperado++
			}
		}
		if len(got) != esperado {
			t.Fatalf("caso %d: %d conflitos, esperava %d (%+v, %+v)", caso, len(got), esperado, c, occ)
		}
		conflitos += esperado
	}
	if conflitos < 2000 || encostos < 2000 {
		t.Fatalf("amostra fraca: %d conflitos, %d encostos", conflitos, encostos)
	}
}

// ─────────────────────────── Avisos ─────────────────────────────────

func TestTimeWarningsMesa(t *testing.T) {
	s := provisorios()
	treinamento := []Busy{{Ref: "B1", Window: janela("2026-11-20", "15:00", "16:00")}}
	agendaDaMaria := []Busy{
		{Ref: "E9", Window: janela("2026-11-20", "09:00", "10:00")},
		{Ref: "E1", Window: janela("2026-11-20", "17:00", "18:00")}, // o próprio, na edição
	}
	casos := []struct {
		nome  string
		c     Candidate
		kinds []WarningKind
	}{
		{"dentro do horário, sem nada", Candidate{Window: janela("2026-11-20", "11:00", "12:00")}, nil},
		{"antes de abrir", Candidate{Window: janela("2026-11-20", "07:30", "08:30")}, []WarningKind{OutsideHours}},
		{"terminando depois de fechar", Candidate{Window: janela("2026-11-20", "19:30", "20:30")}, []WarningKind{OutsideHours}},
		{"encostado no fechamento", Candidate{Window: janela("2026-11-20", "19:30", "20:00")}, nil},
		{"dia inteiro não tem hora", Candidate{AllDay: true, Window: Window{Start: em("2026-11-20", "00:00"), End: em("2026-11-21", "00:00")}}, []WarningKind{OperationBlocked}},
		{"no treinamento da equipe", Candidate{Window: janela("2026-11-20", "15:30", "16:30")}, []WarningKind{OperationBlocked}},
		{"Maria sem folga depois da visita das 9", Candidate{Assignee: "maria", Window: janela("2026-11-20", "10:10", "11:00")}, []WarningKind{AssigneeBusy}},
		{"Maria com folga", Candidate{Assignee: "maria", Window: janela("2026-11-20", "10:15", "11:00")}, nil},
		{"o próprio evento da Maria não conta", Candidate{Event: "E1", Assignee: "maria", Window: janela("2026-11-20", "17:00", "18:30")}, nil},
		{"sem responsável não há folga", Candidate{Window: janela("2026-11-20", "09:30", "10:30")}, nil},
	}
	for _, c := range casos {
		got := TimeWarnings(c.c, s, fz, treinamento, agendaDaMaria)
		if len(got) != len(c.kinds) {
			t.Errorf("%s: %+v, esperava %v", c.nome, got, c.kinds)
			continue
		}
		for i, k := range c.kinds {
			if got[i].Kind != k {
				t.Errorf("%s: aviso %d é %s, esperava %s", c.nome, i, got[i].Kind, k)
			}
		}
	}
}

// Dia fechado: domingo sem intervalo é fora do horário a qualquer hora.
func TestDomingoFechado(t *testing.T) {
	s := provisorios()
	s.BusinessHours = s.BusinessHours[:6] // segunda a sábado
	got := TimeWarnings(Candidate{Window: janela("2026-11-15", "10:00", "11:00")}, s, fz, nil, nil)
	if len(got) != 1 || got[0].Kind != OutsideHours {
		t.Fatalf("domingo fechado: %+v", got)
	}
}

// Aviso nunca é recusa: a mesma janela com conflito de horário E de unidade
// recebe os avisos e a recusa — são perguntas independentes.
func TestAvisoNaoSubstituiRecusa(t *testing.T) {
	c := visita("AP-01", janela("2026-11-20", "12:00", "12:30"))
	if CheckUnit(c, ocupacaoDoDia20(), fz) == nil {
		t.Fatal("conflito de unidade passou")
	}
	c.Window = janela("2026-11-20", "07:00", "07:30")
	if len(TimeWarnings(c, provisorios(), fz, nil, nil)) != 1 {
		t.Fatal("aviso de horário sumiu")
	}
}
