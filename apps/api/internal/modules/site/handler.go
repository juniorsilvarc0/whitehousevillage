// Package site é a área administrativa do site de vendas (menu "Site" do
// painel) — contrato em docs/site-cms.md.
//
// A gestão edita textos, fotos e vídeos do site a partir de um catálogo fixo
// de campos (catalogo.go). Salvar = publicar; "restaurar o original" apaga a
// linha. O site busca GET /public/site e troca o que foi editado; se a API não
// responder, o visitante vê o texto original que está no HTML.
package site

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// Prazos de rede das rotas de arquivo. O servidor tem ReadTimeout de 30 s e
// WriteTimeout de 60 s (cmd/api) — sadios para JSON, curtos demais para um
// vídeo de 300 MB numa conexão de celular. As duas rotas de arquivo estendem
// o próprio prazo, e só elas.
const (
	prazoDeEnvio   = 15 * time.Minute
	prazoDeEntrega = 30 * time.Minute
)

// Cabeçalhos de cache. O conteúdo muda quando a gestão salva: um minuto de
// cache segura o pico sem deixar a gestão esperando. O arquivo de mídia é
// imutável (trocar a foto = novo id), então o cache é de um ano.
const (
	cacheDoConteudo = "public, max-age=60"
	cacheDaMidia    = "public, max-age=31536000, immutable"
)

var errNaoEncontrado = apperr.NotFound("Arquivo")

// Handler decodifica, chama o service e envelopa. Regra nenhuma mora aqui.
type Handler struct {
	svc *Servico
}

// NovoHandler segue o construtor dos módulos, mais o volume de mídia.
func NovoHandler(pool *pgxpool.Pool, tx *db.TxManager, dirDeMidia string) *Handler {
	return &Handler{svc: NovoServico(NewRepository(pool), tx, dirDeMidia)}
}

// Conteudo — GET /site/content
func (h *Handler) Conteudo(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Conteudo(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, out)
}

// Gravar — PUT /site/content/{key}
func (h *Handler) Gravar(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.Decode[PedidoGravar](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Gravar(r.Context(), chi.URLParam(r, "key"), corpo.Value)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// Restaurar — DELETE /site/content/{key}
func (h *Handler) Restaurar(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Restaurar(r.Context(), chi.URLParam(r, "key")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// EnviarMidia — POST /site/media (multipart, campo `file`)
//
// O corpo é lido em FLUXO (MultipartReader), nunca com ParseMultipartForm: um
// vídeo de 300 MB não passa pela memória nem pelo /tmp do contêiner — vai
// direto para o temporário dentro do volume.
func (h *Handler) EnviarMidia(w http.ResponseWriter, r *http.Request) {
	estenderPrazo(r, w, func(rc *http.ResponseController) error {
		return rc.SetReadDeadline(time.Now().Add(prazoDeEnvio))
	})
	parte, err := httpx.ParteDeArquivo(w, r, "file", LimiteDoCorpo)
	if err != nil {
		httpx.Error(w, r, erroDoMultipart(err))
		return
	}
	defer func() { _ = parte.Close() }()

	out, err := h.svc.EnviarMidia(r.Context(), parte, parte.FileName())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Location", out.URL)
	httpx.JSON(w, http.StatusCreated, out)
}

// erroDoMultipart traduz a falha ao achar a parte do arquivo.
func erroDoMultipart(err error) error {
	if errors.Is(err, httpx.ErrSemArquivo) {
		return ErroDeArquivo(msgArquivoVazio).WithCause(err)
	}
	return erroDeLeitura(err, "")
}

// Publico — GET /public/site
func (h *Handler) Publico(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Publico(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", cacheDoConteudo)
	httpx.JSON(w, http.StatusOK, out)
}

// Midia — GET /public/media/{id}
//
// http.ServeContent responde Range (206), If-Modified-Since e HEAD: é o que o
// <video> precisa para começar a tocar antes de baixar tudo.
func (h *Handler) Midia(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, errNaoEncontrado)
		return
	}
	f, m, err := h.svc.AbrirMidia(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	defer func() { _ = f.Close() }()

	estenderPrazo(r, w, func(rc *http.ResponseController) error {
		return rc.SetWriteDeadline(time.Now().Add(prazoDeEntrega))
	})
	w.Header().Set("Content-Type", m.Mime)
	w.Header().Set("Cache-Control", cacheDaMidia)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", m.CriadoEm, f)
}

// estenderPrazo ajusta o prazo da conexão. Sem suporte (ResponseRecorder de
// teste, HTTP/2 por proxy que não expõe) não é erro: a rota segue com o prazo
// padrão do servidor, e o aviso fica no log.
func estenderPrazo(r *http.Request, w http.ResponseWriter, fn func(*http.ResponseController) error) {
	if err := fn(http.NewResponseController(w)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		slog.WarnContext(r.Context(), "site: não consegui estender o prazo da conexão", "err", err)
	}
}
