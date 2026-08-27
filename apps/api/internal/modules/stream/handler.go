// Package stream serve o `GET /stream`: a conexão SSE por onde o mapa de
// ocupação e o kanban se mexem sozinhos.
//
// # Onde cada responsabilidade mora
//
// O evento NASCE em gatilho no banco (migration 20260827120000), viaja por
// `pg_notify`, é recolhido por UMA conexão de `LISTEN` do
// `internal/platform/realtime` e chega aqui já decodificado. Nenhum módulo de
// negócio publica evento: escrever no barramento a partir do Go criaria dois
// modos de falha — o processo morre entre o COMMIT e o `NOTIFY` (a venda
// aconteceu e o mapa não soube), e o `NOTIFY` sai de uma transação que depois
// faz rollback (o mapa mostra reserva que não existe).
//
// O que este pacote acrescenta é o que o barramento não pode saber: QUEM está
// do outro lado. Tópico contra a matriz, escopo `own` contra o dono da linha,
// propriedade contra a do requisitante, e o desligamento honesto quando o token
// vence.
package stream

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/db"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/realtime"
)

// Padrões da conexão.
const (
	// batimentoPadrao — o `: keep-alive` do contrato. Abaixo do ocioso típico
	// de proxy reverso (60 s no Traefik e no nginx), com folga para uma
	// batida perdida.
	batimentoPadrao = 20 * time.Second

	// reconexaoSugerida vira `retry:` no stream. O EventSource usa isso para
	// decidir quanto esperar antes de voltar.
	reconexaoSugerida = 5 * time.Second

	// avisoDeExpiracaoPadrao — quanto antes do `exp` o cliente é avisado. 60 s
	// é tempo de sobra para um `POST /auth/refresh` e a reconexão.
	avisoDeExpiracaoPadrao = 60 * time.Second

	// vidaMaximaSemExpPadrao é o teto de quem chegou por um caminho em que não
	// se conseguiu ler o `exp` do token. Existe para que "não sei quando esse
	// token vence" nunca signifique "então vale para sempre".
	vidaMaximaSemExpPadrao = 15 * time.Minute
)

// Opcoes ajusta os tempos. Zero-value cai nos padrões — o teste encurta tudo.
type Opcoes struct {
	Batimento        time.Duration
	Reconexao        time.Duration
	AvisoDeExpiracao time.Duration
	VidaMaximaSemExp time.Duration
	Log              *slog.Logger
}

