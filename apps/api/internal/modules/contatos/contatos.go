// Package contatos implementa o cadastro de pessoas — o registro único de que
// leads, hóspedes, corretores e proprietários dependem (spec §6).
//
// # Por que este módulo existiu tarde demais
//
// `POST /reservations` e `POST /crm/opportunities` exigem `contact_id`
// obrigatório, e até 27/08/2026 NÃO HAVIA rota para obter um. Medido no stack
// no ar, com token de `admin@wh.local`:
//
//	GET  /api/v1/contacts      → 404 NOT_FOUND
//	POST /api/v1/contacts      → 404 NOT_FOUND
//	GET  /api/v1/contacts/{id} → 404 NOT_FOUND
//	SELECT count(*) FROM contacts → 1   (a linha que o seed insere)
//
// Ou seja: vender pela API era impossível, e a suíte inteira ficava verde
// porque todo teste criava o contato por INSERT de fixture. O defeito não era
// de código — era de superfície ausente, e por isso nenhum teste de código o
// pegava.
//
// # UMA PESSOA, UM REGISTRO
//
// Não existe "cadastro de hóspede" ao lado do "cadastro de lead". Duas fichas
// da mesma pessoa significam que, no dia em que ela trocar de telefone, só uma
// é corrigida — e a outra continua ligada a uma reserva viva. A garantia de
// unicidade é do BANCO no eixo do telefone (`contacts_phone_idx`, UNIQUE
// parcial) e de uma trava de transação no eixo do documento, enquanto o índice
// único de documento não existir (ver repository.go).
//
// # DADO PESSOAL TEM DONO, E O DONO É O TITULAR
//
// Três consequências que atravessam o módulo inteiro:
//
//  1. LEITURA de ficha individual grava `pii_access_log` (spec §16). A lista
//     não grava: ela é a agenda do dia; a ficha é dado identificável.
//  2. `POST /contacts/{id}/anonymize` atende ao direito de eliminação (LGPD
//     art. 18, VI) SEM apagar o razão: o `id` e as FKs sobrevivem, a PII não.
//  3. `GET /contacts/{id}/export` atende à portabilidade (art. 18, V).
//
// E uma quarta, menos óbvia e igualmente obrigatória: a TRILHA DE AUDITORIA
// NÃO GUARDA PII. `audit_log` registra que o nome mudou, nunca de que nome
// para que nome — senão anonimizar a ficha deixaria a cópia do dado eliminado
// numa tabela que meio time consegue ler. Ver auditoria.go.
//
// # Escopo `own`
//
// `contacts` é o único recurso do grupo Comercial com `supports_own = false`
// no catálogo: a tabela NÃO TEM coluna de dono. Escopo `own` concedido a este
// recurso é, portanto, dado corrompido — e a resposta é negar, não servir tudo
// (ver Servico.exigirEscopoAplicavel).
package contatos

