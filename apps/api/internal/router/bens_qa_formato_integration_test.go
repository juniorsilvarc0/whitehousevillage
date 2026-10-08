//go:build integration

package router

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ─────────────────────────── 6. Formato do contrato ─────────────────────────

// Em linguagem de negócio: o painel e o celular leem toda lista do mesmo jeito
// — `{data, meta{page, per_page, total, total_pages}}` — e a paginação não pode
// mentir. Uma tela que pede 1000 por página recebe no máximo 100 (o contrato
// fixa o teto), e o `total_pages` é a conta certa do total.
func TestBensQAListasNoEnvelopeDoContrato(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "bens-listas", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qbFaxina(t, u)
	tk := u.Token
	prop := a.qbPropriedadePadrao(t)
	unidade, _ := f.qbUnidade(t, prop, "")
	outra, _ := f.qbUnidade(t, prop, "")
	var comodos []uuid.UUID
	for i, nome := range []string{"Sala", "Cozinha", "Quarto"} {
		comodos = append(comodos, a.qbComodo(t, tk, unidade, nome, "sala", i))
	}
	bem := a.qbBem(t, f, tk, "Copo "+qbSufixo(), nil)
	for _, c := range comodos {
		a.qbColocar(t, tk, c, bem, 2)
		exigirStatusQB(t, a.chamar(t, http.MethodPost, "/inventory/issues", tk,
			map[string]any{"room_id": c, "item_id": bem, "kind": "quebrado", "qty": 1}), http.StatusCreated, "avaria")
	}
	a.qbAbrir(t, tk, unidade)
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/units/"+outra.String()+"/inventory/copy", tk,
		map[string]any{"source_unit_id": unidade}), http.StatusOK, "cópia")
	a.qbAbrir(t, tk, outra)

	for _, l := range []string{
		"/rooms?unit_id=" + unidade.String(),
		"/inventory/items?room_id=" + comodos[0].String(),
		"/inventory/placements?item_id=" + bem.String(),
		"/inventory/units?q=QA-BENS",
		"/inventory/counts?status=aberta",
		"/inventory/issues?item_id=" + bem.String(),
	} {
		t.Run(l, func(t *testing.T) {
			for _, pp := range []int{1, 2, 1000} {
				r := a.chamar(t, http.MethodGet, l+fmt.Sprintf("&per_page=%d&page=1", pp), tk, nil)
				exigirStatusQB(t, r, http.StatusOK, l)
				var topo map[string]json.RawMessage
				if err := json.Unmarshal(r.Corpo, &topo); err != nil {
					t.Fatal(err)
				}
				if len(topo) != 2 || topo["data"] == nil || topo["meta"] == nil {
					t.Fatalf("lista fora de {data, meta}: %s", r.Corpo)
				}
				if !strings.HasPrefix(string(topo["data"]), "[") {
					t.Fatalf("data não é array: %s", topo["data"])
				}
				var meta map[string]any
				if err := json.Unmarshal(topo["meta"], &meta); err != nil {
					t.Fatal(err)
				}
				for _, k := range []string{"page", "per_page", "total", "total_pages"} {
					if _, ok := meta[k].(float64); !ok {
						t.Errorf("meta.%s ausente ou não numérico: %s", k, topo["meta"])
					}
				}
				if len(meta) != 4 {
					t.Errorf("meta com campos além de page, per_page, total, total_pages: %s", topo["meta"])
				}
				_, m := qbLista[json.RawMessage](t, r, l)
				if m.PerPage > 100 {
					t.Errorf("per_page=%d pedido, %d devolvido: o contrato fixa máximo 100", pp, m.PerPage)
				}
				if want := int((m.Total + int64(m.PerPage) - 1) / int64(m.PerPage)); m.TotalPages != want {
					t.Errorf("total_pages = %d, com total %d e per_page %d deveria ser %d", m.TotalPages, m.Total, m.PerPage, want)
				}
				if m.Page != 1 {
					t.Errorf("page = %d, pedido 1", m.Page)
				}
			}
		})
	}
}

