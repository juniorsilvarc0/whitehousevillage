package crm

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/disponibilidade"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/reservas"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// ═══════════════════════════ Saída ══════════════════════════════════

// Funil é o schema `Funil`. `stage_count` e `open_opportunity_count` são
// derivados na consulta — o segundo já dentro do escopo do requisitante, senão
// o corretor leria no seletor de funil a contagem da casa inteira.
type Funil struct {
	ID         uuid.UUID `json:"id"`
	Nome       string    `json:"name"`
	Padrao     bool      `json:"is_default"`
	Ativo      bool      `json:"active"`
	QtdEtapas  int       `json:"stage_count"`
	QtdAbertas int       `json:"open_opportunity_count"`
}

// FunilComEtapas é o `GET /crm/pipelines/{id}`.
type FunilComEtapas struct {
	Funil
	Etapas []Etapa `json:"stages"`
}

// Etapa é o schema `EtapaDoFunil`. `probability` vem de `numeric(5,2)` no banco
// e sai inteiro no contrato: probabilidade comercial com casa decimal é
// precisão que ninguém digita e que a tela arredondaria de qualquer jeito.
type Etapa struct {
	ID            uuid.UUID `json:"id"`
	FunilID       uuid.UUID `json:"pipeline_id"`
	Nome          string    `json:"name"`
	Posicao       int       `json:"position"`
	Probabilidade int       `json:"probability"`
	Cor           string    `json:"color"`
	Tipo          string    `json:"type"`

	SLADias         *int    `json:"sla_days"`
	TarefaAssunto   *string `json:"auto_task_subject"`
	TarefaTipo      *string `json:"auto_task_type"`
	TarefaPrazoDias *int    `json:"auto_task_due_days"`
	Notificar       bool    `json:"auto_notify"`
}

// TemTarefaAutomatica: é `auto_task_subject` que liga o mecanismo. O resto
// (`type`, `due_days`) só é lido quando ele existe — e o CHECK do banco garante
// que os três andam juntos.
func (e Etapa) TemTarefaAutomatica() bool {
	return e.TarefaAssunto != nil && strings.TrimSpace(*e.TarefaAssunto) != ""
}

// PrazoDaTarefa é o prazo em dias da tarefa automática: `auto_task_due_days`
// quando houver, senão `sla_days` — o prazo da tarefa e o do SLA são a mesma
// promessa. Sem nenhum dos dois não há tarefa (o CHECK do banco já recusaria).
func (e Etapa) PrazoDaTarefa() (int, bool) {
	if e.TarefaPrazoDias != nil {
		return *e.TarefaPrazoDias, true
	}
	if e.SLADias != nil {
		return *e.SLADias, true
	}
	return 0, false
}

// MotivoDePerda é o schema `MotivoDePerda`.
type MotivoDePerda struct {
	ID     uuid.UUID `json:"id"`
	Rotulo string    `json:"label"`
	Ativo  bool      `json:"active"`
	Usos   int       `json:"usage_count"`
}

// ContatoResumo é o contato como o CRM precisa dele. O cadastro completo é de
// `/contacts`: expor só o resumo (sem documento, nascimento nem notas) reduz o
// que a tela da oportunidade entrega. Telefone e e-mail saem CHEIOS — é de
// onde sai o `tel:` —, e por isso `GET /crm/opportunities/{id}/full` grava
// `pii_access_log` com `reason: opportunity` (ver Servico.Completa).
type ContatoResumo struct {
	ID       uuid.UUID `json:"id"`
	Nome     string    `json:"name"`
	Email    *string   `json:"email"`
	Telefone *string   `json:"phone_e164"`
	Cidade   *string   `json:"city"`
	Estado   *string   `json:"state"`
}

// Lead é o schema `Lead`. Guarda o INTERESSE, não a pessoa — nome e telefone
// vivem em `contacts`, um registro por pessoa.
type Lead struct {
	ID             uuid.UUID  `json:"id"`
	ContactID      uuid.UUID  `json:"contact_id"`
	ContatoNome    string     `json:"contact_name"`
	ContatoFone    *string    `json:"contact_phone_e164"`
	Origem         string     `json:"source"`
	CampanhaID     *uuid.UUID `json:"campaign_id"`
	Status         string     `json:"status"`
	Score          int        `json:"score"`
	ProdutoID      *uuid.UUID `json:"interest_unit_type_id"`
	ProdutoNome    *string    `json:"interest_unit_type_name"`
	CheckIn        *string    `json:"desired_check_in"`
	CheckOut       *string    `json:"desired_check_out"`
	Hospedes       *int       `json:"guests_count"`
	DonoID         *uuid.UUID `json:"owner_id"`
	DonoNome       *string    `json:"owner_name"`
	ConvertidoEm   *time.Time `json:"converted_at"`
	OportunidadeID *uuid.UUID `json:"opportunity_id"`
	CriadoEm       time.Time  `json:"created_at"`
	AtualizadoEm   time.Time  `json:"updated_at"`
}

// DetalheDeEvento é o satélite `crm_opportunity_event_details`.
type DetalheDeEvento struct {
	TipoDeEvento   *string `json:"event_type"`
	ConvidadosPrev *int    `json:"guests_expected"`
	PrecisaBuffet  bool    `json:"needs_catering"`
	Observacoes    *string `json:"notes"`
}

// Oportunidade é o schema `Oportunidade`.
//
// NÃO EXISTE CAMPO `title` no contrato, de propósito: o card se identifica por
// contato + produto + datas. A coluna `crm_opportunities.title` existe no schema
// e é preenchida pelo SERVIDOR com essa mesma composição (ver `tituloDoCard`) —
// nenhum cliente escreve nela, e nenhuma resposta a devolve.
type Oportunidade struct {
	ID          uuid.UUID  `json:"id"`
	ContactID   uuid.UUID  `json:"contact_id"`
	ContatoNome string     `json:"contact_name"`
	LeadID      *uuid.UUID `json:"lead_id"`
	FunilID     uuid.UUID  `json:"pipeline_id"`
	FunilNome   string     `json:"pipeline_name"`
	EtapaID     uuid.UUID  `json:"stage_id"`
	EtapaNome   string     `json:"stage_name"`
	EtapaTipo   string     `json:"stage_type"`

	ProdutoID   *uuid.UUID `json:"unit_type_id"`
	ProdutoNome *string    `json:"unit_type_name"`
	CheckIn     *string    `json:"check_in"`
	CheckOut    *string    `json:"check_out"`

	OrcamentoID   *uuid.UUID `json:"quote_id"`
	ReservaID     *uuid.UUID `json:"reservation_id"`
	ReservaCodigo *string    `json:"reservation_code"`

	Valor          int64   `json:"amount_cents"`
	Probabilidade  int     `json:"probability"`
	FechamentoPrev *string `json:"expected_close"`

	DonoID   *uuid.UUID `json:"owner_id"`
	DonoNome *string    `json:"owner_name"`

	Status       string     `json:"status"`
	MotivoID     *uuid.UUID `json:"lost_reason_id"`
	MotivoRotulo *string    `json:"lost_reason_label"`

	EntrouNaEtapaEm  time.Time  `json:"entered_stage_at"`
	SLADias          *int       `json:"sla_days"`
	SLAVenceEm       *time.Time `json:"sla_due_at"`
	SLAEstourado     bool       `json:"sla_breached"`
	TarefasPendentes int        `json:"pending_task_count"`

	Evento *DetalheDeEvento `json:"event"`

	CriadoPor    *uuid.UUID `json:"created_by"`
	CriadoEm     time.Time  `json:"created_at"`
	AtualizadoEm time.Time  `json:"updated_at"`
}

