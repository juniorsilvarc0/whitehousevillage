package auth

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
)

// Repository fala com o banco. Não abre transação: pega o executor do contexto
// com db.From, que devolve a transação em curso quando o service abriu uma.
type Repository struct {
	pool db.DBTX
}

func NewRepository(pool db.DBTX) *Repository { return &Repository{pool: pool} }

func (r *Repository) exec(ctx context.Context) db.DBTX { return db.From(ctx, r.pool) }

// Credenciais é o que o login precisa e nada mais.
type Credenciais struct {
	ID           uuid.UUID
	PropertyID   uuid.UUID
	RoleID       uuid.UUID
	RoleCode     string
	Nome         string
	Email        string
	PasswordHash string
	Ativo        bool
}

// LinhaUsuario espelha a resposta do contrato para o schema Usuario.
type LinhaUsuario struct {
	ID          uuid.UUID
	PropertyID  uuid.UUID
	RoleID      uuid.UUID
	RoleCode    string
	RoleName    string
	Nome        string
	Email       string
	Telefone    *string
	BrokerID    *uuid.UUID
	Ativo       bool
	UltimoLogin *time.Time
	CriadoEm    time.Time
}

// SessaoToken é a linha de refresh_tokens que interessa à rotação.
type SessaoToken struct {
	ID         uuid.UUID
	UsuarioID  uuid.UUID
	FamiliaID  uuid.UUID
	ExpiraEm   time.Time
	RevogadoEm *time.Time
	SubstPor   *uuid.UUID
}

// Rotacionado diz que este token já gerou sucessor. É a ÚNICA assinatura de
// sessão comprometida: o token só some do cliente ao ser trocado, então vê-lo de
// novo significa que existe uma segunda cópia dele em algum lugar.
func (s SessaoToken) Rotacionado() bool { return s.SubstPor != nil }

// Revogado cobre o token derrubado SEM sucessor — logout, troca de senha, conta
// desativada. Isso é TOKEN_INVALID, não TOKEN_REUSED: juntar os dois casos fazia
// um logout numa aba acusar "sessão comprometida" na outra, e ainda mandava
// revogar de novo uma família que já estava inteira revogada.
func (s SessaoToken) Revogado() bool { return s.RevogadoEm != nil }

func (s SessaoToken) Expirado(agora time.Time) bool { return agora.After(s.ExpiraEm) }

// TokenDeReset é a linha de password_resets.
type TokenDeReset struct {
	ID        uuid.UUID
	UsuarioID uuid.UUID
	ExpiraEm  time.Time
	UsadoEm   *time.Time
}

// camposUsuario é a projeção do schema Usuario do contrato. `u.broker_id` está
// aqui porque a coluna existe desde a migration 20260820140000: sem ela no
// SELECT, o painel do corretor não tem como ligar a conta ao cadastro.
const camposUsuario = `
	u.id, u.property_id, u.role_id, p.code, p.name,
	u.name, u.email, u.phone, u.broker_id, u.active, u.last_login_at, u.created_at`

// BuscarCredenciais procura por e-mail sem diferenciar caixa. O login trata
// "não encontrado" e "senha errada" com a mesma resposta, então este método
// devolve pgx.ErrNoRows e quem chama decide.
func (r *Repository) BuscarCredenciais(ctx context.Context, email string) (Credenciais, error) {
	const q = `
		SELECT u.id, u.property_id, u.role_id, p.code, u.name, u.email, u.password_hash, u.active
		  FROM users u
		  JOIN roles p ON p.id = u.role_id
		 WHERE lower(u.email) = lower($1)
		   AND u.deleted_at IS NULL`

	var c Credenciais
	err := r.exec(ctx).QueryRow(ctx, q, email).Scan(
		&c.ID, &c.PropertyID, &c.RoleID, &c.RoleCode, &c.Nome, &c.Email, &c.PasswordHash, &c.Ativo)
	if err != nil {
		return c, err
	}
	return c, nil
}