// Em linguagem de negócio: o front reage ao `code`, nunca ao texto. Cada erro
// das rotas de bens sai em `{error{code, message, details}}`, com `code` do
// vocabulário fechado do contrato, e com os `details` que o contrato promete
// para levar a pessoa à tela certa.
func TestBensQAErrosNoEnvelopeDoContrato(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "bens-erros", a.qbPerfilDoSeed(t, "usuario"))
	corretor := a.criarUsuario(t, "bens-erros-corretor", a.qbPerfilDoSeed(t, "corretor"))
	f := a.qbFaxina(t, u, corretor)
	tk := u.Token
	unidade, _ := f.qbUnidade(t, a.qbPropriedadePadrao(t), "")
	cozinha := a.qbComodo(t, tk, unidade, "Cozinha", "cozinha", 1)
	quarto := a.qbComodo(t, tk, unidade, "Quarto", "quarto", 2)
	prato := a.qbBem(t, f, tk, "Prato "+qbSufixo(), nil)
	toalha := a.qbBem(t, f, tk, "Toalha "+qbSufixo(), nil)
	a.qbColocar(t, tk, cozinha, prato, 12)
	a.qbColocar(t, tk, quarto, toalha, 4)
	conf := a.qbAbrir(t, tk, unidade)
	exigirStatusQB(t, a.qbContar(t, tk, conf.ID, conf.linhaDo(t, prato).ID, 12), http.StatusOK, "contando")

	qbErro(t, a.chamar(t, http.MethodGet, "/rooms", "", nil), http.StatusUnauthorized, "UNAUTHORIZED", "401")
	qbErro(t, a.chamar(t, http.MethodGet, "/rooms", corretor.Token, nil), http.StatusForbidden, "FORBIDDEN", "403")
	qbErro(t, a.chamar(t, http.MethodGet, "/rooms/"+uuid.NewString(), tk, nil), http.StatusNotFound, "NOT_FOUND", "404 cômodo")
	qbErro(t, a.chamar(t, http.MethodGet, "/inventory/placements/"+uuid.NewString()+"_"+uuid.NewString(), tk, nil), http.StatusNotFound, "NOT_FOUND", "404 colocação")
	qbEnvelopeDeErro(t, a.chamar(t, http.MethodGet, "/rooms/nao-e-uuid", tk, nil), "", "id malformado")
	qbEnvelopeDeErro(t, a.chamar(t, http.MethodGet, "/inventory/placements/sem-separador", tk, nil), "", "chave de colocação malformada")
	qbErro(t, a.chamar(t, http.MethodPost, "/rooms", tk, map[string]any{"unit_id": unidade, "name": "Cozinha", "kind": "cozinha"}),
		http.StatusConflict, "CODE_IN_USE", "cômodo repetido")
	e := qbErro(t, a.chamar(t, http.MethodDelete, "/inventory/items/"+prato.String(), tk, nil), http.StatusConflict, "RESOURCE_IN_USE", "apagar bem colocado")
	if e.Details["placements"] == nil {
		t.Errorf("RESOURCE_IN_USE do bem tem de dizer o que segura (details.placements): %+v", e.Details)
	}
	e = qbErro(t, a.chamar(t, http.MethodDelete, "/rooms/"+cozinha.String(), tk, nil), http.StatusConflict, "RESOURCE_IN_USE", "apagar cômodo em conferência")
	if e.Details["count_lines"] == nil {
		t.Errorf("RESOURCE_IN_USE do cômodo tem de dizer o que segura (details.count_lines): %+v", e.Details)
	}
	e = qbErro(t, a.chamar(t, http.MethodPost, "/inventory/counts", tk, map[string]any{"unit_id": unidade}), http.StatusConflict, "COUNT_ALREADY_OPEN", "segunda abertura")
	if e.Details["count_id"] != conf.ID.String() || e.Details["opened_at"] == nil {
		t.Errorf("COUNT_ALREADY_OPEN tem de levar à aberta (count_id, opened_at): %+v", e.Details)
	}
	e = qbErro(t, a.chamar(t, http.MethodPost, "/inventory/counts/"+conf.ID.String()+"/close", tk, nil), http.StatusConflict, "COUNT_HAS_PENDING_LINES", "fechar com pendência")
	if p, ok := e.Details["pending"].(float64); !ok || p != 1 {
		t.Errorf("details.pending tem de ser o total inteiro (1): %+v", e.Details)
	}
	porComodo, ok := e.Details["pending_by_room"].([]any)
	if !ok || len(porComodo) != 1 {
		t.Fatalf("details.pending_by_room tem de ser lista só com os cômodos pendentes: %+v", e.Details)
	}
	item, _ := porComodo[0].(map[string]any)
	if item["room_id"] != quarto.String() || item["room_name"] != "Quarto" || item["pending"] != float64(1) {
		t.Errorf("pending_by_room[0] = %+v, esperado {room_id do Quarto, room_name Quarto, pending 1}", item)
	}
	qbErro(t, a.chamar(t, http.MethodGet, "/rooms?kind=Dormitorio", tk, nil), http.StatusUnprocessableEntity, "VALIDATION_ERROR", "filtro fora do vocabulário")
	qbErro(t, a.chamar(t, http.MethodGet, "/inventory/counts?from=07/10/2026", tk, nil), http.StatusUnprocessableEntity, "VALIDATION_ERROR", "data fora do formato")

	// Campo obrigatório com null, número acima do inteiro do banco: 422, nunca 500.
	for _, c := range []struct{ metodo, caminho, corpo string }{
		{http.MethodPatch, "/rooms/" + quarto.String(), `{"name": null}`},
		{http.MethodPatch, "/rooms/" + quarto.String(), `{"kind": null}`},
		{http.MethodPatch, "/rooms/" + quarto.String(), `{"active": null}`},
		{http.MethodPatch, "/rooms/" + quarto.String(), `{"sort_order": 3000000000}`},
		{http.MethodPatch, "/inventory/items/" + toalha.String(), `{"name": null}`},
		{http.MethodPatch, "/inventory/items/" + toalha.String(), `{"category": null}`},
		{http.MethodPatch, "/inventory/items/" + toalha.String(), `{"replacement_cost_cents": 99999999999999999999}`},
		{http.MethodPatch, "/inventory/placements/" + quarto.String() + "_" + toalha.String(), `{"expected_qty": null}`},
		{http.MethodPatch, "/inventory/placements/" + quarto.String() + "_" + toalha.String(), `{"expected_qty": 3000000000}`},
		{http.MethodPatch, fmt.Sprintf("/inventory/counts/%s/lines/%s", conf.ID, conf.linhaDo(t, toalha).ID), `{"counted_qty": 3000000000}`},
		{http.MethodPost, "/inventory/issues", fmt.Sprintf(`{"room_id":%q,"item_id":%q,"kind":"quebrado","qty":3000000000}`, quarto, toalha)},
	} {
		qbErro(t, a.qbChamarCru(t, c.metodo, c.caminho, tk, c.corpo), http.StatusUnprocessableEntity, "VALIDATION_ERROR", c.metodo+" "+c.caminho+" "+c.corpo)
	}
}

