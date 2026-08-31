package crm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// colunasDaOportunidade é a projeção do schema `Oportunidade`.
//
// `sla_due_at` sai do BANCO (`entered_stage_at + sla_days`) e não de uma coluna
// gravada: coluna gravada precisaria de um job para envelhecer sozinha, e
// envelheceria errado no dia em que o job falhasse. `sla_breached` é comparado
// em Go contra o `now()` do banco, lido uma vez por requisição — assim toda
// linha da mesma resposta é julgada pelo mesmo instante.
//
// `pending_task_count` exclui `nota`: nota não é trabalho pendente de ninguém,
// e contá-la faria a faixa vermelha do card acender por um registro de texto.
//
// `quote_id` é o ORÇAMENTO VIGENTE, e sai de `quotes` — não da coluna
// `crm_opportunities.quote_id`. A coluna existe e ainda referencia
// `reservations(id)`: ela nasceu quando orçamento era reserva em `quote`, e a
// migration que criou a tabela `quotes` (20260827130000) NÃO repontou a FK, de
// propósito, para não quebrar o CRM que está no ar no meio da rodada. Gravar o
// id de um orçamento nela é `23503` na cara. A subconsulta responde o MESMO
// fato sem a coluna: "emitir outro substitui este" é literalmente
// `ORDER BY created_at DESC LIMIT 1`, e `quotes_oportunidade_idx` a cobre.
//
// PARA O INTEGRADOR: quando a FK for repontada, esta subconsulta pode voltar a
// ser `o.quote_id` — mas não precisa. Ver o relatório.
const colunasDaOportunidade = `
	    o.id, o.contact_id, c.name,
	    o.lead_id, o.pipeline_id, p.name, o.stage_id, s.name, s.type,
	    o.unit_type_id, ut.name,
	    o.check_in::text, o.check_out::text,
	    (SELECT q.id FROM quotes q
	      WHERE q.opportunity_id = o.id
	      ORDER BY q.created_at DESC, q.id DESC LIMIT 1),
	    o.reservation_id, res.code,
	    o.amount_cents, o.probability, o.expected_close::text,
	    o.owner_id, u.name,
	    o.status, o.lost_reason_id, lr.label,
	    o.entered_stage_at, s.sla_days,
	    CASE WHEN s.sla_days IS NULL THEN NULL
	         ELSE o.entered_stage_at + make_interval(days => s.sla_days) END,
	    (SELECT count(*) FROM crm_activities a
	      WHERE a.opportunity_id = o.id AND a.status = 'pendente' AND a.type <> 'nota'),
	    ev.event_type, ev.guests_expected, ev.needs_catering, ev.notes,
	    o.created_by, o.created_at, o.updated_at`

const juncoesDaOportunidade = `
	  FROM crm_opportunities o
	  JOIN contacts c      ON c.id = o.contact_id
	  JOIN crm_pipelines p ON p.id = o.pipeline_id
	  JOIN crm_stages s    ON s.id = o.stage_id
	  LEFT JOIN unit_types ut       ON ut.id = o.unit_type_id
	  LEFT JOIN users u             ON u.id = o.owner_id
	  LEFT JOIN reservations res    ON res.id = o.reservation_id
	  LEFT JOIN crm_lost_reasons lr ON lr.id = o.lost_reason_id
	  LEFT JOIN crm_opportunity_event_details ev ON ev.opportunity_id = o.id`

