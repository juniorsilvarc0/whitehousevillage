package bens

import (
	"bytes"
	"strings"
	"testing"
)

// As três escolhas que fazem a planilha abrir no Excel em português: BOM,
// `;` e decimal com vírgula. E o acento chega inteiro.
func TestPlanilhaAbreNoExcelEmPortugues(t *testing.T) {
	var buf bytes.Buffer
	err := escreverCSV(&buf, []linhaExportada{
		{
			UnidadeCodigo: "AP-01", AmbienteNome: "Área da churrasqueira", AmbienteTipo: "area_externa",
			BemNome: "Prato raso branco", BemDescricao: ptr("Borda dourada; 26 cm"), Categoria: "louca",
			Medida: "un", QtdEsperada: 12, Custo: ptr[int64](1890), AvariasAbertas: 2,
		},
		{
			UnidadeCodigo: "AP-01", AmbienteNome: "Quarto", AmbienteTipo: "quarto",
			BemNome: "Jogo de lençol", Categoria: "cama", Medida: "jogo", QtdEsperada: 2,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	saida := buf.String()

	if !strings.HasPrefix(saida, "\xEF\xBB\xBF") {
		t.Fatal("sem BOM: o Excel lê como Windows-1252 e o acento quebra")
	}
	linhas := strings.Split(strings.TrimPrefix(saida, bom), "\r\n")
	if !strings.HasPrefix(linhas[0], "Unidade;Ambiente;Tipo de ambiente;Bem;Descrição;") {
		t.Fatalf("cabeçalho = %q", linhas[0])
	}
	want := `AP-01;Área da churrasqueira;Área externa;Prato raso branco;"Borda dourada; 26 cm";Louça;un;12;18,90;2`
	if linhas[1] != want {
		t.Fatalf("linha 1 =\n  %q\nesperado\n  %q", linhas[1], want)
	}
	if linhas[2] != "AP-01;Quarto;Quarto;Jogo de lençol;;Cama;jogo;2;;0" {
		t.Fatalf("custo não cotado sai vazio, não zero: %q", linhas[2])
	}
}

func TestReaisSemFloat(t *testing.T) {
	for centavos, want := range map[int64]string{1890: "18,90", 5: "0,05", 123456: "1234,56", 100: "1,00"} {
		if got := reais(centavos); got != want {
			t.Errorf("reais(%d) = %q, esperado %q", centavos, got, want)
		}
	}
}

// Nome de bem digitado no celular começando com `=` viraria fórmula ao abrir.
func TestCelulaNeutralizaFormula(t *testing.T) {
	for entrada, want := range map[string]string{
		`=HYPERLINK("http://x")`: `'=HYPERLINK("http://x")`,
		"+55":                    "'+55",
		"-1":                     "'-1",
		"@SOMA":                  "'@SOMA",
		"Prato":                  "Prato",
		"":                       "",
	} {
		if got := celula(entrada); got != want {
			t.Errorf("celula(%q) = %q, esperado %q", entrada, got, want)
		}
	}
}

func TestNomeDoArquivo(t *testing.T) {
	if got := nomeDoArquivo("GV-01", "2026-10-07"); got != "inventario-gv-01-2026-10-07.csv" {
		t.Fatalf("got %q", got)
	}
	if got := nomeDoArquivo("Pôr do Sol / Suíte", "2026-10-07"); got != "inventario-por-do-sol-suite-2026-10-07.csv" {
		t.Fatalf("got %q", got)
	}
	if got := nomeDoArquivo("***", "2026-10-07"); got != "inventario-propriedade-2026-10-07.csv" {
		t.Fatalf("got %q", got)
	}
}
