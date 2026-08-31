//go:build integration

// A jornada que a dívida técnica desta rodada bloqueava, percorrida INTEIRA
// pela porta da frente — e o critério é literal: NENHUM passo pode precisar de
// SQL.
//
// Antes desta rodada a sequência era impossível de executar por uma API só.
// Medido e registrado pelos três agentes que a atacaram:
//
//	POST /contacts                     → 404 (a rota não existia)
//	POST /quotes                       → 200 com 16 chaves e NENHUMA `id`
//	GET  /quotes/{id}                  → 404
//	POST /crm/opportunities/{id}/win   → 422 QUOTE_REQUIRED_TO_WIN, com a dica
//	                                     "emita o orçamento e vincule-o" — uma
//	                                     instrução impossível de seguir
//
// Como `reservations.contact_id` é NOT NULL, vender exigia um `INSERT` à mão em
// `contacts`; e como o orçamento não era gravado, o `/win` não tinha o que
// consumir. Os testes de então não acusavam nada disso porque criavam o contato
// por fixture SQL — o passo que faltava era exatamente o que eles pulavam.
//
// Por isso este arquivo tem uma regra própria, e ela vale como asserção: as
// SETE escritas da jornada saem todas de `a.chamar`/`a.chamarComChave`. O `pool`
// aparece só em duas funções, ambas nomeadas — `exigirSeed` (ler o tarifário
// publicado) e a limpeza do `t.Cleanup`. Se um dia alguém precisar de um
// `INSERT` para fazer esta jornada andar, a dívida voltou.
package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// A janela é distante e fixa (política da casa, docs/testing.md §4): 2031 não
// colide com dado real nem com as datas de novembro/2026 da jornada da Fase 1,
// que roda no mesmo banco. Distante também fixa a faixa de cancelamento no topo
// (mais de 30 dias de antecedência → devolução integral), o que torna o número
// do `dry_run` determinístico sem amarrá-lo ao mês em que a suíte roda.
const (
	r4Entrada = "2031-03-10"
	r4Saida   = "2031-03-13"
	r4Noites  = 3
)

// ─────────────────────────── Formas de resposta ─────────────────────────────

type contatoQA struct {
	ID       uuid.UUID `json:"id"`
	Nome     string    `json:"name"`
	Telefone *string   `json:"phone_e164"`
}

type orcamentoSalvoQA struct {
	ID             uuid.UUID  `json:"id"`
	Noites         int        `json:"night_count"`
	Subtotal       int64      `json:"subtotal_cents"`
	Limpeza        int64      `json:"cleaning_cents"`
	Total          int64      `json:"total_cents"`
	Sinal          int64      `json:"deposit_cents"`
	Saldo          int64      `json:"balance_cents"`
	ContactID      *uuid.UUID `json:"contact_id"`
	OportunidadeID *uuid.UUID `json:"opportunity_id"`
	ValidoAte      time.Time  `json:"valid_until"`
	Vencido        bool       `json:"expired"`
	ReservaID      *uuid.UUID `json:"reservation_id"`
	RateTableID    *string    `json:"rate_table_id"`
	PolicyVersion  int        `json:"policy_version"`
	Diarias        []struct {
		Data  string `json:"date"`
		Tipo  string `json:"date_type"`
		Preco int64  `json:"price_cents"`
	} `json:"nights"`
}

type oportunidadeQA struct {
	ID        uuid.UUID  `json:"id"`
	ContactID uuid.UUID  `json:"contact_id"`
	Status    string     `json:"status"`
	ReservaID *uuid.UUID `json:"reservation_id"`
}

type ganhoQA struct {
	Oportunidade  oportunidadeQA `json:"opportunity"`
	Reserva       reservaQA      `json:"reservation"`
	ReservaCriada bool           `json:"reservation_created"`
}

// ─────────────────────────── Apoio ──────────────────────────────────────────

