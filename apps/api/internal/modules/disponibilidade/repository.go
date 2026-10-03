package disponibilidade

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/booking"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/money"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// OS DOIS PREDICADOS. Desde que a migration 20260826120000 criou o estado
// terminal `completed`, "ocupa o inventário" e "aparece no mapa" DEIXARAM de ser
// a mesma pergunta — e usar um no lugar do outro é o defeito, nos dois sentidos:
//
//	`completed` DENTRO do predicado de venda  → o check-out trava a noite
//	                                            seguinte (estoque some).
//	`completed` FORA do predicado de exibição → a estadia cumprida evapora do
//	                                            mapa (ocupação, ADR e RevPAR
//	                                            contam zero noite numa estadia
//	                                            consumada e faturada).
//
// Repetir as listas aqui não duplica regra: a regra continua sendo a constraint
// `stay_no_overlap`, que é quem RECUSA a gravação. Estas são filtros de LEITURA,
// e casam com os índices que o banco já mantém — o parcial da constraint para o
// primeiro, `stay_blocks_ocupacao_idx` para o segundo.
//
// Não vêm de `reservas.StatusQueBloqueiam`/`StatusVisiveisNoMapa` por
// impedimento de compilação, não por escolha: `reservas` importa este pacote, e
// importá-lo de volta fecha um ciclo. As listas são as mesmas, e é por isso que
// TestOsPredicadosDeVendaEDeExibicaoNaoSaoOMesmo existe.
var (
	// statusQueBloqueiam é o inventário VENDÁVEL: é literalmente a cláusula
	// WHERE da constraint. Bloco `cancelled`/`expired`/`completed` fica de
	// fora — a data está livre para vender de novo.
	statusQueBloqueiam = []string{"hold", "confirmed"}

	// statusVisiveisNoMapa é o HISTÓRICO: acrescenta a estadia consumada, que
	// não impede venda nenhuma mas precisa aparecer no mapa.
	statusVisiveisNoMapa = []string{"hold", "confirmed", "completed"}
)

// blocoConcluido é o status de `stay_blocks` gravado pelo /check-out. Fica como
// constante porque o mapa precisa distingui-lo dos ativos duas vezes: para
// escolher a célula quando os dois coexistem (SQL) e para nomear o status da
// célula no contrato (statusDaCelula).
const blocoConcluido = "completed"

// Contexto é o estado comercial vigente: que dia é hoje na casa, qual tabela de
// tarifas vale e qual política está em vigor.
type Contexto struct {
	Hoje        calendar.Date
	RateTableID uuid.UUID
	Politica    booking.Policy
}

// Produto é o `unit_types` cru — vira booking.Product depois de receber as
// tarifas.
type Produto struct {
	ID     uuid.UUID
	Codigo string
	Nome   string
	// NomePublico é o nome de vitrine (`unit_types.public_name`), o que o site
	// mostra; `Nome` é o interno, o que gestor e corretor leem ("AP 01 — Duplex
	// Aurora" contra "Duplex Aurora"). Sem nome de vitrine cadastrado, é o Nome.
	NomePublico string
	Capacidade  int
	Consome     string
	LimpezaCent money.Cents
}

// ChaveDia indexa a ocupação por produto e dia.
type ChaveDia struct {
	Produto uuid.UUID
	Dia     string // ISO, a mesma chave que sai do banco como texto
}

// Contagem é o estado da composição de um produto num dia.
//
// Os três números são diferentes de propósito. `Declaradas` é o que o produto
// PROMETE; `Ativas` é o que existe de pé; `Ocupadas` é o que já foi vendido
// dentre as ativas. Colapsar declarada e ativa num "total" só foi o defeito
// medido: a Completa aparecia com 7 unidades e `available: 1` enquanto a venda
// respondia 422 COMPOSITION_INCOMPLETE.
type Contagem struct{ Declaradas, Ativas, Ocupadas int }

