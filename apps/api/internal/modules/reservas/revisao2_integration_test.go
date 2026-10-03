//go:build integration

// Os achados da SEGUNDA revisão adversarial deste módulo, cada um com o teste
// que o reproduz. Os três falhavam antes da correção; a saída de cada falha
// está no relatório da rodada.
package reservas_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/reservas"
)

// ─────────────────────────── ALTO — o dinheiro da remarcação ────────

// TestRemarcacaoNaoFazSinalExcedenteSumir.
//
// MEDIDO ANTES: WH-2026-0002, total 895.000, confirmada com
// `deposit_paid_cents = 830.000`. `POST /reschedule` para 1 noite gerou
// WH-2026-0003 com total 225.000 e o evento `confirmed`
// `{"inherited":true,"deposit_paid_cents":830000}` — valor que o próprio
// /confirm recusaria com 422. O /cancel seguinte devolveu
// `{"refund_cents":225000,"retained_cents":0}`: o hóspede pagou R$ 8.300,00,
// recebeu R$ 2.250,00 e o sistema declarou a conta encerrada. O excedente
// desaparecia dentro do `if base > e.Total { base = e.Total }` de
// `calcularCancelamento`, sem crédito e sem dívida.
func TestRemarcacaoNaoFazSinalExcedenteSumir(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)
	apto := a.produto(t, "apto-2s")

	// 2027-03-01 é segunda-feira: quatro diárias normais viram uma estadia
	// cara, e a remarcação para UMA noite normal derruba o total de propósito.
	original := a.criarReserva(t, token, pedido(apto, contato, "2027-03-01", "2027-03-05", 2))
	// Pagamento integral adiantado é legítimo e é o pior caso: é o maior valor
	// que o /confirm aceita, e portanto o maior excedente possível.
	a.confirmar(t, token, original.ID, original.Total)

	chave := "it-rem-credito-" + sufixo()
	a.limparChaves(t, chave)
	resp := a.chamarIdem(t, http.MethodPost, "/reservations/"+original.ID.String()+"/reschedule", token,
		map[string]any{"check_in": "2027-03-08", "check_out": "2027-03-09"}, chave)
	if resp.Status != http.StatusCreated {
		t.Fatalf("POST /reschedule = %d (%s)", resp.Status, resp.Corpo)
	}

	var env struct {
		Data reservaVista `json:"data"`
		Meta struct {
			Diferenca int64 `json:"difference_cents"`
			Credito   int64 `json:"credit_cents"`
		} `json:"meta"`
	}
	decodificar(t, resp, &env)
	nova := env.Data
	excedente := original.Total - nova.Total

	if nova.Total >= original.Total {
		t.Fatalf("o cenário não reproduz: total novo %d não é menor que %d", nova.Total, original.Total)
	}

	// 1. O sinal herdado respeita a MESMA faixa que o /confirm exige.
	pago := a.sinalDoEventoDeConfirmacao(t, nova.ID)
	if pago > nova.Total {
		t.Fatalf("sinal herdado = %d > total da nova = %d — valor que o /confirm recusaria com 422",
			pago, nova.Total)
	}
	if pago != nova.Total {
		t.Fatalf("sinal aplicado = %d, esperado %d (o teto)", pago, nova.Total)
	}

	// 2. O excedente existe em algum lugar AUDITÁVEL.
	if env.Meta.Credito != excedente {
		t.Fatalf("meta.credit_cents = %d, esperado %d", env.Meta.Credito, excedente)
	}
	if c := a.creditoRegistrado(t, nova.ID); c != excedente {
		t.Fatalf("crédito em reservation_events = %d, esperado %d — o excedente sumiu sem registro", c, excedente)
	}

	// 3. E o /cancel não declara a conta encerrada por cima dele.
	r := a.chamar(t, http.MethodPost, "/reservations/"+nova.ID.String()+"/cancel", token, nil)
	if r.Status != http.StatusOK {
		t.Fatalf("POST /cancel = %d (%s)", r.Status, r.Corpo)
	}
	fim := dado[struct {
		Devolucao int64 `json:"refund_cents"`
		Retido    int64 `json:"retained_cents"`
		Credito   int64 `json:"credit_cents"`
	}](t, r)
	if fim.Credito != excedente {
		t.Fatalf("cancel devolveu credit_cents = %d, esperado %d", fim.Credito, excedente)
	}
	// A conta do hóspede fecha: o que ele recebe de volta mais o que a casa
	// retém mais o crédito em aberto é exatamente o que ele pagou.
	if soma := fim.Devolucao + fim.Retido + fim.Credito; soma != original.Total {
		t.Fatalf("devolução %d + retido %d + crédito %d = %d, esperado %d (o que entrou)",
			fim.Devolucao, fim.Retido, fim.Credito, soma, original.Total)
	}
}

