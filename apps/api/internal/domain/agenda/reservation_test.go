package agenda

import (
	"math/rand"
	"testing"
	"time"
)

// ─────────────────────────── Plan ───────────────────────────────────

// A prova do F2-20, em mesa: estadia de 20 a 23/11 com os horários
// provisórios — check-in dia 20 às 14:00, check-out e limpeza dia 23.
func TestPlanMesa(t *testing.T) {
	got, err := Plan(Stay{CheckIn: dia("2026-11-20"), CheckOut: dia("2026-11-23")}, provisorios(), fz)
	if err != nil {
		t.Fatal(err)
	}
	want := []Planned{
		{Type: CheckIn, Window: janela("2026-11-20", "14:00", "14:30"), Status: Scheduled},
		{Type: CheckOut, Window: janela("2026-11-23", "10:30", "11:00"), Status: Scheduled},
		{Type: Cleaning, Window: janela("2026-11-23", "11:00", "14:00"), Status: Scheduled},
	}
	if len(got) != len(want) {
		t.Fatalf("esperava %d eventos, veio %d", len(want), len(got))
	}
	for i := range want {
		if got[i].Type != want[i].Type || !got[i].Window.Equal(want[i].Window) ||
			got[i].Status != want[i].Status || got[i].DoneAt != nil {
			t.Errorf("evento %d: veio %+v, esperava %+v", i, got[i], want[i])
		}
	}
	// 14:00 na casa é 17:00 em UTC: o instante gravado é o da casa, não o do
	// servidor.
	if h := got[0].Window.Start.UTC().Hour(); h != 17 {
		t.Errorf("check-in gravado às %dh UTC — o fuso da casa não foi aplicado", h)
	}
}

// Réveillon: entrada em 2026, saída em 2027 — cada evento no dia civil dele.
func TestPlanAtravessaOAno(t *testing.T) {
	got, err := Plan(Stay{CheckIn: dia("2026-12-28"), CheckOut: dia("2027-01-02")}, provisorios(), fz)
	if err != nil {
		t.Fatal(err)
	}
	if fz.DateOf(got[0].Window.Start).String() != "2026-12-28" ||
		fz.DateOf(got[1].Window.Start).String() != "2027-01-02" ||
		fz.DateOf(got[2].Window.Start).String() != "2027-01-02" {
		t.Fatalf("dias errados: %v %v %v", got[0].Window, got[1].Window, got[2].Window)
	}
}

// A reserva já em curso quando a agenda nasce (o preenchimento): o check-in
// aconteceu, e nasce concluído com o instante dele.
func TestPlanDeReservaEmCurso(t *testing.T) {
	entrou := em("2026-11-14", "15:12")
	got, err := Plan(Stay{CheckIn: dia("2026-11-14"), CheckOut: dia("2026-11-17"), CheckedInAt: &entrou}, provisorios(), fz)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Status != Done || got[0].DoneAt == nil || !got[0].DoneAt.Equal(entrou) {
		t.Fatalf("check-in de reserva em curso: %+v", got[0])
	}
	if got[1].Status != Scheduled || got[2].Status != Scheduled {
		t.Fatalf("check-out e limpeza deveriam seguir agendados: %+v %+v", got[1], got[2])
	}
}

func TestPlanRecusaEntradaQuebrada(t *testing.T) {
	if _, err := Plan(Stay{CheckIn: dia("2026-11-20"), CheckOut: dia("2026-11-20")}, provisorios(), fz); err == nil {
		t.Error("estadia sem noite virou agenda")
	}
	ruim := provisorios()
	ruim.CleaningStart = d("09:00")
	if _, err := Plan(Stay{CheckIn: dia("2026-11-20"), CheckOut: dia("2026-11-21")}, ruim, fz); err == nil {
		t.Error("configuração incoerente virou agenda")
	}
}

