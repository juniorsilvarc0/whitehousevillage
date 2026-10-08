//go:build integration

package bens_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

type linhaContadaResp struct {
	Linha     linhaResp     `json:"line"`
	Progresso progressoResp `json:"progress"`
}

type fechamentoResp struct {
	Conferencia    conferenciaResp   `json:"count"`
	Divergencias   []divergenciaResp `json:"divergences"`
	AvariasCriadas int               `json:"issues_created"`
}

// A abertura CONGELA: linha nasce com 12, `room_inventory` vai para 8 depois,
// e a linha continua dizendo 12. Lida por JOIN, a contagem de março passaria a
// esperar o padrão de maio, e a divergência apurada sumiria do relatório.
func TestQuantidadeCongeladaNaoMudaQuandoAColocacaoMuda(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	cozinha := a.comodo(t, g, unidade, "Cozinha", "cozinha", 1)
	prato := a.bem(t, g, "Prato raso", ptr[int64](1890))
	col := a.colocar(t, g, cozinha.ID, prato.ID, 12)

	conf := a.abrir(t, g, unidade)
	if conf.Status != "aberta" || conf.Progresso.Linhas != 1 || conf.Progresso.Pendentes != 1 {
		t.Fatalf("conferência recém-aberta: %+v", conf)
	}
	if conf.AbertaPor == nil || *conf.AbertaPor != "Contadora de Teste" {
		t.Fatalf("opened_by_name deveria ser o nome do usuário: %v", conf.AbertaPor)
	}

	r := a.chamar(t, http.MethodPatch, "/inventory/placements/"+col.ID, g, map[string]any{"expected_qty": 8})
	exigir(t, r, http.StatusOK, "mudando o padrão da casa")
	if dado[colocacaoResp](t, r).QtdEsperada != 8 {
		t.Fatalf("a colocação deveria ter ido para 8: %s", r.Corpo)
	}

	r = a.chamar(t, http.MethodGet, "/inventory/counts/"+conf.ID.String(), g, nil)
	exigir(t, r, http.StatusOK, "relendo a conferência")
	if l := dado[conferenciaResp](t, r).linhaDo(t, prato.ID); l.QtdEsperada != 12 {
		t.Fatalf("a linha congelada mudou para %d: a conferência está lendo a colocação por JOIN", l.QtdEsperada)
	}
}

