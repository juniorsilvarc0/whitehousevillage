package manutencao

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/maintenance"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// O repositório NUNCA abre transação: recebe o executor do contexto
// (`db.From`), e a transação nasce e morre no service.
//
// Duas regras atravessam o arquivo:
//
//   - a sobreposição de datas é decidida pela constraint `stay_no_overlap` no
//     próprio INSERT/UPDATE de `stay_blocks` (23P01 → 409 DATE_CONFLICT), e
//     nunca por um SELECT antes;
//   - "uma ordem não encerrada por avaria" é decidida pelo índice único
//     parcial, num `INSERT … ON CONFLICT DO NOTHING` — o segundo toque no
//     celular é a corrida que um SELECT antes perderia.

// Repository é o acesso a `maintenance_orders` e ao bloqueio da ordem.
type Repository struct {
	pool db.DBTX
}

// NewRepository monta o repositório sobre o pool.
func NewRepository(pool db.DBTX) *Repository { return &Repository{pool: pool} }

func (r *Repository) exec(ctx context.Context) db.DBTX { return db.From(ctx, r.pool) }

// Os nomes de constraint que decidem a resposta (migration 20261009100000).
const (
	idxAvariaAberta     = "maintenance_orders_avaria_aberta_idx"
	idxBloqueio         = "maintenance_orders_bloqueio_idx"
	fkComodo            = "maintenance_orders_room_id_fkey"
	fkAvaria            = "maintenance_orders_issue_id_fkey"
	fkBem               = "maintenance_orders_item_id_fkey"
	fkUnidade           = "maintenance_orders_unit_id_fkey"
	ckAvariaComComodo   = "maintenance_orders_avaria_exige_comodo_e_bem"
	ckCustoPositivo     = "maintenance_orders_custo_positivo"
	ckTituloNaoVazio    = "maintenance_orders_title_nao_vazio"
	ckTituloTamanho     = "maintenance_orders_title_tamanho"
	ckDescricaoTamanho  = "maintenance_orders_description_tamanho"
	ckPrioridadeValida  = "maintenance_orders_priority_valida"
	ckStatusValido      = "maintenance_orders_status_valido"
	ckFechamento        = "maintenance_orders_fechamento"
	ckFechador          = "maintenance_orders_fechador"
	ckInicio            = "maintenance_orders_inicio"
	ckCronologia        = "maintenance_orders_cronologia"
	sqlstateCheck       = "23514"
	sqlstateExclusao    = "23P01"
	origemDoBloqueio    = "maintenance"
	statusBloqueioVivo  = "confirmed"
	statusBloqueioSolto = "cancelled"
)

// ─────────────────────────── A linha ────────────────────────────────

// ordemGravada é a linha de `maintenance_orders` como está no banco. As tags
// `json` são os nomes da API: é este struct que vai para `audit_log`.
type ordemGravada struct {
	ID         uuid.UUID  `json:"id"`
	UnidadeID  uuid.UUID  `json:"unit_id"`
	ComodoID   *uuid.UUID `json:"room_id"`
	BemID      *uuid.UUID `json:"item_id"`
	AvariaID   *uuid.UUID `json:"issue_id"`
	Titulo     string     `json:"title"`
	Descricao  *string    `json:"description"`
	Prioridade string     `json:"priority"`
	Status     string     `json:"status"`
	BloqueioID *uuid.UUID `json:"stay_block_id"`
	CustoCents *int64     `json:"cost_cents"`
	IniciadaEm *time.Time `json:"started_at"`
	FechadaEm  *time.Time `json:"closed_at"`
	FechadaPor *uuid.UUID `json:"closed_by"`
	// UnidadeCodigo é do JOIN, para o `details.unit_code` do 409 — não vai
	// para a trilha.
	UnidadeCodigo string `json:"-"`
}

func (o ordemGravada) estado() maintenance.Status { return maintenance.Status(o.Status) }

const colunasDaLinha = `mo.id, mo.unit_id, mo.room_id, mo.item_id, mo.issue_id, mo.title, mo.description,
	mo.priority, mo.status, mo.stay_block_id, mo.cost_cents, mo.started_at, mo.closed_at, mo.closed_by, u.code`

func escanearLinha(l pgx.Row) (ordemGravada, error) {
	var o ordemGravada
	err := l.Scan(&o.ID, &o.UnidadeID, &o.ComodoID, &o.BemID, &o.AvariaID, &o.Titulo, &o.Descricao,
		&o.Prioridade, &o.Status, &o.BloqueioID, &o.CustoCents, &o.IniciadaEm, &o.FechadaEm, &o.FechadaPor,
		&o.UnidadeCodigo)
	return o, err
}

