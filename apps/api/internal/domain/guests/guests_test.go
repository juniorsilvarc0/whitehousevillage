package guests

import (
	"errors"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// Os ids dos testes são inteiros: o domínio é genérico justamente para não
// depender de uuid, e inteiro deixa a tabela legível. 1 é sempre o titular.
const (
	titular = 1
	ana     = 2
	bruno   = 3
	carla   = 4
	davi    = 5
)

func achada(id int) Contact[int]      { return Contact[int]{ID: id, Found: true} }
func anonimizada(id int) Contact[int] { return Contact[int]{ID: id, Found: true, Anonymized: true} }
func deOutraCasa(id int) Contact[int] { return Contact[int]{ID: id} }
func ptrContato(c Contact[int]) *Contact[int] {
	return &c
}
func ptrInt(v int) *int { return &v }

// reserva monta a reserva viva padrão: confirmada, titular 1 na lista, seis
// vendidos num produto de dez.
func reserva(listed ...int) Reservation[int] {
	if len(listed) == 0 {
		listed = []int{titular}
	}
	return Reservation[int]{Status: Confirmed, Lead: titular, GuestsCount: 6, Capacity: 10, Listed: listed}
}

func com(r Reservation[int], mudar func(*Reservation[int])) Reservation[int] {
	mudar(&r)
	return r
}

// regra exige uma recusa de negócio com o code esperado e devolve-a.
func regra(t *testing.T, err error, code string) *RuleError {
	t.Helper()
	var re *RuleError
	if !errors.As(err, &re) {
		t.Fatalf("esperava *RuleError %s, veio %v", code, err)
	}
	if re.Code != code {
		t.Fatalf("code = %s, esperado %s (%s)", re.Code, code, re.Message)
	}
	return re
}

// recusa exige a recusa sem devolvê-la — para quando os details não importam.
func recusa(t *testing.T, err error, code string) {
	t.Helper()
	if re := regra(t, err, code); re == nil {
		t.Fatal("regra devolveu nil")
	}
}

func semErro(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("esperava aceitar, veio %v", err)
	}
}

// ─────────────────────────── Estado ─────────────────────────────────

// A lista se edita nos três estados em que a reserva está de pé, e só neles.
// A tabela cobre os nove do CHECK de reservations.status, um por um: um estado
// novo no banco sem linha aqui é exatamente o caso que tem de acender.
func TestEditavelSoNosTresEstadosDePe(t *testing.T) {
	esperado := map[ReservationStatus]bool{
		Quote: false, Hold: true, Confirmed: true, CheckedIn: true,
		CheckedOut: false, Closed: false, Cancelled: false, Expired: false, NoShow: false,
	}
	for s, quer := range esperado {
		if !s.Valid() {
			t.Errorf("%s deveria ser estado conhecido", s)
		}
		if Editable(s) != quer {
			t.Errorf("Editable(%s) = %v, esperado %v", s, Editable(s), quer)
		}
	}
	if ReservationStatus("arquivada").Valid() || Editable("arquivada") {
		t.Error("estado fora do vocabulário não pode ser válido nem editável")
	}
	if got := EditableStatuses(); !reflect.DeepEqual(got, []ReservationStatus{Hold, Confirmed, CheckedIn}) {
		t.Errorf("EditableStatuses = %v", got)
	}
	// A cópia é cópia: quem recebe a lista não muda a regra.
	EditableStatuses()[0] = Closed
	if !Editable(Hold) || Editable(Closed) {
		t.Error("mexer no retorno de EditableStatuses alterou a regra")
	}
}

// ─────────────────────────── Entrada ────────────────────────────────

func TestAdmitMesa(t *testing.T) {
	semErro(t, Admit(achada(ana)))

	re := regra(t, Admit(deOutraCasa(ana)), "VALIDATION_ERROR")
	if _, ok := re.Details["contact_id"].(string); !ok {
		t.Errorf("a recusa da ficha inexistente diz o campo: %v", re.Details)
	}

	re = regra(t, Admit(anonimizada(ana)), "CONTACT_ANONYMIZED")
	if re.Details["contact_id"] != ana {
		t.Errorf("details.contact_id = %v", re.Details["contact_id"])
	}

	// Ficha de outra casa que também está anonimizada: a resposta é a da ficha
	// inexistente. Dizer "anonimizada" ensinaria que o id existe noutra casa.
	recusa(t, Admit(Contact[int]{ID: ana, Anonymized: true}), "VALIDATION_ERROR")
}