func lerOportunidade(linha pgx.Row) (Oportunidade, error) {
	var (
		o             Oportunidade
		probabilidade float64
		evTipo        *string
		evConvidados  *int
		evBuffet      *bool
		evNotas       *string
	)
	err := linha.Scan(&o.ID, &o.ContactID, &o.ContatoNome,
		&o.LeadID, &o.FunilID, &o.FunilNome, &o.EtapaID, &o.EtapaNome, &o.EtapaTipo,
		&o.ProdutoID, &o.ProdutoNome,
		&o.CheckIn, &o.CheckOut,
		&o.OrcamentoID, &o.ReservaID, &o.ReservaCodigo,
		&o.Valor, &probabilidade, &o.FechamentoPrev,
		&o.DonoID, &o.DonoNome,
		&o.Status, &o.MotivoID, &o.MotivoRotulo,
		&o.EntrouNaEtapaEm, &o.SLADias, &o.SLAVenceEm,
		&o.TarefasPendentes,
		&evTipo, &evConvidados, &evBuffet, &evNotas,
		&o.CriadoPor, &o.CriadoEm, &o.AtualizadoEm)
	if err != nil {
		return o, err
	}

	o.Probabilidade = int(math.Round(probabilidade))
	o.Status = EstadoParaContrato(o.Status)

	// O satélite é 1:1 e opcional: o LEFT JOIN traz tudo nulo quando não é
	// evento, e `needs_catering` (NOT NULL na tabela) é o marcador confiável de
	// "a linha existe" — `event_type` pode ser nulo numa linha que existe.
	if evBuffet != nil {
		o.Evento = &DetalheDeEvento{
			TipoDeEvento: evTipo, ConvidadosPrev: evConvidados,
			PrecisaBuffet: *evBuffet, Observacoes: evNotas,
		}
	}
	return o, nil
}

// filtrosDaOportunidade traduz o recorte em predicados. É compartilhado entre a
// listagem e o kanban para que os dois concordem sobre o que "minhas
// oportunidades" significa.
func filtrosDaOportunidade(c *condicoes, f FiltroDeOportunidades) {
	if f.SomenteMinhas {
		// O filtro `owner_id` da query NÃO amplia nada: quem tem escopo `own` só
		// consegue estreitar o que já é dele, e por isso o predicado do escopo
		// vem primeiro e o da query é ignorado.
		c.add("o.owner_id = $%d", f.Usuario)
	} else if f.DonoID != nil {
		c.add("o.owner_id = $%d", *f.DonoID)
	}
	if f.FunilID != nil {
		c.add("o.pipeline_id = $%d", *f.FunilID)
	}
	if f.EtapaID != nil {
		c.add("o.stage_id = $%d", *f.EtapaID)
	}
	if len(f.Status) > 0 {
		c.add("o.status = ANY($%d::text[])", f.Status)
	}
	if f.ContactID != nil {
		c.add("o.contact_id = $%d", *f.ContactID)
	}
	if f.ProdutoID != nil {
		c.add("o.unit_type_id = $%d", *f.ProdutoID)
	}
	if f.MotivoID != nil {
		c.add("o.lost_reason_id = $%d", *f.MotivoID)
	}
	if f.Busca != "" {
		// O contrato fala em "título da oportunidade ou nome do contato". O
		// título é derivado de contato + produto + datas (não há campo `title`
		// no contrato), então a busca cobre as MESMAS partes: contato e produto.
		c.add("(c.name ILIKE '%%' || $%d || '%%' OR ut.name ILIKE '%%' || $%d || '%%')", f.Busca)
	}
	if f.FechamentoDe != "" {
		c.add("o.expected_close >= $%d::date", f.FechamentoDe)
	}
	if f.FechamentoAte != "" {
		c.add("o.expected_close <= $%d::date", f.FechamentoAte)
	}
	switch f.SLA {
	case "estourado":
		// Derivado no SERVIDOR, com o relógio do banco. A mesma conta no front
		// daria uma resposta por fuso do navegador de quem abriu a tela.
		c.partes = append(c.partes,
			"(o.status = 'aberto' AND s.sla_days IS NOT NULL AND o.entered_stage_at + make_interval(days => s.sla_days) < now())")
	case "no_prazo":
		c.partes = append(c.partes,
			"(o.status = 'aberto' AND (s.sla_days IS NULL OR o.entered_stage_at + make_interval(days => s.sla_days) >= now()))")
	}
}

// ListarOportunidades devolve a página e o total.
func (r *Repository) ListarOportunidades(ctx context.Context, propriedade uuid.UUID, f FiltroDeOportunidades) ([]Oportunidade, int64, error) {
	c := condicoes{args: []any{propriedade}}
	filtrosDaOportunidade(&c, f)

	base := juncoesDaOportunidade + ` WHERE o.property_id = $1` + c.where()

	var total int64
	if err := r.exec(ctx).QueryRow(ctx, `SELECT count(*) `+base, c.args...).Scan(&total); err != nil {
		return nil, 0, db.MapError(err)
	}

	limite, deslocamento := c.prox(f.PorPagina), c.prox((f.Pagina-1)*f.PorPagina)
	q := fmt.Sprintf(`SELECT %s %s ORDER BY %s, o.id LIMIT $%d OFFSET $%d`,
		colunasDaOportunidade, base, f.OrderBy, limite, deslocamento)

	linhas, err := r.exec(ctx).Query(ctx, q, c.args...)
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	out := []Oportunidade{}
	for linhas.Next() {
		o, err := lerOportunidade(linhas)
		if err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, o)
	}
	return out, total, db.MapError(linhas.Err())
}

