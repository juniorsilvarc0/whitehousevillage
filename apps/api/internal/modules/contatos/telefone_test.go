package contatos

import "testing"

// A deduplicação por telefone só vale o que a normalização entrega: o índice
// `contacts_phone_idx` é UNIQUE sobre TEXTO, e texto diferente não colide.
// Cada linha aceita abaixo é uma forma que o balcão, o formulário e o inbound
// de WhatsApp produzem para a MESMA pessoa.
func TestNormalizarTelefoneAceita(t *testing.T) {
	casos := map[string]string{
		"+5585999990000":       "+5585999990000",
		"+55 85 99999-0000":    "+5585999990000",
		"+55 (85) 99999-0000":  "+5585999990000",
		" +55 85 9 9999 0000 ": "+5585999990000",
		"5585999990000":        "+5585999990000",
		"85999990000":          "+5585999990000", // nacional com DDD e nono dígito
		"(86) 99999-9999":      "+5586999999999",
		"8532223333":           "+558532223333", // fixo de 10 dígitos
		"+13475550123":         "+13475550123",  // estrangeiro: DDI informado, não se mexe
		"":                     "",
		"   ":                  "",
	}

	for entrada, esperado := range casos {
		t.Run(entrada, func(t *testing.T) {
			got, err := NormalizarTelefone(entrada)
			if err != nil {
				t.Fatalf("NormalizarTelefone(%q) devolveu erro: %v", entrada, err)
			}
			if got != esperado {
				t.Fatalf("NormalizarTelefone(%q) = %q, esperado %q", entrada, got, esperado)
			}
		})
	}
}

// O que continua 422. É o recorte que a divergência de contrato NÃO abriu: aqui
// adivinhar seria adivinhar mesmo.
func TestNormalizarTelefoneRecusa(t *testing.T) {
	casos := []string{
		"99999",               // curto demais para qualquer plano
		"999990000",           // 9 dígitos: sem DDD e sem DDI
		"012345678901",        // 12 dígitos que não começam por 55
		"05999990000",         // DDD 05 não existe
		"85812345678",         // 11 dígitos sem o nono dígito 9 não é celular brasileiro
		"+55",                 // só DDI
		"telefone",            // texto
		"+5585abc990000",      // letras no meio
		"55+85999990000",      // '+' fora do início
		"+558599999000012345", // acima dos 15 dígitos da E.164
	}

	for _, entrada := range casos {
		t.Run(entrada, func(t *testing.T) {
			if got, err := NormalizarTelefone(entrada); err == nil {
				t.Fatalf("NormalizarTelefone(%q) devolveu %q sem erro; deveria recusar", entrada, got)
			}
		})
	}
}

// A propriedade que sustenta a deduplicação: formas diferentes do mesmo número
// convergem para UMA string. Sem isto, "(85) 99999-0000" e "+5585999990000"
// seriam duas linhas que o índice único jamais compararia.
func TestFormasDiferentesDoMesmoNumeroConvergem(t *testing.T) {
	formas := []string{
		"+5585999990000",
		"+55 85 99999-0000",
		"+55 (85) 9 9999-0000",
		"5585999990000",
		"85999990000",
		"(85) 99999-0000",
		"85 9 9999 0000",
	}

	var canonico string
	for i, forma := range formas {
		got, err := NormalizarTelefone(forma)
		if err != nil {
			t.Fatalf("%q: %v", forma, err)
		}
		if i == 0 {
			canonico = got
			continue
		}
		if got != canonico {
			t.Fatalf("%q normalizou para %q, mas %q normalizou para %q — o índice único trataria como pessoas diferentes",
				forma, got, formas[0], canonico)
		}
	}
}
