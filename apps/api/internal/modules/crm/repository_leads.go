package crm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/pii"
)

// condicoes monta o WHERE e os argumentos posicionais.
//
// Existe para que nenhum filtro entre na consulta por concatenação de texto: o
// que o cliente digitou vira sempre `$n`, e o SQL do arquivo é literal. É a
// mesma disciplina do `ParseSort` com whitelist, aplicada ao WHERE.
type condicoes struct {
	partes []string
	args   []any
}

// add acrescenta um predicado. CADA `%d` do fragmento recebe o número do MESMO
// argumento — é o que permite escrever a busca em três colunas com um valor só,
// sem passar o mesmo texto três vezes ao driver.
func (c *condicoes) add(fragmento string, valor any) {
	c.args = append(c.args, valor)

	posicoes := make([]any, strings.Count(fragmento, "%d"))
	for i := range posicoes {
		posicoes[i] = len(c.args)
	}
	c.partes = append(c.partes, fmt.Sprintf(fragmento, posicoes...))
}

// prox reserva o próximo número de argumento sem gerar predicado — é o LIMIT e
// o OFFSET, que não são filtro.
func (c *condicoes) prox(valor any) int {
	c.args = append(c.args, valor)
	return len(c.args)
}

func (c *condicoes) where() string {
	if len(c.partes) == 0 {
		return ""
	}
	return " AND " + strings.Join(c.partes, " AND ")
}

// ═══════════════════════════ Leads ══════════════════════════════════

// colunasDoLead é a projeção do schema `Lead`.
//
// `opportunity_id` é DERIVADO — a oportunidade que nasceu da conversão. Fica
// numa subconsulta e não numa coluna porque a origem verdadeira é
// `crm_opportunities.lead_id`: gravar o espelho aqui criaria duas fontes para o
// mesmo vínculo, que divergem no dia em que só uma for atualizada.
const colunasDoLead = `
	    l.id, l.contact_id, c.name, c.phone_e164,
	    l.source, l.campaign_id, l.status, l.score,
	    l.interest_unit_type_id, ut.name,
	    l.desired_check_in::text, l.desired_check_out::text, l.guests_count,
	    l.owner_id, u.name, l.converted_at,
	    (SELECT o.id FROM crm_opportunities o WHERE o.lead_id = l.id ORDER BY o.created_at LIMIT 1),
	    l.created_at, l.updated_at`

const juncoesDoLead = `
	  FROM crm_leads l
	  JOIN contacts c ON c.id = l.contact_id
	  LEFT JOIN unit_types ut ON ut.id = l.interest_unit_type_id
	  LEFT JOIN users u ON u.id = l.owner_id`

// lerLead é a ÚNICA porta de linha para `Lead` — lista, detalhe e a resposta
// das escritas passam por aqui. Por isso o telefone é mascarado aqui, e não em
// cada service: o contrato manda `contact_phone_e164` SEMPRE mascarado
// (`+*********0000`), e uma máscara por rota é a que alguém esquece na rota
// nova. O lead guarda o interesse, não a pessoa; o número cheio mora na ficha
// (`GET /contacts/{id}`), que registra quem leu. Nenhum código do CRM usa o
// número cheio do lead — a busca `q` compara o valor cheio no SQL, antes.
func lerLead(linha pgx.Row) (Lead, error) {
	var (
		l    Lead
		fone *string
	)
	err := linha.Scan(&l.ID, &l.ContactID, &l.ContatoNome, &fone,
		&l.Origem, &l.CampanhaID, &l.Status, &l.Score,
		&l.ProdutoID, &l.ProdutoNome,
		&l.CheckIn, &l.CheckOut, &l.Hospedes,
		&l.DonoID, &l.DonoNome, &l.ConvertidoEm,
		&l.OportunidadeID, &l.CriadoEm, &l.AtualizadoEm)
	l.ContatoFone = pii.MascararTelefone(fone)
	return l, err
}