func (o Opcoes) normalizar() Opcoes {
	if o.Batimento <= 0 {
		o.Batimento = batimentoPadrao
	}
	if o.Reconexao <= 0 {
		o.Reconexao = reconexaoSugerida
	}
	if o.AvisoDeExpiracao <= 0 {
		o.AvisoDeExpiracao = avisoDeExpiracaoPadrao
	}
	if o.VidaMaximaSemExp <= 0 {
		o.VidaMaximaSemExp = vidaMaximaSemExpPadrao
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return o
}

// Handler expõe o `GET /stream`.
type Handler struct {
	hub    *realtime.Hub
	filtro *Filtro
	cfg    Opcoes

	// hubProprio marca que o hub nasceu aqui e portanto morre aqui. Um hub
	// injetado (teste, ou um dia dois handlers sobre o mesmo barramento) é de
	// quem o criou.
	hubProprio bool
	fechar     sync.Once
}

// NovoHandler é o construtor combinado dos módulos — o mesmo
// `NovoHandler(pool, tx)` dos outros quatro.
//
// O `TxManager` não é usado e é recebido de propósito: o construtor é uniforme
// para o main montar todos os módulos em linhas iguais. Tempo real não escreve
// nada — a única escrita do fluxo é a do módulo de negócio que disparou o
// gatilho, e ela já comitou quando o evento chega aqui.
//
// O hub sobe com `context.Background()`: ele vive enquanto o PROCESSO viver, e
// não enquanto uma requisição viver. Quem quiser derrubá-lo (o teste, um
// encerramento gracioso) chama Fechar.
func NovoHandler(pool *pgxpool.Pool, _ *db.TxManager) *Handler {
	cfg := Opcoes{}.normalizar()

	hub := realtime.NovoHub(realtime.Config{
		Abrir: realtime.AbridorDePool(pool),
		Log:   cfg.Log,
	})
	hub.Iniciar(context.Background())

	return &Handler{hub: hub, filtro: NovoFiltro(pool), cfg: cfg, hubProprio: true}
}

// NovoHandlerCom monta o handler sobre um hub já pronto. É o construtor do
// teste e o que permite ao hub ser exercitado sem banco nenhum.
func NovoHandlerCom(hub *realtime.Hub, filtro *Filtro, o Opcoes) *Handler {
	return &Handler{hub: hub, filtro: filtro, cfg: o.normalizar()}
}

// Fechar derruba o hub que este handler criou. Idempotente.
func (h *Handler) Fechar() {
	h.fechar.Do(func() {
		if h != nil && h.hubProprio && h.hub != nil {
			h.hub.Parar()
		}
	})
}

// eventoDoStream é o `data:` dos eventos de dado — o schema EventoDoStream do
// contrato, e SÓ ele. Nome de hóspede, valor e telefone não trafegam: quem quer
// o dado refaz o fetch autenticado, e aí o RBAC decide.
type eventoDoStream struct {
	Entity string     `json:"entity"`
	ID     uuid.UUID  `json:"id"`
	UnitID *uuid.UUID `json:"unit_id,omitempty"`
	V      int64      `json:"v"`
}

// Assinar — GET /stream?topics=calendar,crm
func (h *Handler) Assinar(w http.ResponseWriter, r *http.Request) {
	// Receptor nulo é o main que esqueceu de preencher `Deps.Stream`. Responder
	// 503 com texto é muito melhor que o pânico que o Recoverer converteria num
	// 500 sem explicação — e o log da linha diz o que fazer.
	if h == nil || h.hub == nil {
		httpx.Error(w, r, apperr.Internal.
			WithMessage("O tempo real não está montado neste processo.").
			WithStatus(http.StatusServiceUnavailable).
			WithCause(errors.New("router.Deps.Stream não foi preenchido")))
		return
	}

	u, ok := auth.UserFrom(r.Context())
	if !ok {
		httpx.Error(w, r, apperr.Unauthorized)
		return
	}

	pedidos, err := topicosPedidos(r)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	permitidos := alcancaveis(u, pedidos)
	if len(permitidos) == 0 {
		// 403, e não uma conexão vazia. Abrir um stream que nunca entrega nada
		// é o pior desfecho: o cliente espera para sempre um evento que não vem
		// e ninguém consegue distinguir isso de "está tudo parado".
		httpx.Error(w, r, apperr.Forbidden.WithDetails(map[string]any{
			"topics": pedidos,
			"reason": "nenhum dos tópicos pedidos é alcançável pela sua matriz de permissões",
		}))
		return
	}

	desde, cursorIlegivel := cursorPedido(r)

	assinatura, reposicao, err := h.hub.Assinar(u.ID.String(), permitidos, desde)
	if err != nil {
		httpx.Error(w, r, erroDeAssinatura(err))
		return
	}
	defer assinatura.Cancelar()

	escritor, err := novoEscritor(w, h.cfg.Log)
	if err != nil {
		httpx.Error(w, r, apperr.Internal.WithCause(err))
		return
	}

	vida, encerrar := vidaDaConexao(r)
	defer encerrar()

	expiraEm := expiracaoDoToken(r, h.cfg.VidaMaximaSemExp)

	// Daqui para baixo o 200 já foi. Nenhum erro pode virar status HTTP: a
	// única linguagem que resta é o próprio stream, e o fim dele.
	escritor.cabecalhos()
	if err := escritor.retry(h.cfg.Reconexao); err != nil {
		return
	}
	if err := escritor.evento("ready", 0, map[string]any{
		"topics":      assinatura.Topicos(),
		"server_time": time.Now().Format(time.RFC3339),
	}); err != nil {
		return
	}

	if reposicao.Resync || cursorIlegivel {
		if err := h.enviarResync(escritor, assinatura, motivoDoResync(reposicao.Resync, cursorIlegivel)); err != nil {
			return
		}
	} else {
		for _, ev := range reposicao.Eventos {
			if err := h.entregar(vida, escritor, assinatura, u, ev); err != nil {
				return
			}
		}
	}

	h.laco(vida, escritor, assinatura, u, expiraEm)
}

// laco é a vida da conexão. Roda na goroutine do PRÓPRIO handler: um SSE não
// precisa de goroutine extra para escrever, e cada goroutine a mais é uma a mais
// para provar que morre.
func (h *Handler) laco(vida context.Context, e *escritor, a *realtime.Assinatura, u *auth.Usuario, expiraEm time.Time) {
	batimento := time.NewTicker(h.cfg.Batimento)
	defer batimento.Stop()

	aviso := time.NewTimer(time.Until(expiraEm.Add(-h.cfg.AvisoDeExpiracao)))
	defer aviso.Stop()
	expiracao := time.NewTimer(time.Until(expiraEm))
	defer expiracao.Stop()

	for {
		select {
		case <-vida.Done():
			return

		case <-expiracao.C:
			// O token venceu. Não dá para responder 401 depois de um 200 já
			// enviado — a única saída honesta é dizer e encerrar de forma
			// limpa, para o cliente renovar em /auth/refresh e voltar com o
			// mesmo Last-Event-ID.
			_ = e.evento("expired", 0, map[string]any{"expired_at": expiraEm.Format(time.RFC3339)})
			return

		case <-aviso.C:
			if err := e.evento("expiring", 0, map[string]any{"expires_at": expiraEm.Format(time.RFC3339)}); err != nil {
				return
			}

		case <-batimento.C:
			// A marca de resync é lida AQUI também, e não só quando chega
			// evento: a conexão que perdeu o fio pode não receber mais nada, e
			// esperar por um evento que não vem para avisar que se perdeu um
			// evento é o defeito se escondendo dentro do próprio sintoma.
			if a.ConsumirResync() {
				if err := h.enviarResync(e, a, "o barramento perdeu eventos desta conexão"); err != nil {
					return
				}
			}
			// O batimento é o que detecta cliente morto quando o contexto da
			// requisição não avisa (ver vidaDaConexao): escrever em socket
			// fechado devolve erro, e o erro encerra o laço.
			if err := e.comentario("keep-alive"); err != nil {
				return
			}

		case av, aberto := <-a.Avisos():
			if !aberto {
				return
			}
			if av.Resync || a.ConsumirResync() {
				if err := h.enviarResync(e, a, "o barramento perdeu eventos desta conexão"); err != nil {
					return
				}
				continue
			}
			if err := h.entregar(vida, e, a, u, av.Evento); err != nil {
				return
			}
		}
	}
}

// entregar aplica o filtro de permissão e escreve o evento.
func (h *Handler) entregar(ctx context.Context, e *escritor, a *realtime.Assinatura, u *auth.Usuario, ev realtime.Evento) error {
	if h.filtro != nil {
		pode, err := h.filtro.PodeEntregar(ctx, u, ev)
		if err != nil {
			// Não dá para decidir se esta pessoa pode saber disso. Entregar
			// seria vazar; engolir seria a tela ficar velha em silêncio. O
			// resync é a terceira saída: não revela nada e manda o cliente
			// refazer o fetch, onde o RBAC de verdade responde.
			h.cfg.Log.Warn("tempo real: escopo não pôde ser conferido", "err", err,
				"entidade", ev.Entidade, "usuario", u.ID)
			return h.enviarResync(e, a, "não foi possível conferir a permissão deste evento")
		}
		if !pode {
			return nil
		}
	}

	dados := eventoDoStream{Entity: ev.Entidade, ID: ev.ID, V: ev.V}
	if ev.UnitID != uuid.Nil {
		unidade := ev.UnitID
		dados.UnitID = &unidade
	}
	return e.evento(ev.Topico, ev.Cursor, dados)
}

// enviarResync manda o cliente refazer o fetch. Sem `id:`: resync não é mudança
// de dado e não pode voltar no replay de uma reconexão futura.
func (h *Handler) enviarResync(e *escritor, a *realtime.Assinatura, motivo string) error {
	return e.evento("resync", 0, map[string]any{
		"topics": a.Topicos(),
		"reason": motivo,
	})
}

func motivoDoResync(bufferVelho, cursorIlegivel bool) string {
	switch {
	case cursorIlegivel:
		return "Last-Event-ID ilegível"
	case bufferVelho:
		return "o cursor pedido é mais antigo que o buffer de reposição"
	default:
		return "recarregue a faixa visível"
	}
}

func erroDeAssinatura(err error) error {
	switch {
	case errors.Is(err, realtime.ErrLimitePorChave):
		return apperr.RateLimited.
			WithMessage("Muitas conexões de tempo real abertas para este usuário.").
			WithDetails(map[string]any{"scope": "user"})
	case errors.Is(err, realtime.ErrLimiteGlobal):
		return apperr.RateLimited.
			WithMessage("O servidor está no limite de conexões de tempo real.").
			WithDetails(map[string]any{"scope": "server"})
	case errors.Is(err, realtime.ErrSemTopicos):
		return apperr.Validation(map[string]any{"topics": "informe ao menos um tópico (calendar, crm)."})
	default:
		return apperr.Internal.WithCause(err)
	}
}

// ─────────────────────────── Cursor e relógio ───────────────────────

// cursorPedido lê o Last-Event-ID. O segundo retorno diz que veio um cursor
// ILEGÍVEL — que é diferente de não ter vindo cursor nenhum: quem não mandou
// nada vai fazer o fetch inicial de qualquer jeito; quem mandou lixo acha que
// está em dia e precisa ouvir um resync.
func cursorPedido(r *http.Request) (int64, bool) {
	bruto := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	if bruto == "" {
		// Cliente que não é navegador não tem como injetar o cabeçalho na
		// reconexão automática; o contrato aceita o mesmo valor por query.
		bruto = strings.TrimSpace(httpx.Query(r, "last_event_id"))
	}
	if bruto == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(bruto, 10, 64)
	if err != nil || n <= 0 {
		return 0, true
	}
	return n, false
}

// expiracaoDoToken lê o `exp` do Bearer sem reverificar a assinatura.
//
// Não reverificar é seguro AQUI e só aqui: o `auth.Autenticador` já rodou e
// recusou o que não fosse válido — este handler só é alcançado por token cuja
// assinatura, emissor e validade já foram conferidos. O que se lê aqui é
// portanto um `exp` autêntico, e serve a um único propósito: agendar o aviso e
// o desligamento.
//
// Repetir a verificação exigiria o segredo JWT dentro deste módulo, ou seja, um
// segundo lugar do sistema capaz de validar sessão. Dois validadores é como um
// deles fica para trás.
func expiracaoDoToken(r *http.Request, maximo time.Duration) time.Time {
	teto := time.Now().Add(maximo)

	partes := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
	if len(partes) != 2 || !strings.EqualFold(partes[0], "Bearer") {
		return teto
	}
	segmentos := strings.Split(strings.TrimSpace(partes[1]), ".")
	if len(segmentos) != 3 {
		return teto
	}
	corpo, err := base64.RawURLEncoding.DecodeString(segmentos[1])
	if err != nil {
		return teto
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(corpo, &claims); err != nil || claims.Exp <= 0 {
		return teto
	}

	exp := time.Unix(claims.Exp, 0)
	// Nunca ALÉM do teto: um token de vida longa não deve virar conexão de vida
	// longa sem ninguém decidir isso.
	if exp.After(teto) {
		return teto
	}
	return exp
}

// vidaDaConexao devolve o contexto que governa a conexão SSE.
//
// # O problema que ele resolve
//
// A cadeia do router monta `middleware.Timeout(50s)` — um teto sadio para
// requisição de consulta, e um desastre para SSE: aos 50 s o contexto da
// requisição é cancelado e toda conexão de tempo real cairia, todas as vezes.
// Um stream que morre de minuto em minuto não é tempo real, é uma enquete cara.
//
// # A distinção que torna isso seguro
//
// Os dois cancelamentos que chegam por aquele contexto têm causas DIFERENTES, e
// o Go as separa: prazo de middleware chega como `DeadlineExceeded`;
// desconexão do cliente chega como `Canceled`. Só o segundo encerra a conexão.
//
// # O que ainda fica de fora, e como é coberto
//
// Depois que o prazo do middleware estoura, aquele contexto já está `Done` e não
// sinaliza de novo — uma desconexão POSTERIOR não chega por aqui. Quem a detecta
// é a escrita: o batimento periódico escreve no socket, e escrever em socket
// fechado devolve erro, que encerra o laço. A janela de detecção é, no pior
// caso, um batimento. A correção definitiva é `/stream` ficar fora do
// `middleware.Timeout` — está no relatório para o integrador.
func vidaDaConexao(r *http.Request) (context.Context, context.CancelFunc) {
	pai := r.Context()

	// WithoutCancel preserva os VALORES do contexto (request id, ator,
	// identidade) e descarta só o cancelamento — é o que permite ao log e ao
	// filtro de escopo continuarem enxergando a requisição.
	ctx, cancelar := context.WithCancel(context.WithoutCancel(pai))
	fim := make(chan struct{})

	go func() {
		select {
		case <-fim:
		case <-pai.Done():
			if !errors.Is(pai.Err(), context.DeadlineExceeded) {
				cancelar()
			}
		}
	}()

	var uma sync.Once
	return ctx, func() {
		// `fim` fecha SEMPRE, mesmo que `pai` nunca complete: é a garantia de
		// que esta goroutine morre junto com o handler, e é ela que o teste de
		// vazamento verifica.
		uma.Do(func() { close(fim) })
		cancelar()
	}
}
