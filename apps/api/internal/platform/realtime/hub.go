package realtime

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

// Erros que quem assina precisa distinguir para responder o HTTP certo.
var (
	// ErrSemTopicos — assinatura sem nenhum tópico. Conexão que nunca entrega
	// nada é o pior desfecho possível: o cliente espera para sempre.
	ErrSemTopicos = errors.New("realtime: assinatura sem tópicos")

	// ErrLimitePorChave — o mesmo usuário abriu conexões demais.
	ErrLimitePorChave = errors.New("realtime: limite de conexões por usuário atingido")

	// ErrLimiteGlobal — o processo atingiu o teto de conexões.
	ErrLimiteGlobal = errors.New("realtime: limite global de conexões atingido")
)

// Escuta é a conexão dedicada de `LISTEN`. É interface, e não `*pgx.Conn`, por
// um motivo só: o teste que prova a reconexão precisa DERRUBAR a conexão à
// vontade. Sem essa costura, "o hub sobrevive à queda" só seria verificável
// matando um container no meio da suíte — ou seja, não seria verificado.
type Escuta interface {
	// Esperar bloqueia até chegar notificação. Erro devolvido = conexão morta:
	// o hub fecha e reabre.
	Esperar(ctx context.Context) (canal, payload string, err error)
	Fechar(ctx context.Context) error
}

// Abridor abre uma Escuta já com os canais registrados.
type Abridor func(ctx context.Context, canais []string) (Escuta, error)

// Config é o que o hub precisa de fora. Zero-value dos campos numéricos e de
// duração cai nos padrões de Normalizar.
type Config struct {
	Abrir  Abridor
	Canais []string

	// JanelaDeReplay é quanto tempo o buffer guarda evento para repor a quem
	// reconecta com Last-Event-ID. Cursor mais velho que isto recebe resync —
	// nunca silêncio.
	JanelaDeReplay time.Duration

	// EventosNoBuffer é o teto absoluto do buffer, em linhas. A janela sozinha
	// não limita memória: um job de expiração em massa cabe inteiro em 5 min.
	EventosNoBuffer int

	// CapacidadePorAssinante é o fôlego de cada conexão SSE antes de ela ser
	// considerada lenta. Estourar não derruba ninguém: marca resync.
	CapacidadePorAssinante int

	// MaximoPorChave é o teto de conexões do MESMO usuário. Uma aba em laço de
	// reconexão é o caso comum, e sem teto ela consome o processo sozinha.
	MaximoPorChave int

	// MaximoGlobal é o teto do processo inteiro.
	MaximoGlobal int

	// Backoff da reconexão do LISTEN.
	EsperaMinima time.Duration
	EsperaMaxima time.Duration

	Log *slog.Logger
}

// Padrões. Os números vêm do contrato (5 min de replay) e do tamanho desta
// operação: uma casa, oito unidades, uma dezena de pessoas na gestão.
const (
	janelaPadrao         = 5 * time.Minute
	eventosNoBufferPadao = 4096
	capacidadePadrao     = 64
	maximoPorChavePadrao = 5
	maximoGlobalPadrao   = 500
	esperaMinimaPadrao   = 250 * time.Millisecond
	esperaMaximaPadrao   = 15 * time.Second
)

func (c Config) normalizar() Config {
	if c.Canais == nil {
		c.Canais = Canais
	}
	if c.JanelaDeReplay <= 0 {
		c.JanelaDeReplay = janelaPadrao
	}
	if c.EventosNoBuffer <= 0 {
		c.EventosNoBuffer = eventosNoBufferPadao
	}
	if c.CapacidadePorAssinante <= 0 {
		c.CapacidadePorAssinante = capacidadePadrao
	}
	if c.MaximoPorChave <= 0 {
		c.MaximoPorChave = maximoPorChavePadrao
	}
	if c.MaximoGlobal <= 0 {
		c.MaximoGlobal = maximoGlobalPadrao
	}
	if c.EsperaMinima <= 0 {
		c.EsperaMinima = esperaMinimaPadrao
	}
	if c.EsperaMaxima < c.EsperaMinima {
		c.EsperaMaxima = esperaMaximaPadrao
	}
	if c.Log == nil {
		c.Log = slog.Default()
	}
	return c
}

