package crm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// Repository fala com o Postgres. Nunca abre transação: pega o executor do
// contexto com db.From, que devolve a transação em curso quando existe — é o
// que permite a mesma função servir dentro e fora de transação.
type Repository struct {
	pool db.DBTX
}

func NewRepository(pool db.DBTX) *Repository { return &Repository{pool: pool} }

func (r *Repository) exec(ctx context.Context) db.DBTX { return db.From(ctx, r.pool) }

// Pool devolve o executor base. Existe para o `audit` receber o mesmo pool do
// repositório sem que o service precise carregar um segundo campo.
func (r *Repository) Pool() db.DBTX { return r.pool }

// ─────────────────────────── Relógio da casa ────────────────────────

// Agora devolve o instante do BANCO. O "agora" de todo derivado deste módulo
// (SLA estourado, tarefa vencida, parado há N dias) sai daqui, e não do relógio
// do processo: duas definições de agora divergem na virada do dia e fariam o
// mesmo card aparecer estourado para um operador e no prazo para outro.
func (r *Repository) Agora(ctx context.Context) (time.Time, error) {
	var t time.Time
	if err := r.exec(ctx).QueryRow(ctx, `SELECT now()`).Scan(&t); err != nil {
		return time.Time{}, db.MapError(err)
	}
	return t, nil
}

// ─────────────────────────── Conferências de escopo ─────────────────

// ConferirContato recusa contato de outra propriedade antes do INSERT.
//
// A foreign key não sabe de propriedade: um `contact_id` da casa vizinha
// passaria por ela e viraria um card com hóspede que a gestão não enxerga na
// própria lista.
func (r *Repository) ConferirContato(ctx context.Context, propriedade, id uuid.UUID) error {
	return r.existe(ctx, `SELECT 1 FROM contacts WHERE id = $1 AND property_id = $2`, id, propriedade, "Contato")
}

// ConferirProduto recusa produto de outra propriedade. Diferente do módulo de
// reservas, aqui produto INATIVO é aceito: a oportunidade é intenção, não venda,
// e um produto que saiu de linha continua sendo o que o cliente pediu.
func (r *Repository) ConferirProduto(ctx context.Context, propriedade, id uuid.UUID) error {
	return r.existe(ctx, `SELECT 1 FROM unit_types WHERE id = $1 AND property_id = $2`, id, propriedade, "Produto")
}

// ConferirUsuario recusa dono de outra propriedade. Sem isto, `owner_id` de um
// usuário da casa vizinha criaria um card que ninguém desta casa enxerga com
// escopo `own` e que ninguém de lá enxerga por causa do `property_id`.
func (r *Repository) ConferirUsuario(ctx context.Context, propriedade, id uuid.UUID) error {
	return r.existe(ctx, `SELECT 1 FROM users WHERE id = $1 AND property_id = $2`, id, propriedade, "Usuário")
}

func (r *Repository) existe(ctx context.Context, q string, id, propriedade uuid.UUID, rotulo string) error {
	var um int
	err := r.exec(ctx).QueryRow(ctx, q, id, propriedade).Scan(&um)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound(rotulo)
	}
	return db.MapError(err)
}

// ═══════════════════════════ Funis ══════════════════════════════════

// colunasDoFunil é a projeção do schema `Funil`, com os dois derivados.
//
// `open_opportunity_count` respeita o ESCOPO do requisitante: sem o
// `($2::uuid IS NULL OR o.owner_id = $2)`, o corretor leria no seletor de funil
// a contagem da casa inteira — um vazamento silencioso pelo lado do número.
// A posição do parâmetro do dono é PARÂMETRO da projeção, não fixa: a listagem
// e a busca por id têm listas de argumentos diferentes, e fixar `$2` obrigava a
// consulta de contagem — que não usa a projeção — a receber um argumento que
// não aparece em lugar nenhum. O Postgres então recusava com
// `could not determine data type of parameter $2` (42P18), e `GET /crm/pipelines`
// devolvia 500. Descoberto rodando o painel: a tela do funil abria com o aviso
// vermelho "não foi possível carregar os funis".
func colunasDoFunil(paramDono int) string {
	return fmt.Sprintf(`
	    p.id, p.name, p.is_default, p.active,
	    (SELECT count(*) FROM crm_stages s WHERE s.pipeline_id = p.id),
	    (SELECT count(*) FROM crm_opportunities o
	      WHERE o.pipeline_id = p.id AND o.status = 'aberto'
	        AND ($%d::uuid IS NULL OR o.owner_id = $%d))`, paramDono, paramDono)
}

