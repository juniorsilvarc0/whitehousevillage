package agenda

import (
	"math/rand"
	"testing"
	"time"
)

// ─────────────────────────── Ações ──────────────────────────────────

// A máquina inteira, linha por linha.
func TestNextMesa(t *testing.T) {
	amanha := hoje.AddDays(1)
	casos := []struct {
		nome   string
		f      Facts
		a      Action
		para   Status
		code   string
		reason Reason
	}{
		{"visita de hoje conclui", Facts{Visit, ByHand, Scheduled, hoje}, Complete, Done, "", ""},
		{"visita de amanhã não conclui", Facts{Visit, ByHand, Scheduled, amanha}, Complete, "", codeInvalidTransition, ReasonNotStarted},
		{"visita de amanhã desmarca", Facts{Visit, ByHand, Scheduled, amanha}, Cancel, Cancelled, "", ""},
		{"evento conclui", Facts{Event, ByHand, Scheduled, hoje.AddDays(-2)}, Complete, Done, "", ""},
		{"faxina avulsa desmarca", Facts{Cleaning, ByHand, Scheduled, hoje}, Cancel, Cancelled, "", ""},
		{"limpeza da reserva conclui", Facts{Cleaning, FromReservation, Scheduled, hoje}, Complete, Done, "", ""},
		{"limpeza da reserva não desmarca", Facts{Cleaning, FromReservation, Scheduled, hoje}, Cancel, "", codeInvalidTransition, ReasonFollowsReservation},
		{"check-in da reserva não conclui por aqui", Facts{CheckIn, FromReservation, Scheduled, hoje}, Complete, "", codeInvalidTransition, ReasonFollowsReservation},
		{"check-out da reserva não desmarca", Facts{CheckOut, FromReservation, Scheduled, hoje}, Cancel, "", codeInvalidTransition, ReasonFollowsReservation},
		{"concluído não reabre", Facts{Visit, ByHand, Done, hoje}, Cancel, "", codeEventClosed, ""},
		{"cancelado não conclui", Facts{Visit, ByHand, Cancelled, hoje}, Complete, "", codeEventClosed, ""},
	}
	for _, c := range casos {
		got, err := Next(c.f, c.a, hoje)
		if c.code == "" {
			if err != nil || got != c.para {
				t.Errorf("%s: (%s, %v), esperava %s", c.nome, got, err, c.para)
			}
			continue
		}
		re := regra(t, err)
		if re.Code != c.code {
			t.Errorf("%s: code %s, esperava %s", c.nome, re.Code, c.code)
		}
		if c.reason != "" && re.Details["reason"] != string(c.reason) {
			t.Errorf("%s: reason %v, esperava %s", c.nome, re.Details["reason"], c.reason)
		}
	}
}

// AllowedActions e Next são a mesma máquina: o que a tela oferece é exatamente
// o que a API aceita, em todo estado e todo dia.
func TestAcoesPermitidasBatemComNext(t *testing.T) {
	for _, tp := range []Type{CheckIn, CheckOut, Cleaning, Visit, Event} {
		for _, o := range []Origin{FromReservation, ByHand} {
			if (o == FromReservation && !tp.Automatic()) || (o == ByHand && !tp.Manual()) {
				continue
			}
			for _, s := range []Status{Scheduled, Done, Cancelled} {
				for _, dd := range []int{-1, 0, 1} {
					f := Facts{tp, o, s, hoje.AddDays(dd)}
					oferecidas := map[Action]bool{}
					for _, a := range AllowedActions(f, hoje) {
						oferecidas[a] = true
					}
					for _, a := range []Action{Complete, Cancel} {
						_, err := Next(f, a, hoje)
						if (err == nil) != oferecidas[a] {
							t.Errorf("%+v %s: Next aceita=%v, tela oferece=%v", f, a, err == nil, oferecidas[a])
						}
					}
				}
			}
		}
	}
}

