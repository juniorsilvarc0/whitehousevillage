package agenda

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	regra "github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/agenda"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// O repositório NUNCA abre transação: recebe o executor do contexto
// (`db.From`), e a transação nasce e morre no service — ou na reserva, quando
// é ela que chama (`Ligacao`).
//
// Três regras atravessam os arquivos `repository*.go`:
//
//   - NENHUMA conversão entre data da casa e instante acontece em SQL. O fuso
//     sai de `properties.timezone` junto com o `now()` do banco (`Relogio`), e
//     quem converte é `calendar.Zone`, no Go — a única porta. As consultas
//     recebem instantes prontos, ou devolvem datas cruas para o Go converter;
//   - quem decide que duas marcações à mão não ocupam a mesma unidade é a
//     constraint `agenda_events_unidade_sem_sobreposicao`, no próprio
//     INSERT/UPDATE (23P01 → 409 UNIT_NOT_AVAILABLE, nunca retry);
//   - quem decide que a reserva tem no máximo um check-in, um check-out e uma
//     limpeza automáticos é o índice `agenda_events_da_reserva_idx`. No
//     `/confirm` o INSERT vai sem ON CONFLICT (o 23505 ali é defeito, 500); no
//     preenchimento das reservas anteriores à agenda, com ON CONFLICT DO
//     NOTHING (idempotente).

// Repository é o acesso a `agenda_events`, `agenda_blocks`,
// `agenda_settings` e ao que a agenda LÊ da reserva.
type Repository struct {
	pool db.DBTX
}

// NewRepository monta o repositório sobre o pool (ou sobre uma transação que
// não está no contexto — o caso do seed).
func NewRepository(pool db.DBTX) *Repository { return &Repository{pool: pool} }

func (r *Repository) exec(ctx context.Context) db.DBTX { return db.From(ctx, r.pool) }

// Os nomes de constraint que decidem a resposta (migration 20261013120000).
const (
	exclusaoDaUnidade = "agenda_events_unidade_sem_sobreposicao"
	indiceDaReserva   = "agenda_events_da_reserva_idx"
	fkUnidade         = "agenda_events_unit_id_fkey"
	fkReserva         = "agenda_events_reservation_id_fkey"
	fkContato         = "agenda_events_contact_id_fkey"
	fkResponsavel     = "agenda_events_assignee_id_fkey"
	sqlstateExclusao  = "23P01"
	sqlstateCheck     = "23514"
)

// Vocabulário da RESERVA que a agenda lê em SQL. É o de `reservas`
// (`EstadoHold`, `EstadoConfirmada`, `EstadoCheckIn`, `EventoCheckIn`), que
// este pacote não pode importar — `reservas` importa a agenda. O teste do
// módulo de reservas confere as duas cópias (`agenda_test.go` lá).
const (
	ReservaEmHold            = "hold"
	ReservaConfirmada        = "confirmed"
	ReservaHospedada         = "checked_in"
	EventoDeEntradaNaReserva = "checked_in"
)

// estadosQueAceitamVinculo são os estados em que uma reserva pode receber um
// evento manual (contrato: "reserva em hold, confirmed ou checked_in").
var estadosQueAceitamVinculo = []string{ReservaEmHold, ReservaConfirmada, ReservaHospedada}

// ─────────────────────────── O relógio da casa ──────────────────────

// relogio é o fuso da propriedade e o `now()` do banco, lidos numa consulta só
// e na transação de quem chama — a mesma pergunta tem a mesma resposta no
// teste e às 22h de Fortaleza num servidor em UTC.
type relogio struct {
	zona  calendar.Zone
	agora time.Time
}

func (c relogio) hoje() calendar.Date { return c.zona.DateOf(c.agora) }

// Relogio lê `properties.timezone` e `now()`.
func (r *Repository) Relogio(ctx context.Context, prop uuid.UUID) (relogio, error) {
	var (
		fuso  string
		agora time.Time
	)
	err := r.exec(ctx).QueryRow(ctx, `SELECT timezone, now() FROM properties WHERE id = $1`, prop).Scan(&fuso, &agora)
	if errors.Is(err, pgx.ErrNoRows) {
		return relogio{}, apperr.Internal.WithCause(fmt.Errorf("agenda: propriedade %s não existe", prop))
	}
	if err != nil {
		return relogio{}, db.MapError(err)
	}
	zona, err := calendar.LoadZone(fuso)
	if err != nil {
		// O cadastro da propriedade só aceita fuso válido: chegar aqui é dado
		// corrompido, e a agenda não tem relógio sem ele.
		return relogio{}, apperr.Internal.WithCause(err)
	}
	return relogio{zona: zona, agora: agora}, nil
}

