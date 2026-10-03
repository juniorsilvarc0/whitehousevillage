// Package reservas implementa o ciclo de vida da venda: pré-reserva, sinal,
// estadia, cancelamento, remarcação — e o bloqueio operacional do calendário.
//
// O QUE NÃO MORA AQUI: preço. Tarifa por noite, precedência de tipo de data,
// estadia mínima, desconto e alçada são calculados por internal/domain/booking
// através do módulo `disponibilidade`, que já carrega o estado comercial do
// banco e traduz os erros do motor. Recalcular qualquer centavo aqui criaria uma
// segunda verdade sobre o preço — e a reserva e o orçamento passariam a
// discordar sobre a mesma estadia.
//
// O QUE MORA AQUI: a máquina de estados, a alocação de unidade, a gravação do
// snapshot e a tradução das recusas do banco. A defesa contra overbooking NÃO é
// código deste pacote: é a constraint `stay_no_overlap`. Este pacote insere e
// traduz o `23P01` que ela devolve.
package reservas

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Recursos do catálogo de RBAC (semeados por cmd/seed/acesso.go). As constantes
// existem só para a tabela de rotas não repetir a string em dezesseis linhas —
// o catálogo continua sendo DADO (CLAUDE.md, regra 8).
const (
	Recurso           = "reservations"
	RecursoCalendario = "calendar"
)

// Consumo — os dois valores de `unit_types.consumes`.
const (
	ConsomeUmMembro     = "one_member"
	ConsomeTodosMembros = "all_members"
)

// Origens aceitas em POST /blocks. `ota` é do importador de canais (Fase 4) e
// `reservation` nasce com a reserva — nenhum dos dois se cria por aquela rota.
const (
	OrigemReserva      = "reservation"
	OrigemManutencao   = "maintenance"
	OrigemProprietario = "owner_hold"
)

// Status de `stay_blocks`. Só `hold` e `confirmed` contam para a constraint:
// é literalmente a cláusula WHERE dela.
//
// `completed` é o estado terminal que a migration 20260826120000 acrescentou. A
// revisão mediu o que faltava: o /check-out mandava os blocos para `cancelled`,
// e a estadia CUMPRIDA ficava byte a byte igual a uma venda que o hóspede
// cancelou sem nunca chegar. O mapa devolvia aqueles dias como `livre`, e toda
// taxa de ocupação calculada sobre `stay_blocks` subestimava — sempre, e sem
// deixar rastro do erro.
const (
	BlocoHold       = "hold"
	BlocoConfirmado = "confirmed"
	BlocoConcluido  = "completed"
	BlocoCancelado  = "cancelled"
	BlocoExpirado   = "expired"
)

// OS DOIS PREDICADOS. Desde que `completed` existe, "ocupa o inventário" e
// "aparece no mapa" DEIXARAM de ser a mesma pergunta — e confundir os dois é
// como o defeito volta.
//
//   - StatusQueBloqueiam é o inventário VENDÁVEL: é literalmente a cláusula
//     `WHERE` da constraint `stay_no_overlap`, e é o que decide um 409.
//   - StatusVisiveisNoMapa é o HISTÓRICO: acrescenta a estadia consumada, que
//     não impede venda nenhuma mas precisa aparecer no mapa, na ocupação, no ADR
//     e no RevPAR. Casa com o índice `stay_blocks_ocupacao_idx`.
//
// Exportados porque o módulo `disponibilidade` desenha o mapa com o MESMO
// vocabulário e não pode importar este pacote (seria ciclo — reservas já importa
// disponibilidade). Duas listas escritas à mão em pastas diferentes divergem no
// dia em que só uma for ajustada; que ao menos a daqui tenha nome e comentário.
var (
	StatusQueBloqueiam   = []string{BlocoHold, BlocoConfirmado}
	StatusVisiveisNoMapa = []string{BlocoHold, BlocoConfirmado, BlocoConcluido}
)

