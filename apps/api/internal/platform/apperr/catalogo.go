package apperr

import (
	"fmt"
	"net/http"
	"sort"
)

// O catálogo de erros do contrato — o ÚNICO lugar onde um código nasce.
//
// Por que um lugar só (F2-03, dívida D3): até a Fase 2, 17 dos 37 códigos eram
// montados dentro dos módulos, cada um com a sua função local (`erro`,
// `conflito`, `invalido`) e a sua frase — RESOURCE_IN_USE existia em cinco
// lugares com quatro frases. Frase diferente é cosmético; o perigo era o
// STATUS: nada impedia a sexta cópia de sair 422 onde as outras saem 409, e o
// painel, que reage ao code, escolheria a tela errada para a mesma situação.
//
// As três partes da garantia:
//   - cada código tem UMA constante abaixo, e o literal só existe nela;
//   - `definir` recusa (pânico na carga do pacote) o mesmo código duas vezes,
//     então um segundo status para o mesmo code não chega a subir;
//   - `catalogo_test.go` confere os 37 contra o enum da OpenAPI e varre
//     internal/ atrás de literal de código, `apperr.Error{…}` ou `.Code =`
//     fora deste pacote.
//
// `definir` é privado de propósito: um construtor público seria exatamente a
// porta por onde um módulo voltaria a declarar código próprio.

// Códigos do contrato, na ordem do enum de `components.responses.Erro`.
const (
	CodeValidationError           = "VALIDATION_ERROR"
	CodeUnauthorized              = "UNAUTHORIZED"
	CodeForbidden                 = "FORBIDDEN"
	CodeNotFound                  = "NOT_FOUND"
	CodeDateConflict              = "DATE_CONFLICT"
	CodeMinStayNotMet             = "MIN_STAY_NOT_MET"
	CodeCapacityExceeded          = "CAPACITY_EXCEEDED"
	CodeDiscountAboveLimit        = "DISCOUNT_ABOVE_LIMIT"
	CodeHoldExpired               = "HOLD_EXPIRED"
	CodeIdempotencyMismatch       = "IDEMPOTENCY_MISMATCH"
	CodeRateLimited               = "RATE_LIMITED"
	CodeInvalidCredentials        = "INVALID_CREDENTIALS"
	CodeTokenInvalid              = "TOKEN_INVALID"
	CodeTokenReused               = "TOKEN_REUSED"
	CodeEmailInUse                = "EMAIL_IN_USE"
	CodeRoleImmutable             = "ROLE_IMMUTABLE"
	CodeRoleInUse                 = "ROLE_IN_USE"
	CodeRateNotFound              = "RATE_NOT_FOUND"
	CodePolicyImmutable           = "POLICY_IMMUTABLE"
	CodeCodeInUse                 = "CODE_IN_USE"
	CodeResourceInUse             = "RESOURCE_IN_USE"
	CodeInvalidStateTransition    = "INVALID_STATE_TRANSITION"
	CodeReservationNotCancellable = "RESERVATION_NOT_CANCELLABLE"
	CodeUnitNotAvailable          = "UNIT_NOT_AVAILABLE"
	CodeCompositionIncomplete     = "COMPOSITION_INCOMPLETE"
	CodeHoldLimitReached          = "HOLD_LIMIT_REACHED"
	CodeStageNotInPipeline        = "STAGE_NOT_IN_PIPELINE"
	CodeStageOrderIncomplete      = "STAGE_ORDER_INCOMPLETE"
	CodeDefaultPipelineRequired   = "DEFAULT_PIPELINE_REQUIRED"
	CodeOpportunityAlreadyClosed  = "OPPORTUNITY_ALREADY_CLOSED"
	CodeLossReasonRequired        = "LOSS_REASON_REQUIRED"
	CodeQuoteRequiredToWin        = "QUOTE_REQUIRED_TO_WIN"
	CodeLeadAlreadyConverted      = "LEAD_ALREADY_CONVERTED"
	CodeContactDuplicate          = "CONTACT_DUPLICATE"
	CodeContactAnonymized         = "CONTACT_ANONYMIZED"
	CodeQuoteNotPending           = "QUOTE_NOT_PENDING"
	CodeCountAlreadyOpen          = "COUNT_ALREADY_OPEN"
	CodeCountClosed               = "COUNT_CLOSED"
	CodeCountHasPendingLines      = "COUNT_HAS_PENDING_LINES"
	CodeMaintenanceOrderClosed    = "MAINTENANCE_ORDER_CLOSED"
	CodeMaintenanceOrderOpen      = "MAINTENANCE_ORDER_ALREADY_OPEN"
	CodeInternal                  = "INTERNAL"
)

