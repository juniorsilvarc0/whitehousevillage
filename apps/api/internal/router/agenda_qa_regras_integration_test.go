//go:build integration

package router

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

// Regras do desenho (spec §9 e tag Agenda) que atravessam reserva e agenda.

// Em linguagem de negócio: no dia de troca, um hóspede sai, a unidade é limpa
// e o seguinte entra — e a agenda não colide consigo mesma (nenhum dos seis
// eventos das duas reservas aparece em conflito). Antecipar a entrada do
// seguinte para dentro da limpeza do anterior é recusado; e empurrar a
// limpeza do anterior para o dia seguinte, com o próximo hóspede dentro,
// também.
func TestAgendaQADiaDeTrocaNaoColideConsigoMesmo(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	h := a.qagHorariosDaCasa(t, u.Token)
	p := f.produto(t, casa.Prop, "one_member", 1)
	tk := u.Token
	d := casa.Hoje.AddDays(330)
	troca := d.AddDays(2)

	sai := a.qagVendida(t, tk, p.ID, f.contato(t, casa.Prop).ID, d, troca)
	entra := a.qagVendida(t, tk, p.ID, f.contato(t, casa.Prop).ID, troca, troca.AddDays(2))
	evSai, evEntra := qagPorTipo(t, a.qagDaReserva(t, tk, sai.ID)), qagPorTipo(t, a.qagDaReserva(t, tk, entra.ID))
	for _, ev := range []map[string]qagEvento{evSai, evEntra} {
		for tipo, e := range ev {
			if e.Conflito {
				t.Errorf("dia de troca: o %s %s aparece em conflito", tipo, e.Titulo)
			}
		}
	}

	// Entrada antecipada para dentro da limpeza do anterior.
	r := a.chamar(t, http.MethodPatch, "/agenda/events/"+evEntra["checkin"].ID.String(), tk, map[string]any{
		"starts_at": casa.rfc(troca, qagSomar(h.LimpezaDe, 60)), "ends_at": casa.rfc(troca, qagSomar(h.LimpezaDe, 60+h.Passo))})
	qbErro(t, r, http.StatusConflict, "UNIT_NOT_AVAILABLE", "check-in antecipado para dentro da limpeza do anterior")
	if cs := qagConflitos(t, r); len(cs) != 1 || cs[0]["kind"] != "limpeza" {
		t.Errorf("o conflito é a limpeza do anterior: %v", cs)
	}
	// A limpeza do anterior empurrada para o dia seguinte, com o próximo dentro.
	r = a.chamar(t, http.MethodPatch, "/agenda/events/"+evSai["limpeza"].ID.String(), tk, map[string]any{
		"starts_at": casa.rfc(troca.AddDays(1), "10:00"), "ends_at": casa.rfc(troca.AddDays(1), "12:00")})
	qbErro(t, r, http.StatusConflict, "UNIT_NOT_AVAILABLE", "limpeza do anterior durante a estadia do seguinte")
	if cs := qagConflitos(t, r); len(cs) != 1 || cs[0]["kind"] != "estadia" {
		t.Errorf("o conflito é a estadia do seguinte: %v", cs)
	}
}

