package router

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNenhumHandlerLeOCorpoPorFora.
//
// EM LINGUAGEM DE NEGÓCIO: a recusa de campo desconhecido — o defeito em que
// `PATCH /units {"ativa": false}` respondia 200 sem desativar nada — vale para a
// API inteira porque TODO handler decodifica pelo mesmo lugar
// (`httpx.Decode` / `httpx.DecodeOpcional`, que chamam `DisallowUnknownFields`).
// A varredura por HTTP prova isso rota a rota nas rotas de hoje; este teste
// protege a propriedade que faz a prova valer AMANHÃ: um handler novo que abra
// `r.Body` por conta própria fica de fora da regra e ninguém percebe, porque
// aceitar campo a mais não quebra nenhum teste — só cria de novo o silêncio em
// que o rename `guests` → `guests_count` atravessou uma rodada inteira.
//
// Roda sem banco e sem tag: é o tipo de erro que tem de aparecer no `make check`
// de quem escreveu a rota, e não três dias depois na suíte de integração.
func TestNenhumHandlerLeOCorpoPorFora(t *testing.T) {
	raiz := filepath.Join("..", "modules")

	var suspeitos []string
	err := filepath.Walk(raiz, func(caminho string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(caminho, ".go") || strings.HasSuffix(caminho, "_test.go") {
			return nil
		}
		bruto, err := os.ReadFile(caminho)
		if err != nil {
			return err
		}
		for numero, linha := range strings.Split(string(bruto), "\n") {
			// `r.Body` só aparece legitimamente dentro de httpx. Num módulo, ele
			// é um decoder paralelo — e decoder paralelo é regra paralela.
			if strings.Contains(linha, "r.Body") || strings.Contains(linha, "req.Body") {
				suspeitos = append(suspeitos, filepath.ToSlash(caminho)+":"+strconv.Itoa(numero+1)+" — "+strings.TrimSpace(linha))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("varrendo %s: %v", raiz, err)
	}

	if len(suspeitos) > 0 {
		t.Errorf("handler lendo o corpo da requisição por fora de `httpx.Decode`:\n  %s\n\n"+
			"Quem decodifica por fora não chama `DisallowUnknownFields`: a rota volta a responder 200 "+
			"para um corpo com o nome do campo errado, e o cliente vai embora achando que salvou.",
			strings.Join(suspeitos, "\n  "))
	}
}
