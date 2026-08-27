//go:build integration

// Os invariantes da Fase 1 que atravessam mais de um módulo, medidos pela porta
// da frente.
//
// O que está aqui e não em `internal/modules/*`: o congelamento do preço é uma
// promessa entre DOIS donos — o tarifário, que muda o preço, e as reservas, que
// prometeram um valor ao hóspede. Cada módulo já prova a sua metade contra o
// próprio banco; nenhum prova que a metade do vizinho continua de pé depois de
// uma alteração feita pela API real. E o desempenho só significa alguma coisa
// com o servidor inteiro no caminho: middleware, RBAC e serialização incluídos.
package router

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// ─────────────────────────── Snapshot ───────────────────────────────────────

// Mudar a tarifa hoje não pode reescrever a venda de ontem.
//
// Em linguagem de negócio: a gestão reajusta a diária da Cobertura de R$ 2.400
// para R$ 3.000. O hóspede que fechou ontem por R$ 7.050 continua devendo
// R$ 7.050 — e quem pedir orçamento hoje ouve o preço novo. Se o valor gravado
// acompanhasse a tabela, todo reajuste viraria uma cobrança retroativa que
// ninguém autorizou.
func TestReajusteDeTarifaNaoAlcancaVendaJaEmitida(t *testing.T) {
	a := subirAPI(t)
	propriedade := exigirSeed(t, a)

	vendedor := a.vendedor(t)
	// Quem mexe no tarifário é outro perfil: `settings`, não `reservations`.
	gestor := a.criarUsuario(t, "tarifas", a.criarPerfil(t, "tarifas", []auth.Permissao{
		{Resource: "settings", Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: "settings", Action: auth.AcaoEditar, Scope: auth.EscopoAll},
	}))

	hospede := a.hospedeDaJornada(t, propriedade)
	cobertura := a.produtoDoSeed(t, propriedade, "cobertura")

	// ── A venda de ontem ──────────────────────────────────────────────────
	reserva := envelopeDe[reservaQA](t,
		a.chamarComChave(t, http.MethodPost, "/reservations", vendedor.Token, map[string]any{
			"unit_type_id": cobertura,
			"contact_id":   hospede,
			"check_in":     jornadaEntrada,
			"check_out":    jornadaSaida,
			"guests_count": 4,
		}, a.chaveNova(t, "snapshot")), http.StatusCreated, "POST /reservations")

	if reserva.Total != jornadaTotal {
		t.Fatalf("a venda saiu por %d, esperado %d — o cenário nem começou certo", reserva.Total, jornadaTotal)
	}

	// ── O reajuste, pela API de verdade ───────────────────────────────────
	//
	// A célula é (tabela vigente × cobertura × fds): as duas noites de fim de
	// semana da estadia.
	var tarifaID uuid.UUID
	var valorOriginal int64
	if err := a.pool.QueryRow(a.ctx, `
		SELECT r.id, r.amount_cents
		  FROM rates r
		  JOIN rate_tables rt ON rt.id = r.rate_table_id
		 WHERE rt.property_id = $1 AND r.unit_type_id = $2 AND r.date_type = 'fds'
		 ORDER BY rt.valid_from DESC
		 LIMIT 1`, propriedade, cobertura).Scan(&tarifaID, &valorOriginal); err != nil {
		t.Fatalf("achando a tarifa de fim de semana da Cobertura: %v", err)
	}

	const valorReajustado = int64(300000)
	reajuste := a.chamar(t, http.MethodPatch, "/rates/"+tarifaID.String(), gestor.Token,
		map[string]any{"amount_cents": valorReajustado})
	if reajuste.Status != http.StatusOK {
		t.Fatalf("PATCH /rates/{id} = %d — corpo: %s", reajuste.Status, reajuste.Corpo)
	}
	t.Cleanup(func() {
		a.executarQA(t, `UPDATE rates SET amount_cents = $1 WHERE id = $2`, valorOriginal, tarifaID)
	})

	// ── O passado não se mexeu ────────────────────────────────────────────
	depois := envelopeDe[reservaQA](t,
		a.chamar(t, http.MethodGet, "/reservations/"+reserva.ID.String(), vendedor.Token, nil),
		http.StatusOK, "GET /reservations/{id}")

	if depois.Total != jornadaTotal || depois.Subtotal != jornadaSubtotal || depois.Sinal != jornadaSinal {
		t.Errorf("depois do reajuste a reserva %s passou a valer total %d / subtotal %d / sinal %d, e o hóspede fechou por %d / %d / %d",
			depois.Codigo, depois.Total, depois.Subtotal, depois.Sinal, jornadaTotal, jornadaSubtotal, jornadaSinal)
	}

	// E noite a noite, que é onde o snapshot de fato mora.
	completa := envelopeDe[struct {
		Noites []struct {
			Noite string `json:"night"`
			Tipo  string `json:"date_type"`
			Preco int64  `json:"price_cents"`
		} `json:"nights"`
	}](t, a.chamar(t, http.MethodGet, "/reservations/"+reserva.ID.String()+"/full", vendedor.Token, nil),
		http.StatusOK, "GET /reservations/{id}/full")

	for _, noite := range completa.Noites {
		if noite.Tipo == "fds" && noite.Preco != 240000 {
			t.Errorf("a noite de %s foi reprecificada para %d — ela foi VENDIDA por 240000", noite.Noite, noite.Preco)
		}
	}

	// ── Mas o preço novo vale para quem pedir agora ───────────────────────
	//
	// O controle do experimento. Sem ele, um sistema que simplesmente ignorasse
	// o PATCH passaria na asserção acima e ninguém notaria que o reajuste não
	// pegou em lugar nenhum.
	novoOrcamento := envelopeDe[orcamentoQA](t,
		a.chamar(t, http.MethodPost, "/quotes", vendedor.Token, map[string]any{
			"unit_type_id": cobertura,
			"check_in":     "2026-12-04", // outra sexta, para não esbarrar na reserva
			"check_out":    "2026-12-06",
			"guests_count": 4,
		}), http.StatusOK, "POST /quotes depois do reajuste")

	for _, diaria := range novoOrcamento.Diarias {
		if diaria.Tipo == "fds" && diaria.Preco != valorReajustado {
			t.Fatalf("o orçamento novo cobrou %d na noite de fim de semana, e a tabela já dizia %d — o reajuste não chegou a quem compra hoje",
				diaria.Preco, valorReajustado)
		}
	}
}

