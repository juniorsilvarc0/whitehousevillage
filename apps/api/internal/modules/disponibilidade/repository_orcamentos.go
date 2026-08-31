package disponibilidade

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// Leitura e escrita de `quotes` + `quote_nights` (migration 20260827130000).
//
// NENHUMA linha deste arquivo toca `stay_blocks`, e isso é regra de negócio e
// não esquecimento (spec §4): orçamento não bloqueia data. A constraint
// `stay_no_overlap` não enxerga `quotes`, e a contrapartida — um orçamento pode
// virar `409 DATE_CONFLICT` na hora de virar reserva — é aceita e correta. Se o
// orçamento segurasse data, cada simulação de preço da White House Completa, que
// consome as oito unidades, travaria a casa inteira para todo mundo.

// OrcamentoGravavel é o snapshot pronto para virar linha. Ele já vem do motor:
// nenhum valor daqui é calculado no repositório.
type OrcamentoGravavel struct {
	PropertyID     uuid.UUID
	ContactID      *uuid.UUID
	OportunidadeID *uuid.UUID
	UnitTypeID     uuid.UUID
	CheckIn        string // ISO — data viaja como texto (regra 5)
	CheckOut       string
	Hospedes       int
	IsEvento       bool

	Subtotal    int64
	DescontoPct float64
	Desconto    int64
	Limpeza     int64
	Caucao      int64
	Total       int64
	Sinal       int64

	RateTableID            uuid.UUID
	PolicyVersion          int
	PoliticaCancelamentoID *uuid.UUID

	DonoID    *uuid.UUID
	ValidoAte time.Time
	CriadoPor *uuid.UUID

	Noites []NoiteDoOrcamento
}

// InserirOrcamento grava o cabeçalho e as noites.
//
// A ORDEM é obrigatória (a FK de `quote_nights` exige o cabeçalho), e é por isso
// que a coerência do snapshot é imposta por CONSTRAINT TRIGGER ADIADO
// (`quote_nights_fecham_o_orcamento`): entre as duas instruções o orçamento
// existe com subtotal cheio e zero noites, que é um estado intermediário
// legítimo. O que o banco recusa é esse estado COMITADO — e por isso a violação
// aparece no COMMIT, e não aqui.
func (r *Repository) InserirOrcamento(ctx context.Context, g OrcamentoGravavel) (uuid.UUID, time.Time, error) {
	const qCabecalho = `
		INSERT INTO quotes (property_id, contact_id, opportunity_id, unit_type_id,
		                    check_in, check_out, guests_count, is_event,
		                    subtotal_cents, discount_pct, discount_cents, cleaning_cents,
		                    event_deposit_cents, total_cents, deposit_cents,
		                    rate_table_id, policy_version, cancellation_policy_id,
		                    owner_id, valid_until, created_by)
		VALUES ($1, $2, $3, $4, $5::date, $6::date, $7, $8,
		        $9, $10::numeric, $11, $12, $13, $14, $15,
		        $16, $17, $18, $19, $20, $21)
		RETURNING id, created_at`

	var (
		id       uuid.UUID
		criadoEm time.Time
	)
	err := r.exec(ctx).QueryRow(ctx, qCabecalho,
		g.PropertyID, g.ContactID, g.OportunidadeID, g.UnitTypeID,
		g.CheckIn, g.CheckOut, g.Hospedes, g.IsEvento,
		g.Subtotal, g.DescontoPct, g.Desconto, g.Limpeza,
		g.Caucao, g.Total, g.Sinal,
		g.RateTableID, g.PolicyVersion, g.PoliticaCancelamentoID,
		g.DonoID, g.ValidoAte, g.CriadoPor).Scan(&id, &criadoEm)
	if err != nil {
		return uuid.Nil, time.Time{}, db.MapError(err)
	}

	// Uma instrução para todas as noites, e não uma por noite: são até 30 linhas
	// por orçamento e cada ida ao banco custaria uma viagem de rede dentro da
	// transação que segura o card.
	const qNoites = `
		INSERT INTO quote_nights (quote_id, night, date_type, unit_type_id, price_cents)
		SELECT $1, x.noite::date, x.tipo, $2, x.preco
		  FROM unnest($3::text[], $4::text[], $5::bigint[]) AS x(noite, tipo, preco)`

	dias := make([]string, 0, len(g.Noites))
	tipos := make([]string, 0, len(g.Noites))
	precos := make([]int64, 0, len(g.Noites))
	for _, n := range g.Noites {
		dias = append(dias, n.Data)
		tipos = append(tipos, string(n.Tipo))
		precos = append(precos, n.Preco)
	}
	if _, err := r.exec(ctx).Exec(ctx, qNoites, id, g.UnitTypeID, dias, tipos, precos); err != nil {
		return uuid.Nil, time.Time{}, db.MapError(err)
	}
	return id, criadoEm, nil
}