// TravarOrdem lê a ordem com FOR UPDATE — a base de toda escrita: a decisão
// do domínio é tomada sobre o estado travado, e duas ações simultâneas
// (concluir × cancelar) se enfileiram aqui em vez de se misturarem.
func (r *Repository) TravarOrdem(ctx context.Context, prop, id uuid.UUID) (ordemGravada, error) {
	o, err := escanearLinha(r.exec(ctx).QueryRow(ctx, `
		SELECT `+colunasDaLinha+`
		  FROM maintenance_orders mo JOIN units u ON u.id = mo.unit_id
		 WHERE mo.id = $1 AND mo.property_id = $2
		   FOR UPDATE OF mo`, id, prop))
	if errors.Is(err, pgx.ErrNoRows) {
		return ordemGravada{}, apperr.NotFound("Ordem de manutenção")
	}
	if err != nil {
		return ordemGravada{}, db.MapError(err)
	}
	return o, nil
}

// ─────────────────────────── Leitura para a API ─────────────────────

// colunasDaOrdem projeta o schema `OrdemDeManutencao`. "Hoje" sai do BANCO, no
// fuso da propriedade (`properties.timezone`), na mesma consulta — é ele que
// dá a fase do bloqueio.
const colunasDaOrdem = `
	mo.id, mo.unit_id, u.code, u.name, mo.room_id, r.name, mo.item_id, i.name,
	mo.issue_id, ii.kind, ii.qty, ii.note, ii.resolution, ii.reported_at,
	mo.title, mo.description, mo.priority, mo.status, mo.cost_cents,
	mo.stay_block_id, lower(sb.period)::text, upper(sb.period)::text, sb.status,
	mo.opened_at, mo.opened_by, ob.name, mo.started_at, mo.closed_at, mo.closed_by, cb.name,
	mo.updated_at, (now() AT TIME ZONE pr.timezone)::date::text`

const juncoesDaOrdem = `
	  FROM maintenance_orders mo
	  JOIN units u       ON u.id = mo.unit_id
	  JOIN properties pr ON pr.id = mo.property_id
	  LEFT JOIN unit_rooms r        ON r.id = mo.room_id
	  LEFT JOIN inventory_items i   ON i.id = mo.item_id
	  LEFT JOIN inventory_issues ii ON ii.id = mo.issue_id
	  LEFT JOIN stay_blocks sb      ON sb.id = mo.stay_block_id
	  LEFT JOIN users ob ON ob.id = mo.opened_by
	  LEFT JOIN users cb ON cb.id = mo.closed_by`

func escanearOrdem(l pgx.Row, extras ...any) (OrdemDeManutencao, error) {
	var (
		o                              OrdemDeManutencao
		tipo, nota, desfecho           *string
		qtd                            *int
		relatadaEm                     *time.Time
		blocoID                        *uuid.UUID
		blocoDe, blocoAte, blocoStatus *string
		hoje                           string
	)
	destinos := []any{&o.ID, &o.UnidadeID, &o.UnidadeCodigo, &o.UnidadeNome, &o.ComodoID, &o.ComodoNome,
		&o.BemID, &o.BemNome, &o.AvariaID, &tipo, &qtd, &nota, &desfecho, &relatadaEm,
		&o.Titulo, &o.Descricao, &o.Prioridade, &o.Status, &o.CustoCents,
		&blocoID, &blocoDe, &blocoAte, &blocoStatus,
		&o.AbertaEm, &o.AbertaPor, &o.AbertaPorNome, &o.IniciadaEm, &o.FechadaEm, &o.FechadaPor, &o.FechadaPorNome,
		&o.AtualizadaEm, &hoje}
	if err := l.Scan(append(destinos, extras...)...); err != nil {
		return OrdemDeManutencao{}, err
	}

	if o.AvariaID != nil && tipo != nil && qtd != nil && relatadaEm != nil {
		o.Avaria = &AvariaDaOrdem{ID: *o.AvariaID, Tipo: *tipo, Qtd: *qtd, Nota: nota, Desfecho: desfecho, RelatadaEm: *relatadaEm}
	}
	if blocoID != nil && blocoDe != nil && blocoAte != nil && blocoStatus != nil {
		b, err := bloqueioDaResposta(*blocoID, *blocoDe, *blocoAte, *blocoStatus, hoje)
		if err != nil {
			return OrdemDeManutencao{}, err
		}
		o.Bloqueio = &b
	}

	estado := maintenance.Status(o.Status)
	o.AcoesPermitidas = nomesDasAcoes(maintenance.AllowedActions(estado))
	o.Editavel = string(maintenance.EditableIn(estado))
	return o, nil
}

