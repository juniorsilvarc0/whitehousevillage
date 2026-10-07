package bens

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// ─────────────────────────── Seletor de unidades ────────────────────────────

// colunasDaUnidadeDoInventario lê de `units` SÓ id, code, name e active — o
// resto vem das tabelas deste módulo. Os números contam o que a contagem olha:
// ambientes ativos, e bens distintos colocados neles (só desta casa, porque
// `room_inventory` não tem `property_id`). A conferência aberta é no máximo uma
// (`inventory_counts_aberta_idx`), então a subconsulta é escalar.
const colunasDaUnidadeDoInventario = `
	u.id, u.code, u.name, u.active,
	(SELECT count(*) FROM unit_rooms r WHERE r.unit_id = u.id AND r.active),
	(SELECT count(DISTINCT ri.item_id)
	   FROM room_inventory ri
	   JOIN unit_rooms r ON r.id = ri.room_id
	   JOIN inventory_items i ON i.id = ri.item_id
	  WHERE r.unit_id = u.id AND r.active AND i.property_id = u.property_id),
	(SELECT c.id FROM inventory_counts c WHERE c.unit_id = u.id AND c.status = 'aberta'),
	(SELECT max(c.closed_at) FROM inventory_counts c WHERE c.unit_id = u.id AND c.status = 'fechada')`

// ListarUnidadesDoInventario é o `GET /inventory/units`, por `code` e `id`.
func (r *Repository) ListarUnidadesDoInventario(ctx context.Context, prop uuid.UUID, f FiltroDeUnidades) ([]UnidadeDoInventario, int64, error) {
	c := novasCondicoes(prop)
	if f.Ativa != nil {
		c.add("u.active = $%d", *f.Ativa)
	}
	if f.Busca != "" {
		c.add("(u.code ILIKE '%%' || $%d || '%%' OR u.name ILIKE '%%' || $%d || '%%')", f.Busca)
	}
	corpo := ` FROM units u WHERE u.property_id = $1` + c.where()
	return listar(ctx, r.exec(ctx), colunasDaUnidadeDoInventario, corpo, "u.code ASC, u.id ASC", c,
		f.Pagina, f.PorPagina, func(l pgx.Rows, total *int64) (UnidadeDoInventario, error) {
			var u UnidadeDoInventario
			err := l.Scan(&u.ID, &u.Codigo, &u.Nome, &u.Ativa, &u.Ambientes, &u.Bens,
				&u.ConferenciaAberta, &u.UltimaFechadaEm, total)
			return u, err
		})
}

// ─────────────────────────── Tela da unidade ────────────────────────────────

// AmbientesDaUnidade lista os cômodos da unidade na ordem de caminhada, com
// os agregados. Inativos só na visão de auditoria (`include_inactive`).
func (r *Repository) AmbientesDaUnidade(ctx context.Context, prop, unidade uuid.UUID, incluirInativos bool) ([]Ambiente, error) {
	q := `SELECT ` + colunasDoAmbiente + juncoesDoAmbiente +
		` WHERE r.property_id = $1 AND r.unit_id = $2`
	if !incluirInativos {
		q += ` AND r.active`
	}
	q += ` ORDER BY r.sort_order ASC, r.name ASC, r.id ASC`

	linhas, err := r.exec(ctx).Query(ctx, q, prop, unidade)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []Ambiente{}
	for linhas.Next() {
		a, err := escanearAmbiente(linhas)
		if err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, a)
	}
	return out, db.MapError(linhas.Err())
}

// ColocacoesDaUnidade traz os bens de todos os cômodos da unidade, na ordem de
// caminhada (cômodo, depois bem por nome). Sem `include_inactive`, ficam de
// fora cômodo e bem desativados — o que a contagem também não olha.
func (r *Repository) ColocacoesDaUnidade(ctx context.Context, prop, unidade uuid.UUID, f FiltroDaUnidade) ([]Colocacao, error) {
	c := &condicoes{args: []any{prop, unidade}}
	if !f.IncluirInativos {
		c.fixo("r.active AND i.active")
	}
	if f.Categoria != "" {
		c.add("i.category = $%d", f.Categoria)
	}
	if f.Busca != "" {
		c.add("i.name ILIKE '%%' || $%d || '%%'", f.Busca)
	}
	return r.colocacoesOnde(ctx, prop, ` AND r.unit_id = $2`+c.where(), ordemDeCaminhada, c.args[1:]...)
}