// Aviso é o que chega na conexão SSE.
//
// Resync não é um evento de dado: é o barramento admitindo que perdeu o fio
// (buffer estourado, `LISTEN` caído e reaberto, assinante lento demais). O
// cliente responde refazendo o fetch da faixa visível. Fingir que nada se perdeu
// é como o mapa de ocupação fica errado sem ninguém perceber.
type Aviso struct {
	Resync bool
	Evento Evento
}

// Reposicao é o que a assinatura devolve para um cliente que chegou com
// Last-Event-ID.
type Reposicao struct {
	Eventos []Evento
	Resync  bool
}

type registro struct {
	ev Evento
	em time.Time
}

// Hub mantém um LISTEN e faz fan-out em memória.
type Hub struct {
	cfg Config

	mu         sync.Mutex
	assinantes map[int64]*Assinatura
	porChave   map[string]int
	proximoID  int64

	cursor int64
	// baseValida é o menor Last-Event-ID que o buffer ainda consegue repor por
	// inteiro. Abaixo dele a resposta honesta é resync.
	baseValida int64
	buffer     []registro

	conectado atomic.Bool

	iniciar sync.Once
	cancela context.CancelFunc
	parou   chan struct{}
}

// NovoHub monta o hub. Não abre conexão nenhuma: quem faz isso é Iniciar.
func NovoHub(cfg Config) *Hub {
	c := cfg.normalizar()

	// O cursor NÃO começa em zero, e sim no relógio em microssegundos.
	//
	// O motivo é a reinicialização do processo: com um contador zerado a cada
	// deploy, o navegador reconectaria com um Last-Event-ID GRANDE (do processo
	// anterior) contra um cursor pequeno (do processo novo), e o hub não teria
	// como distinguir "esse cursor é do futuro" de "esse cursor é meu". Ele
	// reporia zero eventos e o cliente ficaria com a tela congelada achando que
	// estava em dia. Semeado pelo relógio, todo cursor de processo anterior cai
	// ABAIXO da base válida deste — e a resposta vira resync, que é a verdade.
	inicial := time.Now().UnixMicro()

	return &Hub{
		cfg:        c,
		assinantes: map[int64]*Assinatura{},
		porChave:   map[string]int{},
		cursor:     inicial,
		baseValida: inicial,
		parou:      make(chan struct{}),
	}
}

// Iniciar sobe a goroutine do LISTEN. Idempotente: chamar duas vezes não abre
// duas conexões.
func (h *Hub) Iniciar(ctx context.Context) {
	h.iniciar.Do(func() {
		ctx, cancela := context.WithCancel(ctx)
		h.cancela = cancela
		go h.escutar(ctx)
	})
}

// Parar encerra a escuta e espera a goroutine morrer. Sem a espera, o teste com
// `-race` não conseguiria provar que a goroutine morreu — e goroutine que
// "provavelmente" morreu é vazamento não diagnosticado.
func (h *Hub) Parar() {
	h.iniciar.Do(func() {}) // um hub nunca iniciado não fica preso aqui
	if h.cancela != nil {
		h.cancela()
		<-h.parou
	}
}

// Conectado diz se o LISTEN está de pé agora.
func (h *Hub) Conectado() bool { return h.conectado.Load() }

// ─────────────────────────── A escuta ───────────────────────────────

func (h *Hub) escutar(ctx context.Context) {
	defer close(h.parou)

	espera := h.cfg.EsperaMinima
	primeira := true

	for {
		if ctx.Err() != nil {
			return
		}

		esc, err := h.cfg.Abrir(ctx, h.cfg.Canais)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			h.cfg.Log.Warn("tempo real: LISTEN não abriu", "err", err, "proxima_tentativa", espera)
			if !dormir(ctx, espera) {
				return
			}
			espera = proximaEspera(espera, h.cfg.EsperaMaxima)
			continue
		}

		// A espera só volta ao mínimo depois de uma conexão que ABRIU. Zerar o
		// backoff a cada tentativa transformaria banco fora do ar em laço
		// apertado de reconexão — o pior momento possível para martelar o
		// Postgres é justamente quando ele está sufocando.
		espera = h.cfg.EsperaMinima
		if !primeira {
			// Reconexão: tudo o que aconteceu enquanto estivemos fora ficou
			// para trás. Quem está com a tela aberta precisa saber.
			h.marcarLacuna()
		}
		primeira = false

		h.conectado.Store(true)
		h.cfg.Log.Info("tempo real: LISTEN aberto", "canais", h.cfg.Canais)

		erroDaEscuta := h.consumir(ctx, esc)

		h.conectado.Store(false)
		_ = esc.Fechar(context.WithoutCancel(ctx))

		if ctx.Err() != nil {
			return
		}
		h.cfg.Log.Warn("tempo real: LISTEN caiu; reabrindo", "err", erroDaEscuta, "espera", espera)
		if !dormir(ctx, espera) {
			return
		}
		espera = proximaEspera(espera, h.cfg.EsperaMaxima)
	}
}

