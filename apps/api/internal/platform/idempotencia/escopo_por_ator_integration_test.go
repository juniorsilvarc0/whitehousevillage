//go:build integration

// Prova, contra Postgres de verdade, as três promessas do pacote:
//
//  1. a chave é POR ATOR — dois usuários com a MESMA chave são duas requisições
//     independentes, e nenhum dos dois lê a resposta do outro;
//  2. duas transações simultâneas com a mesma chave fazem UM trabalho só, e a
//     segunda recebe a resposta da primeira;
//  3. trabalho abortado não deixa chave gravada — repetir depois de um erro é
//     tentativa nova, não repetição de sucesso.
//
// (1) é a razão de a PK ser `(key, endpoint, actor_id, property_id)` desde a
// migration 20260826120000: com a PK antiga, o corretor que reapresentasse a
// chave do gestor recebia 201 com a reserva inteira do gestor — a mesma que o
// `GET /reservations/{id}` devolve a ele como 404. Este teste é o controle
// negativo dessa correção: sem o ator na chave, ele fica vermelho.
package idempotencia

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// Um pool por PACOTE. Trinta aberturas de pool contra o mesmo Postgres, cada
// uma com o ping de boot de 5 s, é o que já fez outra suíte ver "context
// deadline exceeded" enquanto o pool anterior devolvia as conexões.
var abrirPool = sync.OnceValues(func() (*pgxpool.Pool, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return nil, nil
	}
	return db.New(context.Background(), url)
})

func TestMain(m *testing.M) {
	codigo := m.Run()
	if pool, _ := abrirPool(); pool != nil {
		pool.Close()
	}
	os.Exit(codigo)
}

type ambiente struct {
	pool *pgxpool.Pool
	tx   *db.TxManager
	ctx  context.Context
	// Dois atores da MESMA propriedade: o vazamento que se testa é entre
	// usuários da mesma casa, que é o caso real (gestor e corretor).
	donoA, donoB Dono
	// rota exclusiva deste teste: a suíte roda com `-p 1` num Postgres
	// compartilhado, e contar linhas de `idempotency_keys` por uma rota de
	// verdade contaria o trabalho de outro pacote.
	rota string
}

