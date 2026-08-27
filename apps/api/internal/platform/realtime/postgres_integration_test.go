//go:build integration

package realtime

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// A escuta de verdade, contra um Postgres de verdade — inclusive a morte dela.
//
// O teste de unidade prova a MÁQUINA de reconexão com uma Escuta de mentira.
// Este prova as duas coisas que a mentira não alcança: que o `LISTEN` está sendo
// registrado nos canais certos, e que a morte de uma conexão de banco real
// (`pg_terminate_backend`, que é o que um failover, um `pg_ctl restart` ou o
// OOM-killer fazem) chega até aqui como erro e não como espera eterna.
func TestEscutaRealSobreviveAConexaoSendoDerrubadaNoBanco(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL ausente: teste de integração pulado (use `make test-integration`)")
	}

	ctx, cancelar := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelar()

	pool, err := db.New(ctx, url)
	if err != nil {
		t.Fatalf("abrindo pool: %v", err)
	}
	defer pool.Close()

	h := NovoHub(Config{
		Abrir:        AbridorDePool(pool),
		Log:          silencioso(),
		EsperaMinima: 20 * time.Millisecond,
		EsperaMaxima: 200 * time.Millisecond,
	})
	h.Iniciar(context.Background())
	defer h.Parar()

	aguardarConexaoReal(t, h)

	a, _, err := h.Assinar("usuario", []string{TopicoCalendario}, 0)
	if err != nil {
		t.Fatalf("Assinar: %v", err)
	}
	defer a.Cancelar()

	antes := notificar(t, ctx, pool, TopicoCalendario)
	if ev := receber(t, a, 5*time.Second); ev.Evento.ID != antes {
		t.Fatalf("evento antes da queda: %+v", ev)
	}

	// A queda. É `application_name` que torna a conexão de escuta identificável
	// — e é por isso que o AbridorDePool o define.
	var derrubadas int
	if err := pool.QueryRow(ctx, `
		SELECT count(pg_terminate_backend(pid))
		  FROM pg_stat_activity
		 WHERE application_name = 'whv-api-listen'
		   AND pid <> pg_backend_pid()`).Scan(&derrubadas); err != nil {
		t.Fatalf("derrubando a conexão de escuta: %v", err)
	}
	if derrubadas == 0 {
		t.Fatal("nenhuma conexão com application_name='whv-api-listen' — a escuta não está identificável no pg_stat_activity")
	}

	// Quem estava conectado precisa ouvir que houve buraco.
	if ev := receber(t, a, 10*time.Second); !ev.Resync {
		t.Fatalf("esperava resync depois da queda da conexão; veio %+v", ev)
	}
	aguardarConexaoReal(t, h)

	depois := notificar(t, ctx, pool, TopicoCalendario)
	if ev := receber(t, a, 10*time.Second); ev.Evento.ID != depois {
		t.Fatalf("o hub não voltou a entregar depois de a conexão ser derrubada no banco: %+v", ev)
	}
}

func aguardarConexaoReal(t *testing.T, h *Hub) {
	t.Helper()
	prazo := time.Now().Add(20 * time.Second)
	for time.Now().Before(prazo) {
		if h.Conectado() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("o hub não abriu o LISTEN")
}

// notificar emite um evento pelo canal real e devolve o id que foi anunciado.
func notificar(t *testing.T, ctx context.Context, pool *pgxpool.Pool, topico string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	payload := fmt.Sprintf(`{"topic":%q,"entity":"stay_block","id":%q,"unit_id":%q,"property_id":null,"v":1}`,
		topico, id, uuid.New())
	if _, err := pool.Exec(ctx, `SELECT pg_notify($1, $2)`, CanalCalendario, payload); err != nil {
		t.Fatalf("pg_notify: %v", err)
	}
	return id
}