// bloqueioDaResposta monta o `block` com a fase de HOJE (maintenance.PhaseOf):
// derivada, nunca gravada — gravada, ficaria errada à meia-noite.
func bloqueioDaResposta(id uuid.UUID, de, ate, status, hoje string) (BloqueioDaOrdem, error) {
	p, err := periodoDoBanco(de, ate)
	if err != nil {
		return BloqueioDaOrdem{}, err
	}
	d, err := calendar.Parse(hoje)
	if err != nil {
		return BloqueioDaOrdem{}, fmt.Errorf("manutencao: hoje ilegível %q: %w", hoje, err)
	}
	fase := maintenance.PhaseOf(maintenance.Block{Period: p, Active: status == statusBloqueioVivo}, d)
	return BloqueioDaOrdem{ID: id, De: de, Ate: ate, Noites: p.Nights(), Status: status, Fase: string(fase)}, nil
}

func nomesDasAcoes(as []maintenance.Action) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = string(a)
	}
	return out
}

// BuscarOrdem lê a ordem desta casa, pronta para a resposta.
func (r *Repository) BuscarOrdem(ctx context.Context, prop, id uuid.UUID) (OrdemDeManutencao, error) {
	o, err := escanearOrdem(r.exec(ctx).QueryRow(ctx,
		`SELECT `+colunasDaOrdem+juncoesDaOrdem+` WHERE mo.id = $1 AND mo.property_id = $2`, id, prop))
	if errors.Is(err, pgx.ErrNoRows) {
		return OrdemDeManutencao{}, apperr.NotFound("Ordem de manutenção")
	}
	if err != nil {
		return OrdemDeManutencao{}, db.MapError(err)
	}
	return o, nil
}

// ─────────────────────────── Lista ──────────────────────────────────

// condicoes acumula predicados com argumentos numerados; `$1` é sempre a
// propriedade do ator.
type condicoes struct {
	partes []string
	args   []any
}

func (c *condicoes) add(fragmento string, valor any) {
	c.args = append(c.args, valor)
	posicoes := make([]any, strings.Count(fragmento, "%d"))
	for i := range posicoes {
		posicoes[i] = len(c.args)
	}
	c.partes = append(c.partes, fmt.Sprintf(fragmento, posicoes...))
}

func (c *condicoes) prox(valor any) int {
	c.args = append(c.args, valor)
	return len(c.args)
}