// perfilComercialDaRodada4 é o corretor que fecha a venda inteira: agenda,
// contatos, orçamento, funil e reserva.
//
// Vale como asserção secundária. Os recursos `contacts` e `quotes` nasceram
// nesta rodada; se algum deles não estivesse no catálogo do seed, a criação do
// perfil estouraria a FK de `role_permissions` aqui, e não três passos adiante
// com um 403 sem explicação.
func (a *ambiente) perfilComercialDaRodada4(t *testing.T) usuarioDeTeste {
	t.Helper()

	todas := func(recurso string) []auth.Permissao {
		out := make([]auth.Permissao, 0, 4)
		for _, acao := range []string{auth.AcaoVer, auth.AcaoCriar, auth.AcaoEditar, auth.AcaoExcluir} {
			out = append(out, auth.Permissao{Resource: recurso, Action: acao, Scope: auth.EscopoAll})
		}
		return out
	}

	var matriz []auth.Permissao
	for _, recurso := range []string{
		"contacts", "quotes", "calendar", "reservations",
		"crm.opportunities", "crm.pipelines", "crm.activities",
	} {
		matriz = append(matriz, todas(recurso)...)
	}

	return a.criarUsuario(t, "comercial-r4", a.criarPerfil(t, "comercial-r4", matriz))
}

// limparRastroDoContato leva embora tudo que a jornada gerou.
//
// Ordem obrigatória: `crm_opportunities` primeiro (cascateia atividades e
// `crm_stage_history`), depois `quotes` (cascateia `quote_nights`), depois
// `reservations` (cascateia `stay_blocks`, noites, preço e unidades) e só então
// o contato. Todas as FKs que apontam para `contacts` são NO ACTION, então a
// ordem inversa deixaria a linha presa e a próxima execução herdaria lixo.
func (a *ambiente) limparRastroDoContato(t *testing.T, contato uuid.UUID) {
	t.Helper()

	t.Cleanup(func() {
		a.executarQA(t, `DELETE FROM quotes WHERE contact_id = $1`, contato)
		a.executarQA(t, `DELETE FROM crm_opportunities WHERE contact_id = $1`, contato)
		a.executarQA(t, `DELETE FROM reservation_guests WHERE contact_id = $1`, contato)
		a.executarQA(t, `DELETE FROM reservations WHERE contact_id = $1`, contato)
		a.executarQA(t, `DELETE FROM pii_access_log WHERE contact_id = $1`, contato)
		a.executarQA(t, `DELETE FROM audit_log WHERE entity_id = $1`, contato)
		a.executarQA(t, `DELETE FROM contacts WHERE id = $1`, contato)
	})
}

// telefoneDeTeste devolve um E.164 na faixa 9 0000 xxxx, que não é atribuível a
// celular nenhum no Brasil — a mesma faixa que o seed usa. O sufixo é sorteado
// porque `contacts_phone_idx` é UNIQUE: dois usos do mesmo número em execuções
// diferentes dariam 409 e o teste acusaria defeito onde só houve reexecução.
func telefoneDeTeste() string {
	digitos := strings.ReplaceAll(uuid.NewString(), "-", "")
	numero := ""
	for _, c := range digitos {
		if c >= '0' && c <= '9' {
			numero += string(c)
		}
		if len(numero) == 4 {
			break
		}
	}
	return "+5585900000" + numero[:4][:4]
}

// ─────────────────────────── A jornada ──────────────────────────────────────

