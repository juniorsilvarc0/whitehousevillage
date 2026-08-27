package tarifario

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/domain/calendar"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Handler expõe os endpoints do tarifário. Só decodifica, chama o service e
// envelopa — nenhuma regra comercial mora aqui.
type Handler struct {
	svc *Service
}

// NovoHandler é o construtor combinado com o time: o main monta todos os
// módulos com a mesma assinatura.
func NovoHandler(pool *pgxpool.Pool, tx *db.TxManager) *Handler {
	return &Handler{svc: NovoService(NovoRepository(pool), tx)}
}

// NovoHandlerComService existe para o teste montar o handler sobre um service
// com repositório falso, sem precisar de pool.
func NovoHandlerComService(svc *Service) *Handler { return &Handler{svc: svc} }

// ═══════════════════════ Tabelas de tarifas ═══════════════════════

// ListarTabelas — GET /rate-tables
func (h *Handler) ListarTabelas(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)

	em, err := dataDaQuery(r, "on")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dados, total, err := h.svc.ListarTabelas(r.Context(),
		FiltroDeTabelas{Ativa: httpx.ParseBool(r, "active"), Em: em}, pagina, porPagina)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, Meta(pagina, porPagina, total))
}

// CriarTabela — POST /rate-tables
func (h *Handler) CriarTabela(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[TabelaEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	criada, err := h.svc.CriarTabela(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/rate-tables/"+criada.ID.String())
	httpx.JSON(w, http.StatusCreated, criada)
}

// BuscarTabela — GET /rate-tables/{id}
func (h *Handler) BuscarTabela(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Tabela de tarifas")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	tabela, err := h.svc.BuscarTabela(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, tabela)
}

// SubstituirTabela — PUT /rate-tables/{id}
func (h *Handler) SubstituirTabela(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Tabela de tarifas")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	corpo, err := httpx.Decode[TabelaEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	atualizada, err := h.svc.SubstituirTabela(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, atualizada)
}

// AtualizarTabela — PATCH /rate-tables/{id}
func (h *Handler) AtualizarTabela(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Tabela de tarifas")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	corpo, err := httpx.Decode[TabelaAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	atualizada, err := h.svc.AtualizarTabela(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, atualizada)
}

// DesativarTabela — DELETE /rate-tables/{id}
func (h *Handler) DesativarTabela(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Tabela de tarifas")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.svc.DesativarTabela(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ═══════════════════════ Tarifas ═══════════════════════

// ListarTarifas — GET /rates
func (h *Handler) ListarTarifas(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)

	tabela, err := uuidDaQuery(r, "rate_table_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	produto, err := uuidDaQuery(r, "unit_type_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	tipo, err := tipoDeDataDaQuery(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dados, total, err := h.svc.ListarTarifas(r.Context(),
		FiltroDeTarifas{TabelaID: tabela, ProdutoID: produto, TipoDeData: tipo}, pagina, porPagina)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, Meta(pagina, porPagina, total))
}

// CriarTarifa — POST /rates
func (h *Handler) CriarTarifa(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[TarifaEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	criada, err := h.svc.CriarTarifa(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/rates/"+criada.ID.String())
	httpx.JSON(w, http.StatusCreated, criada)
}

// SalvarGrade — POST /rates/bulk. Devolve 200 com a grade como ficou gravada:
// não é criação de um recurso novo, é a substituição de um estado inteiro.
//
// `data` é a tabela INTEIRA (inclusive os produtos que a chamada não tocou) e
// `meta` é o que a chamada efetivamente fez. O `meta` não é enfeite: com a
// substituição escopada, o produto removido não está na tela de quem salvou, e
// `removed` é o único lugar onde uma remoção não pretendida aparece antes de a
// venda falhar com RATE_NOT_FOUND.
func (h *Handler) SalvarGrade(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[GradeEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	grade, resumo, err := h.svc.SalvarGrade(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	respostaDaGrade(w, grade, resumo)
}

// respostaDaGrade escreve `{"data": [...], "meta": {...}}` com um `meta` que não
// é o de paginação.
//
// Existe aqui, e não em `httpx`, pela mesma razão que os códigos de erro do
// tarifário nascem em `erros.go`: quatro módulos estão sendo escritos em
// paralelo nesta rodada e `httpx.go` é arquivo compartilhado de outro dono.
// `httpx.List` só aceita `httpx.Meta` (page/per_page/total) e o resumo da grade
// não é paginação. Quando a rodada fechar, o lugar disto é um `httpx.Envelope`
// genérico, e o formato na rede não muda em nada.
func respostaDaGrade(w http.ResponseWriter, grade []Tarifa, resumo ResumoDaGrade) {
	if grade == nil {
		grade = []Tarifa{}
	}
	if resumo.ProdutoIDs == nil {
		resumo.ProdutoIDs = []uuid.UUID{}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	corpo := struct {
		Data []Tarifa      `json:"data"`
		Meta ResumoDaGrade `json:"meta"`
	}{Data: grade, Meta: resumo}

	if err := json.NewEncoder(w).Encode(corpo); err != nil {
		slog.Error("falha ao escrever a resposta da grade", "err", err)
	}
}

// BuscarTarifa — GET /rates/{id}
func (h *Handler) BuscarTarifa(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Tarifa")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	tarifa, err := h.svc.BuscarTarifa(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, tarifa)
}

// SubstituirTarifa — PUT /rates/{id}
func (h *Handler) SubstituirTarifa(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Tarifa")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	corpo, err := httpx.Decode[TarifaEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	atualizada, err := h.svc.SubstituirTarifa(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, atualizada)
}

// AtualizarTarifa — PATCH /rates/{id}
func (h *Handler) AtualizarTarifa(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Tarifa")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	corpo, err := httpx.Decode[TarifaAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	atualizada, err := h.svc.AtualizarTarifa(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, atualizada)
}

// ExcluirTarifa — DELETE /rates/{id}
func (h *Handler) ExcluirTarifa(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Tarifa")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.svc.ExcluirTarifa(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ═══════════════════════ Feriados ═══════════════════════

// ListarFeriados — GET /holidays
func (h *Handler) ListarFeriados(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)

	f, err := janelaDaQuery(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	f.Ativo = httpx.ParseBool(r, "active")

	dados, total, err := h.svc.ListarFeriados(r.Context(), f, pagina, porPagina)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, Meta(pagina, porPagina, total))
}

// CriarFeriado — POST /holidays
func (h *Handler) CriarFeriado(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[FeriadoEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	criado, err := h.svc.CriarFeriado(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/holidays/"+criado.ID.String())
	httpx.JSON(w, http.StatusCreated, criado)
}

// BuscarFeriado — GET /holidays/{id}
func (h *Handler) BuscarFeriado(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Feriado")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	feriado, err := h.svc.BuscarFeriado(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, feriado)
}

// SubstituirFeriado — PUT /holidays/{id}
func (h *Handler) SubstituirFeriado(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Feriado")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	corpo, err := httpx.Decode[FeriadoEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	atualizado, err := h.svc.SubstituirFeriado(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, atualizado)
}

// AtualizarFeriado — PATCH /holidays/{id}
func (h *Handler) AtualizarFeriado(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Feriado")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	corpo, err := httpx.Decode[FeriadoAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	atualizado, err := h.svc.AtualizarFeriado(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, atualizado)
}

// ExcluirFeriado — DELETE /holidays/{id}
func (h *Handler) ExcluirFeriado(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Feriado")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.svc.ExcluirFeriado(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ═══════════════════════ Períodos especiais ═══════════════════════

// ListarPeriodos — GET /special-periods
func (h *Handler) ListarPeriodos(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)

	f, err := janelaDaQuery(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	f.Ativo = httpx.ParseBool(r, "active")
	if especie := httpx.Query(r, "kind"); especie != "" {
		if !httpx.ValidarValor(especie, tagEspecie) {
			httpx.Error(w, r, apperr.Validation(map[string]string{
				"kind": "deve ser um de: reveillon, carnaval, alta, evento.",
			}))
			return
		}
		f.Especie = &especie
	}

	dados, total, err := h.svc.ListarPeriodos(r.Context(), f, pagina, porPagina)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, Meta(pagina, porPagina, total))
}

// CriarPeriodo — POST /special-periods
func (h *Handler) CriarPeriodo(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[PeriodoEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	criado, err := h.svc.CriarPeriodo(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/special-periods/"+criado.ID.String())
	httpx.JSON(w, http.StatusCreated, criado)
}

// BuscarPeriodo — GET /special-periods/{id}
func (h *Handler) BuscarPeriodo(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Período especial")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	periodo, err := h.svc.BuscarPeriodo(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, periodo)
}

// SubstituirPeriodo — PUT /special-periods/{id}
func (h *Handler) SubstituirPeriodo(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Período especial")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	corpo, err := httpx.Decode[PeriodoEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	atualizado, err := h.svc.SubstituirPeriodo(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, atualizado)
}

// AtualizarPeriodo — PATCH /special-periods/{id}
func (h *Handler) AtualizarPeriodo(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Período especial")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	corpo, err := httpx.Decode[PeriodoAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	atualizado, err := h.svc.AtualizarPeriodo(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, atualizado)
}

// ExcluirPeriodo — DELETE /special-periods/{id}
func (h *Handler) ExcluirPeriodo(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Período especial")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.svc.ExcluirPeriodo(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ═══════════════════════ Mínimo de noites ═══════════════════════

// ListarMinimos — GET /min-nights
func (h *Handler) ListarMinimos(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)

	tabela, err := uuidDaQuery(r, "rate_table_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	tipo, err := tipoDeDataDaQuery(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dados, total, err := h.svc.ListarMinimos(r.Context(),
		FiltroDeMinimos{TabelaID: tabela, TipoDeData: tipo}, pagina, porPagina)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, Meta(pagina, porPagina, total))
}

// CriarMinimo — POST /min-nights
func (h *Handler) CriarMinimo(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[MinimoEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	criado, err := h.svc.CriarMinimo(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/min-nights/"+criado.ID.String())
	httpx.JSON(w, http.StatusCreated, criado)
}

// BuscarMinimo — GET /min-nights/{id}
func (h *Handler) BuscarMinimo(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Mínimo de noites")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	minimo, err := h.svc.BuscarMinimo(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, minimo)
}

// SubstituirMinimo — PUT /min-nights/{id}
func (h *Handler) SubstituirMinimo(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Mínimo de noites")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	corpo, err := httpx.Decode[MinimoEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	atualizado, err := h.svc.SubstituirMinimo(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, atualizado)
}

// AtualizarMinimo — PATCH /min-nights/{id}
func (h *Handler) AtualizarMinimo(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Mínimo de noites")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	corpo, err := httpx.Decode[MinimoAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	atualizado, err := h.svc.AtualizarMinimo(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, atualizado)
}

// ExcluirMinimo — DELETE /min-nights/{id}
func (h *Handler) ExcluirMinimo(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Mínimo de noites")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.svc.ExcluirMinimo(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ═══════════════════════ Políticas ═══════════════════════

// PoliticaComercial — GET /policies/commercial
func (h *Handler) PoliticaComercial(w http.ResponseWriter, r *http.Request) {
	versao, err := versaoDaQuery(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	pol, err := h.svc.PoliticaComercial(r.Context(), versao)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pol)
}

// PublicarPoliticaComercial — PUT /policies/commercial.
//
// Responde 201, e não 200, porque o PUT aqui CRIA uma versão nova em vez de
// substituir a atual — o status conta a verdade sobre o que aconteceu.
func (h *Handler) PublicarPoliticaComercial(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[PoliticaComercialEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	pol, err := h.svc.PublicarPoliticaComercial(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, pol)
}

// PoliticaDeCancelamento — GET /policies/cancellation
func (h *Handler) PoliticaDeCancelamento(w http.ResponseWriter, r *http.Request) {
	versao, err := versaoDaQuery(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	pol, err := h.svc.PoliticaDeCancelamento(r.Context(), versao)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, pol)
}

// PublicarPoliticaDeCancelamento — PUT /policies/cancellation
func (h *Handler) PublicarPoliticaDeCancelamento(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[PoliticaDeCancelamentoEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	pol, err := h.svc.PublicarPoliticaDeCancelamento(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, pol)
}

// ═══════════════════════ Leitura de rota e query ═══════════════════════

func idDaRota(r *http.Request, recurso string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		// Id malformado é 404, e não 422: o recurso simplesmente não existe
		// naquele endereço, e responder 422 daria ao cliente a impressão de que
		// existe um corpo a corrigir.
		return uuid.Nil, apperr.NotFound(recurso)
	}
	return id, nil
}

func uuidDaQuery(r *http.Request, chave string) (*uuid.UUID, error) {
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

// dataDaQuery lê um filtro de data.
//
// Data ilegível vira 422, e não filtro ignorado: `?on=2026-13-40` devolvendo a
// lista inteira faria a tela mostrar tabelas que não valem na data pedida, e
// ninguém perceberia.
func dataDaQuery(r *http.Request, chave string) (*Data, error) {
	bruto := httpx.Query(r, chave)
	if bruto == "" {
		return nil, nil
	}
	d, err := calendar.Parse(bruto)
	if err != nil {
		return nil, apperr.Validation(map[string]string{chave: "data inválida: use YYYY-MM-DD."})
	}
	return &Data{d}, nil
}

// janelaDaQuery lê o par `from`/`to` que feriados e períodos compartilham.
func janelaDaQuery(r *http.Request) (FiltroDeCalendario, error) {
	var f FiltroDeCalendario

	de, err := dataDaQuery(r, "from")
	if err != nil {
		return f, err
	}
	ate, err := dataDaQuery(r, "to")
	if err != nil {
		return f, err
	}
	if de != nil && ate != nil && ate.Before(de.Date) {
		return f, apperr.Validation(map[string]string{"to": "não pode ser anterior a from."})
	}
	f.De, f.Ate = de, ate
	return f, nil
}

func tipoDeDataDaQuery(r *http.Request) (*string, error) {
	bruto := httpx.Query(r, "date_type")
	if bruto == "" {
		return nil, nil
	}
	if !TipoDeDataValido(bruto) {
		return nil, apperr.Validation(map[string]string{
			"date_type": "deve ser um de: normal, fds, feriado, alta, reveillon, carnaval.",
		})
	}
	return &bruto, nil
}

func versaoDaQuery(r *http.Request) (*int, error) {
	bruto := strings.TrimSpace(r.URL.Query().Get("version"))
	if bruto == "" {
		return nil, nil
	}
	v, err := strconv.Atoi(bruto)
	if err != nil || v < 1 {
		return nil, apperr.Validation(map[string]string{"version": "deve ser um inteiro maior que zero."})
	}
	return &v, nil
}