// ─────────────────────────── Acrescentar ────────────────────────────

func TestCheckAddMesa(t *testing.T) {
	casos := []struct {
		nome    string
		r       Reservation[int]
		c       Contact[int]
		code    string // vazio = aceita
		detalhe map[string]any
	}{
		{nome: "acompanhante na reserva confirmada", r: reserva(), c: achada(ana)},
		{nome: "pré-reserva também aceita", r: com(reserva(), func(r *Reservation[int]) { r.Status = Hold }), c: achada(ana)},
		{nome: "com o hóspede dentro também aceita", r: com(reserva(), func(r *Reservation[int]) { r.Status = CheckedIn }), c: achada(ana)},
		{
			nome: "o último lugar ainda entra",
			r:    com(reserva(titular, ana, bruno), func(r *Reservation[int]) { r.GuestsCount = 4 }),
			c:    achada(carla),
		},
		{
			nome:    "lista cheia: seis vendidos, seis nomes",
			r:       reserva(titular, ana, bruno, carla, davi, 6),
			c:       achada(7),
			code:    "GUEST_LIST_FULL",
			detalhe: map[string]any{"guests_count": 6, "listed": 6, "capacity": 10},
		},
		{
			nome: "acompanhante repetido",
			r:    reserva(titular, ana), c: achada(ana),
			code: "CODE_IN_USE", detalhe: map[string]any{"contact_id": ana, "is_lead_guest": false},
		},
		{
			nome: "o titular como acompanhante",
			r:    reserva(), c: achada(titular),
			code: "CODE_IN_USE", detalhe: map[string]any{"contact_id": titular, "is_lead_guest": true},
		},
		{nome: "ficha de outra casa", r: reserva(), c: deOutraCasa(ana), code: "VALIDATION_ERROR"},
		{nome: "ficha anonimizada", r: reserva(), c: anonimizada(ana), code: "CONTACT_ANONYMIZED"},
		{
			nome: "reserva encerrada vem antes da pessoa",
			r:    com(reserva(), func(r *Reservation[int]) { r.Status = CheckedOut }), c: anonimizada(ana),
			code: "INVALID_STATE_TRANSITION",
		},
		{
			nome: "repetido vem antes de cheio",
			r:    com(reserva(titular, ana), func(r *Reservation[int]) { r.GuestsCount = 2 }), c: achada(ana),
			code: "CODE_IN_USE",
		},
		{
			nome: "anonimizado vem antes de cheio",
			r:    com(reserva(titular, ana), func(r *Reservation[int]) { r.GuestsCount = 2 }), c: anonimizada(bruno),
			code: "CONTACT_ANONYMIZED",
		},
	}
	for _, s := range []ReservationStatus{Quote, CheckedOut, Closed, Cancelled, Expired, NoShow} {
		casos = append(casos, struct {
			nome    string
			r       Reservation[int]
			c       Contact[int]
			code    string
			detalhe map[string]any
		}{
			nome: "estado " + string(s),
			r:    com(reserva(), func(r *Reservation[int]) { r.Status = s }), c: achada(ana),
			code:    "INVALID_STATE_TRANSITION",
			detalhe: map[string]any{"status": string(s), "allowed": []string{"hold", "confirmed", "checked_in"}},
		})
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			err := CheckAdd(c.r, c.c)
			if c.code == "" {
				semErro(t, err)
				return
			}
			re := regra(t, err, c.code)
			for k, v := range c.detalhe {
				if !reflect.DeepEqual(re.Details[k], v) {
					t.Errorf("details.%s = %#v, esperado %#v", k, re.Details[k], v)
				}
			}
		})
	}
}

