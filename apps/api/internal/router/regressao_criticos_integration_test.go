//go:build integration

// Regressão dos três defeitos críticos que reprovaram a Fase 0 em duas revisões
// adversariais independentes.
//
// Todos aqui entram pela PORTA DA FRENTE — HTTP, status, code de erro — e, onde
// o defeito era de persistência, conferem o BANCO. É a diferença que importa:
// os três defeitos originais tinham teste de service verde por cima deles.
// Um teste que só pergunta ao service "deu certo?" acredita na resposta do
// próprio código que está sob suspeita.
package router

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// ─────────────────── Infra: requisição que carrega cookie ───────────────────

// chamarComCookies é o `chamar` com jar manual. O refresh do painel viaja em
// cookie httpOnly, e testar o roubo de sessão pelo corpo do JSON mediria um
// caminho que o navegador nunca usa.
func (a *ambiente) chamarComCookies(t *testing.T, metodo, caminho string, cookies []*http.Cookie, corpo any) resposta {
	t.Helper()

	var body io.Reader
	if corpo != nil {
		bruto, err := json.Marshal(corpo)
		if err != nil {
			t.Fatalf("serializando o corpo: %v", err)
		}
		body = bytes.NewReader(bruto)
	}

	req, err := http.NewRequestWithContext(a.ctx, metodo, a.servidor.URL+PrefixoDaAPI+caminho, body)
	if err != nil {
		t.Fatalf("montando a requisição: %v", err)
	}
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", metodo, caminho, err)
	}
	defer resp.Body.Close()

	lido, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("lendo a resposta: %v", err)
	}
	return resposta{Status: resp.StatusCode, Corpo: lido, Headers: resp.Header}
}

// chamarAutenticadoComCookies é o caso do LOGOUT: rota autenticada que também
// recebe o cookie de refresh. O navegador manda os dois; um teste que mandasse
// só o Bearer mediria um logout que o painel nunca faz — e o serviço, sem
// comprovante para identificar a família, não teria o que revogar.
func (a *ambiente) chamarAutenticadoComCookies(t *testing.T, metodo, caminho, token string, cookies []*http.Cookie, corpo any) resposta {
	t.Helper()

	var body io.Reader
	if corpo != nil {
		bruto, err := json.Marshal(corpo)
		if err != nil {
			t.Fatalf("serializando o corpo: %v", err)
		}
		body = bytes.NewReader(bruto)
	}

	req, err := http.NewRequestWithContext(a.ctx, metodo, a.servidor.URL+PrefixoDaAPI+caminho, body)
	if err != nil {
		t.Fatalf("montando a requisição: %v", err)
	}
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", metodo, caminho, err)
	}
	defer resp.Body.Close()

	lido, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("lendo a resposta: %v", err)
	}
	return resposta{Status: resp.StatusCode, Corpo: lido, Headers: resp.Header}
}

// refreshDe extrai o cookie de sessão longa da resposta. Devolve nil quando a
// resposta apagou o cookie (Max-Age negativo ou valor vazio) — apagar é o que o
// handler faz em sessão morta, e reapresentar um cookie apagado seria o teste
// mentindo para si mesmo.
func refreshDe(r resposta) *http.Cookie {
	for _, c := range (&http.Response{Header: r.Headers}).Cookies() {
		if c.Name != auth.NomeDoCookieRefresh {
			continue
		}
		if c.Value == "" || c.MaxAge < 0 {
			return nil
		}
		return c
	}
	return nil
}

// entrarComCookie faz login e devolve o cookie de refresh, que é o que o ladrão
// deste cenário copia.
func (a *ambiente) entrarComCookie(t *testing.T, email, senha string) *http.Cookie {
	t.Helper()

	resp := a.chamarComCookies(t, http.MethodPost, "/auth/login", nil,
		map[string]string{"email": email, "password": senha})
	if resp.Status != http.StatusOK {
		t.Fatalf("login de %s: status %d — %s", email, resp.Status, resp.Corpo)
	}
	c := refreshDe(resp)
	if c == nil {
		t.Fatalf("login de %s não gravou o cookie %q", email, auth.NomeDoCookieRefresh)
	}
	return c
}

