package agenda

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	regra "github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/agenda"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Os nomes das structs de ENTRADA e de SAÍDA são os nomes dos schemas da
// OpenAPI (`EventoDaAgenda`, `EventoDaAgendaCriar`, `BloqueioDeHorario`,
// `ConfiguracaoDaAgendaEntrada`…), para quem lê o contrato achar o DTO sem
// tradução.
//
// O que o DTO confere é FORMA: obrigatório, tamanho, vocabulário, instante com
// fuso, hora `HH:MM`. Regra — a janela aceitável, quem pode ser o responsável,
// a coerência dos horários, o que o estado deixa editar, o conflito de unidade
// — é de `internal/domain/agenda` (`regra`), e o service a aplica com "hoje" e
// "agora" lidos do banco.

// Limites do contrato. Os tamanhos são em CARACTERES (`char_length` nos CHECKs
// da migration 20261013120000), contados depois de aparar as pontas.
const (
	tamanhoDoTitulo = 200
	tamanhoDasNotas = 2000
	tamanhoDoMotivo = 200

	// janelaMaximaEmDias é o teto de `GET /agenda/events` e de
	// `GET /agenda/blocks` (contrato: "no máximo 92 dias"). A visão de mês
	// pede a grade inteira — até seis semanas —, e um trimestre cobre a lista
	// com folga. Não é regra de negócio: é o teto de uma consulta que lê a
	// janela inteira para ordenar eventos gravados e derivados juntos.
	janelaMaximaEmDias = 92

	// maximoDeIntervalos é o `maxItems` de `business_hours` no contrato: quatro
	// intervalos em cada um dos sete dias (`regra.MaxIntervalsPerDay`).
	maximoDeIntervalos = 7 * regra.MaxIntervalsPerDay
)

// ─────────────────────────── Resposta: o evento ─────────────────────

// UnidadeDoEventoDaAgenda é o schema `UnidadeDoEventoDaAgenda`.
type UnidadeDoEventoDaAgenda struct {
	ID     uuid.UUID `json:"unit_id"`
	Codigo string    `json:"unit_code"`
}

// EventoDaAgenda é o schema `EventoDaAgenda`: o gravado (`reservation`,
// `manual`) e o derivado de `stay_blocks` (`maintenance`, `stay_block`), no
// mesmo formato.
//
// Toda data-hora sai no offset DA CASA (`calendar.Zone.In`): a string traz a
// hora de parede da propriedade, e `date` é o dia da casa em que o evento
// começa — o painel agrupa por ele sem converter fuso.
type EventoDaAgenda struct {
	ID         uuid.UUID `json:"id"`
	Tipo       string    `json:"type"`
	Origem     string    `json:"origin"`
	Status     string    `json:"status"`
	Titulo     string    `json:"title"`
	Inicio     time.Time `json:"starts_at"`
	Fim        time.Time `json:"ends_at"`
	DiaInteiro bool      `json:"all_day"`
	Data       string    `json:"date"`
	// Unidades são as que o evento OCUPA: no automático, as da reserva hoje;
	// no manual, a gravada ou, sem ela, as da reserva ligada.
	Unidades []UnidadeDoEventoDaAgenda `json:"units"`
	// UnidadeID é a unidade GRAVADA no evento manual — nula no automático, no
	// manual que ocupa a reserva inteira e no que não ocupa unidade. Não está
	// no schema `EventoDaAgenda` (que só tem `units`), e é o que faltava para o
	// `PUT` dar a volta: sem ela, quem reenvia o evento lido não sabe se
	// `units` veio do evento ou da reserva, e o anulável omitido vira `null`.
	// Aditiva (o schema não fecha `additionalProperties`); ver o relatório.
	UnidadeID           *uuid.UUID `json:"unit_id"`
	ReservaID           *uuid.UUID `json:"reservation_id"`
	ReservaCodigo       *string    `json:"reservation_code"`
	ContatoID           *uuid.UUID `json:"contact_id"`
	ContatoNome         *string    `json:"contact_name"`
	ResponsavelID       *uuid.UUID `json:"assignee_id"`
	ResponsavelNome     *string    `json:"assignee_name"`
	Notas               *string    `json:"notes"`
	OrdemDeManutencaoID *uuid.UUID `json:"maintenance_order_id"`
	BloqueioID          *uuid.UUID `json:"stay_block_id"`
	ConcluidoEm         *time.Time `json:"done_at"`
	ConcluidoPor        *uuid.UUID `json:"done_by"`
	CanceladoEm         *time.Time `json:"cancelled_at"`
	// ConflitoDeUnidade, AcoesPermitidas e Editavel saem do domínio
	// (UnitConflicts, AllowedActions, EditableIn): o painel desenha a partir
	// deles, sem segunda cópia da regra.
	ConflitoDeUnidade bool       `json:"has_unit_conflict"`
	AcoesPermitidas   []string   `json:"allowed_actions"`
	Editavel          string     `json:"editable"`
	CriadoPor         *uuid.UUID `json:"created_by"`
	CriadoEm          *time.Time `json:"created_at"`
	AtualizadoEm      *time.Time `json:"updated_at"`
}