// ListarOrdens é o `GET /maintenance-orders`.
//
// A ordem padrão (`urgencia`) põe as abertas primeiro; entre elas, a escada de
// `maintenance.ByUrgency()` chega como PARÂMETRO (`array_position`), e não
// como um CASE com a lista copiada; na mesma prioridade, a mais antiga. As
// encerradas vêm depois, da mais recente. Empate final por `id`, para a
// paginação não repetir nem perder linha.
func (r *Repository) ListarOrdens(ctx context.Context, prop uuid.UUID, f FiltroDeOrdens) ([]OrdemDeManutencao, int64, error) {
	c := &condicoes{args: []any{prop}}
	// Os encerrados entram como parâmetro (`maintenance.ClosedStatuses()`), e
	// só quando alguém os usa: parâmetro que a consulta não cita não tem tipo
	// que o Postgres consiga inferir.
	encerrados := 0
	paramEncerrados := func() int {
		if encerrados == 0 {
			encerrados = c.prox(textosDosEstados(maintenance.ClosedStatuses()))
		}
		return encerrados
	}

	if f.Status != "" {
		c.add("mo.status = $%d", f.Status)
	}
	if f.Aberta != nil {
		fechada := fmt.Sprintf("mo.status = ANY($%d::text[])", paramEncerrados())
		if *f.Aberta {
			fechada = "NOT (" + fechada + ")"
		}
		c.partes = append(c.partes, fechada)
	}
	if f.UnidadeID != nil {
		c.add("mo.unit_id = $%d", *f.UnidadeID)
	}
	if f.ComodoID != nil {
		c.add("mo.room_id = $%d", *f.ComodoID)
	}
	if f.BemID != nil {
		c.add("mo.item_id = $%d", *f.BemID)
	}
	if f.AvariaID != nil {
		c.add("mo.issue_id = $%d", *f.AvariaID)
	}
	if f.Prioridade != "" {
		c.add("mo.priority = $%d", f.Prioridade)
	}
	if f.Busca != "" {
		c.add(`(mo.title ILIKE '%%' || $%d || '%%' OR mo.description ILIKE '%%' || $%d || '%%')`, escaparLike(f.Busca))
	}
	corpo := juncoesDaOrdem + ` WHERE mo.property_id = $1`
	if len(c.partes) > 0 {
		corpo += " AND " + strings.Join(c.partes, " AND ")
	}
	// Daqui em diante os parâmetros são só da ordenação e da página; a
	// contagem à parte usa os anteriores.
	argsDoFiltro := len(c.args)

	var ordem string
	switch f.Ordem {
	case "opened_at":
		ordem = "mo.opened_at ASC, mo.id ASC"
	case "-opened_at":
		ordem = "mo.opened_at DESC, mo.id DESC"
	case "-closed_at":
		ordem = "mo.closed_at DESC NULLS LAST, mo.id DESC"
	default:
		fechada := fmt.Sprintf("mo.status = ANY($%d::text[])", paramEncerrados())
		escada := c.prox(textosDasPrioridades(maintenance.ByUrgency()))
		ordem = fmt.Sprintf(`%[1]s ASC,
			CASE WHEN %[1]s THEN NULL ELSE array_position($%[2]d::text[], mo.priority) END ASC,
			CASE WHEN %[1]s THEN NULL ELSE mo.opened_at END ASC,
			CASE WHEN %[1]s THEN mo.closed_at END DESC,
			mo.id ASC`, fechada, escada)
	}

	limite := c.prox(f.PorPagina)
	desloc := c.prox(httpx.Offset(f.Pagina, f.PorPagina))
	q := `SELECT ` + colunasDaOrdem + `, count(*) OVER() ` + corpo +
		` ORDER BY ` + ordem + fmt.Sprintf(` LIMIT $%d OFFSET $%d`, limite, desloc)

	linhas, err := r.exec(ctx).Query(ctx, q, c.args...)
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	out := []OrdemDeManutencao{}
	var total int64
	for linhas.Next() {
		o, err := escanearOrdem(linhas, &total)
		if err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, o)
	}
	if err := linhas.Err(); err != nil {
		return nil, 0, db.MapError(err)
	}
	if len(out) == 0 && f.Pagina > 1 {
		// Página além do fim: o total vem de uma contagem à parte, senão a
		// coleção pareceria vazia.
		if err := r.exec(ctx).QueryRow(ctx, `SELECT count(*) `+corpo, c.args[:argsDoFiltro]...).Scan(&total); err != nil {
			return nil, 0, db.MapError(err)
		}
	}
	return out, total, nil
}

func textosDosEstados(ss []maintenance.Status) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = string(s)
	}
	return out
}

func textosDasPrioridades(ps []maintenance.Priority) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = string(p)
	}
	return out
}

// escaparLike neutraliza `%`, `_` e `\` da busca: "100%" procura o texto, não
// "100 seguido de qualquer coisa".
func escaparLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ─────────────────────────── Vínculos ───────────────────────────────

// unidadeDaOrdem é o que a ordem precisa saber da unidade.
type unidadeDaOrdem struct {
	Codigo string
	Ativa  bool
}

// Unidade lê a unidade desta casa. ok=false se não há.
func (r *Repository) Unidade(ctx context.Context, prop, id uuid.UUID) (unidadeDaOrdem, bool, error) {
	var u unidadeDaOrdem
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

// UnidadeDoComodo devolve a unidade do cômodo desta casa. ok=false se não há.
func (r *Repository) UnidadeDoComodo(ctx context.Context, prop, id uuid.UUID) (uuid.UUID, bool, error) {
	var unidade uuid.UUID
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT unit_id FROM unit_rooms WHERE id = $1 AND property_id = $2`, id, prop).Scan(&unidade)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, db.MapError(err)
	}
	return unidade, true, nil
}

// BemExiste diz se o bem é do catálogo desta casa.
func (r *Repository) BemExiste(ctx context.Context, prop, id uuid.UUID) (bool, error) {
	var existe bool
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM inventory_items WHERE id = $1 AND property_id = $2)`, id, prop).Scan(&existe)
	return existe, db.MapError(err)
}

// avariaDeOrigem é o que a ordem herda da avaria.
type avariaDeOrigem struct {
	ComodoID  uuid.UUID
	BemID     uuid.UUID
	UnidadeID uuid.UUID
}

