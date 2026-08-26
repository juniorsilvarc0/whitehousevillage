package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func requisicao(query string) *http.Request {
	return httptest.NewRequest(http.MethodGet, "/users?"+query, nil)
}

func TestParsePageAplicaPadraoETeto(t *testing.T) {
	casos := []struct {
		query             string
		pagina, porPagina int
	}{
		{"", 1, 25},
		{"page=3&per_page=50", 3, 50},
		{"per_page=100000", 1, 100}, // teto: sem ele, um per_page absurdo arrasta a tabela inteira
		{"page=0", 1, 25},           // página 0 não existe
		{"page=-2&per_page=-5", 1, 25},
		{"page=abc&per_page=xyz", 1, 25}, // lixo cai no padrão, não vira erro
	}

	for _, c := range casos {
		t.Run(c.query, func(t *testing.T) {
			pagina, porPagina := ParsePage(requisicao(c.query))
			if pagina != c.pagina || porPagina != c.porPagina {
				t.Fatalf("(%d, %d), esperado (%d, %d)", pagina, porPagina, c.pagina, c.porPagina)
			}
		})
	}
}

func TestOffset(t *testing.T) {
	if got := Offset(1, 25); got != 0 {
		t.Fatalf("primeira página deveria ter offset 0, veio %d", got)
	}
	if got := Offset(3, 25); got != 50 {
		t.Fatalf("offset da página 3 = %d, esperado 50", got)
	}
}

// O valor do cliente é só a CHAVE do mapa; o que entra no ORDER BY é sempre o
// literal da whitelist. Sem isso, `sort` seria injeção de SQL por definição.
func TestParseSortSoAceitaWhitelist(t *testing.T) {
	permitidos := map[string]string{"name": "u.name", "created_at": "u.created_at"}
	const padrao = "u.name ASC, u.id ASC"

	casos := map[string]string{
		"":                              padrao,
		"sort=name":                     "u.name ASC",
		"sort=-created_at":              "u.created_at DESC",
		"sort=password_hash":            padrao,
		"sort=name%3B+DROP+TABLE+users": padrao,
		"sort=-":                        padrao,
	}

	for query, esperado := range casos {
		t.Run(query, func(t *testing.T) {
			if got := ParseSort(requisicao(query), permitidos, padrao); got != esperado {
				t.Fatalf("ParseSort = %q, esperado %q", got, esperado)
			}
		})
	}
}

// `?active` ausente traz ativos e inativos: o filtro nulo é o "não filtra".
func TestParseBool(t *testing.T) {
	if v := ParseBool(requisicao(""), "active"); v != nil {
		t.Fatalf("ausente deveria ser nil, veio %v", *v)
	}
	if v := ParseBool(requisicao("active=true"), "active"); v == nil || !*v {
		t.Fatal("active=true deveria virar true")
	}
	if v := ParseBool(requisicao("active=false"), "active"); v == nil || *v {
		t.Fatal("active=false deveria virar false")
	}
	if v := ParseBool(requisicao("active=talvez"), "active"); v != nil {
		t.Fatalf("valor ilegível deveria virar nil, veio %v", *v)
	}
}
