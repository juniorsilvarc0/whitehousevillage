package auth

import (
	"net/http"
	"time"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// NomeDoCookieRefresh e CaminhoDoCookieRefresh vêm do contrato.
//
// O Path restrito a /api/v1/auth é deliberado: o refresh só é necessário nos
// quatro endpoints de sessão, então o navegador não o anexa em nenhuma outra
// requisição — menos superfície para CSRF e para log de proxy.
const (
	NomeDoCookieRefresh    = "wh_refresh"
	CaminhoDoCookieRefresh = "/api/v1/auth"
)

type Handler struct {
	svc    *Service
	seguro bool // Secure no cookie — só em produção, senão o dev em http perde o cookie
}

func NewHandler(svc *Service, producao bool) *Handler {
	return &Handler{svc: svc, seguro: producao}
}

// Login — POST /auth/login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	pedido, err := httpx.Decode[PedidoLogin](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	sess, err := h.svc.Login(r.Context(), pedido, origemDe(r))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	h.gravarCookie(w, sess)
	httpx.JSON(w, http.StatusOK, sess.Resposta)
}

// Refresh — POST /auth/refresh.
//
// O token pode chegar no cookie httpOnly (caminho do painel) ou no corpo (para
// cliente sem cookie jar). Vindo os dois, o cookie vence: ele é o que o
// navegador protege, e um corpo divergente é sinal de cliente confuso ou de
// tentativa de fixação de sessão.
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.DecodeOpcional[PedidoRefresh](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	token := corpo.RefreshToken
	if c, err := r.Cookie(NomeDoCookieRefresh); err == nil && c.Value != "" {
		token = c.Value
	}

	sess, err := h.svc.Refresh(r.Context(), token, origemDe(r))
	if err != nil {
		// Sessão morta: apaga o cookie junto, senão o navegador reapresenta o
		// mesmo token morto a cada tentativa.
		h.apagarCookie(w)
		httpx.Error(w, r, err)
		return
	}

	h.gravarCookie(w, sess)
	httpx.JSON(w, http.StatusOK, sess.Resposta)
}

// Logout — POST /auth/logout. Sempre 204.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	corpo, err := httpx.DecodeOpcional[PedidoLogout](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	token := corpo.RefreshToken
	if c, err := r.Cookie(NomeDoCookieRefresh); err == nil && c.Value != "" {
		token = c.Value
	}

	if err := h.svc.Logout(r.Context(), token); err != nil {
		httpx.Error(w, r, err)
		return
	}

	h.apagarCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// Me — GET /auth/me
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	resposta, err := h.svc.Me(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resposta)
}

// EsqueciSenha — POST /auth/password/forgot. Sempre 202 (ou 429).
func (h *Handler) EsqueciSenha(w http.ResponseWriter, r *http.Request) {
	pedido, err := httpx.Decode[PedidoEsqueciSenha](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.svc.EsqueciSenha(r.Context(), pedido.Email, origemDe(r)); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// TrocarSenha — POST /auth/password/reset
func (h *Handler) TrocarSenha(w http.ResponseWriter, r *http.Request) {
	pedido, err := httpx.Decode[PedidoTrocarSenha](r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	if err := h.svc.TrocarSenha(r.Context(), pedido); err != nil {
		httpx.Error(w, r, err)
		return
	}

	// A senha mudou: o refresh que o navegador ainda guarda foi revogado no
	// mesmo instante.
	h.apagarCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// ─────────────────────────── Cookie ─────────────────────────────────────────

func (h *Handler) gravarCookie(w http.ResponseWriter, s Sessao) {
	http.SetCookie(w, &http.Cookie{
		Name:  NomeDoCookieRefresh,
		Value: s.RefreshClaro,
		Path:  CaminhoDoCookieRefresh,
		// httpOnly é o ponto do desenho: fora do alcance de document.cookie,
		// um XSS no painel não consegue exfiltrar a sessão longa.
		HttpOnly: true,
		Secure:   h.seguro,
		// Lax, e não Strict, porque o painel abre links vindos de e-mail; e não
		// None, que exigiria Secure e abriria CSRF entre sites.
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(s.RefreshTTL),
		MaxAge:   int(s.RefreshTTL.Seconds()),
	})
}

func (h *Handler) apagarCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     NomeDoCookieRefresh,
		Value:    "",
		Path:     CaminhoDoCookieRefresh,
		HttpOnly: true,
		Secure:   h.seguro,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1, // vira Max-Age=0 no header
	})
}

func origemDe(r *http.Request) Origem {
	ua := r.UserAgent()
	// O user_agent vai para uma coluna text; truncar evita que um header
	// gigante encha a tabela de sessões.
	if len(ua) > 255 {
		ua = ua[:255]
	}
	return Origem{UserAgent: ua, IP: httpx.IPDoCliente(r)}
}

// LimitadorDeLogin expõe o limitador por IP que o router pluga na rota de
// login, além do bloqueio por e-mail que o service já aplica.
func LimitadorDeLogin() *httpx.Limitador {
	return httpx.NovoLimitador(20, janelaDeLogin)
}
