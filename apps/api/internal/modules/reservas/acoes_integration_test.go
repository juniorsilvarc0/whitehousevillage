//go:build integration

package reservas_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/reservas"
)

// ─────────────────────────── 5. Cancelamento ────────────────────────

type resultadoDoCancelamento struct {
	Rotulo        string `json:"label"`
	Devolucao     int64  `json:"refund_cents"`
	Retido        int64  `json:"retained_cents"`
	SinalPago     int64  `json:"deposit_paid_cents"`
	Antecedencia  int    `json:"days_before"`
	PolicyVersion int    `json:"policy_version"`
	Simulado      bool   `json:"dry_run"`
	Status        string `json:"status"`
}

// confirmar registra o sinal e devolve a reserva confirmada.
func (a *ambiente) confirmar(t *testing.T, token string, reserva uuid.UUID, pago int64) reservaVista {
	t.Helper()

	chave := "it-conf-" + sufixo() + "-" + sufixo()
	a.limparChaves(t, chave)
	resp := a.chamarIdem(t, http.MethodPost, "/reservations/"+reserva.String()+"/confirm", token,
		map[string]any{"deposit_paid_cents": pago, "method": "pix"}, chave)
	if resp.Status != http.StatusOK {
		t.Fatalf("POST /confirm = %d (%s)", resp.Status, resp.Corpo)
	}
	return dado[reservaVista](t, resp)
}

// O cancelamento aplica a política CONGELADA na reserva, não a vigente hoje — e
// o `?dry_run=1` mostra exatamente o mesmo número que a execução vai gravar.
func TestCancelamentoAplicaAPoliticaCongeladaEODryRunBate(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	apto := a.produto(t, "apto-2s")

	t.Run("mais de 30 dias devolve o sinal inteiro", func(t *testing.T) {
		contato := a.contato(t)
		reserva := a.criarReserva(t, token, pedido(apto, contato, a.dia(40), a.dia(44), 2))
		a.confirmar(t, token, reserva.ID, 100000)

		caminho := "/reservations/" + reserva.ID.String() + "/cancel"
		simulado := dado[resultadoDoCancelamento](t,
			a.chamar(t, http.MethodPost, caminho+"?dry_run=1", token, map[string]any{"reason": "desistencia"}))

		if !simulado.Simulado {
			t.Error("dry_run = false numa simulação")
		}
		if simulado.Devolucao != 100000 || simulado.Retido != 0 {
			t.Fatalf("simulação = devolve %d / retém %d, esperado 100000 / 0", simulado.Devolucao, simulado.Retido)
		}
		if simulado.SinalPago != 100000 {
			t.Errorf("base do cálculo = %d, esperado o sinal efetivamente pago", simulado.SinalPago)
		}
		// A simulação não pode ter EXECUTADO nada.
		if got := a.statusDaReserva(t, reserva.ID); got != reservas.EstadoConfirmada {
			t.Fatalf("o dry_run mudou o status para %q", got)
		}

		executado := dado[resultadoDoCancelamento](t,
			a.chamar(t, http.MethodPost, caminho, token, map[string]any{"reason": "desistencia"}))
		if executado.Simulado {
			t.Error("dry_run = true numa execução")
		}
		if executado.Devolucao != simulado.Devolucao || executado.Retido != simulado.Retido || executado.Rotulo != simulado.Rotulo {
			t.Fatalf("execução (%+v) divergiu da simulação (%+v)", executado, simulado)
		}
		if got := a.statusDaReserva(t, reserva.ID); got != reservas.EstadoCancelada {
			t.Fatalf("status = %q, esperado cancelled", got)
		}
		// A data volta ao estoque e o histórico fica.
		if blocos := a.statusDosBlocos(t, reserva.ID); blocos[reservas.BlocoCancelado] != 1 || blocos[reservas.BlocoConfirmado] != 0 {
			t.Fatalf("blocos = %v, esperado 1 cancelado", blocos)
		}

		// Cancelar de novo é 409: reserva encerrada não se encerra duas vezes.
		repetido := a.chamar(t, http.MethodPost, caminho, token, nil)
		if repetido.Status != http.StatusConflict || repetido.codigoDoErro() != "RESERVATION_NOT_CANCELLABLE" {
			t.Fatalf("segundo cancelamento = %d/%s", repetido.Status, repetido.codigoDoErro())
		}
	})

	t.Run("dez dias de antecedência retém metade", func(t *testing.T) {
		contato := a.contato(t)
		reserva := a.criarReserva(t, token, pedido(apto, contato, a.dia(10), a.dia(14), 2))
		a.confirmar(t, token, reserva.ID, 100000)

		r := dado[resultadoDoCancelamento](t, a.chamar(t, http.MethodPost,
			"/reservations/"+reserva.ID.String()+"/cancel", token, nil))
		if r.Devolucao != 50000 || r.Retido != 50000 {
			t.Fatalf("devolve %d / retém %d, esperado 50000 / 50000 (%q)", r.Devolucao, r.Retido, r.Rotulo)
		}
		if r.Antecedencia != 10 {
			t.Errorf("days_before = %d, esperado 10", r.Antecedencia)
		}
	})

	t.Run("no_show termina em no_show e retém tudo", func(t *testing.T) {
		contato := a.contato(t)
		// Antecedência grande de propósito: mesmo assim o no-show aplica a
		// faixa de MENOR antecedência — quem não apareceu não avisou.
		reserva := a.criarReserva(t, token, pedido(apto, contato, a.dia(60), a.dia(64), 2))
		a.confirmar(t, token, reserva.ID, 100000)

		r := dado[resultadoDoCancelamento](t, a.chamar(t, http.MethodPost,
			"/reservations/"+reserva.ID.String()+"/cancel", token,
			map[string]any{"reason": reservas.MotivoNoShow}))

		if r.Status != reservas.EstadoNoShow {
			t.Fatalf("status = %q, esperado no_show", r.Status)
		}
		if r.Devolucao != 0 || r.Retido != 100000 {
			t.Fatalf("devolve %d / retém %d, esperado 0 / 100000", r.Devolucao, r.Retido)
		}
		if got := a.statusDaReserva(t, reserva.ID); got != reservas.EstadoNoShow {
			t.Fatalf("no banco o status ficou %q — o BI precisa distinguir de `cancelled`", got)
		}
	})

	// A prova de que a política é CONGELADA: publicar uma nova, mais dura, não
	// pode mudar o cancelamento de uma venda já fechada.
	t.Run("politica publicada depois nao alcanca a venda de ontem", func(t *testing.T) {
		contato := a.contato(t)
		reserva := a.criarReserva(t, token, pedido(apto, contato, a.dia(80), a.dia(84), 2))
		a.confirmar(t, token, reserva.ID, 100000)

		var nova uuid.UUID
		if err := a.pool.QueryRow(a.ctx, `
			INSERT INTO cancellation_policies (property_id, version, name, valid_from)
			VALUES ($1, 99, 'Sem devolução (teste)', current_date) RETURNING id`, a.propriedade).Scan(&nova); err != nil {
			t.Fatalf("publicando política nova: %v", err)
		}
		t.Cleanup(func() { a.executar(t, `DELETE FROM cancellation_policies WHERE id = $1`, nova) })
		if _, err := a.pool.Exec(a.ctx, `
			INSERT INTO cancellation_tiers (policy_id, days_before_min, days_before_max, refund_pct, label, sort_order)
			VALUES ($1, NULL, NULL, 0, 'Retenção integral', 1)`, nova); err != nil {
			t.Fatalf("faixa da política nova: %v", err)
		}

		r := dado[resultadoDoCancelamento](t, a.chamar(t, http.MethodPost,
			"/reservations/"+reserva.ID.String()+"/cancel?dry_run=1", token, nil))
		if r.Devolucao != 100000 {
			t.Fatalf("devolução = %d — a política nova alcançou uma venda antiga", r.Devolucao)
		}
		if r.PolicyVersion != 1 {
			t.Fatalf("policy_version = %d, esperado a congelada (1)", r.PolicyVersion)
		}
	})
}