// CelulaBruta é uma linha da matriz unidade × dia, antes de virar contrato.
type CelulaBruta struct {
	UnitID   uuid.UUID
	UnitCode string
	UnitName string
	Dia      string

	StayBlockID   *uuid.UUID
	BlocoStatus   *string
	BlocoSource   *string
	ReservaID     *uuid.UUID
	ReservaCodigo *string
	Hospede       *string
	DonoID        *uuid.UUID
}

// Repository lê o estado comercial. Nunca abre transação: pega o executor do
// contexto com db.From, que devolve a transação em curso se houver.
type Repository struct {
	pool db.DBTX
}

func NewRepository(pool db.DBTX) *Repository { return &Repository{pool: pool} }

func (r *Repository) exec(ctx context.Context) db.DBTX { return db.From(ctx, r.pool) }

// Pool devolve o executor de fora da transação. É o que a trilha de auditoria
// exige: `audit_log` é gravado FORA da transação do trabalho, para o registro de
// "alguém tentou" sobreviver ao rollback do que foi tentado.
func (r *Repository) Pool() db.DBTX { return r.pool }

// Contexto resolve, numa consulta só, o "hoje" da casa, a tabela de tarifas
// vigente e a política comercial vigente.
//
// "Hoje" sai do BANCO, com `now() AT TIME ZONE properties.timezone`. O fuso é
// dado (`properties.timezone`), não constante em Go: duas definições de "hoje"
// — uma no processo, outra no banco — divergem na virada do dia e fazem a mesma
// consulta responder com tabelas diferentes conforme quem perguntou.
//
// `tabela` e `versao` não nulos FORÇAM uma tabela/versão específica; é o que
// permite reproduzir um orçamento antigo centavo a centavo.
func (r *Repository) Contexto(ctx context.Context, propriedade uuid.UUID, tabela *uuid.UUID, versao *int) (Contexto, error) {
	const q = `
		WITH hoje AS (
		    SELECT (now() AT TIME ZONE p.timezone)::date AS dia
		      FROM properties p
		     WHERE p.id = $1
		),
		vigente AS (
		    SELECT rt.id
		      FROM rate_tables rt, hoje
		     WHERE rt.property_id = $1
		       AND ( ($2::uuid IS NOT NULL AND rt.id = $2::uuid)
		          OR ($2::uuid IS NULL
		              AND rt.active
		              AND rt.valid_from <= hoje.dia
		              AND (rt.valid_to IS NULL OR rt.valid_to >= hoje.dia)) )
		     ORDER BY rt.valid_from DESC, rt.id
		     LIMIT 1
		),
		politica AS (
		    SELECT cp.version,
		           cp.deposit_pct::float8            AS sinal_pct,
		           cp.balance_due_days,
		           cp.hold_hours,
		           cp.discount_auto_pct::float8      AS auto_pct,
		           cp.discount_approval_pct::float8  AS aprovacao_pct,
		           cp.event_deposit_cents,
		           cp.quote_validity_days
		      FROM commercial_policies cp, hoje
		     WHERE cp.property_id = $1
		       AND ( ($3::int IS NOT NULL AND cp.version = $3::int)
		          OR ($3::int IS NULL AND cp.valid_from <= hoje.dia) )
		     ORDER BY cp.valid_from DESC, cp.version DESC
		     LIMIT 1
		)
		SELECT hoje.dia::text,
		       vigente.id,
		       politica.version, politica.sinal_pct, politica.balance_due_days,
		       politica.hold_hours, politica.auto_pct, politica.aprovacao_pct,
		       politica.event_deposit_cents, politica.quote_validity_days
		  FROM hoje
		  LEFT JOIN vigente  ON true
		  LEFT JOIN politica ON true`

	var (
		c        Contexto
		hoje     string
		rateID   *uuid.UUID
		versaoDB *int
		sinal    *float64
		saldo    *int
		hold     *int
		auto     *float64
		aprov    *float64
		caucao   *int64
		validade *int
	)
	err := r.exec(ctx).QueryRow(ctx, q, propriedade, tabela, versao).
		Scan(&hoje, &rateID, &versaoDB, &sinal, &saldo, &hold, &auto, &aprov, &caucao, &validade)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, apperr.NotFound("Propriedade")
	}
	if err != nil {
		return c, db.MapError(err)
	}

	if c.Hoje, err = deTexto(hoje); err != nil {
		return c, apperr.Internal.WithCause(err)
	}
	if rateID == nil {
		return c, apperr.NotFound("Tabela de tarifas vigente")
	}
	if versaoDB == nil {
		return c, apperr.NotFound("Política comercial vigente")
	}

	c.RateTableID = *rateID
	c.Politica = booking.Policy{
		Version:             *versaoDB,
		DepositPct:          *sinal,
		BalanceDueDays:      *saldo,
		HoldHours:           *hold,
		DiscountAutoPct:     *auto,
		DiscountApprovalPct: *aprov,
		EventDeposit:        money.Cents(*caucao),
		QuoteValidityDays:   *validade,
	}
	return c, nil
}

