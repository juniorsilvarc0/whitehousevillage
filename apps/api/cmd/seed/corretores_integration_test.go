//go:build integration

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// Guardas do F2-09 do lado do seed: a etapa refaz o vínculo do corretor de
// desenvolvimento quando ele some, a segunda passada não muda nada, e o vínculo
// que ela grava é o único que o banco aceita.
//
// Tudo em transação com ROLLBACK, pelo mesmo motivo do teste da política
// comercial: o banco de integração é compartilhado pelos outros pacotes.

func TestEtapaDosCorretoresRefazOVinculoEDepoisNaoMudaNada(t *testing.T) {
	pool := abrirPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("abrindo transação: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	st := &estado{}
	if _, err := propriedade(ctx, tx, st); err != nil {
		t.Fatalf("resolvendo a propriedade do seed: %v", err)
	}

	email := corretoresSeed[0].email

	// Desfaz o que o seed gravou, na ordem que a FK composta exige: a conta
	// solta o cadastro antes de o cadastro sumir.
	if _, err := tx.Exec(ctx, `UPDATE users SET broker_id = NULL WHERE lower(email) = lower($1)`, email); err != nil {
		t.Fatalf("soltando a conta do cadastro: %v", err)
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM brokers
		 WHERE user_id = (SELECT id FROM users WHERE lower(email) = lower($1))`, email)
	if err != nil {
		t.Fatalf("apagando o cadastro do corretor: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("cadastro do corretor %s não encontrado (%d linhas) — o seed rodou antes da suíte?",
			email, tag.RowsAffected())
	}

	primeira, err := corretoresDeDesenvolvimento(ctx, tx, st)
	if err != nil {
		t.Fatalf("primeira passada da etapa: %v", err)
	}
	// A ficha continua lá (inalterada); o cadastro renasce (criado) e a conta
	// volta a apontar para ele (atualizada).
	if primeira.Criadas != 1 || primeira.Atualizadas != 1 {
		t.Fatalf("primeira passada: criadas=%d atualizadas=%d, esperado 1 e 1 (o cadastro e o vínculo)",
			primeira.Criadas, primeira.Atualizadas)
	}

	segunda, err := corretoresDeDesenvolvimento(ctx, tx, st)
	if err != nil {
		t.Fatalf("segunda passada da etapa: %v", err)
	}
	if segunda.Criadas != 0 || segunda.Atualizadas != 0 || segunda.inalteradas() != segunda.Previstas {
		t.Fatalf("segunda passada: criadas=%d atualizadas=%d inalteradas=%d de %d — a etapa não é idempotente",
			segunda.Criadas, segunda.Atualizadas, segunda.inalteradas(), segunda.Previstas)
	}

	var ligado bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1
		                 FROM users u
		                 JOIN brokers b ON b.id = u.broker_id AND b.user_id = u.id
		                WHERE lower(u.email) = lower($1))`, email).Scan(&ligado); err != nil {
		t.Fatalf("conferindo o vínculo: %v", err)
	}
	if !ligado {
		t.Fatalf("depois da etapa, %s não aponta para um cadastro que aponta de volta para ela", email)
	}
}

// Controle negativo da FK composta: o cadastro que o seed criou para o corretor
// NÃO pode ser apontado por outra conta. Com a FK simples
// `(broker_id) → brokers(id)` este UPDATE passaria — e a conta da gestão
// passaria a ser, para o escopo `own` do F2-13, "o corretor" daquela carteira.
func TestOutraContaNaoApontaParaOCadastroDoCorretor(t *testing.T) {
	pool := abrirPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("abrindo transação: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		UPDATE users
		   SET broker_id = (SELECT u.broker_id FROM users u WHERE lower(u.email) = lower($1))
		 WHERE lower(email) = 'gestao@wh.local'`, corretoresSeed[0].email)
	exigirViolacaoDeChave(t, err, "users_broker_id_fkey")
}

// O defeito medido no backlog, agora no banco: venda com corretor que não
// existe. É o `23503` que o `backend-go` traduz para `422` em `broker_id`.
func TestVendaComCorretorInexistenteEhRecusadaPeloBanco(t *testing.T) {
	pool := abrirPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("abrindo transação: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		INSERT INTO reservations (property_id, unit_type_id, contact_id, broker_id,
		                          status, check_in, check_out, guests_count)
		SELECT ut.property_id, ut.id, ct.id, gen_random_uuid(),
		       'quote', DATE '2031-03-10', DATE '2031-03-12', 2
		  FROM unit_types ut
		  JOIN contacts ct ON ct.phone_e164 = $1
		 WHERE ut.consumes = 'one_member'
		 ORDER BY ut.code
		 LIMIT 1`, contatosDemo[0].telefone)
	exigirViolacaoDeChave(t, err, "reservations_broker_id_fkey")
}

func exigirViolacaoDeChave(t *testing.T, err error, constraint string) {
	t.Helper()
	if err == nil {
		t.Fatalf("o banco aceitou a escrita; esperado 23503 em %s", constraint)
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		t.Fatalf("erro sem SQLSTATE (%v); esperado 23503 em %s", err, constraint)
	}
	if pg.Code != "23503" || pg.ConstraintName != constraint {
		t.Fatalf("erro %s em %q; esperado 23503 em %s (%s)", pg.Code, pg.ConstraintName, constraint, pg.Message)
	}
}
