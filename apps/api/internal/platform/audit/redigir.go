package audit

import "strings"

// Redigido é o que fica gravado no lugar do valor sensível.
//
// Marca fixa, e NÃO o campo omitido: o auditor precisa continuar vendo que
// `password_hash` mudou (foi trocada a senha) sem que o hash saia da tabela.
// Omitir o campo apagaria o fato; gravar o valor entregaria o segredo a quem
// tiver leitura em `audit_log` — que é gente demais, e é justamente o público
// da tela de auditoria.
const Redigido = "[redigido]"

// termosSensiveis é a lista de SUBSTRINGS, comparadas sem acento de maiúscula,
// contra o NOME do campo.
//
// Por que por nome e não por valor: heurística sobre o valor (parece um hash?
// parece um JWT?) erra nos dois sentidos e falha em silêncio. O nome do campo é
// escolhido por quem escreve o DTO e é estável.
//
// Por que substring e não igualdade: os nomes reais no repositório são
// `password_hash`, `token_hash`, `senha_atual`, `refresh_token`. Uma lista de
// igualdade exata teria de prever cada composição e ficaria desatualizada no
// primeiro campo novo — falhando aberta, que é o modo de falha errado para
// filtro de segredo.
//
// Consequência aceita: campos inocentes cujo nome contém um destes termos são
// redigidos por engano (o clássico é `hash` dentro de `hashtag`). Perder um
// campo da trilha é reversível — renomeia-se o campo ou acrescenta-se exceção;
// vazar um token não é.
var termosSensiveis = []string{
	"senha",
	"password",
	"passwd",
	"hash",
	"token",
	"secret",
	"segredo",
	"api_key",
	"apikey",
	"authorization",
	"bearer",
	"cookie",
	"credential",
	"credencial",
	"private_key",
	"salt",
	"otp",
	"cvv",
}

// profundidadeMaxima limita a descida em documentos aninhados.
//
// Existe por duas razões: um JSON vindo de fora (payload de webhook guardado
// como "antes") pode ser fundo o bastante para estourar a pilha, e um ciclo
// construído à mão em Campos (mapa que aponta para si) faria a recursão não
// terminar. No limite o galho inteiro vira Redigido — o modo de falha seguro é
// esconder demais, nunca de menos.
const profundidadeMaxima = 12

// Sensivel diz se o nome do campo cai no filtro. Exportada porque quem monta
// Campos à mão pode querer verificar antes; a decisão que VALE, porém, é a de
// Registrar, que redige sempre.
func Sensivel(nome string) bool {
	n := strings.ToLower(nome)
	for _, termo := range termosSensiveis {
		if strings.Contains(n, termo) {
			return true
		}
	}
	return false
}

// Redigir devolve uma CÓPIA do documento com todo campo sensível substituído.
//
// Cópia, e não alteração no lugar, porque o mapa que chega pode ser a struct que
// o service ainda vai usar depois de auditar — mutá-lo faria a auditoria
// corromper o negócio.
func Redigir(c Campos) Campos {
	if len(c) == 0 {
		return nil
	}
	return redigirMapa(c, 0)
}

func redigirMapa(c Campos, nivel int) Campos {
	if nivel > profundidadeMaxima {
		return Campos{"_": Redigido}
	}
	out := make(Campos, len(c))
	for chave, valor := range c {
		if Sensivel(chave) {
			out[chave] = Redigido
			continue
		}
		out[chave] = redigirValor(valor, nivel+1)
	}
	return out
}

// redigirValor desce em mapas e listas porque o campo sensível costuma estar um
// nível abaixo: `{"credenciais": {"token": "..."}}` passaria intacto por um
// filtro que só olhasse o topo.
func redigirValor(v any, nivel int) any {
	if nivel > profundidadeMaxima {
		return Redigido
	}
	switch t := v.(type) {
	case Campos:
		return redigirMapa(t, nivel)
	case map[string]any:
		return redigirMapa(Campos(t), nivel)
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = redigirValor(item, nivel+1)
		}
		return out
	default:
		return v
	}
}
