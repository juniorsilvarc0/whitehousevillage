package users

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// ColunasDeOrdenacao é a whitelist do `?sort=`. Nada fora daqui entra no ORDER
// BY — o valor do cliente é só a CHAVE do mapa, nunca o texto concatenado.
var ColunasDeOrdenacao = map[string]string{
	"name":          "u.name",
	"email":         "u.email",
	"created_at":    "u.created_at",
	"last_login_at": "u.last_login_at",
}

// OrdenacaoPadrao é a ordem quando o cliente não pede nada.
const OrdenacaoPadrao = "u.name ASC"

// desempate fecha TODA ordenação pelo id. Sem critério determinístico, dois
// usuários de mesmo nome podem trocar de posição entre uma página e outra — e
// um deles some da listagem sem nunca ter sido mostrado.
const desempate = ", u.id ASC"

type Repository struct {
	pool db.DBTX
}

func NewRepository(pool db.DBTX) *Repository { return &Repository{pool: pool} }

func (r *Repository) exec(ctx context.Context) db.DBTX { return db.From(ctx, r.pool) }

const camposUsuario = `
	u.id, u.property_id, u.role_id, p.code, p.name,
	u.name, u.email, u.phone, u.broker_id, u.active, u.last_login_at, u.created_at`

// Listar devolve a página e o total na MESMA consulta (count(*) OVER()): duas
// consultas separadas podem discordar sob concorrência e fazer o total mentir.
func (r *Repository) Listar(ctx context.Context, f Filtro) ([]auth.LinhaUsuario, int64, error) {
	ordem := f.OrderBy
	if ordem == "" {
		ordem = OrdenacaoPadrao
	}

	q := `
		SELECT ` + camposUsuario + `, count(*) OVER() AS total
		  FROM users u
		  JOIN roles p ON p.id = u.role_id
		 WHERE u.deleted_at IS NULL
		   AND ($1::text IS NULL OR u.name ILIKE '%' || $1 || '%' OR u.email ILIKE '%' || $1 || '%')
		   AND ($2::text IS NULL OR p.code = $2)
		   AND ($3::boolean IS NULL OR u.active = $3)
		   AND ($4::uuid IS NULL OR u.id = $4)
		 ORDER BY ` + ordem + desempate + `
		 LIMIT $5 OFFSET $6`

	linhas, err := r.exec(ctx).Query(ctx, q,
		nuloSeVazio(f.Busca), nuloSeVazio(f.RoleCode), f.Ativo, f.ApenasID,
		f.PorPagina, httpx.Offset(f.Pagina, f.PorPagina))
	if err != nil {
		return nil, 0, db.MapError(err)
	}
	defer linhas.Close()

	var (
		out   []auth.LinhaUsuario
		total int64
	)
	for linhas.Next() {
		var l auth.LinhaUsuario
		if err := linhas.Scan(
			&l.ID, &l.PropertyID, &l.RoleID, &l.RoleCode, &l.RoleName,
			&l.Nome, &l.Email, &l.Telefone, &l.BrokerID, &l.Ativo, &l.UltimoLogin, &l.CriadoEm, &total); err != nil {
			return nil, 0, db.MapError(err)
		}
		out = append(out, l)
	}
	if err := linhas.Err(); err != nil {
		return nil, 0, db.MapError(err)
	}
	return out, total, nil
}

func (r *Repository) Buscar(ctx context.Context, id uuid.UUID) (auth.LinhaUsuario, error) {
	q := `SELECT ` + camposUsuario + `
		    FROM users u JOIN roles p ON p.id = u.role_id
		   WHERE u.id = $1 AND u.deleted_at IS NULL`

	var l auth.LinhaUsuario
	err := r.exec(ctx).QueryRow(ctx, q, id).Scan(
		&l.ID, &l.PropertyID, &l.RoleID, &l.RoleCode, &l.RoleName,
		&l.Nome, &l.Email, &l.Telefone, &l.BrokerID, &l.Ativo, &l.UltimoLogin, &l.CriadoEm)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, apperr.NotFound("Usuário")
	}
	if err != nil {
		return l, db.MapError(err)
	}
	return l, nil
}

