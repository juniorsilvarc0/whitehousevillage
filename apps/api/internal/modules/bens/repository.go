package bens

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Repository é o SQL do módulo. NUNCA abre transação: o executor vem do
// contexto (db.From), e quem abre é o service.
//
// Toda consulta recebe a propriedade do ator e filtra por ela — inclusive as
// que tocam `room_inventory` e `inventory_count_lines`, que não têm a coluna e
// chegam à propriedade pelos pais (`unit_rooms`, `inventory_items`,
// `inventory_counts`).
type Repository struct {
	pool db.DBTX
}

// NewRepository monta o repositório sobre o pool.
func NewRepository(pool db.DBTX) *Repository { return &Repository{pool: pool} }

func (r *Repository) exec(ctx context.Context) db.DBTX { return db.From(ctx, r.pool) }

// Constraints cujo nome decide a resposta. Nomeadas porque a string crua
// espalhada não diz a ninguém qual regra de negócio protege.
const (
	nomeUnicoDoAmbiente = "unit_rooms_nome_unico"
)

// errEmUso é o 23503 de um DELETE recusado pelas FKs RESTRICT. O repositório
// não consegue contar os vínculos depois do erro — a transação já abortou —,
// então devolve este marcador e o service monta o 409 com os `details` numa
// leitura nova, FORA da transação desfeita. Quem DECIDE continua sendo a FK.
var errEmUso = errors.New("bens: registro segurado por chave estrangeira")

// ─────────────────────────── Montagem de WHERE ──────────────────────────────

// condicoes acumula predicados com argumentos numerados. O `$1` é sempre a
// propriedade do ator, posto pelo construtor — nenhuma listagem nasce sem ele.
type condicoes struct {
	partes []string
	args   []any
}

func novasCondicoes(propriedade uuid.UUID) *condicoes {
	return &condicoes{args: []any{propriedade}}
}

// add acrescenta um predicado. Cada `%d` do fragmento recebe o número do MESMO
// argumento (a busca em duas colunas usa um valor só).
func (c *condicoes) add(fragmento string, valor any) {
	c.args = append(c.args, valor)
	posicoes := make([]any, strings.Count(fragmento, "%d"))
	for i := range posicoes {
		posicoes[i] = len(c.args)
	}
	c.partes = append(c.partes, fmt.Sprintf(fragmento, posicoes...))
}

// fixo acrescenta um predicado sem argumento (literal deste pacote).
func (c *condicoes) fixo(predicado string) { c.partes = append(c.partes, predicado) }

func (c *condicoes) prox(valor any) int {
	c.args = append(c.args, valor)
	return len(c.args)
}

func (c *condicoes) where() string {
	if len(c.partes) == 0 {
		return ""
	}
	return " AND " + strings.Join(c.partes, " AND ")
}

// listar roda a página e o total na mesma consulta (`count(*) OVER()`), e só
// quando a página volta VAZIA com offset > 0 faz a contagem à parte — sem
// isso, pedir a página 9 de 3 responderia `total: 0`, como se a coleção
// estivesse vazia.
func listar[T any](ctx context.Context, exec db.DBTX, colunas, corpo, ordem string, c *condicoes,
	pagina, porPagina int, escanear func(pgx.Rows, *int64) (T, error)) ([]T, int64, error) {
	limite := c.prox(porPagina)
	desloc := c.prox(httpx.Offset(pagina, porPagina))
	q := `SELECT ` + colunas + `, count(*) OVER() ` + corpo +
		` ORDER BY ` + ordem + fmt.Sprintf(` LIMIT $%d OFFSET $%d`, limite, desloc)

	linhas, err := exec.Query(ctx, q, c.args...)
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	var (
		out   []T
		total int64
	)
	for linhas.Next() {
		v, err := escanear(linhas, &total)
		if err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, v)
	}
	if err := linhas.Err(); err != nil {
		return nil, 0, db.MapError(err)
	}

	if len(out) == 0 && pagina > 1 {
		// Os dois últimos argumentos são LIMIT e OFFSET; a contagem não os usa.
		if err := exec.QueryRow(ctx, `SELECT count(*) `+corpo, c.args[:len(c.args)-2]...).Scan(&total); err != nil {
			return nil, 0, db.MapError(err)
		}
	}
	return out, total, nil
}

