package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

// tamanhoMaximoCorpo limita o corpo aceito. Sem teto, um POST de 2 GB derruba o
// processo antes de qualquer validação rodar.
const tamanhoMaximoCorpo = 1 << 20 // 1 MiB

// Validador é o gancho para a validação que as tags não expressam: campos Opt
// (o validator não enxerga dentro do genérico), combinações entre campos e
// regras que dependem de mais de um valor. Devolve campo → mensagem.
type Validador interface {
	Validar() map[string]string
}

var validate = sync.OnceValue(func() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	// Sem isso os details sairiam com o nome do campo em Go ("RoleID"); o front
	// precisa do nome que ele mandou ("role_id").
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		nome := strings.SplitN(f.Tag.Get("json"), ",", 2)[0]
		if nome == "" || nome == "-" {
			return f.Name
		}
		return nome
	})
	return v
})

// Decode lê o corpo JSON, valida e devolve o DTO tipado. Qualquer falha vira
// apperr.Validation com details no formato {campo: mensagem} em português — é o
// contrato que o painel consome para grudar o erro no input certo.
//
// Campo que o DTO não declara é RECUSADO (`DisallowUnknownFields`), conforme o
// `info.description` da OpenAPI. Sem isso, todo erro de digitação do cliente
// vira sucesso silencioso: medido nesta árvore, `PATCH /units/{id}`
// {"ativa":false,"xpto":1} respondia 200 com `active` intacto, e
// `PATCH /reservations/{id}` {"guests":6} respondia 200 com `guests_count`
// intacto. O par 200-sem-efeito é a pior resposta possível — não há erro em
// lugar nenhum para alguém investigar.
func Decode[T any](r *http.Request) (T, error) {
	var alvo T

	if r.Body == nil {
		return alvo, apperr.Validation(map[string]string{"body": "corpo da requisição é obrigatório."})
	}

	// O corpo é lido inteiro (até 1 MiB) porque a recusa de chave com outra
	// caixa precisa das chaves como chegaram — o decoder já as perdeu.
	bruto, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, tamanhoMaximoCorpo))
	if err != nil {
		return alvo, erroDeJSON(err)
	}
	dec := json.NewDecoder(bytes.NewReader(bruto))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&alvo); err != nil {
		return alvo, erroDeJSON(err)
	}
	// Corpo com mais de um documento JSON é cliente com bug; aceitar em silêncio
	// esconde o problema.
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return alvo, apperr.Validation(map[string]string{"body": "corpo deve conter um único documento JSON."})
	}
	if err := recusarChaveComOutraCaixa[T](bruto); err != nil {
		return alvo, err
	}

	return alvo, Validar(alvo)
}

// DecodeOpcional aceita corpo vazio e devolve o zero value — é o caso de
// /auth/refresh e /auth/logout, onde o token normalmente vem no cookie.
func DecodeOpcional[T any](r *http.Request) (T, error) {
	var alvo T
	if r.Body == nil || r.ContentLength == 0 {
		return alvo, nil
	}
	bruto, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, tamanhoMaximoCorpo))
	if err != nil {
		return alvo, erroDeJSON(err)
	}
	dec := json.NewDecoder(bytes.NewReader(bruto))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&alvo); err != nil {
		if errors.Is(err, io.EOF) {
			return alvo, nil
		}
		return alvo, erroDeJSON(err)
	}
	if err := recusarChaveComOutraCaixa[T](bruto); err != nil {
		return alvo, err
	}
	return alvo, Validar(alvo)
}

// ─────────────── Chave com outra caixa não é o campo do contrato ───────────────
//
// O `encoding/json` casa a chave com o campo SEM olhar maiúscula/minúscula, e o
// `DisallowUnknownFields` herda a mesma tolerância: `counted_qty` escrita toda
// em maiúsculas e `{"Note": "x"}` respondiam 200 e GRAVAVAM, contra o
// `additionalProperties: false` do contrato (medido pelo QA nas rotas de bens,
// 07/10/2026). É o mesmo
// silêncio que o DisallowUnknownFields existe para acabar — um cliente com a
// chave "quase certa" passa, e no dia em que o servidor ganhar um campo de
// nome parecido ninguém sabe qual dos dois chegou.
//
// A regra: toda chave do objeto de TOPO tem de ser, byte a byte, o nome JSON de
// um campo do DTO. O nome é o da tag `json` ou, sem tag, o do campo Go — como
// o encoding/json faz; struct embutida sem nome na tag contribui com os campos
// dela (são promovidos); `json:"-"` e campo não exportado não contam. DTO cujo
// topo não é struct (a matriz de permissões é uma lista) ou que decodifica a si
// mesmo (`json.Unmarshaler`) fica fora: não há "chave de topo" a conferir.