// PropriedadePadrao devolve a propriedade à qual o novo usuário será vinculado.
// O contrato de UsuarioCriar não pede property_id — enquanto houver uma única
// propriedade, o cadastro herda a do criador (resolvido no service) ou a única
// existente.
func (r *Repository) PropriedadePadrao(ctx context.Context) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx,
		`SELECT id FROM properties WHERE active ORDER BY created_at LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apperr.Validation(map[string]string{
			"property_id": "nenhuma propriedade cadastrada; rode o seed antes de criar usuários.",
		})
	}
	if err != nil {
		return uuid.Nil, db.MapError(err)
	}
	return id, nil
}

// Criar insere e deixa a violação de unicidade decidir sobre e-mail repetido.
// Um SELECT antes do INSERT perderia a corrida entre duas requisições
// simultâneas com o mesmo e-mail.
func (r *Repository) Criar(ctx context.Context, propriedadeID uuid.UUID, c Criar, senhaHash string) (uuid.UUID, error) {
	const q = `
		INSERT INTO users (property_id, role_id, name, email, password_hash, phone, active)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`

	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q,
		propriedadeID, c.RoleID, c.Nome, c.Email, senhaHash, c.Telefone, c.AtivoOuPadrao()).Scan(&id)
	if err != nil {
		return uuid.Nil, r.traduzir(err)
	}
	return id, nil
}

// Atualizar monta o SET só com o que veio. Os nomes de coluna são literais deste
// arquivo; do cliente vem apenas o VALOR, sempre por placeholder.
func (r *Repository) Atualizar(ctx context.Context, id uuid.UUID, a Atualizar, senhaHash *string) error {
	sets := []string{"updated_at = now()"}
	args := []any{id}

	adicionar := func(coluna string, valor any) {
		args = append(args, valor)
		sets = append(sets, fmt.Sprintf("%s = $%d", coluna, len(args)))
	}

	if v, ok := a.Nome.Definido(); ok {
		adicionar("name", v)
	}
	if v, ok := a.Email.Definido(); ok {
		adicionar("email", v)
	}
	if v, ok := a.RoleID.Definido(); ok {
		adicionar("role_id", v)
	}
	if v, ok := a.Ativo.Definido(); ok {
		adicionar("active", v)
	}
	if senhaHash != nil {
		adicionar("password_hash", *senhaHash)
	}
	switch {
	case a.Telefone.DeveLimpar():
		adicionar("phone", nil)
	default:
		if v, ok := a.Telefone.Definido(); ok {
			adicionar("phone", v)
		}
	}

	if len(sets) == 1 { // só updated_at: nada a fazer
		return nil
	}

	q := `UPDATE users SET ` + strings.Join(sets, ", ") + ` WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.exec(ctx).Exec(ctx, q, args...)
	if err != nil {
		return r.traduzir(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Usuário")
	}
	return nil
}