// BuscarOportunidade devolve uma oportunidade. Fora do escopo é 404, nunca 403.
func (r *Repository) BuscarOportunidade(ctx context.Context, propriedade, id uuid.UUID, somenteMinhas bool, usuario uuid.UUID) (Oportunidade, error) {
	q := `SELECT ` + colunasDaOportunidade + juncoesDaOportunidade + `
	       WHERE o.property_id = $1 AND o.id = $2
	         AND (NOT $3::boolean OR o.owner_id = $4)`
	o, err := lerOportunidade(r.exec(ctx).QueryRow(ctx, q, propriedade, id, somenteMinhas, usuario))
	if errors.Is(err, pgx.ErrNoRows) {
		return Oportunidade{}, apperr.NotFound("Oportunidade")
	}
	return o, db.MapError(err)
}

// OportunidadeTravada é o estado mínimo que as ações precisam ler sob lock.
type OportunidadeTravada struct {
	ID          uuid.UUID
	FunilID     uuid.UUID
	EtapaID     uuid.UUID
	EtapaTipo   string
	Status      string
	ContactID   uuid.UUID
	ProdutoID   *uuid.UUID
	CheckIn     *string
	CheckOut    *string
	Hospedes    *int
	OrcamentoID *uuid.UUID
	ReservaID   *uuid.UUID
	DonoID      *uuid.UUID
	Valor       int64
	LeadID      *uuid.UUID
}

// TravarOportunidade lê o card FOR UPDATE, já com o recorte de escopo.
//
// O lock é o que serializa dois arrastos do mesmo card: sem ele, duas
// requisições leriam a mesma etapa atual, as duas passariam pela guarda do
// `from_stage_id` e as duas gravariam histórico — o quadro terminaria com duas
// linhas dizendo que o card saiu do mesmo lugar duas vezes.
func (r *Repository) TravarOportunidade(ctx context.Context, propriedade, id uuid.UUID, somenteMinhas bool, usuario uuid.UUID) (OportunidadeTravada, error) {
	const q = `
		SELECT o.id, o.pipeline_id, o.stage_id, s.type, o.status, o.contact_id,
		       o.unit_type_id, o.check_in::text, o.check_out::text, o.guests_count,
		       (SELECT q.id FROM quotes q
		         WHERE q.opportunity_id = o.id
		         ORDER BY q.created_at DESC, q.id DESC LIMIT 1),
		       o.reservation_id, o.owner_id, o.amount_cents, o.lead_id
		  FROM crm_opportunities o
		  JOIN crm_stages s ON s.id = o.stage_id
		 WHERE o.property_id = $1 AND o.id = $2
		   AND (NOT $3::boolean OR o.owner_id = $4)
		   FOR UPDATE OF o`

	var t OportunidadeTravada
	err := r.exec(ctx).QueryRow(ctx, q, propriedade, id, somenteMinhas, usuario).
		Scan(&t.ID, &t.FunilID, &t.EtapaID, &t.EtapaTipo, &t.Status, &t.ContactID,
			&t.ProdutoID, &t.CheckIn, &t.CheckOut, &t.Hospedes,
			&t.OrcamentoID, &t.ReservaID, &t.DonoID, &t.Valor, &t.LeadID)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, apperr.NotFound("Oportunidade")
	}
	return t, db.MapError(err)
}

// OportunidadeGravavel é o que o service monta para inserir ou atualizar os
// campos CADASTRAIS. Estado (`stage_id`, `status`, `lost_reason_id`,
// `reservation_id`) não passa por aqui: quem muda estado é a ação nomeada.
type OportunidadeGravavel struct {
	ContactID      uuid.UUID
	LeadID         *uuid.UUID
	FunilID        uuid.UUID
	EtapaID        uuid.UUID
	ProdutoID      *uuid.UUID
	CheckIn        *string
	CheckOut       *string
	Hospedes       *int
	Valor          int64
	Probabilidade  int
	FechamentoPrev *string
	DonoID         *uuid.UUID
	Titulo         string
}

