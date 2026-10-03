package apperr

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A garantia que faltava ao espelho do contrato (F2-03).
//
// `internal/router/contrato_de_erros_test.go` confere que nenhum code sai fora
// do enum — mas aceitava o MESMO code declarado em cinco lugares, cada um com a
// sua frase e, no dia em que alguém errasse, com o seu status. Estes testes
// fecham o outro lado: os 37 do enum existem aqui, cada um uma vez, e nenhum
// arquivo de internal/ fora deste pacote monta código de erro.

const (
	caminhoDaOpenAPI = "../../../openapi/openapi.yaml"

	// Raiz da varredura: internal/, relativa a este pacote.
	raizDeInternal = "../.."

	// Onde o enum começa no contrato — o mesmo marcador do teste do router.
	inicioDoEnumDeErros = "enum: [VALIDATION_ERROR"
)

// literalComCaraDeCodigo casa string em CAIXA_ALTA com ao menos um `_`. Os três
// códigos de uma palavra só (INTERNAL, UNAUTHORIZED, FORBIDDEN) são procurados
// à parte, pelo literal exato.
var literalComCaraDeCodigo = regexp.MustCompile(`"([A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+)"`)

// Formas de montar erro sem passar pelo catálogo: o literal da struct e a
// reescrita do Code de uma cópia — as duas que os módulos usavam até o F2-03
// (`(&apperr.Error{Code: …})` em crm, contatos e disponibilidade; `e.Code = …`
// em tarifario, inventario e reservas).
//
// O `[^*\w]` antes do literal separa a construção (`&apperr.Error{`) do tipo de
// retorno (`func … *apperr.Error {`), que é legítimo.
var montagemForaDoCatalogo = []*regexp.Regexp{
	regexp.MustCompile(`(?m)(^|[^*\w])apperr\.Error\s*\{`),
	regexp.MustCompile(`\.Code\s*=[^=]`),
}

// pastasComLiteralPermitido são as pastas de internal/ onde literal em
// CAIXA_ALTA é permitido, e por quê.
var pastasComLiteralPermitido = map[string]string{
	// Os literais são nomes de variável de ambiente (DATABASE_URL, JWT_SECRET).
	"platform/config/": "variável de ambiente",
	// O domínio é puro e não importa apperr: `booking.RuleError` carrega o code
	// em TEXTO, e quem traduz é `apperr.PorCodigo`. Aqui o literal é permitido,
	// mas TestCodigoDoDominioExisteNoCatalogo exige que cada um esteja no
	// catálogo — senão a tradução cairia em 500.
	"domain/": "code carimbado pelo motor puro",
}

func codigosDoContrato(t *testing.T) []string {
	t.Helper()

	bruto, err := os.ReadFile(caminhoDaOpenAPI)
	if err != nil {
		t.Fatalf("lendo o contrato em %s: %v", caminhoDaOpenAPI, err)
	}
	texto := string(bruto)
	i := strings.Index(texto, inicioDoEnumDeErros)
	if i < 0 {
		t.Fatalf("não achei o enum de erros (%q) em %s: o bloco mudou e este teste virou etapa vazia",
			inicioDoEnumDeErros, caminhoDaOpenAPI)
	}
	i += len("enum: [")
	j := strings.Index(texto[i:], "]")
	if j < 0 {
		t.Fatalf("enum de erros sem fechamento em %s", caminhoDaOpenAPI)
	}
	var codigos []string
	for _, parte := range strings.Split(strings.ReplaceAll(texto[i:i+j], "\n", " "), ",") {
		if p := strings.TrimSpace(parte); p != "" {
			codigos = append(codigos, p)
		}
	}
	if len(codigos) < 10 {
		t.Fatalf("enum de erros com %d entradas: a leitura do contrato quebrou", len(codigos))
	}
	return codigos
}