func subir(t *testing.T) *ambiente {
	t.Helper()

	pool, err := abrirPool()
	if err != nil {
		t.Fatalf("abrindo pool: %v", err)
	}
	if pool == nil {
		t.Skip("DATABASE_URL ausente: teste de integração pulado (use `make test-integration`)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	// Atores REAIS: `actor_id` e `property_id` são chaves estrangeiras, e um
	// uuid inventado transformaria o teste num 23503 em vez de provar o que ele
	// quer provar.
	linhas, err := pool.Query(ctx, `
		SELECT id, property_id FROM users WHERE deleted_at IS NULL ORDER BY email LIMIT 2`)
	if err != nil {
		t.Fatalf("lendo usuários: %v", err)
	}
	defer linhas.Close()

	var donos []Dono
	for linhas.Next() {
		var d Dono
		if err := linhas.Scan(&d.Ator, &d.Propriedade); err != nil {
			t.Fatalf("lendo usuários: %v", err)
		}
		donos = append(donos, d)
	}
	if len(donos) < 2 {
		t.Fatalf("banco com %d usuário(s): rode o seed antes (`make it-seed`)", len(donos))
	}

	a := &ambiente{
		pool:  pool,
		tx:    db.NewTxManager(pool),
		ctx:   ctx,
		donoA: donos[0],
		donoB: donos[1],
		rota:  "POST /it/idempotencia/" + uuid.NewString()[:8],
	}

	t.Cleanup(func() {
		limpar, cancelar := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelar()
		_, _ = pool.Exec(limpar, `DELETE FROM idempotency_keys WHERE endpoint = $1`, a.rota)
	})
	return a
}

// executar é o que um service faz: abre transação, toma a chave, faz o trabalho
// e guarda a resposta. Devolve a resposta GUARDADA quando a chave já concluiu —
// e é justamente isso que o teste do vazamento inspeciona.
func (a *ambiente) executar(t *testing.T, d Dono, chave, hash, marca string) *Resposta {
	t.Helper()

	var guardada *Resposta
	err := a.tx.Do(a.ctx, func(ctx context.Context) error {
		r, err := Reservar(ctx, a.pool, chave, a.rota, hash, d)
		if err != nil {
			return err
		}
		if r != nil {
			guardada = r
			return nil
		}
		return Guardar(ctx, a.pool, chave, a.rota, d, 201, map[string]any{"data": map[string]any{"marca": marca}})
	})
	if err != nil {
		t.Fatalf("execução de %s: %v", marca, err)
	}
	return guardada
}

func (a *ambiente) linhasDaChave(t *testing.T, chave string) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM idempotency_keys WHERE key = $1 AND endpoint = $2`,
		chave, a.rota).Scan(&n); err != nil {
		t.Fatalf("contando as chaves: %v", err)
	}
	return n
}

func novaChave() string { return "it-idem-" + uuid.NewString() }

// ─────────────────────────── 1. A chave é por ator ──────────────────

// CONTROLE NEGATIVO DO ITEM: tire `actor_id` da chave e este teste fica
// vermelho nos dois lados.
//
//   - pelo lado da LEITURA (PK de volta a `(key, endpoint)` e predicados sem o
//     ator): o ator B recebe, no `Reservar`, a resposta que o ator A guardou;
//   - pelo lado da ESCRITA (WHERE de `Guardar` sem `AND actor_id = $3`): o
//     UPDATE de B alcança a linha de A, e o replay de A passa a devolver o
//     corpo de B.
//
// O hash é o MESMO de propósito. Com corpos diferentes, o IDEMPOTENCY_MISMATCH
// esconderia o vazamento atrás de uma recusa que existe por outro motivo.
func TestChaveNaoVazaEntreAtores(t *testing.T) {
	a := subir(t)
	chave := novaChave()
	const hash = "mesma-impressao-para-os-dois"

	if guardada := a.executar(t, a.donoA, chave, hash, "trabalho-do-ator-a"); guardada != nil {
		t.Fatalf("a primeira execução encontrou chave usada: %s", guardada.Corpo)
	}

	// O ator B apresenta a MESMA chave, na MESMA rota, com o MESMO corpo.
	if guardada := a.executar(t, a.donoB, chave, hash, "trabalho-do-ator-b"); guardada != nil {
		t.Fatalf("o ator B recebeu a resposta guardada pelo ator A: %s\n"+
			"a chave deixou de ser por ator — é o vazamento que a migration 20260826120000 fechou",
			guardada.Corpo)
	}

	// Cada um relê a SUA, e nunca a do outro.
	for _, caso := range []struct {
		nome  string
		dono  Dono
		marca string
	}{
		{"ator A", a.donoA, "trabalho-do-ator-a"},
		{"ator B", a.donoB, "trabalho-do-ator-b"},
	} {
		guardada := a.executar(t, caso.dono, chave, hash, "nao-deveria-executar-de-novo")
		if guardada == nil {
			t.Fatalf("%s: o replay refez o trabalho em vez de reler a resposta", caso.nome)
		}
		if guardada.Status != 201 {
			t.Errorf("%s: status do replay = %d, esperado 201", caso.nome, guardada.Status)
		}
		if !strings.Contains(string(guardada.Corpo), caso.marca) {
			t.Errorf("%s: o replay devolveu %s, esperado o corpo com %q",
				caso.nome, guardada.Corpo, caso.marca)
		}
	}

	if n := a.linhasDaChave(t, chave); n != 2 {
		t.Fatalf("linhas guardando a chave = %d, esperado 2 (uma por ator)", n)
	}
}

// ─────────────────────────── 2. Duas simultâneas ────────────────────

// Duas transações SIMULTÂNEAS com a mesma chave: a segunda espera a primeira no
// `ON CONFLICT DO NOTHING` e devolve a resposta dela. O trabalho acontece UMA
// vez — que é a diferença entre um sinal e dois.
//
// Transações explícitas, e não duas chamadas HTTP: com a operação fechando em
// milissegundos, duas requisições em sequência não disputam nada e o teste
// passaria verde sem exercitar a corrida.
func TestMesmaChaveEmTransacoesSimultaneasFazUmTrabalhoSo(t *testing.T) {
	a := subir(t)
	chave := novaChave()
	const hash = "hash-da-disputa"

	var trabalhos atomic.Int64
	entrou, liberar := make(chan struct{}), make(chan struct{})
	primeiraPronta, segundaPronta := make(chan error, 1), make(chan error, 1)
	var daSegunda *Resposta

	go func() {
		primeiraPronta <- a.tx.Do(a.ctx, func(ctx context.Context) error {
			r, err := Reservar(ctx, a.pool, chave, a.rota, hash, a.donoA)
			if err != nil {
				return err
			}
			if r != nil {
				return errors.New("a primeira encontrou a chave já usada")
			}
			trabalhos.Add(1)
			if err := Guardar(ctx, a.pool, chave, a.rota, a.donoA, 201,
				map[string]any{"data": map[string]any{"marca": "primeira"}}); err != nil {
				return err
			}
			// A chave está tomada e a transação AINDA NÃO comitou: é esta a
			// janela em que a segunda tem de esperar em vez de refazer.
			close(entrou)
			<-liberar
			return nil
		})
	}()

	<-entrou
	go func() {
		segundaPronta <- a.tx.Do(a.ctx, func(ctx context.Context) error {
			r, err := Reservar(ctx, a.pool, chave, a.rota, hash, a.donoA)
			if err != nil {
				return err
			}
			if r == nil {
				trabalhos.Add(1)
				return Guardar(ctx, a.pool, chave, a.rota, a.donoA, 201,
					map[string]any{"data": map[string]any{"marca": "segunda"}})
			}
			daSegunda = r
			return nil
		})
	}()

	// Tempo para a segunda chegar ao INSERT e ficar esperando o token da
	// primeira. Fica abaixo do `lock_timeout` do pool (400 ms) de propósito; se
	// ainda assim estourar, o TxManager repete e o desfecho é o mesmo.
	time.Sleep(150 * time.Millisecond)
	close(liberar)

	if err := <-primeiraPronta; err != nil {
		t.Fatalf("primeira transação: %v", err)
	}
	if err := <-segundaPronta; err != nil {
		t.Fatalf("segunda transação: %v", err)
	}

	if n := trabalhos.Load(); n != 1 {
		t.Fatalf("trabalhos executados = %d, esperado 1: a chave não segurou a repetição", n)
	}
	if daSegunda == nil {
		t.Fatal("a segunda não recebeu a resposta guardada pela primeira")
	}
	if !strings.Contains(string(daSegunda.Corpo), "primeira") {
		t.Fatalf("a segunda recebeu %s, esperado o corpo da primeira", daSegunda.Corpo)
	}
	if n := a.linhasDaChave(t, chave); n != 1 {
		t.Fatalf("linhas da chave = %d, esperado 1", n)
	}
}

// ─────────────────────────── 3. Falhou, não gravou ──────────────────

var erroDeNegocio = errors.New("regra de negócio recusou a operação")

// Requisição que FALHA não deixa chave gravada: o rollback leva a linha junto.
// Sem isso, o hóspede que recebe 409 de data ocupada e tenta de novo com a
// mesma chave receberia IDEMPOTENCY_MISMATCH para sempre — a chave ficaria
// envenenada por uma tentativa que não produziu nada.
func TestTrabalhoAbortadoNaoDeixaChaveGravada(t *testing.T) {
	a := subir(t)
	chave := novaChave()
	const hash = "hash-da-tentativa"

	err := a.tx.Do(a.ctx, func(ctx context.Context) error {
		if _, err := Reservar(ctx, a.pool, chave, a.rota, hash, a.donoA); err != nil {
			return err
		}
		if err := Guardar(ctx, a.pool, chave, a.rota, a.donoA, 201,
			map[string]any{"data": map[string]any{"marca": "que-nao-aconteceu"}}); err != nil {
			return err
		}
		return fmt.Errorf("%w: data ocupada", erroDeNegocio)
	})
	if !errors.Is(err, erroDeNegocio) {
		t.Fatalf("erro = %v, esperado o do negócio", err)
	}

	if n := a.linhasDaChave(t, chave); n != 0 {
		t.Fatalf("linhas guardando a chave = %d depois do rollback, esperado 0", n)
	}

	// E a mesma chave volta a servir: a tentativa seguinte é NOVA.
	if guardada := a.executar(t, a.donoA, chave, hash, "tentativa-seguinte"); guardada != nil {
		t.Fatalf("a tentativa seguinte leu uma resposta que nunca existiu: %s", guardada.Corpo)
	}
}