// ListarLeads devolve a página e o total.
//
// O escopo `own` vira `AND l.owner_id = $n` AQUI, no SQL. Peneirar em memória
// faria o `total` da paginação contar o que o requisitante não pode ver — e o
// número de leads da casa vazaria pelo rodapé da lista dele.
func (r *Repository) ListarLeads(ctx context.Context, propriedade uuid.UUID, f FiltroDeLeads) ([]Lead, int64, error) {
	c := condicoes{args: []any{propriedade}}

	if f.SomenteMinhas {
		c.add("l.owner_id = $%d", f.Usuario)
	} else if f.DonoID != nil {
		c.add("l.owner_id = $%d", *f.DonoID)
	}
	if len(f.Status) > 0 {
		c.add("l.status = ANY($%d::text[])", f.Status)
	}
	if f.Origem != "" {
		c.add("l.source = $%d", f.Origem)
	}
	if f.ContactID != nil {
		c.add("l.contact_id = $%d", *f.ContactID)
	}
	if f.Busca != "" {
		c.add("(c.name ILIKE '%%' || $%d || '%%' OR c.email ILIKE '%%' || $%d || '%%' OR c.phone_e164 ILIKE '%%' || $%d || '%%')",
			f.Busca)
	}
	if f.De != "" {
		c.add("l.created_at >= $%d::date", f.De)
	}
	if f.Ate != "" {
		// Meia-aberta no fim: `to` inclui o dia inteiro. Comparar `<= $to::date`
		// cortaria tudo o que entrou depois da meia-noite do último dia.
		c.add("l.created_at < ($%d::date + 1)", f.Ate)
	}

	base := juncoesDoLead + ` WHERE l.property_id = $1` + c.where()

	var total int64
	if err := r.exec(ctx).QueryRow(ctx, `SELECT count(*) `+base, c.args...).Scan(&total); err != nil {
		return nil, 0, db.MapError(err)
	}

	limite, deslocamento := c.prox(f.PorPagina), c.prox((f.Pagina-1)*f.PorPagina)
	q := fmt.Sprintf(`SELECT %s %s ORDER BY %s, l.id LIMIT $%d OFFSET $%d`,
		colunasDoLead, base, f.OrderBy, limite, deslocamento)

	linhas, err := r.exec(ctx).Query(ctx, q, c.args...)
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	out := []Lead{}
	for linhas.Next() {
		l, err := lerLead(linhas)
		if err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, l)
	}
	return out, total, db.MapError(linhas.Err())
}

// BuscarLead devolve um lead.
//
// Fora do escopo é 404, nunca 403: responder 403 confirmaria que o lead existe
// — o mesmo oráculo de enumeração que o `INVALID_CREDENTIALS` do login evita.
func (r *Repository) BuscarLead(ctx context.Context, propriedade, id uuid.UUID, somenteMinhas bool, usuario uuid.UUID) (Lead, error) {
	q := `SELECT ` + colunasDoLead + juncoesDoLead + `
	       WHERE l.property_id = $1 AND l.id = $2
	         AND (NOT $3::boolean OR l.owner_id = $4)`
	l, err := lerLead(r.exec(ctx).QueryRow(ctx, q, propriedade, id, somenteMinhas, usuario))
	if errors.Is(err, pgx.ErrNoRows) {
		return Lead{}, apperr.NotFound("Lead")
	}
	return l, db.MapError(err)
}

// LeadGravavel é o que o service monta para inserir ou atualizar.
type LeadGravavel struct {
	ContactID   uuid.UUID
	Origem      string
	CampanhaID  *uuid.UUID
	Status      string
	Score       int
	ProdutoID   *uuid.UUID
	CheckIn     *string
	CheckOut    *string
	Hospedes    *int
	DonoID      *uuid.UUID
	Observacoes *string
}

