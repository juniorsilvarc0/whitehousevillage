package router

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Espelho entre o vocabulário de erros do CONTRATO e o código Go.
//
// O `components.responses.Erro` da OpenAPI diz, textualmente: "Espelhado em
// `internal/platform/apperr`: acrescentar código aqui obriga a acrescentar lá,
// e o contrário também". Até esta rodada nada conferia isso, e a promessa já
// era falsa nos dois sentidos ao mesmo tempo:
//
//   - o contrato ganhou `CONTACT_DUPLICATE`, `CONTACT_ANONYMIZED` e
//     `QUOTE_NOT_PENDING` numa entrega e os módulos que os emitem nasceram em
//     OUTRA — se as duas metades não tivessem se encontrado, a API responderia
//     um `code` que o painel traduz para "erro interno" (medido pelo agente do
//     painel: `normalizarCodigo("CONTACT_DUPLICATE", 409) === "INTERNAL"`);
//   - e um código inventado só no Go sairia na resposta sem estar no enum, ou
//     seja, sem o painel ter como saber que ele existe.
//
// O teste é de TEXTO nos dois lados, de propósito. Do lado Go ele procura o
// literal `"CODIGO"` em qualquer arquivo não-teste sob `internal/`, e não uma
// chamada a um construtor específico: hoje há CINCO formas diferentes de
// declarar erro (`apperr.define`, e os `erro`/`conflito`/`invalido`/`Code:` de
// cada módulo), e amarrar o teste a uma delas o cegaria para a próxima.
const (
	// Onde o enum começa no contrato. Casar pelo primeiro código evita depender
	// da indentação exata do bloco.
	inicioDoEnumDeErros = "enum: [VALIDATION_ERROR"

	// Raiz da varredura do lado Go, relativa a este pacote.
	raizDoCodigoGo = ".."
)

// pacotesSemCodigoDeErro são os diretórios cujos literais em CAIXA_ALTA não são
// código de erro. Só `config`, e os literais dele são nomes de variável de
// ambiente (`DATABASE_URL`, `JWT_SECRET`). Sem esta exceção o sentido
// Go → contrato acusaria oito falsos positivos.
var pacotesSemCodigoDeErro = map[string]bool{
	"platform/config": true,
}

// literalDeCodigo casa string em CAIXA_ALTA com ao menos um `_`. A exigência do
// underscore é o que separa código de erro de palavra solta em maiúscula; os
// três códigos de uma palavra só (`INTERNAL`, `UNAUTHORIZED`, `FORBIDDEN`) são
// cobertos pelo outro sentido do teste, que procura cada código do contrato
// literalmente.
var literalDeCodigo = regexp.MustCompile(`"([A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+)"`)

func codigosDoContrato(t *testing.T) []string {
	t.Helper()

	bruto, err := os.ReadFile(caminhoDaOpenAPI)
	if err != nil {
		t.Fatalf("lendo o contrato em %s: %v", caminhoDaOpenAPI, err)
	}
	texto := string(bruto)

	i := strings.Index(texto, inicioDoEnumDeErros)
	if i < 0 {
		t.Fatalf("não achei o enum de códigos de erro (%q) em %s: o bloco foi renomeado e este teste virou etapa vazia",
			inicioDoEnumDeErros, caminhoDaOpenAPI)
	}
	i += len("enum: [")
	j := strings.Index(texto[i:], "]")
	if j < 0 {
		t.Fatalf("enum de códigos de erro sem fechamento em %s", caminhoDaOpenAPI)
	}

	var codigos []string
	for _, parte := range strings.Split(strings.ReplaceAll(texto[i:i+j], "\n", " "), ",") {
		if p := strings.TrimSpace(parte); p != "" {
			codigos = append(codigos, p)
		}
	}
	if len(codigos) < 10 {
		t.Fatalf("enum de erros com %d entradas: leitura do contrato quebrou", len(codigos))
	}
	return codigos
}