// ─────────────────────────── Os horários da casa ────────────────────

// configuracaoGravada é a linha de `agenda_settings`.
type configuracaoGravada struct {
	Regras            regra.Settings
	AtualizadaPor     *uuid.UUID
	AtualizadaPorNome *string
	AtualizadaEm      time.Time
}

// trava é o modo de leitura da configuração.
type trava int

const (
	semTrava trava = iota
	// travaCompartilhada — toda chamada vinda da reserva: um `PUT
	// /agenda/settings` em voo espera ela terminar, e ela espera o PUT —
	// nenhum evento automático nasce com o horário que o PUT acabou de trocar
	// sem ser reprogramado por ele.
	travaCompartilhada
	// travaExclusiva — o próprio PUT, que lê o horário antigo para `Retime`.
	travaExclusiva
)

// intervaloGravado é um item do `business_hours` jsonb.
type intervaloGravado struct {
	DiaDaSemana int    `json:"weekday"`
	Inicio      string `json:"start"`
	Fim         string `json:"end"`
}

// Configuracao lê os horários da casa. Se a linha não existe — propriedade
// criada depois da migration por um caminho que não passou pela etapa
// `agenda_settings` do seed —, ela nasce aqui com os DEFAULTs da migration
// (`ON CONFLICT DO NOTHING`), e não com uma cópia deles em Go: a agenda não
// pode derrubar a venda por falta de uma linha de configuração.
func (r *Repository) Configuracao(ctx context.Context, prop uuid.UUID, modo trava) (configuracaoGravada, error) {
	cfg, achou, err := r.lerConfiguracao(ctx, prop, modo)
	if err != nil || achou {
		return cfg, err
	}
	if _, err := r.exec(ctx).Exec(ctx,
		`INSERT INTO agenda_settings (property_id) VALUES ($1) ON CONFLICT (property_id) DO NOTHING`, prop); err != nil {
		return configuracaoGravada{}, db.MapError(err)
	}
	cfg, achou, err = r.lerConfiguracao(ctx, prop, modo)
	if err == nil && !achou {
		err = apperr.Internal.WithCause(fmt.Errorf("agenda: a configuração de %s não existe nem depois de criada", prop))
	}
	return cfg, err
}

func (r *Repository) lerConfiguracao(ctx context.Context, prop uuid.UUID, modo trava) (configuracaoGravada, bool, error) {
	q := `
		SELECT floor(extract(epoch FROM s.checkin_time) / 60)::int,
		       floor(extract(epoch FROM s.checkout_time) / 60)::int,
		       floor(extract(epoch FROM s.cleaning_start) / 60)::int,
		       floor(extract(epoch FROM s.cleaning_end) / 60)::int,
		       s.business_hours, s.slot_minutes, s.buffer_minutes,
		       s.updated_by, u.name, s.updated_at
		  FROM agenda_settings s
		  LEFT JOIN users u ON u.id = s.updated_by
		 WHERE s.property_id = $1`
	switch modo {
	case travaCompartilhada:
		q += ` FOR SHARE OF s`
	case travaExclusiva:
		q += ` FOR UPDATE OF s`
	}

	var (
		cfg                            configuracaoGravada
		entrada, saida, limpeza, fimLp int
		horario                        []byte
	)
	err := r.exec(ctx).QueryRow(ctx, q, prop).Scan(&entrada, &saida, &limpeza, &fimLp, &horario,
		&cfg.Regras.SlotMinutes, &cfg.Regras.BufferMinutes, &cfg.AtualizadaPor, &cfg.AtualizadaPorNome, &cfg.AtualizadaEm)
	if errors.Is(err, pgx.ErrNoRows) {
		return configuracaoGravada{}, false, nil
	}
	if err != nil {
		return configuracaoGravada{}, false, db.MapError(err)
	}
	cfg.Regras.CheckIn = calendar.Clock(entrada)
	cfg.Regras.CheckOut = calendar.Clock(saida)
	cfg.Regras.CleaningStart = calendar.Clock(limpeza)
	cfg.Regras.CleaningEnd = calendar.Clock(fimLp)
	if cfg.Regras.BusinessHours, err = horarioDoBanco(horario); err != nil {
		return configuracaoGravada{}, false, apperr.Internal.WithCause(err)
	}
	return cfg, true, nil
}

