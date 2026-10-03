//go:build integration

// Integração da API pela porta da frente: sobe a árvore de rotas inteira sobre
// um Postgres real e conversa com ela por HTTP.
//
// O que se testa aqui é o CONTRATO — status, code de erro, envelope, cookie,
// permissão e escopo — e não o interior dos services. Um teste que só passa
// porque conhece o caminho interno do service não protege quem consome a API.
package router

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/config"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

const senhaDeIntegracao = "senha-de-integracao-2026"

// ─────────────────────────── Infraestrutura do teste ────────────────────────

type ambiente struct {
	pool     *pgxpool.Pool
	servidor *httptest.Server
	ctx      context.Context
}

func subirAPI(t *testing.T) *ambiente {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL ausente: teste de integração pulado (use `make test-integration`)")
	}

	ctx := context.Background()
	pool, err := db.New(ctx, url)
	if err != nil {
		t.Fatalf("abrindo pool: %v", err)
	}
	t.Cleanup(pool.Close)

	cfg := config.Config{
		Env:         "test",
		JWTSecret:   "segredo-de-integracao-com-mais-de-32-bytes",
		AccessTTL:   15 * time.Minute,
		RefreshTTL:  720 * time.Hour,
		CORSOrigins: []string{"http://localhost:3000"},
		// Volume de mídia do site descartável por teste (docs/site-cms.md).
		MediaDir: t.TempDir(),
	}

	h, err := New(Opcoes{Config: cfg, Pool: pool})
	if err != nil {
		t.Fatalf("montando o router: %v", err)
	}

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	return &ambiente{pool: pool, servidor: srv, ctx: ctx}
}

// resposta é o que os testes inspecionam: status e corpo já lido.
type resposta struct {
	Status  int
	Corpo   []byte
	Headers http.Header
}

// codigoDeErro devolve o `error.code` do envelope, ou "" se a resposta não for
// de erro. O front reage ao code, nunca à mensagem — então é o code que o teste
// confere.
func (r resposta) codigoDeErro(t *testing.T) string {
	t.Helper()

	var envelope struct {
		Erro struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(r.Corpo, &envelope); err != nil {
		t.Fatalf("corpo fora do envelope de erro (status %d): %s", r.Status, r.Corpo)
	}
	return envelope.Erro.Code
}

func (r resposta) decodificar(t *testing.T, destino any) {
	t.Helper()
	if err := json.Unmarshal(r.Corpo, destino); err != nil {
		t.Fatalf("decodificando (status %d): %v — corpo: %s", r.Status, err, r.Corpo)
	}
}

// chamarNaRaiz bate FORA de `/api/v1`. Existe só para as sondas, que moram na
// raiz de propósito (ver `router.Rota.NaRaiz`): quem as chama é o healthcheck do
// Compose e o proxy da frente, com caminho fixo de infraestrutura.
func (a *ambiente) chamarNaRaiz(t *testing.T, metodo, caminho string) resposta {
	t.Helper()

	req, err := http.NewRequestWithContext(a.ctx, metodo, a.servidor.URL+caminho, nil)
	if err != nil {
		t.Fatalf("montando a requisição: %v", err)
	}
	resp, err := a.servidor.Client().Do(req)
	if err != nil {
		t.Fatalf("chamando %s %s: %v", metodo, caminho, err)
	}
	defer resp.Body.Close()

	corpoLido, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("lendo o corpo de %s %s: %v", metodo, caminho, err)
	}
	return resposta{Status: resp.StatusCode, Corpo: corpoLido}
}

// chamada monta a requisição. `token` vazio manda sem Authorization.
func (a *ambiente) chamar(t *testing.T, metodo, caminho, token string, corpo any) resposta {
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

	// Sem redirect automático e sem cookie jar: cada teste diz explicitamente o
	// que manda, para não haver estado escondido entre um caso e outro.
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

// ─────────────────────────── Fixtures ───────────────────────────────────────

type usuarioDeTeste struct {
	ID     uuid.UUID
	RoleID uuid.UUID
	Email  string
	Token  string
}

// garantirRecursos deixa no catálogo os recursos que o teste vai conceder. A FK
// de role_permissions exige que o recurso exista, e o teste não pode depender de
// `make seed` ter rodado antes.
func (a *ambiente) garantirRecursos(t *testing.T) {
	t.Helper()

	const q = `
		INSERT INTO resources (code, label, group_label, actions, supports_own, sort_order)
		VALUES ($1, $2, $3, $4::text[], $5, $6)
		ON CONFLICT (code) DO NOTHING`

	for _, r := range []struct {
		codigo, rotulo, grupo string
		acoes                 []string
		suportaOwn            bool
		ordem                 int32
	}{
		{auth.RecursoUsuarios, "Usuários", "Configurações", auth.AcoesValidas, false, 60},
		{auth.RecursoPerfis, "Perfis de acesso", "Configurações", auth.AcoesValidas, false, 61},
		{"chat", "Chat e WhatsApp", "Atendimento", []string{"ver", "criar", "editar"}, true, 40},
	} {
		if _, err := a.pool.Exec(a.ctx, q, r.codigo, r.rotulo, r.grupo, r.acoes, r.suportaOwn, r.ordem); err != nil {
			t.Fatalf("catálogo de recursos (%s): %v", r.codigo, err)
		}
	}
}

// criarPerfil cria um perfil de teste com a matriz informada e devolve o id.
func (a *ambiente) criarPerfil(t *testing.T, apelido string, permissoes []auth.Permissao) uuid.UUID {
	t.Helper()

	codigo := "qa_" + apelido + "_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`INSERT INTO roles (code, name) VALUES ($1, $2) RETURNING id`,
		codigo, "QA "+apelido).Scan(&id); err != nil {
		t.Fatalf("criando perfil: %v", err)
	}
	t.Cleanup(func() {
		limpeza, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := a.pool.Exec(limpeza, `DELETE FROM roles WHERE id = $1`, id); err != nil {
			t.Logf("LIMPEZA INCOMPLETA: o perfil de teste %s ficou no banco (%v) — normalmente porque um "+
				"usuário que aponta para ele também não saiu.", id, err)
		}
	})

	for _, p := range permissoes {
		if _, err := a.pool.Exec(a.ctx, `
			INSERT INTO role_permissions (role_id, resource_code, action, scope)
			VALUES ($1, $2, $3, $4)`, id, p.Resource, p.Action, p.Scope); err != nil {
			t.Fatalf("concedendo %s:%s: %v", p.Resource, p.Action, err)
		}
	}
	return id
}