// ─────────────────────────── Remarcação ─────────────────────────────

func TestRemarcarPreservaOHistoricoEInformaADiferenca(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)
	cobertura := a.produto(t, "cobertura")

	original := a.criarReserva(t, token, pedido(cobertura, contato, "2026-11-20", "2026-11-23", 4))
	a.confirmar(t, token, original.ID, 352500)

	chave := "it-remarc-" + sufixo()
	a.limparChaves(t, chave)
	resp := a.chamarIdem(t, http.MethodPost, "/reservations/"+original.ID.String()+"/reschedule", token,
		map[string]any{"check_in": "2026-11-24", "check_out": "2026-11-27"}, chave)

	if resp.Status != http.StatusCreated {
		t.Fatalf("POST /reschedule = %d (%s)", resp.Status, resp.Corpo)
	}

	var env struct {
		Data reservaVista `json:"data"`
		Meta struct {
			Anterior    uuid.UUID `json:"previous_reservation_id"`
			CodAnterior string    `json:"previous_code"`
			TotalAntes  int64     `json:"previous_total_cents"`
			Diferenca   int64     `json:"difference_cents"`
		} `json:"meta"`
	}
	decodificar(t, resp, &env)

	nova := env.Data
	if nova.ID == original.ID {
		t.Fatal("remarcar EDITOU a reserva em vez de criar uma nova")
	}
	if nova.RemarcadaDe == nil || *nova.RemarcadaDe != original.ID.String() {
		t.Fatalf("rebooked_from_id = %v, esperado %s", nova.RemarcadaDe, original.ID)
	}
	// A original vai para cancelled com motivo `remarcacao` — o BI não perde a
	// venda original.
	if got := a.statusDaReserva(t, original.ID); got != reservas.EstadoCancelada {
		t.Fatalf("original ficou em %q, esperado cancelled", got)
	}
	if env.Meta.Anterior != original.ID || env.Meta.CodAnterior != original.Codigo {
		t.Errorf("meta aponta para %v/%q", env.Meta.Anterior, env.Meta.CodAnterior)
	}
	if env.Meta.Diferenca != nova.Total-original.Total {
		t.Errorf("difference_cents = %d, esperado %d", env.Meta.Diferenca, nova.Total-original.Total)
	}
	// 24 (ter), 25 (qua), 26 (qui) são diárias normais: 3 × 1.900 + 350 = 6.050.
	if nova.Total != 605000 {
		t.Errorf("total da nova = %d, esperado 605000", nova.Total)
	}
	// Estado comercial herdado: confirmada gera confirmada, e o sinal acompanha.
	if nova.Status != reservas.EstadoConfirmada {
		t.Fatalf("a nova nasceu em %q, esperado confirmed", nova.Status)
	}
	previa := dado[resultadoDoCancelamento](t, a.chamar(t, http.MethodPost,
		"/reservations/"+nova.ID.String()+"/cancel?dry_run=1", token, nil))
	if previa.SinalPago != 352500 {
		t.Fatalf("o sinal não acompanhou a remarcação: base = %d", previa.SinalPago)
	}

	// A data antiga voltou ao estoque; a nova está travada.
	if blocos := a.statusDosBlocos(t, original.ID); blocos[reservas.BlocoConfirmado] != 0 {
		t.Fatalf("a original ainda segura data: %v", blocos)
	}
	if blocos := a.statusDosBlocos(t, nova.ID); blocos[reservas.BlocoConfirmado] != 1 {
		t.Fatalf("a nova não segurou a data: %v", blocos)
	}
}

