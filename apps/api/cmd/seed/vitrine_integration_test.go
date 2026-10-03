//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// Guardas da conta de serviço da vitrine do lado do seed. Tudo em transação com
// ROLLBACK: o banco de integração é compartilhado pelos outros pacotes.

// Célula concedida a mais no perfil da vitrine some na reexecução; escopo
// divergente volta a `all`; e a passada seguinte não muda nada.
func TestPerfilDaVitrineVoltaAExatamenteDuasCelulas(t *testing.T) {
	pool := abrirPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("abrindo transação: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	st := &estado{}
	if _, err := perfilDaVitrine(ctx, tx, st); err != nil {
		t.Fatalf("garantindo o perfil antes do teste: %v", err)
	}

	// Alguém "ajudou" pela tela: deu users:ver e trocou o escopo de contacts.
	if _, err := tx.Exec(ctx, `
		INSERT INTO role_permissions (role_id, resource_code, action, scope)
		SELECT id, $2, $3, 'all' FROM roles WHERE code = $1`,
		perfilVitrine, auth.RecursoUsuarios, auth.AcaoVer); err != nil {
		t.Fatalf("concedendo célula a mais: %v", err)
	}

	primeira, err := perfilDaVitrine(ctx, tx, st)
	if err != nil {
		t.Fatalf("primeira passada: %v", err)
	}
	if primeira.Removidas != 1 {
		t.Fatalf("primeira passada removeu %d células, esperado 1", primeira.Removidas)
	}

	segunda, err := perfilDaVitrine(ctx, tx, st)
	if err != nil {
		t.Fatalf("segunda passada: %v", err)
	}
	if segunda.mudou() {
		t.Fatalf("segunda passada mudou algo: %+v", segunda)
	}

	var celulas []string
	linhas, err := tx.Query(ctx, `
		SELECT rp.resource_code || ':' || rp.action || ':' || rp.scope
		  FROM role_permissions rp JOIN roles ro ON ro.id = rp.role_id
		 WHERE ro.code = $1 ORDER BY 1`, perfilVitrine)
	if err != nil {
		t.Fatal(err)
	}
	for linhas.Next() {
		var c string
		if err := linhas.Scan(&c); err != nil {
			t.Fatal(err)
		}
		celulas = append(celulas, c)
	}
	linhas.Close()
	if len(celulas) != 2 || celulas[0] != "contacts:criar:all" || celulas[1] != "reservations:criar:all" {
		t.Fatalf("matriz da vitrine no banco: %v", celulas)
	}
}

// A reexecução corrige nome e perfil, não reescreve o hash e não reativa a
// conta que a gestão desligou (é o botão de emergência das escritas do site).
func TestContaDaVitrineCorrigeCadastroSemTocarSenhaNemReativar(t *testing.T) {
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
	if _, err := perfilDaVitrine(ctx, tx, st); err != nil {
		t.Fatal(err)
	}
	if _, err := contaDaVitrine(ctx, tx, st); err != nil {
		t.Fatal(err)
	}

	var hashAntes string
	if err := tx.QueryRow(ctx, `SELECT password_hash FROM users WHERE email = $1`, emailContaVitrine).Scan(&hashAntes); err != nil {
		t.Fatalf("a conta da vitrine não existe depois da etapa: %v", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE users SET name = 'Renomeada', active = false,
		       role_id = (SELECT id FROM roles WHERE code = 'admin')
		 WHERE email = $1`, emailContaVitrine); err != nil {
		t.Fatal(err)
	}

	c, err := contaDaVitrine(ctx, tx, st)
	if err != nil {
		t.Fatal(err)
	}
	if c.Atualizadas != 1 || c.Criadas != 0 {
		t.Fatalf("passada de correção: %+v, esperado 1 atualizada", c)
	}

	var (
		nome, perfil, hashDepois string
		ativo                    bool
	)
	if err := tx.QueryRow(ctx, `
		SELECT u.name, ro.code, u.password_hash, u.active
		  FROM users u JOIN roles ro ON ro.id = u.role_id
		 WHERE u.email = $1`, emailContaVitrine).Scan(&nome, &perfil, &hashDepois, &ativo); err != nil {
		t.Fatal(err)
	}
	if nome != nomeContaVitrine || perfil != perfilVitrine {
		t.Fatalf("cadastro não corrigido: nome=%q perfil=%q", nome, perfil)
	}
	if hashDepois != hashAntes {
		t.Fatal("a reexecução reescreveu o hash da conta da vitrine")
	}
	if ativo {
		t.Fatal("o seed reativou a conta que a gestão desativou")
	}

	if c, err := contaDaVitrine(ctx, tx, st); err != nil || c.mudou() {
		t.Fatalf("terceira passada: %+v err=%v", c, err)
	}
}
