package pii

import (
	"regexp"
	"strings"
	"testing"
)

func p(s string) *string { return &s }

func valor(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// Os exemplos são os da tabela do contrato ("Dado pessoal na resposta"); um
// formato diferente aqui é quebra de contrato com o painel.
func TestMascaraDeDocumentoSegueAFormaDoTipo(t *testing.T) {
	for _, c := range []struct {
		nome         string
		tipo, numero *string
		esperado     string
	}{
		{"cpf de 11 dígitos", p("cpf"), p("12345677735"), "***.***.777-35"},
		{"cnpj de 14 dígitos", p("cnpj"), p("11222333000181"), "**.***.***/0001-81"},
		{"passaporte", p("passaporte"), p("FX1234567"), "******567"},
		{"cpf com tamanho errado cai no genérico", p("cpf"), p("1234567890"), "*******890"},
		{"cpf com pontuação cai no genérico", p("cpf"), p("123.456.777-35"), "***********-35"},
		{"tipo nulo", nil, p("12345677735"), "********735"},
		{"cnpj marcado como cpf", p("cpf"), p("11222333000181"), "***********181"},
		{"três caracteres", p("passaporte"), p("ABC"), "***"},
		{"vazio", p("passaporte"), p(""), "***"},
		{"nulo continua nulo", p("cpf"), nil, "<nil>"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			if got := valor(MascararDocumento(c.tipo, c.numero)); got != c.esperado {
				t.Fatalf("MascararDocumento = %q, esperado %q", got, c.esperado)
			}
		})
	}
}

func TestMascaraDeTelefoneMostraSoOsQuatroUltimos(t *testing.T) {
	for entrada, esperado := range map[string]string{
		"+5585999990000": "+*********0000",
		"+12025550123":   "+*******0123",
		"+1234":          "+****",
		"":               "+",
	} {
		if got := valor(MascararTelefone(p(entrada))); got != esperado {
			t.Errorf("MascararTelefone(%q) = %q, esperado %q", entrada, got, esperado)
		}
	}
	if MascararTelefone(nil) != nil {
		t.Error("telefone nulo virou valor")
	}
}

func TestMascaraDeEmailMantemInicialEDominio(t *testing.T) {
	for entrada, esperado := range map[string]string{
		"fernanda.lima@gmail.com": "f***@gmail.com",
		"a@b.co":                  "a***@b.co",
		"élida@exemplo.com.br":    "é***@exemplo.com.br",
		"semarroba":               "***",
		"@dominio.com":            "***@dominio.com",
	} {
		if got := valor(MascararEmail(p(entrada))); got != esperado {
			t.Errorf("MascararEmail(%q) = %q, esperado %q", entrada, got, esperado)
		}
	}
	if MascararEmail(nil) != nil {
		t.Error("e-mail nulo virou valor")
	}
}

// A propriedade que importa, conferida sobre uma amostra: nenhuma máscara
// devolve uma sequência de 11 dígitos (CPF) nem um telefone E.164 inteiro.
func TestNenhumaMascaraDevolveOValorCheio(t *testing.T) {
	cpfCheio := regexp.MustCompile(`\d{11}`)
	foneCheio := regexp.MustCompile(`\+\d{8,}`)
	for _, doc := range []string{"12345677735", "11222333000181", "98765432100", "FX1234567"} {
		for _, tipo := range []string{"cpf", "cnpj", "passaporte", ""} {
			m := valor(MascararDocumento(p(tipo), p(doc)))
			if cpfCheio.MatchString(m) || strings.Contains(m, doc) {
				t.Errorf("documento %q (%s) vazou na máscara %q", doc, tipo, m)
			}
		}
	}
	for _, fone := range []string{"+5585999990000", "+558533334444", "+12025550123"} {
		if m := valor(MascararTelefone(p(fone))); foneCheio.MatchString(m) {
			t.Errorf("telefone %q vazou na máscara %q", fone, m)
		}
	}
}