// Em linguagem de negócio: o que a API cria, ela diz onde ficou. Cada 201 da
// tag Bens traz `Location`, e o endereço abre o que acabou de nascer.
func TestBensQALocationNosSeis201(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "bens-location", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qbFaxina(t, u)
	tk := u.Token
	unidade, _ := f.qbUnidade(t, a.qbPropriedadePadrao(t), "")

	conferir := func(rotulo string, r resposta, esperado string) {
		t.Helper()
		exigirStatusQB(t, r, http.StatusCreated, rotulo)
		loc := r.Headers.Get("Location")
		if loc != esperado {
			t.Errorf("%s: Location = %q, esperado %q", rotulo, loc, esperado)
			return
		}
		if aberto := a.qbBaixar(t, tk, loc); aberto.Status != http.StatusOK {
			t.Errorf("%s: GET %s (o Location) respondeu %d", rotulo, loc, aberto.Status)
		}
	}

	r := a.chamar(t, http.MethodPost, "/rooms", tk, map[string]any{"unit_id": unidade, "name": "Cozinha", "kind": "cozinha"})
	comodo := qbDado[qbIDResp](t, r, http.StatusCreated, "cômodo").ID
	conferir("POST /rooms", r, "/api/v1/rooms/"+comodo.String())

	r = a.chamar(t, http.MethodPost, "/inventory/items", tk, map[string]any{"name": "Prato " + qbSufixo(), "category": "louca"})
	bem := qbDado[qbIDResp](t, r, http.StatusCreated, "bem").ID
	f.item(bem)
	conferir("POST /inventory/items", r, "/api/v1/inventory/items/"+bem.String())

	r = a.chamar(t, http.MethodPost, "/inventory/placements", tk, map[string]any{"room_id": comodo, "item_id": bem, "expected_qty": 6})
	conferir("POST /inventory/placements", r, "/api/v1/inventory/placements/"+comodo.String()+"_"+bem.String())

	r = a.qbEnviarFoto(t, tk, "file", "prato.png", "image/png", qbPNG(t, 20, 20))
	midia := qbDado[qbIDResp](t, r, http.StatusCreated, "foto").ID
	conferir("POST /inventory/media", r, "/api/v1/inventory/media/"+midia.String())

	r = a.chamar(t, http.MethodPost, "/inventory/counts", tk, map[string]any{"unit_id": unidade})
	conf := qbDado[qbIDResp](t, r, http.StatusCreated, "conferência").ID
	conferir("POST /inventory/counts", r, "/api/v1/inventory/counts/"+conf.String())

	r = a.chamar(t, http.MethodPost, "/inventory/issues", tk, map[string]any{"room_id": comodo, "item_id": bem, "kind": "faltando", "qty": 1})
	av := qbDado[qbIDResp](t, r, http.StatusCreated, "avaria").ID
	conferir("POST /inventory/issues", r, "/api/v1/inventory/issues/"+av.String())
}

