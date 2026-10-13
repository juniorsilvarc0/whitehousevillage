package manutencao

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/maintenance"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Os nomes das structs de ENTRADA são os nomes dos schemas da OpenAPI
// (`OrdemDeManutencaoCriar`, `…Substituir`, `…Atualizar`, `ConclusaoDaOrdem`,
// `PeriodoDoBloqueio`), para quem lê o contrato achar o DTO sem tradução.
//
// O que o DTO confere é FORMA: obrigatório, tamanho, vocabulário, data legível.
// Regra de negócio — o período do bloqueio, a máquina de estados, o que o estado
// deixa editar — é de `internal/domain/maintenance`, e o service a aplica.

// Limites do contrato, em CARACTERES (`char_length` nos CHECKs do banco).
const (
	tamanhoDoTitulo    = 200
	tamanhoDaDescricao = 4000
	tamanhoDaBusca     = 200
)

// ─────────────────────────── Resposta ───────────────────────────────

// OrdemDeManutencao é o schema `OrdemDeManutencao` do contrato.
type OrdemDeManutencao struct {
	ID            uuid.UUID      `json:"id"`
	UnidadeID     uuid.UUID      `json:"unit_id"`
	UnidadeCodigo string         `json:"unit_code"`
	UnidadeNome   string         `json:"unit_name"`
	ComodoID      *uuid.UUID     `json:"room_id"`
	ComodoNome    *string        `json:"room_name"`
	BemID         *uuid.UUID     `json:"item_id"`
	BemNome       *string        `json:"item_name"`
	AvariaID      *uuid.UUID     `json:"issue_id"`
	Avaria        *AvariaDaOrdem `json:"issue"`
	Titulo        string         `json:"title"`
	Descricao     *string        `json:"description"`
	Prioridade    string         `json:"priority"`
	Status        string         `json:"status"`
	CustoCents    *int64         `json:"cost_cents"`
	// Bloqueio nulo = a ordem nunca bloqueou o calendário.
	Bloqueio       *BloqueioDaOrdem `json:"block"`
	AbertaEm       time.Time        `json:"opened_at"`
	AbertaPor      *uuid.UUID       `json:"opened_by"`
	AbertaPorNome  *string          `json:"opened_by_name"`
	IniciadaEm     *time.Time       `json:"started_at"`
	FechadaEm      *time.Time       `json:"closed_at"`
	FechadaPor     *uuid.UUID       `json:"closed_by"`
	FechadaPorNome *string          `json:"closed_by_name"`
	AtualizadaEm   time.Time        `json:"updated_at"`
	// AcoesPermitidas e Editavel saem do domínio (AllowedActions, EditableIn):
	// o painel desenha os botões a partir deles, sem segunda máquina de estados.
	AcoesPermitidas []string `json:"allowed_actions"`
	Editavel        string   `json:"editable"`
}

// BloqueioDaOrdem é a linha de `stay_blocks` da ordem, com a fase de HOJE.
type BloqueioDaOrdem struct {
	ID     uuid.UUID `json:"id"`
	De     string    `json:"from"`
	Ate    string    `json:"to"`
	Noites int       `json:"nights"`
	Status string    `json:"status"`
	Fase   string    `json:"phase"`
}

// AvariaDaOrdem é o resumo da avaria de origem.
type AvariaDaOrdem struct {
	ID         uuid.UUID `json:"id"`
	Tipo       string    `json:"kind"`
	Qtd        int       `json:"qty"`
	Nota       *string   `json:"note"`
	Desfecho   *string   `json:"resolution"`
	RelatadaEm time.Time `json:"reported_at"`
}

// ─────────────────────────── Entrada ────────────────────────────────

// PeriodoDoBloqueio é o corpo de `PUT /maintenance-orders/{id}/block` e o
// `block` do POST: half-open `[from, to)`, em datas da casa.
type PeriodoDoBloqueio struct {
	De  string `json:"from"`
	Ate string `json:"to"`
}

// Validar confere só o FORMATO das datas; a regra do período (hoje, tetos,
// em curso) é `maintenance.Replan`, que precisa de HOJE.
func (p PeriodoDoBloqueio) Validar() map[string]string {
	return p.validarCom("")
}