// colunasDoOrcamento é a projeção de `quotes`. `expired` sai do BANCO
// (`valid_until <= now()`) porque "agora" tem de ser um só: comparar em Go
// contra o relógio do processo faria a mesma linha vencer em instantes
// diferentes conforme quem perguntou.
const colunasDoOrcamento = `
	    q.id, q.contact_id, q.opportunity_id, q.unit_type_id,
	    q.check_in::text, q.check_out::text, q.guests_count, q.is_event,
	    q.subtotal_cents, q.discount_pct::float8, q.discount_cents, q.cleaning_cents,
	    q.event_deposit_cents, q.total_cents, q.deposit_cents,
	    q.rate_table_id, q.policy_version, q.cancellation_policy_id,
	    q.owner_id, q.valid_until, q.reservation_id, q.created_at,
	    (q.valid_until <= now())`

// OrcamentoBruto é a linha de `quotes` como ela está no banco.
type OrcamentoBruto struct {
	ID             uuid.UUID
	ContactID      *uuid.UUID
	OportunidadeID *uuid.UUID
	UnitTypeID     uuid.UUID
	CheckIn        string
	CheckOut       string
	Hospedes       int
	IsEvento       bool

	Subtotal    int64
	DescontoPct float64
	Desconto    int64
	Limpeza     int64
	Caucao      int64
	Total       int64
	Sinal       int64

	RateTableID            uuid.UUID
	PolicyVersion          int
	PoliticaCancelamentoID *uuid.UUID

	DonoID    *uuid.UUID
	ValidoAte time.Time
	ReservaID *uuid.UUID
	CriadoEm  time.Time
	Vencido   bool
}

func lerOrcamento(linha pgx.Row) (OrcamentoBruto, error) {
	var o OrcamentoBruto
	err := linha.Scan(&o.ID, &o.ContactID, &o.OportunidadeID, &o.UnitTypeID,
		&o.CheckIn, &o.CheckOut, &o.Hospedes, &o.IsEvento,
		&o.Subtotal, &o.DescontoPct, &o.Desconto, &o.Limpeza,
		&o.Caucao, &o.Total, &o.Sinal,
		&o.RateTableID, &o.PolicyVersion, &o.PoliticaCancelamentoID,
		&o.DonoID, &o.ValidoAte, &o.ReservaID, &o.CriadoEm, &o.Vencido)
	return o, err
}

// BuscarOrcamento lê um orçamento emitido, já com o recorte do escopo.
//
// `somenteMeus` vira `AND q.owner_id = $usuario` NO SQL (regra 8). Orçamento sem
// dono — o que o agente de IA emite — não aparece para quem tem escopo `own`,
// pela mesma razão que o lead sem dono não aparece: é fila da gestão até ser
// distribuído.
func (r *Repository) BuscarOrcamento(ctx context.Context, propriedade, id uuid.UUID,
	somenteMeus bool, usuario uuid.UUID) (OrcamentoBruto, error) {

	q := `SELECT ` + colunasDoOrcamento + `
	        FROM quotes q
	       WHERE q.id = $1 AND q.property_id = $2
	         AND (NOT $3::boolean OR q.owner_id = $4)`

	o, err := lerOrcamento(r.exec(ctx).QueryRow(ctx, q, id, propriedade, somenteMeus, usuario))
	if errors.Is(err, pgx.ErrNoRows) {
		return o, apperr.NotFound("Orçamento")
	}
	return o, db.MapError(err)
}