// textos converte a lista para text[]; o SQL faz o cast para uuid. É a escolha
// de `internal/modules/inventario`: a consulta não depende de como o driver
// decide codificar um array de tipo próprio.
func textos(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

// ordemOu devolve o fragmento de ORDER BY da chave, ou o do padrão quando a
// chave não está na whitelist — nunca string vazia, que quebraria o SQL.
func ordemOu(permitidas map[string]string, chave, padrao string) string {
	if o, ok := permitidas[chave]; ok {
		return o
	}
	return permitidas[padrao]
}

// ─────────────────────────── Capa e mídia ───────────────────────────────────

// lateralDaCapa traz a CAPA do bem `i`: menor `sort_order`, desempatado por
// `media_id` — sem o desempate, bem com duas fotos no mesmo `sort_order` troca
// de capa a cada abertura da tela. Sai de `inventory_item_media_ordem_idx`.
const lateralDaCapa = `
	LEFT JOIN LATERAL (
	    SELECT m.id, m.mime, m.bytes, m.width, m.height, m.original_name, m.thumb_key, m.created_at
	      FROM inventory_item_media im
	      JOIN inventory_media m ON m.id = im.media_id
	     WHERE im.item_id = i.id
	     ORDER BY im.sort_order, im.media_id
	     LIMIT 1) cap ON true`

const colunasDaCapa = `cap.id, cap.mime, cap.bytes, cap.width, cap.height, cap.original_name, cap.thumb_key, cap.created_at`

// capaLida recebe as colunas anuláveis do LEFT JOIN da capa.
type capaLida struct {
	id             *uuid.UUID
	mime           *string
	bytes          *int64
	largura        *int
	altura         *int
	nomeOriginal   *string
	chaveMiniatura *string
	criadoEm       *time.Time
}

func (c *capaLida) destinos() []any {
	return []any{&c.id, &c.mime, &c.bytes, &c.largura, &c.altura, &c.nomeOriginal, &c.chaveMiniatura, &c.criadoEm}
}

func (c capaLida) midia() *Midia {
	if c.id == nil {
		return nil
	}
	m := Midia{
		ID: *c.id, Mime: valorOu(c.mime, ""), Bytes: valorOu(c.bytes, 0),
		Largura: c.largura, Altura: c.altura, NomeOriginal: valorOu(c.nomeOriginal, ""),
		CriadoEm: valorOu(c.criadoEm, time.Time{}),
	}
	m.URL, m.URLMiniatura = urlsDaFoto(m.ID, c.chaveMiniatura != nil)
	return &m
}

// registroDeMidia é uma linha de `inventory_media`, com as chaves do volume —
// que nunca saem na resposta.
type registroDeMidia struct {
	ID             uuid.UUID  `json:"id"`
	PropriedadeID  uuid.UUID  `json:"property_id"`
	Mime           string     `json:"mime"`
	Bytes          int64      `json:"bytes"`
	Largura        *int       `json:"width"`
	Altura         *int       `json:"height"`
	NomeOriginal   string     `json:"original_name"`
	ChaveArquivo   string     `json:"storage_key"`
	ChaveMiniatura *string    `json:"thumb_key"`
	CriadoEm       time.Time  `json:"created_at"`
	CriadoPor      *uuid.UUID `json:"created_by"`
}

func (m registroDeMidia) publica() Midia {
	out := Midia{
		ID: m.ID, Mime: m.Mime, Bytes: m.Bytes, Largura: m.Largura, Altura: m.Altura,
		NomeOriginal: m.NomeOriginal, CriadoEm: m.CriadoEm,
	}
	out.URL, out.URLMiniatura = urlsDaFoto(m.ID, m.ChaveMiniatura != nil)
	return out
}

// urlsDaFoto monta a entrega AUTENTICADA do original e da miniatura. Sem
// miniatura (WebP, ou geração que falhou), `thumb_url` sai igual a `url`: a tela
// não precisa saber da diferença, e o original nunca deixa de valer.
func urlsDaFoto(id uuid.UUID, temMiniatura bool) (original, miniatura string) {
	original = "/api/v1" + RotaDaFoto + id.String()
	if temMiniatura {
		return original, original + "?size=thumb"
	}
	return original, original
}

// RotaDaFoto é o prefixo da entrega (sem o `/api/v1`), para o Location do
// upload e as URLs não divergirem da tabela de rotas.
const RotaDaFoto = "/inventory/media/"

const colunasDaMidia = `id, property_id, mime, bytes, width, height, original_name, storage_key, thumb_key, created_at, created_by`

func escanearMidia(linha pgx.Row) (registroDeMidia, error) {
	var m registroDeMidia
	err := linha.Scan(&m.ID, &m.PropriedadeID, &m.Mime, &m.Bytes, &m.Largura, &m.Altura,
		&m.NomeOriginal, &m.ChaveArquivo, &m.ChaveMiniatura, &m.CriadoEm, &m.CriadoPor)
	return m, err
}

// CriarMidia registra o arquivo JÁ gravado no volume.
func (r *Repository) CriarMidia(ctx context.Context, m registroDeMidia) (registroDeMidia, error) {
	const q = `
		INSERT INTO inventory_media (id, property_id, mime, bytes, width, height, original_name,
		                             storage_key, thumb_key, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING ` + colunasDaMidia
	criada, err := escanearMidia(r.exec(ctx).QueryRow(ctx, q, m.ID, m.PropriedadeID, m.Mime, m.Bytes,
		m.Largura, m.Altura, m.NomeOriginal, m.ChaveArquivo, m.ChaveMiniatura, m.CriadoPor))
	if err != nil {
		return registroDeMidia{}, db.MapError(err)
	}
	return criada, nil
}

// BuscarMidia lê a foto pelo id, só na propriedade do ator. ok=false se não há.
func (r *Repository) BuscarMidia(ctx context.Context, prop, id uuid.UUID) (registroDeMidia, bool, error) {
	m, err := escanearMidia(r.exec(ctx).QueryRow(ctx,
		`SELECT `+colunasDaMidia+` FROM inventory_media WHERE id = $1 AND property_id = $2`, id, prop))
	if errors.Is(err, pgx.ErrNoRows) {
		return registroDeMidia{}, false, nil
	}
	if err != nil {
		return registroDeMidia{}, false, db.MapError(err)
	}
	return m, true, nil
}

// MidiasDaPropriedade devolve, dos ids pedidos, os que existem NESTA casa.
func (r *Repository) MidiasDaPropriedade(ctx context.Context, prop uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	linhas, err := r.exec(ctx).Query(ctx,
		`SELECT id FROM inventory_media WHERE property_id = $1 AND id = ANY($2::text[]::uuid[])`, prop, textos(ids))
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()
	for linhas.Next() {
		var id uuid.UUID
		if err := linhas.Scan(&id); err != nil {
			return nil, db.MapError(err)
		}
		out[id] = true
	}
	return out, db.MapError(linhas.Err())
}

// FotosDoBem devolve a galeria na ordem de exibição — a capa é a primeira.
func (r *Repository) FotosDoBem(ctx context.Context, bem uuid.UUID) ([]FotoDoBem, error) {
	const q = `
		SELECT im.sort_order, m.id, m.property_id, m.mime, m.bytes, m.width, m.height, m.original_name,
		       m.storage_key, m.thumb_key, m.created_at, m.created_by
		  FROM inventory_item_media im
		  JOIN inventory_media m ON m.id = im.media_id
		 WHERE im.item_id = $1
		 ORDER BY im.sort_order, im.media_id`
	linhas, err := r.exec(ctx).Query(ctx, q, bem)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []FotoDoBem{}
	for linhas.Next() {
		var (
			ordem int
			m     registroDeMidia
		)
		if err := linhas.Scan(&ordem, &m.ID, &m.PropriedadeID, &m.Mime, &m.Bytes, &m.Largura, &m.Altura,
			&m.NomeOriginal, &m.ChaveArquivo, &m.ChaveMiniatura, &m.CriadoEm, &m.CriadoPor); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, FotoDoBem{Midia: m.publica(), Ordem: ordem})
	}
	return out, db.MapError(linhas.Err())
}

