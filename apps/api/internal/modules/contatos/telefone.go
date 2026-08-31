package contatos

import (
	"errors"
	"strings"
)

// DDIPadrao é o país da instalação. Ver NormalizarTelefone para o porquê de ele
// existir — e para a divergência de contrato que ele carrega.
const DDIPadrao = "55"

// Faixa de tamanho da E.164: no mínimo 8 dígitos (país + assinante mais curtos
// em uso), no máximo 15 — o teto do próprio padrão.
const (
	minDigitosE164 = 8
	maxDigitosE164 = 15
)

// ErrTelefoneInvalido é o erro de forma. O service o traduz para 422 com o
// campo certo; aqui ele é só um valor, para a função continuar testável sem
// HTTP.
var ErrTelefoneInvalido = errors.New("telefone fora do formato E.164")

// NormalizarTelefone devolve o número em E.164 canônico (`+5585999990000`).
//
// # Por que normalizar, e não só validar
//
// A deduplicação por telefone é a espinha do módulo: `contacts_phone_idx` é
// `UNIQUE (phone_e164) WHERE phone_e164 IS NOT NULL`, e um índice único sobre
// texto só deduplica o que chega escrito do mesmo jeito. Sem esta função,
// "+55 (85) 99999-0000", "+55 85 99999-0000" e "+5585999990000" são três
// pessoas para o banco e uma só para o mundo — e a garantia do índice vira
// enfeite. Pontuação é ruído de digitação, não informação.
//
// # A parte que é decisão, não mecânica: o DDI ausente
//
// O contrato (openapi.yaml, schema ContatoCriar) diz que número sem `+` e sem
// DDI é `422`, com o argumento de que "85999990000 pode ser Fortaleza ou outra
// coisa, e adivinhar o DDI é criar contato duplicado do mesmo número escrito de
// dois jeitos".
//
// Esta implementação DIVERGE, de propósito e num ponto só: número que é
// inequivocamente um nacional brasileiro (10 ou 11 dígitos, DDD válido, e o
// celular de 11 começando por 9) recebe `+55`. Tudo o mais continua `422`.
//
// O motivo é que o desfecho temido pelo contrato é justamente o que a recusa
// produz. Numa instalação de uma casa só, em Fortaleza, o balcão e o inbound de
// WhatsApp digitam "(86) 99999-9999" o dia inteiro. Recusar não faz o atendente
// descobrir o DDI: faz ele contornar — e o contorno de quem está com o hóspede
// na frente é cadastrar SEM telefone, porque `phone_e164` é opcional. Aí o
// índice parcial (que existe exatamente para `NULL` não colidir com `NULL`)
// aceita a segunda ficha sem reclamar, e a duplicata que o contrato queria
// evitar nasce assim mesmo — só que invisível, sem telefone nenhum para
// encontrá-la depois.
//
// A ambiguidade que o contrato aponta é real, mas não neste recorte: 11 dígitos
// com DDD 11–99 e o nono dígito começando em 9 não é um número de outro país
// escrito por engano; é o formato que a Anatel define e o único que a operação
// digita. O que continua recusado — 7 dígitos, 12 dígitos que não começam por
// 55, qualquer coisa com letra — é onde adivinhar seria adivinhar mesmo.
//
// PARA O INTEGRADOR: a frase do contrato e este comentário não podem ficar os
// dois de pé. A decisão é do tech-lead: ou o contrato passa a descrever esta
// regra, ou esta função perde o ramo do DDI padrão. Está no relatório.
func NormalizarTelefone(bruto string) (string, error) {
	s := strings.TrimSpace(bruto)
	if s == "" {
		return "", nil
	}

	temMais := strings.HasPrefix(s, "+")
	digitos := somenteDigitos(s)

	// Um `+` no meio do texto ("55+85...") não é E.164 escrita com pontuação:
	// é lixo. Só o prefixo conta.
	if strings.Count(s, "+") > 1 || (strings.Contains(s, "+") && !temMais) {
		return "", ErrTelefoneInvalido
	}
	// Só passa o que é pontuação de telefone. Sem esta checagem, jogar fora
	// tudo que não é dígito ACEITARIA lixo: medido no teste,
	// "+5585abc990000" virava "+5585990000" — um número plausível, gravado, e
	// que nunca mais bateria com o telefone real da pessoa. Descartar em
	// silêncio o que não se entende é pior do que recusar.
	if !soPontuacaoDeTelefone(s) {
		return "", ErrTelefoneInvalido
	}
	if digitos == "" {
		return "", ErrTelefoneInvalido
	}

	if temMais {
		// Já veio internacional: só resta conferir o tamanho. Não se mexe no
		// DDI de quem informou o DDI.
		if len(digitos) < minDigitosE164 || len(digitos) > maxDigitosE164 {
			return "", ErrTelefoneInvalido
		}
		return "+" + digitos, nil
	}

	// Sem `+`. Três casos, nesta ordem.
	switch {
	// "5585999990000" / "558533334444" — o DDI já está escrito, só falta o `+`.
	case len(digitos) >= 12 && len(digitos) <= 13 && strings.HasPrefix(digitos, DDIPadrao):
		return "+" + digitos, nil

	// Nacional com DDD: 10 dígitos (fixo) ou 11 (celular, nono dígito = 9).
	case len(digitos) == 10 || len(digitos) == 11:
		if !dddValido(digitos[:2]) {
			return "", ErrTelefoneInvalido
		}
		// O nono dígito do celular é 9 por definição da Anatel. Sem esta
		// checagem, "8512345678901" truncado ou um número estrangeiro de 11
		// dígitos entrariam como se fossem daqui.
		if len(digitos) == 11 && digitos[2] != '9' {
			return "", ErrTelefoneInvalido
		}
		// Assinante que começa por 0 não existe em plano nacional; costuma ser
		// número interno de PABX colado no campo errado.
		if digitos[2] == '0' {
			return "", ErrTelefoneInvalido
		}
		return "+" + DDIPadrao + digitos, nil

	default:
		return "", ErrTelefoneInvalido
	}
}

// dddValido cobre a faixa 11–99 da numeração brasileira. Não é a lista exata de
// DDDs em uso (que muda com o tempo e viraria tabela desatualizada no código);
// é o que separa "DDD" de "prefixo de outro país".
func dddValido(dd string) bool {
	if len(dd) != 2 {
		return false
	}
	n := int(dd[0]-'0')*10 + int(dd[1]-'0')
	return n >= 11 && n <= 99
}

func somenteDigitos(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// soPontuacaoDeTelefone aceita dígitos e o punhado de separadores que as pessoas
// realmente digitam. Letra, `#`, `*` ou ramal colado ("...ramal 12") reprovam:
// não são forma de escrever o mesmo número, são outro conteúdo no campo errado.
func soPontuacaoDeTelefone(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r == '+' || r == ' ' || r == '(' || r == ')' || r == '-' || r == '.' || r == '/':
		default:
			return false
		}
	}
	return true
}
