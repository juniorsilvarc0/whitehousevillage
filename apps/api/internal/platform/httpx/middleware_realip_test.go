package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ───────── O IP do cliente não pode ser um campo que o cliente digita ─────────
//
// `chi/middleware.RealIP` está deprecado por spoofing e era o que estava
// montado: ele toma a entrada MAIS À ESQUERDA de `X-Forwarded-For`, que é
// exatamente a única que o cliente escreve. Este IP alimenta a chave do
// limitador do `/auth/login` e a coluna `audit_log.ip` da trilha da spec §16.

func requisicaoDe(peer string, cabecalhos map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.RemoteAddr = peer
	for k, v := range cabecalhos {
		r.Header.Set(k, v)
	}
	return r
}

// resolvido roda a cadeia real (RealIP → handler) e devolve o que
// `IPDoCliente` enxerga lá dentro, que é o valor que o limitador e a trilha usam.
func resolvido(r *http.Request) string {
	var visto string
	RealIP(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		visto = IPDoCliente(req)
	})).ServeHTTP(httptest.NewRecorder(), r)
	return visto
}

func TestRealIPIgnoraCabecalhoDeClienteQueFalaDiretoComAAPI(t *testing.T) {
	casos := []struct {
		nome       string
		cabecalhos map[string]string
	}{
		{"X-Forwarded-For forjado", map[string]string{"X-Forwarded-For": "10.0.0.1"}},
		{"cadeia inteira forjada", map[string]string{"X-Forwarded-For": "1.1.1.1, 2.2.2.2"}},
		{"X-Real-Ip forjado", map[string]string{"X-Real-Ip": "9.9.9.9"}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			// Peer PÚBLICO: a porta da API exposta direto, sem proxy na frente.
			got := resolvido(requisicaoDe("198.51.100.7:44321", c.cabecalhos))
			if got != "198.51.100.7" {
				t.Fatalf("IP resolvido = %q, esperado 198.51.100.7 — o cliente escolheu o próprio "+
					"endereço, e com ele zera o limitador de login a cada requisição e assina a "+
					"trilha de auditoria com o IP que quiser", got)
			}
		})
	}
}

// Atrás do proxy, o cabeçalho vale — senão TODO cliente vira o IP do Traefik e o
// limitador tranca a internet inteira junto com o atacante.
func TestRealIPAceitaOProxyEPegaOSaltoQueOProxyESCREVEU(t *testing.T) {
	casos := []struct {
		nome, xff, quero string
	}{
		{"um salto", "203.0.113.9", "203.0.113.9"},
		// O ATAQUE: o cliente manda `X-Forwarded-For: 1.2.3.4` e o proxy APENDA o
		// que ele mesmo viu. A entrada da direita é a única escrita por um salto
		// confiável — e é a que o chi ignorava.
		{"cliente forja à esquerda", "1.2.3.4, 203.0.113.9", "203.0.113.9"},
		{"proxy interno no fim", "203.0.113.9, 10.1.2.3", "203.0.113.9"},
		{"com porta", "203.0.113.9:5555", "203.0.113.9"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := resolvido(requisicaoDe("172.18.0.4:33333", map[string]string{"X-Forwarded-For": c.xff}))
			if got != c.quero {
				t.Fatalf("IP resolvido = %q, esperado %q (XFF %q)", got, c.quero, c.xff)
			}
		})
	}
}

func TestRealIPCaiNoPeerQuandoNaoHaCabecalho(t *testing.T) {
	if got := resolvido(requisicaoDe("172.18.0.4:33333", nil)); got != "172.18.0.4" {
		t.Fatalf("IP resolvido = %q, esperado o peer 172.18.0.4", got)
	}
	// Cadeia inteira interna (rede privada falando com rede privada): vale a
	// origem, que é a da esquerda.
	got := resolvido(requisicaoDe("172.18.0.4:1", map[string]string{"X-Forwarded-For": "10.9.9.9, 172.18.0.1"}))
	if got != "10.9.9.9" {
		t.Fatalf("cadeia interna: IP resolvido = %q, esperado 10.9.9.9", got)
	}
}

// Sem o middleware na cadeia, IPDoCliente continua devolvendo o peer — é o que
// mantém teste de unidade de outro middleware funcionando sem montar RealIP.
func TestIPDoClienteCaiNoPeerSemOMiddleware(t *testing.T) {
	r := requisicaoDe("198.51.100.7:44321", map[string]string{"X-Forwarded-For": "1.1.1.1"})
	if got := IPDoCliente(r); got != "198.51.100.7" {
		t.Fatalf("IPDoCliente sem RealIP = %q, esperado o peer", got)
	}
}