var (
	nomesExatosPorTipo  sync.Map // reflect.Type → map[string]bool (nil: fora da regra)
	tipoUnmarshalerJSON = reflect.TypeFor[json.Unmarshaler]()
)

// recusarChaveComOutraCaixa roda DEPOIS do decode bem-sucedido: corpo
// malformado, tipo errado e nome inexistente já saíram com a mensagem deles.
// O que sobra aqui é só a chave que o decoder aceitou por casar sem caixa.
func recusarChaveComOutraCaixa[T any](bruto []byte) error {
	nomes := nomesExatos(reflect.TypeFor[T]())
	if nomes == nil {
		return nil
	}
	var chaves map[string]json.RawMessage
	if err := json.Unmarshal(bruto, &chaves); err != nil {
		// Não é objeto (o `null` de um corpo opcional, por exemplo): o decode
		// já decidiu, e não há chave a conferir.
		return nil
	}
	detalhes := map[string]string{}
	for chave := range chaves {
		if nomes[chave] {
			continue
		}
		msg := "campo desconhecido no corpo da requisição."
		for nome := range nomes {
			if strings.EqualFold(nome, chave) {
				msg = fmt.Sprintf("campo desconhecido no corpo da requisição — os nomes diferenciam maiúsculas: o campo é %q.", nome)
				break
			}
		}
		detalhes[chave] = msg
	}
	if len(detalhes) > 0 {
		return apperr.Validation(detalhes)
	}
	return nil
}

// nomesExatos devolve os nomes JSON aceitos no topo do tipo, ou nil quando o
// tipo está fora da regra. Calculado uma vez por tipo.
func nomesExatos(t reflect.Type) map[string]bool {
	if v, ok := nomesExatosPorTipo.Load(t); ok {
		return v.(map[string]bool)
	}
	var nomes map[string]bool
	base := t
	for base.Kind() == reflect.Pointer {
		base = base.Elem()
	}
	if base.Kind() == reflect.Struct && !base.Implements(tipoUnmarshalerJSON) &&
		!reflect.PointerTo(base).Implements(tipoUnmarshalerJSON) {
		nomes = map[string]bool{}
		coletarNomes(base, nomes, map[reflect.Type]bool{})
	}
	nomesExatosPorTipo.Store(t, nomes)
	return nomes
}