// blocosTerminais são os estados de onde um bloco não sai mais. `completed` é
// fato consumado (a estadia aconteceu); `cancelled` e `expired` já devolveram a
// data ao estoque. Liberar qualquer um deles de novo seria reescrever o passado.
var blocosTerminais = map[string]bool{
	BlocoConcluido: true,
	BlocoCancelado: true,
	BlocoExpirado:  true,
}

// BlocoTerminal diz se o bloco já encerrou o ciclo dele.
func BlocoTerminal(status string) bool { return blocosTerminais[status] }

// Tipos de `reservation_events`. Vocabulário aberto no contrato, fechado aqui:
// a timeline é lida por gente, e um `type` digitado errado vira linha órfã que
// nenhuma tela sabe rotular.
const (
	EventoCriada          = "created"
	EventoConfirmada      = "confirmed"
	EventoHoldEstendido   = "hold_extended"
	EventoUnidadeTrocada  = "unit_reassigned"
	EventoRemarcada       = "rescheduled"
	EventoCheckIn         = "checked_in"
	EventoCheckOut        = "checked_out"
	EventoCancelada       = "cancelled"
	EventoExpirada        = "expired"
	EventoNoShow          = "no_show"
	EventoCadastroEditado = "updated"
	// EventoCredito registra dinheiro do hóspede que já entrou e que ESTA
	// reserva não consegue consumir — hoje, o excedente de uma remarcação para
	// uma estadia mais barata. Ver `creditarExcedente` em service.go.
	EventoCredito = "credit_issued"
)

// MotivoRemarcacao é o `cancel_reason` que a própria API grava ao remarcar —
// é o que permite ao BI separar remarcação de desistência.
const MotivoRemarcacao = "remarcacao"

// MotivoNoShow é o motivo com significado especial no /cancel: leva a reserva
// ao estado `no_show`, e não a `cancelled`.
const MotivoNoShow = "no_show"

// ─────────────────────────── Saída ──────────────────────────────────

// UnidadeAlocada é uma linha de `reservation_units`.
type UnidadeAlocada struct {
	UnitID      uuid.UUID  `json:"unit_id"`
	UnitCode    string     `json:"unit_code"`
	UnitName    string     `json:"unit_name"`
	StayBlockID *uuid.UUID `json:"stay_block_id"`
	// Travada tira a reserva da realocação automática: foi o hóspede que
	// pediu aquele apartamento.
	Travada bool `json:"locked"`
}

// Reserva é o schema `Reserva` do contrato.
//
// Todo valor monetário aqui é SNAPSHOT lido de `reservation_pricing` — nada é
// recalculado na leitura. É o que impede a tela de mostrar o preço de hoje para
// uma venda de ontem (CLAUDE.md, regra 7).
type Reserva struct {
	ID           uuid.UUID  `json:"id"`
	Codigo       string     `json:"code"`
	Status       string     `json:"status"`
	UnitTypeID   uuid.UUID  `json:"unit_type_id"`
	UnitTypeNome string     `json:"unit_type_name"`
	ContactID    uuid.UUID  `json:"contact_id"`
	ContactNome  string     `json:"contact_name"`
	BrokerID     *uuid.UUID `json:"broker_id"`
	Origem       string     `json:"source"`

	CheckIn  string `json:"check_in"`
	CheckOut string `json:"check_out"`
	// Noites é `check_out − check_in`, o número de linhas de reservation_nights.
	Noites       int     `json:"night_count"`
	Hospedes     int     `json:"guests_count"`
	IsEvento     bool    `json:"is_event"`
	TipoDeEvento *string `json:"event_type"`

	Subtotal     int64   `json:"subtotal_cents"`
	DescontoPct  float64 `json:"discount_pct"`
	Desconto     int64   `json:"discount_cents"`
	Limpeza      int64   `json:"cleaning_cents"`
	CaucaoEvento int64   `json:"event_deposit_cents"`
	Total        int64   `json:"total_cents"`
	Sinal        int64   `json:"deposit_cents"`
	Saldo        int64   `json:"balance_cents"`

	RateTableID    *uuid.UUID `json:"rate_table_id"`
	PolicyVersion  *int       `json:"policy_version"`
	CancelPolicyID *uuid.UUID `json:"cancellation_policy_id"`

	HoldExpiraEm *time.Time `json:"hold_expires_at"`
	ConfirmadaEm *time.Time `json:"confirmed_at"`
	CanceladaEm  *time.Time `json:"cancelled_at"`
	MotivoDoCanc *string    `json:"cancel_reason"`
	RemarcadaDe  *uuid.UUID `json:"rebooked_from_id"`
	Observacoes  *string    `json:"notes"`

	CriadaEm     time.Time `json:"created_at"`
	AtualizadaEm time.Time `json:"updated_at"`

	Unidades []UnidadeAlocada `json:"units"`
}

