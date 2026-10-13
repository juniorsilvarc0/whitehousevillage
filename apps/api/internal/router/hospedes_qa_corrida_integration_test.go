//go:build integration

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// As corridas que o desenho da rooming list diz ter fechado com TRAVA (a linha
// da reserva em `FOR UPDATE`, a ficha em `FOR KEY SHARE`) e com as garantias do
// banco — e não com SELECT antes do INSERT. Os nomes levam "Disputa" ou
// "Corrida" para entrarem na repetição de `make it-concorrencia`.
//
// Em toda rodada o banco é a testemunha final: a lista nunca passa do número
// vendido, tem exatamente UM titular e ele é o `contact_id` da reserva, e
// nenhuma ficha anonimizada fica numa lista de pé. E ninguém recebe 500.

// qhLargada dispara as chamadas juntas e devolve o desfecho de cada uma, na
// ordem em que foram pedidas.
func qhLargada(chamadas ...func() (resposta, error)) []string {
	var (
		espera sync.WaitGroup
		saida  = make([]string, len(chamadas))
	)
	largada := make(chan struct{})
	for i, c := range chamadas {
		espera.Add(1)
		go func(i int, c func() (resposta, error)) {
			defer espera.Done()
			<-largada
			saida[i] = qhDesfecho(c())
		}(i, c)
	}
	close(largada)
	espera.Wait()
	return saida
}

func qhContagem(desfechos []string) map[string]int {
	out := map[string]int{}
	for _, d := range desfechos {
		out[d]++
	}
	return out
}

func qhSemQuinhentos(t *testing.T, contexto string, desfechos []string) {
	t.Helper()
	for _, d := range desfechos {
		if strings.HasPrefix(d, "5") || strings.HasPrefix(d, "erro") {
			t.Fatalf("%s: alguém recebeu %s — o contrato diz nunca 500 (%v)", contexto, d, qhContagem(desfechos))
		}
	}
}

// ─────────────────────────── 1. O último lugar ──────────────────────────────

// Em linguagem de negócio: dois balcões (aqui, oito) tentam pôr na lista, no
// mesmo instante, pessoas diferentes para o ÚLTIMO lugar vendido. Exatamente uma
// entra; as outras ouvem "a lista já tem o número de hóspedes" (422
// GUEST_LIST_FULL) — nunca 500, nunca duas dentro.
func TestHospedesQADisputaPeloUltimoLugarEntreOitoBalcoes(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	titular := f.qhPessoa(t, casa.Prop, "Titular")
	d := casa.Hoje.AddDays(800)
	r := a.qhReservar(t, u.Token, p.ID, titular.ID, d, d.AddDays(2), 2)

	const balcoes = 8
	for rodada := 1; rodada <= 3; rodada++ {
		candidatos := make([]qhPessoa, balcoes)
		chamadas := make([]func() (resposta, error), balcoes)
		for i := range candidatos {
			candidatos[i] = f.qhPessoa(t, casa.Prop, fmt.Sprintf("Candidato %d", i))
			id := candidatos[i].ID
			chamadas[i] = func() (resposta, error) {
				return a.qhChamarSeguro(http.MethodPost, qhCaminho(r.ID), u.Token, "", map[string]any{"contact_id": id})
			}
		}
		desfechos := qhLargada(chamadas...)
		qhSemQuinhentos(t, fmt.Sprintf("rodada %d", rodada), desfechos)
		c := qhContagem(desfechos)
		if c["201:"] != 1 || c["422:GUEST_LIST_FULL"] != balcoes-1 {
			t.Fatalf("rodada %d: %v — esperado exatamente um 201 e %d GUEST_LIST_FULL", rodada, c, balcoes-1)
		}
		if n := a.qhNomesNoBanco(t, r.ID); n != 2 {
			t.Fatalf("rodada %d: a lista tem %d nomes para 2 vendidos", rodada, n)
		}
		a.qhTitularNoBanco(t, r.ID, fmt.Sprintf("rodada %d", rodada))
		for i, d := range desfechos {
			if d == "201:" {
				exigirStatusQB(t, a.qhRetirar(t, u.Token, r.ID, candidatos[i].ID), http.StatusNoContent, "liberando o lugar")
			}
		}
	}
}