// CardDaOportunidade é o mínimo para desenhar, arrastar e decidir no kanban.
// Mandar a oportunidade inteira aqui faria o quadro carregar oito etapas de
// dados que ninguém leu.
type CardDaOportunidade struct {
	ID               uuid.UUID  `json:"id"`
	ContactID        uuid.UUID  `json:"contact_id"`
	ContatoNome      string     `json:"contact_name"`
	ProdutoNome      *string    `json:"unit_type_name"`
	CheckIn          *string    `json:"check_in"`
	CheckOut         *string    `json:"check_out"`
	Valor            int64      `json:"amount_cents"`
	Probabilidade    int        `json:"probability"`
	FechamentoPrev   *string    `json:"expected_close"`
	DonoID           *uuid.UUID `json:"owner_id"`
	DonoNome         *string    `json:"owner_name"`
	Status           string     `json:"status"`
	EntrouNaEtapaEm  time.Time  `json:"entered_stage_at"`
	SLAVenceEm       *time.Time `json:"sla_due_at"`
	SLAEstourado     bool       `json:"sla_breached"`
	TarefasPendentes int        `json:"pending_task_count"`
	ProximoPrazo     *time.Time `json:"next_due_at"`
	ReservaCodigo    *string    `json:"reservation_code"`
}

// ColunaDoKanban é uma etapa com os cards da PÁGINA e os totais da COLUNA
// INTEIRA. Os dois números vêm do servidor porque a coluna é paginada: somar o
// que veio na tela daria um valor de funil que muda conforme se rola a página.
type ColunaDoKanban struct {
	Etapa   Etapa                `json:"stage"`
	Total   int                  `json:"count"`
	Valor   int64                `json:"amount_cents"`
	TemMais bool                 `json:"has_more"`
	Cards   []CardDaOportunidade `json:"cards"`
}

// TotaisDoKanban é o funil somado, já dentro do escopo do requisitante.
type TotaisDoKanban struct {
	Total int   `json:"count"`
	Valor int64 `json:"amount_cents"`
}

// QuadroKanban é o `GET /crm/opportunities/kanban`.
type QuadroKanban struct {
	Funil   Funil            `json:"pipeline"`
	Colunas []ColunaDoKanban `json:"columns"`
	Totais  TotaisDoKanban   `json:"totals"`
}

// FaixaDeSLA é a faixa da tela da oportunidade. Tudo derivado, com o "agora" da
// casa: a mesma conta feita no navegador daria uma resposta por fuso de quem
// abriu a tela.
type FaixaDeSLA struct {
	EtapaID       uuid.UUID  `json:"stage_id"`
	EtapaNome     string     `json:"stage_name"`
	SLADias       *int       `json:"sla_days"`
	EntrouEm      time.Time  `json:"entered_stage_at"`
	VenceEm       *time.Time `json:"due_at"`
	DiasRestantes *int       `json:"days_left"`
	Estourado     bool       `json:"breached"`
}

// EventoDeEtapa é uma linha de `crm_stage_history` — insert-only.
type EventoDeEtapa struct {
	ID            uuid.UUID  `json:"id"`
	DeEtapaID     *uuid.UUID `json:"from_stage_id"`
	DeEtapaNome   *string    `json:"from_stage_name"`
	ParaEtapaID   uuid.UUID  `json:"to_stage_id"`
	ParaEtapaNome string     `json:"to_stage_name"`
	UsuarioID     *uuid.UUID `json:"user_id"`
	UsuarioNome   *string    `json:"user_name"`
	Motivo        *string    `json:"reason"`
	Instante      time.Time  `json:"at"`
	DiasNaEtapa   *float64   `json:"days_in_stage"`
}

// Atividade é o schema `Atividade`. Tarefa, ligação, reunião, e-mail, WhatsApp
// e nota são o MESMO registro com `type` diferente.
type Atividade struct {
	ID          uuid.UUID  `json:"id"`
	Tipo        string     `json:"type"`
	Assunto     string     `json:"subject"`
	Descricao   *string    `json:"description"`
	VenceEm     *time.Time `json:"due_at"`
	ConcluidaEm *time.Time `json:"done_at"`
	Status      string     `json:"status"`
	Vencida     bool       `json:"overdue"`
	Prioridade  string     `json:"priority"`

	LeadID         *uuid.UUID `json:"lead_id"`
	OportunidadeID *uuid.UUID `json:"opportunity_id"`
	ContactID      *uuid.UUID `json:"contact_id"`
	ContatoNome    *string    `json:"contact_name"`

	DonoID   uuid.UUID `json:"owner_id"`
	DonoNome *string   `json:"owner_name"`

	EtapaID *uuid.UUID `json:"stage_id"`
	Auto    bool       `json:"auto"`

	CriadoEm     time.Time `json:"created_at"`
	AtualizadoEm time.Time `json:"updated_at"`
}

// NotaDaOportunidade é a projeção das atividades de tipo `nota`. Não há tabela
// separada nem CRUD próprio: criar nota é `POST /crm/activities` com
// `type: nota`, e assim ela entra na linha do tempo pelo mesmo caminho.
type NotaDaOportunidade struct {
	ID        uuid.UUID  `json:"id"`
	Corpo     string     `json:"body"`
	AutorID   *uuid.UUID `json:"author_id"`
	AutorNome *string    `json:"author_name"`
	CriadaEm  time.Time  `json:"created_at"`
}

