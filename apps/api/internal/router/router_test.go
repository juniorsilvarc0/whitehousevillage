package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/config"
)

// montarParaTeste monta a árvore de rotas sem banco. Só as rotas que não tocam
// no Postgres podem ser exercidas aqui — o resto é teste de integração do qa.
func montarParaTeste(t *testing.T) http.Handler {
	t.Helper()

	cfg := config.Config{
		Env:         "test",
		JWTSecret:   "segredo-de-teste-com-mais-de-32-bytes!!",
		AccessTTL:   15 * time.Minute,
		RefreshTTL:  720 * time.Hour,
		CORSOrigins: []string{"http://localhost:3000"},
	}

	deps := Deps{Saude: NewSaude(nil)}
	tabela := Rotas(deps)
	if err := ValidarTabela(tabela); err != nil {
		t.Fatalf("ValidarTabela: %v", err)
	}

	return montar(cfg, tabela, auth.NewAutenticador(auth.NovoEmissor(cfg.JWTSecret, cfg.AccessTTL), auth.NewRepository(nil)))
}

func chamar(t *testing.T, h http.Handler, metodo, alvo string, ajustar func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(metodo, alvo, nil)
	if ajustar != nil {
		ajustar(req)
	}
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	return resp
}

func TestHealthzRespondeSemToken(t *testing.T) {
	resp := chamar(t, montarParaTeste(t), http.MethodGet, "/api/v1/healthz", nil)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, corpo %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"status":"ok"`) {
		t.Fatalf("corpo inesperado: %s", resp.Body.String())
	}
}

// A barreira real é o middleware: sem token, nenhuma rota protegida chega ao
// handler.
func TestRotasProtegidasExigemToken(t *testing.T) {
	h := montarParaTeste(t)

	for _, alvo := range []string{
		"/api/v1/users",
		"/api/v1/users/9f1c1e2a-0d3b-4f6a-9a1e-2b7c5d8e0f11",
		"/api/v1/roles",
		"/api/v1/roles/resources",
		"/api/v1/auth/me",
	} {
		t.Run(alvo, func(t *testing.T) {
			resp := chamar(t, h, http.MethodGet, alvo, nil)
			if resp.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, esperado 401 (corpo: %s)", resp.Code, resp.Body.String())
			}
			if code := codigoDoErro(t, resp.Body.Bytes()); code != "UNAUTHORIZED" {
				t.Fatalf("code = %q, esperado UNAUTHORIZED", code)
			}
		})
	}
}

func TestTokenComAlgNoneNaoPassaPeloMiddleware(t *testing.T) {
	resp := chamar(t, montarParaTeste(t), http.MethodGet, "/api/v1/users", func(r *http.Request) {
		// Cabeçalho e payload de um token com alg none, sem assinatura.
		r.Header.Set("Authorization", "Bearer eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0."+
			"eyJzdWIiOiI5ZjFjMWUyYS0wZDNiLTRmNmEtOWExZS0yYjdjNWQ4ZTBmMTEifQ.")
	})

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401 — token sem assinatura foi aceito", resp.Code)
	}
}

func TestRotaInexistenteDevolveEnvelopeDeErro(t *testing.T) {
	resp := chamar(t, montarParaTeste(t), http.MethodGet, "/api/v1/nao-existe", nil)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404", resp.Code)
	}
	if code := codigoDoErro(t, resp.Body.Bytes()); code != "NOT_FOUND" {
		t.Fatalf("code = %q, esperado NOT_FOUND", code)
	}
}

// X-Request-Id que chega é o que volta: é a ponta que liga o erro no painel à
// linha do log da API.
func TestRequestIDDeEntradaEhDevolvido(t *testing.T) {
	resp := chamar(t, montarParaTeste(t), http.MethodGet, "/api/v1/healthz", func(r *http.Request) {
		r.Header.Set("X-Request-Id", "id-vindo-do-proxy")
	})

	if got := resp.Header().Get("X-Request-Id"); got != "id-vindo-do-proxy" {
		t.Fatalf("X-Request-Id = %q, esperado id-vindo-do-proxy", got)
	}

	semHeader := chamar(t, montarParaTeste(t), http.MethodGet, "/api/v1/healthz", nil)
	if semHeader.Header().Get("X-Request-Id") == "" {
		t.Fatal("sem header de entrada, a API deveria gerar um id")
	}
}

func TestCORSRespondePreflightDaOrigemPermitida(t *testing.T) {
	resp := chamar(t, montarParaTeste(t), http.MethodOptions, "/api/v1/users", func(r *http.Request) {
		r.Header.Set("Origin", "http://localhost:3000")
		r.Header.Set("Access-Control-Request-Method", http.MethodPost)
	})

	if got := resp.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("Allow-Origin = %q", got)
	}
	// O refresh viaja em cookie httpOnly: sem credenciais o painel não mantém sessão.
	if got := resp.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Allow-Credentials = %q, esperado true", got)
	}
}

func TestCORSNaoLiberaOrigemDesconhecida(t *testing.T) {
	resp := chamar(t, montarParaTeste(t), http.MethodOptions, "/api/v1/users", func(r *http.Request) {
		r.Header.Set("Origin", "http://site-do-atacante.com")
		r.Header.Set("Access-Control-Request-Method", http.MethodPost)
	})

	if got := resp.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("origem desconhecida recebeu Allow-Origin = %q", got)
	}
}

func codigoDoErro(t *testing.T, corpo []byte) string {
	t.Helper()

	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(corpo, &envelope); err != nil {
		t.Fatalf("corpo fora do envelope de erro: %s", corpo)
	}
	return envelope.Error.Code
}