// NoiteDaReserva é uma linha de `reservation_nights` — o preço que FOI
// aplicado, não o que a tabela diz hoje.
type NoiteDaReserva struct {
	Noite  string            `json:"night"`
	Tipo   calendar.DateType `json:"date_type"`
	Preco  int64             `json:"price_cents"`
	Rotulo string            `json:"-"` // só para agrupar em `lines`
}

// LinhaDoOrcamento agrupa as noites por tipo de tarifa ("Fim de semana 2×").
type LinhaDoOrcamento struct {
	Tipo     calendar.DateType `json:"date_type"`
	Rotulo   string            `json:"label"`
	Noites   int               `json:"nights"`
	Unitario int64             `json:"unit_price_cents"`
	Subtotal int64             `json:"subtotal_cents"`
}

// HospedeDaReserva é a rooming list (`reservation_guests`).
type HospedeDaReserva struct {
	ContactID uuid.UUID `json:"contact_id"`
	Nome      string    `json:"name"`
	Telefone  *string   `json:"phone_e164"`
	// Email vai cheio pelo mesmo motivo do telefone: é a ficha da reserva,
	// de onde a gestão fala com o hóspede — e a mesma leitura já grava o
	// rastro em pii_access_log.
	Email   *string `json:"email"`
	Titular bool    `json:"is_lead_guest"`
}

// EventoDaReserva é uma linha de `reservation_events` — append-only.
type EventoDaReserva struct {
	ID      uuid.UUID      `json:"id"`
	Tipo    string         `json:"type"`
	Payload map[string]any `json:"payload"`
	// AutorID é nulo quando o autor é o job de expiração, não uma pessoa.
	AutorID  *uuid.UUID `json:"actor_id"`
	Instante time.Time  `json:"at"`
}

// ReservaCompleta é tudo o que a tela precisa numa chamada.
type ReservaCompleta struct {
	Reserva  Reserva            `json:"reservation"`
	Noites   []NoiteDaReserva   `json:"nights"`
	Linhas   []LinhaDoOrcamento `json:"lines"`
	Unidades []UnidadeAlocada   `json:"units"`
	Hospedes []HospedeDaReserva `json:"guests"`
	Timeline []EventoDaReserva  `json:"timeline"`
	// PreviaDoCancelamento é o mesmo cálculo do ?dry_run=1; nula quando a
	// reserva não é mais cancelável.
	PreviaDoCancelamento *ResultadoDeCancelamento `json:"cancellation_preview"`
}

// Bloqueio é uma linha de `stay_blocks` no formato do contrato.
type Bloqueio struct {
	ID         uuid.UUID  `json:"id"`
	UnitID     uuid.UUID  `json:"unit_id"`
	UnitCode   string     `json:"unit_code"`
	Origem     string     `json:"source"`
	Status     string     `json:"status"`
	De         string     `json:"from"`
	Ate        string     `json:"to"`
	ReservaID  *uuid.UUID `json:"reservation_id"`
	ExpiraEm   *time.Time `json:"expires_at"`
	Observacao *string    `json:"note"`
	CriadoEm   time.Time  `json:"created_at"`
}

// ─────────────────────────── Entrada ────────────────────────────────