// CarregarSessao monta a identidade da requisição em UMA consulta: usuário,
// perfil e a matriz inteira de permissões. Duas consultas por requisição
// autenticada seriam duas idas ao banco em todo endpoint do sistema.
func (r *Repository) CarregarSessao(ctx context.Context, usuarioID uuid.UUID) (*Usuario, error) {
	const q = `
		SELECT u.id, u.property_id, u.role_id, p.code, p.name, p.is_system, u.name, u.email,
		       COALESCE(
		         (SELECT json_agg(json_build_object(
		                    'resource', rp.resource_code,
		                    'action',   rp.action,
		                    'scope',    rp.scope))
		            FROM role_permissions rp
		           WHERE rp.role_id = u.role_id),
		         '[]'::json) AS permissoes
		  FROM users u
		  JOIN roles p ON p.id = u.role_id
		 WHERE u.id = $1
		   AND u.active
		   AND u.deleted_at IS NULL`

	var (
		u    Usuario
		bruo []byte
	)
	err := r.exec(ctx).QueryRow(ctx, q, usuarioID).Scan(
		&u.ID, &u.PropertyID, &u.RoleID, &u.RoleCode, &u.RoleName, &u.RoleIsSystem,
		&u.Nome, &u.Email, &bruo)
	if errors.Is(err, pgx.ErrNoRows) {
		// Conta desativada ou removida entre a emissão do token e agora: o
		// access token continua assinado, mas a sessão morre na hora.
		return nil, apperr.Unauthorized
	}
	if err != nil {
		return nil, db.MapError(err)
	}

	var perms []Permissao
	if err := json.Unmarshal(bruo, &perms); err != nil {
		return nil, apperr.Internal.WithCause(err)
	}
	u.Permissoes = NovoConjunto(perms)
	return &u, nil
}

// BuscarUsuario devolve o usuário no formato do contrato.
func (r *Repository) BuscarUsuario(ctx context.Context, id uuid.UUID) (LinhaUsuario, error) {
	q := `SELECT ` + camposUsuario + `
		  FROM users u JOIN roles p ON p.id = u.role_id
		 WHERE u.id = $1 AND u.deleted_at IS NULL`

	var l LinhaUsuario
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

// RegistrarLogin carimba o último acesso.
//
// Zero linha afetada aqui não é detalhe cosmético: significa que a conta sumiu
// entre a conferência da senha e o fim da transação de login. Ignorar isso
// entregaria uma sessão válida para um usuário que não existe mais.
func (r *Repository) RegistrarLogin(ctx context.Context, id uuid.UUID) error {
	tag, err := r.exec(ctx).Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, id)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.InvalidCredentials
	}
	return nil
}

// ─────────────────────────── Refresh tokens ─────────────────────────────────

// CriarRefresh grava o hash do token. familiaID nulo abre família nova (login);
// preenchido continua a família (rotação).
func (r *Repository) CriarRefresh(ctx context.Context, usuarioID, familiaID uuid.UUID, hash string, expiraEm time.Time, userAgent, ip string) (uuid.UUID, error) {
	const q = `
		INSERT INTO refresh_tokens (user_id, family_id, token_hash, user_agent, ip, expires_at)
		VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,'')::inet, $6)
		RETURNING id`

	var id uuid.UUID
	if err := r.exec(ctx).QueryRow(ctx, q, usuarioID, familiaID, hash, userAgent, ip, expiraEm).Scan(&id); err != nil {
		return uuid.Nil, db.MapError(err)
	}
	return id, nil
}

// BuscarRefresh procura pelo hash — o token em claro nunca chega ao banco.
func (r *Repository) BuscarRefresh(ctx context.Context, hash string) (SessaoToken, error) {
	const q = `
		SELECT id, user_id, family_id, expires_at, revoked_at, replaced_by
		  FROM refresh_tokens
		 WHERE token_hash = $1`

	var s SessaoToken
	err := r.exec(ctx).QueryRow(ctx, q, hash).Scan(
		&s.ID, &s.UsuarioID, &s.FamiliaID, &s.ExpiraEm, &s.RevogadoEm, &s.SubstPor)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, pgx.ErrNoRows
	}
	if err != nil {
		return s, db.MapError(err)
	}
	return s, nil
}

