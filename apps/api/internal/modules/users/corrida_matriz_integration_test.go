//go:build integration

package users_test

import (
	"net/http"
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// A terceira porta para a MESMA sala vazia: quem administra acesso não sai só
// pelo cadastro de usuários. `PUT /roles/{id}/permissions` também decide quem
// é administrador — tirar `roles:editar` da matriz de um perfil rebaixa TODOS os
// usuários daquele perfil de uma vez.
//
// Sequencialmente isso é seguro: a trava do próprio perfil garante que o ator
// continua administrador depois de mexer na matriz alheia. Em PARALELO com uma
// desativação, não: X tira o bit de administrador do perfil de Y enquanto Y
// desativa X, e as duas requisições leem um mundo em que ainda sobra alguém.
func TestMatrizEDesativacaoSimultaneasNaoDeixamAInstalacaoSemAdministrador(t *testing.T) {
	a := subir(t)

	if _, err := a.pool.Exec(a.ctx, `UPDATE users SET active = false`); err != nil {
		t.Fatalf("zerando a população de usuários: %v", err)
	}

	papelX := a.criarPerfilComCatalogoInteiro(t, "admin_x")
	papelY := a.criarPerfilComCatalogoInteiro(t, "admin_y")
	idX, emailX := a.criarUsuario(t, "x", papelX)
	_, emailY := a.criarUsuario(t, "y", papelY)

	if n := a.administradoresAtivos(t); n != 2 {
		t.Fatalf("administradores ativos = %d, esperado 2", n)
	}

	sx, sy := a.login(t, emailX), a.login(t, emailY)

	// A matriz que X escreve no perfil de Y: tudo menos administrar acesso.
	semAdministracao := []auth.Permissao{
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: auth.RecursoPerfis, Action: auth.AcaoVer, Scope: auth.EscopoAll},
	}

	rMatriz, rDelete := emParalelo(t,
		func() resposta {
			return a.chamar(t, http.MethodPut, "/roles/"+papelY.String()+"/permissions", sx.Token, semAdministracao)
		},
		func() resposta {
			return a.chamar(t, http.MethodDelete, "/users/"+idX.String(), sy.Token, nil)
		},
	)

	// As duas não podem passar: quem chegar depois tem de ver o mundo que a
	// primeira deixou. E quem for recusado tem de ser recusado com 422, não com
	// 500 — a trava é regra de negócio, não acidente de constraint.
	conferirCorrida(t, a, rMatriz, rDelete)
}