// InserirOportunidade grava o card novo.
//
// `title` é NOT NULL no schema e NÃO existe no contrato: o servidor o compõe de
// contato + produto + datas (ver `tituloDoCard`). Deixar o cliente digitá-lo
// daria vinte grafias do mesmo negócio e uma coluna a mais numa tabela cujo
// antiexemplo declarado tinha 118.
func (r *Repository) InserirOportunidade(ctx context.Context, propriedade uuid.UUID, g OportunidadeGravavel, status string, criador uuid.UUID) (uuid.UUID, error) {
	const q = `
		INSERT INTO crm_opportunities (property_id, contact_id, lead_id, pipeline_id, stage_id,
		                               title, unit_type_id, check_in, check_out, guests_count,
		                               amount_cents, probability, expected_close, owner_id,
		                               status, entered_stage_at, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::date,$9::date,$10,$11,$12,$13::date,$14,$15,now(),$16)
		RETURNING id`

	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, propriedade, g.ContactID, g.LeadID, g.FunilID, g.EtapaID,
		g.Titulo, g.ProdutoID, g.CheckIn, g.CheckOut, g.Hospedes,
		g.Valor, g.Probabilidade, g.FechamentoPrev, g.DonoID, status, criador).Scan(&id)
	return id, db.MapError(err)
}

// AtualizarOportunidade grava os campos cadastrais.
func (r *Repository) AtualizarOportunidade(ctx context.Context, id uuid.UUID, g OportunidadeGravavel) error {
	const q = `
		UPDATE crm_opportunities
		   SET contact_id = $2, lead_id = $3, title = $4, unit_type_id = $5,
		       check_in = $6::date, check_out = $7::date, guests_count = $8,
		       amount_cents = $9, probability = $10, expected_close = $11::date,
		       owner_id = $12, updated_at = now()
		 WHERE id = $1`
	_, err := r.exec(ctx).Exec(ctx, q, id, g.ContactID, g.LeadID, g.Titulo, g.ProdutoID,
		g.CheckIn, g.CheckOut, g.Hospedes, g.Valor, g.Probabilidade, g.FechamentoPrev, g.DonoID)
	return db.MapError(err)
}

// GravarDetalheDeEvento faz o UPSERT do satélite 1:1.
func (r *Repository) GravarDetalheDeEvento(ctx context.Context, oportunidade uuid.UUID, d DetalheDeEvento) error {
	const q = `
		INSERT INTO crm_opportunity_event_details
		       (opportunity_id, event_type, guests_expected, needs_catering, notes)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (opportunity_id) DO UPDATE
		   SET event_type = EXCLUDED.event_type, guests_expected = EXCLUDED.guests_expected,
		       needs_catering = EXCLUDED.needs_catering, notes = EXCLUDED.notes, updated_at = now()`
	_, err := r.exec(ctx).Exec(ctx, q, oportunidade, d.TipoDeEvento, d.ConvidadosPrev, d.PrecisaBuffet, d.Observacoes)
	return db.MapError(err)
}

// ApagarDetalheDeEvento remove o satélite — é o `"event": null` do PATCH.
func (r *Repository) ApagarDetalheDeEvento(ctx context.Context, oportunidade uuid.UUID) error {
	_, err := r.exec(ctx).Exec(ctx, `DELETE FROM crm_opportunity_event_details WHERE opportunity_id = $1`, oportunidade)
	return db.MapError(err)
}

// MoverEtapa grava a etapa nova, o carimbo de entrada e a probabilidade.
//
// `entered_stage_at` é reescrito SEMPRE: é o relógio do SLA e a base do alerta
// de card parado. Voltar para uma etapa anterior reinicia o prazo de propósito
// — é um ciclo novo de atendimento, não a continuação do antigo.
func (r *Repository) MoverEtapa(ctx context.Context, id, etapa uuid.UUID, probabilidade int) error {
	const q = `UPDATE crm_opportunities
	              SET stage_id = $2, entered_stage_at = now(), probability = $3, updated_at = now()
	            WHERE id = $1`
	_, err := r.exec(ctx).Exec(ctx, q, id, etapa, probabilidade)
	return db.MapError(err)
}

