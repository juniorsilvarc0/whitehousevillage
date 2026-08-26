package auth

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// UsuarioResposta é o schema `Usuario` do contrato. Vive no pacote auth porque
// /auth/login, /auth/me e /users devolvem exatamente a mesma forma — duplicar a
// struct nos módulos seria abrir espaço para os três divergirem.
type UsuarioResposta struct {
	ID          uuid.UUID  `json:"id"`
	Nome        string     `json:"name"`
	Email       string     `json:"email"`
	Telefone    *string    `json:"phone"`
	RoleID      uuid.UUID  `json:"role_id"`
	Role        string     `json:"role"`
	RoleName    string     `json:"role_name"`
	BrokerID    *uuid.UUID `json:"broker_id"`
	Ativo       bool       `json:"active"`
	UltimoLogin *time.Time `json:"last_login_at"`
	CriadoEm    time.Time  `json:"created_at"`
}

// UsuarioDaLinha traduz a linha do banco para o contrato.
//
// broker_id vem do banco (coluna criada na migration 20260820140000) e é nulo
// só para quem não é corretor. O painel do corretor usa esse campo para ligar a
// conta ao cadastro de corretor — devolver nulo fixo deixava o painel sem dono.
func UsuarioDaLinha(l LinhaUsuario) UsuarioResposta {
	return UsuarioResposta{
		ID:          l.ID,
		Nome:        l.Nome,
		Email:       l.Email,
		Telefone:    l.Telefone,
		RoleID:      l.RoleID,
		Role:        l.RoleCode,
		RoleName:    l.RoleName,
		BrokerID:    l.BrokerID,
		Ativo:       l.Ativo,
		UltimoLogin: l.UltimoLogin,
		CriadoEm:    l.CriadoEm,
	}
}

// ─────────────────────────── Requisições ────────────────────────────────────

type PedidoLogin struct {
	Email string `json:"email" validate:"required,email"`
	Senha string `json:"password" validate:"required"`
}

// NormalizarEmail apara e baixa a caixa: e-mail é case-insensitive na prática, e
// o índice único do banco é sobre lower(email).
func (p *PedidoLogin) NormalizarEmail() { p.Email = strings.ToLower(strings.TrimSpace(p.Email)) }

type PedidoRefresh struct {
	RefreshToken string `json:"refresh_token"`
}

type PedidoLogout struct {
	RefreshToken string `json:"refresh_token"`
}

type PedidoEsqueciSenha struct {
	Email string `json:"email" validate:"required,email"`
}

type PedidoTrocarSenha struct {
	Token string `json:"token" validate:"required"`
	Senha string `json:"password" validate:"required,min=8"`
}

// ─────────────────────────── Respostas ──────────────────────────────────────

// RespostaSessao é o corpo de /auth/login e /auth/refresh — o contrato exige o
// mesmo shape nos dois.
type RespostaSessao struct {
	AccessToken string          `json:"access_token"`
	ExpiresIn   int             `json:"expires_in"`
	Usuario     UsuarioResposta `json:"user"`
}

// RespostaMe é o corpo de /auth/me.
type RespostaMe struct {
	Usuario    UsuarioResposta `json:"user"`
	Permissoes []Permissao     `json:"permissions"`
}

// Sessao carrega, além do corpo da resposta, o refresh em claro que vira cookie.
// O refresh nunca entra em RespostaSessao: fora do cookie httpOnly ele fica ao
// alcance de qualquer XSS.
type Sessao struct {
	Resposta     RespostaSessao
	RefreshClaro string
	RefreshTTL   time.Duration

	// novoTokenID é a linha recém-criada em refresh_tokens; a rotação precisa
	// dele para apontar `replaced_by` do token anterior.
	novoTokenID uuid.UUID
}