// horarioDoBanco lê o `business_hours`. Hora ilegível é dado corrompido (a API
// só grava `HH:MM`), e sai como erro interno.
func horarioDoBanco(bruto []byte) ([]regra.Hours, error) {
	var lidos []intervaloGravado
	if err := json.Unmarshal(bruto, &lidos); err != nil {
		return nil, fmt.Errorf("agenda: business_hours ilegível: %w", err)
	}
	out := make([]regra.Hours, 0, len(lidos))
	for i, h := range lidos {
		inicio, err := calendar.ParseClock(h.Inicio)
		if err != nil {
			return nil, fmt.Errorf("agenda: business_hours[%d].start: %w", i, err)
		}
		fim, err := calendar.ParseClock(h.Fim)
		if err != nil {
			return nil, fmt.Errorf("agenda: business_hours[%d].end: %w", i, err)
		}
		out = append(out, regra.Hours{Weekday: h.DiaDaSemana, Start: inicio, End: fim})
	}
	return out, nil
}

// horarioParaOBanco serializa o funcionamento em ordem de dia e início — a
// ordem que a resposta promete ("Ordenado por dia e início").
func horarioParaOBanco(hs []regra.Hours) ([]byte, error) {
	ordenados := ordenarHorario(hs)
	out := make([]intervaloGravado, len(ordenados))
	for i, h := range ordenados {
		out[i] = intervaloGravado{DiaDaSemana: h.Weekday, Inicio: h.Start.String(), Fim: h.End.String()}
	}
	return json.Marshal(out)
}

// GravarConfiguracao troca os horários e carimba quem gravou — a partir daqui
// `provisional` é `false`. A coerência já passou por `regra.Settings.Validate`;
// um CHECK recusando aqui é defeito (as duas cópias da desigualdade
// divergiram), e sai 500.
func (r *Repository) GravarConfiguracao(ctx context.Context, prop uuid.UUID, s regra.Settings, autor *uuid.UUID) error {
	horario, err := horarioParaOBanco(s.BusinessHours)
	if err != nil {
		return apperr.Internal.WithCause(err)
	}
	_, err = r.exec(ctx).Exec(ctx, `
		UPDATE agenda_settings
		   SET checkin_time = $2::time, checkout_time = $3::time,
		       cleaning_start = $4::time, cleaning_end = $5::time,
		       business_hours = $6::jsonb, slot_minutes = $7, buffer_minutes = $8,
		       updated_by = $9, updated_at = now()
		 WHERE property_id = $1`,
		prop, s.CheckIn.String(), s.CheckOut.String(), s.CleaningStart.String(), s.CleaningEnd.String(),
		string(horario), s.SlotMinutes, s.BufferMinutes, autor)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == sqlstateCheck {
		return apperr.Internal.WithCause(err)
	}
	return db.MapError(err)
}

// ─────────────────────────── O evento gravado ───────────────────────

// eventoGravado é a linha de `agenda_events` como está no banco. As tags `json`
// são os nomes da API: é este struct (com os instantes em UTC, ver
// `auditavel`) que vai para `audit_log`.
type eventoGravado struct {
	ID            uuid.UUID  `json:"id"`
	PropriedadeID uuid.UUID  `json:"-"`
	Tipo          string     `json:"type"`
	Origem        string     `json:"origin"`
	ReservaID     *uuid.UUID `json:"reservation_id"`
	UnidadeID     *uuid.UUID `json:"unit_id"`
	ContatoID     *uuid.UUID `json:"contact_id"`
	ResponsavelID *uuid.UUID `json:"assignee_id"`
	Titulo        *string    `json:"title"`
	Inicio        time.Time  `json:"starts_at"`
	Fim           time.Time  `json:"ends_at"`
	DiaInteiro    bool       `json:"all_day"`
	Status        string     `json:"status"`
	Notas         *string    `json:"notes"`
	ConcluidoEm   *time.Time `json:"done_at"`
	ConcluidoPor  *uuid.UUID `json:"done_by"`
	CanceladoEm   *time.Time `json:"cancelled_at"`
}

func (e eventoGravado) janela() regra.Window { return regra.Window{Start: e.Inicio, End: e.Fim} }

// fatos é o evento como o domínio o enxerga.
func (e eventoGravado) fatos(z calendar.Zone) regra.Facts {
	return regra.Facts{
		Type: regra.Type(e.Tipo), Origin: regra.Origin(e.Origem), Status: regra.Status(e.Status),
		Date: z.DateOf(e.Inicio),
	}
}