// O contrato promete: o DATE_CONFLICT da data nova NÃO deixa a reserva antiga
// cancelada. Ou as duas mudanças valem, ou nenhuma vale.
func TestRemarcacaoRecusadaNaoDeixaAOriginalCancelada(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)
	cobertura := a.produto(t, "cobertura")

	original := a.criarReserva(t, token, pedido(cobertura, contato, a.dia(200), a.dia(204), 2))
	a.confirmar(t, token, original.ID, 100000)

	// Alguém ocupa a data de destino.
	a.bloqueioDireto(t, a.unidade(t, "COB-01"), a.dia(210), a.dia(214))

	chave := "it-remarc-conf-" + sufixo()
	a.limparChaves(t, chave)
	resp := a.chamarIdem(t, http.MethodPost, "/reservations/"+original.ID.String()+"/reschedule", token,
		map[string]any{"check_in": a.dia(210), "check_out": a.dia(214)}, chave)

	if resp.Status != http.StatusConflict || resp.codigoDoErro() != "DATE_CONFLICT" {
		t.Fatalf("remarcação para data ocupada = %d/%s (%s)", resp.Status, resp.codigoDoErro(), resp.Corpo)
	}
	if got := a.statusDaReserva(t, original.ID); got != reservas.EstadoConfirmada {
		t.Fatalf("a original ficou em %q — o rollback não aconteceu", got)
	}
	if blocos := a.statusDosBlocos(t, original.ID); blocos[reservas.BlocoConfirmado] != 1 {
		t.Fatalf("a original perdeu o calendário: %v", blocos)
	}
}

// ─────────────────────────── Check-in e check-out ───────────────────

func TestCheckInMantemOCalendarioECheckOutLibera(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)
	apto := a.produto(t, "apto-2s")

	// A estadia começa HOJE de propósito: check-in e check-out passaram a
	// conferir a data contra o período vendido (o MÉDIO 8 da revisão registrou
	// um check-in de hoje numa reserva de 2031), e uma estadia daqui a um ano
	// não tem check-in legítimo nenhum.
	de, ate := a.dia(0), a.dia(4)
	reserva := a.criarReserva(t, token, pedido(apto, contato, de, ate, 2))
	base := "/reservations/" + reserva.ID.String()

	// Check-in exige sinal: só reserva `confirmed` entra.
	cedo := a.chamar(t, http.MethodPost, base+"/check-in", token, nil)
	if cedo.Status != http.StatusConflict || cedo.codigoDoErro() != "INVALID_STATE_TRANSITION" {
		t.Fatalf("check-in em hold = %d/%s", cedo.Status, cedo.codigoDoErro())
	}
	if permitidos, ok := cedo.detalhes(t)["allowed"].([]any); !ok || len(permitidos) != 1 || permitidos[0] != reservas.EstadoConfirmada {
		t.Errorf("details.allowed = %v, esperado [confirmed]", cedo.detalhes(t)["allowed"])
	}

	a.confirmar(t, token, reserva.ID, 100000)

	dentro := a.chamar(t, http.MethodPost, base+"/check-in", token, map[string]any{"note": "entrou às 15h"})
	if dentro.Status != http.StatusOK {
		t.Fatalf("check-in = %d (%s)", dentro.Status, dentro.Corpo)
	}
	if dado[reservaVista](t, dentro).Status != reservas.EstadoCheckIn {
		t.Fatal("o status não virou checked_in")
	}
	// O hóspede está dentro: a data SEGUE bloqueada.
	if blocos := a.statusDosBlocos(t, reserva.ID); blocos[reservas.BlocoConfirmado] != 1 {
		t.Fatalf("o check-in soltou a data: %v", blocos)
	}

	fora := a.chamar(t, http.MethodPost, base+"/check-out", token, nil)
	if fora.Status != http.StatusOK {
		t.Fatalf("check-out = %d (%s)", fora.Status, fora.Corpo)
	}
	if dado[reservaVista](t, fora).Status != reservas.EstadoCheckOut {
		t.Fatal("o status não virou checked_out")
	}
	// Aqui o calendário libera: é a transição em que a unidade volta ao estoque.
	// Mas a linha vai para `completed`, e não para `cancelled` — a data volta a
	// vender E a estadia continua contando para ocupação, ADR e RevPAR.
	blocos := a.statusDosBlocos(t, reserva.ID)
	if blocos[reservas.BlocoConfirmado] != 0 {
		t.Fatalf("o check-out não liberou a data: %v", blocos)
	}
	if blocos[reservas.BlocoConcluido] != 1 {
		t.Fatalf("a estadia cumprida sumiu do histórico: %v", blocos)
	}
	// E a data realmente vende de novo.
	a.criarReserva(t, token, pedido(apto, contato, de, ate, 2))
}