// catalogo é código → erro base. Serve `PorCodigo` (a tradução do erro do
// domínio, que chega como texto) e o teste de espelho com o contrato.
var catalogo = map[string]*Error{}

// definir registra o erro base de um código. Chamado só nas declarações deste
// arquivo, na carga do pacote.
func definir(code, msg string, status int) *Error {
	if _, repetido := catalogo[code]; repetido {
		panic(fmt.Sprintf("apperr: código %s definido duas vezes; cada código tem um status e uma mensagem padrão", code))
	}
	e := &Error{Code: code, Message: msg, status: status}
	catalogo[code] = e
	return e
}

// PorCodigo devolve o erro base de um código do contrato.
//
// Existe para a fronteira com `internal/domain`: o motor comercial é puro e
// carimba a violação com o code em texto (`booking.RuleError`). Quem traduz
// pega o status DAQUI, e não de uma tabela própria — uma segunda tabela
// divergiria desta no dia em que só uma fosse ajustada.
//
// O erro devolvido é compartilhado: personalize sempre por With*, que copiam.
func PorCodigo(code string) (*Error, bool) {
	e, ok := catalogo[code]
	return e, ok
}

// Codigos lista, em ordem alfabética, todos os códigos do catálogo.
func Codigos() []string {
	codigos := make([]string, 0, len(catalogo))
	for c := range catalogo {
		codigos = append(codigos, c)
	}
	sort.Strings(codigos)
	return codigos
}

// Erros de plataforma.
var (
	Internal     = definir(CodeInternal, "Erro interno.", http.StatusInternalServerError)
	Unauthorized = definir(CodeUnauthorized, "Autenticação necessária.", http.StatusUnauthorized)
	Forbidden    = definir(CodeForbidden, "Você não tem permissão para isso.", http.StatusForbidden)
	RateLimited  = definir(CodeRateLimited, "Muitas requisições. Tente em instantes.", http.StatusTooManyRequests)

	// Bases de NotFound(recurso) e Validation(detalhes), que são as duas
	// formas públicas destes códigos: a frase do 404 nomeia o recurso, e o 422
	// de campo sempre leva o mapa de campos.
	naoEncontrado = definir(CodeNotFound, "Registro não encontrado.", http.StatusNotFound)
	validacao     = definir(CodeValidationError, "Dados inválidos.", http.StatusUnprocessableEntity)
)

// Erros de identidade e acesso.
//
// InvalidCredentials é deliberadamente o MESMO erro para e-mail inexistente,
// senha errada e e-mail bloqueado por tentativas. Um código próprio para
// "bloqueado" confirmaria que a conta existe — vira oráculo de enumeração.
var (
	InvalidCredentials = definir(CodeInvalidCredentials, "E-mail ou senha inválidos.", http.StatusUnauthorized)
	TokenInvalid       = definir(CodeTokenInvalid, "Token ausente, expirado ou desconhecido.", http.StatusUnauthorized)
	TokenReused        = definir(CodeTokenReused, "Sessão comprometida: faça login novamente.", http.StatusUnauthorized)
	EmailInUse         = definir(CodeEmailInUse, "E-mail já cadastrado.", http.StatusConflict)
	RoleImmutable      = definir(CodeRoleImmutable, "Perfil de sistema não pode ser alterado nem excluído.", http.StatusConflict)
	RoleInUse          = definir(CodeRoleInUse, "Ainda há usuários neste perfil.", http.StatusConflict)
)

