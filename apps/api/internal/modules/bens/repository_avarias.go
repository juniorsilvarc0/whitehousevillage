package bens

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// A reserva entra pelo `code` e por mais nada: "o que quebrou nesta estadia"
// se responde sem dizer quem dormiu nela. O JOIN com `reservations` não
// alcança `contacts`, e é de propósito.
const colunasDaAvaria = `
	ii.id, ii.room_id, r.name, r.unit_id, u.code, ii.item_id, i.name, i.category, ` + colunasDaCapa + `,
	ii.kind, ii.qty, ii.note, i.replacement_cost_cents, ii.reservation_id, res.code, ii.count_id,
	ii.resolution, ii.reported_by, rb.name, ii.reported_at, ii.resolved_by, sb.name, ii.resolved_at,
	ii.updated_at`

const juncoesDaAvaria = `
	  FROM inventory_issues ii
	  JOIN unit_rooms r ON r.id = ii.room_id
	  JOIN units u ON u.id = r.unit_id
	  JOIN inventory_items i ON i.id = ii.item_id
	  JOIN properties pr ON pr.id = ii.property_id
	  LEFT JOIN reservations res ON res.id = ii.reservation_id
	  LEFT JOIN users rb ON rb.id = ii.reported_by
	  LEFT JOIN users sb ON sb.id = ii.resolved_by` + lateralDaCapa

func escanearAvaria(linha pgx.Row, extras ...any) (Avaria, error) {
	var (
		a   Avaria
		cap capaLida
	)
	destinos := []any{&a.ID, &a.AmbienteID, &a.AmbienteNome, &a.UnidadeID, &a.UnidadeCodigo, &a.BemID,
		&a.BemNome, &a.BemCategoria}
	destinos = append(destinos, cap.destinos()...)
	destinos = append(destinos, &a.Tipo, &a.Qtd, &a.Nota, &a.CustoDeReposicaoCents, &a.ReservaID,
		&a.ReservaCodigo, &a.ConferenciaID, &a.Desfecho, &a.RelatadaPor, &a.RelatadaPorNome, &a.RelatadaEm,
		&a.ResolvidaPor, &a.ResolvidaPorNome, &a.ResolvidaEm, &a.AtualizadaEm)
	if err := linha.Scan(append(destinos, extras...)...); err != nil {
		return Avaria{}, err
	}
	a.Capa = cap.midia()
	a.CustoTotalCents = custoTotal(a.Qtd, a.CustoDeReposicaoCents)
	return a, nil
}

// OrdensDeAvaria é a whitelist do `?sort=`. Desempate por instante e id.
var OrdensDeAvaria = map[string]string{
	"reported_at":  "ii.reported_at ASC, ii.id ASC",
	"-reported_at": "ii.reported_at DESC, ii.id DESC",
	"qty":          "ii.qty ASC, ii.reported_at DESC, ii.id DESC",
	"-qty":         "ii.qty DESC, ii.reported_at DESC, ii.id DESC",
}

// OrdemPadraoDeAvaria é da mais recente para a mais antiga.
const OrdemPadraoDeAvaria = "-reported_at"

// ListarAvarias é o `GET /inventory/issues`.
func (r *Repository) ListarAvarias(ctx context.Context, prop uuid.UUID, f FiltroDeAvarias) ([]Avaria, int64, error) {
	c := novasCondicoes(prop)
	if f.UnidadeID != nil {
		c.add("r.unit_id = $%d", *f.UnidadeID)
	}
	if f.AmbienteID != nil {
		c.add("ii.room_id = $%d", *f.AmbienteID)
	}
	if f.BemID != nil {
		c.add("ii.item_id = $%d", *f.BemID)
	}
	if f.Tipo != "" {
		c.add("ii.kind = $%d", f.Tipo)
	}
	if f.Aberta != nil {
		// Pendência aberta é `resolution IS NULL` — não há coluna status.
		if *f.Aberta {
			c.fixo("ii.resolution IS NULL")
		} else {
			c.fixo("ii.resolution IS NOT NULL")
		}
	}
	if f.Desfecho != "" {
		c.add("ii.resolution = $%d", f.Desfecho)
	}
	if f.ReservaID != nil {
		c.add("ii.reservation_id = $%d", *f.ReservaID)
	}
	if f.ConferenciaID != nil {
		c.add("ii.count_id = $%d", *f.ConferenciaID)
	}
	filtrarPeriodo(c, "ii.reported_at", f.De, f.Ate)
	corpo := juncoesDaAvaria + ` WHERE ii.property_id = $1` + c.where()
	return listar(ctx, r.exec(ctx), colunasDaAvaria, corpo, ordemOu(OrdensDeAvaria, f.Ordem, OrdemPadraoDeAvaria), c,
		f.Pagina, f.PorPagina, func(l pgx.Rows, total *int64) (Avaria, error) {
			return escanearAvaria(l, total)
		})
}

// BuscarAvaria lê a avaria desta casa.
func (r *Repository) BuscarAvaria(ctx context.Context, prop, id uuid.UUID) (Avaria, error) {
	a, err := escanearAvaria(r.exec(ctx).QueryRow(ctx,
		`SELECT `+colunasDaAvaria+juncoesDaAvaria+` WHERE ii.id = $1 AND ii.property_id = $2`, id, prop))
	if errors.Is(err, pgx.ErrNoRows) {
		return Avaria{}, apperr.NotFound("Avaria")
	}
	if err != nil {
		return Avaria{}, db.MapError(err)
	}
	return a, nil
}

