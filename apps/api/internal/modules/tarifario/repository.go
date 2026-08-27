package tarifario

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

// Repository é o SQL do tarifário. Nunca abre transação: pega o executor do
// contexto com db.From, que devolve a transação em curso quando existe.
type Repository struct {
	pool db.DBTX
}

func NovoRepository(pool db.DBTX) *Repository { return &Repository{pool: pool} }

func (r *Repository) exec(ctx context.Context) db.DBTX { return db.From(ctx, r.pool) }

// hoje é a data civil da propriedade, calculada NO BANCO a partir do fuso
// gravado em `properties.timezone`.
//
// Vem daí, e não do relógio do processo Go, porque "vigente hoje" é uma pergunta
// sobre o calendário da casa: uma API rodando em UTC viraria o dia três horas
// antes de Luís Correia, e a tabela de tarifas do dia seguinte entraria em vigor
// durante a madrugada anterior.
const hoje = `(now() AT TIME ZONE p.timezone)::date`

// PropriedadePadrao devolve a propriedade das configurações quando o contexto
// não traz um usuário autenticado (seed, teste, tarefa de manutenção).
func (r *Repository) PropriedadePadrao(ctx context.Context) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT id FROM properties WHERE active ORDER BY created_at LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apperr.Validation(map[string]string{
			"property_id": "nenhuma propriedade cadastrada; rode o seed antes de configurar o tarifário.",
		})
	}
	if err != nil {
		return uuid.Nil, db.MapError(err)
	}
	return id, nil
}

// ═══════════════════════ Tabelas de tarifas ═══════════════════════

const camposDaTabela = `id, name, valid_from, valid_to, active, created_at`

func lerTabela(linha pgx.Row, extras ...any) (TabelaDeTarifas, error) {
	var (
		t         TabelaDeTarifas
		validoDe  time.Time
		validoAte *time.Time
	)
	alvos := append([]any{&t.ID, &t.Nome, &validoDe, &validoAte, &t.Ativa, &t.CriadaEm}, extras...)
	if err := linha.Scan(alvos...); err != nil {
		return t, err
	}
	t.ValidoDe, t.ValidoAte = DataDe(validoDe), dataDePonteiro(validoAte)
	return t, nil
}

func (r *Repository) ListarTabelas(ctx context.Context, prop uuid.UUID, f FiltroDeTabelas, pagina, porPagina int) ([]TabelaDeTarifas, int64, error) {
	const q = `
		SELECT ` + camposDaTabela + `, count(*) OVER() AS total
		  FROM rate_tables
		 WHERE property_id = $1
		   AND ($2::boolean IS NULL OR active = $2)
		   AND ($3::date IS NULL OR (valid_from <= $3 AND (valid_to IS NULL OR valid_to >= $3)))
		 ORDER BY valid_from DESC, name ASC, id ASC
		 LIMIT $4 OFFSET $5`

	linhas, err := r.exec(ctx).Query(ctx, q, prop, f.Ativa, ponteiroDeTempo(f.Em), porPagina, httpx.Offset(pagina, porPagina))
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	var (
		out   []TabelaDeTarifas
		total int64
	)
	for linhas.Next() {
		t, err := lerTabela(linhas, &total)
		if err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, t)
	}
	return out, total, db.MapError(linhas.Err())
}

func (r *Repository) BuscarTabela(ctx context.Context, prop, id uuid.UUID) (TabelaDeTarifas, error) {
	const q = `SELECT ` + camposDaTabela + ` FROM rate_tables WHERE property_id = $1 AND id = $2`

	t, err := lerTabela(r.exec(ctx).QueryRow(ctx, q, prop, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return t, apperr.NotFound("Tabela de tarifas")
	}
	if err != nil {
		return t, db.MapError(err)
	}
	return t, nil
}

func (r *Repository) CriarTabela(ctx context.Context, prop uuid.UUID, e TabelaEntrada) (uuid.UUID, error) {
	const q = `
		INSERT INTO rate_tables (property_id, name, valid_from, valid_to, active)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`

	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, prop, e.Nome,
		e.ValidoDe.Tempo(), ponteiroDeTempo(e.ValidoAte), e.Ativa.Ou(true)).Scan(&id)
	if err != nil {
		return uuid.Nil, r.traduzirTabela(err)
	}
	return id, nil
}

