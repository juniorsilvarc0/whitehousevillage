package reservas

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/disponibilidade"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Handler expõe os endpoints do módulo: decodifica, valida forma, chama o
// service e envelopa. Regra de negócio nenhuma mora aqui.
type Handler struct {
	svc *Servico
}

// NovoHandler é o construtor combinado entre os módulos da Fase 1 — o main monta
// todos com esta mesma assinatura.
//
// O módulo de orçamento é montado aqui dentro sobre o MESMO pool: os
// repositórios dele usam db.From, então as consultas de preço entram na mesma
// transação da criação da reserva. Sem isso, o orçamento leria um instante do
// banco e a gravação outro.
func NovoHandler(pool *pgxpool.Pool, tx *db.TxManager) *Handler {
	orcamentos := disponibilidade.NovoServico(disponibilidade.NewRepository(pool), tx)
	return &Handler{svc: NovoServico(NewRepository(pool), orcamentos, tx)}
}

// NovoHandlerCom existe para o teste montar o handler sobre um service já
// composto. É o mesmo construtor por dentro.
func NovoHandlerCom(svc *Servico) *Handler { return &Handler{svc: svc} }

// ─────────────────────────── Reservas ───────────────────────────────

// Listar — GET /reservations
func (h *Handler) Listar(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)

	status, err := StatusDaQuery(httpx.Query(r, "status"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	produto, err := uuidOpcional(r, "unit_type_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	contato, err := uuidOpcional(r, "contact_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corretor, err := uuidOpcional(r, "broker_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	de, ate, err := janelaOpcional(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dados, total, err := h.svc.Listar(r.Context(), Filtro{
		Status:     status,
		De:         de,
		Ate:        ate,
		Busca:      httpx.Query(r, "q"),
		UnitTypeID: produto,
		ContactID:  contato,
		BrokerID:   corretor,
		OrderBy:    httpx.ParseSort(r, ColunasDeOrdenacao, OrdemPadrao),
		Pagina:     pagina,
		PorPagina:  porPagina,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, httpx.Meta{Page: pagina, PerPage: porPagina, Total: total})
}

// Criar — POST /reservations
func (h *Handler) Criar(w http.ResponseWriter, r *http.Request) {
	chave, err := ChaveDeIdempotencia(r.Header.Get("Idempotency-Key"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[ReservaCriar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo.Normalizar()

	resultado, err := h.svc.Criar(r.Context(), chave, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	responder(w, r, resultado)
}

// Buscar — GET /reservations/{id}
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

// Substituir — PUT /reservations/{id}
func (h *Handler) Substituir(w http.ResponseWriter, r *http.Request) {
	h.gravarCadastro(w, r, true)
}

// Atualizar — PATCH /reservations/{id}
func (h *Handler) Atualizar(w http.ResponseWriter, r *http.Request) {
	h.gravarCadastro(w, r, false)
}

func (h *Handler) gravarCadastro(w http.ResponseWriter, r *http.Request, substituir bool) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[ReservaAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dado := Reserva{}
	if substituir {
		dado, err = h.svc.Substituir(r.Context(), id, corpo)
	} else {
		dado, err = h.svc.Atualizar(r.Context(), id, corpo)
	}
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// Descartar — DELETE /reservations/{id}
func (h *Handler) Descartar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.Descartar(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Completa — GET /reservations/{id}/full
func (h *Handler) Completa(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
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

// ─────────────────────────── Ações ──────────────────────────────────

// Confirmar — POST /reservations/{id}/confirm
func (h *Handler) Confirmar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	chave, err := ChaveDeIdempotencia(r.Header.Get("Idempotency-Key"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.DecodeOpcional[ConfirmacaoDeReserva](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	resultado, err := h.svc.Confirmar(r.Context(), id, chave, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	responder(w, r, resultado)
}

// Cancelar — POST /reservations/{id}/cancel
func (h *Handler) Cancelar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.DecodeOpcional[PedidoDeCancelamento](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	// `?dry_run=1` ausente ou ilegível é execução? Não: é SIMULAÇÃO só quando
	// explicitamente verdadeiro. Um valor ilegível cair em "executa" faria um
	// erro de digitação cancelar uma reserva de verdade.
	simular := false
	if v := httpx.ParseBool(r, "dry_run"); v != nil {
		simular = *v
	}

	dado, err := h.svc.Cancelar(r.Context(), id, simular, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// Remarcar — POST /reservations/{id}/reschedule
func (h *Handler) Remarcar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	chave, err := ChaveDeIdempotencia(r.Header.Get("Idempotency-Key"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[PedidoDeRemarcacao](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	resultado, err := h.svc.Remarcar(r.Context(), id, chave, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	responder(w, r, resultado)
}

// CheckIn — POST /reservations/{id}/check-in
func (h *Handler) CheckIn(w http.ResponseWriter, r *http.Request) {
	h.registrarEstadia(w, r, true)
}

// CheckOut — POST /reservations/{id}/check-out
func (h *Handler) CheckOut(w http.ResponseWriter, r *http.Request) {
	h.registrarEstadia(w, r, false)
}

func (h *Handler) registrarEstadia(w http.ResponseWriter, r *http.Request, entrada bool) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.DecodeOpcional[RegistroDeEstadia](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dado := Reserva{}
	if entrada {
		dado, err = h.svc.RegistrarCheckIn(r.Context(), id, corpo)
	} else {
		dado, err = h.svc.RegistrarCheckOut(r.Context(), id, corpo)
	}
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// Realocar — POST /reservations/{id}/reassign-unit
func (h *Handler) Realocar(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[PedidoDeRealocacao](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dado, err := h.svc.Realocar(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// EstenderHold — POST /reservations/{id}/extend-hold
func (h *Handler) EstenderHold(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.DecodeOpcional[PedidoDeExtensaoDeHold](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dado, err := h.svc.EstenderHold(r.Context(), id, corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

// ─────────────────────────── Bloqueios ──────────────────────────────

// CriarBloqueio — POST /blocks
func (h *Handler) CriarBloqueio(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[BloqueioCriar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	dados, err := h.svc.CriarBloqueio(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, dados)
}

// LiberarBloqueio — DELETE /blocks/{id}
func (h *Handler) LiberarBloqueio(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, apperr.NotFound("Bloqueio"))
		return
	}
	if err := h.svc.LiberarBloqueio(r.Context(), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─────────────────────────── Auxiliares ─────────────────────────────

// responder escreve o resultado de uma rota idempotente.
//
// Quando a chave foi repetida, o corpo sai COMO FOI GRAVADO na primeira
// execução — inclusive o 201 e o Location. Remontar a resposta a partir da
// reserva de agora devolveria, num retry, uma "resposta de criação" já contendo
// alterações posteriores.
//
// A resposta guardada volta do `jsonb` de `idempotency_keys`, então a ORDEM das
// chaves e o espaçamento podem diferir da primeira vez — o conteúdo é o mesmo, e
// é o conteúdo que o cliente lê. Guardar em `text` preservaria os bytes e
// perderia a possibilidade de consultar a resposta gravada em SQL; a troca
// escolhida foi manter o tipo do schema.
func responder(w http.ResponseWriter, r *http.Request, res Resultado) {
	corpo := res.Bruto
	if corpo == nil {
		var err error
		if corpo, err = json.Marshal(res.Corpo); err != nil {
			httpx.Error(w, r, apperr.Internal.WithCause(err))
			return
		}
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

// idDaRota traduz id malformado em 404, e não em 422: para quem chama, "esse
// endereço não existe" é a resposta honesta — e 422 confirmaria que o endereço
// existe, só que com id de outro formato.
func idDaRota(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, apperr.NotFound("Reserva")
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

// janelaOpcional lê `from`/`to` da listagem. Os dois são opcionais aqui (ao
// contrário de /availability), mas se vierem têm de ser data legível — filtro
// ilegível silenciosamente ignorado é a listagem mentindo sobre o recorte.
func janelaOpcional(r *http.Request) (string, string, error) {
	de, ate := httpx.Query(r, "from"), httpx.Query(r, "to")
	falhas := map[string]string{}
	validarData(falhas, "from", de)
	validarData(falhas, "to", ate)
	if len(falhas) > 0 {
		return "", "", apperr.Validation(falhas)
	}
	return de, ate, nil
}
