package reservas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/booking"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/commission"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/money"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// statusQueBloqueiam é o predicado da constraint `stay_no_overlap`, repetido
// aqui como filtro de LEITURA. Não é regra duplicada: a regra continua sendo a
// constraint, que é quem RECUSA. Esta lista só faz as consultas casarem com o
// índice parcial que a constraint cria.
//
// ATENÇÃO ao que ela NÃO é: desde que `completed` existe, esta lista já não
// serve para desenhar o mapa nem para contar ocupação — ela é o inventário
// VENDÁVEL. O predicado de exibição é StatusVisiveisNoMapa (ver dto.go).
var statusQueBloqueiam = StatusQueBloqueiam

// Repository fala com o Postgres. Nunca abre transação: pega o executor do
// contexto com db.From, que devolve a transação em curso se houver — é o que
// permite a mesma função servir dentro e fora de transação.
type Repository struct {
	pool db.DBTX
}

func NewRepository(pool db.DBTX) *Repository { return &Repository{pool: pool} }

func (r *Repository) exec(ctx context.Context) db.DBTX { return db.From(ctx, r.pool) }

// ─────────────────────────── Contexto comercial ─────────────────────

// ContextoComercial é o que a criação de reserva precisa da política e que o
// orçamento não devolve: prazo do hold, limite de extensão e qual política de
// cancelamento vai ser CONGELADA na reserva.
type ContextoComercial struct {
	Hoje          calendar.Date
	PolicyVersion int
	HoldHoras     int
	// HoldExtensaoHoras e HoldMaxExtensoes vêm das colunas que a migration
	// 20260826110000 acrescentou. NÃO existem em booking.Policy — o domínio
	// ainda não os enxerga (ver relatório), então viajam neste struct.
	HoldExtensaoHoras    int
	HoldMaxExtensoes     int
	PoliticaCancelamento *uuid.UUID
}

// ContextoComercial resolve numa consulta só o "hoje" da casa, a política
// comercial (vigente ou a versão pedida) e a política de cancelamento vigente.
//
// "Hoje" sai do BANCO, com `now() AT TIME ZONE properties.timezone`. O fuso é
// dado, não constante em Go: duas definições de "hoje" divergem na virada do dia
// e fariam a antecedência do cancelamento depender de quem perguntou.
func (r *Repository) ContextoComercial(ctx context.Context, propriedade uuid.UUID, versao *int) (ContextoComercial, error) {
	const q = `
		WITH hoje AS (
		    SELECT (now() AT TIME ZONE p.timezone)::date AS dia
		      FROM properties p WHERE p.id = $1
		),
		politica AS (
		    SELECT cp.version, cp.hold_hours, cp.hold_extension_hours, cp.hold_max_extensions
		      FROM commercial_policies cp, hoje
		     WHERE cp.property_id = $1
		       AND ( ($2::int IS NOT NULL AND cp.version = $2::int)
		          OR ($2::int IS NULL AND cp.valid_from <= hoje.dia) )
		     ORDER BY cp.valid_from DESC, cp.version DESC
		     LIMIT 1
		),
		cancelamento AS (
		    SELECT c.id
		      FROM cancellation_policies c, hoje
		     WHERE c.property_id = $1 AND c.valid_from <= hoje.dia
		     ORDER BY c.valid_from DESC, c.version DESC
		     LIMIT 1
		)
		SELECT hoje.dia::text, politica.version, politica.hold_hours,
		       politica.hold_extension_hours, politica.hold_max_extensions,
		       cancelamento.id
		  FROM hoje
		  LEFT JOIN politica     ON true
		  LEFT JOIN cancelamento ON true`

	var (
		c        ContextoComercial
		hoje     string
		versaoDB *int
		hold     *int
		extensao *int
		maxExt   *int
	)
	err := r.exec(ctx).QueryRow(ctx, q, propriedade, versao).
		Scan(&hoje, &versaoDB, &hold, &extensao, &maxExt, &c.PoliticaCancelamento)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, apperr.NotFound("Propriedade")
	}
	if err != nil {
		return c, db.MapError(err)
	}
	if versaoDB == nil {
		return c, apperr.NotFound("Política comercial vigente")
	}
	if c.Hoje, err = calendar.Parse(hoje); err != nil {
		return c, apperr.Internal.WithCause(err)
	}

	c.PolicyVersion, c.HoldHoras = *versaoDB, *hold
	c.HoldExtensaoHoras, c.HoldMaxExtensoes = *extensao, *maxExt
	return c, nil
}

// DataLocal converte um instante no DIA da casa.
//
// O fuso é DADO (`properties.timezone`), nunca constante em Go: `America/Fortaleza`
// hoje, e a segunda propriedade pode estar em outro. Vale para o instante que o
// operador informou e para `now()` quando ele não informou nada — `COALESCE`
// resolve os dois no mesmo lugar, que é o que impede o check-in "sem `at`" de
// escapar da validação por um caminho diferente do check-in "com `at`". Foi
// exatamente assim que a revisão registrou um check-in em 2019 numa estadia de
// 2026, e um check-in de HOJE numa reserva de 2031.
func (r *Repository) DataLocal(ctx context.Context, propriedade uuid.UUID, instante *time.Time) (calendar.Date, error) {
	const q = `SELECT (COALESCE($2::timestamptz, now()) AT TIME ZONE timezone)::text
	             FROM properties WHERE id = $1`

	var dia string
	err := r.exec(ctx).QueryRow(ctx, q, propriedade, instante).Scan(&dia)
	if errors.Is(err, pgx.ErrNoRows) {
		return calendar.Date{}, apperr.NotFound("Propriedade")
	}
	if err != nil {
		return calendar.Date{}, db.MapError(err)
	}
	d, err := calendar.Parse(dia[:10])
	if err != nil {
		return calendar.Date{}, apperr.Internal.WithCause(err)
	}
	return d, nil
}

