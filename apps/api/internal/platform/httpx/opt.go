package httpx

import (
	"bytes"
	"encoding/json"
)

// Opt distingue os três estados que um campo de PATCH pode ter e que um
// ponteiro simples não consegue separar:
//
//	campo ausente do JSON   → Set=false, Valid=false  ⇒ não mexer
//	campo com null          → Set=true,  Valid=false  ⇒ limpar (gravar NULL)
//	campo com valor         → Set=true,  Valid=true   ⇒ gravar Value
//
// Sem essa distinção, `{"phone": null}` e `{}` chegam idênticos no service e
// "apagar o telefone" vira impossível de expressar.
type Opt[T any] struct {
	Set   bool
	Valid bool
	Value T
}

// Definido devolve o valor e se ele deve ser gravado como valor (e não como
// NULL). É o formato que o repositório consome ao montar o SET.
func (o Opt[T]) Definido() (T, bool) { return o.Value, o.Set && o.Valid }

// DeveLimpar é o caso `null` explícito.
func (o Opt[T]) DeveLimpar() bool { return o.Set && !o.Valid }

// Ou devolve o valor quando presente e não nulo, senão o padrão. Útil no PUT,
// que trata ausente como "volta ao padrão".
func (o Opt[T]) Ou(padrao T) T {
	if o.Set && o.Valid {
		return o.Value
	}
	return padrao
}

// De constrói um Opt com valor — usado em teste e na normalização do PUT.
func De[T any](v T) Opt[T] { return Opt[T]{Set: true, Valid: true, Value: v} }

// Nulo constrói o Opt que representa `null` explícito.
func Nulo[T any]() Opt[T] { return Opt[T]{Set: true} }

// UnmarshalJSON só é chamado quando a chave existe no JSON — é exatamente isso
// que permite marcar Set=true aqui e deixar o zero value significar "ausente".
func (o *Opt[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		o.Valid = false
		var zero T
		o.Value = zero
		return nil
	}
	if err := json.Unmarshal(b, &o.Value); err != nil {
		return err
	}
	o.Valid = true
	return nil
}

// MarshalJSON existe para o Opt poder aparecer em resposta sem virar objeto
// `{"Set":true,...}`.
func (o Opt[T]) MarshalJSON() ([]byte, error) {
	if !o.Set || !o.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(o.Value)
}