// Em linguagem de negócio: o hóspede combinou chegar às 16:00 (o check-in foi
// ajustado). A estadia dele começa na hora COMBINADA: a vistoria marcada para
// as 14:30 do dia da entrada cabe; às 16:30, não.
func TestAgendaQAEstadiaComecaNaHoraCombinada(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	tk := u.Token
	d := casa.Hoje.AddDays(340)

	r := a.qagVendida(t, tk, p.ID, f.contato(t, casa.Prop).ID, d, d.AddDays(2))
	ci := qagPorTipo(t, a.qagDaReserva(t, tk, r.ID))["checkin"]
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+ci.ID.String(), tk, map[string]any{
		"starts_at": casa.rfc(d, "16:00"), "ends_at": casa.rfc(d, "16:30")}), http.StatusOK, "chegada combinada às 16:00")

	cabe := a.chamar(t, http.MethodPost, "/agenda/events", tk, map[string]any{"type": "visita", "title": "Vistoria antes da chegada",
		"unit_id": p.Unidades[0], "starts_at": casa.rfc(d, "14:30"), "ends_at": casa.rfc(d, "15:30")})
	if cabe.Status != http.StatusCreated {
		t.Errorf("a estadia começa na hora combinada (16:00): a vistoria das 14:30 cabe; veio %d — %s", cabe.Status, cabe.Corpo)
	}
	nao := a.chamar(t, http.MethodPost, "/agenda/events", tk, map[string]any{"type": "visita", "title": "Visita com o hóspede dentro",
		"unit_id": p.Unidades[0], "starts_at": casa.rfc(d, "16:30"), "ends_at": casa.rfc(d, "17:00")})
	qbErro(t, nao, http.StatusConflict, "UNIT_NOT_AVAILABLE", "visita depois da chegada combinada")
	for _, c := range qagConflitos(t, nao) {
		if c["kind"] == "estadia" && c["starts_at"] != casa.rfc(d, "16:00") {
			t.Errorf("a estadia do conflito começa na hora combinada: %v", c)
		}
	}
}

// Em linguagem de negócio: a reserva foi cancelada com o hóspede dentro. O
// jantar de boas-vindas de ontem (que aconteceu, mesmo sem ninguém dar baixa)
// fica; o passeio de amanhã, que ainda não começou, é cancelado.
func TestAgendaQACancelarComOHospedeDentroSoCancelaOManualQueNaoComecou(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	tk := u.Token

	r := a.qagVendida(t, tk, p.ID, f.contato(t, casa.Prop).ID, casa.Hoje, casa.Hoje.AddDays(3))
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/check-in", tk, map[string]any{}), http.StatusOK, "check-in")
	ontem := a.qagMarcar(t, tk, map[string]any{"type": "evento", "title": "Jantar de boas-vindas", "reservation_id": r.ID,
		"starts_at": casa.rfc(casa.Hoje.AddDays(-1), "19:00"), "ends_at": casa.rfc(casa.Hoje.AddDays(-1), "21:00")}).Data
	amanha := a.qagMarcar(t, tk, map[string]any{"type": "evento", "title": "Passeio de barco", "reservation_id": r.ID,
		"starts_at": casa.rfc(casa.Hoje.AddDays(1), "09:00"), "ends_at": casa.rfc(casa.Hoje.AddDays(1), "12:00")}).Data
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/cancel", tk, map[string]any{}), http.StatusOK,
		"cancelando com o hóspede dentro")

	evs := a.qagDaReserva(t, tk, r.ID)
	if e, _ := qagAchar(evs, ontem.ID); e.Status != "agendado" {
		t.Errorf("o manual que já começou fica (aconteceu): %s", e.Status)
	}
	if e, _ := qagAchar(evs, amanha.ID); e.Status != "cancelado" {
		t.Errorf("o manual que não começou é cancelado: %s", e.Status)
	}
}

