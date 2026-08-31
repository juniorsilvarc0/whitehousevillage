package contatos

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Handler decodifica, valida FORMA, chama o service e envelopa. Regra de
// negócio nenhuma mora aqui.
type Handler struct {
	svc *Servico
}

// NovoHandler é o construtor combinado — o main monta todos os módulos com esta
// mesma assinatura.
func NovoHandler(pool *pgxpool.Pool, tx *db.TxManager) *Handler {
	return &Handler{svc: NovoServico(NewRepository(pool), tx)}
}

// NovoHandlerCom monta o handler sobre um service já composto. Existe para o
// teste; é o mesmo construtor por dentro.
func NovoHandlerCom(svc *Servico) *Handler { return &Handler{svc: svc} }

// Listar — GET /contacts
func (h *Handler) Listar(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)

	f := Filtro{
		Busca:               httpx.Query(r, "q"),
		Telefone:            httpx.Query(r, "phone"),
		Documento:           httpx.Query(r, "doc_number"),
		OptIn:               httpx.ParseBool(r, "marketing_opt_in"),
		IncluirAnonimizados: booleano(httpx.ParseBool(r, "include_anonymized")),
		OrderBy:             httpx.ParseSort(r, ColunasDeOrdenacao, OrdenacaoPadrao),
		Pagina:              pagina,
		PorPagina:           porPagina,
	}

	dados, total, err := h.svc.Listar(r.Context(), f)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, httpx.Meta{Page: pagina, PerPage: porPagina, Total: total})
}

// Criar — POST /contacts
func (h *Handler) Criar(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[Criar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	criado, err := h.svc.Criar(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/contacts/"+criado.ID.String())
	httpx.JSON(w, http.StatusCreated, criado)
}

// Buscar — GET /contacts/{id}
func (h *Handler) Buscar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Buscar(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// Substituir — PUT /contacts/{id}
func (h *Handler) Substituir(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[Criar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Substituir(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// Atualizar — PATCH /contacts/{id}
func (h *Handler) Atualizar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[Atualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Atualizar(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// Excluir — DELETE /contacts/{id}
func (h *Handler) Excluir(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.Excluir(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Anonimizar — POST /contacts/{id}/anonymize
func (h *Handler) Anonimizar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[PedidoDeAnonimizacao](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Anonimizar(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// Exportar — GET /contacts/{id}/export
func (h *Handler) Exportar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Exportar(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// idDaRota devolve 404 para id malformado, e não 422: um path que não é uuid
// não é um recurso desta API.
func idDaRota(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, apperr.NotFound("Contato")
	}
	return id, nil
}

// booleano resolve o `*bool` de httpx.ParseBool para o default do contrato
// (`include_anonymized: false`).
func booleano(p *bool) bool { return p != nil && *p }
