//go:build integration

package router

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ─────────────────────────── 6. Isolamento entre propriedades ───────────────

// Em linguagem de negócio: a instalação pode ter mais de uma casa, e o banco
// NÃO impede uma ordem da casa A citar a unidade da casa B (db.md §11: "a API
// filtra tudo por property_id"). Então a API é a única barreira. Quem opera a
// casa A não lê, não edita, não conclui, não cancela e não bloqueia a ordem de
// B; não abre ordem na unidade de B nem cita o cômodo, o bem ou a avaria de B;
// não solta o bloqueio de B pelo calendário — e nenhuma resposta para A carrega
// rastro de B.
func TestManutencaoQAIsolamentoEntrePropriedades(t *testing.T) {
	a := subirAPI(t)
	propA := a.qbPropriedadePadrao(t)
	propB := a.qbOutraCasa(t)
	adminSeed := a.qbPerfilDoSeed(t, "admin")
	ua := a.criarUsuario(t, "manut-casa-a", adminSeed)
	ub := a.qbUsuarioEm(t, "manut-casa-b", adminSeed, propB)
	f := a.qbFaxina(t, ua, ub)
	f.propriedades = append(f.propriedades, propB)
	a.qmFaxina(t, f)
	d := a.qmHoje(t, propA)

	// ── Casa B, pela API, com o usuário de B ──
	unidadeB, codigoB := f.qbUnidade(t, propB, "")
	comodoB := a.qbComodo(t, ub.Token, unidadeB, "Cozinha de B", "cozinha", 1)
	nomeDoBemB := "Geladeira de B " + qbSufixo()
	bemB := a.qbBem(t, f, ub.Token, nomeDoBemB, nil)
	a.qbColocar(t, ub.Token, comodoB, bemB, 1)
	avariaB := qbDado[qbIDResp](t, a.chamar(t, http.MethodPost, "/inventory/issues", ub.Token, map[string]any{
		"room_id": comodoB, "item_id": bemB, "kind": "avariado", "qty": 1,
	}), http.StatusCreated, "avaria de B").ID
	tituloB := "Ordem sigilosa de B " + qbSufixo()
	ordemB := a.qmAbrir(t, ub.Token, map[string]any{"unit_id": unidadeB, "issue_id": avariaB, "title": tituloB,
		"block": qmPeriodo(d.AddDays(5), d.AddDays(8))})
	cenarioB := qmCenario{Prop: propB, Unidade: unidadeB, Codigo: codigoB, Ordem: ordemB.ID, D: d}
	antesB := a.qmLerEstado(t, cenarioB)

	// ── Casa A ──
	unidadeA, _ := f.qbUnidade(t, propA, "")
	comodoA := a.qbComodo(t, ua.Token, unidadeA, "Cozinha de A", "cozinha", 1)
	ordemA := a.qmAbrir(t, ua.Token, map[string]any{"unit_id": unidadeA, "room_id": comodoA, "title": "Ordem de A"})

	marcasDeB := []string{tituloB, nomeDoBemB, codigoB, "Cozinha de B", ordemB.ID.String(), unidadeB.String(), comodoB.String(),
		bemB.String(), avariaB.String(), ordemB.Bloqueio.ID.String()}
	semRastroDeB := func(t *testing.T, contexto string, r resposta) {
		t.Helper()
		for _, m := range marcasDeB {
			if strings.Contains(string(r.Corpo), m) {
				t.Errorf("%s: a resposta para a casa A traz %q, que é da casa B — %s", contexto, m, r.Corpo)
			}
		}
	}

	t.Run("as 8 operações sobre a ordem de B respondem 404 para A", func(t *testing.T) {
		for _, op := range qmOperacoesDeManutencao(t) {
			if !strings.Contains(op.Path, "{id}") {
				continue
			}
			r := a.qmExecutar(t, cenarioB, op, ua.Token)
			qbErro(t, r, http.StatusNotFound, "NOT_FOUND", "A em "+op.chave()+" da ordem de B")
			semRastroDeB(t, op.chave(), r)
		}
		if depois := a.qmLerEstado(t, cenarioB); !depois.igual(antesB) {
			t.Fatalf("a casa A mexeu na ordem de B:\n antes  %+v\n depois %+v", antesB, depois)
		}
	})

	t.Run("a lista de A não mostra B, nem filtrando pelos ids de B", func(t *testing.T) {
		r := a.chamar(t, http.MethodGet, "/maintenance-orders?per_page=100", ua.Token, nil)
		qbLista[qmOrdem](t, r, "lista de A")
		semRastroDeB(t, "lista de A", r)
		for _, filtro := range []string{"unit_id=" + unidadeB.String(), "room_id=" + comodoB.String(), "item_id=" + bemB.String(),
			"issue_id=" + avariaB.String(), "q=" + strings.ReplaceAll(tituloB, " ", "+")} {
			r := a.chamar(t, http.MethodGet, "/maintenance-orders?"+filtro, ua.Token, nil)
			itens, m := qbLista[qmOrdem](t, r, filtro)
			if len(itens) != 0 || m.Total != 0 {
				t.Errorf("A filtrando por %s achou %d ordem(ns) de B", filtro, len(itens))
			}
			semRastroDeB(t, filtro, r)
		}
		// E B não vê a de A.
		r = a.chamar(t, http.MethodGet, "/maintenance-orders/"+ordemA.ID.String(), ub.Token, nil)
		qbErro(t, r, http.StatusNotFound, "NOT_FOUND", "B lendo a ordem de A")
	})

	t.Run("A não abre ordem citando unidade, cômodo, bem ou avaria de B", func(t *testing.T) {
		for _, c := range []struct {
			nome, campo string
			corpo       map[string]any
		}{
			{"unidade de B", "unit_id", map[string]any{"unit_id": unidadeB, "title": "Invasão"}},
			{"unidade de B com bloqueio", "unit_id", map[string]any{"unit_id": unidadeB, "title": "Invasão",
				"block": qmPeriodo(d.AddDays(20), d.AddDays(21))}},
			{"cômodo de B", "room_id", map[string]any{"unit_id": unidadeA, "room_id": comodoB, "title": "Invasão"}},
			{"bem de B", "item_id", map[string]any{"unit_id": unidadeA, "item_id": bemB, "title": "Invasão"}},
			{"avaria de B", "issue_id", map[string]any{"unit_id": unidadeA, "issue_id": avariaB, "title": "Invasão"}},
			{"avaria de B na unidade de B", "unit_id", map[string]any{"unit_id": unidadeB, "issue_id": avariaB, "title": "Invasão"}},
		} {
			r := a.chamar(t, http.MethodPost, "/maintenance-orders", ua.Token, c.corpo)
			e := qbErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", c.nome)
			if e.Details[c.campo] == nil {
				t.Errorf("%s: o 422 tem de apontar %s: %s", c.nome, c.campo, r.Corpo)
			}
			semRastroDeB(t, c.nome, r)
		}
		if n := a.qmContar(t, `SELECT count(*) FROM maintenance_orders WHERE unit_id = ANY($1)`, []uuid.UUID{unidadeA, unidadeB}); n != 2 {
			t.Fatalf("as recusas deixaram ordem: %d nas duas unidades (esperado as 2 do cenário)", n)
		}
		if n := a.qmContar(t, `SELECT count(*) FROM stay_blocks WHERE unit_id = $1`, unidadeB); n != 1 {
			t.Fatalf("A bloqueou a unidade de B: %d bloqueio(s) nela", n)
		}
	})

	t.Run("A não pendura cômodo nem bem de B na própria ordem", func(t *testing.T) {
		base := "/maintenance-orders/" + ordemA.ID.String()
		for _, c := range []struct {
			nome, metodo, campo string
			corpo               map[string]any
		}{
			{"PATCH com cômodo de B", http.MethodPatch, "room_id", map[string]any{"room_id": comodoB}},
			{"PATCH com bem de B", http.MethodPatch, "item_id", map[string]any{"item_id": bemB}},
			{"PUT com cômodo de B", http.MethodPut, "room_id", map[string]any{"title": "x", "priority": "normal", "room_id": comodoB}},
			{"PUT com bem de B", http.MethodPut, "item_id", map[string]any{"title": "x", "priority": "normal", "item_id": bemB}},
		} {
			r := a.chamar(t, c.metodo, base, ua.Token, c.corpo)
			e := qbErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", c.nome)
			if e.Details[c.campo] == nil {
				t.Errorf("%s: o 422 tem de apontar %s: %s", c.nome, c.campo, r.Corpo)
			}
			semRastroDeB(t, c.nome, r)
		}
		lida := a.qmLer(t, ua.Token, ordemA.ID)
		if lida.ComodoID == nil || *lida.ComodoID != comodoA || lida.BemID != nil || lida.Titulo != "Ordem de A" {
			t.Fatalf("a ordem de A mudou depois das recusas: %+v", lida)
		}
	})

	t.Run("A não solta pelo calendário o bloqueio da ordem de B", func(t *testing.T) {
		r := a.chamar(t, http.MethodDelete, "/blocks/"+ordemB.Bloqueio.ID.String(), ua.Token, nil)
		qbErro(t, r, http.StatusNotFound, "NOT_FOUND", "A soltando o bloqueio de B")
		semRastroDeB(t, "DELETE /blocks de B", r)
		var status string
		if err := a.pool.QueryRow(a.ctx, `SELECT status FROM stay_blocks WHERE id = $1`, ordemB.Bloqueio.ID).Scan(&status); err != nil || status != "confirmed" {
			t.Fatalf("o bloqueio de B: %s %v", status, err)
		}
	})

	// B, na própria casa, continua operando a ordem normalmente.
	if o := a.qmLer(t, ub.Token, ordemB.ID); o.Titulo != tituloB || o.Status != "aberta" {
		t.Fatalf("B perdeu a própria ordem: %+v", o)
	}
}
