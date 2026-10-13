package agenda

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// bloqueioGravado é a linha de `agenda_blocks` — o TEMPO da operação, não uma
// unidade. As tags `json` são o documento da trilha (`audit_log`), com os
// instantes em UTC (`auditavel`).
type bloqueioGravado struct {
	ID           uuid.UUID  `json:"id"`
	Inicio       time.Time  `json:"starts_at"`
	Fim          time.Time  `json:"ends_at"`
	DiaInteiro   bool       `json:"all_day"`
	Motivo       string     `json:"reason"`
	CriadoPor    *uuid.UUID `json:"created_by"`
	CriadoEm     time.Time  `json:"-"`
	AtualizadoEm time.Time  `json:"-"`
}

func (b bloqueioGravado) auditavel() bloqueioGravado {
	b.Inicio, b.Fim = b.Inicio.UTC(), b.Fim.UTC()
	return b
}

const colunasDoBloqueio = `ab.id, ab.starts_at, ab.ends_at, ab.all_day, ab.reason, ab.created_by, ab.created_at, ab.updated_at`

func escanearBloqueio(l pgx.Row, extras ...any) (bloqueioGravado, error) {
	var b bloqueioGravado
	err := l.Scan(append([]any{&b.ID, &b.Inicio, &b.Fim, &b.DiaInteiro, &b.Motivo, &b.CriadoPor, &b.CriadoEm, &b.AtualizadoEm}, extras...)...)
	return b, err
}

// ListarBloqueios devolve os bloqueios que CRUZAM `[inicio, fim)` — já em
// instantes (as meias-noites da casa, convertidas pelo service) —, por início
// e id, paginados. A expressão da janela é a do índice
// `agenda_blocks_janela_idx`.
func (r *Repository) ListarBloqueios(ctx context.Context, prop uuid.UUID, inicio, fim time.Time, pagina, porPagina int) ([]bloqueioGravado, int64, error) {
	const corpo = `
		  FROM agenda_blocks ab
		 WHERE ab.property_id = $1
		   AND tstzrange(ab.starts_at, ab.ends_at, '[)') && tstzrange($2, $3, '[)')`
	linhas, err := r.exec(ctx).Query(ctx, `SELECT `+colunasDoBloqueio+`, count(*) OVER()`+corpo+`
		 ORDER BY ab.starts_at, ab.id LIMIT $4 OFFSET $5`,
		prop, inicio, fim, porPagina, httpx.Offset(pagina, porPagina))
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	out := []bloqueioGravado{}
	var total int64
	for linhas.Next() {
		b, err := escanearBloqueio(linhas, &total)
		if err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, b)
	}
	if err := linhas.Err(); err != nil {
		return nil, 0, db.MapError(err)
	}
	if len(out) == 0 && pagina > 1 {
		// Página além do fim: o total vem de uma contagem à parte, senão a
		// coleção pareceria vazia.
		if err := r.exec(ctx).QueryRow(ctx, `SELECT count(*)`+corpo, prop, inicio, fim).Scan(&total); err != nil {
			return nil, 0, db.MapError(err)
		}
	}
	return out, total, nil
}

// BuscarBloqueio lê o bloqueio desta casa; `travar` é o FOR UPDATE da escrita.
func (r *Repository) BuscarBloqueio(ctx context.Context, prop, id uuid.UUID, travar bool) (bloqueioGravado, error) {
	q := `SELECT ` + colunasDoBloqueio + ` FROM agenda_blocks ab WHERE ab.id = $1 AND ab.property_id = $2`
	if travar {
		q += ` FOR UPDATE`
	}
	b, err := escanearBloqueio(r.exec(ctx).QueryRow(ctx, q, id, prop))
	if errors.Is(err, pgx.ErrNoRows) {
		return bloqueioGravado{}, apperr.NotFound("Bloqueio de horário")
	}
	if err != nil {
		return bloqueioGravado{}, db.MapError(err)
	}
	return b, nil
}

// InserirBloqueio grava o bloqueio. Não recusa por sobreposição: dois
// bloqueios no mesmo horário são dois avisos.
func (r *Repository) InserirBloqueio(ctx context.Context, prop uuid.UUID, b bloqueioGravado) (bloqueioGravado, error) {
	novo, err := escanearBloqueio(r.exec(ctx).QueryRow(ctx, `
		INSERT INTO agenda_blocks AS ab (property_id, starts_at, ends_at, all_day, reason, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+colunasDoBloqueio, prop, b.Inicio, b.Fim, b.DiaInteiro, b.Motivo, b.CriadoPor))
	if err != nil {
		return bloqueioGravado{}, traduzirBloqueio(err)
	}
	return novo, nil
}

// GravarBloqueio é o UPDATE do PUT e do PATCH.
func (r *Repository) GravarBloqueio(ctx context.Context, b bloqueioGravado) (bloqueioGravado, error) {
	depois, err := escanearBloqueio(r.exec(ctx).QueryRow(ctx, `
		UPDATE agenda_blocks ab
		   SET starts_at = $2, ends_at = $3, all_day = $4, reason = $5, updated_at = now()
		 WHERE ab.id = $1
		RETURNING `+colunasDoBloqueio, b.ID, b.Inicio, b.Fim, b.DiaInteiro, b.Motivo))
	if err != nil {
		return bloqueioGravado{}, traduzirBloqueio(err)
	}
	return depois, nil
}

// ApagarBloqueio apaga DE VERDADE: o bloqueio de horário é configuração do
// tempo da operação, não histórico — o antes fica em `audit_log`.
func (r *Repository) ApagarBloqueio(ctx context.Context, id uuid.UUID) error {
	_, err := r.exec(ctx).Exec(ctx, `DELETE FROM agenda_blocks WHERE id = $1`, id)
	return db.MapError(err)
}

// traduzirBloqueio leva os CHECKs de `agenda_blocks` ao campo do contrato. A
// API valida antes; chegar aqui é corrida ou defeito.
func traduzirBloqueio(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == sqlstateCheck {
		switch pg.ConstraintName {
		case "agenda_blocks_periodo":
			return campoInvalido("ends_at", "deve ser posterior a `starts_at`.", err)
		case "agenda_blocks_reason_nao_vazio", "agenda_blocks_reason_tamanho":
			return campoInvalido("reason", fmt.Sprintf("obrigatório, até %d caracteres.", tamanhoDoMotivo), err)
		}
		return apperr.Internal.WithCause(err)
	}
	return db.MapError(err)
}
