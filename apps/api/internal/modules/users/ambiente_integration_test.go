//go:build integration

// Infraestrutura dos testes de integração de usuários e perfis.
//
// Sobe a árvore de rotas INTEIRA sobre um Postgres real e conversa com ela por
// HTTP. É a única forma honesta de testar uma trava de autorização: os defeitos
// que reprovaram as rodadas anteriores tinham teste de service verde por cima
// deles, porque perguntavam ao próprio código sob suspeita se ele tinha feito a
// coisa certa.
//
// O pacote é `users_test` (teste externo) de propósito: assim ele pode importar
// `internal/router` sem ciclo, e passa a enxergar exatamente o que o painel vê.
package users_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/config"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/router"
)

const senhaDeTeste = "senha-de-integracao-2026"

// senhaDoSeed é a senha das contas de desenvolvimento (cmd/seed). Usada só pelo
// teste que precisa entrar como o administrador REAL do seed, com o catálogo
// inteiro — nenhum fixture consegue reproduzi-lo à mão sem divergir do seed.
const senhaDoSeed = "whv@2026"

type ambiente struct {
	pool     *pgxpool.Pool
	servidor *httptest.Server
	ctx      context.Context
}

func subir(t *testing.T) *ambiente {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL ausente: teste de integração pulado")
	}

	ctx := context.Background()
	pool, err := db.New(ctx, url)
	if err != nil {
		t.Fatalf("abrindo pool: %v", err)
	}
	t.Cleanup(pool.Close)

	h, err := router.New(router.Opcoes{
		Config: config.Config{
			Env:         "test",
			JWTSecret:   "segredo-de-integracao-com-mais-de-32-bytes",
			AccessTTL:   15 * time.Minute,
			RefreshTTL:  720 * time.Hour,
			CORSOrigins: []string{"http://localhost:3000"},
		},
		Pool: pool,
	})
	if err != nil {
		t.Fatalf("montando o router: %v", err)
	}

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	return &ambiente{pool: pool, servidor: srv, ctx: ctx}
}

// ─────────────────────────── HTTP ───────────────────────────────────────────

type resposta struct {
	Status  int
	Corpo   []byte
	Cookies []*http.Cookie
}

// codigoDoErro devolve o `error.code` estável do envelope; vazio quando a
// resposta não é erro.
func (r resposta) codigoDoErro() string {
	var env struct {
		Erro struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(r.Corpo, &env)
	return env.Erro.Code
}

func (a *ambiente) chamar(t *testing.T, metodo, caminho, token string, corpo any, cookies ...*http.Cookie) resposta {
	t.Helper()

	var body io.Reader
	if corpo != nil {
		bruto, err := json.Marshal(corpo)
		if err != nil {
			t.Fatalf("serializando o corpo: %v", err)
		}
		body = bytes.NewReader(bruto)
	}

	req, err := http.NewRequestWithContext(a.ctx, metodo, a.servidor.URL+router.PrefixoDaAPI+caminho, body)
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
	return resposta{Status: resp.StatusCode, Corpo: lido, Cookies: resp.Cookies()}
}

// sessao é o que o login devolve e o teste precisa reapresentar.
type sessao struct {
	Token   string
	Cookies []*http.Cookie
}

func (a *ambiente) login(t *testing.T, email string) sessao {
	t.Helper()

	return a.loginComSenha(t, email, senhaDeTeste)
}

func (a *ambiente) loginComSenha(t *testing.T, email, senha string) sessao {
	t.Helper()

	r := a.chamar(t, http.MethodPost, "/auth/login", "", map[string]string{
		"email": email, "password": senha,
	})
	if r.Status != http.StatusOK {
		t.Fatalf("login de %s: status %d, corpo %s", email, r.Status, r.Corpo)
	}

	// Toda resposta da API vem envelopada em `data` (convenção do contrato).
	var corpo struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(r.Corpo, &corpo); err != nil {
		t.Fatalf("lendo o login: %v", err)
	}
	return sessao{Token: corpo.Data.AccessToken, Cookies: r.Cookies}
}

// ─────────────────────────── Fixtures ───────────────────────────────────────

// criarPerfil cria um perfil não-sistema com a matriz informada.
// `celulas` é uma lista de "recurso:ação:escopo".
func (a *ambiente) criarPerfil(t *testing.T, prefixo string, celulas ...string) uuid.UUID {
	t.Helper()

	codigo := fmt.Sprintf("%s_%s", prefixo, uuid.NewString()[:8])
	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`INSERT INTO roles (code, name, is_system) VALUES ($1, $1, false) RETURNING id`, codigo).
		Scan(&id); err != nil {
		t.Fatalf("criando perfil %s: %v", codigo, err)
	}
	a.concederCelulas(t, id, celulas...)
	return id
}