// Em linguagem de negócio: o gesto do celular é "um número". Corpo sem campo
// nenhum, número negativo, fracionário ou em texto é 422 — e a linha continua
// pendente, porque "não contado" e "contei zero" não podem se confundir. Zero
// é aceito, e é contado.
func TestBensQAContagemDaLinhaRecusaOQueNaoEhUmNumero(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "bens-gesto", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qbFaxina(t, u)
	tk := u.Token
	unidade, _ := f.qbUnidade(t, a.qbPropriedadePadrao(t), "")
	cozinha := a.qbComodo(t, tk, unidade, "Cozinha", "cozinha", 1)
	garfo := a.qbBem(t, f, tk, "Garfo "+qbSufixo(), nil)
	a.qbColocar(t, tk, cozinha, garfo, 12)
	conf := a.qbAbrir(t, tk, unidade)
	linha := fmt.Sprintf("/inventory/counts/%s/lines/%s", conf.ID, conf.linhaDo(t, garfo).ID)

	for _, corpo := range []string{`{}`, `null`, ``, `[]`, `{"counted_qty": -1}`, `{"counted_qty": 1.5}`, `{"counted_qty": "3"}`,
		`{"counted_qty": 3, "counted_by": "` + u.ID.String() + `"}`, `{"counted_qty": 3}{"counted_qty": 4}`} {
		t.Run(corpo, func(t *testing.T) {
			qbErro(t, a.qbChamarCru(t, http.MethodPatch, linha, tk, corpo), http.StatusUnprocessableEntity, "VALIDATION_ERROR", "ContagemDaLinha "+corpo)
		})
	}
	lida := qbDado[qbConferencia](t, a.chamar(t, http.MethodGet, "/inventory/counts/"+conf.ID.String(), tk, nil), http.StatusOK, "relendo")
	if l := lida.linhaDo(t, garfo); l.Contada != nil || l.ContadaEm != nil {
		t.Fatalf("depois de só recusas, a linha tem de continuar pendente: %+v", l)
	}
	r := a.qbChamarCru(t, http.MethodPatch, linha, tk, `{"counted_qty": 0}`)
	contada := qbDado[struct {
		Linha    qbLinha `json:"line"`
		Progress struct {
			Pending int `json:"pending"`
			Counted int `json:"counted"`
		} `json:"progress"`
	}](t, r, http.StatusOK, "contando zero")
	if contada.Linha.Contada == nil || *contada.Linha.Contada != 0 || contada.Linha.ContadaEm == nil || contada.Progress.Pending != 0 || contada.Progress.Counted != 1 {
		t.Fatalf("zero é contado (com instante), não pendente: %s", r.Corpo)
	}
}