// Avaria lê cômodo, bem e unidade da avaria desta casa. ok=false se não há.
func (r *Repository) Avaria(ctx context.Context, prop, id uuid.UUID) (avariaDeOrigem, bool, error) {
	var a avariaDeOrigem
	err := r.exec(ctx).QueryRow(ctx, `
		SELECT ii.room_id, ii.item_id, r.unit_id
		  FROM inventory_issues ii JOIN unit_rooms r ON r.id = ii.room_id
		 WHERE ii.id = $1 AND ii.property_id = $2`, id, prop).Scan(&a.ComodoID, &a.BemID, &a.UnidadeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, false, nil
	}
	if err != nil {
		return a, false, db.MapError(err)
	}
	return a, true, nil
}

// Hoje é o dia D no fuso DA PROPRIEDADE (`properties.timezone`), lido na
// transação de quem chama — a mesma fonte de "hoje" de reservas e bens.
func (r *Repository) Hoje(ctx context.Context, prop uuid.UUID) (calendar.Date, error) {
	var hoje string
	if err := r.exec(ctx).QueryRow(ctx,
		`SELECT (now() AT TIME ZONE timezone)::date::text FROM properties WHERE id = $1`, prop).Scan(&hoje); err != nil {
		return calendar.Date{}, db.MapError(err)
	}
	return calendar.Parse(hoje)
}

// ─────────────────────────── Escritas da ordem ──────────────────────

// novaOrdem é o que o POST grava.
type novaOrdem struct {
	UnidadeID  uuid.UUID
	ComodoID   *uuid.UUID
	BemID      *uuid.UUID
	AvariaID   *uuid.UUID
	Titulo     string
	Descricao  *string
	Prioridade string
	CustoCents *int64
	AbertaPor  *uuid.UUID
}

// CriarOrdem insere a ordem. `criada=false` quando a avaria JÁ tem ordem não
// encerrada: quem decide é o índice parcial, no ON CONFLICT — sem SELECT
// antes. `opened_at` e `updated_at` são o `now()` do banco.
func (r *Repository) CriarOrdem(ctx context.Context, prop uuid.UUID, o novaOrdem) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, `
		INSERT INTO maintenance_orders (property_id, unit_id, room_id, item_id, issue_id, title, description,
		                                priority, cost_cents, opened_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (issue_id) WHERE issue_id IS NOT NULL AND status IN ('aberta','em_andamento') DO NOTHING
		RETURNING id`,
		prop, o.UnidadeID, o.ComodoID, o.BemID, o.AvariaID, o.Titulo, o.Descricao, o.Prioridade, o.CustoCents,
		o.AbertaPor).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, traduzirOrdem(err)
	}
	return id, true, nil
}

// OrdemAbertaDaAvaria é a ordem não encerrada da avaria (no máximo uma) — o
// `details.maintenance_order_id` do 409. Lida DEPOIS de o índice decidir.
func (r *Repository) OrdemAbertaDaAvaria(ctx context.Context, prop, avaria uuid.UUID) (*uuid.UUID, error) {
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, `
		SELECT id FROM maintenance_orders
		 WHERE issue_id = $1 AND property_id = $2 AND status IN ('aberta','em_andamento')`, avaria, prop).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, db.MapError(err)
	}
	return &id, nil
}

// GravarCadastro é o UPDATE do PUT e do PATCH: só os campos de cadastro.
func (r *Repository) GravarCadastro(ctx context.Context, o ordemGravada) error {
	_, err := r.exec(ctx).Exec(ctx, `
		UPDATE maintenance_orders
		   SET room_id = $2, item_id = $3, title = $4, description = $5, priority = $6, cost_cents = $7,
		       updated_at = now()
		 WHERE id = $1`,
		o.ID, o.ComodoID, o.BemID, o.Titulo, o.Descricao, o.Prioridade, o.CustoCents)
	return traduzirOrdem(err)
}

// Iniciar grava `em_andamento` com `started_at` do relógio do banco.
func (r *Repository) Iniciar(ctx context.Context, id uuid.UUID) error {
	_, err := r.exec(ctx).Exec(ctx, `
		UPDATE maintenance_orders SET status = $2, started_at = now(), updated_at = now() WHERE id = $1`,
		id, string(maintenance.InProgress))
	return traduzirOrdem(err)
}