// fontesGo devolve caminho → conteúdo de todo .go não-teste sob internal/.
func fontesGo(t *testing.T) map[string]string {
	t.Helper()

	fontes := map[string]string{}
	err := filepath.WalkDir(raizDoCodigoGo, func(caminho string, d fs.DirEntry, err error) error {
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
		relativo := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(caminho), "../"))
		fontes[relativo] = string(bruto)
		return nil
	})
	if err != nil {
		t.Fatalf("varrendo %s: %v", raizDoCodigoGo, err)
	}
	if len(fontes) < 20 {
		t.Fatalf("varredura achou só %d arquivos .go: o caminho %s mudou e o teste virou etapa vazia",
			len(fontes), raizDoCodigoGo)
	}
	return fontes
}

// literaisDoGo devolve código → arquivos onde ele aparece como literal em
// CAIXA_ALTA com underscore. Usado só no sentido Go → contrato.
func literaisDoGo(t *testing.T) map[string][]string {
	t.Helper()

	achados := map[string][]string{}
	for arquivo, conteudo := range fontesGo(t) {
		for _, m := range literalDeCodigo.FindAllStringSubmatch(conteudo, -1) {
			achados[m[1]] = append(achados[m[1]], arquivo)
		}
	}
	return achados
}

// TestTodoCodigoDoContratoExisteNoGo — sentido contrato → Go.
//
// Código documentado que nenhum arquivo emite é promessa que a API não cumpre:
// o painel escreve o tratamento, testa contra o contrato e nunca recebe a
// resposta.
func TestTodoCodigoDoContratoExisteNoGo(t *testing.T) {
	fontes := fontesGo(t)

	// Busca pelo literal completo, e não pelo mapa de literaisDoGo: os três
	// códigos de uma palavra só (INTERNAL, UNAUTHORIZED, FORBIDDEN) não têm
	// underscore e escapariam daquele regex.
	var faltando []string
	for _, codigo := range codigosDoContrato(t) {
		alvo := `"` + codigo + `"`
		achou := false
		for _, conteudo := range fontes {
			if strings.Contains(conteudo, alvo) {
				achou = true
				break
			}
		}
		if !achou {
			faltando = append(faltando, codigo)
		}
	}

	if len(faltando) > 0 {
		sort.Strings(faltando)
		t.Fatalf("códigos no enum da OpenAPI sem nenhuma ocorrência no Go: %s\n"+
			"Ou o módulo que os emite não foi entregue, ou o código foi renomeado só de um lado. "+
			"O sintoma em produção é o painel recebendo um code que ele traduz para \"erro interno\".",
			strings.Join(faltando, ", "))
	}
}

// TestNenhumCodigoDeErroExisteSoNoGo — sentido Go → contrato.
//
// É o sentido que a revisão humana não pega: o código novo funciona, o teste do
// módulo passa (ele conhece a própria constante) e o painel é o único a
// descobrir, em produção, que existe um `code` fora do vocabulário.
func TestNenhumCodigoDeErroExisteSoNoGo(t *testing.T) {
	noContrato := map[string]bool{}
	for _, c := range codigosDoContrato(t) {
		noContrato[c] = true
	}

	var forasteiros []string
	for codigo, arquivos := range literaisDoGo(t) {
		if noContrato[codigo] {
			continue
		}
		ignorado := false
		for _, arquivo := range arquivos {
			for pacote := range pacotesSemCodigoDeErro {
				if strings.HasPrefix(arquivo, pacote+"/") {
					ignorado = true
				}
			}
		}
		if !ignorado {
			forasteiros = append(forasteiros, codigo+" ("+arquivos[0]+")")
		}
	}

	if len(forasteiros) > 0 {
		sort.Strings(forasteiros)
		t.Fatalf("literais em CAIXA_ALTA no Go que não estão no enum da OpenAPI: %s\n"+
			"Se for código de erro, acrescente ao `components.responses.Erro`. Se não for, "+
			"o lugar dele é fora de um literal — ou nesta lista de exceções, com o motivo escrito.",
			strings.Join(forasteiros, ", "))
	}
}
