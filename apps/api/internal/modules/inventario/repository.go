package inventario

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

// ColunasDeOrdenacaoDoProduto e ColunasDeOrdenacaoDaUnidade são as whitelists do
// `?sort=`. O valor que o cliente manda é só a CHAVE do mapa; o que entra no
// ORDER BY é o texto DESTE arquivo. Concatenar o parâmetro seria injeção de SQL
// por definição.
var (
	ColunasDeOrdenacaoDoProduto = map[string]string{
		"sort_order": "ut.sort_order",
		"code":       "ut.code",
		"name":       "ut.name",
	}
	ColunasDeOrdenacaoDaUnidade = map[string]string{
		"code":       "u.code",
		"sort_order": "u.sort_order",
		"name":       "u.name",
	}
)

// Ordens padrão de cada coleção — as do contrato.
//
// O produto ordena por `sort_order` porque a ordem do catálogo comercial é
// dado, não alfabética. A unidade ordena por `code`, que é a identidade
// operacional e a chave por onde toda inserção em lote de stay_blocks se
// ordena.
const (
	OrdemPadraoDoProduto = "ut.sort_order ASC"
	OrdemPadraoDaUnidade = "u.code ASC"
)

// Desempates fecham TODA ordenação por uma coluna única. Sem critério
// determinístico, duas linhas de mesma chave trocam de posição entre uma página
// e outra — e uma delas some da listagem sem nunca ter sido mostrada.
const (
	desempateDoProduto = ", ut.code ASC, ut.id ASC"
	desempateDaUnidade = ", u.sort_order ASC, u.id ASC"
)

// Nomes das constraints traduzidas. Estão nomeados porque a string crua
// espalhada pelo código não diz a ninguém qual regra de negócio ela protege.
const (
	chaveNaturalDoProduto = "unit_types_property_id_code_key"
	chaveNaturalDaUnidade = "units_property_id_code_key"
)

// Status que OCUPAM o calendário. Reserva ou bloqueio nesses estados é
// operação viva; é por isso que eles impedem desativar o que sustentam.
const (
	statusDeReservaAtiva = `('hold','confirmed','checked_in')`
	// O bloco que OCUPA inventário é `hold`/`confirmed` — o mesmo predicado da
	// `stay_no_overlap`. `completed` (migration 20260826120000) descreve estadia
	// já consumada: aparece no mapa, mas não segura data nenhuma e, portanto,
	// não impede aposentar uma unidade.
	statusDeBloqueioAtivo = `('hold','confirmed')`
)

// formatoDeData é o ISO-8601 que o contrato usa em toda data de estadia.
const formatoDeData = "2006-01-02"

type Repository struct {
	pool db.DBTX
}

func NewRepository(pool db.DBTX) *Repository { return &Repository{pool: pool} }

// exec devolve a transação em curso, quando há, ou o pool. O repositório NUNCA
// abre transação: quem abre é o service.
func (r *Repository) exec(ctx context.Context) db.DBTX { return db.From(ctx, r.pool) }

// ─────────────────────────── Propriedades ───────────────────────────

const camposDaPropriedade = `
	p.id, p.name, p.slug, p.timezone, p.address, p.city, p.state,
	p.active, p.created_at, p.updated_at`

// ListarPropriedades devolve a página e o total na MESMA consulta: duas
// consultas separadas podem discordar sob concorrência e fazer o total mentir.
func (r *Repository) ListarPropriedades(ctx context.Context, propriedadeID uuid.UUID, f Filtro) ([]Propriedade, int64, error) {
	const q = `
		SELECT ` + camposDaPropriedade + `, count(*) OVER() AS total
		  FROM properties p
		 WHERE p.id = $1
		   AND ($2::boolean IS NULL OR p.active = $2)
		 ORDER BY p.name ASC, p.id ASC
		 LIMIT $3 OFFSET $4`

	linhas, err := r.exec(ctx).Query(ctx, q, propriedadeID, f.Ativo,
		f.PorPagina, httpx.Offset(f.Pagina, f.PorPagina))
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	var (
		out   []Propriedade
		total int64
	)
	for linhas.Next() {
		var p Propriedade
		if err := escanearPropriedade(linhas, &p, &total); err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, p)
	}
	return out, total, db.MapError(linhas.Err())
}