// A MESMA pessoa, pedida por seis balcões ao mesmo tempo: entra uma vez; os
// outros cinco ouvem "já está na lista" (409 CODE_IN_USE).
func TestHospedesQADisputaDaMesmaPessoaPorSeisBalcoes(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	titular := f.qhPessoa(t, casa.Prop, "Titular")
	d := casa.Hoje.AddDays(810)
	r := a.qhReservar(t, u.Token, p.ID, titular.ID, d, d.AddDays(2), 6)

	for rodada := 1; rodada <= 3; rodada++ {
		quem := f.qhPessoa(t, casa.Prop, "Repetida")
		chamadas := make([]func() (resposta, error), 6)
		for i := range chamadas {
			chamadas[i] = func() (resposta, error) {
				return a.qhChamarSeguro(http.MethodPost, qhCaminho(r.ID), u.Token, "", map[string]any{"contact_id": quem.ID})
			}
		}
		desfechos := qhLargada(chamadas...)
		qhSemQuinhentos(t, fmt.Sprintf("rodada %d", rodada), desfechos)
		if c := qhContagem(desfechos); c["201:"] != 1 || c["409:CODE_IN_USE"] != 5 {
			t.Fatalf("rodada %d: %v — esperado um 201 e cinco CODE_IN_USE", rodada, c)
		}
		if n := a.qagContar(t, `SELECT count(*) FROM reservation_guests WHERE reservation_id = $1 AND contact_id = $2`, r.ID, quem.ID); n != 1 {
			t.Fatalf("rodada %d: a pessoa está %d vezes na lista", rodada, n)
		}
	}
}

// ─────────────────────────── 2. A troca de titular contra o acréscimo ───────

// No mesmo instante, a gestão troca o titular da reserva pela pessoa X e a
// recepção acrescenta X como acompanhante. Qualquer que seja a ordem, a lista
// termina com X como ÚNICO titular, o titular anterior fora, e nenhum 500: o
// PATCH passa sempre; o POST entra (e X é promovido depois) ou ouve 409
// CODE_IN_USE (X já é o titular).
//
// E a variante com pessoas diferentes: trocar o titular por B enquanto se
// acrescenta Y, com a lista no limite — as duas passam, sempre, porque a troca
// tira um e põe um.
func TestHospedesQACorridaEntreTrocarOTitularEAcrescentar(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 2)
	primeiro := f.qhPessoa(t, casa.Prop, "Titular")
	d := casa.Hoje.AddDays(820)

	// A mesma pessoa nos dois pedidos.
	r := a.qhReservar(t, u.Token, p.ID, primeiro.ID, d, d.AddDays(2), 3)
	for rodada := 1; rodada <= 5; rodada++ {
		x := f.qhPessoa(t, casa.Prop, "X")
		desfechos := qhLargada(
			func() (resposta, error) {
				return a.qhChamarSeguro(http.MethodPatch, "/reservations/"+r.ID.String(), u.Token, "", map[string]any{"contact_id": x.ID})
			},
			func() (resposta, error) {
				return a.qhChamarSeguro(http.MethodPost, qhCaminho(r.ID), u.Token, "", map[string]any{"contact_id": x.ID})
			},
		)
		qhSemQuinhentos(t, fmt.Sprintf("mesma pessoa, rodada %d", rodada), desfechos)
		if desfechos[0] != "200:" || (desfechos[1] != "201:" && desfechos[1] != "409:CODE_IN_USE") {
			t.Fatalf("mesma pessoa, rodada %d: PATCH %s, POST %s — esperado PATCH 200 e POST 201 ou 409 CODE_IN_USE", rodada, desfechos[0], desfechos[1])
		}
		if got := a.qhTitularNoBanco(t, r.ID, fmt.Sprintf("mesma pessoa, rodada %d", rodada)); got != x.ID {
			t.Fatalf("mesma pessoa, rodada %d: titular = %s, esperado X", rodada, got)
		}
		lista := a.qhLer(t, u.Token, r.ID).ids()
		qhExigirLista(t, fmt.Sprintf("mesma pessoa, rodada %d (o anterior sai)", rodada), lista, x.ID)
	}

	// Pessoas diferentes, lista no limite (2 vendidos, só o titular): as duas passam.
	r2 := a.qhReservar(t, u.Token, p.ID, primeiro.ID, d, d.AddDays(2), 2)
	for rodada := 1; rodada <= 5; rodada++ {
		b := f.qhPessoa(t, casa.Prop, "B")
		y := f.qhPessoa(t, casa.Prop, "Y")
		desfechos := qhLargada(
			func() (resposta, error) {
				return a.qhChamarSeguro(http.MethodPatch, "/reservations/"+r2.ID.String(), u.Token, "", map[string]any{"contact_id": b.ID})
			},
			func() (resposta, error) {
				return a.qhChamarSeguro(http.MethodPost, qhCaminho(r2.ID), u.Token, "", map[string]any{"contact_id": y.ID})
			},
		)
		qhSemQuinhentos(t, fmt.Sprintf("pessoas diferentes, rodada %d", rodada), desfechos)
		if desfechos[0] != "200:" || desfechos[1] != "201:" {
			t.Fatalf("pessoas diferentes, rodada %d: PATCH %s, POST %s — a troca tira um e põe um, as duas cabem", rodada, desfechos[0], desfechos[1])
		}
		if got := a.qhTitularNoBanco(t, r2.ID, fmt.Sprintf("pessoas diferentes, rodada %d", rodada)); got != b.ID {
			t.Fatalf("pessoas diferentes, rodada %d: titular = %s, esperado B", rodada, got)
		}
		lista := a.qhLer(t, u.Token, r2.ID).ids()
		sort.Slice(lista, func(i, j int) bool { return lista[i].String() < lista[j].String() })
		quer := []uuid.UUID{b.ID, y.ID}
		sort.Slice(quer, func(i, j int) bool { return quer[i].String() < quer[j].String() })
		qhExigirLista(t, fmt.Sprintf("pessoas diferentes, rodada %d", rodada), lista, quer...)
		// Próxima rodada começa com a lista no limite de novo.
		exigirStatusQB(t, a.qhRetirar(t, u.Token, r2.ID, y.ID), http.StatusNoContent, "liberando o lugar")
	}
}

