package inventario

import (
	"context"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// Entidades da trilha — os NOMES DE TABELA, e não os nomes em português das
// structs. Quem consulta `audit_log` está olhando o banco; `entity = 'units'`
// casa com a tabela que a pessoa vai abrir em seguida.
const (
	entidadePropriedade = "properties"
	entidadeProduto     = "unit_types"
	entidadeUnidade     = "units"
	entidadeComposicao  = "unit_type_members"
)

// Verbos próprios deste módulo, além dos três genéricos de `audit`.
//
// `desativado` e `reativado` existem porque a pergunta que a revisão fez —
// "quem desativou AP-03?" — precisa ser UMA consulta
// (`action = 'units.desativado'`), e não um `LIKE` no documento `after` atrás
// de `"active": false`. Como a desativação chega por três portas (DELETE, PATCH
// e PUT), as três gravam o MESMO verbo: a trilha responde pela decisão tomada,
// não pelo verbo HTTP que a carregou.
const (
	verboDesativado  = "desativado"
	verboReativado   = "reativado"
	verboSubstituida = "substituida"
)

// Trilha é a auditoria vista pelo service.
//
// Interface, e não a chamada direta a `audit.*`, por dois motivos:
//
//   - as funções de `audit` recebem o executor (`db.DBTX`) para poderem entrar
//     na transação do chamador, e o service deste módulo não conhece pool —
//     ele fala com `Repositorio`. A interface guarda o executor uma vez, na
//     montagem, e o service passa a auditar sem carregar um pgxpool no meio das
//     regras de negócio;
//   - o teste unitário consegue afirmar QUE a trilha foi gravada, com qual
//     verbo e sobre qual entidade, sem Postgres. Auditoria que ninguém testa é
//     a que some na primeira refatoração — foi exatamente o que aconteceu na
//     Fase 1, com 1626 tuplas de negócio e zero linhas em `audit_log`.
type Trilha interface {
	Criacao(ctx context.Context, entidade, verbo string, id uuid.UUID, depois any) error
	Alteracao(ctx context.Context, entidade, verbo string, id uuid.UUID, antes, depois any) error

	// Sem `Exclusao`: este módulo não exclui nada. O DELETE do contrato é
	// `active = false`, a linha continua na tabela, e o que a trilha precisa
	// guardar é a MUDANÇA — não um retrato completo de algo que ainda está lá.
	// Quando houver remoção física, o atalho existe em `audit.Exclusao` e entra
	// aqui junto com ela.
}

// trilhaDeAuditoria é a Trilha de produção: um repasse fino para
// `internal/platform/audit`, que resolve ator, propriedade, IP, user-agent e
// request_id a partir do contexto e grava na transação em curso.
type trilhaDeAuditoria struct{ exec db.DBTX }

// NovaTrilha monta a trilha sobre o pool. `audit.Registrar` troca o pool pela
// transação do contexto quando existe uma — e neste módulo existe sempre, porque
// toda escrita roda dentro de `tx.Do`. É isso que faz a trilha desaparecer junto
// com o negócio quando a operação é desfeita.
func NovaTrilha(exec db.DBTX) Trilha { return trilhaDeAuditoria{exec: exec} }

func (t trilhaDeAuditoria) Criacao(ctx context.Context, entidade, verbo string, id uuid.UUID, depois any) error {
	return audit.Criacao(ctx, t.exec, entidade, verbo, id, depois)
}

func (t trilhaDeAuditoria) Alteracao(ctx context.Context, entidade, verbo string, id uuid.UUID, antes, depois any) error {
	return audit.Alteracao(ctx, t.exec, entidade, verbo, id, antes, depois)
}

// verboDaTransicao escolhe o verbo pela mudança da coluna `active`.
//
// A ida e a volta têm nomes diferentes de propósito: são decisões comerciais
// distintas, com guardas distintas (ver service.go), e quem audita precisa
// separá-las sem ler o documento `after`.
func verboDaTransicao(antes, depois bool) string {
	switch {
	case antes && !depois:
		return verboDesativado
	case !antes && depois:
		return verboReativado
	default:
		return audit.VerboAlterado
	}
}

// composicaoAuditada é o retrato da composição para a trilha.
//
// Struct, e não a lista de `UnidadeDaComposicao` direto: `audit.Snapshot`
// serializa para um OBJETO, e uma lista nua cairia na chave genérica "valor".
// Os códigos vêm junto com os ids porque quem lê a auditoria seis meses depois
// precisa ver `AP-03`, não um uuid que talvez já nem exista mais.
type composicaoAuditada struct {
	Codigos []string    `json:"unit_codes"`
	IDs     []uuid.UUID `json:"unit_ids"`
}

func retratoDaComposicao(itens []UnidadeDaComposicao) composicaoAuditada {
	r := composicaoAuditada{
		Codigos: make([]string, 0, len(itens)),
		IDs:     make([]uuid.UUID, 0, len(itens)),
	}
	for _, item := range itens {
		r.Codigos = append(r.Codigos, item.Codigo)
		r.IDs = append(r.IDs, item.UnidadeID)
	}
	return r
}