// SubstituirTabela é o PUT: todos os campos do cadastro, sem exceção.
func (r *Repository) SubstituirTabela(ctx context.Context, prop, id uuid.UUID, e TabelaEntrada) error {
	const q = `
		UPDATE rate_tables
		   SET name = $3, valid_from = $4, valid_to = $5, active = $6
		 WHERE property_id = $1 AND id = $2`

	tag, err := r.exec(ctx).Exec(ctx, q, prop, id, e.Nome,
		e.ValidoDe.Tempo(), ponteiroDeTempo(e.ValidoAte), e.Ativa.Ou(true))
	if err != nil {
		return r.traduzirTabela(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Tabela de tarifas")
	}
	return nil
}

func (r *Repository) AtualizarTabela(ctx context.Context, prop, id uuid.UUID, a TabelaAtualizar) error {
	s := novoSets(prop, id)
	if v, ok := a.Nome.Definido(); ok {
		s.add("name", v)
	}
	if v, ok := a.ValidoDe.Definido(); ok {
		s.add("valid_from", v.Tempo())
	}
	if v, ok := a.ValidoAte.Definido(); ok {
		s.add("valid_to", v.Tempo())
	} else if a.ValidoAte.DeveLimpar() {
		s.add("valid_to", (*time.Time)(nil)) // `null` explícito = tabela sem fim
	}
	if v, ok := a.Ativa.Definido(); ok {
		s.add("active", v)
	}
	if s.vazio() {
		return nil
	}

	tag, err := r.exec(ctx).Exec(ctx, s.update("rate_tables", "property_id = $1 AND id = $2"), s.args...)
	if err != nil {
		return r.traduzirTabela(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Tabela de tarifas")
	}
	return nil
}

// DesativarTabela é o DELETE: `active = false`, nunca remoção física.
//
// Apagar de verdade levaria junto, em cascata, as tarifas e os mínimos de noite
// que as reservas antigas referenciam por `rate_table_id` — e a reserva do ano
// passado perderia a prova de por qual tabela ela foi precificada.
func (r *Repository) DesativarTabela(ctx context.Context, prop, id uuid.UUID) error {
	tag, err := r.exec(ctx).Exec(ctx,
		`UPDATE rate_tables SET active = false WHERE property_id = $1 AND id = $2`, prop, id)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Tabela de tarifas")
	}
	return nil
}

// ContarTabelasVigentes conta quantas tabelas cobrem HOJE, opcionalmente
// ignorando uma. É o que sustenta o 409 do DELETE: desativar a última vigente
// deixaria toda emissão de orçamento em RATE_NOT_FOUND.
func (r *Repository) ContarTabelasVigentes(ctx context.Context, prop uuid.UUID, exceto *uuid.UUID) (int, error) {
	const q = `
		SELECT count(*)
		  FROM rate_tables rt
		  JOIN properties p ON p.id = rt.property_id
		 WHERE rt.property_id = $1
		   AND ($2::uuid IS NULL OR rt.id <> $2)
		   AND rt.active
		   AND rt.valid_from <= ` + hoje + `
		   AND (rt.valid_to IS NULL OR rt.valid_to >= ` + hoje + `)`

	var n int
	if err := r.exec(ctx).QueryRow(ctx, q, prop, exceto).Scan(&n); err != nil {
		return 0, db.MapError(err)
	}
	return n, nil
}

func (r *Repository) traduzirTabela(err error) error {
	if db.IsUniqueViolation(err) {
		return ErroCodigoEmUso.
			WithMessage("Já existe uma tabela de tarifas com este nome.").
			WithDetails(map[string]string{"name": "já está em uso."}).
			WithCause(err)
	}
	return db.MapError(err)
}

// ═══════════════════════ Tarifas ═══════════════════════

// A grade sai na ordem em que a tela a desenha: produto na ordem do inventário,
// tipo de data do menos para o mais caro (a precedência é DADO, spec §3).
const (
	deTarifas = `
		  FROM rates r
		  JOIN rate_tables rt ON rt.id = r.rate_table_id
		  JOIN unit_types  ut ON ut.id = r.unit_type_id
		  LEFT JOIN date_type_rules dt ON dt.kind = r.date_type`
	camposDaTarifa = `r.id, r.rate_table_id, r.unit_type_id, ut.code, r.date_type, r.amount_cents`
	ordemDaTarifa  = ` ORDER BY ut.sort_order, ut.code, dt.precedence, r.date_type`
)

func lerTarifa(linha pgx.Row, extras ...any) (Tarifa, error) {
	var t Tarifa
	alvos := append([]any{&t.ID, &t.TabelaID, &t.ProdutoID, &t.ProdutoCodigo, &t.TipoDeData, &t.ValorCents}, extras...)
	err := linha.Scan(alvos...)
	return t, err
}

func (r *Repository) ListarTarifas(ctx context.Context, prop uuid.UUID, f FiltroDeTarifas, pagina, porPagina int) ([]Tarifa, int64, error) {
	const q = `
		SELECT ` + camposDaTarifa + `, count(*) OVER() AS total` + deTarifas + `
		 WHERE rt.property_id = $1
		   AND ($2::uuid IS NULL OR r.rate_table_id = $2)
		   AND ($3::uuid IS NULL OR r.unit_type_id = $3)
		   AND ($4::text IS NULL OR r.date_type = $4)` + ordemDaTarifa + `
		 LIMIT $5 OFFSET $6`

	linhas, err := r.exec(ctx).Query(ctx, q, prop, f.TabelaID, f.ProdutoID, f.TipoDeData,
		porPagina, httpx.Offset(pagina, porPagina))
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	var (
		out   []Tarifa
		total int64
	)
	for linhas.Next() {
		t, err := lerTarifa(linhas, &total)
		if err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, t)
	}
	return out, total, db.MapError(linhas.Err())
}