// InstanteDoEvento devolve o instante EFETIVO do último evento de um tipo: o
// `at` que o operador informou no payload quando houver, senão o carimbo da
// linha. É o que permite ao /check-out recusar uma saída anterior à entrada
// mesmo quando as duas foram registradas fora de ordem.
func (r *Repository) InstanteDoEvento(ctx context.Context, reserva uuid.UUID, tipo string) (*time.Time, error) {
	const q = `
		SELECT COALESCE((payload->>'at')::timestamptz, at)
		  FROM reservation_events
		 WHERE reservation_id = $1 AND type = $2
		 ORDER BY at DESC LIMIT 1`

	var t *time.Time
	err := r.exec(ctx).QueryRow(ctx, q, reserva, tipo).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, db.MapError(err)
}

// ─────────────────────────── Produto e contato ──────────────────────

// Produto é o mínimo de `unit_types` que este módulo precisa.
type Produto struct {
	ID         uuid.UUID
	Codigo     string
	Nome       string
	Capacidade int
	Consome    string
	// Declaradas é o TAMANHO DA COMPOSIÇÃO: quantas unidades `unit_type_members`
	// promete, sem filtrar por `active`. É o número contra o qual a alocação da
	// Completa se compara — e é a diferença entre "vendi a casa" e "vendi sete
	// oitavos da casa pelo preço da casa".
	Declaradas int
}

// Produto lê o produto ATIVO da propriedade. Inativo é 404 inclusive pelo id:
// o que está desligado não é vendável.
func (r *Repository) Produto(ctx context.Context, propriedade, id uuid.UUID) (Produto, error) {
	const q = `SELECT ut.id, ut.code, ut.name, ut.capacity, ut.consumes,
	                  (SELECT count(*) FROM unit_type_members m WHERE m.unit_type_id = ut.id)
	             FROM unit_types ut
	            WHERE ut.property_id = $1 AND ut.id = $2 AND ut.active`

	var p Produto
	err := r.exec(ctx).QueryRow(ctx, q, propriedade, id).
		Scan(&p.ID, &p.Codigo, &p.Nome, &p.Capacidade, &p.Consome, &p.Declaradas)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, apperr.NotFound("Produto")
	}
	return p, db.MapError(err)
}

// ConferirContato recusa contato de outra propriedade antes do INSERT.
//
// Poderia ficar por conta da foreign key, mas a FK não sabe de propriedade: um
// contact_id da casa vizinha passaria pela FK e viraria uma reserva com hóspede
// que a gestão não enxerga na própria lista.
func (r *Repository) ConferirContato(ctx context.Context, propriedade, id uuid.UUID) error {
	const q = `SELECT 1 FROM contacts WHERE id = $1 AND property_id = $2`

	var existe int
	err := r.exec(ctx).QueryRow(ctx, q, id, propriedade).Scan(&existe)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("Contato")
	}
	return db.MapError(err)
}

// ─────────────────────────── Alocação ───────────────────────────────

// Candidatas devolve as unidades da composição do produto com a distância até a
// ocupação vizinha, para OrdenarPorFragmentacao decidir a ordem de tentativa.
//
// Isto NÃO é uma checagem de disponibilidade: nenhuma unidade é excluída por
// estar ocupada, e a ordem produzida é só preferência. Quem recusa a data é a
// constraint, no INSERT — checar antes e inserir depois é o TOCTOU que a regra 2
// do CLAUDE.md proíbe.
func (r *Repository) Candidatas(ctx context.Context, propriedade, produto uuid.UUID, checkIn, checkOut string) ([]Candidata, error) {
	const q = `
		SELECT u.id, u.code,
		       COALESCE((SELECT min($3::date - upper(sb.period))
		                   FROM stay_blocks sb
		                  WHERE sb.unit_id = u.id
		                    AND sb.status = ANY($5::text[])
		                    AND upper(sb.period) <= $3::date), -1) AS folga_antes,
		       COALESCE((SELECT min(lower(sb.period) - $4::date)
		                   FROM stay_blocks sb
		                  WHERE sb.unit_id = u.id
		                    AND sb.status = ANY($5::text[])
		                    AND lower(sb.period) >= $4::date), -1) AS folga_depois
		  FROM unit_type_members m
		  JOIN units u ON u.id = m.unit_id AND u.active AND u.property_id = $2
		 WHERE m.unit_type_id = $1
		 ORDER BY u.code`

	linhas, err := r.exec(ctx).Query(ctx, q, produto, propriedade, checkIn, checkOut, statusQueBloqueiam)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	var out []Candidata
	for linhas.Next() {
		var c Candidata
		if err := linhas.Scan(&c.ID, &c.Codigo, &c.FolgaAntes, &c.FolgaDepois); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, c)
	}
	return out, db.MapError(linhas.Err())
}

// ─────────────────────────── Criação ────────────────────────────────

