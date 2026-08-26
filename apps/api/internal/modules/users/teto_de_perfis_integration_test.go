//go:build integration

package users_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// ALTO da 3ª rodada — o teto de privilégio derrotado em UMA requisição.
//
// `PUT /roles/{id}/permissions` não comparava a matriz pedida com a do ator.
// Quem tinha `roles:editar` reescrevia o PRÓPRIO perfil com o catálogo inteiro
// e, na requisição seguinte, concedia qualquer coisa a qualquer um — o teto de
// `PATCH /users` virava enfeite, porque o atacante simplesmente ampliava o
// próprio teto antes de usá-lo.
//
// Vive no pacote users_test junto com as travas de usuário porque é a MESMA
// superfície de segurança: papel e cadastro são as duas metades da escalada de
// privilégio, e testá-las separado foi o que deixou passar as duas primeiras
// rodadas.

// catalogoInteiro devolve todas as células do catálogo, como a tela mandaria se
// alguém marcasse a grade inteira.
func (a *ambiente) catalogoInteiro(t *testing.T, token string) []auth.Permissao {
	t.Helper()

	r := a.chamar(t, http.MethodGet, "/roles/resources", token, nil)
	if r.Status != http.StatusOK {
		t.Fatalf("GET /roles/resources = %d: %s", r.Status, r.Corpo)
	}

	var corpo struct {
		Data []struct {
			Codigo string   `json:"code"`
			Acoes  []string `json:"actions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(r.Corpo, &corpo); err != nil {
		t.Fatalf("lendo o catálogo: %v", err)
	}

	var todas []auth.Permissao
	for _, rec := range corpo.Data {
		for _, acao := range rec.Acoes {
			todas = append(todas, auth.Permissao{Resource: rec.Codigo, Action: acao, Scope: auth.EscopoAll})
		}
	}
	if len(todas) < 50 {
		t.Fatalf("catálogo com %d células: o seed não rodou?", len(todas))
	}
	return todas
}

// Trava (b) — o achado, na forma exata em que foi reproduzido: o suporte
// reescreve o PRÓPRIO perfil com as células do catálogo inteiro.
//
// Ninguém edita a matriz do próprio perfil, nem dentro do próprio teto: a
// matriz é o que limita o ator, e deixá-lo mexer nela é deixar o preso guardar
// a chave da cela. Quem precisa de mais acesso pede a outro administrador.
func TestNinguemReescreveAMatrizDoProprioPerfil(t *testing.T) {
	a := subir(t)

	perfilSuporte := a.criarPerfil(t, "suporte",
		"roles:ver:all", "roles:editar:all", "users:ver:all", "users:editar:all")
	_, emailSuporte := a.criarUsuario(t, "suporte", perfilSuporte)

	suporte := a.login(t, emailSuporte)
	r := a.chamar(t, http.MethodPut, "/roles/"+perfilSuporte.String()+"/permissions",
		suporte.Token, a.catalogoInteiro(t, suporte.Token))

	if r.Status != http.StatusForbidden || r.codigoDoErro() != "FORBIDDEN" {
		t.Fatalf("status = %d (%s), esperado 403 FORBIDDEN; corpo %s",
			r.Status, r.codigoDoErro(), r.Corpo)
	}
	if n := a.celulasDoPerfil(t, perfilSuporte); n != 4 {
		t.Fatalf("a matriz do próprio perfil foi para %d células: a trava não impediu a escalada", n)
	}
}

// Trava (b) vale mesmo para um pedido inofensivo: o critério é "é o meu
// perfil", não "o que estou pedindo é maior". Sem isso, o ator poderia se
// aproximar do teto em passos pequenos e a trava viraria discussão de grau.
func TestNemUmSubconjuntoDoProprioPerfilPassa(t *testing.T) {
	a := subir(t)

	perfilSuporte := a.criarPerfil(t, "suporte",
		"roles:ver:all", "roles:editar:all", "users:ver:all", "users:editar:all")
	_, emailSuporte := a.criarUsuario(t, "suporte", perfilSuporte)

	suporte := a.login(t, emailSuporte)
	r := a.chamar(t, http.MethodPut, "/roles/"+perfilSuporte.String()+"/permissions",
		suporte.Token, []auth.Permissao{
			{Resource: auth.RecursoUsuarios, Action: auth.AcaoVer, Scope: auth.EscopoAll},
		})

	if r.Status != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403; corpo %s", r.Status, r.Corpo)
	}
	if n := a.celulasDoPerfil(t, perfilSuporte); n != 4 {
		t.Fatalf("matriz do próprio perfil = %d células, esperado 4 intactas", n)
	}
}

// Trava (a) — a variação que a trava (b) sozinha não pega: o ator escreve o
// catálogo inteiro num perfil de TERCEIRO e depois move alguém para lá (ou
// entra na conta de quem já está). Ninguém concede o que não possui.
func TestMatrizDeTerceiroNaoExcedeOPerfilDoAtor(t *testing.T) {
	a := subir(t)

	perfilSuporte := a.criarPerfil(t, "suporte",
		"roles:ver:all", "roles:editar:all", "users:ver:all", "users:editar:all")
	_, emailSuporte := a.criarUsuario(t, "suporte", perfilSuporte)
	perfilAlvo := a.criarPerfil(t, "alvo")

	suporte := a.login(t, emailSuporte)
	r := a.chamar(t, http.MethodPut, "/roles/"+perfilAlvo.String()+"/permissions",
		suporte.Token, a.catalogoInteiro(t, suporte.Token))

	if r.Status != http.StatusForbidden || r.codigoDoErro() != "FORBIDDEN" {
		t.Fatalf("status = %d (%s), esperado 403 FORBIDDEN; corpo %s",
			r.Status, r.codigoDoErro(), r.Corpo)
	}
	if n := a.celulasDoPerfil(t, perfilAlvo); n != 0 {
		t.Fatalf("o catálogo inteiro foi gravado no perfil de terceiro: %d células", n)
	}

	// O 403 diz o que falta ao ator, senão quem administra não descobre por que
	// o botão de salvar recusou.
	var env struct {
		Erro struct {
			Details struct {
				Missing []string `json:"missing"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(r.Corpo, &env)
	if len(env.Erro.Details.Missing) == 0 {
		t.Fatalf("details.missing veio vazio: %s", r.Corpo)
	}
}

// Escopo entra na comparação: quem só enxerga `own` não concede `all`, senão a
// delegação ampliaria o alcance de quem delegou.
func TestMatrizDeTerceiroNaoAmpliaEscopo(t *testing.T) {
	a := subir(t)

	perfilSuporte := a.criarPerfil(t, "suporte",
		"roles:ver:all", "roles:editar:all", "reservations:ver:own")
	_, emailSuporte := a.criarUsuario(t, "suporte", perfilSuporte)
	perfilAlvo := a.criarPerfil(t, "alvo")

	suporte := a.login(t, emailSuporte)
	r := a.chamar(t, http.MethodPut, "/roles/"+perfilAlvo.String()+"/permissions",
		suporte.Token, []auth.Permissao{
			{Resource: "reservations", Action: auth.AcaoVer, Scope: auth.EscopoAll},
		})

	if r.Status != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403; corpo %s", r.Status, r.Corpo)
	}
}

// Controle positivo: sem ele os testes acima passariam com a tela de perfis
// morta. Delegar o que se tem, em perfil de terceiro, continua funcionando.
func TestDelegarOQueSeTemEmPerfilDeTerceiroPassa(t *testing.T) {
	a := subir(t)

	perfilSuporte := a.criarPerfil(t, "suporte",
		"roles:ver:all", "roles:editar:all", "users:ver:all", "users:editar:all")
	_, emailSuporte := a.criarUsuario(t, "suporte", perfilSuporte)
	perfilAlvo := a.criarPerfil(t, "alvo")

	suporte := a.login(t, emailSuporte)
	r := a.chamar(t, http.MethodPut, "/roles/"+perfilAlvo.String()+"/permissions",
		suporte.Token, []auth.Permissao{
			{Resource: auth.RecursoUsuarios, Action: auth.AcaoVer, Scope: auth.EscopoAll},
			{Resource: auth.RecursoUsuarios, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
		})

	if r.Status != http.StatusOK {
		t.Fatalf("status = %d, esperado 200; corpo %s", r.Status, r.Corpo)
	}
	if n := a.celulasDoPerfil(t, perfilAlvo); n != 2 {
		t.Fatalf("células gravadas = %d, esperado 2", n)
	}
}
