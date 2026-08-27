package crm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// colunasDaAtividade é a projeção do schema `Atividade`.
//
// `overdue` sai do BANCO comparado contra `now()`, e não do relógio do
// navegador: "vencida" é a mesma palavra para todo mundo que abre a tela, em
// qualquer fuso. Nota nunca vence — ela não tem prazo, e o predicado a exclui
// explicitamente para que um `due_at` gravado por engano não acenda alerta.
const colunasDaAtividade = `
	    a.id, a.type, a.subject, a.description, a.due_at, a.done_at, a.status,
	    (a.status = 'pendente' AND a.type <> 'nota' AND a.due_at IS NOT NULL AND a.due_at < now()),
	    a.priority, a.lead_id, a.opportunity_id, a.contact_id, c.name,
	    a.owner_id, u.name, a.stage_id, a.auto, a.created_at, a.updated_at`

const juncoesDaAtividade = `
	  FROM crm_activities a
	  LEFT JOIN contacts c ON c.id = a.contact_id
	  LEFT JOIN users u    ON u.id = a.owner_id`

func lerAtividade(linha pgx.Row) (Atividade, error) {
	var a Atividade
	err := linha.Scan(&a.ID, &a.Tipo, &a.Assunto, &a.Descricao, &a.VenceEm, &a.ConcluidaEm, &a.Status,
		&a.Vencida, &a.Prioridade, &a.LeadID, &a.OportunidadeID, &a.ContactID, &a.ContatoNome,
		&a.DonoID, &a.DonoNome, &a.EtapaID, &a.Auto, &a.CriadoEm, &a.AtualizadoEm)
	return a, err
}

// ListarAtividades devolve a página e o total.
func (r *Repository) ListarAtividades(ctx context.Context, propriedade uuid.UUID, f FiltroDeAtividades) ([]Atividade, int64, error) {
	c := condicoes{args: []any{propriedade}}

	if f.SomenteMinhas {
		c.add("a.owner_id = $%d", f.Usuario)
	} else if f.DonoID != nil {
		c.add("a.owner_id = $%d", *f.DonoID)
	}
	if len(f.Tipos) > 0 {
		c.add("a.type = ANY($%d::text[])", f.Tipos)
	}
	if len(f.Status) > 0 {
		c.add("a.status = ANY($%d::text[])", f.Status)
	}
	if f.OportunidadeID != nil {
		c.add("a.opportunity_id = $%d", *f.OportunidadeID)
	}
	if f.LeadID != nil {
		c.add("a.lead_id = $%d", *f.LeadID)
	}
	if f.ContactID != nil {
		c.add("a.contact_id = $%d", *f.ContactID)
	}
	if f.Auto != nil {
		c.add("a.auto = $%d", *f.Auto)
	}
	if f.VenceDe != "" {
		c.add("a.due_at >= $%d::date", f.VenceDe)
	}
	if f.VenceAte != "" {
		c.add("a.due_at < ($%d::date + 1)", f.VenceAte)
	}
	if f.Vencidas != nil {
		if *f.Vencidas {
			c.partes = append(c.partes, "(a.status = 'pendente' AND a.due_at IS NOT NULL AND a.due_at < now())")
		} else {
			c.partes = append(c.partes, "NOT (a.status = 'pendente' AND a.due_at IS NOT NULL AND a.due_at < now())")
		}
	}

	base := juncoesDaAtividade + ` WHERE a.property_id = $1` + c.where()

	var total int64
	if err := r.exec(ctx).QueryRow(ctx, `SELECT count(*) `+base, c.args...).Scan(&total); err != nil {
		return nil, 0, db.MapError(err)
	}

	limite, deslocamento := c.prox(f.PorPagina), c.prox((f.Pagina-1)*f.PorPagina)
	q := fmt.Sprintf(`SELECT %s %s ORDER BY %s, a.id LIMIT $%d OFFSET $%d`,
		colunasDaAtividade, base, f.OrderBy, limite, deslocamento)

	linhas, err := r.exec(ctx).Query(ctx, q, c.args...)
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	out := []Atividade{}
	for linhas.Next() {
		a, err := lerAtividade(linhas)
		if err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, a)
	}
	return out, total, db.MapError(linhas.Err())
}

// BuscarAtividade devolve uma atividade. Fora do escopo é 404, nunca 403.
func (r *Repository) BuscarAtividade(ctx context.Context, propriedade, id uuid.UUID, somenteMinhas bool, usuario uuid.UUID) (Atividade, error) {
	q := `SELECT ` + colunasDaAtividade + juncoesDaAtividade + `
	       WHERE a.property_id = $1 AND a.id = $2
	         AND (NOT $3::boolean OR a.owner_id = $4)`
	a, err := lerAtividade(r.exec(ctx).QueryRow(ctx, q, propriedade, id, somenteMinhas, usuario))
	if errors.Is(err, pgx.ErrNoRows) {
		return Atividade{}, apperr.NotFound("Atividade")
	}
	return a, db.MapError(err)
}

