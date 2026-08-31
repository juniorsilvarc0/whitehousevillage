package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// A sonda existe para o healthcheck do Compose, que numa imagem distroless só
// pode invocar o próprio binário. Ela ficou "pendente no backend-go" por uma
// versão inteira, e o custo foi silencioso: o container ficou `unhealthy` desde
// o boot e cada sonda subia uma segunda API que morria em
// `address already in use`. Estes testes cobram os dois desfechos que o Docker
// sabe ler — sair 0 e sair 1 — porque é só isso que ele lê.

// servidorLocal sobe um servidor em 127.0.0.1 e devolve a porta, que é como a
// sonda encontra o processo.
func servidorLocal(t *testing.T, h http.Handler) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("url do servidor de teste: %v", err)
	}
	return u.Port()
}

func TestSondarAceitaProcessoVivo(t *testing.T) {
	caminho := "/healthz"

	var pedido string
	porta := servidorLocal(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pedido = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Setenv("API_PORT", porta)

	if err := sondar(); err != nil {
		t.Fatalf("processo vivo devia sair 0, veio erro: %v", err)
	}
	// O caminho importa: as sondas moram na RAIZ, fora de `/api/v1` (ver
	// Rota.NaRaiz), e uma sonda apontada para o prefixo responderia 404 num
	// processo perfeitamente são.
	if pedido != caminho {
		t.Fatalf("sonda bateu em %q, esperado %q", pedido, caminho)
	}
}

func TestSondarRecusaRespostaQueNaoSejaOK(t *testing.T) {
	porta := servidorLocal(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Setenv("API_PORT", porta)

	if err := sondar(); err == nil {
		t.Fatal("503 devia sair 1; a sonda aceitou")
	}
}

func TestSondarRecusaPortaMorta(t *testing.T) {
	// Ninguém escutando: é o container que ainda não subiu, e o caso que o
	// `start_period` do Compose existe para tolerar sem marcar doente.
	t.Setenv("API_PORT", "1")

	if err := sondar(); err == nil {
		t.Fatal("porta sem ninguém escutando devia sair 1; a sonda aceitou")
	}
}
