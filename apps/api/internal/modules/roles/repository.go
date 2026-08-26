package roles

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/users"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

type Repository struct {
	pool db.DBTX
}

func NewRepository(pool db.DBTX) *Repository { return &Repository{pool: pool} }

func (r *Repository) exec(ctx context.Context) db.DBTX { return db.From(ctx, r.pool) }

// Listar devolve a página de perfis, com o total na mesma consulta.
func (r *Repository) Listar(ctx context.Context, busca string, pagina, porPagina int) ([]Perfil, int64, error) {
	const q = `
		SELECT id, code, name, is_system, count(*) OVER() AS total
		  FROM roles
		 WHERE ($1::text IS NULL OR code ILIKE '%' || $1 || '%' OR name ILIKE '%' || $1 || '%')
		 ORDER BY is_system DESC, name ASC, id ASC
		 LIMIT $2 OFFSET $3`

	linhas, err := r.exec(ctx).Query(ctx, q, nuloSeVazio(busca), porPagina, httpx.Offset(pagina, porPagina))
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	var (
		out   []Perfil
		total int64
	)
	for linhas.Next() {
		var p Perfil
		if err := linhas.Scan(&p.ID, &p.Codigo, &p.Nome, &p.IsSystem, &total); err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, p)
	}
	if err := linhas.Err(); err != nil {
		return nil, 0, db.MapError(err)
	}
	return out, total, nil
}

