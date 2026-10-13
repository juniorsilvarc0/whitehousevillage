//go:build integration

package reservas_test

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/reservas"
)

// A rooming list (F2-22): `GET|POST /reservations/{id}/guests` e
// `DELETE /reservations/{id}/guests/{contactId}`, a troca de titular pelo
// PATCH da reserva, o titular anonimizado na criação e a lista que acompanha a
// remarcação. Contrato na OpenAPI (tag Reservas), regras em
// internal/domain/guests, critérios de aceite em docs/spec.md §6.
//
// Tudo por HTTP, atravessando o autenticador e o RBAC de verdade, com a
// migration 20261013180000 aplicada: a FK adiada do titular confere no COMMIT
// cada troca que estes testes fazem.

// ─────────────────────────── Projeções e atalhos ────────────────────

type hospedeVisto struct {
	ContactID uuid.UUID `json:"contact_id"`
	Nome      string    `json:"name"`
	DocTipo   *string   `json:"doc_type"`
	Documento *string   `json:"doc_number"`
	Telefone  *string   `json:"phone_e164"`
	Email     *string   `json:"email"`
	Titular   bool      `json:"is_lead_guest"`
}

type listaDeHospedesVista struct {
	Data []hospedeVisto `json:"data"`
	Meta struct {
		GuestsCount int  `json:"guests_count"`
		Capacity    int  `json:"capacity"`
		Editable    bool `json:"editable"`
	} `json:"meta"`
}

func (l listaDeHospedesVista) ids() []uuid.UUID {
	out := make([]uuid.UUID, len(l.Data))
	for i, h := range l.Data {
		out[i] = h.ContactID
	}
	return out
}

func caminhoDaLista(reserva uuid.UUID) string {
	return "/reservations/" + reserva.String() + "/guests"
}

func (a *ambiente) lerHospedes(t *testing.T, token string, reserva uuid.UUID) listaDeHospedesVista {
	t.Helper()
	r := a.chamar(t, http.MethodGet, caminhoDaLista(reserva), token, nil)
	if r.Status != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", caminhoDaLista(reserva), r.Status, r.Corpo)
	}
	var v listaDeHospedesVista
	decodificar(t, r, &v)
	if v.Data == nil {
		t.Fatalf("data veio nulo, o contrato promete array: %s", r.Corpo)
	}
	return v
}

func (a *ambiente) acrescentarHospede(t *testing.T, token string, reserva, contato uuid.UUID) resposta {
	t.Helper()
	return a.chamar(t, http.MethodPost, caminhoDaLista(reserva), token, map[string]any{"contact_id": contato})
}

func (a *ambiente) retirarHospede(t *testing.T, token string, reserva, contato uuid.UUID) resposta {
	t.Helper()
	return a.chamar(t, http.MethodDelete, caminhoDaLista(reserva)+"/"+contato.String(), token, nil)
}

func exigirRecusa(t *testing.T, r resposta, status int, code, contexto string) map[string]any {
	t.Helper()
	if r.Status != status || r.codigoDoErro() != code {
		t.Fatalf("%s = %d/%s, esperado %d %s: %s", contexto, r.Status, r.codigoDoErro(), status, code, r.Corpo)
	}
	return r.detalhes(t)
}

func exigirMesmaLista(t *testing.T, contexto string, got []uuid.UUID, quer ...uuid.UUID) {
	t.Helper()
	if len(got) != len(quer) {
		t.Fatalf("%s: lista = %v, esperado %v", contexto, got, quer)
	}
	for i := range quer {
		if got[i] != quer[i] {
			t.Fatalf("%s: lista = %v, esperado %v (na ordem)", contexto, got, quer)
		}
	}
}

// pessoa cria uma ficha com nome escolhido e documento — o que a lista exibe
// cheio. O documento é sorteado: o índice único de documento por casa não pode
// colidir com o que uma execução interrompida deixou para trás.
func (a *ambiente) pessoa(t *testing.T, nome string) (uuid.UUID, string) {
	t.Helper()
	id := a.contato(t)
	doc := "QA" + strings.ToUpper(sufixo()+sufixo())
	if _, err := a.pool.Exec(a.ctx,
		`UPDATE contacts SET name = $2, doc_type = 'passaporte', doc_number = $3 WHERE id = $1`,
		id, nome+" "+sufixo(), doc); err != nil {
		t.Fatalf("completando a ficha: %v", err)
	}
	return id, doc
}

func (a *ambiente) nomeDaFicha(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var nome string
	if err := a.pool.QueryRow(a.ctx, `SELECT name FROM contacts WHERE id = $1`, id).Scan(&nome); err != nil {
		t.Fatalf("lendo o nome: %v", err)
	}
	return nome
}

func telefoneSorteado(t *testing.T) string {
	t.Helper()
	n, err := rand.Int(rand.Reader, big.NewInt(90000000))
	if err != nil {
		t.Fatalf("sorteando telefone: %v", err)
	}
	return fmt.Sprintf("+55869%08d", n.Int64()+10000000)
}