// ─────────────────────────── Desempenho ─────────────────────────────────────

// Os dois tetos que a operação sente: o mapa de ocupação, que é a primeira tela
// da recepção todo dia de manhã, e o orçamento, que é recalculado a cada mexida
// no slider de desconto. Medidos por HTTP, com RBAC e serialização no caminho —
// o que o corretor espera não é o tempo da consulta, é o tempo da tela.
//
// A medição é o MELHOR de N execuções, e não a média: o pior caso numa máquina
// de desenvolvimento mede o barulho do laptop, não o sistema. Um teto que
// reprova por causa de um pico de CPU vira teste que se ignora.
func TestTempoDeRespostaDoMapaEDoOrcamento(t *testing.T) {
	a := subirAPI(t)
	propriedade := exigirSeed(t, a)

	vendedor := a.vendedor(t)
	cobertura := a.produtoDoSeed(t, propriedade, "cobertura")

	var hoje time.Time
	if err := a.pool.QueryRow(a.ctx,
		`SELECT (now() AT TIME ZONE timezone)::date FROM properties WHERE id = $1`, propriedade).Scan(&hoje); err != nil {
		t.Fatalf("lendo o dia da casa: %v", err)
	}
	de := hoje.AddDate(0, 0, 400).Format("2006-01-02") // longe do que os outros testes ocupam
	ate := hoje.AddDate(0, 0, 490).Format("2006-01-02")

	medir := func(nome string, teto time.Duration, chamada func()) time.Duration {
		t.Helper()

		chamada() // descarta a primeira: ela paga o plano da consulta e o TLS do cliente

		melhor := time.Hour
		for i := 0; i < 5; i++ {
			inicio := time.Now()
			chamada()
			if d := time.Since(inicio); d < melhor {
				melhor = d
			}
		}
		t.Logf("%s: %s (teto %s)", nome, melhor.Round(100*time.Microsecond), teto)
		if melhor > teto {
			t.Errorf("%s levou %s, e o teto é %s — acima disso a tela parece travada e o operador clica de novo",
				nome, melhor.Round(time.Millisecond), teto)
		}
		return melhor
	}

	// Mapa de ocupação: 90 dias × 8 unidades = 720 células.
	medir("mapa de 90 dias × 8 unidades", 300*time.Millisecond, func() {
		r := a.chamar(t, http.MethodGet,
			fmt.Sprintf("/availability/units?from=%s&to=%s", de, ate), vendedor.Token, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("GET /availability/units = %d: %s", r.Status, r.Corpo)
		}
	})

	// Disponibilidade comercial: 90 dias × 4 produtos.
	medir("disponibilidade de 90 dias × 4 produtos", 300*time.Millisecond, func() {
		r := a.chamar(t, http.MethodGet,
			fmt.Sprintf("/availability?from=%s&to=%s", de, ate), vendedor.Token, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("GET /availability = %d: %s", r.Status, r.Corpo)
		}
	})

	// Orçamento: é o que responde ao slider de desconto, então é o que o
	// corretor sente como "o sistema está lento".
	medir("orçamento de 3 noites", 150*time.Millisecond, func() {
		r := a.chamar(t, http.MethodPost, "/quotes", vendedor.Token, map[string]any{
			"unit_type_id": cobertura,
			"check_in":     jornadaEntrada,
			"check_out":    jornadaSaida,
			"guests_count": 4,
		})
		if r.Status != http.StatusOK {
			t.Fatalf("POST /quotes = %d: %s", r.Status, r.Corpo)
		}
	})
}
