//go:build integration

package bens_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// Cômodo que já entrou em conferência ou avaria não se apaga: a FK RESTRICT
// recusa, e a resposta é 409 RESOURCE_IN_USE dizendo o que segura e mandando
// desativar. Cômodo sem histórico apaga DE VERDADE, levando a lista de bens.
func TestApagarAmbienteComHistoricoEh409(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	cozinha := a.comodo(t, g, unidade, "Cozinha", "cozinha", 1)
	duplicado := a.comodo(t, g, unidade, "Quarto 2", "quarto", 2)
	prato := a.bem(t, g, "Prato", nil)
	a.colocar(t, g, cozinha.ID, prato.ID, 12)
	a.colocar(t, g, duplicado.ID, prato.ID, 1)

	conf := a.abrir(t, g, unidade)
	exigir(t, a.chamar(t, http.MethodDelete, "/inventory/counts/"+conf.ID.String(), g, nil), http.StatusNoContent, "cancelando")

	r := a.chamar(t, http.MethodDelete, "/rooms/"+cozinha.ID.String(), g, nil)
	exigirErro(t, r, http.StatusConflict, "RESOURCE_IN_USE", "apagando cômodo com conferência")
	d := r.detalhes(t)
	if d["count_lines"] != float64(1) || d["issues"] != float64(0) {
		t.Fatalf("details deveria dizer o que segura (1 linha, 0 avaria): %v", d)
	}

	// O que segura o "Quarto 2" também é a conferência (cancelada continua
	// sendo história). Um cômodo novo, sem nada, apaga.
	limpo := a.comodo(t, g, unidade, "Despensa", "outro", 3)
	a.colocar(t, g, limpo.ID, prato.ID, 2)
	exigir(t, a.chamar(t, http.MethodDelete, "/rooms/"+limpo.ID.String(), g, nil), http.StatusNoContent, "apagando cômodo sem histórico")
	exigirErro(t, a.chamar(t, http.MethodGet, "/rooms/"+limpo.ID.String(), g, nil), http.StatusNotFound, "NOT_FOUND", "relendo apagado")
	var colocacoes int
	if err := a.pool.QueryRow(a.ctx, `SELECT count(*) FROM room_inventory WHERE room_id = $1`, limpo.ID).Scan(&colocacoes); err != nil {
		t.Fatal(err)
	}
	if colocacoes != 0 {
		t.Fatalf("a lista de bens do cômodo apagado deveria ir junto (CASCADE); sobraram %d", colocacoes)
	}

	// O caminho certo para o cômodo com histórico é desativar.
	r = a.chamar(t, http.MethodPatch, "/rooms/"+cozinha.ID.String(), g, map[string]any{"active": false})
	exigir(t, r, http.StatusOK, "desativando")
	if dado[ambienteResp](t, r).Ativo {
		t.Fatal("o cômodo deveria estar inativo")
	}
}

// Bem colocado não se apaga (RESTRICT em `room_inventory`); `details` separa
// colocação de histórico, e a mensagem muda conforme o motivo.
func TestApagarBemColocadoEh409(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	sala := a.comodo(t, g, unidade, "Sala", "sala", 1)
	vaso := a.bem(t, g, "Vaso", nil)
	col := a.colocar(t, g, sala.ID, vaso.ID, 1)

	r := a.chamar(t, http.MethodDelete, "/inventory/items/"+vaso.ID.String(), g, nil)
	exigirErro(t, r, http.StatusConflict, "RESOURCE_IN_USE", "apagando bem colocado")
	d := r.detalhes(t)
	if d["placements"] != float64(1) || d["count_lines"] != float64(0) || d["issues"] != float64(0) {
		t.Fatalf("details: %v", d)
	}

	exigir(t, a.chamar(t, http.MethodDelete, "/inventory/placements/"+col.ID, g, nil), http.StatusNoContent, "tirando do ambiente")
	exigir(t, a.chamar(t, http.MethodDelete, "/inventory/items/"+vaso.ID.String(), g, nil), http.StatusNoContent, "apagando bem solto")
}

