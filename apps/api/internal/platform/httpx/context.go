package httpx

import (
	"context"
	"sync/atomic"
)

type chaveRequestID struct{}
type chaveAtor struct{}
type chavePortadorDoAtor struct{}
type chaveIPDoCliente struct{}

// ComRequestID guarda o id da requisição para o log e para a resposta.
func ComRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, chaveRequestID{}, id)
}

// RequestIDDoContexto recupera o id da requisição; string vazia quando não há.
func RequestIDDoContexto(ctx context.Context) string {
	id, _ := ctx.Value(chaveRequestID{}).(string)
	return id
}

// ComAtor registra quem está autenticado, só para o log.
//
// Existe aqui, e não no pacote auth, para quebrar o ciclo de importação: o
// middleware de log é do httpx e o de autenticação é do auth, que já importa
// httpx. Guardar só o id (string) mantém o httpx ignorante do modelo de usuário.
//
// Além do valor no contexto (que só desce), grava no portador instalado pelo
// RequestLogger — ver `comPortadorDoAtor`.
func ComAtor(ctx context.Context, id string) context.Context {
	if p, ok := ctx.Value(chavePortadorDoAtor{}).(*portadorDoAtor); ok {
		p.id.Store(&id)
	}
	return context.WithValue(ctx, chaveAtor{}, id)
}

// Ator recupera o id do usuário autenticado; vazio quando anônimo.
func Ator(ctx context.Context) string {
	if id, _ := ctx.Value(chaveAtor{}).(string); id != "" {
		return id
	}
	// Fallback para o portador: é o caso do RequestLogger, que segura o contexto
	// de ANTES da autenticação e por isso nunca enxerga o valor de baixo.
	if p, ok := ctx.Value(chavePortadorDoAtor{}).(*portadorDoAtor); ok {
		if id := p.id.Load(); id != nil {
			return *id
		}
	}
	return ""
}

// comIPDoCliente guarda o IP já resolvido por RealIP. Fica no contexto, e não
// reescrevendo `r.RemoteAddr` como fazia o middleware do chi, para que o peer
// direto continue legível por quem precisar dele (diagnóstico de proxy).
func comIPDoCliente(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, chaveIPDoCliente{}, ip)
}

// IPDoContexto recupera o IP resolvido; vazio quando RealIP não rodou.
func IPDoContexto(ctx context.Context) string {
	ip, _ := ctx.Value(chaveIPDoCliente{}).(string)
	return ip
}

// portadorDoAtor é a caixa que o RequestLogger deixa no contexto ANTES da
// autenticação, para o autenticador conseguir escrever nela de baixo para cima.
//
// Por que ela precisa existir: `context.WithValue` só desce. O `auth.Middleware`
// roda DEPOIS do RequestLogger e chama `next.ServeHTTP(w, r.WithContext(ctx))`
// com um contexto NOVO — que o logger, segurando o `*http.Request` antigo, nunca
// vê. Resultado medido antes desta correção, na API real com token válido:
// TODA linha do log saía com `user_id=""`, inclusive as escritas
// (`PATCH /units/{id} status=200 user_id=""`). Pior que campo ausente: lê-se
// como "anônimo", e a investigação de um incidente começa por uma mentira.
//
// atomic.Pointer e não string nua: um handler pode disparar goroutine que
// consulta o Ator enquanto o middleware ainda escreve, e o `-race` da suíte
// pegaria a corrida.
type portadorDoAtor struct {
	id atomic.Pointer[string]
}

// comPortadorDoAtor instala a caixa. Chamado só pelo RequestLogger: quem não
// loga não precisa da indireção, e o fallback de Ator continua devolvendo o
// valor direto do contexto.
func comPortadorDoAtor(ctx context.Context) context.Context {
	return context.WithValue(ctx, chavePortadorDoAtor{}, &portadorDoAtor{})
}
