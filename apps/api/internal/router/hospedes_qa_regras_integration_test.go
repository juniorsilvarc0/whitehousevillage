//go:build integration

package router

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ─────────────────────────── O teto é o número vendido ──────────────────────

// Em linguagem de negócio: a reserva foi vendida para 2 pessoas; a recepção não
// nomeia a terceira sem antes aumentar o número de hóspedes (que confere a
// capacidade do produto e fica auditado). A recusa traz os três números que a
// tela usa — vendidos, nomeados e capacidade — e a capacidade é a DO PRODUTO
// (aqui 5, para não coincidir com nenhum padrão).
func TestHospedesQATetoDaListaEhONumeroVendido(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	if _, err := a.pool.Exec(a.ctx, `UPDATE unit_types SET capacity = 5 WHERE id = $1`, p.ID); err != nil {
		t.Fatal(err)
	}
	titular := f.qhPessoa(t, casa.Prop, "Titular")
	ana := f.qhPessoa(t, casa.Prop, "Ana")
	bia := f.qhPessoa(t, casa.Prop, "Bia")
	d := casa.Hoje.AddDays(600)
	r := a.qhReservar(t, u.Token, p.ID, titular.ID, d, d.AddDays(2), 2)
	caminho := "/reservations/" + r.ID.String()

	lida := a.qhLer(t, u.Token, r.ID)
	if lida.Meta != (qhMeta{GuestsCount: 2, Capacity: 5, Editable: true}) {
		t.Errorf("meta = %+v, esperado {guests_count 2, capacity 5, editable true}", lida.Meta)
	}

	a.qhAcrescentarOK(t, u.Token, r.ID, ana.ID)
	e := qbErro(t, a.qhAcrescentar(t, u.Token, r.ID, bia.ID), http.StatusUnprocessableEntity, "GUEST_LIST_FULL", "a terceira em 2 vendidos")
	if e.Details["guests_count"] != float64(2) || e.Details["listed"] != float64(2) || e.Details["capacity"] != float64(5) {
		t.Errorf("details = %v; o contrato pede guests_count 2, listed 2, capacity 5", e.Details)
	}
	if n := a.qhNomesNoBanco(t, r.ID); n != 2 {
		t.Fatalf("a recusa deixou %d nomes", n)
	}

	// Baixar abaixo dos nomes: 422 em guests_count, e nada grava — no PATCH e no PUT.
	e = qbErro(t, a.chamar(t, http.MethodPatch, caminho, u.Token, map[string]any{"guests_count": 1}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "PATCH guests_count abaixo dos nomes")
	if _, ok := e.Details["guests_count"]; !ok {
		t.Errorf("a recusa não aponta guests_count: %v", e.Details)
	}
	e = qbErro(t, a.chamar(t, http.MethodPut, caminho, u.Token, map[string]any{"contact_id": titular.ID, "guests_count": 1}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "PUT guests_count abaixo dos nomes")
	if _, ok := e.Details["guests_count"]; !ok {
		t.Errorf("a recusa do PUT não aponta guests_count: %v", e.Details)
	}
	if n := a.qagContar(t, `SELECT guests_count FROM reservations WHERE id = $1`, r.ID); n != 2 {
		t.Fatalf("a recusa gravou guests_count = %d", n)
	}

	// Subir abre o lugar; acima da capacidade do produto é CAPACITY_EXCEEDED.
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, caminho, u.Token, map[string]any{"guests_count": 3}), http.StatusOK, "PATCH guests_count 3")
	a.qhAcrescentarOK(t, u.Token, r.ID, bia.ID)
	qbErro(t, a.chamar(t, http.MethodPatch, caminho, u.Token, map[string]any{"guests_count": 6}),
		http.StatusUnprocessableEntity, "CAPACITY_EXCEEDED", "guests_count acima da capacidade")
	if lida := a.qhLer(t, u.Token, r.ID); len(lida.Data) != 3 || lida.Meta.GuestsCount != 3 {
		t.Fatalf("lista = %d nomes, meta %+v", len(lida.Data), lida.Meta)
	}
	// Reenviar o número de sempre passa.
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, caminho, u.Token, map[string]any{"guests_count": 3}), http.StatusOK, "PATCH reenviando 3")
}

