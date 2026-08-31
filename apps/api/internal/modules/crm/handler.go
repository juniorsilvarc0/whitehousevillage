package crm

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/disponibilidade"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/reservas"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Handler expõe os endpoints do módulo: decodifica, valida FORMA, chama o
// service e envelopa. Regra de negócio nenhuma mora aqui.
type Handler struct {
	svc *Servico
}

// NovoHandler é o construtor combinado — o main monta todos os módulos com esta
// mesma assinatura.
//
// O módulo de reservas é montado aqui dentro sobre o MESMO pool e o MESMO
// TxManager: os repositórios dele usam `db.From`, então a criação da reserva do
// `/win` entra na transação do ganho. Sem isso, "a reserva falhou" e "a
// oportunidade fechou" poderiam ser dois fatos independentes — que é exatamente
// a venda ganha sem venda que a regra 3 desta rodada proíbe.
func NovoHandler(pool *pgxpool.Pool, tx *db.TxManager) *Handler {
	orcamentos := disponibilidade.NovoServico(disponibilidade.NewRepository(pool), tx)
	reservasRepo := reservas.NewRepository(pool)
	vendas := reservas.NovoServico(reservasRepo, orcamentos, tx)
	return &Handler{svc: NovoServico(NewRepository(pool), vendas, reservasRepo, orcamentos, tx)}
}

// NovoHandlerCom existe para o teste montar o handler sobre um service já
// composto. É o mesmo construtor por dentro.
func NovoHandlerCom(svc *Servico) *Handler { return &Handler{svc: svc} }

// ─────────────────────────── Funis ──────────────────────────────────

// ListarFunis — GET /crm/pipelines
func (h *Handler) ListarFunis(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)
	dados, total, err := h.svc.ListarFunis(r.Context(), httpx.ParseBool(r, "active"), httpx.Query(r, "q"), pagina, porPagina)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, httpx.Meta{Page: pagina, PerPage: porPagina, Total: total})
}