// fontes devolve caminho relativo a internal/ → conteúdo, de todo .go não-teste
// sob `raiz`, sem as linhas de comentário (comentário cita code à vontade).
func fontes(t *testing.T, raiz string) map[string]string {
	t.Helper()

	internal, err := filepath.Abs(raizDeInternal)
	if err != nil {
		t.Fatalf("resolvendo %s: %v", raizDeInternal, err)
	}
	achados := map[string]string{}
	err = filepath.WalkDir(raiz, func(caminho string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		bruto, err := os.ReadFile(caminho)
		if err != nil {
			return err
		}
		var codigo []string
		for _, linha := range strings.Split(string(bruto), "\n") {
			if strings.HasPrefix(strings.TrimSpace(linha), "//") {
				continue
			}
			codigo = append(codigo, linha)
		}
		absoluto, err := filepath.Abs(caminho)
		if err != nil {
			return err
		}
		relativo, err := filepath.Rel(internal, absoluto)
		if err != nil {
			return err
		}
		achados[filepath.ToSlash(relativo)] = strings.Join(codigo, "\n")
		return nil
	})
	if err != nil {
		t.Fatalf("varrendo %s: %v", raiz, err)
	}
	return achados
}

// TestCatalogoEspelhaOEnumDoContrato — os dois conjuntos são o mesmo.
func TestCatalogoEspelhaOEnumDoContrato(t *testing.T) {
	contrato := codigosDoContrato(t)
	noContrato := map[string]bool{}
	for _, c := range contrato {
		noContrato[c] = true
	}

	var faltando, sobrando []string
	for c := range noContrato {
		if _, ok := PorCodigo(c); !ok {
			faltando = append(faltando, c)
		}
	}
	for _, c := range Codigos() {
		if !noContrato[c] {
			sobrando = append(sobrando, c)
		}
	}
	sort.Strings(faltando)
	if len(faltando) > 0 {
		t.Errorf("códigos do enum da OpenAPI sem definição em apperr/catalogo.go: %s", strings.Join(faltando, ", "))
	}
	if len(sobrando) > 0 {
		t.Errorf("códigos em apperr fora do enum da OpenAPI: %s — acrescente ao `components.responses.Erro` antes",
			strings.Join(sobrando, ", "))
	}
	if len(Codigos()) != len(noContrato) {
		t.Errorf("catálogo com %d códigos, contrato com %d", len(Codigos()), len(noContrato))
	}

	// Cada erro base tem status de erro HTTP e frase padrão: um base sem
	// status sairia 0, que o net/http transforma em 200 com corpo de erro.
	for _, c := range Codigos() {
		e, _ := PorCodigo(c)
		if e.Status() < 400 || e.Status() > 599 {
			t.Errorf("%s com status %d", c, e.Status())
		}
		if strings.TrimSpace(e.Message) == "" {
			t.Errorf("%s sem mensagem padrão", c)
		}
	}
}

// TestCadaCodigoEhDeclaradoUmaVezNoApperr — o literal de cada code aparece uma
// vez só no pacote, na constante. Duas declarações são o primeiro passo para
// dois status.
func TestCadaCodigoEhDeclaradoUmaVezNoApperr(t *testing.T) {
	pacote := fontes(t, ".")
	if len(pacote) == 0 {
		t.Fatal("nenhum .go lido no pacote apperr: o teste virou etapa vazia")
	}
	for _, c := range codigosDoContrato(t) {
		alvo := `"` + c + `"`
		var onde []string
		for arquivo, conteudo := range pacote {
			for range strings.Count(conteudo, alvo) {
				onde = append(onde, arquivo)
			}
		}
		if len(onde) != 1 {
			sort.Strings(onde)
			t.Errorf("%s aparece %d vez(es) como literal em apperr (%s); esperado 1, na constante Code*",
				c, len(onde), strings.Join(onde, ", "))
		}
	}
}

// TestDefinirRecusaCodigoRepetido — a trava de execução por trás do teste de
// texto acima: o mesmo code duas vezes não chega a carregar o pacote.
func TestDefinirRecusaCodigoRepetido(t *testing.T) {
	antes := ResourceInUse.Status()
	defer func() {
		if recover() == nil {
			t.Fatal("definir aceitou RESOURCE_IN_USE pela segunda vez")
		}
		if e, _ := PorCodigo(CodeResourceInUse); e.Status() != antes {
			t.Fatalf("a segunda definição sobrescreveu o status: %d, era %d", e.Status(), antes)
		}
	}()
	_ = definir(CodeResourceInUse, "outra frase", 422)
}

