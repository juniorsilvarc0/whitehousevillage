package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func contextoCom(perms ...Permissao) context.Context {
	return WithUser(context.Background(), &Usuario{
		ID:         uuid.New(),
		RoleCode:   "perfil-de-teste",
		Permissoes: NovoConjunto(perms),
	})
}

func TestEscopoAllEOwn(t *testing.T) {
	ctx := contextoCom(
		Permissao{Resource: "reservations", Action: AcaoVer, Scope: EscopoAll},
		Permissao{Resource: "crm.opportunities", Action: AcaoVer, Scope: EscopoOwn},
	)

	if escopo := ScopeOf(ctx, "reservations", AcaoVer); escopo != EscopoAll {
		t.Fatalf("reservations:ver = %q, esperado all", escopo)
	}
	if escopo := ScopeOf(ctx, "crm.opportunities", AcaoVer); escopo != EscopoOwn {
		t.Fatalf("crm.opportunities:ver = %q, esperado own", escopo)
	}

	if SomenteProprios(ctx, "reservations", AcaoVer) {
		t.Fatal("escopo all não pode virar filtro por dono")
	}
	if !SomenteProprios(ctx, "crm.opportunities", AcaoVer) {
		t.Fatal("escopo own tem de virar filtro por dono no SQL")
	}
}

// Ausência é negação: nunca existe permissão por omissão.
func TestAcaoAusenteEhNegada(t *testing.T) {
	ctx := contextoCom(Permissao{Resource: "reservations", Action: AcaoVer, Scope: EscopoAll})

	if escopo := ScopeOf(ctx, "reservations", AcaoExcluir); escopo != "" {
		t.Fatalf("ação não concedida devolveu escopo %q", escopo)
	}
	if escopo := ScopeOf(ctx, "finance.payments", AcaoVer); escopo != "" {
		t.Fatalf("recurso não concedido devolveu escopo %q", escopo)
	}
}

func TestSemUsuarioNoContextoEhNegado(t *testing.T) {
	if escopo := ScopeOf(context.Background(), "reservations", AcaoVer); escopo != "" {
		t.Fatalf("contexto anônimo devolveu escopo %q", escopo)
	}
}

// Escopo desconhecido no banco é dado corrompido; a decisão é negar. Permitir
// por omissão transformaria um typo num vazamento.
func TestEscopoDesconhecidoEhDescartado(t *testing.T) {
	c := NovoConjunto([]Permissao{
		{Resource: "finance.payments", Action: AcaoVer, Scope: "todos"},
		{Resource: "finance.payments", Action: "aprovar", Scope: EscopoAll},
	})

	if c.Pode("finance.payments", AcaoVer) {
		t.Fatal("escopo desconhecido deveria ser descartado, não aceito")
	}
	if c.Pode("finance.payments", "aprovar") {
		t.Fatal("ação fora do catálogo deveria ser descartada")
	}
	if len(c.Lista()) != 0 {
		t.Fatalf("conjunto deveria estar vazio, veio %v", c.Lista())
	}
}

// Recurso com ponto no nome não pode confundir a chave "recurso:ação".
func TestRecursoComPontoSobreviveAoIdaEVolta(t *testing.T) {
	c := NovoConjunto([]Permissao{{Resource: "crm.opportunities", Action: AcaoEditar, Scope: EscopoOwn}})

	lista := c.Lista()
	if len(lista) != 1 {
		t.Fatalf("esperava 1 permissão, veio %d", len(lista))
	}
	if lista[0].Resource != "crm.opportunities" || lista[0].Action != AcaoEditar || lista[0].Scope != EscopoOwn {
		t.Fatalf("permissão deformada no ida e volta: %+v", lista[0])
	}
}

func TestMiddlewareAutorizaENega(t *testing.T) {
	protegido := Middleware("users", AcaoEditar)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	casos := []struct {
		nome     string
		ctx      context.Context
		esperado int
	}{
		{
			nome:     "com a permissão exata",
			ctx:      contextoCom(Permissao{Resource: "users", Action: AcaoEditar, Scope: EscopoAll}),
			esperado: http.StatusNoContent,
		},
		{
			nome:     "com outra ação do mesmo recurso",
			ctx:      contextoCom(Permissao{Resource: "users", Action: AcaoVer, Scope: EscopoAll}),
			esperado: http.StatusForbidden,
		},
		{
			nome:     "sem permissão nenhuma",
			ctx:      contextoCom(),
			esperado: http.StatusForbidden,
		},
		{
			nome:     "anônimo",
			ctx:      context.Background(),
			esperado: http.StatusUnauthorized,
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/1", nil).WithContext(c.ctx)
			resp := httptest.NewRecorder()

			protegido.ServeHTTP(resp, req)

			if resp.Code != c.esperado {
				t.Fatalf("status = %d, esperado %d (corpo: %s)", resp.Code, c.esperado, resp.Body.String())
			}
		})
	}
}
