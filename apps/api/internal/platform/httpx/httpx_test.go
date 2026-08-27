package httpx

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Em linguagem de negócio: uma busca que não encontra nada devolve uma lista
// vazia, não "nada". O painel percorre `data` sem perguntar se ela veio — e
// `null` quebra a tela justamente no caso mais comum, o filtro sem resultado.
func TestListNuncaSerializaDataComoNull(t *testing.T) {
	type item struct {
		ID string `json:"id"`
	}

	casos := []struct {
		nome     string
		dados    any
		esperado string
	}{
		{"slice_tipado_nulo", []item(nil), `[]`},
		{"slice_de_any_nulo", []any(nil), `[]`},
		{"interface_nula", nil, `[]`},
		{"mapa_nulo", map[string]int(nil), `{}`},
		{"slice_vazio", []item{}, `[]`},
		{"slice_com_conteudo", []item{{ID: "a"}}, `[{"id":"a"}]`},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			resp := httptest.NewRecorder()
			List(resp, c.dados, Meta{Page: 1, PerPage: 25, Total: 0})

			corpo := resp.Body.String()
			if strings.Contains(corpo, `"data":null`) {
				t.Fatalf("data saiu como null: %s", corpo)
			}

			var envelope struct {
				Data json.RawMessage `json:"data"`
				Meta Meta            `json:"meta"`
			}
			if err := json.Unmarshal([]byte(corpo), &envelope); err != nil {
				t.Fatalf("corpo ilegível (%v): %s", err, corpo)
			}
			if got := string(envelope.Data); got != c.esperado {
				t.Fatalf("data = %s, esperado %s", got, c.esperado)
			}
		})
	}
}

// meta.total_pages é calculado aqui e o painel confia nele para desenhar a
// paginação; arredondar para baixo esconderia a última página.
func TestListCalculaTotalDePaginasArredondandoParaCima(t *testing.T) {
	casos := []struct {
		total     int64
		porPagina int
		esperado  int
	}{
		{0, 25, 0},
		{1, 25, 1},
		{25, 25, 1},
		{26, 25, 2},
		{100, 30, 4},
	}

	for _, c := range casos {
		resp := httptest.NewRecorder()
		List(resp, []int{}, Meta{Page: 1, PerPage: c.porPagina, Total: c.total})

		var envelope struct {
			Meta Meta `json:"meta"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("corpo ilegível: %v", err)
		}
		if envelope.Meta.TotalPages != c.esperado {
			t.Errorf("total=%d per_page=%d: total_pages = %d, esperado %d",
				c.total, c.porPagina, envelope.Meta.TotalPages, c.esperado)
		}
	}
}

// A marca de "este IP já entrou nesta conta" é o que impede o bloqueio por
// e-mail de virar negação de serviço. Ela precisa valer só para a chave marcada
// e só dentro da janela.
func TestLimitadorMarcaEEsquece(t *testing.T) {
	l := NovoLimitador(1, 40*time.Millisecond)

	if l.Marcado("ok:ana@wh.com|203.0.113.7") {
		t.Fatal("chave nunca marcada não pode aparecer como marcada")
	}

	l.Marcar("ok:ana@wh.com|203.0.113.7")
	if !l.Marcado("ok:ana@wh.com|203.0.113.7") {
		t.Fatal("a marca deveria valer logo depois de gravada")
	}
	if l.Marcado("ok:ana@wh.com|198.51.100.2") {
		t.Fatal("a marca de um IP não pode valer para outro")
	}

	time.Sleep(60 * time.Millisecond)
	if l.Marcado("ok:ana@wh.com|203.0.113.7") {
		t.Fatal("passada a janela, a marca deveria ter caducado")
	}
}

// ─────────── O log tem de dizer QUEM, e não "" para todo mundo ───────────
//
// Antes desta correção, TODA linha de log da API real saía com `user_id=""`,
// mesmo com token válido — porque `context.WithValue` só desce e o
// `auth.Middleware` roda ABAIXO do RequestLogger, servindo o próximo com um
// contexto NOVO que o logger (segurando o request antigo) nunca vê. Medido na
// API no ar, com token de admin:
//
//	PATCH /api/v1/units/{id} status=200 user_id="" ip=::1
//
// A asserção é feita sobre a LINHA DE LOG de verdade, e não sobre um espião:
// espião posto no lugar errado da cadeia passa verde com o defeito presente —
// foi o que aconteceu na primeira versão deste teste.
func TestRequestLoggerRegistraOAtorDefinidoAbaixoDele(t *testing.T) {
	linhas := capturarLog(t)

	// Espelha a cadeia do router: RequestLogger por FORA, autenticação por
	// DENTRO — que é exatamente a ordem que produzia o `user_id` vazio.
	h := RequestLogger(autenticadorDeTeste("u-123", http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPatch, "/x", nil))

	if got := campoDoLog(t, linhas.String(), "user_id"); got != "u-123" {
		t.Fatalf("a linha de log saiu com user_id=%q, esperado \"u-123\" — o log não diz "+
			"quem fez a escrita, e investigar incidente vira adivinhação.\nlinha: %s", got, linhas.String())
	}
}

// Controle: requisição anônima continua saindo com user_id vazio. O portador não
// pode inventar identidade para quem não autenticou.
func TestRequestLoggerNaoInventaAtorParaAnonimo(t *testing.T) {
	linhas := capturarLog(t)

	h := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	if got := campoDoLog(t, linhas.String(), "user_id"); got != "" {
		t.Fatalf("requisição anônima virou ator %q", got)
	}
}

// autenticadorDeTeste faz o que o auth.Middleware faz e é a razão do defeito:
// serve o próximo com um contexto NOVO, que só desce.
func autenticadorDeTeste(id string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(ComAtor(r.Context(), id)))
	})
}

func capturarLog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	anterior := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(anterior) })
	return &buf
}

func campoDoLog(t *testing.T, linha, campo string) string {
	t.Helper()

	if strings.TrimSpace(linha) == "" {
		t.Fatal("o RequestLogger não registrou nenhuma linha")
	}
	var registro map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(linha)), &registro); err != nil {
		t.Fatalf("linha de log não é JSON: %q", linha)
	}
	v, ok := registro[campo]
	if !ok {
		t.Fatalf("a linha de log não tem o campo %q: %s", campo, linha)
	}
	s, _ := v.(string)
	return s
}
