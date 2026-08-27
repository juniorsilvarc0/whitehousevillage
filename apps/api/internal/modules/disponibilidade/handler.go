package disponibilidade

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Handler expõe os três endpoints do módulo.
type Handler struct {
	svc *Servico
}

// NovoHandler monta o módulo. Assinatura combinada entre os módulos da Fase 1
// para o main poder montar todos do mesmo jeito.
//
// `tx` chega e não é usado: as três rotas são de leitura, e POST /quotes calcula
// sem gravar (o contrato diz, textualmente, "não grava nada"). Ele fica no
// Servico para o dia em que o orçamento passar a persistir.
func NovoHandler(pool *pgxpool.Pool, tx *db.TxManager) *Handler {
	return &Handler{svc: NovoServico(NewRepository(pool), tx)}
}

// NovoHandlerCom existe para o teste montar o handler sobre um repositório
// falso, sem Postgres.
func NovoHandlerCom(svc *Servico) *Handler { return &Handler{svc: svc} }

// PorProduto — GET /availability
func (h *Handler) PorProduto(w http.ResponseWriter, r *http.Request) {
	janela, err := JanelaDaRequisicao(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	produto, err := UUIDOpcionalDaQuery(r, "unit_type_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dados, err := h.svc.PorProduto(r.Context(), janela, produto)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// httpx.JSON e não httpx.List: o contrato desta rota declara só `data`.
	// Disponibilidade não é coleção paginável — a janela já é o recorte, e um
	// `meta.page` aqui convidaria o painel a paginar dias.
	httpx.JSON(w, http.StatusOK, dados)
}

// PorUnidade — GET /availability/units
func (h *Handler) PorUnidade(w http.ResponseWriter, r *http.Request) {
	janela, err := JanelaDaRequisicao(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	unidade, err := UUIDOpcionalDaQuery(r, "unit_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dados, err := h.svc.PorUnidade(r.Context(), janela, unidade)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dados)
}

// Orcar — POST /quotes
func (h *Handler) Orcar(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[Pedido](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	entrada, err := corpo.Normalizar()
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	orcamento, err := h.svc.Orcar(r.Context(), entrada)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// 200 e não 201: nada foi criado. O orçamento não segura data nem grava
	// linha — devolver 201 faria o painel achar que existe um recurso para
	// buscar depois.
	httpx.JSON(w, http.StatusOK, orcamento)
}
