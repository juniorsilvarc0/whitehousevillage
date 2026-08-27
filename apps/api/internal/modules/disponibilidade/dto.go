// Package disponibilidade responde as três perguntas do calendário comercial:
// "dá para vender?" (`GET /availability`), "quem está onde?"
// (`GET /availability/units`) e "quanto custa?" (`POST /quotes`).
//
// O CÁLCULO NÃO MORA AQUI. Tarifa, precedência de tipo de data, estadia mínima,
// desconto e alçada vivem em internal/domain/booking e internal/domain/calendar,
// que são puros e testados. Este módulo faz a única coisa que o domínio não pode
// fazer: carrega o estado do banco, entrega ao motor e traduz a saída para o
// contrato. Toda regra reescrita aqui seria uma segunda verdade sobre o preço.
package disponibilidade

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/booking"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

// janelaMaximaEmDias é o teto da consulta de calendário.
//
// 366 e não 365 para caber um ano bissexto inteiro numa requisição só. O teto
// existe porque a consulta é um produto cartesiano dias × unidades: sem ele,
// `from=2026-01-01&to=2126-01-01` pediria 292 mil células ao Postgres.
const janelaMaximaEmDias = 366

// Valores de `unit_types.consumes` — o CHECK da migration 20260820130000.
const (
	ConsomeUma   = "one_member"  // apto-2s, suite-piscina, cobertura
	ConsomeTodas = "all_members" // White House Completa: consome as oito unidades
)

// Status de uma célula do mapa de ocupação (enum do contrato).
const (
	StatusLivre        = "livre"
	StatusHold         = "hold"
	StatusConfirmado   = "confirmed"
	StatusManutencao   = "maintenance"
	StatusProprietario = "owner_hold"
	StatusOTA          = "ota"

	// StatusConcluido é a estadia CUMPRIDA — o bloco `completed` que o
	// /check-out grava. Ela não bloqueia venda nenhuma (a constraint
	// `stay_no_overlap` não a enxerga) e mesmo assim precisa aparecer no mapa:
	// é dela que saem ocupação, ADR e RevPAR. O nome no contrato é
	// `checked_out`, e não `completed`, porque a célula fala a língua da
	// RESERVA (que passou por check-out), não a do bloco.
	StatusConcluido = "checked_out"
)

// Motivos de `unavailable_reason` — por que `available` é 0.
//
// A precedência entre eles é a do contrato e não é arbitrária: ela ordena pelo
// que a GESTÃO precisa consertar primeiro. `composicao_incompleta` e
// `sem_tarifa` são configuração pela metade (alguém tem de agir hoje);
// `ocupado` é o funcionamento normal do negócio (não há o que consertar).
const (
	MotivoComposicaoIncompleta = "composicao_incompleta"
	MotivoSemTarifa            = "sem_tarifa"
	MotivoUnidadeInativa       = "unidade_inativa"
	MotivoOcupado              = "ocupado"
)

// Janela é o intervalo consultado, HALF-OPEN `[De, Ate)`: `Ate` não entra.
//
// É a mesma convenção da estadia (`[check_in, check_out)`) de propósito — pedir
// de 20 a 23 devolve as noites 20, 21 e 22, exatamente as três que seriam
// cobradas. Um intervalo fechado aqui e half-open lá faria o mapa e o orçamento
// discordarem sobre quantas noites existem.
type Janela struct {
	De  calendar.Date
	Ate calendar.Date
}

// Noites devolve as datas de `[De, Ate)`.
func (j Janela) Noites() []calendar.Date { return calendar.Range(j.De, j.Ate) }

// NovaJanela valida e monta a janela a partir do que veio na query string.
//
// Devolve apperr.Validation com o campo exato porque o painel gruda a mensagem
// no input errado — `to` inválido tem de acender o campo `to`, não o formulário.
func NovaJanela(de, ate string) (Janela, error) {
	falhas := map[string]string{}

	inicio, err := calendar.Parse(de)
	if de == "" {
		falhas["from"] = "é obrigatório."
	} else if err != nil {
		falhas["from"] = "data inválida: use AAAA-MM-DD."
	}

	fim, err := calendar.Parse(ate)
	if ate == "" {
		falhas["to"] = "é obrigatório."
	} else if err != nil {
		falhas["to"] = "data inválida: use AAAA-MM-DD."
	}

	if len(falhas) > 0 {
		return Janela{}, apperr.Validation(falhas)
	}

	if !inicio.Before(fim) {
		return Janela{}, apperr.Validation(map[string]string{
			"to": "deve ser posterior a `from` (o intervalo é half-open: `to` não entra).",
		})
	}
	if dias := inicio.Nights(fim); dias > janelaMaximaEmDias {
		return Janela{}, apperr.Validation(map[string]string{
			"to": fmt.Sprintf("janela de %d dias acima do máximo de %d.", dias, janelaMaximaEmDias),
		})
	}
	return Janela{De: inicio, Ate: fim}, nil
}

