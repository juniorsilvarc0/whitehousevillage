package users

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

// Travas de escalada de privilégio do cadastro de usuários.
//
// O problema que elas resolvem, em linguagem de negócio: `role_id`, `password` e
// `email` são campos de FORMULÁRIO, mas o que eles decidem é ACESSO. Sem trava,
// quem recebeu "editar usuários" para trocar o telefone da recepção também podia
// gravar `role_id = <admin>` no próprio cadastro e, na requisição seguinte —
// sem novo login, porque a matriz é lida do banco a cada requisição — passar a
// mandar na instalação inteira. O mesmo caminho servia para trocar a senha de
// terceiros e entrar na conta de quem quer que fosse.
//
// As quatro travas:
//
//  1. ninguém muda o PRÓPRIO papel (nem o administrador; ele usa outra conta),
//     pela mesma razão que ninguém se desativa: o caminho para uma instalação
//     sem administrador não passa por um clique distraído;
//  2. atribuir papel exige `roles:editar` — administrar acesso é ato do
//     administrador de acesso, não de quem cuida do cadastro — e o papel
//     atribuído tem de ser SUBCONJUNTO do papel do ator: ninguém concede o que
//     não tem;
//  3. trocar a senha de terceiro exige a MESMA autoridade sobre o papel atual
//     do alvo, porque quem escolhe a senha de alguém entra na conta e herda o
//     acesso dela — é atribuição de papel por outro caminho;
//  4. trocar o E-MAIL de terceiro exige exatamente a mesma autoridade que a
//     senha. E-mail parece cadastro e é credencial: é o endereço para onde
//     `/auth/password/forgot` manda o token de recuperação. Quem escreve o
//     e-mail do administrador redefine a senha dele minutos depois e entra na
//     conta com o papel dele — a trava 3 sozinha só obrigava o atacante a dar
//     uma volta a mais.
//
// Por que a trava 3 exige autoridade em vez de proibir de vez: a redefinição
// por `/auth/password/forgot` depende de e-mail entregue, e na Fase 0 o
// Notificador ainda cai em log. Proibir agora deixaria a instalação sem nenhuma
// forma de devolver acesso a quem perdeu o e-mail — o administrador teria de ir
// ao SQL. Com o teto de privilégio, quem redefine a senha de alguém não ganha
// nada que já não pudesse conceder atribuindo o papel.
//
// Trocar o próprio e-mail continua livre — é o endereço de contato de quem já
// está autenticado, e exigir chamado ao administrador para isso só geraria
// suporte. Mas QUALQUER troca de e-mail, própria ou de terceiro, derruba as
// sessões da conta afetada e deixa linha de auditoria (ver service.go): a
// credencial de recuperação mudou, então quem estava dentro se reapresenta, e
// uma tomada de conta bem-sucedida precisa deixar rastro para ser investigada.
//
// Nota deliberada: `roles:editar` é, na prática, o bit de administrador — quem
// o tem decide a matriz dos perfis não-sistema em `PUT /roles/{id}/permissions`
// (agora limitado pelo próprio teto, ver modules/roles/service.go). É por isso
// que ele é a autoridade exigida aqui, e é a mesma definição que
// `ContarAdministradoresAtivos` usa para a trava do "último administrador".

// limiteDeCelulasNoErro corta a lista de células que volta no 403. Um perfil
// de administrador tem 79 células; despejar todas num corpo de erro não ajuda
// ninguém a entender e ainda vira ruído no log.
const limiteDeCelulasNoErro = 10

// atorIlimitado diz se o ator é o perfil raiz (`is_system`, o `admin` do seed).
// Ele é definido como "tudo", inclusive os recursos que uma migration futura
// acrescentar: sem esta saída, publicar um módulo novo tornaria o recurso
// inconcedível por qualquer pessoa, porque ninguém teria a célula ainda. Não
// afrouxa nada — quem é raiz já possui todas as células existentes, e
// `POST /roles` grava `is_system=false` sempre, então o sinalizador não é
// forjável pela API.
func atorIlimitado(ator *auth.Usuario) bool { return ator != nil && ator.RoleIsSystem }

