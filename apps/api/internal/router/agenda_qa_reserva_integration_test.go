//go:build integration

package router

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

// A reserva enche a agenda (F2-20), pela porta da frente e com o perfil REAL
// `usuario` do seed — quem opera a casa. O esperado sai do contrato: os
// horários vêm de `GET /agenda/settings`, e a janela de cada automático é a que
// a tag Agenda descreve (check-in: o passo que COMEÇA na hora de entrada;
// check-out: o passo que TERMINA na hora de saída; limpeza: a janela de
// limpeza do dia da saída).

// qagEsperado devolve a janela que o contrato promete para cada automático.
func qagEsperado(casa qagCasa, h qagHorarios, r qagReserva) map[string][2]string {
	de, ate := calendar.MustParse(r.CheckIn), calendar.MustParse(r.CheckOut)
	return map[string][2]string{
		"checkin":  {casa.rfc(de, h.Entrada), casa.rfc(de, qagSomar(h.Entrada, h.Passo))},
		"checkout": {casa.rfc(ate, qagSomar(h.Saida, -h.Passo)), casa.rfc(ate, h.Saida)},
		"limpeza":  {casa.rfc(ate, h.LimpezaDe), casa.rfc(ate, h.LimpezaAte)},
	}
}

// Em linguagem de negócio: confirmar o sinal põe na agenda da operação o
// check-in no dia da entrada e o check-out e a limpeza no dia da saída, nos
// horários que a casa configurou — sem responsável, seguindo a reserva. O
// clique duplo no "confirmar" (mesma Idempotency-Key) não duplica a agenda, e
// a segunda confirmação com chave nova é recusada sem criar nada.
func TestAgendaQAConfirmarPoeTresEventosNosHorariosDaCasa(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	h := a.qagHorariosDaCasa(t, u.Token)
	p := f.produto(t, casa.Prop, "one_member", 1)
	hospede := f.contato(t, casa.Prop)
	de, ate := casa.Hoje.AddDays(60), casa.Hoje.AddDays(63)

	r := a.qagReservar(t, u.Token, p.ID, hospede.ID, de, ate)
	if evs := a.qagDaReserva(t, u.Token, r.ID); len(evs) != 0 {
		t.Fatalf("pré-reserva (hold) não tem agenda; vieram %d eventos", len(evs))
	}
	chave := a.chaveNova(t, "agenda-conf")
	primeira := a.qagConfirmar(t, u.Token, r, chave)
	exigirStatusQB(t, primeira, http.StatusOK, "POST /confirm")

	evs := a.qagDaReserva(t, u.Token, r.ID)
	if len(evs) != 3 {
		t.Fatalf("o /confirm põe três eventos; vieram %d: %+v", len(evs), evs)
	}
	tipos := qagPorTipo(t, evs)
	esperado := qagEsperado(casa, h, r)
	for tipo, janela := range esperado {
		e, ok := tipos[tipo]
		if !ok {
			t.Fatalf("faltou o %s automático: %+v", tipo, evs)
		}
		qagExigirJanela(t, e, janela[0], janela[1], tipo)
		if e.Status != "agendado" || e.ResponsavelID != nil || e.Editavel != "segue_a_reserva" || e.DiaInteiro {
			t.Errorf("%s: status %s, responsável %v, editable %s, all_day %v", tipo, e.Status, e.ResponsavelID, e.Editavel, e.DiaInteiro)
		}
		if e.UnidadeID != nil || len(e.Unidades) != 1 || e.Unidades[0].ID != p.Unidades[0] || e.Unidades[0].Codigo != p.Codigos[0] {
			t.Errorf("%s: o automático lê a unidade da reserva e não a grava: unit_id %v, units %+v", tipo, e.UnidadeID, e.Unidades)
		}
		if e.ContatoID == nil || *e.ContatoID != hospede.ID || e.ContatoNome == nil || *e.ContatoNome != hospede.Nome {
			t.Errorf("%s: o titular é lido da reserva: %v %v", tipo, e.ContatoID, e.ContatoNome)
		}
		if e.ReservaID == nil || *e.ReservaID != r.ID || e.ReservaCodigo == nil || *e.ReservaCodigo != r.Codigo ||
			!strings.Contains(e.Titulo, r.Codigo) {
			t.Errorf("%s: reserva %v/%v, título %q", tipo, e.ReservaID, e.ReservaCodigo, e.Titulo)
		}
		if len(e.Acoes) != 0 || e.Conflito {
			t.Errorf("%s: dia futuro e sem conflito — ações %v, has_unit_conflict %v", tipo, e.Acoes, e.Conflito)
		}
	}
	if tipos["checkin"].Data != de.String() || tipos["checkout"].Data != ate.String() || tipos["limpeza"].Data != ate.String() {
		t.Errorf("date é o dia da casa: check-in %s, check-out %s, limpeza %s", tipos["checkin"].Data, tipos["checkout"].Data, tipos["limpeza"].Data)
	}

	// O clique duplo: a mesma chave devolve a resposta guardada e não cria nada.
	segunda := a.qagConfirmar(t, u.Token, r, chave)
	exigirStatusQB(t, segunda, http.StatusOK, "POST /confirm repetido com a mesma chave")
	if !qagMesmoJSON(t, primeira.Corpo, segunda.Corpo) {
		t.Errorf("a mesma Idempotency-Key devolve a resposta guardada:\n 1ª %s\n 2ª %s", primeira.Corpo, segunda.Corpo)
	}
	if n := a.qagContar(t, `SELECT count(*) FROM agenda_events WHERE reservation_id = $1`, r.ID); n != 3 {
		t.Fatalf("depois da repetição: %d eventos no banco, esperado 3", n)
	}
	// Chave nova sobre a reserva já confirmada: recusa, e a agenda fica.
	qbErro(t, a.qagConfirmar(t, u.Token, r, a.chaveNova(t, "agenda-conf2")), http.StatusConflict, "", "segunda confirmação")
	if n := a.qagContar(t, `SELECT count(*) FROM agenda_events WHERE reservation_id = $1`, r.ID); n != 3 {
		t.Fatalf("depois da segunda confirmação: %d eventos no banco, esperado 3", n)
	}
	// Critério de aceite da spec §9: o quarto automático, direto no banco, é
	// recusado pelo índice único — em qualquer estado.
	_, err := a.pool.Exec(a.ctx, `
		INSERT INTO agenda_events (property_id, type, origin, reservation_id, starts_at, ends_at, status, cancelled_at)
		VALUES ($1, 'checkin', 'reservation', $2, $3, $4, 'cancelado', now())`,
		casa.Prop, r.ID, casa.em(de, "15:00"), casa.em(de, "15:30"))
	if err == nil || !strings.Contains(err.Error(), "agenda_events_da_reserva_idx") {
		t.Errorf("um segundo check-in automático para a reserva tem de ser recusado pelo índice único: %v", err)
	}
	for tipo, e := range tipos {
		if e.BloqueioID != nil || e.OrdemID != nil || e.CriadoEm == nil || e.AtualizadoEm == nil {
			t.Errorf("%s gravado: stay_block_id %v e maintenance_order_id %v só nos derivados; created_at/updated_at presentes (%v/%v)",
				tipo, e.BloqueioID, e.OrdemID, e.CriadoEm, e.AtualizadoEm)
		}
	}
	// E a visão de calendário da unidade traz os três.
	if evs := a.qagJanela(t, u.Token, de, ate.AddDays(1), "unit_id="+p.Unidades[0].String()); len(evs) != 3 {
		t.Errorf("a janela da estadia filtrada pela unidade traz os três: %d", len(evs))
	}
}

