package contatos

import (
	"context"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/pii"
)

// A trilha deste módulo passa TODO documento por [pii.Redigir] antes de
// entregá-lo a `audit.Registrar`.
//
// Motivo, em uma frase: `audit.Redigir` protege segredo e não protege pessoa —
// "name" e "phone_e164" não casam com nenhum termo do filtro dele, e não
// deveriam. O dicionário de campos pessoais, a razão de cada campo estar ou não
// nele e a descida em documento aninhado vivem em `internal/platform/pii`,
// porque a mesma obrigação vale para recebível, comissão e rooming list.

// registrarCriacao grava o nascimento da ficha sem copiar a PII para a trilha.
func registrarCriacao(ctx context.Context, exec db.DBTX, c Contato) error {
	return audit.Registrar(ctx, exec, audit.Evento{
		PropriedadeID: propriedadeDoAtor(ctx),
		Acao:          audit.Acao(Entidade, audit.VerboCriado),
		Entidade:      Entidade,
		EntidadeID:    c.ID,
		Depois:        pii.Redigir(audit.Snapshot(c)),
	})
}

// registrarAlteracao grava SÓ o que mudou, e mascara o valor do que é PII.
//
// A ordem importa e é a razão de esta função não usar `audit.Alteracao`: o
// recorte (`audit.Diff`) roda sobre os valores REAIS, e a máscara vem depois.
// Mascarar primeiro faria os dois lados virarem a mesma marca, o Diff os
// consideraria iguais e a troca de nome sumiria da trilha — auditoria que perde
// justamente a alteração que ela existe para registrar.
func registrarAlteracao(ctx context.Context, exec db.DBTX, antes, depois Contato) error {
	a, d := audit.Diff(audit.Snapshot(antes), audit.Snapshot(depois))
	if len(a) == 0 && len(d) == 0 {
		return nil
	}
	return audit.Registrar(ctx, exec, audit.Evento{
		PropriedadeID: propriedadeDoAtor(ctx),
		Acao:          audit.Acao(Entidade, audit.VerboAlterado),
		Entidade:      Entidade,
		EntidadeID:    depois.ID,
		Antes:         pii.Redigir(a),
		Depois:        pii.Redigir(d),
	})
}

// registrarExclusao guarda o estado que deixou de existir — sem a PII, que é o
// que o `DELETE` acabou de apagar e que a trilha não pode ressuscitar.
func registrarExclusao(ctx context.Context, exec db.DBTX, c Contato) error {
	return audit.Registrar(ctx, exec, audit.Evento{
		PropriedadeID: propriedadeDoAtor(ctx),
		Acao:          audit.Acao(Entidade, audit.VerboExcluido),
		Entidade:      Entidade,
		EntidadeID:    c.ID,
		Antes:         pii.Redigir(audit.Snapshot(c)),
	})
}

// registrarAnonimizacao é a única linha da trilha que carrega texto livre do
// operador: o `reason`.
//
// Ele vai em `after` (e não em `before`) porque é o que passou a valer sobre a
// ficha — e é a única coisa que responde "por que este contato está vazio?"
// seis meses depois. `preserved` entra junto: a prova, gravada, de que a
// eliminação não tocou no razão.
func registrarAnonimizacao(ctx context.Context, exec db.DBTX, id uuid.UUID, motivo string, preservado Vinculos) error {
	return audit.Registrar(ctx, exec, audit.Evento{
		PropriedadeID: propriedadeDoAtor(ctx),
		Acao:          audit.Acao(Entidade, VerboAnonimizado),
		Entidade:      Entidade,
		EntidadeID:    id,
		Depois: audit.Campos{
			"reason":    motivo,
			"preserved": audit.Snapshot(preservado),
		},
	})
}

func propriedadeDoAtor(ctx context.Context) uuid.UUID {
	if u, ok := auth.UserFrom(ctx); ok {
		return u.PropertyID
	}
	return uuid.Nil
}