// TestCreditoAcompanhaACadeiaDeRemarcacoes.
//
// O crédito não pode ficar órfão na reserva do meio: A (paga integral) → B
// (mais barata, gera crédito) → C (cara de novo) tem de consumir o crédito, e
// o hóspede não pode terminar a cadeia devendo o que já pagou.
func TestCreditoAcompanhaACadeiaDeRemarcacoes(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)
	apto := a.produto(t, "apto-2s")

	remarcar := func(t *testing.T, de uuid.UUID, checkIn, checkOut string) reservaVista {
		t.Helper()
		chave := "it-cadeia-" + sufixo()
		a.limparChaves(t, chave)
		r := a.chamarIdem(t, http.MethodPost, "/reservations/"+de.String()+"/reschedule", token,
			map[string]any{"check_in": checkIn, "check_out": checkOut}, chave)
		if r.Status != http.StatusCreated {
			t.Fatalf("POST /reschedule = %d (%s)", r.Status, r.Corpo)
		}
		return dado[reservaVista](t, r)
	}

	a1 := a.criarReserva(t, token, pedido(apto, contato, "2027-04-05", "2027-04-09", 2))
	a.confirmar(t, token, a1.ID, a1.Total)

	// Encurta: sobra crédito.
	b := remarcar(t, a1.ID, "2027-04-12", "2027-04-13")
	if c := a.creditoRegistrado(t, b.ID); c != a1.Total-b.Total {
		t.Fatalf("crédito em B = %d, esperado %d", c, a1.Total-b.Total)
	}

	// Volta ao tamanho original: o crédito é consumido e nada mais fica em
	// aberto. A semana de 26/04 e não a de 19/04 porque 21/04 é Tiradentes — a
	// diária de feriado mudaria o total e o teste mediria outra coisa.
	c := remarcar(t, b.ID, "2027-04-26", "2027-04-30")
	if c.Total != a1.Total {
		t.Fatalf("o cenário não reproduz: total de C = %d, esperado %d", c.Total, a1.Total)
	}
	if pago := a.sinalDoEventoDeConfirmacao(t, c.ID); pago != a1.Total {
		t.Fatalf("sinal aplicado em C = %d — o crédito de B ficou órfão numa reserva cancelada", pago)
	}
	if aberto := a.creditoRegistrado(t, c.ID); aberto != 0 {
		t.Fatalf("crédito em aberto em C = %d, esperado 0", aberto)
	}
}

// sinalDoEventoDeConfirmacao lê o `deposit_paid_cents` do último evento
// `confirmed` — a mesma fonte que o /cancel usa como base.
func (a *ambiente) sinalDoEventoDeConfirmacao(t *testing.T, reserva uuid.UUID) int64 {
	t.Helper()

	var v *int64
	if err := a.pool.QueryRow(a.ctx, `
		SELECT (payload->>'deposit_paid_cents')::bigint
		  FROM reservation_events
		 WHERE reservation_id = $1 AND type = 'confirmed' AND payload ? 'deposit_paid_cents'
		 ORDER BY at DESC LIMIT 1`, reserva).Scan(&v); err != nil {
		t.Fatalf("lendo o evento de confirmação: %v", err)
	}
	if v == nil {
		t.Fatal("a reserva nasceu confirmada sem registrar sinal nenhum")
	}
	return *v
}

// creditoRegistrado soma os créditos a favor do hóspede gravados na timeline.
func (a *ambiente) creditoRegistrado(t *testing.T, reserva uuid.UUID) int64 {
	t.Helper()

	var v int64
	if err := a.pool.QueryRow(a.ctx, `
		SELECT COALESCE(sum((payload->>'credit_cents')::bigint), 0)
		  FROM reservation_events
		 WHERE reservation_id = $1 AND type = $2`, reserva, reservas.EventoCredito).Scan(&v); err != nil {
		t.Fatalf("somando créditos: %v", err)
	}
	return v
}

// ─────────────────────────── MÉDIO — bloqueio sem teto ──────────────

