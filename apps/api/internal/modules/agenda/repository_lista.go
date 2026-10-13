package agenda

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	regra "github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/agenda"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// A LISTA é um `UNION ALL` de duas fontes, no mesmo formato:
//
//   - os eventos GRAVADOS (`agenda_events`, origens `reservation` e `manual`);
//   - a manutenção e o bloqueio de unidade, LIDOS de `stay_blocks` na hora
//     (`confirmed`, origens `maintenance`, `owner_hold` e `ota`) — sem cópia.
//
// O derivado não tem instante no banco: é um período de DATAS da casa. A
// conversão para instante é do Go (`calendar.Zone.StartOfDay`), e por isso a
// ordenação por início e a paginação acontecem DEPOIS da consulta, sobre a
// janela inteira — que o contrato limita a 92 dias. O ESCOPO, não: `own` é
// `assignee_id = <ator>` NO SQL, e o ramo derivado nem entra (não tem
// responsável), de modo que `meta.total` é a contagem do escopo.

// linhaDaLista é uma linha do `UNION ALL`, antes do enriquecimento.
type linhaDaLista struct {
	Derivado        bool
	ID              uuid.UUID
	Tipo            string
	Origem          string
	Status          string
	Titulo          *string
	Inicio, Fim     time.Time
	De, Ate         string // só no derivado: o período de noites [De, Ate)
	DiaInteiro      bool
	UnidadeID       *uuid.UUID
	ReservaID       *uuid.UUID
	ReservaCodigo   *string
	ContatoID       *uuid.UUID
	ContatoNome     *string
	ResponsavelID   *uuid.UUID
	ResponsavelNome *string
	Notas           *string
	OrdemID         *uuid.UUID
	Nota            *string // só no derivado: a nota do bloqueio
	ConcluidoEm     *time.Time
	ConcluidoPor    *uuid.UUID
	CanceladoEm     *time.Time
	CriadoPor       *uuid.UUID
	CriadoEm        *time.Time
	AtualizadoEm    *time.Time
}

// consultaDaLista é o recorte já resolvido em instantes pelo service.
type consultaDaLista struct {
	FiltroDeEventos
	// Responsavel não nulo é o escopo `own`: só o ramo gravado, com
	// `assignee_id = <ator>`.
	Responsavel *uuid.UUID
	// InicioDaJanela e FimDaJanela são `[De, Ate)` convertidos por
	// `calendar.Zone` — a meia-noite DA CASA de cada ponta.
	InicioDaJanela, FimDaJanela *time.Time
	// SoOEvento restringe ao evento gravado `id` (o detalhe e a releitura
	// depois de uma escrita).
	SoOEvento *uuid.UUID
}

// argumentos acumula parâmetros numerados.
type argumentos struct{ valores []any }

func (a *argumentos) prox(v any) string {
	a.valores = append(a.valores, v)
	return fmt.Sprintf("$%d", len(a.valores))
}

const selecaoGravada = `
	SELECT false, ae.id, ae.type, ae.origin, ae.status, ae.title,
	       ae.starts_at, ae.ends_at, NULL::text, NULL::text, ae.all_day,
	       ae.unit_id, ae.reservation_id, r.code,
	       CASE WHEN ae.origin = 'reservation' THEN r.contact_id ELSE ae.contact_id END,
	       c.name, ae.assignee_id, ua.name, ae.notes,
	       NULL::uuid, NULL::text,
	       ae.done_at, ae.done_by, ae.cancelled_at, ae.created_by, ae.created_at, ae.updated_at
	  FROM agenda_events ae
	  LEFT JOIN reservations r ON r.id = ae.reservation_id
	  LEFT JOIN contacts c ON c.id = CASE WHEN ae.origin = 'reservation' THEN r.contact_id ELSE ae.contact_id END
	  LEFT JOIN users ua ON ua.id = ae.assignee_id`

