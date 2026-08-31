package contatos

import (
	"context"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// camposDePII é o conjunto de campos cujo VALOR nunca entra em `audit_log`.
//
// # Por que a trilha deste módulo é diferente da dos outros
//
// `audit.Redigir` protege SEGREDO (senha, token, chave) — o filtro dele é por
// substring no nome do campo, e "name", "email" e "phone_e164" não casam com
// nenhum termo dele, nem deveriam: numa reserva ou numa unidade esses valores
// são justamente o que a auditoria precisa mostrar.
//
// Aqui não. O contrato do `/anonymize` promete, textualmente, que "audit_log e
// pii_access_log guardam contact_id — não o nome". A promessa não é decorativa:
// se a edição da ficha gravasse `{"name": "Maria Silva"}` em `before`,
// anonimizar a ficha deixaria a cópia do dado eliminado numa tabela que todo
// perfil com `audit:ver` consegue ler. O direito de eliminação seria cumprido na
// tabela que o titular vê e descumprido na que ele não vê — que é a pior forma
// de descumprir.
//
// `doc_type`, `city`, `state`, `lgpd_basis`, `marketing_opt_in`, `consent_at` e
// `anonymized_at` FICAM com valor: não identificam ninguém sozinhos e são
// exatamente o que uma fiscalização pergunta ("com que base legal esta ficha
// existia?", "quando o opt-in foi ligado?").
var camposDePII = map[string]bool{
	"name":       true,
	"email":      true,
	"phone_e164": true,
	"doc_number": true,
	"birth_date": true,
	"notes":      true,
}

// registrarCriacao grava o nascimento da ficha sem copiar a PII para a trilha.
func registrarCriacao(ctx context.Context, exec db.DBTX, c Contato) error {
	return audit.Registrar(ctx, exec, audit.Evento{
		PropriedadeID: propriedadeDoAtor(ctx),
		Acao:          audit.Acao(Entidade, audit.VerboCriado),
		Entidade:      Entidade,
		EntidadeID:    c.ID,
		Depois:        semPII(audit.Snapshot(c)),
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
		Antes:         semPII(a),
		Depois:        semPII(d),
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
		Antes:         semPII(audit.Snapshot(c)),
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

// semPII devolve o documento com o VALOR dos campos pessoais trocado pela marca
// de redação, preservando a informação de QUE campo mudou.
func semPII(c audit.Campos) audit.Campos {
	if len(c) == 0 {
		return nil
	}
	out := make(audit.Campos, len(c))
	for chave, valor := range c {
		if camposDePII[chave] {
			// Nulo continua nulo: "o campo foi limpo" é informação de
			// auditoria e não é dado pessoal nenhum.
			if valor == nil {
				out[chave] = nil
				continue
			}
			out[chave] = audit.Redigido
			continue
		}
		out[chave] = valor
	}
	return out
}

func propriedadeDoAtor(ctx context.Context) uuid.UUID {
	if u, ok := auth.UserFrom(ctx); ok {
		return u.PropertyID
	}
	return uuid.Nil
}
