package disponibilidade

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Handler expõe os quatro endpoints do módulo: as duas leituras de calendário,
// o orçamento (simulado ou emitido) e a releitura do orçamento emitido.
type Handler struct {
	svc *Servico
}

// NovoHandler monta o módulo. Assinatura combinada entre os módulos da Fase 1
// para o main poder montar todos do mesmo jeito.
//
// `tx` é usado desde 27/08/2026: `POST /quotes` com `persist: true` grava o
// cabeçalho e as noites do orçamento, e as duas escritas só fazem sentido
// comitadas juntas.
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

	// Simular e emitir são a mesma conta com destinos diferentes, e é `persist`
	// que separa os dois — não a presença de `contact_id`, não uma heurística.
	if entrada.Persistir {
		salvo, err := h.svc.Emitir(r.Context(), entrada)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		// 201 + Location: agora existe um recurso para buscar depois, e o
		// cliente que confia no header sabe onde.
		w.Header().Set("Location", "/api/v1/quotes/"+salvo.ID.String())
		httpx.JSON(w, http.StatusCreated, salvo)
		return
	}

	orcamento, err := h.svc.Orcar(r.Context(), entrada)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// 200 e não 201: nada foi criado. A simulação não segura data nem grava
	// linha — devolver 201 faria o painel achar que existe um recurso para
	// buscar depois.
	httpx.JSON(w, http.StatusOK, orcamento)
}

// Orcamento — GET /quotes/{id}
//
// Devolve o que foi GRAVADO, não o que o motor calcularia hoje. É o ponto
// inteiro de persistir: a única pergunta que se faz a um orçamento antigo —
// "quanto foi que eu prometi?" — seria a única que ele não saberia responder se
// a leitura recalculasse.
func (h *Handler) Orcamento(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, apperr.NotFound("Orçamento"))
		return
	}
	salvo, err := h.svc.BuscarOrcamento(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, salvo)
}