const selecaoDerivada = `
	SELECT true, sb.id,
	       CASE WHEN sb.source = 'maintenance' THEN 'manutencao' ELSE 'bloqueio' END,
	       CASE WHEN sb.source = 'maintenance' THEN 'maintenance' ELSE 'stay_block' END,
	       'agendado', mo.title,
	       NULL::timestamptz, NULL::timestamptz, lower(sb.period)::text, upper(sb.period)::text, true,
	       sb.unit_id, NULL::uuid, NULL::text, NULL::uuid, NULL::text, NULL::uuid, NULL::text, NULL::text,
	       mo.id, sb.note,
	       NULL::timestamptz, NULL::uuid, NULL::timestamptz, NULL::uuid, NULL::timestamptz, NULL::timestamptz
	  FROM stay_blocks sb
	  LEFT JOIN maintenance_orders mo ON mo.stay_block_id = sb.id`

// Linhas devolve as linhas da janela (ou do evento), SEM ordenar nem paginar.
func (r *Repository) Linhas(ctx context.Context, prop uuid.UUID, c consultaDaLista) ([]linhaDaLista, error) {
	args := &argumentos{}
	p := args.prox(prop)

	gravada := []string{"ae.property_id = " + p}
	if c.SoOEvento != nil {
		gravada = append(gravada, "ae.id = "+args.prox(*c.SoOEvento))
	}
	if c.InicioDaJanela != nil && c.FimDaJanela != nil {
		gravada = append(gravada, fmt.Sprintf("tstzrange(ae.starts_at, ae.ends_at, '[)') && tstzrange(%s, %s, '[)')",
			args.prox(*c.InicioDaJanela), args.prox(*c.FimDaJanela)))
	}
	if len(c.Tipos) > 0 {
		gravada = append(gravada, "ae.type = ANY("+args.prox(textosDosTipos(c.Tipos))+"::text[])")
	}
	if len(c.Status) > 0 {
		gravada = append(gravada, "ae.status = ANY("+args.prox(textosDosStatus(c.Status))+"::text[])")
	}
	if c.UnidadeID != nil {
		u := args.prox(*c.UnidadeID)
		gravada = append(gravada, fmt.Sprintf(`(ae.unit_id = %[1]s OR (ae.unit_id IS NULL AND EXISTS (
			SELECT 1 FROM reservation_units ru WHERE ru.reservation_id = ae.reservation_id AND ru.unit_id = %[1]s)))`, u))
	}
	if c.ResponsavelID != nil {
		gravada = append(gravada, "ae.assignee_id = "+args.prox(*c.ResponsavelID))
	}
	if c.SemResponsavel != nil {
		if *c.SemResponsavel {
			gravada = append(gravada, "ae.assignee_id IS NULL")
		} else {
			gravada = append(gravada, "ae.assignee_id IS NOT NULL")
		}
	}
	if c.ReservaID != nil {
		gravada = append(gravada, "ae.reservation_id = "+args.prox(*c.ReservaID))
	}
	if c.Responsavel != nil {
		gravada = append(gravada, "ae.assignee_id = "+args.prox(*c.Responsavel))
	}

	q := selecaoGravada + " WHERE " + strings.Join(gravada, " AND ")

	if fontes, ok := c.fontesDerivadas(); ok {
		derivada := []string{
			"sb.property_id = " + p,
			"sb.status = 'confirmed'",
			"sb.source = ANY(" + args.prox(fontes) + "::text[])",
			fmt.Sprintf("sb.period && daterange(%s::date, %s::date, '[)')", args.prox(c.De.String()), args.prox(c.Ate.String())),
		}
		if c.UnidadeID != nil {
			derivada = append(derivada, "sb.unit_id = "+args.prox(*c.UnidadeID))
		}
		q += " UNION ALL " + selecaoDerivada + " WHERE " + strings.Join(derivada, " AND ")
	}

	linhas, err := r.exec(ctx).Query(ctx, q, args.valores...)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []linhaDaLista{}
	for linhas.Next() {
		var (
			l           linhaDaLista
			inicio, fim *time.Time
			de, ate     *string
		)
		if err := linhas.Scan(&l.Derivado, &l.ID, &l.Tipo, &l.Origem, &l.Status, &l.Titulo,
			&inicio, &fim, &de, &ate, &l.DiaInteiro,
			&l.UnidadeID, &l.ReservaID, &l.ReservaCodigo, &l.ContatoID, &l.ContatoNome,
			&l.ResponsavelID, &l.ResponsavelNome, &l.Notas, &l.OrdemID, &l.Nota,
			&l.ConcluidoEm, &l.ConcluidoPor, &l.CanceladoEm, &l.CriadoPor, &l.CriadoEm, &l.AtualizadoEm); err != nil {
			return nil, db.MapError(err)
		}
		if inicio != nil && fim != nil {
			l.Inicio, l.Fim = *inicio, *fim
		}
		if de != nil && ate != nil {
			l.De, l.Ate = *de, *ate
		}
		out = append(out, l)
	}
	return out, db.MapError(linhas.Err())
}