// NovaReserva é o que a criação grava em `reservations`.
type NovaReserva struct {
	PropertyID uuid.UUID
	UnitTypeID uuid.UUID
	ContactID  uuid.UUID
	BrokerID   *uuid.UUID
	OwnerID    *uuid.UUID
	Origem     string
	Status     string
	CheckIn    string
	CheckOut   string
	Hospedes   int
	IsEvento   bool
	TipoEvento *string
	// HoldHoras é o prazo da pré-reserva em horas. Viaja como NÚMERO, e não
	// como instante calculado em Go, porque o vencimento tem de sair do relógio
	// do BANCO: o job de expiração compara com o `now()` do Postgres, e um
	// processo com o relógio minutos à frente venceria hold ainda válido.
	// Nulo em reserva que não nasce em `hold`.
	HoldHoras   *int
	RemarcadaDe *uuid.UUID
	Observacoes *string
	CriadaPor   *uuid.UUID
	// CorretorRestritoA é o ator em escopo `own` de `reservations:criar`. Com
	// ele, o INSERT só acontece se BrokerID for nulo ou o `users.broker_id`
	// desse ator — conferido na MESMA instrução (ver corretor.go).
	CorretorRestritoA *uuid.UUID
}

// InserirReserva grava a reserva e devolve id e código.
//
// `code` é OMITIDO no INSERT de propósito: quem o gera é o DEFAULT
// `proximo_codigo_reserva()` da migration 20260826110000. Dois ganhos — nenhum
// caminho de criação pode esquecer de numerar, e o lock do contador do ano é
// sempre tomado ANTES dos locks das unidades (a FK de stay_blocks exige que a
// reserva exista primeiro). Ordem única de aquisição é o que impede o impasse
// entre as duas travas.
//
// A AUTORIDADE sobre `broker_id` também é conferida aqui, e não só no service:
// `INSERT ... SELECT ... WHERE` grava zero linhas quando, em escopo `own`
// ($17), o corretor pedido não é o `users.broker_id` do ator lido NESTA
// instrução. A leitura que o service fez antes alimenta o domínio; esta é a
// que vale sob concorrência (a conta desvinculada entre as duas cai aqui).
func (r *Repository) InserirReserva(ctx context.Context, n NovaReserva) (uuid.UUID, string, *time.Time, error) {
	const q = `
		INSERT INTO reservations (property_id, unit_type_id, contact_id, broker_id, owner_id,
		                          source, status, check_in, check_out, guests_count,
		                          is_event, event_type, hold_expires_at, rebooked_from_id,
		                          notes, created_by, confirmed_at)
		SELECT $1,$2,$3,$4,$5,$6,$7,$8::date,$9::date,$10,$11,$12,
		       CASE WHEN $13::int IS NULL THEN NULL ELSE now() + make_interval(hours => $13::int) END,
		       $14,$15,$16,
		       CASE WHEN $7 = 'confirmed' THEN now() ELSE NULL END
		 WHERE $17::uuid IS NULL
		    OR $4::uuid IS NULL
		    OR $4::uuid = (SELECT u.broker_id FROM users u WHERE u.id = $17::uuid)
		RETURNING id, code, hold_expires_at`

	var (
		id     uuid.UUID
		codigo string
		expira *time.Time
	)
	err := r.exec(ctx).QueryRow(ctx, q,
		n.PropertyID, n.UnitTypeID, n.ContactID, n.BrokerID, n.OwnerID,
		n.Origem, n.Status, n.CheckIn, n.CheckOut, n.Hospedes,
		n.IsEvento, n.TipoEvento, n.HoldHoras, n.RemarcadaDe,
		n.Observacoes, n.CriadaPor, n.CorretorRestritoA).Scan(&id, &codigo, &expira)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", nil, recusaDoCorretor(commission.ReasonNotActorBroker)
	}
	return id, codigo, expira, db.MapError(err)
}

// CorretorDaConta é o `users.broker_id` do ator — nil para a conta sem
// cadastro de corretor. Alimenta o domínio; a garantia é a condição dentro da
// escrita (InserirReserva, AtualizarCadastro).
func (r *Repository) CorretorDaConta(ctx context.Context, usuario uuid.UUID) (*uuid.UUID, error) {
	var corretor *uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, `SELECT broker_id FROM users WHERE id = $1`, usuario).Scan(&corretor)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.Unauthorized
	}
	return corretor, db.MapError(err)
}

// Preco é o snapshot financeiro que vai para `reservation_pricing`.
type Preco struct {
	Subtotal       int64
	DescontoPct    float64
	Desconto       int64
	Limpeza        int64
	CaucaoEvento   int64
	Total          int64
	Sinal          int64
	RateTableID    *uuid.UUID
	PolicyVersion  *int
	CancelPolicyID *uuid.UUID
}

// InserirPreco grava o satélite 1:1. A PK é a FK, então duas linhas de preço
// para a mesma reserva são impossíveis por construção.
func (r *Repository) InserirPreco(ctx context.Context, reserva uuid.UUID, p Preco) error {
	const q = `
		INSERT INTO reservation_pricing (reservation_id, subtotal_cents, discount_pct,
		        discount_cents, cleaning_cents, event_deposit_cents, total_cents,
		        deposit_cents, rate_table_id, policy_version, cancellation_policy_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`

	_, err := r.exec(ctx).Exec(ctx, q, reserva, p.Subtotal, p.DescontoPct, p.Desconto,
		p.Limpeza, p.CaucaoEvento, p.Total, p.Sinal, p.RateTableID, p.PolicyVersion, p.CancelPolicyID)
	return db.MapError(err)
}

