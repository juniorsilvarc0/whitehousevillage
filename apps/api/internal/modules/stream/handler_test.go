package stream

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/realtime"
)

// ─────────────────────── Um leitor de SSE de teste ──────────────────

type eventoLido struct {
	Nome       string
	ID         string
	Dados      string
	Comentario string
	Retry      string
}

func (e eventoLido) vazio() bool {
	return e.Nome == "" && e.ID == "" && e.Dados == "" && e.Comentario == "" && e.Retry == ""
}

// leitorSSE quebra o corpo da resposta em eventos. É proposital que ele NÃO use
// biblioteca: o formato do stream faz parte do contrato, e um teste que o
// interpreta com o mesmo código do servidor não provaria nada sobre o formato.
type leitorSSE struct{ eventos chan eventoLido }

func novoLeitorSSE(corpo io.Reader) *leitorSSE {
	l := &leitorSSE{eventos: make(chan eventoLido, 128)}
	go func() {
		defer close(l.eventos)
		leitor := bufio.NewReader(corpo)
		var atual eventoLido
		for {
			linha, err := leitor.ReadString('\n')
			if linha != "" {
				texto := strings.TrimRight(linha, "\r\n")
				switch {
				case texto == "":
					if !atual.vazio() {
						l.eventos <- atual
					}
					atual = eventoLido{}
				case strings.HasPrefix(texto, ":"):
					atual.Comentario = strings.TrimSpace(texto[1:])
				case strings.HasPrefix(texto, "event:"):
					atual.Nome = strings.TrimSpace(texto[len("event:"):])
				case strings.HasPrefix(texto, "id:"):
					atual.ID = strings.TrimSpace(texto[len("id:"):])
				case strings.HasPrefix(texto, "data:"):
					atual.Dados = strings.TrimSpace(texto[len("data:"):])
				case strings.HasPrefix(texto, "retry:"):
					atual.Retry = strings.TrimSpace(texto[len("retry:"):])
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return l
}

func (l *leitorSSE) proximo(t *testing.T, dentro time.Duration) eventoLido {
	t.Helper()
	select {
	case ev, ok := <-l.eventos:
		if !ok {
			t.Fatal("o stream fechou antes do evento esperado")
		}
		return ev
	case <-time.After(dentro):
		t.Fatalf("nenhum evento SSE em %s", dentro)
		return eventoLido{}
	}
}

// proximoDeDados pula batimentos: o teste que espera um evento de dado não
// pode falhar porque um `: keep-alive` chegou antes.
func (l *leitorSSE) proximoDeDados(t *testing.T, dentro time.Duration) eventoLido {
	t.Helper()
	prazo := time.Now().Add(dentro)
	for time.Now().Before(prazo) {
		ev := l.proximo(t, time.Until(prazo))
		if ev.Nome != "" {
			return ev
		}
	}
	t.Fatalf("nenhum evento nomeado em %s", dentro)
	return eventoLido{}
}

// ─────────────────────────── Montagem ───────────────────────────────

func semLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

func usuarioCom(escopos map[string]string) *auth.Usuario {
	perms := make([]auth.Permissao, 0, len(escopos))
	for recurso, escopo := range escopos {
		perms = append(perms, auth.Permissao{Resource: recurso, Action: auth.AcaoVer, Scope: escopo})
	}
	return &auth.Usuario{
		ID:         uuid.New(),
		PropertyID: uuid.New(),
		Permissoes: auth.NovoConjunto(perms),
	}
}

type ambiente struct {
	hub     *realtime.Hub
	handler *Handler
	srv     *httptest.Server
	usuario *auth.Usuario
}

// montar sobe um hub SEM banco (o Abridor devolve uma escuta que nunca recebe
// nada) e o servidor HTTP. Os eventos entram por hub.Publicar — o caminho do
// Postgres já é provado no teste de integração.
func montar(t *testing.T, u *auth.Usuario, o Opcoes, envolver ...func(http.Handler) http.Handler) *ambiente {
	t.Helper()
	return montarComServidor(t, u, o, nil, envolver...)
}

// montarComServidor deixa o teste ajustar o http.Server ANTES de ele subir.
// Mexer em `srv.Config` depois do Start é corrida com a goroutine que serve — e
// o `-race` da suíte pega, com razão.
func montarComServidor(t *testing.T, u *auth.Usuario, o Opcoes, ajustar func(*http.Server), envolver ...func(http.Handler) http.Handler) *ambiente {
	t.Helper()

	hub := realtime.NovoHub(realtime.Config{
		Abrir: func(ctx context.Context, _ []string) (realtime.Escuta, error) {
			return escutaParada{}, nil
		},
		Log: semLog(),
	})
	hub.Iniciar(context.Background())
	t.Cleanup(hub.Parar)

	o.Log = semLog()
	h := NovoHandlerCom(hub, nil, o)

	r := chi.NewRouter()
	for _, e := range envolver {
		r.Use(e)
	}
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.WithUser(req.Context(), u)))
		})
	})
	r.Get("/stream", h.Assinar)

	srv := httptest.NewUnstartedServer(r)
	if ajustar != nil {
		ajustar(srv.Config)
	}
	srv.Start()
	t.Cleanup(srv.Close)

	// A conexão precisa estar de pé antes de o teste pedir reposição por
	// cursor: hub desconectado responde resync de propósito.
	prazo := time.Now().Add(2 * time.Second)
	for !hub.Conectado() && time.Now().Before(prazo) {
		time.Sleep(time.Millisecond)
	}

	return &ambiente{hub: hub, handler: h, srv: srv, usuario: u}
}

