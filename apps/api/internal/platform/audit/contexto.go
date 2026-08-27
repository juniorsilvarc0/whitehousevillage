package audit

import (
	"context"
	"net/http"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

type chaveOrigem struct{}

// Origem é de onde a escrita veio: as duas colunas de `audit_log` que o
// contexto ainda não carregava.
//
// Por que aqui e não em `internal/platform/httpx`, que já guarda request_id e
// ator: o IP e o user-agent só interessam à auditoria, e httpx tem dono
// diferente. Chave própria neste pacote mantém a fronteira de pasta intacta e
// deixa o middleware ser plugado sem alterar o middleware de log.
type Origem struct {
	IP        string
	UserAgent string
}

// ComOrigem injeta IP e user-agent no contexto. Usado pelo Middleware e pelos
// testes; um worker que age em nome de uma requisição anterior pode chamá-la
// para propagar a origem original.
func ComOrigem(ctx context.Context, o Origem) context.Context {
	return context.WithValue(ctx, chaveOrigem{}, o)
}

// OrigemDoContexto recupera a origem; zero quando a chamada não veio de HTTP
// (job, seed) — e aí as duas colunas ficam nulas, que é a verdade.
func OrigemDoContexto(ctx context.Context) Origem {
	o, _ := ctx.Value(chaveOrigem{}).(Origem)
	return o
}

// Middleware captura IP e user-agent da requisição para a trilha.
//
// Roda cedo na cadeia, logo depois do RealIP/RequestID do httpx: é o único
// lugar onde o `*http.Request` ainda existe. Daqui para baixo o service e o
// repositório só veem `context.Context`, e é por isso que quem audita não
// precisa (nem consegue) receber o request como parâmetro — foi assim que a
// assinatura de Registrar ficou com quatro campos de origem que ninguém digita.
//
// O IP vem de httpx.IPDoCliente, que confia no que o RealIP do chi normalizou a
// partir do proxy. Em produção só o proxy fala com a API.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o := Origem{
			IP:        httpx.IPDoCliente(r),
			UserAgent: truncar(r.UserAgent(), tamanhoMaximoUserAgent),
		}
		next.ServeHTTP(w, r.WithContext(ComOrigem(r.Context(), o)))
	})
}
