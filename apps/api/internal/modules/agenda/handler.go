// Package agenda implementa a agenda operacional (tag `Agenda` da OpenAPI,
// spec §9, F2-19 a F2-21): os eventos (`/agenda/events`), os bloqueios de
// horário da operação (`/agenda/blocks`) e os horários da casa
// (`/agenda/settings`) — e a agenda que se enche sozinha a cada passo da
// reserva (`Ligacao`).
//
// As regras moram em `internal/domain/agenda` (importado como `regra`): a
// janela aceitável, o responsável, a máquina de estados, o que o estado deixa
// editar, o conflito de unidade e o aviso de horário, e o que cada passo da
// reserva faz com a agenda. O relógio da casa é `calendar.Zone` — a única
// conversão entre hora de parede e instante no Go. Aqui ficam a transação, o
// SQL, o escopo e a trilha. A rota de cada método está em
// `internal/router/rotas_agenda.go`.
package agenda

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	regra "github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/agenda"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Recurso é o recurso RBAC da agenda (`supports_own = true`). `own` é
// `assignee_id = <ator>` no SQL. Gravar os horários pede `settings:editar`
// — está na tabela de rotas, não aqui.
const Recurso = "agenda"

// Handler decodifica, valida a forma, chama o service e envelopa. Regra
// nenhuma aqui.
//
// Toda rota com corpo LÊ O CORPO ANTES de procurar o registro: um campo
// desconhecido é 422 mesmo para um id que não existe, e é isso que deixa a
// varredura de campo desconhecido (`regressao_rodada3`) alcançar a rota com um
// id qualquer.
type Handler struct {
	svc *Service
}

// NovoHandler segue o construtor dos módulos (`pool, tx`).
func NovoHandler(pool *pgxpool.Pool, tx *db.TxManager) *Handler {
	return &Handler{svc: NewService(NewRepository(pool), tx)}
}

// ═══════════════════════════ Eventos ══════════════════════════════════════

// Listar — GET /agenda/events
func (h *Handler) Listar(w http.ResponseWriter, r *http.Request) {
	f, err := filtroDeEventos(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dados, total, err := h.svc.Listar(r.Context(), f)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, httpx.Meta{Page: f.Pagina, PerPage: f.PorPagina, Total: total})
}

// Criar — POST /agenda/events. 201 com os avisos de horário, se houver.
func (h *Handler) Criar(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[EventoDaAgendaCriar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	res, err := h.svc.Criar(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/agenda/events/"+res.Evento.ID.String())
	httpx.Envelope(w, http.StatusCreated, res.Evento, res.Meta)
}

// Buscar — GET /agenda/events/{id}
func (h *Handler) Buscar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Evento da agenda")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Buscar(r.Context(), id)
	responder(w, r, dado, err)
}

// Substituir — PUT /agenda/events/{id}
func (h *Handler) Substituir(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[EventoDaAgendaSubstituir](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	id, err := idDaRota(r, "Evento da agenda")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	res, err := h.svc.Substituir(r.Context(), id, corpo)
	escrever(w, r, res, err)
}

// Atualizar — PATCH /agenda/events/{id}
func (h *Handler) Atualizar(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[EventoDaAgendaAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	id, err := idDaRota(r, "Evento da agenda")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	res, err := h.svc.Atualizar(r.Context(), id, corpo)
	escrever(w, r, res, err)
}

// Cancelar — DELETE /agenda/events/{id}. Desmarca (cancela, não apaga) e
// responde 200 com o evento como ficou.
func (h *Handler) Cancelar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Evento da agenda")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Cancelar(r.Context(), id)
	responder(w, r, dado, err)
}

// Concluir — POST /agenda/events/{id}/complete. Sem corpo.
func (h *Handler) Concluir(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Evento da agenda")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Concluir(r.Context(), id)
	responder(w, r, dado, err)
}

// ═══════════════════════════ Bloqueios de horário ═════════════════════════

// ListarBloqueios — GET /agenda/blocks
func (h *Handler) ListarBloqueios(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)
	falhas := map[string]string{}
	de, ate := janelaDaQuery(r, falhas, true)
	if len(falhas) > 0 {
		httpx.Error(w, r, apperr.Validation(falhas))
		return
	}
	dados, total, err := h.svc.ListarBloqueios(r.Context(), *de, *ate, pagina, porPagina)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, httpx.Meta{Page: pagina, PerPage: porPagina, Total: total})
}