func lerFunil(linha pgx.Row) (Funil, error) {
	var f Funil
	err := linha.Scan(&f.ID, &f.Nome, &f.Padrao, &f.Ativo, &f.QtdEtapas, &f.QtdAbertas)
	return f, err
}

// ListarFunis devolve a página e o total.
func (r *Repository) ListarFunis(ctx context.Context, propriedade uuid.UUID, dono *uuid.UUID,
	ativo *bool, busca string, pagina, porPagina int) ([]Funil, int64, error) {

	// `crm_pipelines` não tem dono: funil é configuração compartilhada. O dono
	// só entra na CONTAGEM de oportunidades abertas, dentro da projeção — por
	// isso a consulta de count(*) não o recebe.
	const base = `FROM crm_pipelines p
	              WHERE p.property_id = $1
	                AND ($2::boolean IS NULL OR p.active = $2)
	                AND ($3::text = '' OR p.name ILIKE '%%' || $3 || '%%')`
	filtro := strings.ReplaceAll(base, "%%", "%")

	var total int64
	if err := r.exec(ctx).QueryRow(ctx, `SELECT count(*) `+filtro,
		propriedade, ativo, busca).Scan(&total); err != nil {
		return nil, 0, db.MapError(err)
	}

	q := `SELECT ` + colunasDoFunil(4) + ` ` + filtro +
		` ORDER BY p.is_default DESC, p.name ASC LIMIT $5 OFFSET $6`

	linhas, err := r.exec(ctx).Query(ctx, q, propriedade, ativo, busca, dono, porPagina, (pagina-1)*porPagina)
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	out := []Funil{}
	for linhas.Next() {
		f, err := lerFunil(linhas)
		if err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, f)
	}
	return out, total, db.MapError(linhas.Err())
}

// BuscarFunil devolve um funil pelo id.
func (r *Repository) BuscarFunil(ctx context.Context, propriedade uuid.UUID, dono *uuid.UUID, id uuid.UUID) (Funil, error) {
	q := `SELECT ` + colunasDoFunil(2) + ` FROM crm_pipelines p WHERE p.property_id = $1 AND p.id = $3`
	f, err := lerFunil(r.exec(ctx).QueryRow(ctx, q, propriedade, dono, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Funil{}, apperr.NotFound("Funil")
	}
	return f, db.MapError(err)
}

// FunilPadrao devolve o id do funil `is_default` ativo. É onde cai a
// oportunidade criada sem `pipeline_id`.
func (r *Repository) FunilPadrao(ctx context.Context, propriedade uuid.UUID) (uuid.UUID, error) {
	const q = `SELECT id FROM crm_pipelines WHERE property_id = $1 AND is_default AND active`
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, propriedade).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apperr.NotFound("Funil padrão").
			WithMessage("Nenhum funil padrão ativo configurado nesta propriedade.")
	}
	return id, db.MapError(err)
}

// TravarFunil lê o funil FOR UPDATE. Todo caminho que mexe em `is_default`
// passa por aqui: sem o lock, duas requisições simultâneas de "marque este como
// padrão" desmarcariam uma à outra e o índice parcial único recusaria a segunda
// com 23505 — erro certo, mensagem incompreensível.
func (r *Repository) TravarFunil(ctx context.Context, propriedade, id uuid.UUID) (Funil, error) {
	const q = `SELECT p.id, p.name, p.is_default, p.active, 0, 0
	             FROM crm_pipelines p
	            WHERE p.property_id = $1 AND p.id = $2
	              FOR UPDATE OF p`
	f, err := lerFunil(r.exec(ctx).QueryRow(ctx, q, propriedade, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Funil{}, apperr.NotFound("Funil")
	}
	return f, db.MapError(err)
}

// InserirFunil grava o funil novo.
func (r *Repository) InserirFunil(ctx context.Context, propriedade uuid.UUID, nome string, padrao, ativo bool) (uuid.UUID, error) {
	const q = `INSERT INTO crm_pipelines (property_id, name, is_default, active)
	           VALUES ($1, $2, $3, $4) RETURNING id`
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, propriedade, nome, padrao, ativo).Scan(&id)
	if db.IsUniqueViolation(err, "crm_pipelines_property_id_name_key") {
		return uuid.Nil, NomeEmUso.WithMessage("Já existe um funil com esse nome nesta propriedade.").WithCause(err)
	}
	return id, db.MapError(err)
}