// Duas trocas de titular simultâneas, por pessoas diferentes: uma vence por
// último, e a lista termina com UM titular — o da reserva — e sem nenhum dos
// anteriores.
func TestHospedesQACorridaEntreDuasTrocasDeTitular(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	primeiro := f.qhPessoa(t, casa.Prop, "Titular")
	acomp := f.qhPessoa(t, casa.Prop, "Acompanhante")
	d := casa.Hoje.AddDays(830)
	r := a.qhReservar(t, u.Token, p.ID, primeiro.ID, d, d.AddDays(2), 3)
	a.qhAcrescentarOK(t, u.Token, r.ID, acomp.ID)

	for rodada := 1; rodada <= 5; rodada++ {
		b := f.qhPessoa(t, casa.Prop, "B")
		c := f.qhPessoa(t, casa.Prop, "C")
		desfechos := qhLargada(
			func() (resposta, error) {
				return a.qhChamarSeguro(http.MethodPatch, "/reservations/"+r.ID.String(), u.Token, "", map[string]any{"contact_id": b.ID})
			},
			func() (resposta, error) {
				return a.qhChamarSeguro(http.MethodPatch, "/reservations/"+r.ID.String(), u.Token, "", map[string]any{"contact_id": c.ID})
			},
		)
		qhSemQuinhentos(t, fmt.Sprintf("rodada %d", rodada), desfechos)
		if desfechos[0] != "200:" || desfechos[1] != "200:" {
			t.Fatalf("rodada %d: %v — as duas trocas são válidas e passam", rodada, desfechos)
		}
		got := a.qhTitularNoBanco(t, r.ID, fmt.Sprintf("rodada %d", rodada))
		if got != b.ID && got != c.ID {
			t.Fatalf("rodada %d: titular = %s, nenhum dos dois pedidos", rodada, got)
		}
		lista := a.qhLer(t, u.Token, r.ID).ids()
		qhExigirLista(t, fmt.Sprintf("rodada %d", rodada), lista, got, acomp.ID)
	}
}

// ─────────────────────────── 3. O número contra o nome ──────────────────────