// ─────────────────────────── Exportação ─────────────────────────────────────

// linhaExportada é uma linha da planilha: uma colocação com o que o gestor lê.
type linhaExportada struct {
	UnidadeCodigo  string
	AmbienteNome   string
	AmbienteTipo   string
	BemNome        string
	BemDescricao   *string
	Categoria      string
	Medida         string
	QtdEsperada    int
	Custo          *int64
	AvariasAbertas int
}

// LinhasDaExportacao traz TODAS as colocações do recorte, sem paginação — a
// exportação truncada parece completa, e isso é pior do que nenhuma. As
// pendências abertas são do par (cômodo, bem).
func (r *Repository) LinhasDaExportacao(ctx context.Context, prop uuid.UUID, f FiltroDeExportacao) ([]linhaExportada, error) {
	c := novasCondicoes(prop)
	if f.UnidadeID != nil {
		c.add("r.unit_id = $%d", *f.UnidadeID)
	}
	if f.AmbienteID != nil {
		c.add("ri.room_id = $%d", *f.AmbienteID)
	}
	if f.Categoria != "" {
		c.add("i.category = $%d", f.Categoria)
	}
	if !f.IncluirInativos {
		c.fixo("r.active AND i.active")
	}
	q := `
		SELECT u.code, r.name, r.kind, i.name, i.description, i.category, i.unit_measure,
		       ri.expected_qty, i.replacement_cost_cents,
		       (SELECT count(*) FROM inventory_issues ii
		         WHERE ii.room_id = ri.room_id AND ii.item_id = ri.item_id AND ii.resolution IS NULL)
		  FROM room_inventory ri
		  JOIN unit_rooms r ON r.id = ri.room_id
		  JOIN units u ON u.id = r.unit_id
		  JOIN inventory_items i ON i.id = ri.item_id` +
		daPropriedadeNaColocacao + c.where() + ` ORDER BY ` + ordemDeCaminhada

	linhas, err := r.exec(ctx).Query(ctx, q, c.args...)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []linhaExportada{}
	for linhas.Next() {
		var l linhaExportada
		if err := linhas.Scan(&l.UnidadeCodigo, &l.AmbienteNome, &l.AmbienteTipo, &l.BemNome, &l.BemDescricao,
			&l.Categoria, &l.Medida, &l.QtdEsperada, &l.Custo, &l.AvariasAbertas); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, l)
	}
	return out, db.MapError(linhas.Err())
}

// DadosDoArquivo devolve o slug da propriedade e o "hoje" NO FUSO DELA — a
// mesma fonte de "hoje" de reservas e tarifário (`properties.timezone`), para o
// nome do arquivo não mudar de dia às 21h de Fortaleza num servidor em UTC.
func (r *Repository) DadosDoArquivo(ctx context.Context, prop uuid.UUID) (slug, hoje string, err error) {
	err = r.exec(ctx).QueryRow(ctx,
		`SELECT slug, (now() AT TIME ZONE timezone)::date::text FROM properties WHERE id = $1`, prop).
		Scan(&slug, &hoje)
	return slug, hoje, db.MapError(err)
}

// ─────────────────────────── Cópia entre unidades ───────────────────────────