// DocumentoDaOportunidade vem VAZIO nesta rodada e está declarado assim mesmo:
// o módulo de anexos é de outra fase, e a tela precisa nascer com o formato
// final. Acrescentar a chave depois obrigaria o painel a tratar `documents`
// como opcional para sempre.
type DocumentoDaOportunidade struct {
	ID             uuid.UUID  `json:"id"`
	Nome           string     `json:"name"`
	Mime           string     `json:"mime"`
	Tamanho        int64      `json:"size_bytes"`
	URL            string     `json:"url"`
	EnviadoPor     *uuid.UUID `json:"uploaded_by"`
	EnviadoPorNome *string    `json:"uploaded_by_name"`
	CriadoEm       time.Time  `json:"created_at"`
}

// Códigos e severidades de alerta — derivados na leitura, nunca gravados.
const (
	AlertaSLAEstourado  = "sla_estourado"
	AlertaTarefaVencida = "tarefa_vencida"
	AlertaParado        = "parado_ha_n_dias"
	AlertaHoldExpirando = "hold_expirando"

	SeveridadeInfo    = "info"
	SeveridadeAtencao = "atencao"
	SeveridadeCritico = "critico"
)

// AlertaDaOportunidade é o schema `AlertaDaOportunidade`.
type AlertaDaOportunidade struct {
	Codigo     string     `json:"code"`
	Severidade string     `json:"severity"`
	Mensagem   string     `json:"message"`
	VenceEm    *time.Time `json:"due_at"`
	EntidadeID *uuid.UUID `json:"entity_id"`
}

// Tipos da linha do tempo unificada. Vocabulário aberto no contrato, fechado
// aqui: a timeline é lida por gente, e um `type` digitado errado vira linha
// órfã que nenhuma tela sabe rotular.
const (
	LinhaCriada             = "created"
	LinhaEtapaMudou         = "stage_changed"
	LinhaAtividadeCriada    = "activity_created"
	LinhaAtividadeConcluida = "activity_completed"
	LinhaNota               = "note_added"
	LinhaReservaCriada      = "reservation_created"
	LinhaGanha              = "won"
	LinhaPerdida            = "lost"
)

// EventoDaOportunidade é uma linha da timeline unificada.
type EventoDaOportunidade struct {
	ID       uuid.UUID      `json:"id"`
	Tipo     string         `json:"type"`
	Instante time.Time      `json:"at"`
	AtorID   *uuid.UUID     `json:"actor_id"`
	AtorNome *string        `json:"actor_name"`
	Titulo   string         `json:"title"`
	Payload  map[string]any `json:"payload"`
}

// OportunidadeCompleta é tudo o que a tela desenha, numa chamada.
type OportunidadeCompleta struct {
	Oportunidade Oportunidade              `json:"opportunity"`
	Contato      ContatoResumo             `json:"contact"`
	Etapas       []Etapa                   `json:"stages"`
	SLA          FaixaDeSLA                `json:"sla"`
	Historico    []EventoDeEtapa           `json:"stage_history"`
	Atividades   []Atividade               `json:"activities"`
	Notas        []NotaDaOportunidade      `json:"notes"`
	Documentos   []DocumentoDaOportunidade `json:"documents"`
	Alertas      []AlertaDaOportunidade    `json:"alerts"`
	Timeline     []EventoDaOportunidade    `json:"timeline"`
	// Orcamento é o orçamento vigente EMITIDO (`quotes`), aberto noite a noite —
	// não mais uma reserva em `quote`. É `null` quando ainda não há orçamento, e
	// é esse `null` que faz o botão de ganhar responder `422 QUOTE_REQUIRED_TO_WIN`:
	// a tela pode desabilitá-lo antes de o operador descobrir na resposta.
	Orcamento *disponibilidade.OrcamentoSalvo `json:"quote"`
	Reserva   *reservas.Reserva               `json:"reservation"`
}

// ═══════════════════════════ Resultados das ações ═══════════════════

type ResultadoDeConversao struct {
	Lead         Lead         `json:"lead"`
	Oportunidade Oportunidade `json:"opportunity"`
	TarefaAuto   *Atividade   `json:"auto_task"`
}

type ResultadoDeMudancaDeEtapa struct {
	Oportunidade Oportunidade `json:"opportunity"`
	TarefaAuto   *Atividade   `json:"auto_task"`
	SLA          FaixaDeSLA   `json:"sla"`
}

type ResultadoDeGanho struct {
	Oportunidade Oportunidade     `json:"opportunity"`
	Reserva      reservas.Reserva `json:"reservation"`
	// ReservaCriada é `false` quando a oportunidade já tinha reserva (a
	// pré-reserva da etapa anterior) e `/win` apenas a vinculou — a mesma
	// informação que o 200 em vez de 201 carrega, para o cliente que não olha o
	// status.
	ReservaCriada bool `json:"reservation_created"`
}

type ResultadoDePerda struct {
	Oportunidade Oportunidade      `json:"opportunity"`
	Reserva      *reservas.Reserva `json:"reservation"`
}

type ResultadoDeConclusao struct {
	Atividade Atividade  `json:"activity"`
	Proxima   *Atividade `json:"next_activity"`
}

// ═══════════════════════════ Entrada ════════════════════════════════

// FunilEntrada é o corpo do POST e do PUT de `/crm/pipelines`.
type FunilEntrada struct {
	Nome   string          `json:"name" validate:"required,min=1,max=120"`
	Padrao httpx.Opt[bool] `json:"is_default"`
	Ativo  httpx.Opt[bool] `json:"active"`
}

func (f FunilEntrada) Validar() map[string]string {
	falhas := map[string]string{}
	if f.Padrao.DeveLimpar() {
		falhas["is_default"] = "não pode ser nulo."
	}
	if f.Ativo.DeveLimpar() {
		falhas["active"] = "não pode ser nulo."
	}
	return falhas
}

// FunilAtualizar é o corpo do PATCH: ausente não muda.
type FunilAtualizar struct {
	Nome   httpx.Opt[string] `json:"name"`
	Padrao httpx.Opt[bool]   `json:"is_default"`
	Ativo  httpx.Opt[bool]   `json:"active"`
}