// SubstituirFotos troca a galeria inteira: DELETE + INSERT na transação do
// service, com `sort_order` = posição no array. Desvincular não apaga o
// arquivo — a foto pode mostrar outros itens da mesma cena.
func (r *Repository) SubstituirFotos(ctx context.Context, bem uuid.UUID, ids []uuid.UUID) error {
	exec := r.exec(ctx)
	if _, err := exec.Exec(ctx, `DELETE FROM inventory_item_media WHERE item_id = $1`, bem); err != nil {
		return db.MapError(err)
	}
	if len(ids) == 0 {
		return nil
	}
	const q = `
		INSERT INTO inventory_item_media (item_id, media_id, sort_order)
		SELECT $1, pedido.id::uuid, pedido.ordem - 1
		  FROM unnest($2::text[]) WITH ORDINALITY AS pedido(id, ordem)`
	if _, err := exec.Exec(ctx, q, bem, textos(ids)); err != nil {
		return db.MapError(err)
	}
	return nil
}

// ─────────────────────────── Unidade ────────────────────────────────────────

// UnidadeDaPropriedade lê id, código e nome de uma unidade DESTA casa — e só
// isso: esta rota não abre o cadastro comercial.
func (r *Repository) UnidadeDaPropriedade(ctx context.Context, prop, id uuid.UUID) (UnidadeResumida, bool, error) {
	var u UnidadeResumida
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT id, code, name FROM units WHERE id = $1 AND property_id = $2`, id, prop).
		Scan(&u.ID, &u.Codigo, &u.Nome)
	if errors.Is(err, pgx.ErrNoRows) {
		return UnidadeResumida{}, false, nil
	}
	if err != nil {
		return UnidadeResumida{}, false, db.MapError(err)
	}
	return u, true, nil
}

// chaveDaTravaDeInventario é o primeiro inteiro do advisory lock por unidade.
// O valor é arbitrário e só precisa não coincidir com outro uso de
// `pg_advisory_xact_lock(int, int)` nesta base — hoje não há nenhum.
const chaveDaTravaDeInventario = 20261007

// TravarInventarioDaUnidade serializa, POR UNIDADE, as duas operações que não
// podem se cruzar: abrir conferência e copiar inventário para ela. Sem isto a
// cópia conferiria "não há conferência aberta", a abertura congelaria as linhas
// no instante seguinte, e as colocações copiadas nasceriam fora da contagem —
// que fecharia "completa" sem nunca ter olhado para elas.
//
// É trava de transação (solta no COMMIT/ROLLBACK) e respeita o `lock_timeout`
// da sessão: a espera longa vira 55P03, que o TxManager repete.
func (r *Repository) TravarInventarioDaUnidade(ctx context.Context, unidade uuid.UUID) error {
	if _, err := r.exec(ctx).Exec(ctx,
		`SELECT pg_advisory_xact_lock($1::int, hashtext($2::text))`, chaveDaTravaDeInventario, unidade.String()); err != nil {
		return db.MapError(err)
	}
	return nil
}

// ─────────────────────────── Ambientes ──────────────────────────────────────

// Os agregados contam o GRAVADO (ver Ambiente). Subconsultas correlacionadas
// em vez de GROUP BY: cada uma sai de um índice (`room_inventory` pela PK,
// `inventory_issues_room_idx`) e a listagem paginada só as avalia para as
// linhas da página.
const colunasDoAmbiente = `
	r.id, r.unit_id, u.code, u.name, r.name, r.kind, r.sort_order, r.active,
	(SELECT count(*) FROM room_inventory x WHERE x.room_id = r.id),
	(SELECT COALESCE(sum(x.expected_qty), 0) FROM room_inventory x WHERE x.room_id = r.id),
	(SELECT count(*) FROM inventory_issues ii WHERE ii.room_id = r.id AND ii.resolution IS NULL),
	r.created_at, r.updated_at`

const juncoesDoAmbiente = `
	  FROM unit_rooms r
	  JOIN units u ON u.id = r.unit_id`

func escanearAmbiente(linha pgx.Row, extras ...any) (Ambiente, error) {
	var a Ambiente
	destinos := []any{&a.ID, &a.UnidadeID, &a.UnidadeCodigo, &a.UnidadeNome, &a.Nome, &a.Tipo,
		&a.Ordem, &a.Ativo, &a.QtdBens, &a.QtdEsperadaTotal, &a.AvariasAbertas, &a.CriadoEm, &a.AtualizadoEm}
	err := linha.Scan(append(destinos, extras...)...)
	return a, err
}

// OrdensDeAmbiente é a whitelist do `?sort=` de `/rooms`. Todo valor fecha
// com `name` e `id`: sem desempate, dois cômodos com o mesmo `sort_order`
// trocam de lugar entre duas aberturas e a paginação repete ou perde linha.
var OrdensDeAmbiente = map[string]string{
	"sort_order":  "r.sort_order ASC, r.name ASC, r.id ASC",
	"-sort_order": "r.sort_order DESC, r.name ASC, r.id ASC",
	"name":        "r.name ASC, r.id ASC",
	"-name":       "r.name DESC, r.id DESC",
	"created_at":  "r.created_at ASC, r.id ASC",
	"-created_at": "r.created_at DESC, r.id DESC",
}

// OrdemPadraoDeAmbiente é a ordem de caminhada pela casa.
const OrdemPadraoDeAmbiente = "sort_order"

// ListarAmbientes é o `GET /rooms`.
func (r *Repository) ListarAmbientes(ctx context.Context, prop uuid.UUID, f FiltroDeAmbientes) ([]Ambiente, int64, error) {
	c := novasCondicoes(prop)
	if f.UnidadeID != nil {
		c.add("r.unit_id = $%d", *f.UnidadeID)
	}
	if f.Tipo != "" {
		c.add("r.kind = $%d", f.Tipo)
	}
	if f.Ativo != nil {
		c.add("r.active = $%d", *f.Ativo)
	}
	if f.Busca != "" {
		c.add("r.name ILIKE '%%' || $%d || '%%'", f.Busca)
	}
	corpo := juncoesDoAmbiente + ` WHERE r.property_id = $1` + c.where()
	return listar(ctx, r.exec(ctx), colunasDoAmbiente, corpo, ordemOu(OrdensDeAmbiente, f.Ordem, OrdemPadraoDeAmbiente), c,
		f.Pagina, f.PorPagina, func(l pgx.Rows, total *int64) (Ambiente, error) {
			return escanearAmbiente(l, total)
		})
}

// BuscarAmbiente lê o cômodo com os agregados.
func (r *Repository) BuscarAmbiente(ctx context.Context, prop, id uuid.UUID) (Ambiente, error) {
	a, err := escanearAmbiente(r.exec(ctx).QueryRow(ctx,
		`SELECT `+colunasDoAmbiente+juncoesDoAmbiente+` WHERE r.id = $1 AND r.property_id = $2`, id, prop))
	if errors.Is(err, pgx.ErrNoRows) {
		return Ambiente{}, apperr.NotFound("Ambiente")
	}
	if err != nil {
		return Ambiente{}, db.MapError(err)
	}
	return a, nil
}

// ambienteGravado é a linha de `unit_rooms` como está no banco — o "antes" da
// trilha e a base do merge do PATCH.
type ambienteGravado struct {
	ID        uuid.UUID `json:"id"`
	UnidadeID uuid.UUID `json:"unit_id"`
	Nome      string    `json:"name"`
	Tipo      string    `json:"kind"`
	Ordem     int       `json:"sort_order"`
	Ativo     bool      `json:"active"`
}

// TravarAmbiente lê o cômodo com FOR UPDATE: o "antes" da trilha não pode
// mentir sob edição concorrente, e o merge do PATCH parte dele.
func (r *Repository) TravarAmbiente(ctx context.Context, prop, id uuid.UUID) (ambienteGravado, error) {
	var a ambienteGravado
	err := r.exec(ctx).QueryRow(ctx, `
		SELECT id, unit_id, name, kind, sort_order, active
		  FROM unit_rooms WHERE id = $1 AND property_id = $2
		   FOR UPDATE`, id, prop).Scan(&a.ID, &a.UnidadeID, &a.Nome, &a.Tipo, &a.Ordem, &a.Ativo)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, apperr.NotFound("Ambiente")
	}
	if err != nil {
		return a, db.MapError(err)
	}
	return a, nil
}

// CriarAmbiente insere o cômodo. A colisão de nome na unidade é decidida pela
// constraint (`unit_rooms_nome_unico`), não por SELECT antes.
func (r *Repository) CriarAmbiente(ctx context.Context, prop uuid.UUID, a ambienteGravado) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, `
		INSERT INTO unit_rooms (property_id, unit_id, name, kind, sort_order, active)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`, prop, a.UnidadeID, a.Nome, a.Tipo, a.Ordem, a.Ativo).Scan(&id)
	if err != nil {
		return uuid.Nil, traduzirAmbiente(err)
	}
	return id, nil
}

// GravarAmbiente é o UPDATE do PUT e do PATCH (o PATCH já chega mesclado).
func (r *Repository) GravarAmbiente(ctx context.Context, prop uuid.UUID, a ambienteGravado) error {
	tag, err := r.exec(ctx).Exec(ctx, `
		UPDATE unit_rooms
		   SET name = $3, kind = $4, sort_order = $5, active = $6, updated_at = now()
		 WHERE id = $1 AND property_id = $2`, a.ID, prop, a.Nome, a.Tipo, a.Ordem, a.Ativo)
	if err != nil {
		return traduzirAmbiente(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Ambiente")
	}
	return nil
}

// ApagarAmbiente apaga DE VERDADE — e leva junto a lista de bens do cômodo
// (`room_inventory` é CASCADE). Linha de conferência ou avaria segura (RESTRICT):
// aí sai errEmUso, e o service responde 409 com o que segura.
func (r *Repository) ApagarAmbiente(ctx context.Context, prop, id uuid.UUID) error {
	tag, err := r.exec(ctx).Exec(ctx, `DELETE FROM unit_rooms WHERE id = $1 AND property_id = $2`, id, prop)
	if db.IsForeignKeyViolation(err) {
		return fmt.Errorf("%w: %w", errEmUso, err)
	}
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Ambiente")
	}
	return nil
}

// VinculosDoAmbiente conta o que segura o cômodo — os `details` do 409.
type VinculosDoAmbiente struct {
	LinhasDeConferencia int `json:"count_lines"`
	Avarias             int `json:"issues"`
}

func (r *Repository) VinculosDoAmbiente(ctx context.Context, id uuid.UUID) (VinculosDoAmbiente, error) {
	var v VinculosDoAmbiente
	err := r.exec(ctx).QueryRow(ctx, `
		SELECT (SELECT count(*) FROM inventory_count_lines WHERE room_id = $1),
		       (SELECT count(*) FROM inventory_issues WHERE room_id = $1)`, id).
		Scan(&v.LinhasDeConferencia, &v.Avarias)
	return v, db.MapError(err)
}

// AmbienteDaPropriedade diz se o cômodo é desta casa e de que unidade ele é.
func (r *Repository) AmbienteDaPropriedade(ctx context.Context, prop, id uuid.UUID) (unidade uuid.UUID, ok bool, err error) {
	err = r.exec(ctx).QueryRow(ctx,
		`SELECT unit_id FROM unit_rooms WHERE id = $1 AND property_id = $2`, id, prop).Scan(&unidade)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, db.MapError(err)
	}
	return unidade, true, nil
}

func traduzirAmbiente(err error) error {
	if db.IsUniqueViolation(err, nomeUnicoDoAmbiente) {
		return apperr.CodeInUse.
			WithMessage("Já existe um ambiente com este nome nesta unidade.").
			WithCause(err).
			WithDetails(map[string]string{"name": "já existe um ambiente com este nome nesta unidade."})
	}
	return db.MapError(err)
}

// ─────────────────────────── Bens ───────────────────────────────────────────

const colunasDoBem = `
	i.id, i.name, i.description, i.category, i.unit_measure, i.replacement_cost_cents,
	i.active, i.source_ref, ` + colunasDaCapa + `,
	(SELECT count(*) FROM inventory_item_media x WHERE x.item_id = i.id),
	(SELECT count(*) FROM room_inventory x WHERE x.item_id = i.id),
	(SELECT COALESCE(sum(x.expected_qty), 0) FROM room_inventory x WHERE x.item_id = i.id),
	(SELECT count(*) FROM inventory_issues ii WHERE ii.item_id = i.id AND ii.resolution IS NULL),
	i.created_at, i.updated_at`

const juncoesDoBem = `
	  FROM inventory_items i` + lateralDaCapa

func escanearBem(linha pgx.Row, extras ...any) (Bem, error) {
	var (
		b   Bem
		cap capaLida
	)
	destinos := []any{&b.ID, &b.Nome, &b.Descricao, &b.Categoria, &b.Medida, &b.CustoDeReposicaoCents,
		&b.Ativo, &b.OrigemDaImportacao}
	destinos = append(destinos, cap.destinos()...)
	destinos = append(destinos, &b.QtdFotos, &b.QtdColocacoes, &b.QtdEsperadaTotal, &b.AvariasAbertas,
		&b.CriadoEm, &b.AtualizadoEm)
	if err := linha.Scan(append(destinos, extras...)...); err != nil {
		return Bem{}, err
	}
	b.Capa = cap.midia()
	return b, nil
}

// OrdensDeBem é a whitelist do `?sort=` do catálogo. `category` ordena por
// categoria E DEPOIS por nome, que é como `inventory_items_catalogo_idx`
// entrega. Custo `null` (não cotado) vai para o fim nos dois sentidos: no
// topo de "-replacement_cost_cents" ele esconderia o item mais caro.
var OrdensDeBem = map[string]string{
	"name":                    "i.name ASC, i.id ASC",
	"-name":                   "i.name DESC, i.id DESC",
	"category":                "i.category ASC, i.name ASC, i.id ASC",
	"-category":               "i.category DESC, i.name ASC, i.id ASC",
	"replacement_cost_cents":  "i.replacement_cost_cents ASC NULLS LAST, i.name ASC, i.id ASC",
	"-replacement_cost_cents": "i.replacement_cost_cents DESC NULLS LAST, i.name ASC, i.id ASC",
	"created_at":              "i.created_at ASC, i.id ASC",
	"-created_at":             "i.created_at DESC, i.id DESC",
}

// OrdemPadraoDeBem é a ordem alfabética do catálogo.
const OrdemPadraoDeBem = "name"

// ListarBens é o `GET /inventory/items`.
func (r *Repository) ListarBens(ctx context.Context, prop uuid.UUID, f FiltroDeBens) ([]Bem, int64, error) {
	c := novasCondicoes(prop)
	if f.Categoria != "" {
		c.add("i.category = $%d", f.Categoria)
	}
	if f.Ativo != nil {
		c.add("i.active = $%d", *f.Ativo)
	}
	if f.Busca != "" {
		c.add("(i.name ILIKE '%%' || $%d || '%%' OR i.description ILIKE '%%' || $%d || '%%')", f.Busca)
	}
	if f.UnidadeID != nil {
		c.add(`EXISTS (SELECT 1 FROM room_inventory x JOIN unit_rooms xr ON xr.id = x.room_id
		                WHERE x.item_id = i.id AND xr.unit_id = $%d AND xr.property_id = $1)`, *f.UnidadeID)
	}
	if f.AmbienteID != nil {
		c.add(`EXISTS (SELECT 1 FROM room_inventory x WHERE x.item_id = i.id AND x.room_id = $%d)`, *f.AmbienteID)
	}
	if f.TemFoto != nil {
		if *f.TemFoto {
			c.fixo(`EXISTS (SELECT 1 FROM inventory_item_media x WHERE x.item_id = i.id)`)
		} else {
			c.fixo(`NOT EXISTS (SELECT 1 FROM inventory_item_media x WHERE x.item_id = i.id)`)
		}
	}
	corpo := juncoesDoBem + ` WHERE i.property_id = $1` + c.where()
	return listar(ctx, r.exec(ctx), colunasDoBem, corpo, ordemOu(OrdensDeBem, f.Ordem, OrdemPadraoDeBem), c,
		f.Pagina, f.PorPagina, func(l pgx.Rows, total *int64) (Bem, error) {
			return escanearBem(l, total)
		})
}

// BuscarBem lê o bem com capa e agregados.
func (r *Repository) BuscarBem(ctx context.Context, prop, id uuid.UUID) (Bem, error) {
	b, err := escanearBem(r.exec(ctx).QueryRow(ctx,
		`SELECT `+colunasDoBem+juncoesDoBem+` WHERE i.id = $1 AND i.property_id = $2`, id, prop))
	if errors.Is(err, pgx.ErrNoRows) {
		return Bem{}, apperr.NotFound("Bem")
	}
	if err != nil {
		return Bem{}, db.MapError(err)
	}
	return b, nil
}

// bemGravado é a linha de `inventory_items` como está no banco.
type bemGravado struct {
	ID                    uuid.UUID `json:"id"`
	Nome                  string    `json:"name"`
	Descricao             *string   `json:"description"`
	Categoria             string    `json:"category"`
	Medida                string    `json:"unit_measure"`
	CustoDeReposicaoCents *int64    `json:"replacement_cost_cents"`
	Ativo                 bool      `json:"active"`
}

// TravarBem lê o bem com FOR UPDATE. Serve ao merge do PATCH, à trilha e à
// galeria: duas trocas de galeria simultâneas no mesmo bem se enfileiram aqui,
// em vez de uma apagar o que a outra acabou de inserir.
func (r *Repository) TravarBem(ctx context.Context, prop, id uuid.UUID) (bemGravado, error) {
	var b bemGravado
	err := r.exec(ctx).QueryRow(ctx, `
		SELECT id, name, description, category, unit_measure, replacement_cost_cents, active
		  FROM inventory_items WHERE id = $1 AND property_id = $2
		   FOR UPDATE`, id, prop).
		Scan(&b.ID, &b.Nome, &b.Descricao, &b.Categoria, &b.Medida, &b.CustoDeReposicaoCents, &b.Ativo)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, apperr.NotFound("Bem")
	}
	if err != nil {
		return b, db.MapError(err)
	}
	return b, nil
}

// CriarBem insere o item no catálogo. `source_ref` nunca vem daqui: é a chave
// do importador.
func (r *Repository) CriarBem(ctx context.Context, prop uuid.UUID, b bemGravado) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, `
		INSERT INTO inventory_items (property_id, name, description, category, unit_measure,
		                             replacement_cost_cents, active)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`, prop, b.Nome, b.Descricao, b.Categoria, b.Medida, b.CustoDeReposicaoCents, b.Ativo).Scan(&id)
	if err != nil {
		return uuid.Nil, db.MapError(err)
	}
	return id, nil
}

// GravarBem é o UPDATE do PUT e do PATCH.
func (r *Repository) GravarBem(ctx context.Context, prop uuid.UUID, b bemGravado) error {
	tag, err := r.exec(ctx).Exec(ctx, `
		UPDATE inventory_items
		   SET name = $3, description = $4, category = $5, unit_measure = $6,
		       replacement_cost_cents = $7, active = $8, updated_at = now()
		 WHERE id = $1 AND property_id = $2`,
		b.ID, prop, b.Nome, b.Descricao, b.Categoria, b.Medida, b.CustoDeReposicaoCents, b.Ativo)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Bem")
	}
	return nil
}

// ApagarBem apaga o item que nunca foi colocado, conferido nem deu avaria. As
// três FKs que o seguram são RESTRICT; a galeria (`inventory_item_media`) é
// CASCADE e sai junto, mas os ARQUIVOS ficam (podem servir a outros itens).
func (r *Repository) ApagarBem(ctx context.Context, prop, id uuid.UUID) error {
	tag, err := r.exec(ctx).Exec(ctx, `DELETE FROM inventory_items WHERE id = $1 AND property_id = $2`, id, prop)
	if db.IsForeignKeyViolation(err) {
		return fmt.Errorf("%w: %w", errEmUso, err)
	}
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Bem")
	}
	return nil
}

// VinculosDoBem conta o que segura o item — os `details` do 409.
type VinculosDoBem struct {
	Colocacoes          int `json:"placements"`
	LinhasDeConferencia int `json:"count_lines"`
	Avarias             int `json:"issues"`
}

func (r *Repository) VinculosDoBem(ctx context.Context, id uuid.UUID) (VinculosDoBem, error) {
	var v VinculosDoBem
	err := r.exec(ctx).QueryRow(ctx, `
		SELECT (SELECT count(*) FROM room_inventory WHERE item_id = $1),
		       (SELECT count(*) FROM inventory_count_lines WHERE item_id = $1),
		       (SELECT count(*) FROM inventory_issues WHERE item_id = $1)`, id).
		Scan(&v.Colocacoes, &v.LinhasDeConferencia, &v.Avarias)
	return v, db.MapError(err)
}

// BemDaPropriedade diz se o item é desta casa.
func (r *Repository) BemDaPropriedade(ctx context.Context, prop, id uuid.UUID) (bool, error) {
	var existe bool
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM inventory_items WHERE id = $1 AND property_id = $2)`, id, prop).Scan(&existe)
	return existe, db.MapError(err)
}

