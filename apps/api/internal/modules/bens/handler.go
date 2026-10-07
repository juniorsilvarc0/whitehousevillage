package bens

import (
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Prazos de rede das rotas de arquivo. O servidor tem ReadTimeout de 30 s e
// WriteTimeout de 60 s (cmd/api): sadios para JSON, curtos para 15 MB subindo
// ou descendo pelo celular de dentro de um apartamento de alvenaria. As duas
// rotas de foto estendem o próprio prazo, e só elas.
const (
	prazoDeEnvio   = 10 * time.Minute
	prazoDeEntrega = 10 * time.Minute
)

// cacheDaFoto: a linha é imutável (trocar a foto = id novo), então o cache é
// longo — mas `private`: a foto mostra o interior da casa, e o id não pode
// acabar guardado num proxy compartilhado.
const cacheDaFoto = "private, max-age=31536000, immutable"

// Handler decodifica, valida, chama o service e envelopa. Regra nenhuma aqui.
type Handler struct {
	svc *Service
}

// NovoHandler segue o construtor dos módulos (`pool, tx`), mais o volume de
// mídia da API (MEDIA_DIR). As fotos de bens vão para um subdiretório dele.
func NovoHandler(pool *pgxpool.Pool, tx *db.TxManager, dirDeMidia string) *Handler {
	dir := ""
	if dirDeMidia != "" {
		dir = filepath.Join(dirDeMidia, SubdiretorioDoVolume)
	}
	return &Handler{svc: NewService(NewRepository(pool), tx, dir)}
}

// ═══════════════════════════ Ambientes ════════════════════════════════════

// ListarAmbientes — GET /rooms
func (h *Handler) ListarAmbientes(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)
	unidade, err := uuidDaQuery(r, "unit_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	tipo, err := vocabularioDaQuery(r, "kind", tiposDeAmbiente)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dados, total, err := h.svc.ListarAmbientes(r.Context(), FiltroDeAmbientes{
		UnidadeID: unidade, Tipo: tipo, Ativo: httpx.ParseBool(r, "active"), Busca: httpx.Query(r, "q"),
		Ordem:  chaveDeOrdem(r, OrdensDeAmbiente, OrdemPadraoDeAmbiente),
		Pagina: pagina, PorPagina: porPagina,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, httpx.Meta{Page: pagina, PerPage: porPagina, Total: total})
}