// fontesDerivadas diz quais origens de `stay_blocks` entram, e se o ramo
// derivado entra. Fica de fora quando o recorte não o alcança: escopo `own` e
// filtro por responsável (o derivado não tem), filtro por reserva, `unassigned
// =false`, status sem `agendado`, tipos sem `manutencao` nem `bloqueio`, e o
// detalhe de um evento gravado.
func (c consultaDaLista) fontesDerivadas() ([]string, bool) {
	if c.Responsavel != nil || c.ResponsavelID != nil || c.ReservaID != nil || c.SoOEvento != nil {
		return nil, false
	}
	if c.De == nil || c.Ate == nil {
		return nil, false
	}
	if c.SemResponsavel != nil && !*c.SemResponsavel {
		return nil, false
	}
	agendado := len(c.Status) == 0
	for _, s := range c.Status {
		if s == regra.Scheduled {
			agendado = true
		}
	}
	if !agendado {
		return nil, false
	}
	manutencao, bloqueio := len(c.Tipos) == 0, len(c.Tipos) == 0
	for _, t := range c.Tipos {
		switch t {
		case regra.Maintenance:
			manutencao = true
		case regra.Block:
			bloqueio = true
		}
	}
	fontes := []string{}
	if manutencao {
		fontes = append(fontes, origemDeManutencao)
	}
	if bloqueio {
		fontes = append(fontes, origemDoProprietario, origemDoCanal)
	}
	return fontes, len(fontes) > 0
}

// As origens de `stay_blocks` (o CHECK de `source`, 20260820130000).
const (
	origemDaReserva      = "reservation"
	origemDeManutencao   = "maintenance"
	origemDoProprietario = "owner_hold"
	origemDoCanal        = "ota"
)

func textosDosTipos(ts []regra.Type) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = string(t)
	}
	return out
}

func textosDosStatus(ss []regra.Status) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = string(s)
	}
	return out
}

// ─────────────────────────── As unidades ────────────────────────────

// unidadesDasReservas devolve, por reserva, as unidades que ela ocupa hoje
// (`reservation_units`), por código.
func (r *Repository) unidadesDasReservas(ctx context.Context, reservas []uuid.UUID) (map[uuid.UUID][]UnidadeDoEventoDaAgenda, error) {
	out := map[uuid.UUID][]UnidadeDoEventoDaAgenda{}
	if len(reservas) == 0 {
		return out, nil
	}
	linhas, err := r.exec(ctx).Query(ctx, `
		SELECT ru.reservation_id, u.id, u.code
		  FROM reservation_units ru JOIN units u ON u.id = ru.unit_id
		 WHERE ru.reservation_id = ANY($1::uuid[])
		 ORDER BY u.code, u.id`, reservas)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()
	for linhas.Next() {
		var (
			reserva uuid.UUID
			u       UnidadeDoEventoDaAgenda
		)
		if err := linhas.Scan(&reserva, &u.ID, &u.Codigo); err != nil {
			return nil, db.MapError(err)
		}
		out[reserva] = append(out[reserva], u)
	}
	return out, db.MapError(linhas.Err())
}