func (r *Repository) BuscarPropriedade(ctx context.Context, propriedadeID, id uuid.UUID) (Propriedade, error) {
	const q = `SELECT ` + camposDaPropriedade + ` FROM properties p WHERE p.id = $1 AND p.id = $2`

	var p Propriedade
	err := r.exec(ctx).QueryRow(ctx, q, propriedadeID, id).Scan(
		&p.ID, &p.Nome, &p.Slug, &p.Fuso, &p.Endereco, &p.Cidade, &p.UF,
		&p.Ativa, &p.CriadaEm, &p.AtualizadaEm)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, apperr.NotFound("Propriedade")
	}
	if err != nil {
		return p, db.MapError(err)
	}
	return p, nil
}

func (r *Repository) AtualizarPropriedade(ctx context.Context, propriedadeID, id uuid.UUID, a PropriedadeAtualizar) error {
	var s sets
	atribuir(&s, "name", a.Nome, false)
	atribuir(&s, "timezone", a.Fuso, false)
	atribuir(&s, "address", a.Endereco, true)
	atribuir(&s, "city", a.Cidade, true)
	atribuir(&s, "state", a.UF, true)
	atribuir(&s, "active", a.Ativa, false)

	// A mesma coluna duas vezes de propósito: o id da rota tem de ser a
	// propriedade do próprio requisitante. Sem essa segunda comparação, um id
	// conhecido de outra casa seria editável por quem não pertence a ela.
	return db.MapError(r.executarUpdate(ctx, "properties", "Propriedade", &s,
		"id = $%d AND id = $%d", id, propriedadeID))
}

// ─────────────────────────── Produtos ───────────────────────────────

const camposDoProduto = `
	ut.id, ut.code, ut.name, ut.capacity, ut.consumes, ut.cleaning_fee_cents,
	ut.description, ut.sort_order, ut.active, ut.created_at, ut.updated_at`

func (r *Repository) ListarProdutos(ctx context.Context, propriedadeID uuid.UUID, f Filtro) ([]Produto, int64, error) {
	ordem := f.OrderBy
	if ordem == "" {
		ordem = OrdemPadraoDoProduto
	}

	q := `
		SELECT ` + camposDoProduto + `, count(*) OVER() AS total
		  FROM unit_types ut
		 WHERE ut.property_id = $1
		   AND ($2::text IS NULL OR ut.code ILIKE '%' || $2 || '%' OR ut.name ILIKE '%' || $2 || '%')
		   AND ($3::boolean IS NULL OR ut.active = $3)
		 ORDER BY ` + ordem + desempateDoProduto + `
		 LIMIT $4 OFFSET $5`

	linhas, err := r.exec(ctx).Query(ctx, q, propriedadeID, nuloSeVazio(f.Busca), f.Ativo,
		f.PorPagina, httpx.Offset(f.Pagina, f.PorPagina))
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	var (
		out   []Produto
		total int64
	)
	for linhas.Next() {
		var p Produto
		if err := linhas.Scan(&p.ID, &p.Codigo, &p.Nome, &p.Capacidade, &p.Consome,
			&p.TaxaDeLimpezaCents, &p.Descricao, &p.Ordem, &p.Ativo,
			&p.CriadoEm, &p.AtualizadoEm, &total); err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, p)
	}
	return out, total, db.MapError(linhas.Err())
}

func (r *Repository) BuscarProduto(ctx context.Context, propriedadeID, id uuid.UUID) (Produto, error) {
	const q = `SELECT ` + camposDoProduto + ` FROM unit_types ut WHERE ut.id = $1 AND ut.property_id = $2`

	var p Produto
	err := r.exec(ctx).QueryRow(ctx, q, id, propriedadeID).Scan(
		&p.ID, &p.Codigo, &p.Nome, &p.Capacidade, &p.Consome, &p.TaxaDeLimpezaCents,
		&p.Descricao, &p.Ordem, &p.Ativo, &p.CriadoEm, &p.AtualizadoEm)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, apperr.NotFound("Produto")
	}
	if err != nil {
		return p, db.MapError(err)
	}
	return p, nil
}

