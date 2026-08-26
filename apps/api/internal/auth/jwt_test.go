package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const segredoDeTeste = "segredo-de-teste-com-mais-de-32-bytes!!"

func TestIssueEParseFecham(t *testing.T) {
	e := NovoEmissor(segredoDeTeste, 15*time.Minute)
	id := uuid.New()

	token, expira, err := e.Issue(id, "corretor")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if time.Until(expira) > 15*time.Minute+time.Second {
		t.Fatalf("expiração além do TTL: %v", expira)
	}

	claims, err := e.Parse(token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.Subject != id.String() {
		t.Fatalf("sub = %q, esperado %q", claims.Subject, id)
	}
	if claims.Role != "corretor" {
		t.Fatalf("role = %q, esperado corretor", claims.Role)
	}
	if claims.ID == "" {
		t.Fatal("jti vazio: sem ele não há como correlacionar o access com a auditoria")
	}
	if got, err := claims.UsuarioID(); err != nil || got != id {
		t.Fatalf("UsuarioID = %v, %v", got, err)
	}
}

func TestParseRecusaTokenExpirado(t *testing.T) {
	// TTL negativo produz um token que já nasceu vencido.
	e := NovoEmissor(segredoDeTeste, -2*time.Minute)

	token, _, err := e.Issue(uuid.New(), "admin")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if _, err := e.Parse(token); !errors.Is(err, ErrTokenExpirado) {
		t.Fatalf("esperado ErrTokenExpirado, veio %v", err)
	}
}

// A vulnerabilidade clássica: o atacante troca o alg do token para "none",
// remove a assinatura e passa a forjar qualquer identidade. Aceitar o algoritmo
// que o TOKEN declara é exatamente o que este teste proíbe.
func TestParseRecusaAlgNone(t *testing.T) {
	id := uuid.New()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   id.String(),
			Issuer:    "whitehousevillage",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Role: "admin",
	}

	semAssinatura, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("montando token none: %v", err)
	}
	if !strings.HasPrefix(semAssinatura, "eyJhbGciOiJub25lIiw") {
		t.Fatalf("o token de teste deveria declarar alg none: %s", semAssinatura)
	}

	e := NovoEmissor(segredoDeTeste, 15*time.Minute)
	if _, err := e.Parse(semAssinatura); err == nil {
		t.Fatal("token com alg none foi aceito — forja de identidade liberada")
	}
}

func TestParseRecusaAlgDiferenteDeHS256(t *testing.T) {
	id := uuid.New()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   id.String(),
			Issuer:    "whitehousevillage",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	// HS512 com o MESMO segredo: assinatura válida, algoritmo não autorizado.
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString([]byte(segredoDeTeste))
	if err != nil {
		t.Fatalf("montando token HS512: %v", err)
	}

	e := NovoEmissor(segredoDeTeste, 15*time.Minute)
	if _, err := e.Parse(token); err == nil {
		t.Fatal("token HS512 foi aceito; só HS256 é autorizado")
	}
}

func TestParseRecusaAssinaturaDeOutroSegredo(t *testing.T) {
	emissorDoAtacante := NovoEmissor("outro-segredo-completamente-diferente", time.Hour)
	token, _, err := emissorDoAtacante.Issue(uuid.New(), "admin")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	e := NovoEmissor(segredoDeTeste, 15*time.Minute)
	if _, err := e.Parse(token); !errors.Is(err, ErrTokenInvalido) {
		t.Fatalf("esperado ErrTokenInvalido, veio %v", err)
	}
}

func TestParseRecusaTokenComSubNaoUUID(t *testing.T) {
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "admin",
			Issuer:    "whitehousevillage",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(segredoDeTeste))
	if err != nil {
		t.Fatalf("montando token: %v", err)
	}

	e := NovoEmissor(segredoDeTeste, 15*time.Minute)
	if _, err := e.Parse(token); err == nil {
		t.Fatal("sub fora do formato uuid deveria ser recusado")
	}
}