func coletarNomes(t reflect.Type, nomes map[string]bool, vistos map[reflect.Type]bool) {
	if vistos[t] {
		return
	}
	vistos[t] = true
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		nome, _, _ := strings.Cut(tag, ",")
		if f.Anonymous && nome == "" {
			embutido := f.Type
			if embutido.Kind() == reflect.Pointer {
				embutido = embutido.Elem()
			}
			if embutido.Kind() == reflect.Struct {
				coletarNomes(embutido, nomes, vistos)
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if nome == "" {
			nome = f.Name
		}
		nomes[nome] = true
	}
}

// Validar roda as tags do validator e, depois, o gancho Validador. As duas
// fontes de erro se somam no mesmo mapa: o usuário corrige tudo de uma vez, em
// vez de descobrir um erro por requisição.
func Validar(alvo any) error {
	detalhes := map[string]string{}

	if ehValidavelPorTag(alvo) {
		if err := validate().Struct(alvo); err != nil {
			var invalido *validator.InvalidValidationError
			if errors.As(err, &invalido) {
				return apperr.Internal.WithCause(err)
			}
			var falhas validator.ValidationErrors
			if errors.As(err, &falhas) {
				for _, f := range falhas {
					detalhes[caminhoDoCampo(f)] = mensagemDaTag(f)
				}
			}
		}
	}

	if v, ok := alvo.(Validador); ok {
		for campo, msg := range v.Validar() {
			detalhes[campo] = msg
		}
	}

	if len(detalhes) > 0 {
		return apperr.Validation(detalhes)
	}
	return nil
}

// ValidarValor roda uma tag do validator sobre um valor solto. É assim que os
// campos Opt são validados: o validator não enxerga dentro do genérico, então a
// regra sai da tag da struct e vira chamada explícita no Validar() do DTO.
func ValidarValor(valor any, tag string) bool {
	return validate().Var(valor, tag) == nil
}

// ehValidavelPorTag protege o validator, que só aceita struct: DTO que é slice
// no topo (a matriz de permissões) valida pelo gancho Validador.
func ehValidavelPorTag(alvo any) bool {
	v := reflect.ValueOf(alvo)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	return v.Kind() == reflect.Struct
}

func erroDeJSON(err error) error {
	var sintaxe *json.SyntaxError
	var tipo *json.UnmarshalTypeError
	var grande *http.MaxBytesError

	switch {
	case errors.As(err, &sintaxe):
		return apperr.Validation(map[string]string{"body": fmt.Sprintf("JSON malformado na posição %d.", sintaxe.Offset)})
	case errors.As(err, &tipo):
		campo := tipo.Field
		if campo == "" {
			campo = "body"
		}
		return apperr.Validation(map[string]string{campo: fmt.Sprintf("tipo inválido: esperado %s.", tipo.Type.String())})
	case errors.As(err, &grande):
		return apperr.Validation(map[string]string{"body": "corpo da requisição excede o limite de 1 MB."})
	case errors.Is(err, io.EOF):
		return apperr.Validation(map[string]string{"body": "corpo da requisição é obrigatório."})
	case campoDesconhecido(err) != "":
		// O encoding/json não expõe tipo para este erro (proposta golang/go#29035
		// segue aberta), só a mensagem. Por isso o nome sai por extração de
		// string, com fallback para "body" quando o formato mudar numa versão
		// futura do Go — a recusa continua acontecendo, só perde a precisão do
		// campo. O teste `TestDecodeRecusaCampoDesconhecido` trava o formato.
		return apperr.Validation(map[string]string{campoDesconhecido(err): "campo desconhecido no corpo da requisição."})
	default:
		return apperr.Validation(map[string]string{"body": "não foi possível ler o corpo da requisição."}).WithCause(err)
	}
}

// prefixoCampoDesconhecido é a mensagem que o encoding/json monta em
// DisallowUnknownFields. Extraímos o nome entre aspas para o painel conseguir
// grudar o erro no input certo.
const prefixoCampoDesconhecido = "json: unknown field "

// campoDesconhecido devolve o nome do campo recusado, ou "" quando o erro é de
// outra natureza. Devolve "body" quando reconhece o prefixo mas não consegue
// isolar o nome — recusar sem nomear ainda é melhor que aceitar em silêncio.
func campoDesconhecido(err error) string {
	msg := err.Error()
	i := strings.Index(msg, prefixoCampoDesconhecido)
	if i < 0 {
		return ""
	}
	resto := msg[i+len(prefixoCampoDesconhecido):]
	nome := strings.Trim(strings.TrimSpace(resto), `"`)
	if nome == "" {
		return "body"
	}
	return nome
}

// caminhoDoCampo devolve o campo em notação de JSON aninhado, sem o nome da
// struct raiz: "PedidoDeOrcamento.check_in" vira "check_in".
func caminhoDoCampo(f validator.FieldError) string {
	ns := f.Namespace()
	if i := strings.Index(ns, "."); i >= 0 {
		return ns[i+1:]
	}
	return f.Field()
}

func mensagemDaTag(f validator.FieldError) string {
	switch f.Tag() {
	case "required":
		return "é obrigatório."
	case "email":
		return "e-mail inválido."
	case "uuid", "uuid4":
		return "identificador inválido."
	case "min":
		if f.Kind() == reflect.String {
			return fmt.Sprintf("deve ter ao menos %s caracteres.", f.Param())
		}
		return fmt.Sprintf("deve ser no mínimo %s.", f.Param())
	case "max":
		if f.Kind() == reflect.String {
			return fmt.Sprintf("deve ter no máximo %s caracteres.", f.Param())
		}
		return fmt.Sprintf("deve ser no máximo %s.", f.Param())
	case "gt":
		return fmt.Sprintf("deve ser maior que %s.", f.Param())
	case "gte":
		return fmt.Sprintf("deve ser maior ou igual a %s.", f.Param())
	case "lt":
		return fmt.Sprintf("deve ser menor que %s.", f.Param())
	case "lte":
		return fmt.Sprintf("deve ser menor ou igual a %s.", f.Param())
	case "len":
		return fmt.Sprintf("deve ter exatamente %s caracteres.", f.Param())
	case "oneof":
		return fmt.Sprintf("deve ser um de: %s.", strings.ReplaceAll(f.Param(), " ", ", "))
	case "url":
		return "URL inválida."
	case "e164":
		return "telefone deve estar no formato internacional (+5585999990000)."
	default:
		return "valor inválido."
	}
}