import (
	"net/http"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

// RecursoContatos é o código do catálogo de RBAC (cmd/seed/acesso.go:64).
const RecursoContatos = "contacts"

// Entidade é o nome usado em `audit_log.entity` e `pii_access_log`. É o nome da
// TABELA, e não "contato", para a tela de auditoria conseguir filtrar por
// entidade sem tradução no meio.
const Entidade = "contacts"

// Verbos de `audit_log.action`, além dos três genéricos de `audit`.
const (
	VerboAnonimizado = "anonimizado"
	VerboExportado   = "exportado"
)

// Bases legais da LGPD (art. 7) que sustentam GUARDAR a ficha. Não se confundem
// com o consentimento de MARKETING: `contrato` legitima guardar o cadastro de
// quem se hospedou e não legitima mandar oferta para ele.
const (
	BaseConsentimento     = "consentimento"
	BaseContrato          = "contrato"
	BaseObrigacaoLegal    = "obrigacao_legal"
	BaseLegitimoInteresse = "legitimo_interesse"
)

// BasesLegais é o enum fechado do contrato (schema BaseLegalLGPD).
var BasesLegais = []string{BaseConsentimento, BaseContrato, BaseObrigacaoLegal, BaseLegitimoInteresse}

// Tipos de documento (schema TipoDeDocumento).
const (
	DocCPF        = "cpf"
	DocCNPJ       = "cnpj"
	DocPassaporte = "passaporte"
)

// TiposDeDocumento é o enum fechado do contrato.
var TiposDeDocumento = []string{DocCPF, DocCNPJ, DocPassaporte}

// Status de reserva que impedem a anonimização: a pessoa está hospedada hoje,
// ou está prestes a chegar. Enquanto o contrato corre, a base legal é a
// EXECUÇÃO DO CONTRATO — e a operação precisa do nome para entregar a chave.
var statusDeReservaViva = []string{"hold", "confirmed", "checked_in"}

// ─────────────────────────── Erros do módulo ────────────────────────
//
// Os três códigos abaixo pertencem ao vocabulário estável da API e o enum do
// contrato já os lista (`components.responses.Erro`). Estão declarados AQUI, e
// não em `internal/platform/apperr`, pela mesma razão registrada em
// `internal/modules/crm/crm.go`: `internal/platform` não é pasta deste agente
// nesta rodada, e editar o mesmo arquivo que outro agente edita em paralelo é
// exatamente a colisão que a divisão por pasta existe para evitar. São
// construídos pela mesma forma do pacote (código + mensagem + status), então
// mudam de casa sem mudar de comportamento.
//
// Ver "PARA O INTEGRADOR" no relatório.
func erro(codigo, mensagem string, status int) *apperr.Error {
	return (&apperr.Error{Code: codigo, Message: mensagem}).WithStatus(status)
}

var (
	// ContatoDuplicado — já existe alguém com este telefone ou documento.
	//
	// NUNCA sai seco: `details.contact_id` traz o registro que já existe, e é
	// isso que transforma o erro em caminho. Quem tenta cadastrar de novo quer
	// justamente CHEGAR na pessoa; um 409 sem o id manda o atendente procurar
	// na lista o que a API acabou de encontrar para ele.
	ContatoDuplicado = erro("CONTACT_DUPLICATE",
		"Já existe um contato com este telefone ou documento.", http.StatusConflict)

	// ContatoAnonimizado — ficha esvaziada não volta a receber dado pessoal.
	// Aceitar a escrita reintroduziria na base o dado que o titular mandou
	// eliminar, com a agravante de o `anonymized_at` continuar preenchido —
	// a ficha diria "eliminada" carregando PII nova.
	ContatoAnonimizado = erro("CONTACT_ANONYMIZED",
		"Este contato foi anonimizado e não aceita mais dados pessoais.", http.StatusConflict)

	// RecursoEmUso reusa o vocabulário que inventário, tarifário e CRM já usam
	// para "ainda há algo apontando para isto", em vez de inflar o enum com um
	// código novo por módulo.
	RecursoEmUso = erro("RESOURCE_IN_USE",
		"Este contato tem registros vinculados.", http.StatusConflict)
)

// escopoOwnInaplicavel é a recusa quando a matriz concede `own` sobre
// `contacts`.
//
// A tabela não tem coluna de dono: `own` não tem como virar
// `AND owner_id = $usuario` no SQL. Sobram duas saídas, e a segunda é a única
// defensável:
//
//   - servir TUDO, tratando `own` como `all` — o administrador pediu restrição
//     e recebeu a base inteira de dados pessoais, em silêncio;
//   - negar, dizendo o porquê.
//
// Fecha-se a porta. `supports_own = false` no catálogo faz a tela de perfis nem
// oferecer a opção, então na prática isto só dispara com linha inserida por SQL
// — que é dado corrompido, e a regra do `auth.NovoConjunto` para escopo
// desconhecido já é a mesma: ignorar significa negar.
func escopoOwnInaplicavel(acao string) *apperr.Error {
	return apperr.Forbidden.
		WithMessage("O recurso `contacts` não tem dono: o escopo `own` não se aplica a ele. " +
			"Conceda `all` na matriz de perfis (o catálogo marca contacts com supports_own = false).").
		WithDetails(map[string]string{
			"resource": RecursoContatos,
			"action":   acao,
			"scope":    "own",
		})
}
