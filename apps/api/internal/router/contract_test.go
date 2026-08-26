package router

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Teste de contrato. Varre a tabela declarativa de rotas e a compara com o
// arquivo que É o contrato (`openapi/openapi.yaml`).
//
// docs/api.md §2 diz, textualmente, que os seis verbos são garantidos
// "estruturalmente, não por disciplina", e que "um teste de contrato no CI varre
// a tabela e falha se algum recurso não expõe os seis verbos, se algum verbo não
// checa permissão, ou se existe rota fora da OpenAPI". Este arquivo é esse teste.
//
// Ele lê a OpenAPI como TEXTO, com um scanner de indentação, em vez de usar uma
// biblioteca de YAML: acrescentar dependência ao go.mod é decisão do tech-lead,
// e o formato do arquivo (dois espaços para o path, quatro para o verbo) é
// estável o bastante para o que se quer conferir aqui.

const caminhoDaOpenAPI = "../../openapi/openapi.yaml"

var (
	linhaDePath  = regexp.MustCompile(`^  (/\S*):\s*$`)
	linhaDeVerbo = regexp.MustCompile(`^    (get|post|put|patch|delete):\s*$`)
)

// verbosDaOpenAPI devolve path → conjunto de métodos declarados no contrato.
func verbosDaOpenAPI(t *testing.T) map[string]map[string]bool {
	t.Helper()

	bruto, err := os.ReadFile(caminhoDaOpenAPI)
	if err != nil {
		t.Fatalf("lendo o contrato em %s: %v", caminhoDaOpenAPI, err)
	}

	out := map[string]map[string]bool{}
	atual := ""
	for _, linha := range strings.Split(string(bruto), "\n") {
		if m := linhaDePath.FindStringSubmatch(linha); m != nil {
			atual = m[1]
			if out[atual] == nil {
				out[atual] = map[string]bool{}
			}
			continue
		}
		if atual == "" {
			continue
		}
		if m := linhaDeVerbo.FindStringSubmatch(linha); m != nil {
			out[atual][strings.ToUpper(m[1])] = true
		}
	}

	if len(out) == 0 {
		t.Fatalf("nenhum path lido de %s — o scanner do teste ficou defasado do formato do arquivo", caminhoDaOpenAPI)
	}
	return out
}

// verbosDaTabela devolve path → conjunto de métodos registrados no router.
func verbosDaTabela(t *testing.T) map[string]map[string]bool {
	t.Helper()

	out := map[string]map[string]bool{}
	for _, r := range tabela(t) {
		if out[r.Path] == nil {
			out[r.Path] = map[string]bool{}
		}
		out[r.Path][r.Metodo] = true
	}
	return out
}

// Toda rota que a API serve tem de estar no contrato. Rota fora dele é
// superfície que o painel não conhece, que ninguém revisou e que nenhum teste de
// outro time cobre.
func TestNenhumaRotaExisteForaDaOpenAPI(t *testing.T) {
	contrato := verbosDaOpenAPI(t)

	for _, r := range tabela(t) {
		verbos, ok := contrato[r.Path]
		if !ok {
			t.Errorf("%s %s não existe na OpenAPI", r.Metodo, r.Path)
			continue
		}
		if !verbos[r.Metodo] {
			t.Errorf("a OpenAPI conhece %s mas não o verbo %s", r.Path, r.Metodo)
		}
	}
}

// docs/api.md §2: "Todo recurso expõe os seis."
//
// Em linguagem de negócio: quem escreve um cliente da API aprende UMA forma de
// gravar e usa em todos os cadastros. Um recurso que não aceita a substituição
// integral (PUT) quebra esse cliente exatamente onde ele não espera — e o
// `crud.Mount` genérico que o docs/api.md promete não tem como nascer enquanto
// os recursos divergirem.
func TestTodoRecursoCRUDExpoeOsSeisVerbos(t *testing.T) {
	naTabela := verbosDaTabela(t)
	naOpenAPI := verbosDaOpenAPI(t)

	// Coleção = path sem {id} que aceita listar (GET) e criar (POST).
	colecoes := []string{}
	for path, verbos := range naTabela {
		if strings.Contains(path, "{") {
			continue
		}
		if verbos[http.MethodGet] && verbos[http.MethodPost] {
			colecoes = append(colecoes, path)
		}
	}
	sort.Strings(colecoes)

	if len(colecoes) == 0 {
		t.Fatal("nenhum recurso CRUD encontrado na tabela — o teste deixou de proteger alguma coisa")
	}

	for _, colecao := range colecoes {
		item := colecao + "/{id}"

		for _, fonte := range []struct {
			nome   string
			verbos map[string]map[string]bool
		}{
			{"tabela de rotas (internal/router/routes.go)", naTabela},
			{"contrato (openapi/openapi.yaml)", naOpenAPI},
		} {
			faltando := []string{}
			for _, exigido := range []struct {
				path, metodo string
			}{
				{colecao, http.MethodGet},
				{colecao, http.MethodPost},
				{item, http.MethodGet},
				{item, http.MethodPut},
				{item, http.MethodPatch},
				{item, http.MethodDelete},
			} {
				if !fonte.verbos[exigido.path][exigido.metodo] {
					faltando = append(faltando, exigido.metodo+" "+exigido.path)
				}
			}
			if len(faltando) > 0 {
				t.Errorf("recurso %s: %s não expõe %s", colecao, fonte.nome, strings.Join(faltando, ", "))
			}
		}
	}
}