// consumir roda até a conexão morrer. Devolve o erro que a matou.
func (h *Hub) consumir(ctx context.Context, esc Escuta) error {
	for {
		canal, bruto, err := esc.Esperar(ctx)
		if err != nil {
			return err
		}
		ev, err := Decodificar(canal, bruto)
		if err != nil {
			// Descartar e seguir: payload de uma migration futura não pode
			// desligar o tempo real de quem está com a tela aberta.
			h.cfg.Log.Warn("tempo real: notificação descartada", "err", err)
			continue
		}
		h.Publicar(ev)
	}
}

// Publicar injeta um evento no barramento. Exportada porque é o ponto de entrada
// do teste — e porque o dia em que algo além do Postgres precisar publicar, o
// caminho já existe e é o mesmo.
func (h *Hub) Publicar(ev Evento) {
	agora := time.Now()

	h.mu.Lock()
	defer h.mu.Unlock()

	h.cursor++
	ev.Cursor = h.cursor
	h.guardar(registro{ev: ev, em: agora})

	for _, a := range h.assinantes {
		if !a.topicos[ev.Topico] {
			continue
		}
		a.entregar(Aviso{Evento: ev})
	}
}

// guardar acrescenta ao buffer e poda o que saiu da janela. Chamado com o lock.
func (h *Hub) guardar(r registro) {
	h.buffer = append(h.buffer, r)

	corte := r.em.Add(-h.cfg.JanelaDeReplay)
	descartados := 0
	for descartados < len(h.buffer) && h.buffer[descartados].em.Before(corte) {
		descartados++
	}
	if excesso := len(h.buffer) - descartados - h.cfg.EventosNoBuffer; excesso > 0 {
		descartados += excesso
	}
	if descartados == 0 {
		return
	}
	// A base válida passa a ser o cursor do ÚLTIMO evento descartado: um cliente
	// que parou exatamente nele ainda pode ser reposto por inteiro; qualquer
	// coisa antes, não.
	h.baseValida = h.buffer[descartados-1].ev.Cursor
	h.buffer = append(h.buffer[:0], h.buffer[descartados:]...)
}

// marcarLacuna registra que o barramento perdeu eventos: esvazia o buffer, move
// a base para o presente e avisa quem está conectado.
func (h *Hub) marcarLacuna() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.buffer = h.buffer[:0]
	h.cursor++
	h.baseValida = h.cursor
	for _, a := range h.assinantes {
		a.entregar(Aviso{Resync: true})
	}
}

// ─────────────────────────── Assinatura ─────────────────────────────

// Assinatura é a ponta do fan-out: um canal por conexão SSE.
type Assinatura struct {
	hub     *Hub
	id      int64
	chave   string
	topicos map[string]bool
	ch      chan Aviso

	// resync é escrito pelo fan-out (sob o lock do hub) e lido pela conexão
	// SSE (fora dele). Atômico porque o `-race` da suíte pegaria a corrida — e
	// pegaria com razão.
	resync atomic.Bool

	encerrar sync.Once
}

// Avisos é o canal de leitura da conexão. Fecha quando a assinatura é cancelada.
func (a *Assinatura) Avisos() <-chan Aviso { return a.ch }

// Topicos devolve os tópicos efetivamente assinados, em ordem estável.
func (a *Assinatura) Topicos() []string {
	out := make([]string, 0, len(a.topicos))
	for _, t := range []string{TopicoCalendario, TopicoCRM} {
		if a.topicos[t] {
			out = append(out, t)
		}
	}
	return out
}

// ConsumirResync devolve (e zera) a marca de "você perdeu evento".
//
// Existe porque o aviso de resync de um assinante LENTO não cabe no canal dele
// — é justamente o canal cheio que produz a marca. A conexão SSE lê esta marca
// no batimento, então mesmo a conexão que parou de receber evento nenhum
// descobre, em no máximo um batimento, que precisa refazer o fetch.
func (a *Assinatura) ConsumirResync() bool { return a.resync.Swap(false) }