// Calendario monta o calendário comercial da janela: feriados, períodos e quais
// dias contam como fim de semana.
//
// Só o que TOCA a janela é carregado — carregar o calendário inteiro para
// classificar três noites seria trazer anos de dado para a memória a cada
// orçamento.
func (r *Repository) Calendario(ctx context.Context, propriedade uuid.UUID, j Janela) (calendar.Commercial, error) {
	cal := calendar.Commercial{Holidays: map[string]string{}}

	const qFeriados = `
		SELECT date::text, name
		  FROM holidays
		 WHERE property_id = $1 AND active
		   AND date >= $2::date AND date < $3::date`

	linhas, err := r.exec(ctx).Query(ctx, qFeriados, propriedade, j.De.String(), j.Ate.String())
	if err != nil {
		return cal, db.MapError(err)
	}
	for linhas.Next() {
		var dia, nome string
		if err := linhas.Scan(&dia, &nome); err != nil {
			linhas.Close()
			return cal, db.MapError(err)
		}
		cal.Holidays[dia] = nome
	}
	linhas.Close()
	if err := linhas.Err(); err != nil {
		return cal, db.MapError(err)
	}

	// `special_periods` é INCLUSIVO nas duas pontas (é faixa de calendário, não
	// ocupação), então um período toca a janela half-open [De, Ate) quando
	// começa antes de Ate e termina em De ou depois.
	const qPeriodos = `
		SELECT name, kind, starts_on::text, ends_on::text
		  FROM special_periods
		 WHERE property_id = $1 AND active
		   AND starts_on < $3::date AND ends_on >= $2::date
		 ORDER BY starts_on, name`

	linhas, err = r.exec(ctx).Query(ctx, qPeriodos, propriedade, j.De.String(), j.Ate.String())
	if err != nil {
		return cal, db.MapError(err)
	}
	defer linhas.Close()
	for linhas.Next() {
		var nome, tipo, inicio, fim string
		if err := linhas.Scan(&nome, &tipo, &inicio, &fim); err != nil {
			return cal, db.MapError(err)
		}
		de, err := deTexto(inicio)
		if err != nil {
			return cal, apperr.Internal.WithCause(err)
		}
		ate, err := deTexto(fim)
		if err != nil {
			return cal, apperr.Internal.WithCause(err)
		}
		cal.Periods = append(cal.Periods, calendar.Period{
			Name: nome, Type: calendar.DateType(tipo), From: de, To: ate,
		})
	}
	if err := linhas.Err(); err != nil {
		return cal, db.MapError(err)
	}

	if cal.WeekendDays, err = r.fimDeSemana(ctx); err != nil {
		return cal, err
	}
	return cal, nil
}

