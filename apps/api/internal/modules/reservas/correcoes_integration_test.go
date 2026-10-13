//go:build integration

// Os achados da revisão adversarial que este módulo tinha de fechar, cada um com
// o teste que o reproduz. Todos falhavam antes da correção — a saída de cada
// falha está no relatório da rodada.
package reservas_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/reservas"
)

// ─────────────────────────── ALTO 2 ─────────────────────────────────

// TestSinalPagoNaoPassaDoTotalDaReserva.
//
// MEDIDO ANTES: reserva de total_cents=192000 aceitou
// `{"deposit_paid_cents": 96000000}` (500× o total) com 200, e o /cancel
// seguinte devolveu `refund_cents: 96000000` — uma ordem de devolução de
// R$ 960.000 numa venda de R$ 1.920, com o razão concordando. Um dígito a mais
// numa tela, ou um insider, e a devolução é a saída de caixa.
func TestSinalPagoNaoPassaDoTotalDaReserva(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)

	reserva := a.criarReserva(t, token, pedido(a.produto(t, "apto-2s"), contato, a.dia(600), a.dia(603), 2))
	base := "/reservations/" + reserva.ID.String()

	casos := []struct {
		nome string
		pago int64
	}{
		{"acima do total", reserva.Total * 500},
		{"um centavo acima do total", reserva.Total + 1},
		{"zero", 0},
		{"negativo", -1},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			chave := "it-sinal-" + sufixo() + "-" + sufixo()
			a.limparChaves(t, chave)
			r := a.chamarIdem(t, http.MethodPost, base+"/confirm", token,
				map[string]any{"deposit_paid_cents": c.pago}, chave)
			if r.Status != http.StatusUnprocessableEntity {
				t.Fatalf("confirm com deposit_paid_cents=%d = %d, esperado 422 (%s)", c.pago, r.Status, r.Corpo)
			}
			if r.codigoDoErro() != "VALIDATION_ERROR" {
				t.Fatalf("code = %q (%s)", r.codigoDoErro(), r.Corpo)
			}
			if a.statusDaReserva(t, reserva.ID) != reservas.EstadoHold {
				t.Fatal("a reserva mudou de estado apesar do 422")
			}
		})
	}

	// Pagar a estadia INTEIRA adiantada é legítimo — o teto é o total, não o sinal.
	t.Run("pagamento integral é aceito", func(t *testing.T) {
		a.confirmar(t, token, reserva.ID, reserva.Total)
	})

	// E a devolução tem como base o que entrou, nunca mais que isso.
	t.Run("a devolução não passa do que foi pago", func(t *testing.T) {
		r := a.chamar(t, http.MethodPost, base+"/cancel?dry_run=1", token, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("cancel dry_run = %d (%s)", r.Status, r.Corpo)
		}
		previa := dado[previaDoCancelamento](t, r)
		if previa.SinalPago != reserva.Total {
			t.Fatalf("deposit_paid_cents = %d, esperado %d", previa.SinalPago, reserva.Total)
		}
		if previa.Devolucao > previa.SinalPago || previa.Devolucao+previa.Retido != previa.SinalPago {
			t.Fatalf("devolução %d + retido %d ≠ pago %d", previa.Devolucao, previa.Retido, previa.SinalPago)
		}
	})
}

type previaDoCancelamento struct {
	Rotulo    string `json:"label"`
	Devolucao int64  `json:"refund_cents"`
	Retido    int64  `json:"retained_cents"`
	SinalPago int64  `json:"deposit_paid_cents"`
}

// ─────────────────────────── MÉDIO 8 ────────────────────────────────

