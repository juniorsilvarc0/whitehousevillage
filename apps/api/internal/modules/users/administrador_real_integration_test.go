//go:build integration

package users_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// A pergunta que o teto de privilégio da matriz obriga a responder: com a trava
// ligada, o ADMINISTRADOR DE VERDADE — o do seed, com o catálogo inteiro em
// `all` — continua conseguindo administrar?
//
// Este teste existe porque a trava (a) tem uma consequência incômoda e
// deliberada: quem administra acesso sem possuir um recurso deixa de poder
// concedê-lo. É a mesma consequência que `PATCH /users {role_id}` já tinha desde
// a rodada passada (atribuir papel exige possuir as células dele), agora
// estendida à matriz. Se o administrador do seed não passasse por aqui, a trava
// estaria errada — e não a instalação.
func TestAdministradorDoSeedSalvaOPerfilCorretorSemAlteracao(t *testing.T) {
	a := subir(t)

	var corretor uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `SELECT id FROM roles WHERE code = 'corretor'`).Scan(&corretor); err != nil {
		t.Skipf("perfil `corretor` ausente: rode `go run ./cmd/seed` antes (%v)", err)
	}
	// A conta do seed precisa estar ativa: outros testes deste pacote desativam
	// a população inteira para controlar a contagem de administradores.
	if _, err := a.pool.Exec(a.ctx,
		`UPDATE users SET active = true WHERE email = 'admin@wh.local'`); err != nil {
		t.Fatalf("reativando o administrador do seed: %v", err)
	}

	admin := a.loginComSenha(t, "admin@wh.local", senhaDoSeed)

	lido := a.chamar(t, http.MethodGet, "/roles/"+corretor.String(), admin.Token, nil)
	if lido.Status != http.StatusOK {
		t.Fatalf("GET /roles/{corretor}: %d — %s", lido.Status, lido.Corpo)
	}
	var detalhe struct {
		Data struct {
			Permissoes []auth.Permissao `json:"permissions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(lido.Corpo, &detalhe); err != nil {
		t.Fatalf("lendo o perfil: %v", err)
	}

	// A matriz do corretor é quase toda `own`. Sem `own` no meio, o teste não
	// provaria que o ator em `all` consegue conceder o escopo menor.
	temOwn := false
	for _, p := range detalhe.Data.Permissoes {
		if p.Scope == auth.EscopoOwn {
			temOwn = true
			break
		}
	}
	if !temOwn {
		t.Fatal("o corretor semeado não tem nenhuma permissão `own`: o teste não prova o que promete")
	}

	salvo := a.chamar(t, http.MethodPut, "/roles/"+corretor.String()+"/permissions",
		admin.Token, detalhe.Data.Permissoes)
	if salvo.Status != http.StatusOK {
		t.Fatalf("o administrador do seed não conseguiu salvar o Corretor sem alteração: %d — %s",
			salvo.Status, salvo.Corpo)
	}
}
