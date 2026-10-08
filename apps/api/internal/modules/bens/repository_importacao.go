package bens

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// SQL do importador do levantamento fotográfico (importacao.go).
//
// Toda escrita aqui é `INSERT … ON CONFLICT DO NOTHING RETURNING`: quem decide
// "já existe" é a constraint, e a transação da casa NÃO aborta no conflito — o
// importador lê o id da linha que já estava lá e segue. Nada é atualizado: o
// que existe pode ter sido corrigido pelo gestor no painel, e a importação só
// acrescenta.
//
// As funções `…Existe`/`…Por…` são as leituras do dry-run, que roda o mesmo
// caminho numa transação READ ONLY trocando cada INSERT pela pergunta "isto já
// está lá?".

// unidadeLocalizada é a unidade do levantamento resolvida no banco.
type unidadeLocalizada struct {
	Propriedade uuid.UUID
	ID          uuid.UUID
}

// UnidadesPorCodigo procura a unidade pelo código em TODAS as propriedades: o
// importador não tem usuário (e portanto não tem "propriedade do ator"). Mais
// de uma é ambiguidade que o importador recusa.
func (r *Repository) UnidadesPorCodigo(ctx context.Context, codigo string) ([]unidadeLocalizada, error) {
	linhas, err := r.exec(ctx).Query(ctx,
		`SELECT property_id, id FROM units WHERE code = $1 ORDER BY property_id`, codigo)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()
	out := []unidadeLocalizada{}
	for linhas.Next() {
		var u unidadeLocalizada
		if err := linhas.Scan(&u.Propriedade, &u.ID); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, u)
	}
	return out, db.MapError(linhas.Err())
}

// SomenteLeitura torna a transação corrente READ ONLY. É a rede do dry-run:
// mesmo que um defeito chamasse uma escrita, o banco a recusaria.
func (r *Repository) SomenteLeitura(ctx context.Context) error {
	_, err := r.exec(ctx).Exec(ctx, `SET TRANSACTION READ ONLY`)
	return db.MapError(err)
}

// CriarAmbienteImportado cria o cômodo com o `code` DECLARADO pelo
// levantamento. Conflito em qualquer das duas chaves (`code` ou `name` na
// unidade) devolve criado=false sem abortar a transação.
func (r *Repository) CriarAmbienteImportado(ctx context.Context, prop, unidade uuid.UUID, a ambienteGravado) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, `
		INSERT INTO unit_rooms (property_id, unit_id, code, name, kind, sort_order, active)
		VALUES ($1, $2, $3, $4, $5, $6, true)
		ON CONFLICT DO NOTHING
		RETURNING id`, prop, unidade, a.Codigo, a.Nome, a.Tipo, a.Ordem).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, traduzirAmbiente(err)
	}
	return id, true, nil
}

// bemImportado é o que o levantamento grava num item novo do catálogo.
type bemImportado struct {
	OrigemDaImportacao string  `json:"source_ref"`
	Nome               string  `json:"name"`
	Descricao          *string `json:"description"`
	Categoria          string  `json:"category"`
}

// CriarBemImportado insere o item com `source_ref`; o índice único parcial
// `inventory_items_source_ref_idx` é o árbitro. Item que já existe NÃO é
// reescrito — o gestor pode ter corrigido nome ou categoria. `unit_measure` é
// `un` e o custo é NULL (ninguém cotou ainda).
func (r *Repository) CriarBemImportado(ctx context.Context, prop uuid.UUID, b bemImportado) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, `
		INSERT INTO inventory_items (property_id, name, description, category, unit_measure,
		                             replacement_cost_cents, active, source_ref)
		VALUES ($1, $2, $3, $4, 'un', NULL, true, $5)
		ON CONFLICT (property_id, source_ref) WHERE source_ref IS NOT NULL DO NOTHING
		RETURNING id`, prop, b.Nome, b.Descricao, b.Categoria, b.OrigemDaImportacao).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, db.MapError(err)
	}
	return id, true, nil
}

// BemPorOrigem lê o id do item pela origem da importação, nesta casa.
func (r *Repository) BemPorOrigem(ctx context.Context, prop uuid.UUID, origem string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT id FROM inventory_items WHERE property_id = $1 AND source_ref = $2`, prop, origem).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, db.MapError(err)
	}
	return id, true, nil
}

// CriarMidiaImportada registra a foto com `storage_key` DETERMINÍSTICO; a
// `UNIQUE (storage_key)` é o árbitro, e a linha que já existe é reaproveitada.
func (r *Repository) CriarMidiaImportada(ctx context.Context, m registroDeMidia) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, `
		INSERT INTO inventory_media (property_id, mime, bytes, width, height, original_name,
		                             storage_key, thumb_key, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULL)
		ON CONFLICT (storage_key) DO NOTHING
		RETURNING id`, m.PropriedadeID, m.Mime, m.Bytes, m.Largura, m.Altura, m.NomeOriginal,
		m.ChaveArquivo, m.ChaveMiniatura).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, db.MapError(err)
	}
	return id, true, nil
}

// MidiaPorChave lê a foto pela `storage_key`, com a propriedade dela: a chave
// é única no banco inteiro, e uma linha de OUTRA casa com a mesma chave é
// conflito que o importador recusa em vez de reaproveitar.
func (r *Repository) MidiaPorChave(ctx context.Context, chave string) (id, prop uuid.UUID, ok bool, err error) {
	err = r.exec(ctx).QueryRow(ctx,
		`SELECT id, property_id FROM inventory_media WHERE storage_key = $1`, chave).Scan(&id, &prop)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, uuid.Nil, false, db.MapError(err)
	}
	return id, prop, true, nil
}

// LigarFotoImportada liga foto e item na galeria, com `sort_order` = posição
// da foto no levantamento. A chave primária `(item_id, media_id)` é o árbitro.
func (r *Repository) LigarFotoImportada(ctx context.Context, bem, midia uuid.UUID, ordem int) (bool, error) {
	var lixo uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, `
		INSERT INTO inventory_item_media (item_id, media_id, sort_order)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
		RETURNING item_id`, bem, midia, ordem).Scan(&lixo)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, db.MapError(err)
	}
	return true, nil
}

// LigacaoExiste diz se a foto já está na galeria do item.
func (r *Repository) LigacaoExiste(ctx context.Context, bem, midia uuid.UUID) (bool, error) {
	var existe bool
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM inventory_item_media WHERE item_id = $1 AND media_id = $2)`, bem, midia).Scan(&existe)
	return existe, db.MapError(err)
}

// ColocarImportado põe o bem no cômodo com a quantidade do levantamento. A
// chave primária `(room_id, item_id)` é o árbitro: quantidade já ajustada no
// painel NÃO volta ao valor do levantamento.
func (r *Repository) ColocarImportado(ctx context.Context, ambiente, bem uuid.UUID, qtd int, nota *string) (bool, error) {
	var lixo uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, `
		INSERT INTO room_inventory (room_id, item_id, expected_qty, note)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (room_id, item_id) DO NOTHING
		RETURNING room_id`, ambiente, bem, qtd, nota).Scan(&lixo)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, db.MapError(err)
	}
	return true, nil
}
