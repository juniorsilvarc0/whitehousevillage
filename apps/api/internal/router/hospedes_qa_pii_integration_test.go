//go:build integration

package router

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ─────────────────────────── Uma linha por pessoa exibida ───────────────────

// Em linguagem de negócio: a recepção abre a lista de quem vai dormir na casa e
// vê o documento, o telefone e o e-mail de cada um — e a casa precisa saber
// responder "quem viu o meu número?" por pessoa. A regra do contrato, medida
// pela contagem no banco:
//
//   - `GET /guests` grava UMA linha `rooming_list` por hóspede exibido, com o
//     ator e o `contact_id` de cada um;
//   - o `/full` grava o mesmo;
//   - o `POST` grava UMA — a pessoa acrescentada, que volta cheia na resposta;
//   - o `DELETE` não grava nada (204 sem corpo).
//
// E a medida do topo do contrato, repetida: nenhum `doc_number` completo
// aparece numa resposta sem uma linha nova em `pii_access_log`.
func TestHospedesQACadaLeituraGravaUmaLinhaPorHospedeExibido(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	titular := f.qhPessoa(t, casa.Prop, "Tadeu")
	ana := f.qhPessoa(t, casa.Prop, "Ana")
	bia := f.qhPessoa(t, casa.Prop, "Bia")
	caio := f.qhPessoa(t, casa.Prop, "Caio")
	todos := map[uuid.UUID]qhPessoa{titular.ID: titular, ana.ID: ana, bia.ID: bia, caio.ID: caio}
	d := casa.Hoje.AddDays(500)
	r := a.qhReservar(t, u.Token, p.ID, titular.ID, d, d.AddDays(3), 4)

	// A criação não exibe ninguém — a resposta é a `Reserva`, sem a lista.
	if n := a.qhLeiturasDoAtor(t, u.ID); n != 0 {
		t.Fatalf("POST /reservations gravou %d leitura(s) de rooming_list", n)
	}

	// POST: 201, a pessoa cheia, UMA linha — dela.
	for _, quem := range []qhPessoa{ana, bia} {
		antes := a.qhLeiturasPorContato(t, u.ID)
		resp := a.qhAcrescentar(t, u.Token, r.ID, quem.ID)
		h := qbDado[qhHospede](t, resp, http.StatusCreated, "POST /guests")
		if h.ContactID != quem.ID || h.Titular || h.Documento == nil || *h.Documento != quem.Documento ||
			h.Telefone == nil || *h.Telefone != quem.Telefone || h.Email == nil || *h.Email != quem.Email ||
			h.DocTipo == nil || *h.DocTipo != "cpf" || h.Nome != quem.Nome {
			t.Fatalf("o POST não devolveu a pessoa cheia, como acompanhante: %+v", h)
		}
		depois := a.qhLeiturasPorContato(t, u.ID)
		if depois[quem.ID] != antes[quem.ID]+1 || qhSoma(depois)-qhSoma(antes) != 1 {
			t.Fatalf("o POST de %s gravou %d linha(s) (%d dela); o contrato diz UMA, da pessoa acrescentada",
				quem.Nome, qhSoma(depois)-qhSoma(antes), depois[quem.ID]-antes[quem.ID])
		}
	}

	// GET /guests e /full: uma linha por hóspede listado, cada um uma vez.
	for _, leitura := range []string{"GET /guests", "GET /full"} {
		antes := a.qhLeiturasPorContato(t, u.ID)
		var exibidos []qhHospede
		if leitura == "GET /guests" {
			exibidos = a.qhLer(t, u.Token, r.ID).Data
		} else {
			c, _ := a.qhCompleta(t, u.Token, r.ID)
			exibidos = c.Hospedes
		}
		if len(exibidos) != 3 {
			t.Fatalf("%s exibiu %d hóspedes, esperado 3", leitura, len(exibidos))
		}
		depois := a.qhLeiturasPorContato(t, u.ID)
		for _, h := range exibidos {
			p := todos[h.ContactID]
			if h.Documento == nil || *h.Documento != p.Documento || h.Telefone == nil || *h.Telefone != p.Telefone {
				t.Errorf("%s: %s não saiu cheio: %+v", leitura, p.Nome, h)
			}
			if depois[h.ContactID]-antes[h.ContactID] != 1 {
				t.Errorf("%s: %s exibido e %d linha(s) gravada(s) para ele; o contrato diz uma",
					leitura, p.Nome, depois[h.ContactID]-antes[h.ContactID])
			}
		}
		if n := qhSoma(depois) - qhSoma(antes); n != 3 {
			t.Errorf("%s com 3 hóspedes gravou %d linha(s)", leitura, n)
		}
		if depois[caio.ID] != antes[caio.ID] {
			t.Errorf("%s gravou leitura de quem não está na lista", leitura)
		}
	}

	// DELETE: 204, sem corpo e sem linha.
	antes := a.qhLeiturasPorContato(t, u.ID)
	resp := a.qhRetirar(t, u.Token, r.ID, bia.ID)
	exigirStatusQB(t, resp, http.StatusNoContent, "DELETE /guests/{contactId}")
	if len(resp.Corpo) != 0 {
		t.Errorf("o 204 trouxe corpo: %s", resp.Corpo)
	}
	if n := qhSoma(a.qhLeiturasPorContato(t, u.ID)) - qhSoma(antes); n != 0 {
		t.Errorf("o DELETE gravou %d linha(s) de PII", n)
	}

	// Todas as linhas desta bateria têm o motivo do vocabulário fechado.
	if n := a.qagContar(t, `
		SELECT count(*) FROM pii_access_log WHERE actor_id = $1 AND reason IS DISTINCT FROM 'rooming_list'`, u.ID); n != 0 {
		t.Errorf("%d linha(s) gravadas pelo ator com motivo diferente de rooming_list", n)
	}
}