// Fechar grava o desfecho: etapa terminal, status, motivo (na perda),
// probabilidade e `closed_at`.
//
// Os quatro campos são gravados NUMA instrução por causa dos CHECKs que os
// amarram: `perdido` exige motivo, motivo só existe na perda, e fechada tem
// data. Gravar em duas instruções deixaria um estado intermediário que o próprio
// banco recusa.
func (r *Repository) Fechar(ctx context.Context, id, etapa uuid.UUID, status string, motivo *uuid.UUID, probabilidade int, reserva *uuid.UUID) error {
	const q = `
		UPDATE crm_opportunities
		   SET stage_id = $2, entered_stage_at = now(), status = $3, lost_reason_id = $4,
		       probability = $5, reservation_id = COALESCE($6, reservation_id),
		       closed_at = now(), updated_at = now()
		 WHERE id = $1`
	_, err := r.exec(ctx).Exec(ctx, q, id, etapa, status, motivo, probabilidade, reserva)
	return db.MapError(err)
}

// ExcluirOportunidade remove fisicamente. Só chega aqui o que está aberto e sem
// reserva — a guarda é do service.
func (r *Repository) ExcluirOportunidade(ctx context.Context, id uuid.UUID) error {
	tag, err := r.exec(ctx).Exec(ctx, `DELETE FROM crm_opportunities WHERE id = $1`, id)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Oportunidade")
	}
	return nil
}

// ═══════════════════════════ Histórico de etapa ═════════════════════

// RegistrarMovimento grava uma linha de `crm_stage_history`.
//
// INSERT-ONLY por contrato do time, como `reservation_events`: mover é
// REGISTRAR, nunca sobrescrever. É desta tabela que sai a conversão por etapa e
// o tempo médio em cada uma — reescrever a linha anterior apagaria justamente o
// gargalo que se quer medir.
func (r *Repository) RegistrarMovimento(ctx context.Context, oportunidade uuid.UUID, de *uuid.UUID, para uuid.UUID, usuario *uuid.UUID, motivo *string) error {
	const q = `INSERT INTO crm_stage_history (opportunity_id, from_stage_id, to_stage_id, user_id, reason)
	           VALUES ($1,$2,$3,$4,$5)`
	_, err := r.exec(ctx).Exec(ctx, q, oportunidade, de, para, usuario, motivo)
	return db.MapError(err)
}

// HistoricoDeEtapas devolve a timeline de movimentos, do mais recente para o
// mais antigo.
//
// `days_in_stage` sai de uma window function (`lag`), e não de N consultas: é
// quanto o card ficou na etapa ANTERIOR, e é o número que mede gargalo de
// funil.
func (r *Repository) HistoricoDeEtapas(ctx context.Context, oportunidade uuid.UUID) ([]EventoDeEtapa, error) {
	const q = `
		SELECT h.id, h.from_stage_id, de.name, h.to_stage_id, para.name,
		       h.user_id, u.name, h.reason, h.at,
		       CASE WHEN lag(h.at) OVER (ORDER BY h.at) IS NULL THEN NULL
		            ELSE EXTRACT(epoch FROM (h.at - lag(h.at) OVER (ORDER BY h.at))) / 86400.0
		       END
		  FROM crm_stage_history h
		  JOIN crm_stages para ON para.id = h.to_stage_id
		  LEFT JOIN crm_stages de ON de.id = h.from_stage_id
		  LEFT JOIN users u ON u.id = h.user_id
		 WHERE h.opportunity_id = $1
		 ORDER BY h.at DESC, h.id DESC`

	linhas, err := r.exec(ctx).Query(ctx, q, oportunidade)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []EventoDeEtapa{}
	for linhas.Next() {
		var e EventoDeEtapa
		if err := linhas.Scan(&e.ID, &e.DeEtapaID, &e.DeEtapaNome, &e.ParaEtapaID, &e.ParaEtapaNome,
			&e.UsuarioID, &e.UsuarioNome, &e.Motivo, &e.Instante, &e.DiasNaEtapa); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, e)
	}
	return out, db.MapError(linhas.Err())
}

// ═══════════════════════════ Kanban ═════════════════════════════════