// TravarLead lê o lead FOR UPDATE, já com o recorte de escopo.
func (r *Repository) TravarLead(ctx context.Context, propriedade, id uuid.UUID, somenteMinhas bool, usuario uuid.UUID) (LeadGravavel, string, error) {
	const q = `
		SELECT l.contact_id, l.source, l.campaign_id, l.status, l.score,
		       l.interest_unit_type_id, l.desired_check_in::text, l.desired_check_out::text,
		       l.guests_count, l.owner_id, l.notes,
		       (SELECT o.id::text FROM crm_opportunities o WHERE o.lead_id = l.id ORDER BY o.created_at LIMIT 1)
		  FROM crm_leads l
		 WHERE l.property_id = $1 AND l.id = $2
		   AND (NOT $3::boolean OR l.owner_id = $4)
		   FOR UPDATE OF l`

	var (
		g            LeadGravavel
		oportunidade *string
	)
	err := r.exec(ctx).QueryRow(ctx, q, propriedade, id, somenteMinhas, usuario).
		Scan(&g.ContactID, &g.Origem, &g.CampanhaID, &g.Status, &g.Score,
			&g.ProdutoID, &g.CheckIn, &g.CheckOut, &g.Hospedes, &g.DonoID, &g.Observacoes, &oportunidade)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, "", apperr.NotFound("Lead")
	}
	if err != nil {
		return g, "", db.MapError(err)
	}
	if oportunidade == nil {
		return g, "", nil
	}
	return g, *oportunidade, nil
}

// InserirLead grava o lead novo.
func (r *Repository) InserirLead(ctx context.Context, propriedade uuid.UUID, g LeadGravavel, criador uuid.UUID) (uuid.UUID, error) {
	const q = `
		INSERT INTO crm_leads (property_id, contact_id, source, campaign_id, status, score,
		                       interest_unit_type_id, desired_check_in, desired_check_out,
		                       guests_count, owner_id, notes, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::date,$9::date,$10,$11,$12,$13) RETURNING id`

	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, propriedade, g.ContactID, g.Origem, g.CampanhaID, g.Status, g.Score,
		g.ProdutoID, g.CheckIn, g.CheckOut, g.Hospedes, g.DonoID, g.Observacoes, criador).Scan(&id)
	return id, db.MapError(err)
}

// AtualizarLead grava os campos editáveis. `status` e `converted_at` de
// conversão NÃO passam por aqui — quem converte é ConverterLead.
func (r *Repository) AtualizarLead(ctx context.Context, id uuid.UUID, g LeadGravavel) error {
	const q = `
		UPDATE crm_leads
		   SET contact_id = $2, source = $3, campaign_id = $4, status = $5, score = $6,
		       interest_unit_type_id = $7, desired_check_in = $8::date, desired_check_out = $9::date,
		       guests_count = $10, owner_id = $11, notes = $12, updated_at = now()
		 WHERE id = $1`
	_, err := r.exec(ctx).Exec(ctx, q, id, g.ContactID, g.Origem, g.CampanhaID, g.Status, g.Score,
		g.ProdutoID, g.CheckIn, g.CheckOut, g.Hospedes, g.DonoID, g.Observacoes)
	return db.MapError(err)
}

// MarcarLeadConvertido fecha o lado do lead na conversão. `status` e
// `converted_at` andam juntos porque o CHECK `crm_leads_convertido` exige — e o
// CHECK existe para que "convertido sem data" não contamine o funil de origem.
func (r *Repository) MarcarLeadConvertido(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE crm_leads SET status = 'convertido', converted_at = now(), updated_at = now()
	            WHERE id = $1`
	_, err := r.exec(ctx).Exec(ctx, q, id)
	return db.MapError(err)
}

// ExcluirLead remove fisicamente. Só chega aqui o lead que NÃO virou
// oportunidade — a guarda é do service, porque a resposta é 409 com vocabulário
// de negócio, não a violação de FK crua.
func (r *Repository) ExcluirLead(ctx context.Context, id uuid.UUID) error {
	// As atividades do lead caem por `ON DELETE CASCADE`; a linha some inteira.
	tag, err := r.exec(ctx).Exec(ctx, `DELETE FROM crm_leads WHERE id = $1`, id)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Lead")
	}
	return nil
}