// InserirNoites grava o snapshot noite a noite numa instrução só.
//
// É o que faz o passado não mudar quando o tarifário muda (regra 7) — e o que
// permite ADR e RevPAR saírem de um GROUP BY em vez de um recálculo.
func (r *Repository) InserirNoites(ctx context.Context, reserva, produto uuid.UUID, noites []NoiteDaReserva) error {
	if len(noites) == 0 {
		return nil
	}
	datas := make([]string, len(noites))
	tipos := make([]string, len(noites))
	precos := make([]int64, len(noites))
	for i, n := range noites {
		datas[i], tipos[i], precos[i] = n.Noite, string(n.Tipo), n.Preco
	}

	const q = `
		INSERT INTO reservation_nights (reservation_id, night, date_type, unit_type_id, price_cents)
		SELECT $1, x.noite::date, x.tipo, $2, x.preco
		  FROM unnest($3::text[], $4::text[], $5::bigint[]) AS x(noite, tipo, preco)`

	_, err := r.exec(ctx).Exec(ctx, q, reserva, produto, datas, tipos, precos)
	return db.MapError(err)
}

// InserirTitular grava a rooming list mínima: o contato da reserva é o titular.
func (r *Repository) InserirTitular(ctx context.Context, reserva, contato uuid.UUID) error {
	const q = `
		INSERT INTO reservation_guests (reservation_id, contact_id, is_lead_guest)
		VALUES ($1, $2, true)
		ON CONFLICT (reservation_id, contact_id) DO UPDATE SET is_lead_guest = true`

	_, err := r.exec(ctx).Exec(ctx, q, reserva, contato)
	return db.MapError(err)
}

// InserirEvento grava uma linha na timeline append-only.
func (r *Repository) InserirEvento(ctx context.Context, reserva uuid.UUID, tipo string, payload map[string]any, autor *uuid.UUID) error {
	var bruto []byte
	if len(payload) > 0 {
		var err error
		if bruto, err = json.Marshal(payload); err != nil {
			return apperr.Internal.WithCause(err)
		}
	}

	const q = `INSERT INTO reservation_events (reservation_id, type, payload, actor_id)
	           VALUES ($1, $2, $3, $4)`

	_, err := r.exec(ctx).Exec(ctx, q, reserva, tipo, bruto, autor)
	return db.MapError(err)
}

// ContarEventos conta as linhas de um tipo. É daqui que sai `extensions_count`:
// o log append-only já é a verdade sobre quantas vezes o hold foi estendido, e
// um contador denormalizado em `reservations` seria uma segunda verdade.
func (r *Repository) ContarEventos(ctx context.Context, reserva uuid.UUID, tipo string) (int, error) {
	const q = `SELECT count(*) FROM reservation_events WHERE reservation_id = $1 AND type = $2`

	var n int
	err := r.exec(ctx).QueryRow(ctx, q, reserva, tipo).Scan(&n)
	return n, db.MapError(err)
}

// SinalPago devolve o valor do último evento de confirmação, ou nil quando a
// reserva nunca foi confirmada.
//
// O sinal EFETIVAMENTE recebido não tem coluna (a reserva já nasceu acima do
// teto do db.md): vive em `reservation_events`, e é daqui que /cancel lê a base
// da devolução.
func (r *Repository) SinalPago(ctx context.Context, reserva uuid.UUID) (*int64, error) {
	const q = `
		SELECT (payload->>'deposit_paid_cents')::bigint
		  FROM reservation_events
		 WHERE reservation_id = $1 AND type = $2
		   AND payload ? 'deposit_paid_cents'
		 ORDER BY at DESC
		 LIMIT 1`

	var v *int64
	err := r.exec(ctx).QueryRow(ctx, q, reserva, EventoConfirmada).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, db.MapError(err)
}

// CreditoRegistrado soma os créditos a favor do hóspede gravados na timeline
// desta reserva (`credit_issued`).
//
// A soma sai dos EVENTOS, e não de uma coluna, pelo mesmo motivo do sinal pago:
// `reservations` já nasceu acima do teto de colunas do db.md, e um log
// append-only responde melhor "quanto ficou a favor do hóspede, quando e por
// quê" do que um saldo que alguém pode sobrescrever. Quando o módulo financeiro
// existir, esta soma vira `receivables`/`payables` — e o evento continua sendo
// a origem auditável.
func (r *Repository) CreditoRegistrado(ctx context.Context, reserva uuid.UUID) (int64, error) {
	const q = `
		SELECT COALESCE(sum((payload->>'credit_cents')::bigint), 0)
		  FROM reservation_events
		 WHERE reservation_id = $1 AND type = $2
		   AND payload ? 'credit_cents'`

	var v int64
	err := r.exec(ctx).QueryRow(ctx, q, reserva, EventoCredito).Scan(&v)
	return v, db.MapError(err)
}

// ─────────────────────────── Estado (com trava) ─────────────────────

// Estado é a reserva vista por quem vai MUDÁ-LA: o mínimo para decidir a
// transição, sem carregar a resposta inteira.
type Estado struct {
	ID             uuid.UUID
	Codigo         string
	Status         string
	PropertyID     uuid.UUID
	UnitTypeID     uuid.UUID
	Consome        string
	Capacidade     int
	ContactID      uuid.UUID
	BrokerID       *uuid.UUID
	OwnerID        *uuid.UUID
	CheckIn        string
	CheckOut       string
	Hospedes       int
	IsEvento       bool
	TipoEvento     *string
	DescontoPct    float64
	Origem         string
	Observacoes    *string
	Total          int64
	Sinal          int64
	PolicyVersion  *int
	CancelPolicyID *uuid.UUID
	ConfirmadaEm   *time.Time
	HoldExpiraEm   *time.Time
	// HoldVencido é comparado no BANCO, com o relógio do banco: o relógio do
	// processo pode estar minutos à frente e "vencer" um hold que ainda vale.
	HoldVencido bool
	// Antecedencia é `check_in − hoje` no fuso da PROPRIEDADE, em dias.
	Antecedencia int
}