func (a *ambiente) anonimizarDireto(t *testing.T, id uuid.UUID) {
	t.Helper()
	// Direto no banco: pela API, quem está numa lista viva não se anonimiza —
	// e é justamente o dado anterior a essa regra que o teste precisa.
	if _, err := a.pool.Exec(a.ctx, `UPDATE contacts SET anonymized_at = now() WHERE id = $1`, id); err != nil {
		t.Fatalf("anonimizando: %v", err)
	}
}

// linhasDeRooming conta as leituras de lista registradas pelo ator.
func (a *ambiente) linhasDeRooming(t *testing.T, ator uuid.UUID) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*) FROM pii_access_log WHERE actor_id = $1 AND reason = 'rooming_list'`, ator).Scan(&n); err != nil {
		t.Fatalf("contando pii_access_log: %v", err)
	}
	return n
}

func (a *ambiente) contar(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(a.ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("contando (%s): %v", sql, err)
	}
	return n
}

// fichaDeOutraCasa cria uma segunda propriedade com uma ficha. A remoção é
// registrada antes de qualquer reserva do teste, então roda por último.
func (a *ambiente) fichaDeOutraCasa(t *testing.T) uuid.UUID {
	t.Helper()
	var casa, contato uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO properties (name, slug, timezone) VALUES ('Casa B da rooming list', $1, 'America/Fortaleza') RETURNING id`,
		"qa-rooming-b-"+sufixo()).Scan(&casa); err != nil {
		t.Fatalf("criando a segunda propriedade: %v", err)
	}
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO contacts (property_id, name) VALUES ($1, 'Hóspede da outra casa') RETURNING id`, casa).Scan(&contato); err != nil {
		t.Fatalf("criando a ficha da outra casa: %v", err)
	}
	t.Cleanup(func() {
		a.executar(t, `DELETE FROM reservation_guests WHERE contact_id = $1`, contato)
		a.executar(t, `DELETE FROM contacts WHERE id = $1`, contato)
		a.executar(t, `DELETE FROM properties WHERE id = $1`, casa)
	})
	return contato
}

// ─────────────────────────── Ler, acrescentar, retirar ──────────────

// O ciclo da recepção: lê a lista, acrescenta, lê de novo, retira. A lista sai
// cheia (documento e telefone), o titular primeiro e os acompanhantes por nome;
// cada leitura grava uma linha de `pii_access_log` por pessoa exibida, o POST
// grava uma (a pessoa acrescentada) e o DELETE nenhuma. A trilha e a linha do
// tempo falam da lista por `contact_id`, nunca por nome ou documento.
func TestListaDeHospedesLeAcrescentaRetiraERegistraCadaLeitura(t *testing.T) {
	a := subir(t)
	ator, token := a.usuario(t, a.perfil(t, "res_lista",
		"reservations:ver", "reservations:criar", "reservations:editar"))
	apto := a.produto(t, "apto-2s")

	titular, docDoTitular := a.pessoa(t, "Titular")
	beto, docDoBeto := a.pessoa(t, "B Acompanhante")
	ana, _ := a.pessoa(t, "A Acompanhante")
	telefone := telefoneSorteado(t)
	a.executar(t, `UPDATE contacts SET phone_e164 = $2 WHERE id = $1`, beto, telefone)

	reserva := a.criarReserva(t, token, pedido(apto, titular, a.dia(1200), a.dia(1204), 3))

	// 1. A reserva nasce com o titular na lista, e a leitura grava UMA linha.
	lida := a.lerHospedes(t, token, reserva.ID)
	exigirMesmaLista(t, "a reserva nasce com o titular", lida.ids(), titular)
	if h := lida.Data[0]; !h.Titular || h.Documento == nil || *h.Documento != docDoTitular || h.DocTipo == nil || *h.DocTipo != "passaporte" {
		t.Fatalf("o titular não saiu cheio e marcado: %+v", h)
	}
	if lida.Meta.GuestsCount != 3 || lida.Meta.Capacity != 6 || !lida.Meta.Editable {
		t.Fatalf("meta = %+v, esperado {3, 6, true}", lida.Meta)
	}
	if n := a.linhasDeRooming(t, ator); n != 1 {
		t.Fatalf("GET com 1 hóspede gravou %d linha(s) de rooming_list", n)
	}

	// 2. POST: 201, Location do vínculo, a pessoa cheia, UMA linha de PII.
	r := a.acrescentarHospede(t, token, reserva.ID, beto)
	if r.Status != http.StatusCreated {
		t.Fatalf("POST guests = %d: %s", r.Status, r.Corpo)
	}
	if quer := "/api/v1/reservations/" + reserva.ID.String() + "/guests/" + beto.String(); r.Location != quer {
		t.Errorf("Location = %q, esperado %q", r.Location, quer)
	}
	criado := dado[hospedeVisto](t, r)
	if criado.ContactID != beto || criado.Titular || criado.Documento == nil || *criado.Documento != docDoBeto ||
		criado.Telefone == nil || *criado.Telefone != telefone {
		t.Fatalf("o acompanhante não voltou cheio e como acompanhante: %+v", criado)
	}
	if n := a.linhasDeRooming(t, ator); n != 2 {
		t.Fatalf("o POST gravou %d linha(s) de rooming_list, esperado 1 (total 2)", n-1)
	}
	if n := a.contar(t, `SELECT count(*) FROM pii_access_log WHERE actor_id = $1 AND contact_id = $2`, ator, beto); n != 1 {
		t.Fatalf("a linha do POST não é a da pessoa acrescentada: %d", n)
	}

	// 3. Ordem: titular, depois os acompanhantes por nome — "A…" antes de "B…".
	if r = a.acrescentarHospede(t, token, reserva.ID, ana); r.Status != http.StatusCreated {
		t.Fatalf("POST da segunda acompanhante = %d: %s", r.Status, r.Corpo)
	}
	antes := a.linhasDeRooming(t, ator)
	lida = a.lerHospedes(t, token, reserva.ID)
	exigirMesmaLista(t, "titular e acompanhantes por nome", lida.ids(), titular, ana, beto)
	if n := a.linhasDeRooming(t, ator) - antes; n != 3 {
		t.Fatalf("GET com 3 hóspedes gravou %d linha(s), esperado 3", n)
	}

	// 4. DELETE: 204 e nenhuma linha de PII.
	antes = a.linhasDeRooming(t, ator)
	if r = a.retirarHospede(t, token, reserva.ID, beto); r.Status != http.StatusNoContent {
		t.Fatalf("DELETE = %d: %s", r.Status, r.Corpo)
	}
	if n := a.linhasDeRooming(t, ator); n != antes {
		t.Fatalf("o DELETE gravou %d linha(s) de PII", n-antes)
	}
	exigirMesmaLista(t, "depois de retirar", a.lerHospedes(t, token, reserva.ID).ids(), titular, ana)
	if n := a.contar(t, `SELECT count(*) FROM contacts WHERE id = $1`, beto); n != 1 {
		t.Fatal("retirar da lista apagou a ficha")
	}

	// 5. A linha do tempo: guest_added duas vezes, guest_removed uma, com
	// `{contact_id}` e nada mais.
	linhas, err := a.pool.Query(a.ctx, `
		SELECT type, payload::text FROM reservation_events
		 WHERE reservation_id = $1 AND type IN ($2, $3) ORDER BY at, id`,
		reserva.ID, reservas.EventoHospedeAcrescentado, reservas.EventoHospedeRetirado)
	if err != nil {
		t.Fatalf("lendo a timeline: %v", err)
	}
	var eventos []string
	for linhas.Next() {
		var tipo, payload string
		if err := linhas.Scan(&tipo, &payload); err != nil {
			t.Fatalf("lendo a timeline: %v", err)
		}
		eventos = append(eventos, tipo)
		if strings.Count(payload, ":") != 1 || !strings.Contains(payload, `"contact_id"`) {
			t.Errorf("%s com payload além de {contact_id}: %s", tipo, payload)
		}
	}
	linhas.Close()
	if strings.Join(eventos, ",") != "guest_added,guest_added,guest_removed" {
		t.Errorf("timeline da lista = %v", eventos)
	}

	// 6. audit_log: por contact_id, NUNCA nome ou documento.
	linhas, err = a.pool.Query(a.ctx, `
		SELECT action, actor_id, COALESCE(before::text, ''), COALESCE(after::text, '')
		  FROM audit_log WHERE entity = 'reservation_guests' AND entity_id = $1 ORDER BY at, action`, reserva.ID)
	if err != nil {
		t.Fatalf("lendo audit_log: %v", err)
	}
	acoes := map[string]int{}
	for linhas.Next() {
		var (
			acao          string
			autor         *uuid.UUID
			antes, depois string
		)
		if err := linhas.Scan(&acao, &autor, &antes, &depois); err != nil {
			t.Fatalf("lendo audit_log: %v", err)
		}
		acoes[acao]++
		if autor == nil || *autor != ator {
			t.Errorf("%s sem o ator: %v", acao, autor)
		}
		texto := antes + depois
		for _, proibido := range []string{a.nomeDaFicha(t, beto), a.nomeDaFicha(t, ana), docDoBeto, telefone, "Acompanhante"} {
			if strings.Contains(texto, proibido) {
				t.Errorf("audit_log %s carrega dado pessoal (%q): %s", acao, proibido, texto)
			}
		}
		if !strings.Contains(texto, `"contact_id"`) || !strings.Contains(texto, `"is_lead_guest": false`) {
			t.Errorf("audit_log %s sem {contact_id, is_lead_guest}: %s", acao, texto)
		}
	}
	linhas.Close()
	if acoes["reservation_guests.criado"] != 2 || acoes["reservation_guests.excluido"] != 1 {
		t.Errorf("audit_log da lista = %v, esperado 2 criado e 1 excluido", acoes)
	}
}

// ─────────────────────────── Trocar o titular ───────────────────────

// A troca de titular é o PATCH da reserva, e a lista vem junto na mesma
// transação: o novo aparece marcado, o anterior SAI (não vira acompanhante), e
// o /full mostra o titular certo. Sem a sincronização, a FK adiada do F2-22
// derrubaria o COMMIT.
func TestTrocarOTitularPeloPatchLevaAListaJunto(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	apto := a.produto(t, "apto-2s")

	antigo, _ := a.pessoa(t, "Titular Antigo")
	novo, _ := a.pessoa(t, "Titular Novo")
	acompanhante, _ := a.pessoa(t, "Acompanhante Promovido")
	reserva := a.criarReserva(t, token, pedido(apto, antigo, a.dia(1210), a.dia(1214), 3))
	caminho := "/reservations/" + reserva.ID.String()

	// Por quem não estava na lista: sai um, entra um.
	if r := a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"contact_id": novo}); r.Status != http.StatusOK {
		t.Fatalf("PATCH contact_id = %d: %s", r.Status, r.Corpo)
	}
	lida := a.lerHospedes(t, token, reserva.ID)
	exigirMesmaLista(t, "depois da troca", lida.ids(), novo)
	if !lida.Data[0].Titular {
		t.Fatalf("o titular novo não está marcado: %+v", lida.Data[0])
	}

	completa := dado[struct {
		Reserva struct {
			ContactID uuid.UUID `json:"contact_id"`
			Nome      string    `json:"contact_name"`
		} `json:"reservation"`
		Hospedes []hospedeVisto `json:"guests"`
		Editavel *bool          `json:"guests_editable"`
	}](t, a.chamar(t, http.MethodGet, caminho+"/full", token, nil))
	if completa.Reserva.ContactID != novo || completa.Reserva.Nome != a.nomeDaFicha(t, novo) {
		t.Errorf("/full: reservation = %+v, esperado o titular novo", completa.Reserva)
	}
	if len(completa.Hospedes) != 1 || completa.Hospedes[0].ContactID != novo || !completa.Hospedes[0].Titular {
		t.Errorf("/full: guests = %+v, esperado só o titular novo, marcado", completa.Hospedes)
	}
	if completa.Editavel == nil || !*completa.Editavel {
		t.Errorf("/full: guests_editable = %v na reserva de pé", completa.Editavel)
	}

	// Pelo acompanhante: ele é promovido, o anterior sai, a lista diminui.
	if r := a.acrescentarHospede(t, token, reserva.ID, acompanhante); r.Status != http.StatusCreated {
		t.Fatalf("POST guests = %d: %s", r.Status, r.Corpo)
	}
	if r := a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"contact_id": acompanhante}); r.Status != http.StatusOK {
		t.Fatalf("PATCH promovendo o acompanhante = %d: %s", r.Status, r.Corpo)
	}
	lida = a.lerHospedes(t, token, reserva.ID)
	exigirMesmaLista(t, "depois da promoção", lida.ids(), acompanhante)
	if !lida.Data[0].Titular {
		t.Fatalf("o acompanhante promovido não está marcado: %+v", lida.Data[0])
	}

	// Reenviar o titular de sempre não é troca; o PUT inteiro também passa.
	if r := a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"contact_id": acompanhante, "notes": "chega à noite"}); r.Status != http.StatusOK {
		t.Fatalf("PATCH reenviando o titular = %d: %s", r.Status, r.Corpo)
	}
	if r := a.chamar(t, http.MethodPut, caminho, token, map[string]any{"contact_id": acompanhante, "guests_count": 3}); r.Status != http.StatusOK {
		t.Fatalf("PUT com o titular de sempre = %d: %s", r.Status, r.Corpo)
	}
	exigirMesmaLista(t, "reenviar não mexe", a.lerHospedes(t, token, reserva.ID).ids(), acompanhante)

	// A troca não gera guest_added/guest_removed: fica no `updated` e no
	// audit_log da reserva, com contact_id antes e depois.
	if n := a.contar(t, `SELECT count(*) FROM reservation_events WHERE reservation_id = $1 AND type = $2`,
		reserva.ID, reservas.EventoHospedeRetirado); n != 0 {
		t.Errorf("a troca gerou %d guest_removed", n)
	}
	if n := a.contar(t, `
		SELECT count(*) FROM audit_log
		 WHERE entity = 'reservations' AND entity_id = $1 AND action = 'reservations.alterado'
		   AND before->>'contact_id' = $2 AND after->>'contact_id' = $3`,
		reserva.ID, antigo.String(), novo.String()); n != 1 {
		t.Errorf("audit_log da troca antigo → novo: %d linha(s), esperado 1", n)
	}

	// Recusas da troca: anonimizado é 409 e nada muda; inexistente segue 404.
	anonimo, _ := a.pessoa(t, "Anonimizado")
	a.anonimizarDireto(t, anonimo)
	exigirRecusa(t, a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"contact_id": anonimo}),
		http.StatusConflict, "CONTACT_ANONYMIZED", "troca por ficha anonimizada")
	exigirRecusa(t, a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"contact_id": uuid.New()}),
		http.StatusNotFound, "NOT_FOUND", "troca por ficha inexistente")
	exigirMesmaLista(t, "as recusas não mexem", a.lerHospedes(t, token, reserva.ID).ids(), acompanhante)
}

// ─────────────────────────── O teto da lista ────────────────────────

// O teto é o número VENDIDO, não a capacidade: com dois vendidos e dois nomes,
// o terceiro é 422 GUEST_LIST_FULL com os três números da tela. Baixar
// `guests_count` abaixo dos nomes é 422 em `details.guests_count`; subir abre o
// lugar.
func TestTetoDaListaEhONumeroVendido(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	apto := a.produto(t, "apto-2s")

	titular, _ := a.pessoa(t, "Titular do Teto")
	primeiro, _ := a.pessoa(t, "Primeiro")
	segundo, _ := a.pessoa(t, "Segundo")
	reserva := a.criarReserva(t, token, pedido(apto, titular, a.dia(1220), a.dia(1224), 2))
	caminho := "/reservations/" + reserva.ID.String()

	if r := a.acrescentarHospede(t, token, reserva.ID, primeiro); r.Status != http.StatusCreated {
		t.Fatalf("o último lugar = %d: %s", r.Status, r.Corpo)
	}
	d := exigirRecusa(t, a.acrescentarHospede(t, token, reserva.ID, segundo),
		http.StatusUnprocessableEntity, "GUEST_LIST_FULL", "lista cheia")
	if d["guests_count"] != float64(2) || d["listed"] != float64(2) || d["capacity"] != float64(6) {
		t.Errorf("details = %v, esperado guests_count 2, listed 2, capacity 6", d)
	}

	d = exigirRecusa(t, a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"guests_count": 1}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "guests_count abaixo dos nomes")
	if _, ok := d["guests_count"]; !ok {
		t.Errorf("a recusa não aponta guests_count: %v", d)
	}
	if n := a.contar(t, `SELECT guests_count FROM reservations WHERE id = $1`, reserva.ID); n != 2 {
		t.Fatalf("a recusa gravou guests_count = %d", n)
	}

	if r := a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"guests_count": 3}); r.Status != http.StatusOK {
		t.Fatalf("subir o número = %d: %s", r.Status, r.Corpo)
	}
	if r := a.acrescentarHospede(t, token, reserva.ID, segundo); r.Status != http.StatusCreated {
		t.Fatalf("depois de subir o número, o acréscimo = %d: %s", r.Status, r.Corpo)
	}
	if lida := a.lerHospedes(t, token, reserva.ID); len(lida.Data) != 3 || lida.Meta.GuestsCount != 3 {
		t.Fatalf("lista = %d nomes, meta %+v", len(lida.Data), lida.Meta)
	}
	exigirRecusa(t, a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"guests_count": 7}),
		http.StatusUnprocessableEntity, "CAPACITY_EXCEEDED", "guests_count acima da capacidade")
}

// ─────────────────────────── Recusas ────────────────────────────────

// Cada recusa do contrato, com a lista intacta depois de todas.
func TestRecusasDaListaDeHospedes(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	apto := a.produto(t, "apto-2s")

	titular, _ := a.pessoa(t, "Titular das Recusas")
	acompanhante, _ := a.pessoa(t, "Acompanhante das Recusas")
	anonimo, _ := a.pessoa(t, "Ficha Anonimizada")
	a.anonimizarDireto(t, anonimo)
	deFora := a.fichaDeOutraCasa(t)

	reserva := a.criarReserva(t, token, pedido(apto, titular, a.dia(1230), a.dia(1234), 4))

	exigirRecusa(t, a.acrescentarHospede(t, token, reserva.ID, anonimo),
		http.StatusConflict, "CONTACT_ANONYMIZED", "ficha anonimizada")

	// Outra casa e inexistente: a MESMA resposta, no campo contact_id.
	for nome, id := range map[string]uuid.UUID{"de outra propriedade": deFora, "inexistente": uuid.New()} {
		d := exigirRecusa(t, a.acrescentarHospede(t, token, reserva.ID, id),
			http.StatusUnprocessableEntity, "VALIDATION_ERROR", "ficha "+nome)
		if _, ok := d["contact_id"]; !ok {
			t.Errorf("ficha %s: a recusa não aponta contact_id: %v", nome, d)
		}
	}

	if r := a.acrescentarHospede(t, token, reserva.ID, acompanhante); r.Status != http.StatusCreated {
		t.Fatalf("POST = %d: %s", r.Status, r.Corpo)
	}
	d := exigirRecusa(t, a.acrescentarHospede(t, token, reserva.ID, acompanhante),
		http.StatusConflict, "CODE_IN_USE", "pessoa repetida")
	if d["contact_id"] != acompanhante.String() || d["is_lead_guest"] != false {
		t.Errorf("details da pessoa repetida = %v", d)
	}
	d = exigirRecusa(t, a.acrescentarHospede(t, token, reserva.ID, titular),
		http.StatusConflict, "CODE_IN_USE", "o titular como acompanhante")
	if d["is_lead_guest"] != true {
		t.Errorf("details do titular repetido = %v", d)
	}

	d = exigirRecusa(t, a.retirarHospede(t, token, reserva.ID, titular),
		http.StatusConflict, "RESOURCE_IN_USE", "retirar o titular")
	if d["is_lead_guest"] != true {
		t.Errorf("details da retirada do titular = %v", d)
	}
	exigirRecusa(t, a.retirarHospede(t, token, reserva.ID, anonimo),
		http.StatusNotFound, "NOT_FOUND", "retirar quem não está na lista")
	exigirRecusa(t, a.chamar(t, http.MethodDelete, caminhoDaLista(reserva.ID)+"/nao-e-uuid", token, nil),
		http.StatusNotFound, "NOT_FOUND", "retirar com id malformado")

	d = exigirRecusa(t, a.chamar(t, http.MethodPost, caminhoDaLista(reserva.ID), token,
		map[string]any{"contact_id": uuid.New(), "is_lead_guest": true}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "corpo com is_lead_guest")
	if _, ok := d["is_lead_guest"]; !ok {
		t.Errorf("a recusa não aponta is_lead_guest: %v", d)
	}
	exigirRecusa(t, a.acrescentarHospede(t, token, uuid.New(), acompanhante),
		http.StatusNotFound, "NOT_FOUND", "reserva inexistente")

	exigirMesmaLista(t, "depois das recusas", a.lerHospedes(t, token, reserva.ID).ids(), titular, acompanhante)
}

// Depois do check-out a lista é o registro de quem dormiu na casa: não
// acrescenta, não retira e não troca o titular — 409, com a lista intacta.
func TestListaNaoMudaDepoisDoCheckOut(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	suite := a.produto(t, "suite-piscina")

	titular, _ := a.pessoa(t, "Titular que Saiu")
	acompanhante, _ := a.pessoa(t, "Acompanhante que Saiu")
	outro, _ := a.pessoa(t, "Quem Chegou Tarde")

	// A estadia começa HOJE: check-in e check-out conferem a data contra o
	// período vendido.
	reserva := a.criarReserva(t, token, pedido(suite, titular, a.dia(0), a.dia(2), 2))
	caminho := "/reservations/" + reserva.ID.String()
	if r := a.acrescentarHospede(t, token, reserva.ID, acompanhante); r.Status != http.StatusCreated {
		t.Fatalf("POST em hold = %d: %s", r.Status, r.Corpo)
	}
	a.confirmar(t, token, reserva.ID, 1000)
	for _, acao := range []string{"/check-in", "/check-out"} {
		if r := a.chamar(t, http.MethodPost, caminho+acao, token, nil); r.Status != http.StatusOK {
			t.Fatalf("%s = %d: %s", acao, r.Status, r.Corpo)
		}
	}

	d := exigirRecusa(t, a.acrescentarHospede(t, token, reserva.ID, outro),
		http.StatusConflict, "INVALID_STATE_TRANSITION", "acrescentar depois do check-out")
	if d["status"] != reservas.EstadoCheckOut {
		t.Errorf("details.status = %v", d["status"])
	}
	exigirRecusa(t, a.retirarHospede(t, token, reserva.ID, acompanhante),
		http.StatusConflict, "INVALID_STATE_TRANSITION", "retirar depois do check-out")
	d = exigirRecusa(t, a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"contact_id": outro}),
		http.StatusConflict, "INVALID_STATE_TRANSITION", "trocar o titular depois do check-out")
	if d["field"] != "contact_id" {
		t.Errorf("a recusa da troca não aponta o campo: %v", d)
	}
	// Reenviar o titular de sempre não é troca e passa em qualquer estado.
	if r := a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"contact_id": titular, "notes": "saiu bem"}); r.Status != http.StatusOK {
		t.Fatalf("PATCH com o titular de sempre depois do check-out = %d: %s", r.Status, r.Corpo)
	}

	lida := a.lerHospedes(t, token, reserva.ID)
	exigirMesmaLista(t, "a lista de quem dormiu na casa", lida.ids(), titular, acompanhante)
	if lida.Meta.Editable {
		t.Error("meta.editable = true depois do check-out")
	}
}

// ─────────────────────────── Permissão ──────────────────────────────

// O corretor semeado (`reservations` em ver e criar, `own`) lê a lista das
// reservas dele, recebe 404 na de outro — sem gravar leitura nenhuma — e 403
// ao tentar acrescentar ou retirar: quem completa a lista é a recepção.
func TestCorretorLeAListaDeleENaoEditaNenhuma(t *testing.T) {
	a := subir(t)
	gestor := a.gestor(t)
	conta, _, corretor := a.corretorVinculado(t, a.perfilDoSeed(t, "corretor"))
	apto := a.produto(t, "apto-2s")

	clienteDele, _ := a.pessoa(t, "Cliente do Corretor")
	clienteAlheio, _ := a.pessoa(t, "Cliente de Outro")
	acompanhante, _ := a.pessoa(t, "Acompanhante Qualquer")

	dele := a.criarReserva(t, corretor, pedido(apto, clienteDele, a.dia(1240), a.dia(1244), 2))
	alheia := a.criarReserva(t, gestor, pedido(apto, clienteAlheio, a.dia(1250), a.dia(1254), 2))

	lida := a.lerHospedes(t, corretor, dele.ID)
	exigirMesmaLista(t, "o corretor lê a lista dele", lida.ids(), clienteDele)
	if n := a.linhasDeRooming(t, conta); n != 1 {
		t.Fatalf("a leitura do corretor gravou %d linha(s)", n)
	}

	exigirRecusa(t, a.chamar(t, http.MethodGet, caminhoDaLista(alheia.ID), corretor, nil),
		http.StatusNotFound, "NOT_FOUND", "o corretor lendo a lista de outro")
	if n := a.linhasDeRooming(t, conta); n != 1 {
		t.Fatalf("a leitura recusada gravou %d linha(s) de PII", n-1)
	}

	exigirRecusa(t, a.acrescentarHospede(t, corretor, dele.ID, acompanhante),
		http.StatusForbidden, "FORBIDDEN", "o corretor acrescentando")
	exigirRecusa(t, a.retirarHospede(t, corretor, dele.ID, clienteDele),
		http.StatusForbidden, "FORBIDDEN", "o corretor retirando")
	exigirMesmaLista(t, "nada mudou", a.lerHospedes(t, gestor, dele.ID).ids(), clienteDele)
}

// ─────────────────────────── A reserva que nasce ────────────────────

// O titular é o primeiro hóspede da lista, e por isso a regra de entrada vale
// na criação: ficha anonimizada não vira titular (409, e nenhuma reserva
// nasce). Até o F2-22 a criação respondia 201. Inexistente continua 404.
func TestCriarReservaComTitularAnonimizadoEh409(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	apto := a.produto(t, "apto-2s")

	anonimo, _ := a.pessoa(t, "Titular Anonimizado")
	a.anonimizarDireto(t, anonimo)

	exigirRecusa(t, a.postarVenda(t, token, pedido(apto, anonimo, a.dia(1290), a.dia(1294), 2)),
		http.StatusConflict, "CONTACT_ANONYMIZED", "POST /reservations com titular anonimizado")
	if n := a.contar(t, `SELECT count(*) FROM reservations WHERE contact_id = $1`, anonimo); n != 0 {
		t.Fatalf("a recusa deixou %d reserva(s) para trás", n)
	}
	exigirRecusa(t, a.postarVenda(t, token, pedido(apto, uuid.New(), a.dia(1290), a.dia(1294), 2)),
		http.StatusNotFound, "NOT_FOUND", "POST /reservations com titular inexistente")
}

// ─────────────────────────── Remarcação ─────────────────────────────

// A reserva nova leva o titular e os acompanhantes; a original fica com a lista
// dela, congelada. Ficha anonimizada não é carregada. Se não cabem no número da
// nova, 422 em guests_count ANTES de qualquer escrita — e nada muda.
func TestRemarcacaoLevaOsAcompanhantes(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	apto := a.produto(t, "apto-2s")

	titular, _ := a.pessoa(t, "Titular da Remarcação")
	ana, _ := a.pessoa(t, "Ana da Remarcação")
	beto, _ := a.pessoa(t, "Beto da Remarcação")
	carla, _ := a.pessoa(t, "Carla Anonimizada")

	original := a.criarReserva(t, token, pedido(apto, titular, a.dia(1260), a.dia(1264), 4))
	for _, p := range []uuid.UUID{ana, beto, carla} {
		if r := a.acrescentarHospede(t, token, original.ID, p); r.Status != http.StatusCreated {
			t.Fatalf("POST guests = %d: %s", r.Status, r.Corpo)
		}
	}
	a.anonimizarDireto(t, carla)
	caminho := "/reservations/" + original.ID.String() + "/reschedule"

	// Titular + Ana + Beto não cabem em 2 (Carla, anonimizada, nem conta).
	chave := "it-remarca-lista-" + sufixo()
	a.limparChaves(t, chave)
	d := exigirRecusa(t, a.chamarIdem(t, http.MethodPost, caminho, token,
		map[string]any{"check_in": a.dia(1270), "check_out": a.dia(1274), "guests_count": 2}, chave),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "remarcar para menos gente que a lista")
	if _, ok := d["guests_count"]; !ok {
		t.Errorf("a recusa não aponta guests_count: %v", d)
	}
	if s := a.statusDaReserva(t, original.ID); s != reservas.EstadoHold {
		t.Fatalf("a recusa mexeu na original: %s", s)
	}
	if n := a.contar(t, `SELECT count(*) FROM reservations WHERE rebooked_from_id = $1`, original.ID); n != 0 {
		t.Fatalf("a recusa deixou %d reserva nova", n)
	}
	if b := a.statusDosBlocos(t, original.ID); b[reservas.BlocoHold] != 1 {
		t.Fatalf("a recusa soltou o calendário da original: %v", b)
	}

	// Com o número da original (4), cabem.
	chave = "it-remarca-lista-" + sufixo()
	a.limparChaves(t, chave)
	r := a.chamarIdem(t, http.MethodPost, caminho, token,
		map[string]any{"check_in": a.dia(1270), "check_out": a.dia(1274)}, chave)
	if r.Status != http.StatusCreated {
		t.Fatalf("remarcar = %d: %s", r.Status, r.Corpo)
	}
	nova := dado[reservaVista](t, r)

	lida := a.lerHospedes(t, token, nova.ID)
	ids := lida.ids()
	if len(ids) != 3 || ids[0] != titular || !lida.Data[0].Titular {
		t.Fatalf("a nova nasceu com %v, esperado o titular e os dois acompanhantes", ids)
	}
	levados := map[uuid.UUID]bool{}
	for _, h := range lida.Data[1:] {
		levados[h.ContactID] = true
		if h.Titular {
			t.Errorf("acompanhante carregado como titular: %+v", h)
		}
	}
	if !levados[ana] || !levados[beto] || levados[carla] {
		t.Errorf("carregados = %v; esperado Ana e Beto, sem a anonimizada", levados)
	}

	// A original fica com a lista dela, como estava.
	if n := a.contar(t, `SELECT count(*) FROM reservation_guests WHERE reservation_id = $1`, original.ID); n != 4 {
		t.Errorf("a lista da original mudou: %d nomes, esperado 4", n)
	}
	// Um audit_log por pessoa carregada; nenhum guest_added na nova.
	if n := a.contar(t, `
		SELECT count(*) FROM audit_log
		 WHERE entity = 'reservation_guests' AND entity_id = $1 AND action = 'reservation_guests.criado'`, nova.ID); n != 2 {
		t.Errorf("audit_log dos carregados = %d, esperado 2", n)
	}
	if n := a.contar(t, `SELECT count(*) FROM reservation_events WHERE reservation_id = $1 AND type = $2`,
		nova.ID, reservas.EventoHospedeAcrescentado); n != 0 {
		t.Errorf("a remarcação gravou %d guest_added na nova", n)
	}
}

// ─────────────────────────── Concorrência ───────────────────────────

// Duas pessoas pelo último lugar, ao mesmo tempo: exatamente uma entra (201) e
// a outra recebe 422 GUEST_LIST_FULL — nunca 500, nunca as duas. Toda escrita
// na lista trava a linha da reserva, e a segunda conta a lista com a primeira
// dentro. Repete em rodadas, liberando o lugar a cada uma.
func TestDisputaPeloUltimoLugarDaListaDeHospedes(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	apto := a.produto(t, "apto-2s")

	titular, _ := a.pessoa(t, "Titular da Disputa")
	reserva := a.criarReserva(t, token, pedido(apto, titular, a.dia(1280), a.dia(1284), 2))

	const rodadas = 5
	for rodada := 1; rodada <= rodadas; rodada++ {
		candidatos := [2]uuid.UUID{a.contato(t), a.contato(t)}
		var (
			espera    sync.WaitGroup
			respostas [2]resposta
		)
		largada := make(chan struct{})
		for i := range candidatos {
			espera.Add(1)
			go func(i int) {
				defer espera.Done()
				<-largada
				respostas[i] = a.acrescentarHospede(t, token, reserva.ID, candidatos[i])
			}(i)
		}
		close(largada)
		espera.Wait()

		vencedor := -1
		for i, r := range respostas {
			switch {
			case r.Status == http.StatusCreated:
				if vencedor >= 0 {
					t.Fatalf("rodada %d: as duas entraram no último lugar", rodada)
				}
				vencedor = i
			case r.Status == http.StatusUnprocessableEntity && r.codigoDoErro() == "GUEST_LIST_FULL":
			default:
				t.Fatalf("rodada %d: resposta fora de {201, 422 GUEST_LIST_FULL}: %d %s", rodada, r.Status, r.Corpo)
			}
		}
		if vencedor < 0 {
			t.Fatalf("rodada %d: ninguém entrou no lugar livre", rodada)
		}
		if n := a.contar(t, `SELECT count(*) FROM reservation_guests WHERE reservation_id = $1`, reserva.ID); n != 2 {
			t.Fatalf("rodada %d: a lista tem %d nomes para 2 vendidos", rodada, n)
		}
		if r := a.retirarHospede(t, token, reserva.ID, candidatos[vencedor]); r.Status != http.StatusNoContent {
			t.Fatalf("rodada %d: liberando o lugar = %d: %s", rodada, r.Status, r.Corpo)
		}
	}
}