// TestNenhumPacoteDeclaraCodigoDeErroProprio — fora de apperr, nenhum arquivo
// de internal/ escreve um code nem monta *apperr.Error à mão.
//
// O que era permitido até o F2-03 e deixou de ser: módulo com função local
// (`erro`, `conflito`, `invalido`) que copiava um erro do apperr e reescrevia o
// Code. Quem precisa de um code importa a variável do apperr; quem precisa
// COMPARAR um code usa a constante `apperr.Code*`.
func TestNenhumPacoteDeclaraCodigoDeErroProprio(t *testing.T) {
	todas := fontes(t, raizDeInternal)
	if len(todas) < 50 {
		t.Fatalf("varredura achou só %d arquivos em %s: o caminho mudou e o teste virou etapa vazia",
			len(todas), raizDeInternal)
	}

	codigosDeUmaPalavra := []string{}
	for _, c := range codigosDoContrato(t) {
		if !strings.Contains(c, "_") {
			codigosDeUmaPalavra = append(codigosDeUmaPalavra, c)
		}
	}

	var violacoes []string
	modulosVistos := 0
	for arquivo, conteudo := range todas {
		if strings.HasPrefix(arquivo, "platform/apperr/") {
			continue
		}
		if strings.HasPrefix(arquivo, "modules/") {
			modulosVistos++
		}
		for _, re := range montagemForaDoCatalogo {
			if re.MatchString(conteudo) {
				violacoes = append(violacoes, arquivo+": monta erro fora do catálogo ("+re.String()+")")
			}
		}
		if permitido(arquivo) {
			continue
		}
		for _, m := range literalComCaraDeCodigo.FindAllStringSubmatch(conteudo, -1) {
			violacoes = append(violacoes, arquivo+": literal "+m[0])
		}
		for _, c := range codigosDeUmaPalavra {
			if strings.Contains(conteudo, `"`+c+`"`) {
				violacoes = append(violacoes, arquivo+`: literal "`+c+`"`)
			}
		}
	}
	if modulosVistos < 20 {
		t.Fatalf("só %d arquivos de internal/modules na varredura: o teste virou etapa vazia", modulosVistos)
	}

	if len(violacoes) > 0 {
		sort.Strings(violacoes)
		t.Fatalf("código de erro declarado fora de internal/platform/apperr:\n  %s\n"+
			"Use a variável do apperr (ResourceInUse, InvalidStateTransition, …) e troque a frase com "+
			"WithMessage; para comparar, a constante apperr.Code*. Código novo nasce em apperr/catalogo.go "+
			"e no enum da OpenAPI, no mesmo PR.",
			strings.Join(violacoes, "\n  "))
	}
}

// TestCodigoDoDominioExisteNoCatalogo — o domínio carimba o code em texto;
// cada um precisa ter base no catálogo, senão `PorCodigo` falha e a tradução
// do módulo devolve 500 no lugar do 422 que o contrato promete.
func TestCodigoDoDominioExisteNoCatalogo(t *testing.T) {
	dominio := fontes(t, filepath.Join(raizDeInternal, "domain"))
	vistos := 0
	for arquivo, conteudo := range dominio {
		for _, m := range literalComCaraDeCodigo.FindAllStringSubmatch(conteudo, -1) {
			vistos++
			if _, ok := PorCodigo(m[1]); !ok {
				t.Errorf("%s carimba %s, que não existe em apperr/catalogo.go", arquivo, m[1])
			}
		}
	}
	if vistos == 0 {
		t.Fatal("nenhum code achado em internal/domain: o motor deixou de carimbar ou a varredura quebrou")
	}
}

func permitido(arquivo string) bool {
	for pasta := range pastasComLiteralPermitido {
		if strings.HasPrefix(arquivo, pasta) {
			return true
		}
	}
	return false
}
