//go:build integration

// Apoio da bateria de QA da rooming list (F2-22, spec §6 "A rooming list"):
// `GET|POST /reservations/{id}/guests`, `DELETE /reservations/{id}/guests/{contactId}`
// e o que mudou em `/full`, `PUT`/`PATCH /reservations/{id}`, `POST /reservations`
// e `/reschedule` — pela PORTA DA FRENTE: o router completo, sessões abertas por
// `/auth/login` com os perfis REAIS do seed (`admin`, `usuario`, `corretor`,
// `vitrine`) e o contrato lido de `openapi/openapi.yaml`.
//
// Por que aqui e não em `internal/modules/reservas`: os nove testes de
// `hospedes_integration_test.go` montam um chi só com as rotas do módulo e
// perfis feitos à mão. Isso prova o módulo; não prova a matriz que o seed
// entrega, nem a fronteira com contatos (a anonimização) e com a remarcação
// pela mesma porta que o painel atravessa.
//
// O esperado sai do CONTRATO, não do código: o x-rbac de cada operação vem do
// openapi.yaml, a forma de cada hóspede vem do schema `HospedeDaReserva`, e o
// vocabulário de erro do enum fechado. Reaproveita o apoio da bateria da agenda
// (`qagFaxina`, `qagCasa`, `qagReserva`).
package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
)

// ─────────────────────────── O contrato ─────────────────────────────────────

// qhOperacoesDaLista lê do contrato as operações da rooming list — os paths
// `/reservations/{id}/guests` e `/reservations/{id}/guests/{contactId}` — e o
// x-rbac de cada uma. Mesmo scanner de indentação de `qbOperacoesDeBens`.
func qhOperacoesDaLista(t *testing.T) []qbOperacao {
	t.Helper()

	bruto, err := os.ReadFile(caminhoDaOpenAPI)
	if err != nil {
		t.Fatalf("lendo o contrato: %v", err)
	}
	type acumulada struct {
		recurso, acao   string
		metodo, caminho string
	}
	var (
		ordem []*acumulada
		atual *acumulada
		path  string
	)
	for _, linha := range strings.Split(string(bruto), "\n") {
		if linha != "" && linha[0] != ' ' && linha[0] != '#' {
			path, atual = "", nil
			continue
		}
		if m := linhaDePath.FindStringSubmatch(linha); m != nil {
			path, atual = m[1], nil
			continue
		}
		if !strings.HasPrefix(path, "/reservations/{id}/guests") {
			continue
		}
		if m := linhaDeVerbo.FindStringSubmatch(linha); m != nil {
			atual = &acumulada{metodo: strings.ToUpper(m[1]), caminho: path}
			ordem = append(ordem, atual)
			continue
		}
		if atual == nil {
			continue
		}
		if m := qbLinhaDeRBAC.FindStringSubmatch(linha); m != nil {
			atual.recurso, atual.acao = m[1], m[2]
		}
	}
	var out []qbOperacao
	for _, a := range ordem {
		if a.recurso == "" || a.acao == "" {
			t.Fatalf("%s %s sem x-rbac legível no contrato", a.metodo, a.caminho)
		}
		out = append(out, qbOperacao{Metodo: a.metodo, Path: a.caminho, Recurso: a.recurso, Acao: a.acao})
	}
	if len(out) != 3 {
		t.Fatalf("o contrato tem %d operações em /reservations/{id}/guests…; o F2-22 declara 3 (GET, POST, DELETE) — o scanner ficou defasado ou o contrato mudou: %+v", len(out), out)
	}
	return out
}

// ─────────────────────────── As formas da resposta ──────────────────────────

// qhHospede é o schema `HospedeDaReserva`.
type qhHospede struct {
	ContactID uuid.UUID `json:"contact_id"`
	Nome      string    `json:"name"`
	DocTipo   *string   `json:"doc_type"`
	Documento *string   `json:"doc_number"`
	Telefone  *string   `json:"phone_e164"`
	Email     *string   `json:"email"`
	Titular   bool      `json:"is_lead_guest"`
}

// qhMeta é o schema `MetaDaListaDeHospedes`.
type qhMeta struct {
	GuestsCount int  `json:"guests_count"`
	Capacity    int  `json:"capacity"`
	Editable    bool `json:"editable"`
}

type qhLista struct {
	Data []qhHospede `json:"data"`
	Meta qhMeta      `json:"meta"`
}

func (l qhLista) ids() []uuid.UUID {
	out := make([]uuid.UUID, len(l.Data))
	for i, h := range l.Data {
		out[i] = h.ContactID
	}
	return out
}