// Desativar é o soft delete do contrato: `active = false`, sem carimbar
// deleted_at.
//
// deleted_at existe e é filtrado em toda consulta, mas fica reservado para a
// remoção definitiva (LGPD). O DELETE do contrato tem "efeito idêntico a
// PATCH {active:false}" e a reativação é PATCH {active:true} — carimbar
// deleted_at aqui tornaria a reativação impossível pela própria API.
func (r *Repository) Desativar(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE users SET active = false, updated_at = now()
	            WHERE id = $1 AND deleted_at IS NULL`

	tag, err := r.exec(ctx).Exec(ctx, q, id)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Usuário")
	}
	return nil
}

// ContarAdministradoresAtivos conta quantos usuários ativos ainda poderiam
// administrar a instalação, ignorando o usuário informado.
//
// "Administrador" aqui é definido por DADO, não por `code = 'admin'`: é quem
// tem permissão de editar perfis, ou seja, quem consegue consertar o acesso de
// todo mundo. No seed isso dá exatamente o perfil admin, mas continua correto se
// amanhã existir um perfil "suporte" com o mesmo poder.
func (r *Repository) ContarAdministradoresAtivos(ctx context.Context, exceto uuid.UUID) (int, error) {
	const q = `
		SELECT count(*)
		  FROM users u
		  JOIN role_permissions rp ON rp.role_id = u.role_id
		 WHERE u.active
		   AND u.deleted_at IS NULL
		   AND u.id <> $1
		   AND rp.resource_code = $2
		   AND rp.action = $3`

	var n int
	if err := r.exec(ctx).QueryRow(ctx, q, exceto, auth.RecursoPerfis, auth.AcaoEditar).Scan(&n); err != nil {
		return 0, db.MapError(err)
	}
	return n, nil
}

// ChaveDaTravaDeAdministradores é a chave fixa do advisory lock que serializa
// TODA decisão sobre a população de administradores.
//
// É um número arbitrário e ESTÁVEL: o valor não significa nada, o que importa é
// que toda transação que decide sobre administrador use o MESMO. Mudá-lo é
// desligar a trava sem que nada quebre visivelmente, então ele fica aqui,
// sozinho, com este comentário em cima.
//
// É exportada porque `modules/roles` precisa da MESMA chave: tirar `roles:editar`
// da matriz de um perfil rebaixa todos os usuários dele de uma vez, e essa
// transação corre contra a desativação de usuário daqui. Duas cópias do número
// seriam duas travas que não se enxergam — ou seja, trava nenhuma.
const ChaveDaTravaDeAdministradores int64 = 2026_0820_0001

// TravarAdministradores serializa as transações que decidem sobre a população
// de administradores. Devolve só depois que nenhuma outra transação estiver
// entre "contar administradores" e "gravar".
//
// A trava é de TRANSAÇÃO (`_xact_`): solta sozinha no commit e no rollback.
// A variante de sessão exigiria um unlock explícito e vazaria a trava para a
// próxima requisição que pegasse a mesma conexão do pool — um deadlock que só
// aparece em produção sob carga.
func (r *Repository) TravarAdministradores(ctx context.Context) error {
	// Fora de transação o lock seria solto no fim da consulta e a trava não
	// protegeria nada. Falhar alto aqui é melhor do que uma trava decorativa:
	// um refactor futuro que tire o tx.Do de volta quebra o teste, não a
	// instalação do cliente.
	if !db.EmTransacao(ctx) {
		return apperr.Internal.WithCause(errors.New(
			"TravarAdministradores chamado fora de transação: a trava não valeria nada"))
	}

	if _, err := r.exec(ctx).Exec(ctx,
		`SELECT pg_advisory_xact_lock($1)`, ChaveDaTravaDeAdministradores); err != nil {
		return db.MapError(err)
	}
	return nil
}

// RegistrarAuditoria grava uma linha de audit_log.
//
// `before`/`after` viajam como JSON já serializado: a coluna é jsonb e o mapa
// vira o documento inteiro, sem coluna nova a cada campo que a auditoria passe
// a registrar.
func (r *Repository) RegistrarAuditoria(ctx context.Context, a Auditoria) error {
	antes, err := json.Marshal(a.Antes)
	if err != nil {
		return apperr.Internal.WithCause(err)
	}
	depois, err := json.Marshal(a.Depois)
	if err != nil {
		return apperr.Internal.WithCause(err)
	}

	const q = `
		INSERT INTO audit_log (property_id, actor_id, action, entity, entity_id, before, after, request_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	if _, err := r.exec(ctx).Exec(ctx, q,
		nuloSeUUIDVazio(a.PropriedadeID), a.AtorID, a.Acao, a.Entidade, a.EntidadeID,
		antes, depois, nuloSeVazio(a.RequestID)); err != nil {
		return db.MapError(err)
	}
	return nil
}

// PermissoesDoPapel devolve a matriz de um perfil.
//
// O service usa isto para o TETO DE PRIVILÉGIO: quem atribui um papel só pode
// atribuir o que ele próprio já tem. Lê de role_permissions, e não de um mapa em
// Go, porque a matriz é dado (regra 8) e muda em tempo de execução pela tela de
// perfis — comparar contra uma cópia compilada autorizaria hoje o que o
// administrador revogou ontem.
func (r *Repository) PermissoesDoPapel(ctx context.Context, roleID uuid.UUID) ([]auth.Permissao, error) {
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

// traduzir transforma a violação de unicidade do e-mail no código que o contrato
// exige. As duas constraints existem: a UNIQUE da coluna e o índice parcial
// sobre lower(email).
func (r *Repository) traduzir(err error) error {
	if db.IsUniqueViolation(err, "users_email_key", "users_email_active_idx") {
		return apperr.EmailInUse.WithCause(err)
	}
	if db.IsForeignKeyViolation(err, "users_role_id_fkey") {
		return apperr.Validation(map[string]string{"role_id": "perfil inexistente."}).WithCause(err)
	}
	if db.IsForeignKeyViolation(err, "users_property_id_fkey") {
		return apperr.Validation(map[string]string{"property_id": "propriedade inexistente."}).WithCause(err)
	}
	return db.MapError(err)
}

// nuloSeUUIDVazio protege a FK de audit_log.property_id: a coluna é anulável, e
// um uuid zerado (script sem propriedade resolvida) violaria a chave estrangeira
// em vez de gravar "não sei de qual propriedade".
func nuloSeUUIDVazio(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func nuloSeVazio(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}