// Erros do motor comercial (venda, orçamento, estadia).
var (
	DateConflict        = definir(CodeDateConflict, "As datas selecionadas acabaram de ser ocupadas.", http.StatusConflict)
	MinStayNotMet       = definir(CodeMinStayNotMet, "Estadia abaixo do mínimo do período.", http.StatusUnprocessableEntity)
	CapacityExceeded    = definir(CodeCapacityExceeded, "Número de hóspedes acima da capacidade.", http.StatusUnprocessableEntity)
	DiscountAboveLimit  = definir(CodeDiscountAboveLimit, "Desconto acima da alçada.", http.StatusUnprocessableEntity)
	HoldExpired         = definir(CodeHoldExpired, "A pré-reserva expirou.", http.StatusConflict)
	IdempotencyMismatch = definir(CodeIdempotencyMismatch, "Chave de idempotência reutilizada com corpo diferente.", http.StatusConflict)

	// RateNotFound — noite sem tarifa cadastrada. Quem carimba é
	// `booking.Build`; a API só o devolve pela tradução de PorCodigo. 422 e
	// não 404: o recurso pedido (o orçamento) existe como pedido; falta
	// configuração do tarifário.
	RateNotFound = definir(CodeRateNotFound, "Não há tarifa cadastrada para uma das noites.", http.StatusUnprocessableEntity)

	// CompositionIncomplete — o produto não tem, ATIVAS, todas as unidades
	// que a composição declara.
	//
	// POR QUE 422 E NÃO 409: no 409 a data está em disputa, e a ação do
	// operador é tentar outra data. Aqui não há disputa nenhuma — a White House
	// Completa vende oito apartamentos e um deles está em manutenção. NENHUMA
	// data resolve; o que falta é configuração de inventário, e quem age é a
	// gestão. Sai da venda (`reservas`), da cotação (`disponibilidade`) e da
	// constraint adiada `reservation_units_composicao_completa` (`db.MapError`)
	// com o mesmo code e o mesmo status.
	CompositionIncomplete = definir(CodeCompositionIncomplete, "O produto não tem todas as unidades da composição ativas.", http.StatusUnprocessableEntity)

	// QuoteNotPending — o orçamento apontado venceu (`valid_until` no
	// passado) ou já virou venda (`reservation_id` preenchido). 422 e não 409:
	// nenhuma repetição resolve, e o caminho de saída é emitir OUTRO.
	QuoteNotPending = definir(CodeQuoteNotPending, "Este orçamento não está mais de pé: emita outro.", http.StatusUnprocessableEntity)
)

// Erros de cadastro: chave natural, vínculo e versão publicada.
var (
	// CodeInUse — violação de chave natural (`UNIQUE`): código de unidade na
	// propriedade, nome de tabela de tarifas, data de feriado, nome de funil,
	// etapa ou motivo. Quem decide é a constraint, traduzindo o 23505: um
	// SELECT antes do INSERT perderia a corrida.
	CodeInUse = definir(CodeCodeInUse, "Já existe um registro com esta chave.", http.StatusConflict)

	// ResourceInUse — o irmão genérico do ROLE_IN_USE: recusa apagar ou
	// desativar o que ainda sustenta alguma coisa. As telas que precisam dizer
	// O QUÊ trocam a frase e põem a contagem em `details`.
	ResourceInUse = definir(CodeResourceInUse, "Ainda há vínculo ativo neste registro.", http.StatusConflict)

	// PolicyImmutable — versão publicada da política não se reescreve nem se
	// antedata.
	PolicyImmutable = definir(CodePolicyImmutable, "Política já publicada não pode ser reescrita nem antedatada.", http.StatusConflict)
)