// TestCheckInERecusadoForaDoPeriodoDaReserva.
//
// MEDIDO ANTES: reserva de 2031, `POST /check-in` hoje (2026) → 200; e o
// `/check-out` logo depois → 200. Dois cliques devolveram ao estoque uma venda
// confirmada e FUTURA sem passar por /cancel — sem política de cancelamento,
// sem retenção, sem registro de cancelamento no razão.
func TestCheckInERecusadoForaDoPeriodoDaReserva(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)

	// A estadia é daqui a quase dois anos; hoje não é dia de ninguém entrar.
	reserva := a.criarReserva(t, token, pedido(a.produto(t, "apto-2s"), contato, a.dia(700), a.dia(704), 2))
	base := "/reservations/" + reserva.ID.String()
	a.confirmar(t, token, reserva.ID, 100000)

	t.Run("hoje, com a estadia daqui a dois anos", func(t *testing.T) {
		r := a.chamar(t, http.MethodPost, base+"/check-in", token, nil)
		if r.Status != http.StatusUnprocessableEntity {
			t.Fatalf("check-in fora do período = %d, esperado 422 (%s)", r.Status, r.Corpo)
		}
		if a.statusDaReserva(t, reserva.ID) != reservas.EstadoConfirmada {
			t.Fatal("a reserva saiu de confirmed apesar do 422")
		}
	})

	t.Run("`at` explícito antes do check-in", func(t *testing.T) {
		r := a.chamar(t, http.MethodPost, base+"/check-in", token,
			map[string]any{"at": a.dia(699) + "T10:00:00-03:00"})
		if r.Status != http.StatusUnprocessableEntity {
			t.Fatalf("check-in na véspera = %d, esperado 422 (%s)", r.Status, r.Corpo)
		}
	})

	t.Run("`at` no dia do check-out (o intervalo é half-open)", func(t *testing.T) {
		r := a.chamar(t, http.MethodPost, base+"/check-in", token,
			map[string]any{"at": a.dia(704) + "T10:00:00-03:00"})
		if r.Status != http.StatusUnprocessableEntity {
			t.Fatalf("check-in na diária que ninguém comprou = %d, esperado 422 (%s)", r.Status, r.Corpo)
		}
	})

	t.Run("`at` dentro do período é aceito e o check-out fecha na saída", func(t *testing.T) {
		entrada := a.dia(700) + "T15:00:00-03:00"
		r := a.chamar(t, http.MethodPost, base+"/check-in", token, map[string]any{"at": entrada})
		if r.Status != http.StatusOK {
			t.Fatalf("check-in no dia da chegada = %d (%s)", r.Status, r.Corpo)
		}

		// Saída antes da entrada é impossível, e o /check-out precisa dizer isso.
		antes := a.chamar(t, http.MethodPost, base+"/check-out", token,
			map[string]any{"at": a.dia(700) + "T09:00:00-03:00"})
		if antes.Status != http.StatusUnprocessableEntity {
			t.Fatalf("check-out ANTES do check-in = %d, esperado 422 (%s)", antes.Status, antes.Corpo)
		}

		// O teto do check-out é INCLUSIVO: o hóspede sai na manhã do primeiro
		// dia que não foi vendido a ele.
		saida := a.chamar(t, http.MethodPost, base+"/check-out", token,
			map[string]any{"at": a.dia(704) + "T11:00:00-03:00"})
		if saida.Status != http.StatusOK {
			t.Fatalf("check-out no dia da saída = %d (%s)", saida.Status, saida.Corpo)
		}
	})
}

// ─────────────────────────── MÉDIO 7 ────────────────────────────────

// TestCheckOutPreservaAEstadiaNoHistorico.
//
// MEDIDO ANTES: `/check-out` marcava os blocos como `cancelled`, e o mapa
// (`GET /availability/units`) devolvia aqueles dias como `livre`,
// `reservation_code: null`. A estadia CUMPRIDA evaporava — e ficava byte a byte
// igual a uma venda que o hóspede cancelou sem nunca chegar. Nenhuma consulta
// separava receita realizada de venda perdida, e toda taxa de ocupação
// calculada sobre `stay_blocks` subestimava, sempre e sem rastro.
func TestCheckOutPreservaAEstadiaNoHistorico(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)
	apto := a.produto(t, "apto-2s")

	de, ate := a.dia(0), a.dia(3)
	reserva := a.criarReserva(t, token, pedido(apto, contato, de, ate, 2))
	base := "/reservations/" + reserva.ID.String()
	a.confirmar(t, token, reserva.ID, 100000)

	if r := a.chamar(t, http.MethodPost, base+"/check-in", token, nil); r.Status != http.StatusOK {
		t.Fatalf("check-in = %d (%s)", r.Status, r.Corpo)
	}
	if r := a.chamar(t, http.MethodPost, base+"/check-out", token, nil); r.Status != http.StatusOK {
		t.Fatalf("check-out = %d (%s)", r.Status, r.Corpo)
	}

	blocos := a.statusDosBlocos(t, reserva.ID)
	if blocos[reservas.BlocoConcluido] != 1 {
		t.Fatalf("blocos depois do check-out = %v, esperado 1 em %q — a estadia sumiu do histórico",
			blocos, reservas.BlocoConcluido)
	}
	if blocos[reservas.BlocoCancelado] != 0 {
		t.Fatalf("a estadia consumada ficou indistinguível de venda cancelada: %v", blocos)
	}

	// O predicado do MAPA (hold+confirmed+completed) enxerga a estadia...
	if n := a.noitesOcupadasNoMapa(t, reserva.ID); n != 3 {
		t.Fatalf("noites visíveis no mapa = %d, esperado 3", n)
	}
	// ...e o predicado do INVENTÁRIO (hold+confirmed) não: a data volta a vender.
	a.criarReserva(t, token, pedido(apto, contato, de, ate, 2))
}

