package contatos

import "testing"

func TestNormalizarDocumentoCPF(t *testing.T) {
	// A mesma pessoa, três máscaras. Guardar as três formas na coluna faria a
	// busca do balcão — que é igualdade exata — errar conforme quem digitou.
	for _, entrada := range []string{"529.982.247-25", "52998224725", " 529 982 247 25 "} {
		got, err := NormalizarDocumento(DocCPF, entrada)
		if err != nil {
			t.Fatalf("CPF %q: %v", entrada, err)
		}
		if got != "52998224725" {
			t.Fatalf("CPF %q normalizou para %q", entrada, got)
		}
	}

	for _, invalido := range []string{"52998224726", "111.111.111-11", "1234567890", "abcdefghijk"} {
		if _, err := NormalizarDocumento(DocCPF, invalido); err == nil {
			t.Errorf("CPF %q deveria ser recusado", invalido)
		}
	}
}

func TestNormalizarDocumentoCNPJ(t *testing.T) {
	got, err := NormalizarDocumento(DocCNPJ, "11.222.333/0001-81")
	if err != nil {
		t.Fatalf("CNPJ válido recusado: %v", err)
	}
	if got != "11222333000181" {
		t.Fatalf("CNPJ normalizou para %q", got)
	}
	for _, invalido := range []string{"11222333000182", "00000000000000", "112223330001"} {
		if _, err := NormalizarDocumento(DocCNPJ, invalido); err == nil {
			t.Errorf("CNPJ %q deveria ser recusado", invalido)
		}
	}
}

// Passaporte não tem validação de formato: cada país emite o seu, e recusar o
// que não se sabe validar barraria hóspede estrangeiro no balcão.
func TestNormalizarDocumentoPassaporte(t *testing.T) {
	got, err := NormalizarDocumento(DocPassaporte, " fx 123456 ")
	if err != nil {
		t.Fatalf("passaporte recusado: %v", err)
	}
	if got != "FX123456" {
		t.Fatalf("passaporte normalizou para %q, esperado FX123456", got)
	}
	if _, err := NormalizarDocumento(DocPassaporte, "AB#1"); err == nil {
		t.Error("passaporte com símbolo deveria ser recusado")
	}
}

func TestNormalizarDocumentoTipoDesconhecido(t *testing.T) {
	if _, err := NormalizarDocumento("rg", "12345"); err == nil {
		t.Fatal("tipo fora do enum deveria ser recusado")
	}
}