func TestJornadaDaRodada4DoContatoAoCancelamentoSemNenhumSQL(t *testing.T) {
	a := subirAPI(t)
	propriedade := exigirSeed(t, a)

	u := a.perfilComercialDaRodada4(t)
	produto := a.produtoDoSeed(t, propriedade, "apto-2s")

	// O estoque ANTES de qualquer coisa. É contra este número que o passo 7
	// compara — e não contra "sobrou pelo menos uma".
	//
	// Medido: com "pelo menos uma" a asserção final passa VERDE mesmo com o
	// cancelamento deixando o bloco de pé, porque o apto-2s tem TRÊS unidades e
	// uma presa ainda deixa duas livres. A mutação (`UPDATE stay_blocks SET
	// status='confirmed'`) atravessou a versão frouxa e é pega por esta.
	estoqueAntes := a.disponibilidadeDaJanelaR4(t, u.Token, produto)

	// ── 1. O CONTATO, pela API ────────────────────────────────────────────
	//
	// Este é o passo que não existia. Sem ele a jornada inteira dependia de um
	// `INSERT INTO contacts`, e "vender pelo sistema" era mentira.
	telefone := telefoneDeTeste()
	marca := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]

	criado := a.chamar(t, http.MethodPost, "/contacts", u.Token, map[string]any{
		"name":             "Jornada R4 " + marca,
		"phone_e164":       telefone,
		"email":            "jornada-r4-" + marca + "@exemplo.invalid",
		"lgpd_basis":       "contrato",
		"marketing_opt_in": false,
	})
	contato := envelopeDe[contatoQA](t, criado, http.StatusCreated, "POST /contacts")
	a.limparRastroDoContato(t, contato.ID)

	if contato.ID == uuid.Nil {
		t.Fatal("POST /contacts respondeu 201 sem `id`: sem id não há como vender para esta pessoa")
	}
	if contato.Telefone == nil || *contato.Telefone != telefone {
		t.Fatalf("o telefone gravado (%v) não é o que o atendente digitou (%s)", contato.Telefone, telefone)
	}

	// A pessoa tem de estar encontrável pelo telefone — é o caminho do inbound
	// de WhatsApp, e é o que separa "gravou" de "gravou onde alguém acha".
	achado := envelopeDe[[]contatoQA](t, a.chamar(t, http.MethodGet,
		"/contacts?phone="+strings.Replace(telefone, "+", "%2B", 1), u.Token, nil),
		http.StatusOK, "GET /contacts?phone=")
	if len(achado) != 1 || achado[0].ID != contato.ID {
		t.Fatalf("busca por telefone E.164 devolveu %d resultado(s); esperava exatamente o contato recém-criado", len(achado))
	}

	// ── 2. A OPORTUNIDADE no funil ────────────────────────────────────────
	oportunidade := envelopeDe[oportunidadeQA](t, a.chamar(t, http.MethodPost, "/crm/opportunities", u.Token,
		map[string]any{
			"contact_id":   contato.ID,
			"unit_type_id": produto,
			"check_in":     r4Entrada,
			"check_out":    r4Saida,
		}), http.StatusCreated, "POST /crm/opportunities")

	// "aberta" e não "aberto": o banco guarda o masculino (`crm_opportunities_status_check`)
	// e a API publica o feminino, porque o sujeito na tela é "a oportunidade".
	// A tradução tem dono (`crm.crm.go`) e teste próprio; aqui basta usar o
	// vocabulário da PORTA, que é o que o painel consome.
	if oportunidade.Status != "aberta" {
		t.Fatalf("a oportunidade nasceu em %q, esperado aberta", oportunidade.Status)
	}

	// ── 3. O ORÇAMENTO PERSISTIDO, vinculado ao card ──────────────────────
	//
	// `persist: true` é o que separa simular de emitir. Sem ele a resposta é
	// 200 sem `id` — o comportamento de hoje, preservado, porque a tela de
	// orçamento dispara esta rota a cada tecla.
	orcamento := envelopeDe[orcamentoSalvoQA](t, a.chamar(t, http.MethodPost, "/quotes", u.Token,
		map[string]any{
			"unit_type_id":   produto,
			"check_in":       r4Entrada,
			"check_out":      r4Saida,
			"guests_count":   2,
			"persist":        true,
			"contact_id":     contato.ID,
			"opportunity_id": oportunidade.ID,
		}), http.StatusCreated, "POST /quotes {persist:true}")

	if orcamento.ID == uuid.Nil {
		t.Fatal("POST /quotes com persist devolveu 201 sem `id` — era exatamente este o buraco: " +
			"sem id o /win não tem o que consumir e a dica dele fica impossível de seguir")
	}
	if orcamento.Noites != r4Noites {
		t.Fatalf("orçamento de %s a %s tem %d noites, esperado %d (a estadia é half-open)",
			r4Entrada, r4Saida, orcamento.Noites, r4Noites)
	}
	if orcamento.OportunidadeID == nil || *orcamento.OportunidadeID != oportunidade.ID {
		t.Fatalf("o orçamento não ficou vinculado ao card: opportunity_id = %v", orcamento.OportunidadeID)
	}
	if orcamento.ContactID == nil || *orcamento.ContactID != contato.ID {
		t.Fatalf("o orçamento não ficou vinculado ao contato: contact_id = %v", orcamento.ContactID)
	}
	if orcamento.Vencido {
		t.Fatal("o orçamento nasceu vencido")
	}
	if orcamento.ReservaID != nil {
		t.Fatalf("orçamento recém-emitido já aponta para a reserva %v", *orcamento.ReservaID)
	}
	// A identidade que o hóspede confere na proposta.
	if soma := orcamento.Subtotal + orcamento.Limpeza; soma != orcamento.Total {
		t.Fatalf("a proposta não fecha: subtotal %d + limpeza %d = %d, e o total diz %d",
			orcamento.Subtotal, orcamento.Limpeza, soma, orcamento.Total)
	}

	// `GET /quotes/{id}` — a rota que respondia 404. É por ela que o corretor
	// reabre a proposta que mandou para o hóspede.
	relido := envelopeDe[orcamentoSalvoQA](t, a.chamar(t, http.MethodGet,
		fmt.Sprintf("/quotes/%s", orcamento.ID), u.Token, nil), http.StatusOK, "GET /quotes/{id}")

	if relido.Total != orcamento.Total || relido.Sinal != orcamento.Sinal {
		t.Fatalf("reabrir o orçamento mudou o preço: emitido total %d / sinal %d, relido total %d / sinal %d",
			orcamento.Total, orcamento.Sinal, relido.Total, relido.Sinal)
	}
	if len(relido.Diarias) != r4Noites {
		t.Fatalf("o orçamento relido tem %d diárias detalhadas, esperado %d — sem elas a proposta "+
			"não se explica para o hóspede", len(relido.Diarias), r4Noites)
	}

	// ── 4. O /win: o card vira reserva ────────────────────────────────────
	ganho := envelopeDe[ganhoQA](t, a.chamarComChave(t, http.MethodPost,
		fmt.Sprintf("/crm/opportunities/%s/win", oportunidade.ID), u.Token,
		map[string]any{"quote_id": orcamento.ID}, a.chaveNova(t, "win")),
		http.StatusCreated, "POST /crm/opportunities/{id}/win")

	reserva := ganho.Reserva
	if !ganho.ReservaCriada {
		t.Fatal("/win respondeu 201 dizendo que não criou reserva")
	}
	if reserva.Status != "hold" {
		t.Fatalf("a reserva do /win nasceu em %q, esperado hold — o ganho do funil segura a data, não a cobra", reserva.Status)
	}
	if reserva.HoldExpiraEm == nil {
		t.Fatal("pré-reserva sem hold_expires_at: a data fica presa para sempre sem ninguém ter pago")
	}
	if !strings.HasPrefix(reserva.Codigo, "WH-") {
		t.Fatalf("código de reserva %q fora do padrão WH-AAAA-NNNN", reserva.Codigo)
	}
	if ganho.Oportunidade.Status != "ganha" {
		t.Fatalf("a oportunidade ficou em %q depois do /win, esperado ganha", ganho.Oportunidade.Status)
	}
	if ganho.Oportunidade.ReservaID == nil || *ganho.Oportunidade.ReservaID != reserva.ID {
		t.Fatalf("o card não ficou apontando para a reserva: reservation_id = %v", ganho.Oportunidade.ReservaID)
	}

	// O motor NÃO roda de novo: a reserva é a TRANSCRIÇÃO do orçamento.
	//
	// É a regra 7 do CLAUDE.md no ponto em que ela é mais fácil de perder:
	// entre emitir a proposta e ganhar o card passa-se tempo, e nesse tempo a
	// gestão pode reajustar a tarifa. O hóspede fechou pelo número da proposta.
	if reserva.Total != orcamento.Total || reserva.Subtotal != orcamento.Subtotal || reserva.Sinal != orcamento.Sinal {
		t.Fatalf("a reserva não copiou o orçamento: proposta total %d / subtotal %d / sinal %d, "+
			"reserva total %d / subtotal %d / sinal %d — o preço foi recalculado no /win",
			orcamento.Total, orcamento.Subtotal, orcamento.Sinal,
			reserva.Total, reserva.Subtotal, reserva.Sinal)
	}

	// E o orçamento passa a apontar para a reserva — é o que o torna consumido.
	depoisDoGanho := envelopeDe[orcamentoSalvoQA](t, a.chamar(t, http.MethodGet,
		fmt.Sprintf("/quotes/%s", orcamento.ID), u.Token, nil), http.StatusOK, "GET /quotes/{id} depois do /win")
	if depoisDoGanho.ReservaID == nil || *depoisDoGanho.ReservaID != reserva.ID {
		t.Fatalf("depois do ganho o orçamento não aponta para a reserva (%v): "+
			"um orçamento sem dono pode ser convertido duas vezes", depoisDoGanho.ReservaID)
	}

	// ── 5. O SINAL: a pré-reserva vira venda ──────────────────────────────
	confirmada := envelopeDe[reservaQA](t, a.chamarComChave(t, http.MethodPost,
		fmt.Sprintf("/reservations/%s/confirm", reserva.ID), u.Token,
		map[string]any{"deposit_paid_cents": orcamento.Sinal, "method": "pix"},
		a.chaveNova(t, "confirm")), http.StatusOK, "POST /reservations/{id}/confirm")

	if confirmada.Status != "confirmed" {
		t.Fatalf("depois do sinal a reserva ficou em %q, esperado confirmed", confirmada.Status)
	}
	if confirmada.HoldExpiraEm != nil {
		t.Fatalf("reserva paga continua com prazo de pré-reserva (%v): o job de expiração derrubaria "+
			"uma venda confirmada", confirmada.HoldExpiraEm)
	}
	if confirmada.Total != orcamento.Total {
		t.Fatalf("confirmar recalculou o total: %d, e o hóspede fechou por %d", confirmada.Total, orcamento.Total)
	}

	// ── 6. O CANCELAMENTO: o número aparece ANTES de ser executado ────────
	//
	// A ordem é a da tela: simula, mostra, e só então executa. O que se prova
	// aqui é que os dois números são o mesmo — se a simulação e a execução
	// divergissem, o operador estaria lendo uma promessa que a casa não cumpre.
	simulado := envelopeDe[cancelamentoQA](t, a.chamar(t, http.MethodPost,
		fmt.Sprintf("/reservations/%s/cancel?dry_run=1", reserva.ID), u.Token,
		map[string]any{"reason": "desistencia"}), http.StatusOK, "POST /cancel?dry_run=1")

	if !simulado.Simulado {
		t.Fatal("a resposta do dry_run não se declara simulação — o operador não tem como saber que nada aconteceu")
	}
	if simulado.SinalPago != orcamento.Sinal {
		t.Fatalf("a simulação calculou sobre um sinal de %d, e o hóspede pagou %d", simulado.SinalPago, orcamento.Sinal)
	}
	if simulado.Devolucao+simulado.Retido != simulado.SinalPago {
		t.Fatalf("a conta do cancelamento não fecha: devolve %d + retém %d ≠ pago %d",
			simulado.Devolucao, simulado.Retido, simulado.SinalPago)
	}

	// A reserva continua de pé depois da simulação — dry_run que executa é a
	// pior falha possível desta rota.
	aindaViva := envelopeDe[reservaQA](t, a.chamar(t, http.MethodGet,
		fmt.Sprintf("/reservations/%s", reserva.ID), u.Token, nil), http.StatusOK, "GET /reservations/{id}")
	if aindaViva.Status != "confirmed" {
		t.Fatalf("depois do dry_run a reserva está em %q: a simulação executou o cancelamento", aindaViva.Status)
	}

	executado := envelopeDe[cancelamentoQA](t, a.chamarComChave(t, http.MethodPost,
		fmt.Sprintf("/reservations/%s/cancel", reserva.ID), u.Token,
		map[string]any{"reason": "desistencia"}, a.chaveNova(t, "cancel")),
		http.StatusOK, "POST /reservations/{id}/cancel")

	if executado.Devolucao != simulado.Devolucao || executado.Retido != simulado.Retido {
		t.Fatalf("o cancelamento executado (devolve %d, retém %d) não é o que a tela mostrou "+
			"(devolve %d, retém %d)", executado.Devolucao, executado.Retido, simulado.Devolucao, simulado.Retido)
	}
	if executado.Simulado {
		t.Fatal("o cancelamento executado ainda se declara dry_run")
	}
	if executado.Status != "cancelled" {
		t.Fatalf("o desfecho do cancelamento é %q, esperado cancelled", executado.Status)
	}

	// ── 7. A data volta ao estoque ────────────────────────────────────────
	estoqueDepois := a.disponibilidadeDaJanelaR4(t, u.Token, produto)
	if len(estoqueDepois.Dias) != len(estoqueAntes.Dias) {
		t.Fatalf("a janela mudou de tamanho entre as duas consultas (%d → %d dias)",
			len(estoqueAntes.Dias), len(estoqueDepois.Dias))
	}
	for i, dia := range estoqueDepois.Dias {
		if dia.Disponivel != estoqueAntes.Dias[i].Disponivel {
			t.Fatalf("%s: antes da venda havia %d unidade(s) disponível(is) e depois do cancelamento "+
				"há %d — a data não voltou inteira ao estoque",
				dia.Data, estoqueAntes.Dias[i].Disponivel, dia.Disponivel)
		}
	}
}

