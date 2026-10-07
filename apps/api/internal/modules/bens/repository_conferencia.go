package bens

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// progressoDe devolve o agregado do rodapé sobre as linhas de `alvo` (uma
// expressão SQL com o id da conferência). É a mesma aritmética de
// Progresso.somar, escrita em SQL para a listagem e o PATCH da linha não
// precisarem trazer as linhas para o Go.
//
// `counted_qty <> expected_qty` é NULL na linha pendente, e o FILTER a deixa
// de fora — pendente não é divergente, e é diferente de "contei zero".
func progressoDe(alvo string) string {
	return strings.ReplaceAll(`
		SELECT count(*)::int AS linhas,
		       count(l.counted_qty)::int AS contadas,
		       (count(*) FILTER (WHERE l.counted_qty IS NULL))::int AS pendentes,
		       (count(*) FILTER (WHERE l.counted_qty <> l.expected_qty))::int AS divergentes,
		       COALESCE(sum(l.expected_qty - l.counted_qty) FILTER (WHERE l.counted_qty < l.expected_qty), 0)::bigint AS faltas,
		       COALESCE(sum(l.counted_qty - l.expected_qty) FILTER (WHERE l.counted_qty > l.expected_qty), 0)::bigint AS sobras
		  FROM inventory_count_lines l
		 WHERE l.count_id = ALVO`, "ALVO", alvo)
}

const colunasDaConferencia = `
	c.id, c.unit_id, u.code, u.name, c.status, c.note, c.opened_by, ob.name, c.opened_at,
	c.closed_by, cb.name, c.closed_at, c.updated_at,
	p.linhas, p.contadas, p.pendentes, p.divergentes, p.faltas, p.sobras`

// O nome de quem abriu e de quem fechou é o NOME do usuário — e só ele. Nada
// de e-mail nem telefone: quem opera não é contato.
var juncoesDaConferencia = `
	  FROM inventory_counts c
	  JOIN units u ON u.id = c.unit_id
	  JOIN properties pr ON pr.id = c.property_id
	  LEFT JOIN users ob ON ob.id = c.opened_by
	  LEFT JOIN users cb ON cb.id = c.closed_by
	  CROSS JOIN LATERAL (` + progressoDe("c.id") + `) p`

func escanearConferencia(linha pgx.Row, extras ...any) (Conferencia, error) {
	var c Conferencia
	destinos := []any{&c.ID, &c.UnidadeID, &c.UnidadeCodigo, &c.UnidadeNome, &c.Status, &c.Nota,
		&c.AbertaPor, &c.AbertaPorNome, &c.AbertaEm, &c.EncerradaPor, &c.EncerradaPorNome, &c.EncerradaEm,
		&c.AtualizadaEm, &c.Progresso.Linhas, &c.Progresso.Contadas, &c.Progresso.Pendentes,
		&c.Progresso.Divergentes, &c.Progresso.Faltas, &c.Progresso.Sobras}
	err := linha.Scan(append(destinos, extras...)...)
	return c, err
}

// OrdensDeConferencia é a whitelist do `?sort=`. Desempate por id.
var OrdensDeConferencia = map[string]string{
	"opened_at":  "c.opened_at ASC, c.id ASC",
	"-opened_at": "c.opened_at DESC, c.id DESC",
}

// OrdemPadraoDeConferencia é da mais recente para a mais antiga.
const OrdemPadraoDeConferencia = "-opened_at"

// Filtro de data no FUSO DA PROPRIEDADE, que é dado (`properties.timezone`) e
// não constante em Go — a mesma fonte do "hoje" de reservas e tarifário. O dia
// local vira instante com `AT TIME ZONE`, e a comparação fica em `opened_at`
// cru, que é o que o índice entrega. `to` inclui o dia inteiro (meia-aberta no
// dia seguinte).
//
// Quem usa precisa ter `properties` com o apelido `pr` nas junções.
const (
	desdeODiaLocal = "COLUNA >= ($%d::date::timestamp AT TIME ZONE pr.timezone)"
	ateODiaLocal   = "COLUNA < (($%d::date + 1)::timestamp AT TIME ZONE pr.timezone)"
)