// JanelaDaRequisicao lê `from`/`to` da query string.
func JanelaDaRequisicao(r *http.Request) (Janela, error) {
	q := r.URL.Query()
	return NovaJanela(q.Get("from"), q.Get("to"))
}

// UUIDOpcionalDaQuery lê um filtro `?campo=<uuid>`: ausente é nil (não filtra),
// presente e ilegível é 422 — e não "ignora e devolve tudo", que faria a tela
// mostrar o calendário inteiro achando que filtrou.
func UUIDOpcionalDaQuery(r *http.Request, campo string) (*uuid.UUID, error) {
	bruto := r.URL.Query().Get(campo)
	if bruto == "" {
		return nil, nil
	}
	id, err := uuid.Parse(bruto)
	if err != nil {
		return nil, apperr.Validation(map[string]string{campo: "identificador inválido."})
	}
	return &id, nil
}

// ─────────────────────────── Saída: /availability ───────────────────────────

// DiaDoProduto é uma célula da visão comercial.
type DiaDoProduto struct {
	Data string `json:"date"`
	// Disponivel é quantas unidades da composição dá para vender nesse dia.
	// Para `all_members` é 0 ou 1 — nunca um número intermediário.
	Disponivel int               `json:"available"`
	TipoDeData calendar.DateType `json:"date_type"`
	// Preco é nulo quando não há tarifa cadastrada para o tipo de data — a
	// mesma condição que geraria RATE_NOT_FOUND no orçamento. Nulo é mais
	// honesto que zero: zero é uma diária de graça.
	Preco     *int64 `json:"price_cents"`
	MinNoites int    `json:"min_nights"`
	// Motivo é nulo quando Disponivel > 0. Ele é o que separa "já vendido"
	// (avisar o hóspede) de "sem tarifa cadastrada" (avisar a gestão) — sem
	// ele a tela pinta duas células cinzas idênticas com causas opostas.
	Motivo *string `json:"unavailable_reason"`
}

// DisponibilidadeDoProduto é uma linha da visão comercial.
type DisponibilidadeDoProduto struct {
	UnitTypeID   uuid.UUID `json:"unit_type_id"`
	UnitTypeCode string    `json:"unit_type_code"`
	Nome         string    `json:"name"`
	Consome      string    `json:"consumes"`
	// TotalUnidades é o tamanho DECLARADO da composição
	// (`unit_type_members`), ativas ou não: é o que o produto promete. Não
	// encolhe porque alguém desativou uma unidade — encolher aqui esconderia
	// exatamente o defeito que `active_units` existe para mostrar.
	TotalUnidades int `json:"total_units"`
	// UnidadesAtivas é quantas dessas estão de pé. Para `all_members`,
	// UnidadesAtivas < TotalUnidades é produto que NÃO PODE SER ENTREGUE: a
	// Completa é a casa inteira, e 7 de 8 não é a casa inteira.
	UnidadesAtivas int            `json:"active_units"`
	Dias           []DiaDoProduto `json:"days"`
}

// ─────────────────────────── Saída: /availability/units ─────────────────────

// DiaDaUnidade é uma célula do mapa de ocupação.
//
// Sem preço de propósito: preço é do PRODUTO, não da unidade — a mesma `AP-01`
// custa uma coisa vendida como Apartamento 2 Suítes e outra dentro da White
// House Completa. Quem quer valor consulta /availability ou POST /quotes.
type DiaDaUnidade struct {
	Data          string            `json:"date"`
	Status        string            `json:"status"`
	TipoDeData    calendar.DateType `json:"date_type"`
	StayBlockID   *uuid.UUID        `json:"stay_block_id"`
	ReservaID     *uuid.UUID        `json:"reservation_id"`
	ReservaCodigo *string           `json:"reservation_code"`
	Hospede       *string           `json:"guest_name"`
}

// LinhaDoMapa é uma unidade física e seus dias, na ordem de `units.code`.
type LinhaDoMapa struct {
	UnitID   uuid.UUID      `json:"unit_id"`
	UnitCode string         `json:"unit_code"`
	UnitName string         `json:"unit_name"`
	Dias     []DiaDaUnidade `json:"days"`
}

