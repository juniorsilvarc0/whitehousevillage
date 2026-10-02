package reservas

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/commission"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Corretor da venda (F2-09/F2-13; contrato: "Corretor da venda" em
// POST /reservations).
//
// Medido em 31/08/2026: `corretor@wh.local`, com `reservations:criar` em escopo
// `own`, gravou uma venda no `broker_id` de outro corretor e recebeu 201
// (WH-2026-0009). O campo ia do corpo direto para o INSERT. A partir do F2-13
// a comissão nasce desse campo — quem o escreve à vontade se atribui dinheiro.
//
// Três camadas, cada uma fechando o que a outra não fecha:
//
//  1. A FK `reservations_broker_id_fkey` recusa o UUID inventado (23503 →
//     422 em `details.broker_id`, pelo db.MapError). Sem SELECT antes.
//  2. `commission.ResolveBroker` decide o valor e a recusa de autoridade
//     (403), com o escopo lido da MATRIZ — nunca do nome do perfil.
//  3. A instrução que grava confere, no próprio SQL, que em escopo `own` o
//     valor é nulo ou o `users.broker_id` do ator NAQUELE instante. A leitura
//     do corretor do ator que alimenta o domínio é anterior à escrita; se a
//     conta for desvinculada no meio, só a condição dentro do INSERT/UPDATE
//     vê — conferir antes e gravar depois é TOCTOU.

// AtribuicaoDoCorretor é o que a escrita grava e como o banco deve conferir.
type AtribuicaoDoCorretor struct {
	// Corretor é o valor final de `reservations.broker_id`; nil é venda direta.
	Corretor *uuid.UUID
	// RestritoA, quando presente, é o ator em escopo `own`: a instrução só
	// grava se Corretor for nil ou o `users.broker_id` dele.
	RestritoA *uuid.UUID
}

// mensagemDoCorretorInvalido nunca carrega nome nem id: nome de pessoa não
// entra em mensagem de erro, e o id do colega na mensagem seria a enumeração
// que o 403 existe para não fazer.
const mensagemDoCorretorInvalido = "Corretor inválido para o seu escopo."

// recusaDoCorretor é o 403 do contrato, com o `reason` estável que a tela lê.
func recusaDoCorretor(motivo commission.Reason) *apperr.Error {
	return apperr.Forbidden.
		WithMessage(mensagemDoCorretorInvalido).
		WithDetails(map[string]string{
			"field":  "broker_id",
			"scope":  string(commission.ScopeOwn),
			"reason": string(motivo),
		})
}

// escopoDoCorretor traduz o escopo da MATRIZ para o domínio. Sem a célula
// (o `/win` do CRM cria reserva para quem tem `crm.opportunities:editar` e
// talvez não tenha `reservations:criar`) vale `own`: é a falha fechada — quem
// não tem `all` não atribui venda a terceiro.
func escopoDoCorretor(ctx context.Context, acao string) commission.Scope {
	if auth.ScopeOf(ctx, Recurso, acao) == auth.EscopoAll {
		return commission.ScopeAll
	}
	return commission.ScopeOwn
}

// campoDoCorretor converte o Opt do corpo no Field do domínio, que não
// importa a camada HTTP.
func campoDoCorretor(o httpx.Opt[uuid.UUID]) commission.Field[uuid.UUID] {
	if v, ok := o.Definido(); ok {
		return commission.Set(v)
	}
	if o.DeveLimpar() {
		return commission.Null[uuid.UUID]()
	}
	return commission.Absent[uuid.UUID]()
}

// resolverCorretor monta a entrada do domínio e traduz a saída. Roda DENTRO
// da transação da escrita: a leitura do corretor do ator e a gravação
// enxergam a mesma conta — e a condição no SQL cobre o que mudar entre as duas.
func (s *Servico) resolverCorretor(ctx context.Context, u *auth.Usuario, acao string, verbo commission.Write,
	pedido httpx.Opt[uuid.UUID], atual *uuid.UUID, status string) (AtribuicaoDoCorretor, error) {

	escopo := escopoDoCorretor(ctx, acao)
	entrada := commission.BrokerWrite[uuid.UUID]{
		Write:      verbo,
		Scope:      escopo,
		Current:    atual,
		BeyondHold: status != EstadoQuote && status != EstadoHold,
		Requested:  campoDoCorretor(pedido),
	}
	var restrito *uuid.UUID
	if escopo == commission.ScopeOwn {
		corretorDoAtor, err := s.repo.CorretorDaConta(ctx, u.ID)
		if err != nil {
			return AtribuicaoDoCorretor{}, err
		}
		entrada.ActorBroker = corretorDoAtor
		ator := u.ID
		restrito = &ator
	}

	decisao, err := commission.ResolveBroker(entrada)
	if err != nil {
		var recusa *commission.Refusal
		if errors.As(err, &recusa) {
			return AtribuicaoDoCorretor{}, recusaDoCorretor(recusa.Reason).WithCause(err)
		}
		return AtribuicaoDoCorretor{}, apperr.Internal.WithCause(err)
	}
	return AtribuicaoDoCorretor{Corretor: decisao.Broker, RestritoA: restrito}, nil
}