// noitesOcupadasNoMapa conta as noites que o predicado de EXIBIÇÃO enxerga —
// o mesmo `hold, confirmed, completed` do índice `stay_blocks_ocupacao_idx`.
func (a *ambiente) noitesOcupadasNoMapa(t *testing.T, reserva uuid.UUID) int {
	t.Helper()

	var n int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT COALESCE(sum(upper(period) - lower(period)), 0)
		  FROM stay_blocks
		 WHERE reservation_id = $1 AND status IN ('hold','confirmed','completed')`, reserva).Scan(&n); err != nil {
		t.Fatalf("contando noites no mapa: %v", err)
	}
	return n
}

// Bloco `completed` é terminal: liberá-lo pela rota de calendário seria dizer
// que a estadia não aconteceu.
func TestBlocoConcluidoNaoSeLibera(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)

	sp := a.unidade(t, "SP-03")
	de, ate := a.dia(760), a.dia(763)
	resp := a.chamar(t, http.MethodPost, "/blocks", token, map[string]any{
		"unit_ids": []uuid.UUID{sp}, "from": de, "to": ate, "source": "maintenance",
	})
	if resp.Status != http.StatusCreated {
		t.Fatalf("POST /blocks = %d (%s)", resp.Status, resp.Corpo)
	}
	bloco := dado[[]struct {
		ID uuid.UUID `json:"id"`
	}](t, resp)[0].ID
	t.Cleanup(func() { a.executar(t, `DELETE FROM stay_blocks WHERE id = $1`, bloco) })

	a.executar(t, `UPDATE stay_blocks SET status = 'completed' WHERE id = $1`, bloco)

	r := a.chamar(t, http.MethodDelete, "/blocks/"+bloco.String(), token, nil)
	if r.Status != http.StatusConflict || r.codigoDoErro() != "INVALID_STATE_TRANSITION" {
		t.Fatalf("DELETE de bloco terminal = %d/%s (%s)", r.Status, r.codigoDoErro(), r.Corpo)
	}
}

// TestBloqueioDeOrdemDeManutencaoNaoSeLiberaPelaRotaDeCalendario.
//
// O bloqueio de uma ordem de manutenção só se solta pela ordem
// (`DELETE /maintenance-orders/{id}/block` ou o encerramento dela). Soltá-lo
// por `DELETE /blocks/{id}` deixaria a ordem dizendo "bloqueado até sexta"
// com a unidade à venda — e o conserto em curso receberia hóspede.
func TestBloqueioDeOrdemDeManutencaoNaoSeLiberaPelaRotaDeCalendario(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)

	sp := a.unidade(t, "SP-03")
	resp := a.chamar(t, http.MethodPost, "/blocks", token, map[string]any{
		"unit_ids": []uuid.UUID{sp}, "from": a.dia(770), "to": a.dia(773), "source": "maintenance",
	})
	if resp.Status != http.StatusCreated {
		t.Fatalf("POST /blocks = %d (%s)", resp.Status, resp.Corpo)
	}
	bloco := dado[[]struct {
		ID uuid.UUID `json:"id"`
	}](t, resp)[0].ID
	t.Cleanup(func() { a.executar(t, `DELETE FROM stay_blocks WHERE id = $1`, bloco) })

	// A ordem entra pelo banco: este módulo só precisa saber que ela existe.
	var ordem uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO maintenance_orders (property_id, unit_id, title, stay_block_id)
		VALUES ($1, $2, 'Pintura da fachada', $3) RETURNING id`, a.propriedade, sp, bloco).Scan(&ordem); err != nil {
		t.Fatalf("criando a ordem: %v", err)
	}
	// Registrada depois da do bloqueio, roda ANTES dela: a FK é RESTRICT.
	t.Cleanup(func() { a.executar(t, `DELETE FROM maintenance_orders WHERE id = $1`, ordem) })

	r := a.chamar(t, http.MethodDelete, "/blocks/"+bloco.String(), token, nil)
	if r.Status != http.StatusConflict || r.codigoDoErro() != "INVALID_STATE_TRANSITION" {
		t.Fatalf("DELETE do bloqueio da ordem = %d/%s (%s)", r.Status, r.codigoDoErro(), r.Corpo)
	}
	if r.detalhes(t)["maintenance_order_id"] != ordem.String() {
		t.Fatalf("details.maintenance_order_id: %s", r.Corpo)
	}
	var status string
	if err := a.pool.QueryRow(a.ctx, `SELECT status FROM stay_blocks WHERE id = $1`, bloco).Scan(&status); err != nil || status != "confirmed" {
		t.Fatalf("o bloqueio da ordem continua ocupando: %s %v", status, err)
	}
}

