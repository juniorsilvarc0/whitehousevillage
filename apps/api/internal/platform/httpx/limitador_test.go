package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// docs/spec.md §1: 5 tentativas erradas em 15 min bloqueiam o e-mail.
func TestLimitadorBloqueiaDepoisDoQuintoErro(t *testing.T) {
	l := NovoLimitador(5, 15*time.Minute)
	const chave = "login:ana@wh.com"

	for i := 1; i <= 5; i++ {
		if l.Bloqueado(chave) {
			t.Fatalf("bloqueou na tentativa %d; deveria permitir as cinco primeiras", i)
		}
		l.Registrar(chave)
	}

	if !l.Bloqueado(chave) {
		t.Fatal("depois de cinco erros o e-mail deveria estar bloqueado")
	}
}

// Quem lembrou a senha não carrega o histórico de erro.
func TestLimitadorLimpaNoAcerto(t *testing.T) {
	l := NovoLimitador(5, 15*time.Minute)
	const chave = "login:ana@wh.com"

	for i := 0; i < 5; i++ {
		l.Registrar(chave)
	}
	l.Limpar(chave)

	if l.Bloqueado(chave) {
		t.Fatal("Limpar deveria zerar o contador")
	}
}

func TestLimitadorJanelaExpira(t *testing.T) {
	l := NovoLimitador(2, 20*time.Millisecond)
	const chave = "ip:203.0.113.7"

	if !l.Permitir(chave) || !l.Permitir(chave) {
		t.Fatal("as duas primeiras deveriam passar")
	}
	if l.Permitir(chave) {
		t.Fatal("a terceira deveria ser recusada dentro da janela")
	}

	time.Sleep(30 * time.Millisecond)

	if !l.Permitir(chave) {
		t.Fatal("passada a janela, a contagem deveria reiniciar")
	}
}

func TestLimitadorSeparaAsChaves(t *testing.T) {
	l := NovoLimitador(1, time.Minute)

	if !l.Permitir("ip:198.51.100.1") {
		t.Fatal("primeira do IP A deveria passar")
	}
	if !l.Permitir("ip:198.51.100.2") {
		t.Fatal("o contador de um IP não pode afetar o de outro")
	}
	if l.Permitir("ip:198.51.100.1") {
		t.Fatal("segunda do IP A deveria ser recusada")
	}
}

func TestMiddlewareDoLimitadorResponde429(t *testing.T) {
	l := NovoLimitador(1, time.Minute)
	protegido := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	chamar := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		req.RemoteAddr = "203.0.113.9:54321"
		resp := httptest.NewRecorder()
		protegido.ServeHTTP(resp, req)
		return resp.Code
	}

	if got := chamar(); got != http.StatusOK {
		t.Fatalf("primeira chamada = %d, esperado 200", got)
	}
	if got := chamar(); got != http.StatusTooManyRequests {
		t.Fatalf("segunda chamada = %d, esperado 429", got)
	}
}

func TestIPDoCliente(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.9:54321"

	if got := IPDoCliente(req); got != "203.0.113.9" {
		t.Fatalf("IPDoCliente = %q, esperado 203.0.113.9", got)
	}

	req.RemoteAddr = "sem-porta"
	if got := IPDoCliente(req); got != "sem-porta" {
		t.Fatalf("endereço sem porta deveria voltar inteiro, veio %q", got)
	}
}
