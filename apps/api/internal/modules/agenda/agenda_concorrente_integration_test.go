//go:build integration

package agenda_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Disputas. Os nomes casam com o regex `it-concorrencia` do Makefile
// (Concorren|Disputa): uma corrida que passa verde numa passada só é
// exatamente a que a repetição existe para pegar.

// correr dispara `n` chamadas ao mesmo tempo (atrás de uma largada comum).
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

// A spec §9: "duas visitas simultâneas na mesma unidade e hora: exatamente uma
// vence, nenhuma 500". Várias rodadas, com N marcações cada, em janelas que se
// cruzam: uma 201, as outras 409 UNIT_NOT_AVAILABLE — seja quem decidiu a
// conferência do domínio (a outra já tinha comitado), seja a constraint (as
// duas passaram da conferência juntas).
func TestDisputaDeVisitasNaMesmaUnidadeEHora(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	unidade, codigo := a.unidade(t)

	for rodada := range 5 {
		dia := a.hoje.AddDays(60 + rodada)
		const n = 6
		respostas := correr(n, func(i int) resposta {
			// Janelas diferentes, todas cruzando as 10:00–10:30.
			de := []string{"10:00", "09:45", "10:15", "09:30", "10:00", "10:10"}[i]
			return a.chamar(t, http.MethodPost, "/agenda/events", token, map[string]any{
				"type": "visita", "title": "Disputa", "unit_id": unidade,
				"starts_at": a.rfc(dia, de), "ends_at": a.rfc(dia, "10:30"),
			})
		})
		criadas, recusadas := 0, 0
		for _, r := range respostas {
			switch {
			case r.Status == http.StatusCreated:
				criadas++
			case r.Status == http.StatusConflict && r.codigo() == "UNIT_NOT_AVAILABLE":
				recusadas++
				conflitos, _ := r.detalhes(t)["conflicts"].([]any)
				for _, c := range conflitos {
					if c.(map[string]any)["unit_code"] != codigo {
						t.Errorf("rodada %d: conflito sem unit_code: %v", rodada, c)
					}
				}
			default:
				t.Errorf("rodada %d: resposta fora do esperado: %d %s", rodada, r.Status, r.Corpo)
			}
		}
		if criadas != 1 || recusadas != n-1 {
			t.Fatalf("rodada %d: %d criadas e %d recusadas; esperado 1 e %d", rodada, criadas, recusadas, n-1)
		}
		if q := a.contar(t, `SELECT count(*) FROM agenda_events WHERE unit_id = $1 AND status = 'agendado'
			AND starts_at < $3 AND ends_at > $2`, unidade, a.em(dia, "00:00"), a.em(dia.AddDays(1), "00:00")); q != 1 {
			t.Fatalf("rodada %d: %d visitas gravadas no dia; quem perdeu não deixa nada", rodada, q)
		}
	}
}

