//go:build integration

package router

import (
	"net/http"
	"testing"
	"time"
)

// O que se faz com um evento, estado a estado (contrato: `allowed_actions`,
// `editable`, `/complete`, `DELETE`, `PUT`, `PATCH`). Estado só muda por ação
// nomeada; o automático segue a reserva; o fechado não reabre.

// Em linguagem de negócio: a visita marcada à mão tem botões diferentes
// conforme o dia — a de hoje e a de ontem podem ser dadas por feitas ou
// desmarcadas; a de amanhã, só desmarcada. Feita, ela só aceita anotação
// ("faltou toalha"); desmarcada, nada. E o formulário que reenvia o evento
// inteiro como foi lido, mudando só a anotação, passa no concluído.
func TestAgendaQAEstadosDoEventoMarcadoAMao(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	unidade, _ := f.unidade(t, casa.Prop)
	contato := f.contato(t, casa.Prop)
	tk := u.Token
	hoje, ontem, amanha := casa.Hoje, casa.Hoje.AddDays(-1), casa.Hoje.AddDays(1)

	marcar := func(titulo string, dia string, deHora, ateHora string) qagEvento {
		d := casa.Hoje
		switch dia {
		case "ontem":
			d = ontem
		case "amanhã":
			d = amanha
		}
		return a.qagMarcar(t, tk, map[string]any{"type": "visita", "title": titulo, "unit_id": unidade, "contact_id": contato.ID,
			"assignee_id": u.ID, "starts_at": casa.rfc(d, deHora), "ends_at": casa.rfc(d, ateHora)}).Data
	}
	deHoje := marcar("Visita de hoje", "hoje", "09:00", "10:00")
	deOntem := marcar("Visita de ontem", "ontem", "09:00", "10:00")
	deAmanha := marcar("Visita de amanhã", "amanhã", "09:00", "10:00")
	for _, c := range []struct {
		e     qagEvento
		acoes string
	}{{deHoje, "complete,cancel"}, {deOntem, "complete,cancel"}, {deAmanha, "cancel"}} {
		if c.e.acoes() != c.acoes || c.e.Editavel != "tudo" || c.e.Status != "agendado" {
			t.Errorf("%q: ações [%s] editable %s, esperado [%s] e tudo", c.e.Titulo, c.e.acoes(), c.e.Editavel, c.acoes)
		}
	}

	// Amanhã ainda não aconteceu.
	e := qbErro(t, a.chamar(t, http.MethodPost, "/agenda/events/"+deAmanha.ID.String()+"/complete", tk, nil),
		http.StatusConflict, "INVALID_STATE_TRANSITION", "concluindo a visita de amanhã")
	if e.Details["reason"] != "nao_comecou" || e.Details["allowed"] == nil {
		t.Errorf("details do nao_comecou: %v", e.Details)
	}

	// Concluída: só a anotação.
	caminho := "/agenda/events/" + deHoje.ID.String()
	feita := qbDado[qagEvento](t, a.chamar(t, http.MethodPost, caminho+"/complete", tk, nil), http.StatusOK, "concluindo")
	if feita.Status != "concluido" || feita.ConcluidoEm == nil || feita.ConcluidoPor == nil || *feita.ConcluidoPor != u.ID ||
		feita.Editavel != "so_notas" || len(feita.Acoes) != 0 || feita.CanceladoEm != nil {
		t.Fatalf("concluída: %+v", feita)
	}
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, caminho, tk, map[string]any{"notes": "faltou toalha"}), http.StatusOK, "anotando a concluída")
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, caminho, tk, map[string]any{}), http.StatusOK, "PATCH {} na concluída")
	eco := map[string]any{"type": feita.Tipo, "title": feita.Titulo, "starts_at": feita.Inicio, "ends_at": feita.Fim, "all_day": feita.DiaInteiro,
		"unit_id": feita.UnidadeID, "reservation_id": feita.ReservaID, "contact_id": feita.ContatoID, "assignee_id": feita.ResponsavelID,
		"notes": "faltou toalha e sabonete"}
	if r := a.chamar(t, http.MethodPut, caminho, tk, eco); r.Status != http.StatusOK {
		t.Errorf("PUT na concluída repetindo tudo e mudando só a anotação: o contrato diz que passa (so_notas); veio %d — %s", r.Status, r.Corpo)
	}
	for nome, corpo := range map[string]map[string]any{
		"PATCH do horário":                  {"starts_at": casa.rfc(hoje, "08:00")},
		"PATCH do título":                   {"title": "Outro título"},
		"PATCH do responsável para ninguém": {"assignee_id": nil},
	} {
		e := qbErro(t, a.chamar(t, http.MethodPatch, caminho, tk, corpo), http.StatusConflict, "AGENDA_EVENT_CLOSED", nome+" na concluída")
		if e.Details["status"] != "concluido" || e.Details["editable"] != "so_notas" {
			t.Errorf("%s: details %v", nome, e.Details)
		}
	}
	eco["title"] = "Outro título"
	qbErro(t, a.chamar(t, http.MethodPut, caminho, tk, eco), http.StatusConflict, "AGENDA_EVENT_CLOSED", "PUT mudando o título da concluída")
	qbErro(t, a.chamar(t, http.MethodPost, caminho+"/complete", tk, nil), http.StatusConflict, "AGENDA_EVENT_CLOSED", "concluindo de novo")
	qbErro(t, a.chamar(t, http.MethodDelete, caminho, tk, nil), http.StatusConflict, "AGENDA_EVENT_CLOSED", "desmarcando a concluída")

	// Desmarcada: nada, e continua na agenda.
	caminho = "/agenda/events/" + deAmanha.ID.String()
	desmarcada := qbDado[qagEvento](t, a.chamar(t, http.MethodDelete, caminho, tk, nil), http.StatusOK, "desmarcando")
	if desmarcada.Status != "cancelado" || desmarcada.CanceladoEm == nil || desmarcada.Editavel != "nada" || len(desmarcada.Acoes) != 0 {
		t.Fatalf("desmarcada: %+v", desmarcada)
	}
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, caminho, tk, map[string]any{}), http.StatusOK, "PATCH {} na desmarcada")
	e = qbErro(t, a.chamar(t, http.MethodPatch, caminho, tk, map[string]any{"notes": "x"}), http.StatusConflict, "AGENDA_EVENT_CLOSED", "anotando a desmarcada")
	if e.Details["status"] != "cancelado" || e.Details["editable"] != "nada" {
		t.Errorf("details: %v", e.Details)
	}
	qbErro(t, a.chamar(t, http.MethodPut, caminho, tk, map[string]any{"type": "visita", "title": "Visita de amanhã",
		"starts_at": desmarcada.Inicio, "ends_at": desmarcada.Fim}), http.StatusConflict, "AGENDA_EVENT_CLOSED", "PUT na desmarcada")
	qbErro(t, a.chamar(t, http.MethodDelete, caminho, tk, nil), http.StatusConflict, "AGENDA_EVENT_CLOSED", "desmarcando de novo")
	qbErro(t, a.chamar(t, http.MethodPost, caminho+"/complete", tk, nil), http.StatusConflict, "AGENDA_EVENT_CLOSED", "concluindo a desmarcada")
	if n := a.qagContar(t, `SELECT count(*) FROM agenda_events WHERE id = $1 AND status = 'cancelado'`, deAmanha.ID); n != 1 {
		t.Fatal("desmarcar não apaga")
	}
	// A desmarcada não ocupa mais: a visita seguinte no mesmo horário passa.
	a.qagMarcar(t, tk, map[string]any{"type": "visita", "title": "No lugar da desmarcada", "unit_id": unidade,
		"starts_at": casa.rfc(amanha, "09:00"), "ends_at": casa.rfc(amanha, "10:00")})
}