// ─────────────────────────── Colocações ─────────────────────────────────────

const colunasDaColocacao = `
	ri.room_id, ri.item_id, r.name, r.kind, r.unit_id, u.code, i.name, i.category, i.unit_measure,
	i.replacement_cost_cents, ` + colunasDaCapa + `, ri.expected_qty, ri.note, ri.created_at, ri.updated_at`

// juncoesDaColocacao amarra o par aos DOIS pais e exige a propriedade nos
// dois (ver o WHERE de quem usa): `room_inventory` não tem `property_id`, e é
// aqui que a mistura de casas deixa de aparecer.
const juncoesDaColocacao = `
	  FROM room_inventory ri
	  JOIN unit_rooms r ON r.id = ri.room_id
	  JOIN units u ON u.id = r.unit_id
	  JOIN inventory_items i ON i.id = ri.item_id` + lateralDaCapa

const daPropriedadeNaColocacao = ` WHERE r.property_id = $1 AND i.property_id = $1`

func escanearColocacao(linha pgx.Row, extras ...any) (Colocacao, error) {
	var (
		c   Colocacao
		cap capaLida
	)
	destinos := []any{&c.AmbienteID, &c.BemID, &c.AmbienteNome, &c.AmbienteTipo, &c.UnidadeID,
		&c.UnidadeCodigo, &c.BemNome, &c.BemCategoria, &c.BemMedida, &c.custo}
	destinos = append(destinos, cap.destinos()...)
	destinos = append(destinos, &c.QtdEsperada, &c.Nota, &c.CriadoEm, &c.AtualizadoEm)
	if err := linha.Scan(append(destinos, extras...)...); err != nil {
		return Colocacao{}, err
	}
	c.ID = ChaveDaColocacao(c.AmbienteID, c.BemID)
	c.Capa = cap.midia()
	return c, nil
}