// No mesmo instante, a gestão baixa a reserva de 2 para 1 hóspede e a
// recepção põe o segundo nome. Um dos dois perde: ou o número baixa e o nome é
// recusado (GUEST_LIST_FULL), ou o nome entra e o número é recusado (422 em
// guests_count). Nunca os dois — a lista nunca termina maior que o vendido.
func TestHospedesQACorridaEntreBaixarOsHospedesEAcrescentar(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	titular := f.qhPessoa(t, casa.Prop, "Titular")
	d := casa.Hoje.AddDays(840)
	r := a.qhReservar(t, u.Token, p.ID, titular.ID, d, d.AddDays(2), 2)
	caminho := "/reservations/" + r.ID.String()

	for rodada := 1; rodada <= 6; rodada++ {
		x := f.qhPessoa(t, casa.Prop, "Segundo")
		desfechos := qhLargada(
			func() (resposta, error) {
				return a.qhChamarSeguro(http.MethodPatch, caminho, u.Token, "", map[string]any{"guests_count": 1})
			},
			func() (resposta, error) {
				return a.qhChamarSeguro(http.MethodPost, qhCaminho(r.ID), u.Token, "", map[string]any{"contact_id": x.ID})
			},
		)
		qhSemQuinhentos(t, fmt.Sprintf("rodada %d", rodada), desfechos)
		par := desfechos[0] + " | " + desfechos[1]
		if par != "200: | 422:GUEST_LIST_FULL" && par != "422:VALIDATION_ERROR | 201:" {
			t.Fatalf("rodada %d: PATCH %s, POST %s — um dos dois tem de perder", rodada, desfechos[0], desfechos[1])
		}
		vendidos := a.qagContar(t, `SELECT guests_count FROM reservations WHERE id = $1`, r.ID)
		if n := a.qhNomesNoBanco(t, r.ID); n > vendidos {
			t.Fatalf("rodada %d: %d nomes para %d vendido(s)", rodada, n, vendidos)
		}
		// Volta ao ponto de partida: 2 vendidos, só o titular.
		if desfechos[1] == "201:" {
			exigirStatusQB(t, a.qhRetirar(t, u.Token, r.ID, x.ID), http.StatusNoContent, "retirando")
		}
		exigirStatusQB(t, a.chamar(t, http.MethodPatch, caminho, u.Token, map[string]any{"guests_count": 2}), http.StatusOK, "voltando a 2")
	}
}

// ─────────────────────────── 4. A anonimização contra o acréscimo ───────────

// Em linguagem de negócio: no mesmo instante em que a casa atende ao pedido de
// eliminação de dados de uma pessoa, a recepção a põe na lista de uma reserva
// de pé. Uma das duas tem de perder: ou ela é anonimizada e o acréscimo ouve
// 409 CONTACT_ANONYMIZED, ou ela entra na lista e a anonimização ouve 409
// RESOURCE_IN_USE (está numa estadia viva). NUNCA uma ficha anonimizada numa
// lista de pé.
func TestHospedesQACorridaEntreAnonimizarEAcrescentar(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "hq-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qagFaxina(t, admin)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	titular := f.qhPessoa(t, casa.Prop, "Titular")
	d := casa.Hoje.AddDays(850)
	// Lugar para todas as rodadas: o que se mede é a anonimização, não o teto.
	if _, err := a.pool.Exec(a.ctx, `UPDATE unit_types SET capacity = 12 WHERE id = $1`, p.ID); err != nil {
		t.Fatal(err)
	}
	r := a.qhReservar(t, admin.Token, p.ID, titular.ID, d, d.AddDays(2), 9)

	vistos := map[string]int{}
	for rodada := 1; rodada <= 6; rodada++ {
		x := f.qhPessoa(t, casa.Prop, "Quer ser esquecida")
		desfechos := qhLargada(
			func() (resposta, error) {
				return a.qhChamarSeguro(http.MethodPost, "/contacts/"+x.ID.String()+"/anonymize", admin.Token, "",
					map[string]any{"reason": "pedido da titular dos dados"})
			},
			func() (resposta, error) {
				return a.qhChamarSeguro(http.MethodPost, qhCaminho(r.ID), admin.Token, "", map[string]any{"contact_id": x.ID})
			},
		)
		qhSemQuinhentos(t, fmt.Sprintf("rodada %d", rodada), desfechos)
		par := desfechos[0] + " | " + desfechos[1]
		vistos[par]++
		if par != "200: | 409:CONTACT_ANONYMIZED" && par != "409:RESOURCE_IN_USE | 201:" {
			t.Fatalf("rodada %d: anonimizar %s, acrescentar %s — uma das duas tem de perder", rodada, desfechos[0], desfechos[1])
		}
		if n := a.qagContar(t, `
			SELECT count(*) FROM reservation_guests g
			  JOIN reservations r ON r.id = g.reservation_id
			  JOIN contacts c ON c.id = g.contact_id
			 WHERE r.status IN ('hold', 'confirmed', 'checked_in') AND c.anonymized_at IS NOT NULL AND r.id = $1`, r.ID); n != 0 {
			t.Fatalf("rodada %d: %d ficha(s) anonimizada(s) numa lista de pé", rodada, n)
		}
	}
	t.Logf("desfechos observados: %v", vistos)
}