// Em linguagem de negócio: o hóspede desistiu antes de chegar. A agenda dele
// inteira sai de cena — os três automáticos e o que a equipe marcou à mão
// ligado à reserva —, mas nada some: fica `cancelado`, com a hora em que
// caiu. O que não era dessa reserva fica onde estava; a simulação do
// cancelamento não toca em nada; e o no-show tem o mesmo efeito.
func TestAgendaQACancelarAntesDaEntradaCancelaTudoSemApagar(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	hospede := f.contato(t, casa.Prop)
	de, ate := casa.Hoje.AddDays(70), casa.Hoje.AddDays(73)
	unidade := p.Unidades[0]

	r := a.qagVendida(t, u.Token, p.ID, hospede.ID, de, ate)
	preso := a.qagMarcar(t, u.Token, map[string]any{"type": "limpeza", "title": "Faxina do meio da estadia",
		"reservation_id": r.ID, "unit_id": unidade,
		"starts_at": casa.rfc(de.AddDays(1), "10:00"), "ends_at": casa.rfc(de.AddDays(1), "11:00")}).Data
	solto := a.qagMarcar(t, u.Token, map[string]any{"type": "evento", "title": "Jantar do grupo",
		"reservation_id": r.ID, "starts_at": casa.rfc(de.AddDays(1), "19:00"), "ends_at": casa.rfc(de.AddDays(1), "21:00")}).Data
	alheio := a.qagMarcar(t, u.Token, map[string]any{"type": "visita", "title": "Visita de outro cliente",
		"unit_id": unidade, "starts_at": casa.rfc(ate.AddDays(2), "10:00"), "ends_at": casa.rfc(ate.AddDays(2), "11:00")}).Data

	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/cancel?dry_run=true", u.Token, map[string]any{}),
		http.StatusOK, "simulando o cancelamento")
	for _, e := range a.qagDaReserva(t, u.Token, r.ID) {
		if e.Status != "agendado" {
			t.Fatalf("dry_run mexeu na agenda: %s ficou %s", e.Titulo, e.Status)
		}
	}

	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/cancel", u.Token,
		map[string]any{"reason": "desistência"}), http.StatusOK, "cancelando")
	evs := a.qagDaReserva(t, u.Token, r.ID)
	if len(evs) != 5 {
		t.Fatalf("cancelar não apaga: %d eventos ligados, esperado 5 (3 automáticos e 2 manuais)", len(evs))
	}
	for _, e := range evs {
		if e.Status != "cancelado" || e.CanceladoEm == nil || e.Editavel != "nada" || len(e.Acoes) != 0 || e.Conflito {
			t.Errorf("%s %q: status %s, cancelled_at %v, editable %s, ações %v, conflito %v",
				e.Origem, e.Titulo, e.Status, e.CanceladoEm, e.Editavel, e.Acoes, e.Conflito)
		}
	}
	for _, id := range []uuid.UUID{preso.ID, solto.ID} {
		if _, ok := qagAchar(evs, id); !ok {
			t.Errorf("o manual ligado à reserva %s sumiu da lista", id)
		}
	}
	if n := a.qagContar(t, `SELECT count(*) FROM agenda_events WHERE reservation_id = $1`, r.ID); n != 5 {
		t.Fatalf("no banco: %d linhas ligadas à reserva, esperado 5", n)
	}
	if e := a.qagLer(t, u.Token, alheio.ID); e.Status != "agendado" {
		t.Errorf("o que não era da reserva não muda: %s", e.Status)
	}
	if evs, meta := qbLista[qagEvento](t, a.chamar(t, http.MethodGet, "/agenda/events?reservation_id="+r.ID.String(), u.Token, nil),
		"lista padrão"); len(evs) != 0 || meta.Total != 0 {
		t.Errorf("o padrão (agendado,concluido) esconde os cancelados; vieram %d", meta.Total)
	}

	// No-show é cancelar sem o hóspede ter entrado: a mesma agenda.
	r2 := a.qagVendida(t, u.Token, p.ID, hospede.ID, ate.AddDays(5), ate.AddDays(7))
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/reservations/"+r2.ID.String()+"/cancel", u.Token,
		map[string]any{"reason": "no_show"}), http.StatusOK, "no-show")
	evs2 := a.qagDaReserva(t, u.Token, r2.ID)
	if len(evs2) != 3 {
		t.Fatalf("no-show: %d eventos, esperado 3", len(evs2))
	}
	for _, e := range evs2 {
		if e.Status != "cancelado" {
			t.Errorf("no-show: o %s ficou %s", e.Tipo, e.Status)
		}
	}
}

