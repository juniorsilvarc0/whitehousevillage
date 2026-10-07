// Package router monta o servidor HTTP a partir da tabela declarativa de rotas.
package router

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/bens"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/contatos"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/crm"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/disponibilidade"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/inventario"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/reservas"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/roles"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/site"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/stream"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/tarifario"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/users"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/modules/vitrine"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
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

		// Fase 1. Os quatro módulos expõem o MESMO construtor
		// `NovoHandler(pool, tx)` — foi o contrato combinado entre os agentes
		// justamente para a montagem caber em quatro linhas iguais.
		//
		// Preencher os quatro é OBRIGATÓRIO, e não opcional: `rotas_inventario.go`
		// e `rotas_reservas.go` deixaram de guardar contra handler nulo para que
		// o teste de contrato do CI enxergue as rotas deles. Um main que
		// esquecer um campo sobe com as rotas apontando para ponteiro nulo.
		Inventario:      inventario.NovoHandler(o.Pool, tx),
		Tarifario:       tarifario.NovoHandler(o.Pool, tx),
		Disponibilidade: disponibilidade.NovoHandler(o.Pool, tx),
		Reservas:        reservas.NovoHandler(o.Pool, tx),

		// O CRM segue o mesmo construtor. O stream ignora o TxManager (ele não
		// escreve): abre uma conexão dedicada para o LISTEN, porque LISTEN não
		// convive com conexão devolvida ao pool entre consultas.
		CRM:    crm.NovoHandler(o.Pool, tx),
		Stream: stream.NovoHandler(o.Pool, tx),

		// Contatos entra pelo mesmo construtor. Montar aqui é OBRIGATÓRIO: as
		// rotas dele degradam para 503 com handler nulo (rotas_contatos.go) em
		// vez de estourarem panic, o que é bom para não derrubar o processo e
		// péssimo como estado permanente — a agenda simplesmente não existiria.
		Contatos: contatos.NovoHandler(o.Pool, tx),

		// Vitrine: as rotas /public/* do site de vendas.
		Vitrine: vitrine.NovoHandler(o.Pool, tx),

		// Site: conteúdo editável do site de vendas e o volume de mídia.
		Site: site.NovoHandler(o.Pool, tx, o.Config.MediaDir),

		// Bens por ambiente; as fotos vão para MEDIA_DIR/bens. Montar é
		// OBRIGATÓRIO: rotas_inventario_bens.go não guarda contra handler nulo.
		Bens: bens.NovoHandler(o.Pool, tx, o.Config.MediaDir),
	}

	prepararVolumeDeMidia(o.Config.MediaDir)

	tabela := Rotas(deps)
	if err := ValidarTabela(tabela); err != nil {
		return nil, fmt.Errorf("tabela de rotas inválida: %w", err)
	}

	return montar(o.Config, tabela, autenticador), nil
}

func montar(cfg config.Config, tabela []Rota, autenticador *auth.Autenticador) http.Handler {
	r := chi.NewRouter()

	r.Use(httpx.RequestID)
	// httpx.RealIP, e não `chi/middleware.RealIP`: o do chi está DEPRECADO por
	// spoofing (GHSA-3fxj-6jh8-hvhx e dois irmãos) porque lê a entrada mais à
	// ESQUERDA de X-Forwarded-For — a única que o cliente escreve. O nosso só
	// acredita em cabeçalho quando o peer direto é rede interna, e lê a cadeia
	// da direita para a esquerda. Importa aqui porque este IP é a chave do
	// limitador do /auth/login e a coluna `audit_log.ip`.
	r.Use(httpx.RealIP)
	// DEPOIS do RealIP, e não antes: o audit lê `httpx.IPDoCliente`, que devolve
	// o IP que o RealIP resolveu e guardou no contexto — antes dele, a leitura
	// cairia no peer direto (o Traefik). Montado aqui, e não dentro do grupo
	// protegido, porque escrita de rota pública também deixa trilha (troca de
	// senha) e porque o custo é duas leituras de header por requisição.
	//
	// Sem esta linha `audit_log.ip` e `audit_log.user_agent` saem NULOS no
	// sistema montado — as suítes de módulo montam o middleware à mão e passam
	// verde, e foi assim que a ausência atravessou duas revisões.
	r.Use(audit.Middleware)
	r.Use(httpx.Recoverer)
	r.Use(httpx.RequestLogger)
	r.Use(httpx.CORS(cfg.CORSOrigins))
	// O teto absoluto por requisição (middleware.Timeout) é aplicado POR ROTA,
	// em comTeto, e não aqui: as rotas de longa duração (SSE, upload e entrega
	// de vídeo — rotasDeLongaDuracao) precisam ficar fora dele.

	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		httpx.Error(w, req, apperr.NotFound("Recurso"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		httpx.Error(w, req, apperr.NotFound("Recurso"))
	})

	limitadorDeLogin := auth.LimitadorDeLogin()
	limitadorDaVitrine := LimitadorDaVitrine()
	limitadorDePreReserva := LimitadorDePreReserva()
	limitadorDeMidia := LimitadorDeMidia()

	// As sondas primeiro, na RAIZ e fora do prefixo — ver Rota.NaRaiz.
	for _, rota := range tabela {
		if rota.NaRaiz {
			r.Method(rota.Metodo, rota.Path, comTeto(rota, rota.Handler))
		}
	}

	r.Route(PrefixoDaAPI, func(api chi.Router) {
		// Grupo público: só quem está listado como AcessoPublico na tabela.
		api.Group(func(pub chi.Router) {
			for _, rota := range tabela {
				if rota.Acesso != AcessoPublico || rota.NaRaiz {
					continue
				}
				h := http.Handler(rota.Handler)
				if rota.Path == "/auth/login" {
					// Freio por IP na porta de entrada; o bloqueio por e-mail
					// (5 erros em 15 min) fica no service.
					h = limitadorDeLogin.Middleware(h)
				}
				switch {
				case EhMidiaPublica(rota):
					h = limitadorDeMidia.Middleware(h)
				case EhDaVitrine(rota):
					h = limitadorDaVitrine.Middleware(h)
				}
				if rota.Path == RotaDePreReservaPublica {
					h = limitadorDePreReserva.Middleware(h)
				}
				pub.Method(rota.Metodo, rota.Path, comTeto(rota, h))
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
				priv.Method(rota.Metodo, rota.Path, comTeto(rota, h))
			}
		})
	})

	return r
}