// Fechar com linha pendente é 409 COUNT_HAS_PENDING_LINES com o que falta,
// por cômodo. Depois de contar tudo, fecha, apura a falta, abre a avaria
// `faltando` com `count_id`, e daí em diante a conferência é história.
func TestFecharComPendenciaRecusaEDepoisApura(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	cozinha := a.comodo(t, g, unidade, "Cozinha", "cozinha", 1)
	quarto := a.comodo(t, g, unidade, "Quarto", "quarto", 2)
	prato := a.bem(t, g, "Prato", ptr[int64](1890))
	taca := a.bem(t, g, "Taça", nil)
	toalha := a.bem(t, g, "Toalha", ptr[int64](4500))
	a.colocar(t, g, cozinha.ID, prato.ID, 12)
	a.colocar(t, g, cozinha.ID, taca.ID, 6)
	a.colocar(t, g, quarto.ID, toalha.ID, 4)

	conf := a.abrir(t, g, unidade)
	if len(conf.Ambientes) != 2 || conf.Ambientes[0].Nome != "Cozinha" || conf.Ambientes[1].Nome != "Quarto" {
		t.Fatalf("os cômodos deveriam vir na ordem de caminhada: %+v", conf.Ambientes)
	}

	// Conta o prato (falta 3) e a taça em ZERO; a toalha fica pendente.
	exigir(t, a.contar(t, g, conf.ID, conf.linhaDo(t, prato.ID).ID, 9), http.StatusOK, "contando o prato")
	r := a.contar(t, g, conf.ID, conf.linhaDo(t, taca.ID).ID, 0)
	exigir(t, r, http.StatusOK, "contando a taça em zero")
	contada := dado[linhaContadaResp](t, r)
	if contada.Linha.QtdContada == nil || *contada.Linha.QtdContada != 0 || contada.Linha.ContadaEm == nil {
		t.Fatalf("zero é contado (com instante), não pendente: %+v", contada.Linha)
	}
	if contada.Progresso.Pendentes != 1 || contada.Progresso.Contadas != 2 || contada.Progresso.Faltas != 9 {
		t.Fatalf("o rodapé deveria ter 1 pendente, 2 contadas e 9 de falta: %+v", contada.Progresso)
	}

	r = a.chamar(t, http.MethodPost, fmt.Sprintf("/inventory/counts/%s/close", conf.ID), g, nil)
	exigirErro(t, r, http.StatusConflict, "COUNT_HAS_PENDING_LINES", "fechando com pendência")
	d := r.detalhes(t)
	if d["pending"] != float64(1) {
		t.Fatalf("details.pending = %v, esperado 1", d["pending"])
	}
	porComodo, _ := d["pending_by_room"].([]any)
	if len(porComodo) != 1 || porComodo[0].(map[string]any)["room_name"] != "Quarto" {
		t.Fatalf("details.pending_by_room deveria apontar o Quarto: %v", d["pending_by_room"])
	}

	// Desfazer: `null` volta a linha a pendente.
	exigir(t, a.contar(t, g, conf.ID, conf.linhaDo(t, toalha.ID).ID, 4), http.StatusOK, "contando a toalha")
	r = a.contar(t, g, conf.ID, conf.linhaDo(t, toalha.ID).ID, nil)
	exigir(t, r, http.StatusOK, "desfazendo a contagem")
	desfeita := dado[linhaContadaResp](t, r).Linha
	if desfeita.QtdContada != nil || desfeita.ContadaEm != nil || desfeita.ContadaPor != nil {
		t.Fatalf("null deveria desfazer contagem, instante e autor: %+v", desfeita)
	}
	exigir(t, a.contar(t, g, conf.ID, conf.linhaDo(t, toalha.ID).ID, 4), http.StatusOK, "recontando a toalha")

	r = a.chamar(t, http.MethodPost, fmt.Sprintf("/inventory/counts/%s/close", conf.ID), g, map[string]any{"note": "fim do plantão"})
	exigir(t, r, http.StatusOK, "fechando")
	res := dado[fechamentoResp](t, r)
	if res.Conferencia.Status != "fechada" || res.Conferencia.Encerrada == nil {
		t.Fatalf("a conferência deveria estar fechada com instante: %+v", res.Conferencia)
	}
	if res.Conferencia.Nota == nil || *res.Conferencia.Nota != "fim do plantão" {
		t.Fatalf("a observação do fechamento deveria ficar gravada: %v", res.Conferencia.Nota)
	}
	if len(res.Divergencias) != 2 || res.AvariasCriadas != 2 {
		t.Fatalf("prato e taça divergem e abrem avaria: %+v", res)
	}
	pratoDiv := res.Divergencias[0]
	if pratoDiv.BemID != prato.ID || pratoDiv.Diferenca != -3 || pratoDiv.PerdaCents == nil || *pratoDiv.PerdaCents != 3*1890 {
		t.Fatalf("o prato perde 3 × R$ 18,90: %+v", pratoDiv)
	}
	if tacaDiv := res.Divergencias[1]; tacaDiv.PerdaCents != nil {
		t.Fatalf("taça sem custo cotado não tem perda (nem zero): %+v", tacaDiv)
	}

	// A avaria nasceu `faltando`, com a quantidade da falta e o vínculo com a
	// conferência.
	r = a.chamar(t, http.MethodGet, "/inventory/issues/"+pratoDiv.AvariaID.String(), g, nil)
	exigir(t, r, http.StatusOK, "lendo a avaria aberta pelo fechamento")
	av := dado[struct {
		Tipo          string     `json:"kind"`
		Qtd           int        `json:"qty"`
		ConferenciaID *uuid.UUID `json:"count_id"`
		Total         *int64     `json:"total_cost_cents"`
		Desfecho      *string    `json:"resolution"`
	}](t, r)
	if av.Tipo != "faltando" || av.Qtd != 3 || av.ConferenciaID == nil || *av.ConferenciaID != conf.ID ||
		av.Total == nil || *av.Total != 5670 || av.Desfecho != nil {
		t.Fatalf("avaria do fechamento: %+v", av)
	}

	// Encerrada é história: contagem, edição, novo fechamento e cancelamento
	// recebem COUNT_CLOSED com o estado.
	r = a.contar(t, g, conf.ID, conf.linhaDo(t, prato.ID).ID, 12)
	exigirErro(t, r, http.StatusConflict, "COUNT_CLOSED", "recontando conferência fechada")
	if r.detalhes(t)["status"] != "fechada" {
		t.Fatalf("details.status deveria ser fechada: %s", r.Corpo)
	}
	exigirErro(t, a.chamar(t, http.MethodPost, fmt.Sprintf("/inventory/counts/%s/close", conf.ID), g, nil),
		http.StatusConflict, "COUNT_CLOSED", "fechando de novo")
	exigirErro(t, a.chamar(t, http.MethodDelete, "/inventory/counts/"+conf.ID.String(), g, nil),
		http.StatusConflict, "COUNT_CLOSED", "cancelando fechada")
	exigirErro(t, a.chamar(t, http.MethodPatch, "/inventory/counts/"+conf.ID.String(), g, map[string]any{"note": "x"}),
		http.StatusConflict, "COUNT_CLOSED", "editando fechada")

	// A unidade está livre para a próxima.
	a.abrir(t, g, unidade)
}