// Em linguagem de negócio: o hóspede mudou as datas. A agenda da data velha é
// cancelada (e não reaproveitada), e a nova data ganha o check-in, o check-out
// e a limpeza dela, nos horários da casa — o "chega às 16h" combinado para a
// data velha e a anotação não viajam, porque valiam para outro dia.
func TestAgendaQARemarcarCancelaAOriginalECriaOsTresDaNova(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	h := a.qagHorariosDaCasa(t, u.Token)
	p := f.produto(t, casa.Prop, "one_member", 1)
	hospede := f.contato(t, casa.Prop)
	de, ate := casa.Hoje.AddDays(80), casa.Hoje.AddDays(83)

	r := a.qagVendida(t, u.Token, p.ID, hospede.ID, de, ate)
	original := qagPorTipo(t, a.qagDaReserva(t, u.Token, r.ID))
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+original["checkin"].ID.String(), u.Token, map[string]any{
		"starts_at": casa.rfc(de, "16:00"), "ends_at": casa.rfc(de, "16:30"), "notes": "chega de carro", "assignee_id": u.ID,
	}), http.StatusOK, "ajustando o check-in à mão")
	jantar := a.qagMarcar(t, u.Token, map[string]any{"type": "evento", "title": "Jantar de boas-vindas",
		"reservation_id": r.ID, "starts_at": casa.rfc(de, "19:00"), "ends_at": casa.rfc(de, "20:00")}).Data

	novoDe, novoAte := casa.Hoje.AddDays(90), casa.Hoje.AddDays(94)
	resp := a.chamarComChave(t, http.MethodPost, "/reservations/"+r.ID.String()+"/reschedule", u.Token,
		map[string]any{"check_in": novoDe.String(), "check_out": novoAte.String()}, a.chaveNova(t, "agenda-remarcar"))
	nova := qbDado[qagReserva](t, resp, http.StatusCreated, "POST /reschedule")

	velhos := a.qagDaReserva(t, u.Token, r.ID)
	if len(velhos) != 4 {
		t.Fatalf("a original guarda os três automáticos e o jantar: %d", len(velhos))
	}
	for _, e := range velhos {
		if e.Status != "cancelado" {
			t.Errorf("original: %s %q ficou %s", e.Origem, e.Titulo, e.Status)
		}
	}
	if e, ok := qagAchar(velhos, jantar.ID); !ok || e.Status != "cancelado" {
		t.Errorf("o manual ligado à original é cancelado com ela: %+v", e)
	}

	novos := a.qagDaReserva(t, u.Token, nova.ID)
	if len(novos) != 3 {
		t.Fatalf("a nova nasce confirmada com exatamente os três dela — nada copiado da original; vieram %d: %+v", len(novos), novos)
	}
	tipos := qagPorTipo(t, novos)
	for tipo, janela := range qagEsperado(casa, h, nova) {
		e := tipos[tipo]
		qagExigirJanela(t, e, janela[0], janela[1], tipo+" da nova")
		if e.Status != "agendado" || e.Notas != nil || e.ResponsavelID != nil {
			t.Errorf("%s da nova: o ajuste da original não viaja — status %s, notas %v, responsável %v", tipo, e.Status, e.Notas, e.ResponsavelID)
		}
	}
	if nova.CheckIn != novoDe.String() || nova.CheckOut != novoAte.String() {
		t.Errorf("a nova reserva saiu com as datas %s–%s", nova.CheckIn, nova.CheckOut)
	}
}