// AvisoDeAgenda é o schema `AvisoDeAgenda`: um conflito de HORÁRIO — o evento
// foi gravado assim mesmo.
type AvisoDeAgenda struct {
	Tipo     string     `json:"kind"`
	Mensagem string     `json:"message"`
	RefID    *uuid.UUID `json:"ref_id"`
	Inicio   *time.Time `json:"starts_at"`
	Fim      *time.Time `json:"ends_at"`
}

// MetaDaEscrita é o `meta` de `EscritaNaAgenda`: os avisos de horário, `[]`
// quando não há.
type MetaDaEscrita struct {
	Avisos []AvisoDeAgenda `json:"warnings"`
}

// EscritaNaAgenda é a resposta de `POST`, `PUT` e `PATCH` de evento.
type EscritaNaAgenda struct {
	Evento EventoDaAgenda
	Meta   MetaDaEscrita
}

// ─────────────────────────── Entrada: o evento ──────────────────────

// EventoDaAgendaCriar é o corpo de `POST /agenda/events`.
//
// `unit_id`, `reservation_id` e `contact_id` nulos valem o mesmo que ausentes:
// no POST não há o que limpar. `assignee_id` é Opt porque, em escopo `own`,
// ausente vira o próprio ator e `null` é 403 — a diferença tem de chegar ao
// domínio (`regra.ResolveAssignee`). `ends_at` e `all_day` são Opt só para
// recusar `null`: o schema não os declara anuláveis.
type EventoDaAgendaCriar struct {
	Tipo          string               `json:"type"`
	Titulo        string               `json:"title"`
	Inicio        string               `json:"starts_at"`
	Fim           httpx.Opt[string]    `json:"ends_at"`
	DiaInteiro    httpx.Opt[bool]      `json:"all_day"`
	UnidadeID     *uuid.UUID           `json:"unit_id"`
	ReservaID     *uuid.UUID           `json:"reservation_id"`
	ContatoID     *uuid.UUID           `json:"contact_id"`
	ResponsavelID httpx.Opt[uuid.UUID] `json:"assignee_id"`
	Notas         *string              `json:"notes"`
}

func (c EventoDaAgendaCriar) Validar() map[string]string {
	falhas := map[string]string{}
	if strings.TrimSpace(c.Tipo) == "" {
		falhas["type"] = "é obrigatório."
	} else {
		validarTipoManual(falhas, c.Tipo)
	}
	validarTitulo(falhas, c.Titulo)
	validarInstante(falhas, "starts_at", c.Inicio)
	if c.Fim.DeveLimpar() {
		falhas["ends_at"] = "não aceita null; omita para usar um passo da grade (`slot_minutes`)."
	} else if v, ok := c.Fim.Definido(); ok {
		validarInstante(falhas, "ends_at", v)
	}
	if c.DiaInteiro.DeveLimpar() {
		falhas["all_day"] = "não aceita null; use true ou false."
	}
	validarNotas(falhas, c.Notas)
	return falhas
}