// ─────────────────────────── Entrada: POST /quotes ──────────────────────────

// Pedido é o corpo de POST /quotes (schema PedidoDeOrcamento).
//
// Nenhum VALOR entra por aqui: o servidor recalcula tudo pelo motor. As datas
// chegam como texto e são convertidas em Normalizar — `time.Time` num campo de
// estadia é a origem clássica da reserva que pula um dia.
type Pedido struct {
	UnitTypeID uuid.UUID `json:"unit_type_id" validate:"required"`
	CheckIn    string    `json:"check_in" validate:"required"`
	CheckOut   string    `json:"check_out" validate:"required"`
	// `guests_count`, e não `guests`: até 26/08/2026 o mesmo dado tinha dois
	// nomes — `guests` aqui, `guests_count` na edição e em toda resposta —
	// e quem escrevia o cliente acertava a criação e errava a edição sem
	// receber erro. O contrato (openapi.yaml, PedidoDeOrcamento) e o painel
	// (`src/lib/comercial/orcamento.ts`) unificaram no nome longo; o nome
	// curto passou a ser campo desconhecido, e httpx.Decode o recusa.
	Hospedes    int     `json:"guests_count" validate:"required,min=1"`
	DescontoPct float64 `json:"discount_pct" validate:"min=0,max=100"`
	IsEvento    bool    `json:"is_event"`

	// RateTableID e PolicyVersion existem para REPRODUZIR um orçamento antigo
	// centavo a centavo — nunca para o cliente escolher preço na venda.
	RateTableID   *uuid.UUID `json:"rate_table_id"`
	PolicyVersion *int       `json:"policy_version"`
}

// Validar cobre só o FORMATO das datas. Se `check_out` é posterior a `check_in`
// é regra de negócio e quem decide é booking.Build — duplicar aqui criaria dois
// lugares para o mesmo "não".
func (p Pedido) Validar() map[string]string {
	falhas := map[string]string{}
	if p.CheckIn != "" {
		if _, err := calendar.Parse(p.CheckIn); err != nil {
			falhas["check_in"] = "data inválida: use AAAA-MM-DD."
		}
	}
	if p.CheckOut != "" {
		if _, err := calendar.Parse(p.CheckOut); err != nil {
			falhas["check_out"] = "data inválida: use AAAA-MM-DD."
		}
	}
	if p.PolicyVersion != nil && *p.PolicyVersion < 1 {
		falhas["policy_version"] = "deve ser no mínimo 1."
	}
	return falhas
}

// Entrada é o Pedido já convertido para os tipos do domínio.
type Entrada struct {
	UnitTypeID    uuid.UUID
	CheckIn       calendar.Date
	CheckOut      calendar.Date
	Hospedes      int
	DescontoPct   float64
	IsEvento      bool
	RateTableID   *uuid.UUID
	PolicyVersion *int
}

// Normalizar converte o pedido. O erro é defensivo: depois de Validar ele não
// acontece pela API, mas o service também é chamado direto em teste.
func (p Pedido) Normalizar() (Entrada, error) {
	if falhas := p.Validar(); len(falhas) > 0 {
		return Entrada{}, apperr.Validation(falhas)
	}
	entrada, err := calendar.Parse(p.CheckIn)
	if err != nil {
		return Entrada{}, apperr.Validation(map[string]string{"check_in": "data inválida: use AAAA-MM-DD."})
	}
	saida, err := calendar.Parse(p.CheckOut)
	if err != nil {
		return Entrada{}, apperr.Validation(map[string]string{"check_out": "data inválida: use AAAA-MM-DD."})
	}
	return Entrada{
		UnitTypeID:    p.UnitTypeID,
		CheckIn:       entrada,
		CheckOut:      saida,
		Hospedes:      p.Hospedes,
		DescontoPct:   p.DescontoPct,
		IsEvento:      p.IsEvento,
		RateTableID:   p.RateTableID,
		PolicyVersion: p.PolicyVersion,
	}, nil
}

// ─────────────────────────── Saída: POST /quotes ────────────────────────────

// NoiteDoOrcamento é uma diária precificada.
type NoiteDoOrcamento struct {
	Data   string            `json:"date"`
	Tipo   calendar.DateType `json:"date_type"`
	Rotulo string            `json:"label"`
	Preco  int64             `json:"price_cents"`
}