// NoitesDoOrcamento devolve o detalhe congelado, na ordem da estadia.
func (r *Repository) NoitesDoOrcamento(ctx context.Context, orcamento uuid.UUID) ([]NoiteDoOrcamento, error) {
	const q = `
		SELECT night::text, date_type, price_cents
		  FROM quote_nights WHERE quote_id = $1 ORDER BY night`

	linhas, err := r.exec(ctx).Query(ctx, q, orcamento)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := make([]NoiteDoOrcamento, 0, 8)
	for linhas.Next() {
		var (
			n    NoiteDoOrcamento
			tipo string
		)
		if err := linhas.Scan(&n.Data, &tipo, &n.Preco); err != nil {
			return nil, db.MapError(err)
		}
		n.Tipo = calendar.DateType(tipo)
		n.Rotulo = rotuloDoTipo(n.Tipo)
		out = append(out, n)
	}
	return out, db.MapError(linhas.Err())
}

// OrcamentoVigenteDaOportunidade responde "qual proposta está de pé neste card".
//
// É o ÚLTIMO emitido, e a pergunta é respondida por `quotes.opportunity_id` —
// não pela coluna `crm_opportunities.quote_id`, que ainda referencia
// `reservations(id)` (a FK não foi repontada nesta rodada; ver o relatório).
// Ler do lado de `quotes` é o mesmo fato sem a coluna redundante: "emitir outro
// substitui este" é exatamente `ORDER BY created_at DESC LIMIT 1`.
//
// Vencido e já convertido continuam sendo devolvidos de propósito: quem decide o
// que fazer com eles é o `/win`, que precisa distinguir "não há orçamento
// nenhum" (`QUOTE_REQUIRED_TO_WIN`) de "o que existe não serve mais"
// (`QUOTE_NOT_PENDING`). Filtrar aqui colapsaria os dois no primeiro.
func (r *Repository) OrcamentoVigenteDaOportunidade(ctx context.Context, propriedade, oportunidade uuid.UUID) (*uuid.UUID, error) {
	const q = `
		SELECT q.id
		  FROM quotes q
		 WHERE q.property_id = $1 AND q.opportunity_id = $2
		 ORDER BY q.created_at DESC, q.id DESC
		 LIMIT 1`

	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, propriedade, oportunidade).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, db.MapError(err)
	}
	return &id, nil
}

// VincularAOportunidade prende um orçamento solto ao card antes do ganho.
//
// `AND opportunity_id IS NULL` no WHERE: mover um orçamento de uma negociação
// para outra apagaria o histórico de preço da primeira, e o `/win` recusa antes
// de chegar aqui quando o vínculo existe e é de outro card.
func (r *Repository) VincularAOportunidade(ctx context.Context, orcamento, oportunidade uuid.UUID) error {
	const q = `UPDATE quotes SET opportunity_id = $2, updated_at = now()
	            WHERE id = $1 AND opportunity_id IS NULL`
	_, err := r.exec(ctx).Exec(ctx, q, orcamento, oportunidade)
	return db.MapError(err)
}

// MarcarConvertido grava `quotes.reservation_id` — a conversão do funil medida
// sem recalcular nada.
//
// O `AND reservation_id IS NULL` fecha a corrida de dois ganhos simultâneos
// sobre o mesmo orçamento: o segundo afeta zero linhas e recebe o erro, em vez
// de sobrescrever a venda do primeiro. É a mesma garantia do índice
// `quotes_reserva_uniq`, um passo antes — e com mensagem de negócio.
func (r *Repository) MarcarConvertido(ctx context.Context, orcamento, reserva uuid.UUID) error {
	const q = `UPDATE quotes SET reservation_id = $2, updated_at = now()
	            WHERE id = $1 AND reservation_id IS NULL`
	tag, err := r.exec(ctx).Exec(ctx, q, orcamento, reserva)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return OrcamentoNaoEstaDePe.WithDetails(map[string]any{
			"quote_id": orcamento,
			"hint":     "este orçamento já virou venda; emita outro para vender de novo.",
		})
	}
	return nil
}