// `(unit_id, name)` é a chave natural do cômodo; o par (cômodo, bem) é a chave
// da colocação. As duas colisões são 409 CODE_IN_USE decidido pela constraint,
// e a da colocação diz quanto já tem.
func TestChavesNaturaisSao409CodeInUse(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	quarto := a.comodo(t, g, unidade, "Suíte 1", "quarto", 1)

	r := a.chamar(t, http.MethodPost, "/rooms", g, map[string]any{"unit_id": unidade, "name": "Suíte 1", "kind": "quarto"})
	exigirErro(t, r, http.StatusConflict, "CODE_IN_USE", "dois Suíte 1 na mesma unidade")

	outro := a.comodo(t, g, unidade, "Suíte 2", "quarto", 2)
	r = a.chamar(t, http.MethodPatch, "/rooms/"+outro.ID.String(), g, map[string]any{"name": "Suíte 1"})
	exigirErro(t, r, http.StatusConflict, "CODE_IN_USE", "renomeando para nome que já existe")

	toalha := a.bem(t, g, "Toalha", nil)
	a.colocar(t, g, quarto.ID, toalha.ID, 4)
	r = a.chamar(t, http.MethodPost, "/inventory/placements", g, map[string]any{
		"room_id": quarto.ID, "item_id": toalha.ID, "expected_qty": 2,
	})
	exigirErro(t, r, http.StatusConflict, "CODE_IN_USE", "colocando o mesmo bem duas vezes")
	if r.detalhes(t)["expected_qty"] != float64(4) {
		t.Fatalf("details.expected_qty deveria trazer os 4 que já estão lá: %s", r.Corpo)
	}
}

// Campo que o schema não declara é 422 com o nome dele — inclusive os que
// existem na tabela mas não são da tela (`source_ref`, `unit_id` no PUT do
// cômodo, `room_id` no PUT da colocação, `resolved_at` na avaria).
func TestCampoForaDoContratoEh422(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	quarto := a.comodo(t, g, unidade, "Quarto", "quarto", 1)
	lencol := a.bem(t, g, "Lençol", nil)
	col := a.colocar(t, g, quarto.ID, lencol.ID, 2)

	casos := []struct {
		nome, metodo, caminho, corpo, campo string
	}{
		{"source_ref no POST do bem", http.MethodPost, "/inventory/items",
			`{"name":"Prato","category":"louca","source_ref":"chatwoot:1:2"}`, "source_ref"},
		{"unit_id no PUT do cômodo", http.MethodPut, "/rooms/" + quarto.ID.String(),
			`{"name":"Quarto","kind":"quarto","unit_id":"` + uuid.NewString() + `"}`, "unit_id"},
		{"room_id no PUT da colocação", http.MethodPut, "/inventory/placements/" + col.ID,
			`{"expected_qty":3,"room_id":"` + quarto.ID.String() + `"}`, "room_id"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			r := a.chamar(t, c.metodo, c.caminho, g, c.corpo)
			exigirErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", c.nome)
			if r.detalhes(t)[c.campo] == nil {
				t.Fatalf("o 422 deveria nomear %s: %s", c.campo, r.Corpo)
			}
		})
	}

	// Custo zero é 422: null é "não cotado".
	exigirErro(t, a.chamar(t, http.MethodPost, "/inventory/items", g,
		map[string]any{"name": "Copo", "category": "copo", "replacement_cost_cents": 0}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "custo zero")
}

type bemLidoResp struct {
	Descricao *string `json:"description"`
	Custo     *int64  `json:"replacement_cost_cents"`
	Medida    string  `json:"unit_measure"`
	Ativo     bool    `json:"active"`
}