// ResultadoDaRotacao é o veredito do UPDATE condicional da rotação. Existe
// porque "não deu erro" e "rotacionou" são coisas diferentes, e confundir as
// duas foi exatamente o defeito: a rotação perdida commitava em silêncio.
type ResultadoDaRotacao int

const (
	// RotacaoAplicada: esta transação foi a única a queimar o token.
	RotacaoAplicada ResultadoDaRotacao = iota
	// RotacaoPerdida: outra transação já rotacionou o MESMO token. Duas
	// rotações do mesmo comprovante significam duas cópias dele — é reuso, e o
	// chamador trata como roubo.
	RotacaoPerdida
	// RotacaoRevogadaSemSucessor: o token foi derrubado no meio do caminho por
	// logout, troca de senha ou desativação da conta. Sessão morta, não roubo.
	RotacaoRevogadaSemSucessor
)

// RotacionarRefresh marca o token apresentado como revogado e aponta para o
// sucessor. É o que transforma reapresentação em evidência de roubo.
//
// O `RowsAffected` é o coração da defesa, não uma conferência de rotina. Sob
// duas rotações simultâneas do mesmo token, o Postgres serializa os UPDATEs
// nesta linha: a segunda transação espera a primeira commitar e então
// reavalia o `revoked_at IS NULL`, encontrando zero linhas. Ignorar esse zero,
// como o código fazia, deixava a segunda transação commitar assim mesmo —
// nasciam dois sucessores vivos da mesma família, nenhum token ficava marcado
// como rotacionado, e o ladrão que disparasse duas requisições em paralelo
// ganhava um ramo próprio e permanente da família, sem NUNCA disparar
// TOKEN_REUSED na vítima.
//
// Zero linha só tem duas explicações, e a releitura separa as duas. Ela precisa
// ser uma consulta NOVA (e não um CTE junto do UPDATE): em READ COMMITTED cada
// comando pega um snapshot próprio, e só o comando seguinte enxerga o commit da
// transação que venceu a corrida.
//
// Nos caminhos de erro o veredito devolvido é RotacaoPerdida, e não o zero do
// tipo: quem ignorasse o erro por descuido cairia no lado seguro (sessão morre)
// em vez de no lado que entrega token novo.
func (r *Repository) RotacionarRefresh(ctx context.Context, anteriorID, novoID uuid.UUID) (ResultadoDaRotacao, error) {
	const q = `
		UPDATE refresh_tokens
		   SET revoked_at = now(), replaced_by = $2
		 WHERE id = $1 AND revoked_at IS NULL`

	tag, err := r.exec(ctx).Exec(ctx, q, anteriorID, novoID)
	if err != nil {
		return RotacaoPerdida, db.MapError(err)
	}
	if tag.RowsAffected() == 1 {
		return RotacaoAplicada, nil
	}

	const releitura = `SELECT replaced_by IS NOT NULL FROM refresh_tokens WHERE id = $1`
	var rotacionadoPorOutro bool
	switch err := r.exec(ctx).QueryRow(ctx, releitura, anteriorID).Scan(&rotacionadoPorOutro); {
	case errors.Is(err, pgx.ErrNoRows):
		// A linha sumiu (conta removida em cascata) — não há sessão a renovar
		// nem família a acusar de roubo.
		return RotacaoRevogadaSemSucessor, nil
	case err != nil:
		return RotacaoPerdida, db.MapError(err)
	case rotacionadoPorOutro:
		return RotacaoPerdida, nil
	default:
		return RotacaoRevogadaSemSucessor, nil
	}
}