// TravarReserva lê a reserva com `FOR UPDATE`, que é o que serializa duas
// confirmações simultâneas da mesma reserva — sem isso, dois cliques no botão
// "registrar sinal" gravariam dois eventos de pagamento.
//
// `somenteMinhas` é o escopo `own` do RBAC aplicado NO SQL: quem só vê as
// próprias reservas recebe 404 na alheia, e não 403 — 403 confirmaria que a
// reserva existe.
func (r *Repository) TravarReserva(ctx context.Context, propriedade, id uuid.UUID, somenteMinhas bool, usuario uuid.UUID) (Estado, error) {
	return r.lerEstado(ctx, propriedade, id, somenteMinhas, usuario, true)
}

// LerEstado é a mesma leitura SEM travar a linha. Existe para os caminhos de
// LEITURA — a prévia de cancelamento do /full, por exemplo: um GET que segura
// `FOR UPDATE` faria a tela da reserva ficar esperando a confirmação em curso, e
// pior, bloquearia a confirmação enquanto alguém olha a tela.
func (r *Repository) LerEstado(ctx context.Context, propriedade, id uuid.UUID, somenteMinhas bool, usuario uuid.UUID) (Estado, error) {
	return r.lerEstado(ctx, propriedade, id, somenteMinhas, usuario, false)
}

func (r *Repository) lerEstado(ctx context.Context, propriedade, id uuid.UUID, somenteMinhas bool, usuario uuid.UUID, travar bool) (Estado, error) {
	const base = `
		SELECT r.id, r.code, r.status, r.property_id, r.unit_type_id,
		       ut.consumes, ut.capacity,
		       r.contact_id, r.broker_id, r.owner_id,
		       r.check_in::text, r.check_out::text, r.guests_count, r.is_event, r.event_type,
		       COALESCE(p.discount_pct, 0)::float8, r.source, r.notes,
		       COALESCE(p.total_cents, 0), COALESCE(p.deposit_cents, 0),
		       p.policy_version, p.cancellation_policy_id,
		       r.confirmed_at, r.hold_expires_at,
		       (r.hold_expires_at IS NOT NULL AND r.hold_expires_at < now()) AS vencido,
		       (r.check_in - (now() AT TIME ZONE prop.timezone)::date) AS antecedencia
		  FROM reservations r
		  JOIN unit_types ut  ON ut.id = r.unit_type_id
		  JOIN properties prop ON prop.id = r.property_id
		  LEFT JOIN reservation_pricing p ON p.reservation_id = r.id
		 WHERE r.property_id = $1 AND r.id = $2
		   AND ($3::boolean = false OR r.owner_id = $4::uuid)`

	q := base
	if travar {
		// `FOR UPDATE OF r` e não `FOR UPDATE` seco: travar também unit_types,
		// contacts e properties seguraria o catálogo inteiro por reserva.
		q += ` FOR UPDATE OF r`
	}

	var e Estado
	err := r.exec(ctx).QueryRow(ctx, q, propriedade, id, somenteMinhas, usuario).Scan(
		&e.ID, &e.Codigo, &e.Status, &e.PropertyID, &e.UnitTypeID,
		&e.Consome, &e.Capacidade,
		&e.ContactID, &e.BrokerID, &e.OwnerID,
		&e.CheckIn, &e.CheckOut, &e.Hospedes, &e.IsEvento, &e.TipoEvento,
		&e.DescontoPct, &e.Origem, &e.Observacoes,
		&e.Total, &e.Sinal, &e.PolicyVersion, &e.CancelPolicyID,
		&e.ConfirmadaEm, &e.HoldExpiraEm, &e.HoldVencido, &e.Antecedencia)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, apperr.NotFound("Reserva")
	}
	return e, db.MapError(err)
}

// AtualizarStatus muda o estado comercial e, quando informados, os carimbos de
// confirmação e cancelamento.
func (r *Repository) AtualizarStatus(ctx context.Context, id uuid.UUID, status string, confirmadaEm, canceladaEm *time.Time, motivo *string, limparHold bool) error {
	const q = `
		UPDATE reservations
		   SET status = $2,
		       confirmed_at = COALESCE($3, confirmed_at),
		       cancelled_at = COALESCE($4, cancelled_at),
		       cancel_reason = COALESCE($5, cancel_reason),
		       hold_expires_at = CASE WHEN $6 THEN NULL ELSE hold_expires_at END,
		       updated_at = now()
		 WHERE id = $1`

	_, err := r.exec(ctx).Exec(ctx, q, id, status, confirmadaEm, canceladaEm, motivo, limparHold)
	return db.MapError(err)
}

// EstenderHold empurra o vencimento da reserva e dos blocos que ela segura.
//
// O novo prazo é contado a partir de now(), nunca do vencimento antigo: hold
// vencido não se conserta para trás.
func (r *Repository) EstenderHold(ctx context.Context, id uuid.UUID, horas int) (time.Time, error) {
	const qReserva = `
		UPDATE reservations
		   SET hold_expires_at = now() + make_interval(hours => $2), updated_at = now()
		 WHERE id = $1
		RETURNING hold_expires_at`

	var novo time.Time
	if err := r.exec(ctx).QueryRow(ctx, qReserva, id, horas).Scan(&novo); err != nil {
		return time.Time{}, db.MapError(err)
	}

	const qBlocos = `
		UPDATE stay_blocks SET expires_at = $2
		 WHERE reservation_id = $1 AND status = 'hold'`

	if _, err := r.exec(ctx).Exec(ctx, qBlocos, id, novo); err != nil {
		return time.Time{}, db.MapError(err)
	}
	return novo, nil
}