// Erros de máquina de estados (reservas, bloqueios, CRM).
var (
	// InvalidStateTransition — transição fora da máquina de estados.
	// `details.status` traz o estado atual e, quando há, `details.allowed`
	// os aceitos.
	InvalidStateTransition = definir(CodeInvalidStateTransition, "Transição de estado não permitida.", http.StatusConflict)

	// ReservationNotCancellable — /cancel sobre reserva já encerrada.
	ReservationNotCancellable = definir(CodeReservationNotCancellable, "Esta reserva já foi encerrada e não pode ser cancelada.", http.StatusConflict)

	// UnitNotAvailable — /reassign-unit para unidade ocupada. NÃO é
	// DATE_CONFLICT de propósito: a estadia não está em disputa, só a unidade
	// — e a reserva continua exatamente onde estava.
	UnitNotAvailable = definir(CodeUnitNotAvailable, "A unidade de destino está ocupada nesse período.", http.StatusConflict)

	// HoldLimitReached — /extend-hold além do limite da política.
	HoldLimitReached = definir(CodeHoldLimitReached, "Limite de extensões da pré-reserva atingido.", http.StatusConflict)
)

// Erros do funil comercial (CRM).
var (
	// StageNotInPipeline — a etapa citada é de outro funil. Aceitar moveria a
	// oportunidade de funil junto com o card, sem histórico e sem coluna.
	StageNotInPipeline = definir(CodeStageNotInPipeline, "A etapa informada não pertence a este funil.", http.StatusUnprocessableEntity)

	// StageOrderIncomplete — `reorder` sem listar todas as etapas do funil.
	// Aceitar deixaria as ausentes com a posição de antes, e o kanban
	// desenharia duas colunas na mesma casa.
	StageOrderIncomplete = definir(CodeStageOrderIncomplete, "A ordem precisa listar todas as etapas do funil.", http.StatusUnprocessableEntity)

	// DefaultPipelineRequired — tirar o `is_default` (ou desativar) o último
	// funil padrão. Sem padrão, `POST /crm/opportunities` sem `pipeline_id`
	// não tem onde cair.
	DefaultPipelineRequired = definir(CodeDefaultPipelineRequired, "É preciso haver um funil padrão ativo. Marque outro antes de desmarcar este.", http.StatusConflict)

	// OpportunityAlreadyClosed — o que está fechado é história.
	OpportunityAlreadyClosed = definir(CodeOpportunityAlreadyClosed, "Oportunidade já ganha ou perdida não é mais editável.", http.StatusConflict)

	// LossReasonRequired — `/lose` sem motivo, ou com motivo inativo. É a
	// regra que faz o relatório de motivos de perda existir.
	LossReasonRequired = definir(CodeLossReasonRequired, "Informe um motivo de perda ativo do catálogo.", http.StatusUnprocessableEntity)

	// QuoteRequiredToWin — `/win` sem orçamento vigente. Sem preço congelado
	// não há o que virar reserva.
	QuoteRequiredToWin = definir(CodeQuoteRequiredToWin, "Não há orçamento vigente para transformar em reserva.", http.StatusUnprocessableEntity)

	// LeadAlreadyConverted — `/convert` repetido. `details.opportunity_id`
	// leva o segundo clique ao card certo.
	LeadAlreadyConverted = definir(CodeLeadAlreadyConverted, "Este lead já virou oportunidade.", http.StatusConflict)
)

// Erros de contato (LGPD e unicidade).
var (
	// ContactDuplicate — já existe alguém com este telefone ou documento.
	// NUNCA sai seco: `details.contact_id` traz o registro que já existe.
	ContactDuplicate = definir(CodeContactDuplicate, "Já existe um contato com este telefone ou documento.", http.StatusConflict)

	// ContactAnonymized — o titular exerceu o direito de eliminação: a ficha
	// não recebe dado pessoal novo nem proposta.
	ContactAnonymized = definir(CodeContactAnonymized, "Este contato foi anonimizado e não aceita mais dados pessoais.", http.StatusConflict)
)