// TestBloqueioOperacionalTemJanelaMaxima.
//
// MEDIDO ANTES: um POST /blocks do CORRETOR (o menor privilégio) com as oito
// unidades e `from=2040-01-01 to=2050-01-01` respondeu 201 e tirou 29.224
// noites-unidade do mercado numa tecla. A venda da Completa em 2045 passava a
// morrer com 409, e desfazer exige oito DELETEs.
func TestBloqueioOperacionalTemJanelaMaxima(t *testing.T) {
	a := subir(t)
	corretor := a.token(t, a.perfilComEscopo(t, "res_corretor_teto", "own",
		"calendar:ver", "calendar:criar", "calendar:excluir"))

	todas := []uuid.UUID{}
	for _, c := range []string{"AP-01", "AP-02", "AP-03", "SP-01", "SP-02", "SP-03", "SP-04", "COB-01"} {
		todas = append(todas, a.unidade(t, c))
	}

	casos := []struct {
		nome     string
		de, ate  string
		unidades []uuid.UUID
		campo    string
	}{
		{"a década inteira", "2040-01-01", "2050-01-01", todas, "to"},
		{"um ano e um dia", a.dia(10), a.dia(376), todas[:1], "to"},
		{"horizonte longe demais", a.dia(4000), a.dia(4003), todas[:1], "from"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			r := a.chamar(t, http.MethodPost, "/blocks", corretor, map[string]any{
				"unit_ids": c.unidades, "from": c.de, "to": c.ate, "source": "owner_hold",
			})
			// O bloqueio indevido não pode ficar de pé nem quando o teste falha.
			for _, criado := range dado[[]struct {
				ID uuid.UUID `json:"id"`
			}](t, r) {
				id := criado.ID
				t.Cleanup(func() { a.executar(t, `DELETE FROM stay_blocks WHERE id = $1`, id) })
			}
			if r.Status != http.StatusUnprocessableEntity {
				t.Fatalf("POST /blocks [%s, %s) × %d unidades = %d, esperado 422 (%s)",
					c.de, c.ate, len(c.unidades), r.Status, r.Corpo)
			}
			if _, tem := r.detalhes(t)[c.campo]; !tem {
				t.Fatalf("o 422 não aponta o campo %q: %s", c.campo, r.Corpo)
			}
		})
	}

	// O que é operação de verdade continua passando.
	t.Run("uma quinzena de pintura passa", func(t *testing.T) {
		r := a.chamar(t, http.MethodPost, "/blocks", corretor, map[string]any{
			"unit_ids": todas[:1], "from": a.dia(30), "to": a.dia(45), "source": "maintenance",
		})
		if r.Status != http.StatusCreated {
			t.Fatalf("bloqueio operacional legítimo = %d (%s)", r.Status, r.Corpo)
		}
		for _, criado := range dado[[]struct {
			ID uuid.UUID `json:"id"`
		}](t, r) {
			id := criado.ID
			t.Cleanup(func() { a.executar(t, `DELETE FROM stay_blocks WHERE id = $1`, id) })
		}
	})
}

// ─────────────────────────── BAIXO — cancelar quem já dormiu ────────

// TestCancelarEstadiaEmCursoPreservaANoiteDormida.
//
// MEDIDO ANTES: SP-02, 26→30/08, confirmada, check-in feito, `POST /cancel` →
// o dinheiro saía certo (`retained 100.000`), mas os blocos iam para
// `cancelled`. A noite JÁ DORMIDA ficava byte a byte igual a uma venda que
// nunca existiu: sumia do mapa, da ocupação e do razão. É a mesma família do
// check-out — o estado terminal que preserva a história é `completed`.
func TestCancelarEstadiaEmCursoPreservaANoiteDormida(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)

	reserva := a.criarReserva(t, token,
		pedido(a.produto(t, "suite-piscina"), contato, a.dia(0), a.dia(4), 2))
	base := "/reservations/" + reserva.ID.String()
	a.confirmar(t, token, reserva.ID, 100000)

	if r := a.chamar(t, http.MethodPost, base+"/check-in", token, nil); r.Status != http.StatusOK {
		t.Fatalf("check-in = %d (%s)", r.Status, r.Corpo)
	}

	r := a.chamar(t, http.MethodPost, base+"/cancel", token, nil)
	if r.Status != http.StatusOK {
		t.Fatalf("cancel de estadia em curso = %d (%s)", r.Status, r.Corpo)
	}
	var fim struct {
		Retido int64 `json:"retained_cents"`
	}
	if err := json.Unmarshal(dadoBruto(t, r), &fim); err != nil {
		t.Fatalf("lendo o resultado: %v", err)
	}
	if fim.Retido != 100000 {
		t.Fatalf("retained_cents = %d, esperado 100000 (a política não mudou)", fim.Retido)
	}

	blocos := a.statusDosBlocos(t, reserva.ID)
	if blocos[reservas.BlocoCancelado] != 0 {
		t.Fatalf("a noite dormida virou venda que nunca existiu: %v", blocos)
	}
	if blocos[reservas.BlocoConcluido] != 1 {
		t.Fatalf("blocos depois do cancelamento = %v, esperado 1 em %q", blocos, reservas.BlocoConcluido)
	}
	// O mapa continua enxergando a estadia...
	if n := a.noitesOcupadasNoMapa(t, reserva.ID); n != 4 {
		t.Fatalf("noites visíveis no mapa = %d, esperado 4", n)
	}
	// ...e o inventário volta a vender a data.
	a.criarReserva(t, token, pedido(a.produto(t, "suite-piscina"), contato, a.dia(0), a.dia(4), 2))
}

// dadoBruto devolve o `data` do envelope sem decodificar — deixa o teste
// escolher a struct.
func dadoBruto(t *testing.T, r resposta) []byte {
	t.Helper()

	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(r.Corpo, &env); err != nil {
		t.Fatalf("lendo o envelope: %v (corpo %s)", err, r.Corpo)
	}
	return env.Data
}
