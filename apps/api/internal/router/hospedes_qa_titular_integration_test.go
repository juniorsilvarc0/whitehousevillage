//go:build integration

package router

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ─────────────────────────── Exatamente um, e é o da reserva ────────────────

// Em linguagem de negócio: o cartão "Cliente" do painel lê o titular da lista,
// e até o F2-22 ele mostrava o nome e o telefone de quem já não era o cliente.
// A troca de titular é o PATCH (ou o PUT) da reserva, e a lista vem junto:
//
//   - o novo aparece marcado, primeiro, e é o ÚNICO marcado;
//   - o anterior SAI da lista (não vira acompanhante) — e a ficha dele fica;
//   - quem já era acompanhante é promovido, e a lista diminui;
//   - `/full` (reservation e guests), `GET /reservations/{id}` e a resposta da
//     própria edição dizem a mesma pessoa, com o nome da ficha;
//   - o banco confirma: uma linha de titular, igual a `reservations.contact_id`.
func TestHospedesQATrocaDeTitularDeixaListaFullECartaoDeAcordo(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	antigo := f.qhPessoa(t, casa.Prop, "Antigo")
	bia := f.qhPessoa(t, casa.Prop, "Bia")
	caio := f.qhPessoa(t, casa.Prop, "Caio")
	dora := f.qhPessoa(t, casa.Prop, "Dora")
	d := casa.Hoje.AddDays(530)
	r := a.qhReservar(t, u.Token, p.ID, antigo.ID, d, d.AddDays(2), 3)
	caminho := "/reservations/" + r.ID.String()
	a.qhAcrescentarOK(t, u.Token, r.ID, bia.ID)
	a.qhAcrescentarOK(t, u.Token, r.ID, caio.ID)
	a.qhTitularNoBanco(t, r.ID, "a reserva nasce com o titular")

	type cartao struct {
		ContactID   uuid.UUID `json:"contact_id"`
		ContactName string    `json:"contact_name"`
	}
	conferir := func(contexto string, quem qhPessoa, lista ...uuid.UUID) {
		t.Helper()
		lida := a.qhLer(t, u.Token, r.ID)
		qhExigirLista(t, contexto+": GET /guests", lida.ids(), lista...)
		marcados := 0
		for i, h := range lida.Data {
			if h.Titular {
				marcados++
				if i != 0 || h.ContactID != quem.ID {
					t.Errorf("%s: titular marcado é %s na posição %d; esperado %s primeiro", contexto, h.ContactID, i, quem.ID)
				}
			}
		}
		if marcados != 1 {
			t.Errorf("%s: %d hóspede(s) marcados como titular, esperado 1", contexto, marcados)
		}
		c, _ := a.qhCompleta(t, u.Token, r.ID)
		if c.Reserva.ContactID != quem.ID || c.Reserva.ContactName != quem.Nome {
			t.Errorf("%s: /full.reservation = %s %q, esperado %s %q", contexto, c.Reserva.ContactID, c.Reserva.ContactName, quem.ID, quem.Nome)
		}
		if len(c.Hospedes) != len(lida.Data) {
			t.Fatalf("%s: /full.guests tem %d, /guests tem %d", contexto, len(c.Hospedes), len(lida.Data))
		}
		for i := range c.Hospedes {
			if !qhMesmoHospede(c.Hospedes[i], lida.Data[i]) {
				t.Errorf("%s: /full.guests[%d] = %+v, /guests[%d] = %+v — o contrato diz a mesma lista", contexto, i, c.Hospedes[i], i, lida.Data[i])
			}
		}
		if !c.Hospedes[0].Titular || c.Hospedes[0].Nome != c.Reserva.ContactName {
			t.Errorf("%s: o cartão Cliente (titular da lista: %q) e contact_name (%q) não batem", contexto, c.Hospedes[0].Nome, c.Reserva.ContactName)
		}
		one := qbDado[cartao](t, a.chamar(t, http.MethodGet, caminho, u.Token, nil), http.StatusOK, "GET /reservations/{id}")
		if one.ContactID != quem.ID || one.ContactName != quem.Nome {
			t.Errorf("%s: GET /reservations/{id} = %+v, esperado %s %q", contexto, one, quem.ID, quem.Nome)
		}
		if got := a.qhTitularNoBanco(t, r.ID, contexto); got != quem.ID {
			t.Errorf("%s: no banco o titular é %s, esperado %s", contexto, got, quem.ID)
		}
	}

	// 1. PATCH por quem NÃO estava na lista: sai o antigo, entra a Dora — a
	//    lista continua com 3 (cabe nos 3 vendidos).
	resp := a.chamar(t, http.MethodPatch, caminho, u.Token, map[string]any{"contact_id": dora.ID})
	editada := qbDado[cartao](t, resp, http.StatusOK, "PATCH contact_id (fora da lista)")
	if editada.ContactID != dora.ID || editada.ContactName != dora.Nome {
		t.Errorf("a resposta do PATCH diz %+v, esperado a Dora", editada)
	}
	conferir("PATCH por quem não estava na lista", dora, dora.ID, bia.ID, caio.ID)
	if n := a.qagContar(t, `SELECT count(*) FROM reservation_guests WHERE reservation_id = $1 AND contact_id = $2`, r.ID, antigo.ID); n != 0 {
		t.Errorf("o titular anterior ficou na lista (como acompanhante?): %d linha(s)", n)
	}
	if n := a.qagContar(t, `SELECT count(*) FROM contacts WHERE id = $1`, antigo.ID); n != 1 {
		t.Errorf("a troca apagou a ficha do titular anterior")
	}

	// 2. PATCH pelo acompanhante: a Bia é promovida, a Dora sai, a lista diminui.
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, caminho, u.Token, map[string]any{"contact_id": bia.ID}),
		http.StatusOK, "PATCH contact_id (acompanhante)")
	conferir("PATCH promovendo a acompanhante", bia, bia.ID, caio.ID)

	// 3. PUT (substituição integral) também troca: o Caio é promovido.
	exigirStatusQB(t, a.chamar(t, http.MethodPut, caminho, u.Token, map[string]any{"contact_id": caio.ID, "guests_count": 3}),
		http.StatusOK, "PUT com outro contact_id")
	conferir("PUT promovendo o acompanhante", caio, caio.ID)

	// 4. Reenviar o de sempre não é troca: nada muda na lista.
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, caminho, u.Token, map[string]any{"contact_id": caio.ID, "notes": "chega à noite"}),
		http.StatusOK, "PATCH reenviando o titular")
	conferir("reenvio do titular", caio, caio.ID)

	// 5. A troca não gera guest_added/guest_removed — fica no `updated`.
	if n := a.qagContar(t, `
		SELECT count(*) FROM reservation_events WHERE reservation_id = $1 AND type IN ('guest_added', 'guest_removed')`, r.ID); n != 2 {
		t.Errorf("eventos da lista = %d; esperado só os 2 guest_added dos acréscimos (a troca não gera nenhum)", n)
	}
}