// Em linguagem de negócio: o evento ligado a uma reserva precisa de uma
// reserva viva (pré-reserva, confirmada ou hospedada) desta casa, numa unidade
// dela; o responsável tem de ser alguém ativo; e a janela tem teto (31 dias de
// duração, de um ano atrás a três à frente). Fora disso, 422 no campo — e nada
// nasce.
func TestAgendaQAMarcarRecusaVinculoEJanelaForaDoContrato(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	inativo := a.criarUsuario(t, "ag-inativo", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u, inativo)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 2)
	tk := u.Token
	d := casa.Hoje.AddDays(350)
	if _, err := a.pool.Exec(a.ctx, `UPDATE users SET active = false WHERE id = $1`, inativo.ID); err != nil {
		t.Fatal(err)
	}

	preReserva := a.qagReservar(t, tk, p.ID, f.contato(t, casa.Prop).ID, d, d.AddDays(2))
	cancelada := a.qagVendida(t, tk, p.ID, f.contato(t, casa.Prop).ID, d.AddDays(5), d.AddDays(7))
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/reservations/"+cancelada.ID.String()+"/cancel", tk, map[string]any{}), http.StatusOK, "cancelando")
	daPre := preReserva.Unidades[0].ID
	outra := p.Unidades[0]
	if outra == daPre {
		outra = p.Unidades[1]
	}

	// A pré-reserva aceita evento ligado.
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/agenda/events", tk, map[string]any{"type": "visita", "title": "Visita da pré-reserva",
		"reservation_id": preReserva.ID, "unit_id": daPre, "starts_at": casa.rfc(d, "10:00")}), http.StatusCreated, "evento na pré-reserva")

	marca := "Recusado " + qbSufixo()
	casos := []struct {
		nome  string
		corpo map[string]any
		campo string
	}{
		{"reserva cancelada", map[string]any{"reservation_id": cancelada.ID, "starts_at": casa.rfc(d.AddDays(5), "10:00")}, "reservation_id"},
		{"unidade que não é da reserva", map[string]any{"reservation_id": preReserva.ID, "unit_id": outra, "starts_at": casa.rfc(d, "11:00")}, "unit_id"},
		{"responsável inativo", map[string]any{"assignee_id": inativo.ID, "starts_at": casa.rfc(d, "12:00")}, "assignee_id"},
		{"mais de 31 dias", map[string]any{"starts_at": casa.rfc(d, "00:00"), "ends_at": casa.rfc(d.AddDays(32), "00:00"), "all_day": true}, "ends_at"},
		{"mais de um ano atrás", map[string]any{"starts_at": casa.rfc(casa.Hoje.AddDays(-366), "10:00")}, "starts_at"},
		{"título de 201 caracteres", map[string]any{"title": strings.Repeat("t", 201), "starts_at": casa.rfc(d, "13:00")}, "title"},
		{"anotação de 2001 caracteres", map[string]any{"notes": strings.Repeat("n", 2001), "starts_at": casa.rfc(d, "13:00")}, "notes"},
		{"tipo check-in à mão", map[string]any{"type": "checkin", "starts_at": casa.rfc(d, "13:00")}, "type"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			corpo := map[string]any{"type": "visita", "title": marca}
			for k, v := range c.corpo {
				corpo[k] = v
			}
			e := qbErro(t, a.chamar(t, http.MethodPost, "/agenda/events", tk, corpo), http.StatusUnprocessableEntity, "VALIDATION_ERROR", c.nome)
			if e.Details[c.campo] == nil {
				t.Errorf("o 422 não nomeia %s: %v", c.campo, e.Details)
			}
		})
	}
	if n := a.qagContar(t, `SELECT count(*) FROM agenda_events WHERE title = $1 OR length(title) > 200`, marca); n != 0 {
		t.Fatalf("um 422 criou %d eventos", n)
	}
	// Os limites exatos passam: 31 dias, um ano atrás, 200 caracteres aparados.
	for nome, corpo := range map[string]map[string]any{
		"exatamente 31 dias": {"type": "evento", "title": "Temporada de obras", "all_day": true,
			"starts_at": casa.rfc(d.AddDays(40), "00:00"), "ends_at": casa.rfc(d.AddDays(71), "00:00")},
		"exatamente um ano atrás":              {"type": "visita", "title": "Lançamento atrasado", "starts_at": casa.rfc(casa.Hoje.AddDays(-365), "10:00")},
		"200 caracteres com espaço nas pontas": {"type": "visita", "title": "  " + strings.Repeat("t", 200) + "  ", "starts_at": casa.rfc(d, "15:00")},
	} {
		exigirStatusQB(t, a.chamar(t, http.MethodPost, "/agenda/events", tk, corpo), http.StatusCreated, nome)
	}
	// Mudar a reserva de um evento preso a uma unidade que não é da nova: 422.
	ev := a.qagMarcar(t, tk, map[string]any{"type": "visita", "title": "Vai trocar de reserva", "unit_id": outra,
		"starts_at": casa.rfc(d.AddDays(20), "10:00")}).Data
	e := qbErro(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+ev.ID.String(), tk, map[string]any{"reservation_id": preReserva.ID}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "ligando à reserva de outra unidade")
	if e.Details["unit_id"] == nil {
		t.Errorf("o 422 nomeia unit_id: %v", e.Details)
	}
}