func filtrarPeriodo(c *condicoes, coluna, de, ate string) {
	if de != "" {
		c.add(strings.Replace(desdeODiaLocal, "COLUNA", coluna, 1), de)
	}
	if ate != "" {
		c.add(strings.Replace(ateODiaLocal, "COLUNA", coluna, 1), ate)
	}
}

// ListarConferencias é o `GET /inventory/counts`.
func (r *Repository) ListarConferencias(ctx context.Context, prop uuid.UUID, f FiltroDeConferencias) ([]Conferencia, int64, error) {
	c := novasCondicoes(prop)
	if f.UnidadeID != nil {
		c.add("c.unit_id = $%d", *f.UnidadeID)
	}
	if f.Status != "" {
		c.add("c.status = $%d", f.Status)
	}
	filtrarPeriodo(c, "c.opened_at", f.De, f.Ate)
	corpo := juncoesDaConferencia + ` WHERE c.property_id = $1` + c.where()
	return listar(ctx, r.exec(ctx), colunasDaConferencia, corpo, ordemOu(OrdensDeConferencia, f.Ordem, OrdemPadraoDeConferencia), c,
		f.Pagina, f.PorPagina, func(l pgx.Rows, total *int64) (Conferencia, error) {
			return escanearConferencia(l, total)
		})
}

// BuscarConferencia lê o cabeçalho com o progresso.
func (r *Repository) BuscarConferencia(ctx context.Context, prop, id uuid.UUID) (Conferencia, error) {
	c, err := escanearConferencia(r.exec(ctx).QueryRow(ctx,
		`SELECT `+colunasDaConferencia+juncoesDaConferencia+` WHERE c.id = $1 AND c.property_id = $2`, id, prop))
	if errors.Is(err, pgx.ErrNoRows) {
		return Conferencia{}, apperr.NotFound("Conferência")
	}
	if err != nil {
		return Conferencia{}, db.MapError(err)
	}
	return c, nil
}

// conferenciaDaUnidade devolve a conferência da unidade no estado pedido — a
// aberta (no máximo uma, pelo índice parcial) ou a última fechada. nil se não há.
func (r *Repository) conferenciaDaUnidade(ctx context.Context, prop, unidade uuid.UUID, status, ordem string) (*Conferencia, error) {
	c, err := escanearConferencia(r.exec(ctx).QueryRow(ctx,
		`SELECT `+colunasDaConferencia+juncoesDaConferencia+
			` WHERE c.unit_id = $1 AND c.property_id = $2 AND c.status = $3 ORDER BY `+ordem+` LIMIT 1`,
		unidade, prop, status))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, db.MapError(err)
	}
	return &c, nil
}

// ConferenciaAbertaDaUnidade é a conferência em curso, se houver.
func (r *Repository) ConferenciaAbertaDaUnidade(ctx context.Context, prop, unidade uuid.UUID) (*Conferencia, error) {
	return r.conferenciaDaUnidade(ctx, prop, unidade, StatusAberta, "c.opened_at DESC, c.id DESC")
}

// UltimaFechadaDaUnidade é a de "conferido em". Cancelada não conta: ninguém
// terminou de conferir nada nela.
func (r *Repository) UltimaFechadaDaUnidade(ctx context.Context, prop, unidade uuid.UUID) (*Conferencia, error) {
	return r.conferenciaDaUnidade(ctx, prop, unidade, StatusFechada, "c.closed_at DESC, c.id DESC")
}

// ProgressoDaConferencia é o rodapé, sem trazer as linhas.
func (r *Repository) ProgressoDaConferencia(ctx context.Context, id uuid.UUID) (Progresso, error) {
	var p Progresso
	err := r.exec(ctx).QueryRow(ctx, progressoDe("$1"), id).
		Scan(&p.Linhas, &p.Contadas, &p.Pendentes, &p.Divergentes, &p.Faltas, &p.Sobras)
	return p, db.MapError(err)
}

// ─────────────────────────── Linhas ─────────────────────────────────────────

const colunasDaLinha = `
	l.id, l.count_id, l.room_id, r.name, r.kind, l.item_id, i.name, i.description, i.category,
	i.unit_measure, ` + colunasDaCapa + `, l.expected_qty, l.counted_qty, l.note, l.replacement_cost_cents,
	l.counted_by, ub.name, l.counted_at`