// ─────────────────────── Vizinhos que o orçamento consulta ──────────

// PoliticaDeCancelamentoVigente devolve a política que será CONGELADA junto com
// o preço.
//
// Ela não entra na conta do total (por isso `quotes.cancellation_policy_id` é
// anulável), mas precisa ser a mesma que a reserva congelaria — senão o
// orçamento prometeria uma regra de reembolso e a venda gravaria outra. A
// consulta é gêmea da de `reservas.ContextoComercial`, e não uma importação,
// porque `reservas` importa este pacote.
func (r *Repository) PoliticaDeCancelamentoVigente(ctx context.Context, propriedade uuid.UUID) (*uuid.UUID, error) {
	const q = `
		SELECT c.id
		  FROM cancellation_policies c, properties p
		 WHERE p.id = $1 AND c.property_id = $1
		   AND c.valid_from <= (now() AT TIME ZONE p.timezone)::date
		 ORDER BY c.valid_from DESC, c.version DESC
		 LIMIT 1`

	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, propriedade).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, db.MapError(err)
	}
	return &id, nil
}

// Agora devolve o instante do BANCO. Toda decisão de validade sai daqui: dois
// relógios — o do processo e o do Postgres — divergem, e a diferença apareceria
// como orçamento que nasce vencido na virada do segundo.
func (r *Repository) Agora(ctx context.Context) (time.Time, error) {
	var t time.Time
	err := r.exec(ctx).QueryRow(ctx, `SELECT now()`).Scan(&t)
	return t, db.MapError(err)
}

// ConferirContato recusa contato de outra propriedade — e o anonimizado.
func (r *Repository) ConferirContato(ctx context.Context, propriedade, id uuid.UUID) error {
	const q = `SELECT anonymized_at IS NOT NULL FROM contacts WHERE id = $1 AND property_id = $2`
	var anonimizado bool
	err := r.exec(ctx).QueryRow(ctx, q, id, propriedade).Scan(&anonimizado)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("Contato")
	}
	if err != nil {
		return db.MapError(err)
	}
	if anonimizado {
		// O contato pediu eliminação: não se emite proposta nova para ele. É o
		// mesmo código que `/contacts` usa — um código, um status.
		return ContatoAnonimizado.WithDetails(map[string]any{"contact_id": id})
	}
	return nil
}

// OportunidadeDoOrcamento lê o mínimo do card que a emissão precisa conferir.
//
// Leitura de tabela do CRM a partir daqui, e é deliberado: `crm` importa este
// pacote (o `/win` chama o motor por ele), então a dependência inversa fecharia
// um ciclo de compilação. O que este módulo faz com `crm_opportunities` é o
// mínimo: conferir que o card existe na casa e de quem ele é.
func (r *Repository) OportunidadeDoOrcamento(ctx context.Context, propriedade, id uuid.UUID) (uuid.UUID, error) {
	const q = `SELECT contact_id FROM crm_opportunities WHERE id = $1 AND property_id = $2`
	var contato uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, id, propriedade).Scan(&contato)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apperr.NotFound("Oportunidade")
	}
	return contato, db.MapError(err)
}

// AtualizarValorEsperado copia o total do orçamento para `amount_cents` do card.
//
// É a segunda (e última) escrita deste módulo numa tabela do CRM, pelo mesmo
// impedimento de ciclo — e é o que o contrato pede em `Oportunidade.amount_cents`:
// "nasce do orçamento vigente quando existe um". Sem isso, o total da coluna do
// kanban continuaria mostrando a estimativa digitada à mão ao lado de uma
// proposta já emitida, e a previsão de receita divergiria da soma das propostas.
//
// `status = 'aberto'` no WHERE: card fechado é história, e história não se
// reescreve porque alguém emitiu um orçamento novo depois.
func (r *Repository) AtualizarValorEsperado(ctx context.Context, oportunidade uuid.UUID, valor int64) error {
	const q = `UPDATE crm_opportunities SET amount_cents = $2, updated_at = now()
	            WHERE id = $1 AND status = 'aberto'`
	_, err := r.exec(ctx).Exec(ctx, q, oportunidade, valor)
	return db.MapError(err)
}