// A frase da lista cheia diz o que fazer: aumentar o número — a não ser que o
// número já seja a capacidade do produto, e aí não há o que aumentar.
func TestListaCheiaDizSeDaParaAumentar(t *testing.T) {
	cheia := reserva(titular, ana, bruno, carla, davi, 6)
	re := regra(t, CheckAdd(cheia, achada(7)), "GUEST_LIST_FULL")
	if !strings.Contains(re.Message, "aumente") {
		t.Errorf("com folga na capacidade, a frase manda aumentar: %q", re.Message)
	}
	cheia.Capacity = 6
	re = regra(t, CheckAdd(cheia, achada(7)), "GUEST_LIST_FULL")
	if strings.Contains(re.Message, "aumente") {
		t.Errorf("sem folga na capacidade, a frase não pode mandar aumentar: %q", re.Message)
	}
}

func TestEntradaQueNenhumChamadorCorretoProduzEhDefeito(t *testing.T) {
	for nome, r := range map[string]Reservation[int]{
		"estado desconhecido": com(reserva(), func(r *Reservation[int]) { r.Status = "arquivada" }),
		"zero hóspedes":       com(reserva(), func(r *Reservation[int]) { r.GuestsCount = 0 }),
	} {
		t.Run(nome, func(t *testing.T) {
			for op, err := range map[string]error{
				"CheckAdd":    CheckAdd(r, achada(ana)),
				"CheckRemove": CheckRemove(r, ana),
				"PlanEdit": func() error {
					_, err := PlanEdit(r, Edit[int]{})
					return err
				}(),
			} {
				if !errors.Is(err, ErrInvalidInput) {
					t.Errorf("%s: esperava ErrInvalidInput, veio %v", op, err)
				}
			}
		})
	}
	if _, err := PlanEdit(reserva(), Edit[int]{GuestsCount: ptrInt(0)}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("guests_count pedido 0: esperava ErrInvalidInput, veio %v", err)
	}
	if _, err := Carry(titular, nil, 0); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("Carry com 0 hóspedes: esperava ErrInvalidInput, veio %v", err)
	}
}

// ─────────────────────────── Retirar ────────────────────────────────

func TestCheckRemoveMesa(t *testing.T) {
	semErro(t, CheckRemove(reserva(titular, ana), ana))
	semErro(t, CheckRemove(com(reserva(titular, ana), func(r *Reservation[int]) { r.Status = CheckedIn }), ana))

	re := regra(t, CheckRemove(reserva(titular, ana), titular), "RESOURCE_IN_USE")
	if re.Details["is_lead_guest"] != true || re.Details["contact_id"] != titular {
		t.Errorf("a recusa do titular diz quem e por quê: %v", re.Details)
	}
	recusa(t, CheckRemove(reserva(titular, ana), bruno), "NOT_FOUND")

	for _, s := range []ReservationStatus{Quote, CheckedOut, Closed, Cancelled, Expired, NoShow} {
		encerrada := com(reserva(titular, ana), func(r *Reservation[int]) { r.Status = s })
		recusa(t, CheckRemove(encerrada, ana), "INVALID_STATE_TRANSITION")
		// O estado vem antes: na reserva encerrada, nem o titular nem quem não
		// está na lista mudam a resposta.
		recusa(t, CheckRemove(encerrada, titular), "INVALID_STATE_TRANSITION")
		recusa(t, CheckRemove(encerrada, bruno), "INVALID_STATE_TRANSITION")
	}
}

// ─────────────────────────── Editar a reserva ───────────────────────