// Nenhum endpoint fica sem checagem de permissão (docs/spec.md §1). Quem abre
// exceção escreve por quê, e o motivo aparece na revisão — não é comentário
// solto, é campo obrigatório da tabela.
func TestTodaRotaProtegidaDeclaraRecursoEAcao(t *testing.T) {
	for _, r := range tabela(t) {
		chave := r.Metodo + " " + r.Path

		switch r.Acesso {
		case AcessoPermissao:
			if r.Recurso == "" {
				t.Errorf("%s: rota protegida sem recurso", chave)
			}
			if r.Acao == "" {
				t.Errorf("%s: rota protegida sem ação", chave)
			}
		case AcessoAutenticado:
			if strings.TrimSpace(r.Motivo) == "" {
				t.Errorf("%s: não consulta a matriz de permissões e não explica por quê", chave)
			}
		case AcessoPublico:
			if r.Recurso != "" || r.Acao != "" {
				t.Errorf("%s: rota pública declarando recurso/ação — classificação contraditória", chave)
			}
		default:
			t.Errorf("%s: acesso não classificado", chave)
		}
	}
}

// A OpenAPI marca `security: []` só no que é público de propósito. A tabela e o
// contrato têm de concordar sobre isso: um endpoint aberto no código e fechado no
// papel (ou o inverso) é a divergência que ninguém percebe até vazar.
func TestPublicasDaTabelaBatemComOSecurityVazioDaOpenAPI(t *testing.T) {
	bruto, err := os.ReadFile(caminhoDaOpenAPI)
	if err != nil {
		t.Fatalf("lendo o contrato: %v", err)
	}

	// path → verbo → tem `security: []`
	semSeguranca := map[string]map[string]bool{}
	path, verbo := "", ""
	for _, linha := range strings.Split(string(bruto), "\n") {
		if m := linhaDePath.FindStringSubmatch(linha); m != nil {
			path, verbo = m[1], ""
			continue
		}
		if m := linhaDeVerbo.FindStringSubmatch(linha); m != nil {
			verbo = strings.ToUpper(m[1])
			continue
		}
		if path != "" && verbo != "" && strings.TrimSpace(linha) == "security: []" {
			if semSeguranca[path] == nil {
				semSeguranca[path] = map[string]bool{}
			}
			semSeguranca[path][verbo] = true
		}
	}

	for _, r := range tabela(t) {
		// /healthz e /readyz são sondas: o contrato as marca `security: []` e a
		// tabela as marca públicas; o resto precisa bater.
		aberta := semSeguranca[r.Path][r.Metodo]
		publica := r.Acesso == AcessoPublico

		if publica && !aberta {
			t.Errorf("%s %s é pública no código mas o contrato não a marca `security: []`", r.Metodo, r.Path)
		}
		if aberta && !publica {
			t.Errorf("%s %s é `security: []` no contrato mas exige token no código", r.Metodo, r.Path)
		}
	}
}

// A constante que o /readyz usa para decidir se o schema está defasado precisa
// acompanhar a última migration entregue — é o que o próprio comentário dela
// promete ("sobe junto com a migration nova, sempre no mesmo commit").
//
// Em linguagem de negócio: com a constante atrasada, a API entra no balanceador
// dizendo "estou pronta" com o banco numa versão anterior à que o time entregou.
// O primeiro cliente que tocar numa coluna nova é quem descobre.
func TestSchemaVersionEsperadaAcompanhaAUltimaMigration(t *testing.T) {
	arquivos, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatalf("procurando migrations: %v", err)
	}
	if len(arquivos) == 0 {
		t.Fatal("nenhuma migration encontrada — o caminho do teste ficou defasado")
	}

	var (
		maior     uint64
		maiorNome string
	)
	for _, arquivo := range arquivos {
		nome := filepath.Base(arquivo)
		partes := strings.SplitN(nome, "_", 2)
		versao, err := strconv.ParseUint(partes[0], 10, 64)
		if err != nil {
			t.Fatalf("migration %q não começa com timestamp: %v", nome, err)
		}
		if versao > maior {
			maior, maiorNome = versao, nome
		}
	}

	if SchemaVersionEsperada != maior {
		t.Fatalf("SchemaVersionEsperada = %d, mas a última migration é %d (%s).\n%s",
			SchemaVersionEsperada, maior, maiorNome,
			fmt.Sprintf("Enquanto divergirem, /readyz devolve 200 com o banco em %d — "+
				"a instância entra no balanceador com o schema atrasado.", SchemaVersionEsperada))
	}
}