// ─────────────────────────── Realocação ─────────────────────────────

func TestRealocarTrocaAUnidadeSemMexerEmDataNemPreco(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)
	apto := a.produto(t, "apto-2s")
	de, ate := a.dia(320), a.dia(324)

	reserva := a.criarReserva(t, token, pedido(apto, contato, de, ate, 2))
	origem := reserva.Unidades[0]

	// Destino: outra unidade do MESMO produto.
	destinoCodigo := "AP-02"
	if origem.UnitCode == "AP-02" {
		destinoCodigo = "AP-03"
	}
	destino := a.unidade(t, destinoCodigo)

	resp := a.chamar(t, http.MethodPost, "/reservations/"+reserva.ID.String()+"/reassign-unit", token,
		map[string]any{"to_unit_id": destino, "reason": "pedido do hóspede"})
	if resp.Status != http.StatusOK {
		t.Fatalf("reassign = %d (%s)", resp.Status, resp.Corpo)
	}

	trocada := dado[reservaVista](t, resp)
	if got := trocada.codigosDasUnidades(); len(got) != 1 || got[0] != destinoCodigo {
		t.Fatalf("unidades = %v, esperado [%s]", got, destinoCodigo)
	}
	if !trocada.Unidades[0].Travada {
		t.Error("locked deveria vir true por padrão — a escolha foi da gestão")
	}
	if trocada.Total != reserva.Total || trocada.CheckIn != reserva.CheckIn {
		t.Errorf("realocar mexeu em preço ou data: %d/%s", trocada.Total, trocada.CheckIn)
	}
	// A unidade antiga foi solta e a nova travada — uma linha ativa, uma cancelada.
	if blocos := a.statusDosBlocos(t, reserva.ID); blocos[reservas.BlocoHold] != 1 || blocos[reservas.BlocoCancelado] != 1 {
		t.Fatalf("blocos = %v, esperado 1 em hold e 1 cancelado", blocos)
	}

	t.Run("unidade ocupada e UNIT_NOT_AVAILABLE, nao DATE_CONFLICT", func(t *testing.T) {
		terceira := "AP-03"
		if destinoCodigo == "AP-03" {
			terceira = "AP-01"
		}
		a.bloqueioDireto(t, a.unidade(t, terceira), de, ate)

		resp := a.chamar(t, http.MethodPost, "/reservations/"+reserva.ID.String()+"/reassign-unit", token,
			map[string]any{"to_unit_id": a.unidade(t, terceira)})
		if resp.Status != http.StatusConflict || resp.codigoDoErro() != "UNIT_NOT_AVAILABLE" {
			t.Fatalf("realocar para unidade ocupada = %d/%s", resp.Status, resp.codigoDoErro())
		}
		// A reserva continua exatamente onde estava.
		atual := dado[reservaVista](t, a.chamar(t, http.MethodGet, "/reservations/"+reserva.ID.String(), token, nil))
		if got := atual.codigosDasUnidades(); len(got) != 1 || got[0] != destinoCodigo {
			t.Fatalf("a realocação recusada mexeu na reserva: %v", got)
		}
	})

	t.Run("unidade fora do produto e 422", func(t *testing.T) {
		resp := a.chamar(t, http.MethodPost, "/reservations/"+reserva.ID.String()+"/reassign-unit", token,
			map[string]any{"to_unit_id": a.unidade(t, "SP-01")})
		if resp.Status != http.StatusUnprocessableEntity {
			t.Fatalf("SP-01 não compõe apto-2s: esperado 422, veio %d (%s)", resp.Status, resp.Corpo)
		}
	})

	t.Run("a Completa nao realoca", func(t *testing.T) {
		outroContato := a.contato(t)
		casa := a.criarReserva(t, token, pedido(a.produto(t, "completa"), outroContato, a.dia(340), a.dia(344), 4))
		resp := a.chamar(t, http.MethodPost, "/reservations/"+casa.ID.String()+"/reassign-unit", token,
			map[string]any{"to_unit_id": a.unidade(t, "AP-01")})
		if resp.Status != http.StatusConflict || resp.codigoDoErro() != "INVALID_STATE_TRANSITION" {
			t.Fatalf("realocar a Completa = %d/%s", resp.Status, resp.codigoDoErro())
		}
	})
}