// PATCH do bem: `null` limpa a descrição e devolve o custo a "não cotado";
// ausente não mexe. O PUT volta ao padrão (`un`, ativo).
func TestPatchEPutDoBem(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	b := a.bem(t, g, "Faca", ptr[int64](990))
	r := a.chamar(t, http.MethodPatch, "/inventory/items/"+b.ID.String(), g, map[string]any{"description": "serrilhada"})
	exigir(t, r, http.StatusOK, "descrevendo")

	r = a.chamar(t, http.MethodPatch, "/inventory/items/"+b.ID.String(), g, `{"replacement_cost_cents": null}`)
	exigir(t, r, http.StatusOK, "voltando a não cotado")
	lido := dado[bemLidoResp](t, r)
	if lido.Custo != nil || lido.Descricao == nil || *lido.Descricao != "serrilhada" {
		t.Fatalf("null limpou o custo e manteve a descrição? %+v", lido)
	}

	r = a.chamar(t, http.MethodPut, "/inventory/items/"+b.ID.String(), g,
		map[string]any{"name": "Faca de pão", "category": "talher", "unit_measure": "par", "active": false})
	exigir(t, r, http.StatusOK, "substituindo")
	r = a.chamar(t, http.MethodPut, "/inventory/items/"+b.ID.String(), g, map[string]any{"name": "Faca de pão", "category": "talher"})
	exigir(t, r, http.StatusOK, "substituindo de novo, sem os opcionais")
	lido = dado[bemLidoResp](t, r)
	if lido.Medida != "un" || !lido.Ativo || lido.Descricao != nil {
		t.Fatalf("PUT sem os opcionais volta ao padrão do schema: %+v", lido)
	}
}

// `room_inventory` não tem `property_id`: é a API que impede colocar item de
// uma casa em cômodo de outra, e esconder o que é da outra.
func TestMisturaDePropriedadesEhRecusada(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	outraCasa := a.outraPropriedade(t)
	unidadeDeLa, _ := a.unidadeEm(t, outraCasa)

	var comodoDeLa, bemDeLa uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO unit_rooms (property_id, unit_id, name, kind) VALUES ($1, $2, 'Cozinha', 'cozinha') RETURNING id`,
		outraCasa, unidadeDeLa).Scan(&comodoDeLa); err != nil {
		t.Fatal(err)
	}
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO inventory_items (property_id, name, category) VALUES ($1, 'Prato de lá', 'louca') RETURNING id`,
		outraCasa).Scan(&bemDeLa); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.itens = append(a.itens, bemDeLa)
	a.mu.Unlock()

	unidade, _ := a.unidade(t)
	daqui := a.comodo(t, g, unidade, "Cozinha", "cozinha", 1)
	bemDaqui := a.bem(t, g, "Prato daqui", nil)

	r := a.chamar(t, http.MethodPost, "/inventory/placements", g, map[string]any{
		"room_id": daqui.ID, "item_id": bemDeLa, "expected_qty": 1,
	})
	exigirErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "bem de outra casa")
	if r.detalhes(t)["item_id"] == nil {
		t.Fatalf("o 422 deveria apontar item_id: %s", r.Corpo)
	}
	r = a.chamar(t, http.MethodPost, "/inventory/placements", g, map[string]any{
		"room_id": comodoDeLa, "item_id": bemDaqui.ID, "expected_qty": 1,
	})
	exigirErro(t, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "cômodo de outra casa")

	exigirErro(t, a.chamar(t, http.MethodGet, "/rooms/"+comodoDeLa.String(), g, nil), http.StatusNotFound, "NOT_FOUND", "cômodo de lá")
	exigirErro(t, a.chamar(t, http.MethodGet, "/inventory/items/"+bemDeLa.String(), g, nil), http.StatusNotFound, "NOT_FOUND", "bem de lá")
	exigirErro(t, a.chamar(t, http.MethodGet, "/units/"+unidadeDeLa.String()+"/inventory", g, nil), http.StatusNotFound, "NOT_FOUND", "unidade de lá")
	exigirErro(t, a.chamar(t, http.MethodPost, "/rooms", g, map[string]any{"unit_id": unidadeDeLa, "name": "X", "kind": "sala"}),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "cômodo em unidade de lá")

	// Mesmo que uma colocação mista exista no banco (escrita por fora da API),
	// ela não aparece nas listagens desta casa.
	if _, err := a.pool.Exec(a.ctx, `INSERT INTO room_inventory (room_id, item_id, expected_qty) VALUES ($1, $2, 3)`,
		daqui.ID, bemDeLa); err != nil {
		t.Fatal(err)
	}
	r = a.chamar(t, http.MethodGet, "/inventory/placements?room_id="+daqui.ID.String(), g, nil)
	exigir(t, r, http.StatusOK, "listando")
	if cols, _ := lista[colocacaoResp](t, r); len(cols) != 0 {
		t.Fatalf("colocação com bem de outra casa vazou na listagem: %+v", cols)
	}
}