// TestBloqueioDeManutencaoTerminadoNaoSeLibera.
//
// Bloqueio de manutenção cujo período JÁ TERMINOU (`to <= hoje`, no fuso da
// casa) é o histórico do conserto — inclusive quando nenhuma ordem o cita
// mais (o antigo de uma remarcação) — e noite que já passou não muda: 409 com
// `details.period`, e a linha continua `confirmed`. Bloqueio de OUTRA origem
// terminado continua liberável, como sempre foi.
func TestBloqueioDeManutencaoTerminadoNaoSeLibera(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	sp := a.unidade(t, "SP-03")

	criar := func(origem string, de, ate int) uuid.UUID {
		t.Helper()
		resp := a.chamar(t, http.MethodPost, "/blocks", token, map[string]any{
			"unit_ids": []uuid.UUID{sp}, "from": a.dia(de), "to": a.dia(ate), "source": origem,
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("POST /blocks (%s) = %d (%s)", origem, resp.Status, resp.Corpo)
		}
		id := dado[[]struct {
			ID uuid.UUID `json:"id"`
		}](t, resp)[0].ID
		t.Cleanup(func() { a.executar(t, `DELETE FROM stay_blocks WHERE id = $1`, id) })
		return id
	}

	manutencao := criar("maintenance", -40, -37)
	r := a.chamar(t, http.MethodDelete, "/blocks/"+manutencao.String(), token, nil)
	if r.Status != http.StatusConflict || r.codigoDoErro() != "INVALID_STATE_TRANSITION" {
		t.Fatalf("DELETE do bloqueio de manutenção terminado = %d/%s (%s)", r.Status, r.codigoDoErro(), r.Corpo)
	}
	if p := r.detalhes(t)["period"]; p != "["+a.dia(-40)+", "+a.dia(-37)+")" {
		t.Fatalf("details.period: %s", r.Corpo)
	}
	var status string
	if err := a.pool.QueryRow(a.ctx, `SELECT status FROM stay_blocks WHERE id = $1`, manutencao).Scan(&status); err != nil || status != "confirmed" {
		t.Fatalf("o histórico continua confirmed: %s %v", status, err)
	}

	// Terminando HOJE (`to` = hoje) também já terminou: a noite de hoje não é dele.
	ateHoje := criar("maintenance", -3, 0)
	if r := a.chamar(t, http.MethodDelete, "/blocks/"+ateHoje.String(), token, nil); r.Status != http.StatusConflict {
		t.Fatalf("bloqueio de manutenção com to = hoje: %d (%s)", r.Status, r.Corpo)
	}
	// Ainda não terminou: libera.
	vivo := criar("maintenance", 790, 792)
	if r := a.chamar(t, http.MethodDelete, "/blocks/"+vivo.String(), token, nil); r.Status != http.StatusNoContent {
		t.Fatalf("bloqueio de manutenção em curso continua liberável: %d (%s)", r.Status, r.Corpo)
	}
	// Outra origem, terminado: continua como era.
	proprietario := criar("owner_hold", -36, -34)
	if r := a.chamar(t, http.MethodDelete, "/blocks/"+proprietario.String(), token, nil); r.Status != http.StatusNoContent {
		t.Fatalf("uso do proprietário terminado continua liberável: %d (%s)", r.Status, r.Corpo)
	}
}

// ─────────────────────────── BAIXO 9 ────────────────────────────────

// TestRealocarRecusaHoldVencido.
//
// MEDIDO ANTES: `Confirmar` e `EstenderHold` recusavam com HOLD_EXPIRED;
// `Realocar` não. Um hold vencido migrou de AP-01 para AP-02 e voltou a
// bloquear o calendário — ressuscitado por uma ação que nem devia enxergá-lo.
// As três ações precisam concordar sobre o que é um hold vivo.
func TestRealocarRecusaHoldVencido(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)

	reserva := a.criarReserva(t, token, pedido(a.produto(t, "apto-2s"), contato, a.dia(800), a.dia(803), 2))
	a.vencerHold(t, reserva.ID)

	destino := reserva.Unidades[0].UnitID
	for _, codigo := range []string{"AP-01", "AP-02", "AP-03"} {
		if id := a.unidade(t, codigo); id != reserva.Unidades[0].UnitID {
			destino = id
			break
		}
	}

	r := a.chamar(t, http.MethodPost, "/reservations/"+reserva.ID.String()+"/reassign-unit", token,
		map[string]any{"to_unit_id": destino})
	if r.Status != http.StatusConflict || r.codigoDoErro() != "HOLD_EXPIRED" {
		t.Fatalf("realocar hold vencido = %d/%s, esperado 409/HOLD_EXPIRED (%s)", r.Status, r.codigoDoErro(), r.Corpo)
	}
}