// Em linguagem de negócio: a visita que estava em conflito (a manutenção
// chegou depois) e que a equipe deu por feita deixa de aparecer em conflito —
// fechado não ocupa mais nada. Dois bloqueios de horário no mesmo horário não
// se recusam: são dois avisos.
func TestAgendaQAFechadoNaoMostraConflitoEBloqueiosSeSomam(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	tk := u.Token
	unidade, _ := f.unidade(t, casa.Prop)
	hoje := casa.Hoje

	visita := a.qagMarcar(t, tk, map[string]any{"type": "visita", "title": "Visita de hoje", "unit_id": unidade,
		"starts_at": casa.rfc(hoje, "09:00"), "ends_at": casa.rfc(hoje, "09:30")}).Data
	ordem := qbDado[qmOrdem](t, a.chamar(t, http.MethodPost, "/maintenance-orders", tk, map[string]any{
		"unit_id": unidade, "title": "Vazamento urgente", "block": qmPeriodo(hoje, hoje.AddDays(1)),
	}), http.StatusCreated, "manutenção de hoje")
	f.ordem(ordem.ID)
	if v := a.qagLer(t, tk, visita.ID); !v.Conflito {
		t.Fatalf("a manutenção chegou depois: a visita aparece em conflito: %+v", v)
	}
	feita := qbDado[qagEvento](t, a.chamar(t, http.MethodPost, "/agenda/events/"+visita.ID.String()+"/complete", tk, nil), http.StatusOK, "concluindo")
	if feita.Conflito {
		t.Errorf("fechado não mostra conflito (has_unit_conflict sempre false em fechado)")
	}

	d := hoje.AddDays(360)
	for i := 0; i < 2; i++ {
		exigirStatusQB(t, a.chamar(t, http.MethodPost, "/agenda/blocks", tk, map[string]any{"reason": "Treinamento",
			"starts_at": casa.rfc(d, "14:00"), "ends_at": casa.rfc(d, "16:00")}), http.StatusCreated, "bloqueio sobreposto")
	}
	e := a.qagMarcar(t, tk, map[string]any{"type": "evento", "title": "Na hora dos dois treinamentos",
		"starts_at": casa.rfc(d, "15:00"), "ends_at": casa.rfc(d, "15:30")})
	if e.tipos() != "bloqueio_de_horario,bloqueio_de_horario" {
		t.Errorf("dois bloqueios no mesmo horário são dois avisos: [%s]", e.tipos())
	}
}