// codigosDasUnidades devolve id → código.
func (r *Repository) codigosDasUnidades(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(ids) == 0 {
		return out, nil
	}
	linhas, err := r.exec(ctx).Query(ctx, `SELECT id, code FROM units WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()
	for linhas.Next() {
		var (
			id     uuid.UUID
			codigo string
		)
		if err := linhas.Scan(&id, &codigo); err != nil {
			return nil, db.MapError(err)
		}
		out[id] = codigo
	}
	return out, db.MapError(linhas.Err())
}

// ─────────────────────────── As ocupações ───────────────────────────

// Ocupacoes carrega tudo o que OCUPA as `unidades` em torno da `janela`, no
// formato de `regra.Occupancy` — a mesma entrada para recusar a escrita
// (`regra.CheckUnit`) e para mostrar o conflito na leitura
// (`regra.UnitConflicts` → `has_unit_conflict`):
//
//   - a ESTADIA (`stay_blocks` da reserva em `hold` ou `confirmed`), da hora de
//     entrada do primeiro dia à hora de saída do último — pelos horários dos
//     eventos automáticos da reserva, quando existem, e pela configuração
//     quando não (a pré-reserva ainda não tem agenda);
//   - a MANUTENÇÃO e o BLOQUEIO de unidade (as outras origens de
//     `stay_blocks`), o dia inteiro;
//   - todo EVENTO agendado: o automático nas unidades da reserva; o manual na
//     unidade gravada ou, sem ela, nas da reserva ligada.
//
// As datas são lidas com um dia de folga dos dois lados (a estadia que sai na
// manhã do dia ainda ocupa a unidade até a hora de saída), e quem decide a
// sobreposição exata, em instantes, é o domínio.
func (r *Repository) Ocupacoes(ctx context.Context, prop uuid.UUID, unidades []uuid.UUID, janela regra.Window,
	s regra.Settings, z calendar.Zone) ([]regra.Occupancy, error) {
	if len(unidades) == 0 || !janela.Valid() {
		return nil, nil
	}
	de := z.DateOf(janela.Start).AddDays(-1).String()
	ate := z.DateOf(janela.End).AddDays(1).String()
	out := []regra.Occupancy{}

	// 1. A estadia.
	estadias, err := r.exec(ctx).Query(ctx, `
		SELECT sb.unit_id, sb.reservation_id, lower(sb.period)::text, upper(sb.period)::text,
		       (SELECT ci.starts_at FROM agenda_events ci
		         WHERE ci.reservation_id = sb.reservation_id AND ci.origin = 'reservation'
		           AND ci.type = 'checkin' AND ci.status <> 'cancelado'),
		       (SELECT co.ends_at FROM agenda_events co
		         WHERE co.reservation_id = sb.reservation_id AND co.origin = 'reservation'
		           AND co.type = 'checkout' AND co.status <> 'cancelado')
		  FROM stay_blocks sb
		 WHERE sb.property_id = $1 AND sb.unit_id = ANY($2::uuid[])
		   AND sb.source = $3 AND sb.reservation_id IS NOT NULL
		   AND sb.status IN ('hold', 'confirmed')
		   AND sb.period && daterange($4::date, $5::date, '[)')`,
		prop, unidades, origemDaReserva, de, ate)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer estadias.Close()
	for estadias.Next() {
		var (
			unidade, reserva uuid.UUID
			entrada, saida   string
			comeca, termina  *time.Time
		)
		if err := estadias.Scan(&unidade, &reserva, &entrada, &saida, &comeca, &termina); err != nil {
			return nil, db.MapError(err)
		}
		in, out2, err := periodo(entrada, saida)
		if err != nil {
			return nil, apperr.Internal.WithCause(err)
		}
		out = append(out, regra.StayOccupancy(unidade.String(), reserva.String(), in, out2, comeca, termina, s, z))
	}
	if err := estadias.Err(); err != nil {
		return nil, db.MapError(err)
	}
	estadias.Close()

	// 2. A manutenção e o bloqueio de unidade.
	bloqueios, err := r.exec(ctx).Query(ctx, `
		SELECT sb.id, sb.unit_id, sb.source, lower(sb.period)::text, upper(sb.period)::text
		  FROM stay_blocks sb
		 WHERE sb.property_id = $1 AND sb.unit_id = ANY($2::uuid[])
		   AND sb.source = ANY($3::text[]) AND sb.status IN ('hold', 'confirmed')
		   AND sb.period && daterange($4::date, $5::date, '[)')`,
		prop, unidades, []string{origemDeManutencao, origemDoProprietario, origemDoCanal}, de, ate)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer bloqueios.Close()
	for bloqueios.Next() {
		var (
			id, unidade    uuid.UUID
			origem         string
			entrada, saida string
		)
		if err := bloqueios.Scan(&id, &unidade, &origem, &entrada, &saida); err != nil {
			return nil, db.MapError(err)
		}
		from, to, err := periodo(entrada, saida)
		if err != nil {
			return nil, apperr.Internal.WithCause(err)
		}
		tipo := regra.Block
		if origem == origemDeManutencao {
			tipo = regra.Maintenance
		}
		out = append(out, regra.BlockOccupancy(tipo, unidade.String(), id.String(), from, to, z))
	}
	if err := bloqueios.Err(); err != nil {
		return nil, db.MapError(err)
	}
	bloqueios.Close()

	// 3. Os eventos agendados.
	eventos, err := r.exec(ctx).Query(ctx, `
		SELECT ae.id, ae.type, ae.reservation_id, ae.starts_at, ae.ends_at, ocupa.unit_id
		  FROM agenda_events ae
		  JOIN LATERAL (
		        SELECT ae.unit_id AS unit_id WHERE ae.unit_id IS NOT NULL
		        UNION ALL
		        SELECT ru.unit_id FROM reservation_units ru
		         WHERE ae.unit_id IS NULL AND ru.reservation_id = ae.reservation_id
		  ) ocupa ON true
		 WHERE ae.property_id = $1 AND ae.status = 'agendado'
		   AND ocupa.unit_id = ANY($2::uuid[])
		   AND tstzrange(ae.starts_at, ae.ends_at, '[)') && tstzrange($3, $4, '[)')`,
		prop, unidades, janela.Start, janela.End)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer eventos.Close()
	for eventos.Next() {
		var (
			id, unidade uuid.UUID
			tipo        string
			reserva     *uuid.UUID
			inicio, fim time.Time
		)
		if err := eventos.Scan(&id, &tipo, &reserva, &inicio, &fim, &unidade); err != nil {
			return nil, db.MapError(err)
		}
		ref := ""
		if reserva != nil {
			ref = reserva.String()
		}
		out = append(out, regra.EventOccupancy(regra.Type(tipo), unidade.String(), id.String(), ref,
			regra.Window{Start: inicio, End: fim}))
	}
	return out, db.MapError(eventos.Err())
}

// Compromissos carrega o que pode virar AVISO de horário em torno da janela:
// os bloqueios de horário da operação e, com responsável, os outros eventos
// agendados dele (já com a folga dos dois lados — `regra.TimeWarnings` decide).
func (r *Repository) Compromissos(ctx context.Context, prop uuid.UUID, janela regra.Window, folga time.Duration,
	responsavel *uuid.UUID) (bloqueios, agendaDoResponsavel []regra.Busy, err error) {
	if !janela.Valid() {
		return nil, nil, nil
	}
	ler := func(q string, args ...any) ([]regra.Busy, error) {
		linhas, err := r.exec(ctx).Query(ctx, q, args...)
		if err != nil {
			return nil, db.MapError(err)
		}
		defer linhas.Close()
		out := []regra.Busy{}
		for linhas.Next() {
			var (
				id          uuid.UUID
				inicio, fim time.Time
			)
			if err := linhas.Scan(&id, &inicio, &fim); err != nil {
				return nil, db.MapError(err)
			}
			out = append(out, regra.Busy{Ref: id.String(), Window: regra.Window{Start: inicio, End: fim}})
		}
		return out, db.MapError(linhas.Err())
	}
	if bloqueios, err = ler(`
		SELECT id, starts_at, ends_at FROM agenda_blocks
		 WHERE property_id = $1 AND tstzrange(starts_at, ends_at, '[)') && tstzrange($2, $3, '[)')`,
		prop, janela.Start, janela.End); err != nil {
		return nil, nil, err
	}
	if responsavel == nil {
		return bloqueios, nil, nil
	}
	agendaDoResponsavel, err = ler(`
		SELECT id, starts_at, ends_at FROM agenda_events
		 WHERE property_id = $1 AND assignee_id = $2 AND status = 'agendado'
		   AND tstzrange(starts_at, ends_at, '[)') && tstzrange($3, $4, '[)')`,
		prop, *responsavel, janela.Start.Add(-folga), janela.End.Add(folga))
	return bloqueios, agendaDoResponsavel, err
}

// classeDaTravaDaUnidade é a classe do advisory lock (forma de dois inteiros)
// que enfileira, POR UNIDADE, quem marca ou move evento à mão. Distinta da de
// `manutencao` (2026_1009): aquela enfileira quem escreve em `stay_blocks`.
const classeDaTravaDaUnidade int32 = 2026_1013

// TravarUnidades enfileira, por unidade e em ordem fixa (a do id), as escritas
// manuais que vão ocupar as `unidades`. NÃO é ela que decide a sobreposição —
// quem decide continua sendo a constraint `agenda_events_unidade_sem_
// sobreposicao`, no INSERT/UPDATE. Ela existe pelo que a constraint faz sob
// disputa, o mesmo que `manutencao.TravarCalendarioDaUnidade` mediu em
// `stay_blocks`: INSERTs simultâneos de janelas que se cruzam enxergam a tupla
// um do outro em andamento e esperam UM PELO OUTRO; o `lock_timeout` (400 ms,
// `db.New`) derruba todos com 55P03, e a repetição do TxManager os recoloca
// na mesma espera cruzada. Medido em 13/10/2026, sem esta trava: 2 de 10
// execuções de `TestDisputaDeVisitasNaMesmaUnidadeEHora` (seis visitas na
// mesma hora) terminaram com ZERO visitas criadas e seis 409 vindos de 55P03.
// Com ela, a primeira grava e comita, e as outras enxergam a gravada na
// conferência (`regra.CheckUnit`) logo em seguida.
//
// A ordem fixa evita o impasse entre duas escritas que ocupam mais de uma
// unidade (o evento da reserva da casa inteira).
func (r *Repository) TravarUnidades(ctx context.Context, unidades []uuid.UUID) error {
	if !db.EmTransacao(ctx) {
		return apperr.Internal.WithCause(errors.New("agenda: trava das unidades fora de transação"))
	}
	ordenadas := append([]uuid.UUID(nil), unidades...)
	sort.Slice(ordenadas, func(i, j int) bool { return ordenadas[i].String() < ordenadas[j].String() })
	for i, u := range ordenadas {
		if i > 0 && u == ordenadas[i-1] {
			continue
		}
		if _, err := r.exec(ctx).Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2::text))`,
			classeDaTravaDaUnidade, u.String()); err != nil {
			return db.MapError(err)
		}
	}
	return nil
}

// periodo lê o par de datas de um `daterange`.
func periodo(de, ate string) (calendar.Date, calendar.Date, error) {
	from, err := calendar.Parse(de)
	if err != nil {
		return calendar.Date{}, calendar.Date{}, fmt.Errorf("agenda: início do período ilegível %q: %w", de, err)
	}
	to, err := calendar.Parse(ate)
	if err != nil {
		return calendar.Date{}, calendar.Date{}, fmt.Errorf("agenda: fim do período ilegível %q: %w", ate, err)
	}
	return from, to, nil
}