// `raise_issues: false` fecha sem abrir pendência; sobra nunca abre nada.
func TestFecharSemAbrirAvarias(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	sala := a.comodo(t, g, unidade, "Sala", "sala", 1)
	almofada := a.bem(t, g, "Almofada", nil)
	copo := a.bem(t, g, "Copo", nil)
	a.colocar(t, g, sala.ID, almofada.ID, 4)
	a.colocar(t, g, sala.ID, copo.ID, 6)

	conf := a.abrir(t, g, unidade)
	exigir(t, a.contar(t, g, conf.ID, conf.linhaDo(t, almofada.ID).ID, 2), http.StatusOK, "falta")
	exigir(t, a.contar(t, g, conf.ID, conf.linhaDo(t, copo.ID).ID, 8), http.StatusOK, "sobra")

	r := a.chamar(t, http.MethodPost, fmt.Sprintf("/inventory/counts/%s/close", conf.ID), g, map[string]any{"raise_issues": false})
	exigir(t, r, http.StatusOK, "fechando sem avaria")
	res := dado[struct {
		Divergencias []any `json:"divergences"`
		Criadas      int   `json:"issues_created"`
	}](t, r)
	if len(res.Divergencias) != 2 || res.Criadas != 0 {
		t.Fatalf("2 divergências, 0 avarias: %+v", res)
	}
}

// Cancelar não apaga: status `cancelada` com instante, as linhas contadas
// ficam, e a unidade é liberada.
func TestCancelarLiberaAUnidadeEGuardaOQueFoiContado(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	banheiro := a.comodo(t, g, unidade, "Banheiro", "banheiro", 1)
	toalha := a.bem(t, g, "Toalha", nil)
	a.colocar(t, g, banheiro.ID, toalha.ID, 4)

	conf := a.abrir(t, g, unidade)
	exigir(t, a.contar(t, g, conf.ID, conf.linhaDo(t, toalha.ID).ID, 3), http.StatusOK, "contando")
	exigir(t, a.chamar(t, http.MethodDelete, "/inventory/counts/"+conf.ID.String(), g, nil), http.StatusNoContent, "cancelando")

	r := a.chamar(t, http.MethodGet, "/inventory/counts/"+conf.ID.String(), g, nil)
	exigir(t, r, http.StatusOK, "relendo a cancelada")
	cancelada := dado[conferenciaResp](t, r)
	if cancelada.Status != "cancelada" || cancelada.Encerrada == nil || cancelada.Progresso.Contadas != 1 {
		t.Fatalf("cancelada guarda o contado e o instante: %+v", cancelada)
	}
	a.abrir(t, g, unidade)
}

// Unidade sem nenhum bem colocado em ambiente ativo: conferência de nada é
// 422, e o cabeçalho não sobra.
func TestAbrirSemColocacaoEh422(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	quarto := a.comodo(t, g, unidade, "Quarto", "quarto", 1)
	lencol := a.bem(t, g, "Lençol", nil)
	a.colocar(t, g, quarto.ID, lencol.ID, 2)
	// O único cômodo sai de linha: não há o que contar.
	exigir(t, a.chamar(t, http.MethodPatch, "/rooms/"+quarto.ID.String(), g, map[string]any{"active": false}),
		http.StatusOK, "desativando o cômodo")

	r := a.chamar(t, http.MethodPost, "/inventory/counts", g, map[string]any{"unit_id": unidade})
	exigirErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "abrindo sem nada a contar")
	var n int
	if err := a.pool.QueryRow(a.ctx, `SELECT count(*) FROM inventory_counts WHERE unit_id = $1`, unidade).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("o cabeçalho da conferência vazia sobrou no banco (%d linhas)", n)
	}

	// Unidade de outra casa também é 422.
	outra, _ := a.unidadeEm(t, a.outraPropriedade(t))
	exigirErro(t, a.chamar(t, http.MethodPost, "/inventory/counts", g, map[string]any{"unit_id": outra}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "abrindo em unidade de outra casa")
}

// `counted_at` e `counted_by` são do servidor: mandá-los é campo desconhecido.
func TestContagemNaoAceitaInstanteNemAutorDoAparelho(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	cozinha := a.comodo(t, g, unidade, "Cozinha", "cozinha", 1)
	garfo := a.bem(t, g, "Garfo", nil)
	a.colocar(t, g, cozinha.ID, garfo.ID, 12)
	conf := a.abrir(t, g, unidade)
	linha := conf.linhaDo(t, garfo.ID)

	r := a.chamar(t, http.MethodPatch, fmt.Sprintf("/inventory/counts/%s/lines/%s", conf.ID, linha.ID), g,
		`{"counted_qty": 12, "counted_at": "2020-01-01T00:00:00Z"}`)
	exigirErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "mandando counted_at")
	if r.detalhes(t)["counted_at"] == nil {
		t.Fatalf("o 422 deveria nomear counted_at: %s", r.Corpo)
	}

	// Linha de OUTRA conferência é 404.
	outraUnidade, _ := a.unidade(t)
	outroCom := a.comodo(t, g, outraUnidade, "Cozinha", "cozinha", 1)
	a.colocar(t, g, outroCom.ID, garfo.ID, 1)
	outra := a.abrir(t, g, outraUnidade)
	exigirErro(t, a.contar(t, g, conf.ID, outra.linhaDo(t, garfo.ID).ID, 1),
		http.StatusNotFound, "NOT_FOUND", "linha de outra conferência")
}