// ReservaCriar é o corpo do POST /reservations.
//
// Nenhum VALOR entra por aqui — nem subtotal, nem total, nem sinal. O servidor
// recalcula tudo pelo motor e congela o resultado: preço que chega do cliente é
// preço que o cliente escolheu.
type ReservaCriar struct {
	UnitTypeID uuid.UUID `json:"unit_type_id" validate:"required"`
	CheckIn    string    `json:"check_in" validate:"required"`
	CheckOut   string    `json:"check_out" validate:"required"`
	// Hospedes é `guests_count` — o MESMO nome da resposta, do PATCH e do
	// orçamento. Era `guests` só aqui, e a revisão mediu o preço da diferença:
	// `POST` com `guests_count` respondia 422 "guests é obrigatório", e o
	// `PATCH {"guests": 6}` respondia 200 sem mudar nada, porque nenhum decoder
	// recusa campo desconhecido. A tela monta os dois do mesmo formulário.
	Hospedes    int     `json:"guests_count" validate:"required,min=1"`
	DescontoPct float64 `json:"discount_pct" validate:"min=0,max=100"`
	IsEvento    bool    `json:"is_event"`
	TipoEvento  *string `json:"event_type"`
	// ContactID é obrigatório: reserva sem contato não tem a quem cobrar
	// (`reservations.contact_id` é NOT NULL no schema).
	ContactID uuid.UUID `json:"contact_id" validate:"required"`
	// BrokerID é Opt, e não ponteiro, porque AUSENTE e `null` decidem coisas
	// diferentes em escopo `own`: ausente grava o corretor do próprio ator,
	// `null` grava venda direta (commission.ResolveBroker).
	BrokerID    httpx.Opt[uuid.UUID] `json:"broker_id"`
	Origem      string               `json:"source"`
	Observacoes *string              `json:"notes"`
}

func (c *ReservaCriar) Normalizar() {
	c.Origem = strings.TrimSpace(c.Origem)
	if c.Origem == "" {
		c.Origem = "direto"
	}
	c.TipoEvento = textoOuNulo(c.TipoEvento)
	c.Observacoes = textoOuNulo(c.Observacoes)
}

// Validar cobre só o FORMATO. "check_out depois de check_in", capacidade e
// alçada são regra de negócio, e quem decide é booking.Build — duplicar aqui
// criaria dois lugares para o mesmo "não", que divergem no dia em que só um for
// editado.
func (c ReservaCriar) Validar() map[string]string {
	falhas := map[string]string{}
	validarData(falhas, "check_in", c.CheckIn)
	validarData(falhas, "check_out", c.CheckOut)
	limitar(falhas, "event_type", c.TipoEvento, 60)
	limitar(falhas, "notes", c.Observacoes, 2000)
	if len(c.Origem) > 40 {
		falhas["source"] = "no máximo 40 caracteres."
	}
	return falhas
}

// ReservaAtualizar é o corpo do PUT e do PATCH.
//
// Datas, produto e preço NÃO estão aqui de propósito: quem muda estadia é
// /reschedule, quem muda unidade é /reassign-unit, quem muda estado é a ação
// correspondente. Deixar datas editáveis aqui seria um caminho para alterar
// `stay_blocks` por fora da constraint — e sem registrar a remarcação.
type ReservaAtualizar struct {
	ContactID   httpx.Opt[uuid.UUID] `json:"contact_id"`
	BrokerID    httpx.Opt[uuid.UUID] `json:"broker_id"`
	Hospedes    httpx.Opt[int]       `json:"guests_count"`
	IsEvento    httpx.Opt[bool]      `json:"is_event"`
	TipoEvento  httpx.Opt[string]    `json:"event_type"`
	Origem      httpx.Opt[string]    `json:"source"`
	Observacoes httpx.Opt[string]    `json:"notes"`
}