// AtualizarFunil grava nome, padrão e ativo.
func (r *Repository) AtualizarFunil(ctx context.Context, id uuid.UUID, nome string, padrao, ativo bool) error {
	const q = `UPDATE crm_pipelines SET name = $2, is_default = $3, active = $4, updated_at = now()
	            WHERE id = $1`
	_, err := r.exec(ctx).Exec(ctx, q, id, nome, padrao, ativo)
	if db.IsUniqueViolation(err, "crm_pipelines_property_id_name_key") {
		return NomeEmUso.WithMessage("Já existe um funil com esse nome nesta propriedade.").WithCause(err)
	}
	return db.MapError(err)
}

// DesmarcarOutrosPadroes tira o `is_default` dos demais funis da propriedade.
//
// Roda ANTES de marcar o novo, na MESMA transação: o índice parcial único
// `crm_pipelines_default_idx` não é adiável, então a ordem "desmarca, depois
// marca" é a única que atravessa. Marcar primeiro estouraria 23505 com dois
// defaults momentâneos.
func (r *Repository) DesmarcarOutrosPadroes(ctx context.Context, propriedade, exceto uuid.UUID) error {
	const q = `UPDATE crm_pipelines SET is_default = false, updated_at = now()
	            WHERE property_id = $1 AND id <> $2 AND is_default`
	_, err := r.exec(ctx).Exec(ctx, q, propriedade, exceto)
	return db.MapError(err)
}

// OutroPadraoAtivo diz se existe OUTRO funil padrão ativo além deste. É a
// guarda de `DEFAULT_PIPELINE_REQUIRED`: sem padrão, `POST /crm/opportunities`
// sem `pipeline_id` não tem onde cair, e o erro apareceria longe daqui.
func (r *Repository) OutroPadraoAtivo(ctx context.Context, propriedade, exceto uuid.UUID) (bool, error) {
	const q = `SELECT EXISTS (SELECT 1 FROM crm_pipelines
	                           WHERE property_id = $1 AND id <> $2 AND is_default AND active)`
	var existe bool
	err := r.exec(ctx).QueryRow(ctx, q, propriedade, exceto).Scan(&existe)
	return existe, db.MapError(err)
}

// OportunidadesAbertasNoFunil conta o que ainda está vivo dentro do funil. É a
// guarda da desativação: esconder o funil deixaria esses cards sem coluna em
// que aparecer.
func (r *Repository) OportunidadesAbertasNoFunil(ctx context.Context, funil uuid.UUID) (int, error) {
	const q = `SELECT count(*) FROM crm_opportunities WHERE pipeline_id = $1 AND status = 'aberto'`
	var n int
	err := r.exec(ctx).QueryRow(ctx, q, funil).Scan(&n)
	return n, db.MapError(err)
}

// ═══════════════════════════ Etapas ═════════════════════════════════

const colunasDaEtapa = `
	    s.id, s.pipeline_id, s.name, s.position, s.probability, s.color, s.type,
	    s.sla_days, s.auto_task_subject, s.auto_task_type, s.auto_task_due_days, s.auto_notify`

func lerEtapa(linha pgx.Row) (Etapa, error) {
	var (
		e             Etapa
		probabilidade float64
	)
	err := linha.Scan(&e.ID, &e.FunilID, &e.Nome, &e.Posicao, &probabilidade, &e.Cor, &e.Tipo,
		&e.SLADias, &e.TarefaAssunto, &e.TarefaTipo, &e.TarefaPrazoDias, &e.Notificar)
	e.Probabilidade = int(math.Round(probabilidade))
	return e, err
}

func (r *Repository) etapas(ctx context.Context, q string, args ...any) ([]Etapa, error) {
	linhas, err := r.exec(ctx).Query(ctx, q, args...)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []Etapa{}
	for linhas.Next() {
		e, err := lerEtapa(linhas)
		if err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, e)
	}
	return out, db.MapError(linhas.Err())
}