// criarUsuario cria a conta e já faz login, devolvendo o access token — é assim
// que todos os testes seguintes falam com a API.
func (a *ambiente) criarUsuario(t *testing.T, apelido string, perfil uuid.UUID) usuarioDeTeste {
	t.Helper()

	sufixo := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	email := "qa-" + apelido + "-" + sufixo + "@wh.local"

	hash, err := auth.Hash(senhaDeIntegracao)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	var propriedade uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO properties (name, slug, timezone)
		VALUES ('White House Village', 'white-house-village', 'America/Fortaleza')
		ON CONFLICT (slug) DO UPDATE SET slug = EXCLUDED.slug
		RETURNING id`).Scan(&propriedade); err != nil {
		t.Fatalf("propriedade: %v", err)
	}

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `
		INSERT INTO users (property_id, role_id, name, email, password_hash)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		propriedade, perfil, "QA "+apelido, email, hash).Scan(&id); err != nil {
		t.Fatalf("criando usuário: %v", err)
	}
	t.Cleanup(func() {
		limpeza, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		// `audit_log.actor_id` referencia `users` SEM `ON DELETE` — e está
		// certo assim: apagar a pessoa não pode apagar a prova do que ela fez.
		// A consequência para o teste é que, desde que a Fase 1 passou a
		// auditar, o `DELETE` do usuário estoura `23503` e a conta descartável
		// FICA no banco. Medido nesta árvore antes do conserto: 7 usuários e 9
		// perfis órfãos depois de uma passada da suíte.
		//
		// A trilha de um usuário de teste é descartável junto com ele.
		_, _ = a.pool.Exec(limpeza, `DELETE FROM audit_log WHERE actor_id = $1`, id)

		// O erro deixou de ser engolido: limpeza que falha em silêncio é como a
		// suíte passa a depender do lixo que ela mesma deixou.
		if _, err := a.pool.Exec(limpeza, `DELETE FROM users WHERE id = $1`, id); err != nil {
			t.Logf("LIMPEZA INCOMPLETA: o usuário de teste %s ficou no banco (%v) — alguma linha ainda o "+
				"referencia. Cada execução da suíte deixa mais um.", id, err)
		}
	})

	u := usuarioDeTeste{ID: id, RoleID: perfil, Email: email}
	u.Token = a.entrar(t, email, senhaDeIntegracao)
	return u
}

func (a *ambiente) entrar(t *testing.T, email, senha string) string {
	t.Helper()

	resp := a.chamar(t, http.MethodPost, "/auth/login", "",
		map[string]string{"email": email, "password": senha})
	if resp.Status != http.StatusOK {
		t.Fatalf("login de %s: status %d — %s", email, resp.Status, resp.Corpo)
	}

	var corpo struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	resp.decodificar(t, &corpo)
	if corpo.Data.AccessToken == "" {
		t.Fatalf("login de %s não devolveu access_token", email)
	}
	return corpo.Data.AccessToken
}

// ─────────────────────────── Sessão pela porta da frente ────────────────────

// Em linguagem de negócio: o comprovante de sessão longa viaja num cookie que o
// JavaScript da página não consegue ler. Se ele aparecesse no corpo da resposta,
// bastaria um script injetado no painel para copiar a sessão de quem estiver
// logado.
func TestLoginDevolveCookieHttpOnlyENuncaOTokenNoCorpo(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)
	perfil := a.criarPerfil(t, "sessao", nil)
	u := a.criarUsuario(t, "sessao", perfil)

	resp := a.chamar(t, http.MethodPost, "/auth/login", "",
		map[string]string{"email": u.Email, "password": senhaDeIntegracao})
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, corpo: %s", resp.Status, resp.Corpo)
	}

	if strings.Contains(string(resp.Corpo), "refresh") {
		t.Errorf("o corpo do login menciona refresh: %s", resp.Corpo)
	}

	var cookie *http.Cookie
	for _, c := range (&http.Response{Header: resp.Headers}).Cookies() {
		if c.Name == auth.NomeDoCookieRefresh {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatalf("login não gravou o cookie %q — sem ele o painel não mantém sessão",
			auth.NomeDoCookieRefresh)
	}
	if !cookie.HttpOnly {
		t.Error("cookie sem HttpOnly: um XSS no painel exfiltra a sessão de 30 dias")
	}
	if cookie.Path != auth.CaminhoDoCookieRefresh {
		t.Errorf("Path = %q, esperado %q", cookie.Path, auth.CaminhoDoCookieRefresh)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, esperado Lax", cookie.SameSite)
	}
}

func TestLoginComSenhaErradaDevolve401ComCodeEstavel(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)
	perfil := a.criarPerfil(t, "senha", nil)
	u := a.criarUsuario(t, "senha", perfil)

	resp := a.chamar(t, http.MethodPost, "/auth/login", "",
		map[string]string{"email": u.Email, "password": "errada"})

	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401 — corpo: %s", resp.Status, resp.Corpo)
	}
	if code := resp.codigoDeErro(t); code != "INVALID_CREDENTIALS" {
		t.Fatalf("code = %q, esperado INVALID_CREDENTIALS", code)
	}
}