// ─────────────────────────── A lista só se edita com a reserva de pé ───────

// A lista aceita acrescentar, retirar e trocar o titular em `hold`, `confirmed`
// e `checked_in` — e em NENHUM outro dos nove estados. Fora deles: 409
// INVALID_STATE_TRANSITION com `details.status` (o estado de agora) e
// `details.allowed` (os três), a lista intacta, e `meta.editable` /
// `guests_editable` falsos — a mesma regra nos dois lugares.
func TestHospedesQAListaSoSeEditaComAReservaDePe(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 6)
	hoje := casa.Hoje
	longe := hoje.AddDays(610)

	type caso struct {
		estado  string
		reserva uuid.UUID
		titular qhPessoa
		acomp   qhPessoa
	}
	novo := func(estado string) caso {
		c := caso{estado: estado,
			titular: f.qhPessoa(t, casa.Prop, "Titular "+estado),
			acomp:   f.qhPessoa(t, casa.Prop, "Acomp "+estado)}
		return c
	}
	var casos []caso

	// Pela API: hold, confirmed, checked_in, checked_out, cancelled, no_show.
	for _, estado := range []string{"hold", "confirmed", "checked_in", "checked_out", "cancelled", "no_show"} {
		c := novo(estado)
		de := longe
		if estado == "checked_in" || estado == "checked_out" {
			de = hoje // check-in e check-out conferem a data contra o período vendido
		}
		r := a.qhReservar(t, u.Token, p.ID, c.titular.ID, de, de.AddDays(2), 3)
		a.qhAcrescentarOK(t, u.Token, r.ID, c.acomp.ID)
		switch estado {
		case "confirmed":
			a.qhConfirmar(t, u.Token, r)
		case "checked_in":
			a.qhConfirmar(t, u.Token, r)
			a.qhAcao(t, u.Token, r.ID, "check-in", nil)
		case "checked_out":
			a.qhConfirmar(t, u.Token, r)
			a.qhAcao(t, u.Token, r.ID, "check-in", nil)
			a.qhAcao(t, u.Token, r.ID, "check-out", nil)
		case "cancelled":
			a.qhAcao(t, u.Token, r.ID, "cancel", map[string]any{"reason": "desistencia"})
		case "no_show":
			a.qhConfirmar(t, u.Token, r)
			a.qhAcao(t, u.Token, r.ID, "cancel", map[string]any{"reason": "no_show"})
		}
		c.reserva = r.ID
		casos = append(casos, c)
	}
	// Pelo banco: os três que nenhuma rota produz de forma barata.
	for i, estado := range []string{"quote", "closed", "expired"} {
		c := novo(estado)
		de := longe.AddDays(10 + 3*i)
		c.reserva = a.qhReservaPorSQL(t, casa.Prop, p.ID, c.titular.ID, estado, de, de.AddDays(2), 3)
		if _, err := a.pool.Exec(a.ctx, `INSERT INTO reservation_guests (reservation_id, contact_id) VALUES ($1, $2)`, c.reserva, c.acomp.ID); err != nil {
			t.Fatal(err)
		}
		casos = append(casos, c)
	}

	editaveis := map[string]bool{"hold": true, "confirmed": true, "checked_in": true}
	for _, c := range casos {
		t.Run(c.estado, func(t *testing.T) {
			if s := a.qagContar(t, `SELECT CASE status WHEN $2 THEN 1 ELSE 0 END FROM reservations WHERE id = $1`, c.reserva, c.estado); s != 1 {
				t.Fatalf("a reserva do caso não está em %s", c.estado)
			}
			lida := a.qhLer(t, u.Token, c.reserva)
			completa, _ := a.qhCompleta(t, u.Token, c.reserva)
			if lida.Meta.Editable != editaveis[c.estado] || completa.Editavel == nil || *completa.Editavel != editaveis[c.estado] {
				t.Errorf("em %s: meta.editable = %v, guests_editable = %v; esperado %v nos dois",
					c.estado, lida.Meta.Editable, completa.Editavel, editaveis[c.estado])
			}
			quem := f.qhPessoa(t, casa.Prop, "Chegou "+c.estado)
			post := a.qhAcrescentar(t, u.Token, c.reserva, quem.ID)
			del := a.qhRetirar(t, u.Token, c.reserva, c.acomp.ID)
			troca := a.chamar(t, http.MethodPatch, "/reservations/"+c.reserva.String(), u.Token, map[string]any{"contact_id": quem.ID})
			if editaveis[c.estado] {
				exigirStatusQB(t, post, http.StatusCreated, "POST em "+c.estado)
				exigirStatusQB(t, del, http.StatusNoContent, "DELETE em "+c.estado)
				exigirStatusQB(t, troca, http.StatusOK, "troca de titular em "+c.estado)
				qhExigirLista(t, "em "+c.estado, a.qhLer(t, u.Token, c.reserva).ids(), quem.ID)
				return
			}
			for nome, r := range map[string]resposta{"POST": post, "DELETE": del, "PATCH contact_id": troca} {
				e := qbErro(t, r, http.StatusConflict, "INVALID_STATE_TRANSITION", nome+" em "+c.estado)
				if e.Details["status"] != c.estado {
					t.Errorf("%s em %s: details.status = %v", nome, c.estado, e.Details["status"])
				}
				permitidos, _ := e.Details["allowed"].([]any)
				var lista []string
				for _, x := range permitidos {
					lista = append(lista, fmt.Sprint(x))
				}
				sort.Strings(lista)
				if strings.Join(lista, ",") != "checked_in,confirmed,hold" {
					t.Errorf("%s em %s: details.allowed = %v; o contrato diz hold, confirmed, checked_in", nome, c.estado, e.Details["allowed"])
				}
				if nome == "PATCH contact_id" && e.Details["field"] != "contact_id" {
					t.Errorf("a recusa da troca em %s não aponta details.field = contact_id: %v", c.estado, e.Details)
				}
			}
			qhExigirLista(t, "a lista de "+c.estado+" ficou como estava", a.qhLer(t, u.Token, c.reserva).ids(), c.titular.ID, c.acomp.ID)
			// Reenviar o titular de sempre não é troca e passa em qualquer estado.
			exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/reservations/"+c.reserva.String(), u.Token,
				map[string]any{"contact_id": c.titular.ID}), http.StatusOK, "reenvio do titular em "+c.estado)
		})
	}
}