// vencerHold empurra o vencimento para o passado, como o relógio faria.
func (a *ambiente) vencerHold(t *testing.T, reserva uuid.UUID) {
	t.Helper()
	if _, err := a.pool.Exec(a.ctx, `
		UPDATE reservations SET hold_expires_at = now() - interval '1 minute' WHERE id = $1`, reserva); err != nil {
		t.Fatalf("vencendo o hold: %v", err)
	}
}

// ─────────────────────────── MÉDIO 6 ────────────────────────────────

// TestIdempotenciaNaoVazaEntreUsuarios.
//
// MEDIDO ANTES: PK `(key, endpoint)`. O admin criou WH-2026-0004 com
// `Idempotency-Key: reserva-1`; o corretor mandou a MESMA chave e recebeu 200
// com a reserva DO ADMIN inteira — id, código, unidades, stay_block_id. A mesma
// reserva que o `GET /reservations/{id}` devolve a ele como 404, porque o
// escopo dele é `own`. O cache de idempotência entregava pelo POST o que o GET
// protegia.
func TestIdempotenciaNaoVazaEntreUsuarios(t *testing.T) {
	a := subir(t)
	gestor := a.gestor(t)
	contato := a.contato(t)
	// `cobertura` tem UMA unidade: com ela, a segunda tentativa nas mesmas datas
	// tem um único desfecho possível (409), e o teste não depende de o produto
	// ter apartamento sobrando.
	cobertura := a.produto(t, "cobertura")

	corretor := a.token(t, a.perfilComEscopo(t, "res_corretor_idem", "own",
		"reservations:ver", "reservations:criar", "reservations:editar", "quotes:criar"))

	chave := "it-vaza-" + sufixo() + "-" + sufixo()
	a.limparChaves(t, chave)

	corpo := pedido(cobertura, contato, a.dia(820), a.dia(823), 2)
	doGestor := a.chamarIdem(t, http.MethodPost, "/reservations", gestor, corpo, chave)
	if doGestor.Status != http.StatusCreated {
		t.Fatalf("POST do gestor = %d (%s)", doGestor.Status, doGestor.Corpo)
	}
	primeira := dado[reservaVista](t, doGestor)

	// O corretor reapresenta a MESMA chave com o MESMO corpo. A data já está
	// ocupada pela reserva do gestor, então o correto é 409 — e nunca a
	// resposta do gestor.
	doCorretor := a.chamarIdem(t, http.MethodPost, "/reservations", corretor, corpo, chave)
	if doCorretor.Status == http.StatusCreated || doCorretor.Status == http.StatusOK {
		vista := dado[reservaVista](t, doCorretor)
		if vista.ID == primeira.ID {
			t.Fatalf("o corretor recebeu a reserva do gestor pelo replay: %s (%s)", vista.Codigo, doCorretor.Corpo)
		}
		t.Fatalf("o replay do corretor devolveu %d inesperado (%s)", doCorretor.Status, doCorretor.Corpo)
	}
	if doCorretor.Status != http.StatusConflict || doCorretor.codigoDoErro() != "DATE_CONFLICT" {
		t.Fatalf("replay do corretor = %d/%s, esperado 409/DATE_CONFLICT (%s)",
			doCorretor.Status, doCorretor.codigoDoErro(), doCorretor.Corpo)
	}

	// A chave é POR ATOR: cada um tem a sua linha.
	// A linha do gestor é DELE: a chave é (key, endpoint, actor_id, property_id).
	// O corretor não gravou porque a transação dele abortou no 409 — e é
	// exatamente esse o comportamento certo: repetir uma chave depois de um erro
	// é tentativa nova, não repetição de sucesso.
	var atores int
	if err := a.pool.QueryRow(a.ctx,
		`SELECT count(DISTINCT actor_id) FROM idempotency_keys WHERE key = $1`, chave).Scan(&atores); err != nil {
		t.Fatal(err)
	}
	if atores != 1 {
		t.Fatalf("atores distintos guardando a chave = %d, esperado 1", atores)
	}

	// E o replay do PRÓPRIO gestor continua devolvendo a resposta dele.
	repetido := a.chamarIdem(t, http.MethodPost, "/reservations", gestor, corpo, chave)
	if repetido.Status != http.StatusCreated {
		t.Fatalf("replay do próprio gestor = %d (%s)", repetido.Status, repetido.Corpo)
	}
	if !mesmoJSON(t, doGestor.Corpo, repetido.Corpo) {
		t.Fatal("o replay do gestor não devolveu a resposta original")
	}
}

