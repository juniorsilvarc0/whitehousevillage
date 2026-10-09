//go:build integration

package manutencao_test

import (
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// Disputas. Os nomes casam com o regex `it-concorrencia` do Makefile
// (Concorren|Disputa): uma corrida que passa verde numa passada só é
// exatamente a que a repetição existe para pegar.

// correr dispara `n` chamadas ao mesmo tempo (atrás de uma largada comum) e
// devolve as respostas.
func correr(n int, chamada func(i int) resposta) []resposta {
	var (
		wg      sync.WaitGroup
		largada = make(chan struct{})
		out     = make([]resposta, n)
	)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-largada
			out[i] = chamada(i)
		}()
	}
	close(largada)
	wg.Wait()
	return out
}

// N ordens pedindo bloqueios sobrepostos na MESMA unidade: uma 201, as outras
// 409 DATE_CONFLICT — e nenhuma 500. Quem decide é a constraint de
// `stay_blocks`; a ordem que perdeu não sobra sem bloqueio (nasce tudo ou
// nada).
func TestDisputaDeBloqueioSobrepostoNaMesmaUnidade(t *testing.T) {
	a := subir(t)
	token, _ := a.gestor(t)
	unidade, codigo := a.unidade(t)
	d := a.hoje(t)

	const n = 10
	respostas := correr(n, func(i int) resposta {
		// Períodos diferentes, todos cruzando a noite D+5.
		return a.chamar(t, http.MethodPost, "/maintenance-orders", token, map[string]any{
			"unit_id": unidade, "title": "Disputa", "block": periodo(d.AddDays(1+i%3), d.AddDays(6+i%4)),
		})
	})
	criadas, conflitos := 0, 0
	for _, r := range respostas {
		switch {
		case r.Status == http.StatusCreated:
			criadas++
		case r.Status == http.StatusConflict && r.codigo() == "DATE_CONFLICT":
			conflitos++
			if r.detalhes(t)["unit_code"] != codigo || r.detalhes(t)["period"] == nil {
				t.Errorf("DATE_CONFLICT sem unit_code/period: %s", r.Corpo)
			}
		default:
			t.Errorf("resposta fora do esperado: %d %s", r.Status, r.Corpo)
		}
	}
	if criadas != 1 || conflitos != n-1 {
		t.Fatalf("%d criadas e %d conflitos; esperado 1 e %d", criadas, conflitos, n-1)
	}
	var ordens, bloqueios int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT (SELECT count(*) FROM maintenance_orders WHERE unit_id = $1),
		       (SELECT count(*) FROM stay_blocks WHERE unit_id = $1 AND status = 'confirmed')`, unidade).
		Scan(&ordens, &bloqueios); err != nil {
		t.Fatal(err)
	}
	if ordens != 1 || bloqueios != 1 {
		t.Fatalf("no banco: %d ordens e %d bloqueios; quem perdeu não deixa nada", ordens, bloqueios)
	}
}

// O segundo (e o décimo) toque em "Abrir ordem" na mesma avaria: uma ordem
// nasce; as outras recebem 409 MAINTENANCE_ORDER_ALREADY_OPEN apontando para
// ELA. Decidido pelo índice parcial, sem SELECT antes.
func TestDisputaPelaMesmaAvaria(t *testing.T) {
	a := subir(t)
	token, _ := a.gestor(t)
	unidade, _ := a.unidade(t)
	avaria := a.avaria(t, a.comodo(t, unidade, "Banheiro"), a.bem(t, "Chuveiro"))

	const n = 10
	respostas := correr(n, func(int) resposta {
		return a.chamar(t, http.MethodPost, "/maintenance-orders", token, map[string]any{
			"unit_id": unidade, "issue_id": avaria, "title": "Chuveiro não esquenta",
		})
	})
	var criada uuid.UUID
	repetidas := []resposta{}
	for _, r := range respostas {
		switch {
		case r.Status == http.StatusCreated:
			if criada != uuid.Nil {
				t.Fatalf("duas ordens abertas para a mesma avaria")
			}
			criada = dado[ordemResp](t, r).ID
		case r.Status == http.StatusConflict && r.codigo() == "MAINTENANCE_ORDER_ALREADY_OPEN":
			repetidas = append(repetidas, r)
		default:
			t.Errorf("resposta fora do esperado: %d %s", r.Status, r.Corpo)
		}
	}
	if criada == uuid.Nil || len(repetidas) != n-1 {
		t.Fatalf("criada %s, %d repetidas; esperado uma e %d", criada, len(repetidas), n-1)
	}
	for _, r := range repetidas {
		if r.detalhes(t)["maintenance_order_id"] != criada.String() {
			t.Errorf("o 409 leva à ordem que existe (%s): %s", criada, r.Corpo)
		}
	}
}

// Concluir contra cancelar a mesma ordem, ao mesmo tempo: uma vence, a outra
// recebe 409 MAINTENANCE_ORDER_CLOSED — e o banco nunca fica em estado misto
// (concluída com avaria intacta, cancelada com avaria consertada, bloqueio
// ainda ocupando).
func TestConcorrenciaEntreConcluirECancelar(t *testing.T) {
	a := subir(t)
	token, _ := a.gestor(t)
	d := a.hoje(t)

	for rodada := range 6 {
		unidade, _ := a.unidade(t)
		avaria := a.avaria(t, a.comodo(t, unidade, "Varanda"), a.bem(t, "Rede"))
		o := a.abrir(t, token, map[string]any{
			"unit_id": unidade, "issue_id": avaria, "title": "Rede rasgada", "block": periodo(d.AddDays(1), d.AddDays(3)),
		})
		if rodada%2 == 1 {
			exigir(t, a.chamar(t, http.MethodPost, "/maintenance-orders/"+o.ID.String()+"/start", token, nil), http.StatusOK, "começando")
		}
		caminho := "/maintenance-orders/" + o.ID.String()
		respostas := correr(2, func(i int) resposta {
			if i == 0 {
				return a.chamar(t, http.MethodPost, caminho+"/complete", token, map[string]any{"cost_cents": 5000})
			}
			return a.chamar(t, http.MethodDelete, caminho, token, nil)
		})
		ok, fechadas := 0, 0
		for _, r := range respostas {
			switch {
			case r.Status == http.StatusOK:
				ok++
			case r.Status == http.StatusConflict && r.codigo() == "MAINTENANCE_ORDER_CLOSED":
				fechadas++
			default:
				t.Fatalf("rodada %d: resposta fora do esperado: %d %s", rodada, r.Status, r.Corpo)
			}
		}
		if ok != 1 || fechadas != 1 {
			t.Fatalf("rodada %d: %d sucessos e %d recusas; esperado 1 e 1", rodada, ok, fechadas)
		}

		var (
			status, blocoStatus string
			custo               *int64
			desfecho            *string
		)
		if err := a.pool.QueryRow(a.ctx, `
			SELECT mo.status, mo.cost_cents, ii.resolution, sb.status
			  FROM maintenance_orders mo
			  JOIN inventory_issues ii ON ii.id = mo.issue_id
			  JOIN stay_blocks sb ON sb.id = mo.stay_block_id
			 WHERE mo.id = $1`, o.ID).Scan(&status, &custo, &desfecho, &blocoStatus); err != nil {
			t.Fatal(err)
		}
		if blocoStatus != "cancelled" {
			t.Fatalf("rodada %d: encerrada de qualquer jeito, o bloqueio não começado sai: %s", rodada, blocoStatus)
		}
		switch status {
		case "concluida":
			if textoDe(desfecho) != "consertado" || custo == nil || *custo != 5000 {
				t.Fatalf("rodada %d: concluída com avaria %s e custo %v", rodada, textoDe(desfecho), custo)
			}
		case "cancelada":
			if desfecho != nil || custo != nil {
				t.Fatalf("rodada %d: cancelada com avaria %s e custo %v — estado misto", rodada, textoDe(desfecho), custo)
			}
		default:
			t.Fatalf("rodada %d: estado %q depois da disputa", rodada, status)
		}
	}
}
