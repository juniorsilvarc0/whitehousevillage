//go:build integration

package router

import (
	"net/http"
	"testing"
)

// Em linguagem de negócio: o hóspede confirmado remarca a estadia. Quem ia
// dormir na casa vai junto para as datas novas — o titular e os acompanhantes,
// menos quem pediu para ser apagado (ficha anonimizada) —, e a reserva antiga
// fica com a lista dela, congelada, como história da venda.
//
// Antes, a remarcação com menos hóspedes do que a lista nomeia é recusada com
// 422 em `guests_count` ANTES de qualquer escrita: a original continua
// confirmada, com o calendário, a agenda e a lista intactos, e nenhuma reserva
// nasce. Ninguém é escolhido para ficar de fora. E a chave de idempotência da
// tentativa recusada não fica queimada.
func TestHospedesQARemarcacaoLevaOsAcompanhantesECongelaAOriginal(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	titular := f.qhPessoa(t, casa.Prop, "Titular")
	ana := f.qhPessoa(t, casa.Prop, "Ana")
	beto := f.qhPessoa(t, casa.Prop, "Beto")
	carla := f.qhPessoa(t, casa.Prop, "Carla")
	quem := f.qhPessoa(t, casa.Prop, "Davi")
	d := casa.Hoje.AddDays(700)
	original := a.qhReservar(t, u.Token, p.ID, titular.ID, d, d.AddDays(3), 4)
	for _, x := range []qhPessoa{ana, beto, carla} {
		a.qhAcrescentarOK(t, u.Token, original.ID, x.ID)
	}
	a.qhConfirmar(t, u.Token, original)
	// Dado anterior à regra "ninguém de uma lista viva é anonimizado".
	a.qhAnonimizarDireto(t, carla.ID)
	caminho := "/reservations/" + original.ID.String() + "/reschedule"
	nova := d.AddDays(20)

	agendaAntes := a.qagContar(t, `SELECT count(*) FROM agenda_events WHERE reservation_id = $1 AND status = 'agendado'`, original.ID)
	if agendaAntes == 0 {
		t.Fatal("a reserva confirmada deveria ter a agenda dela (premissa do cenário)")
	}

	// 1. Titular + Ana + Beto não cabem em 2 (a Carla, anonimizada, nem conta).
	chave := a.chaveNova(t, "rooming-remarca")
	e := qbErro(t, a.chamarComChave(t, http.MethodPost, caminho, u.Token,
		map[string]any{"check_in": nova.String(), "check_out": nova.AddDays(3).String(), "guests_count": 2}, chave),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "remarcar para menos gente que a lista")
	if _, ok := e.Details["guests_count"]; !ok {
		t.Errorf("a recusa não aponta guests_count: %v", e.Details)
	}
	if s := a.qagContar(t, `SELECT count(*) FROM reservations WHERE id = $1 AND status = 'confirmed' AND cancelled_at IS NULL`, original.ID); s != 1 {
		t.Fatalf("a recusa mexeu na original")
	}
	if n := a.qagContar(t, `SELECT count(*) FROM reservations WHERE rebooked_from_id = $1`, original.ID); n != 0 {
		t.Fatalf("a recusa deixou %d reserva nova", n)
	}
	if n := a.qagContar(t, `SELECT count(*) FROM stay_blocks WHERE reservation_id = $1 AND status = 'confirmed'`, original.ID); n != 1 {
		t.Fatalf("a recusa soltou o calendário da original: %d bloco(s) confirmado(s)", n)
	}
	if n := a.qagContar(t, `SELECT count(*) FROM agenda_events WHERE reservation_id = $1 AND status = 'agendado'`, original.ID); n != agendaAntes {
		t.Fatalf("a recusa cancelou a agenda da original: %d de %d agendados", n, agendaAntes)
	}
	if n := a.qhNomesNoBanco(t, original.ID); n != 4 {
		t.Fatalf("a recusa mexeu na lista da original: %d nomes", n)
	}
	if n := a.qagContar(t, `SELECT count(*) FROM reservation_events WHERE reservation_id = $1 AND type = 'rescheduled'`, original.ID); n != 0 {
		t.Fatalf("a recusa deixou %d evento(s) de remarcação", n)
	}

	// 2. Com o número da original (4), cabem — e pela MESMA chave: a recusa
	//    não a gravou.
	resp := a.chamarComChave(t, http.MethodPost, caminho, u.Token,
		map[string]any{"check_in": nova.String(), "check_out": nova.AddDays(3).String()}, chave)
	criada := qbDado[qagReserva](t, resp, http.StatusCreated, "remarcar com o número da original, mesma chave")

	lida := a.qhLer(t, u.Token, criada.ID)
	qhExigirLista(t, "a nova nasce com o titular e os acompanhantes", lida.ids(), titular.ID, ana.ID, beto.ID)
	if !lida.Data[0].Titular || lida.Data[1].Titular || lida.Data[2].Titular {
		t.Errorf("marcação de titular na nova: %+v", lida.Data)
	}
	if got := a.qhTitularNoBanco(t, criada.ID, "a reserva nova"); got != titular.ID {
		t.Errorf("titular da nova = %s", got)
	}
	if !lida.Meta.Editable || lida.Meta.GuestsCount != 4 {
		t.Errorf("meta da nova = %+v", lida.Meta)
	}
	// Um audit_log por carregado, por contact_id; nenhum guest_added na nova.
	if n := a.qagContar(t, `
		SELECT count(*) FROM audit_log WHERE entity = 'reservation_guests' AND entity_id = $1
		   AND action = 'reservation_guests.criado' AND after->>'contact_id' = ANY($2::text[])`,
		criada.ID, []string{ana.ID.String(), beto.ID.String()}); n != 2 {
		t.Errorf("audit_log dos carregados = %d, esperado 2 (Ana e Beto)", n)
	}
	if n := a.qagContar(t, `SELECT count(*) FROM audit_log WHERE entity = 'reservation_guests' AND entity_id = $1 AND after->>'contact_id' = $2`,
		criada.ID, carla.ID.String()); n != 0 {
		t.Errorf("a anonimizada aparece na trilha da nova")
	}
	if n := a.qagContar(t, `SELECT count(*) FROM reservation_events WHERE reservation_id = $1 AND type = 'guest_added'`, criada.ID); n != 0 {
		t.Errorf("a remarcação gravou %d guest_added na nova", n)
	}

	// 3. A original fica com a lista dela, congelada.
	velha := a.qhLer(t, u.Token, original.ID)
	if len(velha.Data) != 4 || velha.Data[0].ContactID != titular.ID || velha.Meta.Editable {
		t.Errorf("a original depois da remarcação: %d nomes, meta %+v", len(velha.Data), velha.Meta)
	}
	qbErro(t, a.qhAcrescentar(t, u.Token, original.ID, quem.ID), http.StatusConflict, "INVALID_STATE_TRANSITION", "acrescentar na original remarcada")
	qbErro(t, a.qhRetirar(t, u.Token, original.ID, ana.ID), http.StatusConflict, "INVALID_STATE_TRANSITION", "retirar da original remarcada")
	qbErro(t, a.chamar(t, http.MethodPatch, "/reservations/"+original.ID.String(), u.Token, map[string]any{"contact_id": quem.ID}),
		http.StatusConflict, "INVALID_STATE_TRANSITION", "trocar o titular da original remarcada")
	if n := a.qhNomesNoBanco(t, original.ID); n != 4 {
		t.Errorf("a lista da original mudou: %d nomes", n)
	}

	// 4. A nova se edita como qualquer reserva de pé.
	a.qhAcrescentarOK(t, u.Token, criada.ID, quem.ID)
}