func TestVocabularioDesconhecidoNaoViraRegra(t *testing.T) {
	for _, f := range []Facts{
		{Maintenance, ByHand, Scheduled, hoje}, // derivado nunca é gravado
		{Visit, FromMaintenance, Scheduled, hoje},
		{Visit, ByHand, "pendente", hoje},
	} {
		if _, err := Next(f, Complete, hoje); err == nil {
			t.Errorf("%+v passou", f)
		} else if _, regra := err.(*RuleError); regra {
			t.Errorf("%+v virou regra de negócio (409) em vez de defeito (500)", f)
		}
	}
	if _, err := Next(Facts{Visit, ByHand, Scheduled, hoje}, "reabrir", hoje); err == nil {
		t.Error("ação desconhecida passou")
	}
}

// ─────────────────────────── Edição ─────────────────────────────────

func TestEditableIn(t *testing.T) {
	casos := map[Editable]Facts{
		EditAll:                {Visit, ByHand, Scheduled, hoje},
		EditFollowsReservation: {CheckOut, FromReservation, Scheduled, hoje},
		EditNotesOnly:          {Cleaning, FromReservation, Done, hoje},
		EditNone:               {Visit, ByHand, Cancelled, hoje},
	}
	for want, f := range casos {
		if got := EditableIn(f); got != want {
			t.Errorf("%+v: %s, esperava %s", f, got, want)
		}
	}
}

func TestCheckEditMesa(t *testing.T) {
	data := dia("2026-11-20")
	limites := AutoBounds{CheckoutEnd: em("2026-11-20", "11:00")}
	w := func(de, ate string) *Window { j := janela("2026-11-20", de, ate); return &j }
	casos := []struct {
		nome   string
		f      Facts
		e      Edit
		code   string
		campos []string
	}{
		{"corpo vazio em cancelado", Facts{Visit, ByHand, Cancelled, data}, Edit{}, "", nil},
		{"cancelado não aceita nota", Facts{Visit, ByHand, Cancelled, data}, Edit{Notes: true}, codeEventClosed, nil},
		{"concluído aceita nota", Facts{Cleaning, FromReservation, Done, data}, Edit{Notes: true}, "", nil},
		{"concluído não aceita horário", Facts{Cleaning, FromReservation, Done, data}, Edit{Window: w("12:00", "15:00")}, codeEventClosed, nil},
		{"concluído não troca responsável", Facts{Visit, ByHand, Done, data}, Edit{Assignee: true}, codeEventClosed, nil},
		{"manual muda tudo", Facts{Visit, ByHand, Scheduled, data}, Edit{Identity: []string{"type", "unit_id"}, Window: w("09:00", "10:00"), Assignee: true}, "", nil},
		{"automático não muda de tipo nem de reserva", Facts{CheckIn, FromReservation, Scheduled, data},
			Edit{Identity: []string{"type", "reservation_id"}}, codeValidation, []string{"type", "reservation_id"}},
		{"check-in muda de hora", Facts{CheckIn, FromReservation, Scheduled, data}, Edit{Window: w("12:00", "12:30"), Assignee: true}, "", nil},
		{"check-in não muda de dia", Facts{CheckIn, FromReservation, Scheduled, data},
			Edit{Window: &Window{Start: em("2026-11-19", "22:00"), End: em("2026-11-19", "22:30")}}, codeValidation, []string{"starts_at"}},
		{"check-out não atravessa a meia-noite", Facts{CheckOut, FromReservation, Scheduled, data},
			Edit{Window: &Window{Start: em("2026-11-20", "23:30"), End: em("2026-11-21", "00:30")}}, codeValidation, []string{"ends_at"}},
		{"limpeza para depois do check-out", Facts{Cleaning, FromReservation, Scheduled, data}, Edit{Window: w("13:00", "16:00")}, "", nil},
		{"limpeza para amanhã (a equipe só vem amanhã)", Facts{Cleaning, FromReservation, Scheduled, data},
			Edit{Window: &Window{Start: em("2026-11-21", "08:00"), End: em("2026-11-21", "11:00")}}, "", nil},
		{"limpeza antes do hóspede sair", Facts{Cleaning, FromReservation, Scheduled, data}, Edit{Window: w("10:00", "13:00")}, codeValidation, []string{"starts_at"}},
	}
	for _, c := range casos {
		err := CheckEdit(c.f, c.e, limites, fz)
		if c.code == "" {
			if err != nil {
				t.Errorf("%s: recusou — %v", c.nome, err)
			}
			continue
		}
		re := regra(t, err)
		if re.Code != c.code {
			t.Errorf("%s: code %s, esperava %s", c.nome, re.Code, c.code)
		}
		for _, campo := range c.campos {
			if _, ok := re.Details[campo]; !ok {
				t.Errorf("%s: faltou %q em %v", c.nome, campo, re.Details)
			}
		}
		if c.code == codeEventClosed && re.Details["editable"] != string(EditableIn(c.f)) {
			t.Errorf("%s: details.editable %v", c.nome, re.Details["editable"])
		}
	}
}