func TestPlanEditMesa(t *testing.T) {
	casos := []struct {
		nome string
		r    Reservation[int]
		e    Edit[int]
		quer Plan[int]
		code string
	}{
		{nome: "corpo sem titular nem número não toca a lista", r: reserva(titular, ana), quer: Plan[int]{Listed: 2}},
		{
			nome: "reenviar o titular de sempre na reserva encerrada passa",
			r:    com(reserva(titular, ana), func(r *Reservation[int]) { r.Status = CheckedOut }),
			e:    Edit[int]{Lead: ptrContato(achada(titular)), GuestsCount: ptrInt(6)},
			quer: Plan[int]{Listed: 2},
		},
		{
			nome: "reenviar o titular de sempre mesmo anonimizado passa: não é troca",
			r:    reserva(titular, ana),
			e:    Edit[int]{Lead: ptrContato(anonimizada(titular))},
			quer: Plan[int]{Listed: 2},
		},
		{
			nome: "troca por quem não estava na lista: sai um, entra um",
			r:    reserva(titular, ana),
			e:    Edit[int]{Lead: ptrContato(achada(bruno))},
			quer: Plan[int]{LeadChanged: true, Remove: ptrInt(titular), Listed: 2},
		},
		{
			nome: "troca pelo acompanhante: ele é promovido, o anterior sai, a lista diminui",
			r:    reserva(titular, ana),
			e:    Edit[int]{Lead: ptrContato(achada(ana))},
			quer: Plan[int]{LeadChanged: true, Remove: ptrInt(titular), Promote: true, Listed: 1},
		},
		{
			nome: "troca com a lista cheia não é recusada: não cresce",
			r:    com(reserva(titular, ana), func(r *Reservation[int]) { r.GuestsCount = 2 }),
			e:    Edit[int]{Lead: ptrContato(achada(bruno))},
			quer: Plan[int]{LeadChanged: true, Remove: ptrInt(titular), Listed: 2},
		},
		{
			nome: "troca na reserva encerrada",
			r:    com(reserva(titular, ana), func(r *Reservation[int]) { r.Status = Cancelled }),
			e:    Edit[int]{Lead: ptrContato(achada(bruno))},
			code: "INVALID_STATE_TRANSITION",
		},
		{nome: "troca por ficha anonimizada", r: reserva(), e: Edit[int]{Lead: ptrContato(anonimizada(bruno))}, code: "CONTACT_ANONYMIZED"},
		{nome: "troca por ficha de outra casa", r: reserva(), e: Edit[int]{Lead: ptrContato(deOutraCasa(bruno))}, code: "VALIDATION_ERROR"},
		{
			nome: "reduzir o número abaixo dos nomes",
			r:    reserva(titular, ana, bruno),
			e:    Edit[int]{GuestsCount: ptrInt(2)},
			code: "VALIDATION_ERROR",
		},
		{nome: "reduzir o número até os nomes", r: reserva(titular, ana, bruno), e: Edit[int]{GuestsCount: ptrInt(3)}, quer: Plan[int]{Listed: 3}},
		{nome: "aumentar o número", r: reserva(titular, ana, bruno), e: Edit[int]{GuestsCount: ptrInt(9)}, quer: Plan[int]{Listed: 3}},
		{
			nome: "reduzir o número na reserva encerrada também esbarra nos nomes",
			r:    com(reserva(titular, ana, bruno), func(r *Reservation[int]) { r.Status = CheckedOut }),
			e:    Edit[int]{GuestsCount: ptrInt(2)},
			code: "VALIDATION_ERROR",
		},
		{
			nome: "troca e redução juntas: a conta é da lista depois da troca",
			r:    com(reserva(titular, ana, bruno), func(r *Reservation[int]) { r.GuestsCount = 3 }),
			e:    Edit[int]{Lead: ptrContato(achada(ana)), GuestsCount: ptrInt(2)},
			quer: Plan[int]{LeadChanged: true, Remove: ptrInt(titular), Promote: true, Listed: 2},
		},
		{
			nome: "troca e redução que não cabem",
			r:    com(reserva(titular, ana, bruno), func(r *Reservation[int]) { r.GuestsCount = 3 }),
			e:    Edit[int]{Lead: ptrContato(achada(carla)), GuestsCount: ptrInt(2)},
			code: "VALIDATION_ERROR",
		},
		// Dado anterior ao F2-22: lista acima do número, ou sem a linha do titular.
		{
			nome: "lista legada acima do número: editar a anotação passa",
			r:    com(reserva(titular, ana, bruno, carla), func(r *Reservation[int]) { r.GuestsCount = 3 }),
			e:    Edit[int]{},
			quer: Plan[int]{Listed: 4},
		},
		{
			nome: "lista legada acima do número: reenviar o mesmo número passa",
			r:    com(reserva(titular, ana, bruno, carla), func(r *Reservation[int]) { r.GuestsCount = 3 }),
			e:    Edit[int]{GuestsCount: ptrInt(3)},
			quer: Plan[int]{Listed: 4},
		},
		{
			nome: "lista legada acima do número: reduzir mais é recusado",
			r:    com(reserva(titular, ana, bruno, carla), func(r *Reservation[int]) { r.GuestsCount = 3 }),
			e:    Edit[int]{GuestsCount: ptrInt(2)},
			code: "VALIDATION_ERROR",
		},
		{
			nome: "sem a linha do titular, a troca cresceria além do número",
			r:    com(reserva(ana, bruno), func(r *Reservation[int]) { r.GuestsCount = 2 }),
			e:    Edit[int]{Lead: ptrContato(achada(carla))},
			code: "GUEST_LIST_FULL",
		},
		{
			nome: "sem a linha do titular, a troca cabe se o número sobe junto",
			r:    com(reserva(ana, bruno), func(r *Reservation[int]) { r.GuestsCount = 2 }),
			e:    Edit[int]{Lead: ptrContato(achada(carla)), GuestsCount: ptrInt(3)},
			quer: Plan[int]{LeadChanged: true, Listed: 3},
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			p, err := PlanEdit(c.r, c.e)
			if c.code != "" {
				re := regra(t, err, c.code)
				if c.code == "INVALID_STATE_TRANSITION" && re.Details["field"] != "contact_id" {
					t.Errorf("a recusa da troca diz o campo: %v", re.Details)
				}
				if c.code == "VALIDATION_ERROR" && c.e.GuestsCount != nil {
					if _, ok := re.Details["guests_count"]; !ok {
						t.Errorf("a recusa do número diz o campo: %v", re.Details)
					}
				}
				return
			}
			semErro(t, err)
			if !reflect.DeepEqual(p, c.quer) {
				t.Errorf("plano = %+v (remove %v), esperado %+v (remove %v)", p, deref(p.Remove), c.quer, deref(c.quer.Remove))
			}
		})
	}
}

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// ─────────────────────────── Remarcar ───────────────────────────────

