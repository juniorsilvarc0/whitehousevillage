// Teste de contrato das rotas do tarifário.
//
// Por que ele mora AQUI e não em `internal/router`: o teste de contrato de lá
// monta a tabela com `Deps{}` zerado, e `rotasTarifario` devolve nil enquanto o
// `main` não constrói o handler — ou seja, as 34 rotas deste módulo passariam
// invisíveis pela varredura do CI até alguém lembrar de ligá-las.
//
// O pacote é `tarifario_test` (teste externo) para poder importar
// `internal/router` sem ciclo.
package tarifario_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/tarifario"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/router"
)

const caminhoDaOpenAPI = "../../../openapi/openapi.yaml"

var (
	linhaDePath  = regexp.MustCompile(`^  (/\S*):\s*$`)
	linhaDeVerbo = regexp.MustCompile(`^    (get|post|put|patch|delete):\s*$`)
	linhaDeRBAC  = regexp.MustCompile(`x-rbac:\s*\{\s*recurso:\s*(\S+?),\s*acao:\s*(\S+?)\s*\}`)
)

type operacao struct{ recurso, acao string }

// contrato devolve "MÉTODO /path" → (recurso, ação) declarado no `x-rbac`.
func contrato(t *testing.T) map[string]operacao {
	t.Helper()

	bruto, err := os.ReadFile(caminhoDaOpenAPI)
	if err != nil {
		t.Fatalf("lendo o contrato em %s: %v", caminhoDaOpenAPI, err)
	}

	out := map[string]operacao{}
	path, verbo := "", ""
	for _, linha := range strings.Split(string(bruto), "\n") {
		if m := linhaDePath.FindStringSubmatch(linha); m != nil {
			path, verbo = m[1], ""
			continue
		}
		if m := linhaDeVerbo.FindStringSubmatch(linha); m != nil {
			verbo = strings.ToUpper(m[1])
			out[verbo+" "+path] = operacao{}
			continue
		}
		if path != "" && verbo != "" {
			if m := linhaDeRBAC.FindStringSubmatch(linha); m != nil {
				out[verbo+" "+path] = operacao{recurso: m[1], acao: m[2]}
			}
		}
	}

	if len(out) == 0 {
		t.Fatalf("nenhuma operação lida de %s — o scanner do teste ficou defasado", caminhoDaOpenAPI)
	}
	return out
}

// rotasDoTarifario isola as linhas que ESTE módulo acrescenta: a diferença entre
// a tabela montada com e sem o handler.
func rotasDoTarifario(t *testing.T) []router.Rota {
	t.Helper()

	semModulo := map[string]bool{}
	for _, r := range router.Rotas(router.Deps{}) {
		semModulo[r.Metodo+" "+r.Path] = true
	}

	var minhas []router.Rota
	for _, r := range router.Rotas(router.Deps{Tarifario: tarifario.NovoHandlerComService(nil)}) {
		if !semModulo[r.Metodo+" "+r.Path] {
			minhas = append(minhas, r)
		}
	}
	if len(minhas) == 0 {
		t.Fatal("o módulo não registrou rota nenhuma — rotasTarifario voltou a devolver nil?")
	}
	return minhas
}

// Rota fora do contrato é superfície que o painel não conhece e que ninguém
// revisou.
func TestRotasDoTarifarioExistemNaOpenAPI(t *testing.T) {
	decl := contrato(t)

	for _, r := range rotasDoTarifario(t) {
		if _, ok := decl[r.Metodo+" "+r.Path]; !ok {
			t.Errorf("%s %s não existe na OpenAPI", r.Metodo, r.Path)
		}
	}
}

// O `x-rbac` do contrato é o par que a tabela de rotas tem de declarar. Divergir
// aqui é servir um endpoint protegido por uma permissão que ninguém concedeu —
// ou, pior, por uma mais fraca do que a documentada.
func TestRBACDaTabelaBateComOContrato(t *testing.T) {
	decl := contrato(t)

	for _, r := range rotasDoTarifario(t) {
		chave := r.Metodo + " " + r.Path
		esperado, ok := decl[chave]
		if !ok {
			continue // já reprovado no teste acima
		}
		if esperado.recurso == "" {
			t.Errorf("%s: a OpenAPI não declara x-rbac", chave)
			continue
		}
		if r.Recurso != esperado.recurso || r.Acao != esperado.acao {
			t.Errorf("%s: tabela declara (%s, %s), contrato declara (%s, %s)",
				chave, r.Recurso, r.Acao, esperado.recurso, esperado.acao)
		}
	}
}

// Todo endpoint do tarifário exige permissão: preço e política são acesso de
// gestão. Nenhuma linha deste módulo pode ser pública nem "só autenticada".
func TestNenhumaRotaDoTarifarioFicaSemPermissao(t *testing.T) {
	for _, r := range rotasDoTarifario(t) {
		if r.Acesso != router.AcessoPermissao {
			t.Errorf("%s %s tem acesso %q — o tarifário inteiro exige permissão", r.Metodo, r.Path, r.Acesso)
		}
	}
}

// Os cinco cadastros do módulo expõem os seis verbos (docs/api.md §2): quem
// escreve um cliente aprende UMA forma de gravar e usa em todos.
func TestCadastrosDoTarifarioExpoemOsSeisVerbos(t *testing.T) {
	naTabela := map[string]bool{}
	for _, r := range rotasDoTarifario(t) {
		naTabela[r.Metodo+" "+r.Path] = true
	}

	for _, colecao := range []string{"/rate-tables", "/rates", "/holidays", "/special-periods", "/min-nights"} {
		exigidos := []string{
			"GET " + colecao, "POST " + colecao,
			"GET " + colecao + "/{id}", "PUT " + colecao + "/{id}",
			"PATCH " + colecao + "/{id}", "DELETE " + colecao + "/{id}",
		}
		for _, e := range exigidos {
			if !naTabela[e] {
				t.Errorf("recurso %s não expõe %s", colecao, e)
			}
		}
	}
}

// Política NÃO tem PATCH nem DELETE: ela é histórico, não cadastro. Voltar atrás
// é publicar de novo a versão antiga, que entra como versão nova e deixa rastro.
func TestPoliticaNaoExpoePatchNemDelete(t *testing.T) {
	for _, r := range rotasDoTarifario(t) {
		if !strings.HasPrefix(r.Path, "/policies/") {
			continue
		}
		if r.Metodo == "PATCH" || r.Metodo == "DELETE" {
			t.Errorf("%s %s existe — política versionada não se edita nem se apaga", r.Metodo, r.Path)
		}
	}
}
