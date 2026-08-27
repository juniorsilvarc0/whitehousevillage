package audit

import (
	"encoding/json"
	"reflect"
)

// Snapshot converte a struct do repositório em Campos usando as tags `json` que
// ela JÁ tem.
//
// Por que assim, e não reflexão própria: o passo é `json.Marshal` seguido de
// `json.Unmarshal` num mapa — duas funções da biblioteca padrão que qualquer
// pessoa do time lê sem manual. O nome do campo na trilha passa a ser o mesmo
// que o painel recebe na API, então "quem leu a tela sabe ler a auditoria".
// Uma reflexão artesanal daria os nomes em Go (`PrecoCentavos` em vez de
// `price_cents`) e exigiria decidir sozinha o que fazer com ponteiro, embutido,
// tempo e enum — decisões que o encoding/json já tomou e documentou.
//
// Consequência que precisa estar escrita: campo marcado `json:"-"` NÃO aparece
// na trilha. Se um campo deve ser auditado e não trafega na API, quem chama
// monta Campos à mão para ele.
//
// Nunca falha. Valor que não vira objeto JSON (uma string, um número, uma
// lista) fica sob a chave "valor" — a trilha é um registro, e recusá-lo por
// formato seria perder o rastro por causa da embalagem.
func Snapshot(v any) Campos {
	if v == nil || ehNiloTipado(v) {
		return nil
	}
	bruto, err := json.Marshal(v)
	if err != nil {
		// Só acontece com tipo não serializável (canal, func, ciclo). Guarda o
		// motivo em vez de sumir com a linha inteira.
		return Campos{"_erro_snapshot": err.Error()}
	}
	var m Campos
	if err := json.Unmarshal(bruto, &m); err != nil {
		return Campos{"valor": json.RawMessage(bruto)}
	}
	return m
}

// Diff recorta os dois documentos para os campos que MUDARAM, dos dois lados.
//
// É o que faz a linha responder "quem triplicou a tarifa" de relance: sem o
// recorte, `before` e `after` trazem a entidade inteira duas vezes e o campo
// alterado fica escondido entre vinte idênticos.
//
// Campo presente em um lado só conta como mudança (foi acrescentado ou sumiu).
// Os valores comparados vêm de json.Unmarshal — só mapa, lista, string, float64,
// bool e nil —, e é sobre esse conjunto fechado que reflect.DeepEqual é
// previsível.
func Diff(antes, depois Campos) (Campos, Campos) {
	if len(antes) == 0 || len(depois) == 0 {
		return antes, depois
	}

	a, d := Campos{}, Campos{}
	for chave, valorAntes := range antes {
		valorDepois, existe := depois[chave]
		if existe && reflect.DeepEqual(valorAntes, valorDepois) {
			continue
		}
		a[chave] = valorAntes
		if existe {
			d[chave] = valorDepois
		}
	}
	for chave, valorDepois := range depois {
		if _, existia := antes[chave]; !existia {
			d[chave] = valorDepois
		}
	}

	if len(a) == 0 && len(d) == 0 {
		return nil, nil
	}
	return a, d
}

// ehNiloTipado pega o ponteiro nulo dentro de uma interface — `var u *Unidade;
// Snapshot(u)`. Sem isto, json.Marshal devolve "null", o Unmarshal falha e a
// trilha ganharia um campo "valor": null sem significado nenhum.
func ehNiloTipado(v any) bool {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
}
