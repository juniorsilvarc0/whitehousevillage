//go:build integration

package users_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// CRÍTICO da 3ª rodada — tomada de conta do administrador por troca de e-mail.
//
// O caminho completo do ataque, reproduzido ao vivo antes da correção:
// quem tinha só `users:ver/criar/editar` mandava
// `PATCH /users/{admin} {"email":"eu@atacante.example"}`, recebia 200, e a
// partir daí controlava a RECUPERAÇÃO DE SENHA da conta do administrador —
// `/auth/password/forgot` passava a entregar o token no endereço do atacante.
//
// Por isso a trava não é "e-mail é campo de cadastro": e-mail é a credencial de
// recuperação, e trocá-lo em terceiro vale tanto quanto trocar a senha ou o
// papel. Exige a mesma autoridade das outras duas travas.

// O achado, na forma exata em que foi reproduzido.
func TestEmailDeTerceiroExigeAutoridadeDeAcesso(t *testing.T) {
	a := subir(t)

	perfilAtacante := a.criarPerfil(t, "atacante",
		"users:ver:all", "users:criar:all", "users:editar:all")
	_, emailAtacante := a.criarUsuario(t, "atacante", perfilAtacante)

	perfilAdmin := a.criarPerfilComCatalogoInteiro(t, "admin_alvo")
	idAdmin, emailAdmin := a.criarUsuario(t, "alvo", perfilAdmin)

	atacante := a.login(t, emailAtacante)
	r := a.chamar(t, http.MethodPatch, "/users/"+idAdmin.String(), atacante.Token,
		map[string]string{"email": "eu@atacante.example"})

	if r.Status != http.StatusForbidden || r.codigoDoErro() != "FORBIDDEN" {
		t.Fatalf("status = %d (%s), esperado 403 FORBIDDEN; corpo %s",
			r.Status, r.codigoDoErro(), r.Corpo)
	}
	if agora := a.emailDe(t, idAdmin); !strings.EqualFold(agora, emailAdmin) {
		t.Fatalf("o e-mail do administrador FOI trocado no banco: %s", agora)
	}
}

// O teto de privilégio vale aqui como vale para senha e papel: nem quem
// administra acesso troca o e-mail de alguém mais poderoso que ele.
func TestEmailDeAlguemAcimaDoTetoEhRecusado(t *testing.T) {
	a := subir(t)

	// Gestor administra acesso, mas não enxerga o catálogo inteiro.
	perfilGestor := a.criarPerfil(t, "gestor",
		"users:ver:all", "users:editar:all", "roles:ver:all", "roles:editar:all")
	_, emailGestor := a.criarUsuario(t, "gestor", perfilGestor)

	perfilAdmin := a.criarPerfilComCatalogoInteiro(t, "admin_alvo")
	idAdmin, emailAdmin := a.criarUsuario(t, "alvo", perfilAdmin)

	gestor := a.login(t, emailGestor)
	r := a.chamar(t, http.MethodPatch, "/users/"+idAdmin.String(), gestor.Token,
		map[string]string{"email": "gestor.tomou@wh.local"})

	if r.Status != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403; corpo %s", r.Status, r.Corpo)
	}
	if agora := a.emailDe(t, idAdmin); !strings.EqualFold(agora, emailAdmin) {
		t.Fatalf("o e-mail do administrador foi trocado por quem tem menos que ele: %s", agora)
	}
}