// Kanban devolve, NUMA consulta, os cards da página de cada coluna E os totais
// da coluna inteira.
//
// A window function faz as duas coisas de uma vez: `row_number()` recorta a
// página por etapa e `count()`/`sum()` somam a coluna toda, ANTES do recorte. É
// o que permite a tela mostrar "312 cards · R$ 1,2 mi" numa coluna de que só 50
// vieram — somar o que chegou daria um valor de funil que muda conforme se rola
// a página.
//
// A alternativa seria uma consulta por coluna (oito no funil padrão) mais uma de
// totais: nove viagens ao banco para desenhar um quadro.
func (r *Repository) Kanban(ctx context.Context, propriedade, funil uuid.UUID, f FiltroDoKanban) (map[uuid.UUID][]CardDaOportunidade, map[uuid.UUID]TotaisDoKanban, error) {
	c := condicoes{args: []any{propriedade, funil}}
	c.partes = append(c.partes, "o.pipeline_id = $2")

	if f.SomenteMinhas {
		c.add("o.owner_id = $%d", f.Usuario)
	} else if f.DonoID != nil {
		c.add("o.owner_id = $%d", *f.DonoID)
	}
	if f.ProdutoID != nil {
		c.add("o.unit_type_id = $%d", *f.ProdutoID)
	}
	if f.Busca != "" {
		c.add("(c.name ILIKE '%%' || $%d || '%%' OR ut.name ILIKE '%%' || $%d || '%%')", f.Busca)
	}
	if f.De != "" {
		c.add("o.check_in >= $%d::date", f.De)
	}
	if f.Ate != "" {
		c.add("o.check_in <= $%d::date", f.Ate)
	}
	if !f.IncluirFechadas {
		// As colunas terminais trazem só o que fechou na janela recente. Sem
		// isto, a coluna "Perdido" de um ano de operação vira a coluna mais
		// pesada do quadro — e é carregada em TODA abertura da tela.
		c.add("(o.status = 'aberto' OR o.closed_at >= now() - make_interval(days => $%d))", JanelaDeFechadasEmDias)
	}

	limite := c.prox(f.PorColuna)

	q := fmt.Sprintf(`
		WITH filtradas AS (
		    SELECT o.id, o.stage_id, o.contact_id, c.name AS contato, ut.name AS produto,
		           o.check_in::text AS entrada, o.check_out::text AS saida,
		           o.amount_cents, o.probability, o.expected_close::text AS fechamento,
		           o.owner_id, u.name AS dono, o.status, o.entered_stage_at,
		           CASE WHEN s.sla_days IS NULL THEN NULL
		                ELSE o.entered_stage_at + make_interval(days => s.sla_days) END AS sla,
		           (SELECT count(*) FROM crm_activities a
		             WHERE a.opportunity_id = o.id AND a.status = 'pendente' AND a.type <> 'nota') AS pendentes,
		           (SELECT min(a.due_at) FROM crm_activities a
		             WHERE a.opportunity_id = o.id AND a.status = 'pendente' AND a.due_at IS NOT NULL) AS proximo,
		           res.code AS reserva
		      FROM crm_opportunities o
		      JOIN contacts c   ON c.id = o.contact_id
		      JOIN crm_stages s ON s.id = o.stage_id
		      LEFT JOIN unit_types ut    ON ut.id = o.unit_type_id
		      LEFT JOIN users u          ON u.id = o.owner_id
		      LEFT JOIN reservations res ON res.id = o.reservation_id
		     WHERE o.property_id = $1 %s
		),
		numeradas AS (
		    SELECT *,
		           row_number() OVER (PARTITION BY stage_id ORDER BY entered_stage_at ASC, id) AS posicao,
		           count(*)     OVER (PARTITION BY stage_id) AS total_coluna,
		           sum(amount_cents) OVER (PARTITION BY stage_id) AS valor_coluna
		      FROM filtradas
		)
		SELECT id, stage_id, contact_id, contato, produto, entrada, saida,
		       amount_cents, probability, fechamento, owner_id, dono, status,
		       entered_stage_at, sla, pendentes, proximo, reserva,
		       total_coluna, valor_coluna
		  FROM numeradas
		 WHERE posicao <= $%d
		 ORDER BY stage_id, posicao`, c.where(), limite)

	linhas, err := r.exec(ctx).Query(ctx, q, c.args...)
	if err != nil {
		return nil, nil, db.MapError(err)
	}
	defer linhas.Close()

	cardsPorEtapa := map[uuid.UUID][]CardDaOportunidade{}
	totais := map[uuid.UUID]TotaisDoKanban{}
	for linhas.Next() {
		var (
			card          CardDaOportunidade
			etapa         uuid.UUID
			probabilidade float64
			totalColuna   int
			valorColuna   int64
		)
		if err := linhas.Scan(&card.ID, &etapa, &card.ContactID, &card.ContatoNome, &card.ProdutoNome,
			&card.CheckIn, &card.CheckOut, &card.Valor, &probabilidade, &card.FechamentoPrev,
			&card.DonoID, &card.DonoNome, &card.Status, &card.EntrouNaEtapaEm, &card.SLAVenceEm,
			&card.TarefasPendentes, &card.ProximoPrazo, &card.ReservaCodigo,
			&totalColuna, &valorColuna); err != nil {
			return nil, nil, db.MapError(err)
		}
		card.Probabilidade = int(math.Round(probabilidade))
		card.Status = EstadoParaContrato(card.Status)
		cardsPorEtapa[etapa] = append(cardsPorEtapa[etapa], card)
		totais[etapa] = TotaisDoKanban{Total: totalColuna, Valor: valorColuna}
	}
	return cardsPorEtapa, totais, db.MapError(linhas.Err())
}