// ─────────────────────────── 5. A troca de titular contra a remarcação ──────

// No mesmo instante, a gestão troca o titular e remarca a estadia. Ou a troca
// vem antes, e a reserva nova nasce com o titular NOVO e o acompanhante; ou a
// remarcação vem antes, e a troca na original cancelada é 409. Em qualquer
// ordem, cada uma das duas reservas tem exatamente um titular — o dela.
func TestHospedesQACorridaEntreTrocarOTitularERemarcar(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)

	for rodada := 1; rodada <= 4; rodada++ {
		titular := f.qhPessoa(t, casa.Prop, "Titular")
		acomp := f.qhPessoa(t, casa.Prop, "Acompanhante")
		novo := f.qhPessoa(t, casa.Prop, "Novo")
		d := casa.Hoje.AddDays(860 + 10*rodada)
		r := a.qhReservar(t, u.Token, p.ID, titular.ID, d, d.AddDays(2), 2)
		a.qhAcrescentarOK(t, u.Token, r.ID, acomp.ID)
		chave := a.chaveNova(t, "rooming-corrida-remarca")
		desfechos := qhLargada(
			func() (resposta, error) {
				return a.qhChamarSeguro(http.MethodPatch, "/reservations/"+r.ID.String(), u.Token, "", map[string]any{"contact_id": novo.ID})
			},
			func() (resposta, error) {
				return a.qhChamarSeguro(http.MethodPost, "/reservations/"+r.ID.String()+"/reschedule", u.Token, chave,
					map[string]any{"check_in": d.AddDays(4).String(), "check_out": d.AddDays(6).String()})
			},
		)
		qhSemQuinhentos(t, fmt.Sprintf("rodada %d", rodada), desfechos)
		if desfechos[1] != "201:" || (desfechos[0] != "200:" && desfechos[0] != "409:INVALID_STATE_TRANSITION") {
			t.Fatalf("rodada %d: troca %s, remarcação %s", rodada, desfechos[0], desfechos[1])
		}
		var nova uuid.UUID
		if err := a.pool.QueryRow(a.ctx, `SELECT id FROM reservations WHERE rebooked_from_id = $1`, r.ID).Scan(&nova); err != nil {
			t.Fatalf("rodada %d: a reserva nova: %v", rodada, err)
		}
		titularDaOriginal := a.qhTitularNoBanco(t, r.ID, fmt.Sprintf("rodada %d, original", rodada))
		titularDaNova := a.qhTitularNoBanco(t, nova, fmt.Sprintf("rodada %d, nova", rodada))
		quer := titular.ID
		if desfechos[0] == "200:" {
			quer = novo.ID // a troca veio antes: a nova nasce com o titular novo
		}
		if titularDaNova != quer || titularDaOriginal != quer {
			t.Fatalf("rodada %d (troca %s): titular da original %s, da nova %s; esperado %s nas duas",
				rodada, desfechos[0], titularDaOriginal, titularDaNova, quer)
		}
		qhExigirLista(t, fmt.Sprintf("rodada %d, a nova leva o acompanhante", rodada), a.qhLer(t, u.Token, nova).ids(), quer, acomp.ID)
	}
}