// ─────────────────────────── Janela ─────────────────────────────────

func TestCheckWindowMesa(t *testing.T) {
	diaInteiro := Window{Start: em("2026-11-20", "00:00"), End: em("2026-11-22", "00:00")}
	meiaNoiteDoNavegador := Window{ // 00:00 em UTC é 21:00 na casa
		Start: time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC),
	}
	casos := []struct {
		nome   string
		w      Window
		allDay bool
		max    int
		campos []string
	}{
		{"visita de meia hora", janela("2026-11-20", "10:00", "10:30"), false, MaxEventDays, nil},
		{"dia inteiro na meia-noite da casa", diaInteiro, true, MaxEventDays, nil},
		{"dia inteiro na meia-noite UTC", meiaNoiteDoNavegador, true, MaxEventDays, []string{"starts_at", "ends_at"}},
		{"fim igual ao início", janela("2026-11-20", "10:00", "10:00"), false, MaxEventDays, []string{"ends_at"}},
		{"fim antes do início", janela("2026-11-20", "10:00", "09:00"), false, MaxEventDays, []string{"ends_at"}},
		{"evento de 32 dias", Window{Start: em("2026-11-20", "00:00"), End: em("2026-12-22", "00:00")}, true, MaxEventDays, []string{"ends_at"}},
		{"bloqueio de 32 dias", Window{Start: em("2026-11-20", "00:00"), End: em("2026-12-22", "00:00")}, true, MaxOperationBlockDays, nil},
		{"visita de ontem", janela("2026-11-14", "10:00", "11:00"), false, MaxEventDays, nil},
		{"dois anos atrás", janela("2024-11-14", "10:00", "11:00"), false, MaxEventDays, []string{"starts_at"}},
		{"dez anos à frente", janela("2036-11-14", "10:00", "11:00"), false, MaxEventDays, []string{"starts_at"}},
	}
	for _, c := range casos {
		err := CheckWindow(c.w, c.allDay, fz, hoje, c.max)
		if len(c.campos) == 0 {
			if err != nil {
				t.Errorf("%s: recusou — %v", c.nome, err)
			}
			continue
		}
		re := regra(t, err)
		for _, campo := range c.campos {
			if _, ok := re.Details[campo]; !ok {
				t.Errorf("%s: faltou %q em %v", c.nome, campo, re.Details)
			}
		}
	}
}

// ─────────────────────────── Responsável ────────────────────────────