// ligado é o evento como as regras da reserva o enxergam.
func (e eventoGravado) ligado() regra.Linked {
	l := regra.Linked{
		Type: regra.Type(e.Tipo), Origin: regra.Origin(e.Origem), Status: regra.Status(e.Status),
		Window: e.janela(),
	}
	if e.UnidadeID != nil {
		l.UnitRef = e.UnidadeID.String()
	}
	return l
}

// auditavel normaliza os instantes em UTC: o pgx devolve o `timestamptz` no
// fuso do processo, e o `audit.Diff` compara TEXTO — o mesmo instante em dois
// fusos pareceria mudança.
func (e eventoGravado) auditavel() eventoGravado {
	e.Inicio, e.Fim = e.Inicio.UTC(), e.Fim.UTC()
	e.ConcluidoEm = emUTC(e.ConcluidoEm)
	e.CanceladoEm = emUTC(e.CanceladoEm)
	return e
}

func emUTC(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

const colunasDoEvento = `ae.id, ae.property_id, ae.type, ae.origin, ae.reservation_id, ae.unit_id, ae.contact_id,
	ae.assignee_id, ae.title, ae.starts_at, ae.ends_at, ae.all_day, ae.status, ae.notes,
	ae.done_at, ae.done_by, ae.cancelled_at`

func escanearEvento(l pgx.Row) (eventoGravado, error) {
	var e eventoGravado
	err := l.Scan(&e.ID, &e.PropriedadeID, &e.Tipo, &e.Origem, &e.ReservaID, &e.UnidadeID, &e.ContatoID,
		&e.ResponsavelID, &e.Titulo, &e.Inicio, &e.Fim, &e.DiaInteiro, &e.Status, &e.Notas,
		&e.ConcluidoEm, &e.ConcluidoPor, &e.CanceladoEm)
	return e, err
}

func escanearEventos(linhas pgx.Rows) ([]eventoGravado, error) {
	defer linhas.Close()
	out := []eventoGravado{}
	for linhas.Next() {
		e, err := escanearEvento(linhas)
		if err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, e)
	}
	return out, db.MapError(linhas.Err())
}

// TravarEvento lê o evento com FOR UPDATE — a base de toda escrita: a decisão
// do domínio é tomada sobre o estado travado, e duas ações simultâneas
// (concluir × desmarcar) se enfileiram aqui.
//
// `responsavel` não nulo é o escopo `own` aplicado NO SQL: o evento de outro
// responsável (ou sem responsável) é 404, nunca 403 — "existe, mas não é seu"
// já vaza que existe.
func (r *Repository) TravarEvento(ctx context.Context, prop, id uuid.UUID, responsavel *uuid.UUID) (eventoGravado, error) {
	e, err := escanearEvento(r.exec(ctx).QueryRow(ctx, `
		SELECT `+colunasDoEvento+`
		  FROM agenda_events ae
		 WHERE ae.id = $1 AND ae.property_id = $2
		   AND ($3::uuid IS NULL OR ae.assignee_id = $3::uuid)
		   FOR UPDATE`, id, prop, responsavel))
	if errors.Is(err, pgx.ErrNoRows) {
		return eventoGravado{}, apperr.NotFound("Evento da agenda")
	}
	if err != nil {
		return eventoGravado{}, db.MapError(err)
	}
	return e, nil
}

// novoEvento é o que o POST grava.
type novoEvento struct {
	Tipo          string
	ReservaID     *uuid.UUID
	UnidadeID     *uuid.UUID
	ContatoID     *uuid.UUID
	ResponsavelID *uuid.UUID
	Titulo        string
	Janela        regra.Window
	DiaInteiro    bool
	Notas         *string
	CriadoPor     *uuid.UUID
}

// InserirManual grava o evento marcado à mão. A disputa pela unidade é decidida
// AQUI, pela constraint (23P01 → `errDisputaDeUnidade`, que o service traduz
// depois do rollback).
func (r *Repository) InserirManual(ctx context.Context, prop uuid.UUID, n novoEvento) (eventoGravado, error) {
	e, err := escanearEvento(r.exec(ctx).QueryRow(ctx, `
		INSERT INTO agenda_events AS ae (property_id, type, origin, reservation_id, unit_id, contact_id, assignee_id,
		                                 title, starts_at, ends_at, all_day, notes, created_by)
		VALUES ($1, $2, 'manual', $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING `+colunasDoEvento,
		prop, n.Tipo, n.ReservaID, n.UnidadeID, n.ContatoID, n.ResponsavelID,
		n.Titulo, n.Janela.Start, n.Janela.End, n.DiaInteiro, n.Notas, n.CriadoPor))
	if err != nil {
		return eventoGravado{}, traduzirEscrita(err)
	}
	return e, nil
}

