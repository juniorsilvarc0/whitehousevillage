package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// algoritmoAceito é HS256 e nada mais.
//
// A vulnerabilidade clássica de JWT é aceitar o `alg` que o TOKEN declara: o
// atacante troca para "none" (ou para RS256 usando a chave pública como segredo
// HMAC) e forja qualquer identidade. Por isso o algoritmo é decidido AQUI, na
// verificação, e o token que chegar com outro é recusado antes de a assinatura
// sequer ser conferida.
const algoritmoAceito = "HS256"

var (
	ErrTokenInvalido = errors.New("token de acesso inválido")
	ErrTokenExpirado = errors.New("token de acesso expirado")
)

// Claims é o conteúdo do access token. Fica deliberadamente magro: id do
// usuário, papel e identificador do token. Permissão NÃO vai aqui — se fosse,
// mudar a matriz do perfil só valeria no próximo login, e o contrato promete
// que vale na requisição seguinte.
type Claims struct {
	jwt.RegisteredClaims
	Role string `json:"role"`
}

// Emissor assina e verifica o access token.
type Emissor struct {
	segredo []byte
	ttl     time.Duration
	emissor string
}

func NovoEmissor(segredo string, ttl time.Duration) *Emissor {
	return &Emissor{segredo: []byte(segredo), ttl: ttl, emissor: "whitehousevillage"}
}

// TTL é o tempo de vida do access token, que a resposta do login devolve em
// expires_in.
func (e *Emissor) TTL() time.Duration { return e.ttl }

// Issue assina o access token do usuário. O `jti` existe para correlacionar o
// access com a linha de auditoria, não para lista negra: access token curto
// (15 min) é o que dispensa manter revogação de JWT.
func (e *Emissor) Issue(usuarioID uuid.UUID, papel string) (string, time.Time, error) {
	agora := time.Now()
	expira := agora.Add(e.ttl)

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   usuarioID.String(),
			ID:        uuid.NewString(),
			Issuer:    e.emissor,
			IssuedAt:  jwt.NewNumericDate(agora),
			ExpiresAt: jwt.NewNumericDate(expira),
			NotBefore: jwt.NewNumericDate(agora),
		},
		Role: papel,
	}

	assinado, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(e.segredo)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("assinando token: %w", err)
	}
	return assinado, expira, nil
}

// Parse verifica assinatura, algoritmo e validade e devolve as claims.
func (e *Emissor) Parse(token string) (*Claims, error) {
	claims := &Claims{}

	_, err := jwt.ParseWithClaims(token, claims,
		func(t *jwt.Token) (any, error) {
			// Segunda barreira, redundante de propósito: se um dia alguém
			// afrouxar WithValidMethods, esta linha ainda barra "none".
			if t.Method.Alg() != algoritmoAceito {
				return nil, fmt.Errorf("algoritmo inesperado: %s", t.Method.Alg())
			}
			return e.segredo, nil
		},
		jwt.WithValidMethods([]string{algoritmoAceito}),
		jwt.WithIssuer(e.emissor),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(30*time.Second), // tolera relógio levemente fora de sincronia
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpirado
		}
		return nil, fmt.Errorf("%w: %v", ErrTokenInvalido, err)
	}

	if _, err := uuid.Parse(claims.Subject); err != nil {
		return nil, fmt.Errorf("%w: sub não é uuid", ErrTokenInvalido)
	}
	return claims, nil
}

// UsuarioID extrai o uuid do `sub` já validado.
func (c *Claims) UsuarioID() (uuid.UUID, error) { return uuid.Parse(c.Subject) }
