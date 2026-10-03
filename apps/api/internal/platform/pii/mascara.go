package pii

import (
	"strings"
	"unicode/utf8"
)

// As máscaras de COLEÇÃO (F2-23; contrato: "Dado pessoal na resposta").
//
// Coleção não serve documento, telefone nem e-mail cheios: até 02/10/2026
// `GET /contacts?per_page=100` devolvia 11 CPFs completos a um corretor, e
// `pii_access_log` ficava parado (43 → 43). A regra fechada no contrato é
// "coleção mascara; leitura de UM registro com valor cheio registra". Estas
// funções são a primeira metade e moram na plataforma porque recebível,
// comissão e lead embutem as mesmas pessoas — uma máscara por módulo
// divergiria no primeiro ajuste de formato.
//
// São puras e totais: `nil` devolve `nil` (a máscara não inventa dado) e
// entrada fora do formato esperado cai na forma genérica, nunca no valor cheio.
// Falhar aberto aqui seria servir o CPF justamente do cadastro malformado.

// Tipos de documento do contrato (schema TipoDeDocumento). Repetidos aqui em
// vez de importados de `contatos` porque plataforma não importa módulo.
const (
	tipoCPF  = "cpf"
	tipoCNPJ = "cnpj"
)

// MascararDocumento aplica a máscara pela forma do `doc_type`:
//
//	cpf  com 11 dígitos  12345677735     → ***.***.777-35
//	cnpj com 14 dígitos  11222333000181  → **.***.***/0001-81
//	qualquer outro caso  FX1234567       → ******567   (3 ou menos → ***)
//
// "Qualquer outro caso" inclui o CPF com tamanho errado e o tipo nulo: a
// forma bonita só sai quando o valor tem a forma que ela promete.
func MascararDocumento(tipo, numero *string) *string {
	if numero == nil {
		return nil
	}
	n := *numero
	t := ""
	if tipo != nil {
		t = *tipo
	}
	var out string
	switch {
	case t == tipoCPF && len(n) == 11 && soDigitos(n):
		out = "***.***." + n[6:9] + "-" + n[9:11]
	case t == tipoCNPJ && len(n) == 14 && soDigitos(n):
		out = "**.***.***/" + n[8:12] + "-" + n[12:14]
	default:
		out = ultimosVisiveis(n, 3)
	}
	return &out
}

// MascararTelefone devolve `+` e um `*` por dígito, menos os 4 últimos:
// `+5585999990000` → `+*********0000`. Só os dígitos contam — pontuação não
// é gravada (o cadastro guarda E.164), e se viesse não deveria sobreviver à
// máscara como pista do formato original.
//
// O resultado NÃO é E.164: não serve para `tel:` — é essa a intenção.
func MascararTelefone(telefone *string) *string {
	if telefone == nil {
		return nil
	}
	var digitos strings.Builder
	for _, r := range *telefone {
		if r >= '0' && r <= '9' {
			digitos.WriteRune(r)
		}
	}
	d := digitos.String()
	visiveis := 4
	if len(d) <= visiveis {
		// Telefone com 4 dígitos ou menos não existe no cadastro (o validador
		// exige E.164). Se aparecer, sai inteiro coberto: mostrar "os 4
		// últimos" seria mostrar tudo.
		out := "+" + strings.Repeat("*", len(d))
		return &out
	}
	out := "+" + strings.Repeat("*", len(d)-visiveis) + d[len(d)-visiveis:]
	return &out
}

// MascararEmail mantém o primeiro caractere antes do `@` e o domínio inteiro:
// `fernanda.lima@gmail.com` → `f***@gmail.com`. Sem `@`, `***`.
//
// O domínio fica porque distingue `@gmail.com` de endereço corporativo sem
// identificar ninguém sozinho; o tamanho do nome NÃO fica (são sempre três
// `*`), porque "f" + 12 asteriscos já estreita a busca.
func MascararEmail(email *string) *string {
	if email == nil {
		return nil
	}
	e := *email
	arroba := strings.LastIndex(e, "@")
	if arroba < 0 {
		out := "***"
		return &out
	}
	primeiro := ""
	if arroba > 0 {
		r, _ := utf8.DecodeRuneInString(e)
		primeiro = string(r)
	}
	out := primeiro + "***" + e[arroba:]
	return &out
}

// ultimosVisiveis cobre com `*` todo caractere menos os `n` últimos. Conta
// RUNAS, não bytes: um passaporte com letra acentuada não pode ser cortado no
// meio de um caractere.
func ultimosVisiveis(s string, n int) string {
	runas := []rune(s)
	if len(runas) <= n {
		return "***"
	}
	return strings.Repeat("*", len(runas)-n) + string(runas[len(runas)-n:])
}

func soDigitos(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}