// CriarAmbiente — POST /rooms
func (h *Handler) CriarAmbiente(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[AmbienteCriar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.CriarAmbiente(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/rooms/"+dado.ID.String())
	httpx.JSON(w, http.StatusCreated, dado)
}

// BuscarAmbiente — GET /rooms/{id}
func (h *Handler) BuscarAmbiente(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Ambiente")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.BuscarAmbiente(r.Context(), id)
	responder(w, r, dado, err)
}

// SubstituirAmbiente — PUT /rooms/{id}
func (h *Handler) SubstituirAmbiente(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Ambiente")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[AmbienteSubstituir](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.SubstituirAmbiente(r.Context(), id, corpo)
	responder(w, r, dado, err)
}

// AtualizarAmbiente — PATCH /rooms/{id}
func (h *Handler) AtualizarAmbiente(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Ambiente")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[AmbienteAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.AtualizarAmbiente(r.Context(), id, corpo)
	responder(w, r, dado, err)
}

// ApagarAmbiente — DELETE /rooms/{id}
func (h *Handler) ApagarAmbiente(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Ambiente")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	semConteudo(w, r, h.svc.ApagarAmbiente(r.Context(), id))
}

// ═══════════════════════════ Bens ═════════════════════════════════════════

// ListarBens — GET /inventory/items
func (h *Handler) ListarBens(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)
	categoria, err := vocabularioDaQuery(r, "category", categorias)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	unidade, err := uuidDaQuery(r, "unit_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	ambiente, err := uuidDaQuery(r, "room_id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dados, total, err := h.svc.ListarBens(r.Context(), FiltroDeBens{
		Categoria: categoria, Ativo: httpx.ParseBool(r, "active"), Busca: httpx.Query(r, "q"),
		UnidadeID: unidade, AmbienteID: ambiente, TemFoto: httpx.ParseBool(r, "has_photo"),
		Ordem:  chaveDeOrdem(r, OrdensDeBem, OrdemPadraoDeBem),
		Pagina: pagina, PorPagina: porPagina,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, httpx.Meta{Page: pagina, PerPage: porPagina, Total: total})
}

// CriarBem — POST /inventory/items
func (h *Handler) CriarBem(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[BemCriar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.CriarBem(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/inventory/items/"+dado.ID.String())
	httpx.JSON(w, http.StatusCreated, dado)
}

// BuscarBem — GET /inventory/items/{id}
func (h *Handler) BuscarBem(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Bem")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.BuscarBem(r.Context(), id)
	responder(w, r, dado, err)
}

// SubstituirBem — PUT /inventory/items/{id}
func (h *Handler) SubstituirBem(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Bem")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[BemCriar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.SubstituirBem(r.Context(), id, corpo)
	responder(w, r, dado, err)
}

// AtualizarBem — PATCH /inventory/items/{id}
func (h *Handler) AtualizarBem(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Bem")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[BemAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.AtualizarBem(r.Context(), id, corpo)
	responder(w, r, dado, err)
}

// ApagarBem — DELETE /inventory/items/{id}
func (h *Handler) ApagarBem(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Bem")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	semConteudo(w, r, h.svc.ApagarBem(r.Context(), id))
}

// FotosDoBem — GET /inventory/items/{id}/photos
func (h *Handler) FotosDoBem(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Bem")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dados, err := h.svc.FotosDoBem(r.Context(), id)
	responder(w, r, dados, err)
}

// SubstituirFotos — PUT /inventory/items/{id}/photos
func (h *Handler) SubstituirFotos(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Bem")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[GaleriaDoBem](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dados, err := h.svc.SubstituirFotos(r.Context(), id, corpo)
	responder(w, r, dados, err)
}

// ═══════════════════════════ Colocações ═══════════════════════════════════

// ListarColocacoes — GET /inventory/placements
func (h *Handler) ListarColocacoes(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)
	f := FiltroDeColocacoes{
		Busca: httpx.Query(r, "q"), Ordem: chaveDeOrdem(r, OrdensDeColocacao, OrdemPadraoDeColocacao),
		Pagina: pagina, PorPagina: porPagina,
	}
	var err error
	if f.UnidadeID, err = uuidDaQuery(r, "unit_id"); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if f.AmbienteID, err = uuidDaQuery(r, "room_id"); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if f.BemID, err = uuidDaQuery(r, "item_id"); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if f.Categoria, err = vocabularioDaQuery(r, "category", categorias); err != nil {
		httpx.Error(w, r, err)
		return
	}
	dados, total, err := h.svc.ListarColocacoes(r.Context(), f)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, httpx.Meta{Page: pagina, PerPage: porPagina, Total: total})
}

// CriarColocacao — POST /inventory/placements
func (h *Handler) CriarColocacao(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[ColocacaoCriar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.CriarColocacao(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/inventory/placements/"+dado.ID)
	httpx.JSON(w, http.StatusCreated, dado)
}

// BuscarColocacao — GET /inventory/placements/{id}
func (h *Handler) BuscarColocacao(w http.ResponseWriter, r *http.Request) {
	ambiente, bem, err := chaveDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.BuscarColocacao(r.Context(), ambiente, bem)
	responder(w, r, dado, err)
}

// SubstituirColocacao — PUT /inventory/placements/{id}
func (h *Handler) SubstituirColocacao(w http.ResponseWriter, r *http.Request) {
	ambiente, bem, err := chaveDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[ColocacaoSubstituir](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.SubstituirColocacao(r.Context(), ambiente, bem, corpo)
	responder(w, r, dado, err)
}

// AtualizarColocacao — PATCH /inventory/placements/{id}
func (h *Handler) AtualizarColocacao(w http.ResponseWriter, r *http.Request) {
	ambiente, bem, err := chaveDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[ColocacaoAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.AtualizarColocacao(r.Context(), ambiente, bem, corpo)
	responder(w, r, dado, err)
}

// ApagarColocacao — DELETE /inventory/placements/{id}
func (h *Handler) ApagarColocacao(w http.ResponseWriter, r *http.Request) {
	ambiente, bem, err := chaveDaRota(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	semConteudo(w, r, h.svc.ApagarColocacao(r.Context(), ambiente, bem))
}

// ═══════════════════════════ Fotos ════════════════════════════════════════

// EnviarFoto — POST /inventory/media (multipart, campo `file`)
//
// O corpo é lido em FLUXO (httpx.ParteDeArquivo), nunca com
// ParseMultipartForm: os 15 MB vão direto para o temporário dentro do volume.
func (h *Handler) EnviarFoto(w http.ResponseWriter, r *http.Request) {
	estenderPrazo(r, w, func(rc *http.ResponseController) error {
		return rc.SetReadDeadline(time.Now().Add(prazoDeEnvio))
	})
	parte, err := httpx.ParteDeArquivo(w, r, "file", LimiteDoCorpo)
	if err != nil {
		if errors.Is(err, httpx.ErrSemArquivo) {
			httpx.Error(w, r, ErroDeArquivo(msgSemArquivo).WithCause(err))
			return
		}
		httpx.Error(w, r, erroDeLeitura(err))
		return
	}
	defer func() { _ = parte.Close() }()

	dado, err := h.svc.EnviarFoto(r.Context(), parte, parte.FileName())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Location", dado.URL)
	httpx.JSON(w, http.StatusCreated, dado)
}

// Foto — GET /inventory/media/{id}[?size=thumb]
//
// AUTENTICADA, nunca pública: a foto mostra o interior da casa. ServeContent
// responde Range, If-Modified-Since e HEAD.
func (h *Handler) Foto(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Foto")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	miniatura := false
	switch httpx.Query(r, "size") {
	case "", "original":
	case "thumb":
		miniatura = true
	default:
		httpx.Error(w, r, apperr.Validation(map[string]string{"size": "deve ser original ou thumb."}))
		return
	}

	foto, err := h.svc.AbrirFoto(r.Context(), id, miniatura)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	defer func() { _ = foto.Arquivo.Close() }()

	estenderPrazo(r, w, func(rc *http.ResponseController) error {
		return rc.SetWriteDeadline(time.Now().Add(prazoDeEntrega))
	})
	w.Header().Set("Content-Type", foto.Mime)
	w.Header().Set("Cache-Control", cacheDaFoto)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", foto.Midia.CriadoEm, foto.Arquivo)
}

// ═══════════════════════════ Unidade ══════════════════════════════════════

// porPaginaDoSeletor é o padrão de `GET /inventory/units`: a casa tem uma
// dúzia de unidades, e o seletor quer todas numa página.
const porPaginaDoSeletor = 100

// ListarUnidades — GET /inventory/units
func (h *Handler) ListarUnidades(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)
	if httpx.Query(r, "per_page") == "" {
		porPagina = porPaginaDoSeletor
	}
	dados, total, err := h.svc.ListarUnidades(r.Context(), FiltroDeUnidades{
		Ativa: httpx.ParseBool(r, "active"), Busca: httpx.Query(r, "q"), Pagina: pagina, PorPagina: porPagina,
	})
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, httpx.Meta{Page: pagina, PerPage: porPagina, Total: total})
}

// InventarioDaUnidade — GET /units/{id}/inventory
func (h *Handler) InventarioDaUnidade(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Unidade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	categoria, err := vocabularioDaQuery(r, "category", categorias)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.InventarioDaUnidade(r.Context(), id, FiltroDaUnidade{
		IncluirInativos: boolDaQuery(r, "include_inactive"), Categoria: categoria, Busca: httpx.Query(r, "q"),
	})
	responder(w, r, dado, err)
}

// CopiarInventario — POST /units/{id}/inventory/copy[?dry_run=1]
func (h *Handler) CopiarInventario(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Unidade")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[PedidoDeCopia](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.CopiarInventario(r.Context(), id, corpo, boolDaQuery(r, "dry_run"))
	responder(w, r, dado, err)
}

// Exportar — GET /inventory/export
func (h *Handler) Exportar(w http.ResponseWriter, r *http.Request) {
	f := FiltroDeExportacao{IncluirInativos: boolDaQuery(r, "include_inactive")}
	var err error
	if f.UnidadeID, err = uuidDaQuery(r, "unit_id"); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if f.AmbienteID, err = uuidDaQuery(r, "room_id"); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if f.Categoria, err = vocabularioDaQuery(r, "category", categorias); err != nil {
		httpx.Error(w, r, err)
		return
	}
	planilha, err := h.svc.Exportar(r.Context(), f)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+planilha.NomeDoArquivo+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if err := planilha.Escrever(w); err != nil {
		// O cabeçalho já saiu; resta registrar. O cliente vê o download cortado.
		slog.WarnContext(r.Context(), "bens: exportação interrompida", "err", err)
	}
}

// ═══════════════════════════ Conferências ═════════════════════════════════

// ListarConferencias — GET /inventory/counts
func (h *Handler) ListarConferencias(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)
	f := FiltroDeConferencias{
		Ordem: chaveDeOrdem(r, OrdensDeConferencia, OrdemPadraoDeConferencia), Pagina: pagina, PorPagina: porPagina,
	}
	var err error
	if f.UnidadeID, err = uuidDaQuery(r, "unit_id"); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if f.Status, err = vocabularioDaQuery(r, "status", statusDeConf); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if f.De, err = dataDaQuery(r, "from"); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if f.Ate, err = dataDaQuery(r, "to"); err != nil {
		httpx.Error(w, r, err)
		return
	}
	dados, total, err := h.svc.ListarConferencias(r.Context(), f)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, httpx.Meta{Page: pagina, PerPage: porPagina, Total: total})
}

// AbrirConferencia — POST /inventory/counts
func (h *Handler) AbrirConferencia(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[ConferenciaAbrir](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.AbrirConferencia(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/inventory/counts/"+dado.ID.String())
	httpx.JSON(w, http.StatusCreated, dado)
}

// BuscarConferencia — GET /inventory/counts/{id}
func (h *Handler) BuscarConferencia(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Conferência")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.BuscarConferencia(r.Context(), id)
	responder(w, r, dado, err)
}

// SubstituirConferencia — PUT /inventory/counts/{id}
func (h *Handler) SubstituirConferencia(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Conferência")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[ConferenciaSubstituir](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.SubstituirConferencia(r.Context(), id, corpo)
	responder(w, r, dado, err)
}

// AtualizarConferencia — PATCH /inventory/counts/{id}
func (h *Handler) AtualizarConferencia(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Conferência")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[ConferenciaAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.AtualizarConferencia(r.Context(), id, corpo)
	responder(w, r, dado, err)
}

// CancelarConferencia — DELETE /inventory/counts/{id}
func (h *Handler) CancelarConferencia(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Conferência")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	semConteudo(w, r, h.svc.CancelarConferencia(r.Context(), id))
}

// ContarLinha — PATCH /inventory/counts/{id}/lines/{lineId}
func (h *Handler) ContarLinha(w http.ResponseWriter, r *http.Request) {
	conferencia, err := idDaRota(r, "id", "Conferência")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	linha, err := idDaRota(r, "lineId", "Linha da conferência")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[ContagemDaLinha](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.ContarLinha(r.Context(), conferencia, linha, corpo)
	responder(w, r, dado, err)
}

// FecharConferencia — POST /inventory/counts/{id}/close (corpo opcional)
func (h *Handler) FecharConferencia(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Conferência")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.DecodeOpcional[PedidoDeFechamento](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.FecharConferencia(r.Context(), id, corpo)
	responder(w, r, dado, err)
}

// ═══════════════════════════ Avarias ══════════════════════════════════════

// ListarAvarias — GET /inventory/issues
func (h *Handler) ListarAvarias(w http.ResponseWriter, r *http.Request) {
	pagina, porPagina := httpx.ParsePage(r)
	f := FiltroDeAvarias{
		Aberta: httpx.ParseBool(r, "open"),
		Ordem:  chaveDeOrdem(r, OrdensDeAvaria, OrdemPadraoDeAvaria), Pagina: pagina, PorPagina: porPagina,
	}
	var err error
	for _, alvo := range []struct {
		chave string
		dest  **uuid.UUID
	}{
		{"unit_id", &f.UnidadeID}, {"room_id", &f.AmbienteID}, {"item_id", &f.BemID},
		{"reservation_id", &f.ReservaID}, {"count_id", &f.ConferenciaID},
	} {
		if *alvo.dest, err = uuidDaQuery(r, alvo.chave); err != nil {
			httpx.Error(w, r, err)
			return
		}
	}
	if f.Tipo, err = vocabularioDaQuery(r, "kind", tiposDeAvaria); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if f.Desfecho, err = vocabularioDaQuery(r, "resolution", desfechos); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if f.De, err = dataDaQuery(r, "from"); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if f.Ate, err = dataDaQuery(r, "to"); err != nil {
		httpx.Error(w, r, err)
		return
	}
	dados, total, err := h.svc.ListarAvarias(r.Context(), f)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.List(w, dados, httpx.Meta{Page: pagina, PerPage: porPagina, Total: total})
}

// CriarAvaria — POST /inventory/issues
func (h *Handler) CriarAvaria(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[AvariaCriar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.CriarAvaria(r.Context(), corpo)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/inventory/issues/"+dado.ID.String())
	httpx.JSON(w, http.StatusCreated, dado)
}

// BuscarAvaria — GET /inventory/issues/{id}
func (h *Handler) BuscarAvaria(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Avaria")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.BuscarAvaria(r.Context(), id)
	responder(w, r, dado, err)
}

// SubstituirAvaria — PUT /inventory/issues/{id}
func (h *Handler) SubstituirAvaria(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Avaria")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[AvariaSubstituir](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.SubstituirAvaria(r.Context(), id, corpo)
	responder(w, r, dado, err)
}

// AtualizarAvaria — PATCH /inventory/issues/{id}
func (h *Handler) AtualizarAvaria(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Avaria")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	corpo, err := httpx.Decode[AvariaAtualizar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	dado, err := h.svc.AtualizarAvaria(r.Context(), id, corpo)
	responder(w, r, dado, err)
}

// ApagarAvaria — DELETE /inventory/issues/{id}
func (h *Handler) ApagarAvaria(w http.ResponseWriter, r *http.Request) {
	id, err := idDaRota(r, "id", "Avaria")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	semConteudo(w, r, h.svc.ApagarAvaria(r.Context(), id))
}

// ═══════════════════════════ Utilidades ═══════════════════════════════════

func responder(w http.ResponseWriter, r *http.Request, dado any, err error) {
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, dado)
}

func semConteudo(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// idDaRota traduz id malformado em 404, e não em 422: para quem chama, "esse
// endereço não existe" é a resposta honesta.
func idDaRota(r *http.Request, param, recurso string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, param))
	if err != nil {
		return uuid.Nil, apperr.NotFound(recurso)
	}
	return id, nil
}

// chaveDaRota lê a chave composta `room_id_item_id` da colocação.
func chaveDaRota(r *http.Request) (uuid.UUID, uuid.UUID, error) {
	ambiente, bem, ok := LerChaveDaColocacao(chi.URLParam(r, "id"))
	if !ok {
		return uuid.Nil, uuid.Nil, apperr.NotFound("Colocação")
	}
	return ambiente, bem, nil
}

// uuidDaQuery lê um filtro por id. Ilegível é 422: devolver a lista inteira
// fingindo que filtrou seria pior.
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

// vocabularioDaQuery lê um filtro de vocabulário fechado; valor fora dele é
// 422, não filtro ignorado.
func vocabularioDaQuery(r *http.Request, chave string, vocabulario []string) (string, error) {
	v := httpx.Query(r, chave)
	if v == "" || DoVocabulario(v, vocabulario) {
		return v, nil
	}
	return "", apperr.Validation(map[string]string{chave: "valor desconhecido."})
}

// dataDaQuery lê AAAA-MM-DD. É a data LOCAL da casa; a conversão para
// instante é do SQL, pelo fuso da propriedade.
func dataDaQuery(r *http.Request, chave string) (string, error) {
	v := httpx.Query(r, chave)
	if v == "" {
		return "", nil
	}
	if _, err := time.Parse(time.DateOnly, v); err != nil {
		return "", apperr.Validation(map[string]string{chave: "data inválida; use AAAA-MM-DD."})
	}
	return v, nil
}

// boolDaQuery é o booleano com padrão `false` (`include_inactive`, `dry_run`).
func boolDaQuery(r *http.Request, chave string) bool {
	v := httpx.ParseBool(r, chave)
	return v != nil && *v
}

// chaveDeOrdem resolve `?sort=` contra a whitelist; o que não está nela cai
// no padrão, como em httpx.ParseSort. O que chega ao SQL é o fragmento DESTE
// pacote, nunca o texto do cliente.
func chaveDeOrdem(r *http.Request, permitidas map[string]string, padrao string) string {
	if v := httpx.Query(r, "sort"); v != "" {
		if _, ok := permitidas[v]; ok {
			return v
		}
	}
	return padrao
}

// estenderPrazo ajusta o prazo da conexão. Sem suporte (ResponseRecorder de
// teste, HTTP/2 atrás de proxy que não expõe) não é erro: a rota segue com o
// prazo padrão do servidor, e o aviso fica no log.
func estenderPrazo(r *http.Request, w http.ResponseWriter, fn func(*http.ResponseController) error) {
	if err := fn(http.NewResponseController(w)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		slog.WarnContext(r.Context(), "bens: não consegui estender o prazo da conexão", "err", err)
	}
}