func (p PeriodoDoBloqueio) validarCom(prefixo string) map[string]string {
	falhas := map[string]string{}
	for campo, valor := range map[string]string{"from": p.De, "to": p.Ate} {
		switch {
		case strings.TrimSpace(valor) == "":
			falhas[prefixo+campo] = "é obrigatório."
		default:
			if _, err := calendar.Parse(valor); err != nil {
				falhas[prefixo+campo] = "data inválida; use AAAA-MM-DD."
			}
		}
	}
	return falhas
}

// periodo converte para o domínio. Só depois de Validar.
func (p PeriodoDoBloqueio) periodo() (maintenance.Period, error) {
	de, err := calendar.Parse(p.De)
	if err != nil {
		return maintenance.Period{}, err
	}
	ate, err := calendar.Parse(p.Ate)
	if err != nil {
		return maintenance.Period{}, err
	}
	return maintenance.Period{From: de, To: ate}, nil
}

// OrdemDeManutencaoCriar é o corpo do `POST /maintenance-orders`.
//
// `room_id` e `item_id` nulos valem o mesmo que ausentes: no POST não há o
// que limpar, e com `issue_id` os dois vêm da avaria.
type OrdemDeManutencaoCriar struct {
	UnidadeID  uuid.UUID          `json:"unit_id"`
	ComodoID   *uuid.UUID         `json:"room_id"`
	BemID      *uuid.UUID         `json:"item_id"`
	AvariaID   *uuid.UUID         `json:"issue_id"`
	Titulo     string             `json:"title"`
	Descricao  *string            `json:"description"`
	Prioridade *string            `json:"priority"`
	CustoCents *int64             `json:"cost_cents"`
	Bloqueio   *PeriodoDoBloqueio `json:"block"`
}

func (c OrdemDeManutencaoCriar) Validar() map[string]string {
	falhas := map[string]string{}
	if c.UnidadeID == uuid.Nil {
		falhas["unit_id"] = "é obrigatório."
	}
	validarTitulo(falhas, c.Titulo)
	validarDescricao(falhas, c.Descricao)
	if c.Prioridade != nil {
		validarPrioridade(falhas, *c.Prioridade)
	}
	validarCusto(falhas, c.CustoCents)
	if c.Bloqueio != nil {
		for campo, msg := range c.Bloqueio.validarCom("block.") {
			falhas[campo] = msg
		}
	}
	return falhas
}

// OrdemDeManutencaoSubstituir é o corpo do `PUT /maintenance-orders/{id}`.
//
// `room_id` e `item_id` são Opt porque o PUT precisa separar "omitido" de
// "null": com avaria ligada, omitido fica o da avaria e `null` é 422. Os
// demais anuláveis omitidos viram `null`, como em todo PUT.
type OrdemDeManutencaoSubstituir struct {
	ComodoID   httpx.Opt[uuid.UUID] `json:"room_id"`
	BemID      httpx.Opt[uuid.UUID] `json:"item_id"`
	Titulo     string               `json:"title"`
	Descricao  *string              `json:"description"`
	Prioridade string               `json:"priority"`
	CustoCents *int64               `json:"cost_cents"`
}

func (c OrdemDeManutencaoSubstituir) Validar() map[string]string {
	falhas := map[string]string{}
	validarTitulo(falhas, c.Titulo)
	validarDescricao(falhas, c.Descricao)
	if c.Prioridade == "" {
		falhas["priority"] = "é obrigatório."
	} else {
		validarPrioridade(falhas, c.Prioridade)
	}
	validarCusto(falhas, c.CustoCents)
	return falhas
}

// OrdemDeManutencaoAtualizar é o corpo do `PATCH /maintenance-orders/{id}`:
// ausente não muda, `null` limpa. `title` e `priority` não aceitam `null`.
type OrdemDeManutencaoAtualizar struct {
	ComodoID   httpx.Opt[uuid.UUID] `json:"room_id"`
	BemID      httpx.Opt[uuid.UUID] `json:"item_id"`
	Titulo     httpx.Opt[string]    `json:"title"`
	Descricao  httpx.Opt[string]    `json:"description"`
	Prioridade httpx.Opt[string]    `json:"priority"`
	CustoCents httpx.Opt[int64]     `json:"cost_cents"`
}