// AtividadesDaOportunidade devolve TUDO — tarefas e notas — numa consulta só.
//
// A separação em `activities` e `notes` acontece em Go, e não em duas
// consultas: as duas listas saem da mesma tabela e do mesmo recorte, e duas
// viagens ao banco para o mesmo `opportunity_id` seriam a segunda metade de um
// N+1 disfarçado de organização.
//
// Pendentes primeiro, por prazo — é a ordem da tela.
func (r *Repository) AtividadesDaOportunidade(ctx context.Context, oportunidade uuid.UUID) ([]Atividade, error) {
	q := `SELECT ` + colunasDaAtividade + juncoesDaAtividade + `
	       WHERE a.opportunity_id = $1
	       ORDER BY (a.status = 'pendente') DESC, a.due_at ASC NULLS LAST, a.created_at DESC`

	linhas, err := r.exec(ctx).Query(ctx, q, oportunidade)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []Atividade{}
	for linhas.Next() {
		a, err := lerAtividade(linhas)
		if err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, a)
	}
	return out, db.MapError(linhas.Err())
}

// AtividadeGravavel é o que o service monta para inserir.
type AtividadeGravavel struct {
	Tipo           string
	Assunto        string
	Descricao      *string
	VenceEm        *time.Time
	Prioridade     string
	LeadID         *uuid.UUID
	OportunidadeID *uuid.UUID
	ContactID      *uuid.UUID
	EtapaID        *uuid.UUID
	DonoID         uuid.UUID
	Auto           bool
}

// InserirAtividade grava a atividade.
func (r *Repository) InserirAtividade(ctx context.Context, propriedade uuid.UUID, g AtividadeGravavel, criador uuid.UUID) (uuid.UUID, error) {
	const q = `
		INSERT INTO crm_activities (property_id, type, subject, description, due_at, priority,
		                            lead_id, opportunity_id, contact_id, stage_id, owner_id,
		                            auto, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`

	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, propriedade, g.Tipo, g.Assunto, g.Descricao, g.VenceEm, g.Prioridade,
		g.LeadID, g.OportunidadeID, g.ContactID, g.EtapaID, g.DonoID, g.Auto, criador).Scan(&id)
	return id, db.MapError(err)
}

// CriarTarefaAutomatica grava a tarefa da etapa, IDEMPOTENTE.
//
// ═══ A idempotência é do BANCO, não deste código ═══
//
// O índice parcial `crm_activities_auto_idx` é
// `UNIQUE (opportunity_id, stage_id) WHERE auto AND status = 'pendente'`. Aqui
// o `ON CONFLICT DO NOTHING` só COLHE o que ele decidiu.
//
// Escrever isto como `SELECT ... IF NOT EXISTS THEN INSERT` seria o mesmo TOCTOU
// que a regra 2 do CLAUDE.md proíbe para o overbooking, um nível acima: dois
// cliques no mesmo card, duas abas abertas ou o POST que o navegador repetiu, e
// o corretor abre a manhã com a mesma ligação duas vezes na lista.
//
// O predicado tem `status = 'pendente'` de propósito: voltar à etapa DEPOIS de a
// tarefa ter sido concluída cria outra, porque é um ciclo de SLA novo e não uma
// duplicata. O que não pode existir são duas pendentes ao mesmo tempo.
//
// Devolve (nil, nil) quando não criou — e é essa a informação que vira
// `auto_task: null` na resposta: a tela precisa saber que a idempotência agiu,
// não receber a tarefa antiga como se fosse nova.
func (r *Repository) CriarTarefaAutomatica(ctx context.Context, propriedade uuid.UUID, g AtividadeGravavel, criador uuid.UUID) (*uuid.UUID, error) {
	const q = `
		INSERT INTO crm_activities (property_id, type, subject, description, due_at, priority,
		                            opportunity_id, contact_id, stage_id, owner_id, auto, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,true,$11)
		ON CONFLICT (opportunity_id, stage_id) WHERE auto AND status = 'pendente'
		DO NOTHING
		RETURNING id`

	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, propriedade, g.Tipo, g.Assunto, g.Descricao, g.VenceEm, g.Prioridade,
		g.OportunidadeID, g.ContactID, g.EtapaID, g.DonoID, criador).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// Já havia pendente da mesma etapa: o `DO NOTHING` não devolveu linha.
		return nil, nil
	}
	if err != nil {
		return nil, db.MapError(err)
	}
	return &id, nil
}

// AtividadeTravada é o estado mínimo que as ações precisam ler sob lock.
type AtividadeTravada struct {
	ID             uuid.UUID
	Tipo           string
	Status         string
	Descricao      *string
	ConcluidaEm    *time.Time
	DonoID         uuid.UUID
	LeadID         *uuid.UUID
	OportunidadeID *uuid.UUID
	ContactID      *uuid.UUID
}