// LimitadorDaVitrine freia as rotas /public/* por IP: 120 pedidos por minuto.
//
// O site faz poucos pedidos por visita (catálogo, política, dois meses de
// calendário e um orçamento por par de datas); 120 por minuto não alcança um
// visitante de verdade e corta quem varre o calendário inteiro em laço.
//
// Vive na memória do processo — a dívida D1 do roadmap. Com UMA réplica da API
// ele faz o que promete; com duas, o limite multiplica. O limitador
// distribuído (B0 do plano) é pré-requisito antes de subir a segunda réplica.
func LimitadorDaVitrine() *httpx.Limitador { return httpx.NovoLimitador(120, time.Minute) }

// RotaDePreReservaPublica é a única rota pública que grava.
const RotaDePreReservaPublica = "/public/holds"

// LimitadorDePreReserva freia a pré-reserva pública por IP: 6 por hora, além
// dos 120/min da vitrine. Uma família faz uma, talvez duas; a sétima na mesma
// hora é alguém tentando segurar o calendário sem pagar. O teto por telefone
// (módulo vitrine) cobre quem troca de IP; este cobre quem troca de telefone.
func LimitadorDePreReserva() *httpx.Limitador { return httpx.NovoLimitador(6, time.Hour) }

// EhDaVitrine diz se a rota é da superfície pública do site e passa pelo
// limitador de 120/min. A mídia pública tem limitador próprio (EhMidiaPublica).
func EhDaVitrine(r Rota) bool {
	return r.Acesso == AcessoPublico && strings.HasPrefix(r.Path, "/public/") && !EhMidiaPublica(r)
}

// EhMidiaPublica diz se a rota é a entrega de foto/vídeo do site.
func EhMidiaPublica(r Rota) bool {
	return r.Acesso == AcessoPublico && r.Path == RotaDaMidiaPublica
}

// LimitadorDeMidia freia a entrega de foto e vídeo do site por IP: 1200 por
// minuto. Um <video> pede o arquivo em pedaços (Range) — dezenas de pedidos
// para tocar um vídeo de capa —, e uma página com várias fotos soma outros
// tantos. Sob os 120/min da vitrine, o vídeo travaria no meio para um
// visitante comum; 1200 ainda corta quem baixa em laço.
func LimitadorDeMidia() *httpx.Limitador { return httpx.NovoLimitador(1200, time.Minute) }

// tetoPorRequisicao: consulta presa não deve segurar conexão para sempre.
// Abaixo do WriteTimeout do servidor, para o cliente receber a resposta de
// timeout em vez da conexão cortada.
const tetoPorRequisicao = 50 * time.Second

// comTeto aplica o teto por requisição, exceto às rotas de longa duração.
func comTeto(rota Rota, h http.Handler) http.Handler {
	if EhDeLongaDuracao(rota) {
		return h
	}
	return middleware.Timeout(tetoPorRequisicao)(h)
}

// prepararVolumeDeMidia cria o diretório de mídia se faltar. Não derruba a
// subida: sem o volume a API inteira continua servindo, e só o envio de
// arquivo falha (com erro claro no log) — melhor que o painel todo fora.
func prepararVolumeDeMidia(dir string) {
	if dir == "" {
		slog.Warn("MEDIA_DIR vazio: envio de fotos e vídeos do site desligado")
		return
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		slog.Error("não consegui preparar o volume de mídia do site", "dir", dir, "err", err)
	}
}