// fimDeSemana lê da `date_type_rules` quais dias contam como fim de semana
// comercial. É DADO: na White House o hóspede vai embora no domingo, então
// domingo é diária normal — e mudar isso tem de ser uma linha na tabela, não um
// deploy.
//
// De quebra, confere a precedência do banco contra a do domínio. A precedência
// também é dado (`date_type_rules.precedence`), mas `calendar.Precedence` é um
// mapa de pacote que este módulo NÃO pode reescrever sem virar regra de negócio
// fora de internal/domain. Divergir em silêncio seria pior: a tela mostraria
// uma precedência e o preço sairia por outra. Enquanto o domínio não aceitar a
// precedência por parâmetro (ver relatório), o mínimo honesto é gritar no log.
func (r *Repository) fimDeSemana(ctx context.Context) ([]time.Weekday, error) {
	const q = `SELECT kind, precedence, weekday_mask FROM date_type_rules`

	linhas, err := r.exec(ctx).Query(ctx, q)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	var dias []time.Weekday
	for linhas.Next() {
		var (
			tipo       string
			precedenza int32
			mascara    int32
		)
		if err := linhas.Scan(&tipo, &precedenza, &mascara); err != nil {
			return nil, db.MapError(err)
		}
		if esperada, ok := calendar.Precedence[calendar.DateType(tipo)]; ok && esperada != int(precedenza) {
			slog.WarnContext(ctx, "precedência do banco diverge do domínio",
				"date_type", tipo, "banco", precedenza, "dominio", esperada)
		}
		if mascara != 0 {
			dias = append(dias, diaDaSemana(mascara)...)
		}
	}
	if err := linhas.Err(); err != nil {
		return nil, db.MapError(err)
	}
	return dias, nil
}

// Tarifas devolve produto → tipo de data → valor, da tabela informada.
func (r *Repository) Tarifas(ctx context.Context, tabela uuid.UUID, produto *uuid.UUID) (map[uuid.UUID]map[calendar.DateType]money.Cents, error) {
	const q = `
		SELECT unit_type_id, date_type, amount_cents
		  FROM rates
		 WHERE rate_table_id = $1
		   AND ($2::uuid IS NULL OR unit_type_id = $2::uuid)`

	linhas, err := r.exec(ctx).Query(ctx, q, tabela, produto)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := map[uuid.UUID]map[calendar.DateType]money.Cents{}
	for linhas.Next() {
		var (
			id    uuid.UUID
			tipo  string
			valor int64
		)
		if err := linhas.Scan(&id, &tipo, &valor); err != nil {
			return nil, db.MapError(err)
		}
		if out[id] == nil {
			out[id] = map[calendar.DateType]money.Cents{}
		}
		out[id][calendar.DateType(tipo)] = money.Cents(valor)
	}
	return out, db.MapError(linhas.Err())
}

// EstadiaMinima devolve tipo de data → mínimo de noites, da tabela informada.
func (r *Repository) EstadiaMinima(ctx context.Context, tabela uuid.UUID) (map[calendar.DateType]int, error) {
	const q = `SELECT date_type, nights FROM min_nights_rules WHERE rate_table_id = $1`

	linhas, err := r.exec(ctx).Query(ctx, q, tabela)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := map[calendar.DateType]int{}
	for linhas.Next() {
		var (
			tipo   string
			noites int
		)
		if err := linhas.Scan(&tipo, &noites); err != nil {
			return nil, db.MapError(err)
		}
		out[calendar.DateType(tipo)] = noites
	}
	return out, db.MapError(linhas.Err())
}

// RegrasDoProduto são as regras comerciais PRÓPRIAS de um produto na tabela:
// a estadia mínima que substitui a geral (`unit_type_min_nights`) e os preços
// por duração (`rate_packages`). Produto sem nenhuma das duas não aparece no
// mapa — e o motor cai na regra geral e na diária avulsa.
type RegrasDoProduto struct {
	MinNoites map[calendar.DateType]int
	Pacotes   []booking.Package
}