// Ordem de caminhada: unidade, cômodo pela ordem da casa (desempatado por nome
// e id) e, dentro dele, bem por nome e id.
const ordemDeCaminhada = "u.code ASC, r.sort_order ASC, r.name ASC, r.id ASC, i.name ASC, i.id ASC"

// OrdensDeColocacao é a whitelist do `?sort=` de `/inventory/placements`.
var OrdensDeColocacao = map[string]string{
	"room":          ordemDeCaminhada,
	"item":          "i.name ASC, i.id ASC, u.code ASC, r.sort_order ASC, r.name ASC, r.id ASC",
	"expected_qty":  "ri.expected_qty ASC, " + ordemDeCaminhada,
	"-expected_qty": "ri.expected_qty DESC, " + ordemDeCaminhada,
}

// OrdemPadraoDeColocacao é a ordem de caminhada pela casa.
const OrdemPadraoDeColocacao = "room"

// ListarColocacoes é o `GET /inventory/placements`.
func (r *Repository) ListarColocacoes(ctx context.Context, prop uuid.UUID, f FiltroDeColocacoes) ([]Colocacao, int64, error) {
	c := novasCondicoes(prop)
	if f.UnidadeID != nil {
		c.add("r.unit_id = $%d", *f.UnidadeID)
	}
	if f.AmbienteID != nil {
		c.add("ri.room_id = $%d", *f.AmbienteID)
	}
	if f.BemID != nil {
		c.add("ri.item_id = $%d", *f.BemID)
	}
	if f.Categoria != "" {
		c.add("i.category = $%d", f.Categoria)
	}
	if f.Busca != "" {
		c.add("i.name ILIKE '%%' || $%d || '%%'", f.Busca)
	}
	corpo := juncoesDaColocacao + daPropriedadeNaColocacao + c.where()
	return listar(ctx, r.exec(ctx), colunasDaColocacao, corpo, ordemOu(OrdensDeColocacao, f.Ordem, OrdemPadraoDeColocacao), c,
		f.Pagina, f.PorPagina, func(l pgx.Rows, total *int64) (Colocacao, error) {
			return escanearColocacao(l, total)
		})
}