// As recusas da troca deixam tudo como estava: ficha anonimizada é 409
// CONTACT_ANONYMIZED; inexistente e de outra casa continuam o 404 desta rota;
// `contact_id: null` é 422 (o titular não se apaga) — nunca 500.
func TestHospedesQARecusasDaTrocaDeTitularNaoMexemEmNada(t *testing.T) {
	a := subirAPI(t)
	casaB := a.qagOutraCasa(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	titular := f.qhPessoa(t, casa.Prop, "Titular")
	ana := f.qhPessoa(t, casa.Prop, "Ana")
	anonima := f.qhPessoa(t, casa.Prop, "Anonimizada")
	a.qhAnonimizarDireto(t, anonima.ID)
	deLa := f.qhPessoa(t, casaB, "Da Casa B")
	d := casa.Hoje.AddDays(540)
	r := a.qhReservar(t, u.Token, p.ID, titular.ID, d, d.AddDays(2), 3)
	a.qhAcrescentarOK(t, u.Token, r.ID, ana.ID)
	caminho := "/reservations/" + r.ID.String()

	e := qbErro(t, a.chamar(t, http.MethodPatch, caminho, u.Token, map[string]any{"contact_id": anonima.ID}),
		http.StatusConflict, "CONTACT_ANONYMIZED", "troca para ficha anonimizada")
	if e.Details["contact_id"] != anonima.ID.String() {
		t.Errorf("details da recusa: %v", e.Details)
	}
	qbErro(t, a.chamar(t, http.MethodPut, caminho, u.Token, map[string]any{"contact_id": anonima.ID, "guests_count": 3}),
		http.StatusConflict, "CONTACT_ANONYMIZED", "PUT para ficha anonimizada")
	r1 := a.chamar(t, http.MethodPatch, caminho, u.Token, map[string]any{"contact_id": deLa.ID})
	r2 := a.chamar(t, http.MethodPatch, caminho, u.Token, map[string]any{"contact_id": uuid.New()})
	qbErro(t, r1, http.StatusNotFound, "NOT_FOUND", "troca para ficha de outra casa")
	qbErro(t, r2, http.StatusNotFound, "NOT_FOUND", "troca para ficha inexistente")
	if string(r1.Corpo) != string(r2.Corpo) {
		t.Errorf("ficha de outra casa e inexistente respondem diferente na troca:\n %s\n %s", r1.Corpo, r2.Corpo)
	}
	nulo := a.chamar(t, http.MethodPatch, caminho, u.Token, map[string]any{"contact_id": nil})
	if e := qbErro(t, nulo, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "contact_id null"); e.Details["contact_id"] == nil {
		t.Errorf("a recusa do null não aponta contact_id: %v", e.Details)
	}

	qhExigirLista(t, "as recusas não mexem", a.qhLer(t, u.Token, r.ID).ids(), titular.ID, ana.ID)
	a.qhTitularNoBanco(t, r.ID, "depois das recusas")
}

// ─────────────────────────── O banco como última defesa ─────────────────────

// Escrevendo DIRETO no banco, como faria um script ou outro serviço:
//
//   - um segundo titular na mesma reserva é recusado na própria instrução
//     (23505 em reservation_guests_titular_idx) — por INSERT e por UPDATE;
//   - trocar `reservations.contact_id` sem mexer na lista passa na instrução e
//     falha no COMMIT (23503 em reservation_guests_titular_fkey);
//   - trocar a lista sem a reserva, idem;
//   - `SET CONSTRAINTS … IMMEDIATE` antecipa o 23503 para a instrução;
//   - trocar as DUAS pontas, em qualquer ordem, comita;
//   - ninguém escreve a coluna gerada (428C9).
func TestHospedesQABancoRecusaDoisTitularesETrocaDeUmLadoSo(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	titular := f.qhPessoa(t, casa.Prop, "Titular")
	ana := f.qhPessoa(t, casa.Prop, "Ana")
	estranho := f.qhPessoa(t, casa.Prop, "Estranho")
	d := casa.Hoje.AddDays(550)
	r := a.qhReservar(t, u.Token, p.ID, titular.ID, d, d.AddDays(2), 3)
	a.qhAcrescentarOK(t, u.Token, r.ID, ana.ID)

	exigirPG := func(err error, sqlstate, restricao, contexto string) {
		t.Helper()
		var pg *pgconn.PgError
		if !errors.As(err, &pg) {
			t.Fatalf("%s: esperado %s em %s, veio %v", contexto, sqlstate, restricao, err)
		}
		if pg.Code != sqlstate || (restricao != "" && pg.ConstraintName != restricao) {
			t.Fatalf("%s: %s em %q, esperado %s em %q (%s)", contexto, pg.Code, pg.ConstraintName, sqlstate, restricao, pg.Message)
		}
	}
	emTx := func(contexto string, passos func(tx pgx.Tx) error) error {
		t.Helper()
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
		defer cancel()
		tx, err := a.pool.Begin(ctx)
		if err != nil {
			t.Fatalf("%s: abrindo transação: %v", contexto, err)
		}
		if err := passos(tx); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		return tx.Commit(ctx)
	}

	// Segundo titular: 23505 na hora.
	_, err := a.pool.Exec(a.ctx, `INSERT INTO reservation_guests (reservation_id, contact_id, is_lead_guest) VALUES ($1, $2, true)`, r.ID, estranho.ID)
	exigirPG(err, "23505", "reservation_guests_titular_idx", "INSERT de um segundo titular")
	_, err = a.pool.Exec(a.ctx, `UPDATE reservation_guests SET is_lead_guest = true WHERE reservation_id = $1 AND contact_id = $2`, r.ID, ana.ID)
	exigirPG(err, "23505", "reservation_guests_titular_idx", "UPDATE promovendo a acompanhante sem tirar o titular")

	// A reserva sem a lista: a instrução passa, o COMMIT não.
	err = emTx("reserva sem a lista", func(tx pgx.Tx) error {
		if _, err := tx.Exec(a.ctx, `UPDATE reservations SET contact_id = $2 WHERE id = $1`, r.ID, ana.ID); err != nil {
			t.Fatalf("a instrução deveria passar (a FK é adiada): %v", err)
		}
		return nil
	})
	exigirPG(err, "23503", "reservation_guests_titular_fkey", "COMMIT da reserva trocada sem a lista")

	// A lista sem a reserva: idem.
	err = emTx("lista sem a reserva", func(tx pgx.Tx) error {
		if _, err := tx.Exec(a.ctx, `DELETE FROM reservation_guests WHERE reservation_id = $1 AND is_lead_guest`, r.ID); err != nil {
			t.Fatalf("apagando o titular: %v", err)
		}
		if _, err := tx.Exec(a.ctx, `UPDATE reservation_guests SET is_lead_guest = true WHERE reservation_id = $1 AND contact_id = $2`, r.ID, ana.ID); err != nil {
			t.Fatalf("a instrução deveria passar (a FK é adiada): %v", err)
		}
		return nil
	})
	exigirPG(err, "23503", "reservation_guests_titular_fkey", "COMMIT da lista trocada sem a reserva")

	// IMMEDIATE antecipa a recusa para a instrução.
	err = emTx("IMMEDIATE", func(tx pgx.Tx) error {
		if _, err := tx.Exec(a.ctx, `SET CONSTRAINTS reservation_guests_titular_fkey IMMEDIATE`); err != nil {
			return err
		}
		_, err := tx.Exec(a.ctx, `UPDATE reservations SET contact_id = $2 WHERE id = $1`, r.ID, ana.ID)
		return err
	})
	exigirPG(err, "23503", "reservation_guests_titular_fkey", "UPDATE com a FK em IMMEDIATE")

	// A coluna gerada não se escreve.
	_, err = a.pool.Exec(a.ctx, `INSERT INTO reservation_guests (reservation_id, contact_id, is_lead_guest, lead_contact_id) VALUES ($1, $2, false, $2)`,
		r.ID, estranho.ID)
	exigirPG(err, "428C9", "", "INSERT escrevendo lead_contact_id")

	a.qhTitularNoBanco(t, r.ID, "depois das recusas do banco")
	qhExigirLista(t, "a lista ficou como estava", a.qhLer(t, u.Token, r.ID).ids(), titular.ID, ana.ID)

	// As duas pontas, a reserva primeiro: comita.
	if err := emTx("reserva e lista", func(tx pgx.Tx) error {
		if _, err := tx.Exec(a.ctx, `UPDATE reservations SET contact_id = $2 WHERE id = $1`, r.ID, ana.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(a.ctx, `DELETE FROM reservation_guests WHERE reservation_id = $1 AND contact_id = $2`, r.ID, titular.ID); err != nil {
			return err
		}
		_, err := tx.Exec(a.ctx, `UPDATE reservation_guests SET is_lead_guest = true WHERE reservation_id = $1 AND contact_id = $2`, r.ID, ana.ID)
		return err
	}); err != nil {
		t.Fatalf("trocar as duas pontas (reserva primeiro) deveria comitar: %v", err)
	}
	if got := a.qhTitularNoBanco(t, r.ID, "depois da troca pelas duas pontas"); got != ana.ID {
		t.Fatalf("titular = %s, esperado a Ana", got)
	}
	// E a lista primeiro: comita também.
	if err := emTx("lista e reserva", func(tx pgx.Tx) error {
		if _, err := tx.Exec(a.ctx, `DELETE FROM reservation_guests WHERE reservation_id = $1 AND contact_id = $2`, r.ID, ana.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(a.ctx, `INSERT INTO reservation_guests (reservation_id, contact_id, is_lead_guest) VALUES ($1, $2, true)`, r.ID, titular.ID); err != nil {
			return err
		}
		_, err := tx.Exec(a.ctx, `UPDATE reservations SET contact_id = $2 WHERE id = $1`, r.ID, titular.ID)
		return err
	}); err != nil {
		t.Fatalf("trocar as duas pontas (lista primeiro) deveria comitar: %v", err)
	}
	a.qhTitularNoBanco(t, r.ID, "de volta ao titular original")
}

// Retirar o titular é 409 RESOURCE_IN_USE com `details.is_lead_guest: true` —
// troca-se o titular pela reserva — e a lista, a trilha e a linha do tempo
// ficam como estavam.
func TestHospedesQARetirarOTitularEh409EDeixaTudoComoEstava(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "hq-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qagFaxina(t, admin)
	c := a.qhMontarCenario(t, f, admin.Token, 560)
	antes := a.qhLerEstado(t, c)

	e := qbErro(t, a.qhRetirar(t, admin.Token, c.Reserva.ID, c.Titular.ID), http.StatusConflict, "RESOURCE_IN_USE", "retirar o titular")
	if e.Details["is_lead_guest"] != true {
		t.Errorf("details da recusa = %v; o contrato pede is_lead_guest: true", e.Details)
	}
	if depois := a.qhLerEstado(t, c); depois != antes {
		t.Fatalf("a recusa mexeu em algo:\n antes  %+v\n depois %+v", antes, depois)
	}
	a.qhTitularNoBanco(t, c.Reserva.ID, "depois da recusa")
}

// Descartar a reserva rascunho (`DELETE /reservations/{id}`, só em `quote`)
// leva a lista junto: as duas FKs da lista para a reserva são `ON DELETE
// CASCADE`, a do titular inclusive — a cascata não é adiada, e o descarte não
// pode parar num 500 nem deixar linha órfã.
func TestHospedesQADescartarARascunhoLevaAListaJunto(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	titular := f.qhPessoa(t, casa.Prop, "Titular")
	ana := f.qhPessoa(t, casa.Prop, "Ana")
	d := casa.Hoje.AddDays(570)
	rascunho := a.qhReservaPorSQL(t, casa.Prop, p.ID, titular.ID, "quote", d, d.AddDays(2), 2)
	a.executarQA(t, `INSERT INTO reservation_guests (reservation_id, contact_id) VALUES ($1, $2)`, rascunho, ana.ID)

	exigirStatusQB(t, a.chamar(t, http.MethodDelete, "/reservations/"+rascunho.String(), u.Token, nil), http.StatusNoContent,
		"DELETE /reservations/{id} do rascunho")
	if n := a.qhNomesNoBanco(t, rascunho); n != 0 {
		t.Fatalf("o descarte deixou %d linha(s) órfã(s) na lista", n)
	}
	if n := a.qagContar(t, `SELECT count(*) FROM contacts WHERE id = ANY($1::uuid[])`, []uuid.UUID{titular.ID, ana.ID}); n != 2 {
		t.Fatalf("o descarte apagou ficha(s): sobraram %d de 2", n)
	}
}