// CriarProduto grava o produto SEM composição: ela é definida em
// PUT /unit-types/{id}/members, e sem ela nenhuma venda é possível.
func (r *Repository) CriarProduto(ctx context.Context, propriedadeID uuid.UUID, c ProdutoEntrada) (uuid.UUID, error) {
	const q = `
		INSERT INTO unit_types
		       (property_id, code, name, capacity, consumes, cleaning_fee_cents, description, sort_order, active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`

	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, propriedadeID, c.Codigo, c.Nome, c.Capacidade,
		c.Consome, c.TaxaDeLimpezaCents, c.Descricao, c.Ordem, c.Ativo).Scan(&id)
	if err != nil {
		return uuid.Nil, r.traduzirProduto(err)
	}
	return id, nil
}

// SubstituirProduto é o PUT: substituição integral. Todo campo do schema é
// escrito, inclusive os que voltaram ao padrão por omissão.
func (r *Repository) SubstituirProduto(ctx context.Context, propriedadeID, id uuid.UUID, c ProdutoEntrada) error {
	const q = `
		UPDATE unit_types
		   SET code = $3, name = $4, capacity = $5, consumes = $6,
		       cleaning_fee_cents = $7, description = $8, sort_order = $9,
		       active = $10, updated_at = now()
		 WHERE id = $1 AND property_id = $2`

	tag, err := r.exec(ctx).Exec(ctx, q, id, propriedadeID, c.Codigo, c.Nome, c.Capacidade,
		c.Consome, c.TaxaDeLimpezaCents, c.Descricao, c.Ordem, c.Ativo)
	if err != nil {
		return r.traduzirProduto(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Produto")
	}
	return nil
}

func (r *Repository) AtualizarProduto(ctx context.Context, propriedadeID, id uuid.UUID, a ProdutoAtualizar) error {
	var s sets
	atribuir(&s, "code", a.Codigo, false)
	atribuir(&s, "name", a.Nome, false)
	atribuir(&s, "capacity", a.Capacidade, false)
	atribuir(&s, "consumes", a.Consome, false)
	atribuir(&s, "cleaning_fee_cents", a.TaxaDeLimpezaCents, false)
	atribuir(&s, "description", a.Descricao, true)
	atribuir(&s, "sort_order", a.Ordem, false)
	atribuir(&s, "active", a.Ativo, false)

	if err := r.executarUpdate(ctx, "unit_types", "Produto", &s,
		"id = $%d AND property_id = $%d", id, propriedadeID); err != nil {
		return r.traduzirProduto(err)
	}
	return nil
}

// DesativarProduto é o DELETE do contrato: `active = false`, nunca remoção.
// Apagar produto referenciado por reserva ou por tarifa levaria o histórico
// junto — e o que a reserva antiga vendeu deixaria de ter nome.
func (r *Repository) DesativarProduto(ctx context.Context, propriedadeID, id uuid.UUID) error {
	const q = `UPDATE unit_types SET active = false, updated_at = now()
	            WHERE id = $1 AND property_id = $2`

	tag, err := r.exec(ctx).Exec(ctx, q, id, propriedadeID)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Produto")
	}
	return nil
}

// ContarReservasAtivasDoProduto alimenta o details.reservations_count do 409.
func (r *Repository) ContarReservasAtivasDoProduto(ctx context.Context, id uuid.UUID) (int, error) {
	const q = `SELECT count(*) FROM reservations
	            WHERE unit_type_id = $1 AND status IN ` + statusDeReservaAtiva

	var n int
	if err := r.exec(ctx).QueryRow(ctx, q, id).Scan(&n); err != nil {
		return 0, db.MapError(err)
	}
	return n, nil
}

// ─────────────────────────── Composição ─────────────────────────────