// AtualizarCadastro grava os campos editáveis por PUT/PATCH. Datas, produto e
// preço não estão aqui de propósito — ver ReservaAtualizar.
//
// Em escopo `own` ($9 = o ator), TROCAR o corretor só acontece se o valor novo
// e o gravado forem, cada um, nulo ou o `users.broker_id` do ator lido NESTA
// instrução — a mesma conferência do INSERT. Reenviar o valor gravado não é
// troca e passa. A linha já está travada por TravarReserva (status e corretor
// gravado não mudam por baixo); o que pode mudar é a conta do ator, e só esta
// condição a vê. Zero linhas = recusa.
func (r *Repository) AtualizarCadastro(ctx context.Context, id uuid.UUID, contato uuid.UUID, corretor AtribuicaoDoCorretor, hospedes int, isEvento bool, tipoEvento *string, origem string, notas *string) error {
	const q = `
		UPDATE reservations r
		   SET contact_id = $2, broker_id = $3, guests_count = $4,
		       is_event = $5, event_type = $6, source = $7, notes = $8,
		       updated_at = now()
		 WHERE r.id = $1
		   AND ( $9::uuid IS NULL
		      OR r.broker_id IS NOT DISTINCT FROM $3::uuid
		      OR ( ($3::uuid IS NULL OR $3::uuid = (SELECT u.broker_id FROM users u WHERE u.id = $9::uuid))
		       AND (r.broker_id IS NULL OR r.broker_id = (SELECT u.broker_id FROM users u WHERE u.id = $9::uuid)) ) )`

	tag, err := r.exec(ctx).Exec(ctx, q, id, contato, corretor.Corretor, hospedes, isEvento, tipoEvento, origem, notas,
		corretor.RestritoA)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return recusaDoCorretor(commission.ReasonNotActorBroker)
	}
	return nil
}

// DescartarQuote apaga de verdade — e só o que nunca existiu comercialmente.
// A guarda de status está no WHERE, e não só no service, porque entre a leitura
// e o DELETE outra transação pode ter confirmado a reserva.
func (r *Repository) DescartarQuote(ctx context.Context, id uuid.UUID) (bool, error) {
	const q = `DELETE FROM reservations WHERE id = $1 AND status = $2`

	tag, err := r.exec(ctx).Exec(ctx, q, id, EstadoQuote)
	if err != nil {
		return false, db.MapError(err)
	}
	return tag.RowsAffected() > 0, nil
}

// ─────────────────────────── Política de cancelamento ───────────────

// PoliticaDeCancelamento monta o objeto do domínio a partir da política
// CONGELADA na reserva — nunca da vigente hoje. Publicar uma política nova
// amanhã não pode alterar o cancelamento de uma venda de ontem.
func (r *Repository) PoliticaDeCancelamento(ctx context.Context, id uuid.UUID) (booking.CancellationPolicy, error) {
	const qPolitica = `SELECT version, name FROM cancellation_policies WHERE id = $1`

	var p booking.CancellationPolicy
	err := r.exec(ctx).QueryRow(ctx, qPolitica, id).Scan(&p.Version, &p.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, apperr.NotFound("Política de cancelamento")
	}
	if err != nil {
		return p, db.MapError(err)
	}

	const qFaixas = `
		SELECT days_before_min, days_before_max, refund_pct::float8, label
		  FROM cancellation_tiers WHERE policy_id = $1 ORDER BY sort_order`

	linhas, err := r.exec(ctx).Query(ctx, qFaixas, id)
	if err != nil {
		return p, db.MapError(err)
	}
	defer linhas.Close()

	for linhas.Next() {
		var (
			min, max *int
			t        booking.Tier
		)
		if err := linhas.Scan(&min, &max, &t.RefundPct, &t.Label); err != nil {
			return p, db.MapError(err)
		}
		// NULL no banco é "sem piso/sem teto"; -1 é como o domínio representa
		// a mesma ausência (booking.Tier).
		t.MinDaysBefore, t.MaxDaysBefore = -1, -1
		if min != nil {
			t.MinDaysBefore = *min
		}
		if max != nil {
			t.MaxDaysBefore = *max
		}
		p.Tiers = append(p.Tiers, t)
	}
	return p, db.MapError(linhas.Err())
}

// ─────────────────────────── Leitura ────────────────────────────────

const colunasDaReserva = `
	r.id, r.code, r.status, r.unit_type_id, ut.name, r.contact_id, c.name,
	r.broker_id, r.source, r.check_in::text, r.check_out::text,
	(r.check_out - r.check_in) AS noites,
	r.guests_count, r.is_event, r.event_type,
	COALESCE(p.subtotal_cents,0), COALESCE(p.discount_pct,0)::float8,
	COALESCE(p.discount_cents,0), COALESCE(p.cleaning_cents,0),
	COALESCE(p.event_deposit_cents,0), COALESCE(p.total_cents,0),
	COALESCE(p.deposit_cents,0),
	p.rate_table_id, p.policy_version, p.cancellation_policy_id,
	r.hold_expires_at, r.confirmed_at, r.cancelled_at, r.cancel_reason,
	r.rebooked_from_id, r.notes, r.created_at, r.updated_at`

const deDaReserva = `
	  FROM reservations r
	  JOIN unit_types ut ON ut.id = r.unit_type_id
	  JOIN contacts    c ON c.id  = r.contact_id
	  LEFT JOIN reservation_pricing p ON p.reservation_id = r.id`

