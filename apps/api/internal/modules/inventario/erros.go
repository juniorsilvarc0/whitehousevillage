package inventario

import (
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

// Os dois códigos de conflito que o contrato da Fase 1 declara para este módulo
// e que `internal/platform/apperr` ainda não expõe.
//
// Por que montados aqui e não lá: `apperr` é pacote de plataforma, e nesta
// rodada quatro módulos estão sendo escritos em paralelo — todos precisam de
// CODE_IN_USE. Quatro edições simultâneas do mesmo arquivo é a colisão que a
// divisão por pasta existe para evitar. Assim que o tech-lead acrescentar
// `apperr.CodeInUse` e `apperr.ResourceInUse`, estas duas linhas viram alias e
// o resto do módulo não muda: o que o código consome é o símbolo, não a string.
//
// O status sai de um 409 já definido (a cópia que WithMessage devolve), e só o
// `Code` é reescrito — é o campo público que o front consome.
var (
	// CodeInUse — violação da chave natural `UNIQUE (property_id, code)`.
	// Quem decide é a constraint, traduzindo o 23505: um SELECT antes do
	// INSERT perderia a corrida contra outra requisição no mesmo instante.
	CodeInUse = conflito("CODE_IN_USE", "Já existe registro com esse código nesta propriedade.")

	// ResourceInUse — o irmão genérico do ROLE_IN_USE que já existe. Recusa
	// desativar o que ainda sustenta operação viva.
	ResourceInUse = conflito("RESOURCE_IN_USE", "Ainda há vínculo ativo neste registro.")
)

func conflito(code, mensagem string) *apperr.Error {
	e := apperr.DateConflict.WithMessage(mensagem)
	e.Code = code
	return e
}
