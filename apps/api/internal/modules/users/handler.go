package users

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Listar — GET /users
func (h *Handler) Listar(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)

	f := Filtro{
		Busca:     httpx.Query(r, "q"),
		RoleCode:  httpx.Query(r, "role"), // é o code do perfil, não o uuid
		Ativo:     httpx.ParseBool(r, "active"),
		OrderBy:   httpx.ParseSort(r, ColunasDeOrdenacao, OrdenacaoPadrao),
		Pagina:    pagina,
		PorPagina: porPagina,
	}

	dados, total, err := h.svc.Listar(r.Context(), f)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, Meta(pagina, porPagina, total))
}

// Criar — POST /users
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

	w.Header().Set("Location", "/api/v1/users/"+criado.ID.String())
	httpx.JSON(w, http.StatusCreated, criado)
}

// Buscar — GET /users/{id}
func (h *Handler) Buscar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	usuario, err := h.svc.Buscar(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, usuario)
}

// Substituir — PUT /users/{id}
func (h *Handler) Substituir(w http.ResponseWriter, r *http.Request) {
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
	// O required do PUT vive no allOf do contrato, não no schema base — por isso
	// a checagem é explícita aqui, e não em tag.
	if faltando := corpo.ObrigatoriosDoPut(); len(faltando) > 0 {
		httpx.Error(w, r, apperr.Validation(faltando))
		return
	}

	atualizado, err := h.svc.Substituir(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, atualizado)
}

// Atualizar — PATCH /users/{id}
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

	atualizado, err := h.svc.Atualizar(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, atualizado)
}

// Desativar — DELETE /users/{id}
func (h *Handler) Desativar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.svc.Desativar(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// idDaRota devolve 404 para id malformado, e não 422: um path que não é uuid
// não é um recurso desta API.
func idDaRota(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, apperr.NotFound("Usuário")
	}
	return id, nil
}