// Em linguagem de negócio: o hóspede chegou às 19h12 (atrasado para o check-in
// das 14h) e saiu às 10h05 do dia combinado. A agenda registra os dois como
// feitos, no instante informado na reserva, sem redesenhar o horário
// combinado; a limpeza continua por fazer.
func TestAgendaQAEntradaESaidaRegistradasConcluemOsAutomaticos(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	h := a.qagHorariosDaCasa(t, u.Token)
	p := f.produto(t, casa.Prop, "one_member", 1)
	hospede := f.contato(t, casa.Prop)
	de, ate := casa.Hoje.AddDays(100), casa.Hoje.AddDays(103)

	r := a.qagVendida(t, u.Token, p.ID, hospede.ID, de, ate)
	esperado := qagEsperado(casa, h, r)

	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/check-in", u.Token,
		map[string]any{"at": casa.rfc(de, "19:12")}), http.StatusOK, "check-in")
	tipos := qagPorTipo(t, a.qagDaReserva(t, u.Token, r.ID))
	ci := tipos["checkin"]
	if ci.Status != "concluido" || ci.ConcluidoEm == nil || *ci.ConcluidoEm != casa.rfc(de, "19:12") {
		t.Fatalf("check-in: %s em %v, esperado concluido em %s", ci.Status, ci.ConcluidoEm, casa.rfc(de, "19:12"))
	}
	qagExigirJanela(t, ci, esperado["checkin"][0], esperado["checkin"][1], "o atrasado continua no horário combinado")
	if ci.Editavel != "so_notas" || len(ci.Acoes) != 0 {
		t.Errorf("check-in concluído: editable %s, ações %v", ci.Editavel, ci.Acoes)
	}
	if tipos["checkout"].Status != "agendado" || tipos["limpeza"].Status != "agendado" {
		t.Errorf("a entrada só conclui o check-in: check-out %s, limpeza %s", tipos["checkout"].Status, tipos["limpeza"].Status)
	}

	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/check-out", u.Token,
		map[string]any{"at": casa.rfc(ate, "10:05")}), http.StatusOK, "check-out no dia combinado")
	tipos = qagPorTipo(t, a.qagDaReserva(t, u.Token, r.ID))
	co := tipos["checkout"]
	if co.Status != "concluido" || co.ConcluidoEm == nil || *co.ConcluidoEm != casa.rfc(ate, "10:05") {
		t.Fatalf("check-out: %s em %v, esperado concluido em %s", co.Status, co.ConcluidoEm, casa.rfc(ate, "10:05"))
	}
	qagExigirJanela(t, co, esperado["checkout"][0], esperado["checkout"][1], "a saída no dia não redesenha o check-out")
	l := tipos["limpeza"]
	if l.Status != "agendado" {
		t.Fatalf("a limpeza continua por fazer: %s", l.Status)
	}
	qagExigirJanela(t, l, esperado["limpeza"][0], esperado["limpeza"][1], "a limpeza fica na janela do dia da saída")
}