func TestCarryMesa(t *testing.T) {
	lista := []Contact[int]{achada(titular), achada(ana), achada(bruno)}

	got, err := Carry(titular, lista, 3)
	semErro(t, err)
	if !reflect.DeepEqual(got, []int{ana, bruno}) {
		t.Errorf("Carry = %v, esperado os dois acompanhantes", got)
	}

	re := regra(t, func() error { _, err := Carry(titular, lista, 2); return err }(), "VALIDATION_ERROR")
	if _, ok := re.Details["guests_count"]; !ok {
		t.Errorf("a recusa diz o campo guests_count: %v", re.Details)
	}

	got, err = Carry(titular, []Contact[int]{achada(titular), anonimizada(ana), achada(bruno)}, 2)
	semErro(t, err)
	if !reflect.DeepEqual(got, []int{bruno}) {
		t.Errorf("o anonimizado não é carregado: %v", got)
	}

	got, err = Carry(titular, []Contact[int]{achada(titular)}, 1)
	semErro(t, err)
	if got == nil || len(got) != 0 {
		t.Errorf("só o titular: lista vazia e não nula, veio %#v", got)
	}

	// Sem a linha do titular (dado legado), ele ainda conta: a reserva nova
	// nasce com ele.
	if _, err := Carry(titular, []Contact[int]{achada(ana), achada(bruno)}, 2); err == nil {
		t.Error("titular + dois acompanhantes não cabem em 2")
	}

	// Found não é lido: quem está na lista existe. Um service que monte o
	// contato sem Found não pode ver os acompanhantes sumirem em silêncio.
	got, err = Carry(titular, []Contact[int]{{ID: titular}, {ID: ana}}, 2)
	semErro(t, err)
	if !reflect.DeepEqual(got, []int{ana}) {
		t.Errorf("Carry ignorou acompanhante sem Found: %v", got)
	}
}

