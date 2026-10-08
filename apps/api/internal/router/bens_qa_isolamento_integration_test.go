//go:build integration

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// qbCasaB é a segunda propriedade, montada pela API por um usuário DELA.
type qbCasaB struct {
	Prop        uuid.UUID
	Usuario     usuarioDeTeste
	Unidade     uuid.UUID
	Codigo      string
	Comodo      uuid.UUID
	Bem         uuid.UUID
	NomeDoBem   string
	Colocacao   string
	Midia       uuid.UUID
	Conferencia uuid.UUID
	Linha       uuid.UUID
	Hospede     qbHospede
}

// ─────────────────────────── 3. Isolamento entre propriedades ───────────────

// Em linguagem de negócio: a instalação pode ter mais de uma casa, e
// `room_inventory` e `inventory_count_lines` não têm `property_id` — a API é a
// única barreira. Quem opera a casa A não coloca o prato de B na cozinha de A,
// não abre avaria cruzando as duas (nem pela reserva nem pela conferência de
// B), não vê a foto do interior de B e não copia a planta de B para A. E nada
// disso pode deixar rastro no banco.
func TestBensQAIsolamentoEntrePropriedades(t *testing.T) {
	a := subirAPI(t)
	propA := a.qbPropriedadePadrao(t)
	propB := a.qbOutraCasa(t)
	adminSeed := a.qbPerfilDoSeed(t, "admin")
	ua := a.criarUsuario(t, "bens-casa-a", adminSeed)
	ub := a.qbUsuarioEm(t, "casa-b", adminSeed, propB)
	f := a.qbFaxina(t, ua, ub)
	f.propriedades = append(f.propriedades, propB)

	// ── Casa B, pela API, com o usuário de B ──
	b := qbCasaB{Prop: propB, Usuario: ub}
	b.Unidade, b.Codigo = f.qbUnidade(t, propB, "")
	b.Comodo = a.qbComodo(t, ub.Token, b.Unidade, "Cozinha de B", "cozinha", 1)
	b.NomeDoBem = "Prato de B " + qbSufixo()
	b.Bem = a.qbBem(t, f, ub.Token, b.NomeDoBem, ptrInt64(5000))
	b.Colocacao = a.qbColocar(t, ub.Token, b.Comodo, b.Bem, 6)
	r := a.qbEnviarFoto(t, ub.Token, "file", "cofre-de-b.png", "image/png", qbPNG(t, 32, 32))
	b.Midia = qbDado[qbIDResp](t, r, http.StatusCreated, "foto de B").ID
	exigirStatusQB(t, a.chamar(t, http.MethodPut, "/inventory/items/"+b.Bem.String()+"/photos", ub.Token,
		map[string]any{"media_ids": []uuid.UUID{b.Midia}}), http.StatusOK, "galeria de B")
	confB := a.qbAbrir(t, ub.Token, b.Unidade)
	b.Conferencia, b.Linha = confB.ID, confB.linhaDo(t, b.Bem).ID
	b.Hospede = f.qbHospedeComEstadia(t, propB)

	// ── Casa A, pela API, com o usuário de A ──
	unidadeA, _ := f.qbUnidade(t, propA, "")
	destinoA, _ := f.qbUnidade(t, propA, "")
	comodoA := a.qbComodo(t, ua.Token, unidadeA, "Cozinha de A", "cozinha", 1)
	bemA := a.qbBem(t, f, ua.Token, "Prato de A "+qbSufixo(), ptrInt64(1890))
	a.qbColocar(t, ua.Token, comodoA, bemA, 12)
	r = a.chamar(t, http.MethodPost, "/inventory/issues", ua.Token, map[string]any{
		"room_id": comodoA, "item_id": bemA, "kind": "quebrado", "qty": 1,
	})
	avariaA := qbDado[qbIDResp](t, r, http.StatusCreated, "avaria de A").ID
	r = a.qbEnviarFoto(t, ua.Token, "file", "prato-de-a.png", "image/png", qbPNG(t, 16, 16))
	midiaA := qbDado[qbIDResp](t, r, http.StatusCreated, "foto de A").ID

	marcasDeB := []string{b.NomeDoBem, "Cozinha de B", b.Codigo, b.Comodo.String(), b.Bem.String(), b.Midia.String(),
		b.Conferencia.String(), b.Hospede.Codigo, b.Hospede.ReservaID.String()}
	semRastroDeB := func(t *testing.T, contexto string, r resposta) {
		t.Helper()
		for _, m := range marcasDeB {
			if strings.Contains(string(r.Corpo), m) {
				t.Errorf("%s: a resposta para a casa A traz %q, que é da casa B — %s", contexto, m, r.Corpo)
			}
		}
	}

	t.Run("colocar o item de A no cômodo de B e o de B no de A", func(t *testing.T) {
		r := a.chamar(t, http.MethodPost, "/inventory/placements", ua.Token, map[string]any{"room_id": b.Comodo, "item_id": bemA, "expected_qty": 1})
		if e := qbErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "item de A no cômodo de B"); e.Details["room_id"] == nil {
			t.Errorf("o 422 tem de apontar room_id: %s", r.Corpo)
		}
		r = a.chamar(t, http.MethodPost, "/inventory/placements", ua.Token, map[string]any{"room_id": comodoA, "item_id": b.Bem, "expected_qty": 1})
		if e := qbErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "item de B no cômodo de A"); e.Details["item_id"] == nil {
			t.Errorf("o 422 tem de apontar item_id: %s", r.Corpo)
		}
		semRastroDeB(t, "422 da colocação", r)
	})

	t.Run("abrir avaria cruzando as duas casas", func(t *testing.T) {
		casos := []struct {
			nome, campo string
			corpo       map[string]any
		}{
			{"cômodo de B, bem de A", "room_id", map[string]any{"room_id": b.Comodo, "item_id": bemA, "kind": "quebrado", "qty": 1}},
			{"cômodo de A, bem de B", "item_id", map[string]any{"room_id": comodoA, "item_id": b.Bem, "kind": "quebrado", "qty": 1}},
			{"conferência de B", "count_id", map[string]any{"room_id": comodoA, "item_id": bemA, "kind": "faltando", "qty": 1, "count_id": b.Conferencia}},
			{"reserva de B pelo id", "reservation_id", map[string]any{"room_id": comodoA, "item_id": bemA, "kind": "quebrado", "qty": 1, "reservation_id": b.Hospede.ReservaID}},
			{"reserva de B pelo código", "reservation_code", map[string]any{"room_id": comodoA, "item_id": bemA, "kind": "quebrado", "qty": 1, "reservation_code": b.Hospede.Codigo}},
		}
		for _, c := range casos {
			r := a.chamar(t, http.MethodPost, "/inventory/issues", ua.Token, c.corpo)
			e := qbErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", c.nome)
			if e.Details[c.campo] == nil {
				t.Errorf("%s: o 422 tem de apontar %s: %s", c.nome, c.campo, r.Corpo)
			}
			semRastroDeB(t, c.nome, r)
		}
		// Vincular a avaria de A à reserva de B depois de criada também não pode.
		for _, corpo := range []map[string]any{
			{"reservation_id": b.Hospede.ReservaID},
			{"reservation_code": b.Hospede.Codigo},
		} {
			qbErro(t, a.chamar(t, http.MethodPatch, "/inventory/issues/"+avariaA.String(), ua.Token, corpo),
				http.StatusUnprocessableEntity, "VALIDATION_ERROR", fmt.Sprintf("PATCH avaria de A com %v", corpo))
		}
		qbErro(t, a.chamar(t, http.MethodPut, "/inventory/issues/"+avariaA.String(), ua.Token,
			map[string]any{"kind": "quebrado", "qty": 1, "reservation_code": b.Hospede.Codigo}),
			http.StatusUnprocessableEntity, "VALIDATION_ERROR", "PUT avaria de A com reserva de B")

		var cruzadas int
		if err := a.pool.QueryRow(a.ctx, `
			SELECT count(*) FROM inventory_issues ii
			  JOIN unit_rooms r ON r.id = ii.room_id
			  JOIN inventory_items i ON i.id = ii.item_id
			  LEFT JOIN reservations rv ON rv.id = ii.reservation_id
			  LEFT JOIN inventory_counts c ON c.id = ii.count_id
			 WHERE ii.property_id = $1
			   AND (r.property_id <> $1 OR i.property_id <> $1 OR rv.property_id <> $1 OR c.property_id <> $1)`, propA).Scan(&cruzadas); err != nil {
			t.Fatal(err)
		}
		if cruzadas != 0 {
			t.Fatalf("%d avaria(s) da casa A apontam para algo da casa B", cruzadas)
		}
	})

	t.Run("ler a foto de B com a sessão de A", func(t *testing.T) {
		for _, url := range []string{"/api/v1/inventory/media/" + b.Midia.String(), "/api/v1/inventory/media/" + b.Midia.String() + "?size=thumb"} {
			r := a.qbBaixar(t, ua.Token, url)
			qbErro(t, r, http.StatusNotFound, "NOT_FOUND", "GET "+url+" com sessão de A")
			if strings.HasPrefix(r.Headers.Get("Content-Type"), "image/") {
				t.Errorf("GET %s devolveu bytes de imagem para a casa errada", url)
			}
		}
		// E o inverso: B não lê a foto de A.
		qbErro(t, a.qbBaixar(t, ub.Token, "/api/v1/inventory/media/"+midiaA.String()), http.StatusNotFound, "NOT_FOUND", "B lendo foto de A")
		// Nem pendurar a foto de B na galeria de um bem de A.
		r := a.chamar(t, http.MethodPut, "/inventory/items/"+bemA.String()+"/photos", ua.Token, map[string]any{"media_ids": []uuid.UUID{b.Midia}})
		if e := qbErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "foto de B na galeria de A"); e.Details["media_ids[0]"] == nil {
			t.Errorf("o 422 tem de apontar media_ids[0]: %s", r.Corpo)
		}
		// Nem filtrar o catálogo de A pelo cômodo/unidade de B para achar a capa.
		for _, q := range []string{"room_id=" + b.Comodo.String(), "unit_id=" + b.Unidade.String(), "has_photo=true&q=" + url.QueryEscape(b.NomeDoBem)} {
			r := a.chamar(t, http.MethodGet, "/inventory/items?"+q, ua.Token, nil)
			itens, meta := qbLista[qbIDResp](t, r, "GET /inventory/items?"+q)
			if len(itens) != 0 || meta.Total != 0 {
				t.Errorf("GET /inventory/items?%s com sessão de A trouxe %d item(ns)", q, len(itens))
			}
			semRastroDeB(t, "GET /inventory/items?"+q, r)
		}
	})

	t.Run("copiar de uma unidade de B para uma de A", func(t *testing.T) {
		for _, sufixo := range []string{"?dry_run=1", ""} {
			r := a.chamar(t, http.MethodPost, "/units/"+destinoA.String()+"/inventory/copy"+sufixo, ua.Token,
				map[string]any{"source_unit_id": b.Unidade})
			e := qbErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "cópia B → A"+sufixo)
			if e.Details["source_unit_id"] == nil {
				t.Errorf("o 422 tem de explicar em details.source_unit_id: %s", r.Corpo)
			}
			semRastroDeB(t, "cópia B → A"+sufixo, r)
		}
		var comodos int
		if err := a.pool.QueryRow(a.ctx, `SELECT count(*) FROM unit_rooms WHERE unit_id = $1`, destinoA).Scan(&comodos); err != nil {
			t.Fatal(err)
		}
		if comodos != 0 {
			t.Fatalf("a cópia recusada deixou %d cômodo(s) no destino", comodos)
		}
		// E de A para uma unidade de B: o destino nem existe para A.
		qbErro(t, a.chamar(t, http.MethodPost, "/units/"+b.Unidade.String()+"/inventory/copy", ua.Token,
			map[string]any{"source_unit_id": unidadeA}), http.StatusNotFound, "NOT_FOUND", "cópia A → B")
		if err := a.pool.QueryRow(a.ctx, `SELECT count(*) FROM unit_rooms WHERE unit_id = $1`, b.Unidade).Scan(&comodos); err != nil {
			t.Fatal(err)
		}
		if comodos != 1 {
			t.Fatalf("a unidade de B tem %d cômodos; deveria continuar só com a Cozinha de B", comodos)
		}
	})

	t.Run("nada de B é legível nem gravável pela sessão de A", func(t *testing.T) {
		col := b.Colocacao
		lin := fmt.Sprintf("/inventory/counts/%s/lines/%s", b.Conferencia, b.Linha)
		for _, c := range []struct {
			metodo, caminho string
			corpo           any
		}{
			{http.MethodGet, "/rooms/" + b.Comodo.String(), nil},
			{http.MethodPatch, "/rooms/" + b.Comodo.String(), map[string]any{"name": "tomada"}},
			{http.MethodDelete, "/rooms/" + b.Comodo.String(), nil},
			{http.MethodGet, "/inventory/items/" + b.Bem.String(), nil},
			{http.MethodGet, "/inventory/items/" + b.Bem.String() + "/photos", nil},
			{http.MethodPatch, "/inventory/items/" + b.Bem.String(), map[string]any{"name": "tomado"}},
			{http.MethodDelete, "/inventory/items/" + b.Bem.String(), nil},
			{http.MethodGet, "/inventory/placements/" + col, nil},
			{http.MethodPatch, "/inventory/placements/" + col, map[string]any{"expected_qty": 0}},
			{http.MethodDelete, "/inventory/placements/" + col, nil},
			{http.MethodGet, "/units/" + b.Unidade.String() + "/inventory", nil},
			{http.MethodGet, "/inventory/counts/" + b.Conferencia.String(), nil},
			{http.MethodPatch, lin, map[string]any{"counted_qty": 0}},
			{http.MethodPost, "/inventory/counts/" + b.Conferencia.String() + "/close", map[string]any{}},
			{http.MethodDelete, "/inventory/counts/" + b.Conferencia.String(), nil},
			{http.MethodGet, "/inventory/export?unit_id=" + b.Unidade.String(), nil},
			{http.MethodGet, "/inventory/export?room_id=" + b.Comodo.String(), nil},
		} {
			r := a.chamar(t, c.metodo, c.caminho, ua.Token, c.corpo)
			qbErro(t, r, http.StatusNotFound, "NOT_FOUND", c.metodo+" "+c.caminho+" (recurso de B, sessão de A)")
			semRastroDeB(t, c.metodo+" "+c.caminho, r)
		}
		qbErro(t, a.chamar(t, http.MethodPost, "/inventory/counts", ua.Token, map[string]any{"unit_id": b.Unidade}),
			http.StatusUnprocessableEntity, "VALIDATION_ERROR", "abrir conferência na unidade de B")

		for _, q := range []string{
			"/rooms?unit_id=" + b.Unidade.String(),
			"/inventory/placements?item_id=" + b.Bem.String(),
			"/inventory/placements?room_id=" + b.Comodo.String(),
			"/inventory/counts?unit_id=" + b.Unidade.String(),
			"/inventory/issues?count_id=" + b.Conferencia.String(),
			"/inventory/issues?reservation_id=" + b.Hospede.ReservaID.String(),
			"/inventory/units?q=" + b.Codigo,
		} {
			r := a.chamar(t, http.MethodGet, q, ua.Token, nil)
			itens, meta := qbLista[qbIDResp](t, r, "GET "+q)
			if len(itens) != 0 || meta.Total != 0 {
				t.Errorf("GET %s com sessão de A trouxe %d linha(s) de B", q, len(itens))
			}
			semRastroDeB(t, "GET "+q, r)
		}
		// A exportação da casa A inteira não pode carregar uma linha de B.
		r := a.chamar(t, http.MethodGet, "/inventory/export", ua.Token, nil)
		exigirStatusQB(t, r, http.StatusOK, "exportação da casa A")
		semRastroDeB(t, "GET /inventory/export", r)

		// B continua intacta, vista por B.
		var nome string
		var qtd int
		var status string
		if err := a.pool.QueryRow(a.ctx, `
			SELECT r.name, ri.expected_qty, c.status
			  FROM unit_rooms r JOIN room_inventory ri ON ri.room_id = r.id
			  JOIN inventory_counts c ON c.unit_id = r.unit_id
			 WHERE r.id = $1 AND c.id = $2`, b.Comodo, b.Conferencia).Scan(&nome, &qtd, &status); err != nil {
			t.Fatalf("a casa B perdeu dado: %v", err)
		}
		if nome != "Cozinha de B" || qtd != 6 || status != "aberta" {
			t.Fatalf("a casa B foi alterada pela sessão de A: cômodo %q, qtd %d, conferência %s", nome, qtd, status)
		}
	})
}

