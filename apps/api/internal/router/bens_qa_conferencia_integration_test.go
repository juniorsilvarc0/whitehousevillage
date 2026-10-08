//go:build integration

package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// qbApuracao é o pedaço comparável entre o `POST /close` e o `result` do GET.
type qbApuracao struct {
	Divergencias  json.RawMessage `json:"divergences"`
	AvariasAberta json.RawMessage `json:"issues_created"`
}

func qbCanonico(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := json.Compact(&out, b); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// qbCasaContada monta uma unidade com três bens em dois cômodos, abre, conta
// tudo e fecha. Prato: esperava 12, contou 9 (falta 3, R$ 18,90 cada). Taça:
// sem custo, esperava 6, contou 4 (falta 2, perda nula). Toalha: esperava 4,
// contou 5 (sobra, não abre avaria).
type qbCasaContada struct {
	Unidade               uuid.UUID
	Cozinha, Quarto       uuid.UUID
	Prato, Taca, Toalha   uuid.UUID
	ColPrato, ColTaca     string
	Conferencia           uuid.UUID
	Fechamento            qbApuracao
	FechamentoCru         resposta
	LinhaPrato, LinhaTaca uuid.UUID
	AvariaDoPrato         uuid.UUID
	AvariaDaTaca          uuid.UUID
}

func (a *ambiente) qbContarEFechar(t *testing.T, f *qbFaxina, tk string) qbCasaContada {
	t.Helper()
	var c qbCasaContada
	c.Unidade, _ = f.qbUnidade(t, a.qbPropriedadePadrao(t), "")
	c.Cozinha = a.qbComodo(t, tk, c.Unidade, "Cozinha", "cozinha", 1)
	c.Quarto = a.qbComodo(t, tk, c.Unidade, "Quarto", "quarto", 2)
	c.Prato = a.qbBem(t, f, tk, "Prato "+qbSufixo(), ptrInt64(1890))
	c.Taca = a.qbBem(t, f, tk, "Taça "+qbSufixo(), nil)
	c.Toalha = a.qbBem(t, f, tk, "Toalha "+qbSufixo(), ptrInt64(4500))
	c.ColPrato = a.qbColocar(t, tk, c.Cozinha, c.Prato, 12)
	c.ColTaca = a.qbColocar(t, tk, c.Cozinha, c.Taca, 6)
	a.qbColocar(t, tk, c.Quarto, c.Toalha, 4)

	conf := a.qbAbrir(t, tk, c.Unidade)
	c.Conferencia = conf.ID
	c.LinhaPrato, c.LinhaTaca = conf.linhaDo(t, c.Prato).ID, conf.linhaDo(t, c.Taca).ID
	exigirStatusQB(t, a.qbContar(t, tk, conf.ID, c.LinhaPrato, 9), http.StatusOK, "contando o prato")
	exigirStatusQB(t, a.qbContar(t, tk, conf.ID, c.LinhaTaca, 4), http.StatusOK, "contando a taça")
	exigirStatusQB(t, a.qbContar(t, tk, conf.ID, conf.linhaDo(t, c.Toalha).ID, 5), http.StatusOK, "contando a toalha")

	c.FechamentoCru = a.chamar(t, http.MethodPost, "/inventory/counts/"+conf.ID.String()+"/close", tk, map[string]any{})
	c.Fechamento = qbDado[qbApuracao](t, c.FechamentoCru, http.StatusOK, "fechando")
	lido := qbDado[struct {
		Divergencias []struct {
			Bem    uuid.UUID  `json:"item_id"`
			Avaria *uuid.UUID `json:"issue_id"`
		} `json:"divergences"`
		Criadas int `json:"issues_created"`
	}](t, c.FechamentoCru, http.StatusOK, "fechando")
	if len(lido.Divergencias) != 3 || lido.Criadas != 2 {
		t.Fatalf("premissa: 3 divergências (prato, taça, toalha) e 2 avarias (só as faltas): %s", c.FechamentoCru.Corpo)
	}
	for _, d := range lido.Divergencias {
		switch d.Bem {
		case c.Prato:
			c.AvariaDoPrato = *d.Avaria
		case c.Taca:
			c.AvariaDaTaca = *d.Avaria
		}
	}
	return c
}

func (a *ambiente) qbResultDoGET(t *testing.T, tk string, conferencia uuid.UUID) (qbApuracao, resposta) {
	t.Helper()
	r := a.chamar(t, http.MethodGet, "/inventory/counts/"+conferencia.String(), tk, nil)
	c := qbDado[struct {
		Result *qbApuracao `json:"result"`
	}](t, r, http.StatusOK, "GET da conferência")
	if c.Result == nil {
		t.Fatalf("conferência fechada sem `result`: %s", r.Corpo)
	}
	return *c.Result, r
}

// ─────────────────────────── 5. Imutabilidade da conferência ────────────────

// Em linguagem de negócio: a contagem de março, depois de fechada, é história —
// dela saiu uma pendência que talvez vire cobrança. Ninguém reconta, reanota,
// refecha nem cancela (409 COUNT_CLOSED com o estado), e o relatório dela
// (`result`) continua dizendo exatamente o que o fechamento disse, mesmo
// depois de o prato ser recotado, o padrão da casa mudar, a taça sair do
// cômodo e o quarto e a toalha saírem de linha.
func TestBensQAConferenciaFechadaEhHistoria(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "bens-conferencia", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qbFaxina(t, u)
	tk := u.Token
	c := a.qbContarEFechar(t, f, tk)

	conf := "/inventory/counts/" + c.Conferencia.String()
	linha := fmt.Sprintf("%s/lines/%s", conf, c.LinhaPrato)
	for _, op := range []struct {
		nome, metodo, caminho string
		corpo                 string
	}{
		{"recontar", http.MethodPatch, linha, `{"counted_qty": 12}`},
		{"desfazer a contagem", http.MethodPatch, linha, `{"counted_qty": null}`},
		{"anotar a linha", http.MethodPatch, linha, `{"note": "achei mais 3 no armário"}`},
		{"PUT da nota", http.MethodPut, conf, `{"note": "x"}`},
		{"PATCH da nota", http.MethodPatch, conf, `{"note": "x"}`},
		{"fechar de novo", http.MethodPost, conf + "/close", `{}`},
		{"fechar de novo sem avarias", http.MethodPost, conf + "/close", `{"raise_issues": false}`},
		{"cancelar", http.MethodDelete, conf, ``},
	} {
		t.Run(op.nome, func(t *testing.T) {
			e := qbErro(t, a.qbChamarCru(t, op.metodo, op.caminho, tk, op.corpo), http.StatusConflict, "COUNT_CLOSED", op.nome+" em conferência fechada")
			if e.Details["status"] != "fechada" || e.Details["closed_at"] == nil {
				t.Errorf("details deve trazer status=fechada e closed_at: %+v", e.Details)
			}
		})
	}

	// O catálogo e o padrão da casa mudam depois do fechamento.
	for _, m := range []struct {
		nome, metodo, caminho string
		corpo                 any
		status                int
	}{
		{"recotar o prato", http.MethodPatch, "/inventory/items/" + c.Prato.String(), map[string]any{"replacement_cost_cents": 9999}, 200},
		{"a toalha deixa de ter custo", http.MethodPatch, "/inventory/items/" + c.Toalha.String(), `{"replacement_cost_cents": null}`, 200},
		{"cotar a taça", http.MethodPatch, "/inventory/items/" + c.Taca.String(), map[string]any{"replacement_cost_cents": 3500}, 200},
		{"padrão da casa do prato vai a 8", http.MethodPatch, "/inventory/placements/" + c.ColPrato, map[string]any{"expected_qty": 8}, 200},
		{"a taça sai da cozinha", http.MethodDelete, "/inventory/placements/" + c.ColTaca, nil, 204},
		{"o quarto sai de linha", http.MethodPatch, "/rooms/" + c.Quarto.String(), map[string]any{"active": false}, 200},
		{"a toalha sai de linha", http.MethodPatch, "/inventory/items/" + c.Toalha.String(), map[string]any{"active": false}, 200},
		{"a avaria do prato é resolvida", http.MethodPatch, "/inventory/issues/" + c.AvariaDoPrato.String(), map[string]any{"resolution": "cobrado", "qty": 1}, 200},
	} {
		var r resposta
		if s, ok := m.corpo.(string); ok {
			r = a.qbChamarCru(t, m.metodo, m.caminho, tk, s)
		} else {
			r = a.chamar(t, m.metodo, m.caminho, tk, m.corpo)
		}
		exigirStatusQB(t, r, m.status, m.nome)
	}

	depois, r := a.qbResultDoGET(t, tk, c.Conferencia)
	if got, want := qbCanonico(t, depois), qbCanonico(t, c.Fechamento); got != want {
		t.Fatalf("o `result` da conferência fechada mudou depois de mexer no catálogo:\n /close %s\n GET    %s", want, got)
	}
	// As linhas guardam o que congelaram: quantidade na abertura, custo no fechamento.
	conferencia := qbDado[qbConferencia](t, r, http.StatusOK, "GET")
	for _, l := range []struct {
		bem      uuid.UUID
		esperada int
		custo    *int64
	}{{c.Prato, 12, ptrInt64(1890)}, {c.Taca, 6, nil}, {c.Toalha, 4, ptrInt64(4500)}} {
		got := conferencia.linhaDo(t, l.bem)
		if got.Esperada != l.esperada || (got.Custo == nil) != (l.custo == nil) || (got.Custo != nil && *got.Custo != *l.custo) {
			t.Errorf("linha do bem %s: esperada %d custo %v; congelado era %d e %v", l.bem, got.Esperada, got.Custo, l.esperada, l.custo)
		}
	}
	if conferencia.Status != "fechada" {
		t.Fatalf("status = %s", conferencia.Status)
	}
}

// Em linguagem de negócio: quem desiste da contagem CANCELA, e a cancelada é
// tão história quanto a fechada — não aceita contagem, nota, fechamento nem
// novo cancelamento. Não tem `result` (ninguém terminou de conferir nada), e o
// que foi contado até ali fica gravado.
func TestBensQAConferenciaCanceladaEhHistoria(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "bens-cancelada", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qbFaxina(t, u)
	tk := u.Token
	unidade, _ := f.qbUnidade(t, a.qbPropriedadePadrao(t), "")
	sala := a.qbComodo(t, tk, unidade, "Sala", "sala", 1)
	vaso := a.qbBem(t, f, tk, "Vaso "+qbSufixo(), ptrInt64(5000))
	almofada := a.qbBem(t, f, tk, "Almofada "+qbSufixo(), nil)
	a.qbColocar(t, tk, sala, vaso, 1)
	a.qbColocar(t, tk, sala, almofada, 4)

	c := a.qbAbrir(t, tk, unidade)
	exigirStatusQB(t, a.qbContar(t, tk, c.ID, c.linhaDo(t, vaso).ID, 0), http.StatusOK, "contando o vaso em zero")
	exigirStatusQB(t, a.chamar(t, http.MethodDelete, "/inventory/counts/"+c.ID.String(), tk, nil), http.StatusNoContent, "cancelando")

	conf := "/inventory/counts/" + c.ID.String()
	pendente := fmt.Sprintf("%s/lines/%s", conf, c.linhaDo(t, almofada).ID)
	contada := fmt.Sprintf("%s/lines/%s", conf, c.linhaDo(t, vaso).ID)
	for _, op := range []struct {
		nome, metodo, caminho, corpo string
	}{
		{"contar a linha pendente", http.MethodPatch, pendente, `{"counted_qty": 4}`},
		{"recontar a linha contada", http.MethodPatch, contada, `{"counted_qty": 1}`},
		{"desfazer a contagem", http.MethodPatch, contada, `{"counted_qty": null}`},
		{"anotar a linha", http.MethodPatch, contada, `{"note": "x"}`},
		{"PUT da nota", http.MethodPut, conf, `{"note": "x"}`},
		{"PATCH da nota", http.MethodPatch, conf, `{"note": null}`},
		{"fechar", http.MethodPost, conf + "/close", `{}`},
		{"cancelar de novo", http.MethodDelete, conf, ``},
	} {
		t.Run(op.nome, func(t *testing.T) {
			e := qbErro(t, a.qbChamarCru(t, op.metodo, op.caminho, tk, op.corpo), http.StatusConflict, "COUNT_CLOSED", op.nome+" em conferência cancelada")
			if e.Details["status"] != "cancelada" || e.Details["closed_at"] == nil {
				t.Errorf("details deve trazer status=cancelada e closed_at: %+v", e.Details)
			}
		})
	}

	lida := qbDado[qbConferencia](t, a.chamar(t, http.MethodGet, conf, tk, nil), http.StatusOK, "GET da cancelada")
	if lida.Status != "cancelada" || lida.Fechada == nil {
		t.Fatalf("cancelada com instante: %+v", lida)
	}
	if string(lida.Result) != "" && string(lida.Result) != "null" {
		t.Fatalf("cancelada não tem result: %s", lida.Result)
	}
	if l := lida.linhaDo(t, vaso); l.Contada == nil || *l.Contada != 0 {
		t.Fatalf("o zero contado antes do cancelamento tem de ficar gravado: %+v", l)
	}
	if l := lida.linhaDo(t, almofada); l.Contada != nil {
		t.Fatalf("a linha pendente continua pendente: %+v", l)
	}
}

// Regressão do defeito MÉDIO achado pelo QA em 07/10/2026: as avarias
// "nascidas no fechamento" são reconhecidas por `count_id` + `reported_at =
// closed_at`. Em linguagem de negócio: a conferência de março fechou com
// "faltam 3 pratos, avaria aberta, 1 pendência gerada". Apagar essa avaria
// (`DELETE /inventory/issues/{id}`, o botão de "registro digitado errado") fazia
// o relatório DAQUELA conferência fechada passar a dizer que ela não gerou
// pendência nenhuma — e o vínculo entre a falta apurada e a cobrança sumia do
// histórico. Respondia 204.
//
// O contrato agora fecha a porta: avaria ligada a conferência `fechada` não se
// apaga — `409 RESOURCE_IN_USE` com `details.count_id`, e o caminho é encerrar
// com `resolution: descartado`. O `result` continua "o que o POST /close
// apurou, lido de volta".
//
// Alcance que o teste cobre: o perfil `usuario` do SEED — o de quem opera a
// casa — tem `inventory.goods:excluir`, então é ele que precisa levar o 409.
// Quem só tem ver/criar/editar leva 403 antes de chegar à regra.
func TestBensQAAvariaDeConferenciaFechadaNaoSeApaga(t *testing.T) {
	a := subirAPI(t)
	usuario := a.criarUsuario(t, "bens-operador", a.qbPerfilDoSeed(t, "usuario"))
	semExcluir := a.criarUsuario(t, "bens-sem-excluir", a.criarPerfil(t, "bens_sem_excluir", []auth.Permissao{
		{Resource: "inventory.goods", Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: "inventory.goods", Action: auth.AcaoCriar, Scope: auth.EscopoAll},
		{Resource: "inventory.goods", Action: auth.AcaoEditar, Scope: auth.EscopoAll},
	}))
	f := a.qbFaxina(t, usuario, semExcluir)
	c := a.qbContarEFechar(t, f, usuario.Token)

	t.Run("sem inventory.goods:excluir não alcança", func(t *testing.T) {
		e := qbErro(t, a.chamar(t, http.MethodDelete, "/inventory/issues/"+c.AvariaDoPrato.String(), semExcluir.Token, nil),
			http.StatusForbidden, "FORBIDDEN", "apagar avaria sem excluir")
		if e.Details["action"] != "excluir" {
			t.Errorf("o 403 deveria apontar a ação excluir: %+v", e.Details)
		}
		depois, _ := a.qbResultDoGET(t, usuario.Token, c.Conferencia)
		if qbCanonico(t, depois) != qbCanonico(t, c.Fechamento) {
			t.Fatal("o result mudou mesmo com o DELETE recusado")
		}
	})

	t.Run("perfil usuario do seed leva 409 e o result da conferência fechada não muda", func(t *testing.T) {
		e := qbErro(t, a.chamar(t, http.MethodDelete, "/inventory/issues/"+c.AvariaDoPrato.String(), usuario.Token, nil),
			http.StatusConflict, "RESOURCE_IN_USE", "apagar avaria de conferência fechada")
		if e.Details["count_id"] != c.Conferencia.String() {
			t.Errorf("o 409 deveria apontar a conferência que segura a avaria (%s): %+v", c.Conferencia, e.Details)
		}
		depois, _ := a.qbResultDoGET(t, usuario.Token, c.Conferencia)
		if got, want := qbCanonico(t, depois), qbCanonico(t, c.Fechamento); got != want {
			t.Fatalf("o relatório da conferência FECHADA mudou depois da tentativa de apagar a avaria:\n"+
				" /close %s\n GET    %s\n(o contrato diz que result é o que o /close apurou, lido de volta)", want, got)
		}
	})
}