// ─────────────────────────── MÉDIO 5 ────────────────────────────────

// TestBloqueioTemDonoEEscopoOwnFiltraNoSQL.
//
// MEDIDO ANTES: `CriarBloqueio` nunca chamava `somenteMinhas`, e
// `LiberarBloqueio` filtrava só por `property_id`. Com a matriz corrigida
// (corretor com `calendar` nas quatro ações, escopo `own`), sem filtro por dono
// o `own` degradaria para `all`: o corretor apagaria bloqueio alheio — pior que
// a assimetria original.
func TestBloqueioTemDonoEEscopoOwnFiltraNoSQL(t *testing.T) {
	a := subir(t)
	gestor := a.gestor(t)

	corretor := a.token(t, a.perfilComEscopo(t, "res_corretor_cal", "own",
		"calendar:ver", "calendar:criar", "calendar:excluir"))
	outro := a.token(t, a.perfilComEscopo(t, "res_corretor_cal2", "own",
		"calendar:ver", "calendar:criar", "calendar:excluir"))

	criar := func(t *testing.T, token, unidade string, de, ate string) uuid.UUID {
		t.Helper()
		r := a.chamar(t, http.MethodPost, "/blocks", token, map[string]any{
			"unit_ids": []uuid.UUID{a.unidade(t, unidade)},
			"from":     de, "to": ate, "source": "maintenance",
		})
		if r.Status != http.StatusCreated {
			t.Fatalf("POST /blocks = %d (%s)", r.Status, r.Corpo)
		}
		id := dado[[]struct {
			ID uuid.UUID `json:"id"`
		}](t, r)[0].ID
		t.Cleanup(func() { a.executar(t, `DELETE FROM stay_blocks WHERE id = $1`, id) })
		return id
	}

	meu := criar(t, corretor, "SP-01", a.dia(840), a.dia(842))

	// O bloqueio nasce COM dono — sem isso o escopo `own` não tem por onde
	// virar SQL.
	var dono *uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `SELECT owner_id FROM stay_blocks WHERE id = $1`, meu).Scan(&dono); err != nil {
		t.Fatal(err)
	}
	if dono == nil {
		t.Fatal("o bloqueio nasceu sem owner_id: o escopo `own` não filtra nada")
	}

	// Quem cria pode desfazer — o par criar/excluir é simétrico.
	if r := a.chamar(t, http.MethodDelete, "/blocks/"+meu.String(), corretor, nil); r.Status != http.StatusNoContent {
		t.Fatalf("DELETE do próprio bloqueio = %d (%s) — cadeado sem chave", r.Status, r.Corpo)
	}

	// Bloqueio de outro dono responde 404, e não 403: 403 confirmaria que ele
	// existe, pela mesma razão que a reserva alheia dá 404.
	alheio := criar(t, outro, "SP-02", a.dia(845), a.dia(847))
	if r := a.chamar(t, http.MethodDelete, "/blocks/"+alheio.String(), corretor, nil); r.Status != http.StatusNotFound {
		t.Fatalf("DELETE de bloqueio alheio com escopo own = %d, esperado 404 (%s)", r.Status, r.Corpo)
	}
	// E continua lá.
	var status string
	if err := a.pool.QueryRow(a.ctx, `SELECT status FROM stay_blocks WHERE id = $1`, alheio).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != reservas.BlocoConfirmado {
		t.Fatalf("o bloqueio alheio virou %q — o escopo own degradou para all", status)
	}

	// Escopo `all` continua alcançando o de qualquer um.
	if r := a.chamar(t, http.MethodDelete, "/blocks/"+alheio.String(), gestor, nil); r.Status != http.StatusNoContent {
		t.Fatalf("DELETE com escopo all = %d (%s)", r.Status, r.Corpo)
	}
}

