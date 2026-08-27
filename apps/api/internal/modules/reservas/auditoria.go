package reservas

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
)

// A trilha de `audit_log` deste módulo — spec §16: "toda escrita grava ator,
// entidade, antes e depois, IP e request_id".
//
// A Fase 1 não cumpria. A revisão mediu: uma execução da suíte de integração de
// inventário, reservas e tarifário escreveu 1626 tuplas nas tabelas de negócio e
// ZERO linhas em `audit_log`. Sem elas não existe resposta para "quem cancelou
// esta reserva?", "quem registrou um sinal de R$ 960.000?" ou "quem bloqueou a
// casa inteira por um ano?" — e essas três perguntas são justamente as que a
// revisão fez.
//
// COMO AS CHAMADAS SÃO FEITAS: `audit.Registrar` pega o executor por `db.From`,
// então uma chamada DENTRO do `tx.Do` do negócio entra na transação do negócio.
// Isso importa nos dois sentidos — o rollback da venda leva a trilha junto (não
// existe cancelamento auditado que não aconteceu), e o commit da venda não
// acontece sem a trilha. Por isso toda chamada daqui está dentro do `tx.Do`, e
// o erro dela é RETORNADO, nunca engolido: auditoria que falha em silêncio é
// pior que auditoria ausente, porque parece que existe.
//
// Ator, IP, user-agent, propriedade e request_id NÃO são digitados: saem do
// contexto. É o que torna difícil esquecê-los — não existe parâmetro para
// esquecer.

// As entidades auditadas por este módulo, no nome da TABELA.
const (
	entidadeReserva = "reservations"
	entidadeBloco   = "stay_blocks"
)

// Os verbos. Os genéricos vêm de `audit.Verbo*` para a tela de auditoria poder
// agrupar; os específicos existem porque "cancelada" e "remarcada" não são a
// mesma coisa que "alterada" para quem lê a trilha depois.
const (
	VerboConfirmada    = "confirmada"
	VerboCancelada     = "cancelada"
	VerboRemarcada     = "remarcada"
	VerboCheckIn       = "check_in"
	VerboCheckOut      = "check_out"
	VerboRealocada     = "realocada"
	VerboHoldEstendido = "hold_estendido"
	VerboBloqueada     = "bloqueada"
	VerboDesbloqueada  = "desbloqueada"
)

// reservaAuditada é o recorte que a trilha guarda em toda transição de estado.
//
// Struct tipada, e não mapa: `audit.Snapshot` serializa pelas tags `json`, então
// os nomes na trilha ficam iguais aos da API (`total_cents`, e não `Total`) — e
// é assim que a tela de auditoria consegue exibir a linha sem um tradutor por
// entidade. O `audit.Diff` recorta para o que mudou, que é o que faz a linha
// responder "quem confirmou por quanto" de relance em vez de repetir a reserva
// inteira duas vezes.
type reservaAuditada struct {
	Codigo       string     `json:"code"`
	Status       string     `json:"status"`
	CheckIn      string     `json:"check_in"`
	CheckOut     string     `json:"check_out"`
	Hospedes     int        `json:"guests_count"`
	Total        int64      `json:"total_cents"`
	SinalPago    *int64     `json:"deposit_paid_cents,omitempty"`
	HoldExpiraEm *time.Time `json:"hold_expires_at,omitempty"`
	// Unidades só aparece onde ela é o assunto (realocação): carregá-la em toda
	// linha encheria a trilha com o campo que nunca muda.
	Unidades []string `json:"unit_codes,omitempty"`
}

// instantaneo tira o retrato do estado travado. É a mesma função dos dois lados
// da edição de propósito: `antes` e `depois` construídos por caminhos diferentes
// produziriam um diff que acusa mudança onde só houve mudança de formato.
func instantaneo(e Estado) reservaAuditada {
	return reservaAuditada{
		Codigo:       e.Codigo,
		Status:       e.Status,
		CheckIn:      e.CheckIn,
		CheckOut:     e.CheckOut,
		Hospedes:     e.Hospedes,
		Total:        e.Total,
		HoldExpiraEm: e.HoldExpiraEm,
	}
}

// comStatus devolve o retrato com o estado já movido — é o `depois` das ações
// que só mudam a máquina de estados.
func (r reservaAuditada) comStatus(status string) reservaAuditada {
	r.Status = status
	return r
}

func (r reservaAuditada) comSinalPago(pago int64) reservaAuditada {
	r.SinalPago = &pago
	return r
}

func (r reservaAuditada) comUnidades(codigos ...string) reservaAuditada {
	r.Unidades = codigos
	return r
}

// cadastroAuditado é o recorte do PUT/PATCH: só os campos que aquelas rotas
// editam. Datas, produto e preço não estão aqui porque não passam por lá.
type cadastroAuditado struct {
	ContactID   uuid.UUID  `json:"contact_id"`
	BrokerID    *uuid.UUID `json:"broker_id"`
	Hospedes    int        `json:"guests_count"`
	IsEvento    bool       `json:"is_event"`
	TipoEvento  *string    `json:"event_type"`
	Origem      string     `json:"source"`
	Observacoes *string    `json:"notes"`
}

// ─────────────────────────── Atalhos ────────────────────────────────

// auditarReserva grava a alteração de uma reserva na transação em curso.
func (s *Servico) auditarReserva(ctx context.Context, verbo string, id uuid.UUID, antes, depois any) error {
	return audit.Alteracao(ctx, s.repo.pool, entidadeReserva, verbo, id, antes, depois)
}

// auditarBloco grava a criação/remoção de uma linha de calendário.
func (s *Servico) auditarBloco(ctx context.Context, verbo string, b Bloqueio, excluido bool) error {
	if excluido {
		return audit.Exclusao(ctx, s.repo.pool, entidadeBloco, verbo, b.ID, b)
	}
	return audit.Criacao(ctx, s.repo.pool, entidadeBloco, verbo, b.ID, b)
}