func (f FunilAtualizar) Validar() map[string]string {
	falhas := map[string]string{}
	if v, ok := f.Nome.Definido(); ok && !textoNaFaixa(v, 1, 120) {
		falhas["name"] = "de 1 a 120 caracteres."
	}
	if f.Nome.DeveLimpar() {
		falhas["name"] = "não pode ser nulo."
	}
	if f.Padrao.DeveLimpar() {
		falhas["is_default"] = "não pode ser nulo."
	}
	if f.Ativo.DeveLimpar() {
		falhas["active"] = "não pode ser nulo."
	}
	return falhas
}

// EtapaEntrada é o corpo do POST e do PUT de `/crm/stages`.
type EtapaEntrada struct {
	FunilID       uuid.UUID         `json:"pipeline_id" validate:"required"`
	Nome          string            `json:"name" validate:"required,min=1,max=120"`
	Posicao       httpx.Opt[int]    `json:"position"`
	Probabilidade httpx.Opt[int]    `json:"probability"`
	Cor           httpx.Opt[string] `json:"color"`
	Tipo          httpx.Opt[string] `json:"type"`

	SLADias         httpx.Opt[int]    `json:"sla_days"`
	TarefaAssunto   httpx.Opt[string] `json:"auto_task_subject"`
	TarefaTipo      httpx.Opt[string] `json:"auto_task_type"`
	TarefaPrazoDias httpx.Opt[int]    `json:"auto_task_due_days"`
	Notificar       httpx.Opt[bool]   `json:"auto_notify"`
}

func (e EtapaEntrada) Validar() map[string]string {
	return validarCamposDaEtapa(e.Posicao, e.Probabilidade, e.Cor, e.Tipo,
		e.SLADias, e.TarefaAssunto, e.TarefaTipo, e.TarefaPrazoDias, e.Notificar)
}

// EtapaAtualizar é o corpo do PATCH. `pipeline_id` fica de fora: etapa não
// troca de funil — mudar arrastaria as oportunidades dela junto, sem histórico.
type EtapaAtualizar struct {
	Nome          httpx.Opt[string] `json:"name"`
	Posicao       httpx.Opt[int]    `json:"position"`
	Probabilidade httpx.Opt[int]    `json:"probability"`
	Cor           httpx.Opt[string] `json:"color"`
	Tipo          httpx.Opt[string] `json:"type"`

	SLADias         httpx.Opt[int]    `json:"sla_days"`
	TarefaAssunto   httpx.Opt[string] `json:"auto_task_subject"`
	TarefaTipo      httpx.Opt[string] `json:"auto_task_type"`
	TarefaPrazoDias httpx.Opt[int]    `json:"auto_task_due_days"`
	Notificar       httpx.Opt[bool]   `json:"auto_notify"`
}

func (e EtapaAtualizar) Validar() map[string]string {
	falhas := validarCamposDaEtapa(e.Posicao, e.Probabilidade, e.Cor, e.Tipo,
		e.SLADias, e.TarefaAssunto, e.TarefaTipo, e.TarefaPrazoDias, e.Notificar)
	if v, ok := e.Nome.Definido(); ok && !textoNaFaixa(v, 1, 120) {
		falhas["name"] = "de 1 a 120 caracteres."
	}
	if e.Nome.DeveLimpar() {
		falhas["name"] = "não pode ser nulo."
	}
	return falhas
}

func validarCamposDaEtapa(posicao, probabilidade httpx.Opt[int], cor, tipo httpx.Opt[string],
	slaDias httpx.Opt[int], tarefaAssunto, tarefaTipo httpx.Opt[string],
	tarefaPrazo httpx.Opt[int], notificar httpx.Opt[bool]) map[string]string {

	falhas := map[string]string{}
	if v, ok := posicao.Definido(); ok && v < 0 {
		falhas["position"] = "deve ser maior ou igual a 0."
	}
	if posicao.DeveLimpar() {
		falhas["position"] = "não pode ser nulo."
	}
	if v, ok := probabilidade.Definido(); ok && (v < 0 || v > 100) {
		falhas["probability"] = "deve estar entre 0 e 100."
	}
	if probabilidade.DeveLimpar() {
		falhas["probability"] = "não pode ser nulo."
	}
	if v, ok := cor.Definido(); ok && !corHexadecimal(v) {
		falhas["color"] = "use o formato #RRGGBB."
	}
	if cor.DeveLimpar() {
		falhas["color"] = "não pode ser nulo."
	}
	if v, ok := tipo.Definido(); ok && v != EtapaAberta && v != EtapaGanho && v != EtapaPerdido {
		falhas["type"] = "deve ser um de: aberto, ganho, perdido."
	}
	if tipo.DeveLimpar() {
		falhas["type"] = "não pode ser nulo."
	}
	if v, ok := slaDias.Definido(); ok && v <= 0 {
		falhas["sla_days"] = "deve ser maior que 0; use null para desligar o SLA."
	}
	if v, ok := tarefaAssunto.Definido(); ok && !textoNaFaixa(v, 1, 200) {
		falhas["auto_task_subject"] = "de 1 a 200 caracteres."
	}
	if v, ok := tarefaTipo.Definido(); ok && !tipoDeAtividadeValido(v) {
		falhas["auto_task_type"] = "tipo de atividade desconhecido."
	}
	if v, ok := tarefaPrazo.Definido(); ok && v < 0 {
		falhas["auto_task_due_days"] = "deve ser maior ou igual a 0."
	}
	if notificar.DeveLimpar() {
		falhas["auto_notify"] = "não pode ser nulo."
	}
	return falhas
}

// PedidoDeReordenacao é o corpo do POST /crm/stages/reorder.
type PedidoDeReordenacao struct {
	FunilID  uuid.UUID   `json:"pipeline_id" validate:"required"`
	EtapaIDs []uuid.UUID `json:"stage_ids" validate:"required,min=1"`
}

func (p PedidoDeReordenacao) Validar() map[string]string {
	vistos := map[uuid.UUID]bool{}
	for _, id := range p.EtapaIDs {
		if vistos[id] {
			// Id repetido na lista: duas posições para a mesma etapa, e a
			// contagem bateria com o total do funil escondendo uma etapa
			// ausente. É o mesmo defeito que STAGE_ORDER_INCOMPLETE evita.
			return map[string]string{"stage_ids": "há identificador repetido na lista."}
		}
		vistos[id] = true
	}
	return nil
}

// MotivoDePerdaEntrada é o corpo do POST e do PUT.
type MotivoDePerdaEntrada struct {
	Rotulo string          `json:"label" validate:"required,min=1,max=200"`
	Ativo  httpx.Opt[bool] `json:"active"`
}