func TestResolveAssigneeMesa(t *testing.T) {
	eu, outro := "corretor-a", "corretor-b"
	valor := func(s string) Field[string] { return Field[string]{Present: true, Value: &s} }
	nulo := Field[string]{Present: true}
	ausente := Field[string]{}
	atual := &eu

	casos := []struct {
		nome   string
		w      AssigneeWrite[string]
		final  *string
		recusa bool
	}{
		{"all: POST sem responsável", AssigneeWrite[string]{Create, ScopeAll, eu, nil, ausente}, nil, false},
		{"all: POST para outro", AssigneeWrite[string]{Create, ScopeAll, eu, nil, valor(outro)}, &outro, false},
		{"all: PATCH ausente não muda", AssigneeWrite[string]{Patch, ScopeAll, eu, &outro, ausente}, &outro, false},
		{"all: PATCH null limpa", AssigneeWrite[string]{Patch, ScopeAll, eu, &outro, nulo}, nil, false},
		{"all: PUT ausente limpa", AssigneeWrite[string]{Replace, ScopeAll, eu, &outro, ausente}, nil, false},
		{"own: POST ausente sou eu", AssigneeWrite[string]{Create, ScopeOwn, eu, nil, ausente}, &eu, false},
		{"own: POST eu mesmo", AssigneeWrite[string]{Create, ScopeOwn, eu, nil, valor(eu)}, &eu, false},
		{"own: POST para o colega", AssigneeWrite[string]{Create, ScopeOwn, eu, nil, valor(outro)}, nil, true},
		{"own: POST para ninguém", AssigneeWrite[string]{Create, ScopeOwn, eu, nil, nulo}, nil, true},
		{"own: PUT ausente sou eu", AssigneeWrite[string]{Replace, ScopeOwn, eu, atual, ausente}, &eu, false},
		{"own: PATCH ausente continua eu", AssigneeWrite[string]{Patch, ScopeOwn, eu, atual, ausente}, &eu, false},
		{"own: PATCH passa para o colega", AssigneeWrite[string]{Patch, ScopeOwn, eu, atual, valor(outro)}, nil, true},
		{"own: PATCH me tira", AssigneeWrite[string]{Patch, ScopeOwn, eu, atual, nulo}, nil, true},
	}
	for _, c := range casos {
		got, err := ResolveAssignee(c.w)
		if c.recusa {
			re := regra(t, err)
			if re.Code != codeForbidden || re.Details["reason"] != string(ReasonAssigneeNotActor) {
				t.Errorf("%s: %+v", c.nome, re)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.nome, err)
			continue
		}
		if (got == nil) != (c.final == nil) || (got != nil && *got != *c.final) {
			t.Errorf("%s: %v, esperava %v", c.nome, got, c.final)
		}
	}
	if _, err := ResolveAssignee(AssigneeWrite[string]{Create, "dono", eu, nil, ausente}); err == nil {
		t.Error("escopo desconhecido passou — tem de falhar fechado")
	}
}

// Propriedade: em `own`, o que ResolveAssignee aceita é SEMPRE o próprio ator
// — é a frase "own na agenda é assignee_id = ator" virando garantia.
func TestOwnSoGravaOProprioAtor(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	pessoas := []string{"a", "b", "c"}
	var aceitos, recusados int
	for i := 0; i < 20000; i++ {
		w := AssigneeWrite[string]{
			Write: []Write{Create, Replace, Patch}[rng.Intn(3)],
			Scope: ScopeOwn,
			Actor: pessoas[rng.Intn(3)],
		}
		if rng.Intn(2) == 0 {
			p := pessoas[rng.Intn(3)]
			w.Current = &p
		}
		switch rng.Intn(3) {
		case 0:
			p := pessoas[rng.Intn(3)]
			w.Requested = Field[string]{Present: true, Value: &p}
		case 1:
			w.Requested = Field[string]{Present: true}
		}
		got, err := ResolveAssignee(w)
		if err != nil {
			recusados++
			continue
		}
		aceitos++
		if got == nil || *got != w.Actor {
			t.Fatalf("caso %d: own gravou %v para o ator %s", i, got, w.Actor)
		}
	}
	if aceitos < 2000 || recusados < 2000 {
		t.Fatalf("amostra fraca: %d aceitos, %d recusados", aceitos, recusados)
	}
}