// ─────────────────────────── Quem entra, e em que ordem se recusa ───────────

// As recusas do POST, cada uma com o code e os details do contrato, e a ORDEM em
// que o contrato diz que são conferidas — estado, ficha, anonimização, repetida,
// cheia. A ordem importa para a tela: com a lista cheia, quem está anonimizado
// ou já está na lista não pode ouvir "aumente o número de hóspedes".
func TestHospedesQAQuemEntraEAOrdemDasRecusas(t *testing.T) {
	a := subirAPI(t)
	casaB := a.qagOutraCasa(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 2)
	titular := f.qhPessoa(t, casa.Prop, "Titular")
	ana := f.qhPessoa(t, casa.Prop, "Ana")
	anonima := f.qhPessoa(t, casa.Prop, "Anonimizada")
	a.qhAnonimizarDireto(t, anonima.ID)
	deLa := f.qhPessoa(t, casaB, "Da Casa B")
	d := casa.Hoje.AddDays(640)

	// Com lugar sobrando: cada recusa sozinha.
	r := a.qhReservar(t, u.Token, p.ID, titular.ID, d, d.AddDays(2), 4)
	e := qbErro(t, a.qhAcrescentar(t, u.Token, r.ID, anonima.ID), http.StatusConflict, "CONTACT_ANONYMIZED", "ficha anonimizada")
	if e.Details["contact_id"] != anonima.ID.String() {
		t.Errorf("details da anonimizada: %v", e.Details)
	}
	a.qhAcrescentarOK(t, u.Token, r.ID, ana.ID)
	e = qbErro(t, a.qhAcrescentar(t, u.Token, r.ID, ana.ID), http.StatusConflict, "CODE_IN_USE", "acompanhante repetida")
	if e.Details["contact_id"] != ana.ID.String() || e.Details["is_lead_guest"] != false {
		t.Errorf("details da repetida: %v; o contrato pede contact_id e is_lead_guest false", e.Details)
	}
	e = qbErro(t, a.qhAcrescentar(t, u.Token, r.ID, titular.ID), http.StatusConflict, "CODE_IN_USE", "o titular como acompanhante")
	if e.Details["contact_id"] != titular.ID.String() || e.Details["is_lead_guest"] != true {
		t.Errorf("details do titular repetido: %v; o contrato pede is_lead_guest true", e.Details)
	}

	// Lista CHEIA (2 vendidos, 2 nomes): o que vem antes de "cabe" continua
	// respondendo o que é — e não GUEST_LIST_FULL.
	cheia := a.qhReservar(t, u.Token, p.ID, titular.ID, d.AddDays(10), d.AddDays(12), 2)
	a.qhAcrescentarOK(t, u.Token, cheia.ID, ana.ID)
	for nome, quer := range map[string]struct {
		contato uuid.UUID
		status  int
		code    string
	}{
		"anonimizada na lista cheia":       {anonima.ID, http.StatusConflict, "CONTACT_ANONYMIZED"},
		"repetida na lista cheia":          {ana.ID, http.StatusConflict, "CODE_IN_USE"},
		"o titular na lista cheia":         {titular.ID, http.StatusConflict, "CODE_IN_USE"},
		"ficha de outra casa na cheia":     {deLa.ID, http.StatusUnprocessableEntity, "VALIDATION_ERROR"},
		"ficha inexistente na lista cheia": {uuid.New(), http.StatusUnprocessableEntity, "VALIDATION_ERROR"},
	} {
		qbErro(t, a.qhAcrescentar(t, u.Token, cheia.ID, quer.contato), quer.status, quer.code, nome)
	}

	// Reserva fora de pé: o estado vem ANTES de tudo — inclusive da ficha.
	a.qhAcao(t, u.Token, cheia.ID, "cancel", map[string]any{"reason": "desistencia"})
	for nome, contato := range map[string]uuid.UUID{
		"ficha inexistente na cancelada": uuid.New(),
		"anonimizada na cancelada":       anonima.ID,
		"repetida na cancelada":          ana.ID,
		"de outra casa na cancelada":     deLa.ID,
	} {
		qbErro(t, a.qhAcrescentar(t, u.Token, cheia.ID, contato), http.StatusConflict, "INVALID_STATE_TRANSITION", nome)
	}
	// E no DELETE: estado antes de "é o titular" e antes de "não está na lista".
	qbErro(t, a.qhRetirar(t, u.Token, cheia.ID, titular.ID), http.StatusConflict, "INVALID_STATE_TRANSITION", "retirar o titular da cancelada")
	qbErro(t, a.qhRetirar(t, u.Token, cheia.ID, uuid.New()), http.StatusConflict, "INVALID_STATE_TRANSITION", "retirar quem não está, na cancelada")

	// De pé: titular antes de "não está"; quem não está é 404 (o segundo clique).
	qbErro(t, a.qhRetirar(t, u.Token, r.ID, titular.ID), http.StatusConflict, "RESOURCE_IN_USE", "retirar o titular")
	exigirStatusQB(t, a.qhRetirar(t, u.Token, r.ID, ana.ID), http.StatusNoContent, "retirar a acompanhante")
	qbErro(t, a.qhRetirar(t, u.Token, r.ID, ana.ID), http.StatusNotFound, "NOT_FOUND", "o segundo clique no retirar")
	qbErro(t, a.qhRetirar(t, u.Token, r.ID, deLa.ID), http.StatusNotFound, "NOT_FOUND", "retirar a ficha de outra casa")

	qhExigirLista(t, "a lista de pé no fim", a.qhLer(t, u.Token, r.ID).ids(), titular.ID)
	qhExigirLista(t, "a lista cancelada no fim", a.qhLer(t, u.Token, cheia.ID).ids(), titular.ID, ana.ID)
}

