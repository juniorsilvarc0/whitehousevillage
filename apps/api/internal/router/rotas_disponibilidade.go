package router

import (
	"net/http"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

// rotasDisponibilidade declara as rotas do módulo de disponibilidade.
//
// Este arquivo pertence a quem implementa o módulo: mexer só aqui evita
// disputa com os outros módulos que estão sendo escritos em paralelo.
//
// Os pares (recurso, ação) são cópia literal do `x-rbac` de cada operação na
// OpenAPI, e os códigos vêm do catálogo semeado em cmd/seed/acesso.go. Ficam
// como literal, e não como constante em internal/auth, porque acrescentar
// constante lá é editar arquivo compartilhado — e recurso é DADO (regra 8),
// não vocabulário do Go.
//
// Nenhuma das três é coleção CRUD (`/availability` só tem GET, `/quotes` só tem
// POST), então o teste dos seis verbos não as alcança — e não deve.
func rotasDisponibilidade(d Deps) []Rota {
	if d.Disponibilidade == nil {
		return nil
	}
	return []Rota{
		// Rota estática antes da paramétrica na leitura humana; o chi resolve a
		// precedência sozinho, mas a ordem aqui evita a dúvida de quem revisa.
		{
			Metodo: http.MethodGet, Path: "/availability",
			Acesso: AcessoPermissao, Recurso: "calendar", Acao: auth.AcaoVer,
			Handler: d.Disponibilidade.PorProduto,
		},
		{
			Metodo: http.MethodGet, Path: "/availability/units",
			Acesso: AcessoPermissao, Recurso: "calendar", Acao: auth.AcaoVer,
			Handler: d.Disponibilidade.PorUnidade,
		},
		// `criar` e não `ver`: orçamento é ato comercial (é ele que carimba a
		// alçada do desconto), e o catálogo dá ao corretor escrita em `quotes`.
		{
			Metodo: http.MethodPost, Path: "/quotes",
			Acesso: AcessoPermissao, Recurso: "quotes", Acao: auth.AcaoCriar,
			Handler: d.Disponibilidade.Orcar,
		},
	}
}