// ListarEtapas devolve a página, ordenada por funil e posição.
func (r *Repository) ListarEtapas(ctx context.Context, propriedade uuid.UUID, funil *uuid.UUID,
	tipo string, pagina, porPagina int) ([]Etapa, int64, error) {

	const base = `FROM crm_stages s
	              JOIN crm_pipelines p ON p.id = s.pipeline_id AND p.property_id = $1
	             WHERE ($2::uuid IS NULL OR s.pipeline_id = $2)
	               AND ($3::text = '' OR s.type = $3)`

	var total int64
	if err := r.exec(ctx).QueryRow(ctx, `SELECT count(*) `+base, propriedade, funil, tipo).Scan(&total); err != nil {
		return nil, 0, db.MapError(err)
	}

	out, err := r.etapas(ctx, `SELECT `+colunasDaEtapa+` `+base+
		` ORDER BY s.pipeline_id, s.position LIMIT $4 OFFSET $5`,
		propriedade, funil, tipo, porPagina, (pagina-1)*porPagina)
	return out, total, err
}

// EtapasDoFunil devolve a trilha inteira, na ordem. É o que a tela `/full` e o
// kanban desenham — inclusive as etapas que a oportunidade ainda não alcançou.
func (r *Repository) EtapasDoFunil(ctx context.Context, funil uuid.UUID) ([]Etapa, error) {
	return r.etapas(ctx, `SELECT `+colunasDaEtapa+` FROM crm_stages s
	                       WHERE s.pipeline_id = $1 ORDER BY s.position`, funil)
}

// BuscarEtapa devolve uma etapa, confinada à propriedade.
func (r *Repository) BuscarEtapa(ctx context.Context, propriedade, id uuid.UUID) (Etapa, error) {
	const q = `SELECT ` + colunasDaEtapa + `
	             FROM crm_stages s
	             JOIN crm_pipelines p ON p.id = s.pipeline_id AND p.property_id = $1
	            WHERE s.id = $2`
	e, err := lerEtapa(r.exec(ctx).QueryRow(ctx, q, propriedade, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Etapa{}, apperr.NotFound("Etapa")
	}
	return e, db.MapError(err)
}

// PrimeiraEtapaDoFunil é a de menor `position`: onde cai a oportunidade criada
// sem `stage_id`.
func (r *Repository) PrimeiraEtapaDoFunil(ctx context.Context, funil uuid.UUID) (Etapa, error) {
	const q = `SELECT ` + colunasDaEtapa + ` FROM crm_stages s
	            WHERE s.pipeline_id = $1 ORDER BY s.position LIMIT 1`
	e, err := lerEtapa(r.exec(ctx).QueryRow(ctx, q, funil))
	if errors.Is(err, pgx.ErrNoRows) {
		return Etapa{}, apperr.Validation(map[string]string{
			"pipeline_id": "este funil não tem nenhuma etapa; crie ao menos uma em /crm/stages.",
		})
	}
	return e, db.MapError(err)
}

// EtapaTerminalDoFunil devolve a etapa de tipo `ganho` ou `perdido` do funil.
// É ela que `/win` e `/lose` procuram — e o funil que não tiver a sua recusa a
// ação com 422, em vez de fechar a oportunidade num lugar inventado.
func (r *Repository) EtapaTerminalDoFunil(ctx context.Context, funil uuid.UUID, tipo string) (Etapa, error) {
	const q = `SELECT ` + colunasDaEtapa + ` FROM crm_stages s
	            WHERE s.pipeline_id = $1 AND s.type = $2 ORDER BY s.position LIMIT 1`
	e, err := lerEtapa(r.exec(ctx).QueryRow(ctx, q, funil, tipo))
	if errors.Is(err, pgx.ErrNoRows) {
		return Etapa{}, apperr.Validation(map[string]string{
			"stage_id": fmt.Sprintf("o funil não tem etapa de tipo %q; configure-a em /crm/stages.", tipo),
		})
	}
	return e, db.MapError(err)
}

// ProximaPosicaoDoFunil devolve a posição de "no fim do funil".
func (r *Repository) ProximaPosicaoDoFunil(ctx context.Context, funil uuid.UUID) (int, error) {
	const q = `SELECT COALESCE(max(position) + 1, 0) FROM crm_stages WHERE pipeline_id = $1`
	var n int
	err := r.exec(ctx).QueryRow(ctx, q, funil).Scan(&n)
	return n, db.MapError(err)
}

// TerminalJaExiste diz se o funil já tem etapa daquele tipo terminal. Duas
// candidatas transformariam "ganhar" numa escolha ambígua do servidor.
func (r *Repository) TerminalJaExiste(ctx context.Context, funil uuid.UUID, tipo string, exceto uuid.UUID) (bool, error) {
	const q = `SELECT EXISTS (SELECT 1 FROM crm_stages
	                           WHERE pipeline_id = $1 AND type = $2 AND id <> $3)`
	var existe bool
	err := r.exec(ctx).QueryRow(ctx, q, funil, tipo, exceto).Scan(&existe)
	return existe, db.MapError(err)
}

// InserirEtapa grava a etapa nova.
func (r *Repository) InserirEtapa(ctx context.Context, e Etapa) (uuid.UUID, error) {
	const q = `
		INSERT INTO crm_stages (pipeline_id, name, position, probability, color, type,
		                        sla_days, auto_task_subject, auto_task_type, auto_task_due_days, auto_notify)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`

	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, e.FunilID, e.Nome, e.Posicao, e.Probabilidade, e.Cor, e.Tipo,
		e.SLADias, e.TarefaAssunto, e.TarefaTipo, e.TarefaPrazoDias, e.Notificar).Scan(&id)
	if db.IsUniqueViolation(err, "crm_stages_pipeline_id_name_key") {
		return uuid.Nil, NomeEmUso.WithMessage("Já existe uma etapa com esse nome neste funil.").WithCause(err)
	}
	return id, db.MapError(err)
}