// Regras lê as regras próprias dos produtos da tabela (ou de um só).
func (r *Repository) Regras(ctx context.Context, tabela uuid.UUID, produto *uuid.UUID) (map[uuid.UUID]RegrasDoProduto, error) {
	out := map[uuid.UUID]RegrasDoProduto{}
	pegar := func(id uuid.UUID) RegrasDoProduto {
		rg, ok := out[id]
		if !ok {
			rg = RegrasDoProduto{MinNoites: map[calendar.DateType]int{}}
		}
		return rg
	}

	linhas, err := r.exec(ctx).Query(ctx, `
		SELECT unit_type_id, date_type, nights
		  FROM unit_type_min_nights
		 WHERE rate_table_id = $1 AND ($2::uuid IS NULL OR unit_type_id = $2::uuid)`, tabela, produto)
	if err != nil {
		return nil, db.MapError(err)
	}
	for linhas.Next() {
		var (
			id     uuid.UUID
			tipo   string
			noites int
		)
		if err := linhas.Scan(&id, &tipo, &noites); err != nil {
			linhas.Close()
			return nil, db.MapError(err)
		}
		rg := pegar(id)
		rg.MinNoites[calendar.DateType(tipo)] = noites
		out[id] = rg
	}
	linhas.Close()
	if err := linhas.Err(); err != nil {
		return nil, db.MapError(err)
	}

	linhas, err = r.exec(ctx).Query(ctx, `
		SELECT unit_type_id, nights, date_types, total_cents
		  FROM rate_packages
		 WHERE rate_table_id = $1 AND ($2::uuid IS NULL OR unit_type_id = $2::uuid)
		 ORDER BY unit_type_id, nights DESC`, tabela, produto)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()
	for linhas.Next() {
		var (
			id     uuid.UUID
			noites int
			tipos  []string
			total  int64
		)
		if err := linhas.Scan(&id, &noites, &tipos, &total); err != nil {
			return nil, db.MapError(err)
		}
		pk := booking.Package{Nights: noites, Total: money.Cents(total)}
		for _, t := range tipos {
			pk.Types = append(pk.Types, calendar.DateType(t))
		}
		rg := pegar(id)
		rg.Pacotes = append(rg.Pacotes, pk)
		out[id] = rg
	}
	return out, db.MapError(linhas.Err())
}

// Produtos devolve os produtos ATIVOS da propriedade, na ordem da vitrine.
//
// Produto inativo fica de fora inclusive quando pedido pelo id: o que está
// desligado não é vendável, e devolver preço para ele convidaria a gestão a
// prometer o que não existe mais.
func (r *Repository) Produtos(ctx context.Context, propriedade uuid.UUID, produto *uuid.UUID) ([]Produto, error) {
	const q = `
		SELECT id, code, name, COALESCE(public_name, name), capacity, consumes, cleaning_fee_cents
		  FROM unit_types
		 WHERE property_id = $1 AND active
		   AND ($2::uuid IS NULL OR id = $2::uuid)
		 ORDER BY sort_order, code`

	linhas, err := r.exec(ctx).Query(ctx, q, propriedade, produto)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	var out []Produto
	for linhas.Next() {
		var (
			p       Produto
			limpeza int64
		)
		if err := linhas.Scan(&p.ID, &p.Codigo, &p.Nome, &p.NomePublico, &p.Capacidade, &p.Consome, &limpeza); err != nil {
			return nil, db.MapError(err)
		}
		p.LimpezaCent = money.Cents(limpeza)
		out = append(out, p)
	}
	return out, db.MapError(linhas.Err())
}

// ComposicaoDoProduto é o estado da composição de UM produto, sem recorte de
// data: o que ele promete, o que está de pé e o que falta reativar.
type ComposicaoDoProduto struct {
	Declaradas int
	Ativas     int
	// Faltando são os códigos das unidades inativas, em ordem. É o que o
	// operador precisa para ter ação: sem "AP-03", um 422 vira "tente outra
	// data" para sempre — e nenhuma data resolve composição quebrada.
	Faltando []string
}

