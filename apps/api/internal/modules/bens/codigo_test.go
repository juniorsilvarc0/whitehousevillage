package bens

import (
	"strings"
	"testing"
)

// Os exemplos da migration 20261007213000 e do coordenador: a derivação em Go
// tem de dar o mesmo que o backfill em SQL, caractere a caractere.
func TestBaseDoCodigoSegueARegraDaMigration(t *testing.T) {
	casos := map[string]string{
		"Área da churrasqueira": "area-da-churrasqueira",
		"Suíte 1 (térreo)":      "suite-1-terreo",
		"🛏️":                    "comodo",
		"Øresund ß":             "resund",
		"Sala":                  "sala",
		"SALA!":                 "sala",
		"  --Cozinha--  ":       "cozinha",
		"Ação & Ñandú":          "acao-nandu",
		"Ýmir":                  "mir", // Ý fora da tabela: vira hífen, e a ponta sai
		"quarto_2":              "quarto-2",
		"":                      "comodo",
		"!!!":                   "comodo",
		// Passo 1: nome decomposto (NFD) — "A" + U+0301. Sem tirar a marca
		// combinante, o acento solto viraria hífen ("a-rea").
		"Área externa": "area-externa",
	}
	for nome, want := range casos {
		if got := BaseDoCodigo(nome); got != want {
			t.Errorf("BaseDoCodigo(%q) = %q, esperado %q", nome, got, want)
		}
		if got := BaseDoCodigo(nome); !CodigoValido(got) {
			t.Errorf("BaseDoCodigo(%q) = %q fora do formato do contrato", nome, got)
		}
	}
}

// Passo 5: acima de 60 corta, e o hífen que sobrar no fim sai.
func TestBaseDoCodigoCortaEmSessenta(t *testing.T) {
	nome := strings.Repeat("a", 59) + " bcd"
	if got := BaseDoCodigo(nome); got != strings.Repeat("a", 59) {
		t.Fatalf("corte em 60 deixa %q (len %d); o hífen da posição 60 tem de sair", got, len(got))
	}
	if got := BaseDoCodigo(strings.Repeat("x", 120)); len(got) != 60 {
		t.Fatalf("base de 120 x deveria ter 60, tem %d", len(got))
	}
}

func TestCandidatoDoCodigo(t *testing.T) {
	if CandidatoDoCodigo("sala", 1) != "sala" || CandidatoDoCodigo("sala", 2) != "sala-2" || CandidatoDoCodigo("sala", 10) != "sala-10" {
		t.Fatal("sequência base, base-2, base-10")
	}
	longa := strings.Repeat("a", 57) + "-bc" // 60
	got := CandidatoDoCodigo(longa, 2)       // corte em 58 = 57 a + "-", que sai
	if got != strings.Repeat("a", 57)+"-2" || len(got) > 60 {
		t.Fatalf("CandidatoDoCodigo(longa, 2) = %q", got)
	}
	if got := CandidatoDoCodigo(strings.Repeat("z", 60), 12); len(got) != 60 || !strings.HasSuffix(got, "-12") {
		t.Fatalf("o candidato nunca passa de 60: %q (%d)", got, len(got))
	}
}

func TestCodigoValido(t *testing.T) {
	for _, ok := range []string{"sala", "suite-1-terreo", "a", "a1-b2"} {
		if !CodigoValido(ok) {
			t.Errorf("%q deveria ser válido", ok)
		}
	}
	for _, ruim := range []string{"", "Sala", "sala-", "-sala", "sa--la", "suíte", "sala_2", "sala 2", strings.Repeat("a", 61)} {
		if CodigoValido(ruim) {
			t.Errorf("%q deveria ser recusado", ruim)
		}
	}
}