// Composicao devolve `unit_type_members` SEMPRE ordenado por `units.code`.
//
// Não é estética: é a mesma ordem em que o servidor insere as linhas de
// stay_blocks ao vender a Completa. Se a tela lesse numa ordem e o servidor
// inserisse em outra, duas transações concorrentes travariam as mesmas oito
// unidades em sequências diferentes — que é a definição de deadlock.
func (r *Repository) Composicao(ctx context.Context, unitTypeID uuid.UUID) ([]UnidadeDaComposicao, error) {
	const q = `
		SELECT u.id, u.code, u.name, u.active
		  FROM unit_type_members m
		  JOIN units u ON u.id = m.unit_id
		 WHERE m.unit_type_id = $1
		 ORDER BY u.code ASC`

	linhas, err := r.exec(ctx).Query(ctx, q, unitTypeID)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []UnidadeDaComposicao{}
	for linhas.Next() {
		var m UnidadeDaComposicao
		if err := linhas.Scan(&m.UnidadeID, &m.Codigo, &m.Nome, &m.Ativa); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, m)
	}
	return out, db.MapError(linhas.Err())
}

// UnidadesDaPropriedade devolve, dos ids pedidos, quais existem NA PROPRIEDADE
// do requisitante — e em que estado. O service compara com o que foi pedido
// para dizer, por índice, qual unidade não existe — em vez de deixar a FK
// estourar sem apontar o campo.
//
// `active` entra no SELECT porque a composição também recusa unidade INATIVA
// (service.go): a chave do mapa responde "existe aqui?" e o valor responde
// "está vendável?", que são as duas perguntas que a validação faz.
func (r *Repository) UnidadesDaPropriedade(ctx context.Context, propriedadeID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]UnidadeResumida, error) {
	const q = `
		SELECT u.id, u.code, u.active
		  FROM units u
		  JOIN unnest($2::text[]) AS pedido(id) ON u.id = pedido.id::uuid
		 WHERE u.property_id = $1`

	linhas, err := r.exec(ctx).Query(ctx, q, propriedadeID, textosDeUUID(ids))
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	achadas := map[uuid.UUID]UnidadeResumida{}
	for linhas.Next() {
		var (
			id uuid.UUID
			u  UnidadeResumida
		)
		if err := linhas.Scan(&id, &u.Codigo, &u.Ativa); err != nil {
			return nil, db.MapError(err)
		}
		achadas[id] = u
	}
	return achadas, db.MapError(linhas.Err())
}

// SubstituirComposicao troca o conjunto inteiro. Roda DENTRO da transação
// aberta pelo service — se o INSERT falhar, o produto não fica sem composição
// nenhuma, que é o estado em que ele viraria overbooking silencioso.
//
// DELETE + INSERT em vez de diferença: a tela manda o estado completo da grade,
// e calcular diferença no cliente é onde nasce o membro fantasma que ninguém
// marcou (mesma forma do PUT /roles/{id}/permissions).
func (r *Repository) SubstituirComposicao(ctx context.Context, propriedadeID, unitTypeID uuid.UUID, ids []uuid.UUID) error {
	exec := r.exec(ctx)

	if _, err := exec.Exec(ctx,
		`DELETE FROM unit_type_members WHERE unit_type_id = $1`, unitTypeID); err != nil {
		return db.MapError(err)
	}
	if len(ids) == 0 {
		return nil
	}

	// ORDER BY u.code no INSERT ... SELECT não é enfeite: é o que garante que
	// as linhas nasçam na MESMA ordem em que o motor de reservas trava as
	// unidades. Ordens divergentes entre transações concorrentes causam
	// deadlock (db.md §1).
	const q = `
		INSERT INTO unit_type_members (unit_type_id, unit_id)
		SELECT $1, u.id
		  FROM units u
		  JOIN unnest($2::text[]) AS pedido(id) ON u.id = pedido.id::uuid
		 WHERE u.property_id = $3
		 ORDER BY u.code ASC`

	if _, err := exec.Exec(ctx, q, unitTypeID, textosDeUUID(ids), propriedadeID); err != nil {
		return db.MapError(err)
	}
	return nil
}

// ─────────────────────────── Unidades ───────────────────────────────

const camposDaUnidade = `
	u.id, u.code, u.name, u.floor, u.notes, u.sort_order, u.active,
	u.created_at, u.updated_at`