// Em linguagem de negócio: o nome errado de um campo não pode virar "salvo" em
// silêncio. A varredura geral (regressao_rodada3) manda campo desconhecido com
// id SORTEADO; aqui vai com alvo REAL e corpo VÁLIDO — inclusive no fechamento,
// que é irreversível — e o banco tem de ficar como estava.
func TestBensQACampoDesconhecidoComAlvoRealNaoGravaNada(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "bens-campo", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qbFaxina(t, u)
	c := a.qbMontarCenario(t, f, u.Token)
	antes := a.qbLerEstado(t, c)

	const intruso = "campo_fora_do_contrato"
	varridas := 0
	for _, op := range qbOperacoesDeBens(t) {
		corpo, ok := c.corpo(op).(map[string]any)
		if !ok {
			continue
		}
		varridas++
		t.Run(op.chave(), func(t *testing.T) {
			com := map[string]any{intruso: true}
			for k, v := range corpo {
				com[k] = v
			}
			r := a.chamar(t, op.Metodo, c.caminho(op), u.Token, com)
			e := qbErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", op.chave()+" com campo desconhecido")
			if e.Details[intruso] == nil {
				t.Errorf("o 422 tem de nomear o campo: %s", r.Corpo)
			}
		})
	}
	if varridas != 19 {
		t.Errorf("esperava 19 operações JSON com corpo no cenário; varri %d", varridas)
	}
	if depois := a.qbLerEstado(t, c); depois != antes {
		t.Fatalf("um corpo com campo desconhecido gravou alguma coisa:\n antes  %+v\n depois %+v", antes, depois)
	}
}

// Em linguagem de negócio: JSON diferencia maiúscula de minúscula, e o
// contrato declara `additionalProperties: false`. `"COUNTED_QTY"` não é
// `counted_qty`: é um campo que o contrato não conhece, e tem de ser 422 como
// qualquer outro. Aceitá-lo como se fosse o campo certo é o mesmo "o cliente
// acha que mandou uma coisa e o servidor decidiu outra" que a varredura de
// campo desconhecido existe para impedir — só que pela porta da caixa.
func TestBensQAChaveComOutraCaixaNaoEhOCampoDoContrato(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "bens-caixa", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qbFaxina(t, u)
	tk := u.Token
	unidade, _ := f.qbUnidade(t, a.qbPropriedadePadrao(t), "")
	sala := a.qbComodo(t, tk, unidade, "Sala", "sala", 1)
	copo := a.qbBem(t, f, tk, "Copo "+qbSufixo(), nil)
	a.qbColocar(t, tk, sala, copo, 6)
	conf := a.qbAbrir(t, tk, unidade)
	linha := conf.linhaDo(t, copo)

	casos := []struct{ nome, metodo, caminho, corpo string }{
		{"COUNTED_QTY na contagem", http.MethodPatch, fmt.Sprintf("/inventory/counts/%s/lines/%s", conf.ID, linha.ID), `{"COUNTED_QTY": 5}`},
		{"Note na conferência", http.MethodPatch, "/inventory/counts/" + conf.ID.String(), `{"Note": "plantão"}`},
		{"Expected_Qty na colocação", http.MethodPatch, "/inventory/placements/" + sala.String() + "_" + copo.String(), `{"Expected_Qty": 1}`},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			r := a.qbChamarCru(t, c.metodo, c.caminho, tk, c.corpo)
			if r.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s %s %s → %d (esperado 422: a chave não existe no contrato) — %s", c.metodo, c.caminho, c.corpo, r.Status, r.Corpo)
			}
		})
	}
	lida := qbDado[qbConferencia](t, a.chamar(t, http.MethodGet, "/inventory/counts/"+conf.ID.String(), tk, nil), http.StatusOK, "relendo")
	if l := lida.linhaDo(t, copo); l.Contada != nil {
		t.Errorf("`COUNTED_QTY` foi gravado como contagem (%d) — a linha deveria continuar pendente", *l.Contada)
	}
}