// Propriedade da agenda automática, sobre estadias encadeadas sorteadas na
// mesma unidade (back-to-back incluído):
//
//  1. cada estadia produz exatamente um check-in, um check-out e uma limpeza;
//  2. o check-in é no dia da entrada; check-out e limpeza no da saída;
//  3. nenhum evento automático de estadias DIFERENTES colide na unidade, nem
//     com a estadia da outra — o dia de troca é a regra, não a exceção;
//
// e o controle conta quantas trocas no mesmo dia a amostra teve.
func TestAgendaAutomaticaNaoColideNaMesmaUnidade(t *testing.T) {
	rng := rand.New(rand.NewSource(2011))
	s := provisorios()
	trocas := 0
	for caso := 0; caso < 2000; caso++ {
		var occ []Occupancy
		var cands []Candidate
		inicio := dia("2026-01-01").AddDays(rng.Intn(600))
		for k := 0; k < 6; k++ {
			saida := inicio.AddDays(1 + rng.Intn(6))
			res := "R" + string(rune('A'+k))
			evs, err := Plan(Stay{CheckIn: inicio, CheckOut: saida}, s, fz)
			if err != nil {
				t.Fatal(err)
			}
			tipos := map[Type]int{}
			for _, e := range evs {
				tipos[e.Type]++
				occ = append(occ, EventOccupancy(e.Type, "AP-01", res+string(e.Type), res, e.Window))
				cands = append(cands, Candidate{Event: res + string(e.Type), Reservation: res, Units: []string{"AP-01"}, Window: e.Window})
			}
			if tipos[CheckIn] != 1 || tipos[CheckOut] != 1 || tipos[Cleaning] != 1 {
				t.Fatalf("caso %d: tipos %v", caso, tipos)
			}
			if fz.DateOf(evs[0].Window.Start) != inicio || fz.DateOf(evs[1].Window.Start) != saida ||
				fz.DateOf(evs[2].Window.Start) != saida {
				t.Fatalf("caso %d: dias errados", caso)
			}
			occ = append(occ, StayOccupancy("AP-01", res, inicio, saida, &evs[0].Window.Start, &evs[1].Window.End, s, fz))
			if rng.Intn(2) == 0 {
				inicio = saida // troca no mesmo dia
				trocas++
			} else {
				inicio = saida.AddDays(1 + rng.Intn(3))
			}
		}
		for _, c := range cands {
			if cs := UnitConflicts(c, occ); len(cs) > 0 {
				t.Fatalf("caso %d: %s colide com %+v", caso, c.Event, cs)
			}
		}
	}
	if trocas < 2000 {
		t.Fatalf("amostra fraca: %d trocas no mesmo dia", trocas)
	}
}

// ─────────────────────────── Ciclo de vida ──────────────────────────

func automaticos(entrada, saida string) []Linked {
	evs, _ := Plan(Stay{CheckIn: dia(entrada), CheckOut: dia(saida)}, provisorios(), fz)
	out := make([]Linked, len(evs))
	for i, e := range evs {
		out[i] = Linked{Type: e.Type, Origin: FromReservation, Status: e.Status, Window: e.Window}
	}
	return out
}

func TestOnCheckInConcluiSoOCheckIn(t *testing.T) {
	evs := automaticos("2026-11-15", "2026-11-18")
	evs = append(evs, Linked{Type: Event, Origin: ByHand, Status: Scheduled, Window: janela("2026-11-16", "18:00", "22:00")})
	chegou := em("2026-11-15", "19:05")
	ds := OnCheckIn(evs, chegou)
	if ds[0].Effect != CompleteIt || ds[0].DoneAt == nil || !ds[0].DoneAt.Equal(chegou) || !ds[0].Window.Equal(evs[0].Window) {
		t.Fatalf("check-in: %+v", ds[0])
	}
	for i := 1; i < len(ds); i++ {
		if ds[i].Effect != Keep {
			t.Errorf("evento %d mexido no check-in: %+v", i, ds[i])
		}
	}
}

// Saída no dia contratado: conclui o check-out, a limpeza fica onde estava.
func TestOnCheckOutNoDia(t *testing.T) {
	evs := automaticos("2026-11-12", "2026-11-15")
	evs[0].Status = Done
	saiu := em("2026-11-15", "10:40")
	ds := OnCheckOut(evs, saiu, Stay{CheckIn: dia("2026-11-12"), CheckOut: dia("2026-11-15")}, provisorios(), fz)
	if ds[0].Effect != Keep {
		t.Errorf("check-in concluído mexido: %+v", ds[0])
	}
	if ds[1].Effect != CompleteIt || !ds[1].Window.Equal(evs[1].Window) || !ds[1].DoneAt.Equal(saiu) {
		t.Errorf("check-out: %+v", ds[1])
	}
	if ds[2].Effect != Keep {
		t.Errorf("limpeza mexida numa saída no dia: %+v", ds[2])
	}
}

