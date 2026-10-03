package site

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// Repository é o SQL do módulo. Não abre transação: o executor vem do
// contexto (db.From), e quem abre é o service.
type Repository struct {
	pool db.DBTX
}

func NewRepository(pool db.DBTX) *Repository { return &Repository{pool: pool} }

func (r *Repository) exec(ctx context.Context) db.DBTX { return db.From(ctx, r.pool) }

// Linha é uma linha de site_content.
type Linha struct {
	Chave         string          `json:"key"`
	Valor         json.RawMessage `json:"value"`
	AtualizadoEm  time.Time       `json:"updated_at"`
	AtualizadoPor *uuid.UUID      `json:"updated_by"`
}

// Midia é uma linha de site_media.
type Midia struct {
	ID         uuid.UUID  `json:"id"`
	Tipo       string     `json:"kind"`
	Mime       string     `json:"mime"`
	Bytes      int64      `json:"bytes"`
	NomeOrig   string     `json:"original_name"`
	StorageKey string     `json:"storage_key"`
	CriadoEm   time.Time  `json:"created_at"`
	CriadoPor  *uuid.UUID `json:"created_by"`
}

// ListarConteudo devolve todas as linhas editadas. São dezenas, no máximo o
// tamanho do catálogo — sem paginação.
func (r *Repository) ListarConteudo(ctx context.Context) ([]Linha, error) {
	linhas, err := r.exec(ctx).Query(ctx,
		`SELECT key, value, updated_at, updated_by FROM site_content ORDER BY key`)
	if err != nil {
		return nil, db.MapError(err)
	}
	out, err := pgx.CollectRows(linhas, func(row pgx.CollectableRow) (Linha, error) {
		var l Linha
		err := row.Scan(&l.Chave, &l.Valor, &l.AtualizadoEm, &l.AtualizadoPor)
		return l, err
	})
	if err != nil {
		return nil, db.MapError(err)
	}
	return out, nil
}

// TravarConteudo lê a linha da chave com FOR UPDATE (para o antes da
// auditoria não mentir sob edição concorrente). ok=false se não há linha.
func (r *Repository) TravarConteudo(ctx context.Context, chave string) (Linha, bool, error) {
	var l Linha
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT key, value, updated_at, updated_by FROM site_content WHERE key = $1 FOR UPDATE`, chave).
		Scan(&l.Chave, &l.Valor, &l.AtualizadoEm, &l.AtualizadoPor)
	if errors.Is(err, pgx.ErrNoRows) {
		return Linha{}, false, nil
	}
	if err != nil {
		return Linha{}, false, db.MapError(err)
	}
	return l, true, nil
}

// GravarConteudo insere ou substitui o valor da chave.
func (r *Repository) GravarConteudo(ctx context.Context, chave string, valor json.RawMessage, por *uuid.UUID) (Linha, error) {
	var l Linha
	err := r.exec(ctx).QueryRow(ctx, `
		INSERT INTO site_content (key, value, updated_by)
		VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE
		   SET value = EXCLUDED.value, updated_at = now(), updated_by = EXCLUDED.updated_by
		RETURNING key, value, updated_at, updated_by`, chave, []byte(valor), por).
		Scan(&l.Chave, &l.Valor, &l.AtualizadoEm, &l.AtualizadoPor)
	if err != nil {
		return Linha{}, db.MapError(err)
	}
	return l, nil
}

// ApagarConteudo apaga a linha da chave (restaurar o original).
func (r *Repository) ApagarConteudo(ctx context.Context, chave string) error {
	if _, err := r.exec(ctx).Exec(ctx, `DELETE FROM site_content WHERE key = $1`, chave); err != nil {
		return db.MapError(err)
	}
	return nil
}

// TiposDeMidia devolve id → kind das mídias que existem entre as pedidas.
func (r *Repository) TiposDeMidia(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(ids) == 0 {
		return out, nil
	}
	linhas, err := r.exec(ctx).Query(ctx, `SELECT id, kind FROM site_media WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()
	for linhas.Next() {
		var (
			id   uuid.UUID
			tipo string
		)
		if err := linhas.Scan(&id, &tipo); err != nil {
			return nil, db.MapError(err)
		}
		out[id] = tipo
	}
	if err := linhas.Err(); err != nil {
		return nil, db.MapError(err)
	}
	return out, nil
}

// CriarMidia registra o arquivo já gravado no volume.
func (r *Repository) CriarMidia(ctx context.Context, m Midia) (Midia, error) {
	err := r.exec(ctx).QueryRow(ctx, `
		INSERT INTO site_media (id, kind, mime, bytes, original_name, storage_key, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at`,
		m.ID, m.Tipo, m.Mime, m.Bytes, m.NomeOrig, m.StorageKey, m.CriadoPor).Scan(&m.CriadoEm)
	if err != nil {
		return Midia{}, db.MapError(err)
	}
	return m, nil
}

// BuscarMidia lê uma mídia pelo id. ok=false se não existe.
func (r *Repository) BuscarMidia(ctx context.Context, id uuid.UUID) (Midia, bool, error) {
	var m Midia
	err := r.exec(ctx).QueryRow(ctx, `
		SELECT id, kind, mime, bytes, original_name, storage_key, created_at, created_by
		  FROM site_media WHERE id = $1`, id).
		Scan(&m.ID, &m.Tipo, &m.Mime, &m.Bytes, &m.NomeOrig, &m.StorageKey, &m.CriadoEm, &m.CriadoPor)
	if errors.Is(err, pgx.ErrNoRows) {
		return Midia{}, false, nil
	}
	if err != nil {
		return Midia{}, false, db.MapError(err)
	}
	return m, true, nil
}