type escutaParada struct{}

func (escutaParada) Esperar(ctx context.Context) (string, string, error) {
	<-ctx.Done()
	return "", "", ctx.Err()
}
func (escutaParada) Fechar(context.Context) error { return nil }

// abrir faz o GET e devolve a resposta com o leitor já rodando.
func (a *ambiente) abrir(t *testing.T, consulta string, cabecalhos map[string]string) (*http.Response, *leitorSSE, context.CancelFunc) {
	t.Helper()
	ctx, cancelar := context.WithCancel(context.Background())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.srv.URL+"/stream?"+consulta, nil)
	if err != nil {
		cancelar()
		t.Fatalf("montando requisição: %v", err)
	}
	for k, v := range cabecalhos {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancelar()
		t.Fatalf("GET /stream: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	// O leitor SSE só é ligado quando a resposta É um stream.
	//
	// `novoLeitorSSE` sobe uma goroutine que DRENA `resp.Body`. Quando a
	// resposta é uma recusa (422, 403, 429), o corpo é o envelope de erro em
	// JSON, e essa goroutine disputava a leitura com `corpoDeErro` — quem
	// chegasse primeiro levava os bytes. Sob carga (a suíte inteira com
	// `-race`) a goroutine ganhava e o teste morria em "lendo o erro: EOF",
	// com o status 429 JÁ conferido na linha anterior: intermitência do
	// arreio, não do produto. Reproduzido 3 em 3 injetando 50 ms de espera
	// antes de ler o corpo, e 0 em 40 depois desta guarda.
	if resp.StatusCode != http.StatusOK {
		return resp, nil, cancelar
	}
	return resp, novoLeitorSSE(resp.Body), cancelar
}

func corpoDeErro(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var envelope struct {
		Error map[string]any `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("lendo o erro: %v", err)
	}
	return envelope.Error
}

func gestao() *auth.Usuario {
	return usuarioCom(map[string]string{recursoCalendario: auth.EscopoAll, recursoCRM: auth.EscopoAll})
}

func eventoDeBloco(u *auth.Usuario) realtime.Evento {
	return realtime.Evento{
		Topico: realtime.TopicoCalendario, Entidade: "stay_block",
		ID: uuid.New(), UnitID: uuid.New(), PropertyID: u.PropertyID, V: 7,
	}
}

// ─────────────────────────── Handshake ──────────────────────────────

func TestTopicsAusenteOuDesconhecidoEh422(t *testing.T) {
	a := montar(t, gestao(), Opcoes{})

	for nome, consulta := range map[string]string{
		"ausente":        "",
		"vazio":          "topics=",
		"nome inventado": "topics=calendar,chat",
	} {
		t.Run(nome, func(t *testing.T) {
			resp, _, cancelar := a.abrir(t, consulta, nil)
			defer cancelar()
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, esperado 422", resp.StatusCode)
			}
			if e := corpoDeErro(t, resp); e["code"] != "VALIDATION_ERROR" {
				t.Fatalf("code = %v, esperado VALIDATION_ERROR", e["code"])
			}
		})
	}
}

// Conexão aberta que nunca entrega nada é o pior desfecho: o cliente espera
// para sempre e ninguém consegue distinguir isso de "está tudo parado".
func TestSemNenhumTopicoAlcancavelEh403(t *testing.T) {
	a := montar(t, usuarioCom(map[string]string{"reservations": auth.EscopoAll}), Opcoes{})

	resp, _, cancelar := a.abrir(t, "topics=calendar,crm", nil)
	defer cancelar()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403", resp.StatusCode)
	}
	if e := corpoDeErro(t, resp); e["code"] != "FORBIDDEN" {
		t.Fatalf("code = %v, esperado FORBIDDEN", e["code"])
	}
}

// Quem alcança um tópico e não o outro entra assim mesmo — e o `ready` diz o
// que sobrou, para o cliente não ficar perguntando por que o kanban não se mexe.
func TestTopicoSemPermissaoEhDescartadoEOReadyDiz(t *testing.T) {
	a := montar(t, usuarioCom(map[string]string{recursoCalendario: auth.EscopoAll}), Opcoes{})

	resp, leitor, cancelar := a.abrir(t, "topics=calendar,crm", nil)
	defer cancelar()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-cache") || !strings.Contains(cc, "no-store") {
		t.Fatalf("Cache-Control = %q; proxy com cache ligado guardaria o stream", cc)
	}
	if ab := resp.Header.Get("X-Accel-Buffering"); ab != "no" {
		t.Fatalf("X-Accel-Buffering = %q; com o buffer do proxy ligado o tempo real deixa de ser real", ab)
	}

	if r := leitor.proximo(t, 2*time.Second); r.Retry == "" {
		t.Fatalf("o primeiro quadro devia ser o retry:, veio %+v", r)
	}

	pronto := leitor.proximoDeDados(t, 2*time.Second)
	if pronto.Nome != "ready" {
		t.Fatalf("primeiro evento = %q, esperado ready", pronto.Nome)
	}
	if pronto.ID != "" {
		t.Fatal("ready não pode ter id: evento de controle não entra no replay")
	}
	var corpo struct {
		Topics []string `json:"topics"`
	}
	if err := json.Unmarshal([]byte(pronto.Dados), &corpo); err != nil {
		t.Fatalf("ready ilegível: %v", err)
	}
	if len(corpo.Topics) != 1 || corpo.Topics[0] != realtime.TopicoCalendario {
		t.Fatalf("topics do ready = %v, esperado [calendar]", corpo.Topics)
	}
}

// ─────────────────────────── Entrega ────────────────────────────────

func TestEventoChegaComEnvelopeDoContrato(t *testing.T) {
	u := gestao()
	a := montar(t, u, Opcoes{})

	_, leitor, cancelar := a.abrir(t, "topics=calendar,crm", nil)
	defer cancelar()
	if pronto := leitor.proximoDeDados(t, 2*time.Second); pronto.Nome != "ready" {
		t.Fatalf("esperava ready, veio %q", pronto.Nome)
	}

	ev := eventoDeBloco(u)
	a.hub.Publicar(ev)

	recebido := leitor.proximoDeDados(t, 2*time.Second)
	if recebido.Nome != realtime.TopicoCalendario {
		t.Fatalf("event: = %q, esperado calendar", recebido.Nome)
	}
	if recebido.ID == "" {
		t.Fatal("evento de dado precisa de id: — é ele que volta em Last-Event-ID")
	}

	var dados eventoDoStream
	if err := json.Unmarshal([]byte(recebido.Dados), &dados); err != nil {
		t.Fatalf("data ilegível: %v", err)
	}
	if dados.Entity != "stay_block" || dados.ID != ev.ID || dados.V != 7 {
		t.Fatalf("envelope = %+v", dados)
	}
	if dados.UnitID == nil || *dados.UnitID != ev.UnitID {
		t.Fatal("stay_block precisa de unit_id: é o que deixa o mapa repintar UMA coluna")
	}

	// O envelope é magro por decisão de segurança: nada além do contrato pode
	// atravessar o barramento, porque o pg_notify não tem RBAC.
	var cru map[string]any
	if err := json.Unmarshal([]byte(recebido.Dados), &cru); err != nil {
		t.Fatalf("data ilegível: %v", err)
	}
	for chave := range cru {
		switch chave {
		case "entity", "id", "unit_id", "v":
		default:
			t.Fatalf("campo %q vazou no data: do stream; o contrato só declara entity, id, unit_id e v", chave)
		}
	}
}

func TestQuemNaoAssinouOTopicoNaoRecebe(t *testing.T) {
	u := gestao()
	a := montar(t, u, Opcoes{})

	_, leitor, cancelar := a.abrir(t, "topics=calendar", nil)
	defer cancelar()
	leitor.proximoDeDados(t, 2*time.Second) // ready

	a.hub.Publicar(realtime.Evento{
		Topico: realtime.TopicoCRM, Entidade: "opportunity",
		ID: uuid.New(), PropertyID: u.PropertyID, V: 1,
	})
	bloco := eventoDeBloco(u)
	a.hub.Publicar(bloco)

	recebido := leitor.proximoDeDados(t, 2*time.Second)
	if recebido.Nome != realtime.TopicoCalendario {
		t.Fatalf("chegou %q; o assinante de calendar não pode ver movimento de CRM", recebido.Nome)
	}
}

// O flush por evento é o que separa tempo real de relatório: sem ele o
// bufio do net/http segura os bytes até encher 4 kB.
func TestCadaEventoSaiNaHoraSemEsperarEncherOBuffer(t *testing.T) {
	u := gestao()
	a := montar(t, u, Opcoes{})

	_, leitor, cancelar := a.abrir(t, "topics=calendar", nil)
	defer cancelar()
	leitor.proximoDeDados(t, 2*time.Second)

	// Um evento tem ~150 bytes. Se dependesse do buffer, o primeiro só sairia
	// depois de umas 25 publicações.
	inicio := time.Now()
	a.hub.Publicar(eventoDeBloco(u))
	leitor.proximoDeDados(t, 2*time.Second)

	if decorrido := time.Since(inicio); decorrido > time.Second {
		t.Fatalf("o primeiro evento levou %s para chegar", decorrido)
	}
}

func TestBatimentoMantemAConexaoViva(t *testing.T) {
	a := montar(t, gestao(), Opcoes{Batimento: 30 * time.Millisecond})

	_, leitor, cancelar := a.abrir(t, "topics=calendar", nil)
	defer cancelar()
	leitor.proximoDeDados(t, 2*time.Second)

	// Sem batimento, um proxy com ocioso de 60 s corta a conexão e o painel
	// reconecta em laço.
	for i := range 2 {
		ev := leitor.proximo(t, 2*time.Second)
		if ev.Comentario == "" {
			t.Fatalf("batimento %d: esperava comentário SSE, veio %+v", i, ev)
		}
	}
}

// ─────────────────────── Reconexão e cursor ─────────────────────────

func TestLastEventIDRepoeOQuePassou(t *testing.T) {
	u := gestao()
	a := montar(t, u, Opcoes{})

	_, leitor, cancelar := a.abrir(t, "topics=calendar", nil)
	leitor.proximoDeDados(t, 2*time.Second)

	a.hub.Publicar(eventoDeBloco(u))
	primeiro := leitor.proximoDeDados(t, 2*time.Second)
	cancelar()

	// Enquanto o cliente esteve fora, o mundo andou.
	perdido := eventoDeBloco(u)
	a.hub.Publicar(perdido)

	_, segundoLeitor, cancelar2 := a.abrir(t, "topics=calendar", map[string]string{"Last-Event-ID": primeiro.ID})
	defer cancelar2()

	if pronto := segundoLeitor.proximoDeDados(t, 2*time.Second); pronto.Nome != "ready" {
		t.Fatalf("esperava ready, veio %q", pronto.Nome)
	}
	reposto := segundoLeitor.proximoDeDados(t, 2*time.Second)
	if reposto.Nome != realtime.TopicoCalendario {
		t.Fatalf("esperava a reposição do evento perdido, veio %q", reposto.Nome)
	}
	var dados eventoDoStream
	if err := json.Unmarshal([]byte(reposto.Dados), &dados); err != nil {
		t.Fatalf("data ilegível: %v", err)
	}
	if dados.ID != perdido.ID {
		t.Fatalf("repôs o evento errado: %v", dados.ID)
	}
}

func TestCursorVelhoOuIlegivelViraResyncENaoSilencio(t *testing.T) {
	a := montar(t, gestao(), Opcoes{})

	for nome, cursor := range map[string]string{
		"velho demais": "1",
		"ilegível":     "isso-não-é-cursor",
	} {
		t.Run(nome, func(t *testing.T) {
			_, leitor, cancelar := a.abrir(t, "topics=calendar", map[string]string{"Last-Event-ID": cursor})
			defer cancelar()

			if pronto := leitor.proximoDeDados(t, 2*time.Second); pronto.Nome != "ready" {
				t.Fatalf("esperava ready, veio %q", pronto.Nome)
			}
			resync := leitor.proximoDeDados(t, 2*time.Second)
			if resync.Nome != "resync" {
				t.Fatalf("esperava resync, veio %q", resync.Nome)
			}
			if resync.ID != "" {
				t.Fatal("resync não pode ter id: ele voltaria no replay de uma reconexão futura")
			}
		})
	}
}

// ─────────────────────── Token que vence no meio ────────────────────

// tokenComExp devolve um Bearer de mentira com o `exp` pedido.
//
// A assinatura é lixo de propósito: neste ponto da cadeia o `auth.Autenticador`
// já rodou, e o handler só decodifica o `exp` para agendar o aviso e o
// desligamento — reverificar aqui exigiria um segundo validador de sessão.
//
// `daqui` é arredondado para o segundo ANTES de somar porque `exp` é um inteiro
// em segundos: sem isso, o token pediria 2,0 s e o servidor leria entre 1,0 s e
// 2,0 s, e o teste ficaria flaky pela fração de segundo em que rodou.
func tokenComExp(daqui time.Duration) (string, time.Duration) {
	exp := time.Now().Truncate(time.Second).Add(daqui + time.Second)
	corpo := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"sub":"x","exp":%d}`, exp.Unix())))
	return "cabecalho." + corpo + ".assinatura", time.Until(exp)
}