// LinhaDoOrcamento agrupa as noites por tipo de tarifa.
type LinhaDoOrcamento struct {
	Tipo     calendar.DateType `json:"date_type"`
	Rotulo   string            `json:"label"`
	Noites   int               `json:"nights"`
	Unitario int64             `json:"unit_price_cents"`
	Subtotal int64             `json:"subtotal_cents"`
}

// Orcamento é a saída de booking.Build no formato do contrato.
//
// Existe como DTO próprio, e não como `booking.Quote` serializado direto, por um
// motivo mecânico: `calendar.Date` é uma struct `{Year, Month, Day}` sem
// MarshalJSON, então `Quote` sairia com `"date":{"Year":2026,...}` em vez de
// `"date":"2026-11-20"`. Enquanto o domínio não tiver MarshalJSON (ver
// relatório), a conversão mora aqui — e nenhum NÚMERO é recalculado no caminho,
// só copiado.
type Orcamento struct {
	Noites        int                       `json:"night_count"`
	Subtotal      int64                     `json:"subtotal_cents"`
	DescontoPct   float64                   `json:"discount_pct"`
	Desconto      int64                     `json:"discount_cents"`
	Limpeza       int64                     `json:"cleaning_cents"`
	CaucaoEvento  int64                     `json:"event_deposit_cents"`
	Total         int64                     `json:"total_cents"`
	Sinal         int64                     `json:"deposit_cents"`
	Saldo         int64                     `json:"balance_cents"`
	MediaPorNoite int64                     `json:"avg_nightly_cents"`
	MinNoites     int                       `json:"min_nights"`
	Alcada        booking.DiscountAuthority `json:"discount_authority"`
	PolicyVersion int                       `json:"policy_version"`
	RateTableID   uuid.UUID                 `json:"rate_table_id"`
	Linhas        []LinhaDoOrcamento        `json:"lines"`
	Diarias       []NoiteDoOrcamento        `json:"nights"`
}

// novoOrcamento traduz a saída do motor. Nenhuma conta acontece aqui.
func novoOrcamento(q booking.Quote, rateTableID uuid.UUID) Orcamento {
	o := Orcamento{
		Noites:        q.NightCount,
		Subtotal:      int64(q.Subtotal),
		DescontoPct:   q.DiscountPct,
		Desconto:      int64(q.Discount),
		Limpeza:       int64(q.Cleaning),
		CaucaoEvento:  int64(q.EventDeposit),
		Total:         int64(q.Total),
		Sinal:         int64(q.Deposit),
		Saldo:         int64(q.Balance),
		MediaPorNoite: int64(q.AvgNightly),
		MinNoites:     q.MinNights,
		Alcada:        q.Authority,
		PolicyVersion: q.PolicyVer,
		RateTableID:   rateTableID,
		Linhas:        make([]LinhaDoOrcamento, 0, len(q.Lines)),
		Diarias:       make([]NoiteDoOrcamento, 0, len(q.Nights)),
	}
	for _, l := range q.Lines {
		o.Linhas = append(o.Linhas, LinhaDoOrcamento{
			Tipo: l.Type, Rotulo: l.Label, Noites: l.Nights,
			Unitario: int64(l.UnitPrice), Subtotal: int64(l.Subtotal),
		})
	}
	for _, n := range q.Nights {
		o.Diarias = append(o.Diarias, NoiteDoOrcamento{
			Data: n.Date.String(), Tipo: n.Type, Rotulo: n.Label, Preco: int64(n.Price),
		})
	}
	return o
}

// ─────────────────────────── Conversões de data ─────────────────────────────

// deTexto converte a data que veio do Postgres como texto.
//
// As datas viajam como TEXTO nas duas direções de propósito. `date` do Postgres
// virando `time.Time` e voltando passa por fuso duas vezes, e um `::date` sobre
// timestamptz em America/Fortaleza (UTC−3) transforma a meia-noite UTC do dia 20
// no dia 19. Texto ISO não tem fuso para errar (CLAUDE.md, regra 5).
func deTexto(s string) (calendar.Date, error) { return calendar.Parse(s) }

// diaDaSemana traduz o bit da `date_type_rules.weekday_mask` para time.Weekday.
// A máscara usa domingo = 0, que é ao mesmo tempo o `time.Weekday` do Go e o
// `EXTRACT(DOW)` do Postgres — as duas pontas leem a mesma máscara.
func diaDaSemana(mascara int32) []time.Weekday {
	var dias []time.Weekday
	for d := time.Sunday; d <= time.Saturday; d++ {
		if mascara&(1<<uint(d)) != 0 {
			dias = append(dias, d)
		}
	}
	return dias
}