// Composicao lê a composição declarada do produto e quanto dela está ativa.
//
// Serve ao ORÇAMENTO, que precisa recusar antes de precificar. Não substitui a
// conferência do módulo de reservas: lá a checagem e o INSERT são a mesma
// instrução, justamente para não haver instante entre uma coisa e outra. Aqui
// não há gravação nenhuma, então TOCTOU não se aplica — a pergunta é só "dá
// para prometer isto ao hóspede agora?".
func (r *Repository) Composicao(ctx context.Context, propriedade, produto uuid.UUID) (ComposicaoDoProduto, error) {
	const q = `
		SELECT count(*),
		       count(*) FILTER (WHERE u.active),
		       COALESCE(array_agg(u.code ORDER BY u.code) FILTER (WHERE NOT u.active), '{}')
		  FROM unit_type_members m
		  JOIN units u ON u.id = m.unit_id AND u.property_id = $1
		 WHERE m.unit_type_id = $2`

	var c ComposicaoDoProduto
	err := r.exec(ctx).QueryRow(ctx, q, propriedade, produto).
		Scan(&c.Declaradas, &c.Ativas, &c.Faltando)
	return c, db.MapError(err)
}

// Ocupacao devolve, por produto e por dia da janela, o tamanho DECLARADO da
// composição, quantas dessas unidades estão ativas e quantas estão ocupadas.
//
// O predicado aqui é o de VENDA (`statusQueBloqueiam`) — e continua sendo: a
// noite de uma estadia já cumprida está livre para vender de novo, e é o mapa,
// não esta consulta, que guarda a história.
//
// Uma consulta só para toda a janela — nunca um SELECT por dia nem por produto.
//
// O CTE `blocos` recorta `stay_blocks` pela janela ANTES do produto cartesiano,
// e isso não é enfeite: `period && daterange(...)` é o predicado que o índice
// gist da constraint `stay_no_overlap` sabe responder, então o recorte custa um
// índice em vez de uma varredura. Sem ele, o planejador junta 720 células contra
// a tabela inteira e reavalia `period @> dia` linha a linha — medido neste
// repositório com 9.392 blocos: 115 ms sem o recorte, 4 ms com ele.
//
// O `count(DISTINCT)` é cinto de segurança: a constraint `stay_no_overlap` já
// garante no máximo um bloco por unidade e dia, mas se ela algum dia for
// afrouxada, dois blocos na mesma célula fariam `count(*)` contar a unidade
// duas vezes e o mapa passaria a dizer que sobram unidades negativas.
func (r *Repository) Ocupacao(ctx context.Context, propriedade uuid.UUID, j Janela, produto *uuid.UUID) (map[ChaveDia]Contagem, error) {
	const q = `
		WITH dias AS (
		    SELECT d::date AS dia
		      FROM generate_series($2::date, $3::date - 1, interval '1 day') AS d
		),
		membros AS (
		    -- u.active NÃO filtra aqui, e essa é a correção: a unidade
		    -- inativa continua na composição declarada. Filtrá-la fazia a
		    -- Completa aparecer como um produto de 7 unidades, todas livres,
		    -- em vez de um produto de 8 que não pode ser entregue.
		    SELECT m.unit_type_id, m.unit_id, u.active
		      FROM unit_type_members m
		      JOIN units      u ON u.id = m.unit_id      AND u.property_id = $1
		      JOIN unit_types t ON t.id = m.unit_type_id AND t.active AND t.property_id = $1
		     WHERE ($4::uuid IS NULL OR m.unit_type_id = $4::uuid)
		),
		blocos AS (
		    SELECT sb.id, sb.unit_id, sb.period
		      FROM stay_blocks sb
		     WHERE sb.property_id = $1
		       AND sb.status = ANY($5::text[])
		       AND sb.period && daterange($2::date, $3::date, '[)')
		)
		SELECT m.unit_type_id,
		       d.dia::text,
		       count(DISTINCT m.unit_id)                             AS declaradas,
		       count(DISTINCT m.unit_id) FILTER (WHERE m.active)     AS ativas,
		       count(DISTINCT m.unit_id) FILTER (WHERE m.active
		                                           AND sb.id IS NOT NULL) AS ocupadas
		  FROM membros m
		 CROSS JOIN dias d
		  LEFT JOIN blocos sb
		         ON sb.unit_id = m.unit_id
		        AND sb.period @> d.dia
		 GROUP BY m.unit_type_id, d.dia`

	linhas, err := r.exec(ctx).Query(ctx, q,
		propriedade, j.De.String(), j.Ate.String(), produto, statusQueBloqueiam)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := map[ChaveDia]Contagem{}
	for linhas.Next() {
		var (
			chave ChaveDia
			c     Contagem
		)
		if err := linhas.Scan(&chave.Produto, &chave.Dia, &c.Declaradas, &c.Ativas, &c.Ocupadas); err != nil {
			return nil, db.MapError(err)
		}
		out[chave] = c
	}
	return out, db.MapError(linhas.Err())
}

