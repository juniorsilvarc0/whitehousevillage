package tarifario

import (
	"net/http"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

// Códigos de erro do tarifário. Os três estão no enum da OpenAPI da Fase 1 e
// ainda NÃO existem em `internal/platform/apperr`.
//
// Por que nascem aqui e não lá: `apperr.go` é arquivo compartilhado, e nesta
// rodada quatro módulos estão sendo escritos em paralelo — três deles precisam
// de códigos novos. Cada um editando o mesmo arquivo é a colisão anunciada. O
// código estável (que é o que o painel consome) sai correto de qualquer forma;
// quando a rodada fechar, o lugar destes três é o pacote apperr, e este arquivo
// some sem o front perceber.
var (
	// ErroCodigoEmUso é a violação de chave natural: `rate_tables(property_id,
	// name)`, `holidays(property_id, date)`, `rates(table, produto, tipo)`,
	// `min_nights_rules(table, tipo)`, `special_periods(property_id, name)`.
	ErroCodigoEmUso = conflito("CODE_IN_USE", "Já existe um registro com esta chave.")

	// ErroRecursoEmUso recusa apagar o que ainda sustenta alguma coisa — o irmão
	// genérico do ROLE_IN_USE que já existe.
	ErroRecursoEmUso = conflito("RESOURCE_IN_USE", "Este registro ainda está em uso.")

	// ErroPoliticaImutavel é a trava do versionamento: versão publicada não se
	// reescreve nem se antedata.
	ErroPoliticaImutavel = conflito("POLICY_IMMUTABLE", "Política já publicada não pode ser reescrita nem antedatada.")
)

// conflito monta um erro 409 com código próprio a partir de um erro já definido
// no apperr.
//
// O truque existe porque `apperr.define` é privado: as três funções `With*`
// devolvem uma CÓPIA do erro, e é essa cópia — nunca a variável do pacote
// apperr — que tem o Code trocado. Sem a cópia, isto reescreveria o
// ROLE_IN_USE do módulo de perfis em tempo de import.
func conflito(codigo, mensagem string) *apperr.Error {
	e := apperr.RoleInUse.WithStatus(http.StatusConflict).WithMessage(mensagem)
	e.Code = codigo
	return e
}