const juncoesDaLinha = `
	  FROM inventory_count_lines l
	  JOIN unit_rooms r ON r.id = l.room_id
	  JOIN inventory_items i ON i.id = l.item_id
	  LEFT JOIN users ub ON ub.id = l.counted_by` + lateralDaCapa

// Ordem de caminhada dentro de UMA unidade: cômodo pela ordem da casa e, nele,
// bem por nome — a ordem da folha impressa e do celular.
const ordemDasLinhas = "r.sort_order ASC, r.name ASC, r.id ASC, i.name ASC, i.id ASC"

func escanearLinha(linha pgx.Row) (LinhaDeConferencia, error) {
	var (
		l   LinhaDeConferencia
		cap capaLida
	)
	destinos := []any{&l.ID, &l.ConferenciaID, &l.AmbienteID, &l.AmbienteNome, &l.AmbienteTipo, &l.BemID,
		&l.BemNome, &l.BemDescricao, &l.BemCategoria, &l.BemMedida}
	destinos = append(destinos, cap.destinos()...)
	destinos = append(destinos, &l.QtdEsperada, &l.QtdContada, &l.Nota, &l.CustoCongelado,
		&l.ContadaPor, &l.ContadaPorNome, &l.ContadaEm)
	if err := linha.Scan(destinos...); err != nil {
		return LinhaDeConferencia{}, err
	}
	l.Capa = cap.midia()
	l.Diferenca = diferenca(l.QtdEsperada, l.QtdContada)
	return l, nil
}

// LinhasDaConferencia traz todas as linhas, na ordem de caminhada.
func (r *Repository) LinhasDaConferencia(ctx context.Context, id uuid.UUID) ([]LinhaDeConferencia, error) {
	linhas, err := r.exec(ctx).Query(ctx,
		`SELECT `+colunasDaLinha+juncoesDaLinha+` WHERE l.count_id = $1 ORDER BY `+ordemDasLinhas, id)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []LinhaDeConferencia{}
	for linhas.Next() {
		l, err := escanearLinha(linhas)
		if err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, l)
	}
	return out, db.MapError(linhas.Err())
}

// BuscarLinha lê uma linha DESTA conferência; de outra é 404.
func (r *Repository) BuscarLinha(ctx context.Context, conferencia, id uuid.UUID) (LinhaDeConferencia, error) {
	l, err := escanearLinha(r.exec(ctx).QueryRow(ctx,
		`SELECT `+colunasDaLinha+juncoesDaLinha+` WHERE l.id = $1 AND l.count_id = $2`, id, conferencia))
	if errors.Is(err, pgx.ErrNoRows) {
		return LinhaDeConferencia{}, apperr.NotFound("Linha da conferência")
	}
	if err != nil {
		return LinhaDeConferencia{}, db.MapError(err)
	}
	return l, nil
}

// ─────────────────────────── Escrita ────────────────────────────────────────

// estadoDaConferencia é o cabeçalho como está no banco, travado.
type estadoDaConferencia struct {
	ID          uuid.UUID  `json:"id"`
	UnidadeID   uuid.UUID  `json:"unit_id"`
	Status      string     `json:"status"`
	Nota        *string    `json:"note"`
	EncerradaEm *time.Time `json:"closed_at"`
}

// TravarConferencia lê o cabeçalho com trava de linha.
//
// `exclusiva` (FOR UPDATE) é de quem MUDA o cabeçalho: fechar, cancelar,
// editar a nota. Sem ela, duas abas fechando a mesma conferência apurariam a
// divergência duas vezes e abririam as avarias em dobro.
//
// A compartilhada (FOR SHARE) é do gesto do celular: muitas linhas contadas ao
// mesmo tempo não se bloqueiam entre si, mas TODAS esperam um fechamento em
// curso — e, depois dele, releem o status e recebem COUNT_CLOSED. É o que
// impede a contagem de entrar numa conferência que acabou de ser encerrada.
func (r *Repository) TravarConferencia(ctx context.Context, prop, id uuid.UUID, exclusiva bool) (estadoDaConferencia, error) {
	trava := "FOR SHARE"
	if exclusiva {
		trava = "FOR UPDATE"
	}
	var e estadoDaConferencia
	err := r.exec(ctx).QueryRow(ctx, `
		SELECT id, unit_id, status, note, closed_at
		  FROM inventory_counts
		 WHERE id = $1 AND property_id = $2 `+trava, id, prop).
		Scan(&e.ID, &e.UnidadeID, &e.Status, &e.Nota, &e.EncerradaEm)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, apperr.NotFound("Conferência")
	}
	if err != nil {
		return e, db.MapError(err)
	}
	return e, nil
}