// ColocacoesDoBem é a lista "onde está este prato" da ficha do bem.
func (r *Repository) ColocacoesDoBem(ctx context.Context, prop, bem uuid.UUID) ([]Colocacao, error) {
	return r.colocacoesOnde(ctx, prop, ` AND ri.item_id = $2`, ordemDeCaminhada, bem)
}

func (r *Repository) colocacoesOnde(ctx context.Context, prop uuid.UUID, filtro, ordem string, args ...any) ([]Colocacao, error) {
	q := `SELECT ` + colunasDaColocacao + juncoesDaColocacao + daPropriedadeNaColocacao + filtro + ` ORDER BY ` + ordem
	linhas, err := r.exec(ctx).Query(ctx, q, append([]any{prop}, args...)...)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []Colocacao{}
	for linhas.Next() {
		c, err := escanearColocacao(linhas)
		if err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, c)
	}
	return out, db.MapError(linhas.Err())
}

// BuscarColocacao lê o par, só se os DOIS lados são desta casa.
func (r *Repository) BuscarColocacao(ctx context.Context, prop, ambiente, bem uuid.UUID) (Colocacao, error) {
	c, err := escanearColocacao(r.exec(ctx).QueryRow(ctx,
		`SELECT `+colunasDaColocacao+juncoesDaColocacao+daPropriedadeNaColocacao+
			` AND ri.room_id = $2 AND ri.item_id = $3`, prop, ambiente, bem))
	if errors.Is(err, pgx.ErrNoRows) {
		return Colocacao{}, apperr.NotFound("Colocação")
	}
	if err != nil {
		return Colocacao{}, db.MapError(err)
	}
	return c, nil
}