// TravarAtividade lê a atividade FOR UPDATE, já com o recorte de escopo.
func (r *Repository) TravarAtividade(ctx context.Context, propriedade, id uuid.UUID, somenteMinhas bool, usuario uuid.UUID) (AtividadeTravada, error) {
	const q = `
		SELECT a.id, a.type, a.status, a.description, a.done_at, a.owner_id,
		       a.lead_id, a.opportunity_id, a.contact_id
		  FROM crm_activities a
		 WHERE a.property_id = $1 AND a.id = $2
		   AND (NOT $3::boolean OR a.owner_id = $4)
		   FOR UPDATE OF a`

	var t AtividadeTravada
	err := r.exec(ctx).QueryRow(ctx, q, propriedade, id, somenteMinhas, usuario).
		Scan(&t.ID, &t.Tipo, &t.Status, &t.Descricao, &t.ConcluidaEm, &t.DonoID,
			&t.LeadID, &t.OportunidadeID, &t.ContactID)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, apperr.NotFound("Atividade")
	}
	return t, db.MapError(err)
}

// AtualizarAtividade grava os campos editáveis. `done_at` NÃO passa por aqui:
// concluir é `/complete`, com o relógio do servidor.
func (r *Repository) AtualizarAtividade(ctx context.Context, id uuid.UUID, tipo, assunto string,
	descricao *string, venceEm *time.Time, prioridade string, dono uuid.UUID, status string) error {

	const q = `
		UPDATE crm_activities
		   SET type = $2, subject = $3, description = $4, due_at = $5, priority = $6,
		       owner_id = $7, status = $8, updated_at = now()
		 WHERE id = $1`
	_, err := r.exec(ctx).Exec(ctx, q, id, tipo, assunto, descricao, venceEm, prioridade, dono, status)
	return db.MapError(err)
}

// ConcluirAtividade carimba `status` e `done_at` com o relógio do SERVIDOR.
//
// Idempotente por construção: o `WHERE status = 'pendente'` faz a segunda
// chamada não afetar linha nenhuma, e o `done_at` original fica de pé. O duplo
// clique no botão "Concluir" é o gesto mais comum da tela, e mover `done_at`
// para a frente falsearia o tempo de resposta do §15.
func (r *Repository) ConcluirAtividade(ctx context.Context, id uuid.UUID, nota *string) error {
	const q = `
		UPDATE crm_activities
		   SET status = 'concluida', done_at = now(), updated_at = now(),
		       description = CASE WHEN $2::text IS NULL THEN description
		                          WHEN description IS NULL OR btrim(description) = '' THEN $2::text
		                          ELSE description || E'\n\n' || $2::text END
		 WHERE id = $1 AND status = 'pendente'`
	_, err := r.exec(ctx).Exec(ctx, q, id, nota)
	return db.MapError(err)
}

// ConcluirTarefasAutomaticas fecha as tarefas automáticas pendentes da
// oportunidade. É o passo 5 do `/win`: cobrar follow-up de negócio fechado é
// ruído que ensina a operação a ignorar o alerta — e o alerta ignorado é o
// mesmo que alerta inexistente.
func (r *Repository) ConcluirTarefasAutomaticas(ctx context.Context, oportunidade uuid.UUID) error {
	const q = `UPDATE crm_activities SET status = 'concluida', done_at = now(), updated_at = now()
	            WHERE opportunity_id = $1 AND auto AND status = 'pendente'`
	_, err := r.exec(ctx).Exec(ctx, q, oportunidade)
	return db.MapError(err)
}

// CancelarTarefasAutomaticas é o equivalente do `/lose`.
//
// CANCELADA, e não concluída: a tarefa não foi feita, o negócio acabou. Marcar
// como concluída inflaria a taxa de conclusão de tarefas do §15 com trabalho que
// ninguém executou.
func (r *Repository) CancelarTarefasAutomaticas(ctx context.Context, oportunidade uuid.UUID) error {
	const q = `UPDATE crm_activities SET status = 'cancelada', updated_at = now()
	            WHERE opportunity_id = $1 AND auto AND status = 'pendente'`
	_, err := r.exec(ctx).Exec(ctx, q, oportunidade)
	return db.MapError(err)
}

// ExcluirAtividade remove fisicamente.
//
// Apagar a tarefa automática pendente é permitido — o operador que resolveu por
// fora não deve ficar com um alerta eterno na tela — e ela NÃO renasce sozinha:
// a criação só acontece numa nova entrada na etapa.
func (r *Repository) ExcluirAtividade(ctx context.Context, id uuid.UUID) error {
	tag, err := r.exec(ctx).Exec(ctx, `DELETE FROM crm_activities WHERE id = $1`, id)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Atividade")
	}
	return nil
}

// ConferirLead e ConferirOportunidade recusam vínculo de outra propriedade —
// pela mesma razão de ConferirContato: a FK não sabe de propriedade.
func (r *Repository) ConferirLead(ctx context.Context, propriedade, id uuid.UUID) error {
	return r.existe(ctx, `SELECT 1 FROM crm_leads WHERE id = $1 AND property_id = $2`, id, propriedade, "Lead")
}

func (r *Repository) ConferirOportunidade(ctx context.Context, propriedade, id uuid.UUID) error {
	return r.existe(ctx, `SELECT 1 FROM crm_opportunities WHERE id = $1 AND property_id = $2`, id, propriedade, "Oportunidade")
}