// No mesmo instante, a gestão promove o acompanhante X a titular e a recepção
// tira X da lista. Se a retirada vem antes, a troca põe X de volta, como
// titular; se a troca vem antes, a retirada ouve 409 RESOURCE_IN_USE (X agora é
// o titular). Nos dois casos a lista termina com X como único titular e sem o
// anterior — e ninguém recebe 500.
func TestHospedesQACorridaEntreTrocarOTitularERetirarAMesmaPessoa(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	primeiro := f.qhPessoa(t, casa.Prop, "Titular")
	d := casa.Hoje.AddDays(890)
	r := a.qhReservar(t, u.Token, p.ID, primeiro.ID, d, d.AddDays(2), 3)

	vistos := map[string]int{}
	for rodada := 1; rodada <= 6; rodada++ {
		x := f.qhPessoa(t, casa.Prop, "X")
		a.qhAcrescentarOK(t, u.Token, r.ID, x.ID)
		desfechos := qhLargada(
			func() (resposta, error) {
				return a.qhChamarSeguro(http.MethodPatch, "/reservations/"+r.ID.String(), u.Token, "", map[string]any{"contact_id": x.ID})
			},
			func() (resposta, error) {
				return a.qhChamarSeguro(http.MethodDelete, qhCaminho(r.ID)+"/"+x.ID.String(), u.Token, "", nil)
			},
		)
		qhSemQuinhentos(t, fmt.Sprintf("rodada %d", rodada), desfechos)
		vistos[desfechos[0]+" | "+desfechos[1]]++
		if desfechos[0] != "200:" || (desfechos[1] != "204:" && desfechos[1] != "409:RESOURCE_IN_USE") {
			t.Fatalf("rodada %d: troca %s, retirada %s", rodada, desfechos[0], desfechos[1])
		}
		if got := a.qhTitularNoBanco(t, r.ID, fmt.Sprintf("rodada %d", rodada)); got != x.ID {
			t.Fatalf("rodada %d: titular = %s, esperado X", rodada, got)
		}
		qhExigirLista(t, fmt.Sprintf("rodada %d", rodada), a.qhLer(t, u.Token, r.ID).ids(), x.ID)
	}
	t.Logf("desfechos observados: %v", vistos)
}

// As outras duas portas da regra de entrada (`guests.Admit`) contra a
// anonimização, no mesmo instante: a TROCA de titular para X e a RESERVA que
// nasce com X. Em cada rodada, uma das duas perde — ou X é anonimizado e a
// escrita ouve 409 CONTACT_ANONYMIZED, ou X vira titular de uma reserva de pé e
// a anonimização ouve 409 RESOURCE_IN_USE. Nunca uma reserva de pé com titular
// anonimizado.
func TestHospedesQACorridaEntreAnonimizarETornarTitular(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "hq-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qagFaxina(t, admin)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	titular := f.qhPessoa(t, casa.Prop, "Titular")
	d := casa.Hoje.AddDays(940)
	r := a.qhReservar(t, admin.Token, p.ID, titular.ID, d, d.AddDays(2), 3)

	anonimizar := func(x uuid.UUID) func() (resposta, error) {
		return func() (resposta, error) {
			return a.qhChamarSeguro(http.MethodPost, "/contacts/"+x.String()+"/anonymize", admin.Token, "",
				map[string]any{"reason": "pedido do titular dos dados"})
		}
	}
	semTitularAnonimizado := func(contexto string) {
		t.Helper()
		if n := a.qagContar(t, `
			SELECT count(*) FROM reservations r JOIN contacts c ON c.id = r.contact_id
			 WHERE r.unit_type_id = $1 AND r.status IN ('hold', 'confirmed', 'checked_in') AND c.anonymized_at IS NOT NULL`, p.ID); n != 0 {
			t.Fatalf("%s: %d reserva(s) de pé com titular anonimizado", contexto, n)
		}
	}

	vistos := map[string]int{}
	for rodada := 1; rodada <= 5; rodada++ {
		// A troca de titular.
		x := f.qhPessoa(t, casa.Prop, "Troca")
		desfechos := qhLargada(anonimizar(x.ID), func() (resposta, error) {
			return a.qhChamarSeguro(http.MethodPatch, "/reservations/"+r.ID.String(), admin.Token, "", map[string]any{"contact_id": x.ID})
		})
		qhSemQuinhentos(t, fmt.Sprintf("troca, rodada %d", rodada), desfechos)
		par := "troca: " + desfechos[0] + " | " + desfechos[1]
		vistos[par]++
		if par != "troca: 200: | 409:CONTACT_ANONYMIZED" && par != "troca: 409:RESOURCE_IN_USE | 200:" {
			t.Fatalf("rodada %d: anonimizar %s, trocar o titular %s — uma das duas tem de perder", rodada, desfechos[0], desfechos[1])
		}
		semTitularAnonimizado(fmt.Sprintf("troca, rodada %d", rodada))
		a.qhTitularNoBanco(t, r.ID, fmt.Sprintf("troca, rodada %d", rodada))

		// A reserva que nasce.
		y := f.qhPessoa(t, casa.Prop, "Nova venda")
		dd := d.AddDays(10 + 3*rodada)
		chave := a.chaveNova(t, "rooming-corrida-anon")
		desfechos = qhLargada(anonimizar(y.ID), func() (resposta, error) {
			return a.qhChamarSeguro(http.MethodPost, "/reservations", admin.Token, chave, map[string]any{
				"unit_type_id": p.ID, "contact_id": y.ID, "check_in": dd.String(), "check_out": dd.AddDays(2).String(), "guests_count": 2,
			})
		})
		qhSemQuinhentos(t, fmt.Sprintf("criação, rodada %d", rodada), desfechos)
		par = "criação: " + desfechos[0] + " | " + desfechos[1]
		vistos[par]++
		if par != "criação: 200: | 409:CONTACT_ANONYMIZED" && par != "criação: 409:RESOURCE_IN_USE | 201:" {
			t.Fatalf("rodada %d: anonimizar %s, criar a reserva %s — uma das duas tem de perder", rodada, desfechos[0], desfechos[1])
		}
		semTitularAnonimizado(fmt.Sprintf("criação, rodada %d", rodada))
	}
	t.Logf("desfechos observados: %v", vistos)
}