// GravarEdicao é o UPDATE do PUT e do PATCH. No automático, tipo, título,
// unidade, reserva e contato voltam iguais (o domínio já recusou a mudança), e
// a forma do automático (`agenda_events_automatico`) segue de pé.
func (r *Repository) GravarEdicao(ctx context.Context, e eventoGravado) (eventoGravado, error) {
	depois, err := escanearEvento(r.exec(ctx).QueryRow(ctx, `
		UPDATE agenda_events ae
		   SET type = $2, title = $3, starts_at = $4, ends_at = $5, all_day = $6, unit_id = $7,
		       reservation_id = $8, contact_id = $9, assignee_id = $10, notes = $11, updated_at = now()
		 WHERE ae.id = $1
		RETURNING `+colunasDoEvento,
		e.ID, e.Tipo, e.Titulo, e.Inicio, e.Fim, e.DiaInteiro, e.UnidadeID,
		e.ReservaID, e.ContatoID, e.ResponsavelID, e.Notas))
	if err != nil {
		return eventoGravado{}, traduzirEscrita(err)
	}
	return depois, nil
}

// Concluir grava `concluido`. `em` nulo é o `now()` do banco (o `/complete`);
// no check-in e no check-out é o instante registrado na reserva. `janela` não
// nula é a do check-out da saída antecipada (`regra.OnCheckOut`).
func (r *Repository) Concluir(ctx context.Context, id uuid.UUID, em *time.Time, por *uuid.UUID, janela *regra.Window) (eventoGravado, error) {
	var inicio, fim *time.Time
	if janela != nil {
		inicio, fim = &janela.Start, &janela.End
	}
	e, err := escanearEvento(r.exec(ctx).QueryRow(ctx, `
		UPDATE agenda_events ae
		   SET status = 'concluido', done_at = COALESCE($2::timestamptz, now()), done_by = $3,
		       starts_at = COALESCE($4::timestamptz, starts_at), ends_at = COALESCE($5::timestamptz, ends_at),
		       updated_at = now()
		 WHERE ae.id = $1
		RETURNING `+colunasDoEvento, id, em, por, inicio, fim))
	if err != nil {
		return eventoGravado{}, traduzirEscrita(err)
	}
	return e, nil
}

// Cancelar grava `cancelado` com `cancelled_at` do banco. Nunca DELETE: a
// visita desmarcada continua na agenda como desmarcada.
func (r *Repository) Cancelar(ctx context.Context, id uuid.UUID) (eventoGravado, error) {
	e, err := escanearEvento(r.exec(ctx).QueryRow(ctx, `
		UPDATE agenda_events ae
		   SET status = 'cancelado', cancelled_at = now(), updated_at = now()
		 WHERE ae.id = $1
		RETURNING `+colunasDoEvento, id))
	if err != nil {
		return eventoGravado{}, traduzirEscrita(err)
	}
	return e, nil
}

// MoverJanela troca só a janela — a limpeza que vem para hoje, a
// reprogramação pelos horários novos.
func (r *Repository) MoverJanela(ctx context.Context, id uuid.UUID, w regra.Window) (eventoGravado, error) {
	e, err := escanearEvento(r.exec(ctx).QueryRow(ctx, `
		UPDATE agenda_events ae SET starts_at = $2, ends_at = $3, updated_at = now()
		 WHERE ae.id = $1
		RETURNING `+colunasDoEvento, id, w.Start, w.End))
	if err != nil {
		return eventoGravado{}, traduzirEscrita(err)
	}
	return e, nil
}

// MoverUnidade troca a unidade gravada do evento manual — a realocação da
// reserva. A constraint decide se a unidade nova está livre DE OUTRO MANUAL.
func (r *Repository) MoverUnidade(ctx context.Context, id, unidade uuid.UUID) (eventoGravado, error) {
	e, err := escanearEvento(r.exec(ctx).QueryRow(ctx, `
		UPDATE agenda_events ae SET unit_id = $2, updated_at = now()
		 WHERE ae.id = $1
		RETURNING `+colunasDoEvento, id, unidade))
	if err != nil {
		return eventoGravado{}, traduzirEscrita(err)
	}
	return e, nil
}

