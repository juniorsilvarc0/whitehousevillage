//go:build integration

package router

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// A brecha que o próprio módulo de inventário declarou não ter fechado: a guarda
// que impede mexer na composição de uma casa vendida roda DENTRO da transação,
// o que a serializa contra outra alteração de composição — mas não contra uma
// VENDA concorrente. Em READ COMMITTED, entre o SELECT da guarda e o commit cabe
// um POST /reservations inteiro.
//
// A direção perigosa é a composição CRESCER durante a venda: a venda lê 8
// unidades e insere 8 blocos; a composição vira 9; a reserva da casa "inteira"
// passa a segurar 8 de 9, e a nona é vendida a um estranho. A constraint
// stay_no_overlap não enxerga isso, porque o defeito é a AUSÊNCIA de uma linha,
// e constraint nenhuma vê ausência.
//
// O juiz aqui não é o código de resposta: é o banco. Para toda reserva viva de
// produto `all_members`, |reservation_units| tem de ser igual a |composição|.
func TestComposicaoCrescendoDuranteAVendaNaoAbreBuracoNaCasaInteira(t *testing.T) {
	a := subirAPI(t)

	admin := a.criarUsuario(t, "concorrencia-composicao", a.perfilRaiz(t))

	var propriedade uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `SELECT id FROM properties LIMIT 1`).Scan(&propriedade); err != nil {
		t.Fatalf("propriedade do seed: %v", err)
	}
	completa := a.produtoDoSeed(t, propriedade, "completa")

	var hospede uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `SELECT id FROM contacts LIMIT 1`).Scan(&hospede); err != nil {
		t.Skipf("nenhum contato no seed: %v", err)
	}

	// Unidades sobressalentes: uma por rodada, para a composição ter o que crescer.
	const rodadas = 10
	sobressalentes := make([]uuid.UUID, rodadas)
	for i := range sobressalentes {
		var id uuid.UUID
		err := a.pool.QueryRow(a.ctx, `
			INSERT INTO units (property_id, code, name, sort_order)
			VALUES ($1, $2, $3, 900)
			ON CONFLICT (property_id, code) DO UPDATE SET name = EXCLUDED.name
			RETURNING id`,
			propriedade, fmt.Sprintf("TESTE-%02d", i), fmt.Sprintf("Sobressalente %02d", i)).Scan(&id)
		if err != nil {
			t.Fatalf("criando unidade sobressalente: %v", err)
		}
		sobressalentes[i] = id
	}
	t.Cleanup(func() {
		_, _ = a.pool.Exec(a.ctx, `DELETE FROM units WHERE code LIKE 'TESTE-%'`)
	})

	// O teste precisa começar com a janela de 2039 vazia: rodar duas vezes contra
	// o mesmo banco faria a venda bater nas reservas da execução anterior e
	// devolver 409 sem nunca exercitar a disputa — verde por motivo errado.
	limparJanelaDeTeste := func() {
		_, _ = a.pool.Exec(a.ctx, `
			DELETE FROM reservations
			 WHERE unit_type_id = $1 AND check_in >= DATE '2039-01-01' AND check_in < DATE '2040-01-01'`,
			completa)
	}
	limparJanelaDeTeste()
	t.Cleanup(limparJanelaDeTeste)

	composicaoBase := a.idsDaComposicao(t, completa)
	if len(composicaoBase) < 2 {
		t.Fatalf("a composição da Completa tem %d unidades; o teste não prova nada", len(composicaoBase))
	}

	buracosNaCorrida := 0
	for i := 0; i < rodadas; i++ {
		// Cada rodada usa um intervalo próprio, longe do seed e das outras.
		entrada := fmt.Sprintf("2039-%02d-05", i+1)
		saida := fmt.Sprintf("2039-%02d-08", i+1)

		alvo := append(append([]uuid.UUID{}, composicaoBase...), sobressalentes[i])

		var venda, mudanca resposta
		var wg sync.WaitGroup
		largada := make(chan struct{})
		wg.Add(2)

		go func() {
			defer wg.Done()
			<-largada
			venda = a.chamarComChave(t, http.MethodPost, "/reservations", admin.Token, map[string]any{
				"unit_type_id": completa,
				"contact_id":   hospede,
				"check_in":     entrada,
				"check_out":    saida,
				"guests_count": 4,
			}, a.chaveNova(t, fmt.Sprintf("concorrencia-%d", i)))
		}()
		go func() {
			defer wg.Done()
			<-largada
			mudanca = a.chamar(t, http.MethodPut, "/unit-types/"+completa.String()+"/members",
				admin.Token, map[string]any{"unit_ids": alvo})
		}()
		close(largada)
		wg.Wait()

		// O JUIZ RODA AQUI, ANTES do reset. Conferir só no fim seria o teste
		// apagando a própria evidência: devolver a composição ao normal reduz
		// |composição| de volta a |reservation_units| e o buraco some do SELECT.
		if venda.Status == http.StatusCreated {
			unidades, composicao := a.tamanhoDaVendaEDaComposicao(t, completa)
			if unidades < composicao {
				buracosNaCorrida++
				t.Errorf("rodada %d (venda=%d mudança=%d): a casa foi vendida INTEIRA segurando "+
					"%d de %d unidades da composição — as %d que sobraram são vendáveis a um "+
					"estranho, e a EXCLUDE não vê, porque o defeito é a ausência de linha",
					i, venda.Status, mudanca.Status, unidades, composicao, composicao-unidades)
			}
		}

		// Volta a composição ao normal para a próxima rodada não herdar estado.
		_ = a.chamar(t, http.MethodPut, "/unit-types/"+completa.String()+"/members",
			admin.Token, map[string]any{"unit_ids": composicaoBase})

		t.Logf("rodada %d: venda=%d mudança=%d", i, venda.Status, mudanca.Status)
	}

	// ── O JUIZ: o banco, não a resposta HTTP ──────────────────────────────
	// Toda reserva viva de produto `all_members` precisa segurar exatamente as
	// unidades da composição do produto. Menos que isso é buraco vendável.
	linhas, err := a.pool.Query(a.ctx, `
		SELECT r.code,
		       (SELECT count(*) FROM reservation_units ru WHERE ru.reservation_id = r.id)  AS unidades,
		       (SELECT count(*) FROM unit_type_members m WHERE m.unit_type_id = r.unit_type_id) AS composicao
		  FROM reservations r
		  JOIN unit_types ut ON ut.id = r.unit_type_id
		 WHERE ut.consumes = 'all_members'
		   AND r.status IN ('hold','confirmed','checked_in')`)
	if err != nil {
		t.Fatalf("consultando o juiz: %v", err)
	}
	defer linhas.Close()

	buracos := 0
	for linhas.Next() {
		var codigo string
		var unidades, composicao int
		if err := linhas.Scan(&codigo, &unidades, &composicao); err != nil {
			t.Fatalf("lendo o juiz: %v", err)
		}
		if unidades < composicao {
			buracos++
			t.Errorf("%s é uma venda da casa INTEIRA segurando %d de %d unidades da composição: "+
				"as %d que sobraram podem ser vendidas a um estranho, e a EXCLUDE não vê, "+
				"porque o defeito é a ausência de linha",
				codigo, unidades, composicao, composicao-unidades)
		}
	}
	if err := linhas.Err(); err != nil {
		t.Fatalf("varrendo o juiz: %v", err)
	}
	if buracos == 0 {
		t.Logf("nenhuma reserva de casa inteira ficou com buraco após %d disputas", rodadas)
	}
}

// tamanhoDaVendaEDaComposicao devolve, para a reserva viva mais recente do
// produto, quantas unidades ela segura e quantas a composição tem agora.
func (a *ambiente) tamanhoDaVendaEDaComposicao(t *testing.T, produto uuid.UUID) (int, int) {
	t.Helper()

	var unidades, composicao int
	err := a.pool.QueryRow(a.ctx, `
		SELECT (SELECT count(*) FROM reservation_units ru WHERE ru.reservation_id = r.id),
		       (SELECT count(*) FROM unit_type_members m WHERE m.unit_type_id = r.unit_type_id)
		  FROM reservations r
		 WHERE r.unit_type_id = $1
		   AND r.status IN ('hold','confirmed','checked_in')
		 ORDER BY r.created_at DESC
		 LIMIT 1`, produto).Scan(&unidades, &composicao)
	if err != nil {
		t.Fatalf("medindo venda x composição: %v", err)
	}
	return unidades, composicao
}
