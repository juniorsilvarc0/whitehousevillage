package inventario

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Handler expõe os endpoints do módulo: decodifica, valida, chama o service e
// envelopa. Regra de negócio nenhuma mora aqui.
type Handler struct {
	svc *Service
}

// NovoHandler é o construtor combinado com o time — o main monta todos os
// módulos com esta mesma assinatura. O service, o repositório e a trilha de
// auditoria são montados aqui dentro porque ninguém de fora precisa conhecê-los.
//
// A trilha recebe o MESMO pool: `audit.Registrar` o troca pela transação do
// contexto, e como toda escrita deste módulo roda dentro de `tx.Do`, a linha de
// `audit_log` nasce e morre junto com a operação que ela testemunha.
func NovoHandler(pool *pgxpool.Pool, tx *db.TxManager) *Handler {
	return &Handler{svc: NewService(NewRepository(pool), tx, NovaTrilha(pool))}
}

// NewHandler existe para o teste montar o handler sobre um repositório falso,
// sem Postgres. É o mesmo construtor por dentro.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// ─────────────────────────── Propriedades ───────────────────────────

// ListarPropriedades — GET /properties
func (h *Handler) ListarPropriedades(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)

	dados, total, err := h.svc.ListarPropriedades(r.Context(), Filtro{
		Ativo:     httpx.ParseBool(r, "active"),
		Pagina:    pagina,
		PorPagina: porPagina,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, Meta(pagina, porPagina, total))
}

// BuscarPropriedade — GET /properties/{id}
func (h *Handler) BuscarPropriedade(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Propriedade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dado, err := h.svc.BuscarPropriedade(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// AtualizarPropriedade — PATCH /properties/{id}
func (h *Handler) AtualizarPropriedade(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Propriedade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	corpo, err := httpx.Decode[PropriedadeAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dado, err := h.svc.AtualizarPropriedade(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// ─────────────────────────── Produtos ───────────────────────────────

// ListarProdutos — GET /unit-types
func (h *Handler) ListarProdutos(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)

	dados, total, err := h.svc.ListarProdutos(r.Context(), Filtro{
		Busca:     httpx.Query(r, "q"),
		Ativo:     httpx.ParseBool(r, "active"),
		OrderBy:   httpx.ParseSort(r, ColunasDeOrdenacaoDoProduto, OrdemPadraoDoProduto),
		Pagina:    pagina,
		PorPagina: porPagina,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, Meta(pagina, porPagina, total))
}

// CriarProduto — POST /unit-types
func (h *Handler) CriarProduto(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[ProdutoEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	criado, err := h.svc.CriarProduto(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	w.Header().Set("Location", "/api/v1/unit-types/"+criado.ID.String())
	httpx.JSON(w, http.StatusCreated, criado)
}

// BuscarProduto — GET /unit-types/{id}
func (h *Handler) BuscarProduto(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Produto")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dado, err := h.svc.BuscarProduto(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// SubstituirProduto — PUT /unit-types/{id}
func (h *Handler) SubstituirProduto(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Produto")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	corpo, err := httpx.Decode[ProdutoEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dado, err := h.svc.SubstituirProduto(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// AtualizarProduto — PATCH /unit-types/{id}
func (h *Handler) AtualizarProduto(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Produto")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	corpo, err := httpx.Decode[ProdutoAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dado, err := h.svc.AtualizarProduto(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// DesativarProduto — DELETE /unit-types/{id}
func (h *Handler) DesativarProduto(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Produto")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.svc.DesativarProduto(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Composicao — GET /unit-types/{id}/members
func (h *Handler) Composicao(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Produto")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dados, err := h.svc.Composicao(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// Sem `meta`: a composição não é paginada. São oito unidades no total da
	// casa, e a tela precisa da grade inteira para desenhar as caixas de
	// seleção — paginar aqui só criaria a chance de salvar meia composição.
	httpx.JSON(w, http.StatusOK, dados)
}

// SubstituirComposicao — PUT /unit-types/{id}/members
func (h *Handler) SubstituirComposicao(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Produto")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	corpo, err := httpx.Decode[ComposicaoEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dados, err := h.svc.SubstituirComposicao(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dados)
}

// ─────────────────────────── Unidades ───────────────────────────────

// ListarUnidades — GET /units
func (h *Handler) ListarUnidades(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)

	produtoID, err := uuidOpcionalDaQuery(r, "unit_type_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dados, total, err := h.svc.ListarUnidades(r.Context(), Filtro{
		Busca:     httpx.Query(r, "q"),
		Ativo:     httpx.ParseBool(r, "active"),
		ProdutoID: produtoID,
		OrderBy:   httpx.ParseSort(r, ColunasDeOrdenacaoDaUnidade, OrdemPadraoDaUnidade),
		Pagina:    pagina,
		PorPagina: porPagina,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, Meta(pagina, porPagina, total))
}

// CriarUnidade — POST /units
func (h *Handler) CriarUnidade(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[UnidadeEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	criada, err := h.svc.CriarUnidade(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	w.Header().Set("Location", "/api/v1/units/"+criada.ID.String())
	httpx.JSON(w, http.StatusCreated, criada)
}

// BuscarUnidade — GET /units/{id}
func (h *Handler) BuscarUnidade(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Unidade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dado, err := h.svc.BuscarUnidade(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// SubstituirUnidade — PUT /units/{id}
func (h *Handler) SubstituirUnidade(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Unidade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	corpo, err := httpx.Decode[UnidadeEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dado, err := h.svc.SubstituirUnidade(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// AtualizarUnidade — PATCH /units/{id}
func (h *Handler) AtualizarUnidade(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Unidade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	corpo, err := httpx.Decode[UnidadeAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dado, err := h.svc.AtualizarUnidade(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// DesativarUnidade — DELETE /units/{id}
func (h *Handler) DesativarUnidade(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Unidade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.svc.DesativarUnidade(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─────────────────────────── Utilidades ─────────────────────────────

// Meta monta o bloco de paginação da listagem.
func Meta(pagina, porPagina int, total int64) httpx.Meta {
	return httpx.Meta{Page: pagina, PerPage: porPagina, Total: total}
}

// idDaRota traduz id malformado em 404, e não em 422: para quem chama, "esse
// endereço não existe" é a resposta honesta — e responder 422 confirmaria que o
// endereço existe, só que com id de outro formato.
func idDaRota(r *http.Request, recurso string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, apperr.NotFound(recurso)
	}
	return id, nil
}

// uuidOpcionalDaQuery lê um filtro por id. Diferente do id de rota, aqui o
// valor ilegível é 422: o cliente mandou um filtro que não dá para aplicar, e
// devolver a lista inteira fingindo que filtrou seria pior.
func uuidOpcionalDaQuery(r *http.Request, chave string) (*uuid.UUID, error) {
	bruto := httpx.Query(r, chave)
	if bruto == "" {
		return nil, nil
	}
	id, err := uuid.Parse(bruto)
	if err != nil {
		return nil, apperr.Validation(map[string]string{chave: "identificador inválido."})
	}
	return &id, nil
}
