package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// TestRequestLoggerDeixaOControllerEmpurrarOPrazoDeEscrita trava a correção que
// mantém o SSE vivo.
//
// `httpx.RequestLogger` embrulha o http.ResponseWriter para contar bytes e
// status. O http.ResponseController só desce a cadeia de embrulhos por
// `Unwrap() http.ResponseWriter` — repassar `Flush` NÃO basta. Sem o Unwrap,
// `SetWriteDeadline` devolve "feature not supported", o handler do /stream não
// consegue empurrar o prazo, e toda conexão de longa duração morre no
// `WriteTimeout` do http.Server (60s em cmd/api/main.go).
//
// O defeito é silencioso por construção: a API continua saudável, o handler não
// registra erro, e o sintoma é o mapa de ocupação parando de receber evento de
// minuto em minuto. Por isso a garantia é teste, e não comentário.
func TestRequestLoggerDeixaOControllerEmpurrarOPrazoDeEscrita(t *testing.T) {
	casos := []struct {
		nome  string
		monta func(http.Handler) http.Handler
	}{
		{"sem middleware", nil},
		{"com httpx.RequestLogger", httpx.RequestLogger},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			var erro error
			var flusher bool
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				erro = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Minute))
				_, flusher = w.(http.Flusher)
			})

			var final http.Handler = h
			if caso.monta != nil {
				final = caso.monta(h)
			}

			srv := httptest.NewServer(final)
			defer srv.Close()

			resp, err := http.Get(srv.URL) //nolint:noctx // requisição de teste local
			if err != nil {
				t.Fatalf("requisição: %v", err)
			}
			_ = resp.Body.Close()

			if erro != nil {
				t.Fatalf("SetWriteDeadline devolveu %v — o ResponseController não alcançou o writer de baixo; falta Unwrap() no embrulho", erro)
			}
			// O Flusher é a outra metade: sem ele o evento fica preso no buffer.
			if !flusher {
				t.Fatal("o writer não é http.Flusher — o SSE não conseguiria despachar evento nenhum")
			}
		})
	}
}