// SoltarUnidade grava o evento manual SEM unidade própria: ele passa a ocupar
// as unidades da reserva ligada. É o destino do evento que não coube na
// unidade nova de uma realocação — fora do predicado da constraint, a
// colisão aparece na leitura em vez de travar a reserva.
func (r *Repository) SoltarUnidade(ctx context.Context, id uuid.UUID) (eventoGravado, error) {
	e, err := escanearEvento(r.exec(ctx).QueryRow(ctx, `
		UPDATE agenda_events ae SET unit_id = NULL, updated_at = now()
		 WHERE ae.id = $1
		RETURNING `+colunasDoEvento, id))
	if err != nil {
		return eventoGravado{}, traduzirEscrita(err)
	}
	return e, nil
}

// EventosDaReserva trava os eventos ligados à reserva — automáticos e manuais
// — na ordem da agenda. É o que as passagens da reserva (`Ligacao`) decidem.
func (r *Repository) EventosDaReserva(ctx context.Context, reserva uuid.UUID) ([]eventoGravado, error) {
	linhas, err := r.exec(ctx).Query(ctx, `
		SELECT `+colunasDoEvento+`
		  FROM agenda_events ae
		 WHERE ae.reservation_id = $1
		 ORDER BY ae.starts_at, ae.id
		   FOR UPDATE`, reserva)
	if err != nil {
		return nil, db.MapError(err)
	}
	return escanearEventos(linhas)
}

// AutomaticosAgendadosDesde trava os automáticos agendados que começam a
// partir de `desde` — os candidatos de `regra.Retime`.
func (r *Repository) AutomaticosAgendadosDesde(ctx context.Context, prop uuid.UUID, desde time.Time) ([]eventoGravado, error) {
	linhas, err := r.exec(ctx).Query(ctx, `
		SELECT `+colunasDoEvento+`
		  FROM agenda_events ae
		 WHERE ae.property_id = $1 AND ae.origin = 'reservation' AND ae.status = 'agendado'
		   AND ae.starts_at >= $2
		 ORDER BY ae.starts_at, ae.id
		   FOR UPDATE`, prop, desde)
	if err != nil {
		return nil, db.MapError(err)
	}
	return escanearEventos(linhas)
}

// FimDoCheckOut é o fim do check-out automático da reserva, quando ele existe
// e não foi cancelado — o `AutoBounds.CheckoutEnd` da limpeza.
func (r *Repository) FimDoCheckOut(ctx context.Context, reserva uuid.UUID) (*time.Time, error) {
	var fim time.Time
	err := r.exec(ctx).QueryRow(ctx, `
		SELECT ends_at FROM agenda_events
		 WHERE reservation_id = $1 AND origin = 'reservation' AND type = 'checkout' AND status <> 'cancelado'`,
		reserva).Scan(&fim)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, db.MapError(err)
	}
	return &fim, nil
}

// InserirAutomaticos grava os eventos planejados por `regra.Plan`, em ordem.
//
// `idempotente=false` é o `/confirm` (e a remarcação): INSERT sem ON CONFLICT,
// e o 23505 no índice da reserva é DEFEITO — a reserva acabou de sair de
// `hold`, e `hold` não tem agenda —, 500. `idempotente=true` é o
// preenchimento das reservas anteriores à agenda: o que já existe é absorvido
// pelo índice, e só o que nasceu volta.
func (r *Repository) InserirAutomaticos(ctx context.Context, prop, reserva uuid.UUID, planos []regra.Planned,
	autor *uuid.UUID, idempotente bool) ([]eventoGravado, error) {
	q := `
		INSERT INTO agenda_events AS ae (property_id, type, origin, reservation_id, starts_at, ends_at, status, done_at, created_by)
		VALUES ($1, $2, 'reservation', $3, $4, $5, $6, $7, $8)`
	if idempotente {
		q += ` ON CONFLICT (reservation_id, type) WHERE origin = 'reservation' DO NOTHING`
	}
	q += ` RETURNING ` + colunasDoEvento

	out := []eventoGravado{}
	for _, p := range planos {
		e, err := escanearEvento(r.exec(ctx).QueryRow(ctx, q,
			prop, string(p.Type), reserva, p.Window.Start, p.Window.End, string(p.Status), p.DoneAt, autor))
		if errors.Is(err, pgx.ErrNoRows) && idempotente {
			continue
		}
		if err != nil {
			if db.IsUniqueViolation(err, indiceDaReserva) {
				return nil, apperr.Internal.WithCause(fmt.Errorf(
					"agenda: a reserva %s já tinha %s automático ao ser confirmada: %w", reserva, p.Type, err))
			}
			return nil, traduzirEscrita(err)
		}
		out = append(out, e)
	}
	return out, nil
}

// ─────────────────────────── O que a agenda lê da reserva ───────────