// AtualizarEtapa grava a etapa inteira.
//
// Mudar `sla_days` ou a tarefa automática NÃO mexe em tarefa já criada: a
// tarefa que existe tem prazo próprio, e reescrevê-lo à distância moveria o
// compromisso de alguém sem aviso. O ajuste vale da próxima entrada na etapa em
// diante.
func (r *Repository) AtualizarEtapa(ctx context.Context, e Etapa) error {
	const q = `
		UPDATE crm_stages
		   SET name = $2, position = $3, probability = $4, color = $5, type = $6,
		       sla_days = $7, auto_task_subject = $8, auto_task_type = $9,
		       auto_task_due_days = $10, auto_notify = $11, updated_at = now()
		 WHERE id = $1`

	_, err := r.exec(ctx).Exec(ctx, q, e.ID, e.Nome, e.Posicao, e.Probabilidade, e.Cor, e.Tipo,
		e.SLADias, e.TarefaAssunto, e.TarefaTipo, e.TarefaPrazoDias, e.Notificar)
	if db.IsUniqueViolation(err, "crm_stages_pipeline_id_name_key") {
		return NomeEmUso.WithMessage("Já existe uma etapa com esse nome neste funil.").WithCause(err)
	}
	if db.IsUniqueViolation(err, "crm_stages_posicao_unica") {
		return NomeEmUso.WithMessage("Já existe uma etapa nessa posição; use POST /crm/stages/reorder.").WithCause(err)
	}
	return db.MapError(err)
}

// UsoDaEtapa conta o que impede a remoção física: oportunidades (abertas ou
// fechadas) e linhas de histórico. `crm_stage_history` é insert-only e é a
// matéria-prima da conversão por etapa — apagar a etapa deixaria o histórico
// apontando para o nada.
func (r *Repository) UsoDaEtapa(ctx context.Context, id uuid.UUID) (oportunidades, historico int, err error) {
	const q = `
		SELECT (SELECT count(*) FROM crm_opportunities WHERE stage_id = $1),
		       (SELECT count(*) FROM crm_stage_history WHERE to_stage_id = $1 OR from_stage_id = $1)`
	err = r.exec(ctx).QueryRow(ctx, q, id).Scan(&oportunidades, &historico)
	return oportunidades, historico, db.MapError(err)
}