// /auth/me é o que o painel lê para esconder o que o usuário não alcança. O
// espelho tem de refletir a matriz do banco, senão a tela oferece botão que a
// API recusa.
func TestMeDevolveAMatrizDoPerfil(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	perfil := a.criarPerfil(t, "me", []auth.Permissao{
		{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoOwn},
	})
	u := a.criarUsuario(t, "me", perfil)

	resp := a.chamar(t, http.MethodGet, "/auth/me", u.Token, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, corpo: %s", resp.Status, resp.Corpo)
	}

	var corpo struct {
		Data struct {
			Usuario struct {
				ID    string `json:"id"`
				Email string `json:"email"`
				Role  string `json:"role"`
			} `json:"user"`
			Permissoes []auth.Permissao `json:"permissions"`
		} `json:"data"`
	}
	resp.decodificar(t, &corpo)

	if corpo.Data.Usuario.Email != u.Email {
		t.Errorf("user.email = %q, esperado %q", corpo.Data.Usuario.Email, u.Email)
	}
	if len(corpo.Data.Permissoes) != 1 {
		t.Fatalf("permissões = %d, esperado 1: %+v", len(corpo.Data.Permissoes), corpo.Data.Permissoes)
	}
	if p := corpo.Data.Permissoes[0]; p.Resource != "chat" || p.Action != auth.AcaoVer || p.Scope != auth.EscopoOwn {
		t.Errorf("permissão = %+v, esperado chat/ver/own", p)
	}
}

// ─────────────────────────── Permissão e escopo ─────────────────────────────

// docs/spec.md §1: "Corretor autenticado recebe 403 (...) — provado por teste de
// integração."
//
// Em linguagem de negócio: um corretor logado que digite o endereço da tela de
// usuários na barra do navegador tem de bater numa porta fechada. Esconder o
// menu não é trancar a porta.
func TestPerfilSemPermissaoRecebe403EmTodosOsVerbos(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	// O perfil enxerga chat e nada mais — é o corretor do seed em miniatura.
	perfil := a.criarPerfil(t, "corretor", []auth.Permissao{
		{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoOwn},
	})
	u := a.criarUsuario(t, "corretor", perfil)

	casos := []struct{ metodo, caminho string }{
		{http.MethodGet, "/users"},
		{http.MethodPost, "/users"},
		{http.MethodGet, "/users/" + u.ID.String()},
		{http.MethodPatch, "/users/" + u.ID.String()},
		{http.MethodDelete, "/users/" + u.ID.String()},
		{http.MethodGet, "/roles"},
		{http.MethodGet, "/roles/resources"},
	}

	for _, caso := range casos {
		t.Run(caso.metodo+"_"+caso.caminho, func(t *testing.T) {
			resp := a.chamar(t, caso.metodo, caso.caminho, u.Token, map[string]string{})
			if resp.Status != http.StatusForbidden {
				t.Fatalf("status = %d, esperado 403 — corpo: %s", resp.Status, resp.Corpo)
			}
			if code := resp.codigoDeErro(t); code != "FORBIDDEN" {
				t.Fatalf("code = %q, esperado FORBIDDEN", code)
			}
		})
	}
}