// RevogarFamilia derruba a árvore inteira de sessões nascida de um login.
// Chamada no logout e, principalmente, na detecção de reuso.
//
// Aqui zero linha afetada É esperado e não vira erro: a família pode já estar
// inteira revogada por um logout anterior ou por outra requisição que detectou
// o mesmo reuso ao mesmo tempo. A operação é idempotente de propósito.
func (r *Repository) RevogarFamilia(ctx context.Context, familiaID uuid.UUID) error {
	const q = `UPDATE refresh_tokens SET revoked_at = now() WHERE family_id = $1 AND revoked_at IS NULL`
	_, err := r.exec(ctx).Exec(ctx, q, familiaID)
	return db.MapError(err)
}

// RevogarSessoesDoUsuario derruba TODAS as famílias. É o efeito de trocar a
// senha, desativar a conta, do reset e do logout sem cookie — em todos, o
// pressuposto é que a conta pode estar comprometida.
//
// Como em RevogarFamilia, zero linha afetada é resultado legítimo: quem não
// tinha sessão aberta continua sem sessão aberta.
func (r *Repository) RevogarSessoesDoUsuario(ctx context.Context, usuarioID uuid.UUID) error {
	const q = `UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`
	_, err := r.exec(ctx).Exec(ctx, q, usuarioID)
	return db.MapError(err)
}

// ─────────────────────────── Recuperação de senha ───────────────────────────

func (r *Repository) CriarReset(ctx context.Context, usuarioID uuid.UUID, hash string, expiraEm time.Time) error {
	const q = `INSERT INTO password_resets (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`
	_, err := r.exec(ctx).Exec(ctx, q, usuarioID, hash, expiraEm)
	return db.MapError(err)
}

func (r *Repository) BuscarReset(ctx context.Context, hash string) (TokenDeReset, error) {
	const q = `SELECT id, user_id, expires_at, used_at FROM password_resets WHERE token_hash = $1`

	var t TokenDeReset
	err := r.exec(ctx).QueryRow(ctx, q, hash).Scan(&t.ID, &t.UsuarioID, &t.ExpiraEm, &t.UsadoEm)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, pgx.ErrNoRows
	}
	if err != nil {
		return t, db.MapError(err)
	}
	return t, nil
}

// ConsumirReset marca o token como usado condicionando ao `used_at IS NULL`.
// A condição no UPDATE (e não um SELECT antes) é o que impede duas requisições
// simultâneas de gastarem o mesmo token de uso único.
func (r *Repository) ConsumirReset(ctx context.Context, id uuid.UUID) (bool, error) {
	const q = `UPDATE password_resets SET used_at = now() WHERE id = $1 AND used_at IS NULL`
	tag, err := r.exec(ctx).Exec(ctx, q, id)
	if err != nil {
		return false, db.MapError(err)
	}
	return tag.RowsAffected() == 1, nil
}

// InvalidarResetsPendentes evita que um link antigo continue válido depois de
// pedir outro. Zero linha afetada é o caso comum (não havia link pendente), não
// erro.
func (r *Repository) InvalidarResetsPendentes(ctx context.Context, usuarioID uuid.UUID) error {
	const q = `UPDATE password_resets SET used_at = now() WHERE user_id = $1 AND used_at IS NULL`
	_, err := r.exec(ctx).Exec(ctx, q, usuarioID)
	return db.MapError(err)
}

// AtualizarSenha grava o novo hash.
func (r *Repository) AtualizarSenha(ctx context.Context, usuarioID uuid.UUID, hash string) error {
	const q = `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.exec(ctx).Exec(ctx, q, usuarioID, hash)
	if err != nil {
		return db.MapError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Usuário")
	}
	return nil
}

// BuscarIDPorEmail serve ao /auth/password/forgot, que responde igual exista ou
// não a conta.
func (r *Repository) BuscarIDPorEmail(ctx context.Context, email string) (uuid.UUID, error) {
	const q = `SELECT id FROM users WHERE lower(email) = lower($1) AND active AND deleted_at IS NULL`

	var id uuid.UUID
	err := r.exec(ctx).QueryRow(ctx, q, email).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, pgx.ErrNoRows
	}
	if err != nil {
		return uuid.Nil, db.MapError(err)
	}
	return id, nil
}