// EventoDaAgendaSubstituir é o corpo de `PUT /agenda/events/{id}`.
//
// Tudo é Opt, menos a janela: no evento AUTOMÁTICO, tipo, título, dia inteiro,
// unidade, reserva e contato omitidos ficam como estão (são da reserva), e
// isso exige saber o que veio. No manual, o service aplica a regra do PUT —
// anulável omitido vira `null`; `type` e `title` são obrigatórios no manual
// agendado.
type EventoDaAgendaSubstituir struct {
	Tipo          httpx.Opt[string]    `json:"type"`
	Titulo        httpx.Opt[string]    `json:"title"`
	Inicio        string               `json:"starts_at"`
	Fim           string               `json:"ends_at"`
	DiaInteiro    httpx.Opt[bool]      `json:"all_day"`
	UnidadeID     httpx.Opt[uuid.UUID] `json:"unit_id"`
	ReservaID     httpx.Opt[uuid.UUID] `json:"reservation_id"`
	ContatoID     httpx.Opt[uuid.UUID] `json:"contact_id"`
	ResponsavelID httpx.Opt[uuid.UUID] `json:"assignee_id"`
	Notas         httpx.Opt[string]    `json:"notes"`
}

func (c EventoDaAgendaSubstituir) Validar() map[string]string {
	falhas := map[string]string{}
	validarTipoOpcional(falhas, c.Tipo)
	validarTituloOpcional(falhas, c.Titulo)
	validarInstante(falhas, "starts_at", c.Inicio)
	validarInstante(falhas, "ends_at", c.Fim)
	if c.DiaInteiro.DeveLimpar() {
		falhas["all_day"] = "não aceita null; use true ou false."
	}
	if v, ok := c.Notas.Definido(); ok {
		validarNotas(falhas, &v)
	}
	return falhas
}

// EventoDaAgendaAtualizar é o corpo de `PATCH /agenda/events/{id}`: ausente
// não muda, `null` limpa. `type`, `title`, `starts_at`, `ends_at` e `all_day`
// não aceitam `null`.
type EventoDaAgendaAtualizar struct {
	Tipo          httpx.Opt[string]    `json:"type"`
	Titulo        httpx.Opt[string]    `json:"title"`
	Inicio        httpx.Opt[string]    `json:"starts_at"`
	Fim           httpx.Opt[string]    `json:"ends_at"`
	DiaInteiro    httpx.Opt[bool]      `json:"all_day"`
	UnidadeID     httpx.Opt[uuid.UUID] `json:"unit_id"`
	ReservaID     httpx.Opt[uuid.UUID] `json:"reservation_id"`
	ContatoID     httpx.Opt[uuid.UUID] `json:"contact_id"`
	ResponsavelID httpx.Opt[uuid.UUID] `json:"assignee_id"`
	Notas         httpx.Opt[string]    `json:"notes"`
}

func (c EventoDaAgendaAtualizar) Validar() map[string]string {
	falhas := map[string]string{}
	validarTipoOpcional(falhas, c.Tipo)
	validarTituloOpcional(falhas, c.Titulo)
	for campo, v := range map[string]httpx.Opt[string]{"starts_at": c.Inicio, "ends_at": c.Fim} {
		if v.DeveLimpar() {
			falhas[campo] = "não aceita null."
		} else if valor, ok := v.Definido(); ok {
			validarInstante(falhas, campo, valor)
		}
	}
	if c.DiaInteiro.DeveLimpar() {
		falhas["all_day"] = "não aceita null; use true ou false."
	}
	if v, ok := c.Notas.Definido(); ok {
		validarNotas(falhas, &v)
	}
	return falhas
}

// vazio é o corpo `{}`: responde 200 sem escrever, em qualquer estado.
func (c EventoDaAgendaAtualizar) vazio() bool {
	return !c.Tipo.Set && !c.Titulo.Set && !c.Inicio.Set && !c.Fim.Set && !c.DiaInteiro.Set &&
		!c.UnidadeID.Set && !c.ReservaID.Set && !c.ContatoID.Set && !c.ResponsavelID.Set && !c.Notas.Set
}

// ─────────────────────────── Filtro da lista ────────────────────────