// reservaDaAgenda é o que as passagens da reserva precisam dela.
type reservaDaAgenda struct {
	ID            uuid.UUID
	PropriedadeID uuid.UUID
	Codigo        string
	Status        string
	ContatoID     uuid.UUID
	Entrada       calendar.Date
	Saida         calendar.Date
}

func (r reservaDaAgenda) estadia() regra.Stay {
	return regra.Stay{CheckIn: r.Entrada, CheckOut: r.Saida}
}

// Reserva lê a reserva pelo id. Quem chama (a reserva, na mesma transação) já
// a travou.
func (r *Repository) Reserva(ctx context.Context, id uuid.UUID) (reservaDaAgenda, error) {
	var (
		res            reservaDaAgenda
		entrada, saida string
	)
	err := r.exec(ctx).QueryRow(ctx, `
		SELECT id, property_id, code, status, contact_id, check_in::text, check_out::text
		  FROM reservations WHERE id = $1`, id).
		Scan(&res.ID, &res.PropriedadeID, &res.Codigo, &res.Status, &res.ContatoID, &entrada, &saida)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, apperr.Internal.WithCause(fmt.Errorf("agenda: reserva %s não existe", id))
	}
	if err != nil {
		return res, db.MapError(err)
	}
	if res.Entrada, err = calendar.Parse(entrada); err != nil {
		return res, apperr.Internal.WithCause(err)
	}
	if res.Saida, err = calendar.Parse(saida); err != nil {
		return res, apperr.Internal.WithCause(err)
	}
	return res, nil
}

// ReservasParaPreencher são as reservas `confirmed` e `checked_in` da casa com
// saída de `hoje` em diante, em ordem de entrada.
func (r *Repository) ReservasParaPreencher(ctx context.Context, prop uuid.UUID, hoje calendar.Date) ([]reservaDaAgenda, error) {
	linhas, err := r.exec(ctx).Query(ctx, `
		SELECT id, property_id, code, status, contact_id, check_in::text, check_out::text
		  FROM reservations
		 WHERE property_id = $1 AND status = ANY($2::text[]) AND check_out >= $3::date
		 ORDER BY check_in, id`,
		prop, []string{ReservaConfirmada, ReservaHospedada}, hoje.String())
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()
	out := []reservaDaAgenda{}
	for linhas.Next() {
		var (
			res            reservaDaAgenda
			entrada, saida string
		)
		if err := linhas.Scan(&res.ID, &res.PropriedadeID, &res.Codigo, &res.Status, &res.ContatoID, &entrada, &saida); err != nil {
			return nil, db.MapError(err)
		}
		if res.Entrada, err = calendar.Parse(entrada); err != nil {
			return nil, apperr.Internal.WithCause(err)
		}
		if res.Saida, err = calendar.Parse(saida); err != nil {
			return nil, apperr.Internal.WithCause(err)
		}
		out = append(out, res)
	}
	return out, db.MapError(linhas.Err())
}

// InstanteDaEntrada é o instante EFETIVO do último check-in registrado na
// reserva — o `at` informado, senão o carimbo da linha —, como
// `reservas.Repository.InstanteDoEvento` o lê.
func (r *Repository) InstanteDaEntrada(ctx context.Context, reserva uuid.UUID) (*time.Time, error) {
	var t time.Time
	err := r.exec(ctx).QueryRow(ctx, `
		SELECT COALESCE((payload->>'at')::timestamptz, at)
		  FROM reservation_events
		 WHERE reservation_id = $1 AND type = $2
		 ORDER BY at DESC LIMIT 1`, reserva, EventoDeEntradaNaReserva).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, db.MapError(err)
	}
	return &t, nil
}

// ─────────────────────────── Os vínculos de um evento manual ────────

// unidadeDoVinculo é o que a escrita precisa saber da unidade citada.
type unidadeDoVinculo struct {
	Codigo string
	Ativa  bool
}

// Unidade lê a unidade desta casa. ok=false se não há.
func (r *Repository) Unidade(ctx context.Context, prop, id uuid.UUID) (unidadeDoVinculo, bool, error) {
	var u unidadeDoVinculo
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT code, active FROM units WHERE id = $1 AND property_id = $2`, id, prop).Scan(&u.Codigo, &u.Ativa)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, false, nil
	}
	if err != nil {
		return u, false, db.MapError(err)
	}
	return u, true, nil
}

// EstadoDaReserva lê o estado da reserva desta casa com FOR KEY SHARE: um
// `/cancel` em voo (FOR UPDATE na reserva) termina antes desta leitura — e
// ela enxerga o `cancelled` —, ou espera o evento nascer e o cancela junto.
// Sem a trava, a visita nasceria ligada a uma reserva que acabou de cair.
func (r *Repository) EstadoDaReserva(ctx context.Context, prop, id uuid.UUID) (string, bool, error) {
	var status string
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT status FROM reservations WHERE id = $1 AND property_id = $2 FOR KEY SHARE`, id, prop).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, db.MapError(err)
	}
	return status, true, nil
}