// Erros da conferência de inventário de bens (20261007170000, docs/db.md §11).
//
// Os três nasceram junto com o contrato das rotas de `/inventory/*`
// (`internal/router/rotas_inventario_bens.go`) e **antes** do módulo que os
// emite, de propósito: o enum da OpenAPI e o literal em Go têm de entrar no
// mesmo commit, senão `contrato_de_erros_test.go` reprova de um lado ou do
// outro — e o sintoma em produção é o painel recebendo um `code` que ele
// traduz para "erro interno".
var (
	// CountAlreadyOpen — a unidade já tem conferência aberta. Decidido pelo
	// índice único PARCIAL `inventory_counts_aberta_idx`
	// (`UNIQUE (unit_id) WHERE status = 'aberta'`), traduzindo o `23505`:
	// `SELECT` antes do `INSERT` é TOCTOU e perde a corrida entre dois
	// funcionários abrindo a contagem do AP-01 no mesmo plantão — cada um
	// contaria metade e o fechamento de um sobrescreveria o do outro.
	//
	// `details.count_id` e `details.opened_at` existem para o segundo toque
	// levar à conferência que já está aberta, em vez de virar beco. Sai também
	// de `POST /units/{id}/inventory/copy`: copiar ambiente ou colocação com a
	// contagem em curso criaria bem sem linha congelada, e a conferência
	// fecharia "completa" sem nunca ter olhado para ele.
	CountAlreadyOpen = definir(CodeCountAlreadyOpen,
		"Esta unidade já tem uma conferência aberta. Termine ou cancele a que está em andamento.",
		http.StatusConflict)

	// CountClosed — conferência `fechada` ou `cancelada` não aceita contagem,
	// edição, novo fechamento nem cancelamento. O que está encerrado é
	// história: recontar mudaria em silêncio uma divergência que já virou
	// pendência (e talvez cobrança), e apagar tiraria o lastro dela —
	// `inventory_issues.count_id` é `ON DELETE RESTRICT` justamente por isso.
	// `details.status` e `details.closed_at`.
	CountClosed = definir(CodeCountClosed,
		"Esta conferência já foi encerrada: para contar de novo, abra uma nova conferência.",
		http.StatusConflict)

	// CountHasPendingLines — `/close` com linha sem contagem
	// (`counted_qty IS NULL`). Fechar assim produziria divergência falsa:
	// "esperava 12, contou nada" fica indistinguível de "esperava 12, não achei
	// nenhum" no relatório, e a segunda é uma perda que alguém vai cobrar de um
	// hóspede. Quem não vai terminar CANCELA, que é outro fato.
	// `details.pending` e `details.pending_by_room`.
	CountHasPendingLines = definir(CodeCountHasPendingLines,
		"Ainda há itens sem contagem nesta conferência. Conte o que falta ou cancele a conferência.",
		http.StatusConflict)
)

// Erros da ordem de manutenção (spec §12, `docs/db.md` §11).
//
// Nasceram com o contrato de `/maintenance-orders` e antes do módulo que os
// emite, pelo mesmo motivo dos três da conferência: o enum da OpenAPI e o
// literal em Go entram no mesmo commit, ou `contrato_de_erros_test.go` reprova.
var (
	// MaintenanceOrderClosed — escrita numa ordem `concluida` ou `cancelada`
	// que o estado não aceita: transição (`/start`, `/complete`, `DELETE`),
	// edição de qualquer campo que não o custo, e qualquer mexida no
	// bloqueio. Encerrada não reabre: retrabalho é ordem nova.
	//
	// Quem carimba é `internal/domain/maintenance` (Next e CheckEdit), com
	// `details.status` e `details.editable` (`so_custo` na concluída, `nada`
	// na cancelada) — a tela usa o segundo para explicar por que o campo
	// travou. O módulo acrescenta `details.closed_at`.
	MaintenanceOrderClosed = definir(CodeMaintenanceOrderClosed,
		"Esta ordem de manutenção já foi encerrada: retrabalho é uma ordem nova.",
		http.StatusConflict)

	// MaintenanceOrderOpen — a avaria citada já tem uma ordem aberta ou em
	// andamento. Decidido pelo índice único PARCIAL sobre `issue_id` das
	// ordens não encerradas, traduzindo o `23505` — o segundo toque em "Abrir
	// ordem de manutenção" no celular é a corrida que um SELECT antes do
	// INSERT perderia. `details.maintenance_order_id` leva à ordem que já
	// existe, para o toque repetido virar navegação em vez de beco.
	MaintenanceOrderOpen = definir(CodeMaintenanceOrderOpen,
		"Esta avaria já tem uma ordem de manutenção em aberto.",
		http.StatusConflict)
)