// CriarBloqueio — POST /agenda/blocks
func (h *Handler) CriarBloqueio(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[BloqueioDeHorarioCriar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.CriarBloqueio(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/agenda/blocks/"+dado.ID.String())
	httpx.JSON(w, http.StatusCreated, dado)
}

// BuscarBloqueio — GET /agenda/blocks/{id}
func (h *Handler) BuscarBloqueio(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Bloqueio de horário")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.BuscarBloqueio(r.Context(), id)
	responder(w, r, dado, err)
}

// SubstituirBloqueio — PUT /agenda/blocks/{id}
func (h *Handler) SubstituirBloqueio(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[BloqueioDeHorarioCriar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	id, err := idDaRota(r, "Bloqueio de horário")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.SubstituirBloqueio(r.Context(), id, corpo)
	responder(w, r, dado, err)
}

// AtualizarBloqueio — PATCH /agenda/blocks/{id}
func (h *Handler) AtualizarBloqueio(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[BloqueioDeHorarioAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	id, err := idDaRota(r, "Bloqueio de horário")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.AtualizarBloqueio(r.Context(), id, corpo)
	responder(w, r, dado, err)
}

// RemoverBloqueio — DELETE /agenda/blocks/{id}. Apaga de verdade; 204.
func (h *Handler) RemoverBloqueio(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Bloqueio de horário")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.RemoverBloqueio(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ═══════════════════════════ Horários da operação ═════════════════════════

// Configuracao — GET /agenda/settings
func (h *Handler) Configuracao(w http.ResponseWriter, r *http.Request) {
	dado, err := h.svc.Configuracao(r.Context())
	responder(w, r, dado, err)
}

// GravarConfiguracao — PUT /agenda/settings. 200 com `meta.retimed_events`.
func (h *Handler) GravarConfiguracao(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[ConfiguracaoDaAgendaEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, meta, err := h.svc.GravarConfiguracao(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.Envelope(w, http.StatusOK, dado, meta)
}

// ═══════════════════════════ Utilidades ═══════════════════════════════════

func responder(w http.ResponseWriter, r *http.Request, dado any, err error) {
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// escrever responde `EscritaNaAgenda`: o evento e os avisos de horário.
func escrever(w http.ResponseWriter, r *http.Request, res EscritaNaAgenda, err error) {
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.Envelope(w, http.StatusOK, res.Evento, res.Meta)
}

// idDaRota traduz id malformado em 404: "esse endereço não existe" é a
// resposta honesta.
func idDaRota(r *http.Request, recurso string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, apperr.NotFound(recurso)
	}
	return id, nil
}

// filtroDeEventos lê os filtros da lista. Filtro inválido é 422 — nunca a
// lista inteira fingindo que filtrou (dívida D12): vocabulário desconhecido,
// id ilegível, `unassigned` que não seja booleano, `unassigned` junto com
// `assignee_id`, `sort` fora da lista, janela ausente ou maior que 92 dias.
func filtroDeEventos(r *http.Request) (FiltroDeEventos, error) {
	pagina, porPagina := httpx.ParsePage(r)
	f := FiltroDeEventos{Pagina: pagina, PorPagina: porPagina, Status: statusPadrao()}
	falhas := map[string]string{}

	for chave, destino := range map[string]**uuid.UUID{
		"unit_id": &f.UnidadeID, "assignee_id": &f.ResponsavelID, "reservation_id": &f.ReservaID,
	} {
		bruto := httpx.Query(r, chave)
		if bruto == "" {
			continue
		}
		id, err := uuid.Parse(bruto)
		if err != nil {
			falhas[chave] = "identificador inválido."
			continue
		}
		*destino = &id
	}

	// A janela é obrigatória, exceto com `reservation_id` (os eventos de uma
	// reserva cabem numa resposta).
	semReserva := f.ReservaID == nil && falhas["reservation_id"] == ""
	f.De, f.Ate = janelaDaQuery(r, falhas, semReserva)

	if bruto := httpx.Query(r, "type"); bruto != "" {
		if len(listaDaQuery(bruto)) == 0 {
			falhas["type"] = "lista vazia: informe ao menos um tipo, ou omita o filtro."
		}
		for _, t := range listaDaQuery(bruto) {
			if !regra.Type(t).Valid() {
				falhas["type"] = "use checkin, checkout, limpeza, manutencao, visita, evento ou bloqueio (separados por vírgula)."
				break
			}
			f.Tipos = append(f.Tipos, regra.Type(t))
		}
	}
	if bruto := httpx.Query(r, "status"); bruto != "" {
		f.Status = nil
		if len(listaDaQuery(bruto)) == 0 {
			falhas["status"] = "lista vazia: informe ao menos um estado, ou omita o filtro."
		}
		for _, s := range listaDaQuery(bruto) {
			if !regra.Status(s).Valid() {
				falhas["status"] = "use agendado, concluido ou cancelado (separados por vírgula)."
				break
			}
			f.Status = append(f.Status, regra.Status(s))
		}
	}
	// `unassigned` é `true` ou `false`, EXATAMENTE — o schema do parâmetro é
	// `boolean`, e `1`, `t` ou `TRUE` não são nem um nem outro. Qualquer outro
	// valor é 422, e não "sem filtro": devolver a agenda inteira seria fingir
	// que filtrou (D12). Nem `httpx.ParseBool` (que trata ilegível como
	// ausente) nem `strconv.ParseBool` (que aceita `1` e `T`) servem aqui.
	if bruto := r.URL.Query().Get("unassigned"); bruto != "" {
		switch {
		case bruto != "true" && bruto != "false":
			falhas["unassigned"] = "use true ou false."
		case f.ResponsavelID != nil:
			falhas["unassigned"] = "não combina com assignee_id: um pede os sem responsável, o outro os de alguém."
		default:
			v := bruto == "true"
			f.SemResponsavel = &v
		}
	}
	switch httpx.Query(r, "sort") {
	case "", "starts_at":
	case "-starts_at":
		f.Decrescente = true
	default:
		falhas["sort"] = "use starts_at ou -starts_at."
	}
	if len(falhas) > 0 {
		return FiltroDeEventos{}, apperr.Validation(falhas)
	}
	return f, nil
}

// janelaDaQuery lê `from` e `to` — dias da casa, `[from, to)` —, com `to >
// from` e no máximo 92 dias. `obrigatoria=false` aceita a ausência dos dois.
func janelaDaQuery(r *http.Request, falhas map[string]string, obrigatoria bool) (*calendar.Date, *calendar.Date) {
	brutoDe, brutoAte := httpx.Query(r, "from"), httpx.Query(r, "to")
	if brutoDe == "" && brutoAte == "" {
		if obrigatoria {
			falhas["from"] = "é obrigatório (ou informe reservation_id, onde ele existir)."
			falhas["to"] = "é obrigatório (ou informe reservation_id, onde ele existir)."
		}
		return nil, nil
	}
	ler := func(campo, bruto string) *calendar.Date {
		if bruto == "" {
			falhas[campo] = "é obrigatório junto com o outro extremo da janela."
			return nil
		}
		d, err := calendar.Parse(bruto)
		if err != nil {
			falhas[campo] = "data inválida; use AAAA-MM-DD."
			return nil
		}
		return &d
	}
	de, ate := ler("from", brutoDe), ler("to", brutoAte)
	if de == nil || ate == nil {
		return nil, nil
	}
	switch noites := de.Nights(*ate); {
	case noites <= 0:
		falhas["to"] = "deve ser posterior a `from` (o intervalo é half-open: `to` não entra)."
	case noites > janelaMaximaEmDias:
		falhas["to"] = fmt.Sprintf("a janela tem no máximo %d dias.", janelaMaximaEmDias)
	}
	return de, ate
}

// listaDaQuery parte "checkin, checkout" em itens aparados, sem vazios.
func listaDaQuery(bruto string) []string {
	out := []string{}
	for _, p := range strings.Split(bruto, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