// FiltroDeEventos é o recorte de `GET /agenda/events`, já conferido.
type FiltroDeEventos struct {
	// De e Ate são a janela [De, Ate) em dias da casa; nulos só com ReservaID.
	De, Ate        *calendar.Date
	Tipos          []regra.Type
	Status         []regra.Status
	UnidadeID      *uuid.UUID
	ResponsavelID  *uuid.UUID
	SemResponsavel *bool
	ReservaID      *uuid.UUID
	Decrescente    bool
	Pagina         int
	PorPagina      int
}

// statusPadrao esconde os cancelados (contrato: `default: "agendado,concluido"`).
func statusPadrao() []regra.Status { return []regra.Status{regra.Scheduled, regra.Done} }

// ─────────────────────────── Bloqueio de horário ────────────────────

// BloqueioDeHorario é o schema `BloqueioDeHorario`.
type BloqueioDeHorario struct {
	ID           uuid.UUID  `json:"id"`
	Inicio       time.Time  `json:"starts_at"`
	Fim          time.Time  `json:"ends_at"`
	DiaInteiro   bool       `json:"all_day"`
	Motivo       string     `json:"reason"`
	CriadoPor    *uuid.UUID `json:"created_by"`
	CriadoEm     time.Time  `json:"created_at"`
	AtualizadoEm time.Time  `json:"updated_at"`
}

// BloqueioDeHorarioCriar é o corpo do `POST` e do `PUT /agenda/blocks`.
type BloqueioDeHorarioCriar struct {
	Inicio     string          `json:"starts_at"`
	Fim        string          `json:"ends_at"`
	DiaInteiro httpx.Opt[bool] `json:"all_day"`
	Motivo     string          `json:"reason"`
}

func (c BloqueioDeHorarioCriar) Validar() map[string]string {
	falhas := map[string]string{}
	validarInstante(falhas, "starts_at", c.Inicio)
	validarInstante(falhas, "ends_at", c.Fim)
	if c.DiaInteiro.DeveLimpar() {
		falhas["all_day"] = "não aceita null; use true ou false."
	}
	validarMotivo(falhas, c.Motivo)
	return falhas
}

// BloqueioDeHorarioAtualizar é o corpo do `PATCH`: ausente não muda; nenhum
// campo aceita `null`.
type BloqueioDeHorarioAtualizar struct {
	Inicio     httpx.Opt[string] `json:"starts_at"`
	Fim        httpx.Opt[string] `json:"ends_at"`
	DiaInteiro httpx.Opt[bool]   `json:"all_day"`
	Motivo     httpx.Opt[string] `json:"reason"`
}

func (c BloqueioDeHorarioAtualizar) Validar() map[string]string {
	falhas := map[string]string{}
	for campo, v := range map[string]httpx.Opt[string]{"starts_at": c.Inicio, "ends_at": c.Fim} {
		if v.DeveLimpar() {
			falhas[campo] = "não aceita null."
		} else if valor, ok := v.Definido(); ok {
			validarInstante(falhas, campo, valor)
		}
	}
	if c.DiaInteiro.DeveLimpar() {
		falhas["all_day"] = "não aceita null; use true ou false."
	}
	if c.Motivo.DeveLimpar() {
		falhas["reason"] = "não aceita null."
	} else if v, ok := c.Motivo.Definido(); ok {
		validarMotivo(falhas, v)
	}
	return falhas
}

func (c BloqueioDeHorarioAtualizar) vazio() bool {
	return !c.Inicio.Set && !c.Fim.Set && !c.DiaInteiro.Set && !c.Motivo.Set
}

// ─────────────────────────── Configuração ───────────────────────────

// IntervaloDeFuncionamento é o schema `IntervaloDeFuncionamento`. Na entrada,
// o dia é ponteiro para separar "ausente" de zero; na saída, sempre vem.
type IntervaloDeFuncionamento struct {
	DiaDaSemana *int   `json:"weekday"`
	Inicio      string `json:"start"`
	Fim         string `json:"end"`
}