func (r *Repository) Buscar(ctx context.Context, id uuid.UUID) (Perfil, error) {
	var p Perfil
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT id, code, name, is_system FROM roles WHERE id = $1`, id).
		Scan(&p.ID, &p.Codigo, &p.Nome, &p.IsSystem)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, apperr.NotFound("Perfil")
	}
	if err != nil {
		return p, db.MapError(err)
	}
	return p, nil
}

// Permissoes devolve a matriz do perfil, ordenada para a tela não precisar
// ordenar.
func (r *Repository) Permissoes(ctx context.Context, roleID uuid.UUID) ([]auth.Permissao, error) {
	const q = `
		SELECT resource_code, action, scope
		  FROM role_permissions
		 WHERE role_id = $1
		 ORDER BY resource_code, action`

	linhas, err := r.exec(ctx).Query(ctx, q, roleID)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []auth.Permissao{}
	for linhas.Next() {
		var p auth.Permissao
		if err := linhas.Scan(&p.Resource, &p.Action, &p.Scope); err != nil {
			return nil, db.MapError(err)
		}
		out = append(out, p)
	}
	return out, db.MapError(linhas.Err())
}

func (r *Repository) Criar(ctx context.Context, c Criar) (uuid.UUID, error) {
	const q = `INSERT INTO roles (code, name, is_system) VALUES ($1, $2, false) RETURNING id`

	var id uuid.UUID
	if err := r.exec(ctx).QueryRow(ctx, q, c.Codigo, c.Nome).Scan(&id); err != nil {
		return uuid.Nil, r.traduzir(err)
	}
	return id, nil
}

// Atualizar troca código e/ou nome. Nomes de coluna são literais deste arquivo.
func (r *Repository) Atualizar(ctx context.Context, id uuid.UUID, a Atualizar) error {
	sets := []string{}
	args := []any{id}

	if v, ok := a.Codigo.Definido(); ok {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("code = $%d", len(args)))
	}
	if v, ok := a.Nome.Definido(); ok {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("name = $%d", len(args)))
	}
	if len(sets) == 0 {
		return nil
	}

	q := `UPDATE roles SET ` + strings.Join(sets, ", ") + ` WHERE id = $1`
	tag, err := r.exec(ctx).Exec(ctx, q, args...)
	if err != nil {
		return r.traduzir(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Perfil")
	}
	return nil
}

func (r *Repository) Excluir(ctx context.Context, id uuid.UUID) error {
	tag, err := r.exec(ctx).Exec(ctx, `DELETE FROM roles WHERE id = $1`, id)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Perfil")
	}
	return nil
}

// ContarUsuarios alimenta o details.users_count do 409 ROLE_IN_USE.
func (r *Repository) ContarUsuarios(ctx context.Context, roleID uuid.UUID) (int, error) {
	const q = `SELECT count(*) FROM users WHERE role_id = $1 AND deleted_at IS NULL`

	var n int
	if err := r.exec(ctx).QueryRow(ctx, q, roleID).Scan(&n); err != nil {
		return 0, db.MapError(err)
	}
	return n, nil
}

// camposDoCatalogo é o catálogo INTEIRO, e o catálogo é dado (regra 8): quais
// ações o recurso oferece e se ele tem dono saem das colunas que a migration
// 20260820140000 criou, nunca de uma lista em Go. Uma cópia compilada diverge
// do seed no primeiro recurso novo — e foi o que aconteceu: a lista antiga não
// conhecia `chat`, `agenda`, `quotes` nem `calendar`, e o perfil Corretor ficou
// impossível de salvar.
const camposDoCatalogo = `code, label, group_label, actions, supports_own`

// ordemDoCatalogo é a ordem em que a grade da tela desenha as linhas.
// `sort_order` vem do seed e agrupa por família; rótulo e grupo entram só como
// desempate, para a lista não trocar de ordem entre duas chamadas.
const ordemDoCatalogo = ` ORDER BY sort_order, group_label, label`

// Catalogo lê a tabela `resources`, que é a fonte da grade de permissões.
func (r *Repository) Catalogo(ctx context.Context) ([]Recurso, error) {
	const q = `SELECT ` + camposDoCatalogo + ` FROM resources` + ordemDoCatalogo

	linhas, err := r.exec(ctx).Query(ctx, q)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := []Recurso{}
	for linhas.Next() {
		var (
			rec        Recurso
			suportaOwn bool
		)
		if err := linhas.Scan(&rec.Codigo, &rec.Label, &rec.Grupo, &rec.Acoes, &suportaOwn); err != nil {
			return nil, db.MapError(err)
		}
		rec.Escopos = escoposDoRecurso(suportaOwn)
		out = append(out, rec)
	}
	return out, db.MapError(linhas.Err())
}

// MetadadosDoCatalogo devolve o catálogo indexado por código, para o service
// recusar ação e escopo que o recurso não oferece ANTES de gravar.
//
// Substitui o antigo CodigosDoCatalogo, que só sabia dizer se o recurso existia:
// com ele, a grade oferecia "excluir" no razão financeiro (append-only, spec
// §10) e no chat (mensagem não se apaga, spec §8), e o PUT aceitava e gravava.
func (r *Repository) MetadadosDoCatalogo(ctx context.Context) (map[string]MetaRecurso, error) {
	const q = `SELECT code, actions, supports_own FROM resources`

	linhas, err := r.exec(ctx).Query(ctx, q)
	if err != nil {
		return nil, db.MapError(err)
	}
	defer linhas.Close()

	out := map[string]MetaRecurso{}
	for linhas.Next() {
		var (
			codigo string
			meta   MetaRecurso
		)
		if err := linhas.Scan(&codigo, &meta.Acoes, &meta.SuportaOwn); err != nil {
			return nil, db.MapError(err)
		}
		out[codigo] = meta
	}
	return out, db.MapError(linhas.Err())
}

// escoposDoRecurso traduz `supports_own` para o que a tela consome. Onde não há
// dono identificável na linha, a grade desabilita "só os meus" em vez de
// oferecer um escopo que o SQL não sabe aplicar.
func escoposDoRecurso(suportaOwn bool) []string {
	if suportaOwn {
		return []string{auth.EscopoAll, auth.EscopoOwn}
	}
	return []string{auth.EscopoAll}
}

// TravarAdministradores pega o MESMO advisory lock que `modules/users` usa para
// a trava do "último administrador" — a chave é a de lá, importada, e não uma
// cópia do número (ver users.ChaveDaTravaDeAdministradores).
//
// Por que este módulo precisa dela: tirar `roles:editar` da matriz de um perfil
// rebaixa TODOS os usuários daquele perfil de uma vez. Sem a trava compartilhada,
// esta transação e a desativação de usuário decidem cada uma olhando um mundo em
// que ainda sobra administrador — e a instalação acaba sem nenhum. Reproduzido
// ao vivo: `PUT /roles/{B}/permissions` sem `roles:editar` em paralelo com
// `DELETE /users/{X}` zerava a contagem em 4 de 20 execuções.
func (r *Repository) TravarAdministradores(ctx context.Context) error {
	// Fora de transação o lock seria solto no fim da consulta e a trava não
	// protegeria nada. Falhar alto é melhor que uma trava decorativa.
	if !db.EmTransacao(ctx) {
		return apperr.Internal.WithCause(errors.New(
			"TravarAdministradores chamado fora de transação: a trava não valeria nada"))
	}

	if _, err := r.exec(ctx).Exec(ctx,
		`SELECT pg_advisory_xact_lock($1)`, users.ChaveDaTravaDeAdministradores); err != nil {
		return db.MapError(err)
	}
	return nil
}

// ContarAdministradoresAtivosForaDoPerfil conta quem continuaria capaz de
// administrar a instalação se este perfil deixasse de conceder acesso.
//
// `DISTINCT` porque o JOIN com role_permissions repete o usuário por célula.
func (r *Repository) ContarAdministradoresAtivosForaDoPerfil(ctx context.Context, roleID uuid.UUID) (int, error) {
	const q = `
		SELECT count(DISTINCT u.id)
		  FROM users u
		  JOIN role_permissions rp ON rp.role_id = u.role_id
		 WHERE u.active
		   AND u.deleted_at IS NULL
		   AND u.role_id <> $1
		   AND rp.resource_code = $2
		   AND rp.action = $3`

	var n int
	if err := r.exec(ctx).QueryRow(ctx, q, roleID, auth.RecursoPerfis, auth.AcaoEditar).Scan(&n); err != nil {
		return 0, db.MapError(err)
	}
	return n, nil
}

// SubstituirPermissoes apaga e regrava a matriz.
//
// DELETE + INSERT (em vez de diferença) porque a tela manda o estado inteiro da
// grade: calcular diferença no cliente é onde nasce a permissão fantasma que
// ninguém marcou. Roda dentro da transação aberta pelo service — se o INSERT
// falhar, o perfil não fica sem nenhuma permissão.
func (r *Repository) SubstituirPermissoes(ctx context.Context, roleID uuid.UUID, m Matriz) error {
	exec := r.exec(ctx)

	if _, err := exec.Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, roleID); err != nil {
		return db.MapError(err)
	}
	if len(m) == 0 {
		return nil
	}

	// Um único INSERT com VALUES múltiplos: N idas ao banco para uma grade de
	// 40 células custaria mais que a transação inteira.
	valores := make([]string, 0, len(m))
	args := make([]any, 0, len(m)*4)
	for i, p := range m {
		base := i * 4
		valores = append(valores, fmt.Sprintf("($%d, $%d, $%d, $%d)", base+1, base+2, base+3, base+4))
		args = append(args, roleID, p.Resource, p.Action, p.Scope)
	}

	q := `INSERT INTO role_permissions (role_id, resource_code, action, scope) VALUES ` +
		strings.Join(valores, ", ")

	if _, err := exec.Exec(ctx, q, args...); err != nil {
		return r.traduzirPermissao(err)
	}
	return nil
}

// traduzir mapeia a colisão de `code`. O contrato NÃO criou código próprio para
// isso: é 422 VALIDATION_ERROR com o campo em details, e não 409.
func (r *Repository) traduzir(err error) error {
	if db.IsUniqueViolation(err, "roles_code_key") {
		return apperr.Validation(map[string]string{"code": "já está em uso por outro perfil."}).WithCause(err)
	}
	return db.MapError(err)
}

func (r *Repository) traduzirPermissao(err error) error {
	if db.IsForeignKeyViolation(err, "role_permissions_resource_code_fkey") {
		return apperr.Validation(map[string]string{
			"permissions": "recurso fora do catálogo de /roles/resources.",
		}).WithCause(err)
	}
	if db.IsUniqueViolation(err, "role_permissions_pkey") {
		return apperr.Validation(map[string]string{
			"permissions": "par (resource, action) repetido.",
		}).WithCause(err)
	}
	return db.MapError(err)
}

func nuloSeVazio(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}