// CLAUDE.md, convenção do time: "Escopo `own` vira `AND owner_id = $user` no
// SQL, não filtro em memória."
//
// Em linguagem de negócio: quem só pode ver o que é seu não vê o resto — e o
// contador da listagem também não conta o resto. Um total que diz "137" numa
// lista de 1 linha entrega o tamanho da base a quem não deveria conhecê-lo.
func TestEscopoOwnRestringeAListagemEOTotalDaPaginacao(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	perfilRestrito := a.criarPerfil(t, "own", []auth.Permissao{
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoVer, Scope: auth.EscopoOwn},
	})
	restrito := a.criarUsuario(t, "own", perfilRestrito)

	// Vizinhos que existem no banco e não podem aparecer para ele.
	perfilAmplo := a.criarPerfil(t, "all", []auth.Permissao{
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoVer, Scope: auth.EscopoAll},
	})
	vizinho := a.criarUsuario(t, "vizinho", perfilAmplo)
	a.criarUsuario(t, "outro", perfilAmplo)

	resp := a.chamar(t, http.MethodGet, "/users?per_page=100", restrito.Token, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, corpo: %s", resp.Status, resp.Corpo)
	}

	var corpo struct {
		Data []struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"data"`
		Meta struct {
			Page       int   `json:"page"`
			PerPage    int   `json:"per_page"`
			Total      int64 `json:"total"`
			TotalPages int   `json:"total_pages"`
		} `json:"meta"`
	}
	resp.decodificar(t, &corpo)

	if len(corpo.Data) != 1 {
		t.Fatalf("linhas = %d, esperado 1 (só a própria conta): %+v", len(corpo.Data), corpo.Data)
	}
	if corpo.Data[0].ID != restrito.ID.String() {
		t.Errorf("linha devolvida = %s, esperado a própria conta %s", corpo.Data[0].Email, restrito.Email)
	}
	if corpo.Meta.Total != 1 {
		t.Errorf("meta.total = %d, esperado 1 — o total precisa contar o mesmo WHERE da listagem, "+
			"senão a paginação denuncia o tamanho da base", corpo.Meta.Total)
	}
	if corpo.Meta.TotalPages != 1 {
		t.Errorf("meta.total_pages = %d, esperado 1", corpo.Meta.TotalPages)
	}

	// Fora do escopo, o registro do vizinho não existe: 404 e não 403, porque um
	// 403 confirmaria que aquele id é de alguém.
	detalhe := a.chamar(t, http.MethodGet, "/users/"+vizinho.ID.String(), restrito.Token, nil)
	if detalhe.Status != http.StatusNotFound {
		t.Fatalf("detalhe do vizinho: status = %d, esperado 404 — corpo: %s", detalhe.Status, detalhe.Corpo)
	}

	// E quem tem escopo `all` continua vendo todo mundo.
	amplo := a.chamar(t, http.MethodGet, "/users?per_page=100", vizinho.Token, nil)
	var visaoAmpla struct {
		Meta struct {
			Total int64 `json:"total"`
		} `json:"meta"`
	}
	amplo.decodificar(t, &visaoAmpla)
	if visaoAmpla.Meta.Total < 3 {
		t.Errorf("com escopo all o total foi %d, esperado ao menos as 3 contas do teste", visaoAmpla.Meta.Total)
	}
}

// ─────────────────────────── Usuários ───────────────────────────────────────

// Em linguagem de negócio: dois cadastros com o mesmo e-mail não podem existir —
// e quem decide isso é o banco, não uma consulta antes de gravar, que perderia a
// corrida entre dois cadastros simultâneos.
func TestCadastrarComEmailRepetidoDevolve409EmailInUse(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	perfil := a.criarPerfil(t, "gestao", []auth.Permissao{
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoCriar, Scope: auth.EscopoAll},
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoExcluir, Scope: auth.EscopoAll},
		// Cadastrar usuário é atribuir papel, e atribuir papel é administrar
		// acesso: sem `roles:editar` o POST devolve 403 (ver
		// TestCriarUsuarioNaoConcedePapelAcimaDoAtor).
		{Resource: auth.RecursoPerfis, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
	})
	gestor := a.criarUsuario(t, "gestao", perfil)

	novo := map[string]any{
		"name":     "Recepção",
		"email":    "qa-duplicado-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10] + "@wh.local",
		"password": senhaDeIntegracao,
		"role_id":  perfil.String(),
	}

	criado := a.chamar(t, http.MethodPost, "/users", gestor.Token, novo)
	if criado.Status != http.StatusCreated {
		t.Fatalf("status = %d, esperado 201 — corpo: %s", criado.Status, criado.Corpo)
	}
	if loc := criado.Headers.Get("Location"); !strings.HasPrefix(loc, "/api/v1/users/") {
		t.Errorf("Location = %q, esperado a URI do recurso criado", loc)
	}

	var corpo struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	criado.decodificar(t, &corpo)
	t.Cleanup(func() {
		limpeza, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = a.pool.Exec(limpeza, `DELETE FROM users WHERE id = $1`, corpo.Data.ID)
	})

	repetido := a.chamar(t, http.MethodPost, "/users", gestor.Token, novo)
	if repetido.Status != http.StatusConflict {
		t.Fatalf("status = %d, esperado 409 — corpo: %s", repetido.Status, repetido.Corpo)
	}
	if code := repetido.codigoDeErro(t); code != "EMAIL_IN_USE" {
		t.Fatalf("code = %q, esperado EMAIL_IN_USE", code)
	}
}

// O DELETE do contrato tem "efeito idêntico a PATCH {active:false}", e reativar
// é PATCH {active:true}.
//
// Em linguagem de negócio: desligar alguém não apaga o histórico dele — as
// reservas que ele fechou continuam com autor. E readmitir é religar a conta, não
// recadastrar a pessoa.
func TestDeleteDesativaSemApagarEPermiteReativar(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	perfil := a.criarPerfil(t, "rh", []auth.Permissao{
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoExcluir, Scope: auth.EscopoAll},
		// Sem esta permissão o alvo seria o "último administrador" e a API
		// recusaria desativá-lo — que é outra regra, testada logo abaixo.
		{Resource: auth.RecursoPerfis, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
	})
	gestor := a.criarUsuario(t, "rh", perfil)
	alvo := a.criarUsuario(t, "alvo", perfil)

	apagou := a.chamar(t, http.MethodDelete, "/users/"+alvo.ID.String(), gestor.Token, nil)
	if apagou.Status != http.StatusNoContent {
		t.Fatalf("status = %d, esperado 204 — corpo: %s", apagou.Status, apagou.Corpo)
	}

	// A linha continua no banco, só que inativa.
	depois := a.chamar(t, http.MethodGet, "/users/"+alvo.ID.String(), gestor.Token, nil)
	if depois.Status != http.StatusOK {
		t.Fatalf("o usuário desativado sumiu da API: status %d — %s", depois.Status, depois.Corpo)
	}
	var corpo struct {
		Data struct {
			Ativo bool `json:"active"`
		} `json:"data"`
	}
	depois.decodificar(t, &corpo)
	if corpo.Data.Ativo {
		t.Error("active continuou true depois do DELETE")
	}

	// E a sessão dele cai no mesmo instante: desativar que só vale no próximo
	// login não desativa nada.
	semAcesso := a.chamar(t, http.MethodGet, "/auth/me", alvo.Token, nil)
	if semAcesso.Status != http.StatusUnauthorized {
		t.Errorf("a sessão do desativado continuou valendo: status %d", semAcesso.Status)
	}

	reativado := a.chamar(t, http.MethodPatch, "/users/"+alvo.ID.String(), gestor.Token,
		map[string]any{"active": true})
	if reativado.Status != http.StatusOK {
		t.Fatalf("reativação: status = %d — corpo: %s", reativado.Status, reativado.Corpo)
	}
	reativado.decodificar(t, &corpo)
	if !corpo.Data.Ativo {
		t.Error("PATCH {active:true} não reativou a conta")
	}
}

// Em linguagem de negócio: ninguém pode se desligar sozinho e ninguém pode
// desligar o último usuário capaz de administrar o sistema — sem isso a
// instalação fica sem ninguém que consiga devolver acesso a quem quer que seja.
func TestNaoDaParaDesativarASiMesmo(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	perfil := a.criarPerfil(t, "auto", []auth.Permissao{
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoExcluir, Scope: auth.EscopoAll},
	})
	u := a.criarUsuario(t, "auto", perfil)

	resp := a.chamar(t, http.MethodDelete, "/users/"+u.ID.String(), u.Token, nil)
	if resp.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, esperado 422 — corpo: %s", resp.Status, resp.Corpo)
	}
	if code := resp.codigoDeErro(t); code != "VALIDATION_ERROR" {
		t.Fatalf("code = %q, esperado VALIDATION_ERROR", code)
	}
}

// ─────────────────────────── Perfis e catálogo ──────────────────────────────

// A grade da tela de perfis é desenhada com o que `GET /roles/resources`
// devolve. O catálogo é DADO (`resources`, regra 8 do CLAUDE.md): `supports_own`
// é a única fonte de "este recurso aceita só os meus", e `actions` é a única
// fonte de quais colunas a grade desenha.
//
// O que este teste protege, em linguagem de negócio: enquanto a resposta vinha
// de uma lista em Go, ela oferecia "excluir" no razão financeiro (append-only) e
// no chat (mensagem não se apaga), não oferecia "só os meus" em chat, agenda,
// orçamentos e calendário — e o perfil Corretor ficava impossível de salvar.
func TestCatalogoDeRecursosRefleteOSupportsOwnDoBanco(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	perfil := a.criarPerfil(t, "catalogo", []auth.Permissao{
		{Resource: auth.RecursoPerfis, Action: auth.AcaoVer, Scope: auth.EscopoAll},
	})
	u := a.criarUsuario(t, "catalogo", perfil)

	resp := a.chamar(t, http.MethodGet, "/roles/resources", u.Token, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, corpo: %s", resp.Status, resp.Corpo)
	}

	var corpo struct {
		Data []struct {
			Codigo  string   `json:"code"`
			Escopos []string `json:"scopes"`
			Acoes   []string `json:"actions"`
		} `json:"data"`
	}
	resp.decodificar(t, &corpo)
	if len(corpo.Data) == 0 {
		t.Fatal("catálogo vazio")
	}

	// A verdade está na tabela: é ela que o seed escreve e a tela consome.
	type metaDoBanco struct {
		own   bool
		acoes []string
	}
	esperado := map[string]metaDoBanco{}
	linhas, err := a.pool.Query(a.ctx, `SELECT code, supports_own, actions FROM resources`)
	if err != nil {
		t.Fatalf("lendo resources: %v", err)
	}
	for linhas.Next() {
		var codigo string
		var m metaDoBanco
		if err := linhas.Scan(&codigo, &m.own, &m.acoes); err != nil {
			linhas.Close()
			t.Fatalf("lendo resources: %v", err)
		}
		esperado[codigo] = m
	}
	linhas.Close()
	if err := linhas.Err(); err != nil {
		t.Fatalf("lendo resources: %v", err)
	}

	for _, r := range corpo.Data {
		ofereceOwn := false
		for _, e := range r.Escopos {
			if e == auth.EscopoOwn {
				ofereceOwn = true
			}
		}
		if ofereceOwn != esperado[r.Codigo].own {
			t.Errorf("recurso %q: /roles/resources oferece own = %v, mas resources.supports_own = %v",
				r.Codigo, ofereceOwn, esperado[r.Codigo].own)
		}
		if !mesmasAcoes(r.Acoes, esperado[r.Codigo].acoes) {
			t.Errorf("recurso %q: /roles/resources oferece %v, mas resources.actions = %v — "+
				"a grade desenharia uma coluna que o produto não faz",
				r.Codigo, r.Acoes, esperado[r.Codigo].acoes)
		}
	}
}

// mesmasAcoes compara os dois conjuntos sem depender da ordem.
func mesmasAcoes(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	conta := map[string]int{}
	for _, x := range a {
		conta[x]++
	}
	for _, x := range b {
		conta[x]--
	}
	for _, n := range conta {
		if n != 0 {
			return false
		}
	}
	return true
}

// Em linguagem de negócio: abrir o perfil Corretor na tela de perfis e clicar em
// "salvar" sem mexer em nada tem de funcionar. Se não funciona, a gestão não
// consegue ajustar uma única permissão sem antes rebaixar o escopo de quatro
// recursos — e nem sempre percebe que rebaixou.
func TestSalvarAMatrizDoPerfilSemMudarNadaEhAceito(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	// A matriz que o seed instala no Corretor para o chat: ver, em escopo `own`.
	matrizInstalada := []auth.Permissao{
		{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoOwn},
	}
	alvo := a.criarPerfil(t, "roundtrip", matrizInstalada)

	perfilAdmin := a.criarPerfil(t, "admin", []auth.Permissao{
		{Resource: auth.RecursoPerfis, Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: auth.RecursoPerfis, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
		// Precisa possuir a célula para poder concedê-la: o teto recusa conceder
		// o que o ator não tem. `all` cobre `own`, então gravar `chat/ver/own`
		// no perfil alvo é conceder MENOS do que este ator possui — que é
		// exatamente o que a ida-e-volta precisa exercitar.
		{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoAll},
	})
	gestor := a.criarUsuario(t, "admin", perfilAdmin)

	// Lê como a API mostra o perfil...
	lido := a.chamar(t, http.MethodGet, "/roles/"+alvo.String(), gestor.Token, nil)
	if lido.Status != http.StatusOK {
		t.Fatalf("GET /roles/{id}: status %d — %s", lido.Status, lido.Corpo)
	}
	var detalhe struct {
		Data struct {
			Permissoes []auth.Permissao `json:"permissions"`
		} `json:"data"`
	}
	lido.decodificar(t, &detalhe)

	if len(detalhe.Data.Permissoes) != 1 || detalhe.Data.Permissoes[0].Scope != auth.EscopoOwn {
		t.Fatalf("a API leu a matriz como %+v, esperado chat/ver/own", detalhe.Data.Permissoes)
	}

	// ...e devolve exatamente o que leu.
	salvo := a.chamar(t, http.MethodPut, "/roles/"+alvo.String()+"/permissions",
		gestor.Token, detalhe.Data.Permissoes)
	if salvo.Status != http.StatusOK {
		t.Fatalf("salvar a matriz sem alteração devolveu %d (%s) — corpo: %s",
			salvo.Status, salvo.codigoDeErro(t), salvo.Corpo)
	}

	var gravada struct {
		Data []auth.Permissao `json:"data"`
	}
	salvo.decodificar(t, &gravada)
	if len(gravada.Data) != 1 || gravada.Data[0].Scope != auth.EscopoOwn {
		t.Fatalf("depois de salvar, a matriz virou %+v — o escopo `own` foi perdido", gravada.Data)
	}
}

// O perfil de sistema não se altera: bastaria remover `roles:editar` do admin
// para ninguém mais conseguir consertar o acesso de ninguém.
func TestMatrizDoPerfilDeSistemaEhImutavel(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	perfilAdmin := a.criarPerfil(t, "sistema", []auth.Permissao{
		{Resource: auth.RecursoPerfis, Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: auth.RecursoPerfis, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
		{Resource: auth.RecursoPerfis, Action: auth.AcaoExcluir, Scope: auth.EscopoAll},
	})
	gestor := a.criarUsuario(t, "sistema", perfilAdmin)

	sistema := a.criarPerfil(t, "travado", nil)
	if _, err := a.pool.Exec(a.ctx, `UPDATE roles SET is_system = true WHERE id = $1`, sistema); err != nil {
		t.Fatalf("marcando como perfil de sistema: %v", err)
	}

	for nome, resp := range map[string]resposta{
		"PUT /permissions": a.chamar(t, http.MethodPut, "/roles/"+sistema.String()+"/permissions",
			gestor.Token, []auth.Permissao{{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoAll}}),
		"PATCH": a.chamar(t, http.MethodPatch, "/roles/"+sistema.String(), gestor.Token,
			map[string]any{"name": "Outro nome"}),
		"DELETE": a.chamar(t, http.MethodDelete, "/roles/"+sistema.String(), gestor.Token, nil),
	} {
		if resp.Status != http.StatusConflict {
			t.Errorf("%s: status = %d, esperado 409 — corpo: %s", nome, resp.Status, resp.Corpo)
			continue
		}
		if code := resp.codigoDeErro(t); code != "ROLE_IMMUTABLE" {
			t.Errorf("%s: code = %q, esperado ROLE_IMMUTABLE", nome, code)
		}
	}
}

// Em linguagem de negócio: perfil com gente dentro não some — apagar deixaria
// usuários sem papel nenhum, e um usuário sem papel não tem sequer como entrar.
func TestPerfilComUsuariosNaoPodeSerExcluido(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	perfilAdmin := a.criarPerfil(t, "excluir", []auth.Permissao{
		{Resource: auth.RecursoPerfis, Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: auth.RecursoPerfis, Action: auth.AcaoExcluir, Scope: auth.EscopoAll},
	})
	gestor := a.criarUsuario(t, "excluir", perfilAdmin)

	resp := a.chamar(t, http.MethodDelete, "/roles/"+perfilAdmin.String(), gestor.Token, nil)
	if resp.Status != http.StatusConflict {
		t.Fatalf("status = %d, esperado 409 — corpo: %s", resp.Status, resp.Corpo)
	}
	if code := resp.codigoDeErro(t); code != "ROLE_IN_USE" {
		t.Fatalf("code = %q, esperado ROLE_IN_USE", code)
	}
}

// ────────────────── Escalada de privilégio (travas de /users) ───────────────

// perfilAdministrador é a fixture do ator que administra acesso de verdade.
// perfilRaiz devolve o perfil `admin` do seed — o único `is_system`, definido
// como "tudo". Testes cuja premissa é "um administrador de verdade administra
// perfis" precisam DELE, não de `perfilAdministrador`: aquele concede oito
// células (usuários, perfis e chat) e o teto de privilégio corretamente recusa
// que ele conceda agenda, contatos ou financeiro, que não possui.
func (a *ambiente) perfilRaiz(t *testing.T) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := a.pool.QueryRow(a.ctx,
		`SELECT id FROM roles WHERE is_system AND code = 'admin'`).Scan(&id)
	if err != nil {
		t.Skipf("perfil raiz ausente: rode `go run ./cmd/seed` contra o banco de integração (%v)", err)
	}
	return id
}

// perfilAdministrador cria um GESTOR DE ACESSOS: usuários, perfis e chat, e
// mais nada. Não é o perfil raiz — e é de propósito, porque é com ele que se
// prova que o teto de privilégio segura quem administra contas sem possuir o
// sistema inteiro.
func (a *ambiente) perfilAdministrador(t *testing.T, apelido string) uuid.UUID {
	t.Helper()

	return a.criarPerfil(t, apelido, []auth.Permissao{
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoCriar, Scope: auth.EscopoAll},
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoExcluir, Scope: auth.EscopoAll},
		{Resource: auth.RecursoPerfis, Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: auth.RecursoPerfis, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
		{Resource: auth.RecursoPerfis, Action: auth.AcaoExcluir, Scope: auth.EscopoAll},
		{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoAll},
	})
}

// papelNoBanco lê o papel gravado — é o que decide se a trava recusou antes ou
// depois de escrever.
func (a *ambiente) papelNoBanco(t *testing.T, usuario uuid.UUID) uuid.UUID {
	t.Helper()

	var papel uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `SELECT role_id FROM users WHERE id = $1`, usuario).Scan(&papel); err != nil {
		t.Fatalf("lendo o papel gravado: %v", err)
	}
	return papel
}

// Reprodução do achado, pela porta da frente: um perfil de suporte com
// `users:ver/criar/editar` e mais nada grava o perfil de administrador no
// PRÓPRIO cadastro. Como a matriz é lida do banco a cada requisição, sem a
// trava ele passaria a mandar na instalação já na chamada seguinte — sem novo
// login, com o mesmo access token.
func TestSuporteNaoSePromoveTrocandoOProprioPapel(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	suporte := a.criarPerfil(t, "suporte", []auth.Permissao{
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoCriar, Scope: auth.EscopoAll},
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
	})
	administrador := a.perfilAdministrador(t, "chefe")
	u := a.criarUsuario(t, "suporte", suporte)

	patch := a.chamar(t, http.MethodPatch, "/users/"+u.ID.String(), u.Token,
		map[string]any{"role_id": administrador.String()})
	if patch.Status != http.StatusUnprocessableEntity {
		t.Fatalf("PATCH do próprio role_id: status = %d, esperado 422 — corpo: %s", patch.Status, patch.Corpo)
	}
	if code := patch.codigoDeErro(t); code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, esperado VALIDATION_ERROR", code)
	}

	// O PUT é o mesmo caminho com outro verbo, e não pode ser a porta dos fundos.
	put := a.chamar(t, http.MethodPut, "/users/"+u.ID.String(), u.Token, map[string]any{
		"name":    "QA suporte",
		"email":   u.Email,
		"role_id": administrador.String(),
	})
	if put.Status != http.StatusUnprocessableEntity {
		t.Errorf("PUT do próprio role_id: status = %d, esperado 422 — corpo: %s", put.Status, put.Corpo)
	}

	if papel := a.papelNoBanco(t, u.ID); papel != suporte {
		t.Fatalf("o papel foi GRAVADO (%s): a recusa veio depois do UPDATE", papel)
	}

	// E a sessão continua sem poder nenhum sobre perfis.
	me := a.chamar(t, http.MethodGet, "/roles", u.Token, nil)
	if me.Status != http.StatusForbidden {
		t.Errorf("GET /roles com o mesmo token: status = %d, esperado 403", me.Status)
	}
}

// Promover um TERCEIRO exige `roles:editar`: quem só cuida do cadastro não
// distribui acesso — nem por interposta pessoa, que é a mesma escalada em dois
// passos (promove um colega, entra na conta dele).
func TestQuemSoEditaUsuarioNaoPromoveTerceiro(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	suporte := a.criarPerfil(t, "suporte2", []auth.Permissao{
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoCriar, Scope: auth.EscopoAll},
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
	})
	administrador := a.perfilAdministrador(t, "chefe2")
	ator := a.criarUsuario(t, "suporte2", suporte)
	colega := a.criarUsuario(t, "colega", suporte)

	resp := a.chamar(t, http.MethodPatch, "/users/"+colega.ID.String(), ator.Token,
		map[string]any{"role_id": administrador.String()})
	if resp.Status != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403 — corpo: %s", resp.Status, resp.Corpo)
	}
	if code := resp.codigoDeErro(t); code != "FORBIDDEN" {
		t.Errorf("code = %q, esperado FORBIDDEN", code)
	}
	if papel := a.papelNoBanco(t, colega.ID); papel != suporte {
		t.Fatalf("o papel do colega foi GRAVADO (%s)", papel)
	}

	// E o mesmo vale para o cadastro novo: criar já é atribuir papel.
	criado := a.chamar(t, http.MethodPost, "/users", ator.Token, map[string]any{
		"name":     "Conta plantada",
		"email":    "qa-plantada-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10] + "@wh.local",
		"password": senhaDeIntegracao,
		"role_id":  administrador.String(),
	})
	if criado.Status != http.StatusForbidden {
		t.Fatalf("POST /users com perfil de administrador: status = %d, esperado 403 — corpo: %s",
			criado.Status, criado.Corpo)
	}
}

// Teto de privilégio: nem quem administra acesso concede o que não tem. Sem
// isto, bastaria promover um terceiro ao perfil mais forte e usar a conta dele.
func TestPapelAcimaDoTetoDoAtorEhRecusado(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	// Administra acesso, mas não enxerga o chat.
	gestor := a.criarPerfil(t, "gestor", []auth.Permissao{
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
		{Resource: auth.RecursoPerfis, Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: auth.RecursoPerfis, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
	})
	maisForte := a.perfilAdministrador(t, "forte")

	ator := a.criarUsuario(t, "gestor", gestor)
	alvo := a.criarUsuario(t, "alvo-teto", gestor)

	acima := a.chamar(t, http.MethodPatch, "/users/"+alvo.ID.String(), ator.Token,
		map[string]any{"role_id": maisForte.String()})
	if acima.Status != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403 — corpo: %s", acima.Status, acima.Corpo)
	}
	if papel := a.papelNoBanco(t, alvo.ID); papel != gestor {
		t.Fatalf("o papel acima do teto foi GRAVADO (%s)", papel)
	}

	// Controle positivo: dentro do teto, a atribuição passa. Sem ele o teste
	// passaria mesmo se a trava recusasse tudo.
	dentro := a.criarPerfil(t, "menor", []auth.Permissao{
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoVer, Scope: auth.EscopoAll},
	})
	ok := a.chamar(t, http.MethodPatch, "/users/"+alvo.ID.String(), ator.Token,
		map[string]any{"role_id": dentro.String()})
	if ok.Status != http.StatusOK {
		t.Fatalf("atribuir papel subconjunto do ator: status = %d, esperado 200 — corpo: %s",
			ok.Status, ok.Corpo)
	}
	if papel := a.papelNoBanco(t, alvo.ID); papel != dentro {
		t.Fatalf("o papel dentro do teto não foi gravado: %s", papel)
	}
}

// Quem escolhe a senha de alguém entra na conta e herda o acesso dela: é
// atribuição de papel por outro caminho, e exige a mesma autoridade.
func TestTrocarSenhaDeTerceiroExigeAutoridadeDeAcesso(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	suporte := a.criarPerfil(t, "suporte3", []auth.Permissao{
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoEditar, Scope: auth.EscopoAll},
	})
	administrador := a.perfilAdministrador(t, "chefe3")

	ator := a.criarUsuario(t, "suporte3", suporte)
	chefe := a.criarUsuario(t, "chefe3", administrador)

	const senhaDoAtacante = "senha-escolhida-pelo-atacante"

	resp := a.chamar(t, http.MethodPatch, "/users/"+chefe.ID.String(), ator.Token,
		map[string]any{"password": senhaDoAtacante})
	if resp.Status != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403 — corpo: %s", resp.Status, resp.Corpo)
	}

	// A prova de que nada foi gravado: a senha do chefe continua sendo a dele.
	tentativa := a.chamar(t, http.MethodPost, "/auth/login", "",
		map[string]string{"email": chefe.Email, "password": senhaDoAtacante})
	if tentativa.Status == http.StatusOK {
		t.Fatal("a senha do administrador FOI trocada: o atacante entrou na conta dele")
	}
	if entrou := a.entrar(t, chefe.Email, senhaDeIntegracao); entrou == "" {
		t.Fatal("o administrador perdeu a própria senha")
	}

	// A própria senha continua livre — é a credencial de quem já está dentro.
	propria := a.chamar(t, http.MethodPatch, "/users/"+ator.ID.String(), ator.Token,
		map[string]any{"password": "minha-senha-nova-2026"})
	if propria.Status != http.StatusOK {
		t.Fatalf("trocar a própria senha: status = %d, esperado 200 — corpo: %s", propria.Status, propria.Corpo)
	}
	if depois := a.entrar(t, ator.Email, "minha-senha-nova-2026"); depois == "" {
		t.Fatal("a própria senha não foi trocada")
	}
}

// ─────────────────────── PUT /roles/{id} e catálogo ─────────────────────────

// docs/api.md §2: todo recurso expõe os seis verbos. O PUT do perfil é
// substituição integral do CADASTRO — a matriz tem endpoint próprio e sai
// intacta, senão um PUT de renomear tiraria o acesso de todos os usuários
// daquele perfil sem ninguém ter pedido.
func TestPutDePerfilSubstituiOCadastroSemMexerNaMatriz(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	gestor := a.criarUsuario(t, "putroles", a.perfilAdministrador(t, "putroles"))
	alvo := a.criarPerfil(t, "alvoput", []auth.Permissao{
		{Resource: "chat", Action: auth.AcaoVer, Scope: auth.EscopoOwn},
	})

	codigoNovo := "qa_put_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	resp := a.chamar(t, http.MethodPut, "/roles/"+alvo.String(), gestor.Token,
		map[string]any{"code": codigoNovo, "name": "Recepção"})
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 — corpo: %s", resp.Status, resp.Corpo)
	}

	var corpo struct {
		Data struct {
			Codigo     string           `json:"code"`
			Nome       string           `json:"name"`
			Permissoes []auth.Permissao `json:"permissions"`
		} `json:"data"`
	}
	resp.decodificar(t, &corpo)

	if corpo.Data.Codigo != codigoNovo || corpo.Data.Nome != "Recepção" {
		t.Errorf("o cadastro não foi substituído: %+v", corpo.Data)
	}
	if len(corpo.Data.Permissoes) != 1 || corpo.Data.Permissoes[0].Scope != auth.EscopoOwn {
		t.Fatalf("o PUT mexeu na matriz: %+v", corpo.Data.Permissoes)
	}

	// `code` e `name` são obrigatórios: o corpo do PUT é o estado final, não um
	// remendo.
	incompleto := a.chamar(t, http.MethodPut, "/roles/"+alvo.String(), gestor.Token,
		map[string]any{"name": "Só o nome"})
	if incompleto.Status != http.StatusUnprocessableEntity {
		t.Errorf("PUT sem code: status = %d, esperado 422 — corpo: %s", incompleto.Status, incompleto.Corpo)
	}

	// Perfil de sistema não se altera nem pelo PUT.
	sistema := a.criarPerfil(t, "sistemaput", nil)
	if _, err := a.pool.Exec(a.ctx, `UPDATE roles SET is_system = true WHERE id = $1`, sistema); err != nil {
		t.Fatalf("marcando como perfil de sistema: %v", err)
	}
	travado := a.chamar(t, http.MethodPut, "/roles/"+sistema.String(), gestor.Token,
		map[string]any{"code": "qa_sistema_renomeado", "name": "Renomeado"})
	if travado.Status != http.StatusConflict {
		t.Fatalf("PUT em perfil de sistema: status = %d, esperado 409 — corpo: %s", travado.Status, travado.Corpo)
	}
	if code := travado.codigoDeErro(t); code != "ROLE_IMMUTABLE" {
		t.Errorf("code = %q, esperado ROLE_IMMUTABLE", code)
	}
}

// A matriz não concede ação que o recurso não oferece: `chat` não tem
// "excluir" (spec §8 — mensagem enviada não se apaga), e o catálogo do banco é
// quem diz isso.
func TestMatrizRecusaAcaoForaDoCatalogoDoRecurso(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	gestor := a.criarUsuario(t, "acoes", a.perfilAdministrador(t, "acoes"))
	alvo := a.criarPerfil(t, "alvoacoes", nil)

	resp := a.chamar(t, http.MethodPut, "/roles/"+alvo.String()+"/permissions", gestor.Token,
		[]auth.Permissao{{Resource: "chat", Action: auth.AcaoExcluir, Scope: auth.EscopoAll}})
	if resp.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, esperado 422 — corpo: %s", resp.Status, resp.Corpo)
	}
	if code := resp.codigoDeErro(t); code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, esperado VALIDATION_ERROR", code)
	}

	var n int
	if err := a.pool.QueryRow(a.ctx,
		`SELECT count(*) FROM role_permissions WHERE role_id = $1`, alvo).Scan(&n); err != nil {
		t.Fatalf("contando permissões: %v", err)
	}
	if n != 0 {
		t.Fatalf("a ação fora do catálogo foi GRAVADA (%d linhas)", n)
	}
}

// ─────────────────────────── Sondas ─────────────────────────────────────────

// /readyz é o que decide se a instância entra no balanceador. Com o banco no ar
// e as migrations aplicadas, tem de responder 200 e dizer em que versão está.
func TestReadyzRespondeComAVersaoDoSchema(t *testing.T) {
	a := subirAPI(t)

	resp := a.chamarNaRaiz(t, http.MethodGet, "/readyz")
	if resp.Status != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 — corpo: %s", resp.Status, resp.Corpo)
	}

	var corpo struct {
		Data struct {
			Status  string `json:"status"`
			Versao  uint64 `json:"schema_version"`
			Banco   string `json:"database"`
			Detalhe string `json:"detail"`
		} `json:"data"`
	}
	resp.decodificar(t, &corpo)

	if corpo.Data.Status != "ok" {
		t.Fatalf("status = %q — %s", corpo.Data.Status, resp.Corpo)
	}
	if corpo.Data.Versao == 0 {
		t.Error("schema_version = 0: o /readyz não conseguiu ler schema_migrations")
	}
	if corpo.Data.Versao < SchemaVersionEsperada {
		t.Errorf("schema_version = %d, abaixo da esperada %d", corpo.Data.Versao, SchemaVersionEsperada)
	}
}

// Garantia de que o teste não mente por acidente: se a fixture parar de criar
// permissão nenhuma, os testes de 403 passariam por engano.
func TestFixtureDePerfilRealmenteConcedeOQuePede(t *testing.T) {
	a := subirAPI(t)
	a.garantirRecursos(t)

	perfil := a.criarPerfil(t, "fixture", []auth.Permissao{
		{Resource: auth.RecursoUsuarios, Action: auth.AcaoVer, Scope: auth.EscopoAll},
	})

	var n int
	if err := a.pool.QueryRow(a.ctx,
		`SELECT count(*) FROM role_permissions WHERE role_id = $1`, perfil).Scan(&n); err != nil {
		t.Fatalf("contando permissões: %v", err)
	}
	if n != 1 {
		t.Fatalf("permissões gravadas = %d, esperado 1", n)
	}
}
