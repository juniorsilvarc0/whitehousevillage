package contatos

import (
	"errors"
	"strings"
)

var (
	// ErrDocumentoInvalido — o número não bate com o tipo declarado.
	ErrDocumentoInvalido = errors.New("documento inválido para o tipo informado")
	// ErrTipoDeDocumentoInvalido — tipo fora do enum do contrato.
	ErrTipoDeDocumentoInvalido = errors.New("tipo de documento desconhecido")
)

// NormalizarDocumento devolve o número no formato em que a coluna guarda.
//
// CPF e CNPJ ficam SÓ COM DÍGITOS, como o contrato descreve, e são conferidos
// pelo dígito verificador. Guardar "123.456.789-09" e "12345678909" na mesma
// coluna faria a busca do balcão — que é igualdade exata — errar conforme a
// máscara que o operador usou, e faria a deduplicação por documento não
// deduplicar nada.
//
// Passaporte é alfanumérico em CAIXA ALTA e NÃO tem validação de formato: cada
// país emite o seu, e recusar o que não se sabe validar barraria hóspede
// estrangeiro no balcão — que é exatamente quem mais chega sem CPF.
func NormalizarDocumento(tipo, numero string) (string, error) {
	n := strings.TrimSpace(numero)
	if n == "" {
		return "", nil
	}

	switch tipo {
	case DocCPF:
		d := somenteDigitos(n)
		if len(d) != 11 || !cpfValido(d) {
			return "", ErrDocumentoInvalido
		}
		return d, nil

	case DocCNPJ:
		d := somenteDigitos(n)
		if len(d) != 14 || !cnpjValido(d) {
			return "", ErrDocumentoInvalido
		}
		return d, nil

	case DocPassaporte:
		p := strings.ToUpper(strings.Join(strings.Fields(n), ""))
		if len(p) < 5 || len(p) > 20 || !alfanumerico(p) {
			return "", ErrDocumentoInvalido
		}
		return p, nil

	default:
		return "", ErrTipoDeDocumentoInvalido
	}
}

// cpfValido confere os dois dígitos verificadores.
//
// A sequência repetida ("11111111111") passa na conta do DV e é recusada à
// parte: são os valores que aparecem quando alguém preenche o campo para se
// livrar dele, e deixá-los entrar cria um "contato duplicado" legítimo — vinte
// pessoas com o mesmo CPF de mentira, que o índice único (quando existir)
// passaria a recusar de vez.
func cpfValido(d string) bool {
	if todosIguais(d) {
		return false
	}
	for _, pos := range []int{9, 10} {
		soma := 0
		peso := pos + 1
		for i := 0; i < pos; i++ {
			soma += int(d[i]-'0') * peso
			peso--
		}
		dv := (soma * 10) % 11
		if dv == 10 {
			dv = 0
		}
		if dv != int(d[pos]-'0') {
			return false
		}
	}
	return true
}

// cnpjValido confere os dois dígitos verificadores pelos pesos 2..9 cíclicos.
func cnpjValido(d string) bool {
	if todosIguais(d) {
		return false
	}
	pesos := []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	for _, pos := range []int{12, 13} {
		p := pesos[len(pesos)-pos:]
		soma := 0
		for i := 0; i < pos; i++ {
			soma += int(d[i]-'0') * p[i]
		}
		dv := soma % 11
		if dv < 2 {
			dv = 0
		} else {
			dv = 11 - dv
		}
		if dv != int(d[pos]-'0') {
			return false
		}
	}
	return true
}

func todosIguais(d string) bool {
	for i := 1; i < len(d); i++ {
		if d[i] != d[0] {
			return false
		}
	}
	return true
}

func alfanumerico(s string) bool {
	for _, r := range s {
		ehLetra := r >= 'A' && r <= 'Z'
		ehDigito := r >= '0' && r <= '9'
		if !ehLetra && !ehDigito {
			return false
		}
	}
	return true
}