// AmbientesParaCopia lista os cômodos da unidade (só os ativos, se pedido), na
// ordem de caminhada.
func (r *Repository) AmbientesParaCopia(ctx context.Context, prop, unidade uuid.UUID, soAtivos bool) ([]ambienteGravado, error) {
	q := `SELECT id, unit_id, name, kind, sort_order, active FROM unit_rooms
	       WHERE property_id = $1 AND unit_id = $2`
	if soAtivos {
		q += ` AND active`
	}
	q += ` ORDER BY sort_order, name, id`

	linhas, err := r.exec(ctx).Query(ctx, q, prop, unidade)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []ambienteGravado{}
	for linhas.Next() {
		var a ambienteGravado
		if err := linhas.Scan(&a.ID, &a.UnidadeID, &a.Nome, &a.Tipo, &a.Ordem, &a.Ativo); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, a)
	}
	return out, db.MapError(linhas.Err())
}

// ColocacoesParaCopia lista as colocações da unidade por NOME de cômodo — que
// é a chave pela qual a cópia casa origem e destino. Na origem (`soAtivos`)
// entram só cômodo e bem ativos: copiar o que saiu de linha criaria colocação
// que nenhuma contagem olha.
func (r *Repository) ColocacoesParaCopia(ctx context.Context, prop, unidade uuid.UUID, soAtivos bool) ([]colocacaoDaCopia, error) {
	q := `
		SELECT r.name, ri.item_id, i.name, ri.expected_qty
		  FROM room_inventory ri
		  JOIN unit_rooms r ON r.id = ri.room_id
		  JOIN inventory_items i ON i.id = ri.item_id
		 WHERE r.property_id = $1 AND i.property_id = $1 AND r.unit_id = $2`
	if soAtivos {
		q += ` AND r.active AND i.active`
	}
	q += ` ORDER BY r.sort_order, r.name, r.id, i.name, i.id`

	linhas, err := r.exec(ctx).Query(ctx, q, prop, unidade)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []colocacaoDaCopia{}
	for linhas.Next() {
		var c colocacaoDaCopia
		if err := linhas.Scan(&c.AmbienteNome, &c.BemID, &c.BemNome, &c.Qtd); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, c)
	}
	return out, db.MapError(linhas.Err())
}

// CriarAmbienteSeAusente cria o cômodo no destino; se o nome já existe (outra
// aba criou entre o plano e a execução), a constraint decide e nada acontece —
// a cópia só ACRESCENTA.
func (r *Repository) CriarAmbienteSeAusente(ctx context.Context, prop, unidade uuid.UUID, a ambienteGravado) error {
	_, err := r.exec(ctx).Exec(ctx, `
		INSERT INTO unit_rooms (property_id, unit_id, name, kind, sort_order, active)
		VALUES ($1, $2, $3, $4, $5, true)
		ON CONFLICT ON CONSTRAINT `+nomeUnicoDoAmbiente+` DO NOTHING`, prop, unidade, a.Nome, a.Tipo, a.Ordem)
	return db.MapError(err)
}

// IDsDosAmbientes devolve nome → id dos cômodos da unidade.
func (r *Repository) IDsDosAmbientes(ctx context.Context, prop, unidade uuid.UUID) (map[string]uuid.UUID, error) {
	linhas, err := r.exec(ctx).Query(ctx,
		`SELECT name, id FROM unit_rooms WHERE property_id = $1 AND unit_id = $2`, prop, unidade)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := map[string]uuid.UUID{}
	for linhas.Next() {
		var (
			nome string
			id   uuid.UUID
		)
		if err := linhas.Scan(&nome, &id); err != nil {
			return nil, db.MapError(err)
		}
		out[nome] = id
	}
	return out, db.MapError(linhas.Err())
}

// CriarColocacaoSeAusente coloca o bem; colocação que já existe é MANTIDA com
// a quantidade que tem (a chave primária decide, `DO NOTHING`).
func (r *Repository) CriarColocacaoSeAusente(ctx context.Context, ambiente, bem uuid.UUID, qtd int) error {
	_, err := r.exec(ctx).Exec(ctx, `
		INSERT INTO room_inventory (room_id, item_id, expected_qty)
		VALUES ($1, $2, $3)
		ON CONFLICT (room_id, item_id) DO NOTHING`, ambiente, bem, qtd)
	return db.MapError(err)
}