// Em linguagem de negócio: o check-in e o check-out da reserva só se movem
// pela reserva — registrar a entrada e a saída é na reserva, e eles não saem
// da agenda por aqui. A limpeza da reserva quem limpa dá por feita no celular,
// mas não se desmarca. Do automático, a operação muda o horário (check-in e
// check-out no mesmo dia), o responsável e a anotação; o resto é da reserva.
func TestAgendaQAAutomaticosSeguemAReserva(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 2)
	hospede := f.contato(t, casa.Prop)
	outro := f.contato(t, casa.Prop)
	tk := u.Token
	de, ate := casa.Hoje.AddDays(140), casa.Hoje.AddDays(143)

	r := a.qagVendida(t, tk, p.ID, hospede.ID, de, ate)
	outraReserva := a.qagVendida(t, tk, p.ID, outro.ID, ate.AddDays(5), ate.AddDays(7))
	ev := qagPorTipo(t, a.qagDaReserva(t, tk, r.ID))
	ci, co, lp := ev["checkin"], ev["checkout"], ev["limpeza"]
	outraUnidade := p.Unidades[0]
	if outraUnidade == r.Unidades[0].ID {
		outraUnidade = p.Unidades[1]
	}

	for _, x := range []qagEvento{ci, co} {
		e := qbErro(t, a.chamar(t, http.MethodPost, "/agenda/events/"+x.ID.String()+"/complete", tk, nil),
			http.StatusConflict, "INVALID_STATE_TRANSITION", "concluindo o "+x.Tipo+" por aqui")
		if e.Details["reason"] != "segue_a_reserva" || e.Details["allowed"] == nil {
			t.Errorf("%s/complete: details %v", x.Tipo, e.Details)
		}
	}
	for _, x := range []qagEvento{ci, co, lp} {
		e := qbErro(t, a.chamar(t, http.MethodDelete, "/agenda/events/"+x.ID.String(), tk, nil),
			http.StatusConflict, "INVALID_STATE_TRANSITION", "desmarcando o "+x.Tipo)
		if e.Details["reason"] != "segue_a_reserva" {
			t.Errorf("DELETE %s: details %v", x.Tipo, e.Details)
		}
	}
	e := qbErro(t, a.chamar(t, http.MethodPost, "/agenda/events/"+lp.ID.String()+"/complete", tk, nil),
		http.StatusConflict, "INVALID_STATE_TRANSITION", "concluindo a limpeza do mês que vem")
	if e.Details["reason"] != "nao_comecou" {
		t.Errorf("limpeza futura: details %v", e.Details)
	}

	// O que é da reserva não muda por aqui — diferente é 422 no campo.
	caminho := "/agenda/events/" + ci.ID.String()
	for campo, valor := range map[string]any{
		"title": "Outro título", "type": "visita", "all_day": true, "unit_id": outraUnidade,
		"reservation_id": outraReserva.ID, "contact_id": outro.ID,
	} {
		e := qbErro(t, a.chamar(t, http.MethodPatch, caminho, tk, map[string]any{campo: valor}), http.StatusUnprocessableEntity,
			"VALIDATION_ERROR", "mudando "+campo+" do check-in automático")
		if e.Details[campo] == nil {
			t.Errorf("o 422 não nomeia %s: %v", campo, e.Details)
		}
	}
	// Repetido passa: o formulário que reenvia o que leu.
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, caminho, tk, map[string]any{
		"title": ci.Titulo, "unit_id": r.Unidades[0].ID, "reservation_id": r.ID, "contact_id": hospede.ID, "all_day": false,
	}), http.StatusOK, "reenviando o automático como foi lido")
	exigirStatusQB(t, a.chamar(t, http.MethodPut, "/agenda/events/"+lp.ID.String(), tk, map[string]any{
		"type": "limpeza", "title": lp.Titulo, "starts_at": lp.Inicio, "ends_at": lp.Fim, "unit_id": r.Unidades[0].ID,
		"reservation_id": r.ID, "contact_id": hospede.ID, "all_day": false,
	}), http.StatusOK, "PUT da limpeza automática repetindo o que leu")

	// Check-in muda de hora, não de dia.
	qbErro(t, a.chamar(t, http.MethodPatch, caminho, tk, map[string]any{
		"starts_at": casa.rfc(de.AddDays(1), "14:00"), "ends_at": casa.rfc(de.AddDays(1), "14:30")}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "check-in em outro dia")
	qbErro(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+co.ID.String(), tk, map[string]any{
		"starts_at": casa.rfc(ate, "23:30"), "ends_at": casa.rfc(ate.AddDays(1), "00:30")}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "check-out terminando no dia seguinte")
	ok := qagEscritaDe(t, a.chamar(t, http.MethodPatch, caminho, tk, map[string]any{
		"starts_at": casa.rfc(de, "17:00"), "ends_at": casa.rfc(de, "17:30"), "assignee_id": u.ID, "notes": "late check-in"}))
	if ok.Data.Inicio != casa.rfc(de, "17:00") || ok.Data.ResponsavelID == nil || ok.Data.Notas == nil || ok.Data.Editavel != "segue_a_reserva" {
		t.Errorf("horário, responsável e anotação do automático: %+v", ok.Data)
	}

	// A limpeza nunca antes do fim do check-out; depois, inclusive outro dia.
	qbErro(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+lp.ID.String(), tk, map[string]any{
		"starts_at": casa.rfc(ate, "10:00"), "ends_at": casa.rfc(ate, "12:00")}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "limpeza antes de o hóspede sair")
	// Mover o check-out para depois cruza a limpeza: 409, e nada muda — a
	// ordem certa é mover a limpeza primeiro.
	r409 := a.chamar(t, http.MethodPatch, "/agenda/events/"+co.ID.String(), tk, map[string]any{
		"starts_at": casa.rfc(ate, "11:30"), "ends_at": casa.rfc(ate, "12:00")})
	qbErro(t, r409, http.StatusConflict, "UNIT_NOT_AVAILABLE", "check-out por cima da limpeza")
	if cs := qagConflitos(t, r409); len(cs) != 1 || cs[0]["kind"] != "limpeza" {
		t.Errorf("o conflito do check-out tardio é a limpeza: %v", cs)
	}
	if x := a.qagLer(t, tk, co.ID); x.Inicio != co.Inicio || x.Fim != co.Fim {
		t.Fatalf("o 409 mudou o check-out: %s–%s", x.Inicio, x.Fim)
	}
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+lp.ID.String(), tk, map[string]any{
		"starts_at": casa.rfc(ate.AddDays(1), "09:00"), "ends_at": casa.rfc(ate.AddDays(1), "12:00")}), http.StatusOK,
		"limpeza para amanhã (a equipe só vem no dia seguinte)")
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+co.ID.String(), tk, map[string]any{
		"starts_at": casa.rfc(ate, "11:30"), "ends_at": casa.rfc(ate, "12:00")}), http.StatusOK, "check-out tardio depois de mover a limpeza")
}

