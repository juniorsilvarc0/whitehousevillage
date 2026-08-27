//go:build integration

package crm_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Campo desconhecido no corpo é 422 em TODA rota de escrita do CRM.
//
// ═══ Por que esta varredura vive aqui, e não só no router ═══
//
// `internal/router/regressao_rodada3_integration_test.go` já faz esta varredura
// para a API inteira, e ela é a certa: uma rota nova com corpo tem de nascer
// dentro da cobertura. Só que aquela varredura resolve `{id}` por PREFIXO DE
// PRIMEIRO SEGMENTO — `/reservations/{id}` acha o alvo em `alvos["/reservations"]`
// —, e o CRM tem SEIS coleções sob um prefixo só (`/crm`). Com um alvo por
// prefixo, ela não sabe alcançar `/crm/leads/{id}` nem `/crm/stages/{id}`, e
// reporta exatamente isso. A correção é dela (ver o relatório); enquanto não
// vem, a garantia existe aqui, com os ids reais de cada coleção.
//
// O que a regra impede: 200 sem efeito. Medido nesta árvore antes de o decoder
// recusar campo desconhecido, `PATCH /units/{id} {"ativa":false}` respondia 200
// com `active` intacto — a pior resposta possível, porque o cliente acha que
// gravou e não há erro em lugar nenhum para alguém investigar.
func TestNenhumaRotaDoCRMAceitaCampoDesconhecidoNoCorpo(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	contato := a.contato(t)

	funil, etapas := a.funilDoSeed(t)
	motivo := a.motivoDePerda(t)
	lead := a.criarLead(t, token, map[string]any{"contact_id": contato})
	card := a.criarOportunidade(t, token, map[string]any{"contact_id": contato})

	resp := a.chamar(t, http.MethodPost, "/crm/activities", token, map[string]any{
		"type": "tarefa", "subject": "alvo da varredura", "contact_id": contato,
	})
	if resp.Status != http.StatusCreated {
		t.Fatalf("preparando a atividade: %d (%s)", resp.Status, resp.Corpo)
	}
	atividade := dado[atividadeVista](t, resp).ID

	// Um alvo por COLEÇÃO, e não por prefixo: é a diferença que falta na
	// varredura do router.
	alvos := map[string]uuid.UUID{
		"/crm/pipelines":     funil,
		"/crm/stages":        etapas["Novo lead"],
		"/crm/lost-reasons":  motivo,
		"/crm/leads":         lead,
		"/crm/opportunities": card.ID,
		"/crm/activities":    atividade,
	}

	const desconhecido = "campo_que_o_contrato_nao_declara"

	var varridas int
	for _, r := range rotas(nil) {
		if r.metodo == http.MethodGet || r.metodo == http.MethodDelete {
			continue
		}

		caminho := r.path
		if strings.Contains(caminho, "{id}") {
			colecao := caminho[:strings.Index(caminho, "/{id}")]
			alvo, ok := alvos[colecao]
			if !ok {
				t.Errorf("%s %s: a varredura não sabe alcançar esta rota (falta alvo para %q)",
					r.metodo, r.path, colecao)
				continue
			}
			caminho = strings.ReplaceAll(caminho, "{id}", alvo.String())
		}
		varridas++

		// A chave de idempotência entra em todas: sem ela, o `/win` recusaria
		// pelo header antes de o decoder chegar ao corpo, e a rota passaria pela
		// varredura sem ter sido varrida.
		resposta := a.chamarIdem(t, r.metodo, caminho, token,
			map[string]any{desconhecido: "x"}, "varredura-"+sufixo())

		if resposta.Status != http.StatusUnprocessableEntity {
			t.Errorf("%s %s respondeu %d para campo desconhecido, esperado 422 — corpo: %s",
				r.metodo, caminho, resposta.Status, resposta.Corpo)
			continue
		}
		if _, nomeado := resposta.detalhes(t)[desconhecido]; !nomeado {
			// Sem o NOME em `details`, o painel não consegue grudar o erro no
			// input e a mensagem vira "algo está errado".
			t.Errorf("%s %s recusou mas não nomeou o campo: %s", r.metodo, caminho, resposta.Corpo)
		}
	}

	if varridas == 0 {
		t.Fatal("a varredura não alcançou rota nenhuma — ela deixou de proteger alguma coisa")
	}
	t.Logf("varredura: %d rotas de escrita do CRM, todas recusando campo desconhecido", varridas)
}