func (a ReservaAtualizar) Validar() map[string]string {
	falhas := map[string]string{}
	if a.ContactID.DeveLimpar() {
		falhas["contact_id"] = "não pode ser nulo."
	}
	if a.IsEvento.DeveLimpar() {
		falhas["is_event"] = "não pode ser nulo."
	}
	if a.Origem.DeveLimpar() {
		falhas["source"] = "não pode ser nulo."
	}
	if v, ok := a.Hospedes.Definido(); ok && v < 1 {
		falhas["guests_count"] = "deve ser no mínimo 1."
	}
	if a.Hospedes.DeveLimpar() {
		falhas["guests_count"] = "não pode ser nulo."
	}
	if v, ok := a.TipoEvento.Definido(); ok && len(v) > 60 {
		falhas["event_type"] = "no máximo 60 caracteres."
	}
	if v, ok := a.Observacoes.Definido(); ok && len(v) > 2000 {
		falhas["notes"] = "no máximo 2000 caracteres."
	}
	if v, ok := a.Origem.Definido(); ok && (strings.TrimSpace(v) == "" || len(v) > 40) {
		falhas["source"] = "de 1 a 40 caracteres."
	}
	return falhas
}

// ConfirmacaoDeReserva é o corpo (opcional) do POST /confirm.
type ConfirmacaoDeReserva struct {
	// SinalPago ausente significa "recebeu o sinal cheio" e assume
	// `deposit_cents`. É esta a base que a política de cancelamento usa depois.
	SinalPago   *int64  `json:"deposit_paid_cents"`
	Meio        string  `json:"method"`
	PagoEm      *string `json:"paid_at"`
	ReferExt    *string `json:"external_ref"`
	Observacoes *string `json:"note"`
}

var meiosDePagamento = map[string]bool{"pix": true, "cartao": true, "transferencia": true, "dinheiro": true}

func (c ConfirmacaoDeReserva) Validar() map[string]string {
	falhas := map[string]string{}
	// Só o PISO cabe aqui: o TETO é `total_cents`, que o DTO não conhece — quem
	// o aplica é o service, com a reserva travada em mãos (ver Confirmar).
	// Piso 1, e não 0: confirmar sem dinheiro nenhum é o que o `hold` já faz, e
	// com prazo.
	if c.SinalPago != nil && *c.SinalPago < 1 {
		falhas["deposit_paid_cents"] = "deve ser de no mínimo 1 centavo."
	}
	if c.Meio != "" && !meiosDePagamento[c.Meio] {
		falhas["method"] = "use pix, cartao, transferencia ou dinheiro."
	}
	if c.PagoEm != nil {
		if _, err := time.Parse(time.RFC3339, *c.PagoEm); err != nil {
			falhas["paid_at"] = "instante inválido: use RFC 3339."
		}
	}
	return falhas
}

// PedidoDeCancelamento é o corpo (opcional) do POST /cancel.
type PedidoDeCancelamento struct {
	// Motivo é texto livre. Dois valores têm significado no sistema:
	// `no_show` leva ao estado no_show, e `remarcacao` é o que a própria API
	// grava ao remarcar.
	Motivo string `json:"reason"`
}

func (p PedidoDeCancelamento) Validar() map[string]string {
	if len(p.Motivo) > 200 {
		return map[string]string{"reason": "no máximo 200 caracteres."}
	}
	return nil
}

// ResultadoDeCancelamento é a saída de booking.CancellationPolicy.Simulate com
// o contexto que a tela precisa mostrar.
type ResultadoDeCancelamento struct {
	Rotulo    string `json:"label"`
	Devolucao int64  `json:"refund_cents"`
	Retido    int64  `json:"retained_cents"`
	// SinalPago é a base do cálculo — o sinal EFETIVAMENTE recebido, lido de
	// reservation_events, e não o `deposit_cents` teórico da reserva.
	SinalPago int64 `json:"deposit_paid_cents"`
	// Antecedencia pode ser negativa quando o check-in já passou.
	Antecedencia  int    `json:"days_before"`
	PolicyVersion int    `json:"policy_version"`
	Simulado      bool   `json:"dry_run"`
	Status        string `json:"status"`
	// Credito é o dinheiro do hóspede que ESTE cancelamento NÃO liquida:
	// os créditos registrados na timeline (o excedente de uma remarcação para
	// estadia mais barata) mais o que uma confirmação antiga gravou acima do
	// total e que a base do cálculo teve de truncar.
	//
	// Sem este campo a tela lê `refund 225.000 / retained 0` como "conta
	// encerrada" numa venda em que entraram 830.000 — foi exatamente assim que
	// R$ 6.050,00 sumiram na medição da revisão. Zero é o caso normal.
	Credito int64 `json:"credit_cents"`
}

