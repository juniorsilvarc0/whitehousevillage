package router

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A etapa `it-concorrencia` do Makefile repete os testes de disputa
// REPETICOES_CONCORRENCIA vezes, e escolhe quais por NOME — Go não tem
// categoria de teste. O Makefile já avisa por escrito: "quem escrever uma
// disputa nova batiza com uma destas palavras, senão o teste fica de fora da
// repetição". A guarda que existia lá só reprova quando o regex não casa com
// NADA; ela é cega para o caso real, que é o teste de disputa que existe,
// passa uma vez e nunca é repetido.
//
// Foi o que aconteceu, e é por isso que este arquivo nasceu. Medido em
// 27/08/2026, rodando a etapa inteira: `internal/router` respondeu
// "[no tests to run]" enquanto guardava dois dos testes de disputa mais caros
// da casa —
//
//	TestComposicaoCrescendo... (a composição crescer no meio de uma venda
//	  all_members, que deixa a nona unidade da casa "inteira" à venda para um
//	  estranho) e
//	TestTrocaDeConsumes...     (a troca de `consumes` contra uma venda em voo,
//	  a janela que a migration 20260827140000 fechou com FOR UPDATE)
//
// — porque os NOMES deles não continham nenhuma das palavras. Os dois estavam
// verdes numa passada só, que é exatamente a passada em que uma corrida
// intermitente se esconde: o Makefile documenta que uma execução isolada já
// passou verde com o defeito presente, e é por isso que a repetição existe.
//
// O critério aqui é o do ARQUIVO, e é deliberadamente conservador: um arquivo
// batizado de disputa é uma declaração do autor de que ali dentro há disputa.
// Adivinhar pelo corpo (`go func`, `sync.WaitGroup`) apanharia helper de
// fixture e faria a guarda ser desligada por barulho.
//
// Roda SEM banco e SEM a tag `integration`, de propósito: a omissão que ela
// pega é de nome de teste, e tem de aparecer no `make check` de quem escreveu
// o teste, não três etapas adiante.

// vocabularioDeArquivo são as palavras que declaram "este arquivo é de
// disputa". É o vocabulário dos nomes de arquivo que a árvore já usa, e é
// separado do regex do Makefile por construção: se os dois viessem da mesma
// fonte, a guarda não teria com o que comparar.
var vocabularioDeArquivo = []string{"concorren", "concurrency", "corrida", "simultane", "disputa", "overbook"}

func TestTodoTesteDeDisputaEntraNaRepeticaoDoMakefile(t *testing.T) {
	raiz := filepath.Join("..", "..", "..", "..")

	regex := regexDeConcorrenciaDoMakefile(t, filepath.Join(raiz, "Makefile"))
	casa := regexp.MustCompile(regex)

	declaracao := regexp.MustCompile(`(?m)^func (Test\w+)\(`)

	var forasDaLista []string
	arquivosVistos := 0

	err := filepath.Walk(filepath.Join(raiz, "apps", "api"), func(caminho string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), "_test.go") {
			return nil
		}
		nome := strings.ToLower(info.Name())
		declaradoDeDisputa := false
		for _, palavra := range vocabularioDeArquivo {
			if strings.Contains(nome, palavra) {
				declaradoDeDisputa = true
				break
			}
		}
		if !declaradoDeDisputa {
			return nil
		}
		arquivosVistos++

		fonte, err := os.ReadFile(caminho)
		if err != nil {
			return err
		}
		for _, m := range declaracao.FindAllStringSubmatch(string(fonte), -1) {
			if !casa.MatchString(m[1]) {
				relativo, _ := filepath.Rel(raiz, caminho)
				forasDaLista = append(forasDaLista, relativo+": "+m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("varrendo a árvore de testes: %v", err)
	}

	// Sem este piso a guarda passaria verde numa árvore em que os arquivos de
	// disputa tivessem sido renomeados para fora do vocabulário — que é o
	// mesmo defeito, um nível acima.
	if arquivosVistos < 4 {
		t.Fatalf("só %d arquivo(s) de disputa encontrado(s) na árvore: ou eles sumiram, "+
			"ou foram renomeados para fora de %v e a repetição do Makefile ficou vazia",
			arquivosVistos, vocabularioDeArquivo)
	}

	if len(forasDaLista) > 0 {
		t.Fatalf("teste(s) de disputa que a etapa `make it-concorrencia` NUNCA repete — "+
			"eles rodam UMA vez e uma corrida intermitente atravessa:\n  %s\n"+
			"o nome do teste precisa casar com TESTES_CONCORRENCIA (%s) — "+
			"rebatize o teste, não afrouxe o regex",
			strings.Join(forasDaLista, "\n  "), regex)
	}
}

// regexDeConcorrenciaDoMakefile lê a linha `TESTES_CONCORRENCIA ?= ...`.
//
// Lê do Makefile em vez de repetir o valor aqui porque uma cópia divergiria em
// silêncio, e a cópia divergente é a própria falha que este teste existe para
// impedir.
func regexDeConcorrenciaDoMakefile(t *testing.T, caminho string) string {
	t.Helper()

	bruto, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatalf("lendo o Makefile em %s: %v — a guarda não consegue saber quais testes a etapa repete", caminho, err)
	}
	linha := regexp.MustCompile(`(?m)^TESTES_CONCORRENCIA\s*\?=\s*(.+)$`).FindSubmatch(bruto)
	if linha == nil {
		t.Fatal("o Makefile não declara mais TESTES_CONCORRENCIA: a etapa `it-concorrencia` " +
			"perdeu a lista de testes que repete, e nada mais diz quais são as disputas")
	}
	return strings.TrimSpace(string(linha[1]))
}