// conferenciaJaAberta é o que o 409 COUNT_ALREADY_OPEN leva em `details`.
type conferenciaJaAberta struct {
	ID       uuid.UUID `json:"count_id"`
	AbertaEm time.Time `json:"opened_at"`
}

// AbrirConferencia insere o cabeçalho. Quem DECIDE se a unidade já tem uma
// aberta é o índice único parcial `inventory_counts_aberta_idx`, como árbitro
// do `ON CONFLICT` — nunca um SELECT antes do INSERT, que perderia a corrida
// entre dois funcionários abrindo a contagem do AP-01 no mesmo plantão.
//
// `DO NOTHING` em vez de deixar o 23505 estourar: a transação continua viva, e
// a conferência que venceu pode ser lida AQUI para os `details` do 409 — que é
// o que leva o segundo toque à tela certa em vez de virar beco. Se a outra
// transação ainda não comitou, o INSERT espera por ela (é o índice único quem
// segura), e só decide depois.
func (r *Repository) AbrirConferencia(ctx context.Context, prop, unidade uuid.UUID, nota *string, por *uuid.UUID) (uuid.UUID, *conferenciaJaAberta, error) {
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, `
		INSERT INTO inventory_counts (property_id, unit_id, note, opened_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (unit_id) WHERE status = 'aberta' DO NOTHING
		RETURNING id`, prop, unidade, nota, por).Scan(&id)
	if err == nil {
		return id, nil, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil, db.MapError(err)
	}

	var ja conferenciaJaAberta
	err = r.exec(ctx).QueryRow(ctx, `
		SELECT id, opened_at FROM inventory_counts
		 WHERE unit_id = $1 AND status = 'aberta'`, unidade).Scan(&ja.ID, &ja.AbertaEm)
	if errors.Is(err, pgx.ErrNoRows) {
		// A que venceu já foi encerrada entre o conflito e esta leitura. O
		// conflito aconteceu e é ele que se responde; só faltam os detalhes.
		return uuid.Nil, &conferenciaJaAberta{}, nil
	}
	if err != nil {
		return uuid.Nil, nil, db.MapError(err)
	}
	return uuid.Nil, &ja, nil
}

// CongelarLinhas copia `room_inventory.expected_qty` para as linhas da
// conferência — CÓPIA, não JOIN (regra 7 do CLAUDE.md). Mudar o padrão da casa
// em maio não pode reescrever o que a contagem de março esperava.
//
// Entram só cômodos ATIVOS e bens ATIVOS da unidade, e só da propriedade do
// ator nos dois lados: `room_inventory` não tem `property_id`, e é aqui que um
// item de outra casa colocado por engano deixaria de entrar na contagem.
//
// Roda na MESMA transação do INSERT do cabeçalho: zero linhas desfaz o
// cabeçalho junto (conferência de nada é 422).
func (r *Repository) CongelarLinhas(ctx context.Context, prop, conferencia, unidade uuid.UUID) (int64, error) {
	tag, err := r.exec(ctx).Exec(ctx, `
		INSERT INTO inventory_count_lines (count_id, room_id, item_id, expected_qty)
		SELECT $1, ri.room_id, ri.item_id, ri.expected_qty
		  FROM room_inventory ri
		  JOIN unit_rooms r ON r.id = ri.room_id
		  JOIN inventory_items i ON i.id = ri.item_id
		 WHERE r.unit_id = $2
		   AND r.property_id = $3 AND i.property_id = $3
		   AND r.active AND i.active
		 ORDER BY r.sort_order, r.name, r.id, i.name, i.id`, conferencia, unidade, prop)
	if err != nil {
		return 0, db.MapError(err)
	}
	return tag.RowsAffected(), nil
}

