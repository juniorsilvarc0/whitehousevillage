//go:build integration

// A EXCLUSIVIDADE DA WHITE HOUSE COMPLETA.
//
// O produto `all_members` vende a casa INTEIRA. A revisão adversarial mediu que
// essa exclusividade não era invariante de nada: ela era consequência de uma
// consulta devolver oito linhas. Bastava uma unidade sair do ar — `PATCH
// /units/{id} {"active": false}`, que é manutenção rotineira — para
// `Candidatas` devolver sete, `AlocarTodasAsUnidades` inserir sete blocos, e a
// venda sair com sete apartamentos pelo preço de oito. A constraint
// `stay_no_overlap` não pega: a oitava linha nunca chegou a existir, e o que
// não é inserido não colide com nada.
//
// O desfecho medido não é "entregamos 7 de 8". É estranho dormindo dentro da
// casa que alguém alugou inteira: com AP-03 de volta ao ar, um Apartamento
// 2 Suítes é vendido nas MESMAS datas e a constraint aceita, porque aquela
// unidade está livre no calendário.
package reservas_test

import (
	"net/http"
	"testing"
)

// TestCompletaRecusaVendaComUnidadeInativa é o teste do CRÍTICO 1.
func TestCompletaRecusaVendaComUnidadeInativa(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)
	completa := a.produto(t, "completa")

	de, ate := a.dia(400), a.dia(403)

	a.desativarUnidade(t, "AP-03")

	chave := "it-" + sufixo() + "-" + sufixo()
	a.limparChaves(t, chave)
	resp := a.chamarIdem(t, http.MethodPost, "/reservations", token,
		pedido(completa, contato, de, ate, 10), chave)

	if resp.Status == http.StatusCreated {
		venda := dado[reservaVista](t, resp)
		a.apagarReserva(t, venda.ID)
		t.Fatalf("a Completa foi VENDIDA com a composição incompleta: %d unidades (%v), total_cents=%d — o mesmo preço da casa inteira",
			len(venda.Unidades), venda.codigosDasUnidades(), venda.Total)
	}
	if resp.Status != http.StatusUnprocessableEntity {
		t.Fatalf("POST /reservations = %d, esperado 422 (%s)", resp.Status, resp.Corpo)
	}
	if c := resp.codigoDoErro(); c != "COMPOSITION_INCOMPLETE" {
		t.Fatalf("code = %q, esperado COMPOSITION_INCOMPLETE (%s)", c, resp.Corpo)
	}

	d := resp.detalhes(t)
	if d["unit_type_code"] != "completa" {
		t.Errorf("details.unit_type_code = %v, esperado completa", d["unit_type_code"])
	}
	if d["expected_units"] != float64(8) || d["active_units"] != float64(7) {
		t.Errorf("details = %v, esperado expected_units 8 e active_units 7", d)
	}
	faltando, _ := d["missing_unit_codes"].([]any)
	if len(faltando) != 1 || faltando[0] != "AP-03" {
		t.Errorf("details.missing_unit_codes = %v, esperado [AP-03]", d["missing_unit_codes"])
	}
}

// TestCompletaVendidaInteiraNaoDeixaBrechaParaOutraVenda fecha a segunda metade
// do achado: uma vez que a casa inteira foi vendida, NENHUMA unidade dela
// aceita outra reserva nas mesmas datas — e quem recusa é a constraint.
func TestCompletaVendidaInteiraNaoDeixaBrechaParaOutraVenda(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)

	de, ate := a.dia(410), a.dia(413)

	casa := a.criarReserva(t, token, pedido(a.produto(t, "completa"), contato, de, ate, 10))
	if len(casa.Unidades) != 8 {
		t.Fatalf("a Completa alocou %d unidades (%v), esperado 8", len(casa.Unidades), casa.codigosDasUnidades())
	}

	chave := "it-" + sufixo() + "-" + sufixo()
	a.limparChaves(t, chave)
	resp := a.chamarIdem(t, http.MethodPost, "/reservations", token,
		pedido(a.produto(t, "apto-2s"), contato, de, ate, 4), chave)
	if resp.Status != http.StatusConflict {
		t.Fatalf("apto-2s dentro da casa alugada inteira = %d, esperado 409 (%s)", resp.Status, resp.Corpo)
	}
}