// O bloco de RESERVA herda o dono da venda, não de quem apertou o botão.
func TestBlocoDeReservaHerdaODonoDaVenda(t *testing.T) {
	a := subir(t)
	gestor := a.gestor(t)
	contato := a.contato(t)

	perfil := a.perfilComEscopo(t, "res_corretor_dono", "own",
		"reservations:ver", "reservations:criar", "reservations:editar", "quotes:criar")
	dono, token := a.usuario(t, perfil)

	reserva := a.criarReserva(t, token, pedido(a.produto(t, "apto-2s"), contato, a.dia(860), a.dia(863), 2))
	// Quem confirma é OUTRA pessoa — é o cenário que mostra por que `created_by`
	// não serve de dono.
	a.confirmar(t, gestor, reserva.ID, 100000)

	var donos []uuid.UUID
	linhas, err := a.pool.Query(a.ctx, `SELECT owner_id FROM stay_blocks WHERE reservation_id = $1`, reserva.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer linhas.Close()
	for linhas.Next() {
		var d *uuid.UUID
		if err := linhas.Scan(&d); err != nil {
			t.Fatal(err)
		}
		if d == nil {
			t.Fatal("bloco de reserva sem owner_id")
		}
		donos = append(donos, *d)
	}
	if len(donos) != 1 || donos[0] != dono {
		t.Fatalf("owner_id dos blocos = %v, esperado o dono da venda %v", donos, dono)
	}
}

// ─────────────────────────── ALTO 3 ─────────────────────────────────

// TestToda EscritaDeixaTrilhaEmAuditLog.
//
// MEDIDO ANTES: uma execução da suíte de integração produziu 1626 tuplas
// escritas nas tabelas de negócio e ZERO linhas em `audit_log`. Sem elas não há
// resposta para "quem cancelou esta reserva?" nem para "quem bloqueou a casa
// inteira?" — e a spec §16 promete que existe.
func TestTodaEscritaDeixaTrilhaEmAuditLog(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)
	apto := a.produto(t, "apto-2s")

	reserva := a.criarReserva(t, token, pedido(apto, contato, a.dia(880), a.dia(883), 2))
	base := "/reservations/" + reserva.ID.String()
	t.Cleanup(func() { a.executar(t, `DELETE FROM audit_log WHERE entity_id = $1`, reserva.ID) })

	a.confirmar(t, token, reserva.ID, 100000)

	if r := a.chamar(t, http.MethodPatch, base, token, map[string]any{"guests_count": 3}); r.Status != http.StatusOK {
		t.Fatalf("PATCH = %d (%s)", r.Status, r.Corpo)
	}
	if r := a.chamar(t, http.MethodPost, base+"/reassign-unit", token,
		map[string]any{"to_unit_id": a.outraUnidadeDoProduto(t, apto, reserva.Unidades[0].UnitID)}); r.Status != http.StatusOK {
		t.Fatalf("reassign-unit = %d (%s)", r.Status, r.Corpo)
	}
	if r := a.chamar(t, http.MethodPost, base+"/cancel", token, nil); r.Status != http.StatusOK {
		t.Fatalf("cancel = %d (%s)", r.Status, r.Corpo)
	}

	acoes := a.acoesAuditadas(t, reserva.ID)
	for _, esperada := range []string{
		"reservations.criado", "reservations.confirmada",
		"reservations.alterado", "reservations.realocada", "reservations.cancelada",
	} {
		if !acoes[esperada] {
			t.Errorf("audit_log sem %q — as ações gravadas foram %v", esperada, chaves(acoes))
		}
	}

	// A trilha guarda ATOR e ANTES/DEPOIS: sem os dois ela não responde nada.
	var (
		ator  *uuid.UUID
		antes []byte
		req   *string
	)
	if err := a.pool.QueryRow(a.ctx, `
		SELECT actor_id, before, request_id FROM audit_log
		 WHERE entity_id = $1 AND action = 'reservations.cancelada'`, reserva.ID).Scan(&ator, &antes, &req); err != nil {
		t.Fatalf("lendo a linha do cancelamento: %v", err)
	}
	if ator == nil {
		t.Error("a linha de cancelamento não tem ator")
	}
	if len(antes) == 0 {
		t.Error("a linha de cancelamento não tem `before`: não dá para saber de que estado ela saiu")
	}
}