func lerReserva(linha pgx.Row) (Reserva, error) {
	var v Reserva
	err := linha.Scan(&v.ID, &v.Codigo, &v.Status, &v.UnitTypeID, &v.UnitTypeNome,
		&v.ContactID, &v.ContactNome, &v.BrokerID, &v.Origem,
		&v.CheckIn, &v.CheckOut, &v.Noites, &v.Hospedes, &v.IsEvento, &v.TipoDeEvento,
		&v.Subtotal, &v.DescontoPct, &v.Desconto, &v.Limpeza, &v.CaucaoEvento,
		&v.Total, &v.Sinal,
		&v.RateTableID, &v.PolicyVersion, &v.CancelPolicyID,
		&v.HoldExpiraEm, &v.ConfirmadaEm, &v.CanceladaEm, &v.MotivoDoCanc,
		&v.RemarcadaDe, &v.Observacoes, &v.CriadaEm, &v.AtualizadaEm)
	if err != nil {
		return v, err
	}
	// O saldo é derivado, não armazenado: `total − sinal`. Guardá-lo numa
	// coluna criaria um terceiro número para manter em dia com os outros dois.
	v.Saldo = v.Total - v.Sinal
	return v, nil
}

// Buscar devolve a reserva com as unidades alocadas.
func (r *Repository) Buscar(ctx context.Context, propriedade, id uuid.UUID, somenteMinhas bool, usuario uuid.UUID) (Reserva, error) {
	q := `SELECT ` + colunasDaReserva + deDaReserva + `
		 WHERE r.property_id = $1 AND r.id = $2
		   AND ($3::boolean = false OR r.owner_id = $4::uuid)`

	v, err := lerReserva(r.exec(ctx).QueryRow(ctx, q, propriedade, id, somenteMinhas, usuario))
	if errors.Is(err, pgx.ErrNoRows) {
		return v, apperr.NotFound("Reserva")
	}
	if err != nil {
		return v, db.MapError(err)
	}

	unidades, err := r.UnidadesDe(ctx, []uuid.UUID{id})
	if err != nil {
		return v, err
	}
	v.Unidades = unidades[id]
	return v, nil
}

// Listar devolve a página e o total.
func (r *Repository) Listar(ctx context.Context, propriedade uuid.UUID, f Filtro) ([]Reserva, int64, error) {
	// `to` é EXCLUSIVO como toda janela do sistema: uma reserva toca [De, Ate)
	// quando começa antes de Ate e termina depois de De.
	where := `
		 WHERE r.property_id = $1
		   AND ($2::text[] IS NULL OR r.status = ANY($2::text[]))
		   AND ($3::date IS NULL OR r.check_out > $3::date)
		   AND ($4::date IS NULL OR r.check_in  < $4::date)
		   AND ($5::text = '' OR r.code ILIKE '%' || $5 || '%' OR c.name ILIKE '%' || $5 || '%')
		   AND ($6::uuid IS NULL OR r.unit_type_id = $6::uuid)
		   AND ($7::uuid IS NULL OR r.contact_id  = $7::uuid)
		   AND ($8::uuid IS NULL OR r.broker_id   = $8::uuid)
		   AND ($9::boolean = false OR r.owner_id = $10::uuid)`

	args := []any{
		propriedade, textoOuNil(f.Status), dataOuNil(f.De), dataOuNil(f.Ate), f.Busca,
		f.UnitTypeID, f.ContactID, f.BrokerID, f.SomenteMinhas, f.Usuario,
	}

	var total int64
	if err := r.exec(ctx).QueryRow(ctx, `SELECT count(*)`+deDaReserva+where, args...).Scan(&total); err != nil {
		return nil, 0, db.MapError(err)
	}
	if total == 0 {
		return nil, 0, nil
	}

	ordem := f.OrderBy
	if ordem == "" {
		ordem = OrdemPadrao
	}
	// Desempate determinístico: sem ele, duas páginas seguidas podem repetir ou
	// pular uma linha quando várias reservas nascem no mesmo instante.
	q := fmt.Sprintf(`SELECT %s%s%s ORDER BY %s, r.id LIMIT $11 OFFSET $12`,
		colunasDaReserva, deDaReserva, where, ordem)

	linhas, err := r.exec(ctx).Query(ctx, q, append(args, f.PorPagina, (f.Pagina-1)*f.PorPagina)...)
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	var (
		out []Reserva
		ids []uuid.UUID
	)
	for linhas.Next() {
		v, err := lerReserva(linhas)
		if err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, v)
		ids = append(ids, v.ID)
	}
	if err := linhas.Err(); err != nil {
		return nil, 0, db.MapError(err)
	}

	// Uma consulta para as unidades da página inteira, nunca uma por reserva.
	unidades, err := r.UnidadesDe(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	for i := range out {
		out[i].Unidades = unidades[out[i].ID]
	}
	return out, total, nil
}

// UnidadesDe devolve as unidades alocadas de várias reservas, na ordem de
// `units.code` — a mesma ordem em que elas foram travadas na inserção.
func (r *Repository) UnidadesDe(ctx context.Context, reservas []uuid.UUID) (map[uuid.UUID][]UnidadeAlocada, error) {
	out := map[uuid.UUID][]UnidadeAlocada{}
	if len(reservas) == 0 {
		return out, nil
	}

	const q = `
		SELECT ru.reservation_id, u.id, u.code, u.name, ru.stay_block_id, ru.locked
		  FROM reservation_units ru
		  JOIN units u ON u.id = ru.unit_id
		 WHERE ru.reservation_id = ANY($1::uuid[])
		 ORDER BY ru.reservation_id, u.code`

	linhas, err := r.exec(ctx).Query(ctx, q, reservas)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	for linhas.Next() {
		var (
			reserva uuid.UUID
			u       UnidadeAlocada
		)
		if err := linhas.Scan(&reserva, &u.UnitID, &u.UnitCode, &u.UnitName, &u.StayBlockID, &u.Travada); err != nil {
			return nil, db.MapError(err)
		}
		out[reserva] = append(out[reserva], u)
	}
	return out, db.MapError(linhas.Err())
}