// ConfiguracaoDaAgenda é o schema `ConfiguracaoDaAgenda`.
type ConfiguracaoDaAgenda struct {
	Fuso              string                     `json:"timezone"`
	Hoje              string                     `json:"today"`
	Entrada           string                     `json:"checkin_time"`
	Saida             string                     `json:"checkout_time"`
	InicioDaLimpeza   string                     `json:"cleaning_start"`
	FimDaLimpeza      string                     `json:"cleaning_end"`
	Funcionamento     []IntervaloDeFuncionamento `json:"business_hours"`
	PassoEmMinutos    int                        `json:"slot_minutes"`
	FolgaEmMinutos    int                        `json:"buffer_minutes"`
	Provisoria        bool                       `json:"provisional"`
	AtualizadaEm      time.Time                  `json:"updated_at"`
	AtualizadaPor     *uuid.UUID                 `json:"updated_by"`
	AtualizadaPorNome *string                    `json:"updated_by_name"`
}

// MetaDaConfiguracao é o `meta` de `PUT /agenda/settings`.
type MetaDaConfiguracao struct {
	Reprogramados int `json:"retimed_events"`
}

// ConfiguracaoDaAgendaEntrada é o corpo de `PUT /agenda/settings` — a
// configuração inteira. Os inteiros são ponteiros para "ausente" ser
// "obrigatório", e não um zero que o domínio leria como passo zero.
type ConfiguracaoDaAgendaEntrada struct {
	Entrada         string                     `json:"checkin_time"`
	Saida           string                     `json:"checkout_time"`
	InicioDaLimpeza string                     `json:"cleaning_start"`
	FimDaLimpeza    string                     `json:"cleaning_end"`
	Funcionamento   []IntervaloDeFuncionamento `json:"business_hours"`
	PassoEmMinutos  *int                       `json:"slot_minutes"`
	FolgaEmMinutos  *int                       `json:"buffer_minutes"`
}

// Validar confere a FORMA: presença e `HH:MM`. A coerência entre os horários,
// as faixas e o funcionamento por dia é de `regra.Settings.Validate`, que
// devolve todas as violações de uma vez.
func (c ConfiguracaoDaAgendaEntrada) Validar() map[string]string {
	falhas := map[string]string{}
	for campo, valor := range map[string]string{
		"checkin_time": c.Entrada, "checkout_time": c.Saida,
		"cleaning_start": c.InicioDaLimpeza, "cleaning_end": c.FimDaLimpeza,
	} {
		validarHora(falhas, campo, valor)
	}
	switch {
	case c.Funcionamento == nil:
		falhas["business_hours"] = "é obrigatório (lista vazia é a operação fechada todos os dias)."
	case len(c.Funcionamento) > maximoDeIntervalos:
		falhas["business_hours"] = fmt.Sprintf("no máximo %d intervalos.", maximoDeIntervalos)
	}
	for i, h := range c.Funcionamento {
		prefixo := fmt.Sprintf("business_hours[%d].", i)
		if h.DiaDaSemana == nil {
			falhas[prefixo+"weekday"] = "é obrigatório."
		}
		validarHora(falhas, prefixo+"start", h.Inicio)
		validarHora(falhas, prefixo+"end", h.Fim)
	}
	if c.PassoEmMinutos == nil {
		falhas["slot_minutes"] = "é obrigatório."
	}
	if c.FolgaEmMinutos == nil {
		falhas["buffer_minutes"] = "é obrigatório."
	}
	return falhas
}

