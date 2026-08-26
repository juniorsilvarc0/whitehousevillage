//go:build integration

// Guarda de regressão dos parâmetros de sessão do pool.
//
// O lock_timeout não é ajuste fino de performance: é ele que impede que a
// disputa pela mesma data vire `40P01 deadlock detected` — e 40P01 foi, por duas
// rodadas de revisão, o motivo de 49 de 50 hóspedes receberem HTTP 500 numa
// situação rotineira. Se alguém remover a linha do db.New, nenhum teste unitário
// fica vermelho e o teste de concorrência volta a falhar só às vezes. Este
// arquivo fecha essa porta.
package db

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPoolAplicaOsParametrosDeSessao(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL ausente: teste de integração pulado (use `make test-integration`)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := New(ctx, url)
	if err != nil {
		t.Fatalf("abrindo pool: %v", err)
	}
	defer pool.Close()

	esperado := map[string]string{
		"lock_timeout":                        "400ms",
		"statement_timeout":                   "30s",
		"idle_in_transaction_session_timeout": "30s",
	}

	for parametro, valor := range esperado {
		var lido string
		if err := pool.QueryRow(ctx, `SELECT current_setting($1)`, parametro).Scan(&lido); err != nil {
			t.Fatalf("lendo %s: %v", parametro, err)
		}
		if lido != valor {
			t.Errorf("%s = %q na sessão, esperado %q", parametro, lido, valor)
		}
	}

	// A ordem entre os dois é o que garante que espera por lock estoure como
	// 55P03 (contenção, que o TxManager repete) e nunca como 57014 (consulta
	// lenta, que não se repete). Invertê-los desligaria a repetição inteira.
	var lock, statement time.Duration
	if err := pool.QueryRow(ctx,
		`SELECT current_setting('lock_timeout')::interval, current_setting('statement_timeout')::interval`,
	).Scan(&lock, &statement); err != nil {
		t.Fatalf("comparando os tetos: %v", err)
	}
	if lock >= statement {
		t.Fatalf("lock_timeout (%s) precisa ser MENOR que statement_timeout (%s)", lock, statement)
	}

	// E o lock_timeout precisa ficar abaixo do deadlock_timeout do servidor: é
	// essa desigualdade que faz a espera ser cortada ANTES de o detector de
	// impasse do Postgres transformá-la em 40P01.
	var deadlock time.Duration
	if err := pool.QueryRow(ctx, `SELECT current_setting('deadlock_timeout')::interval`).Scan(&deadlock); err != nil {
		t.Fatalf("lendo deadlock_timeout: %v", err)
	}
	if lock >= deadlock {
		t.Fatalf("lock_timeout (%s) precisa ser MENOR que deadlock_timeout (%s) — "+
			"acima dele a disputa por data volta a chegar como 40P01", lock, deadlock)
	}
}
