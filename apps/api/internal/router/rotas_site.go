package router

import (
	"net/http"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/site"
)

// RotaDaMidiaPublica é o arquivo enviado pelo painel (foto ou vídeo do site).
// Fica FORA do limitador de 120/min da vitrine — um vídeo sozinho faz dezenas
// de pedidos Range — e ganha um limitador próprio, mais largo (router.go). O
// nginx do site tem um `location` dedicado a este caminho: não renomear.
const RotaDaMidiaPublica = "/public/media/{id}"

// RotaDeEnvioDeMidia é o upload do painel: corpo de até 300 MB, fora do teto
// de 50 s por requisição (ver rotasDeLongaDuracao).
const RotaDeEnvioDeMidia = "/site/media"

// rotasSite é a área administrativa do site de vendas (docs/site-cms.md §5).
//
// Sem guarda contra handler nulo, como a vitrine: o teste de contrato monta a
// tabela com Deps zerado e precisa enxergar estas rotas.
func rotasSite(d Deps) []Rota {
	return []Rota{
		{Metodo: http.MethodGet, Path: "/site/content", Acesso: AcessoPermissao, Recurso: site.Recurso, Acao: auth.AcaoVer, Handler: d.Site.Conteudo},
		{Metodo: http.MethodPut, Path: "/site/content/{key}", Acesso: AcessoPermissao, Recurso: site.Recurso, Acao: auth.AcaoEditar, Handler: d.Site.Gravar},
		{Metodo: http.MethodDelete, Path: "/site/content/{key}", Acesso: AcessoPermissao, Recurso: site.Recurso, Acao: auth.AcaoEditar, Handler: d.Site.Restaurar},
		{Metodo: http.MethodPost, Path: RotaDeEnvioDeMidia, Acesso: AcessoPermissao, Recurso: site.Recurso, Acao: auth.AcaoEditar, Handler: d.Site.EnviarMidia},
		{
			Metodo: http.MethodGet, Path: "/public/site", Acesso: AcessoPublico,
			Motivo: "textos, fotos e vídeos do site editados pela gestão — o mesmo conteúdo que o visitante já lê no HTML; " +
				"só o que foi editado, sem autor, sem data e sem id interno",
			Handler: d.Site.Publico,
		},
		{
			Metodo: http.MethodGet, Path: RotaDaMidiaPublica, Acesso: AcessoPublico,
			Motivo: "arquivo de foto ou vídeo publicado no site pela gestão; o id é aleatório e o arquivo é imutável. " +
				"Limitador próprio, mais largo que o da vitrine, porque o vídeo é pedido em pedaços (Range)",
			Handler: d.Site.Midia,
		},
	}
}