// Saída ANTECIPADA: o hóspede de 12 a 18 sai no dia 15. As noites de 15 a 17
// voltam à venda hoje — a limpeza vem para hoje, e o check-out é desenhado
// hoje.
func TestOnCheckOutAntecipado(t *testing.T) {
	evs := automaticos("2026-11-12", "2026-11-18")
	evs[0].Status = Done
	faxina := Linked{Type: Cleaning, Origin: ByHand, Status: Scheduled, Window: janela("2026-11-19", "09:00", "12:00")}
	evs = append(evs, faxina)
	saiu := em("2026-11-15", "09:20")
	ds := OnCheckOut(evs, saiu, Stay{CheckIn: dia("2026-11-12"), CheckOut: dia("2026-11-18")}, provisorios(), fz)

	if ds[1].Effect != CompleteIt || !ds[1].Window.Equal(janela("2026-11-15", "10:30", "11:00")) {
		t.Errorf("check-out antecipado: %+v", ds[1])
	}
	if ds[2].Effect != MoveIt || !ds[2].Window.Equal(janela("2026-11-15", "11:00", "14:00")) {
		t.Errorf("limpeza deveria vir para o dia da saída: %+v", ds[2])
	}
	if ds[3].Effect != Keep {
		t.Errorf("a faxina pós-estadia marcada à mão foi mexida: %+v", ds[3])
	}
}

// Cancelar sem o hóspede ter entrado: tudo que estava agendado é cancelado —
// inclusive o manual ligado e o check-in vencido; o concluído fica.
func TestOnCancelSemEntrada(t *testing.T) {
	evs := automaticos("2026-11-14", "2026-11-17") // check-in ontem, vencido
	evs = append(evs,
		Linked{Type: Event, Origin: ByHand, Status: Scheduled, Window: janela("2026-11-16", "18:00", "22:00")},
		Linked{Type: Visit, Origin: ByHand, Status: Done, Window: janela("2026-11-10", "10:00", "11:00")},
	)
	ds := OnCancel(evs, false, em("2026-11-15", "09:00"), provisorios(), fz)
	for i := 0; i < 4; i++ {
		if ds[i].Effect != CancelIt {
			t.Errorf("evento %d (%s) deveria ser cancelado: %+v", i, evs[i].Type, ds[i])
		}
	}
	if ds[4].Effect != Keep {
		t.Errorf("concluído mexido: %+v", ds[4])
	}
}

// Cancelar com o hóspede dentro, às 16:00 do dia 15, numa estadia de 12 a 18.
func TestOnCancelComOHospedeDentro(t *testing.T) {
	evs := automaticos("2026-11-12", "2026-11-18")
	evs[0].Status = Done
	evs = append(evs,
		Linked{Type: Cleaning, Origin: ByHand, Status: Scheduled, Window: janela("2026-11-15", "09:00", "10:00")}, // já começou
		Linked{Type: Event, Origin: ByHand, Status: Scheduled, Window: janela("2026-11-16", "18:00", "22:00")},    // ainda não
	)
	agora := em("2026-11-15", "16:00")
	ds := OnCancel(evs, true, agora, provisorios(), fz)
	if ds[0].Effect != Keep {
		t.Errorf("check-in concluído mexido: %+v", ds[0])
	}
	if ds[1].Effect != CancelIt {
		t.Errorf("check-out deveria ser cancelado (não houve saída registrada): %+v", ds[1])
	}
	if ds[2].Effect != MoveIt || !ds[2].Window.Equal(janela("2026-11-15", "11:00", "14:00")) {
		t.Errorf("limpeza deveria vir para hoje: %+v", ds[2])
	}
	if ds[3].Effect != Keep {
		t.Errorf("manual que já começou foi cancelado: %+v", ds[3])
	}
	if ds[4].Effect != CancelIt {
		t.Errorf("manual que não começou deveria ser cancelado: %+v", ds[4])
	}
}

// Com o hóspede dentro e a saída contratada para hoje, a limpeza já é hoje e
// fica.
func TestOnCancelComALimpezaJaHoje(t *testing.T) {
	evs := automaticos("2026-11-12", "2026-11-15")
	evs[0].Status = Done
	ds := OnCancel(evs, true, em("2026-11-15", "08:00"), provisorios(), fz)
	if ds[2].Effect != Keep {
		t.Fatalf("limpeza de hoje foi mexida: %+v", ds[2])
	}
}

