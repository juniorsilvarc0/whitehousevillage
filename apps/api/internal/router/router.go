// Package router monta o servidor HTTP a partir da tabela declarativa de rotas.
package router

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/roles"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/users"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/config"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// PrefixoDaAPI casa com o `servers` da OpenAPI e com o Path do cookie de
// refresh: mudar aqui exige mudar os dois.
const PrefixoDaAPI = "/api/v1"

// Opcoes é o que o router precisa de fora.
type Opcoes struct {
	Config config.Config
	Pool   *pgxpool.Pool

	// Notificador entrega o e-mail de recuperação de senha. Nulo cai no
	// registro em log — bom para desenvolvimento, nunca para produção.
	Notificador auth.Notificador
}

// New monta as dependências e devolve o handler pronto.
func New(o Opcoes) (http.Handler, error) {
	tx := db.NewTxManager(o.Pool)

	authRepo := auth.NewRepository(o.Pool)
	emissor := auth.NovoEmissor(o.Config.JWTSecret, o.Config.AccessTTL)
	authSvc := auth.NewService(authRepo, tx, emissor, o.Config.RefreshTTL, o.Notificador)
	autenticador := auth.NewAutenticador(emissor, authRepo)

	usersSvc := users.NewService(users.NewRepository(o.Pool), authRepo, tx)
	rolesSvc := roles.NewService(roles.NewRepository(o.Pool), tx)

	deps := Deps{
		Saude: NewSaude(o.Pool),
		Auth:  auth.NewHandler(authSvc, o.Config.IsProduction()),
		Users: users.NewHandler(usersSvc),
		Roles: roles.NewHandler(rolesSvc),
	}

	tabela := Rotas(deps)
	if err := ValidarTabela(tabela); err != nil {
		return nil, fmt.Errorf("tabela de rotas inválida: %w", err)
	}

	return montar(o.Config, tabela, autenticador), nil
}

func montar(cfg config.Config, tabela []Rota, autenticador *auth.Autenticador) http.Handler {
	r := chi.NewRouter()

	r.Use(httpx.RequestID)
	// RealIP confia no X-Forwarded-For do proxy. Vale porque em produção só o
	// Traefik fala com a API; expor a porta direto na internet faria o IP do
	// limitador virar campo controlado pelo atacante.
	r.Use(middleware.RealIP)
	r.Use(httpx.Recoverer)
	r.Use(httpx.RequestLogger)
	r.Use(httpx.CORS(cfg.CORSOrigins))
	// Teto absoluto por requisição: consulta presa não deve segurar conexão
	// para sempre. Abaixo do WriteTimeout do servidor, para o cliente receber a
	// resposta de timeout em vez da conexão cortada.
	r.Use(middleware.Timeout(50 * time.Second))

	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		httpx.Error(w, req, apperr.NotFound("Recurso"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		httpx.Error(w, req, apperr.NotFound("Recurso"))
	})

	limitadorDeLogin := auth.LimitadorDeLogin()

	r.Route(PrefixoDaAPI, func(api chi.Router) {
		// Grupo público: só quem está listado como AcessoPublico na tabela.
		api.Group(func(pub chi.Router) {
			for _, rota := range tabela {
				if rota.Acesso != AcessoPublico {
					continue
				}
				h := http.Handler(rota.Handler)
				if rota.Path == "/auth/login" {
					// Freio por IP na porta de entrada; o bloqueio por e-mail
					// (5 erros em 15 min) fica no service.
					h = limitadorDeLogin.Middleware(h)
				}
				pub.Method(rota.Metodo, rota.Path, h)
			}
		})

		// Grupo protegido: token obrigatório e, salvo justificativa escrita na
		// tabela, uma célula da matriz de permissões.
		api.Group(func(priv chi.Router) {
			priv.Use(autenticador.Middleware)
			for _, rota := range tabela {
				if rota.Acesso == AcessoPublico {
					continue
				}
				h := http.Handler(rota.Handler)
				if rota.Acesso == AcessoPermissao {
					h = auth.Middleware(rota.Recurso, rota.Acao)(h)
				}
				priv.Method(rota.Metodo, rota.Path, h)
			}
		})
	})

	return r
}