func qhSoma(m map[uuid.UUID]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// ─────────────────────────── A trilha não guarda a pessoa ───────────────────

// O `audit_log` registra QUE a lista mudou e quem mexeu — por `contact_id`,
// nunca por nome, documento, telefone ou e-mail; e a linha do tempo da reserva
// ganha `guest_added`/`guest_removed` com `{contact_id}` e nada mais. Anonimizar
// a ficha depois não alcançaria uma cópia do nome deixada na trilha.
//
// O cenário passa pelas três escritas que tocam a lista (acrescentar, retirar,
// trocar o titular) e depois varre TUDO o que ficou gravado sobre a reserva.
func TestHospedesQATrilhaELinhaDoTempoGuardamSoOContactID(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	titular := f.qhPessoa(t, casa.Prop, "Titular")
	ana := f.qhPessoa(t, casa.Prop, "Ana")
	beto := f.qhPessoa(t, casa.Prop, "Beto")
	novo := f.qhPessoa(t, casa.Prop, "Novo Titular")
	d := casa.Hoje.AddDays(510)
	r := a.qhReservar(t, u.Token, p.ID, titular.ID, d, d.AddDays(2), 4)

	a.qhAcrescentarOK(t, u.Token, r.ID, ana.ID)
	a.qhAcrescentarOK(t, u.Token, r.ID, beto.ID)
	exigirStatusQB(t, a.qhRetirar(t, u.Token, r.ID, beto.ID), http.StatusNoContent, "DELETE")
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/reservations/"+r.ID.String(), u.Token,
		map[string]any{"contact_id": novo.ID}), http.StatusOK, "PATCH contact_id")
	// Uma recusa também passa pela trilha? Não pode — e a mensagem dela não
	// carrega a pessoa.
	recusa := a.qhAcrescentar(t, u.Token, r.ID, ana.ID)
	qbErro(t, recusa, http.StatusConflict, "CODE_IN_USE", "pessoa repetida")

	proibidos := func(ps ...qhPessoa) []string {
		var out []string
		for _, p := range ps {
			out = append(out, p.Nome, p.Documento, p.Telefone, p.Email, p.Cidade, p.Anotacao, qhNascimento)
		}
		return out
	}(titular, ana, beto, novo)
	for _, s := range proibidos {
		if strings.Contains(string(recusa.Corpo), s) {
			t.Errorf("a mensagem de erro carrega dado pessoal (%q): %s", s, recusa.Corpo)
		}
	}

	// audit_log da lista: só {contact_id, is_lead_guest}.
	linhas, err := a.pool.Query(a.ctx, `
		SELECT action, coalesce(before::text, ''), coalesce(after::text, ''), actor_id
		  FROM audit_log WHERE entity = 'reservation_guests' AND entity_id = $1 ORDER BY at, action`, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	acoes := map[string]int{}
	for linhas.Next() {
		var (
			acao, antes, depois string
			ator                *uuid.UUID
		)
		if err := linhas.Scan(&acao, &antes, &depois, &ator); err != nil {
			t.Fatal(err)
		}
		acoes[acao]++
		if ator == nil || *ator != u.ID {
			t.Errorf("%s sem o ator: %v", acao, ator)
		}
		// A forma do contrato: `criado` com `after` = {contact_id,
		// is_lead_guest} e sem `before`; `excluido` com `before` = {contact_id,
		// is_lead_guest} e sem `after`.
		cheio, vazio := depois, antes
		if acao == "reservation_guests.excluido" {
			cheio, vazio = antes, depois
		}
		if vazio != "" && vazio != "null" {
			t.Errorf("audit_log %s com o lado que devia ser vazio preenchido: %q", acao, vazio)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(cheio), &m); err != nil {
			t.Fatalf("audit_log %s: %v — %q", acao, err, cheio)
		}
		if len(m) != 2 || m["is_lead_guest"] != false || m["contact_id"] == nil {
			t.Errorf("audit_log %s = %s; o contrato diz {contact_id, is_lead_guest: false} e nada mais", acao, cheio)
		}
		for _, s := range proibidos {
			if strings.Contains(antes+depois, s) {
				t.Errorf("audit_log %s carrega dado pessoal (%q)", acao, s)
			}
		}
	}
	linhas.Close()
	if acoes["reservation_guests.criado"] != 2 || acoes["reservation_guests.excluido"] != 1 {
		t.Errorf("audit_log da lista = %v; esperado 2 criado (Ana, Beto) e 1 excluido (Beto)", acoes)
	}

	// A troca de titular fica no audit_log DA RESERVA, por contact_id.
	var trocaAntes, trocaDepois string
	if err := a.pool.QueryRow(a.ctx, `
		SELECT before::text, after::text FROM audit_log
		 WHERE entity = 'reservations' AND entity_id = $1 AND action = 'reservations.alterado'
		   AND before->>'contact_id' = $2 AND after->>'contact_id' = $3`,
		r.ID, titular.ID.String(), novo.ID.String()).Scan(&trocaAntes, &trocaDepois); err != nil {
		t.Errorf("a troca de titular não está no audit_log da reserva com contact_id antes e depois: %v", err)
	}
	for _, s := range proibidos {
		if strings.Contains(trocaAntes+trocaDepois, s) {
			t.Errorf("o audit_log da troca de titular carrega dado pessoal (%q)", s)
		}
	}

	// A linha do tempo: guest_added ×2 e guest_removed ×1, com {contact_id} e
	// nada mais; a troca não gera os dois. E NENHUM evento da reserva carrega
	// a pessoa.
	tipos := map[string]int{}
	ev, err := a.pool.Query(a.ctx, `SELECT type, coalesce(payload::text, '') FROM reservation_events WHERE reservation_id = $1`, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	for ev.Next() {
		var tipo, payload string
		if err := ev.Scan(&tipo, &payload); err != nil {
			t.Fatal(err)
		}
		tipos[tipo]++
		for _, s := range proibidos {
			if strings.Contains(payload, s) {
				t.Errorf("evento %s carrega dado pessoal (%q): %s", tipo, s, payload)
			}
		}
		if tipo != "guest_added" && tipo != "guest_removed" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(payload), &m); err != nil {
			t.Fatalf("%s: %v", tipo, err)
		}
		chaves := make([]string, 0, len(m))
		for k := range m {
			chaves = append(chaves, k)
		}
		sort.Strings(chaves)
		if strings.Join(chaves, ",") != "contact_id" {
			t.Errorf("%s com payload além de {contact_id}: %s", tipo, payload)
		}
	}
	ev.Close()
	if tipos["guest_added"] != 2 || tipos["guest_removed"] != 1 {
		t.Errorf("linha do tempo da lista = added %d, removed %d; esperado 2 e 1 (a troca de titular não gera os dois)",
			tipos["guest_added"], tipos["guest_removed"])
	}

	// O /full mostra a mesma linha do tempo, sem a pessoa.
	_, bruto := a.qhCompleta(t, u.Token, r.ID)
	var corpo struct {
		Data struct {
			Timeline json.RawMessage `json:"timeline"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bruto.Corpo, &corpo); err != nil {
		t.Fatal(err)
	}
	for _, s := range proibidos {
		if strings.Contains(string(corpo.Data.Timeline), s) {
			t.Errorf("a timeline do /full carrega dado pessoal (%q)", s)
		}
	}
}

// ─────────────────────────── O mínimo necessário ────────────────────────────

// O hóspede da lista é "quem é a pessoa e como alcançá-la": `contact_id`,
// `name`, `doc_type`, `doc_number`, `phone_e164`, `email`, `is_lead_guest` —
// os sete do schema, todos presentes. Nascimento, cidade e anotação ficam na
// ficha, que registra a própria leitura; nenhuma das três rotas os devolve,
// nem com outro nome.
func TestHospedesQAHospedeTemOsSeteCamposEMaisNada(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	titular := f.qhPessoa(t, casa.Prop, "Titular")
	ana := f.qhPessoa(t, casa.Prop, "Ana")
	d := casa.Hoje.AddDays(520)
	r := a.qhReservar(t, u.Token, p.ID, titular.ID, d, d.AddDays(2), 3)

	sete := []string{"contact_id", "doc_number", "doc_type", "email", "is_lead_guest", "name", "phone_e164"}
	conferir := func(contexto string, itens []map[string]any, corpo []byte) {
		t.Helper()
		if len(itens) == 0 {
			t.Fatalf("%s: nenhum hóspede para conferir", contexto)
		}
		for _, h := range itens {
			chaves := make([]string, 0, len(h))
			for k := range h {
				chaves = append(chaves, k)
			}
			sort.Strings(chaves)
			if strings.Join(chaves, ",") != strings.Join(sete, ",") {
				t.Errorf("%s: o hóspede traz [%s]; o schema HospedeDaReserva é [%s]", contexto, strings.Join(chaves, ","), strings.Join(sete, ","))
			}
		}
		// As CHAVES já foram conferidas item a item acima (a `Reserva` do /full
		// tem `notes` próprio, legítimo); aqui, os VALORES da ficha, em
		// qualquer lugar da resposta.
		for _, s := range []string{qhNascimento, titular.Cidade, titular.Anotacao, ana.Cidade, ana.Anotacao} {
			if strings.Contains(string(corpo), s) {
				t.Errorf("%s: a resposta traz %q — nascimento, cidade e anotação ficam na ficha", contexto, s)
			}
		}
	}

	post := a.qhAcrescentar(t, u.Token, r.ID, ana.ID)
	exigirStatusQB(t, post, http.StatusCreated, "POST /guests")
	var umItem struct {
		Data map[string]any `json:"data"`
	}
	post.decodificar(t, &umItem)
	conferir("POST /guests", []map[string]any{umItem.Data}, post.Corpo)
	if fora := qbChavesForaDoContrato(t, "HospedeDaReserva", post.Corpo); len(fora) > 0 {
		t.Errorf("POST /guests: chaves fora do schema HospedeDaReserva: %v", fora)
	}

	get := a.chamar(t, http.MethodGet, qhCaminho(r.ID), u.Token, nil)
	exigirStatusQB(t, get, http.StatusOK, "GET /guests")
	var lista struct {
		Data []map[string]any `json:"data"`
		Meta map[string]any   `json:"meta"`
	}
	get.decodificar(t, &lista)
	conferir("GET /guests", lista.Data, get.Corpo)
	if fora := qbChavesForaDoContrato(t, "HospedeDaReserva", get.Corpo); len(fora) > 0 {
		t.Errorf("GET /guests: chaves fora do schema HospedeDaReserva: %v", fora)
	}
	metaChaves := make([]string, 0, len(lista.Meta))
	for k := range lista.Meta {
		metaChaves = append(metaChaves, k)
	}
	sort.Strings(metaChaves)
	if strings.Join(metaChaves, ",") != "capacity,editable,guests_count" {
		t.Errorf("GET /guests: meta = [%s]; o schema MetaDaListaDeHospedes é [capacity, editable, guests_count]", strings.Join(metaChaves, ","))
	}

	full := a.chamar(t, http.MethodGet, "/reservations/"+r.ID.String()+"/full", u.Token, nil)
	exigirStatusQB(t, full, http.StatusOK, "GET /full")
	var completa struct {
		Data struct {
			Guests []map[string]any `json:"guests"`
		} `json:"data"`
	}
	full.decodificar(t, &completa)
	conferir("GET /full (guests)", completa.Data.Guests, full.Corpo)
}

// Uma ficha anonimizada continua na lista de uma estadia que já terminou (é o
// registro de quem dormiu na casa), mas aparece como "Contato anonimizado #…",
// sem documento nem canal — a lista lê a ficha a cada leitura, nunca uma cópia.
func TestHospedesQAFichaAnonimizadaApareceSemDocumentoNemCanal(t *testing.T) {
	a := subirAPI(t)
	admin := a.criarUsuario(t, "hq-admin", a.qbPerfilDoSeed(t, "admin"))
	f := a.qagFaxina(t, admin)
	casa := a.qagCasa(t, a.qbPropriedadePadrao(t))
	p := f.produto(t, casa.Prop, "one_member", 1)
	titular := f.qhPessoa(t, casa.Prop, "Titular")
	ana := f.qhPessoa(t, casa.Prop, "Ana")

	// A estadia começa HOJE, para o check-in e o check-out valerem.
	r := a.qhReservar(t, admin.Token, p.ID, titular.ID, casa.Hoje, casa.Hoje.AddDays(2), 2)
	a.qhAcrescentarOK(t, admin.Token, r.ID, ana.ID)
	a.qhConfirmar(t, admin.Token, r)
	a.qhAcao(t, admin.Token, r.ID, "check-in", nil)
	a.qhAcao(t, admin.Token, r.ID, "check-out", nil)

	// Pela API: a estadia terminou, então a anonimização é aceita.
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/contacts/"+ana.ID.String()+"/anonymize", admin.Token,
		map[string]any{"reason": "pedido da titular dos dados"}), http.StatusOK, "POST /contacts/{id}/anonymize")

	lida := a.qhLer(t, admin.Token, r.ID)
	qhExigirLista(t, "a lista de quem dormiu na casa", lida.ids(), titular.ID, ana.ID)
	h := lida.Data[1]
	if !strings.HasPrefix(h.Nome, "Contato anonimizado #") || h.Documento != nil || h.DocTipo != nil || h.Telefone != nil || h.Email != nil {
		t.Errorf("a ficha anonimizada saiu com dado: %+v", h)
	}
	for _, s := range []string{ana.Nome, ana.Documento, ana.Telefone, ana.Email} {
		if strings.Contains(string(a.chamar(t, http.MethodGet, qhCaminho(r.ID), admin.Token, nil).Corpo), s) {
			t.Errorf("a lista ainda mostra %q depois da anonimização", s)
		}
	}
}

// ─────────────────────────── Tempo real ─────────────────────────────────────

// "Nenhum evento de tempo real fala da lista nesta fase; se um dia falar,
// carrega `reservation_id` e nada mais." Escutando os canais do tempo real
// enquanto a lista é lida, acrescida, retirada e o titular trocado, nenhuma
// notificação carrega o `contact_id`, o nome, o documento ou o telefone de
// ninguém. A espera termina numa SENTINELA publicada depois das escritas — as
// notificações saem na ordem dos commits, então tudo o que as escritas
// publicaram chega antes dela.
func TestHospedesQANenhumEventoDeTempoRealFalaDaPessoa(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	c := a.qhMontarCenario(t, f, u.Token, 950)

	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	conn, err := a.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	for _, canal := range []string{"whv_calendar", "whv_crm"} {
		if _, err := conn.Exec(ctx, "LISTEN "+canal); err != nil {
			t.Fatalf("LISTEN %s: %v", canal, err)
		}
	}
	defer func() { _, _ = conn.Exec(context.Background(), "UNLISTEN *") }()

	a.qhLer(t, u.Token, c.Reserva.ID)
	a.qhAcrescentarOK(t, u.Token, c.Reserva.ID, c.Fora.ID)
	exigirStatusQB(t, a.qhRetirar(t, u.Token, c.Reserva.ID, c.Acompanhante.ID), http.StatusNoContent, "DELETE")
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/reservations/"+c.Reserva.ID.String(), u.Token,
		map[string]any{"contact_id": c.Fora.ID}), http.StatusOK, "PATCH contact_id")
	a.qhCompleta(t, u.Token, c.Reserva.ID)

	sentinela := "sentinela-rooming-" + qbSufixo()
	if _, err := a.pool.Exec(ctx, `SELECT pg_notify('whv_calendar', $1)`, sentinela); err != nil {
		t.Fatal(err)
	}
	var recebidas []string
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			t.Fatalf("esperando a sentinela: %v (recebidas: %v)", err, recebidas)
		}
		if n.Payload == sentinela {
			break
		}
		recebidas = append(recebidas, n.Channel+": "+n.Payload)
	}
	for _, msg := range recebidas {
		for _, p := range []qhPessoa{c.Titular, c.Acompanhante, c.Fora} {
			for _, s := range []string{p.ID.String(), p.Nome, p.Documento, p.Telefone, p.Email} {
				if strings.Contains(msg, s) {
					t.Errorf("evento de tempo real carrega dado da pessoa (%q): %s", s, msg)
				}
			}
		}
	}
	t.Logf("%d notificação(ões) durante as operações da lista", len(recebidas))
}

// ─────────────────────────── Falha fechada ──────────────────────────────────

// "Falha fechada: não conseguir gravar a linha aborta a leitura com erro, nunca
// serve sem rastro." Com o registro de leitura de UMA pessoa da lista
// indisponível (um gatilho de teste recusa o INSERT em `pii_access_log` só para
// ela), a lista não sai — nem pelo GET /guests, nem pelo /full — e nenhum dado
// de ninguém vaza no corpo do erro; o acréscimo cuja resposta não pode ser
// registrada não acontece (sem linha na lista, sem trilha, sem evento). Tirado o
// gatilho, tudo volta.
func TestHospedesQASemRastroALeituraNaoSai(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	c := a.qhMontarCenario(t, f, u.Token, 960)

	nome := "qa_pii_falha_" + qbSufixo()
	a.executarQA(t, `CREATE FUNCTION `+nome+`() RETURNS trigger LANGUAGE plpgsql AS $f$
		BEGIN RAISE EXCEPTION 'registro de leitura indisponível (QA)'; END $f$`)
	a.executarQA(t, `CREATE TRIGGER `+nome+` BEFORE INSERT ON pii_access_log FOR EACH ROW
		WHEN (NEW.contact_id IN ('`+c.Acompanhante.ID.String()+`'::uuid, '`+c.Fora.ID.String()+`'::uuid))
		EXECUTE FUNCTION `+nome+`()`)
	desligar := func() {
		a.executarQA(t, `DROP TRIGGER IF EXISTS `+nome+` ON pii_access_log`)
		a.executarQA(t, `DROP FUNCTION IF EXISTS `+nome+`()`)
	}
	t.Cleanup(desligar)

	antes := a.qhLerEstado(t, c)
	vazou := func(contexto string, r resposta) {
		t.Helper()
		for _, p := range []qhPessoa{c.Titular, c.Acompanhante, c.Fora} {
			for _, s := range []string{p.Documento, p.Telefone, p.Email} {
				if strings.Contains(string(r.Corpo), s) {
					t.Errorf("%s: a resposta sem rastro carrega %q", contexto, s)
				}
			}
		}
	}

	for _, leitura := range []string{qhCaminho(c.Reserva.ID), "/reservations/" + c.Reserva.ID.String() + "/full"} {
		r := a.chamar(t, http.MethodGet, leitura, u.Token, nil)
		if r.Status < 500 {
			t.Errorf("GET %s com o registro indisponível: status %d — a lista saiu sem rastro: %s", leitura, r.Status, r.Corpo)
		}
		vazou("GET "+leitura, r)
	}
	post := a.qhAcrescentar(t, u.Token, c.Reserva.ID, c.Fora.ID)
	if post.Status < 500 {
		t.Errorf("POST /guests com o registro indisponível: status %d — %s", post.Status, post.Corpo)
	}
	vazou("POST /guests", post)
	if depois := a.qhLerEstado(t, c); depois != antes {
		t.Errorf("sem rastro, algo mudou (o acréscimo, a trilha ou uma leitura parcial):\n antes  %+v\n depois %+v", antes, depois)
	}

	desligar()
	lida := a.qhLer(t, u.Token, c.Reserva.ID)
	if len(lida.Data) != 2 {
		t.Fatalf("com o registro de volta, a lista tem %d nomes", len(lida.Data))
	}
	a.qhAcrescentarOK(t, u.Token, c.Reserva.ID, c.Fora.ID)
}

// ─────────────────────────── O log da aplicação ─────────────────────────────

// "A resposta não entra em log de aplicação." Capturando o log da aplicação
// (o `slog` padrão, que o registrador de requisições e o tradutor de erros
// usam) enquanto a lista é lida, acrescida, recusada, retirada e o titular
// trocado: nenhuma linha carrega nome, documento, telefone ou e-mail.
func TestHospedesQAOLogDaAplicacaoNaoGuardaAPessoa(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "hq-usuario", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qagFaxina(t, u)
	c := a.qhMontarCenario(t, f, u.Token, 970)
	anonima := f.qhPessoa(t, c.Casa.Prop, "Anonimizada")
	a.qhAnonimizarDireto(t, anonima.ID)

	var (
		mu  sync.Mutex
		buf bytes.Buffer
	)
	anterior := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&qhEscritorTravado{mu: &mu, w: &buf}, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(anterior) })

	a.qhLer(t, u.Token, c.Reserva.ID)
	a.qhCompleta(t, u.Token, c.Reserva.ID)
	a.qhAcrescentarOK(t, u.Token, c.Reserva.ID, c.Fora.ID)
	qbErro(t, a.qhAcrescentar(t, u.Token, c.Reserva.ID, c.Fora.ID), http.StatusConflict, "CODE_IN_USE", "repetida")
	qbErro(t, a.qhAcrescentar(t, u.Token, c.Reserva.ID, anonima.ID), http.StatusConflict, "CONTACT_ANONYMIZED", "anonimizada")
	exigirStatusQB(t, a.qhRetirar(t, u.Token, c.Reserva.ID, c.Acompanhante.ID), http.StatusNoContent, "DELETE")
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/reservations/"+c.Reserva.ID.String(), u.Token,
		map[string]any{"contact_id": c.Fora.ID}), http.StatusOK, "PATCH contact_id")

	slog.SetDefault(anterior)
	mu.Lock()
	log := buf.String()
	mu.Unlock()
	if !strings.Contains(log, "/guests") {
		t.Fatalf("o log capturado não tem as requisições da lista — a captura não pegou o registrador: %q", log)
	}
	for _, p := range []qhPessoa{c.Titular, c.Acompanhante, c.Fora, anonima} {
		for _, s := range []string{p.Nome, p.Documento, p.Telefone, p.Email} {
			if s != "" && strings.Contains(log, s) {
				t.Errorf("o log da aplicação carrega dado pessoal (%q)", s)
			}
		}
	}
}

type qhEscritorTravado struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (e *qhEscritorTravado) Write(p []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.w.Write(p)
}