// Encerrar grava `concluida` ou `cancelada`, com `closed_at` e `closed_by`, e
// o custo quando vier (nil não mexe).
//
// `closed_at` é `GREATEST(now(), started_at)`, e não `now()` puro: `now()` é o
// INÍCIO da transação, e esta pode ter começado antes de um `/start` que ela
// esperou na trava da ordem — o `started_at` gravado por ele seria posterior
// ao `now()` daqui, e `maintenance_orders_cronologia` recusaria. Os dois
// instantes continuam saindo do mesmo relógio (o do banco).
func (r *Repository) Encerrar(ctx context.Context, id uuid.UUID, status maintenance.Status, por *uuid.UUID, custo *int64) error {
	_, err := r.exec(ctx).Exec(ctx, `
		UPDATE maintenance_orders
		   SET status = $2, closed_at = GREATEST(now(), started_at), closed_by = $3,
		       cost_cents = COALESCE($4, cost_cents), updated_at = GREATEST(now(), started_at)
		 WHERE id = $1`, id, string(status), por, custo)
	return traduzirOrdem(err)
}

// ApontarBloqueio liga a ordem à linha de `stay_blocks` que ela criou.
func (r *Repository) ApontarBloqueio(ctx context.Context, ordem, bloco uuid.UUID) error {
	_, err := r.exec(ctx).Exec(ctx,
		`UPDATE maintenance_orders SET stay_block_id = $2, updated_at = now() WHERE id = $1`, ordem, bloco)
	return traduzirOrdem(err)
}

// TocarOrdem marca a ordem como alterada quando só o bloqueio dela mudou.
func (r *Repository) TocarOrdem(ctx context.Context, ordem uuid.UUID) error {
	_, err := r.exec(ctx).Exec(ctx, `UPDATE maintenance_orders SET updated_at = now() WHERE id = $1`, ordem)
	return traduzirOrdem(err)
}

// traduzirOrdem converte o erro de escrita em `maintenance_orders` pelo NOME
// da constraint. As de forma (título, custo, prioridade) a API já validou
// antes; chegam aqui só pela corrida ou por defeito, e saem como o 422 do
// campo. As de ESTADO (fechamento, início, cronologia, fechador, status) só
// caem por defeito nosso — o domínio decide a transição antes —, e saem 500.
func traduzirOrdem(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case db.IsUniqueViolation(err, idxAvariaAberta):
		return apperr.MaintenanceOrderOpen.WithCause(err)
	case db.IsUniqueViolation(err, idxBloqueio):
		return apperr.Internal.WithCause(err)
	case db.IsForeignKeyViolation(err, fkComodo):
		return campoInvalido("room_id", "o cômodo não é desta unidade.", err)
	case db.IsForeignKeyViolation(err, fkAvaria):
		return campoInvalido("issue_id", "a avaria não é deste cômodo e bem.", err)
	case db.IsForeignKeyViolation(err, fkBem):
		return campoInvalido("item_id", "bem não encontrado nesta propriedade.", err)
	case db.IsForeignKeyViolation(err, fkUnidade):
		return campoInvalido("unit_id", "unidade não encontrada nesta propriedade.", err)
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == sqlstateCheck {
		switch pg.ConstraintName {
		case ckAvariaComComodo:
			return campoInvalido("issue_id", "a ordem de uma avaria cita o cômodo e o bem dela.", err)
		case ckCustoPositivo:
			return campoInvalido("cost_cents", "deve ser positivo, em centavos.", err)
		case ckTituloNaoVazio, ckTituloTamanho:
			return campoInvalido("title", fmt.Sprintf("obrigatório, até %d caracteres.", tamanhoDoTitulo), err)
		case ckDescricaoTamanho:
			return campoInvalido("description", fmt.Sprintf("no máximo %d caracteres.", tamanhoDaDescricao), err)
		case ckPrioridadeValida:
			return campoInvalido("priority", "use baixa, normal, alta ou urgente.", err)
		case ckStatusValido, ckFechamento, ckFechador, ckInicio, ckCronologia:
			return apperr.Internal.WithCause(err)
		}
	}
	return db.MapError(err)
}

func campoInvalido(campo, msg string, causa error) error {
	return apperr.Validation(map[string]string{campo: msg}).WithCause(causa)
}

// ─────────────────────────── O bloqueio ─────────────────────────────

// bloqueioGravado é a linha de `stay_blocks` da ordem. As tags `json` são o
// documento da trilha (`audit_log`), no formato de `POST /blocks`.
type bloqueioGravado struct {
	ID            uuid.UUID  `json:"id"`
	UnidadeID     uuid.UUID  `json:"unit_id"`
	UnidadeCodigo string     `json:"unit_code"`
	Origem        string     `json:"source"`
	Status        string     `json:"status"`
	De            string     `json:"from"`
	Ate           string     `json:"to"`
	Nota          *string    `json:"note"`
	OrdemID       *uuid.UUID `json:"maintenance_order_id,omitempty"`
}

