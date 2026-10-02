package pii

import (
	"strings"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
)

// camposPessoais é o conjunto de campos cujo VALOR nunca entra numa trilha.
//
// # Por que `audit.Redigir` não resolve isto
//
// O filtro de `audit` protege SEGREDO: são 18 substrings (`senha`, `token`,
// `hash`, `cvv`, `authorization`…) e — medido — NENHUMA delas casa com um campo
// de pessoa. `audit.Sensivel("name")`, `("email")`, `("phone_e164")`,
// `("doc_number")` e `("birth_date")` devolvem todas `false`, e é correto que
// devolvam: numa unidade ou numa reserva esses valores são justamente o que a
// auditoria precisa mostrar. Entregar um contato a `audit.Redigir` publica a
// pessoa inteira em `audit_log`.
//
// # O que se perde quando este filtro falha
//
// `POST /contacts/{id}/anonymize` promete, textualmente, que "audit_log e
// pii_access_log guardam contact_id — não o nome". Se a edição da ficha
// gravasse `{"name":"Maria Silva"}` em `before`, anonimizar deixaria a cópia do
// dado eliminado numa tabela que todo perfil com `audit:ver` consegue ler: o
// direito de eliminação cumprido na tabela que o titular vê e descumprido na
// que ele não vê — a pior forma de descumprir.
//
// `doc_type`, `city`, `state`, `lgpd_basis`, `marketing_opt_in`, `consent_at` e
// `anonymized_at` NÃO entram: não identificam ninguém sozinhos e são exatamente
// o que uma fiscalização pergunta ("com que base legal esta ficha existia?",
// "quando o opt-in foi ligado?").
var camposPessoais = map[string]bool{
	"name":       true,
	"email":      true,
	"phone_e164": true,
	"doc_number": true,
	"birth_date": true,
	// `notes` é texto livre digitado pelo atendimento sobre a pessoa. Não é
	// campo de identificação, mas é onde "ela pediu para não ligar depois das
	// 18h, mora com a mãe" acaba escrito.
	"notes": true,
}

// Sensivel diz se o NOME do campo carrega dado pessoal.
//
// Casa o nome exato e também o nome COMPOSTO terminado nele (`guest_name`,
// `holder_doc_number`, `contact_email`) — que é como a rooming list e o
// financeiro vão chamar os mesmos campos quando a pessoa não for o assunto
// principal do documento. Sem o sufixo, o filtro protegeria a ficha de contato
// e falharia aberta justamente nas telas para as quais este pacote existe.
//
// A troca aceita, que é a mesma de `audit.Sensivel`: campo inocente terminado
// num destes nomes (`unit_name` dentro de um documento de reserva) é mascarado
// por engano. Perder um campo da trilha é reversível — renomeia-se o campo ou
// monta-se o documento sem ele; vazar o nome do hóspede não é. E a assimetria
// só custa onde alguém DECIDIU chamar esta função: diferente de `audit.Redigir`,
// que roda em toda escrita, aqui quem chama já declarou que o documento
// descreve gente.
func Sensivel(nome string) bool {
	n := strings.ToLower(strings.TrimSpace(nome))
	if camposPessoais[n] {
		return true
	}
	for campo := range camposPessoais {
		if strings.HasSuffix(n, "_"+campo) {
			return true
		}
	}
	return false
}

// Redigir devolve uma CÓPIA do documento com o valor pessoal trocado pela marca
// de redação, PRESERVANDO a informação de que campo mudou.
//
// Reusa a descida de `audit` de propósito: o vazamento que interessa está no
// nível aninhado (`{"guests":[{"name":…}]}`), e uma segunda descida escrita
// aqui seria a segunda chance de esquecer de entrar numa lista.
//
// Ordem de uso, quando o documento vem de uma edição: recorte primeiro
// (`audit.Diff` sobre os valores REAIS), máscara depois. Mascarar antes faria
// os dois lados virarem a mesma marca, o Diff os consideraria iguais e a troca
// de nome sumiria da trilha — auditoria que perde justamente a alteração que
// ela existe para registrar.
func Redigir(campos audit.Campos) audit.Campos {
	return audit.RedigirCom(campos, Sensivel)
}