// qhCompleta é o recorte de `ReservaCompleta` que a rooming list toca.
type qhCompleta struct {
	Reserva struct {
		ID           uuid.UUID `json:"id"`
		Status       string    `json:"status"`
		ContactID    uuid.UUID `json:"contact_id"`
		ContactName  string    `json:"contact_name"`
		ContactPhone *string   `json:"contact_phone_e164"`
		GuestsCount  int       `json:"guests_count"`
	} `json:"reservation"`
	Hospedes []qhHospede `json:"guests"`
	Editavel *bool       `json:"guests_editable"`
	Timeline []struct {
		Tipo    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	} `json:"timeline"`
}

// ─────────────────────────── Fixtures ───────────────────────────────────────

// qhPessoa é uma ficha com TODO dado pessoal preenchido — inclusive o que a
// lista NÃO pode devolver (nascimento, cidade, anotação) e o que a trilha
// nunca pode guardar (nome, documento, telefone, e-mail).
type qhPessoa struct {
	ID        uuid.UUID
	Nome      string
	Email     string
	Telefone  string
	Documento string
	Cidade    string
	Anotacao  string
}

// qhNascimento é a data de nascimento de toda ficha da bateria. Fixa, e
// procurada em toda resposta: o contrato diz que a lista não a traz.
const qhNascimento = "1987-03-14"

// qhPessoa cria a ficha pelo banco. Documento e telefone saem de
// `uuid.New().ID()` (32 bits) — um byte do UUID dava 256 valores e já colidiu
// no CI em `contacts_doc_unico_idx`.
func (f *qagFaxina) qhPessoa(t *testing.T, prop uuid.UUID, nome string) qhPessoa {
	t.Helper()
	s := qbSufixo()
	p := qhPessoa{
		Nome:      nome + " " + s,
		Email:     "qa-rooming-" + s + "@exemplo.invalid",
		Telefone:  fmt.Sprintf("+55879%08d", uuid.New().ID()%100000000),
		Documento: fmt.Sprintf("8%010d", uuid.New().ID()),
		Cidade:    "Cidade Sigilosa " + s,
		Anotacao:  "Anotação sigilosa " + s,
	}
	if err := f.a.pool.QueryRow(f.a.ctx, `
		INSERT INTO contacts (property_id, name, email, phone_e164, doc_type, doc_number, birth_date, city, notes)
		VALUES ($1, $2, $3, $4, 'cpf', $5, DATE '`+qhNascimento+`', $6, $7) RETURNING id`,
		prop, p.Nome, p.Email, p.Telefone, p.Documento, p.Cidade, p.Anotacao).Scan(&p.ID); err != nil {
		t.Fatalf("criando a ficha %q: %v", nome, err)
	}
	f.mu.Lock()
	f.contatos = append(f.contatos, p.ID)
	f.mu.Unlock()
	return p
}

// qhAnonimizarDireto anonimiza a ficha PELO BANCO, como a anonimização faria,
// sem passar pela recusa de "reserva viva" — é o dado anterior a essa regra
// (ou anterior ao F2-22) que alguns cenários precisam.
func (a *ambiente) qhAnonimizarDireto(t *testing.T, contato uuid.UUID) {
	t.Helper()
	if _, err := a.pool.Exec(a.ctx, `
		UPDATE contacts SET name = 'Contato anonimizado #' || left(replace(id::text, '-', ''), 8),
		       email = NULL, phone_e164 = NULL, doc_type = NULL, doc_number = NULL,
		       birth_date = NULL, city = NULL, notes = NULL, anonymized_at = now()
		 WHERE id = $1`, contato); err != nil {
		t.Fatalf("anonimizando pelo banco: %v", err)
	}
}

// qhReservar segura a data pela API, com o número de hóspedes pedido.
func (a *ambiente) qhReservar(t *testing.T, token string, produto, titular uuid.UUID, de, ate calendar.Date, hospedes int) qagReserva {
	t.Helper()
	r := a.chamarComChave(t, http.MethodPost, "/reservations", token, map[string]any{
		"unit_type_id": produto, "contact_id": titular,
		"check_in": de.String(), "check_out": ate.String(), "guests_count": hospedes,
	}, a.chaveNova(t, "rooming-res"))
	return qbDado[qagReserva](t, r, http.StatusCreated, "POST /reservations")
}

func (a *ambiente) qhConfirmar(t *testing.T, token string, r qagReserva) {
	t.Helper()
	exigirStatusQB(t, a.qagConfirmar(t, token, r, a.chaveNova(t, "rooming-conf")), http.StatusOK, "POST /confirm")
}