// ─────────────────────────── A reserva que nasce ────────────────────────────

// O titular é o primeiro hóspede da lista, e a regra de entrada vale na
// criação: `POST /reservations` com titular anonimizado é 409
// CONTACT_ANONYMIZED, e NADA nasce — nem reserva, nem calendário, nem lista. A
// chave de idempotência da tentativa recusada não fica queimada: a mesma chave,
// com o titular certo, cria a reserva. Ficha inexistente e de outra casa
// continuam o 404 de sempre desta rota.
func TestHospedesQACriarComTitularAnonimizadoNaoCriaNada(t *testing.T) {
	a := subirAPI(t)
	casaB := a.qagOutraCasa(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	anonimo := f.qhPessoa(t, casa.Prop, "Titular Anonimizado")
	a.qhAnonimizarDireto(t, anonimo.ID)
	certo := f.qhPessoa(t, casa.Prop, "Titular Certo")
	deLa := f.qhPessoa(t, casaB, "Da Casa B")
	d := casa.Hoje.AddDays(660)
	pedido := func(contato uuid.UUID) map[string]any {
		return map[string]any{"unit_type_id": p.ID, "contact_id": contato,
			"check_in": d.String(), "check_out": d.AddDays(2).String(), "guests_count": 2}
	}

	chave := a.chaveNova(t, "rooming-anon")
	e := qbErro(t, a.chamarComChave(t, http.MethodPost, "/reservations", u.Token, pedido(anonimo.ID), chave),
		http.StatusConflict, "CONTACT_ANONYMIZED", "POST /reservations com titular anonimizado")
	if e.Details["contact_id"] != anonimo.ID.String() {
		t.Errorf("details: %v", e.Details)
	}
	if n := a.qagContar(t, `SELECT count(*) FROM reservations WHERE contact_id = $1 OR unit_type_id = $2`, anonimo.ID, p.ID); n != 0 {
		t.Fatalf("a recusa deixou %d reserva(s)", n)
	}
	if n := a.qagContar(t, `SELECT count(*) FROM stay_blocks WHERE unit_id = ANY($1::uuid[])`, p.Unidades); n != 0 {
		t.Fatalf("a recusa segurou o calendário: %d bloco(s)", n)
	}
	if n := a.qagContar(t, `SELECT count(*) FROM reservation_guests WHERE contact_id = $1`, anonimo.ID); n != 0 {
		t.Fatalf("a recusa pôs a ficha anonimizada numa lista: %d linha(s)", n)
	}

	r1 := a.chamarComChave(t, http.MethodPost, "/reservations", u.Token, pedido(deLa.ID), a.chaveNova(t, "rooming-b"))
	r2 := a.chamarComChave(t, http.MethodPost, "/reservations", u.Token, pedido(uuid.New()), a.chaveNova(t, "rooming-x"))
	qbErro(t, r1, http.StatusNotFound, "NOT_FOUND", "POST /reservations com titular de outra casa")
	qbErro(t, r2, http.StatusNotFound, "NOT_FOUND", "POST /reservations com titular inexistente")

	// A mesma chave, com o titular certo, é pedido novo — a recusa não a gravou.
	criada := qbDado[qagReserva](t, a.chamarComChave(t, http.MethodPost, "/reservations", u.Token, pedido(certo.ID), chave),
		http.StatusCreated, "POST /reservations reaproveitando a chave da recusa")
	if got := a.qhTitularNoBanco(t, criada.ID, "a reserva criada"); got != certo.ID {
		t.Fatalf("titular = %s", got)
	}
}

// ─────────────────────────── Nenhuma recusa vira oráculo ────────────────────

// A ficha de OUTRA casa não pode ser distinguida de uma ficha que não existe —
// nem quando ela foi anonimizada. Se a anonimizada de lá respondesse 409
// CONTACT_ANONYMIZED, a recusa contaria a quem tenta ids que aquela pessoa
// existe noutra propriedade e pediu para ser esquecida. Vale nas três portas
// por onde uma pessoa entra numa lista: o acréscimo (422), a troca de titular
// (404) e a reserva que nasce (404) — e em cada porta as três respostas são
// IDÊNTICAS, byte a byte.
func TestHospedesQAFichaDeOutraCasaNaoViraOraculoNemAnonimizada(t *testing.T) {
	a := subirAPI(t)
	casaB := a.qagOutraCasa(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	c := a.qhMontarCenario(t, f, u.Token, 690)
	anonimaDeLa := f.qhPessoa(t, casaB, "Anonimizada da Casa B")
	a.qhAnonimizarDireto(t, anonimaDeLa.ID)
	vivaDeLa := f.qhPessoa(t, casaB, "Viva da Casa B")
	d := c.Casa.Hoje.AddDays(695)

	alvos := map[string]uuid.UUID{
		"anonimizada de outra casa": anonimaDeLa.ID,
		"viva de outra casa":        vivaDeLa.ID,
		"inexistente":               uuid.New(),
	}
	portas := []struct {
		nome   string
		status int
		code   string
		chamar func(contato uuid.UUID) resposta
	}{
		{"POST /guests", http.StatusUnprocessableEntity, "VALIDATION_ERROR", func(x uuid.UUID) resposta {
			return a.qhAcrescentar(t, u.Token, c.Reserva.ID, x)
		}},
		{"PATCH contact_id", http.StatusNotFound, "NOT_FOUND", func(x uuid.UUID) resposta {
			return a.chamar(t, http.MethodPatch, "/reservations/"+c.Reserva.ID.String(), u.Token, map[string]any{"contact_id": x})
		}},
		{"POST /reservations", http.StatusNotFound, "NOT_FOUND", func(x uuid.UUID) resposta {
			return a.chamarComChave(t, http.MethodPost, "/reservations", u.Token, map[string]any{
				"unit_type_id": c.Produto.ID, "contact_id": x, "check_in": d.String(), "check_out": d.AddDays(2).String(), "guests_count": 2,
			}, a.chaveNova(t, "rooming-oraculo"))
		}},
	}
	antes := a.qhLerEstado(t, c)
	for _, porta := range portas {
		corpos := map[string]string{}
		for nome, x := range alvos {
			r := porta.chamar(x)
			qbErro(t, r, porta.status, porta.code, porta.nome+" com ficha "+nome)
			corpos[nome] = string(r.Corpo)
		}
		if corpos["anonimizada de outra casa"] != corpos["inexistente"] || corpos["viva de outra casa"] != corpos["inexistente"] {
			t.Errorf("%s: as três fichas não respondem igual — oráculo:\n anonimizada de lá %s\n viva de lá        %s\n inexistente       %s",
				porta.nome, corpos["anonimizada de outra casa"], corpos["viva de outra casa"], corpos["inexistente"])
		}
	}
	if depois := a.qhLerEstado(t, c); depois != antes {
		t.Fatalf("as recusas mexeram na lista:\n antes  %+v\n depois %+v", antes, depois)
	}
	if n := a.qagContar(t, `SELECT count(*) FROM reservations WHERE contact_id = ANY($1::uuid[])`,
		[]uuid.UUID{anonimaDeLa.ID, vivaDeLa.ID}); n != 0 {
		t.Fatalf("nasceu reserva com ficha de outra casa: %d", n)
	}
}

// ─────────────────────────── O ganho do funil ───────────────────────────────

// O contrato estende a regra da reserva que nasce ao `/win` do CRM: o contato
// foi anonimizado DEPOIS da emissão do orçamento (pela API — a anonimização não
// olha oportunidade aberta), e a reserva não nasce com titular anonimizado: 409
// CONTACT_ANONYMIZED, e "em qualquer recusa nada muda" — a oportunidade continua
// aberta, sem reserva, e o orçamento não é convertido.
func TestHospedesQAGanharNoFunilComContatoAnonimizadoEh409ENadaMuda(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "hq-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qagFaxina(t, admin)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	cliente := f.qhPessoa(t, casa.Prop, "Cliente do Funil")
	// Roda ANTES da faxina (Cleanup é LIFO): orçamento e card seguram a ficha.
	t.Cleanup(func() {
		a.executarQA(t, `DELETE FROM quotes WHERE contact_id = $1`, cliente.ID)
		a.executarQA(t, `DELETE FROM crm_opportunities WHERE contact_id = $1`, cliente.ID)
	})
	d := casa.Hoje.AddDays(680)

	card := envelopeDe[oportunidadeQA](t, a.chamar(t, http.MethodPost, "/crm/opportunities", admin.Token, map[string]any{
		"contact_id": cliente.ID, "unit_type_id": p.ID, "check_in": d.String(), "check_out": d.AddDays(2).String(),
	}), http.StatusCreated, "POST /crm/opportunities")
	orcamento := envelopeDe[orcamentoSalvoQA](t, a.chamar(t, http.MethodPost, "/quotes", admin.Token, map[string]any{
		"unit_type_id": p.ID, "check_in": d.String(), "check_out": d.AddDays(2).String(), "guests_count": 2,
		"persist": true, "contact_id": cliente.ID, "opportunity_id": card.ID,
	}), http.StatusCreated, "POST /quotes {persist}")

	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/contacts/"+cliente.ID.String()+"/anonymize", admin.Token,
		map[string]any{"reason": "pedido do titular dos dados"}), http.StatusOK, "POST /contacts/{id}/anonymize")

	qbErro(t, a.chamarComChave(t, http.MethodPost, "/crm/opportunities/"+card.ID.String()+"/win", admin.Token,
		map[string]any{"quote_id": orcamento.ID}, a.chaveNova(t, "rooming-win")),
		http.StatusConflict, "CONTACT_ANONYMIZED", "/win com o contato anonimizado depois do orçamento")

	relido := envelopeDe[oportunidadeQA](t, a.chamar(t, http.MethodGet, "/crm/opportunities/"+card.ID.String(), admin.Token, nil),
		http.StatusOK, "GET /crm/opportunities/{id}")
	if relido.Status != "aberta" || relido.ReservaID != nil {
		t.Errorf("a recusa mexeu no card: status %q, reservation_id %v", relido.Status, relido.ReservaID)
	}
	if n := a.qagContar(t, `SELECT count(*) FROM reservations WHERE contact_id = $1 OR unit_type_id = $2`, cliente.ID, p.ID); n != 0 {
		t.Fatalf("nasceu %d reserva(s) com o titular anonimizado", n)
	}
	if n := a.qagContar(t, `SELECT count(*) FROM quotes WHERE id = $1 AND reservation_id IS NOT NULL`, orcamento.ID); n != 0 {
		t.Fatalf("o orçamento ficou convertido numa reserva que não existe")
	}
}