func (a *ambiente) concederCelulas(t *testing.T, perfil uuid.UUID, celulas ...string) {
	t.Helper()

	for _, c := range celulas {
		recurso, acao, escopo := partesDaCelula(t, c)
		if _, err := a.pool.Exec(a.ctx,
			`INSERT INTO role_permissions (role_id, resource_code, action, scope) VALUES ($1,$2,$3,$4)
			 ON CONFLICT (role_id, resource_code, action) DO UPDATE SET scope = EXCLUDED.scope`,
			perfil, recurso, acao, escopo); err != nil {
			t.Fatalf("concedendo %s: %v", c, err)
		}
	}
}

// criarPerfilComCatalogoInteiro é o perfil de administrador em tudo menos no
// `is_system`: serve para montar dois administradores de verdade sem tocar no
// perfil do seed.
func (a *ambiente) criarPerfilComCatalogoInteiro(t *testing.T, prefixo string) uuid.UUID {
	t.Helper()

	id := a.criarPerfil(t, prefixo)
	if _, err := a.pool.Exec(a.ctx,
		`INSERT INTO role_permissions (role_id, resource_code, action, scope)
		 SELECT $1, code, unnest(actions), 'all' FROM resources`, id); err != nil {
		t.Fatalf("copiando o catálogo para o perfil: %v", err)
	}
	return id
}

func partesDaCelula(t *testing.T, celula string) (recurso, acao, escopo string) {
	t.Helper()

	var partes []string
	inicio := 0
	for i := 0; i < len(celula); i++ {
		if celula[i] == ':' {
			partes = append(partes, celula[inicio:i])
			inicio = i + 1
		}
	}
	partes = append(partes, celula[inicio:])
	if len(partes) != 3 {
		t.Fatalf("célula %q: use recurso:ação:escopo", celula)
	}
	return partes[0], partes[1], partes[2]
}

func (a *ambiente) criarUsuario(t *testing.T, nome string, perfil uuid.UUID) (uuid.UUID, string) {
	t.Helper()

	hash, err := auth.Hash(senhaDeTeste)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	email := fmt.Sprintf("%s.%s@wh.local", nome, uuid.NewString()[:8])

	var id uuid.UUID
	if err := a.pool.QueryRow(a.ctx,
		`INSERT INTO users (property_id, role_id, name, email, password_hash, active)
		 SELECT p.id, $1, $2, $3, $4, true FROM properties p WHERE p.active ORDER BY p.created_at LIMIT 1
		 RETURNING id`, perfil, nome, email, hash).Scan(&id); err != nil {
		t.Fatalf("criando usuário %s: %v", email, err)
	}
	return id, email
}

// ─────────────────────────── Leitura do banco ───────────────────────────────

func (a *ambiente) emailDe(t *testing.T, id uuid.UUID) string {
	t.Helper()

	var email string
	if err := a.pool.QueryRow(a.ctx, `SELECT email FROM users WHERE id = $1`, id).Scan(&email); err != nil {
		t.Fatalf("lendo o e-mail: %v", err)
	}
	return email
}

func (a *ambiente) papelDe(t *testing.T, id uuid.UUID) uuid.UUID {
	t.Helper()

	var papel uuid.UUID
	if err := a.pool.QueryRow(a.ctx, `SELECT role_id FROM users WHERE id = $1`, id).Scan(&papel); err != nil {
		t.Fatalf("lendo o papel: %v", err)
	}
	return papel
}

func (a *ambiente) celulasDoPerfil(t *testing.T, perfil uuid.UUID) int {
	t.Helper()

	var n int
	if err := a.pool.QueryRow(a.ctx,
		`SELECT count(*) FROM role_permissions WHERE role_id = $1`, perfil).Scan(&n); err != nil {
		t.Fatalf("contando a matriz: %v", err)
	}
	return n
}

// administradoresAtivos conta quem, no banco, ainda pode administrar o acesso —
// a mesma definição por DADO que o service usa.
func (a *ambiente) administradoresAtivos(t *testing.T) int {
	t.Helper()

	var n int
	if err := a.pool.QueryRow(a.ctx, `
		SELECT count(DISTINCT u.id)
		  FROM users u
		  JOIN role_permissions rp ON rp.role_id = u.role_id
		 WHERE u.active AND u.deleted_at IS NULL
		   AND rp.resource_code = $1 AND rp.action = $2`,
		auth.RecursoPerfis, auth.AcaoEditar).Scan(&n); err != nil {
		t.Fatalf("contando administradores: %v", err)
	}
	return n
}

func (a *ambiente) sessoesVivas(t *testing.T, usuario uuid.UUID) int {
	t.Helper()

	var n int
	if err := a.pool.QueryRow(a.ctx,
		`SELECT count(*) FROM refresh_tokens WHERE user_id = $1 AND revoked_at IS NULL`, usuario).
		Scan(&n); err != nil {
		t.Fatalf("contando sessões: %v", err)
	}
	return n
}

func (a *ambiente) auditoriasDe(t *testing.T, entidadeID uuid.UUID, acao string) int {
	t.Helper()

	var n int
	if err := a.pool.QueryRow(a.ctx,
		`SELECT count(*) FROM audit_log WHERE entity_id = $1 AND action = $2`, entidadeID, acao).
		Scan(&n); err != nil {
		t.Fatalf("contando auditoria: %v", err)
	}
	return n
}