// dominio é o bloqueio como o domínio o enxerga: período e se ainda ocupa.
func (b bloqueioGravado) dominio() (maintenance.Block, error) {
	p, err := periodoDoBanco(b.De, b.Ate)
	if err != nil {
		return maintenance.Block{}, err
	}
	return maintenance.Block{Period: p, Active: b.Status == statusBloqueioVivo}, nil
}

func (b bloqueioGravado) comPeriodo(p maintenance.Period) bloqueioGravado {
	b.De, b.Ate = p.From.String(), p.To.String()
	return b
}

func (b bloqueioGravado) comStatus(s string) bloqueioGravado {
	b.Status = s
	return b
}

func periodoDoBanco(de, ate string) (maintenance.Period, error) {
	from, err := calendar.Parse(de)
	if err != nil {
		return maintenance.Period{}, fmt.Errorf("manutencao: início do bloqueio ilegível %q: %w", de, err)
	}
	to, err := calendar.Parse(ate)
	if err != nil {
		return maintenance.Period{}, fmt.Errorf("manutencao: fim do bloqueio ilegível %q: %w", ate, err)
	}
	return maintenance.Period{From: from, To: to}, nil
}

// classeDaTravaDoBloqueio é a classe do advisory lock (forma de dois inteiros)
// que serializa, POR UNIDADE, quem cria ou estende o bloqueio de uma ordem.
const classeDaTravaDoBloqueio int32 = 2026_1009

