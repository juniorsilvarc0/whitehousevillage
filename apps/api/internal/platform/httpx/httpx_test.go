package httpx

import (
	"encoding/json"
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