// Em linguagem de negócio: o gestor abre a planilha no Excel em português e
// ela tem de abrir em colunas, com acento certo e vírgula decimal — e um nome
// de bem digitado como fórmula (`=HYPERLINK(...)`, `+`, `-`, `@`) não pode
// virar fórmula ao abrir. O nome do arquivo diz o recorte e a data da casa.
func TestBensQAExportacaoCSVParaOExcelEmPortugues(t *testing.T) {
	a := subirAPI(t)
	u := a.criarUsuario(t, "bens-csv", a.qbPerfilDoSeed(t, "usuario"))
	f := a.qbFaxina(t, u)
	tk := u.Token
	prop := a.qbPropriedadePadrao(t)
	s := strings.ToUpper(qbSufixo())
	unidade, codigo := f.qbUnidade(t, prop, "QA-BENS-"+s)
	formula, _ := f.qbUnidade(t, prop, "=QA"+s)

	sala := a.qbComodo(t, tk, unidade, "+Sala de estar", "sala", 1)
	varanda := a.qbComodo(t, tk, unidade, "-Varanda; fundos", "varanda", 2)
	quarto := a.qbComodo(t, tk, formula, "@Quarto", "quarto", 1)

	hyperlink := a.qbBem(t, f, tk, `=HYPERLINK("http://exemplo.invalid";"clique")`, ptrInt64(123456))
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/inventory/items/"+hyperlink.String(), tk,
		map[string]any{"description": "-1+2"}), http.StatusOK, "descrição com fórmula")
	centavos := a.qbBem(t, f, tk, `Toalha "felpuda" `+s, ptrInt64(5))
	semCusto := a.qbBem(t, f, tk, "@SOMA(A1:A9)", nil)
	exigirStatusQB(t, a.chamar(t, http.MethodPatch, "/inventory/items/"+semCusto.String(), tk,
		map[string]any{"description": "Pôr na prateleira; não empilhar"}), http.StatusOK, "descrição com ;")
	a.qbColocar(t, tk, sala, hyperlink, 2)
	a.qbColocar(t, tk, varanda, centavos, 10)
	a.qbColocar(t, tk, quarto, semCusto, 1)
	exigirStatusQB(t, a.chamar(t, http.MethodPost, "/inventory/issues", tk,
		map[string]any{"room_id": varanda, "item_id": centavos, "kind": "faltando", "qty": 2}), http.StatusCreated, "avaria aberta")

	var agora time.Time
	if err := a.pool.QueryRow(a.ctx, `SELECT now()`).Scan(&agora); err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("America/Fortaleza")
	if err != nil {
		t.Skipf("sem tzdata: %v", err)
	}
	hoje := agora.In(loc).Format(time.DateOnly)

	ler := func(t *testing.T, r resposta) [][]string {
		t.Helper()
		exigirStatusQB(t, r, http.StatusOK, "exportando")
		if ct := r.Headers.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
			t.Errorf("Content-Type = %q, o contrato diz text/csv", ct)
		}
		corpo := string(r.Corpo)
		if !strings.HasPrefix(corpo, "\xEF\xBB\xBF") {
			t.Fatal("sem BOM UTF-8: o Excel em português abre com acento quebrado")
		}
		leitor := csv.NewReader(strings.NewReader(strings.TrimPrefix(corpo, "\xEF\xBB\xBF")))
		leitor.Comma = ';'
		linhas, err := leitor.ReadAll()
		if err != nil {
			t.Fatalf("o CSV não fecha com `;` como separador: %v\n%s", err, corpo)
		}
		for i, l := range linhas {
			if len(l) != 10 {
				t.Errorf("linha %d com %d colunas (o cabeçalho tem 10): %q", i, len(l), l)
			}
			for j, celula := range l {
				if celula != "" && strings.ContainsRune("=+-@\t\r", rune(celula[0])) {
					t.Errorf("linha %d coluna %d começa com %q e o Excel a executa como fórmula: %q", i, j, celula[0], celula)
				}
			}
		}
		return linhas
	}

	t.Run("recorte por unidade", func(t *testing.T) {
		r := a.chamar(t, http.MethodGet, "/inventory/export?unit_id="+unidade.String(), tk, nil)
		esperado := fmt.Sprintf(`attachment; filename="inventario-%s-%s.csv"`, strings.ToLower(codigo), hoje)
		if cd := r.Headers.Get("Content-Disposition"); cd != esperado {
			t.Errorf("Content-Disposition = %q, esperado %q", cd, esperado)
		}
		linhas := ler(t, r)
		if len(linhas) != 3 {
			t.Fatalf("cabeçalho + 2 colocações; veio %d linhas", len(linhas))
		}
		porBem := map[string][]string{}
		for _, l := range linhas[1:] {
			porBem[strings.TrimPrefix(l[3], "'")] = l
		}
		h := porBem[`=HYPERLINK("http://exemplo.invalid";"clique")`]
		if h == nil {
			t.Fatalf("a linha do bem com fórmula não voltou íntegra (o `;` dentro do nome quebrou a coluna?): %q", linhas)
		}
		if h[1] != "'+Sala de estar" || h[4] != "'-1+2" || h[8] != "1234,56" || h[7] != "2" {
			t.Errorf("linha do bem com fórmula: %q (esperado ambiente e descrição neutralizados e 1234,56)", h)
		}
		toalha := porBem[`Toalha "felpuda" `+s]
		if toalha == nil || toalha[1] != "'-Varanda; fundos" || toalha[8] != "0,05" || toalha[9] != "1" {
			t.Errorf("linha da toalha: %q (esperado ambiente neutralizado, 0,05 e 1 pendência aberta)", toalha)
		}
	})

	t.Run("código da unidade com fórmula", func(t *testing.T) {
		linhas := ler(t, a.chamar(t, http.MethodGet, "/inventory/export?unit_id="+formula.String(), tk, nil))
		if len(linhas) != 2 || linhas[1][0] != "'=QA"+s || linhas[1][1] != "'@Quarto" || linhas[1][3] != "'@SOMA(A1:A9)" ||
			linhas[1][4] != "Pôr na prateleira; não empilhar" || linhas[1][8] != "" {
			t.Errorf("linha da unidade com código-fórmula: %q", linhas)
		}
	})

	t.Run("casa inteira leva o slug da propriedade", func(t *testing.T) {
		r := a.chamar(t, http.MethodGet, "/inventory/export", tk, nil)
		esperado := fmt.Sprintf(`attachment; filename="inventario-white-house-village-%s.csv"`, hoje)
		if cd := r.Headers.Get("Content-Disposition"); cd != esperado {
			t.Errorf("Content-Disposition = %q, esperado %q", cd, esperado)
		}
		ler(t, r)
	})
}