func (r *Repository) ListarUnidades(ctx context.Context, propriedadeID uuid.UUID, f Filtro) ([]Unidade, int64, error) {
	ordem := f.OrderBy
	if ordem == "" {
		ordem = OrdemPadraoDaUnidade
	}

	q := `
		SELECT ` + camposDaUnidade + `, count(*) OVER() AS total
		  FROM units u
		 WHERE u.property_id = $1
		   AND ($2::text IS NULL OR u.code ILIKE '%' || $2 || '%' OR u.name ILIKE '%' || $2 || '%')
		   AND ($3::boolean IS NULL OR u.active = $3)
		   AND ($4::uuid IS NULL OR EXISTS (
		         SELECT 1 FROM unit_type_members m
		          WHERE m.unit_id = u.id AND m.unit_type_id = $4))
		 ORDER BY ` + ordem + desempateDaUnidade + `
		 LIMIT $5 OFFSET $6`

	linhas, err := r.exec(ctx).Query(ctx, q, propriedadeID, nuloSeVazio(f.Busca), f.Ativo,
		f.ProdutoID, f.PorPagina, httpx.Offset(f.Pagina, f.PorPagina))
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	var (
		out   []Unidade
		total int64
	)
	for linhas.Next() {
		var u Unidade
		if err := linhas.Scan(&u.ID, &u.Codigo, &u.Nome, &u.Andar, &u.Observacoes,
			&u.Ordem, &u.Ativa, &u.CriadaEm, &u.AtualizadaEm, &total); err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, u)
	}
	return out, total, db.MapError(linhas.Err())
}

func (r *Repository) BuscarUnidade(ctx context.Context, propriedadeID, id uuid.UUID) (Unidade, error) {
	const q = `SELECT ` + camposDaUnidade + ` FROM units u WHERE u.id = $1 AND u.property_id = $2`

	var u Unidade
	err := r.exec(ctx).QueryRow(ctx, q, id, propriedadeID).Scan(
		&u.ID, &u.Codigo, &u.Nome, &u.Andar, &u.Observacoes, &u.Ordem, &u.Ativa,
		&u.CriadaEm, &u.AtualizadaEm)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, apperr.NotFound("Unidade")
	}
	if err != nil {
		return u, db.MapError(err)
	}
	return u, nil
}

func (r *Repository) CriarUnidade(ctx context.Context, propriedadeID uuid.UUID, c UnidadeEntrada) (uuid.UUID, error) {
	const q = `
		INSERT INTO units (property_id, code, name, floor, notes, sort_order, active)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`

	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, propriedadeID, c.Codigo, c.Nome,
		c.Andar, c.Observacoes, c.Ordem, c.Ativa).Scan(&id)
	if err != nil {
		return uuid.Nil, r.traduzirUnidade(err)
	}
	return id, nil
}

func (r *Repository) SubstituirUnidade(ctx context.Context, propriedadeID, id uuid.UUID, c UnidadeEntrada) error {
	const q = `
		UPDATE units
		   SET code = $3, name = $4, floor = $5, notes = $6,
		       sort_order = $7, active = $8, updated_at = now()
		 WHERE id = $1 AND property_id = $2`

	tag, err := r.exec(ctx).Exec(ctx, q, id, propriedadeID, c.Codigo, c.Nome,
		c.Andar, c.Observacoes, c.Ordem, c.Ativa)
	if err != nil {
		return r.traduzirUnidade(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Unidade")
	}
	return nil
}

func (r *Repository) AtualizarUnidade(ctx context.Context, propriedadeID, id uuid.UUID, a UnidadeAtualizar) error {
	var s sets
	atribuir(&s, "code", a.Codigo, false)
	atribuir(&s, "name", a.Nome, false)
	atribuir(&s, "floor", a.Andar, true)
	atribuir(&s, "notes", a.Observacoes, true)
	atribuir(&s, "sort_order", a.Ordem, false)
	atribuir(&s, "active", a.Ativa, false)

	if err := r.executarUpdate(ctx, "units", "Unidade", &s,
		"id = $%d AND property_id = $%d", id, propriedadeID); err != nil {
		return r.traduzirUnidade(err)
	}
	return nil
}

func (r *Repository) DesativarUnidade(ctx context.Context, propriedadeID, id uuid.UUID) error {
	const q = `UPDATE units SET active = false, updated_at = now()
	            WHERE id = $1 AND property_id = $2`

	tag, err := r.exec(ctx).Exec(ctx, q, id, propriedadeID)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Unidade")
	}
	return nil
}