// GravarNotaDaConferencia é o PUT/PATCH do cabeçalho — só a observação.
func (r *Repository) GravarNotaDaConferencia(ctx context.Context, id uuid.UUID, nota *string) error {
	_, err := r.exec(ctx).Exec(ctx,
		`UPDATE inventory_counts SET note = $2, updated_at = now() WHERE id = $1`, id, nota)
	return db.MapError(err)
}

// EncerrarConferencia leva a conferência a `fechada` ou `cancelada`. O
// `closed_at` vai junto porque o CHECK `inventory_counts_fechamento` amarra os
// dois: encerrada sem instante é estado impossível.
func (r *Repository) EncerrarConferencia(ctx context.Context, id uuid.UUID, status string, nota *string, por *uuid.UUID) error {
	_, err := r.exec(ctx).Exec(ctx, `
		UPDATE inventory_counts
		   SET status = $2, note = $3, closed_at = now(), closed_by = $4, updated_at = now()
		 WHERE id = $1`, id, status, nota, por)
	return db.MapError(err)
}

// linhaGravada é a linha de `inventory_count_lines` como está no banco.
type linhaGravada struct {
	ID         uuid.UUID  `json:"id"`
	QtdContada *int       `json:"counted_qty"`
	Nota       *string    `json:"note"`
	ContadaPor *uuid.UUID `json:"counted_by"`
	ContadaEm  *time.Time `json:"counted_at"`
}

// TravarLinha lê a linha da conferência com FOR UPDATE; de outra conferência
// é 404.
func (r *Repository) TravarLinha(ctx context.Context, conferencia, id uuid.UUID) (linhaGravada, error) {
	var l linhaGravada
	err := r.exec(ctx).QueryRow(ctx, `
		SELECT id, counted_qty, note, counted_by, counted_at
		  FROM inventory_count_lines
		 WHERE id = $1 AND count_id = $2
		   FOR UPDATE`, id, conferencia).Scan(&l.ID, &l.QtdContada, &l.Nota, &l.ContadaPor, &l.ContadaEm)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, apperr.NotFound("Linha da conferência")
	}
	if err != nil {
		return l, db.MapError(err)
	}
	return l, nil
}

// GravarContagem grava a nota e, quando `mexer`, lança (ou desfaz) a
// contagem. `counted_at` e `counted_by` são do SERVIDOR: `now()` da transação
// e o ator, nunca o relógio do aparelho. Contada nula leva os dois a nulo — o
// CHECK `inventory_count_lines_contagem` não aceita um sem o outro. Sem
// `mexer` (corpo só com `note`), os três ficam como estão.
func (r *Repository) GravarContagem(ctx context.Context, id uuid.UUID, mexer bool, contada *int, nota *string, por *uuid.UUID) error {
	_, err := r.exec(ctx).Exec(ctx, `
		UPDATE inventory_count_lines
		   SET counted_qty = CASE WHEN $5 THEN $2::int ELSE counted_qty END,
		       counted_at  = CASE WHEN NOT $5 THEN counted_at
		                          WHEN $2::int IS NULL THEN NULL ELSE now() END,
		       counted_by  = CASE WHEN NOT $5 THEN counted_by
		                          WHEN $2::int IS NULL THEN NULL ELSE $4::uuid END,
		       note        = $3,
		       updated_at  = now()
		 WHERE id = $1`, id, contada, nota, por, mexer)
	return db.MapError(err)
}

// PendenciaDoAmbiente é uma entrada de `details.pending_by_room`.
type PendenciaDoAmbiente struct {
	AmbienteID   uuid.UUID `json:"room_id"`
	AmbienteNome string    `json:"room_name"`
	Pendentes    int       `json:"pending"`
}

// PendenciasPorAmbiente conta as linhas ainda não contadas, por cômodo, na
// ordem de caminhada. Sai do índice parcial `inventory_count_lines_pendentes_idx`.
func (r *Repository) PendenciasPorAmbiente(ctx context.Context, conferencia uuid.UUID) ([]PendenciaDoAmbiente, error) {
	linhas, err := r.exec(ctx).Query(ctx, `
		SELECT r.id, r.name, count(*)::int
		  FROM inventory_count_lines l
		  JOIN unit_rooms r ON r.id = l.room_id
		 WHERE l.count_id = $1 AND l.counted_qty IS NULL
		 GROUP BY r.id, r.name, r.sort_order
		 ORDER BY r.sort_order, r.name, r.id`, conferencia)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []PendenciaDoAmbiente{}
	for linhas.Next() {
		var p PendenciaDoAmbiente
		if err := linhas.Scan(&p.AmbienteID, &p.AmbienteNome, &p.Pendentes); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, p)
	}
	return out, db.MapError(linhas.Err())
}

