package realtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ─────────────────────── Um Postgres de mentira ─────────────────────
//
// O hub existe para sobreviver à queda do `LISTEN`. Provar isso com um Postgres
// de verdade exigiria matar um container no meio da suíte — caro, lento e
// intermitente. A interface Escuta existe exatamente para que a queda seja um
// `chan error` que o teste escreve quando quer.

type notificacao struct{ canal, payload string }

type sessaoFalsa struct {
	notificacoes chan notificacao
	morte        chan error
	fechada      chan struct{}
	umaVez       sync.Once
}

func novaSessao() *sessaoFalsa {
	return &sessaoFalsa{
		notificacoes: make(chan notificacao, 16),
		morte:        make(chan error, 1),
		fechada:      make(chan struct{}),
	}
}

func (s *sessaoFalsa) Esperar(ctx context.Context) (string, string, error) {
	select {
	case <-ctx.Done():
		return "", "", ctx.Err()
	case err := <-s.morte:
		return "", "", err
	case n := <-s.notificacoes:
		return n.canal, n.payload, nil
	}
}

func (s *sessaoFalsa) Fechar(context.Context) error {
	s.umaVez.Do(func() { close(s.fechada) })
	return nil
}

type bancoFalso struct {
	abertas chan *sessaoFalsa

	mu             sync.Mutex
	tentativas     int
	falharAte      int // as N primeiras tentativas de abrir falham
	instantes      []time.Time
	erroDeAbertura error
}

func novoBancoFalso() *bancoFalso {
	return &bancoFalso{abertas: make(chan *sessaoFalsa, 8), erroDeAbertura: errors.New("banco fora do ar")}
}

func (b *bancoFalso) Abrir(context.Context, []string) (Escuta, error) {
	b.mu.Lock()
	b.tentativas++
	n := b.tentativas
	b.instantes = append(b.instantes, time.Now())
	falhar := n <= b.falharAte
	err := b.erroDeAbertura
	b.mu.Unlock()

	if falhar {
		return nil, err
	}
	s := novaSessao()
	b.abertas <- s
	return s, nil
}

func (b *bancoFalso) contagem() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tentativas
}

// ─────────────────────────── Auxiliares ─────────────────────────────

func silencioso() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

func hubDeTeste(t *testing.T, banco *bancoFalso, ajustar func(*Config)) *Hub {
	t.Helper()
	cfg := Config{
		Abrir:        banco.Abrir,
		Log:          silencioso(),
		EsperaMinima: time.Millisecond,
		EsperaMaxima: 4 * time.Millisecond,
	}
	if ajustar != nil {
		ajustar(&cfg)
	}
	h := NovoHub(cfg)
	h.Iniciar(context.Background())
	t.Cleanup(h.Parar)
	return h
}

func payloadDeBloco(id, unidade uuid.UUID, v int64) string {
	return fmt.Sprintf(`{"topic":"calendar","entity":"stay_block","id":%q,"unit_id":%q,"property_id":null,"v":%d}`,
		id, unidade, v)
}

func payloadDeOportunidade(id uuid.UUID, v int64) string {
	return fmt.Sprintf(`{"topic":"crm","entity":"opportunity","id":%q,"pipeline_id":null,"property_id":null,"v":%d}`, id, v)
}

func receber(t *testing.T, a *Assinatura, dentro time.Duration) Aviso {
	t.Helper()
	select {
	case av, ok := <-a.Avisos():
		if !ok {
			t.Fatal("canal da assinatura fechou antes do esperado")
		}
		return av
	case <-time.After(dentro):
		t.Fatalf("nenhum aviso em %s", dentro)
		return Aviso{}
	}
}

func esperarSessao(t *testing.T, b *bancoFalso, dentro time.Duration) *sessaoFalsa {
	t.Helper()
	select {
	case s := <-b.abertas:
		return s
	case <-time.After(dentro):
		t.Fatalf("o hub não abriu conexão em %s", dentro)
		return nil
	}
}