// ContarBloqueiosFuturosDaUnidade conta ocupação viva que ainda não terminou.
//
// "Hoje" sai do FUSO DA PROPRIEDADE, não do relógio do servidor: a spec exige
// uma definição única de hoje, e um servidor em UTC consideraria já passada,
// depois das 21h de Fortaleza, uma estadia que termina amanhã.
//
// A comparação é com `upper(period)`, que é o dia de check-out e NÃO pertence à
// estadia (daterange half-open). Estadia terminada não impede desativar.
func (r *Repository) ContarBloqueiosFuturosDaUnidade(ctx context.Context, propriedadeID, id uuid.UUID) (int, error) {
	const q = `
		SELECT count(*)
		  FROM stay_blocks sb
		 WHERE sb.unit_id = $1
		   AND sb.status IN ` + statusDeBloqueioAtivo + `
		   AND upper(sb.period) > (now() AT TIME ZONE
		         (SELECT timezone FROM properties WHERE id = $2))::date`

	var n int
	if err := r.exec(ctx).QueryRow(ctx, q, id, propriedadeID).Scan(&n); err != nil {
		return 0, db.MapError(err)
	}
	return n, nil
}

// ProdutosQueUsamAUnidade devolve os produtos que ainda a consomem, com o
// `consumes` de cada um. Desativar unidade que compõe um produto vendável faria
// a venda seguinte travar menos unidades do que promete — a Completa deixaria
// de ser exclusiva sem ninguém ter mudado a regra.
//
// `consumes` vem junto para a recusa poder dizer QUAL dano ela está evitando:
// `all_members` é quebra de exclusividade (a casa inteira vendida com sete das
// oito unidades), `one_member` é só encolhimento do inventário.
func (r *Repository) ProdutosQueUsamAUnidade(ctx context.Context, id uuid.UUID) ([]VinculoDeComposicao, error) {
	const q = `
		SELECT ut.code, ut.consumes
		  FROM unit_type_members m
		  JOIN unit_types ut ON ut.id = m.unit_type_id
		 WHERE m.unit_id = $1
		 ORDER BY ut.code ASC`

	linhas, err := r.exec(ctx).Query(ctx, q, id)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []VinculoDeComposicao{}
	for linhas.Next() {
		var v VinculoDeComposicao
		if err := linhas.Scan(&v.Codigo, &v.Consome); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, v)
	}
	return out, db.MapError(linhas.Err())
}