// A alocação escolhe a unidade que MENOS fragmenta o calendário: aquela cuja
// ocupação vizinha termina exatamente no check-in.
func TestAlocacaoPrefereAUnidadeQueEncaixaColada(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)
	apto := a.produto(t, "apto-2s")
	de, ate := a.dia(360), a.dia(364)

	// AP-03 termina EXATAMENTE no check-in: encaixe perfeito (o back-to-back
	// que o intervalo half-open permite). AP-01 e AP-02 estão virgens.
	a.bloqueioDireto(t, a.unidade(t, "AP-03"), a.dia(356), de)

	reserva := a.criarReserva(t, token, pedido(apto, contato, de, ate, 2))
	if got := reserva.codigosDasUnidades(); len(got) != 1 || got[0] != "AP-03" {
		t.Fatalf("unidade alocada = %v, esperado [AP-03] (a que menos fragmenta)", got)
	}

	// E entre duas sobras, vence a MENOR: dois dias de janela desperdiçada em
	// AP-02 contra dez em AP-01. Preservar o bloco contínuo de AP-01 é o que
	// deixa espaço para a próxima estadia longa.
	outro := a.contato(t)
	longe, fimLonge := a.dia(900), a.dia(904)
	a.bloqueioDireto(t, a.unidade(t, "AP-01"), a.dia(886), a.dia(890)) // sobra 10
	a.bloqueioDireto(t, a.unidade(t, "AP-02"), a.dia(894), a.dia(898)) // sobra 2

	escolhida := a.criarReserva(t, token, pedido(apto, outro, longe, fimLonge, 2))
	if got := escolhida.codigosDasUnidades(); len(got) != 1 || got[0] != "AP-02" {
		t.Fatalf("unidade alocada = %v, esperado [AP-02] (sobra de 2 dias contra 10)", got)
	}
}

// ─────────────────────────── Extensão do hold ───────────────────────