// TravarCalendarioDaUnidade enfileira, por unidade, as ordens que vão escrever
// período em `stay_blocks`. NÃO é ela que decide a sobreposição — quem decide
// continua sendo a constraint `stay_no_overlap`, no INSERT/UPDATE. Ela existe
// pelo que a constraint faz sob disputa: dois INSERTs simultâneos de períodos
// que se cruzam enxergam a tupla um do outro em andamento e esperam UM PELO
// OUTRO; o `lock_timeout` (400 ms, `db.New`) derruba os dois, e com dez
// pedidos no mesmo instante a repetição do TxManager recolocava todos na
// mesma espera cruzada. Medido em 09/10/2026, sem esta trava: 2 de 6
// execuções de `TestDisputaDeBloqueioSobrepostoNaMesmaUnidade` terminaram com
// ZERO ordens criadas e dez 409 vindos de `55P03` (tempo de espera, não data
// ocupada). Com ela, 10 de 10 execuções frias e 20 repetições seguidas
// verdes: a primeira ordem insere e comita, e as outras recebem o 23P01 de
// verdade logo em seguida.
//
// O bloqueio da ordem é mais longo que o de `POST /blocks` (a ordem, a trilha e
// a leitura da resposta vão na mesma transação), e é essa janela maior que
// fazia a espera cruzada virar regra em vez de exceção.
func (r *Repository) TravarCalendarioDaUnidade(ctx context.Context, unidade uuid.UUID) error {
	if !db.EmTransacao(ctx) {
		return apperr.Internal.WithCause(errors.New("manutencao: trava do calendário fora de transação"))
	}
	_, err := r.exec(ctx).Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2::text))`,
		classeDaTravaDoBloqueio, unidade.String())
	return db.MapError(err)
}

// TravarBloqueio lê a linha de `stay_blocks` com FOR UPDATE. A ordem já está
// travada por quem chama: a ordem de aquisição é sempre ordem → bloqueio.
func (r *Repository) TravarBloqueio(ctx context.Context, id, ordem uuid.UUID) (bloqueioGravado, error) {
	b := bloqueioGravado{OrdemID: &ordem}
	err := r.exec(ctx).QueryRow(ctx, `
		SELECT sb.id, sb.unit_id, u.code, sb.source, sb.status,
		       lower(sb.period)::text, upper(sb.period)::text, sb.note
		  FROM stay_blocks sb JOIN units u ON u.id = sb.unit_id
		 WHERE sb.id = $1
		   FOR UPDATE OF sb`, id).
		Scan(&b.ID, &b.UnidadeID, &b.UnidadeCodigo, &b.Origem, &b.Status, &b.De, &b.Ate, &b.Nota)
	if err != nil {
		return bloqueioGravado{}, db.MapError(err)
	}
	return b, nil
}

// CriarBloqueio insere a linha `source = 'maintenance'`, `confirmed`, na
// unidade da ordem; `owner_id` e `created_by` = quem pediu, `note` = o título.
// A sobreposição é decidida AQUI, pela constraint (23P01 → 409 DATE_CONFLICT
// com o período PEDIDO), e nunca é repetida.
func (r *Repository) CriarBloqueio(ctx context.Context, prop, unidade uuid.UUID, codigo string,
	p maintenance.Period, nota string, autor *uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, `
		INSERT INTO stay_blocks (property_id, unit_id, source, status, period, note, created_by, owner_id)
		VALUES ($1, $2, $3, $4, daterange($5::date, $6::date, '[)'), $7, $8, $8)
		RETURNING id`,
		prop, unidade, origemDoBloqueio, statusBloqueioVivo, p.From.String(), p.To.String(), nota, autor).Scan(&id)
	if err != nil {
		return uuid.Nil, traduzirBloqueio(err, codigo, p)
	}
	return id, nil
}

// MudarPeriodo troca o período da linha. Estender sobre outra ocupação cai na
// constraint no próprio UPDATE (23P01 → 409); encurtar nunca colide.
func (r *Repository) MudarPeriodo(ctx context.Context, id uuid.UUID, codigo string, p maintenance.Period) error {
	_, err := r.exec(ctx).Exec(ctx,
		`UPDATE stay_blocks SET period = daterange($2::date, $3::date, '[)') WHERE id = $1`,
		id, p.From.String(), p.To.String())
	return traduzirBloqueio(err, codigo, p)
}

// SoltarBloqueio libera a linha: `cancelled`, NUNCA DELETE — a ordem continua
// apontando para ela (FK RESTRICT), e a trilha de "esteve bloqueada" fica.
func (r *Repository) SoltarBloqueio(ctx context.Context, id uuid.UUID) error {
	_, err := r.exec(ctx).Exec(ctx,
		`UPDATE stay_blocks SET status = $2, expires_at = NULL WHERE id = $1`, id, statusBloqueioSolto)
	return db.MapError(err)
}

// traduzirBloqueio leva o 23P01 ao DATE_CONFLICT do contrato, com
// `details.unit_code` e `details.period` — o período PEDIDO, como em
// `reservas`.
func traduzirBloqueio(err error, codigo string, p maintenance.Period) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == sqlstateExclusao {
		return apperr.DateConflict.
			WithMessage("A unidade já está ocupada nesse período: o bloqueio não foi feito.").
			WithDetails(map[string]any{
				"unit_code":  codigo,
				"period":     fmt.Sprintf("[%s, %s)", p.From, p.To),
				"constraint": pg.ConstraintName,
			}).WithCause(err)
	}
	return db.MapError(err)
}

// ─────────────────────────── A avaria ───────────────────────────────

// avariaAuditada é o recorte da trilha do desfecho.
type avariaAuditada struct {
	Desfecho     *string    `json:"resolution"`
	ResolvidaEm  *time.Time `json:"resolved_at"`
	ResolvidaPor *uuid.UUID `json:"resolved_by"`
	OrdemID      *uuid.UUID `json:"maintenance_order_id,omitempty"`
}

// ConsertarAvaria encerra a avaria AINDA ABERTA com o desfecho do domínio
// (`consertado`). Avaria que alguém já resolveu não é tocada — o `WHERE
// resolution IS NULL` é a regra "o desfecho escolhido por uma pessoa vence o
// inferido pela ordem", aplicada na mesma instrução, sem SELECT antes.
// `mudou=false` quando não havia o que encerrar.
func (r *Repository) ConsertarAvaria(ctx context.Context, avaria uuid.UUID, desfecho string, por *uuid.UUID) (avariaAuditada, bool, error) {
	var depois avariaAuditada
	err := r.exec(ctx).QueryRow(ctx, `
		UPDATE inventory_issues
		   SET resolution = $2, resolved_at = now(), resolved_by = $3, updated_at = now()
		 WHERE id = $1 AND resolution IS NULL
		RETURNING resolution, resolved_at, resolved_by`, avaria, desfecho, por).
		Scan(&depois.Desfecho, &depois.ResolvidaEm, &depois.ResolvidaPor)
	if errors.Is(err, pgx.ErrNoRows) {
		return avariaAuditada{}, false, nil
	}
	if err != nil {
		return avariaAuditada{}, false, db.MapError(err)
	}
	return depois, true, nil
}
