package roles

import (
	"fmt"
	"sort"
	"strings"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

// Travas de escalada de privilégio da MATRIZ de permissões.
//
// O defeito que elas fecham, em linguagem de negócio: `PUT
// /roles/{id}/permissions` gravava a grade inteira sem nunca comparar o que foi
// pedido com o que o ator possui. Quem tinha `roles:editar` num perfil
// não-sistema reescrevia o PRÓPRIO perfil com as 79 células do catálogo e, na
// requisição seguinte — sem novo login, porque a matriz é lida do banco a cada
// requisição —, passava a poder conceder qualquer coisa a qualquer um. O teto de
// privilégio do cadastro de usuários virava enfeite: em vez de tentar promover
// alguém acima do próprio teto e tomar 403, o atacante ampliava o teto primeiro.
//
// As duas travas:
//
//  1. ninguém grava célula que NÃO POSSUI — o mesmo teto de privilégio que
//     `PATCH /users` aplica a `role_id`, agora aplicado à matriz inteira;
//  2. ninguém edita a matriz do PRÓPRIO perfil, nem para menos. A matriz é o
//     que limita o ator; deixá-lo mexer nela é deixar o preso guardar a chave da
//     cela. Quem precisa de mais acesso pede a outro administrador — a mesma
//     regra que já vale para o próprio `role_id`.
//
// Por que a trava 2 existindo a trava 1: sem ela a escalada continuaria de pé
// em passos pequenos e a discussão viraria de grau. E há o caso concreto do
// rebaixamento acidental — o administrador que se tira `roles:editar` sem
// perceber e tranca a chave dentro do carro.
//
// A comparação é feita aqui, e não reaproveitada de `modules/users`, porque a
// função é privada daquele pacote e `internal/auth` (o único lugar comum) não é
// deste agente. Duas cópias de quinze linhas, cada uma com teste próprio, valem
// mais que um import que ninguém pode escrever.

// limiteDeCelulasNoErro corta a lista que volta no 403: o catálogo tem 79
// células e despejar todas num corpo de erro só vira ruído.
const limiteDeCelulasNoErro = 10

// excedentes devolve as células da matriz pedida que o ator NÃO possui, no
// formato "recurso:ação:escopo". Lista vazia significa que a matriz cabe dentro
// do ator.
//
// Escopo entra na comparação: quem só enxerga `own` não concede `all`, senão a
// delegação ampliaria o alcance de quem delegou.
func excedentes(pedida Matriz, ator auth.Conjunto) []string {
	var faltando []string
	for _, p := range pedida {
		escopoDoAtor, tem := ator.Escopo(p.Resource, p.Action)
		if !tem {
			faltando = append(faltando, celula(p))
			continue
		}
		if escopoDoAtor == auth.EscopoOwn && p.Scope != auth.EscopoOwn {
			faltando = append(faltando, celula(p))
		}
	}
	sort.Strings(faltando)
	return faltando
}

func celula(p auth.Permissao) string {
	escopo := p.Scope
	if escopo == "" {
		escopo = auth.EscopoAll
	}
	return p.Resource + ":" + p.Action + ":" + escopo
}

// forbiddenPorTeto monta o 403 dizendo o que falta ao ator — sem isso, quem
// administra vê o botão de salvar recusar e não descobre por quê.
//
// É 403 e não 422: não é valor mal digitado, é operação que este usuário não tem
// autoridade para pedir.
func forbiddenPorTeto(faltando []string) error {
	mostradas := faltando
	if len(mostradas) > limiteDeCelulasNoErro {
		mostradas = append(append([]string(nil), mostradas[:limiteDeCelulasNoErro]...),
			fmt.Sprintf("… e mais %d", len(faltando)-limiteDeCelulasNoErro))
	}
	return apperr.Forbidden.
		WithMessage(fmt.Sprintf(
			"Você não pode conceder mais acesso do que possui (falta a você: %s).",
			strings.Join(mostradas, ", "))).
		WithDetails(map[string]any{
			"field":   "permissions",
			"missing": mostradas,
		})
}

// forbiddenPorSerOProprioPerfil é a trava 2.
func forbiddenPorSerOProprioPerfil() error {
	return apperr.Forbidden.
		WithMessage("Você não pode alterar as permissões do seu próprio perfil; peça a outro administrador.").
		WithDetails(map[string]any{
			"field":  "permissions",
			"reason": "proprio_perfil",
		})
}

// autorizarMatriz roda as duas travas. Sem sessão no contexto não há ator a
// limitar: sobra seed e script de manutenção, onde quem roda já tem o banco na
// mão (a mesma decisão de modules/users).
func autorizarMatriz(ator *auth.Usuario, perfil Perfil, m Matriz) error {
	if perfil.ID == ator.RoleID {
		return forbiddenPorSerOProprioPerfil()
	}
	// O perfil raiz (`is_system`, o `admin` do seed) não passa pelo teto: ele é
	// definido como "tudo". Sem esta saída, um recurso criado por migration
	// futura ficaria inconcedível por QUALQUER pessoa — ninguém teria a célula
	// nova, e o único caminho seria SQL na mão. Não afrouxa nada: quem é raiz já
	// possui todas as células existentes, e `POST /roles` grava `is_system=false`
	// sempre, então o sinalizador não é forjável pela API.
	if ator.RoleIsSystem {
		return nil
	}
	if faltando := excedentes(m, ator.Permissoes); len(faltando) > 0 {
		return forbiddenPorTeto(faltando)
	}
	return nil
}

// concedeAdministracao diz se a matriz carrega o bit de administrador de acesso
// — `roles:editar`, quem consegue consertar o acesso de todo mundo. Mesma
// definição por DADO que `modules/users` usa; não é `code = 'admin'`.
func concedeAdministracao(perms []auth.Permissao) bool {
	for _, p := range perms {
		if p.Resource == auth.RecursoPerfis && p.Action == auth.AcaoEditar {
			return true
		}
	}
	return false
}
