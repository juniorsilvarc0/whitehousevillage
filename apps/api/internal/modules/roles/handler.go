package roles

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

// Listar — GET /roles
func (h *Handler) Listar(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)

	dados, total, err := h.svc.Listar(r.Context(), httpx.Query(r, "q"), pagina, porPagina)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, Meta(pagina, porPagina, total))
}

// Criar — POST /roles
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

	w.Header().Set("Location", "/api/v1/roles/"+criado.ID.String())
	httpx.JSON(w, http.StatusCreated, criado)
}

// Recursos — GET /roles/resources. Não é paginado: o catálogo é pequeno,
// versionado por seed, e a tela precisa dele inteiro para desenhar a grade.
func (h *Handler) Recursos(w http.ResponseWriter, r *http.Request) {
	catalogo, err := h.svc.Catalogo(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, catalogo)
}

// Buscar — GET /roles/{id}
func (h *Handler) Buscar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	perfil, err := h.svc.Buscar(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, perfil)
}

// Substituir — PUT /roles/{id}
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
	// O required do PUT vive no requestBody do contrato, não no schema que o
	// PATCH também usa — por isso a checagem é explícita aqui, e não em tag.
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

// Atualizar — PATCH /roles/{id}
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

// Excluir — DELETE /roles/{id}
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

// SubstituirPermissoes — PUT /roles/{id}/permissions
func (h *Handler) SubstituirPermissoes(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	// O corpo é um array no TOPO do JSON, não um objeto com uma chave.
	corpo, err := httpx.Decode[Matriz](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	gravada, err := h.svc.SubstituirPermissoes(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, gravada)
}

func idDaRota(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, apperr.NotFound("Perfil")
	}
	return id, nil
}
