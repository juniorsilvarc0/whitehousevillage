package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Autenticador resolve a identidade da requisição a partir do Bearer token.
type Autenticador struct {
	jwt  *Emissor
	repo *Repository
}

func NewAutenticador(emissor *Emissor, repo *Repository) *Autenticador {
	return &Autenticador{jwt: emissor, repo: repo}
}

// Middleware exige um access token válido e carrega usuário + permissões do
// BANCO a cada requisição.
//
// As permissões vêm do banco, e não do JWT, de propósito: o contrato promete
// que alterar a matriz de um perfil "vale na requisição seguinte, sem deploy e
// sem novo login". Com a matriz assada no token, ela só valeria daqui a 15 min
// — e revogar acesso de alguém demoraria o mesmo tanto. O custo é uma consulta
// indexada por requisição.
func (a *Autenticador) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bruto := tokenDoHeader(r)
		if bruto == "" {
			httpx.Error(w, r, apperr.Unauthorized)
			return
		}

		claims, err := a.jwt.Parse(bruto)
		if err != nil {
			if errors.Is(err, ErrTokenExpirado) {
				// details distingue "expirou" de "inválido" para o painel saber
				// que vale a pena chamar /auth/refresh antes de mandar relogar.
				httpx.Error(w, r, apperr.Unauthorized.WithDetails(map[string]string{"reason": "expired"}))
				return
			}
			httpx.Error(w, r, apperr.Unauthorized.WithCause(err))
			return
		}

		usuarioID, err := claims.UsuarioID()
		if err != nil {
			httpx.Error(w, r, apperr.Unauthorized.WithCause(err))
			return
		}

		usuario, err := a.repo.CarregarSessao(r.Context(), usuarioID)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}

		ctx := WithUser(r.Context(), usuario)
		ctx = httpx.ComAtor(ctx, usuario.ID.String())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func tokenDoHeader(r *http.Request) string {
	cabecalho := r.Header.Get("Authorization")
	if cabecalho == "" {
		return ""
	}
	// O esquema é case-insensitive pela RFC 7235; "bearer" minúsculo é comum em
	// cliente escrito à mão e recusá-lo só gera suporte.
	partes := strings.SplitN(cabecalho, " ", 2)
	if len(partes) != 2 || !strings.EqualFold(partes[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(partes[1])
}