// Propriedade do cancelamento, sobre eventos e instantes sorteados:
//
//  1. concluído e cancelado nunca mudam;
//  2. sem entrada, nenhum agendado sobrevive;
//  3. com entrada, nenhum check-in/check-out automático agendado sobrevive, a
//     limpeza automática nunca fica depois de hoje, e nenhum manual que
//     começa de agora em diante sobrevive.
func TestOnCancelPropriedades(t *testing.T) {
	rng := rand.New(rand.NewSource(1511))
	s := provisorios()
	tipos := []Type{CheckIn, CheckOut, Cleaning, Visit, Event, Cleaning}
	status := []Status{Scheduled, Scheduled, Done, Cancelled}
	var cancelados, movidos int
	for caso := 0; caso < 5000; caso++ {
		agora := em("2026-11-15", "00:00").Add(time.Duration(rng.Intn(48*60)) * time.Minute)
		hojeAqui := fz.DateOf(agora)
		var evs []Linked
		for k := 0; k < 8; k++ {
			tp := tipos[rng.Intn(len(tipos))]
			o := ByHand
			if tp.Automatic() && (tp != Cleaning || rng.Intn(2) == 0) {
				o = FromReservation
			}
			ini := em("2026-11-10", "00:00").Add(time.Duration(rng.Intn(12*24*60)) * time.Minute)
			evs = append(evs, Linked{Type: tp, Origin: o, Status: status[rng.Intn(len(status))],
				Window: Window{Start: ini, End: ini.Add(time.Duration(30+rng.Intn(300)) * time.Minute)}})
		}
		dentro := rng.Intn(2) == 0
		ds := OnCancel(evs, dentro, agora, s, fz)
		for i, e := range evs {
			dd := ds[i]
			if e.Status != Scheduled {
				if dd.Effect != Keep {
					t.Fatalf("caso %d: %s fechado mexido (%+v)", caso, e.Status, dd)
				}
				continue
			}
			sobrevive := dd.Effect == Keep || dd.Effect == MoveIt
			switch {
			case !dentro && sobrevive:
				t.Fatalf("caso %d: agendado sobreviveu a cancelamento sem entrada (%+v → %+v)", caso, e, dd)
			case dentro && e.Origin == FromReservation && e.Type != Cleaning && sobrevive:
				t.Fatalf("caso %d: %s automático sobreviveu (%+v)", caso, e.Type, dd)
			case dentro && e.Origin == FromReservation && e.Type == Cleaning && fz.DateOf(dd.Window.Start).After(hojeAqui):
				t.Fatalf("caso %d: limpeza ficou para depois de hoje (%+v)", caso, dd)
			case dentro && e.Origin == ByHand && !e.Window.Start.Before(agora) && sobrevive:
				t.Fatalf("caso %d: manual futuro sobreviveu (%+v)", caso, dd)
			}
			if dd.Effect == CancelIt {
				cancelados++
			}
			if dd.Effect == MoveIt {
				movidos++
			}
		}
	}
	if cancelados < 1000 || movidos < 100 {
		t.Fatalf("amostra fraca: %d cancelados, %d movidos", cancelados, movidos)
	}
}

func TestMovesOnReassign(t *testing.T) {
	casos := []struct {
		l    Linked
		move bool
	}{
		{Linked{Type: Cleaning, Origin: ByHand, Status: Scheduled, UnitRef: "AP-01"}, true},
		{Linked{Type: Cleaning, Origin: ByHand, Status: Scheduled, UnitRef: "AP-02"}, false},
		{Linked{Type: Cleaning, Origin: ByHand, Status: Done, UnitRef: "AP-01"}, false},
		{Linked{Type: Event, Origin: ByHand, Status: Scheduled, UnitRef: ""}, false},
		{Linked{Type: Cleaning, Origin: FromReservation, Status: Scheduled, UnitRef: ""}, false},
	}
	for i, c := range casos {
		if got := MovesOnReassign(c.l, "AP-01"); got != c.move {
			t.Errorf("caso %d: %v, esperava %v", i, got, c.move)
		}
	}
}

// ─────────────────────────── Retime ─────────────────────────────────

