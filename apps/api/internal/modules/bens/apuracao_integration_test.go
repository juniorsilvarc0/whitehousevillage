//go:build integration

package bens_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// O custo de reposição é CONGELADO no fechamento (regra 7): fechar, recotar o
// item no catálogo, e o `GET` continua devolvendo a perda apurada — a mesma que
// o `POST /close` devolveu, lida de volta e não recalculada.
func TestPerdaApuradaSobreviveARecotacao(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	cozinha := a.comodo(t, g, unidade, "Cozinha", "cozinha", 1)
	prato := a.bem(t, g, "Prato", ptr[int64](1890))
	copo := a.bem(t, g, "Copo", nil)
	a.colocar(t, g, cozinha.ID, prato.ID, 12)
	a.colocar(t, g, cozinha.ID, copo.ID, 6)

	conf := a.abrir(t, g, unidade)
	if conf.Resultado != nil {
		t.Fatalf("conferência aberta não tem result: %+v", conf.Resultado)
	}
	for _, l := range conf.linhas() {
		if l.Custo != nil {
			t.Fatalf("o custo só congela no fechamento; aberta veio %d", *l.Custo)
		}
	}
	exigir(t, a.contar(t, g, conf.ID, conf.linhaDo(t, prato.ID).ID, 9), http.StatusOK, "contando o prato")
	exigir(t, a.contar(t, g, conf.ID, conf.linhaDo(t, copo.ID).ID, 6), http.StatusOK, "contando o copo")

	r := a.chamar(t, http.MethodPost, fmt.Sprintf("/inventory/counts/%s/close", conf.ID), g, nil)
	exigir(t, r, http.StatusOK, "fechando")
	fechou := dado[fechamentoResp](t, r)
	if len(fechou.Divergencias) != 1 || fechou.AvariasCriadas != 1 {
		t.Fatalf("uma divergência (o prato) e uma avaria: %+v", fechou)
	}
	d := fechou.Divergencias[0]
	if d.AmbienteID != cozinha.ID || d.Custo == nil || *d.Custo != 1890 ||
		d.PerdaCents == nil || *d.PerdaCents != 3*1890 || d.AvariaID == nil {
		t.Fatalf("divergência do fechamento: %+v", d)
	}

	// Recotação: o prato passa a custar R$ 99,99 no catálogo.
	exigir(t, a.chamar(t, http.MethodPatch, "/inventory/items/"+prato.ID.String(), g,
		map[string]any{"replacement_cost_cents": 9999}), http.StatusOK, "recotando")

	r = a.chamar(t, http.MethodGet, "/inventory/counts/"+conf.ID.String(), g, nil)
	exigir(t, r, http.StatusOK, "relendo a fechada")
	lida := dado[conferenciaResp](t, r)
	if lida.Resultado == nil {
		t.Fatalf("conferência fechada traz result: %s", r.Corpo)
	}
	posterior, _ := json.Marshal(lida.Resultado)
	original, _ := json.Marshal(apuracaoResp{Divergencias: fechou.Divergencias, AvariasCriadas: fechou.AvariasCriadas})
	if string(posterior) != string(original) {
		t.Fatalf("o result lido de volta mudou depois da recotação:\n fechamento %s\n hoje       %s", original, posterior)
	}
	if l := lida.linhaDo(t, prato.ID); l.Custo == nil || *l.Custo != 1890 {
		t.Fatalf("a linha guarda o custo congelado (1890), veio %v", l.Custo)
	}
	if l := lida.linhaDo(t, copo.ID); l.Custo != nil {
		t.Fatalf("bem sem custo cotado no fechamento fica nulo na linha, veio %d", *l.Custo)
	}

	// Avaria relatada à mão citando a conferência DEPOIS do fechamento não é
	// "aberta por este fechamento": não entra em issues_created.
	exigir(t, a.chamar(t, http.MethodPost, "/inventory/issues", g, map[string]any{
		"room_id": cozinha.ID, "item_id": copo.ID, "kind": "quebrado", "qty": 1, "count_id": conf.ID,
	}), http.StatusCreated, "relatando à mão com count_id")
	r = a.chamar(t, http.MethodGet, "/inventory/counts/"+conf.ID.String(), g, nil)
	if res := dado[conferenciaResp](t, r).Resultado; res == nil || res.AvariasCriadas != 1 {
		t.Fatalf("issues_created conta só as nascidas no fechamento: %s", r.Corpo)
	}
}

