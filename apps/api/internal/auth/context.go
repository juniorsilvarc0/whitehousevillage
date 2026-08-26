package auth

import (
	"context"

	"github.com/google/uuid"
)

type chaveUsuario struct{}

// Usuario é a identidade resolvida da requisição: quem é, de que propriedade,
// de que perfil e o que pode fazer. Montada uma vez por requisição pelo
// middleware, a partir do banco — nunca a partir do JWT, que ficaria defasado
// quando a matriz do perfil mudasse.
type Usuario struct {
	ID         uuid.UUID
	PropertyID uuid.UUID
	RoleID     uuid.UUID
	RoleCode   string
	RoleName   string
	// RoleIsSystem marca o perfil raiz (o `admin` do seed). Ele é definido como
	// "tudo", inclusive os recursos que uma migration futura acrescentar — sem
	// isso, publicar um módulo novo tornaria o recurso inconcedível por
	// qualquer pessoa, porque ninguém teria a célula ainda.
	RoleIsSystem bool
	Nome         string
	Email        string
	Permissoes   Conjunto
}

// WithUser injeta a identidade no contexto da requisição.
func WithUser(ctx context.Context, u *Usuario) context.Context {
	return context.WithValue(ctx, chaveUsuario{}, u)
}

// UserFrom recupera a identidade. O segundo retorno é falso em rota pública.
func UserFrom(ctx context.Context) (*Usuario, bool) {
	u, ok := ctx.Value(chaveUsuario{}).(*Usuario)
	return u, ok && u != nil
}

// UsuarioID é o atalho para o repositório montar `AND owner_id = $user`.
func UsuarioID(ctx context.Context) (uuid.UUID, bool) {
	u, ok := UserFrom(ctx)
	if !ok {
		return uuid.Nil, false
	}
	return u.ID, true
}
