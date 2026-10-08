//go:build integration

package bens_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/bens"
)

// A derivação em Go e a do backfill em SQL (20261007213000) têm de dar o
// mesmo resultado, caractere a caractere: o teste roda AQUI, no Postgres, a
// mesma expressão do bloco 2 da migration e compara com BaseDoCodigo.
func TestDerivacaoDoCodigoIgualADaMigration(t *testing.T) {
	a := subir(t)
	const baseSQL = `
		SELECT CASE WHEN b = '' THEN 'comodo' ELSE b END FROM (
		  SELECT rtrim(left(btrim(regexp_replace(translate(
		           regexp_replace($1::text, E'[̀-ͯ]', '', 'g'),
		           'ÀÁÂÃÄàáâãä' || 'ÈÉÊËèéêë' || 'ÌÍÎÏìíîï' || 'ÒÓÔÕÖòóôõö' || 'ÙÚÛÜùúûü'
		             || 'Çç' || 'Ññ' || 'ABCDEFGHIJKLMNOPQRSTUVWXYZ',
		           'aaaaaaaaaa' || 'eeeeeeee' || 'iiiiiiii' || 'oooooooooo' || 'uuuuuuuu'
		             || 'cc' || 'nn' || 'abcdefghijklmnopqrstuvwxyz'),
		         '[^a-z0-9]+', '-', 'g'), '-'), 60), '-') AS b) x`
	nomes := []string{
		"Área da churrasqueira", "Suíte 1 (térreo)", "🛏️", "Øresund ß", "SALA!", "Ação & Ñandú",
		"Área externa", "Quarto_2 — vista mar", "  --Cozinha--  ", "Ýmir", "",
		"Um nome comprido demais para caber nos sessenta caracteres do code do cômodo",
	}
	for _, nome := range nomes {
		var sql string
		if err := a.pool.QueryRow(a.ctx, baseSQL, nome).Scan(&sql); err != nil {
			t.Fatalf("rodando a regra da migration para %q: %v", nome, err)
		}
		if got := bens.BaseDoCodigo(nome); got != sql {
			t.Errorf("%q: Go %q, migration %q", nome, got, sql)
		}
	}
}

// Sem `code`, o servidor deriva do nome (sem acento, minúsculo, hífen) e, na
// colisão dentro da unidade, acrescenta `-2`, `-3`… A mesma base em outra
// unidade não colide.
func TestCodigoDerivadoComAcentoEColisao(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	outra, _ := a.unidade(t)

	casos := []struct{ nome, code string }{
		{"Área da churrasqueira", "area-da-churrasqueira"},
		{"Suíte 1 (térreo)", "suite-1-terreo"},
		{"Sala", "sala"},
		{"SALA!", "sala-2"},
		{"sala.", "sala-3"},
		{"🛏️", "comodo"},
	}
	for _, c := range casos {
		if got := a.comodo(t, g, unidade, c.nome, "outro", 0).Codigo; got != c.code {
			t.Errorf("POST %q: code %q, esperado %q", c.nome, got, c.code)
		}
	}
	if got := a.comodo(t, g, outra, "Sala", "sala", 0).Codigo; got != "sala" {
		t.Errorf("a mesma base em outra unidade não colide: %q", got)
	}

	// O GET devolve o mesmo code.
	// `q` é ILIKE: "SALA!" (com a exclamação) acha só o segundo.
	r := a.chamar(t, http.MethodGet, "/rooms?unit_id="+unidade.String()+"&q=SALA%21", g, nil)
	if itens, _ := lista[ambienteResp](t, r); len(itens) != 1 || itens[0].Codigo != "sala-2" {
		t.Fatalf("listagem: %s", r.Corpo)
	}
}

