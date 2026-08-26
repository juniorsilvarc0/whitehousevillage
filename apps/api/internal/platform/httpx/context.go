package httpx

import "context"

type chaveRequestID struct{}
type chaveAtor struct{}

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
func ComAtor(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, chaveAtor{}, id)
}

// Ator recupera o id do usuário autenticado; vazio quando anônimo.
func Ator(ctx context.Context) string {
	id, _ := ctx.Value(chaveAtor{}).(string)
	return id
}
