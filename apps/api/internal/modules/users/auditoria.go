package users

import (
	"context"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Auditoria é uma linha de `audit_log` escrita por este módulo.
//
// Por que a troca de e-mail é auditada e a de telefone não: o e-mail é a
// credencial de recuperação de senha. Uma tomada de conta bem-sucedida começa
// exatamente aqui, e sem esta linha ninguém consegue responder, depois, "quem
// trocou o endereço do administrador e quando" — as colunas `before`/`after`
// guardam os dois endereços porque o UPDATE já apagou o antigo da tabela.
type Auditoria struct {
	PropriedadeID uuid.UUID
	AtorID        *uuid.UUID
	Acao          string
	Entidade      string
	EntidadeID    uuid.UUID
	Antes         map[string]any
	Depois        map[string]any
	RequestID     string
}

// AcaoEmailAlterado é o `action` da linha de auditoria da troca de e-mail.
// Prefixado pelo recurso para a tela de auditoria poder filtrar por família.
const AcaoEmailAlterado = auth.RecursoUsuarios + ".email_alterado"

// auditoriaDeEmail monta a linha a partir do contexto da requisição.
//
// O ator sai do contexto e pode ser nulo (seed, script de manutenção): a coluna
// `actor_id` é anulável justamente para o rastro do script não ser descartado
// por falta de um usuário para culpar.
func auditoriaDeEmail(ctx context.Context, alvo auth.LinhaUsuario, m mudancas) Auditoria {
	var ator *uuid.UUID
	if u, ok := auth.UserFrom(ctx); ok {
		id := u.ID
		ator = &id
	}

	return Auditoria{
		PropriedadeID: alvo.PropertyID,
		AtorID:        ator,
		Acao:          AcaoEmailAlterado,
		Entidade:      auth.RecursoUsuarios,
		EntidadeID:    alvo.ID,
		Antes:         map[string]any{"email": m.emailAnterior},
		Depois:        map[string]any{"email": m.emailNovo},
		RequestID:     httpx.RequestIDDoContexto(ctx),
	}
}