// `code` informado: já usado na unidade é 409 CODE_IN_USE (o cliente escolheu
// aquele, não se inventa sufixo); fora do formato é 422.
func TestCodigoInformado(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)

	r := a.chamar(t, http.MethodPost, "/rooms", g, map[string]any{"unit_id": unidade, "name": "Suíte Master", "kind": "quarto", "code": "suite-1"})
	exigir(t, r, http.StatusCreated, "criando com code")
	if c := dado[ambienteResp](t, r); c.Codigo != "suite-1" {
		t.Fatalf("o code informado vale: %+v", c)
	}
	r = a.chamar(t, http.MethodPost, "/rooms", g, map[string]any{"unit_id": unidade, "name": "Outra suíte", "kind": "quarto", "code": "suite-1"})
	exigirErro(t, r, http.StatusConflict, "CODE_IN_USE", "code repetido")
	if r.detalhes(t)["code"] == nil {
		t.Fatalf("o 409 deveria apontar code: %s", r.Corpo)
	}
	for _, ruim := range []string{"Suite-1", "suíte", "suite_1", "-suite", "suite--1", "suite 1", ""} {
		r = a.chamar(t, http.MethodPost, "/rooms", g, map[string]any{"unit_id": unidade, "name": "X " + ruim, "kind": "quarto", "code": ruim})
		exigirErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", fmt.Sprintf("code %q", ruim))
		if r.detalhes(t)["code"] == nil {
			t.Fatalf("o 422 deveria apontar code: %s", r.Corpo)
		}
	}
	// Nome repetido continua sendo 409 no `name`, mesmo com code novo.
	r = a.chamar(t, http.MethodPost, "/rooms", g, map[string]any{"unit_id": unidade, "name": "Suíte Master", "kind": "quarto", "code": "outra"})
	exigirErro(t, r, http.StatusConflict, "CODE_IN_USE", "nome repetido")
	if r.detalhes(t)["name"] == nil {
		t.Fatalf("o 409 de nome deveria apontar name: %s", r.Corpo)
	}
}

// A cópia herda o `code` da origem e, depois de o cômodo ser renomeado no
// destino, o reencontra pelo `code` — sem criar o fantasma com o nome antigo.
func TestCopiaCasaPeloCodigoDepoisDoRename(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	origem, _ := a.unidade(t)
	destino, _ := a.unidade(t)
	quartoOrigem := a.comodo(t, g, origem, "Quarto grande", "quarto", 1)
	cama := a.bem(t, g, "Cama", nil)
	abajur := a.bem(t, g, "Abajur", nil)
	a.colocar(t, g, quartoOrigem.ID, cama.ID, 1)

	caminho := fmt.Sprintf("/units/%s/inventory/copy", destino)
	exigir(t, a.chamar(t, http.MethodPost, caminho, g, map[string]any{"source_unit_id": origem}), http.StatusOK, "primeira cópia")

	comodos := func() []ambienteResp {
		r := a.chamar(t, http.MethodGet, "/rooms?unit_id="+destino.String(), g, nil)
		itens, _ := lista[ambienteResp](t, r)
		return itens
	}
	noDestino := comodos()
	if len(noDestino) != 1 || noDestino[0].Codigo != quartoOrigem.Codigo || noDestino[0].Nome != "Quarto grande" {
		t.Fatalf("o cômodo criado herda o code da origem (%s): %+v", quartoOrigem.Codigo, noDestino)
	}

	// Renomeia no destino e acrescenta um bem na origem.
	exigir(t, a.chamar(t, http.MethodPatch, "/rooms/"+noDestino[0].ID.String(), g, map[string]any{"name": "Suíte Master"}),
		http.StatusOK, "renomeando no destino")
	a.colocar(t, g, quartoOrigem.ID, abajur.ID, 2)

	r := a.chamar(t, http.MethodPost, caminho, g, map[string]any{"source_unit_id": origem})
	exigir(t, r, http.StatusOK, "segunda cópia")
	seg := dado[copiaResp](t, r)
	if len(seg.Ambientes) != 0 {
		t.Fatalf("o renomeado é o MESMO cômodo — nenhum fantasma \"Quarto grande\": %+v", seg.Ambientes)
	}
	if len(seg.Colocacoes) != 1 || seg.Colocacoes[0].AmbienteNome != "Suíte Master" || seg.Colocacoes[0].BemID != abajur.ID {
		t.Fatalf("o abajur vai para a Suíte Master: %+v", seg.Colocacoes)
	}
	if len(seg.Mantidas) != 1 || seg.Mantidas[0].AmbienteNome != "Suíte Master" {
		t.Fatalf("a cama já estava lá: %+v", seg.Mantidas)
	}
	if depois := comodos(); len(depois) != 1 || depois[0].Nome != "Suíte Master" {
		t.Fatalf("o destino continua com um cômodo só: %+v", depois)
	}
}