func (a *ambiente) acoesAuditadas(t *testing.T, entidade uuid.UUID) map[string]bool {
	t.Helper()

	linhas, err := a.pool.Query(a.ctx, `SELECT action FROM audit_log WHERE entity_id = $1`, entidade)
	if err != nil {
		t.Fatalf("lendo audit_log: %v", err)
	}
	defer linhas.Close()

	out := map[string]bool{}
	for linhas.Next() {
		var acao string
		if err := linhas.Scan(&acao); err != nil {
			t.Fatal(err)
		}
		out[acao] = true
	}
	return out
}

func chaves(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func (a *ambiente) outraUnidadeDoProduto(t *testing.T, produto, atual uuid.UUID) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		SELECT m.unit_id FROM unit_type_members m JOIN units u ON u.id = m.unit_id AND u.active
		 WHERE m.unit_type_id = $1 AND m.unit_id <> $2 ORDER BY u.code LIMIT 1`, produto, atual).Scan(&id); err != nil {
		t.Fatalf("outra unidade do produto: %v", err)
	}
	return id
}

// ─────────────────────────── Contrato ───────────────────────────────

// O contrato passou a chamar o campo de `guests_count` em toda a superfície —
// era `guests` na criação e `guests_count` na resposta e no PATCH, e a revisão
// mediu o resultado: `POST` com `guests_count` dava 422 "guests é obrigatório",
// e um `PATCH {"guests": 6}` respondia 200 sem mudar nada.
func TestCriacaoUsaGuestsCount(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)

	corpo := map[string]any{
		"unit_type_id": a.produto(t, "apto-2s"),
		"contact_id":   contato,
		"check_in":     a.dia(900),
		"check_out":    a.dia(903),
		"guests_count": 4,
	}
	chave := "it-gc-" + sufixo() + "-" + sufixo()
	a.limparChaves(t, chave)

	r := a.chamarIdem(t, http.MethodPost, "/reservations", token, corpo, chave)
	if r.Status != http.StatusCreated {
		t.Fatalf("POST com guests_count = %d (%s)", r.Status, r.Corpo)
	}
	var vista struct {
		Data struct {
			ID       uuid.UUID `json:"id"`
			Hospedes int       `json:"guests_count"`
		} `json:"data"`
	}
	decodificar(t, r, &vista)
	if vista.Data.Hospedes != 4 {
		t.Fatalf("guests_count = %d, esperado 4", vista.Data.Hospedes)
	}
}