// NoitesDe devolve o snapshot noite a noite, em ordem cronológica.
func (r *Repository) NoitesDe(ctx context.Context, reserva uuid.UUID) ([]NoiteDaReserva, error) {
	const q = `
		SELECT night::text, date_type, price_cents
		  FROM reservation_nights WHERE reservation_id = $1 ORDER BY night`

	linhas, err := r.exec(ctx).Query(ctx, q, reserva)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	var out []NoiteDaReserva
	for linhas.Next() {
		var (
			n    NoiteDaReserva
			tipo string
		)
		if err := linhas.Scan(&n.Noite, &tipo, &n.Preco); err != nil {
			return nil, db.MapError(err)
		}
		n.Tipo = calendar.DateType(tipo)
		out = append(out, n)
	}
	return out, db.MapError(linhas.Err())
}

// HospedesDe devolve a rooming list, titular primeiro.
func (r *Repository) HospedesDe(ctx context.Context, reserva uuid.UUID) ([]HospedeDaReserva, error) {
	const q = `
		SELECT c.id, c.name, c.phone_e164, c.email, g.is_lead_guest
		  FROM reservation_guests g
		  JOIN contacts c ON c.id = g.contact_id
		 WHERE g.reservation_id = $1
		 ORDER BY g.is_lead_guest DESC, c.name`

	linhas, err := r.exec(ctx).Query(ctx, q, reserva)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	var out []HospedeDaReserva
	for linhas.Next() {
		var h HospedeDaReserva
		if err := linhas.Scan(&h.ContactID, &h.Nome, &h.Telefone, &h.Email, &h.Titular); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, h)
	}
	return out, db.MapError(linhas.Err())
}

// tetoDaTimeline limita a linha do tempo devolvida pelo /full. Uma reserva
// muito remexida acumula eventos, e a tela mostra os últimos.
const tetoDaTimeline = 200

// TimelineDe devolve os eventos do mais recente para o mais antigo.
func (r *Repository) TimelineDe(ctx context.Context, reserva uuid.UUID) ([]EventoDaReserva, error) {
	const q = `
		SELECT id, type, payload, actor_id, at
		  FROM reservation_events WHERE reservation_id = $1
		 ORDER BY at DESC, id DESC LIMIT $2`

	linhas, err := r.exec(ctx).Query(ctx, q, reserva, tetoDaTimeline)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	var out []EventoDaReserva
	for linhas.Next() {
		var (
			e     EventoDaReserva
			bruto []byte
		)
		if err := linhas.Scan(&e.ID, &e.Tipo, &bruto, &e.AutorID, &e.Instante); err != nil {
			return nil, db.MapError(err)
		}
		if len(bruto) > 0 {
			_ = json.Unmarshal(bruto, &e.Payload)
		}
		out = append(out, e)
	}
	return out, db.MapError(linhas.Err())
}

// AgruparEmLinhas monta o "Fim de semana 2× R$ 4.800" a partir do snapshot.
//
// Agrupa o que FOI gravado, e não recalcula nada: o rótulo sai do tipo de data,
// e o valor unitário é o preço da primeira noite do grupo — que é o mesmo de
// todas, porque a tarifa é por (produto, tipo de data).
func AgruparEmLinhas(noites []NoiteDaReserva) []LinhaDoOrcamento {
	var (
		out    []LinhaDoOrcamento
		indice = map[calendar.DateType]int{}
	)
	for _, n := range noites {
		if i, visto := indice[n.Tipo]; visto {
			out[i].Noites++
			out[i].Subtotal += n.Preco
			continue
		}
		indice[n.Tipo] = len(out)
		out = append(out, LinhaDoOrcamento{
			Tipo: n.Tipo, Rotulo: rotuloDoTipo(n.Tipo), Noites: 1,
			Unitario: n.Preco, Subtotal: n.Preco,
		})
	}
	return out
}

// rotuloDoTipo traduz o tipo de data para o texto que a tela mostra.
//
// O rótulo original ("Natal", "Réveillon 2026/2027") vive no calendário e NÃO é
// gravado em `reservation_nights` — a coluna guarda o tipo, não o nome do
// feriado. Rótulo genérico é honesto; inventar o nome do feriado na leitura
// seria recalcular o passado com o calendário de hoje.
func rotuloDoTipo(t calendar.DateType) string {
	switch t {
	case calendar.Normal:
		return "Diária normal"
	case calendar.Weekend:
		return "Fim de semana"
	case calendar.Holiday:
		return "Feriado"
	case calendar.HighSeason:
		return "Alta temporada"
	case calendar.NewYear:
		return "Réveillon"
	case calendar.Carnival:
		return "Carnaval"
	default:
		return string(t)
	}
}

// ─────────────────────────── Auxiliares ─────────────────────────────

func textoOuNil(v []string) any {
	if len(v) == 0 {
		return nil
	}
	return v
}

func dataOuNil(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

// centavos converte money.Cents para o int64 que o schema usa. Existe para o
// service não espalhar conversões e para a intenção ficar legível.
func centavos(c money.Cents) int64 { return int64(c) }