// ExcluirEtapa remove a etapa e RECOMPACTA as posições do funil, na mesma
// instrução lógica. Sem a recompactação o funil fica com buraco e a próxima
// etapa criada "no fim" pode cair na casa de um buraco antigo.
func (r *Repository) ExcluirEtapa(ctx context.Context, funil, id uuid.UUID) error {
	if _, err := r.exec(ctx).Exec(ctx, `DELETE FROM crm_stages WHERE id = $1`, id); err != nil {
		return db.MapError(err)
	}
	return r.RecompactarPosicoes(ctx, funil)
}

// RecompactarPosicoes tira os buracos de `position` PRESERVANDO a ordem e a
// base do funil.
//
// Numa instrução só, e não num laço de UPDATE: a `UNIQUE (pipeline_id,
// position)` é DEFERRABLE INITIALLY DEFERRED, então a colisão intermediária é
// legítima e a unicidade é conferida no commit. É a mesma propriedade que faz o
// `/reorder` caber num comando.
//
// A BASE é `min(position)` do próprio funil, e não zero fixo: o funil do seed
// nasce em 1 (`cmd/seed/crm.go`), e renumerar para 0 a cada etapa criada faria
// o `make seed` seguinte reconciliar as oito linhas de volta e reportar
// "atualizadas" num banco em que ninguém tocou na configuração. Recompactar é
// tirar buraco, não escolher onde o funil começa.
func (r *Repository) RecompactarPosicoes(ctx context.Context, funil uuid.UUID) error {
	const q = `
		UPDATE crm_stages s
		   SET position = ordenadas.nova, updated_at = now()
		  FROM (SELECT id,
		               (row_number() OVER (ORDER BY position, name) - 1
		                + (SELECT min(position) FROM crm_stages WHERE pipeline_id = $1))::int AS nova
		          FROM crm_stages WHERE pipeline_id = $1) ordenadas
		 WHERE s.id = ordenadas.id AND s.position <> ordenadas.nova`
	_, err := r.exec(ctx).Exec(ctx, q, funil)
	return db.MapError(err)
}

// IDsDasEtapasDoFunil devolve o conjunto de etapas do funil. É o que
// `/reorder` compara com a lista recebida para responder
// `STAGE_ORDER_INCOMPLETE` com os ids que faltaram.
func (r *Repository) IDsDasEtapasDoFunil(ctx context.Context, funil uuid.UUID) ([]uuid.UUID, error) {
	linhas, err := r.exec(ctx).Query(ctx,
		`SELECT id FROM crm_stages WHERE pipeline_id = $1 ORDER BY position`, funil)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []uuid.UUID{}
	for linhas.Next() {
		var id uuid.UUID
		if err := linhas.Scan(&id); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, id)
	}
	return out, db.MapError(linhas.Err())
}

// Reordenar reescreve as posições do funil inteiro NUMA instrução.
//
// É aqui que a constraint adiável paga o que promete: arrastar a etapa 5 para a
// posição 2 reescreve quatro linhas, e com unicidade imediata a primeira já
// colidiria com a posição que a segunda ainda não liberou. O truque das
// posições negativas seria a alternativa — duas passadas de UPDATE, o dobro de
// escritas e um estado visivelmente errado no meio.
func (r *Repository) Reordenar(ctx context.Context, funil uuid.UUID, ordem []uuid.UUID) error {
	const q = `
		UPDATE crm_stages s
		   SET position = nova.posicao, updated_at = now()
		  FROM (SELECT id, (ordinalidade - 1)::int AS posicao
		          FROM unnest($2::uuid[]) WITH ORDINALITY AS t(id, ordinalidade)) nova
		 WHERE s.id = nova.id AND s.pipeline_id = $1`
	_, err := r.exec(ctx).Exec(ctx, q, funil, ordem)
	return db.MapError(err)
}

// ═══════════════════════════ Motivos de perda ═══════════════════════

// colunasDoMotivo traz `usage_count` derivado: quantas oportunidades perdidas
// apontam para ele. É o número que justifica manter (ou aposentar) o motivo.
const colunasDoMotivo = `
	    m.id, m.label, m.active,
	    (SELECT count(*) FROM crm_opportunities o WHERE o.lost_reason_id = m.id)`

func lerMotivo(linha pgx.Row) (MotivoDePerda, error) {
	var m MotivoDePerda
	err := linha.Scan(&m.ID, &m.Rotulo, &m.Ativo, &m.Usos)
	return m, err
}

