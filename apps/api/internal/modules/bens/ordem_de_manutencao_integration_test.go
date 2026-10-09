//go:build integration

package bens_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// A ordem de manutenção (outro módulo) segura a avaria, o cômodo e o bem pelas
// FKs RESTRICT — e a avaria passa a dizer qual ordem ainda está aberta sobre
// ela. A ordem entra aqui pelo banco: o módulo de bens só a ENXERGA.
func TestOrdemDeManutencaoSeguraAvariaComodoEBem(t *testing.T) {
	a := subir(t)
	g := a.gestor(t)
	unidade, _ := a.unidade(t)
	cozinha := a.comodo(t, g, unidade, "Cozinha", "cozinha", 1)
	geladeira := a.bem(t, g, "Geladeira", nil)
	a.colocar(t, g, cozinha.ID, geladeira.ID, 1)

	r := a.chamar(t, http.MethodPost, "/inventory/issues", g, map[string]any{
		"room_id": cozinha.ID, "item_id": geladeira.ID, "kind": "avariado", "qty": 1,
	})
	exigir(t, r, http.StatusCreated, "relatando")
	type avaria struct {
		ID        uuid.UUID  `json:"id"`
		UnidadeID uuid.UUID  `json:"unit_id"`
		Ordem     *uuid.UUID `json:"open_maintenance_order_id"`
	}
	av := dado[avaria](t, r)
	if av.Ordem != nil || av.UnidadeID != unidade {
		t.Fatalf("avaria nova: sem ordem, com unit_id: %+v", av)
	}

	var ordem uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO maintenance_orders (property_id, unit_id, room_id, item_id, issue_id, title)
		VALUES ($1, $2, $3, $4, $5, 'Geladeira não gela') RETURNING id`,
		a.propriedade, unidade, cozinha.ID, geladeira.ID, av.ID).Scan(&ordem); err != nil {
		t.Fatal(err)
	}

	// Toda leitura da avaria diz qual ordem está aberta sobre ela.
	caminho := "/inventory/issues/" + av.ID.String()
	if lida := dado[avaria](t, a.chamar(t, http.MethodGet, caminho, g, nil)); lida.Ordem == nil || *lida.Ordem != ordem {
		t.Fatalf("detalhe: open_maintenance_order_id = %v, esperado %s", lida.Ordem, ordem)
	}
	r = a.chamar(t, http.MethodGet, "/inventory/issues?room_id="+cozinha.ID.String(), g, nil)
	itens, m := lista[avaria](t, r)
	if len(itens) != 1 || m.Total != 1 || itens[0].Ordem == nil || *itens[0].Ordem != ordem {
		t.Fatalf("lista: %s", r.Corpo)
	}
	r = a.chamar(t, http.MethodPatch, caminho, g, map[string]any{"note": "porta não veda"})
	exigir(t, r, http.StatusOK, "editando a avaria")
	if p := dado[avaria](t, r); p.Ordem == nil || *p.Ordem != ordem {
		t.Fatalf("resposta do PATCH: %+v", p)
	}

	// Citada por ordem, a avaria não se apaga: 409 com a ordem.
	r = a.chamar(t, http.MethodDelete, caminho, g, nil)
	exigirErro(t, r, http.StatusConflict, "RESOURCE_IN_USE", "apagando avaria com ordem")
	if r.detalhes(t)["maintenance_order_id"] != ordem.String() {
		t.Fatalf("details.maintenance_order_id: %s", r.Corpo)
	}

	// Ordem cancelada: a avaria deixa de ter ordem ABERTA, mas continua citada.
	if _, err := a.pool.Exec(a.ctx,
		`UPDATE maintenance_orders SET status = 'cancelada', closed_at = now() WHERE id = $1`, ordem); err != nil {
		t.Fatal(err)
	}
	if lida := dado[avaria](t, a.chamar(t, http.MethodGet, caminho, g, nil)); lida.Ordem != nil {
		t.Fatalf("ordem encerrada não é ordem aberta: %v", lida.Ordem)
	}
	r = a.chamar(t, http.MethodDelete, caminho, g, nil)
	exigirErro(t, r, http.StatusConflict, "RESOURCE_IN_USE", "apagando avaria de ordem cancelada")
	if r.detalhes(t)["maintenance_order_id"] != ordem.String() {
		t.Fatalf("a ordem cancelada continua citando a avaria: %s", r.Corpo)
	}

	// Cômodo e bem citados: o 409 conta as ordens.
	r = a.chamar(t, http.MethodDelete, "/rooms/"+cozinha.ID.String(), g, nil)
	exigirErro(t, r, http.StatusConflict, "RESOURCE_IN_USE", "apagando o cômodo")
	if d := r.detalhes(t); d["maintenance_orders"] != float64(1) || d["issues"] != float64(1) {
		t.Fatalf("details do cômodo: %s", r.Corpo)
	}
	r = a.chamar(t, http.MethodDelete, "/inventory/items/"+geladeira.ID.String(), g, nil)
	exigirErro(t, r, http.StatusConflict, "RESOURCE_IN_USE", "apagando o bem")
	if d := r.detalhes(t); d["maintenance_orders"] != float64(1) {
		t.Fatalf("details do bem: %s", r.Corpo)
	}
}