func TestEstenderHoldEhAuditadoETemLimite(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)

	reserva := a.criarReserva(t, token, pedido(a.produto(t, "suite-piscina"), contato, a.dia(420), a.dia(424), 2))
	base := "/reservations/" + reserva.ID.String() + "/extend-hold"

	primeira := a.chamar(t, http.MethodPost, base, token, map[string]any{"reason": "cliente pediu prazo"})
	if primeira.Status != http.StatusOK {
		t.Fatalf("extend-hold = %d (%s)", primeira.Status, primeira.Corpo)
	}
	resultado := dado[struct {
		Expira    string `json:"hold_expires_at"`
		Extensoes int    `json:"extensions_count"`
		Max       int    `json:"max_extensions"`
	}](t, primeira)
	if resultado.Extensoes != 1 {
		t.Errorf("extensions_count = %d, esperado 1", resultado.Extensoes)
	}
	if resultado.Max != 1 {
		t.Errorf("max_extensions = %d, esperado o 1 da política semeada", resultado.Max)
	}
	if resultado.Expira == "" || resultado.Expira == valorOuVazio(reserva.HoldExpiraEm) {
		t.Errorf("o prazo não mudou: %q", resultado.Expira)
	}

	// O prazo dos BLOCOS acompanha — senão o job expiraria as linhas de uma
	// pré-reserva que a gestão acabou de estender.
	var desalinhados int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*) FROM stay_blocks sb JOIN reservations r ON r.id = sb.reservation_id
		 WHERE sb.reservation_id = $1 AND sb.status = 'hold'
		   AND sb.expires_at IS DISTINCT FROM r.hold_expires_at`, reserva.ID).Scan(&desalinhados); err != nil {
		t.Fatal(err)
	}
	if desalinhados != 0 {
		t.Fatalf("%d bloco(s) com prazo diferente do da reserva", desalinhados)
	}

	// A segunda estoura o limite: a decisão passa a ser humana.
	segunda := a.chamar(t, http.MethodPost, base, token, nil)
	if segunda.Status != http.StatusConflict || segunda.codigoDoErro() != "HOLD_LIMIT_REACHED" {
		t.Fatalf("segunda extensão = %d/%s", segunda.Status, segunda.codigoDoErro())
	}
	detalhes := segunda.detalhes(t)
	if detalhes["max_extensions"] == nil || detalhes["extensions_count"] == nil {
		t.Errorf("details incompletos: %v", detalhes)
	}

	// Reserva confirmada não tem hold para estender.
	a.confirmar(t, token, reserva.ID, 50000)
	depois := a.chamar(t, http.MethodPost, base, token, nil)
	if depois.Status != http.StatusConflict || depois.codigoDoErro() != "INVALID_STATE_TRANSITION" {
		t.Fatalf("extend-hold em confirmada = %d/%s", depois.Status, depois.codigoDoErro())
	}
}

// ─────────────────────────── Bloqueio operacional ───────────────────

func TestBloqueioOperacionalEhTudoOuNada(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	de, ate := a.dia(440), a.dia(443)

	sp1, sp2 := a.unidade(t, "SP-01"), a.unidade(t, "SP-02")
	resp := a.chamar(t, http.MethodPost, "/blocks", token, map[string]any{
		"unit_ids": []uuid.UUID{sp2, sp1}, // fora de ordem de propósito
		"from":     de, "to": ate, "source": "maintenance", "note": "pintura",
	})
	if resp.Status != http.StatusCreated {
		t.Fatalf("POST /blocks = %d (%s)", resp.Status, resp.Corpo)
	}

	criados := dado[[]struct {
		ID       uuid.UUID `json:"id"`
		UnitCode string    `json:"unit_code"`
		Status   string    `json:"status"`
		De       string    `json:"from"`
		Ate      string    `json:"to"`
	}](t, resp)
	for _, b := range criados {
		id := b.ID
		t.Cleanup(func() { a.executar(t, `DELETE FROM stay_blocks WHERE id = $1`, id) })
	}
	if len(criados) != 2 {
		t.Fatalf("bloqueios criados = %d, esperado 2", len(criados))
	}
	// A resposta sai em ordem de `units.code` — a mesma ordem da inserção.
	if criados[0].UnitCode != "SP-01" || criados[1].UnitCode != "SP-02" {
		t.Fatalf("ordem = %s, %s — esperado por units.code", criados[0].UnitCode, criados[1].UnitCode)
	}
	if criados[0].De != de || criados[0].Ate != ate {
		t.Errorf("período = [%s, %s), esperado [%s, %s)", criados[0].De, criados[0].Ate, de, ate)
	}

	// Bloqueio parcial não existe: SP-02 ocupada derruba as duas linhas.
	sp3 := a.unidade(t, "SP-03")
	conflito := a.chamar(t, http.MethodPost, "/blocks", token, map[string]any{
		"unit_ids": []uuid.UUID{sp3, sp2},
		"from":     de, "to": ate, "source": "owner_hold",
	})
	if conflito.Status != http.StatusConflict || conflito.codigoDoErro() != "DATE_CONFLICT" {
		t.Fatalf("bloqueio sobre unidade ocupada = %d/%s", conflito.Status, conflito.codigoDoErro())
	}
	var sobrou int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*) FROM stay_blocks
		 WHERE unit_id = $1 AND period && daterange($2::date, $3::date, '[)')
		   AND status IN ('hold','confirmed')`, sp3, de, ate).Scan(&sobrou); err != nil {
		t.Fatal(err)
	}
	if sobrou != 0 {
		t.Fatalf("a transação recusada deixou %d linha(s) em SP-03", sobrou)
	}

	// Liberar devolve a data e mantém o registro.
	if r := a.chamar(t, http.MethodDelete, "/blocks/"+criados[0].ID.String(), token, nil); r.Status != http.StatusNoContent {
		t.Fatalf("DELETE /blocks = %d (%s)", r.Status, r.Corpo)
	}
	var status string
	if err := a.pool.QueryRow(a.ctx, `SELECT status FROM stay_blocks WHERE id = $1`, criados[0].ID).Scan(&status); err != nil {
		t.Fatalf("o bloqueio sumiu em vez de ser cancelado: %v", err)
	}
	if status != reservas.BlocoCancelado {
		t.Fatalf("status = %q, esperado cancelled", status)
	}
}

// Bloqueio de reserva não se solta por /blocks: apagá-lo por fora deixaria uma
// reserva confirmada sem calendário, e ninguém veria.
func TestBloqueioDeReservaNaoSeLiberaPelaRotaDeCalendario(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)

	reserva := a.criarReserva(t, token, pedido(a.produto(t, "cobertura"), contato, a.dia(460), a.dia(464), 2))
	if reserva.Unidades[0].Bloco == nil {
		t.Fatal("a reserva nasceu sem stay_block")
	}

	resp := a.chamar(t, http.MethodDelete, "/blocks/"+*reserva.Unidades[0].Bloco, token, nil)
	if resp.Status != http.StatusConflict || resp.codigoDoErro() != "INVALID_STATE_TRANSITION" {
		t.Fatalf("DELETE de bloco de reserva = %d/%s (%s)", resp.Status, resp.codigoDoErro(), resp.Corpo)
	}
}