// ─────────────────────────── 4. Vazamento ───────────────────────────────────

// Em linguagem de negócio: quem conta taça não precisa saber quem dormiu na
// casa. "O que quebrou nesta estadia" se responde pelo CÓDIGO da reserva, e
// nenhuma resposta de bens — lista, detalhe, escrita, fechamento, planilha —
// traz nome, e-mail, telefone ou documento do hóspede, nem e-mail ou telefone
// do operador. Além do texto, nenhuma resposta carrega campo que o contrato
// não declara.
func TestBensQANenhumaRespostaDeBensTrazDadoDeContatoOuDeHospede(t *testing.T) {
	a := subirAPI(t)
	prop := a.qbPropriedadePadrao(t)
	u := a.criarUsuario(t, "bens-vazamento", a.qbPerfilDoSeed(t, "usuario"))
	telefoneDoOperador := a.qbComTelefone(t, u)
	f := a.qbFaxina(t, u)
	h := f.qbHospedeComEstadia(t, prop)

	unidade, _ := f.qbUnidade(t, prop, "")
	destino, _ := f.qbUnidade(t, prop, "")
	comodo := a.qbComodo(t, u.Token, unidade, "Suíte", "quarto", 1)
	bem := a.qbBem(t, f, u.Token, "Abajur QA "+qbSufixo(), ptrInt64(15900))
	col := a.qbColocar(t, u.Token, comodo, bem, 2)
	r := a.qbEnviarFoto(t, u.Token, "file", "abajur.png", "image/png", qbPNG(t, 16, 16))
	midia := qbDado[qbIDResp](t, r, http.StatusCreated, "foto").ID
	exigirStatusQB(t, a.chamar(t, http.MethodPut, "/inventory/items/"+bem.String()+"/photos", u.Token,
		map[string]any{"media_ids": []uuid.UUID{midia}}), http.StatusOK, "galeria")

	type coleta struct {
		rotulo, schema string
		r              resposta
	}
	var respostas []coleta
	guardar := func(rotulo, schema string, r resposta) resposta {
		respostas = append(respostas, coleta{rotulo, schema, r})
		return r
	}

	// A avaria entra pelo CÓDIGO (o que a governanta tem na mão) e pelo id.
	r = guardar("POST /inventory/issues (code)", "Avaria", a.chamar(t, http.MethodPost, "/inventory/issues", u.Token, map[string]any{
		"room_id": comodo, "item_id": bem, "kind": "quebrado", "qty": 1, "reservation_code": h.Codigo, "note": "abajur caiu",
	}))
	avaria := qbDado[struct {
		ID     uuid.UUID `json:"id"`
		Codigo *string   `json:"reservation_code"`
		Res    *string   `json:"reservation_id"`
	}](t, r, http.StatusCreated, "avaria pelo código")
	if avaria.Codigo == nil || *avaria.Codigo != h.Codigo {
		t.Fatalf("a avaria tem de devolver o código da reserva (%s): %s", h.Codigo, r.Corpo)
	}
	guardar("POST /inventory/issues (id)", "Avaria", a.chamar(t, http.MethodPost, "/inventory/issues", u.Token, map[string]any{
		"room_id": comodo, "item_id": bem, "kind": "avariado", "qty": 1, "reservation_id": h.ReservaID,
	}))
	guardar("PATCH /inventory/issues/{id}", "Avaria", a.chamar(t, http.MethodPatch, "/inventory/issues/"+avaria.ID.String(), u.Token, map[string]any{"resolution": "cobrado"}))
	guardar("PUT /inventory/issues/{id}", "Avaria", a.chamar(t, http.MethodPut, "/inventory/issues/"+avaria.ID.String(), u.Token,
		map[string]any{"kind": "quebrado", "qty": 1, "reservation_code": h.Codigo, "resolution": "cobrado"}))

	conf := a.qbAbrir(t, u.Token, unidade)
	guardar("PATCH /inventory/counts/{id}/lines/{lineId}", "LinhaContada", a.qbContar(t, u.Token, conf.ID, conf.linhaDo(t, bem).ID, 1))
	guardar("POST /inventory/counts/{id}/close", "ResultadoDoFechamento", a.chamar(t, http.MethodPost, "/inventory/counts/"+conf.ID.String()+"/close", u.Token, map[string]any{}))
	guardar("POST /units/{id}/inventory/copy", "ResultadoDaCopiaDeInventario", a.chamar(t, http.MethodPost, "/units/"+destino.String()+"/inventory/copy?dry_run=1", u.Token,
		map[string]any{"source_unit_id": unidade}))

	for _, l := range []struct{ rotulo, schema, caminho string }{
		{"GET /rooms", "Ambiente", "/rooms?unit_id=" + unidade.String()},
		{"GET /rooms/{id}", "Ambiente", "/rooms/" + comodo.String()},
		{"GET /inventory/items", "Bem", "/inventory/items?unit_id=" + unidade.String()},
		{"GET /inventory/items/{id}", "BemCompleto", "/inventory/items/" + bem.String()},
		{"GET /inventory/items/{id}/photos", "FotoDoBem", "/inventory/items/" + bem.String() + "/photos"},
		{"GET /inventory/placements", "Colocacao", "/inventory/placements?unit_id=" + unidade.String()},
		{"GET /inventory/placements/{id}", "Colocacao", "/inventory/placements/" + col},
		{"GET /inventory/units", "UnidadeDoInventario", "/inventory/units"},
		{"GET /units/{id}/inventory", "InventarioDaUnidade", "/units/" + unidade.String() + "/inventory?include_inactive=true"},
		{"GET /inventory/counts", "Conferencia", "/inventory/counts?unit_id=" + unidade.String()},
		{"GET /inventory/counts/{id}", "ConferenciaCompleta", "/inventory/counts/" + conf.ID.String()},
		{"GET /inventory/issues", "Avaria", "/inventory/issues?unit_id=" + unidade.String()},
		{"GET /inventory/issues?reservation_id", "Avaria", "/inventory/issues?reservation_id=" + h.ReservaID.String()},
		{"GET /inventory/issues/{id}", "Avaria", "/inventory/issues/" + avaria.ID.String()},
	} {
		guardar(l.rotulo, l.schema, a.chamar(t, http.MethodGet, l.caminho, u.Token, nil))
	}
	csv := a.chamar(t, http.MethodGet, "/inventory/export?unit_id="+unidade.String(), u.Token, nil)
	exigirStatusQB(t, csv, http.StatusOK, "exportação")

	proibidos := map[string]string{
		"nome do hóspede":            h.Nome,
		"e-mail do hóspede":          h.Email,
		"telefone do hóspede":        h.Telefone,
		"documento do hóspede":       h.Documento,
		"e-mail do operador":         u.Email,
		"telefone do operador":       telefoneDoOperador,
		"id do contato":              h.ContatoID.String(),
		"telefone sem o +":           strings.TrimPrefix(h.Telefone, "+"),
		"telefone do operador sem +": strings.TrimPrefix(telefoneDoOperador, "+"),
	}
	chavesProibidas := []string{"email", "phone", "phone_e164", "doc_number", "doc_type", "document", "contact",
		"contact_id", "contact_name", "guest", "guests", "guest_name", "guests_count", "birth_date", "check_in", "check_out",
		"total_cents", "price", "amount_cents"}

	for _, c := range respostas {
		t.Run(c.rotulo, func(t *testing.T) {
			if c.r.Status >= 300 {
				t.Fatalf("status %d — %s", c.r.Status, c.r.Corpo)
			}
			for rotulo, valor := range proibidos {
				if strings.Contains(string(c.r.Corpo), valor) {
					t.Errorf("a resposta carrega o %s (%q): %s", rotulo, valor, c.r.Corpo)
				}
			}
			var qualquer any
			if err := json.Unmarshal(c.r.Corpo, &qualquer); err != nil {
				t.Fatalf("corpo não é JSON: %v", err)
			}
			for _, k := range chavesProibidas {
				if qbTemChave(qualquer, k) {
					t.Errorf("a resposta tem a chave %q, que é dado da venda ou do hóspede: %s", k, c.r.Corpo)
				}
			}
			if fora := qbChavesForaDoContrato(t, c.schema, c.r.Corpo); len(fora) > 0 {
				t.Errorf("campos que o schema %s não declara: %v — %s", c.schema, fora, c.r.Corpo)
			}
		})
	}
	t.Run("planilha", func(t *testing.T) {
		for rotulo, valor := range proibidos {
			if strings.Contains(string(csv.Corpo), valor) {
				t.Errorf("a planilha carrega o %s (%q)", rotulo, valor)
			}
		}
	})

	// A avaria referencia a reserva SÓ pelo código e pelo id: nenhum outro
	// campo derivado da venda sai junto.
	t.Run("avaria com reservation_code devolve só o código", func(t *testing.T) {
		r := a.chamar(t, http.MethodGet, "/inventory/issues/"+avaria.ID.String(), u.Token, nil)
		var env struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(r.Corpo, &env); err != nil {
			t.Fatal(err)
		}
		if env.Data["reservation_code"] != h.Codigo || env.Data["reservation_id"] != h.ReservaID.String() {
			t.Fatalf("reservation_code/id: %v / %v", env.Data["reservation_code"], env.Data["reservation_id"])
		}
		for k := range env.Data {
			if strings.HasPrefix(k, "reservation") && k != "reservation_id" && k != "reservation_code" {
				t.Errorf("campo da reserva além do código: %q", k)
			}
		}
	})
}

func qbTemChave(v any, chave string) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, filho := range x {
			if k == chave || qbTemChave(filho, chave) {
				return true
			}
		}
	case []any:
		for _, filho := range x {
			if qbTemChave(filho, chave) {
				return true
			}
		}
	}
	return false
}