// Em linguagem de negócio: o hóspede saiu dois dias antes do combinado. A
// limpeza vem para hoje — e quem limpa a dá por feita no celular. A limpeza da
// reserva não se desmarca; feita, só aceita anotação.
func TestAgendaQALimpezaDaSaidaAntecipadaSeConcluiHoje(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	hospede := f.contato(t, casa.Prop)
	tk := u.Token
	h := a.qagHorariosDaCasa(t, tk)

	r := a.qagVendida(t, tk, p.ID, hospede.ID, casa.Hoje, casa.Hoje.AddDays(3))
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/check-in", tk, map[string]any{}), http.StatusOK, "check-in agora")
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/check-out", tk, map[string]any{}), http.StatusOK, "saída antecipada agora")

	ev := qagPorTipo(t, a.qagDaReserva(t, tk, r.ID))
	lp := ev["limpeza"]
	qagExigirJanela(t, lp, casa.rfc(casa.Hoje, h.LimpezaDe), casa.rfc(casa.Hoje, h.LimpezaAte), "a limpeza vem para o dia em que a unidade esvaziou")
	if lp.Status != "agendado" || lp.acoes() != "complete" || lp.Editavel != "segue_a_reserva" {
		t.Fatalf("limpeza de hoje: status %s, ações [%s] (o contrato: só complete), editable %s", lp.Status, lp.acoes(), lp.Editavel)
	}
	co := ev["checkout"]
	if co.Status != "concluido" || co.Data != casa.Hoje.String() {
		t.Errorf("o check-out é desenhado no dia em que aconteceu: %s em %s", co.Status, co.Data)
	}
	qbErro(t, a.chamar(t, http.MethodDelete, "/agenda/events/"+lp.ID.String(), tk, nil), http.StatusConflict, "INVALID_STATE_TRANSITION",
		"desmarcando a limpeza da reserva")
	// A equipe só chega mais tarde: a limpeza vai para depois, no mesmo dia.
	maisTarde := a.chamar(t, http.MethodPatch, "/agenda/events/"+lp.ID.String(), tk, map[string]any{
		"starts_at": casa.rfc(casa.Hoje, qagSomar(h.LimpezaAte, 60)), "ends_at": casa.rfc(casa.Hoje, qagSomar(h.LimpezaAte, 180))})
	exigirStatusQB(t, maisTarde, http.StatusOK, "limpeza da saída antecipada para mais tarde hoje")
	feita := qbDado[qagEvento](t, a.chamar(t, http.MethodPost, "/agenda/events/"+lp.ID.String()+"/complete", tk, nil), http.StatusOK,
		"concluindo a limpeza de hoje")
	if feita.Status != "concluido" || feita.ConcluidoPor == nil || *feita.ConcluidoPor != u.ID || feita.Editavel != "so_notas" {
		t.Errorf("limpeza concluída: %+v", feita)
	}
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+lp.ID.String(), tk, map[string]any{"notes": "toalhas repostas"}),
		http.StatusOK, "anotando a limpeza feita")
	qbErro(t, a.chamar(t, http.MethodPatch, "/agenda/events/"+lp.ID.String(), tk, map[string]any{
		"starts_at": casa.rfc(casa.Hoje, "15:00"), "ends_at": casa.rfc(casa.Hoje, "16:00")}),
		http.StatusConflict, "AGENDA_EVENT_CLOSED", "mudando o horário da limpeza feita")
}