// CongelarCustos copia o custo de reposição do catálogo para TODAS as linhas
// da conferência — CÓPIA, não JOIN (regra 7): recotar o prato em junho não
// pode mudar a perda apurada em março. Roda na transação do fechamento, com a
// conferência travada; é o único escritor da coluna. `updated_at` não muda: a
// contagem da linha não foi editada.
func (r *Repository) CongelarCustos(ctx context.Context, conferencia uuid.UUID) error {
	_, err := r.exec(ctx).Exec(ctx, `
		UPDATE inventory_count_lines AS l
		   SET replacement_cost_cents = i.replacement_cost_cents
		  FROM inventory_items AS i
		 WHERE i.id = l.item_id AND l.count_id = $1`, conferencia)
	return db.MapError(err)
}

// linhaApurada é uma linha contada que diverge, com o custo CONGELADO e a
// avaria que o fechamento abriu para ela, quando abriu.
type linhaApurada struct {
	AmbienteID   uuid.UUID
	AmbienteNome string
	BemID        uuid.UUID
	BemNome      string
	QtdEsperada  int
	QtdContada   int
	Custo        *int64
	AvariaID     *uuid.UUID
}

// nascidaNoFechamento reconhece a avaria que o PRÓPRIO fechamento abriu: tem o
// `count_id` desta conferência e `reported_at` igual ao `closed_at` dela. Os
// dois são o `now()` da mesma transação — o do fechamento —, e nenhuma outra
// transação escreve com esse instante. É o que separa a falta apurada da
// avaria que alguém relatou à mão citando a conferência, antes ou depois.
const nascidaNoFechamento = `ii.count_id = c.id AND ii.reported_at = c.closed_at`

// LinhasDivergentes lê, na ordem de caminhada, as linhas em que contado ≠
// esperado, com o custo congelado na linha (nulo antes do fechamento) e a
// avaria nascida no fechamento. Serve ao próprio fechamento e à leitura de
// volta de `result` — a mesma consulta, para as duas respostas não divergirem.
func (r *Repository) LinhasDivergentes(ctx context.Context, conferencia uuid.UUID) ([]linhaApurada, error) {
	linhas, err := r.exec(ctx).Query(ctx, `
		SELECT l.room_id, r.name, l.item_id, i.name, l.expected_qty, l.counted_qty, l.replacement_cost_cents,
		       (SELECT ii.id FROM inventory_issues ii
		         WHERE `+nascidaNoFechamento+` AND ii.room_id = l.room_id AND ii.item_id = l.item_id
		         ORDER BY ii.id LIMIT 1)
		  FROM inventory_count_lines l
		  JOIN inventory_counts c ON c.id = l.count_id
		  JOIN unit_rooms r ON r.id = l.room_id
		  JOIN inventory_items i ON i.id = l.item_id
		 WHERE l.count_id = $1 AND l.counted_qty <> l.expected_qty
		 ORDER BY `+ordemDasLinhas, conferencia)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []linhaApurada{}
	for linhas.Next() {
		var l linhaApurada
		if err := linhas.Scan(&l.AmbienteID, &l.AmbienteNome, &l.BemID, &l.BemNome,
			&l.QtdEsperada, &l.QtdContada, &l.Custo, &l.AvariaID); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, l)
	}
	return out, db.MapError(linhas.Err())
}

// AvariasDoFechamento conta as avarias que o fechamento abriu (`issues_created`).
func (r *Repository) AvariasDoFechamento(ctx context.Context, conferencia uuid.UUID) (int, error) {
	var n int
	err := r.exec(ctx).QueryRow(ctx, `
		SELECT count(*) FROM inventory_issues ii JOIN inventory_counts c ON c.id = ii.count_id
		 WHERE c.id = $1 AND `+nascidaNoFechamento, conferencia).Scan(&n)
	return n, db.MapError(err)
}