// familiaDoToken descobre a que família pertence o comprovante que o navegador
// está segurando. A família é a unidade que o roubo derruba — consultar por
// `user_id` confundiria "revogou a família certa" com "revogou tudo o que o
// usuário tinha", que são coisas diferentes e uma delas é outro defeito.
func (a *ambiente) familiaDoToken(t *testing.T, cookie *http.Cookie) uuid.UUID {
	t.Helper()

	var familia uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`SELECT family_id FROM refresh_tokens WHERE token_hash = $1`,
		auth.HashRefreshToken(cookie.Value)).Scan(&familia); err != nil {
		t.Fatalf("localizando a família do token: %v", err)
	}
	return familia
}

// familiaNoBanco conta os tokens da família e quantos deles já têm `revoked_at`.
// É a única prova aceitável de que a revogação COMMITOU: o log da aplicação
// dizia "família revogada" mesmo quando o rollback a desfazia.
func (a *ambiente) familiaNoBanco(t *testing.T, familia uuid.UUID) (total, revogados int) {
	t.Helper()

	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(*), count(revoked_at)
		  FROM refresh_tokens
		 WHERE family_id = $1`, familia).Scan(&total, &revogados); err != nil {
		t.Fatalf("lendo a família de tokens: %v", err)
	}
	return total, revogados
}

// ────────────────────────────── CRÍTICO 1 ───────────────────────────────────

// Em linguagem de negócio: alguém copiou o comprovante de sessão de uma
// recepcionista — extensão de navegador, notebook emprestado, backup do perfil.
// No instante em que o sistema percebe a cópia (o mesmo comprovante apresentado
// duas vezes), a sessão inteira daquela pessoa tem de morrer NO BANCO, e o
// ladrão tem de parar de entrar. Não basta o painel mostrar "sessão expirada"
// para o dono: se o ladrão continua renovando, o roubo continua.
//
// O defeito original: a revogação da família rodava DENTRO da transação que
// terminava devolvendo TOKEN_REUSED. O `TxManager` fazia rollback do erro e
// levava a revogação junto. O dono via 401, o alarme subia no log, e o ladrão
// seguia rotacionando o token roubado indefinidamente.
//
// Este teste falha se alguém puser a revogação de volta dentro dessa transação,
// porque ele não pergunta ao service se revogou — ele lê `refresh_tokens` depois
// do commit e tenta usar o token do ladrão de novo.
func TestReusoDeRefreshMataAFamiliaNoBancoEDerrubaOLadrao(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	perfil := a.criarPerfil(t, "reuso", []auth.Permissao{
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoVer, Scope: auth.EscopoOwn},
	})
	u := a.criarUsuario(t, "reuso", perfil)

	// Uma segunda sessão do MESMO usuário, aberta antes do roubo e nunca tocada
	// por ele: é o celular que ficou logado enquanto o roubo acontece no
	// desktop. Serve de controle — a revogação tem de acertar a família
	// comprometida e só ela. Revogar todas as sessões do usuário passaria na
	// asserção principal e seria outro defeito (a operação inteira deslogada a
	// cada alarme).
	celular := a.entrarComCookie(t, u.Email, senhaDeIntegracao)
	familiaDoCelular := a.familiaDoToken(t, celular)

	// O dono entra no desktop. Este é o comprovante que vaza.
	vazado := a.entrarComCookie(t, u.Email, senhaDeIntegracao)
	familia := a.familiaDoToken(t, vazado)
	if familia == familiaDoCelular {
		t.Fatal("dois logins caíram na mesma família: o controle não separaria as sessões")
	}

	// Rotação normal do painel: o comprovante vazado é trocado por um novo.
	rotacao := a.chamarComCookies(t, http.MethodPost, "/auth/refresh", []*http.Cookie{vazado}, nil)
	if rotacao.Status != http.StatusOK {
		t.Fatalf("rotação legítima: status = %d, esperado 200 — corpo: %s", rotacao.Status, rotacao.Corpo)
	}
	emUso := refreshDe(rotacao)
	if emUso == nil {
		t.Fatal("a rotação não devolveu cookie novo: o cenário do roubo não se monta")
	}

	// Controle de sanidade: até aqui a família tem token vivo. Sem isto, o teste
	// passaria mesmo se a sessão já estivesse morta por outro motivo — e a
	// asserção principal ("tudo revogado") seria verdadeira por acidente.
	if total, revogados := a.familiaNoBanco(t, familia); total != 2 || revogados != 1 {
		t.Fatalf("antes da detecção a família tinha %d tokens e %d revogados — "+
			"esperado 2 e 1 (o rotacionado e o novo)", total, revogados)
	}

	// O LADRÃO apresenta a cópia do comprovante já rotacionado.
	roubo := a.chamarComCookies(t, http.MethodPost, "/auth/refresh", []*http.Cookie{vazado}, nil)
	if roubo.Status != http.StatusUnauthorized {
		t.Fatalf("reuso do refresh: status = %d, esperado 401 — corpo: %s", roubo.Status, roubo.Corpo)
	}
	if code := roubo.codigoDeErro(t); code != "TOKEN_REUSED" {
		t.Errorf("code = %q, esperado TOKEN_REUSED", code)
	}
	if c := refreshDe(roubo); c != nil {
		t.Errorf("a resposta de roubo devolveu cookie vivo (%q): o navegador seguiria com sessão", c.Value)
	}

	// A PROVA: depois do 401, a família inteira está revogada NO BANCO.
	total, revogados := a.familiaNoBanco(t, familia)
	if total == 0 {
		t.Fatal("nenhum refresh token na família: o teste não provaria nada")
	}
	if revogados != total {
		t.Errorf("%d de %d tokens da família ficaram SEM revoked_at — a revogação foi desfeita "+
			"pelo rollback da transação que devolveu TOKEN_REUSED; a sessão roubada continua viva",
			total-revogados, total)
	}

	// E a consequência prática: o comprovante que o ladrão vinha usando morre.
	// Enquanto a revogação era desfeita pelo rollback, ESTA chamada respondia
	// 200 e o roubo seguia indefinidamente.
	morto := a.chamarComCookies(t, http.MethodPost, "/auth/refresh", []*http.Cookie{emUso}, nil)
	if morto.Status != http.StatusUnauthorized {
		t.Fatalf("o token que o ladrão vinha usando respondeu %d — ele continua renovando a sessão "+
			"depois de o sistema ter acusado o roubo; corpo: %s", morto.Status, morto.Corpo)
	}
	// TOKEN_INVALID e não TOKEN_REUSED: revogado sem sucessor é sessão morta,
	// não evidência nova de roubo.
	if code := morto.codigoDeErro(t); code != "TOKEN_INVALID" {
		t.Errorf("code = %q, esperado TOKEN_INVALID", code)
	}

	// A outra sessão do mesmo usuário sobrevive: o alarme derruba a família
	// comprometida, não a pessoa.
	if _, revogadosDoCelular := a.familiaNoBanco(t, familiaDoCelular); revogadosDoCelular != 0 {
		t.Errorf("a sessão que nunca foi tocada pelo ladrão também foi revogada: "+
			"o alarme desloga o usuário de todos os aparelhos (%d revogados)", revogadosDoCelular)
	}
	viva := a.chamarComCookies(t, http.MethodPost, "/auth/refresh", []*http.Cookie{celular}, nil)
	if viva.Status != http.StatusOK {
		t.Errorf("a outra sessão respondeu %d, esperado 200", viva.Status)
	}

	// O dono precisa conseguir voltar: derrubar a família não pode trancar a
	// conta. Sem este controle, uma trava que revogasse tudo para sempre
	// passaria nas asserções acima e deixaria a recepcionista de fora do sistema.
	if novo := a.entrarComCookie(t, u.Email, senhaDeIntegracao); novo.Value == "" {
		t.Error("o dono não conseguiu abrir sessão nova depois do roubo")
	}
}

// Contraprova do CRÍTICO 1: logout normal NÃO é roubo. O mesmo mecanismo que
// derruba a família tem de saber distinguir "o usuário saiu" de "alguém copiou
// o comprovante" — senão todo logout viraria alarme de segurança e o alarme
// deixaria de significar alguma coisa.
func TestLogoutNaoEhTratadoComoRouboDeSessao(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	perfil := a.criarPerfil(t, "saida", []auth.Permissao{
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoVer, Scope: auth.EscopoOwn},
	})
	u := a.criarUsuario(t, "saida", perfil)

	cookie := a.entrarComCookie(t, u.Email, senhaDeIntegracao)

	saida := a.chamarAutenticadoComCookies(t, http.MethodPost, "/auth/logout", u.Token,
		[]*http.Cookie{cookie}, nil)
	if saida.Status != http.StatusNoContent {
		t.Fatalf("logout: status = %d, esperado 204 — corpo: %s", saida.Status, saida.Corpo)
	}

	depois := a.chamarComCookies(t, http.MethodPost, "/auth/refresh", []*http.Cookie{cookie}, nil)
	if depois.Status != http.StatusUnauthorized {
		t.Fatalf("refresh depois do logout: status = %d, esperado 401", depois.Status)
	}
	if code := depois.codigoDeErro(t); code != "TOKEN_INVALID" {
		t.Errorf("code = %q, esperado TOKEN_INVALID — sair do sistema não é evidência de roubo, "+
			"e TOKEN_REUSED aqui transformaria cada logout num alarme falso", code)
	}
}

// ────────────────────────────── CRÍTICO 2 ───────────────────────────────────

// Em linguagem de negócio: ninguém se promove sozinho — nem o dono da conta mais
// forte da instalação. Mudar o PRÓPRIO papel é sempre recusado, porque é o único
// movimento que não tem revisor: quem edita e quem é editado são a mesma pessoa.
//
// A metade "quem tem `users:editar` e não tem `roles:editar` não se promove" já
// está em TestSuporteNaoSePromoveTrocandoOProprioPapel. Esta aqui fecha a outra
// metade, que é a que costuma escapar: a trava não pode ser "quem não administra
// acesso não mexe no papel", senão o administrador continua sendo o caminho de
// escalada — basta o atacante chegar a uma conta de administrador com escopo
// reduzido e ampliá-la sozinho.
func TestNemOAdministradorTrocaOProprioPapel(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	administrador := a.perfilAdministrador(t, "chefao")
	outroForte := a.perfilAdministrador(t, "chefao2")

	chefe := a.criarUsuario(t, "chefao", administrador)

	patch := a.chamar(t, http.MethodPatch, "/users/"+chefe.ID.String(), chefe.Token,
		map[string]any{"role_id": outroForte.String()})
	if patch.Status != http.StatusUnprocessableEntity {
		t.Fatalf("PATCH do próprio role_id pelo administrador: status = %d, esperado 422 — corpo: %s",
			patch.Status, patch.Corpo)
	}
	if code := patch.codigoDeErro(t); code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, esperado VALIDATION_ERROR", code)
	}

	put := a.chamar(t, http.MethodPut, "/users/"+chefe.ID.String(), chefe.Token, map[string]any{
		"name":    "QA chefao",
		"email":   chefe.Email,
		"role_id": outroForte.String(),
	})
	if put.Status != http.StatusUnprocessableEntity {
		t.Errorf("PUT do próprio role_id pelo administrador: status = %d, esperado 422 — corpo: %s",
			put.Status, put.Corpo)
	}

	if papel := a.papelNoBanco(t, chefe.ID); papel != administrador {
		t.Fatalf("o papel do administrador foi GRAVADO (%s): a recusa veio depois do UPDATE", papel)
	}

	// Controle positivo 1: reenviar o MESMO papel não é atribuição. O PUT exige
	// `role_id` no corpo; se repetir o próprio papel fosse recusado, o
	// administrador não conseguiria corrigir o próprio telefone.
	mesmo := a.chamar(t, http.MethodPut, "/users/"+chefe.ID.String(), chefe.Token, map[string]any{
		"name":    "QA chefao renomeado",
		"email":   chefe.Email,
		"role_id": administrador.String(),
	})
	if mesmo.Status != http.StatusOK {
		t.Errorf("PUT reenviando o próprio papel: status = %d, esperado 200 — corpo: %s",
			mesmo.Status, mesmo.Corpo)
	}

	// Controle positivo 2: o administrador continua atribuindo papel a TERCEIRO.
	// Sem ele, uma trava que recusasse toda mudança de papel passaria acima e
	// deixaria a instalação sem como conceder acesso a ninguém.
	colega := a.criarUsuario(t, "colega-chefao", administrador)
	terceiro := a.chamar(t, http.MethodPatch, "/users/"+colega.ID.String(), chefe.Token,
		map[string]any{"role_id": outroForte.String()})
	if terceiro.Status != http.StatusOK {
		t.Fatalf("administrador atribuindo papel a terceiro: status = %d, esperado 200 — corpo: %s",
			terceiro.Status, terceiro.Corpo)
	}
	if papel := a.papelNoBanco(t, colega.ID); papel != outroForte {
		t.Errorf("o papel do colega não foi gravado: %s", papel)
	}
}

// ────────────────────────────── CRÍTICO 3 ───────────────────────────────────

// recursoNovoNoBanco insere no catálogo um recurso cujo código NÃO existe em
// lugar nenhum do código Go. É esse o ponto do teste: um mapa Go compilado não
// tem como conhecer um código sorteado em tempo de execução.
func (a *ambiente) recursoNovoNoBanco(t *testing.T, suportaOwn bool) string {
	t.Helper()

	codigo := "qa.inventado." + strings.ReplaceAll(uuid.NewString(), "-", "")[:10]

	if _, err := a.pool.Exec(a.ctx, `
		INSERT INTO resources (code, label, group_label, actions, supports_own, sort_order)
		VALUES ($1, 'Recurso inventado pelo QA', 'QA', $2::text[], $3, 999)`,
		codigo, auth.AcoesValidas, suportaOwn); err != nil {
		t.Fatalf("inserindo recurso no catálogo: %v", err)
	}
	t.Cleanup(func() {
		limpeza, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = a.pool.Exec(limpeza, `DELETE FROM resources WHERE code = $1`, codigo)
	})
	return codigo
}

// Em linguagem de negócio: quando o produto ganhar um módulo novo — "Manutenção",
// "Portaria" —, cadastrá-lo tem de ser um INSERT no catálogo. Se for preciso
// recompilar e publicar a API para que a tela de perfis ofereça "só os meus"
// naquele módulo, o RBAC voltou a ser código.
//
// O defeito original: `internal/modules/roles/catalogo.go` guardava num mapa Go
// quais recursos têm dono e quais ações cada um oferece. A tabela `resources`
// existia, o seed a populava, e ninguém a lia.
//
// Este teste não consegue ser satisfeito por mapa nenhum: o código do recurso é
// sorteado agora, e o MESMO processo, sem reiniciar, tem de mudar de resposta
// quando a coluna `supports_own` muda no banco.
func TestEscopoValidoVemDoBancoEAceitaRecursoNovoSemRecompilar(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	novo := a.recursoNovoNoBanco(t, true)

	gestor := a.criarUsuario(t, "catalogo-vivo", a.perfilRaiz(t))
	alvo := a.criarPerfil(t, "recebe-novo", nil)

	// 1. O catálogo que a tela consome já enxerga o recurso novo, com `own`.
	cat := a.chamar(t, http.MethodGet, "/roles/resources", gestor.Token, nil)
	if cat.Status != http.StatusOK {
		t.Fatalf("GET /roles/resources: status %d — %s", cat.Status, cat.Corpo)
	}
	var catalogo struct {
		Data []struct {
			Codigo  string   `json:"code"`
			Escopos []string `json:"scopes"`
			Acoes   []string `json:"actions"`
		} `json:"data"`
	}
	cat.decodificar(t, &catalogo)

	achou := false
	for _, r := range catalogo.Data {
		if r.Codigo != novo {
			continue
		}
		achou = true
		if !contem(r.Escopos, auth.EscopoOwn) {
			t.Errorf("recurso %q tem supports_own = true no banco, mas o catálogo oferece escopos %v",
				novo, r.Escopos)
		}
	}
	if !achou {
		t.Fatalf("o recurso %q foi inserido em `resources` e não apareceu em /roles/resources — "+
			"o catálogo continua saindo de uma lista compilada", novo)
	}

	// 2. E a matriz aceita `own` nele. Este é o passo que o mapa Go recusava.
	matriz := []auth.Permissao{{Resource: novo, Action: auth.AcaoVer, Scope: auth.EscopoOwn}}
	salvo := a.chamar(t, http.MethodPut, "/roles/"+alvo.String()+"/permissions", gestor.Token, matriz)
	if salvo.Status != http.StatusOK {
		t.Fatalf("conceder `own` num recurso que o banco diz ter dono: status = %d (%s) — corpo: %s",
			salvo.Status, salvo.codigoDeErro(t), salvo.Corpo)
	}

	var escopoGravado string
	if err := a.pool.QueryRow(a.ctx, `
		SELECT scope FROM role_permissions
		 WHERE role_id = $1 AND resource_code = $2 AND action = $3`,
		alvo, novo, auth.AcaoVer).Scan(&escopoGravado); err != nil {
		t.Fatalf("lendo a permissão gravada: %v", err)
	}
	if escopoGravado != auth.EscopoOwn {
		t.Errorf("escopo gravado = %q, esperado %q", escopoGravado, auth.EscopoOwn)
	}

	// 3. A prova de que a resposta é LIDA do banco a cada requisição, e não
	//    memorizada na subida: o mesmo binário, o mesmo processo, o mesmo
	//    recurso — só a coluna muda — e a resposta vira 422.
	if _, err := a.pool.Exec(a.ctx,
		`UPDATE resources SET supports_own = false WHERE code = $1`, novo); err != nil {
		t.Fatalf("mudando supports_own: %v", err)
	}

	recusado := a.chamar(t, http.MethodPut, "/roles/"+alvo.String()+"/permissions", gestor.Token, matriz)
	if recusado.Status != http.StatusUnprocessableEntity {
		t.Fatalf("depois de `supports_own = false`, conceder `own` devolveu %d, esperado 422 — "+
			"o escopo válido está congelado em algum lugar do binário; corpo: %s",
			recusado.Status, recusado.Corpo)
	}
	if code := recusado.codigoDeErro(t); code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, esperado VALIDATION_ERROR", code)
	}
}

// Mesma ideia pelo outro lado: as AÇÕES também são dado. Um recurso que o banco
// diz oferecer só `ver` não pode aceitar `excluir` na matriz — senão a tela de
// perfis desenha um botão que o produto não tem.
func TestAcoesValidasVemDoBancoPorRecurso(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	codigo := "qa.somente-ver." + strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	if _, err := a.pool.Exec(a.ctx, `
		INSERT INTO resources (code, label, group_label, actions, supports_own, sort_order)
		VALUES ($1, 'Só leitura', 'QA', ARRAY['ver']::text[], false, 999)`, codigo); err != nil {
		t.Fatalf("inserindo recurso: %v", err)
	}
	t.Cleanup(func() {
		limpeza, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = a.pool.Exec(limpeza, `DELETE FROM resources WHERE code = $1`, codigo)
	})

	gestor := a.criarUsuario(t, "acoes-do-banco", a.perfilRaiz(t))
	alvo := a.criarPerfil(t, "recebe-acoes", nil)

	recusado := a.chamar(t, http.MethodPut, "/roles/"+alvo.String()+"/permissions", gestor.Token,
		[]auth.Permissao{{Resource: codigo, Action: auth.AcaoExcluir, Scope: auth.EscopoAll}})
	if recusado.Status != http.StatusUnprocessableEntity {
		t.Fatalf("conceder `excluir` num recurso que só oferece `ver`: status = %d, esperado 422 — corpo: %s",
			recusado.Status, recusado.Corpo)
	}

	// Controle positivo: a ação que o banco oferece passa.
	aceito := a.chamar(t, http.MethodPut, "/roles/"+alvo.String()+"/permissions", gestor.Token,
		[]auth.Permissao{{Resource: codigo, Action: auth.AcaoVer, Scope: auth.EscopoAll}})
	if aceito.Status != http.StatusOK {
		t.Fatalf("conceder `ver` no mesmo recurso: status = %d, esperado 200 — corpo: %s",
			aceito.Status, aceito.Corpo)
	}
}

// Em linguagem de negócio: a gestão abre o perfil Corretor — o que o seed
// instala, não um fabricado pelo teste —, não mexe em nada e clica em "salvar".
// Tem de responder 200 e devolver a matriz idêntica.
//
// Antes da correção respondia 422: o mapa Go não sabia que `finance.commissions`
// (e outros) têm dono, então o `own` que o próprio seed havia gravado era
// recusado na volta. Efeito prático: para ajustar UMA permissão do corretor a
// gestão precisava antes rebaixar o escopo de vários recursos — e não percebia
// que estava ampliando o acesso de todos os corretores.
//
// O alvo é o perfil SEMEADO de propósito: uma fixture montada pelo teste só
// prova o que o teste já sabe. Quem descreve o produto é o seed.
func TestPerfilCorretorSemeadoSalvaSemAlteracao(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	var corretor uuid.UUID
	err := a.pool.QueryRow(a.ctx, `SELECT id FROM roles WHERE code = 'corretor'`).Scan(&corretor)
	if err != nil {
		t.Skipf("perfil `corretor` ausente: rode `go run ./cmd/seed` contra o banco de integração "+
			"antes de `go test -tags=integration` (%v)", err)
	}

	gestor := a.criarUsuario(t, "gestor-corretor", a.perfilRaiz(t))

	lido := a.chamar(t, http.MethodGet, "/roles/"+corretor.String(), gestor.Token, nil)
	if lido.Status != http.StatusOK {
		t.Fatalf("GET /roles/{corretor}: status %d — %s", lido.Status, lido.Corpo)
	}
	var detalhe struct {
		Data struct {
			Permissoes []auth.Permissao `json:"permissions"`
		} `json:"data"`
	}
	lido.decodificar(t, &detalhe)

	if len(detalhe.Data.Permissoes) == 0 {
		t.Fatal("o corretor semeado veio sem matriz: o teste não provaria nada")
	}
	// A matriz do corretor é quase toda `own` — é exatamente o que o mapa Go
	// recusava. Sem esta conferência, um seed que virasse tudo `all` faria o
	// teste passar sem cobrir o defeito.
	temOwn := false
	for _, p := range detalhe.Data.Permissoes {
		if p.Scope == auth.EscopoOwn {
			temOwn = true
			break
		}
	}
	if !temOwn {
		t.Fatal("o corretor semeado não tem nenhuma permissão `own`: o defeito original " +
			"não seria alcançado por este teste")
	}

	salvo := a.chamar(t, http.MethodPut, "/roles/"+corretor.String()+"/permissions",
		gestor.Token, detalhe.Data.Permissoes)
	if salvo.Status != http.StatusOK {
		t.Fatalf("salvar o Corretor semeado sem alteração devolveu %d (%s) — a gestão não consegue "+
			"ajustar uma permissão sem rebaixar o escopo de outras; corpo: %s",
			salvo.Status, salvo.codigoDeErro(t), salvo.Corpo)
	}

	var gravada struct {
		Data []auth.Permissao `json:"data"`
	}
	salvo.decodificar(t, &gravada)
	if !mesmaMatriz(detalhe.Data.Permissoes, gravada.Data) {
		t.Errorf("a matriz mudou ao salvar sem alteração:\nantes: %+v\ndepois: %+v",
			detalhe.Data.Permissoes, gravada.Data)
	}
}

// ─────────────────────────────── Auxiliares ─────────────────────────────────

func contem(lista []string, alvo string) bool {
	for _, x := range lista {
		if x == alvo {
			return true
		}
	}
	return false
}

// mesmaMatriz compara sem depender da ordem: a API pode ordenar como quiser, o
// que não pode é ganhar, perder ou rebaixar célula.
func mesmaMatriz(a, b []auth.Permissao) bool {
	if len(a) != len(b) {
		return false
	}
	conta := map[auth.Permissao]int{}
	for _, p := range a {
		conta[p]++
	}
	for _, p := range b {
		conta[p]--
	}
	for _, n := range conta {
		if n != 0 {
			return false
		}
	}
	return true
}