// Em linguagem de negócio: a reserva trocou de apartamento (AP-1 → AP-2). O
// check-in, o check-out e a limpeza já aparecem no apartamento novo, porque
// leem a unidade da reserva. A faxina marcada à mão no apartamento velho muda
// junto; e, se no apartamento novo já houver outra marcação à mão no mesmo
// horário, a troca NÃO é recusada por causa da agenda: o evento segue a
// reserva sem unidade própria e a lista mostra o conflito para a operação
// resolver (contrato de `/reassign-unit`, "Na agenda").
func TestAgendaQARealocarLevaOsAutomaticosENaoTravaPelaAgenda(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 2)
	hospede := f.contato(t, casa.Prop)
	de, ate := casa.Hoje.AddDays(110), casa.Hoje.AddDays(113)

	r := a.qagVendida(t, u.Token, p.ID, hospede.ID, de, ate)
	if len(r.Unidades) != 1 {
		t.Fatalf("o produto one_member aloca uma unidade: %+v", r.Unidades)
	}
	origem := r.Unidades[0].ID
	destino := p.Unidades[0]
	if destino == origem {
		destino = p.Unidades[1]
	}
	meio := de.AddDays(1)

	colide := a.qagMarcar(t, u.Token, map[string]any{"type": "limpeza", "title": "Faxina do meio da estadia",
		"reservation_id": r.ID, "unit_id": origem,
		"starts_at": casa.rfc(meio, "10:00"), "ends_at": casa.rfc(meio, "11:00")}).Data
	cabe := a.qagMarcar(t, u.Token, map[string]any{"type": "visita", "title": "Vistoria do ar-condicionado",
		"reservation_id": r.ID, "unit_id": origem,
		"starts_at": casa.rfc(meio, "16:00"), "ends_at": casa.rfc(meio, "16:30")}).Data
	// A vistoria de ontem, ligada à reserva e já feita, aconteceu onde aconteceu.
	feita := a.qagMarcar(t, u.Token, map[string]any{"type": "visita", "title": "Vistoria de ontem", "reservation_id": r.ID,
		"unit_id": origem, "starts_at": casa.rfc(casa.Hoje.AddDays(-1), "10:00"), "ends_at": casa.rfc(casa.Hoje.AddDays(-1), "10:30")}).Data
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/agenda/events/"+feita.ID.String()+"/complete", u.Token, nil), http.StatusOK, "concluindo a vistoria")
	outra := a.qagMarcar(t, u.Token, map[string]any{"type": "visita", "title": "Visita de outro cliente",
		"unit_id": destino, "starts_at": casa.rfc(meio, "10:30"), "ends_at": casa.rfc(meio, "11:30")}).Data

	resp := a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/reassign-unit", u.Token, map[string]any{"to_unit_id": destino})
	if resp.Status != http.StatusOK {
		t.Fatalf("DEFEITO — a agenda travou a realocação: status %d (o contrato de /reassign-unit diz que a agenda nunca a trava; "+
			"o evento que cai sobre outra marcação manual muda sem unidade própria) — %s", resp.Status, resp.Corpo)
	}
	depois := qbDado[qagReserva](t, a.chamar(t, http.MethodGet, "/reservations/"+r.ID.String(), u.Token, nil), http.StatusOK, "GET /reservations/{id}")
	if len(depois.Unidades) != 1 || depois.Unidades[0].ID != destino {
		t.Fatalf("a reserva tem de estar na unidade nova: %+v", depois.Unidades)
	}

	for tipo, e := range qagPorTipo(t, a.qagDaReserva(t, u.Token, r.ID)) {
		if e.UnidadeID != nil || len(e.Unidades) != 1 || e.Unidades[0].ID != destino {
			t.Errorf("automático %s: já aparece na unidade nova (lê da reserva): unit_id %v, units %+v", tipo, e.UnidadeID, e.Unidades)
		}
	}
	if e := a.qagLer(t, u.Token, colide.ID); e.UnidadeID != nil || e.Status != "agendado" || len(e.Unidades) != 1 ||
		e.Unidades[0].ID != destino || !e.Conflito {
		t.Errorf("o manual que caiu sobre outra marcação segue a reserva SEM unidade própria e mostra o conflito: "+
			"unit_id %v, status %s, units %+v, has_unit_conflict %v", e.UnidadeID, e.Status, e.Unidades, e.Conflito)
	}
	if e := a.qagLer(t, u.Token, cabe.ID); e.UnidadeID == nil || *e.UnidadeID != destino || e.Conflito {
		t.Errorf("o manual preso à unidade que saiu e que cabe na nova muda junto: unit_id %v, conflito %v", e.UnidadeID, e.Conflito)
	}
	if e := a.qagLer(t, u.Token, outra.ID); e.UnidadeID == nil || *e.UnidadeID != destino || e.Status != "agendado" || !e.Conflito {
		t.Errorf("a outra marcação fica onde estava e também mostra o conflito: unit_id %v, status %s, conflito %v", e.UnidadeID, e.Status, e.Conflito)
	}
	if e := a.qagLer(t, u.Token, feita.ID); e.UnidadeID == nil || *e.UnidadeID != origem || e.Status != "concluido" {
		t.Errorf("o concluído fica onde aconteceu: unit_id %v, status %s", e.UnidadeID, e.Status)
	}
	// A visão de calendário da unidade nova mostra os dois em conflito.
	marcados := 0
	for _, e := range a.qagJanela(t, u.Token, meio, meio.AddDays(1), "unit_id="+destino.String()) {
		if (e.ID == colide.ID || e.ID == outra.ID) && e.Conflito {
			marcados++
		}
	}
	if marcados != 2 {
		t.Errorf("a lista da unidade nova mostra os dois eventos em conflito; marcou %d", marcados)
	}
}