// dominio converte para `regra.Settings`. Só depois de Validar.
func (c ConfiguracaoDaAgendaEntrada) dominio() (regra.Settings, error) {
	s := regra.Settings{BusinessHours: make([]regra.Hours, 0, len(c.Funcionamento))}
	var err error
	for _, par := range []struct {
		destino *calendar.Clock
		valor   string
	}{
		{&s.CheckIn, c.Entrada}, {&s.CheckOut, c.Saida},
		{&s.CleaningStart, c.InicioDaLimpeza}, {&s.CleaningEnd, c.FimDaLimpeza},
	} {
		if *par.destino, err = calendar.ParseClock(par.valor); err != nil {
			return regra.Settings{}, err
		}
	}
	for _, h := range c.Funcionamento {
		inicio, err := calendar.ParseClock(h.Inicio)
		if err != nil {
			return regra.Settings{}, err
		}
		fim, err := calendar.ParseClock(h.Fim)
		if err != nil {
			return regra.Settings{}, err
		}
		dia := 0
		if h.DiaDaSemana != nil {
			dia = *h.DiaDaSemana
		}
		s.BusinessHours = append(s.BusinessHours, regra.Hours{Weekday: dia, Start: inicio, End: fim})
	}
	if c.PassoEmMinutos != nil {
		s.SlotMinutes = *c.PassoEmMinutos
	}
	if c.FolgaEmMinutos != nil {
		s.BufferMinutes = *c.FolgaEmMinutos
	}
	return s, nil
}

// ─────────────────────────── Regras de forma ────────────────────────

func validarTipoManual(falhas map[string]string, tipo string) {
	if !regra.Type(tipo).Manual() {
		falhas["type"] = "use visita, evento ou limpeza (check-in e check-out nascem da reserva; " +
			"manutenção é a ordem de manutenção; bloqueio de unidade é POST /blocks)."
	}
}

func validarTipoOpcional(falhas map[string]string, tipo httpx.Opt[string]) {
	if tipo.DeveLimpar() {
		falhas["type"] = "não aceita null."
	} else if v, ok := tipo.Definido(); ok {
		validarTipoManual(falhas, v)
	}
}

func validarTitulo(falhas map[string]string, titulo string) {
	t := strings.TrimSpace(titulo)
	switch {
	case t == "":
		falhas["title"] = "é obrigatório."
	case utf8.RuneCountInString(t) > tamanhoDoTitulo:
		falhas["title"] = fmt.Sprintf("no máximo %d caracteres.", tamanhoDoTitulo)
	}
}

func validarTituloOpcional(falhas map[string]string, titulo httpx.Opt[string]) {
	if titulo.DeveLimpar() {
		falhas["title"] = "não aceita null."
	} else if v, ok := titulo.Definido(); ok {
		validarTitulo(falhas, v)
	}
}

func validarNotas(falhas map[string]string, notas *string) {
	if notas != nil && utf8.RuneCountInString(strings.TrimSpace(*notas)) > tamanhoDasNotas {
		falhas["notes"] = fmt.Sprintf("no máximo %d caracteres.", tamanhoDasNotas)
	}
}

func validarMotivo(falhas map[string]string, motivo string) {
	m := strings.TrimSpace(motivo)
	switch {
	case m == "":
		falhas["reason"] = "é obrigatório."
	case utf8.RuneCountInString(m) > tamanhoDoMotivo:
		falhas["reason"] = fmt.Sprintf("no máximo %d caracteres.", tamanhoDoMotivo)
	}
}

// validarInstante exige RFC 3339 COM fuso: instante sem offset é 422 — não se
// adivinha o fuso de quem mandou.
func validarInstante(falhas map[string]string, campo, valor string) {
	if strings.TrimSpace(valor) == "" {
		falhas[campo] = "é obrigatório."
		return
	}
	if _, err := time.Parse(time.RFC3339, valor); err != nil {
		falhas[campo] = "instante inválido: use RFC 3339 com fuso (2026-11-20T14:00:00-03:00)."
	}
}

func validarHora(falhas map[string]string, campo, valor string) {
	if strings.TrimSpace(valor) == "" {
		falhas[campo] = "é obrigatório."
		return
	}
	if _, err := calendar.ParseClock(valor); err != nil {
		falhas[campo] = "hora inválida: use HH:MM, de 00:00 a 24:00."
	}
}

// instante converte o texto já validado. O erro só existe por defesa.
func instante(valor string) (time.Time, error) { return time.Parse(time.RFC3339, valor) }

// textoOuNulo apara e troca o vazio por nulo: anotação em branco é ausência de
// anotação, não um segundo jeito de escrever "nada".
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

// nomesDasAcoes devolve as ações do domínio como texto, nunca nil.
func nomesDasAcoes(as []regra.Action) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = string(a)
	}
	return out
}