// UnidadesDaReserva são as unidades que a reserva ocupa hoje, por código.
func (r *Repository) UnidadesDaReserva(ctx context.Context, reserva uuid.UUID) ([]UnidadeDoEventoDaAgenda, error) {
	porReserva, err := r.unidadesDasReservas(ctx, []uuid.UUID{reserva})
	if err != nil {
		return nil, err
	}
	return porReserva[reserva], nil
}

// Contato lê se o contato desta casa existe e se foi anonimizado, com FOR KEY
// SHARE: a anonimização trava a ficha em FOR UPDATE antes de esvaziá-la, então
// esta leitura enxerga o resultado dela ou a faz esperar o evento nascer.
func (r *Repository) Contato(ctx context.Context, prop, id uuid.UUID) (anonimizado, achou bool, err error) {
	err = r.exec(ctx).QueryRow(ctx,
		`SELECT anonymized_at IS NOT NULL FROM contacts WHERE id = $1 AND property_id = $2 FOR KEY SHARE`,
		id, prop).Scan(&anonimizado)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, db.MapError(err)
	}
	return anonimizado, true, nil
}

// UsuarioAtivo diz se o usuário é desta casa, ativo e não apagado.
func (r *Repository) UsuarioAtivo(ctx context.Context, prop, id uuid.UUID) (bool, error) {
	var ativo bool
	err := r.exec(ctx).QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM users
		                WHERE id = $1 AND property_id = $2 AND active AND deleted_at IS NULL)`,
		id, prop).Scan(&ativo)
	return ativo, db.MapError(err)
}

// ─────────────────────────── Tradução da escrita ────────────────────

// errDisputaDeUnidade é o 23P01 da `EXCLUDE` dos manuais: outra marcação à mão
// ocupa a unidade no mesmo instante. O service o traduz em 409
// UNIT_NOT_AVAILABLE DEPOIS do rollback, relendo os conflitos — a transação
// abortada não responde mais nada.
type errDisputaDeUnidade struct{ causa error }

func (e *errDisputaDeUnidade) Error() string {
	return "agenda: a unidade já tem outra marcação nesse horário: " + e.causa.Error()
}
func (e *errDisputaDeUnidade) Unwrap() error { return e.causa }

// traduzirEscrita converte o erro de escrita em `agenda_events` pelo NOME da
// constraint. As de forma (título, notas, tipo, janela) a API já validou; as de
// estado e de origem só caem por defeito nosso — o domínio decide antes —, e
// saem 500.
func traduzirEscrita(err error) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == sqlstateExclusao && pg.ConstraintName == exclusaoDaUnidade {
		return &errDisputaDeUnidade{causa: err}
	}
	switch {
	case db.IsForeignKeyViolation(err, fkUnidade):
		return campoInvalido("unit_id", "unidade não encontrada nesta propriedade.", err)
	case db.IsForeignKeyViolation(err, fkReserva):
		return campoInvalido("reservation_id", "reserva não encontrada nesta propriedade.", err)
	case db.IsForeignKeyViolation(err, fkContato):
		return campoInvalido("contact_id", "contato não encontrado nesta propriedade.", err)
	case db.IsForeignKeyViolation(err, fkResponsavel):
		return campoInvalido("assignee_id", "usuário não encontrado nesta propriedade.", err)
	}
	if errors.As(err, &pg) && pg.Code == sqlstateCheck {
		switch pg.ConstraintName {
		case "agenda_events_title_nao_vazio", "agenda_events_title_tamanho":
			return campoInvalido("title", fmt.Sprintf("obrigatório, até %d caracteres.", tamanhoDoTitulo), err)
		case "agenda_events_notes_tamanho":
			return campoInvalido("notes", fmt.Sprintf("no máximo %d caracteres.", tamanhoDasNotas), err)
		case "agenda_events_periodo":
			return campoInvalido("ends_at", "deve ser posterior a `starts_at`.", err)
		default:
			return apperr.Internal.WithCause(err)
		}
	}
	return db.MapError(err)
}

func campoInvalido(campo, msg string, causa error) error {
	return apperr.Validation(map[string]string{campo: msg}).WithCause(causa)
}