// Em linguagem de negócio: a White House Completa ocupa a casa inteira, mas é
// UMA reserva — um check-in, um check-out e uma limpeza, cada um mostrando
// todas as unidades, e não três eventos por apartamento. O casamento do grupo
// que alugou a casa (evento ligado à reserva, sem unidade) ocupa a casa toda e
// não colide com a estadia do próprio grupo; a visita de terceiro numa das
// unidades, sim.
func TestAgendaQACompletaTemTresEventosENaoTresPorUnidade(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "all_members", 3)
	hospede := f.contato(t, casa.Prop)
	de, ate := casa.Hoje.AddDays(120), casa.Hoje.AddDays(123)

	r := a.qagVendida(t, u.Token, p.ID, hospede.ID, de, ate)
	if len(r.Unidades) != 3 {
		t.Fatalf("a Completa aloca as três unidades: %+v", r.Unidades)
	}
	todas := strings.Join(sortedCopy(p.Codigos), ",")
	evs := a.qagDaReserva(t, u.Token, r.ID)
	if len(evs) != 3 {
		t.Fatalf("a Completa tem três eventos, não três por unidade: vieram %d", len(evs))
	}
	for tipo, e := range qagPorTipo(t, evs) {
		if e.codigos() != todas {
			t.Errorf("%s da Completa mostra todas as unidades (%s): %s", tipo, todas, e.codigos())
		}
	}
	if n := a.qagContar(t, `SELECT count(*) FROM agenda_events WHERE reservation_id = $1`, r.ID); n != 3 {
		t.Fatalf("no banco: %d linhas, esperado 3", n)
	}

	casamento := a.qagMarcar(t, u.Token, map[string]any{"type": "evento", "title": "Casamento do grupo",
		"reservation_id": r.ID, "starts_at": casa.rfc(de.AddDays(1), "17:00"), "ends_at": casa.rfc(de.AddDays(1), "23:00")}).Data
	if casamento.codigos() != todas || casamento.UnidadeID != nil || casamento.Conflito {
		t.Errorf("o evento do grupo ocupa a casa inteira sem colidir com a própria estadia: units %s, unit_id %v, conflito %v",
			casamento.codigos(), casamento.UnidadeID, casamento.Conflito)
	}
	for i, unidade := range p.Unidades {
		if _, ok := qagAchar(a.qagJanela(t, u.Token, de.AddDays(1), de.AddDays(2), "unit_id="+unidade.String()), casamento.ID); !ok {
			t.Errorf("o evento do grupo ocupa a casa inteira: falta na agenda da unidade %s", p.Codigos[i])
		}
	}
	r409 := a.chamar(t, http.MethodPost, "/agenda/events", u.Token, map[string]any{"type": "visita", "title": "Visita de terceiro",
		"unit_id": p.Unidades[1], "starts_at": casa.rfc(de.AddDays(1), "10:00"), "ends_at": casa.rfc(de.AddDays(1), "11:00")})
	qbErro(t, r409, http.StatusConflict, "UNIT_NOT_AVAILABLE", "visita de terceiro dentro da Completa")
}

