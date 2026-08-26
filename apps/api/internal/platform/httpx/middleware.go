package httpx

import (
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

const HeaderRequestID = "X-Request-Id"

// RequestID aceita o id que o cliente (ou o proxy) mandou e o devolve na
// resposta. Correlacionar um erro no painel com a linha do log da API depende
// disso; gerar um id novo aqui quebraria a ponta que o proxy já tinha.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get(HeaderRequestID))
		// Id de fora entra no log; limitar o tamanho evita que alguém use o
		// header como canal para inflar o log.
		if id == "" || len(id) > 128 {
			id = uuid.NewString()
		}
		w.Header().Set(HeaderRequestID, id)
		next.ServeHTTP(w, r.WithContext(ComRequestID(r.Context(), id)))
	})
}

// Recoverer transforma panic em 500 com o mesmo envelope de erro de sempre.
// A stack vai para o log; a resposta não diz mais que "erro interno" — stack
// trace na resposta é mapa da aplicação entregue de graça.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			p := recover()
			if p == nil {
				return
			}
			// ErrAbortHandler é o sinal do net/http de conexão abortada; logar
			// como pânico da aplicação seria ruído.
			if p == http.ErrAbortHandler {
				panic(p)
			}
			slog.ErrorContext(r.Context(), "pânico no handler",
				"panic", p,
				"method", r.Method,
				"path", r.URL.Path,
				"request_id", RequestIDDe(r),
				"stack", string(debug.Stack()),
			)
			Error(w, r, apperr.Internal)
		}()
		next.ServeHTTP(w, r)
	})
}

// RequestLogger registra uma linha por requisição, depois de servida — é onde a
// rota (padrão do chi, não o path com id) e o status ficam disponíveis.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inicio := time.Now()
		gravador := &respostaObservada{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(gravador, r)

		// Usar o padrão da rota ("/users/{id}") em vez do path evita explodir a
		// cardinalidade do log e das métricas com um valor por uuid.
		rota := r.URL.Path
		if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
			rota = rc.RoutePattern()
		}

		nivel := slog.LevelInfo
		switch {
		case gravador.status >= 500:
			nivel = slog.LevelError
		case gravador.status >= 400:
			nivel = slog.LevelWarn
		}

		slog.Log(r.Context(), nivel, "requisição",
			"method", r.Method,
			"route", rota,
			"status", gravador.status,
			"bytes", gravador.bytes,
			"duration_ms", time.Since(inicio).Milliseconds(),
			"request_id", RequestIDDe(r),
			"user_id", Ator(r.Context()),
			"ip", IPDoCliente(r),
		)
	})
}

// CORS monta a política a partir da configuração. Origens vêm do ambiente:
// `*` com credenciais é recusado pelo navegador, então a lista tem de ser
// explícita.
func CORS(origens []string) func(http.Handler) http.Handler {
	return cors.Handler(cors.Options{
		AllowedOrigins:   origens,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", HeaderRequestID, "Idempotency-Key", "If-Match"},
		ExposedHeaders:   []string{HeaderRequestID, "Location", "ETag"},
		AllowCredentials: true, // o refresh viaja em cookie httpOnly
		MaxAge:           300,
	})
}

// ─────────────────────────── Limite de tentativas ───────────────────────────

// Limitador é um contador por chave em janela fixa, guardado em memória.
//
// ATENÇÃO: em memória do processo. Com duas instâncias da API atrás de um load
// balancer, o atacante ganha o limite multiplicado pelo número de instâncias, e
// um deploy zera todos os contadores. Isto é o piso — barra o ataque
// oportunista de força bruta a partir de um IP —, não a defesa definitiva. A
// versão que aguenta multi-instância mora no Redis (ou numa tabela com janela
// deslizante) e está anotada como pendência no relatório.
type Limitador struct {
	limite int
	janela time.Duration

	mu           sync.Mutex
	entradas     map[string]*janelaFixa
	proximaVarre time.Time
}

// intervaloMaximoDeVarredura limita de quanto em quanto tempo o mapa é limpo.
const intervaloMaximoDeVarredura = 5 * time.Minute

type janelaFixa struct {
	contagem int
	expiraEm time.Time
}