// GradeDaTabela devolve a grade inteira de uma tabela, sem paginar. É a resposta
// de POST /rates/bulk: a tela acabou de mandar o estado completo e precisa ver
// como ele ficou gravado.
func (r *Repository) GradeDaTabela(ctx context.Context, prop, tabela uuid.UUID) ([]Tarifa, error) {
	const q = `
		SELECT ` + camposDaTarifa + deTarifas + `
		 WHERE rt.property_id = $1 AND r.rate_table_id = $2` + ordemDaTarifa

	linhas, err := r.exec(ctx).Query(ctx, q, prop, tabela)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []Tarifa{}
	for linhas.Next() {
		t, err := lerTarifa(linhas)
		if err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, t)
	}
	return out, db.MapError(linhas.Err())
}

func (r *Repository) BuscarTarifa(ctx context.Context, prop, id uuid.UUID) (Tarifa, error) {
	const q = `SELECT ` + camposDaTarifa + deTarifas + ` WHERE rt.property_id = $1 AND r.id = $2`

	t, err := lerTarifa(r.exec(ctx).QueryRow(ctx, q, prop, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return t, apperr.NotFound("Tarifa")
	}
	if err != nil {
		return t, db.MapError(err)
	}
	return t, nil
}

// CriarTarifa insere a célula. O `INSERT ... SELECT` com os dois JOINs é o que
// garante que tabela e produto são da MESMA propriedade do requisitante: sem
// isso, um id copiado de outra casa entraria pela FK sem ninguém notar.
func (r *Repository) CriarTarifa(ctx context.Context, prop uuid.UUID, e TarifaEntrada) (uuid.UUID, error) {
	const q = `
		INSERT INTO rates (rate_table_id, unit_type_id, date_type, amount_cents)
		SELECT rt.id, ut.id, $4, $5
		  FROM rate_tables rt
		  JOIN unit_types  ut ON ut.property_id = rt.property_id
		 WHERE rt.property_id = $1 AND rt.id = $2 AND ut.id = $3
		RETURNING id`

	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, prop, e.TabelaID, e.ProdutoID, e.TipoDeData, e.ValorCents).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apperr.NotFound("Tabela de tarifas ou produto")
	}
	if err != nil {
		return uuid.Nil, r.traduzirTarifa(err)
	}
	return id, nil
}