// Em linguagem de negócio: o hóspede estava dentro e a reserva foi cancelada
// hoje. O contrato manda a limpeza para HOJE (as noites restantes voltaram à
// venda). A equipe só chega às 15:00 e a gestão tenta mover a limpeza para as
// 15:00 de hoje — mais tarde do que estava, com a unidade vazia. O contrato
// diz que a limpeza "pode ir para depois", nunca para antes do fim do
// check-out; aqui o check-out foi CANCELADO (o hóspede saiu pelo
// cancelamento), e o que vale é a unidade vazia hoje — e não o dia em que a
// estadia acabaria se não tivesse sido cancelada.
func TestAgendaQALimpezaQueOCancelamentoTrouxeParaHojeAindaSeAjusta(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	hospede := f.contato(t, casa.Prop)
	tk := u.Token
	h := a.qagHorariosDaCasa(t, tk)

	r := a.qagVendida(t, tk, p.ID, hospede.ID, casa.Hoje, casa.Hoje.AddDays(3))
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/check-in", tk, map[string]any{}), http.StatusOK, "check-in agora")
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/reservations/"+r.ID.String()+"/cancel", tk, map[string]any{"reason": "hóspede foi embora"}),
		http.StatusOK, "cancelando com o hóspede dentro")

	ev := qagPorTipo(t, a.qagDaReserva(t, tk, r.ID))
	if ev["checkout"].Status != "cancelado" || ev["checkin"].Status != "concluido" {
		t.Fatalf("check-in %s, check-out %s", ev["checkin"].Status, ev["checkout"].Status)
	}
	lp := ev["limpeza"]
	qagExigirJanela(t, lp, casa.rfc(casa.Hoje, h.LimpezaDe), casa.rfc(casa.Hoje, h.LimpezaAte), "a limpeza vem para hoje")
	if lp.Status != "agendado" || lp.Editavel != "segue_a_reserva" {
		t.Fatalf("a limpeza de hoje continua por fazer e ajustável: %s %s", lp.Status, lp.Editavel)
	}

	// O limite, com o check-out cancelado, é o INSTANTE do cancelamento (a
	// unidade esvaziou ali). Rodando depois das 15h da casa, "fim da limpeza +
	// 1h" já é antes desse instante — e o 422 seria o certo. Por isso o novo
	// início é o maior entre 15h e a próxima hora cheia depois de agora: o
	// teste não depende do relógio em que roda.
	var agora time.Time
	if err := a.pool.QueryRow(a.ctx, `SELECT now()`).Scan(&agora); err != nil {
		t.Fatal(err)
	}
	ini := casa.em(casa.Hoje, qagSomar(h.LimpezaAte, 60))
	if depois := agora.In(casa.Loc).Truncate(time.Hour).Add(time.Hour); depois.After(ini) {
		ini = depois
	}
	inicio, fim := ini.Format(time.RFC3339), ini.Add(2*time.Hour).Format(time.RFC3339)
	resp := a.chamar(t, http.MethodPatch, "/agenda/events/"+lp.ID.String(), tk, map[string]any{"starts_at": inicio, "ends_at": fim})
	if resp.Status != http.StatusOK {
		t.Fatalf("DEFEITO — a limpeza que o cancelamento trouxe para hoje não pode ir para mais tarde HOJE: "+
			"PATCH %s → %s respondeu %d (o limite usado é o check-out ORIGINAL da reserva cancelada, %s) — %s",
			inicio, fim, resp.Status, r.CheckOut, resp.Corpo)
	}
	if e := qagEscritaDe(t, resp).Data; e.Inicio != inicio || e.Fim != fim {
		t.Errorf("a limpeza ajustada: %s–%s", e.Inicio, e.Fim)
	}
}