func (m MotivoDePerdaEntrada) Validar() map[string]string {
	if m.Ativo.DeveLimpar() {
		return map[string]string{"active": "não pode ser nulo."}
	}
	return nil
}

// MotivoDePerdaAtualizar é o corpo do PATCH.
type MotivoDePerdaAtualizar struct {
	Rotulo httpx.Opt[string] `json:"label"`
	Ativo  httpx.Opt[bool]   `json:"active"`
}

func (m MotivoDePerdaAtualizar) Validar() map[string]string {
	falhas := map[string]string{}
	if v, ok := m.Rotulo.Definido(); ok && !textoNaFaixa(v, 1, 200) {
		falhas["label"] = "de 1 a 200 caracteres."
	}
	if m.Rotulo.DeveLimpar() {
		falhas["label"] = "não pode ser nulo."
	}
	if m.Ativo.DeveLimpar() {
		falhas["active"] = "não pode ser nulo."
	}
	return falhas
}

// LeadCriar é o corpo do POST /crm/leads.
type LeadCriar struct {
	ContactID  uuid.UUID            `json:"contact_id" validate:"required"`
	Origem     httpx.Opt[string]    `json:"source"`
	CampanhaID httpx.Opt[uuid.UUID] `json:"campaign_id"`
	Status     httpx.Opt[string]    `json:"status"`
	Score      httpx.Opt[int]       `json:"score"`
	ProdutoID  httpx.Opt[uuid.UUID] `json:"interest_unit_type_id"`
	CheckIn    httpx.Opt[string]    `json:"desired_check_in"`
	CheckOut   httpx.Opt[string]    `json:"desired_check_out"`
	DonoID     httpx.Opt[uuid.UUID] `json:"owner_id"`
}

func (l LeadCriar) Validar() map[string]string {
	return validarCamposDoLead(l.Origem, l.Status, l.Score, l.CheckIn, l.CheckOut)
}

// LeadAtualizar é o corpo do PUT e do PATCH.
//
// `status: convertido` é recusado: quem converte é `/convert`, que cria a
// oportunidade na mesma transação. Marcar convertido na mão deixaria o lead
// dizendo que virou negócio sem negócio nenhum do outro lado.
type LeadAtualizar struct {
	ContactID  httpx.Opt[uuid.UUID] `json:"contact_id"`
	Origem     httpx.Opt[string]    `json:"source"`
	CampanhaID httpx.Opt[uuid.UUID] `json:"campaign_id"`
	Status     httpx.Opt[string]    `json:"status"`
	Score      httpx.Opt[int]       `json:"score"`
	ProdutoID  httpx.Opt[uuid.UUID] `json:"interest_unit_type_id"`
	CheckIn    httpx.Opt[string]    `json:"desired_check_in"`
	CheckOut   httpx.Opt[string]    `json:"desired_check_out"`
	DonoID     httpx.Opt[uuid.UUID] `json:"owner_id"`
}

func (l LeadAtualizar) Validar() map[string]string {
	falhas := validarCamposDoLead(l.Origem, l.Status, l.Score, l.CheckIn, l.CheckOut)
	if l.ContactID.DeveLimpar() {
		falhas["contact_id"] = "não pode ser nulo."
	}
	return falhas
}

func validarCamposDoLead(origem, status httpx.Opt[string], score httpx.Opt[int],
	checkIn, checkOut httpx.Opt[string]) map[string]string {

	falhas := map[string]string{}
	if v, ok := origem.Definido(); ok && !textoNaFaixa(v, 1, 40) {
		falhas["source"] = "de 1 a 40 caracteres."
	}
	if origem.DeveLimpar() {
		falhas["source"] = "não pode ser nulo."
	}
	if v, ok := status.Definido(); ok {
		switch v {
		case LeadNovo, LeadEmAtendimento, LeadQualificado, LeadDescartado:
		case LeadConvertido:
			falhas["status"] = "use POST /crm/leads/{id}/convert; converter cria a oportunidade na mesma transação."
		default:
			falhas["status"] = "deve ser um de: novo, em_atendimento, qualificado, descartado."
		}
	}
	if status.DeveLimpar() {
		falhas["status"] = "não pode ser nulo."
	}
	if v, ok := score.Definido(); ok && (v < 0 || v > 100) {
		falhas["score"] = "deve estar entre 0 e 100."
	}
	if v, ok := checkIn.Definido(); ok && !dataISO(v) {
		falhas["desired_check_in"] = "data inválida; use AAAA-MM-DD."
	}
	if v, ok := checkOut.Definido(); ok && !dataISO(v) {
		falhas["desired_check_out"] = "data inválida; use AAAA-MM-DD."
	}
	entrada, temEntrada := checkIn.Definido()
	saida, temSaida := checkOut.Definido()
	if temEntrada && temSaida && dataISO(entrada) && dataISO(saida) && saida <= entrada {
		falhas["desired_check_out"] = "deve ser depois de desired_check_in."
	}
	return falhas
}

// PedidoDeConversao é o corpo (opcional) do POST /crm/leads/{id}/convert. O que
// vier aqui SOBREPÕE o que o lead trazia; o que faltar é herdado dele.
type PedidoDeConversao struct {
	FunilID        httpx.Opt[uuid.UUID] `json:"pipeline_id"`
	EtapaID        httpx.Opt[uuid.UUID] `json:"stage_id"`
	ProdutoID      httpx.Opt[uuid.UUID] `json:"unit_type_id"`
	CheckIn        httpx.Opt[string]    `json:"check_in"`
	CheckOut       httpx.Opt[string]    `json:"check_out"`
	Valor          httpx.Opt[int64]     `json:"amount_cents"`
	FechamentoPrev httpx.Opt[string]    `json:"expected_close"`
	DonoID         httpx.Opt[uuid.UUID] `json:"owner_id"`
}

func (p PedidoDeConversao) Validar() map[string]string {
	falhas := map[string]string{}
	validarDataOpcional(falhas, "check_in", p.CheckIn)
	validarDataOpcional(falhas, "check_out", p.CheckOut)
	validarDataOpcional(falhas, "expected_close", p.FechamentoPrev)
	if v, ok := p.Valor.Definido(); ok && v < 0 {
		falhas["amount_cents"] = "não pode ser negativo."
	}
	return falhas
}