func sortedCopy(xs []string) []string {
	out := append([]string(nil), xs...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// qagMesmoJSON compara dois corpos pelo CONTEÚDO: a resposta guardada pela
// idempotência volta do jsonb com outra ordem de chaves e outro espaçamento.
func qagMesmoJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatalf("corpo 1 não é JSON: %v", err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatalf("corpo 2 não é JSON: %v", err)
	}
	return reflect.DeepEqual(x, y)
}

// Em linguagem de negócio: a reserva trocou de titular (quem pagou não é quem
// vem) e, depois, o nome do novo titular foi corrigido. O check-in, o
// check-out e a limpeza mostram na hora o titular e o nome atuais — a agenda
// não guarda cópia da pessoa de antes. A visita marcada com o cliente dela
// continua com o cliente dela.
func TestAgendaQATrocarOTitularOuONomeApareceNaHora(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "ag-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qagFaxina(t, admin)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	tk := admin.Token
	p := f.produto(t, casa.Prop, "one_member", 1)
	quemPagou := f.contato(t, casa.Prop)
	quemVem := f.contato(t, casa.Prop)
	cliente := f.contato(t, casa.Prop)
	d := casa.Hoje.AddDays(125)

	r := a.qagVendida(t, tk, p.ID, quemPagou.ID, d, d.AddDays(2))
	visita := a.qagMarcar(t, tk, map[string]any{"type": "visita", "title": "Visita com o cliente", "reservation_id": r.ID,
		"contact_id": cliente.ID, "starts_at": casa.rfc(d.AddDays(1), "10:00"), "ends_at": casa.rfc(d.AddDays(1), "10:30")}).Data
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/reservations/"+r.ID.String(), tk, map[string]any{"contact_id": quemVem.ID}),
		http.StatusOK, "trocando o titular")
	novoNome := "Titular Corrigido " + qbSufixo()
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/contacts/"+quemVem.ID.String(), tk, map[string]any{"name": novoNome}),
		http.StatusOK, "corrigindo o nome do titular")

	for tipo, e := range qagPorTipo(t, a.qagDaReserva(t, tk, r.ID)) {
		if e.ContatoID == nil || *e.ContatoID != quemVem.ID || e.ContatoNome == nil || *e.ContatoNome != novoNome {
			t.Errorf("%s: mostra o titular atual (%s, %q); veio %v %v", tipo, quemVem.ID, novoNome, e.ContatoID, e.ContatoNome)
		}
	}
	if v := a.qagLer(t, tk, visita.ID); v.ContatoID == nil || *v.ContatoID != cliente.ID {
		t.Errorf("a visita continua com o cliente dela: %v", v.ContatoID)
	}
}