// qhAcao roda uma ação do ciclo sem Idempotency-Key (check-in, check-out,
// cancel) e exige 200.
func (a *ambiente) qhAcao(t *testing.T, token string, reserva uuid.UUID, acao string, corpo any) {
	t.Helper()
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/reservations/"+reserva.String()+"/"+acao, token, corpo),
		http.StatusOK, "POST /"+acao)
}

// qhReservaPorSQL cria a reserva direto no banco, com a linha do titular, num
// estado que NÃO segura data (o gatilho adiado da composição só confere os
// estados vivos). Serve aos estados que nenhuma rota produz de forma barata:
// `quote`, `closed`, `expired`.
func (a *ambiente) qhReservaPorSQL(t *testing.T, prop, produto, titular uuid.UUID, status string, de, ate calendar.Date, hospedes int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO reservations (property_id, unit_type_id, contact_id, status, check_in, check_out, guests_count)
		VALUES ($1, $2, $3, $4, $5::date, $6::date, $7) RETURNING id`,
		prop, produto, titular, status, de.String(), ate.String(), hospedes).Scan(&id); err != nil {
		t.Fatalf("criando a reserva %s pelo banco: %v", status, err)
	}
	if _, err := a.pool.Exec(a.ctx, `
		INSERT INTO reservation_guests (reservation_id, contact_id, is_lead_guest) VALUES ($1, $2, true)`, id, titular); err != nil {
		t.Fatalf("linha do titular da reserva %s: %v", status, err)
	}
	return id
}

// ─────────────────────────── A lista, pela porta da frente ──────────────────

func qhCaminho(reserva uuid.UUID) string { return "/reservations/" + reserva.String() + "/guests" }

func (a *ambiente) qhLer(t *testing.T, token string, reserva uuid.UUID) qhLista {
	t.Helper()
	r := a.chamar(t, http.MethodGet, qhCaminho(reserva), token, nil)
	if r.Status != http.StatusOK {
		t.Fatalf("GET %s: status %d — %s", qhCaminho(reserva), r.Status, r.Corpo)
	}
	var l qhLista
	r.decodificar(t, &l)
	if l.Data == nil {
		t.Fatalf("GET %s: `data` veio nulo; o contrato promete array — %s", qhCaminho(reserva), r.Corpo)
	}
	return l
}

func (a *ambiente) qhAcrescentar(t *testing.T, token string, reserva, contato uuid.UUID) resposta {
	t.Helper()
	return a.chamar(t, http.MethodPost, qhCaminho(reserva), token, map[string]any{"contact_id": contato})
}

func (a *ambiente) qhAcrescentarOK(t *testing.T, token string, reserva, contato uuid.UUID) {
	t.Helper()
	exigirStatusQB(t, a.qhAcrescentar(t, token, reserva, contato), http.StatusCreated, "POST /guests")
}

func (a *ambiente) qhRetirar(t *testing.T, token string, reserva, contato uuid.UUID) resposta {
	t.Helper()
	return a.chamar(t, http.MethodDelete, qhCaminho(reserva)+"/"+contato.String(), token, nil)
}

func (a *ambiente) qhCompleta(t *testing.T, token string, reserva uuid.UUID) (qhCompleta, resposta) {
	t.Helper()
	r := a.chamar(t, http.MethodGet, "/reservations/"+reserva.String()+"/full", token, nil)
	return qbDado[qhCompleta](t, r, http.StatusOK, "GET /full"), r
}

// qhMesmoHospede compara dois hóspedes pelo VALOR de cada campo (os opcionais
// são ponteiros, e `==` compararia endereços).
func qhMesmoHospede(x, y qhHospede) bool {
	igual := func(p, q *string) bool { return (p == nil && q == nil) || (p != nil && q != nil && *p == *q) }
	return x.ContactID == y.ContactID && x.Nome == y.Nome && x.Titular == y.Titular &&
		igual(x.DocTipo, y.DocTipo) && igual(x.Documento, y.Documento) && igual(x.Telefone, y.Telefone) && igual(x.Email, y.Email)
}

// qhExigirLista confere a lista, NA ORDEM.
func qhExigirLista(t *testing.T, contexto string, got []uuid.UUID, quer ...uuid.UUID) {
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

// qhPorNome devolve os ids na ordem que o contrato promete para os
// acompanhantes: por nome e, no empate, por `contact_id`.
func qhPorNome(ps ...qhPessoa) []uuid.UUID {
	c := append([]qhPessoa(nil), ps...)
	sort.Slice(c, func(i, j int) bool {
		if c[i].Nome != c[j].Nome {
			return c[i].Nome < c[j].Nome
		}
		return c[i].ID.String() < c[j].ID.String()
	})
	out := make([]uuid.UUID, len(c))
	for i, p := range c {
		out[i] = p.ID
	}
	return out
}

// ─────────────────────────── O banco como testemunha ────────────────────────

// qhLeiturasDoAtor conta as linhas `rooming_list` que o ator gravou.
func (a *ambiente) qhLeiturasDoAtor(t *testing.T, ator uuid.UUID) int {
	t.Helper()
	return a.qagContar(t, `SELECT count(*) FROM pii_access_log WHERE actor_id = $1 AND reason = 'rooming_list'`, ator)
}

// qhLeiturasDe conta TODA linha de `pii_access_log` destes contatos — de
// qualquer ator e por qualquer motivo.
func (a *ambiente) qhLeiturasDe(t *testing.T, contatos ...uuid.UUID) int {
	t.Helper()
	return a.qagContar(t, `SELECT count(*) FROM pii_access_log WHERE contact_id = ANY($1::uuid[])`, contatos)
}

// qhLeiturasPorContato devolve, por contato, quantas linhas `rooming_list` o
// ator gravou.
func (a *ambiente) qhLeiturasPorContato(t *testing.T, ator uuid.UUID) map[uuid.UUID]int {
	t.Helper()
	linhas, err := a.pool.Query(a.ctx, `
		SELECT contact_id, count(*) FROM pii_access_log
		 WHERE actor_id = $1 AND reason = 'rooming_list' GROUP BY contact_id`, ator)
	if err != nil {
		t.Fatalf("lendo pii_access_log: %v", err)
	}
	defer linhas.Close()
	out := map[uuid.UUID]int{}
	for linhas.Next() {
		var (
			c uuid.UUID
			n int
		)
		if err := linhas.Scan(&c, &n); err != nil {
			t.Fatalf("lendo pii_access_log: %v", err)
		}
		out[c] = n
	}
	return out
}

// qhTitularNoBanco confere, NO BANCO, que a lista tem exatamente uma linha de
// titular e que ela é o `contact_id` da reserva — devolve esse contato.
func (a *ambiente) qhTitularNoBanco(t *testing.T, reserva uuid.UUID, contexto string) uuid.UUID {
	t.Helper()
	var (
		contato   uuid.UUID
		titulares int
		lead      *uuid.UUID
	)
	if err := a.pool.QueryRow(a.ctx, `
		SELECT r.contact_id,
		       (SELECT count(*) FROM reservation_guests g WHERE g.reservation_id = r.id AND g.is_lead_guest),
		       (SELECT g.contact_id FROM reservation_guests g WHERE g.reservation_id = r.id AND g.is_lead_guest LIMIT 1)
		  FROM reservations r WHERE r.id = $1`, reserva).Scan(&contato, &titulares, &lead); err != nil {
		t.Fatalf("%s: lendo o titular: %v", contexto, err)
	}
	if titulares != 1 || lead == nil || *lead != contato {
		t.Fatalf("%s: a lista tem %d titular(es) (%v) para a reserva de contact_id %s — o contrato diz exatamente um, e é o da reserva",
			contexto, titulares, lead, contato)
	}
	return contato
}

// qhNomesNoBanco devolve quantas linhas a lista tem no banco.
func (a *ambiente) qhNomesNoBanco(t *testing.T, reserva uuid.UUID) int {
	t.Helper()
	return a.qagContar(t, `SELECT count(*) FROM reservation_guests WHERE reservation_id = $1`, reserva)
}

// ─────────────────────────── Concorrência ───────────────────────────────────

// qhChamarSeguro é o `chamar` que pode rodar fora da goroutine do teste: devolve
// o erro em vez de chamar t.Fatal.
func (a *ambiente) qhChamarSeguro(metodo, caminho, token, chave string, corpo any) (resposta, error) {
	var body io.Reader
	if corpo != nil {
		bruto, err := json.Marshal(corpo)
		if err != nil {
			return resposta{}, err
		}
		body = bytes.NewReader(bruto)
	}
	req, err := http.NewRequestWithContext(a.ctx, metodo, a.servidor.URL+PrefixoDaAPI+caminho, body)
	if err != nil {
		return resposta{}, err
	}
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if chave != "" {
		req.Header.Set("Idempotency-Key", chave)
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return resposta{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	lido, err := io.ReadAll(resp.Body)
	if err != nil {
		return resposta{}, err
	}
	return resposta{Status: resp.StatusCode, Corpo: lido, Headers: resp.Header}, nil
}

// qhDesfecho resume a resposta em "status:code" — `201:` no sucesso.
func qhDesfecho(r resposta, err error) string {
	if err != nil {
		return "erro de transporte: " + err.Error()
	}
	return fmt.Sprintf("%d:%s", r.Status, qagCodigo(r))
}