func NovoLimitador(limite int, janela time.Duration) *Limitador {
	return &Limitador{limite: limite, janela: janela, entradas: map[string]*janelaFixa{}}
}

// Permitir contabiliza uma tentativa e diz se ela cabe na janela.
func (l *Limitador) Permitir(chave string) bool {
	agora := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()
	l.varrer(agora)

	e, ok := l.entradas[chave]
	if !ok || agora.After(e.expiraEm) {
		l.entradas[chave] = &janelaFixa{contagem: 1, expiraEm: agora.Add(l.janela)}
		return true
	}
	e.contagem++
	return e.contagem <= l.limite
}

// Bloqueado consulta sem contabilizar — o login precisa saber se o e-mail está
// bloqueado antes de gastar tempo com argon2.
func (l *Limitador) Bloqueado(chave string) bool {
	agora := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	e, ok := l.entradas[chave]
	if !ok || agora.After(e.expiraEm) {
		return false
	}
	// >= e não >: com limite 5, o bloqueio começa DEPOIS do quinto erro, que é
	// o que "5 tentativas erradas bloqueiam" quer dizer. Com > a sexta ainda
	// passaria.
	return e.contagem >= l.limite
}

// Registrar conta uma falha sem responder nada — é o que o login chama quando a
// senha erra.
func (l *Limitador) Registrar(chave string) { _ = l.Permitir(chave) }

// Marcar anota a chave como vista, para consulta por Marcado dentro da janela.
// Não é contagem: é memória curta de "isto já deu certo aqui" — o login usa para
// lembrar de quais IPs a conta já entrou e assim não trancar o dono junto com o
// atacante.
func (l *Limitador) Marcar(chave string) {
	agora := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()
	l.varrer(agora)

	l.entradas[chave] = &janelaFixa{contagem: l.limite, expiraEm: agora.Add(l.janela)}
}

// Marcado responde se a marca ainda vale. É a mesma leitura de Bloqueado — o que
// muda é o significado que quem chama dá à chave.
func (l *Limitador) Marcado(chave string) bool { return l.Bloqueado(chave) }

// Limpar zera a chave. O login chama no acerto: quem lembrou a senha não deve
// carregar o histórico de erro.
func (l *Limitador) Limpar(chave string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entradas, chave)
}

// varrer descarta janelas vencidas. Sem isso o mapa cresce com um IP por
// tentativa e vira vazamento de memória num processo de vida longa.
func (l *Limitador) varrer(agora time.Time) {
	if agora.Before(l.proximaVarre) {
		return
	}
	// Teto no intervalo: um limitador de janela longa (o de IP conhecido tem
	// sete dias) varreria uma vez por semana e seguraria até lá a memória de
	// entradas já vencidas.
	l.proximaVarre = agora.Add(min(l.janela, intervaloMaximoDeVarredura))
	for chave, e := range l.entradas {
		if agora.After(e.expiraEm) {
			delete(l.entradas, chave)
		}
	}
}

// Middleware aplica o limitador por IP. Usado no /auth/login, onde o custo do
// argon2 torna a força bruta cara para o servidor também.
func (l *Limitador) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Permitir("ip:" + IPDoCliente(r)) {
			Error(w, r, apperr.RateLimited)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ─────────────────────────── Auxiliares ─────────────────────────────────────

// IPDoCliente devolve o IP sem a porta. Confia no que o middleware RealIP do
// chi já normalizou a partir do proxy — em produção só o proxy fala com a API.
func IPDoCliente(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// RequestIDDe é o atalho para o log dentro de middleware.
func RequestIDDe(r *http.Request) string { return RequestIDDoContexto(r.Context()) }

type respostaObservada struct {
	http.ResponseWriter
	status  int
	bytes   int
	escrito bool
}

func (w *respostaObservada) WriteHeader(status int) {
	if w.escrito {
		return
	}
	w.status = status
	w.escrito = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *respostaObservada) Write(b []byte) (int, error) {
	if !w.escrito {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Flush mantém o SSE funcionando através do wrapper: sem repassar, o evento
// fica preso no buffer.
func (w *respostaObservada) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