// Não dá para responder 401 depois do 200. O contrato manda avisar e desligar.
func TestTokenVencendoAvisaEDepoisDesligaOStream(t *testing.T) {
	token, vida := tokenComExp(time.Second)
	a := montar(t, gestao(), Opcoes{
		Batimento:        time.Hour, // fora do caminho
		AvisoDeExpiracao: vida / 2,
	})

	_, leitor, cancelar := a.abrir(t, "topics=calendar", map[string]string{
		"Authorization": "Bearer " + token,
	})
	defer cancelar()

	if pronto := leitor.proximoDeDados(t, 2*time.Second); pronto.Nome != "ready" {
		t.Fatalf("esperava ready, veio %q", pronto.Nome)
	}

	aviso := leitor.proximoDeDados(t, 3*time.Second)
	if aviso.Nome != "expiring" {
		t.Fatalf("esperava expiring, veio %q", aviso.Nome)
	}
	if !strings.Contains(aviso.Dados, "expires_at") {
		t.Fatalf("o expiring precisa dizer quando vence, para o cliente renovar antes: %s", aviso.Dados)
	}

	fim := leitor.proximoDeDados(t, 3*time.Second)
	if fim.Nome != "expired" {
		t.Fatalf("esperava expired, veio %q", fim.Nome)
	}
	// E o stream tem de FECHAR — de forma limpa, sem erro de transporte.
	select {
	case _, aberto := <-leitor.eventos:
		if aberto {
			t.Fatal("o servidor continuou mandando evento depois do expired")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("o stream não encerrou depois do expired")
	}
}

// Token ilegível (ou ausente, como no caminho do cookie pelo BFF) não pode
// significar conexão eterna.
func TestSemExpLegivelAConexaoTemTetoDeVida(t *testing.T) {
	a := montar(t, gestao(), Opcoes{
		Batimento:        time.Hour,
		VidaMaximaSemExp: 400 * time.Millisecond,
		AvisoDeExpiracao: 200 * time.Millisecond,
	})

	_, leitor, cancelar := a.abrir(t, "topics=calendar", nil)
	defer cancelar()
	leitor.proximoDeDados(t, 2*time.Second)

	if aviso := leitor.proximoDeDados(t, 3*time.Second); aviso.Nome != "expiring" {
		t.Fatalf("esperava expiring, veio %q", aviso.Nome)
	}
	if fim := leitor.proximoDeDados(t, 3*time.Second); fim.Nome != "expired" {
		t.Fatalf("esperava expired, veio %q", fim.Nome)
	}
}

// ─────────────────────── Higiene da conexão ─────────────────────────

// O teste que o relatório pediu: a goroutine morre quando o cliente some.
func TestGoroutinesMorremQuandoOClienteDesconecta(t *testing.T) {
	u := gestao()
	a := montar(t, u, Opcoes{Batimento: 20 * time.Millisecond})

	base := goroutinesEstaveis(t)

	const conexoes = 4
	var cancelamentos []context.CancelFunc
	for range conexoes {
		_, leitor, cancelar := a.abrir(t, "topics=calendar", nil)
		cancelamentos = append(cancelamentos, cancelar)
		leitor.proximoDeDados(t, 2*time.Second)
	}

	// A prova de que as conexões estão de fato vivas antes de medir.
	a.hub.Publicar(eventoDeBloco(u))

	comConexoes := runtime.NumGoroutine()
	if comConexoes <= base {
		t.Fatalf("as conexões não criaram goroutine nenhuma (%d → %d): o teste não estaria medindo nada", base, comConexoes)
	}

	for _, cancelar := range cancelamentos {
		cancelar()
	}

	depois := goroutinesEstaveis(t)
	// Folga de 2: o pool de conexões ociosas do http.Transport do teste também
	// mantém goroutines, e elas não são nossas.
	if depois > base+2 {
		t.Fatalf("goroutines: %d antes, %d com %d conexões, %d depois — sobrou gente viva",
			base, comConexoes, conexoes, depois)
	}
}

// goroutinesEstaveis espera o número parar de mudar. Ler NumGoroutine de uma vez
// só mede o escalonador, não o vazamento.
func goroutinesEstaveis(t *testing.T) int {
	t.Helper()
	anterior := -1
	estaveis := 0
	prazo := time.Now().Add(5 * time.Second)
	for time.Now().Before(prazo) {
		runtime.Gosched()
		time.Sleep(25 * time.Millisecond)
		atual := runtime.NumGoroutine()
		if atual == anterior {
			if estaveis++; estaveis >= 4 {
				return atual
			}
			continue
		}
		anterior, estaveis = atual, 0
	}
	t.Fatal("o número de goroutines não estabilizou")
	return 0
}

// Teto de conexões por usuário: uma aba em laço de reconexão não pode consumir
// o processo sozinha.
func TestLimiteDeConexoesPorUsuarioResponde429(t *testing.T) {
	u := gestao()

	hub := realtime.NovoHub(realtime.Config{
		Abrir:          func(context.Context, []string) (realtime.Escuta, error) { return escutaParada{}, nil },
		Log:            semLog(),
		MaximoPorChave: 1,
	})
	hub.Iniciar(context.Background())
	t.Cleanup(hub.Parar)

	h := NovoHandlerCom(hub, nil, Opcoes{Log: semLog()})
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.WithUser(req.Context(), u)))
		})
	})
	r.Get("/stream", h.Assinar)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	a := &ambiente{hub: hub, handler: h, srv: srv, usuario: u}

	_, leitor, cancelar := a.abrir(t, "topics=calendar", nil)
	defer cancelar()
	leitor.proximoDeDados(t, 2*time.Second)

	resp, leitor2, cancelar2 := a.abrir(t, "topics=calendar", nil)
	defer cancelar2()
	// A recusa NÃO pode vir com leitor SSE ligado: a goroutine dele drenaria o
	// envelope de erro antes de `corpoDeErro` e o teste morreria em EOF com o
	// produto certo. A política da casa proíbe `sleep`, então a guarda é esta
	// asserção estrutural, e não uma espera adversária.
	if leitor2 != nil {
		t.Fatal("a segunda conexão foi recusada e mesmo assim ganhou leitor SSE: " +
			"o corpo do erro será drenado por baixo de quem for lê-lo")
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status da segunda conexão = %d, esperado 429", resp.StatusCode)
	}
	if e := corpoDeErro(t, resp); e["code"] != "RATE_LIMITED" {
		t.Fatalf("code = %v, esperado RATE_LIMITED", e["code"])
	}
}