// Lista paginada com desempate: duas páginas não repetem nem perdem cômodo,
// mesmo com todos no mesmo `sort_order`.
func TestPaginacaoDosAmbientesComDesempate(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	for _, nome := range []string{"E", "C", "A", "D", "B"} {
		a.comodo(t, g, unidade, nome, "quarto", 0)
	}
	vistos := map[string]bool{}
	var ordem []string
	for pagina := 1; pagina <= 3; pagina++ {
		r := a.chamar(t, http.MethodGet, "/rooms?per_page=2&page="+string(rune('0'+pagina))+"&unit_id="+unidade.String(), g, nil)
		exigir(t, r, http.StatusOK, "paginando")
		itens, m := lista[ambienteResp](t, r)
		if m.Total != 5 || m.TotalPages != 3 {
			t.Fatalf("meta: %+v", m)
		}
		for _, it := range itens {
			if vistos[it.Nome] {
				t.Fatalf("%s apareceu em duas páginas", it.Nome)
			}
			vistos[it.Nome] = true
			ordem = append(ordem, it.Nome)
		}
	}
	if len(ordem) != 5 || ordem[0] != "A" || ordem[4] != "E" {
		t.Fatalf("empate em sort_order desfeito por nome: %v", ordem)
	}

	// Página além do fim: lista vazia com o total verdadeiro.
	r := a.chamar(t, http.MethodGet, "/rooms?per_page=2&page=9&unit_id="+unidade.String(), g, nil)
	if itens, m := lista[ambienteResp](t, r); len(itens) != 0 || m.Total != 5 {
		t.Fatalf("página além do fim: %d itens, total %d", len(itens), m.Total)
	}
	exigirErro(t, a.chamar(t, http.MethodGet, "/rooms?kind=Dormitorio", g, nil),
		http.StatusUnprocessableEntity, "VALIDATION_ERROR", "filtro fora do vocabulário")
}

// O RBAC é o de `inventory.goods`: quem só vê não grava, quem não tem o
// recurso não entra, e sem token é 401. O recurso `inventory` (cadastro
// comercial) NÃO abre esta porta.
func TestPermissaoDoRecursoProprio(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	leitor := a.token(t, a.perfil(t, "bens_leitor", celulaVer))
	comercial := a.token(t, a.perfil(t, "bens_comercial", "inventory:ver", "inventory:criar", "inventory:editar", "inventory:excluir"))
	unidade, _ := a.unidade(t)

	exigir(t, a.chamar(t, http.MethodGet, "/rooms", leitor, nil), http.StatusOK, "leitor lista")
	exigirErro(t, a.chamar(t, http.MethodPost, "/rooms", leitor, map[string]any{"unit_id": unidade, "name": "X", "kind": "sala"}),
		http.StatusForbidden, "FORBIDDEN", "leitor cria")
	exigirErro(t, a.chamar(t, http.MethodGet, "/inventory/items", comercial, nil),
		http.StatusForbidden, "FORBIDDEN", "o cadastro comercial não abre os bens")
	if r := a.chamar(t, http.MethodGet, "/inventory/items", "", nil); r.Status != http.StatusUnauthorized {
		t.Fatalf("sem token: %d", r.Status)
	}
	exigir(t, a.chamar(t, http.MethodGet, "/inventory/items", g, nil), http.StatusOK, "gestor lista")
}