// Mapa devolve a matriz unidade × dia já ordenada por `units.code`, que é a
// ordem em que a tela desenha as linhas — e a mesma ordem que a inserção de
// reserva usa para não gerar impasse.
//
// Mesmo recorte da Ocupacao: `blocos` resolve reserva, código e hóspede uma vez
// por BLOCO (dezenas), não uma vez por CÉLULA (centenas) — e é o `period &&`
// que deixa o índice gist fazer o trabalho.
//
// O PREDICADO AQUI É O DE EXIBIÇÃO (`statusVisiveisNoMapa`), não o de venda. Era
// esse o defeito medido: o /check-out grava `completed`, o mapa filtrava só
// `hold, confirmed`, e três noites vividas e faturadas voltavam como
// `{"status":"livre","reservation_code":null}`.
//
// O `DISTINCT ON` é consequência direta disso. `completed` está FORA da
// constraint `stay_no_overlap`, então o banco permite — e o back-to-back
// produz — uma estadia nova sobre a noite de uma já cumprida: duas linhas de
// `stay_blocks` na mesma unidade e no mesmo dia. Sem desempate a consulta
// devolveria DUAS células para a mesma coordenada, o agrupamento do service
// escreveria 91 dias numa janela de 90 e a tela desalinharia a grade inteira.
// A ordem do desempate é a do contrato: vence o ATIVO, porque o mapa mostra
// quem está na casa, nunca quem já saiu.
func (r *Repository) Mapa(ctx context.Context, propriedade uuid.UUID, j Janela, unidade *uuid.UUID) ([]CelulaBruta, error) {
	const q = `
		WITH dias AS (
		    SELECT d::date AS dia
		      FROM generate_series($2::date, $3::date - 1, interval '1 day') AS d
		),
		blocos AS (
		    SELECT sb.id, sb.unit_id, sb.status, sb.source, sb.period,
		           r.id AS reserva_id, r.code AS reserva_code, r.owner_id, c.name AS hospede
		      FROM stay_blocks sb
		      LEFT JOIN reservations r ON r.id = sb.reservation_id
		      LEFT JOIN contacts     c ON c.id = r.contact_id
		     WHERE sb.property_id = $1
		       AND sb.status = ANY($5::text[])
		       AND sb.period && daterange($2::date, $3::date, '[)')
		)
		SELECT DISTINCT ON (u.code, d.dia)
		       u.id, u.code, u.name, d.dia::text,
		       sb.id, sb.status, sb.source,
		       sb.reserva_id, sb.reserva_code, sb.hospede, sb.owner_id
		  FROM units u
		 CROSS JOIN dias d
		  LEFT JOIN blocos sb ON sb.unit_id = u.id AND sb.period @> d.dia
		 WHERE u.property_id = $1 AND u.active
		   AND ($4::uuid IS NULL OR u.id = $4::uuid)
		 ORDER BY u.code, d.dia, (sb.status = $6::text), sb.id`

	linhas, err := r.exec(ctx).Query(ctx, q,
		propriedade, j.De.String(), j.Ate.String(), unidade, statusVisiveisNoMapa, blocoConcluido)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	var out []CelulaBruta
	for linhas.Next() {
		var c CelulaBruta
		if err := linhas.Scan(&c.UnitID, &c.UnitCode, &c.UnitName, &c.Dia,
			&c.StayBlockID, &c.BlocoStatus, &c.BlocoSource,
			&c.ReservaID, &c.ReservaCodigo, &c.Hospede, &c.DonoID); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, c)
	}
	return out, db.MapError(linhas.Err())
}