// Com o titular anonimizado (dado anterior à regra), a reserva nova não nasce
// com ele: 409 CONTACT_ANONYMIZED, e nada muda — a original continua de pé e
// nenhuma reserva nasce.
func TestHospedesQARemarcacaoComTitularAnonimizadoEh409ENadaMuda(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	titular := f.qhPessoa(t, casa.Prop, "Titular")
	ana := f.qhPessoa(t, casa.Prop, "Ana")
	d := casa.Hoje.AddDays(740)
	original := a.qhReservar(t, u.Token, p.ID, titular.ID, d, d.AddDays(2), 3)
	a.qhAcrescentarOK(t, u.Token, original.ID, ana.ID)
	a.qhAnonimizarDireto(t, titular.ID)

	qbErro(t, a.chamarComChave(t, http.MethodPost, "/reservations/"+original.ID.String()+"/reschedule", u.Token,
		map[string]any{"check_in": d.AddDays(10).String(), "check_out": d.AddDays(12).String()}, a.chaveNova(t, "rooming-remarca-anon")),
		http.StatusConflict, "CONTACT_ANONYMIZED", "remarcar com titular anonimizado")
	if n := a.qagContar(t, `SELECT count(*) FROM reservations WHERE id = $1 AND status = 'hold'`, original.ID); n != 1 {
		t.Fatalf("a recusa mexeu na original")
	}
	if n := a.qagContar(t, `SELECT count(*) FROM reservations WHERE rebooked_from_id = $1`, original.ID); n != 0 {
		t.Fatalf("a recusa deixou %d reserva nova", n)
	}
	if n := a.qagContar(t, `SELECT count(*) FROM stay_blocks WHERE reservation_id = $1 AND status = 'hold'`, original.ID); n != 1 {
		t.Fatalf("a recusa soltou o calendário da original")
	}
	if n := a.qhNomesNoBanco(t, original.ID); n != 2 {
		t.Fatalf("a recusa mexeu na lista da original: %d", n)
	}
}