// OportunidadeCriar é o corpo do POST /crm/opportunities.
type OportunidadeCriar struct {
	ContactID      uuid.UUID                         `json:"contact_id" validate:"required"`
	LeadID         httpx.Opt[uuid.UUID]              `json:"lead_id"`
	FunilID        httpx.Opt[uuid.UUID]              `json:"pipeline_id"`
	EtapaID        httpx.Opt[uuid.UUID]              `json:"stage_id"`
	ProdutoID      httpx.Opt[uuid.UUID]              `json:"unit_type_id"`
	CheckIn        httpx.Opt[string]                 `json:"check_in"`
	CheckOut       httpx.Opt[string]                 `json:"check_out"`
	Valor          httpx.Opt[int64]                  `json:"amount_cents"`
	Probabilidade  httpx.Opt[int]                    `json:"probability"`
	FechamentoPrev httpx.Opt[string]                 `json:"expected_close"`
	DonoID         httpx.Opt[uuid.UUID]              `json:"owner_id"`
	Evento         httpx.Opt[DetalheDeEventoEntrada] `json:"event"`
}

func (o OportunidadeCriar) Validar() map[string]string {
	return validarCamposDaOportunidade(o.CheckIn, o.CheckOut, o.Valor,
		o.Probabilidade, o.FechamentoPrev, o.Evento)
}

// OportunidadeAtualizar é o corpo do PUT e do PATCH.
//
// `stage_id`, `status`, `lost_reason_id`, `quote_id` e `reservation_id` NÃO
// estão aqui: cada um tem uma ação com efeito colateral obrigatório (`/stage`,
// `/win`, `/lose`). Um PATCH que movesse a etapa não gravaria
// `crm_stage_history`, e a conversão por etapa do BI passaria a mentir.
type OportunidadeAtualizar struct {
	ContactID      httpx.Opt[uuid.UUID]              `json:"contact_id"`
	LeadID         httpx.Opt[uuid.UUID]              `json:"lead_id"`
	ProdutoID      httpx.Opt[uuid.UUID]              `json:"unit_type_id"`
	CheckIn        httpx.Opt[string]                 `json:"check_in"`
	CheckOut       httpx.Opt[string]                 `json:"check_out"`
	Valor          httpx.Opt[int64]                  `json:"amount_cents"`
	Probabilidade  httpx.Opt[int]                    `json:"probability"`
	FechamentoPrev httpx.Opt[string]                 `json:"expected_close"`
	DonoID         httpx.Opt[uuid.UUID]              `json:"owner_id"`
	Evento         httpx.Opt[DetalheDeEventoEntrada] `json:"event"`
}

func (o OportunidadeAtualizar) Validar() map[string]string {
	falhas := validarCamposDaOportunidade(o.CheckIn, o.CheckOut, o.Valor,
		o.Probabilidade, o.FechamentoPrev, o.Evento)
	if o.ContactID.DeveLimpar() {
		falhas["contact_id"] = "não pode ser nulo."
	}
	return falhas
}

// DetalheDeEventoEntrada é o corpo do satélite de evento.
type DetalheDeEventoEntrada struct {
	TipoDeEvento   httpx.Opt[string] `json:"event_type"`
	ConvidadosPrev httpx.Opt[int]    `json:"guests_expected"`
	PrecisaBuffet  httpx.Opt[bool]   `json:"needs_catering"`
	Observacoes    httpx.Opt[string] `json:"notes"`
}

func validarCamposDaOportunidade(checkIn, checkOut httpx.Opt[string],
	valor httpx.Opt[int64], probabilidade httpx.Opt[int], fechamento httpx.Opt[string],
	evento httpx.Opt[DetalheDeEventoEntrada]) map[string]string {

	falhas := map[string]string{}
	validarDataOpcional(falhas, "check_in", checkIn)
	validarDataOpcional(falhas, "check_out", checkOut)
	validarDataOpcional(falhas, "expected_close", fechamento)

	entrada, temEntrada := checkIn.Definido()
	saida, temSaida := checkOut.Definido()
	if temEntrada && temSaida && dataISO(entrada) && dataISO(saida) && saida <= entrada {
		falhas["check_out"] = "deve ser depois de check_in."
	}
	if v, ok := valor.Definido(); ok && v < 0 {
		falhas["amount_cents"] = "não pode ser negativo."
	}
	if valor.DeveLimpar() {
		falhas["amount_cents"] = "não pode ser nulo."
	}
	if v, ok := probabilidade.Definido(); ok && (v < 0 || v > 100) {
		falhas["probability"] = "deve estar entre 0 e 100."
	}
	if e, ok := evento.Definido(); ok {
		if v, ok := e.ConvidadosPrev.Definido(); ok && v <= 0 {
			falhas["event.guests_expected"] = "deve ser maior que 0."
		}
		if v, ok := e.Observacoes.Definido(); ok && len(v) > 2000 {
			falhas["event.notes"] = "no máximo 2000 caracteres."
		}
	}
	return falhas
}

// PedidoDeMudancaDeEtapa é o corpo do POST /crm/opportunities/{id}/stage.
type PedidoDeMudancaDeEtapa struct {
	EtapaID uuid.UUID `json:"stage_id" validate:"required"`
	// DeEtapaID é a guarda otimista do kanban: informada e diferente da etapa
	// atual, a resposta é 409 com `details.current_stage_id`. É o que impede
	// dois corretores arrastando o mesmo card de terminar com o último clique
	// vencendo em silêncio.
	DeEtapaID     httpx.Opt[uuid.UUID] `json:"from_stage_id"`
	Motivo        httpx.Opt[string]    `json:"reason"`
	Probabilidade httpx.Opt[int]       `json:"probability"`
}

func (p PedidoDeMudancaDeEtapa) Validar() map[string]string {
	falhas := map[string]string{}
	if v, ok := p.Motivo.Definido(); ok && len(v) > 500 {
		falhas["reason"] = "no máximo 500 caracteres."
	}
	if v, ok := p.Probabilidade.Definido(); ok && (v < 0 || v > 100) {
		falhas["probability"] = "deve estar entre 0 e 100."
	}
	return falhas
}

// PedidoDeGanho é o corpo (opcional) do POST /crm/opportunities/{id}/win.
type PedidoDeGanho struct {
	OrcamentoID httpx.Opt[uuid.UUID] `json:"quote_id"`
	Nota        httpx.Opt[string]    `json:"note"`
}

func (p PedidoDeGanho) Validar() map[string]string {
	if v, ok := p.Nota.Definido(); ok && len(v) > 500 {
		return map[string]string{"note": "no máximo 500 caracteres."}
	}
	return nil
}

// PedidoDePerda é o corpo do POST /crm/opportunities/{id}/lose.
type PedidoDePerda struct {
	MotivoID uuid.UUID         `json:"lost_reason_id" validate:"required"`
	Nota     httpx.Opt[string] `json:"note"`
}