// PedidoDeRemarcacao é o corpo do POST /reschedule. O que não vier é herdado da
// reserva original — remarcar normalmente só muda datas.
type PedidoDeRemarcacao struct {
	CheckIn     string     `json:"check_in" validate:"required"`
	CheckOut    string     `json:"check_out" validate:"required"`
	UnitTypeID  *uuid.UUID `json:"unit_type_id"`
	Hospedes    *int       `json:"guests_count"`
	DescontoPct *float64   `json:"discount_pct"`
	Motivo      *string    `json:"reason"`
}

func (p PedidoDeRemarcacao) Validar() map[string]string {
	falhas := map[string]string{}
	validarData(falhas, "check_in", p.CheckIn)
	validarData(falhas, "check_out", p.CheckOut)
	if p.Hospedes != nil && *p.Hospedes < 1 {
		falhas["guests_count"] = "deve ser no mínimo 1."
	}
	if p.DescontoPct != nil && (*p.DescontoPct < 0 || *p.DescontoPct > 100) {
		falhas["discount_pct"] = "deve estar entre 0 e 100."
	}
	limitar(falhas, "reason", p.Motivo, 200)
	return falhas
}

// MetaDaRemarcacao acompanha a resposta do /reschedule.
type MetaDaRemarcacao struct {
	ReservaAnteriorID uuid.UUID `json:"previous_reservation_id"`
	CodigoAnterior    string    `json:"previous_code"`
	TotalAnterior     int64     `json:"previous_total_cents"`
	// Diferenca é `total novo − total antigo`: positivo é o que falta cobrar,
	// negativo é o que há a devolver. A Fase 1 apenas INFORMA — gerar o
	// recebível é do módulo financeiro.
	Diferenca int64 `json:"difference_cents"`
	// Credito é o sinal já pago que NÃO coube na reserva nova, virado crédito
	// a favor do hóspede (evento `credit_issued`). Zero na esmagadora maioria
	// das remarcações — só aparece quando a estadia nova custa menos do que o
	// hóspede já tinha adiantado.
	Credito int64 `json:"credit_cents"`
}

// RemarcacaoFeita é o par (reserva nova, meta) que o handler envelopa.
type RemarcacaoFeita struct {
	Reserva Reserva
	Meta    MetaDaRemarcacao
}

// RegistroDeEstadia é o corpo (opcional) de /check-in e /check-out.
type RegistroDeEstadia struct {
	// Instante ausente é now(). Vai para a timeline, não para uma coluna de
	// `reservations` — a tabela já nasceu acima do teto de colunas do db.md, e
	// um evento append-only registra melhor o "quando" do que uma coluna que
	// alguém pode sobrescrever.
	Instante   *string `json:"at"`
	Observacao *string `json:"note"`
}

func (r RegistroDeEstadia) Validar() map[string]string {
	falhas := map[string]string{}
	if r.Instante != nil {
		if _, err := time.Parse(time.RFC3339, *r.Instante); err != nil {
			falhas["at"] = "instante inválido: use RFC 3339."
		}
	}
	limitar(falhas, "note", r.Observacao, 2000)
	return falhas
}

// PedidoDeRealocacao é o corpo do POST /reassign-unit.
type PedidoDeRealocacao struct {
	// UnidadeDeOrigem pode ser omitida quando a reserva ocupa uma só unidade —
	// que é o caso de todo produto `one_member`.
	UnidadeDeOrigem  *uuid.UUID      `json:"from_unit_id"`
	UnidadeDeDestino uuid.UUID       `json:"to_unit_id" validate:"required"`
	Travar           httpx.Opt[bool] `json:"locked"`
	Motivo           *string         `json:"reason"`
}

func (p PedidoDeRealocacao) Validar() map[string]string {
	falhas := map[string]string{}
	if p.Travar.DeveLimpar() {
		falhas["locked"] = "não pode ser nulo."
	}
	limitar(falhas, "reason", p.Motivo, 200)
	return falhas
}