// A cadeia do router monta middleware.Timeout(50s). Um stream que morre no teto
// de uma requisição curta não é tempo real.
func TestStreamSobreviveAoTetoDeTempoDeRequisicao(t *testing.T) {
	u := gestao()
	a := montar(t, u, Opcoes{Batimento: 25 * time.Millisecond}, middleware.Timeout(150*time.Millisecond))

	_, leitor, cancelar := a.abrir(t, "topics=calendar", nil)
	defer cancelar()
	leitor.proximoDeDados(t, 2*time.Second)

	// Bem depois do teto da requisição.
	time.Sleep(400 * time.Millisecond)

	ev := eventoDeBloco(u)
	a.hub.Publicar(ev)

	recebido := leitor.proximoDeDados(t, 2*time.Second)
	if recebido.Nome != realtime.TopicoCalendario {
		t.Fatalf("depois do teto de 150 ms o stream devia continuar entregando; veio %q", recebido.Nome)
	}
	var dados eventoDoStream
	if err := json.Unmarshal([]byte(recebido.Dados), &dados); err != nil || dados.ID != ev.ID {
		t.Fatalf("evento errado depois do teto: %s", recebido.Dados)
	}
}

// Handler com receptor nulo é o main que esqueceu de preencher Deps.Stream. O
// 503 com texto é muito melhor que o pânico convertido em 500 sem explicação.
func TestHandlerNaoMontadoResponde503(t *testing.T) {
	var h *Handler
	req := httptest.NewRequest(http.MethodGet, "/stream?topics=calendar", nil)
	w := httptest.NewRecorder()
	h.Assinar(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, esperado 503", w.Code)
	}
}