// colocacaoGravada é a linha de `room_inventory` como está no banco.
type colocacaoGravada struct {
	AmbienteID  uuid.UUID `json:"room_id"`
	BemID       uuid.UUID `json:"item_id"`
	QtdEsperada int       `json:"expected_qty"`
	Nota        *string   `json:"note"`
}

// TravarColocacao lê o par com FOR UPDATE, exigindo a propriedade nos dois pais.
func (r *Repository) TravarColocacao(ctx context.Context, prop, ambiente, bem uuid.UUID) (colocacaoGravada, error) {
	var c colocacaoGravada
	err := r.exec(ctx).QueryRow(ctx, `
		SELECT ri.room_id, ri.item_id, ri.expected_qty, ri.note
		  FROM room_inventory ri
		  JOIN unit_rooms r ON r.id = ri.room_id
		  JOIN inventory_items i ON i.id = ri.item_id
		 WHERE ri.room_id = $2 AND ri.item_id = $3
		   AND r.property_id = $1 AND i.property_id = $1
		   FOR UPDATE OF ri`, prop, ambiente, bem).Scan(&c.AmbienteID, &c.BemID, &c.QtdEsperada, &c.Nota)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, apperr.NotFound("Colocação")
	}
	if err != nil {
		return c, db.MapError(err)
	}
	return c, nil
}

