package router

import "net/http"

// rotasVitrine é a superfície pública do site de vendas (apps/site) — passo A1
// de docs/unificacao-site-crm.md. Sem token e sem matriz, por definição: quem
// pergunta é um visitante anônimo. Em troca, três travas que as rotas internas
// não precisam: cada uma declara por que é pública (Motivo, exigido por
// ValidarTabela), toda `/public/*` passa pelo limitador por IP (router.go) e
// nenhuma resposta carrega dado de terceiro (o recorte é do módulo vitrine).
//
// Só uma grava: POST /public/holds (B1), a pré-reserva do próprio cliente, que
// passa pelo MESMO serviço de reservas do painel, assinada pela conta de
// serviço do site, e ganha um segundo limitador, mais apertado (router.go).
//
// Sem guarda contra handler nulo, como inventário e reservas: o teste de
// contrato monta a tabela com Deps zerado e PRECISA enxergar estas rotas —
// senão uma rota pública podia divergir da OpenAPI sem nenhuma linha vermelha.
// Criar o method value sobre ponteiro nulo é seguro; só chamá-lo não seria, e
// router.New sempre preenche o campo.
func rotasVitrine(d Deps) []Rota {
	return []Rota{
		{
			Metodo: http.MethodGet, Path: "/public/products", Acesso: AcessoPublico,
			Motivo:  "catálogo do site de vendas: só campos de vitrine (nome, lotação, tarifa vigente por tipo de data)",
			Handler: d.Vitrine.Produtos,
		},
		{
			Metodo: http.MethodGet, Path: "/public/policy", Acesso: AcessoPublico,
			Motivo:  "sinal, prazo do saldo e da pré-reserva exibidos ao hóspede antes da escolha da data; sem alçada de desconto",
			Handler: d.Vitrine.Politica,
		},
		{
			Metodo: http.MethodGet, Path: "/public/availability", Acesso: AcessoPublico,
			Motivo:  "calendário do site: livre ou ocupado por dia, sem motivo, sem contagem de unidades, janela curta e perto de hoje",
			Handler: d.Vitrine.Disponibilidade,
		},
		{
			Metodo: http.MethodPost, Path: "/public/quotes", Acesso: AcessoPublico,
			Motivo:  "orçamento do visitante pelo mesmo motor do painel; sem desconto, não grava e não segura data",
			Handler: d.Vitrine.Orcar,
		},
		{
			Metodo: http.MethodPost, Path: "/public/holds", Acesso: AcessoPublico,
			Motivo: "pré-reserva feita pelo próprio cliente no site: o mesmo reservas.Criar do painel (idempotência, " +
				"orçamento no servidor, EXCLUDE contra overbooking), assinado pela conta de serviço do site; " +
				"consentimento LGPD obrigatório, teto por telefone e limite próprio por IP",
			Handler: d.Vitrine.PreReservar,
		},
	}
}