// avariaGravada é a linha de `inventory_issues` como está no banco.
type avariaGravada struct {
	ID            uuid.UUID  `json:"id"`
	AmbienteID    uuid.UUID  `json:"room_id"`
	BemID         uuid.UUID  `json:"item_id"`
	Tipo          string     `json:"kind"`
	Qtd           int        `json:"qty"`
	Nota          *string    `json:"note"`
	ReservaID     *uuid.UUID `json:"reservation_id"`
	ConferenciaID *uuid.UUID `json:"count_id"`
	Desfecho      *string    `json:"resolution"`
	ResolvidaPor  *uuid.UUID `json:"resolved_by"`
	ResolvidaEm   *time.Time `json:"resolved_at"`
}

// CriarAvaria insere a avaria. `reported_at` é o DEFAULT now() da tabela, e
// `reported_by` é o ator — nenhum dos dois vem do corpo.
func (r *Repository) CriarAvaria(ctx context.Context, prop uuid.UUID, a avariaGravada, por *uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, `
		INSERT INTO inventory_issues (property_id, room_id, item_id, kind, qty, note,
		                              reservation_id, count_id, reported_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`, prop, a.AmbienteID, a.BemID, a.Tipo, a.Qtd, a.Nota, a.ReservaID, a.ConferenciaID, por).Scan(&id)
	if err != nil {
		return uuid.Nil, db.MapError(err)
	}
	return id, nil
}

// TravarAvaria lê a avaria com FOR UPDATE — base do merge do PATCH e do
// "antes" da trilha.
func (r *Repository) TravarAvaria(ctx context.Context, prop, id uuid.UUID) (avariaGravada, error) {
	var a avariaGravada
	err := r.exec(ctx).QueryRow(ctx, `
		SELECT id, room_id, item_id, kind, qty, note, reservation_id, count_id, resolution,
		       resolved_by, resolved_at
		  FROM inventory_issues WHERE id = $1 AND property_id = $2
		   FOR UPDATE`, id, prop).
		Scan(&a.ID, &a.AmbienteID, &a.BemID, &a.Tipo, &a.Qtd, &a.Nota, &a.ReservaID, &a.ConferenciaID,
			&a.Desfecho, &a.ResolvidaPor, &a.ResolvidaEm)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, apperr.NotFound("Avaria")
	}
	if err != nil {
		return a, db.MapError(err)
	}
	return a, nil
}

// GravarAvaria é o UPDATE do PUT e do PATCH.
//
// `resolved_at`/`resolved_by` são carimbados AQUI, pelo servidor, e só quando
// o desfecho MUDA: de nulo para um valor (resolveu) ou de um valor para outro
// (decidiu de novo). Repetir o mesmo desfecho não reescreve quem resolveu nem
// quando. `resolution` nulo reabre e limpa os dois — o CHECK
// `inventory_issues_desfecho` não aceita um sem o outro. No SET, `resolution`
// à direita é o valor ANTIGO da linha (semântica do UPDATE do Postgres).
func (r *Repository) GravarAvaria(ctx context.Context, prop uuid.UUID, a avariaGravada, por *uuid.UUID) error {
	tag, err := r.exec(ctx).Exec(ctx, `
		UPDATE inventory_issues
		   SET kind = $3, qty = $4, note = $5, reservation_id = $6, resolution = $7::text,
		       resolved_at = CASE WHEN $7::text IS NULL THEN NULL
		                          WHEN resolution IS DISTINCT FROM $7::text THEN now()
		                          ELSE resolved_at END,
		       resolved_by = CASE WHEN $7::text IS NULL THEN NULL
		                          WHEN resolution IS DISTINCT FROM $7::text THEN $8::uuid
		                          ELSE resolved_by END,
		       updated_at = now()
		 WHERE id = $1 AND property_id = $2`,
		a.ID, prop, a.Tipo, a.Qtd, a.Nota, a.ReservaID, a.Desfecho, por)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Avaria")
	}
	return nil
}

// ApagarAvaria apaga a linha (registro que nunca devia ter nascido). Nada no
// banco a referencia.
func (r *Repository) ApagarAvaria(ctx context.Context, prop, id uuid.UUID) error {
	tag, err := r.exec(ctx).Exec(ctx, `DELETE FROM inventory_issues WHERE id = $1 AND property_id = $2`, id, prop)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Avaria")
	}
	return nil
}

// ReservaDaPropriedade diz se a reserva é desta casa. Só existência: o código e
// o resto da venda não interessam a este módulo.
func (r *Repository) ReservaDaPropriedade(ctx context.Context, prop, id uuid.UUID) (bool, error) {
	var existe bool
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM reservations WHERE id = $1 AND property_id = $2)`, id, prop).Scan(&existe)
	return existe, db.MapError(err)
}

// ReservaPorCodigo troca o código legível ("WH-2026-0142") pelo id, só nesta
// casa. `reservations.code` é único; nada além do id sai daqui.
func (r *Repository) ReservaPorCodigo(ctx context.Context, prop uuid.UUID, codigo string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT id FROM reservations WHERE code = $1 AND property_id = $2`, codigo, prop).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, db.MapError(err)
	}
	return id, true, nil
}

// UnidadeDaConferencia devolve a unidade de uma conferência desta casa.
func (r *Repository) UnidadeDaConferencia(ctx context.Context, prop, id uuid.UUID) (uuid.UUID, bool, error) {
	var unidade uuid.UUID
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT unit_id FROM inventory_counts WHERE id = $1 AND property_id = $2`, id, prop).Scan(&unidade)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, db.MapError(err)
	}
	return unidade, true, nil
}