// CriarColocacao insere o par. A chave primária decide a colisão: com
// `ON CONFLICT DO NOTHING` a transação NÃO aborta, e a quantidade que já está
// lá pode ser lida na mesma transação para os `details` do 409 — a pergunta de
// quem tocou duas vezes é "quanto já tem?".
func (r *Repository) CriarColocacao(ctx context.Context, c colocacaoGravada) (criada bool, existente *int, err error) {
	var qtd int
	err = r.exec(ctx).QueryRow(ctx, `
		INSERT INTO room_inventory (room_id, item_id, expected_qty, note)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (room_id, item_id) DO NOTHING
		RETURNING expected_qty`, c.AmbienteID, c.BemID, c.QtdEsperada, c.Nota).Scan(&qtd)
	if err == nil {
		return true, nil, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, nil, db.MapError(err)
	}
	// Conflito. Em READ COMMITTED este SELECT tem foto nova e enxerga a linha
	// que a outra transação gravou.
	err = r.exec(ctx).QueryRow(ctx,
		`SELECT expected_qty FROM room_inventory WHERE room_id = $1 AND item_id = $2`, c.AmbienteID, c.BemID).Scan(&qtd)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, db.MapError(err)
	}
	return false, &qtd, nil
}

// GravarColocacao é o UPDATE do PUT e do PATCH. Não toca conferência nenhuma:
// `inventory_count_lines.expected_qty` é cópia congelada.
func (r *Repository) GravarColocacao(ctx context.Context, c colocacaoGravada) error {
	tag, err := r.exec(ctx).Exec(ctx, `
		UPDATE room_inventory SET expected_qty = $3, note = $4, updated_at = now()
		 WHERE room_id = $1 AND item_id = $2`, c.AmbienteID, c.BemID, c.QtdEsperada, c.Nota)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Colocação")
	}
	return nil
}

// ApagarColocacao tira o bem do ambiente. Nada no banco a segura.
func (r *Repository) ApagarColocacao(ctx context.Context, ambiente, bem uuid.UUID) error {
	tag, err := r.exec(ctx).Exec(ctx,
		`DELETE FROM room_inventory WHERE room_id = $1 AND item_id = $2`, ambiente, bem)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Colocação")
	}
	return nil
}

// ColocacaoExiste diz se o bem está colocado no ambiente.
func (r *Repository) ColocacaoExiste(ctx context.Context, ambiente, bem uuid.UUID) (bool, error) {
	var existe bool
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM room_inventory WHERE room_id = $1 AND item_id = $2)`, ambiente, bem).Scan(&existe)
	return existe, db.MapError(err)
}