func (p PedidoDePerda) Validar() map[string]string {
	if v, ok := p.Nota.Definido(); ok && len(v) > 500 {
		return map[string]string{"note": "no máximo 500 caracteres."}
	}
	return nil
}

// AtividadeCriar é o corpo do POST /crm/activities.
//
// `auto` NÃO é aceito: automática é a tarefa que o SERVIDOR criou ao entrar
// numa etapa, e deixar o cliente se declarar automática quebraria a
// idempotência dessa criação (a parcial única do banco passaria a ser disputada
// por linhas que ninguém controla).
type AtividadeCriar struct {
	Tipo           string               `json:"type" validate:"required"`
	Assunto        string               `json:"subject" validate:"required,min=1,max=200"`
	Descricao      httpx.Opt[string]    `json:"description"`
	VenceEm        httpx.Opt[time.Time] `json:"due_at"`
	Prioridade     httpx.Opt[string]    `json:"priority"`
	LeadID         httpx.Opt[uuid.UUID] `json:"lead_id"`
	OportunidadeID httpx.Opt[uuid.UUID] `json:"opportunity_id"`
	ContactID      httpx.Opt[uuid.UUID] `json:"contact_id"`
	DonoID         httpx.Opt[uuid.UUID] `json:"owner_id"`
}

func (a AtividadeCriar) Validar() map[string]string {
	falhas := map[string]string{}
	if !tipoDeAtividadeValido(a.Tipo) {
		falhas["type"] = "deve ser um de: tarefa, ligacao, reuniao, email, whatsapp, nota."
	}
	if v, ok := a.Prioridade.Definido(); ok && !prioridadeValida(v) {
		falhas["priority"] = "deve ser um de: baixa, normal, alta."
	}
	_, temLead := a.LeadID.Definido()
	_, temOportunidade := a.OportunidadeID.Definido()
	_, temContato := a.ContactID.Definido()
	if !temLead && !temOportunidade && !temContato {
		// Atividade solta não aparece em tela nenhuma e nunca mais é vista. O
		// CHECK do banco recusa igual; recusar aqui é o que nomeia o campo.
		falhas["opportunity_id"] = "informe ao menos um vínculo: opportunity_id, lead_id ou contact_id."
	}
	return falhas
}

// AtividadeAtualizar é o corpo do PUT e do PATCH.
//
// `done_at` fica de fora e `status: concluida` é recusado: concluir é
// `POST /{id}/complete`, com o relógio do servidor. `done_at` escrito pelo
// cliente é o tempo médio de resposta do §15 medido pelo relógio do navegador.
type AtividadeAtualizar struct {
	Tipo       httpx.Opt[string]    `json:"type"`
	Assunto    httpx.Opt[string]    `json:"subject"`
	Descricao  httpx.Opt[string]    `json:"description"`
	VenceEm    httpx.Opt[time.Time] `json:"due_at"`
	Prioridade httpx.Opt[string]    `json:"priority"`
	DonoID     httpx.Opt[uuid.UUID] `json:"owner_id"`
	Status     httpx.Opt[string]    `json:"status"`
}

func (a AtividadeAtualizar) Validar() map[string]string {
	falhas := map[string]string{}
	if v, ok := a.Tipo.Definido(); ok && !tipoDeAtividadeValido(v) {
		falhas["type"] = "deve ser um de: tarefa, ligacao, reuniao, email, whatsapp, nota."
	}
	if v, ok := a.Assunto.Definido(); ok && !textoNaFaixa(v, 1, 200) {
		falhas["subject"] = "de 1 a 200 caracteres."
	}
	if a.Assunto.DeveLimpar() {
		falhas["subject"] = "não pode ser nulo."
	}
	if v, ok := a.Prioridade.Definido(); ok && !prioridadeValida(v) {
		falhas["priority"] = "deve ser um de: baixa, normal, alta."
	}
	if v, ok := a.Status.Definido(); ok {
		switch v {
		case AtividadePendente, AtividadeCancelada:
		case AtividadeConcluida:
			falhas["status"] = "use POST /crm/activities/{id}/complete; a conclusão é carimbada pelo servidor."
		default:
			falhas["status"] = "deve ser um de: pendente, cancelada."
		}
	}
	if a.Status.DeveLimpar() {
		falhas["status"] = "não pode ser nulo."
	}
	return falhas
}

// PedidoDeConclusao é o corpo (opcional) do POST /crm/activities/{id}/complete.
type PedidoDeConclusao struct {
	Nota    httpx.Opt[string]           `json:"note"`
	Proxima httpx.Opt[ProximaAtividade] `json:"next_activity"`
}

func (p PedidoDeConclusao) Validar() map[string]string {
	falhas := map[string]string{}
	if v, ok := p.Nota.Definido(); ok && len(v) > 1000 {
		falhas["note"] = "no máximo 1000 caracteres."
	}
	if prox, ok := p.Proxima.Definido(); ok {
		for campo, msg := range prox.Validar() {
			falhas["next_activity."+campo] = msg
		}
	}
	return falhas
}

// ProximaAtividade é o "concluir e agendar o próximo passo". Herda
// `opportunity_id`, `lead_id`, `contact_id` e `owner_id` da atividade concluída.
type ProximaAtividade struct {
	Tipo       string               `json:"type"`
	Assunto    string               `json:"subject"`
	VenceEm    time.Time            `json:"due_at"`
	Prioridade httpx.Opt[string]    `json:"priority"`
	DonoID     httpx.Opt[uuid.UUID] `json:"owner_id"`
}

func (p ProximaAtividade) Validar() map[string]string {
	falhas := map[string]string{}
	if !tipoDeAtividadeValido(p.Tipo) {
		falhas["type"] = "deve ser um de: tarefa, ligacao, reuniao, email, whatsapp, nota."
	}
	if !textoNaFaixa(p.Assunto, 1, 200) {
		falhas["subject"] = "de 1 a 200 caracteres."
	}
	if p.VenceEm.IsZero() {
		falhas["due_at"] = "é obrigatório."
	}
	if v, ok := p.Prioridade.Definido(); ok && !prioridadeValida(v) {
		falhas["priority"] = "deve ser um de: baixa, normal, alta."
	}
	return falhas
}

// ═══════════════════════════ Filtros de listagem ════════════════════