func aguardarConexao(t *testing.T, h *Hub) {
	t.Helper()
	prazo := time.Now().Add(2 * time.Second)
	for time.Now().Before(prazo) {
		if h.Conectado() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("o hub não reportou conexão")
}

// ─────────────────────────── Os testes ──────────────────────────────

func TestAssinanteRecebeSoOsTopicosQueAssinou(t *testing.T) {
	banco := novoBancoFalso()
	h := hubDeTeste(t, banco, nil)
	sessao := esperarSessao(t, banco, time.Second)
	aguardarConexao(t, h)

	so, _, err := h.Assinar("usuario", []string{TopicoCalendario}, 0)
	if err != nil {
		t.Fatalf("Assinar: %v", err)
	}
	defer so.Cancelar()

	// O evento de CRM tem de ser descartado para este assinante. Não é
	// economia de banda: o `id` de uma oportunidade alheia já é informação.
	sessao.notificacoes <- notificacao{CanalCRM, payloadDeOportunidade(uuid.New(), 1)}

	bloco := uuid.New()
	sessao.notificacoes <- notificacao{CanalCalendario, payloadDeBloco(bloco, uuid.New(), 7)}

	av := receber(t, so, 2*time.Second)
	if av.Resync {
		t.Fatalf("resync inesperado")
	}
	if av.Evento.ID != bloco {
		t.Fatalf("chegou o evento errado: %+v", av.Evento)
	}
	if av.Evento.Topico != TopicoCalendario || av.Evento.Entidade != "stay_block" || av.Evento.V != 7 {
		t.Fatalf("envelope errado: %+v", av.Evento)
	}
	select {
	case extra := <-so.Avisos():
		t.Fatalf("assinante de calendar recebeu evento a mais: %+v", extra)
	case <-time.After(50 * time.Millisecond):
	}
}

// O teste que o relatório pediu: o hub tem de sobreviver à queda da conexão.
func TestHubReabreOListenDepoisDaQuedaEAvisaQuemPerdeuEvento(t *testing.T) {
	banco := novoBancoFalso()
	h := hubDeTeste(t, banco, nil)

	primeira := esperarSessao(t, banco, time.Second)
	aguardarConexao(t, h)

	a, _, err := h.Assinar("usuario", []string{TopicoCalendario}, 0)
	if err != nil {
		t.Fatalf("Assinar: %v", err)
	}
	defer a.Cancelar()

	antes := uuid.New()
	primeira.notificacoes <- notificacao{CanalCalendario, payloadDeBloco(antes, uuid.New(), 1)}
	if av := receber(t, a, 2*time.Second); av.Evento.ID != antes {
		t.Fatalf("evento antes da queda: %+v", av)
	}

	// A queda.
	primeira.morte <- errors.New("conexão com o Postgres caiu")

	segunda := esperarSessao(t, banco, 3*time.Second)
	select {
	case <-primeira.fechada:
	case <-time.After(2 * time.Second):
		t.Fatal("a conexão morta não foi fechada — vazamento de conexão de banco a cada queda")
	}
	aguardarConexao(t, h)

	// Quem estava conectado precisa SABER que houve buraco. Silêncio aqui é o
	// mapa de ocupação ficando errado sem ninguém perceber.
	if av := receber(t, a, 2*time.Second); !av.Resync {
		t.Fatalf("esperava resync depois da reconexão, veio %+v", av)
	}

	depois := uuid.New()
	segunda.notificacoes <- notificacao{CanalCalendario, payloadDeBloco(depois, uuid.New(), 2)}
	if av := receber(t, a, 2*time.Second); av.Evento.ID != depois {
		t.Fatalf("o hub não voltou a entregar depois da queda: %+v", av)
	}
}

// Banco fora do ar não pode virar laço apertado de reconexão: o pior momento
// para martelar o Postgres é justamente quando ele está sufocando.
func TestReconexaoUsaBackoffEmVezDeLacoApertado(t *testing.T) {
	banco := novoBancoFalso()
	banco.falharAte = 1000 // nunca abre
	hubDeTeste(t, banco, func(c *Config) {
		c.EsperaMinima = 20 * time.Millisecond
		c.EsperaMaxima = 200 * time.Millisecond
	})

	time.Sleep(300 * time.Millisecond)

	// Sem backoff seriam milhares de tentativas em 300 ms. Com o dobrar a
	// partir de 20 ms, a soma das esperas passa dos 300 ms por volta da quinta.
	if n := banco.contagem(); n == 0 || n > 12 {
		t.Fatalf("tentativas de reconexão em 300 ms = %d; esperado entre 1 e 12 (backoff)", n)
	}
}

// Assinante lento não pode parar o barramento inteiro: o fan-out roda com o
// lock do hub na mão.
func TestAssinanteLentoRecebeResyncEmVezDeSegurarOHub(t *testing.T) {
	banco := novoBancoFalso()
	h := hubDeTeste(t, banco, func(c *Config) { c.CapacidadePorAssinante = 2 })
	esperarSessao(t, banco, time.Second)
	aguardarConexao(t, h)

	lento, _, err := h.Assinar("lento", []string{TopicoCalendario}, 0)
	if err != nil {
		t.Fatalf("Assinar: %v", err)
	}
	defer lento.Cancelar()

	rapido, _, err := h.Assinar("rapido", []string{TopicoCalendario}, 0)
	if err != nil {
		t.Fatalf("Assinar: %v", err)
	}
	defer rapido.Cancelar()

	// Ninguém drena o "lento". Publicar mais que a capacidade dele não pode
	// bloquear — se bloqueasse, este teste travaria aqui.
	pronto := make(chan struct{})
	go func() {
		defer close(pronto)
		for i := range 20 {
			h.Publicar(Evento{Topico: TopicoCalendario, Entidade: "stay_block", ID: uuid.New(), V: int64(i)})
		}
	}()
	select {
	case <-pronto:
	case <-time.After(2 * time.Second):
		t.Fatal("o fan-out travou num assinante lento: o barramento inteiro parou")
	}

	if !lento.ConsumirResync() {
		t.Fatal("o assinante lento perdeu evento sem ser avisado — a tela dele ficaria velha em silêncio")
	}
	// E o rápido, que drena, não pode ter sido punido pelo lento.
	if n := len(rapido.Avisos()); n == 0 {
		t.Fatal("o assinante rápido não recebeu nada")
	}
}

func TestReposicaoPorCursor(t *testing.T) {
	banco := novoBancoFalso()
	h := hubDeTeste(t, banco, nil)
	esperarSessao(t, banco, time.Second)
	aguardarConexao(t, h)

	// Uma conexão qualquer só para os eventos entrarem no buffer.
	inicial, _, err := h.Assinar("usuario", []string{TopicoCalendario, TopicoCRM}, 0)
	if err != nil {
		t.Fatalf("Assinar: %v", err)
	}

	var cursores []int64
	for i := range 3 {
		h.Publicar(Evento{Topico: TopicoCalendario, Entidade: "stay_block", ID: uuid.New(), V: int64(i)})
		cursores = append(cursores, receber(t, inicial, time.Second).Evento.Cursor)
	}
	h.Publicar(Evento{Topico: TopicoCRM, Entidade: "opportunity", ID: uuid.New(), V: 99})
	receber(t, inicial, time.Second)
	inicial.Cancelar()

	t.Run("repõe o que passou desde o cursor", func(t *testing.T) {
		a, rep, err := h.Assinar("usuario", []string{TopicoCalendario}, cursores[0])
		if err != nil {
			t.Fatalf("Assinar: %v", err)
		}
		defer a.Cancelar()
		if rep.Resync {
			t.Fatal("resync indevido: o cursor está dentro do buffer")
		}
		if len(rep.Eventos) != 2 {
			t.Fatalf("repostos %d eventos, esperado 2 (os dois de calendar depois do cursor)", len(rep.Eventos))
		}
		for _, ev := range rep.Eventos {
			if ev.Topico != TopicoCalendario {
				t.Fatalf("a reposição vazou um tópico não assinado: %+v", ev)
			}
		}
	})

	t.Run("cursor velho demais vira resync, nunca silêncio", func(t *testing.T) {
		a, rep, err := h.Assinar("usuario", []string{TopicoCalendario}, 1)
		if err != nil {
			t.Fatalf("Assinar: %v", err)
		}
		defer a.Cancelar()
		if !rep.Resync || len(rep.Eventos) != 0 {
			t.Fatalf("cursor abaixo do buffer devia dar resync; veio %+v", rep)
		}
	})

	// O caso que o cursor semeado pelo relógio existe para resolver: o cliente
	// volta com o Last-Event-ID de um processo ANTERIOR.
	t.Run("cursor de outro processo vira resync", func(t *testing.T) {
		a, rep, err := h.Assinar("usuario", []string{TopicoCalendario}, cursores[2]+1_000_000)
		if err != nil {
			t.Fatalf("Assinar: %v", err)
		}
		defer a.Cancelar()
		if !rep.Resync {
			t.Fatal("cursor do futuro devia dar resync: repor zero eventos deixaria a tela congelada achando que está em dia")
		}
	})

	t.Run("sem cursor não repõe nada", func(t *testing.T) {
		a, rep, err := h.Assinar("usuario", []string{TopicoCalendario}, 0)
		if err != nil {
			t.Fatalf("Assinar: %v", err)
		}
		defer a.Cancelar()
		if rep.Resync || len(rep.Eventos) != 0 {
			t.Fatalf("conexão nova não repõe: %+v", rep)
		}
	})
}

func TestBufferDescartaOQueSaiuDaJanelaEAvisaOsAtrasados(t *testing.T) {
	banco := novoBancoFalso()
	h := hubDeTeste(t, banco, func(c *Config) { c.EventosNoBuffer = 3 })
	esperarSessao(t, banco, time.Second)
	aguardarConexao(t, h)

	primeiro := h.cursorAtualParaTeste() + 1
	for range 10 {
		h.Publicar(Evento{Topico: TopicoCalendario, Entidade: "stay_block", ID: uuid.New(), V: 1})
	}

	a, rep, err := h.Assinar("usuario", []string{TopicoCalendario}, primeiro)
	if err != nil {
		t.Fatalf("Assinar: %v", err)
	}
	defer a.Cancelar()
	if !rep.Resync {
		t.Fatalf("cursor além do teto do buffer devia dar resync; veio %d eventos", len(rep.Eventos))
	}
}

func TestLimitesDeConexao(t *testing.T) {
	banco := novoBancoFalso()
	h := hubDeTeste(t, banco, func(c *Config) {
		c.MaximoPorChave = 2
		c.MaximoGlobal = 3
	})
	esperarSessao(t, banco, time.Second)
	aguardarConexao(t, h)

	var abertas []*Assinatura
	defer func() {
		for _, a := range abertas {
			a.Cancelar()
		}
	}()

	for range 2 {
		a, _, err := h.Assinar("ana", []string{TopicoCalendario}, 0)
		if err != nil {
			t.Fatalf("Assinar: %v", err)
		}
		abertas = append(abertas, a)
	}
	// A aba em laço de reconexão é o caso comum, e sem teto ela consome o
	// processo sozinha.
	if _, _, err := h.Assinar("ana", []string{TopicoCalendario}, 0); !errors.Is(err, ErrLimitePorChave) {
		t.Fatalf("terceira conexão da mesma pessoa: err = %v, esperado ErrLimitePorChave", err)
	}

	a, _, err := h.Assinar("bruno", []string{TopicoCalendario}, 0)
	if err != nil {
		t.Fatalf("Assinar bruno: %v", err)
	}
	abertas = append(abertas, a)

	if _, _, err := h.Assinar("carla", []string{TopicoCalendario}, 0); !errors.Is(err, ErrLimiteGlobal) {
		t.Fatalf("quarta conexão do processo: err = %v, esperado ErrLimiteGlobal", err)
	}

	// Cancelar devolve a vaga — senão o teto viraria uma contagem só de subida
	// e a API pararia de servir tempo real depois de um dia no ar.
	abertas[0].Cancelar()
	abertas = abertas[1:]
	if _, _, err := h.Assinar("carla", []string{TopicoCalendario}, 0); err != nil {
		t.Fatalf("depois de cancelar, a vaga devia ter voltado: %v", err)
	}
}

func TestAssinaturaSemTopicoConhecidoEhRecusada(t *testing.T) {
	banco := novoBancoFalso()
	h := hubDeTeste(t, banco, nil)
	esperarSessao(t, banco, time.Second)

	if _, _, err := h.Assinar("usuario", []string{"chat"}, 0); !errors.Is(err, ErrSemTopicos) {
		t.Fatalf("err = %v, esperado ErrSemTopicos", err)
	}
}

func TestCancelarEhIdempotenteEFechaOCanal(t *testing.T) {
	banco := novoBancoFalso()
	h := hubDeTeste(t, banco, nil)
	esperarSessao(t, banco, time.Second)

	a, _, err := h.Assinar("usuario", []string{TopicoCalendario}, 0)
	if err != nil {
		t.Fatalf("Assinar: %v", err)
	}
	a.Cancelar()
	a.Cancelar() // o handler cancela no defer; o teste, de novo. Nenhum pânico.

	if _, aberto := <-a.Avisos(); aberto {
		t.Fatal("o canal devia estar fechado depois do Cancelar")
	}
}

// Payload que o hub não entende é descartado — nunca derruba a escuta. Uma
// migration futura que acrescente um campo não pode desligar o tempo real de
// quem está com a tela aberta.
func TestPayloadIlegivelNaoDerrubaAEscuta(t *testing.T) {
	banco := novoBancoFalso()
	h := hubDeTeste(t, banco, nil)
	sessao := esperarSessao(t, banco, time.Second)
	aguardarConexao(t, h)

	a, _, err := h.Assinar("usuario", []string{TopicoCalendario}, 0)
	if err != nil {
		t.Fatalf("Assinar: %v", err)
	}
	defer a.Cancelar()

	sessao.notificacoes <- notificacao{CanalCalendario, "{isso não é json"}
	sessao.notificacoes <- notificacao{CanalCalendario, `{"entity":"marciano","id":"x","v":1}`}

	bom := uuid.New()
	sessao.notificacoes <- notificacao{CanalCalendario, payloadDeBloco(bom, uuid.New(), 3)}

	if av := receber(t, a, 2*time.Second); av.Evento.ID != bom {
		t.Fatalf("depois do payload ilegível o hub devia seguir entregando; veio %+v", av)
	}
	if banco.contagem() != 1 {
		t.Fatalf("a escuta reabriu %d vezes por causa de payload ruim — devia ser 0 reaberturas", banco.contagem()-1)
	}
}

func TestPararMataAGoroutineDaEscuta(t *testing.T) {
	banco := novoBancoFalso()
	cfg := Config{Abrir: banco.Abrir, Log: silencioso(), EsperaMinima: time.Millisecond}
	h := NovoHub(cfg)
	h.Iniciar(context.Background())
	esperarSessao(t, banco, time.Second)
	aguardarConexao(t, h)

	pronto := make(chan struct{})
	go func() { h.Parar(); close(pronto) }()
	select {
	case <-pronto:
	case <-time.After(2 * time.Second):
		t.Fatal("Parar não voltou: a goroutine da escuta ficou de pé")
	}
	if h.Conectado() {
		t.Fatal("o hub continua se dizendo conectado depois do Parar")
	}
}

func TestPararSemIniciarNaoTrava(t *testing.T) {
	h := NovoHub(Config{Abrir: novoBancoFalso().Abrir, Log: silencioso()})
	pronto := make(chan struct{})
	go func() { h.Parar(); close(pronto) }()
	select {
	case <-pronto:
	case <-time.After(time.Second):
		t.Fatal("Parar travou num hub que nunca foi iniciado")
	}
}

func TestDecodificar(t *testing.T) {
	id := uuid.New()
	unidade := uuid.New()
	casa := uuid.New()

	t.Run("bloco de estadia", func(t *testing.T) {
		ev, err := Decodificar(CanalCalendario, fmt.Sprintf(
			`{"topic":"calendar","entity":"stay_block","id":%q,"unit_id":%q,"property_id":%q,"v":853}`, id, unidade, casa))
		if err != nil {
			t.Fatalf("Decodificar: %v", err)
		}
		if ev.Topico != TopicoCalendario || ev.Entidade != "stay_block" ||
			ev.ID != id || ev.UnitID != unidade || ev.PropertyID != casa || ev.V != 853 {
			t.Fatalf("evento errado: %+v", ev)
		}
	})

	t.Run("reserva não tem chave de escopo", func(t *testing.T) {
		ev, err := Decodificar(CanalCalendario, fmt.Sprintf(
			`{"topic":"calendar","entity":"reservation","id":%q,"property_id":%q,"v":853}`, id, casa))
		if err != nil {
			t.Fatalf("Decodificar: %v", err)
		}
		if ev.UnitID != uuid.Nil {
			t.Fatalf("reserva não devia trazer unit_id: %+v", ev)
		}
	})

	// O canal é a autoridade: é nele que este processo deu LISTEN, e é ele que
	// nenhum erro de digitação numa migration futura consegue forjar.
	t.Run("topic divergente do canal é recusado", func(t *testing.T) {
		_, err := Decodificar(CanalCalendario, fmt.Sprintf(
			`{"topic":"crm","entity":"opportunity","id":%q,"v":1}`, id))
		if err == nil || !strings.Contains(err.Error(), "o canal manda") {
			t.Fatalf("err = %v, esperado recusa por divergência de tópico", err)
		}
	})

	for nome, bruto := range map[string]string{
		"json quebrado":           `{`,
		"entidade fora do tópico": fmt.Sprintf(`{"topic":"calendar","entity":"opportunity","id":%q,"v":1}`, id),
		"id não é uuid":           `{"topic":"calendar","entity":"stay_block","id":"nada","v":1}`,
		"unit_id não é uuid":      fmt.Sprintf(`{"topic":"calendar","entity":"stay_block","id":%q,"unit_id":"nada","v":1}`, id),
	} {
		t.Run(nome, func(t *testing.T) {
			if _, err := Decodificar(CanalCalendario, bruto); err == nil {
				t.Fatal("devia ter recusado")
			}
		})
	}

	t.Run("canal desconhecido", func(t *testing.T) {
		if _, err := Decodificar("whv_qualquer", `{}`); err == nil {
			t.Fatal("devia ter recusado")
		}
	})
}

// cursorAtualParaTeste devolve o cursor corrente. Mora num arquivo de teste de
// propósito: cursor é detalhe do envelope SSE, e quem consome o hub em produção
// não tem nada que ler o contador por fora.
func (h *Hub) cursorAtualParaTeste() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cursor
}