// excedentes devolve as células do papel que o ator NÃO possui, já no formato
// "recurso:ação:escopo". Lista vazia significa subconjunto — o papel cabe
// dentro do ator.
//
// Escopo entra na comparação: um ator que só enxerga `own` não pode conceder
// `all`, senão a delegação ampliaria o alcance de quem delegou.
func excedentes(papel []auth.Permissao, ator auth.Conjunto) []string {
	var faltando []string
	for _, p := range papel {
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

// concedeAdministracao diz se o papel carrega o bit de administrador de acesso.
func concedeAdministracao(perms []auth.Permissao) bool {
	for _, p := range perms {
		if p.Resource == auth.RecursoPerfis && p.Action == auth.AcaoEditar {
			return true
		}
	}
	return false
}

// forbiddenPorTeto monta o 403 com as células que faltam ao ator. O contrato
// documenta 403 FORBIDDEN para os dois casos (sem `roles:editar` e papel acima
// do teto), e não 422: não é um valor mal digitado, é uma operação que este
// usuário não tem autoridade para pedir.
func forbiddenPorTeto(campo string, faltando []string) error {
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
			"field":   campo,
			"missing": mostradas,
		})
}

// exigirAdministracaoDeAcesso é a trava 2 na sua parte binária.
func exigirAdministracaoDeAcesso(ator *auth.Usuario) error {
	if ator.Permissoes.Pode(auth.RecursoPerfis, auth.AcaoEditar) {
		return nil
	}
	return apperr.Forbidden.
		WithMessage("Atribuir perfil é administrar acesso: requer permissão de editar perfis.").
		WithDetails(map[string]any{
			"resource": auth.RecursoPerfis,
			"action":   auth.AcaoEditar,
		})
}

// autorizarPapelAtribuido é a trava 2 inteira: autoridade + teto de privilégio.
// Usada no POST /users e na troca de `role_id` do PATCH/PUT.
//
// Devolve a matriz do papel atribuído para quem chama não precisar relê-la — a
// trava do último administrador logo abaixo depende dela.
func (s *Service) autorizarPapelAtribuido(ctx context.Context, ator *auth.Usuario, papel uuid.UUID) ([]auth.Permissao, error) {
	if err := exigirAdministracaoDeAcesso(ator); err != nil {
		return nil, err
	}

	// Papel inexistente devolve matriz vazia aqui e cai no 422 `role_id:
	// perfil inexistente` da FK, no INSERT/UPDATE — a ordem não importa para a
	// segurança, e duplicar a checagem de existência custaria outra consulta.
	perms, err := s.repo.PermissoesDoPapel(ctx, papel)
	if err != nil {
		return nil, err
	}
	if !atorIlimitado(ator) {
		if faltando := excedentes(perms, ator.Permissoes); len(faltando) > 0 {
			return nil, forbiddenPorTeto("role_id", faltando)
		}
	}
	return perms, nil
}

// mudancas resume o que um PATCH/PUT autorizado vai mexer e que a GRAVAÇÃO
// precisa saber. Existe para a decisão ser tomada uma vez só: recalcular
// "trocou o e-mail?" dentro da transação obrigaria a reler o alvo, e recalcular
// "rebaixou administrador?" obrigaria a reler as duas matrizes.
type mudancas struct {
	// trocouEmail é verdadeiro só quando o endereço REALMENTE muda — reenviar o
	// mesmo e-mail (o PUT obriga o campo no corpo) não é troca.
	trocouEmail   bool
	emailAnterior string
	emailNovo     string

	// rebaixouAdministrador diz que o papel novo tira do alvo o bit de
	// administrador. Quem confere se ainda sobra alguém é o service, DENTRO da
	// transação e com trava — a contagem feita aqui fora perdia a corrida.
	rebaixouAdministrador bool
}

