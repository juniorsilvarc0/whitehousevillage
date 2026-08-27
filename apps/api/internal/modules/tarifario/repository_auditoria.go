package tarifario

import (
	"context"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
)

// A trilha de `audit_log` do tarifário (spec §16, ALTO 3 desta revisão).
//
// Medido nesta árvore antes da correção, contra Postgres real com o seed
// aplicado: 66 linhas inseridas e 19 removidas nas tabelas do tarifário
// (`rates`, `rate_tables`, `holidays`, `special_periods`, `min_nights_rules`,
// `commercial_policies`, `cancellation_policies`, `cancellation_tiers`) e ZERO
// linhas em `audit_log`. "Quem triplicou a tarifa?" não tinha resposta.
//
// # Por que os atalhos passam pelo repositório
//
// Quem decide auditar é o service — é ele que sabe o que a operação significa e
// que já tem o `antes` em mãos. Mas `audit.Registrar` precisa de um `db.DBTX`, e
// o service não conhece pool nenhum: ele fala com a interface `Repositorio`.
// Três delegadores de uma linha resolvem sem furar a camada, e têm um efeito
// colateral que vale por si — o repositório falso dos testes de unidade
// implementa os mesmos três métodos e passa a poder AFIRMAR que o service
// auditou, sem subir banco.
//
// O executor entregue é `r.pool`; `audit.Registrar` o troca pela transação do
// contexto quando existe uma, que é sempre o caso aqui (todo caminho de escrita
// do service roda dentro de `tx.Do`). É isso que faz a trilha ser desfeita junto
// com o negócio quando a transação aborta: não existe tarifa auditada que não
// foi gravada.

func (r *Repository) AuditarCriacao(ctx context.Context, entidade, verbo string, id uuid.UUID, depois any) error {
	return audit.Criacao(ctx, r.pool, entidade, verbo, id, depois)
}

func (r *Repository) AuditarAlteracao(ctx context.Context, entidade, verbo string, id uuid.UUID, antes, depois any) error {
	return audit.Alteracao(ctx, r.pool, entidade, verbo, id, antes, depois)
}

func (r *Repository) AuditarExclusao(ctx context.Context, entidade, verbo string, id uuid.UUID, antes any) error {
	return audit.Exclusao(ctx, r.pool, entidade, verbo, id, antes)
}

// AuditarEvento é a saída para quando os três atalhos não servem: a grade do
// bulk, cujo `antes`/`depois` não é o retrato de uma struct, e sim o recorte
// `codigo.tipo → centavos` montado por planejarGrade.
func (r *Repository) AuditarEvento(ctx context.Context, ev audit.Evento) error {
	return audit.Registrar(ctx, r.pool, ev)
}