// ─────────────────────────── Propriedade ────────────────────────────

// O teste de propriedade simula o service: sorteia uma reserva coerente e uma
// sequência de escritas (acrescentar, retirar, editar a reserva), aplica só o
// que o domínio aceita — exatamente como o plano manda — e confere, depois de
// cada passo, as invariantes que a rooming list promete:
//
//   - o titular está na lista, uma vez só, e é o titular da reserva;
//   - ninguém aparece duas vezes;
//   - a lista nunca passa de guests_count;
//   - ninguém anonimizado, nem de outra casa, entra;
//   - reserva fora dos estados de pé não muda a lista nem o titular;
//   - Plan.Listed é o tamanho real da lista depois de aplicar o plano;
//   - toda recusa é uma *RuleError com code do vocabulário (nunca defeito).
func TestPropriedadeDaListaDeHospedes(t *testing.T) {
	rng := rand.New(rand.NewSource(20261013))
	estados := []ReservationStatus{Quote, Hold, Confirmed, CheckedIn, CheckedOut, Closed, Cancelled, Expired, NoShow}
	codes := map[string]bool{
		"VALIDATION_ERROR": true, "NOT_FOUND": true, "INVALID_STATE_TRANSITION": true, "CODE_IN_USE": true,
		"RESOURCE_IN_USE": true, "CONTACT_ANONYMIZED": true, "GUEST_LIST_FULL": true,
	}
	const pessoas = 14

	aceitas := map[string]int{}
	for seq := 0; seq < 3000; seq++ {
		capacidade := 1 + rng.Intn(10)
		vendidos := 1 + rng.Intn(capacidade)
		lead := 1 + rng.Intn(pessoas)
		anon := map[int]bool{}
		fora := map[int]bool{}
		for id := 1; id <= pessoas; id++ {
			switch rng.Intn(6) {
			case 0:
				anon[id] = true
			case 1:
				fora[id] = true
			}
		}
		delete(anon, lead)
		delete(fora, lead)
		listed := []int{lead}
		for _, id := range rng.Perm(pessoas) {
			id++
			if len(listed) >= vendidos || id == lead || anon[id] || fora[id] {
				continue
			}
			if rng.Intn(2) == 0 {
				listed = append(listed, id)
			}
		}
		r := Reservation[int]{
			Status: estados[rng.Intn(len(estados))], Lead: lead,
			GuestsCount: vendidos, Capacity: capacidade, Listed: listed,
		}
		contato := func(id int) Contact[int] {
			return Contact[int]{ID: id, Found: !fora[id], Anonymized: anon[id]}
		}

		for passo := 0; passo < 25; passo++ {
			antes := append([]int(nil), r.Listed...)
			antesLead := r.Lead
			alvo := 1 + rng.Intn(pessoas)
			var err error
			op := rng.Intn(3)
			switch op {
			case 0:
				if err = CheckAdd(r, contato(alvo)); err == nil {
					r.Listed = append(r.Listed, alvo)
					aceitas["add"]++
				}
			case 1:
				if err = CheckRemove(r, alvo); err == nil {
					r.Listed = sem(r.Listed, alvo)
					aceitas["remove"]++
				}
			case 2:
				var e Edit[int]
				if rng.Intn(2) == 0 {
					e.Lead = ptrContato(contato(alvo))
				}
				if rng.Intn(2) == 0 {
					// O número pedido nunca passa da capacidade: essa recusa
					// (CAPACITY_EXCEEDED) é do service, antes daqui.
					e.GuestsCount = ptrInt(1 + rng.Intn(capacidade))
				}
				var p Plan[int]
				if p, err = PlanEdit(r, e); err == nil {
					if p.LeadChanged {
						if p.Remove != nil {
							r.Listed = sem(r.Listed, *p.Remove)
						}
						if !p.Promote {
							r.Listed = append(r.Listed, e.Lead.ID)
						}
						r.Lead = e.Lead.ID
						aceitas["troca"]++
					}
					if e.GuestsCount != nil {
						r.GuestsCount = *e.GuestsCount
					}
					if p.Listed != len(r.Listed) {
						t.Fatalf("seq %d passo %d: Plan.Listed = %d, lista aplicada tem %d", seq, passo, p.Listed, len(r.Listed))
					}
				}
			}

			if err != nil {
				var re *RuleError
				if !errors.As(err, &re) || !codes[re.Code] {
					t.Fatalf("seq %d passo %d: recusa fora do vocabulário: %v", seq, passo, err)
				}
				if !reflect.DeepEqual(antes, r.Listed) || antesLead != r.Lead {
					t.Fatalf("seq %d passo %d: recusa mexeu no estado", seq, passo)
				}
				continue
			}

			if !Editable(r.Status) && (!reflect.DeepEqual(antes, r.Listed) || antesLead != r.Lead) {
				t.Fatalf("seq %d passo %d: lista mudou com a reserva em %s: %v → %v", seq, passo, r.Status, antes, r.Listed)
			}
			vistos := map[int]int{}
			for _, id := range r.Listed {
				vistos[id]++
				if vistos[id] > 1 {
					t.Fatalf("seq %d passo %d: %d aparece duas vezes: %v", seq, passo, id, r.Listed)
				}
				if anon[id] || fora[id] {
					t.Fatalf("seq %d passo %d: %d (anonimizado=%v, de fora=%v) entrou na lista", seq, passo, id, anon[id], fora[id])
				}
			}
			if vistos[r.Lead] != 1 {
				t.Fatalf("seq %d passo %d: o titular %d não está na lista %v", seq, passo, r.Lead, r.Listed)
			}
			if len(r.Listed) > r.GuestsCount {
				t.Fatalf("seq %d passo %d: %d nomes para %d vendidos", seq, passo, len(r.Listed), r.GuestsCount)
			}
		}
	}
	// Sequência que nunca aceita nada prova nada: as três escritas têm de ter
	// acontecido de verdade, muitas vezes.
	for _, op := range []string{"add", "remove", "troca"} {
		if aceitas[op] < 500 {
			t.Errorf("só %d escritas %q aceitas: o sorteio deixou de exercitar a regra", aceitas[op], op)
		}
	}
}