// Cancelada não tem result nem custo congelado.
func TestCanceladaNaoTemResultNemCustoCongelado(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	sala := a.comodo(t, g, unidade, "Sala", "sala", 1)
	vaso := a.bem(t, g, "Vaso", ptr[int64](5000))
	a.colocar(t, g, sala.ID, vaso.ID, 1)
	conf := a.abrir(t, g, unidade)
	exigir(t, a.contar(t, g, conf.ID, conf.linhaDo(t, vaso.ID).ID, 0), http.StatusOK, "contando")
	exigir(t, a.chamar(t, http.MethodDelete, "/inventory/counts/"+conf.ID.String(), g, nil), http.StatusNoContent, "cancelando")

	r := a.chamar(t, http.MethodGet, "/inventory/counts/"+conf.ID.String(), g, nil)
	cancelada := dado[conferenciaResp](t, r)
	if cancelada.Resultado != nil || cancelada.linhaDo(t, vaso.ID).Custo != nil {
		t.Fatalf("cancelada: result nulo e custo nulo — %s", r.Corpo)
	}
}

// `minProperties: 1` no gesto do celular: só a nota anota sem tocar na
// contagem, e corpo vazio é 422. No fechamento, `note` ausente mantém a
// observação, texto substitui e `null` limpa.
func TestNotaDaLinhaEDoFechamento(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	banheiro := a.comodo(t, g, unidade, "Banheiro", "banheiro", 1)
	toalha := a.bem(t, g, "Toalha", nil)
	a.colocar(t, g, banheiro.ID, toalha.ID, 4)
	conf := a.abrir(t, g, unidade)
	linha := conf.linhaDo(t, toalha.ID)
	caminho := fmt.Sprintf("/inventory/counts/%s/lines/%s", conf.ID, linha.ID)

	exigir(t, a.contar(t, g, conf.ID, linha.ID, 3), http.StatusOK, "contando")
	r := a.chamar(t, http.MethodPatch, caminho, g, map[string]any{"note": "uma manchada"})
	exigir(t, r, http.StatusOK, "só anotando")
	anotada := dado[linhaContadaResp](t, r).Linha
	if anotada.QtdContada == nil || *anotada.QtdContada != 3 || anotada.ContadaEm == nil ||
		anotada.Nota == nil || *anotada.Nota != "uma manchada" {
		t.Fatalf("a nota não pode apagar a contagem: %+v", anotada)
	}
	exigirErro(t, a.chamar(t, http.MethodPatch, caminho, g, map[string]any{}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "corpo vazio")

	exigir(t, a.chamar(t, http.MethodPatch, "/inventory/counts/"+conf.ID.String(), g,
		map[string]any{"note": "plantão da manhã"}), http.StatusOK, "anotando a conferência")
	r = a.chamar(t, http.MethodPost, fmt.Sprintf("/inventory/counts/%s/close", conf.ID), g, map[string]any{"raise_issues": false})
	exigir(t, r, http.StatusOK, "fechando sem note")
	if n := dado[fechamentoResp](t, r).Conferencia.Nota; n == nil || *n != "plantão da manhã" {
		t.Fatalf("note ausente no fechamento mantém a observação: %v", n)
	}

	// Uma segunda conferência para o `null` limpar.
	outra := a.abrir(t, g, unidade)
	exigir(t, a.contar(t, g, outra.ID, outra.linhaDo(t, toalha.ID).ID, 4), http.StatusOK, "contando")
	exigir(t, a.chamar(t, http.MethodPatch, "/inventory/counts/"+outra.ID.String(), g,
		map[string]any{"note": "x"}), http.StatusOK, "anotando")
	r = a.chamar(t, http.MethodPost, fmt.Sprintf("/inventory/counts/%s/close", outra.ID), g, `{"note": null}`)
	exigir(t, r, http.StatusOK, "fechando com note null")
	if n := dado[fechamentoResp](t, r).Conferencia.Nota; n != nil {
		t.Fatalf("note null no fechamento limpa a observação: %q", *n)
	}
}