// A disputa decidida pela CONSTRAINT, de forma determinística: uma marcação à
// mão está gravada numa transação ainda aberta — a conferência do domínio não
// a enxerga (não comitou) —, e a API grava a sua na mesma unidade e hora. O
// INSERT da API espera a outra transação; ela comita; o banco devolve 23P01
// em `agenda_events_unidade_sem_sobreposicao` — e a resposta é 409
// UNIT_NOT_AVAILABLE com os conflitos relidos depois do rollback, nunca 500 e
// nunca uma segunda visita.
func TestDisputaDecididaPelaConstraintReleOsConflitos(t *testing.T) {
	a := subir(t)
	quem, token := a.gestor(t)
	unidade, codigo := a.unidade(t)
	dia := a.hoje.AddDays(70)

	tx, err := a.pool.Begin(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var primeira uuid.UUID
	if err := tx.QueryRow(a.ctx, `
		INSERT INTO agenda_events (property_id, type, origin, unit_id, title, starts_at, ends_at, created_by)
		VALUES ($1, 'visita', 'manual', $2, 'Primeira', $3, $4, $5) RETURNING id`,
		a.propriedade, unidade, a.em(dia, "10:00"), a.em(dia, "11:00"), quem).Scan(&primeira); err != nil {
		t.Fatal(err)
	}

	resultado := make(chan resposta, 1)
	go func() {
		resultado <- a.chamar(t, http.MethodPost, "/agenda/events", token, map[string]any{
			"type": "visita", "title": "Segunda", "unit_id": unidade,
			"starts_at": a.rfc(dia, "10:30"), "ends_at": a.rfc(dia, "11:30"),
		})
	}()
	// Só comita quando o INSERT da API está ESPERANDO esta transação (lock na
	// verificação da EXCLUDE): é isso que garante que quem decide é a
	// constraint, e não a conferência — que, a esta altura, já passou sem
	// enxergar a primeira. Se o lock_timeout (400 ms) estourar antes, o
	// TxManager repete e o INSERT volta a esperar; o teto de 5 s só existe
	// para o teste não pendurar.
	esperando := func() bool {
		var n int
		if err := a.pool.QueryRow(a.ctx, `
			SELECT count(*) FROM pg_stat_activity
			 WHERE datname = current_database() AND wait_event_type = 'Lock' AND query LIKE '%INSERT INTO agenda_events%'`).
			Scan(&n); err != nil {
			t.Fatalf("lendo pg_stat_activity: %v", err)
		}
		return n > 0
	}
	limite := time.Now().Add(5 * time.Second)
	for !esperando() && time.Now().Before(limite) {
		time.Sleep(10 * time.Millisecond)
	}
	if !esperando() {
		t.Log("o INSERT da API não chegou a esperar a primeira: a recusa veio da conferência, não da constraint")
	}
	if err := tx.Commit(a.ctx); err != nil {
		t.Fatal(err)
	}

	r := <-resultado
	exigirErro(t, r, http.StatusConflict, "UNIT_NOT_AVAILABLE", "a segunda visita")
	conflitos, _ := r.detalhes(t)["conflicts"].([]any)
	if len(conflitos) != 1 {
		t.Fatalf("os conflitos são relidos depois do rollback: %s", r.Corpo)
	}
	c := conflitos[0].(map[string]any)
	if c["kind"] != "visita" || c["unit_code"] != codigo || c["starts_at"] != a.rfc(dia, "10:00") {
		t.Errorf("conflito: %v", c)
	}
	if q := a.contar(t, `SELECT count(*) FROM agenda_events WHERE unit_id = $1`, unidade); q != 1 {
		t.Fatalf("%d eventos na unidade; a segunda visita não pode ter nascido", q)
	}
}

// Concluir contra desmarcar a mesma visita, ao mesmo tempo: uma vence, a outra
// recebe 409 AGENDA_EVENT_CLOSED — e o evento nunca fica concluído E
// cancelado.
func TestConcorrenciaEntreConcluirEDesmarcar(t *testing.T) {
	a := subir(t)
	_, token := a.gestor(t)
	unidade, _ := a.unidade(t)

	for rodada := range 6 {
		ontem := a.hoje.AddDays(-1)
		hora := []string{"08:00", "09:00", "10:00", "11:00", "12:00", "13:00"}[rodada]
		ev := a.marcar(t, token, map[string]any{"type": "visita", "title": "Concorrência", "unit_id": unidade,
			"starts_at": a.rfc(ontem, hora)}).Data
		caminho := "/agenda/events/" + ev.ID.String()
		respostas := correr(2, func(i int) resposta {
			if i == 0 {
				return a.chamar(t, http.MethodPost, caminho+"/complete", token, nil)
			}
			return a.chamar(t, http.MethodDelete, caminho, token, nil)
		})
		ok, fechadas := 0, 0
		for _, r := range respostas {
			switch {
			case r.Status == http.StatusOK:
				ok++
			case r.Status == http.StatusConflict && r.codigo() == "AGENDA_EVENT_CLOSED":
				fechadas++
			default:
				t.Fatalf("resposta fora do esperado: %d %s", r.Status, r.Corpo)
			}
		}
		if ok != 1 || fechadas != 1 {
			t.Fatalf("%d sucessos e %d recusas; esperado 1 e 1", ok, fechadas)
		}
		var concluido, cancelado bool
		if err := a.pool.QueryRow(a.ctx, `SELECT done_at IS NOT NULL, cancelled_at IS NOT NULL FROM agenda_events WHERE id = $1`,
			ev.ID).Scan(&concluido, &cancelado); err != nil {
			t.Fatal(err)
		}
		if concluido == cancelado {
			t.Fatalf("estado misto: concluído=%v cancelado=%v", concluido, cancelado)
		}
	}
}
