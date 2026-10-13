// Package manutencao implementa as ordens de manutenção (tag `Manutenção` da
// OpenAPI, `/maintenance-orders`): a ordem de uma unidade, o bloqueio de
// calendário que ela segura e a avaria que ela conserta.
//
// As regras moram em `internal/domain/maintenance` (máquina de estados, o que
// cada estado deixa editar, o destino do bloqueio no dia de hoje, o desfecho
// da avaria); aqui ficam a transação, o SQL e a trilha. A rota de cada método
// está em `internal/router/rotas_manutencao.go`.
package manutencao

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/maintenance"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Recurso é o recurso RBAC das dez operações. Sem escopo `own`: a ordem é da
// casa, não de quem a abriu.
const Recurso = "maintenance"

// Handler decodifica, valida, chama o service e envelopa. Regra nenhuma aqui.
//
// Toda rota com corpo LÊ O CORPO ANTES de procurar a ordem: um campo
// desconhecido é 422 mesmo para uma ordem que não existe, e é isso que deixa a
// varredura de campo desconhecido (`regressao_rodada3`) alcançar a rota com um
// id qualquer.
type Handler struct {
	svc *Service
}

// NovoHandler segue o construtor dos módulos (`pool, tx`).
func NovoHandler(pool *pgxpool.Pool, tx *db.TxManager) *Handler {
	return &Handler{svc: NewService(NewRepository(pool), tx)}
}

// Listar — GET /maintenance-orders
func (h *Handler) Listar(w http.ResponseWriter, r *http.Request) {
	f, err := filtroDaQuery(r)
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

// Criar — POST /maintenance-orders
func (h *Handler) Criar(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[OrdemDeManutencaoCriar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Criar(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/maintenance-orders/"+dado.ID.String())
	httpx.JSON(w, http.StatusCreated, dado)
}

// Buscar — GET /maintenance-orders/{id}
func (h *Handler) Buscar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Buscar(r.Context(), id)
	responder(w, r, dado, err)
}

// Substituir — PUT /maintenance-orders/{id}
func (h *Handler) Substituir(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[OrdemDeManutencaoSubstituir](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Substituir(r.Context(), id, corpo)
	responder(w, r, dado, err)
}

// Atualizar — PATCH /maintenance-orders/{id}
func (h *Handler) Atualizar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[OrdemDeManutencaoAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Atualizar(r.Context(), id, corpo)
	responder(w, r, dado, err)
}

// Cancelar — DELETE /maintenance-orders/{id}. Cancela (não apaga) e responde
// 200 com a ordem: a tela mostra o que aconteceu com o bloqueio.
func (h *Handler) Cancelar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Cancelar(r.Context(), id)
	responder(w, r, dado, err)
}

// Iniciar — POST /maintenance-orders/{id}/start. Sem corpo.
func (h *Handler) Iniciar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Iniciar(r.Context(), id)
	responder(w, r, dado, err)
}

// Concluir — POST /maintenance-orders/{id}/complete. Corpo opcional.
func (h *Handler) Concluir(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.DecodeOpcional[ConclusaoDaOrdem](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Concluir(r.Context(), id, corpo)
	responder(w, r, dado, err)
}

// Bloquear — PUT /maintenance-orders/{id}/block
func (h *Handler) Bloquear(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[PeriodoDoBloqueio](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Bloquear(r.Context(), id, corpo)
	responder(w, r, dado, err)
}

// SoltarBloqueio — DELETE /maintenance-orders/{id}/block
func (h *Handler) SoltarBloqueio(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.SoltarBloqueio(r.Context(), id)
	responder(w, r, dado, err)
}

// ═══════════════════════════ Utilidades ═══════════════════════════════════

func responder(w http.ResponseWriter, r *http.Request, dado any, err error) {
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// idDaRota traduz id malformado em 404: "esse endereço não existe" é a
// resposta honesta.
func idDaRota(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, apperr.NotFound("Ordem de manutenção")
	}
	return id, nil
}

// filtroDaQuery lê os filtros da lista. Vocabulário desconhecido (`status`,
// `priority`, `sort`) e id ilegível são 422: devolver a lista inteira fingindo
// que filtrou seria pior.
func filtroDaQuery(r *http.Request) (FiltroDeOrdens, error) {
	pagina, porPagina := httpx.ParsePage(r)
	f := FiltroDeOrdens{
		Status: httpx.Query(r, "status"), Prioridade: httpx.Query(r, "priority"), Busca: httpx.Query(r, "q"),
		Ordem: httpx.Query(r, "sort"), Pagina: pagina, PorPagina: porPagina,
	}
	falhas := map[string]string{}
	// `open` ilegível é 422, e não "sem filtro": `open=sim` devolvendo a lista
	// com as encerradas misturadas é fingir que filtrou. Lido aqui, e não por
	// httpx.ParseBool, que trata ilegível como ausente nas outras rotas.
	if bruto := httpx.Query(r, "open"); bruto != "" {
		if v, err := strconv.ParseBool(bruto); err != nil {
			falhas["open"] = "use true ou false."
		} else {
			f.Aberta = &v
		}
	}
	if f.Status != "" && !maintenance.Status(f.Status).Valid() {
		falhas["status"] = "use aberta, em_andamento, concluida ou cancelada."
	}
	if f.Prioridade != "" && !maintenance.Priority(f.Prioridade).Valid() {
		falhas["priority"] = "use baixa, normal, alta ou urgente."
	}
	if f.Ordem == "" {
		f.Ordem = OrdemPadrao
	} else if !Ordens[f.Ordem] {
		falhas["sort"] = "use urgencia, opened_at, -opened_at ou -closed_at."
	}
	if len([]rune(f.Busca)) > tamanhoDaBusca {
		falhas["q"] = "a busca tem no máximo 200 caracteres."
	}
	for chave, destino := range map[string]**uuid.UUID{
		"unit_id": &f.UnidadeID, "room_id": &f.ComodoID, "item_id": &f.BemID, "issue_id": &f.AvariaID,
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
	if len(falhas) > 0 {
		return FiltroDeOrdens{}, apperr.Validation(falhas)
	}
	return f, nil
}
