//go:build integration

package router

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// A guarda de `consumes` tem duas metades, e a suíte só tinha uma.
//
// A metade coberta é a de aplicação: `PATCH`/`PUT` com uma venda JÁ COMITADA
// devolve 409 (`inventario/composicao_vendida_integration_test.go`). Essa metade
// é a fácil — a venda já está lá para ser vista.
//
// A que faltava é a venda EM VOO. Em READ COMMITTED, uma reserva inserida e
// ainda não comitada é invisível para qualquer `SELECT` da guarda, e o motivo de
// as duas correrem lado a lado é específico: inserir em `reservations` toma
// `FOR KEY SHARE` na linha do produto (pela FK), o `UPDATE ... SET consumes`
// toma `FOR NO KEY UPDATE`, e **esses dois modos não conflitam**.
//
// A migration 20260827140000 fecha a janela com `FOR UPDATE` dentro da
// conferência — o único modo que conflita com `FOR KEY SHARE`.
//
// Este teste NÃO corre pelo HTTP de propósito. Uma corrida entre `POST
// /reservations` e `PATCH /unit-types/{id}` não exercita nada: o PATCH fecha em
// ~3 ms e a venda em ~10 ms, então a troca comita antes de a venda sequer
// começar, e o teste passa sem nunca ter havido disputa — medido, e o motivo de
// a primeira versão deste arquivo ter sido jogada fora. A janela é de
// transações, então é com transações que se prova.
//
// Sem o `FOR UPDATE`, o desfecho medido não foi "a troca passa despercebida":
// foi **a venda do hóspede morrer**. O produto virava `all_members` no meio do
// caminho e a reserva, criada sob `one_member`, batia no invariante da casa
// inteira no próprio commit. Edição de cadastro de um operador destruindo a
// reserva de um cliente, com o log culpando a reserva.
func TestTrocaDeConsumesEmDisputaComVendaEmVooEsperaEDepoisRecusa(t *testing.T) {
	a := subirAPI(t)

	var propriedade uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `SELECT id FROM properties LIMIT 1`).Scan(&propriedade); err != nil {
		t.Fatalf("propriedade do seed: %v", err)
	}
	cobertura := a.produtoDoSeed(t, propriedade, "cobertura")

	var hospede uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `SELECT id FROM contacts LIMIT 1`).Scan(&hospede); err != nil {
		t.Skipf("nenhum contato no seed: %v", err)
	}

	consumes := func() string {
		var c string
		if err := a.pool.QueryRow(a.ctx,
			`SELECT consumes FROM unit_types WHERE id = $1`, cobertura).Scan(&c); err != nil {
			t.Fatalf("lendo consumes: %v", err)
		}
		return c
	}

	original := consumes()

	const codigo = "WH-EM-VOO"
	limpar := func() {
		_, _ = a.pool.Exec(a.ctx,
			`DELETE FROM reservation_units WHERE reservation_id IN (SELECT id FROM reservations WHERE code = $1)`, codigo)
		_, _ = a.pool.Exec(a.ctx, `DELETE FROM reservations WHERE code = $1`, codigo)
		_, _ = a.pool.Exec(a.ctx,
			`UPDATE unit_types SET consumes = $2 WHERE id = $1`, cobertura, original)
	}
	limpar()
	t.Cleanup(limpar)

	// ── Sessão A: a venda, aberta e NÃO comitada ──────────────────────────
	venda, err := a.pool.Begin(a.ctx)
	if err != nil {
		t.Fatalf("abrindo a transação da venda: %v", err)
	}
	defer func() { _ = venda.Rollback(a.ctx) }()

	var reserva uuid.UUID
	err = venda.QueryRow(a.ctx, `
		INSERT INTO reservations (property_id, contact_id, unit_type_id, code, status,
		                          check_in, check_out, guests_count)
		VALUES ($1, $2, $3, $4, 'confirmed', DATE '2038-03-05', DATE '2038-03-08', 2)
		RETURNING id`,
		propriedade, hospede, cobertura, codigo).Scan(&reserva)
	if err != nil {
		t.Fatalf("inserindo a venda em voo: %v", err)
	}
	if _, err := venda.Exec(a.ctx, `
		INSERT INTO reservation_units (reservation_id, unit_id)
		SELECT $1, m.unit_id FROM unit_type_members m WHERE m.unit_type_id = $2`,
		reserva, cobertura); err != nil {
		t.Fatalf("alocando as unidades da venda: %v", err)
	}

	// ── Sessão B: a troca de cadastro, concorrente ────────────────────────
	resultado := make(chan error, 1)
	comecou := make(chan struct{})
	go func() {
		ctx, cancelar := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancelar()

		tx, err := a.pool.Begin(ctx)
		if err != nil {
			close(comecou)
			resultado <- err
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()

		// O pool da API roda com `lock_timeout = 400ms` (platform/db) para que
		// disputa de reserva estoure sempre como 55P03 e nunca como impasse.
		// Aqui isso atrapalharia a prova: a troca morreria no teto de espera
		// sem nunca chegar a LER a venda, e o teste passaria a atestar o
		// timeout em vez da guarda. Com um teto largo, quem recusa é a guarda,
		// e é a mensagem dela que se cobra abaixo.
		//
		// Os dois desfechos protegem — no ar, com o teto curto, a troca leva
		// 55P03 e a aplicação a trata como disputa retentável. O que nenhum dos
		// dois pode fazer é DEIXAR PASSAR, que é o caso sem `FOR UPDATE`.
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '15s'`); err != nil {
			close(comecou)
			resultado <- err
			return
		}

		// O UPDATE em si passa: o gatilho é adiado, e quem decide é o COMMIT.
		if _, err := tx.Exec(ctx,
			`UPDATE unit_types SET consumes = 'all_members' WHERE id = $1`, cobertura); err != nil {
			close(comecou)
			resultado <- err
			return
		}
		close(comecou)
		resultado <- tx.Commit(ctx)
	}()

	<-comecou
	largada := time.Now()

	// ── Prova 1: o COMMIT da troca BLOQUEIA enquanto a venda está no ar ────
	//
	// É esta espera que o `FOR UPDATE` compra. Sem ele o commit voltaria na
	// hora — foi o que se mediu: 0 s, e a venda morrendo em seguida.
	select {
	case err := <-resultado:
		t.Fatalf("o COMMIT da troca voltou em menos de 1s (err=%v) com uma venda em voo: "+
			"a conferência não esperou, então ela não enxerga venda não comitada — "+
			"é a janela que o FOR UPDATE existe para fechar", err)
	case <-time.After(1 * time.Second):
		// Bloqueado, como tem de ser.
	}

	// ── Agora a venda comita, e a troca acorda para enxergá-la ────────────
	if err := venda.Commit(a.ctx); err != nil {
		t.Fatalf("comitando a venda: %v", err)
	}

	var erroDaTroca error
	select {
	case erroDaTroca = <-resultado:
	case <-time.After(15 * time.Second):
		t.Fatal("a troca não voltou 15s depois de a venda comitar: a trava não foi liberada")
	}

	// ── Prova 2: a troca é RECUSADA, e pelo motivo certo ──────────────────
	if erroDaTroca == nil {
		t.Fatalf("a troca de consumes foi ACEITA por cima de uma venda em voo; consumes = %q", consumes())
	}
	var pgErr *pgconn.PgError
	if !errors.As(erroDaTroca, &pgErr) {
		t.Fatalf("erro da troca não é de banco: %v", erroDaTroca)
	}
	if pgErr.ConstraintName != "unit_types_consumes_com_venda_viva" {
		t.Errorf("a troca falhou por %q (%s), não pela guarda de consumes: %s",
			pgErr.ConstraintName, pgErr.Code, pgErr.Message)
	}

	// Medido daqui, e não do `time.After` acima: aquele um segundo é escolha do
	// teste e mediria a si mesmo. O que interessa é que o COMMIT só voltou
	// DEPOIS de a venda resolver — é isso que a trava compra.
	t.Logf("o COMMIT da troca ficou %s bloqueado e só voltou depois de a venda comitar",
		time.Since(largada).Round(time.Millisecond))

	// ── Prova 3: o juiz é o banco ─────────────────────────────────────────
	if depois := consumes(); depois != original {
		t.Errorf("consumes ficou %q, esperado %q: a troca foi recusada mas o dado mudou", depois, original)
	}
	var viva bool
	if err := a.pool.QueryRow(a.ctx, `
		SELECT status IN ('hold','confirmed','checked_in') FROM reservations WHERE code = $1`,
		codigo).Scan(&viva); err != nil {
		t.Fatalf("relendo a venda: %v", err)
	}
	if !viva {
		t.Error("a venda não sobreviveu à disputa — quem tem de perder a corrida é a edição de cadastro")
	}
}