// ListarMotivos devolve a página e o total.
func (r *Repository) ListarMotivos(ctx context.Context, propriedade uuid.UUID, ativo *bool,
	pagina, porPagina int) ([]MotivoDePerda, int64, error) {

	const base = `FROM crm_lost_reasons m
	              WHERE m.property_id = $1 AND ($2::boolean IS NULL OR m.active = $2)`

	var total int64
	if err := r.exec(ctx).QueryRow(ctx, `SELECT count(*) `+base, propriedade, ativo).Scan(&total); err != nil {
		return nil, 0, db.MapError(err)
	}

	linhas, err := r.exec(ctx).Query(ctx, `SELECT `+colunasDoMotivo+` `+base+
		` ORDER BY m.sort_order, m.label LIMIT $3 OFFSET $4`,
		propriedade, ativo, porPagina, (pagina-1)*porPagina)
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	out := []MotivoDePerda{}
	for linhas.Next() {
		m, err := lerMotivo(linhas)
		if err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, m)
	}
	return out, total, db.MapError(linhas.Err())
}

// BuscarMotivo devolve um motivo pelo id.
func (r *Repository) BuscarMotivo(ctx context.Context, propriedade, id uuid.UUID) (MotivoDePerda, error) {
	const q = `SELECT ` + colunasDoMotivo + ` FROM crm_lost_reasons m
	            WHERE m.property_id = $1 AND m.id = $2`
	m, err := lerMotivo(r.exec(ctx).QueryRow(ctx, q, propriedade, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return MotivoDePerda{}, apperr.NotFound("Motivo de perda")
	}
	return m, db.MapError(err)
}

// MotivoAtivo confere que o motivo existe, é desta propriedade e está ATIVO. É
// a guarda do `/lose`: motivo inativo some do seletor, e aceitar um id inativo
// que veio de uma tela antiga reabriria o catálogo aposentado pela porta dos
// fundos.
func (r *Repository) MotivoAtivo(ctx context.Context, propriedade, id uuid.UUID) error {
	const q = `SELECT active FROM crm_lost_reasons WHERE id = $1 AND property_id = $2`
	var ativo bool
	err := r.exec(ctx).QueryRow(ctx, q, id, propriedade).Scan(&ativo)
	if errors.Is(err, pgx.ErrNoRows) {
		return MotivoDePerdaObrigatorio.WithDetails(map[string]any{
			"lost_reason_id": "motivo desconhecido nesta propriedade.",
		})
	}
	if err != nil {
		return db.MapError(err)
	}
	if !ativo {
		return MotivoDePerdaObrigatorio.WithDetails(map[string]any{
			"lost_reason_id": "motivo inativo; escolha um do catálogo ativo.",
		})
	}
	return nil
}

// InserirMotivo grava o motivo novo.
func (r *Repository) InserirMotivo(ctx context.Context, propriedade uuid.UUID, rotulo string, ativo bool) (uuid.UUID, error) {
	const q = `INSERT INTO crm_lost_reasons (property_id, label, active,
	                  sort_order)
	           VALUES ($1, $2, $3, COALESCE((SELECT max(sort_order) + 1 FROM crm_lost_reasons WHERE property_id = $1), 0))
	           RETURNING id`
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, propriedade, rotulo, ativo).Scan(&id)
	if db.IsUniqueViolation(err, "crm_lost_reasons_property_id_label_key") {
		return uuid.Nil, NomeEmUso.WithMessage("Já existe um motivo com esse rótulo.").WithCause(err)
	}
	return id, db.MapError(err)
}

// AtualizarMotivo grava rótulo e ativo.
func (r *Repository) AtualizarMotivo(ctx context.Context, id uuid.UUID, rotulo string, ativo bool) error {
	const q = `UPDATE crm_lost_reasons SET label = $2, active = $3, updated_at = now() WHERE id = $1`
	_, err := r.exec(ctx).Exec(ctx, q, id, rotulo, ativo)
	if db.IsUniqueViolation(err, "crm_lost_reasons_property_id_label_key") {
		return NomeEmUso.WithMessage("Já existe um motivo com esse rótulo.").WithCause(err)
	}
	return db.MapError(err)
}