// Em linguagem de negócio: o check-in da reserva mostra o titular (lido da
// reserva). O formulário que manda "sem cliente" (`contact_id: null`) está
// pedindo uma mudança que o automático não aceita — o contrato diz que, no
// automático, o `contact_id` repetido passa e o DIFERENTE é 422 no campo.
// Responder 200 sem mudar nada é dizer à tela que o titular foi tirado quando
// não foi.
func TestAgendaQAAutomaticoNaoFingeQueTirouOTitular(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "ag-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	hospede := f.contato(t, casa.Prop)
	tk := u.Token
	d := casa.Hoje.AddDays(145)

	r := a.qagVendida(t, tk, p.ID, hospede.ID, d, d.AddDays(2))
	ci := qagPorTipo(t, a.qagDaReserva(t, tk, r.ID))["checkin"]
	if ci.ContatoID == nil || *ci.ContatoID != hospede.ID {
		t.Fatalf("premissa: o check-in mostra o titular: %v", ci.ContatoID)
	}
	for nome, resp := range map[string]resposta{
		"PATCH": a.chamar(t, http.MethodPatch, "/agenda/events/"+ci.ID.String(), tk, map[string]any{"contact_id": nil}),
		"PUT": a.chamar(t, http.MethodPut, "/agenda/events/"+ci.ID.String(), tk, map[string]any{
			"starts_at": ci.Inicio, "ends_at": ci.Fim, "contact_id": nil}),
	} {
		if resp.Status != http.StatusUnprocessableEntity {
			depois := a.qagLer(t, tk, ci.ID)
			t.Errorf("%s {contact_id: null} no check-in automático: status %d (o contrato pede 422 no campo — o titular é da reserva); "+
				"depois da resposta o evento ainda mostra contact_id %v — %s", nome, resp.Status, depois.ContatoID, resp.Corpo)
		}
	}
}
