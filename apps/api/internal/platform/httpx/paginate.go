package httpx

import (
	"net/http"
	"strconv"
	"strings"
)

const (
	PaginaPadrao    = 1
	PorPaginaPadrao = 25
	PorPaginaTeto   = 100
)

// ParsePage lê page e per_page da query. Valor ausente, não numérico ou fora da
// faixa cai no padrão em vez de virar erro: paginação inválida não é motivo para
// recusar uma listagem, e o teto de 100 é o que impede `per_page=100000` de
// arrastar a tabela inteira para a memória.
func ParsePage(r *http.Request) (pagina, porPagina int) {
	pagina = inteiroOu(r.URL.Query().Get("page"), PaginaPadrao)
	if pagina < 1 {
		pagina = PaginaPadrao
	}
	porPagina = inteiroOu(r.URL.Query().Get("per_page"), PorPaginaPadrao)
	if porPagina < 1 {
		porPagina = PorPaginaPadrao
	}
	if porPagina > PorPaginaTeto {
		porPagina = PorPaginaTeto
	}
	return pagina, porPagina
}

// Offset traduz página/tamanho para o OFFSET do SQL.
func Offset(pagina, porPagina int) int { return (pagina - 1) * porPagina }

// ParseSort resolve `?sort=-created_at` contra uma whitelist do recurso e
// devolve o fragmento de ORDER BY. Nada do que o cliente digitou entra na
// consulta: só o valor mapeado pela whitelist, senão `sort` seria injeção de SQL
// por definição.
func ParseSort(r *http.Request, permitidos map[string]string, padrao string) string {
	bruto := strings.TrimSpace(r.URL.Query().Get("sort"))
	if bruto == "" {
		return padrao
	}
	desc := strings.HasPrefix(bruto, "-")
	campo := strings.TrimPrefix(bruto, "-")

	coluna, ok := permitidos[campo]
	if !ok {
		return padrao
	}
	if desc {
		return coluna + " DESC"
	}
	return coluna + " ASC"
}

// ParseBool devolve o filtro booleano opcional: nil quando ausente ou ilegível,
// que é o "não filtra" do contrato (?active ausente traz ativos e inativos).
func ParseBool(r *http.Request, chave string) *bool {
	bruto := strings.TrimSpace(r.URL.Query().Get(chave))
	if bruto == "" {
		return nil
	}
	v, err := strconv.ParseBool(bruto)
	if err != nil {
		return nil
	}
	return &v
}

// Query devolve o parâmetro de busca já aparado.
func Query(r *http.Request, chave string) string {
	return strings.TrimSpace(r.URL.Query().Get(chave))
}

func inteiroOu(s string, padrao int) int {
	if s == "" {
		return padrao
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return padrao
	}
	return v
}