func (c OrdemDeManutencaoAtualizar) Validar() map[string]string {
	falhas := map[string]string{}
	if c.Titulo.DeveLimpar() {
		falhas["title"] = "não aceita null."
	} else if v, ok := c.Titulo.Definido(); ok {
		validarTitulo(falhas, v)
	}
	if v, ok := c.Descricao.Definido(); ok {
		validarDescricao(falhas, &v)
	}
	if c.Prioridade.DeveLimpar() {
		falhas["priority"] = "não aceita null."
	} else if v, ok := c.Prioridade.Definido(); ok {
		validarPrioridade(falhas, v)
	}
	if v, ok := c.CustoCents.Definido(); ok {
		validarCusto(falhas, &v)
	}
	return falhas
}

// edicao diz ao domínio o que o PATCH mexe: `Cost` pelo `cost_cents`, `Other`
// por qualquer outro campo PRESENTE — ainda que `null`. Corpo vazio não mexe
// em nada e passa em qualquer estado.
func (c OrdemDeManutencaoAtualizar) edicao() maintenance.Edit {
	return maintenance.Edit{
		Cost:  c.CustoCents.Set,
		Other: c.ComodoID.Set || c.BemID.Set || c.Titulo.Set || c.Descricao.Set || c.Prioridade.Set,
	}
}

// vazio é o corpo sem nenhum campo: responde 200 sem escrever.
func (c OrdemDeManutencaoAtualizar) vazio() bool {
	e := c.edicao()
	return !e.Cost && !e.Other
}

// ConclusaoDaOrdem é o corpo OPCIONAL de `POST /{id}/complete`. O custo é Opt
// só para recusar `null`: o schema o declara inteiro, e "ausente" já é o jeito
// de não mexer no custo.
type ConclusaoDaOrdem struct {
	CustoCents httpx.Opt[int64] `json:"cost_cents"`
}

func (c ConclusaoDaOrdem) Validar() map[string]string {
	falhas := map[string]string{}
	if c.CustoCents.DeveLimpar() {
		falhas["cost_cents"] = "não aceita null; omita o campo para concluir sem mexer no custo."
	} else if v, ok := c.CustoCents.Definido(); ok {
		validarCusto(falhas, &v)
	}
	return falhas
}

// ─────────────────────────── Filtro ─────────────────────────────────

// FiltroDeOrdens é o recorte de `GET /maintenance-orders`.
type FiltroDeOrdens struct {
	Status     string
	Aberta     *bool
	UnidadeID  *uuid.UUID
	ComodoID   *uuid.UUID
	BemID      *uuid.UUID
	AvariaID   *uuid.UUID
	Prioridade string
	Busca      string
	Ordem      string
	Pagina     int
	PorPagina  int
}

// OrdemPadrao é a lista de trabalho: abertas primeiro, da mais urgente.
const OrdemPadrao = "urgencia"

// Ordens é a whitelist do `?sort=`. O valor é a chave que o repositório
// traduz em ORDER BY; texto do cliente nunca chega ao SQL.
var Ordens = map[string]bool{
	"urgencia":   true,
	"opened_at":  true,
	"-opened_at": true,
	"-closed_at": true,
}

// ─────────────────────────── Regras de forma ────────────────────────

func validarTitulo(falhas map[string]string, titulo string) {
	t := strings.TrimSpace(titulo)
	switch {
	case t == "":
		falhas["title"] = "é obrigatório."
	case utf8.RuneCountInString(t) > tamanhoDoTitulo:
		falhas["title"] = fmt.Sprintf("no máximo %d caracteres.", tamanhoDoTitulo)
	}
}

func validarDescricao(falhas map[string]string, d *string) {
	if d != nil && utf8.RuneCountInString(strings.TrimSpace(*d)) > tamanhoDaDescricao {
		falhas["description"] = fmt.Sprintf("no máximo %d caracteres.", tamanhoDaDescricao)
	}
}

func validarPrioridade(falhas map[string]string, p string) {
	if !maintenance.Priority(p).Valid() {
		falhas["priority"] = "use baixa, normal, alta ou urgente."
	}
}

func validarCusto(falhas map[string]string, c *int64) {
	if c != nil && *c < 1 {
		falhas["cost_cents"] = "deve ser positivo, em centavos; omita (ou null) se o custo ainda não chegou."
	}
}

// textoOuNulo apara e troca o vazio por nulo: descrição em branco é ausência
// de descrição, não um segundo jeito de escrever "nada".
func textoOuNulo(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}