// ReservasExclusivasSemAUnidade responde à pergunta da REATIVAÇÃO: existe venda
// de produto `all_members` de pé, ainda no futuro, que contém esta unidade na
// composição e NÃO tem bloco nela?
//
// Cada linha devolvida é um buraco: a casa inteira foi vendida enquanto a
// unidade estava fora do ar, então o alocador travou as outras sete e esta
// ficou sem `stay_blocks`. Reativá-la a devolve ao inventário vendável DENTRO
// das datas de uma estadia exclusiva já fechada — e a `EXCLUDE` não protege,
// porque não há linha nenhuma com que a próxima venda possa colidir.
//
// O `NOT EXISTS` é o coração: `sb` prova que a reserva ocupa o calendário; o
// subselect prova que ESTA unidade ficou de fora dela. Hoje sai do fuso da
// propriedade, como no resto do módulo — estadia já terminada não segura mais
// nada, e `upper(period)` é o dia de check-out, que não pertence à estadia.
func (r *Repository) ReservasExclusivasSemAUnidade(ctx context.Context, propriedadeID, id uuid.UUID) ([]ReservaExclusiva, error) {
	const q = `
		SELECT r.code, ut.code,
		       min(lower(sb.period)) AS entrada,
		       max(upper(sb.period)) AS saida
		  FROM unit_type_members m
		  JOIN unit_types ut ON ut.id = m.unit_type_id
		                    AND ut.consumes = '` + ConsomeTodosMembros + `'
		                    AND ut.property_id = $2
		  JOIN reservations r ON r.unit_type_id = ut.id
		                     AND r.status IN ` + statusDeReservaAtiva + `
		  JOIN stay_blocks sb ON sb.reservation_id = r.id
		                     AND sb.status IN ` + statusDeBloqueioAtivo + `
		 WHERE m.unit_id = $1
		   AND upper(sb.period) > (now() AT TIME ZONE
		         (SELECT timezone FROM properties WHERE id = $2))::date
		   AND NOT EXISTS (
		         SELECT 1
		           FROM stay_blocks meu
		          WHERE meu.reservation_id = r.id
		            AND meu.unit_id = $1
		            AND meu.status IN ` + statusDeBloqueioAtivo + `)
		 GROUP BY r.id, r.code, ut.code
		 ORDER BY entrada ASC`

	linhas, err := r.exec(ctx).Query(ctx, q, id, propriedadeID)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []ReservaExclusiva{}
	for linhas.Next() {
		var (
			res            ReservaExclusiva
			entrada, saida time.Time
		)
		if err := linhas.Scan(&res.Codigo, &res.ProdutoCodigo, &entrada, &saida); err != nil {
			return nil, db.MapError(err)
		}
		res.CheckIn = entrada.Format(formatoDeData)
		res.CheckOut = saida.Format(formatoDeData)
		out = append(out, res)
	}
	return out, db.MapError(linhas.Err())
}

// ─────────────────────────── Tradução de erro ───────────────────────

// traduzirProduto e traduzirUnidade transformam a violação da chave natural no
// 409 CODE_IN_USE que o contrato promete. Quem decide é a CONSTRAINT: um SELECT
// antes do INSERT perderia a corrida contra outra requisição no mesmo instante.
func (r *Repository) traduzirProduto(err error) error {
	if db.IsUniqueViolation(err, chaveNaturalDoProduto) {
		return CodeInUse.WithCause(err).WithDetails(map[string]string{
			"code": "já existe um produto com esse código nesta propriedade.",
		})
	}
	return db.MapError(err)
}

func (r *Repository) traduzirUnidade(err error) error {
	if db.IsUniqueViolation(err, chaveNaturalDaUnidade) {
		return CodeInUse.WithCause(err).WithDetails(map[string]string{
			"code": "já existe uma unidade com esse código nesta propriedade.",
		})
	}
	return db.MapError(err)
}

// ─────────────────────────── SET dinâmico ───────────────────────────

// sets acumula o SET de um PATCH. Nomes de coluna são literais dos arquivos
// deste pacote; só valores viram argumento.
type sets struct {
	colunas []string
	args    []any
}

// atribuir traduz um Opt para o SET, respeitando os três estados: ausente não
// entra na consulta, `null` vira `coluna = NULL` (só onde a coluna aceita), e
// valor vira argumento.
//
// É função e não método porque Go não permite método genérico — e sem o
// genérico, esta mesma lógica apareceria seis vezes.
func atribuir[T any](s *sets, coluna string, o httpx.Opt[T], anulavel bool) {
	if v, ok := o.Definido(); ok {
		s.args = append(s.args, v)
		s.colunas = append(s.colunas, fmt.Sprintf("%s = $%d", coluna, len(s.args)))
		return
	}
	if anulavel && o.DeveLimpar() {
		s.colunas = append(s.colunas, coluna+" = NULL")
	}
}

// executarUpdate roda o UPDATE montado. PATCH sem nenhum campo é no-op de
// propósito: devolver erro obrigaria a tela a checar antes de salvar um
// formulário que ninguém tocou.
func (r *Repository) executarUpdate(ctx context.Context, tabela, recurso string, s *sets, formatoDoWhere string, chaves ...any) error {
	if len(s.colunas) == 0 {
		return nil
	}

	posicoes := make([]any, 0, len(chaves))
	for i := range chaves {
		posicoes = append(posicoes, len(s.args)+i+1)
	}
	where := fmt.Sprintf(formatoDoWhere, posicoes...)

	q := `UPDATE ` + tabela + ` SET ` + strings.Join(s.colunas, ", ") +
		`, updated_at = now() WHERE ` + where

	tag, err := r.exec(ctx).Exec(ctx, q, append(s.args, chaves...)...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound(recurso)
	}
	return nil
}