// ═══════════════════════════ Orçamento e reserva ════════════════════

// O orçamento vigente NÃO é lido aqui.
//
// Até 27/08/2026 este arquivo tinha `ReservaDaOportunidade`, `OrcamentoDaOportunidade`
// e `VincularOrcamento`: o "orçamento" era uma reserva em `reservations` com
// status `quote`, lida daqui e apontada por `crm_opportunities.quote_id`. Nenhum
// endpoint criava essa linha — só o fixture da suíte —, e por isso `/win`
// respondia SEMPRE `422 QUOTE_REQUIRED_TO_WIN` (medido contra o stack no ar).
//
// Desde a migration 20260827130000 o orçamento é uma linha de `quotes`, tabela
// própria com dono próprio: quem a lê e a escreve é `internal/modules/
// disponibilidade`, e o CRM pergunta por lá (`Servico.orcamentos`). Repetir a
// projeção de `quotes` aqui criaria a segunda verdade sobre o mesmo registro —
// que é exatamente o que o comentário da versão anterior dizia sobre a reserva.

// AtualizarValorEsperado copia o total do orçamento ganho para `amount_cents`.
//
// O valor vem do orçamento, e não da digitação: `amount_cents` é o que soma no
// total da coluna do kanban, e um número digitado à mão ao lado de uma proposta
// emitida faria a previsão de receita divergir da soma das propostas.
func (r *Repository) AtualizarValorEsperado(ctx context.Context, id uuid.UUID, valor int64) error {
	const q = `UPDATE crm_opportunities SET amount_cents = $2, updated_at = now() WHERE id = $1`
	_, err := r.exec(ctx).Exec(ctx, q, id, valor)
	return db.MapError(err)
}

// HoldDaReserva devolve o vencimento do hold, para o alerta derivado de
// pré-reserva expirando.
func (r *Repository) HoldDaReserva(ctx context.Context, reserva uuid.UUID) (*time.Time, error) {
	const q = `SELECT hold_expires_at FROM reservations WHERE id = $1 AND status = 'hold'`
	var t *time.Time
	err := r.exec(ctx).QueryRow(ctx, q, reserva).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, db.MapError(err)
}

// ContatoResumido lê o contato no formato que o CRM expõe.
func (r *Repository) ContatoResumido(ctx context.Context, propriedade, id uuid.UUID) (ContatoResumo, error) {
	const q = `SELECT id, name, email, phone_e164, city, state
	             FROM contacts WHERE id = $1 AND property_id = $2`
	var c ContatoResumo
	err := r.exec(ctx).QueryRow(ctx, q, id, propriedade).
		Scan(&c.ID, &c.Nome, &c.Email, &c.Telefone, &c.Cidade, &c.Estado)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, apperr.NotFound("Contato")
	}
	return c, db.MapError(err)
}