// CriarFunil — POST /crm/pipelines
func (h *Handler) CriarFunil(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[FunilEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.CriarFunil(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	criado(w, "/api/v1/crm/pipelines/"+dado.ID.String(), dado)
}

// BuscarFunil — GET /crm/pipelines/{id}
func (h *Handler) BuscarFunil(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Funil")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.BuscarFunilComEtapas(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// SubstituirFunil — PUT /crm/pipelines/{id}
func (h *Handler) SubstituirFunil(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Funil")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[FunilEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.SubstituirFunil(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// AtualizarFunil — PATCH /crm/pipelines/{id}
func (h *Handler) AtualizarFunil(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Funil")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[FunilAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.AtualizarFunil(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// DesativarFunil — DELETE /crm/pipelines/{id}
func (h *Handler) DesativarFunil(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Funil")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.DesativarFunil(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─────────────────────────── Etapas ─────────────────────────────────

// ListarEtapas — GET /crm/stages
func (h *Handler) ListarEtapas(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)
	funil, err := uuidOpcional(r, "pipeline_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	tipo := httpx.Query(r, "type")
	if tipo != "" && tipo != EtapaAberta && tipo != EtapaGanho && tipo != EtapaPerdido {
		httpx.Error(w, r, apperr.Validation(map[string]string{"type": "deve ser um de: aberto, ganho, perdido."}))
		return
	}

	dados, total, err := h.svc.ListarEtapas(r.Context(), funil, tipo, pagina, porPagina)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, httpx.Meta{Page: pagina, PerPage: porPagina, Total: total})
}

// CriarEtapa — POST /crm/stages
func (h *Handler) CriarEtapa(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[EtapaEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.CriarEtapa(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	criado(w, "/api/v1/crm/stages/"+dado.ID.String(), dado)
}

// Reordenar — POST /crm/stages/reorder
func (h *Handler) Reordenar(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[PedidoDeReordenacao](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dados, err := h.svc.Reordenar(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dados)
}

// BuscarEtapa — GET /crm/stages/{id}
func (h *Handler) BuscarEtapa(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Etapa")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.BuscarEtapa(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// SubstituirEtapa — PUT /crm/stages/{id}
func (h *Handler) SubstituirEtapa(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Etapa")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[EtapaEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.SubstituirEtapa(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// AtualizarEtapa — PATCH /crm/stages/{id}
func (h *Handler) AtualizarEtapa(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Etapa")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[EtapaAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.AtualizarEtapa(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// ExcluirEtapa — DELETE /crm/stages/{id}
func (h *Handler) ExcluirEtapa(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Etapa")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.ExcluirEtapa(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─────────────────────────── Motivos de perda ───────────────────────

// ListarMotivos — GET /crm/lost-reasons
func (h *Handler) ListarMotivos(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)
	dados, total, err := h.svc.ListarMotivos(r.Context(), httpx.ParseBool(r, "active"), pagina, porPagina)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, httpx.Meta{Page: pagina, PerPage: porPagina, Total: total})
}

// CriarMotivo — POST /crm/lost-reasons
func (h *Handler) CriarMotivo(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[MotivoDePerdaEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.CriarMotivo(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	criado(w, "/api/v1/crm/lost-reasons/"+dado.ID.String(), dado)
}

// BuscarMotivo — GET /crm/lost-reasons/{id}
func (h *Handler) BuscarMotivo(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Motivo de perda")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.BuscarMotivo(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// SubstituirMotivo — PUT /crm/lost-reasons/{id}
func (h *Handler) SubstituirMotivo(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Motivo de perda")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[MotivoDePerdaEntrada](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.SubstituirMotivo(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// AtualizarMotivo — PATCH /crm/lost-reasons/{id}
func (h *Handler) AtualizarMotivo(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Motivo de perda")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[MotivoDePerdaAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.AtualizarMotivo(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// DesativarMotivo — DELETE /crm/lost-reasons/{id}
func (h *Handler) DesativarMotivo(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Motivo de perda")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.DesativarMotivo(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─────────────────────────── Leads ──────────────────────────────────

// ListarLeads — GET /crm/leads
func (h *Handler) ListarLeads(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)

	status, err := ListaDaQuery("status", httpx.Query(r, "status"), estadoDeLeadValido)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dono, err := uuidOpcional(r, "owner_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	contato, err := uuidOpcional(r, "contact_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	de, ate := httpx.Query(r, "from"), httpx.Query(r, "to")
	if err := ValidarDataDaQuery("from", de); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := ValidarDataDaQuery("to", ate); err != nil {
		httpx.Error(w, r, err)
		return
	}

	dados, total, err := h.svc.ListarLeads(r.Context(), FiltroDeLeads{
		Status:    status,
		Origem:    httpx.Query(r, "source"),
		DonoID:    dono,
		ContactID: contato,
		Busca:     httpx.Query(r, "q"),
		De:        de,
		Ate:       ate,
		OrderBy:   httpx.ParseSort(r, OrdemDeLeads, OrdemPadraoDeLeads),
		Pagina:    pagina,
		PorPagina: porPagina,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, httpx.Meta{Page: pagina, PerPage: porPagina, Total: total})
}

// CriarLead — POST /crm/leads
func (h *Handler) CriarLead(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[LeadCriar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.CriarLead(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	criado(w, "/api/v1/crm/leads/"+dado.ID.String(), dado)
}

// BuscarLead — GET /crm/leads/{id}
func (h *Handler) BuscarLead(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Lead")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.BuscarLead(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// SubstituirLead — PUT /crm/leads/{id}
func (h *Handler) SubstituirLead(w http.ResponseWriter, r *http.Request) {
	h.gravarLead(w, r, true)
}

// AtualizarLead — PATCH /crm/leads/{id}
func (h *Handler) AtualizarLead(w http.ResponseWriter, r *http.Request) {
	h.gravarLead(w, r, false)
}

func (h *Handler) gravarLead(w http.ResponseWriter, r *http.Request, substituir bool) {
	id, err := idDaRota(r, "Lead")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[LeadAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dado := Lead{}
	if substituir {
		dado, err = h.svc.SubstituirLead(r.Context(), id, corpo)
	} else {
		dado, err = h.svc.AtualizarLead(r.Context(), id, corpo)
	}
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// ExcluirLead — DELETE /crm/leads/{id}
func (h *Handler) ExcluirLead(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Lead")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.ExcluirLead(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Converter — POST /crm/leads/{id}/convert
func (h *Handler) Converter(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Lead")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.DecodeOpcional[PedidoDeConversao](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Converter(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	criado(w, "/api/v1/crm/opportunities/"+dado.Oportunidade.ID.String(), dado)
}

// ─────────────────────────── Oportunidades ──────────────────────────

// ListarOportunidades — GET /crm/opportunities
func (h *Handler) ListarOportunidades(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)

	status, err := estadosDaQuery(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	ids := map[string]**uuid.UUID{}
	var funil, etapa, dono, contato, produto, motivo *uuid.UUID
	ids["pipeline_id"], ids["stage_id"] = &funil, &etapa
	ids["owner_id"], ids["contact_id"] = &dono, &contato
	ids["unit_type_id"], ids["lost_reason_id"] = &produto, &motivo
	for chave, destino := range ids {
		v, err := uuidOpcional(r, chave)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		*destino = v
	}

	de, ate := httpx.Query(r, "expected_close_from"), httpx.Query(r, "expected_close_to")
	if err := ValidarDataDaQuery("expected_close_from", de); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := ValidarDataDaQuery("expected_close_to", ate); err != nil {
		httpx.Error(w, r, err)
		return
	}
	sla := httpx.Query(r, "sla")
	if sla != "" && sla != "estourado" && sla != "no_prazo" {
		httpx.Error(w, r, apperr.Validation(map[string]string{"sla": "deve ser `estourado` ou `no_prazo`."}))
		return
	}

	dados, total, err := h.svc.ListarOportunidades(r.Context(), FiltroDeOportunidades{
		FunilID: funil, EtapaID: etapa, Status: status,
		DonoID: dono, ContactID: contato, ProdutoID: produto, MotivoID: motivo,
		Busca:         httpx.Query(r, "q"),
		FechamentoDe:  de,
		FechamentoAte: ate,
		SLA:           sla,
		OrderBy:       httpx.ParseSort(r, OrdemDeOportunidades, OrdemPadraoDeOportunidades),
		Pagina:        pagina,
		PorPagina:     porPagina,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, httpx.Meta{Page: pagina, PerPage: porPagina, Total: total})
}

// CriarOportunidade — POST /crm/opportunities
func (h *Handler) CriarOportunidade(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[OportunidadeCriar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.CriarOportunidade(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	criado(w, "/api/v1/crm/opportunities/"+dado.ID.String(), dado)
}

// Kanban — GET /crm/opportunities/kanban
func (h *Handler) Kanban(w http.ResponseWriter, r *http.Request) {
	funil, err := uuidOpcional(r, "pipeline_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dono, err := uuidOpcional(r, "owner_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	produto, err := uuidOpcional(r, "unit_type_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	de, ate := httpx.Query(r, "from"), httpx.Query(r, "to")
	if err := ValidarDataDaQuery("from", de); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := ValidarDataDaQuery("to", ate); err != nil {
		httpx.Error(w, r, err)
		return
	}

	// Teto de cards por coluna: uma etapa com 5.000 cards não pode derrubar a
	// tela na primeira abertura. Valor ilegível cai no padrão, como em
	// `ParsePage` — paginação inválida não é motivo para recusar o quadro.
	porColuna := PorColunaPadrao
	if v := httpx.Query(r, "per_column"); v != "" {
		if n, err := parseInteiro(v); err == nil && n > 0 {
			porColuna = n
		}
	}
	if porColuna > PorColunaTeto {
		porColuna = PorColunaTeto
	}

	incluirFechadas := false
	if v := httpx.ParseBool(r, "include_closed"); v != nil {
		incluirFechadas = *v
	}

	dado, err := h.svc.Kanban(r.Context(), FiltroDoKanban{
		FunilID:         funil,
		PorColuna:       porColuna,
		DonoID:          dono,
		Busca:           httpx.Query(r, "q"),
		ProdutoID:       produto,
		De:              de,
		Ate:             ate,
		IncluirFechadas: incluirFechadas,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// BuscarOportunidade — GET /crm/opportunities/{id}
func (h *Handler) BuscarOportunidade(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Oportunidade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.BuscarOportunidade(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// SubstituirOportunidade — PUT /crm/opportunities/{id}
func (h *Handler) SubstituirOportunidade(w http.ResponseWriter, r *http.Request) {
	h.gravarOportunidade(w, r, true)
}

// AtualizarOportunidade — PATCH /crm/opportunities/{id}
func (h *Handler) AtualizarOportunidade(w http.ResponseWriter, r *http.Request) {
	h.gravarOportunidade(w, r, false)
}

func (h *Handler) gravarOportunidade(w http.ResponseWriter, r *http.Request, substituir bool) {
	id, err := idDaRota(r, "Oportunidade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[OportunidadeAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dado := Oportunidade{}
	if substituir {
		dado, err = h.svc.SubstituirOportunidade(r.Context(), id, corpo)
	} else {
		dado, err = h.svc.AtualizarOportunidade(r.Context(), id, corpo)
	}
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// ExcluirOportunidade — DELETE /crm/opportunities/{id}
func (h *Handler) ExcluirOportunidade(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Oportunidade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.ExcluirOportunidade(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Completa — GET /crm/opportunities/{id}/full
func (h *Handler) Completa(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Oportunidade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Completa(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// MudarEtapa — POST /crm/opportunities/{id}/stage
func (h *Handler) MudarEtapa(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Oportunidade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[PedidoDeMudancaDeEtapa](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.MudarEtapa(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// Ganhar — POST /crm/opportunities/{id}/win
func (h *Handler) Ganhar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Oportunidade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// Obrigatória porque isto cria reserva (CLAUDE.md, convenções de API).
	chave, err := ChaveDeIdempotencia(r.Header.Get("Idempotency-Key"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.DecodeOpcional[PedidoDeGanho](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	resultado, err := h.svc.Ganhar(r.Context(), id, chave, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	responder(w, r, resultado)
}

// Perder — POST /crm/opportunities/{id}/lose
func (h *Handler) Perder(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Oportunidade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[PedidoDePerda](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Perder(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// ─────────────────────────── Atividades ─────────────────────────────

// ListarAtividades — GET /crm/activities
func (h *Handler) ListarAtividades(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)

	tipos, err := ListaDaQuery("type", httpx.Query(r, "type"), tipoDeAtividadeValido)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	status, err := ListaDaQuery("status", httpx.Query(r, "status"), estadoDeAtividadeValido)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	var dono, oportunidade, lead, contato *uuid.UUID
	for chave, destino := range map[string]**uuid.UUID{
		"owner_id": &dono, "opportunity_id": &oportunidade,
		"lead_id": &lead, "contact_id": &contato,
	} {
		v, err := uuidOpcional(r, chave)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		*destino = v
	}

	de, ate := httpx.Query(r, "due_from"), httpx.Query(r, "due_to")
	if err := ValidarDataDaQuery("due_from", de); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := ValidarDataDaQuery("due_to", ate); err != nil {
		httpx.Error(w, r, err)
		return
	}

	dados, total, err := h.svc.ListarAtividades(r.Context(), FiltroDeAtividades{
		Tipos: tipos, Status: status,
		DonoID: dono, OportunidadeID: oportunidade, LeadID: lead, ContactID: contato,
		Auto:      httpx.ParseBool(r, "auto"),
		VenceDe:   de,
		VenceAte:  ate,
		Vencidas:  httpx.ParseBool(r, "overdue"),
		OrderBy:   httpx.ParseSort(r, OrdemDeAtividades, OrdemPadraoDeAtividades),
		Pagina:    pagina,
		PorPagina: porPagina,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, httpx.Meta{Page: pagina, PerPage: porPagina, Total: total})
}

// CriarAtividade — POST /crm/activities
func (h *Handler) CriarAtividade(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[AtividadeCriar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.CriarAtividade(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	criado(w, "/api/v1/crm/activities/"+dado.ID.String(), dado)
}

// BuscarAtividade — GET /crm/activities/{id}
func (h *Handler) BuscarAtividade(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Atividade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.BuscarAtividade(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// SubstituirAtividade — PUT /crm/activities/{id}
func (h *Handler) SubstituirAtividade(w http.ResponseWriter, r *http.Request) {
	h.gravarAtividade(w, r, true)
}

// AtualizarAtividade — PATCH /crm/activities/{id}
func (h *Handler) AtualizarAtividade(w http.ResponseWriter, r *http.Request) {
	h.gravarAtividade(w, r, false)
}

func (h *Handler) gravarAtividade(w http.ResponseWriter, r *http.Request, substituir bool) {
	id, err := idDaRota(r, "Atividade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[AtividadeAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dado := Atividade{}
	if substituir {
		dado, err = h.svc.SubstituirAtividade(r.Context(), id, corpo)
	} else {
		dado, err = h.svc.AtualizarAtividade(r.Context(), id, corpo)
	}
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// ExcluirAtividade — DELETE /crm/activities/{id}
func (h *Handler) ExcluirAtividade(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Atividade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.ExcluirAtividade(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Concluir — POST /crm/activities/{id}/complete
func (h *Handler) Concluir(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "Atividade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.DecodeOpcional[PedidoDeConclusao](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.Concluir(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// ─────────────────────────── Auxiliares ─────────────────────────────

// criado escreve o 201 com Location.
func criado(w http.ResponseWriter, local string, dado any) {
	w.Header().Set("Location", local)
	httpx.JSON(w, http.StatusCreated, dado)
}

// responder escreve o resultado de uma rota idempotente.
//
// Quando a chave foi repetida, o corpo sai COMO FOI GRAVADO na primeira
// execução — inclusive o 201 e o Location. Remontar a resposta a partir do
// estado de agora devolveria, num retry, uma "resposta de criação" já contendo
// alterações posteriores.
func responder(w http.ResponseWriter, r *http.Request, res Resultado) {
	corpo := res.Bruto
	if corpo == nil {
		var err error
		if corpo, err = json.Marshal(res.Corpo); err != nil {
			httpx.Error(w, r, apperr.Internal.WithCause(err))
			return
		}
	}
	if res.Local == "" {
		// Repetir um 201 sem Location faria o cliente que confia no header
		// falhar justamente no retry, que é quando ele mais precisa funcionar.
		res.Local = localDaResposta(corpo)
	}
	if res.Local != "" {
		w.Header().Set("Location", res.Local)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(res.Status)
	if _, err := w.Write(corpo); err != nil {
		slog.ErrorContext(r.Context(), "falha ao escrever a resposta", "err", err)
	}
}

func localDaResposta(bruto []byte) string {
	var env struct {
		Data struct {
			Reserva struct {
				ID uuid.UUID `json:"id"`
			} `json:"reservation"`
			Criada bool `json:"reservation_created"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bruto, &env); err != nil || !env.Data.Criada || env.Data.Reserva.ID == uuid.Nil {
		return ""
	}
	return "/api/v1/reservations/" + env.Data.Reserva.ID.String()
}

// idDaRota traduz id malformado em 404, e não em 422: para quem chama, "esse
// endereço não existe" é a resposta honesta — e 422 confirmaria que o endereço
// existe, só que com id de outro formato.
func idDaRota(r *http.Request, rotulo string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, apperr.NotFound(rotulo)
	}
	return id, nil
}

// uuidOpcional lê um filtro por id. Diferente do id de rota, aqui o valor
// ilegível é 422: devolver a lista inteira fingindo que filtrou seria pior.
func uuidOpcional(r *http.Request, chave string) (*uuid.UUID, error) {
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

// estadosDaQuery traduz `aberta,ganha,perdida` (o vocabulário do CONTRATO) para
// `aberto,ganho,perdido` (o do BANCO).
func estadosDaQuery(r *http.Request) ([]string, error) {
	bruto := httpx.Query(r, "status")
	if bruto == "" {
		return nil, nil
	}
	nomes, err := ListaDaQuery("status", bruto, func(v string) bool {
		_, ok := EstadoDoContrato(v)
		return ok
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(nomes))
	for _, n := range nomes {
		v, _ := EstadoDoContrato(n)
		out = append(out, v)
	}
	return out, nil
}

// parseInteiro lê um inteiro da query. Vive aqui, e não em httpx, porque o
// `per_column` do kanban é o único parâmetro numérico deste módulo fora da
// paginação — acrescentar um helper ao pacote compartilhado por um caso só é
// dívida na pasta de outro agente.
func parseInteiro(v string) (int, error) { return strconv.Atoi(v) }

func estadoDeLeadValido(v string) bool {
	switch v {
	case LeadNovo, LeadEmAtendimento, LeadQualificado, LeadConvertido, LeadDescartado:
		return true
	}
	return false
}

func estadoDeAtividadeValido(v string) bool {
	switch v {
	case AtividadePendente, AtividadeConcluida, AtividadeCancelada:
		return true
	}
	return false
}