// Caminho legítimo 1: quem tem autoridade sobre o papel do alvo troca o e-mail
// dele — e a troca derruba as sessões da conta afetada e deixa rastro.
//
// Derrubar sessão é parte da correção, não enfeite: o endereço de recuperação
// mudou, então quem estava dentro precisa se reapresentar; e sem auditoria uma
// tomada de conta bem-sucedida não deixaria nenhuma linha para investigar.
func TestAdministradorTrocaEmailDeTerceiroERevogaSessoes(t *testing.T) {
	a := subir(t)

	perfilAdmin := a.criarPerfilComCatalogoInteiro(t, "admin")
	_, emailAdmin := a.criarUsuario(t, "admin", perfilAdmin)

	perfilComum := a.criarPerfil(t, "comum", "dashboard:ver:all")
	idComum, emailComum := a.criarUsuario(t, "comum", perfilComum)

	// A vítima entra: é a sessão que a troca de e-mail tem de derrubar.
	vitima := a.login(t, emailComum)
	if vivas := a.sessoesVivas(t, idComum); vivas != 1 {
		t.Fatalf("sessões vivas antes = %d, esperado 1", vivas)
	}

	admin := a.login(t, emailAdmin)
	novo := "trocado." + emailComum
	r := a.chamar(t, http.MethodPatch, "/users/"+idComum.String(), admin.Token,
		map[string]string{"email": novo})
	if r.Status != http.StatusOK {
		t.Fatalf("status = %d, esperado 200; corpo %s", r.Status, r.Corpo)
	}

	if agora := a.emailDe(t, idComum); !strings.EqualFold(agora, novo) {
		t.Fatalf("e-mail no banco = %s, esperado %s", agora, novo)
	}
	if vivas := a.sessoesVivas(t, idComum); vivas != 0 {
		t.Fatalf("sessões vivas = %d: a troca de e-mail não derrubou o acesso", vivas)
	}
	if n := a.auditoriasDe(t, idComum, auth.RecursoUsuarios+".email_alterado"); n == 0 {
		t.Fatal("a troca de e-mail não deixou linha de auditoria")
	}

	// Pela porta da frente: o refresh da vítima morreu junto.
	rr := a.chamar(t, http.MethodPost, "/auth/refresh", "", nil, vitima.Cookies...)
	if rr.Status != http.StatusUnauthorized {
		t.Fatalf("refresh da vítima = %d, esperado 401; corpo %s", rr.Status, rr.Corpo)
	}
}

// Caminho legítimo 2: o PRÓPRIO e-mail continua livre — trocar o endereço de
// contato não pode exigir chamado ao administrador. A sessão cai do mesmo
// jeito, porque a credencial de recuperação mudou.
func TestTrocarOProprioEmailPelaAPIContinuaPermitido(t *testing.T) {
	a := subir(t)

	perfil := a.criarPerfil(t, "recepcao", "users:ver:own", "users:editar:own")
	id, email := a.criarUsuario(t, "recepcao", perfil)

	s := a.login(t, email)
	novo := "novo." + email
	r := a.chamar(t, http.MethodPatch, "/users/"+id.String(), s.Token,
		map[string]string{"email": novo})
	if r.Status != http.StatusOK {
		t.Fatalf("status = %d, esperado 200; corpo %s", r.Status, r.Corpo)
	}
	if agora := a.emailDe(t, id); !strings.EqualFold(agora, novo) {
		t.Fatalf("e-mail no banco = %s, esperado %s", agora, novo)
	}
	if vivas := a.sessoesVivas(t, id); vivas != 0 {
		t.Fatalf("sessões vivas = %d: trocar o próprio e-mail deveria derrubar a sessão", vivas)
	}
	if n := a.auditoriasDe(t, id, auth.RecursoUsuarios+".email_alterado"); n == 0 {
		t.Fatal("a troca do próprio e-mail não deixou linha de auditoria")
	}
}

// Reenviar o MESMO e-mail não é troca: o PUT é substituição integral e obriga
// `email` no corpo. Recusar aqui impediria alguém com `users:editar` de
// corrigir o telefone de um colega pelo PUT.
func TestReenviarOMesmoEmailPelaAPINaoEhTroca(t *testing.T) {
	a := subir(t)

	perfilSuporte := a.criarPerfil(t, "suporte",
		"users:ver:all", "users:criar:all", "users:editar:all")
	_, emailSuporte := a.criarUsuario(t, "suporte", perfilSuporte)

	perfilComum := a.criarPerfil(t, "comum", "dashboard:ver:all")
	idComum, emailComum := a.criarUsuario(t, "comum", perfilComum)

	suporte := a.login(t, emailSuporte)
	r := a.chamar(t, http.MethodPut, "/users/"+idComum.String(), suporte.Token, map[string]any{
		"name":    "Comum Renomeado",
		"email":   strings.ToUpper(emailComum), // caixa diferente é o MESMO e-mail
		"role_id": perfilComum.String(),
		"phone":   "85999990000",
	})
	if r.Status != http.StatusOK {
		t.Fatalf("status = %d, esperado 200; corpo %s", r.Status, r.Corpo)
	}
}