// O `/full` existe para a tela da reserva não mostrar "meia tela" (contrato:
// cinco chamadas encadeadas são cinco chances de um estado intermediário). Com
// a gestão trocando o titular enquanto a recepção abre a reserva, CADA resposta
// do `/full` tem de ser coerente: o `contact_id`/`contact_name` da reserva e o
// titular marcado em `guests` são a mesma pessoa — o critério "o /full e o
// cartão Cliente batem" vale em toda leitura, não só em repouso.
func TestHospedesQACorridaEntreAbrirOFullETrocarOTitular(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	ana := f.qhPessoa(t, casa.Prop, "Ana")
	beto := f.qhPessoa(t, casa.Prop, "Beto")
	d := casa.Hoje.AddDays(980)
	r := a.qhReservar(t, u.Token, p.ID, ana.ID, d, d.AddDays(2), 3)
	caminho := "/reservations/" + r.ID.String()

	const trocas, leituras = 40, 120
	var (
		espera         sync.WaitGroup
		mu             sync.Mutex
		incoerentes    []string
		falhas         []string
		leiturasFeitas int
	)
	largada := make(chan struct{})
	espera.Add(2)
	go func() {
		defer espera.Done()
		<-largada
		for i := 0; i < trocas; i++ {
			quem := beto.ID
			if i%2 == 1 {
				quem = ana.ID
			}
			resp, err := a.qhChamarSeguro(http.MethodPatch, caminho, u.Token, "", map[string]any{"contact_id": quem})
			if err != nil || resp.Status != http.StatusOK {
				mu.Lock()
				falhas = append(falhas, "PATCH "+qhDesfecho(resp, err))
				mu.Unlock()
			}
		}
	}()
	go func() {
		defer espera.Done()
		<-largada
		for i := 0; i < leituras; i++ {
			resp, err := a.qhChamarSeguro(http.MethodGet, caminho+"/full", u.Token, "", nil)
			if err != nil || resp.Status != http.StatusOK {
				mu.Lock()
				falhas = append(falhas, "GET /full "+qhDesfecho(resp, err))
				mu.Unlock()
				continue
			}
			var env struct {
				Data qhCompleta `json:"data"`
			}
			if err := json.Unmarshal(resp.Corpo, &env); err != nil {
				mu.Lock()
				falhas = append(falhas, "GET /full ilegível: "+err.Error())
				mu.Unlock()
				continue
			}
			c := env.Data
			var titulares []qhHospede
			for _, h := range c.Hospedes {
				if h.Titular {
					titulares = append(titulares, h)
				}
			}
			mu.Lock()
			leiturasFeitas++
			if len(titulares) != 1 || titulares[0].ContactID != c.Reserva.ContactID || titulares[0].Nome != c.Reserva.ContactName {
				marcados := make([]string, len(titulares))
				for i, h := range titulares {
					marcados[i] = fmt.Sprintf("%s %q", h.ContactID, h.Nome)
				}
				incoerentes = append(incoerentes, fmt.Sprintf("reservation.contact = %s %q, titular em guests = %v",
					c.Reserva.ContactID, c.Reserva.ContactName, marcados))
			}
			mu.Unlock()
		}
	}()
	close(largada)
	espera.Wait()

	if len(falhas) > 0 {
		t.Errorf("respostas fora do esperado durante a corrida: %v", falhas)
	}
	if len(incoerentes) > 0 {
		t.Errorf("%d de %d leituras do /full saíram com a reserva e a lista dizendo titulares diferentes (meia tela). Primeira: %s",
			len(incoerentes), leiturasFeitas, incoerentes[0])
	}
	a.qhTitularNoBanco(t, r.ID, "depois da corrida")
}

