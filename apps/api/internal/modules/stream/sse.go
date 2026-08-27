package stream

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// prazoDeUmaEscrita é quanto tempo uma escrita pode ficar presa antes de a
// conexão ser considerada morta.
//
// Ele tem DUAS funções, e a segunda é a que faz o SSE existir neste servidor:
//
//  1. cliente travado (aba congelada, celular que perdeu o sinal) não segura a
//     goroutine para sempre;
//  2. `http.Server{WriteTimeout: 60s}` do cmd/api arma um prazo de escrita para
//     a resposta INTEIRA quando a requisição começa — e a resposta de um SSE só
//     termina quando alguém desliga. Sem redefinir o prazo a cada escrita, toda
//     conexão de tempo real morreria aos 60 s, todas as vezes, com o cliente
//     reconectando em laço. Redefinir aqui EMPURRA o prazo para frente a cada
//     evento e a cada batimento.
const prazoDeUmaEscrita = 15 * time.Second

// escritor formata SSE e garante o flush por evento.
//
// Flush por evento não é ajuste fino: sem ele o `bufio.Writer` do net/http
// segura os bytes até encher 4 kB, e um evento de ~200 bytes ficaria parado até
// o vigésimo. Tempo real com vinte eventos de atraso é um relatório.
type escritor struct {
	w  http.ResponseWriter
	f  http.Flusher
	rc *http.ResponseController
	// semPrazo lembra que o ResponseController não alcança o writer desta
	// cadeia de middlewares — ver `avisarSemPrazo`. Uma vez descoberto, não se
	// tenta de novo a cada escrita.
	semPrazo bool
	log      *slog.Logger
}

func novoEscritor(w http.ResponseWriter, log *slog.Logger) (*escritor, error) {
	f, ok := w.(http.Flusher)
	if !ok {
		// Sem Flusher não há tempo real possível: a resposta sairia inteira no
		// fim. Falhar aqui, antes do 200, é a única chance de dizer isso ao
		// cliente com um status.
		return nil, errors.New("o ResponseWriter desta cadeia não implementa http.Flusher")
	}
	return &escritor{w: w, f: f, rc: http.NewResponseController(w), log: log}, nil
}

// cabecalhos escreve o 200 e abre o stream.
func (e *escritor) cabecalhos() {
	h := e.w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	// `no-cache` e `no-store` juntos: o primeiro é o que navegador e CDN antigos
	// entendem, o segundo é o que o contrato registra no cabeçalho da resposta.
	h.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	// Sem isto o nginx/Traefik com buffer ligado segura os eventos e entrega
	// tudo junto, no fim — o tempo real deixa de ser real e ninguém descobre em
	// desenvolvimento, onde não há proxy.
	h.Set("X-Accel-Buffering", "no")
	// A resposta varia por token: cache compartilhado que ignorasse isso
	// entregaria o stream de um usuário para outro.
	h.Add("Vary", "Authorization")

	e.w.WriteHeader(http.StatusOK)
	e.f.Flush()
}

// retry instrui o EventSource sobre a espera de reconexão.
func (e *escritor) retry(d time.Duration) error {
	return e.enviar(fmt.Sprintf("retry: %d\n\n", d.Milliseconds()))
}

// comentario é o batimento. Linha iniciada por ':' é comentário SSE: o cliente
// ignora, o proxy vê tráfego e não mata a conexão ociosa.
func (e *escritor) comentario(texto string) error {
	return e.enviar(": " + texto + "\n\n")
}

// evento escreve um evento nomeado. `cursor` zero omite o `id:` — é o que
// distingue os eventos de CONTROLE (ready, resync, expiring, expired), que não
// entram no replay, dos eventos de DADO.
func (e *escritor) evento(nome string, cursor int64, dados any) error {
	corpo, err := json.Marshal(dados)
	if err != nil {
		return fmt.Errorf("serializando evento %s: %w", nome, err)
	}

	var b []byte
	b = append(b, "event: "...)
	b = append(b, nome...)
	b = append(b, '\n')
	if cursor > 0 {
		b = append(b, "id: "...)
		b = strconv.AppendInt(b, cursor, 10)
		b = append(b, '\n')
	}
	b = append(b, "data: "...)
	b = append(b, corpo...)
	b = append(b, '\n', '\n')

	return e.enviar(string(b))
}

func (e *escritor) enviar(texto string) error {
	e.renovarPrazo()
	if _, err := e.w.Write([]byte(texto)); err != nil {
		return err
	}
	// O Flush precisa vir pelo http.Flusher e não pelo ResponseController: o
	// `respostaObservada` do httpx.RequestLogger repassa Flush mas não expõe
	// Unwrap, então o controller não o enxerga. Ver avisarSemPrazo.
	e.f.Flush()
	return nil
}

func (e *escritor) renovarPrazo() {
	if e.semPrazo {
		return
	}
	if err := e.rc.SetWriteDeadline(time.Now().Add(prazoDeUmaEscrita)); err != nil {
		e.semPrazo = true
		e.avisarSemPrazo(err)
	}
}

// avisarSemPrazo registra, UMA vez por conexão, que o prazo de escrita não pôde
// ser redefinido.
//
// Acontece quando algum middleware embrulha o ResponseWriter sem oferecer
// `Unwrap() http.ResponseWriter` — é o caso do `respostaObservada` do
// `httpx.RequestLogger` hoje. A consequência é concreta e vale estar no log com
// todas as letras: a conexão passa a morrer no `WriteTimeout` do servidor
// (60 s), e o cliente reconecta em laço a cada minuto.
func (e *escritor) avisarSemPrazo(err error) {
	e.log.Warn("tempo real: prazo de escrita não pôde ser redefinido; a conexão vai morrer no WriteTimeout do servidor",
		"err", err,
		"correcao", "httpx.respostaObservada precisa de Unwrap() http.ResponseWriter")
}