// ─────────────────── Prazo de escrita e WriteTimeout ────────────────

// respostaEmbrulhadaComUnwrap é o `respostaObservada` do httpx.RequestLogger
// COM o `Unwrap() http.ResponseWriter` que ele ainda não tem.
//
// Sem esse método, `http.NewResponseController` não alcança o writer de baixo e
// `SetWriteDeadline` devolve "feature not supported" — e a conexão SSE passa a
// morrer no `WriteTimeout` do http.Server (60 s no cmd/api), todas as vezes.
// Este teste é a prova de que o lado de cá está certo: dado um embrulho que se
// deixa desembrulhar, a conexão atravessa o prazo do servidor.
type respostaEmbrulhadaComUnwrap struct{ http.ResponseWriter }

func (w respostaEmbrulhadaComUnwrap) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w respostaEmbrulhadaComUnwrap) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func TestConexaoAtravessaOWriteTimeoutDoServidor(t *testing.T) {
	u := gestao()
	embrulhar := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(respostaEmbrulhadaComUnwrap{w}, r)
		})
	}

	a := montarComServidor(t, u, Opcoes{Batimento: 50 * time.Millisecond},
		func(srv *http.Server) { srv.WriteTimeout = 300 * time.Millisecond }, embrulhar)

	_, leitor, cancelar := a.abrir(t, "topics=calendar", nil)
	defer cancelar()
	leitor.proximoDeDados(t, 2*time.Second)

	// Bem depois do prazo de escrita da resposta inteira. Drenar é obrigatório:
	// sem isso o teste leria um batimento ANTERIOR ao prazo e concluiria o
	// contrário do que aconteceu.
	fim := time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(fim) {
		select {
		case _, aberto := <-leitor.eventos:
			if !aberto {
				t.Fatal("o stream fechou no WriteTimeout do servidor: o prazo de escrita não está sendo renovado")
			}
		case <-time.After(50 * time.Millisecond):
		}
	}

	ev := eventoDeBloco(u)
	a.hub.Publicar(ev)
	recebido := leitor.proximoDeDados(t, 2*time.Second)
	if recebido.Nome != realtime.TopicoCalendario {
		t.Fatalf("depois do WriteTimeout o stream devia seguir entregando; veio %q", recebido.Nome)
	}
}

// Quando o prazo NÃO pode ser renovado (embrulho sem Unwrap), a conexão continua
// funcionando — só passa a depender do WriteTimeout do servidor. O que não pode
// acontecer é isso ser silencioso, nem a tentativa se repetir a cada escrita.
func TestPrazoIndisponivelEhDetectadoUmaVezSoENaoDerrubaAEscrita(t *testing.T) {
	gravador := httptest.NewRecorder()
	e, err := novoEscritor(gravador, semLog())
	if err != nil {
		t.Fatalf("novoEscritor: %v", err)
	}

	if err := e.comentario("keep-alive"); err != nil {
		t.Fatalf("a escrita não pode falhar só porque o prazo não pôde ser renovado: %v", err)
	}
	if !e.semPrazo {
		t.Skip("este ResponseWriter aceitou SetWriteDeadline; nada a verificar aqui")
	}
	if err := e.comentario("keep-alive"); err != nil {
		t.Fatalf("segunda escrita: %v", err)
	}
	if got := strings.Count(gravador.Body.String(), ": keep-alive"); got != 2 {
		t.Fatalf("escreveu %d batimentos, esperado 2", got)
	}
}