// entregar é não-bloqueante de propósito: com o lock do hub na mão, uma escrita
// bloqueante numa conexão travada pararia o barramento INTEIRO. Canal cheio vira
// marca de resync — o assinante lento perde o detalhe e recebe a ordem de
// recarregar, que é a degradação correta.
func (a *Assinatura) entregar(av Aviso) {
	if av.Resync {
		// A marca vale por si: é ela que a conexão lê no batimento, e é o único
		// caminho que sobrevive ao canal cheio. O envio abaixo é só para o
		// aviso chegar NA HORA quando há fôlego, em vez de esperar o batimento.
		a.resync.Store(true)
	}
	select {
	case a.ch <- av:
	default:
		a.resync.Store(true)
	}
}

// Cancelar solta a assinatura. Idempotente: o handler chama no defer e o teste
// chama de novo sem medo.
func (a *Assinatura) Cancelar() {
	a.encerrar.Do(func() {
		h := a.hub
		h.mu.Lock()
		defer h.mu.Unlock()

		delete(h.assinantes, a.id)
		if n := h.porChave[a.chave] - 1; n > 0 {
			h.porChave[a.chave] = n
		} else {
			delete(h.porChave, a.chave)
		}
		close(a.ch)
	})
}

// Assinar registra uma conexão e devolve, na MESMA seção crítica, o que ela
// precisa repor.
//
// A atomicidade é o ponto: registrar primeiro e consultar o buffer depois
// duplicaria os eventos do intervalo; consultar antes e registrar depois os
// perderia. Um evento perdido no mapa de ocupação é uma unidade que aparece
// livre depois de vendida.
//
// `desde` zero significa "conexão nova, sem cursor" — o cliente vai fazer o
// fetch inicial de qualquer jeito.
func (h *Hub) Assinar(chave string, topicos []string, desde int64) (*Assinatura, Reposicao, error) {
	assinados := map[string]bool{}
	for _, t := range topicos {
		if TopicoConhecido(t) {
			assinados[t] = true
		}
	}
	if len(assinados) == 0 {
		return nil, Reposicao{}, ErrSemTopicos
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.assinantes) >= h.cfg.MaximoGlobal {
		return nil, Reposicao{}, ErrLimiteGlobal
	}
	if h.porChave[chave] >= h.cfg.MaximoPorChave {
		return nil, Reposicao{}, ErrLimitePorChave
	}

	h.proximoID++
	a := &Assinatura{
		hub:     h,
		id:      h.proximoID,
		chave:   chave,
		topicos: assinados,
		ch:      make(chan Aviso, h.cfg.CapacidadePorAssinante),
	}
	h.assinantes[a.id] = a
	h.porChave[chave]++

	return a, h.reposicaoDe(desde, assinados), nil
}

// reposicaoDe monta o replay. Chamada com o lock.
func (h *Hub) reposicaoDe(desde int64, topicos map[string]bool) Reposicao {
	if desde <= 0 {
		return Reposicao{}
	}
	// Fora do que o buffer alcança — velho demais, ou vindo de outro processo
	// (cursor do futuro). Nos dois casos a resposta honesta é a mesma.
	if desde < h.baseValida || desde > h.cursor {
		return Reposicao{Resync: true}
	}
	// O LISTEN está fora do ar AGORA: não há como prometer continuidade a quem
	// está reconectando. Dizer isso é mais barato que deixar a tela mentir.
	if !h.conectado.Load() {
		return Reposicao{Resync: true}
	}

	var out []Evento
	for _, r := range h.buffer {
		if r.ev.Cursor > desde && topicos[r.ev.Topico] {
			out = append(out, r.ev)
		}
	}
	return Reposicao{Eventos: out}
}

// ─────────────────────────── Auxiliares ─────────────────────────────

// dormir espera d, ou desiste se o contexto morrer. Devolve false quando não há
// próxima tentativa.
func dormir(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// proximaEspera dobra com jitter. O jitter não é enfeite: N instâncias da API
// que perderam o banco ao mesmo tempo voltariam em lockstep e derrubariam o
// Postgres de novo no instante em que ele subisse.
func proximaEspera(atual, teto time.Duration) time.Duration {
	proxima := atual * 2
	if proxima > teto {
		proxima = teto
	}
	return proxima/2 + time.Duration(rand.N(int64(proxima/2)+1))
}