// O `meta` de `GET /guests` é o que a tela usa para desenhar os botões ("a lista
// já tem o número de hóspedes", "aumentar para N"). Com a gestão subindo e
// baixando o número enquanto a recepção acrescenta e retira, CADA leitura tem de
// ser coerente: a lista nunca nomeia mais gente do que o `meta.guests_count` da
// mesma resposta diz que a reserva acomoda.
func TestHospedesQACorridaEntreLerAListaEMudarONumero(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	titular := f.qhPessoa(t, casa.Prop, "Titular")
	x := f.qhPessoa(t, casa.Prop, "Segundo")
	d := casa.Hoje.AddDays(985)
	r := a.qhReservar(t, u.Token, p.ID, titular.ID, d, d.AddDays(2), 1)
	caminho := "/reservations/" + r.ID.String()

	const ciclos, leituras = 25, 150
	var (
		espera      sync.WaitGroup
		mu          sync.Mutex
		incoerentes []string
		falhas      []string
	)
	largada := make(chan struct{})
	espera.Add(2)
	go func() {
		defer espera.Done()
		<-largada
		passos := []struct {
			metodo, caminho string
			corpo           any
		}{
			{http.MethodPatch, caminho, map[string]any{"guests_count": 2}},
			{http.MethodPost, qhCaminho(r.ID), map[string]any{"contact_id": x.ID}},
			{http.MethodDelete, qhCaminho(r.ID) + "/" + x.ID.String(), nil},
			{http.MethodPatch, caminho, map[string]any{"guests_count": 1}},
		}
		for i := 0; i < ciclos; i++ {
			for _, ps := range passos {
				resp, err := a.qhChamarSeguro(ps.metodo, ps.caminho, u.Token, "", ps.corpo)
				if err != nil || resp.Status >= 300 {
					mu.Lock()
					falhas = append(falhas, ps.metodo+" "+qhDesfecho(resp, err))
					mu.Unlock()
				}
			}
		}
	}()
	go func() {
		defer espera.Done()
		<-largada
		for i := 0; i < leituras; i++ {
			resp, err := a.qhChamarSeguro(http.MethodGet, qhCaminho(r.ID), u.Token, "", nil)
			if err != nil || resp.Status != http.StatusOK {
				mu.Lock()
				falhas = append(falhas, "GET "+qhDesfecho(resp, err))
				mu.Unlock()
				continue
			}
			var l qhLista
			if err := json.Unmarshal(resp.Corpo, &l); err != nil {
				continue
			}
			if len(l.Data) > l.Meta.GuestsCount {
				mu.Lock()
				incoerentes = append(incoerentes, fmt.Sprintf("%d nomes com meta.guests_count %d", len(l.Data), l.Meta.GuestsCount))
				mu.Unlock()
			}
		}
	}()
	close(largada)
	espera.Wait()

	if len(falhas) > 0 {
		t.Errorf("respostas fora do esperado durante a corrida: %v", falhas)
	}
	if len(incoerentes) > 0 {
		t.Errorf("%d de %d leituras de GET /guests saíram com mais nomes do que o meta da mesma resposta diz caber: %v",
			len(incoerentes), leituras, incoerentes[0])
	}
	vendidos := a.qagContar(t, `SELECT guests_count FROM reservations WHERE id = $1`, r.ID)
	if n := a.qhNomesNoBanco(t, r.ID); n > vendidos {
		t.Fatalf("no banco: %d nomes para %d vendido(s)", n, vendidos)
	}
}