// ─────────────────────────── Utilidades ─────────────────────────────

func escanearPropriedade(linhas pgx.Rows, p *Propriedade, total *int64) error {
	return linhas.Scan(&p.ID, &p.Nome, &p.Slug, &p.Fuso, &p.Endereco, &p.Cidade,
		&p.UF, &p.Ativa, &p.CriadaEm, &p.AtualizadaEm, total)
}

func nuloSeVazio(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// textosDeUUID converte a lista para text[]. O SQL faz o cast explícito para
// uuid: assim a consulta não depende de como o driver decide codificar um
// array de tipo próprio, e um id malformado vira erro do Postgres com posição.
func textosDeUUID(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

// ReservasVivasDoProduto devolve as vendas do produto que AINDA OCUPAM o
// calendário, com as unidades que cada uma de fato segura.
//
// É a consulta do CRÍTICO desta rodada. `ContarReservasAtivasDoProduto` não
// serve aqui e a diferença importa: aquela conta reservas por STATUS e é usada
// para recusar desativar o produto; esta exige BLOCO VIVO E FUTURO, porque a
// pergunta é outra — "existe venda cuja alocação derivou da composição e ainda
// vai acontecer?". Reserva em `quote` não tem bloco nenhum e não entra;
// estadia já terminada não segura mais nada e também não entra.
//
// `upper(sb.period) > hoje` usa o fuso DA PROPRIEDADE, como o resto do módulo:
// `upper` é o dia de check-out e não pertence à estadia (daterange half-open),
// e um servidor em UTC consideraria terminada, depois das 21h de Fortaleza, uma
// estadia que termina amanhã.
//
// Os ids saem como `text` pelo mesmo motivo de `textosDeUUID`: não depender de
// como o driver decide codificar um array de tipo próprio.
func (r *Repository) ReservasVivasDoProduto(ctx context.Context, propriedadeID, unitTypeID uuid.UUID) ([]ReservaViva, error) {
	const q = `
		SELECT r.id, r.code,
		       min(lower(sb.period)) AS entrada,
		       max(upper(sb.period)) AS saida,
		       array_agg(DISTINCT sb.unit_id::text),
		       array_agg(DISTINCT u.code ORDER BY u.code)
		  FROM reservations r
		  JOIN stay_blocks sb ON sb.reservation_id = r.id
		                     AND sb.status IN ` + statusDeBloqueioAtivo + `
		  JOIN units u ON u.id = sb.unit_id
		 WHERE r.unit_type_id = $1
		   AND r.property_id = $2
		   AND r.status IN ` + statusDeReservaAtiva + `
		   AND upper(sb.period) > (now() AT TIME ZONE
		         (SELECT timezone FROM properties WHERE id = $2))::date
		 GROUP BY r.id, r.code
		 ORDER BY entrada ASC, r.code ASC`

	linhas, err := r.exec(ctx).Query(ctx, q, unitTypeID, propriedadeID)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []ReservaViva{}
	for linhas.Next() {
		var (
			res            ReservaViva
			entrada, saida time.Time
			ids            []string
		)
		if err := linhas.Scan(&res.ID, &res.Codigo, &entrada, &saida, &ids, &res.Unidades); err != nil {
			return nil, db.MapError(err)
		}
		res.CheckIn = entrada.Format(formatoDeData)
		res.CheckOut = saida.Format(formatoDeData)
		for _, bruto := range ids {
			id, err := uuid.Parse(bruto)
			if err != nil {
				return nil, db.MapError(err)
			}
			res.UnidadeIDs = append(res.UnidadeIDs, id)
		}
		out = append(out, res)
	}
	return out, db.MapError(linhas.Err())
}