// Em linguagem de negócio: uma casa num fuso muito longe de UTC (do outro lado
// da linha de data, ou na Samoa Americana). O "hoje" da agenda é o dia DESSA
// casa — o painel recebe `today` e `date` prontos —, a visita de hoje se
// conclui hoje, a de amanhã não, e toda data-hora volta no offset dela. Quando
// os horários mudam, "de hoje em diante" também é o hoje da casa: o check-in
// de ontem (dela) fica; a limpeza de hoje (dela) muda.
func TestAgendaQAOHojeEhODaCasaMesmoLongeDeUTC(t *testing.T) {
	a := subirAPI(t)
	// A hora UTC vem do relógio do BANCO — o mesmo que a API lê —, e não do
	// processo: escolhe o fuso em que, agora, o dia da casa não é o de UTC.
	var (
		horaUTC int
		diaUTC  string
	)
	if err := a.pool.QueryRow(a.ctx, `SELECT extract(hour FROM now() AT TIME ZONE 'UTC')::int, (now() AT TIME ZONE 'UTC')::date::text`).
		Scan(&horaUTC, &diaUTC); err != nil {
		t.Fatal(err)
	}
	fuso := "Pacific/Kiritimati" // UTC+14: das 10:00 UTC em diante já é amanhã lá
	if horaUTC >= 3 && horaUTC < 10 {
		fuso = "Pacific/Pago_Pago" // UTC−11: ainda é ontem lá
	}
	var prop uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `INSERT INTO properties (name, slug, timezone) VALUES ('Casa longe do QA', $1, $2) RETURNING id`,
		"qa-agenda-longe-"+qbSufixo(), fuso).Scan(&prop); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{
			`DELETE FROM agenda_events WHERE property_id = $1`, `DELETE FROM agenda_blocks WHERE property_id = $1`,
			`DELETE FROM agenda_settings WHERE property_id = $1`, `DELETE FROM reservations WHERE property_id = $1`,
			`DELETE FROM unit_types WHERE property_id = $1`, `DELETE FROM contacts WHERE property_id = $1`,
			`DELETE FROM units WHERE property_id = $1`, `DELETE FROM properties WHERE id = $1`,
		} {
			if _, err := a.pool.Exec(ctx, q, prop); err != nil {
				t.Logf("LIMPEZA INCOMPLETA da casa longe (%s): %v", q, err)
			}
		}
	})
	admin := a.qbUsuarioEm(t, "ag-longe", a.qbPerfilDoSeed(t, "admin"), prop)
	f := a.qagFaxina(t, admin)
	casa := a.qagCasa(t, prop)
	off := qagOffsetDaCasa(casa)
	if casa.Hoje.String() == diaUTC {
		t.Logf("aviso: neste horário (%dh UTC) o dia da casa em %s coincide com o de UTC — o teste roda, mas não separa os dois dias", horaUTC, fuso)
	}
	tk := admin.Token
	hoje, ontem, amanha := casa.Hoje, casa.Hoje.AddDays(-1), casa.Hoje.AddDays(1)

	h := a.qagHorariosDaCasa(t, tk)
	if h.Fuso != fuso || h.Hoje != hoje.String() {
		t.Fatalf("timezone %q today %q; a casa é %q e hoje lá é %s", h.Fuso, h.Hoje, fuso, hoje)
	}
	deHoje := a.qagMarcar(t, tk, map[string]any{"type": "evento", "title": "Reunião de hoje", "starts_at": casa.rfc(hoje, "08:00"), "ends_at": casa.rfc(hoje, "09:00")})
	deAmanha := a.qagMarcar(t, tk, map[string]any{"type": "evento", "title": "Reunião de amanhã", "starts_at": casa.rfc(amanha, "08:00"), "ends_at": casa.rfc(amanha, "09:00")})
	deOntem := a.qagMarcar(t, tk, map[string]any{"type": "evento", "title": "Reunião de ontem", "starts_at": casa.rfc(ontem, "23:00"), "ends_at": casa.rfc(ontem, "23:30")})
	for _, c := range []struct {
		e     qagEscrita
		dia   calendar.Date
		acoes string
	}{{deHoje, hoje, "complete,cancel"}, {deAmanha, amanha, "cancel"}, {deOntem, ontem, "complete,cancel"}} {
		if c.e.Data.Data != c.dia.String() || c.e.Data.acoes() != c.acoes {
			t.Errorf("%q: date %s, ações [%s]; esperado %s e [%s] (dia da casa)", c.e.Data.Titulo, c.e.Data.Data, c.e.Data.acoes(), c.dia, c.acoes)
		}
		if !strings.HasSuffix(c.e.Data.Inicio, off) {
			t.Errorf("%q: starts_at %s fora do offset da casa (%s)", c.e.Data.Titulo, c.e.Data.Inicio, off)
		}
	}
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/agenda/events/"+deHoje.Data.ID.String()+"/complete", tk, nil), http.StatusOK, "concluindo a de hoje da casa")
	qbErro(t, a.chamar(t, http.MethodPost, "/agenda/events/"+deAmanha.Data.ID.String()+"/complete", tk, nil), http.StatusConflict,
		"INVALID_STATE_TRANSITION", "concluindo a de amanhã da casa")
	doDia := a.qagJanela(t, tk, hoje, amanha, "")
	if _, ok := qagAchar(doDia, deHoje.Data.ID); !ok {
		t.Errorf("a lista do dia da casa traz a reunião de hoje")
	}
	if _, ok := qagAchar(doDia, deOntem.Data.ID); ok {
		t.Errorf("a lista do dia da casa não traz a de ontem às 23:00")
	}

	// "De hoje em diante" é o hoje da casa: um check-in de ONTEM (da casa) no
	// horário provisório fica; a limpeza de HOJE (da casa) vai para o horário novo.
	unidade, _ := f.unidade(t, prop)
	contato := f.contato(t, prop)
	var produto, reserva uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `INSERT INTO unit_types (property_id, code, name, capacity, consumes, sort_order)
		VALUES ($1, $2, 'Produto da casa longe', 2, 'one_member', 1) RETURNING id`, prop, "qa-longe-"+qbSufixo()).Scan(&produto); err != nil {
		t.Fatal(err)
	}
	if err := a.pool.QueryRow(a.ctx, `INSERT INTO reservations (property_id, unit_type_id, contact_id, status, check_in, check_out, guests_count)
		VALUES ($1, $2, $3, 'confirmed', $4::date, $5::date, 2) RETURNING id`, prop, produto, contato.ID, ontem.String(), hoje.String()).Scan(&reserva); err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(a.ctx, `INSERT INTO reservation_units (reservation_id, unit_id) VALUES ($1, $2)`, reserva, unidade); err != nil {
		t.Fatal(err)
	}
	// A reserva entrou pelo banco; a agenda dela, também — nos horários provisórios.
	if _, err := a.pool.Exec(a.ctx, `
		INSERT INTO agenda_events (property_id, type, origin, reservation_id, starts_at, ends_at)
		VALUES ($1, 'checkin', 'reservation', $2, $3, $4), ($1, 'checkout', 'reservation', $2, $5, $6), ($1, 'limpeza', 'reservation', $2, $7, $8)`,
		prop, reserva, casa.em(ontem, "14:00"), casa.em(ontem, "14:30"), casa.em(hoje, "10:30"), casa.em(hoje, "11:00"),
		casa.em(hoje, "11:00"), casa.em(hoje, "14:00")); err != nil {
		t.Fatal(err)
	}
	r := a.chamar(t, http.MethodPut, "/agenda/settings", tk, qagCorpoDosHorarios("15:00", "10:00", "10:00", "15:00", 30, 15))
	exigirStatusQB(t, r, http.StatusOK, "gravando os horários da casa longe")
	var gravado struct {
		Meta struct {
			Reprogramados int `json:"retimed_events"`
		} `json:"meta"`
	}
	r.decodificar(t, &gravado)
	ev := qagPorTipo(t, a.qagDaReserva(t, tk, reserva))
	qagExigirJanela(t, ev["checkin"], casa.rfc(ontem, "14:00"), casa.rfc(ontem, "14:30"), "o check-in de ontem (da casa) fica")
	qagExigirJanela(t, ev["checkout"], casa.rfc(hoje, "09:30"), casa.rfc(hoje, "10:00"), "o check-out de hoje (da casa) muda")
	qagExigirJanela(t, ev["limpeza"], casa.rfc(hoje, "10:00"), casa.rfc(hoje, "15:00"), "a limpeza de hoje (da casa) muda")
	if gravado.Meta.Reprogramados != 2 {
		t.Errorf("meta.retimed_events = %d, esperado 2 (o check-out e a limpeza de hoje da casa)", gravado.Meta.Reprogramados)
	}
}
