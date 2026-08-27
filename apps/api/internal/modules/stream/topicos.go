package stream

import (
	"net/http"
	"strings"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/realtime"
)

// Recursos que o tempo real consulta na matriz. Não são constantes do pacote
// `auth` porque lá só moram os recursos que o Go precisa nomear em mais de um
// lugar; o catálogo de verdade é a tabela `resources` (seed).
const (
	recursoCalendario = "calendar"
	recursoCRM        = "crm.opportunities"
)

// permissaoDoTopico é o `x-rbac-topicos` do contrato, em Go.
//
// Este mapa é a razão de `/stream` ser `AcessoAutenticado` e não
// `AcessoPermissao`: a rota não é um recurso. Uma única célula (recurso, ação)
// na tabela de rotas não conseguiria dizer "calendário exige uma coisa e o
// funil exige outra, e quem só alcança um dos dois entra assim mesmo".
type permissaoDoTopico struct {
	Recurso string
	Acao    string
}

var permissaoPorTopico = map[string]permissaoDoTopico{
	realtime.TopicoCalendario: {Recurso: recursoCalendario, Acao: auth.AcaoVer},
	realtime.TopicoCRM:        {Recurso: recursoCRM, Acao: auth.AcaoVer},
}

// topicosPedidos lê e valida o parâmetro `topics`.
//
// Nome desconhecido é 422 com a lista em `details.topics`, e não silêncio: uma
// conexão aberta que nunca entrega nada é indistinguível de "ainda não
// aconteceu nada" — o defeito mais caro de diagnosticar num tempo real.
func topicosPedidos(r *http.Request) ([]string, error) {
	bruto := strings.TrimSpace(httpx.Query(r, "topics"))
	if bruto == "" {
		return nil, apperr.Validation(map[string]any{
			"topics": "informe ao menos um tópico (calendar, crm).",
		})
	}

	var (
		pedidos      []string
		vistos       = map[string]bool{}
		desconhecido []string
	)
	for _, parte := range strings.Split(bruto, ",") {
		nome := strings.TrimSpace(parte)
		if nome == "" {
			continue
		}
		if !realtime.TopicoConhecido(nome) {
			desconhecido = append(desconhecido, nome)
			continue
		}
		if vistos[nome] {
			continue
		}
		vistos[nome] = true
		pedidos = append(pedidos, nome)
	}

	if len(desconhecido) > 0 {
		return nil, apperr.Validation(map[string]any{
			"topics":       "tópico desconhecido: " + strings.Join(desconhecido, ", "),
			"unknown":      desconhecido,
			"known_topics": []string{realtime.TopicoCalendario, realtime.TopicoCRM},
		})
	}
	if len(pedidos) == 0 {
		return nil, apperr.Validation(map[string]any{
			"topics": "informe ao menos um tópico (calendar, crm).",
		})
	}
	return pedidos, nil
}

// alcancaveis filtra os pedidos pela matriz do requisitante.
//
// Tópico fora da matriz é DESCARTADO da assinatura, não recusado: quem só pode
// ver o calendário e pediu `calendar,crm` continua recebendo o calendário. O
// `ready` diz o que sobrou, para o cliente saber o que não conseguiu — e é isso
// que evita a pergunta "por que meu kanban não se mexe?".
func alcancaveis(u *auth.Usuario, pedidos []string) []string {
	var out []string
	for _, t := range pedidos {
		p, ok := permissaoPorTopico[t]
		if !ok {
			continue
		}
		if u.Permissoes.Pode(p.Recurso, p.Acao) {
			out = append(out, t)
		}
	}
	return out
}