// disponibilidadeDaJanelaR4 consulta a janela da jornada da rodada 4.
func (a *ambiente) disponibilidadeDaJanelaR4(t *testing.T, token string, produto uuid.UUID) linhaDaDisponibilidade {
	t.Helper()

	caminho := fmt.Sprintf("/availability?from=%s&to=%s&unit_type_id=%s", r4Entrada, r4Saida, produto)
	linhas := envelopeDe[[]linhaDaDisponibilidade](t, a.chamar(t, http.MethodGet, caminho, token, nil),
		http.StatusOK, "GET /availability da janela de 2031")
	if len(linhas) != 1 {
		t.Fatalf("GET /availability filtrado por um produto devolveu %d linhas, esperado 1", len(linhas))
	}
	return linhas[0]
}

// ─────────────────────────────────────────────────────────────────────────────

// A dica do 422 tem de ser EXECUTÁVEL.
//
// Antes desta rodada, `/win` sem orçamento respondia 422 mandando "emita o
// orçamento e vincule-o à oportunidade" — e as duas metades da instrução eram
// impossíveis: `POST /quotes` não gravava e o `PATCH` do card recusa `quote_id`
// de propósito. Este teste segue a dica ao pé da letra e exige que ela funcione.
//
// Um teste que só conferisse o código do erro passaria com a dica mentindo.
func TestADicaDoGanhoSemOrcamentoEhExecutavel(t *testing.T) {
	a := subirAPI(t)
	propriedade := exigirSeed(t, a)

	u := a.perfilComercialDaRodada4(t)
	produto := a.produtoDoSeed(t, propriedade, "suite-piscina")

	marca := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	contato := envelopeDe[contatoQA](t, a.chamar(t, http.MethodPost, "/contacts", u.Token, map[string]any{
		"name":       "Dica R4 " + marca,
		"phone_e164": telefoneDeTeste(),
	}), http.StatusCreated, "POST /contacts")
	a.limparRastroDoContato(t, contato.ID)

	oportunidade := envelopeDe[oportunidadeQA](t, a.chamar(t, http.MethodPost, "/crm/opportunities", u.Token,
		map[string]any{"contact_id": contato.ID}), http.StatusCreated, "POST /crm/opportunities")

	// O 422 e a dica.
	recusa := a.chamarComChave(t, http.MethodPost,
		fmt.Sprintf("/crm/opportunities/%s/win", oportunidade.ID), u.Token, nil, a.chaveNova(t, "dica"))
	if recusa.Status != http.StatusUnprocessableEntity {
		t.Fatalf("/win sem orçamento respondeu %d, esperado 422 — corpo: %s", recusa.Status, recusa.Corpo)
	}
	if c := recusa.codigoDeErro(t); c != "QUOTE_REQUIRED_TO_WIN" {
		t.Fatalf("code = %q, esperado QUOTE_REQUIRED_TO_WIN", c)
	}

	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Details struct {
				Hint string `json:"hint"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recusa.Corpo, &envelope); err != nil {
		t.Fatalf("corpo do erro ilegível: %v — %s", err, recusa.Corpo)
	}
	dica := envelope.Error.Details.Hint + " " + envelope.Error.Message
	if !strings.Contains(strings.ToLower(dica), "orçamento") {
		t.Fatalf("o 422 não diz o que fazer: %q", dica)
	}

	// Agora a dica, seguida. Se qualquer um destes dois passos falhar, a
	// mensagem do 422 está mandando o operador fazer algo que o sistema não faz.
	orcamento := envelopeDe[orcamentoSalvoQA](t, a.chamar(t, http.MethodPost, "/quotes", u.Token,
		map[string]any{
			"unit_type_id":   produto,
			"check_in":       r4Entrada,
			"check_out":      r4Saida,
			"guests_count":   2,
			"persist":        true,
			"contact_id":     contato.ID,
			"opportunity_id": oportunidade.ID,
		}), http.StatusCreated, "POST /quotes seguindo a dica do 422")

	segunda := a.chamarComChave(t, http.MethodPost,
		fmt.Sprintf("/crm/opportunities/%s/win", oportunidade.ID), u.Token,
		map[string]any{"quote_id": orcamento.ID}, a.chaveNova(t, "dica-win"))
	if segunda.Status != http.StatusCreated {
		t.Fatalf("depois de seguir a dica do 422 ao pé da letra, /win respondeu %d — a instrução "+
			"que a API dá ao operador não é executável. Corpo: %s", segunda.Status, segunda.Corpo)
	}
}