// autorizarMudancasSensiveis roda as quatro travas sobre um PATCH/PUT já
// normalizado. Devolve o erro da PRIMEIRA trava violada.
func (s *Service) autorizarMudancasSensiveis(ctx context.Context, alvo auth.LinhaUsuario, a Atualizar) (mudancas, error) {
	var m mudancas

	ator, temAtor := auth.UserFrom(ctx)
	if !temAtor {
		// Sem sessão no contexto a chamada não veio da API — toda rota de
		// /users é AcessoPermissao, e o middleware sempre injeta o usuário.
		// Sobra seed e script de manutenção, onde não há privilégio de ator a
		// limitar (quem roda já tem o banco na mão). A troca de e-mail ainda
		// assim é registrada: auditoria de script é o que explica, meses
		// depois, por que o endereço de alguém mudou sozinho.
		m.trocouEmail, m.emailAnterior, m.emailNovo = trocaDeEmail(alvo, a)
		return m, nil
	}

	novoPapel, papelNoCorpo := a.RoleID.Definido()
	// Reenviar o MESMO papel não é atribuição: o PUT é substituição integral e
	// obriga `role_id` no corpo, então exigir autoridade aqui impediria que
	// alguém com `users:editar` corrigisse um telefone pelo PUT.
	trocandoPapel := papelNoCorpo && novoPapel != alvo.RoleID

	_, senhaNoCorpo := a.Senha.Definido()
	trocandoSenhaDeTerceiro := senhaNoCorpo && ator.ID != alvo.ID

	m.trocouEmail, m.emailAnterior, m.emailNovo = trocaDeEmail(alvo, a)
	trocandoEmailDeTerceiro := m.trocouEmail && ator.ID != alvo.ID

	// Trava 1 — o próprio papel é intocável, inclusive para o administrador.
	if trocandoPapel && ator.ID == alvo.ID {
		return m, apperr.Validation(map[string]string{
			"role_id": "você não pode alterar o próprio perfil; peça a outro administrador.",
		})
	}

	// A matriz atual do alvo serve às três travas seguintes; lida uma vez só.
	var permsDoAlvo []auth.Permissao
	if trocandoSenhaDeTerceiro || trocandoEmailDeTerceiro || trocandoPapel {
		var err error
		if permsDoAlvo, err = s.repo.PermissoesDoPapel(ctx, alvo.RoleID); err != nil {
			return m, err
		}
	}

	// Travas 3 e 4 — senha e e-mail de terceiro exigem autoridade sobre o papel
	// ATUAL do alvo: quem define a senha entra na conta, e quem define o e-mail
	// manda para si o token que define a senha. As duas herdam o acesso do alvo,
	// então as duas pedem autoridade sobre esse acesso.
	for _, t := range []struct {
		aplicavel bool
		campo     string
	}{
		{trocandoSenhaDeTerceiro, "password"},
		{trocandoEmailDeTerceiro, "email"},
	} {
		if !t.aplicavel {
			continue
		}
		if err := exigirAdministracaoDeAcesso(ator); err != nil {
			return m, err
		}
		if !atorIlimitado(ator) {
			if faltando := excedentes(permsDoAlvo, ator.Permissoes); len(faltando) > 0 {
				return m, forbiddenPorTeto(t.campo, faltando)
			}
		}
	}

	if !trocandoPapel {
		return m, nil
	}

	// Trava 2 — autoridade + teto sobre o papel NOVO.
	permsNovas, err := s.autorizarPapelAtribuido(ctx, ator, novoPapel)
	if err != nil {
		return m, err
	}

	m.rebaixouAdministrador = rebaixaAdministrador(permsDoAlvo, permsNovas)
	return m, nil
}

// trocaDeEmail diz se o corpo realmente muda o endereço, e qual era o anterior.
//
// A comparação ignora caixa porque o índice único de `users` é sobre
// `lower(email)`: "Fulano@wh.local" e "fulano@wh.local" são a MESMA conta, e
// tratar a diferença de caixa como troca derrubaria a sessão de quem só
// reenviou o cadastro pelo PUT.
func trocaDeEmail(alvo auth.LinhaUsuario, a Atualizar) (trocou bool, anterior, novo string) {
	v, noCorpo := a.Email.Definido()
	if !noCorpo || strings.EqualFold(v, alvo.Email) {
		return false, alvo.Email, alvo.Email
	}
	return true, alvo.Email, v
}

// rebaixaAdministrador diz se o papel novo tira do alvo a capacidade de
// administrar acesso. É função pura: quem decide se ainda sobra administrador
// é o service, dentro da transação.
func rebaixaAdministrador(permsAtuais, permsNovas []auth.Permissao) bool {
	return concedeAdministracao(permsAtuais) && !concedeAdministracao(permsNovas)
}