// ─────────────────────────── Cadastro e descarte ────────────────────

func TestPatchNaoMexeEmDataNemPrecoEDeleteSoApagaQuote(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)

	reserva := a.criarReserva(t, token, pedido(a.produto(t, "apto-2s"), contato, a.dia(480), a.dia(484), 2))
	caminho := "/reservations/" + reserva.ID.String()

	// Campo de data e de preço no corpo é RECUSADO, e não ignorado.
	//
	// Até 26/08/2026 nenhum decoder da API chamava `DisallowUnknownFields`, e
	// `check_in`/`total_cents` num PATCH de reserva respondiam 200 sem efeito
	// nenhum — o corretor via "salvo" na tela e ia embora acreditando ter mudado
	// a data da estadia. Agora vem 422 nomeando o campo, e o motivo original
	// deste bloco fica MAIS forte, não mais fraco: a rota continua não sendo
	// caminho para alterar `stay_blocks` por fora da constraint, e agora diz
	// isso a quem tentou.
	recusado := a.chamar(t, http.MethodPatch, caminho, token, map[string]any{
		"notes": "aniversário de casamento", "guests_count": 4,
		"check_in": a.dia(999), "total_cents": 1,
	})
	if recusado.Status != http.StatusUnprocessableEntity || recusado.codigoDoErro() != "VALIDATION_ERROR" {
		t.Fatalf("PATCH com `check_in`/`total_cents` = %d/%s, esperado 422 VALIDATION_ERROR (%s)",
			recusado.Status, recusado.codigoDoErro(), recusado.Corpo)
	}

	// Controle positivo: o corpo que o DTO declara continua passando — sem ele,
	// uma rota que recusasse TODO PATCH satisfaria a asserção acima.
	resp := a.chamar(t, http.MethodPatch, caminho, token, map[string]any{
		"notes": "aniversário de casamento", "guests_count": 4,
	})
	if resp.Status != http.StatusOK {
		t.Fatalf("PATCH = %d (%s)", resp.Status, resp.Corpo)
	}
	editada := dado[reservaVista](t, resp)
	if editada.CheckIn != reserva.CheckIn || editada.Total != reserva.Total {
		t.Fatalf("PATCH mexeu em data/preço: %s / %d", editada.CheckIn, editada.Total)
	}

	// Capacidade continua sendo do produto e DECLARADA: 7 excede os 6 do apto.
	acima := a.chamar(t, http.MethodPatch, caminho, token, map[string]any{"guests_count": 7})
	if acima.Status != http.StatusUnprocessableEntity || acima.codigoDoErro() != "CAPACITY_EXCEEDED" {
		t.Fatalf("hóspedes acima da capacidade = %d/%s", acima.Status, acima.codigoDoErro())
	}

	// A partir de hold, apagar é 409 — apagar reserva some com a prova de quem
	// segurou a data.
	del := a.chamar(t, http.MethodDelete, caminho, token, nil)
	if del.Status != http.StatusConflict || del.codigoDoErro() != "INVALID_STATE_TRANSITION" {
		t.Fatalf("DELETE de reserva em hold = %d/%s (%s)", del.Status, del.codigoDoErro(), del.Corpo)
	}
}

// ─────────────────────────── RBAC e escopo `own` ────────────────────