// Ordenações permitidas. Nada do que o cliente digitou entra na consulta: só o
// valor mapeado por estas tabelas, senão `sort` seria injeção por definição.
var (
	OrdemDeLeads = map[string]string{
		"created_at": "l.created_at",
		"score":      "l.score",
	}
	OrdemDeOportunidades = map[string]string{
		"created_at":       "o.created_at",
		"updated_at":       "o.updated_at",
		"amount_cents":     "o.amount_cents",
		"expected_close":   "o.expected_close",
		"entered_stage_at": "o.entered_stage_at",
	}
	OrdemDeAtividades = map[string]string{
		"due_at":     "a.due_at",
		"created_at": "a.created_at",
		// A prioridade ordena pelo PESO, não pelo alfabeto: por texto, "alta"
		// viria antes de "baixa" e de "normal" por acaso, e o dia em que alguém
		// acrescentasse "urgente" a ordem mudaria sozinha.
		"priority": "CASE a.priority WHEN 'alta' THEN 3 WHEN 'normal' THEN 2 ELSE 1 END",
	}
)

const (
	OrdemPadraoDeLeads         = "l.created_at DESC"
	OrdemPadraoDeOportunidades = "o.updated_at DESC"
	// Pendente primeiro, prazo mais próximo no topo, e quem não tem prazo por
	// último: é a caixa de entrada do corretor, não uma lista alfabética.
	OrdemPadraoDeAtividades = "a.due_at ASC NULLS LAST"
)

// FiltroDeLeads é o recorte do GET /crm/leads.
type FiltroDeLeads struct {
	Status    []string
	Origem    string
	DonoID    *uuid.UUID
	ContactID *uuid.UUID
	Busca     string
	De, Ate   string
	OrderBy   string
	Pagina    int
	PorPagina int

	// SomenteMinhas/Usuario traduzem o escopo `own` do RBAC. Viram
	// `AND l.owner_id = $usuario` no SQL — nunca peneira em memória.
	SomenteMinhas bool
	Usuario       uuid.UUID
}

// FiltroDeOportunidades é o recorte do GET /crm/opportunities.
type FiltroDeOportunidades struct {
	FunilID                     *uuid.UUID
	EtapaID                     *uuid.UUID
	Status                      []string
	DonoID                      *uuid.UUID
	ContactID                   *uuid.UUID
	ProdutoID                   *uuid.UUID
	MotivoID                    *uuid.UUID
	Busca                       string
	FechamentoDe, FechamentoAte string
	// SLA é `estourado` ou `no_prazo` — recorte DERIVADO, calculado no servidor.
	SLA       string
	OrderBy   string
	Pagina    int
	PorPagina int

	SomenteMinhas bool
	Usuario       uuid.UUID
}

// FiltroDeAtividades é o recorte do GET /crm/activities.
type FiltroDeAtividades struct {
	Tipos             []string
	Status            []string
	DonoID            *uuid.UUID
	OportunidadeID    *uuid.UUID
	LeadID            *uuid.UUID
	ContactID         *uuid.UUID
	Auto              *bool
	VenceDe, VenceAte string
	Vencidas          *bool
	OrderBy           string
	Pagina            int
	PorPagina         int

	SomenteMinhas bool
	Usuario       uuid.UUID
}

// FiltroDoKanban é o recorte do GET /crm/opportunities/kanban.
type FiltroDoKanban struct {
	FunilID         *uuid.UUID
	PorColuna       int
	DonoID          *uuid.UUID
	Busca           string
	ProdutoID       *uuid.UUID
	De, Ate         string
	IncluirFechadas bool

	SomenteMinhas bool
	Usuario       uuid.UUID
}

// PorColunaPadrao e PorColunaTeto espelham o contrato. O teto existe porque uma
// etapa com 5.000 cards não pode derrubar a tela na primeira abertura.
const (
	PorColunaPadrao = 50
	PorColunaTeto   = 200
	// JanelaDeFechadasEmDias é o recorte padrão das colunas terminais: sem ele,
	// a coluna "Perdido" de um ano de operação vira a coluna mais pesada do
	// quadro e é carregada em toda abertura.
	JanelaDeFechadasEmDias = 30
)

// ═══════════════════════════ Auxiliares de forma ════════════════════

func textoNaFaixa(v string, min, max int) bool {
	n := len([]rune(strings.TrimSpace(v)))
	return n >= min && n <= max
}

func corHexadecimal(v string) bool {
	if len(v) != 7 || v[0] != '#' {
		return false
	}
	for _, c := range v[1:] {
		// nolint:staticcheck // QF1001: "não é dígito hexadecimal" se lê de uma vez na
		// forma negada; aplicar De Morgan viraria uma corrente de seis comparações.
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func tipoDeAtividadeValido(v string) bool {
	switch v {
	case AtividadeTarefa, AtividadeLigacao, AtividadeReuniao, AtividadeEmail, AtividadeWhatsApp, AtividadeNota:
		return true
	}
	return false
}

func prioridadeValida(v string) bool {
	return v == PrioridadeBaixa || v == PrioridadeNormal || v == PrioridadeAlta
}

func dataISO(v string) bool {
	_, err := time.Parse(time.DateOnly, v)
	return err == nil
}

func validarDataOpcional(falhas map[string]string, campo string, v httpx.Opt[string]) {
	if valor, ok := v.Definido(); ok && !dataISO(valor) {
		falhas[campo] = "data inválida; use AAAA-MM-DD."
	}
}

// ValidarDataDaQuery recusa filtro de data ilegível em vez de ignorá-lo.
// Filtro silenciosamente descartado é a listagem mentindo sobre o recorte.
func ValidarDataDaQuery(campo, valor string) error {
	if valor == "" || dataISO(valor) {
		return nil
	}
	return apperr.Validation(map[string]string{campo: "data inválida; use AAAA-MM-DD."})
}

// ListaDaQuery quebra `a,b,c` e valida cada item contra o vocabulário. Valor
// desconhecido é 422, e não filtro ignorado: devolver a lista inteira fingindo
// que filtrou é pior que recusar.
func ListaDaQuery(campo, bruto string, valido func(string) bool) ([]string, error) {
	bruto = strings.TrimSpace(bruto)
	if bruto == "" {
		return nil, nil
	}
	var out []string
	for _, parte := range strings.Split(bruto, ",") {
		v := strings.TrimSpace(parte)
		if v == "" {
			continue
		}
		if !valido(v) {
			return nil, apperr.Validation(map[string]string{campo: "valor desconhecido: " + v})
		}
		out = append(out, v)
	}
	return out, nil
}