// O dono confirma os horários: entrada às 15:00, saída às 12:00, limpeza das
// 12:00 às 15:00. O que estava no horário provisório muda; o que alguém
// ajustou, o passado e o concluído não.
func TestRetimeMesa(t *testing.T) {
	velho := provisorios()
	novo := provisorios()
	novo.CheckIn, novo.CheckOut, novo.CleaningStart, novo.CleaningEnd = d("15:00"), d("12:00"), d("12:00"), d("15:00")

	casos := []struct {
		nome  string
		l     Linked
		muda  bool
		final Window
	}{
		{"check-in futuro no provisório", Linked{Type: CheckIn, Origin: FromReservation, Status: Scheduled, Window: janela("2026-11-20", "14:00", "14:30")}, true, janela("2026-11-20", "15:00", "15:30")},
		{"check-out futuro no provisório", Linked{Type: CheckOut, Origin: FromReservation, Status: Scheduled, Window: janela("2026-11-20", "10:30", "11:00")}, true, janela("2026-11-20", "11:30", "12:00")},
		{"limpeza de hoje no provisório", Linked{Type: Cleaning, Origin: FromReservation, Status: Scheduled, Window: janela("2026-11-15", "11:00", "14:00")}, true, janela("2026-11-15", "12:00", "15:00")},
		{"check-out ajustado à mão", Linked{Type: CheckOut, Origin: FromReservation, Status: Scheduled, Window: janela("2026-11-20", "12:30", "13:00")}, false, janela("2026-11-20", "12:30", "13:00")},
		{"limpeza de ontem", Linked{Type: Cleaning, Origin: FromReservation, Status: Scheduled, Window: janela("2026-11-14", "11:00", "14:00")}, false, janela("2026-11-14", "11:00", "14:00")},
		{"check-in concluído", Linked{Type: CheckIn, Origin: FromReservation, Status: Done, Window: janela("2026-11-20", "14:00", "14:30")}, false, janela("2026-11-20", "14:00", "14:30")},
		{"limpeza manual no horário provisório", Linked{Type: Cleaning, Origin: ByHand, Status: Scheduled, Window: janela("2026-11-20", "11:00", "14:00")}, false, janela("2026-11-20", "11:00", "14:00")},
	}
	for _, c := range casos {
		got, mudou := Retime(c.l, velho, novo, fz, hoje)
		if mudou != c.muda || !got.Equal(c.final) {
			t.Errorf("%s: (%v, %v), esperava (%v, %v)", c.nome, got, mudou, c.final, c.muda)
		}
	}
}

// Propriedade da reprogramação, sobre configurações e eventos sorteados:
//
//  1. aplicar duas vezes é aplicar uma (de `novo` para `novo` nada muda);
//  2. o que muda passa a ser exatamente o horário de `novo` para aquele dia;
//  3. nada antes de hoje, nada manual, nada fechado muda.
func TestRetimePropriedades(t *testing.T) {
	rng := rand.New(rand.NewSource(14))
	var mudaram int
	for caso := 0; caso < 100000; caso++ {
		velho, novo := configuracaoSorteada(rng), configuracaoSorteada(rng)
		if velho.Validate() != nil || novo.Validate() != nil {
			continue
		}
		tp := []Type{CheckIn, CheckOut, Cleaning}[rng.Intn(3)]
		data := hoje.AddDays(rng.Intn(20) - 5)
		w, _ := AutoWindow(tp, data, velho, fz)
		if rng.Intn(4) == 0 {
			w.Start = w.Start.Add(15 * time.Minute) // ajustado à mão
		}
		l := Linked{Type: tp, Origin: []Origin{FromReservation, FromReservation, ByHand}[rng.Intn(3)],
			Status: []Status{Scheduled, Scheduled, Done}[rng.Intn(3)], Window: w}

		got, mudou := Retime(l, velho, novo, fz, hoje)
		de2, mudou2 := Retime(Linked{Type: l.Type, Origin: l.Origin, Status: l.Status, Window: got}, novo, novo, fz, hoje)
		if mudou2 || !de2.Equal(got) {
			t.Fatalf("caso %d: reaplicar mudou de novo", caso)
		}
		if !mudou {
			if !got.Equal(l.Window) {
				t.Fatalf("caso %d: disse que não mudou e mudou", caso)
			}
			continue
		}
		mudaram++
		if l.Origin != FromReservation || l.Status != Scheduled || data.Before(hoje) {
			t.Fatalf("caso %d: mexeu no que não podia (%+v)", caso, l)
		}
		if esperado, _ := AutoWindow(tp, data, novo, fz); !got.Equal(esperado) {
			t.Fatalf("caso %d: foi para %v, e o horário novo é %v", caso, got, esperado)
		}
	}
	if mudaram < 500 {
		t.Fatalf("amostra fraca: %d reprogramados", mudaram)
	}
}