// Para qualquer lista sorteada, Carry devolve só acompanhantes que podem
// entrar, sem o titular, e o que devolve cabe na reserva nova.
func TestPropriedadeDaRemarcacao(t *testing.T) {
	rng := rand.New(rand.NewSource(13102026))
	for i := 0; i < 5000; i++ {
		lead := 1 + rng.Intn(8)
		var lista []Contact[int]
		for _, id := range rng.Perm(12) {
			id++
			if rng.Intn(2) == 0 {
				lista = append(lista, Contact[int]{ID: id, Found: true, Anonymized: rng.Intn(5) == 0})
			}
		}
		vendidos := 1 + rng.Intn(10)
		got, err := Carry(lead, lista, vendidos)
		elegiveis := 0
		for _, c := range lista {
			if c.ID != lead && !c.Anonymized {
				elegiveis++
			}
		}
		if err != nil {
			recusa(t, err, "VALIDATION_ERROR")
			if 1+elegiveis <= vendidos {
				t.Fatalf("recusou %d + titular em %d vendidos", elegiveis, vendidos)
			}
			continue
		}
		if len(got) != elegiveis || 1+len(got) > vendidos {
			t.Fatalf("Carry = %v para %d elegíveis e %d vendidos", got, elegiveis, vendidos)
		}
		for _, id := range got {
			if id == lead {
				t.Fatalf("o titular foi carregado como acompanhante: %v", got)
			}
			for _, c := range lista {
				if c.ID == id && c.Anonymized {
					t.Fatalf("anonimizado carregado: %d", id)
				}
			}
		}
	}
}

func sem(lista []int, id int) []int {
	out := lista[:0:0]
	for _, x := range lista {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}