// PedidoDeExtensaoDeHold é o corpo (opcional) do POST /extend-hold.
type PedidoDeExtensaoDeHold struct {
	Horas  *int    `json:"hours"`
	Motivo *string `json:"reason"`
}

func (p PedidoDeExtensaoDeHold) Validar() map[string]string {
	falhas := map[string]string{}
	if p.Horas != nil && *p.Horas < 1 {
		falhas["hours"] = "deve ser no mínimo 1."
	}
	if p.Horas != nil && *p.Horas > 24*30 {
		falhas["hours"] = "no máximo 720 horas (30 dias)."
	}
	limitar(falhas, "reason", p.Motivo, 200)
	return falhas
}

// ResultadoDeExtensaoDeHold é a saída do POST /extend-hold.
type ResultadoDeExtensaoDeHold struct {
	ID           uuid.UUID `json:"id"`
	HoldExpiraEm time.Time `json:"hold_expires_at"`
	// Extensoes sai da CONTAGEM de eventos `hold_extended`, não de uma coluna:
	// contador denormalizado seria uma segunda verdade sobre o mesmo fato.
	Extensoes    int `json:"extensions_count"`
	MaxExtensoes int `json:"max_extensions"`
}

// BloqueioCriar é o corpo do POST /blocks.
type BloqueioCriar struct {
	Unidades   []uuid.UUID `json:"unit_ids" validate:"required,min=1"`
	De         string      `json:"from" validate:"required"`
	Ate        string      `json:"to" validate:"required"`
	Origem     string      `json:"source" validate:"required"`
	Observacao *string     `json:"note"`
}

// tetoDeUnidadesPorBloqueio existe porque o corpo vira N inserções numa
// transação só: sem teto, uma lista de mil uuids segura o índice gist da
// constraint enquanto a transação inteira é verificada.
const tetoDeUnidadesPorBloqueio = 100

// OS DOIS TETOS DA JANELA — o MÉDIO da segunda revisão.
//
// MEDIDO ANTES: um POST /blocks do CORRETOR (o menor privilégio do sistema)
// com as oito unidades e `from=2040-01-01 to=2050-01-01` respondeu 201 e tirou
// 29.224 noites-unidade do mercado. A venda da White House Completa em 2045
// passava a morrer com 409, e desfazer o estrago exigia oito DELETEs.
//
// POR QUE UM TETO, E NÃO UMA PERMISSÃO MAIOR: a permissão já ficou simétrica
// na rodada passada (quem bloqueia desfaz o próprio bloqueio) e mesmo assim o
// dano de uma tecla foi a década. Exigir `admin` para janelas longas só mudaria
// QUEM erra de dedo — o admin digita `2050` tão fácil quanto o corretor. O teto
// vale para todo mundo, e é por isso que ele vive no DTO.
//
// QUEM PRECISA DE MAIS tem dois caminhos, e nenhum deles é este endpoint:
//
//   - reforma de uma temporada inteira: parte em bloqueios de até um ano. Cada
//     linha vira uma decisão revisável e liberável sozinha, que é justamente o
//     que uma linha gigante de dez anos impede.
//   - unidade que sai do catálogo: `units.active = false` no módulo de
//     inventário. Tirar do mercado para sempre é decisão de INVENTÁRIO, não de
//     calendário — e o calendário não tem como distinguir uma da outra.
const (
	// tetoDeNoitesPorBloqueio: manutenção e uso do proprietário se medem em
	// dias ou semanas. Um ano é folgado para a temporada mais longa que a casa
	// consegue justificar, e acima dele o que se está fazendo é retirar a
	// unidade do catálogo.
	tetoDeNoitesPorBloqueio = 365
	// horizonteMaximoDoBloqueio: `from` no máximo três anos além de HOJE na
	// casa. É o que impede a década numa tecla — 2040 fica a catorze anos, e
	// nem o tarifário nem a política comercial alcançam lá. Três anos cobre
	// com folga o horizonte de venda real (a suíte já exercita bloqueio a 840
	// dias) e o `from` no passado continua livre: registrar manutenção que já
	// aconteceu é legítimo.
	horizonteMaximoDoBloqueio = 3 * 365
)