func (r *Repository) SubstituirTarifa(ctx context.Context, prop, id uuid.UUID, e TarifaEntrada) error {
	const q = `
		UPDATE rates r
		   SET rate_table_id = $3, unit_type_id = $4, date_type = $5, amount_cents = $6
		 WHERE r.id = $2
		   AND r.rate_table_id IN (SELECT id FROM rate_tables WHERE property_id = $1)
		   AND EXISTS (SELECT 1 FROM rate_tables t WHERE t.id = $3 AND t.property_id = $1)
		   AND EXISTS (SELECT 1 FROM unit_types  u WHERE u.id = $4 AND u.property_id = $1)`

	tag, err := r.exec(ctx).Exec(ctx, q, prop, id, e.TabelaID, e.ProdutoID, e.TipoDeData, e.ValorCents)
	if err != nil {
		return r.traduzirTarifa(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Tarifa")
	}
	return nil
}

func (r *Repository) AtualizarTarifa(ctx context.Context, prop, id uuid.UUID, a TarifaAtualizar) error {
	s := novoSets(prop, id)
	if v, ok := a.ValorCents.Definido(); ok {
		s.add("amount_cents", v)
	}
	if v, ok := a.TipoDeData.Definido(); ok {
		s.add("date_type", v)
	}
	if v, ok := a.ProdutoID.Definido(); ok {
		s.add("unit_type_id", v)
	}
	if s.vazio() {
		return nil
	}

	onde := `id = $2
		   AND rate_table_id IN (SELECT id FROM rate_tables WHERE property_id = $1)`
	if _, trocaProduto := a.ProdutoID.Definido(); trocaProduto {
		onde += `
		   AND EXISTS (SELECT 1 FROM unit_types u WHERE u.id = ` + s.refDe("unit_type_id") + ` AND u.property_id = $1)`
	}

	tag, err := r.exec(ctx).Exec(ctx, s.update("rates", onde), s.args...)
	if err != nil {
		return r.traduzirTarifa(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Tarifa")
	}
	return nil
}

// ExcluirTarifa remove a célula de verdade. A linha não tem histórico próprio: o
// histórico está congelado em `reservation_nights`. Sem a célula, a noite
// daquele tipo passa a devolver RATE_NOT_FOUND no próximo orçamento — recusar é
// melhor do que vender por um valor inventado.
func (r *Repository) ExcluirTarifa(ctx context.Context, prop, id uuid.UUID) error {
	const q = `
		DELETE FROM rates
		 WHERE id = $2 AND rate_table_id IN (SELECT id FROM rate_tables WHERE property_id = $1)`

	tag, err := r.exec(ctx).Exec(ctx, q, prop, id)
	if err != nil {
		return r.traduzirTarifa(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Tarifa")
	}
	return nil
}

// CodigosDosProdutos devolve o código de cada produto informado que pertence a
// esta propriedade.
//
// Uma consulta que serve a dois propósitos de propósito: o TAMANHO do mapa é a
// validação do escopo (id que não voltou não é desta casa, e a grade inteira é
// recusada antes de qualquer escrita), e o CONTEÚDO é o que deixa a trilha de
// auditoria legível — `apto-2s.reveillon` em vez de dois uuids.
func (r *Repository) CodigosDosProdutos(ctx context.Context, prop uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	const q = `SELECT id, code FROM unit_types WHERE property_id = $1 AND id = ANY($2::uuid[])`

	linhas, err := r.exec(ctx).Query(ctx, q, prop, ids)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := make(map[uuid.UUID]string, len(ids))
	for linhas.Next() {
		var (
			id     uuid.UUID
			codigo string
		)
		if err := linhas.Scan(&id, &codigo); err != nil {
			return nil, db.MapError(err)
		}
		out[id] = codigo
	}
	return out, db.MapError(linhas.Err())
}

// CelulasDoEscopo lê — TRAVANDO — as células dos produtos que a chamada vai
// reescrever.
//
// Só os produtos do escopo: é esta cláusula que impede o bulk de um produto de
// enxergar (e, portanto, de apagar) a tarifa dos outros.
//
// `FOR UPDATE OF r` porque o número que sai em `meta.removed` é o que avisa o
// operador de uma remoção que ele não pretendia; se dois salvamentos do MESMO
// escopo correrem juntos, a contagem de um deles descreveria um estado que já
// não existe, e um `removed: 0` mentiroso é pior que nenhum número. `OF r`
// limita a trava a `rates`: travar `unit_types` no caminho do JOIN bloquearia a
// edição do inventário sem nenhum motivo.
func (r *Repository) CelulasDoEscopo(ctx context.Context, tabela uuid.UUID, escopo []uuid.UUID) ([]CelulaGravada, error) {
	if len(escopo) == 0 {
		return nil, nil
	}

	const q = `
		SELECT r.id, r.unit_type_id, ut.code, r.date_type, r.amount_cents
		  FROM rates r
		  JOIN unit_types ut ON ut.id = r.unit_type_id
		 WHERE r.rate_table_id = $1 AND r.unit_type_id = ANY($2::uuid[])
		 ORDER BY ut.sort_order, ut.code, r.date_type
		   FOR UPDATE OF r`

	linhas, err := r.exec(ctx).Query(ctx, q, tabela, escopo)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []CelulaGravada{}
	for linhas.Next() {
		var c CelulaGravada
		if err := linhas.Scan(&c.ID, &c.ProdutoID, &c.ProdutoCodigo, &c.TipoDeData, &c.ValorCents); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, c)
	}
	return out, db.MapError(linhas.Err())
}

// AplicarGrade executa o plano: apaga as células que saíram e grava as que
// ficam. Roda dentro da transação do service.
//
// A remoção é por `id` — os ids que o plano decidiu, todos dentro do escopo. A
// versão anterior apagava por `rate_table_id` e era esse o defeito.
//
// A gravação é UPSERT, e não DELETE seguido de INSERT, por dois motivos: a
// célula que não mudou de valor conserva a mesma linha (o `rates.id` que a tela
// já tem em mãos continua válido depois de salvar), e o que acontece no banco
// passa a ser o que a trilha diz que aconteceu — UPDATE onde o preço mudou,
// INSERT onde a célula nasceu.
func (r *Repository) AplicarGrade(ctx context.Context, tabela uuid.UUID, remover []uuid.UUID, gravar []CelulaDaGrade) error {
	exec := r.exec(ctx)

	if len(remover) > 0 {
		const q = `DELETE FROM rates WHERE rate_table_id = $1 AND id = ANY($2::uuid[])`
		if _, err := exec.Exec(ctx, q, tabela, remover); err != nil {
			return db.MapError(err)
		}
	}
	if len(gravar) == 0 {
		return nil
	}

	// Um INSERT só: 24 idas ao banco para uma grade de 4 produtos × 6 tipos
	// custariam mais que a transação inteira.
	valores := make([]string, 0, len(gravar))
	args := make([]any, 0, 1+len(gravar)*3)
	args = append(args, tabela)
	for _, c := range gravar {
		base := len(args)
		valores = append(valores, fmt.Sprintf("($1, $%d, $%d, $%d)", base+1, base+2, base+3))
		args = append(args, c.ProdutoID, c.TipoDeData, c.ValorCents)
	}

	q := `INSERT INTO rates (rate_table_id, unit_type_id, date_type, amount_cents) VALUES ` +
		strings.Join(valores, ", ") + `
		ON CONFLICT (rate_table_id, unit_type_id, date_type)
		DO UPDATE SET amount_cents = EXCLUDED.amount_cents`
	if _, err := exec.Exec(ctx, q, args...); err != nil {
		return r.traduzirTarifa(err)
	}
	return nil
}

func (r *Repository) traduzirTarifa(err error) error {
	if db.IsUniqueViolation(err) {
		return ErroCodigoEmUso.
			WithMessage("Já existe tarifa para este produto e tipo de data nesta tabela.").
			WithCause(err)
	}
	if db.IsForeignKeyViolation(err) {
		return apperr.Validation(map[string]string{
			"date_type": "tipo de data fora do vocabulário de date_type_rules.",
		}).WithCause(err)
	}
	return db.MapError(err)
}

// ═══════════════════════ Feriados ═══════════════════════

const camposDoFeriado = `id, date, name, active`

func lerFeriado(linha pgx.Row, extras ...any) (Feriado, error) {
	var (
		f    Feriado
		data time.Time
	)
	alvos := append([]any{&f.ID, &data, &f.Nome, &f.Ativo}, extras...)
	if err := linha.Scan(alvos...); err != nil {
		return f, err
	}
	f.Data = DataDe(data)
	return f, nil
}

func (r *Repository) ListarFeriados(ctx context.Context, prop uuid.UUID, f FiltroDeCalendario, pagina, porPagina int) ([]Feriado, int64, error) {
	const q = `
		SELECT ` + camposDoFeriado + `, count(*) OVER() AS total
		  FROM holidays
		 WHERE property_id = $1
		   AND ($2::date IS NULL OR date >= $2)
		   AND ($3::date IS NULL OR date <= $3)
		   AND ($4::boolean IS NULL OR active = $4)
		 ORDER BY date ASC, id ASC
		 LIMIT $5 OFFSET $6`

	linhas, err := r.exec(ctx).Query(ctx, q, prop, ponteiroDeTempo(f.De), ponteiroDeTempo(f.Ate), f.Ativo,
		porPagina, httpx.Offset(pagina, porPagina))
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	var (
		out   []Feriado
		total int64
	)
	for linhas.Next() {
		f, err := lerFeriado(linhas, &total)
		if err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, f)
	}
	return out, total, db.MapError(linhas.Err())
}

func (r *Repository) BuscarFeriado(ctx context.Context, prop, id uuid.UUID) (Feriado, error) {
	const q = `SELECT ` + camposDoFeriado + ` FROM holidays WHERE property_id = $1 AND id = $2`

	f, err := lerFeriado(r.exec(ctx).QueryRow(ctx, q, prop, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return f, apperr.NotFound("Feriado")
	}
	if err != nil {
		return f, db.MapError(err)
	}
	return f, nil
}

func (r *Repository) CriarFeriado(ctx context.Context, prop uuid.UUID, e FeriadoEntrada) (uuid.UUID, error) {
	const q = `INSERT INTO holidays (property_id, date, name, active) VALUES ($1,$2,$3,$4) RETURNING id`

	var id uuid.UUID
	if err := r.exec(ctx).QueryRow(ctx, q, prop, e.Data.Tempo(), e.Nome, e.Ativo.Ou(true)).Scan(&id); err != nil {
		return uuid.Nil, r.traduzirFeriado(err)
	}
	return id, nil
}

func (r *Repository) SubstituirFeriado(ctx context.Context, prop, id uuid.UUID, e FeriadoEntrada) error {
	const q = `UPDATE holidays SET date = $3, name = $4, active = $5 WHERE property_id = $1 AND id = $2`

	tag, err := r.exec(ctx).Exec(ctx, q, prop, id, e.Data.Tempo(), e.Nome, e.Ativo.Ou(true))
	if err != nil {
		return r.traduzirFeriado(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Feriado")
	}
	return nil
}

func (r *Repository) AtualizarFeriado(ctx context.Context, prop, id uuid.UUID, a FeriadoAtualizar) error {
	s := novoSets(prop, id)
	if v, ok := a.Data.Definido(); ok {
		s.add("date", v.Tempo())
	}
	if v, ok := a.Nome.Definido(); ok {
		s.add("name", v)
	}
	if v, ok := a.Ativo.Definido(); ok {
		s.add("active", v)
	}
	if s.vazio() {
		return nil
	}

	tag, err := r.exec(ctx).Exec(ctx, s.update("holidays", "property_id = $1 AND id = $2"), s.args...)
	if err != nil {
		return r.traduzirFeriado(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Feriado")
	}
	return nil
}

func (r *Repository) ExcluirFeriado(ctx context.Context, prop, id uuid.UUID) error {
	tag, err := r.exec(ctx).Exec(ctx, `DELETE FROM holidays WHERE property_id = $1 AND id = $2`, prop, id)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Feriado")
	}
	return nil
}

func (r *Repository) traduzirFeriado(err error) error {
	if db.IsUniqueViolation(err) {
		return ErroCodigoEmUso.
			WithMessage("Já existe feriado cadastrado nesta data.").
			WithDetails(map[string]string{"date": "já está em uso."}).
			WithCause(err)
	}
	return db.MapError(err)
}

// ═══════════════════════ Períodos especiais ═══════════════════════

const camposDoPeriodo = `id, name, kind, starts_on, ends_on, active`

func lerPeriodo(linha pgx.Row, extras ...any) (PeriodoEspecial, error) {
	var (
		p           PeriodoEspecial
		comeca, fim time.Time
	)
	alvos := append([]any{&p.ID, &p.Nome, &p.Especie, &comeca, &fim, &p.Ativo}, extras...)
	if err := linha.Scan(alvos...); err != nil {
		return p, err
	}
	p.ComecaEm, p.TerminaEm = DataDe(comeca), DataDe(fim)
	return p, nil
}

// ListarPeriodos usa `daterange(..., '[]')` na janela porque o período é
// INCLUSIVO nas duas pontas — ao contrário da estadia. `&&` é o mesmo operador
// que o índice GiST da tabela serve.
func (r *Repository) ListarPeriodos(ctx context.Context, prop uuid.UUID, f FiltroDeCalendario, pagina, porPagina int) ([]PeriodoEspecial, int64, error) {
	const q = `
		SELECT ` + camposDoPeriodo + `, count(*) OVER() AS total
		  FROM special_periods
		 WHERE property_id = $1
		   AND ($2::date IS NULL OR ends_on   >= $2)
		   AND ($3::date IS NULL OR starts_on <= $3)
		   AND ($4::text IS NULL OR kind = $4)
		   AND ($5::boolean IS NULL OR active = $5)
		 ORDER BY starts_on ASC, name ASC, id ASC
		 LIMIT $6 OFFSET $7`

	linhas, err := r.exec(ctx).Query(ctx, q, prop, ponteiroDeTempo(f.De), ponteiroDeTempo(f.Ate), f.Especie, f.Ativo,
		porPagina, httpx.Offset(pagina, porPagina))
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	var (
		out   []PeriodoEspecial
		total int64
	)
	for linhas.Next() {
		p, err := lerPeriodo(linhas, &total)
		if err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, p)
	}
	return out, total, db.MapError(linhas.Err())
}

func (r *Repository) BuscarPeriodo(ctx context.Context, prop, id uuid.UUID) (PeriodoEspecial, error) {
	const q = `SELECT ` + camposDoPeriodo + ` FROM special_periods WHERE property_id = $1 AND id = $2`

	p, err := lerPeriodo(r.exec(ctx).QueryRow(ctx, q, prop, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return p, apperr.NotFound("Período especial")
	}
	if err != nil {
		return p, db.MapError(err)
	}
	return p, nil
}

func (r *Repository) CriarPeriodo(ctx context.Context, prop uuid.UUID, e PeriodoEntrada) (uuid.UUID, error) {
	const q = `
		INSERT INTO special_periods (property_id, name, kind, starts_on, ends_on, active)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`

	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, prop, e.Nome, e.Especie,
		e.ComecaEm.Tempo(), e.TerminaEm.Tempo(), e.Ativo.Ou(true)).Scan(&id)
	if err != nil {
		return uuid.Nil, r.traduzirPeriodo(err)
	}
	return id, nil
}

func (r *Repository) SubstituirPeriodo(ctx context.Context, prop, id uuid.UUID, e PeriodoEntrada) error {
	const q = `
		UPDATE special_periods
		   SET name = $3, kind = $4, starts_on = $5, ends_on = $6, active = $7
		 WHERE property_id = $1 AND id = $2`

	tag, err := r.exec(ctx).Exec(ctx, q, prop, id, e.Nome, e.Especie,
		e.ComecaEm.Tempo(), e.TerminaEm.Tempo(), e.Ativo.Ou(true))
	if err != nil {
		return r.traduzirPeriodo(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Período especial")
	}
	return nil
}

func (r *Repository) AtualizarPeriodo(ctx context.Context, prop, id uuid.UUID, a PeriodoAtualizar) error {
	s := novoSets(prop, id)
	if v, ok := a.Nome.Definido(); ok {
		s.add("name", v)
	}
	if v, ok := a.Especie.Definido(); ok {
		s.add("kind", v)
	}
	if v, ok := a.ComecaEm.Definido(); ok {
		s.add("starts_on", v.Tempo())
	}
	if v, ok := a.TerminaEm.Definido(); ok {
		s.add("ends_on", v.Tempo())
	}
	if v, ok := a.Ativo.Definido(); ok {
		s.add("active", v)
	}
	if s.vazio() {
		return nil
	}

	tag, err := r.exec(ctx).Exec(ctx, s.update("special_periods", "property_id = $1 AND id = $2"), s.args...)
	if err != nil {
		return r.traduzirPeriodo(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Período especial")
	}
	return nil
}

func (r *Repository) ExcluirPeriodo(ctx context.Context, prop, id uuid.UUID) error {
	tag, err := r.exec(ctx).Exec(ctx,
		`DELETE FROM special_periods WHERE property_id = $1 AND id = $2`, prop, id)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Período especial")
	}
	return nil
}

func (r *Repository) traduzirPeriodo(err error) error {
	if db.IsUniqueViolation(err) {
		return ErroCodigoEmUso.
			WithMessage("Já existe um período especial com este nome.").
			WithDetails(map[string]string{
				"name": "já está em uso — o nome carrega o ano de propósito (\"Réveillon 2026/2027\").",
			}).
			WithCause(err)
	}
	return db.MapError(err)
}

// ═══════════════════════ Mínimo de noites ═══════════════════════

const camposDoMinimo = `mn.id, mn.rate_table_id, mn.date_type, mn.nights`

func lerMinimo(linha pgx.Row, extras ...any) (MinimoDeNoites, error) {
	var m MinimoDeNoites
	alvos := append([]any{&m.ID, &m.TabelaID, &m.TipoDeData, &m.Noites}, extras...)
	err := linha.Scan(alvos...)
	return m, err
}

func (r *Repository) ListarMinimos(ctx context.Context, prop uuid.UUID, f FiltroDeMinimos, pagina, porPagina int) ([]MinimoDeNoites, int64, error) {
	const q = `
		SELECT ` + camposDoMinimo + `, count(*) OVER() AS total
		  FROM min_nights_rules mn
		  JOIN rate_tables rt ON rt.id = mn.rate_table_id
		  LEFT JOIN date_type_rules dt ON dt.kind = mn.date_type
		 WHERE rt.property_id = $1
		   AND ($2::uuid IS NULL OR mn.rate_table_id = $2)
		   AND ($3::text IS NULL OR mn.date_type = $3)
		 ORDER BY rt.valid_from DESC, dt.precedence, mn.date_type
		 LIMIT $4 OFFSET $5`

	linhas, err := r.exec(ctx).Query(ctx, q, prop, f.TabelaID, f.TipoDeData,
		porPagina, httpx.Offset(pagina, porPagina))
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	var (
		out   []MinimoDeNoites
		total int64
	)
	for linhas.Next() {
		m, err := lerMinimo(linhas, &total)
		if err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, m)
	}
	return out, total, db.MapError(linhas.Err())
}

func (r *Repository) BuscarMinimo(ctx context.Context, prop, id uuid.UUID) (MinimoDeNoites, error) {
	const q = `
		SELECT ` + camposDoMinimo + `
		  FROM min_nights_rules mn
		  JOIN rate_tables rt ON rt.id = mn.rate_table_id
		 WHERE rt.property_id = $1 AND mn.id = $2`

	m, err := lerMinimo(r.exec(ctx).QueryRow(ctx, q, prop, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return m, apperr.NotFound("Mínimo de noites")
	}
	if err != nil {
		return m, db.MapError(err)
	}
	return m, nil
}

func (r *Repository) CriarMinimo(ctx context.Context, prop uuid.UUID, e MinimoEntrada) (uuid.UUID, error) {
	const q = `
		INSERT INTO min_nights_rules (rate_table_id, date_type, nights)
		SELECT rt.id, $3, $4 FROM rate_tables rt WHERE rt.property_id = $1 AND rt.id = $2
		RETURNING id`

	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, prop, e.TabelaID, e.TipoDeData, e.Noites).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apperr.NotFound("Tabela de tarifas")
	}
	if err != nil {
		return uuid.Nil, r.traduzirMinimo(err)
	}
	return id, nil
}

func (r *Repository) SubstituirMinimo(ctx context.Context, prop, id uuid.UUID, e MinimoEntrada) error {
	const q = `
		UPDATE min_nights_rules
		   SET rate_table_id = $3, date_type = $4, nights = $5
		 WHERE id = $2
		   AND rate_table_id IN (SELECT rt.id FROM rate_tables rt WHERE rt.property_id = $1)
		   AND EXISTS (SELECT 1 FROM rate_tables t WHERE t.id = $3 AND t.property_id = $1)`

	tag, err := r.exec(ctx).Exec(ctx, q, prop, id, e.TabelaID, e.TipoDeData, e.Noites)
	if err != nil {
		return r.traduzirMinimo(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Mínimo de noites")
	}
	return nil
}

func (r *Repository) AtualizarMinimo(ctx context.Context, prop, id uuid.UUID, a MinimoAtualizar) error {
	s := novoSets(prop, id)
	if v, ok := a.TipoDeData.Definido(); ok {
		s.add("date_type", v)
	}
	if v, ok := a.Noites.Definido(); ok {
		s.add("nights", v)
	}
	if s.vazio() {
		return nil
	}

	onde := `id = $2 AND rate_table_id IN (SELECT rt.id FROM rate_tables rt WHERE rt.property_id = $1)`
	tag, err := r.exec(ctx).Exec(ctx, s.update("min_nights_rules", onde), s.args...)
	if err != nil {
		return r.traduzirMinimo(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Mínimo de noites")
	}
	return nil
}

func (r *Repository) ExcluirMinimo(ctx context.Context, prop, id uuid.UUID) error {
	const q = `
		DELETE FROM min_nights_rules
		 WHERE id = $2 AND rate_table_id IN (SELECT rt.id FROM rate_tables rt WHERE rt.property_id = $1)`

	tag, err := r.exec(ctx).Exec(ctx, q, prop, id)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Mínimo de noites")
	}
	return nil
}

func (r *Repository) traduzirMinimo(err error) error {
	if db.IsUniqueViolation(err) {
		return ErroCodigoEmUso.
			WithMessage("Já existe mínimo de noites para este tipo de data nesta tabela.").
			WithCause(err)
	}
	return db.MapError(err)
}
