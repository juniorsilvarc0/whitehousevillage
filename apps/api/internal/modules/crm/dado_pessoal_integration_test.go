//go:build integration

package crm_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// telefoneMascarado é a forma do contrato: `+`, um `*` por dígito escondido e
// os 4 últimos. Não é E.164 — não serve para `tel:`.
var telefoneMascarado = regexp.MustCompile(`^\+\*+\d{4}$`)

// O lead guarda o INTERESSE, não a pessoa: `contact_phone_e164` sai SEMPRE
// mascarado — na lista, no detalhe e na resposta das escritas. O número cheio
// mora na ficha do contato, que registra quem leu. A busca `q` continua
// comparando o valor cheio no servidor.
func TestTelefoneDoLeadSaiSempreMascarado(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)
	cheio := a.texto(t, `SELECT phone_e164 FROM contacts WHERE id = $1`, contato)

	exigir := func(onde string, r resposta, comLista bool) {
		t.Helper()
		if r.Status != http.StatusOK && r.Status != http.StatusCreated {
			t.Fatalf("%s = %d: %s", onde, r.Status, r.Corpo)
		}
		if strings.Contains(string(r.Corpo), cheio) {
			t.Fatalf("%s devolveu o telefone cheio %s", onde, cheio)
		}
		var linhas []struct {
			Fone *string `json:"contact_phone_e164"`
		}
		if comLista {
			linhas, _ = lista[struct {
				Fone *string `json:"contact_phone_e164"`
			}](t, r)
		} else {
			linhas = append(linhas, dado[struct {
				Fone *string `json:"contact_phone_e164"`
			}](t, r))
		}
		if len(linhas) == 0 {
			t.Fatalf("%s não devolveu lead nenhum", onde)
		}
		for _, l := range linhas {
			if l.Fone != nil && !telefoneMascarado.MatchString(*l.Fone) {
				t.Fatalf("%s: contact_phone_e164 = %q, fora da máscara", onde, *l.Fone)
			}
		}
	}

	r := a.chamar(t, http.MethodPost, "/crm/leads", token, map[string]any{"contact_id": contato})
	exigir("POST /crm/leads", r, false)
	lead := dado[struct {
		ID   uuid.UUID `json:"id"`
		Fone *string   `json:"contact_phone_e164"`
	}](t, r)
	if lead.Fone == nil || *lead.Fone != "+"+strings.Repeat("*", len(cheio)-5)+cheio[len(cheio)-4:] {
		t.Fatalf("máscara = %v, esperado %d asteriscos e os 4 últimos de %s", lead.Fone, len(cheio)-5, cheio)
	}

	exigir("GET /crm/leads/{id}", a.chamar(t, http.MethodGet, "/crm/leads/"+lead.ID.String(), token, nil), false)
	exigir("PATCH /crm/leads/{id}", a.chamar(t, http.MethodPatch, "/crm/leads/"+lead.ID.String(), token,
		map[string]any{"score": 40}), false)
	exigir("GET /crm/leads?per_page=100", a.chamar(t, http.MethodGet, "/crm/leads?per_page=100", token, nil), true)

	// A busca pelo número CHEIO acha o lead — e devolve mascarado.
	r = a.chamar(t, http.MethodGet, "/crm/leads?q="+strings.TrimPrefix(cheio, "+"), token, nil)
	exigir("GET /crm/leads?q=<telefone cheio>", r, true)
	if achados, _ := lista[struct {
		ID uuid.UUID `json:"id"`
	}](t, r); len(achados) != 1 || achados[0].ID != lead.ID {
		t.Fatalf("q pelo telefone cheio achou %v, esperado só o lead %s", achados, lead.ID)
	}
}

// `GET /crm/opportunities/{id}/full` traz o contato com telefone e e-mail
// CHEIOS (é de onde sai o `tel:`), e por isso grava `pii_access_log` com
// `reason: opportunity`. Até esta rodada o schema dizia "o resumo embutido no
// card não grava" — valor cheio sem máscara e sem rastro. Fora do escopo: 404
// sem gravar. A lista e o kanban não carregam canal de contato.
func TestFullDaOportunidadeRegistraOContato(t *testing.T) {
	a := subir(t)
	gestor, token := a.gestor(t)
	_, corretor := a.corretor(t)
	contato := a.contato(t)
	cheio := a.texto(t, `SELECT phone_e164 FROM contacts WHERE id = $1`, contato)
	t.Cleanup(func() { a.executar(t, `DELETE FROM pii_access_log WHERE contact_id = $1`, contato) })

	card := a.criarOportunidade(t, token, map[string]any{"contact_id": contato})
	contar := func() int {
		t.Helper()
		return a.contar(t, `SELECT count(*) FROM pii_access_log WHERE contact_id = $1`, contato)
	}

	r := a.chamar(t, http.MethodGet, "/crm/opportunities/"+card.ID.String()+"/full", token, nil)
	if r.Status != http.StatusOK || !strings.Contains(string(r.Corpo), cheio) {
		t.Fatalf("GET /full = %d, sem o telefone cheio do contato: %s", r.Status, r.Corpo)
	}
	if n := a.contar(t, `SELECT count(*) FROM pii_access_log WHERE contact_id = $1 AND actor_id = $2 AND reason = 'opportunity'`,
		contato, gestor); n != 1 {
		t.Fatalf("%d linha(s) opportunity do gestor; esperado 1", n)
	}

	// O corretor (own) não é dono do card: 404, e nada gravado.
	if r = a.chamar(t, http.MethodGet, "/crm/opportunities/"+card.ID.String()+"/full", corretor, nil); r.Status != http.StatusNotFound {
		t.Fatalf("GET /full fora do escopo = %d, esperado 404", r.Status)
	}
	if n := contar(); n != 1 {
		t.Fatalf("a leitura recusada gravou acesso: %d linhas, esperado continuar em 1", n)
	}

	// Lista e kanban: coleções, sem canal de contato — e sem gravar.
	for _, caminho := range []string{"/crm/opportunities?per_page=100", "/crm/opportunities/kanban"} {
		r = a.chamar(t, http.MethodGet, caminho, token, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", caminho, r.Status, r.Corpo)
		}
		if strings.Contains(string(r.Corpo), cheio) || strings.Contains(string(r.Corpo), `"phone_e164"`) {
			t.Fatalf("GET %s trouxe telefone de contato", caminho)
		}
		var bruto map[string]json.RawMessage
		if err := json.Unmarshal(r.Corpo, &bruto); err != nil {
			t.Fatalf("GET %s: corpo ilegível: %v", caminho, err)
		}
	}
	if n := contar(); n != 1 {
		t.Fatalf("coleção gravou acesso: %d linhas, esperado 1", n)
	}
}