func (b BloqueioCriar) Validar() map[string]string {
	falhas := map[string]string{}
	validarData(falhas, "from", b.De)
	validarData(falhas, "to", b.Ate)

	de, errDe := calendar.Parse(b.De)
	ate, errAte := calendar.Parse(b.Ate)
	if errDe == nil && errAte == nil {
		switch noites := de.Nights(ate); {
		case noites <= 0:
			falhas["to"] = "deve ser posterior a `from` (o intervalo é half-open: `to` não entra)."
		case noites > tetoDeNoitesPorBloqueio:
			// O erro cai em `to` porque é a data que o operador encurta.
			falhas["to"] = fmt.Sprintf(
				"o bloqueio cobre %d noites; o máximo é %d (um ano). Parta em bloqueios menores, "+
					"ou desative a unidade no inventário se ela sai do catálogo.",
				noites, tetoDeNoitesPorBloqueio)
		}
	}

	if b.Origem != OrigemManutencao && b.Origem != OrigemProprietario {
		falhas["source"] = "use maintenance ou owner_hold."
	}
	if len(b.Unidades) > tetoDeUnidadesPorBloqueio {
		falhas["unit_ids"] = "no máximo 100 unidades por bloqueio."
	}
	vistas := map[uuid.UUID]bool{}
	for _, u := range b.Unidades {
		if vistas[u] {
			falhas["unit_ids"] = "unidade repetida na lista."
			break
		}
		vistas[u] = true
	}
	limitar(falhas, "note", b.Observacao, 2000)
	return falhas
}

// ─────────────────────────── Filtro da listagem ─────────────────────

// Filtro é o recorte de GET /reservations.
type Filtro struct {
	Status     []string
	De         string // ISO; reservas que TOCAM [De, Ate)
	Ate        string
	Busca      string
	UnitTypeID *uuid.UUID
	ContactID  *uuid.UUID
	BrokerID   *uuid.UUID
	OrderBy    string
	Pagina     int
	PorPagina  int
	// SomenteMinhas vira `AND r.owner_id = $usuario` no SQL — nunca filtro em
	// memória, que faria o `total` da paginação mentir.
	SomenteMinhas bool
	Usuario       uuid.UUID
}

// ColunasDeOrdenacao é a whitelist do `?sort`. Nada do que o cliente digitou
// entra na consulta: só o valor mapeado aqui.
var ColunasDeOrdenacao = map[string]string{
	"check_in":   "r.check_in",
	"created_at": "r.created_at",
	"code":       "r.code",
}

// OrdemPadrao é o `default` declarado na OpenAPI (`-created_at`).
const OrdemPadrao = "r.created_at DESC"

// ─────────────────────────── Auxiliares ─────────────────────────────

func validarData(falhas map[string]string, campo, valor string) {
	if valor == "" {
		return // `required` da tag já cobre a ausência
	}
	if _, err := calendar.Parse(valor); err != nil {
		falhas[campo] = "data inválida: use AAAA-MM-DD."
	}
}

func limitar(falhas map[string]string, campo string, valor *string, max int) {
	if valor != nil && len(*valor) > max {
		falhas[campo] = "texto acima do limite."
	}
}

func textoOuNulo(v *string) *string {
	if v == nil {
		return nil
	}
	t := strings.TrimSpace(*v)
	if t == "" {
		return nil
	}
	return &t
}

// StatusDaQuery lê `?status=hold,confirmed`. Valor desconhecido é 422 e nunca
// "ignora e devolve tudo": a tela pediu um recorte e precisa saber que ele não
// existe.
func StatusDaQuery(bruto string) ([]string, error) {
	bruto = strings.TrimSpace(bruto)
	if bruto == "" {
		return nil, nil
	}
	var out []string
	for _, parte := range strings.Split(bruto, ",") {
		s := strings.TrimSpace(parte)
		if s == "" {
			continue
		}
		if !EstadoConhecido(s) {
			return nil, apperr.Validation(map[string]string{"status": "estado desconhecido: " + s})
		}
		out = append(out, s)
	}
	return out, nil
}