func TestPermissaoEEscopoOwn(t *testing.T) {
	a := subir(t)
	gestor := a.gestor(t)
	contato := a.contato(t)
	apto := a.produto(t, "apto-2s")

	t.Run("sem token e 401", func(t *testing.T) {
		if r := a.chamar(t, http.MethodGet, "/reservations", "", nil); r.Status != http.StatusUnauthorized {
			t.Fatalf("sem token = %d", r.Status)
		}
	})

	t.Run("so-leitura nao cria nem edita", func(t *testing.T) {
		leitor := a.token(t, a.perfil(t, "res_leitor", "reservations:ver"))
		if r := a.chamar(t, http.MethodGet, "/reservations", leitor, nil); r.Status != http.StatusOK {
			t.Fatalf("GET com `ver` = %d", r.Status)
		}
		r := a.chamarIdem(t, http.MethodPost, "/reservations", leitor,
			pedido(apto, contato, a.dia(500), a.dia(504), 2), "it-rbac-"+sufixo())
		if r.Status != http.StatusForbidden {
			t.Fatalf("POST sem `criar` = %d (%s)", r.Status, r.Corpo)
		}
	})

	t.Run("quem cuida do calendario nao emite reserva", func(t *testing.T) {
		operacao := a.token(t, a.perfil(t, "res_operacao", "calendar:criar", "calendar:excluir"))
		r := a.chamarIdem(t, http.MethodPost, "/reservations", operacao,
			pedido(apto, contato, a.dia(500), a.dia(504), 2), "it-rbac-"+sufixo())
		if r.Status != http.StatusForbidden {
			t.Fatalf("POST /reservations com só `calendar` = %d", r.Status)
		}
		// Mas bloqueia data, que é o trabalho dele.
		b := a.chamar(t, http.MethodPost, "/blocks", operacao, map[string]any{
			"unit_ids": []uuid.UUID{a.unidade(t, "SP-04")},
			"from":     a.dia(520), "to": a.dia(522), "source": "maintenance",
		})
		if b.Status != http.StatusCreated {
			t.Fatalf("POST /blocks com `calendar:criar` = %d (%s)", b.Status, b.Corpo)
		}
		for _, criado := range dado[[]struct {
			ID uuid.UUID `json:"id"`
		}](t, b) {
			id := criado.ID
			t.Cleanup(func() { a.executar(t, `DELETE FROM stay_blocks WHERE id = $1`, id) })
		}
	})

	t.Run("escopo own filtra no SQL", func(t *testing.T) {
		corretor := a.token(t, a.perfilComEscopo(t, "res_corretor", "own",
			"reservations:ver", "reservations:criar", "reservations:editar", "quotes:criar"))

		minha := a.criarReserva(t, corretor, pedido(apto, contato, a.dia(540), a.dia(544), 2))
		alheia := a.criarReserva(t, gestor, pedido(apto, contato, a.dia(560), a.dia(564), 2))

		// A listagem do corretor traz só a dele — e o `total` acompanha, porque
		// o filtro está no WHERE e não em memória.
		linhas, meta := lista[reservaVista](t, a.chamar(t, http.MethodGet, "/reservations?per_page=100", corretor, nil))
		for _, r := range linhas {
			if r.ID == alheia.ID {
				t.Fatal("o corretor enxergou a reserva de outro dono")
			}
		}
		if meta.Total != int64(len(linhas)) {
			t.Errorf("meta.total = %d mas a página trouxe %d linhas", meta.Total, len(linhas))
		}
		if len(linhas) == 0 {
			t.Fatal("o corretor não enxergou nem a própria reserva")
		}

		// A alheia responde 404, e não 403: 403 confirmaria que ela existe.
		if r := a.chamar(t, http.MethodGet, "/reservations/"+alheia.ID.String(), corretor, nil); r.Status != http.StatusNotFound {
			t.Fatalf("GET da reserva alheia = %d", r.Status)
		}
		// E nem editar por caminho travesso.
		if r := a.chamar(t, http.MethodPost, "/reservations/"+alheia.ID.String()+"/cancel", corretor, nil); r.Status != http.StatusNotFound {
			t.Fatalf("cancelar reserva alheia = %d", r.Status)
		}
		// A própria continua acessível.
		if r := a.chamar(t, http.MethodGet, "/reservations/"+minha.ID.String(), corretor, nil); r.Status != http.StatusOK {
			t.Fatalf("GET da própria reserva = %d", r.Status)
		}
	})
}

// ─────────────────────────── /full ──────────────────────────────────

func TestFullMontaATelaComTimelineEPrevia(t *testing.T) {
	a := subir(t)
	token := a.gestor(t)
	contato := a.contato(t)

	reserva := a.criarReserva(t, token, pedido(a.produto(t, "apto-2s"), contato, a.dia(600), a.dia(604), 2))
	a.confirmar(t, token, reserva.ID, 80000)

	completa := dado[struct {
		Reserva  reservaVista `json:"reservation"`
		Noites   []any        `json:"nights"`
		Linhas   []any        `json:"lines"`
		Unidades []any        `json:"units"`
		Hospedes []struct {
			ContactID uuid.UUID `json:"contact_id"`
			Titular   bool      `json:"is_lead_guest"`
		} `json:"guests"`
		Timeline []struct {
			Tipo string `json:"type"`
		} `json:"timeline"`
		Previa *resultadoDoCancelamento `json:"cancellation_preview"`
	}](t, a.chamar(t, http.MethodGet, "/reservations/"+reserva.ID.String()+"/full", token, nil))

	if len(completa.Noites) != 4 || len(completa.Unidades) != 1 {
		t.Fatalf("noites = %d, unidades = %d", len(completa.Noites), len(completa.Unidades))
	}
	if len(completa.Linhas) == 0 {
		t.Error("as linhas agrupadas vieram vazias")
	}
	if len(completa.Hospedes) != 1 || completa.Hospedes[0].ContactID != contato || !completa.Hospedes[0].Titular {
		t.Errorf("rooming list = %+v", completa.Hospedes)
	}
	// A timeline é append-only e vem do mais recente para o mais antigo.
	if len(completa.Timeline) < 2 || completa.Timeline[0].Tipo != reservas.EventoConfirmada {
		t.Fatalf("timeline = %+v", completa.Timeline)
	}
	if completa.Previa == nil || !completa.Previa.Simulado {
		t.Fatal("cancellation_preview veio vazia numa reserva cancelável")
	}
	if completa.Previa.SinalPago != 80000 {
		t.Errorf("a prévia usou base %d, esperado o sinal pago", completa.Previa.SinalPago)
	}
}
