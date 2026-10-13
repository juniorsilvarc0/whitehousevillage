//go:build integration

package router

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ─────────────────────────── O corpo do POST ────────────────────────────────

// O corpo do acréscimo é SÓ `contact_id`: campo desconhecido é 422 com o nome do
// campo em `details` — inclusive `is_lead_guest`, com qualquer valor (o titular
// se troca pela reserva) e `name`/`doc_number` (a pessoa nasce em
// `POST /contacts`, nunca aqui). Ausente, malformado e nulo também são 422. Em
// toda recusa, nada entra na lista e nada é registrado.
func TestHospedesQACorpoDoAcrescimoSoAceitaContactID(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	c := a.qhMontarCenario(t, f, u.Token, 900)
	antes := a.qhLerEstado(t, c)
	x := c.Fora.ID.String()

	// `campo` vazio: o contrato só promete o code (422 VALIDATION_ERROR), não
	// em que chave de `details` — é o caso do UUID malformado, que o decoder
	// recusa antes de saber de que campo se trata.
	for nome, caso := range map[string]struct {
		corpo, campo string
	}{
		"is_lead_guest true":    {`{"contact_id":"` + x + `","is_lead_guest":true}`, "is_lead_guest"},
		"is_lead_guest false":   {`{"contact_id":"` + x + `","is_lead_guest":false}`, "is_lead_guest"},
		"nome e documento":      {`{"contact_id":"` + x + `","name":"Fulano","doc_number":"12345678909"}`, "name"},
		"campo desconhecido":    {`{"contact_id":"` + x + `","quarto":"AP-01"}`, "quarto"},
		"contact_id ausente":    {`{}`, "contact_id"},
		"contact_id malformado": {`{"contact_id":"nao-e-uuid"}`, ""},
		"contact_id nulo":       {`{"contact_id":null}`, "contact_id"},
	} {
		t.Run(nome, func(t *testing.T) {
			e := qbErro(t, a.qbChamarCru(t, http.MethodPost, qhCaminho(c.Reserva.ID), u.Token, caso.corpo),
				http.StatusUnprocessableEntity, "VALIDATION_ERROR", nome)
			if _, ok := e.Details[caso.campo]; caso.campo != "" && !ok {
				t.Errorf("%s: a recusa não aponta %q em details: %v", nome, caso.campo, e.Details)
			}
		})
	}
	for nome, corpo := range map[string]string{"corpo vazio": ``, "corpo null": `null`, "corpo lista": `[]`} {
		r := a.qbChamarCru(t, http.MethodPost, qhCaminho(c.Reserva.ID), u.Token, corpo)
		if r.Status != http.StatusUnprocessableEntity && r.Status != http.StatusBadRequest {
			t.Errorf("%s: status %d — corpo inválido não pode ser aceito nem virar 500: %s", nome, r.Status, r.Corpo)
		}
	}
	if depois := a.qhLerEstado(t, c); depois != antes {
		t.Fatalf("um corpo recusado mexeu em algo:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// ─────────────────────────── As respostas ───────────────────────────────────

// O 201 traz `Location` com o caminho do DELETE do vínculo
// (`/api/v1/reservations/{id}/guests/{contactId}`) e, no corpo, a pessoa como
// a lista a mostra; o DELETE é 204 sem corpo; id malformado no caminho é 404
// (não 422, que confirmaria que o endereço existe); a lista de uma reserva sem
// nenhuma linha é `[]`, nunca `null`.
func TestHospedesQAFormatoDasRespostas(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	c := a.qhMontarCenario(t, f, u.Token, 910)

	post := a.qhAcrescentar(t, u.Token, c.Reserva.ID, c.Fora.ID)
	h := qbDado[qhHospede](t, post, http.StatusCreated, "POST /guests")
	if loc := post.Headers.Get("Location"); loc != PrefixoDaAPI+qhCaminho(c.Reserva.ID)+"/"+c.Fora.ID.String() {
		t.Errorf("Location = %q", loc)
	}
	lida := a.qhLer(t, u.Token, c.Reserva.ID)
	achou := false
	for _, x := range lida.Data {
		if x.ContactID == h.ContactID {
			achou = true
			if !qhMesmoHospede(x, h) {
				t.Errorf("o POST devolveu %+v; a lista mostra a mesma pessoa como %+v", h, x)
			}
		}
	}
	if !achou {
		t.Fatal("a pessoa do 201 não está na lista")
	}
	del := a.chamar(t, http.MethodDelete, post.Headers.Get("Location")[len(PrefixoDaAPI):], u.Token, nil)
	exigirStatusQB(t, del, http.StatusNoContent, "DELETE no Location do 201")
	if len(del.Corpo) != 0 {
		t.Errorf("204 com corpo: %s", del.Corpo)
	}

	qbErro(t, a.chamar(t, http.MethodDelete, qhCaminho(c.Reserva.ID)+"/nao-e-uuid", u.Token, nil), http.StatusNotFound, "NOT_FOUND",
		"DELETE com contactId malformado")
	qbErro(t, a.chamar(t, http.MethodGet, "/reservations/nao-e-uuid/guests", u.Token, nil), http.StatusNotFound, "NOT_FOUND",
		"GET com id de reserva malformado")
	qbErro(t, a.chamar(t, http.MethodGet, qhCaminho(uuid.New()), u.Token, nil), http.StatusNotFound, "NOT_FOUND",
		"GET de reserva inexistente")
	qbErro(t, a.qhAcrescentar(t, u.Token, uuid.New(), c.Fora.ID), http.StatusNotFound, "NOT_FOUND", "POST em reserva inexistente")
	qbErro(t, a.qhRetirar(t, u.Token, uuid.New(), c.Fora.ID), http.StatusNotFound, "NOT_FOUND", "DELETE em reserva inexistente")

	// Reserva sem nenhuma linha na lista (criada fora da API): `[]` e nenhuma
	// leitura registrada — não exibiu ninguém.
	vazia := a.qhReservaPorSQL(t, c.Casa.Prop, c.Produto.ID, c.Titular.ID, "closed", c.Casa.Hoje.AddDays(915), c.Casa.Hoje.AddDays(917), 2)
	a.executarQA(t, `DELETE FROM reservation_guests WHERE reservation_id = $1`, vazia)
	antes := a.qhLeiturasDoAtor(t, u.ID)
	cru := a.chamar(t, http.MethodGet, qhCaminho(vazia), u.Token, nil)
	exigirStatusQB(t, cru, http.StatusOK, "GET /guests da lista vazia")
	if !strings.Contains(strings.ReplaceAll(string(cru.Corpo), " ", ""), `"data":[]`) {
		t.Errorf("lista vazia não veio como []: %s", cru.Corpo)
	}
	if n := a.qhLeiturasDoAtor(t, u.ID); n != antes {
		t.Errorf("a lista vazia gravou %d leitura(s)", n-antes)
	}
}

// O `/full` traz a MESMA lista de `GET /guests` (mesmo schema, mesma ordem,
// documento cheio), o `guests_editable` igual ao `meta.editable`, e
// `reservation.contact_phone_e164` nulo — o telefone do titular está em
// `guests`, e "nas demais respostas vem null".
//
// A ordem: o titular primeiro; os acompanhantes por nome e, no EMPATE, por
// `contact_id` — homônimo não troca de lugar entre duas leituras.
func TestHospedesQAFullTrazAMesmaListaNaMesmaOrdem(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	titular := f.qhPessoa(t, casa.Prop, "Zuleica")
	ana := f.qhPessoa(t, casa.Prop, "Ana")
	homonimo1 := f.qhPessoa(t, casa.Prop, "Homonimo")
	homonimo2 := f.qhPessoa(t, casa.Prop, "Homonimo")
	nome := "Joao da Silva " + qbSufixo()
	for _, h := range []*qhPessoa{&homonimo1, &homonimo2} {
		a.executarQA(t, `UPDATE contacts SET name = $2 WHERE id = $1`, h.ID, nome)
		h.Nome = nome
	}
	d := casa.Hoje.AddDays(920)
	r := a.qhReservar(t, u.Token, p.ID, titular.ID, d, d.AddDays(2), 4)
	// Acrescentados fora da ordem esperada.
	for _, x := range []qhPessoa{homonimo2, ana, homonimo1} {
		a.qhAcrescentarOK(t, u.Token, r.ID, x.ID)
	}

	quer := append([]uuid.UUID{titular.ID}, qhPorNome(ana, homonimo1, homonimo2)...)
	for leitura := 1; leitura <= 2; leitura++ {
		qhExigirLista(t, "GET /guests", a.qhLer(t, u.Token, r.ID).ids(), quer...)
	}
	lida := a.qhLer(t, u.Token, r.ID)
	completa, _ := a.qhCompleta(t, u.Token, r.ID)
	if len(completa.Hospedes) != len(lida.Data) {
		t.Fatalf("/full.guests tem %d, /guests tem %d", len(completa.Hospedes), len(lida.Data))
	}
	for i := range lida.Data {
		if !qhMesmoHospede(completa.Hospedes[i], lida.Data[i]) {
			t.Errorf("posição %d: /full %+v, /guests %+v", i, completa.Hospedes[i], lida.Data[i])
		}
		if completa.Hospedes[i].Documento == nil || completa.Hospedes[i].DocTipo == nil {
			t.Errorf("/full.guests[%d] sem doc_type/doc_number: %+v", i, completa.Hospedes[i])
		}
	}
	if completa.Editavel == nil || *completa.Editavel != lida.Meta.Editable {
		t.Errorf("guests_editable = %v, meta.editable = %v", completa.Editavel, lida.Meta.Editable)
	}
	if completa.Reserva.ContactPhone != nil {
		t.Errorf("/full.reservation.contact_phone_e164 = %q; o contrato diz null fora de GET /reservations", *completa.Reserva.ContactPhone)
	}
}
