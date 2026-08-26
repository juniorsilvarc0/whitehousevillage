// Package httpx concentra o formato da resposta HTTP: envelope, erro e
// paginação. Handler nenhum escreve JSON na mão.
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"reflect"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

// Meta acompanha toda listagem paginada.
type Meta struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type envelope struct {
	Data any   `json:"data,omitempty"`
	Meta *Meta `json:"meta,omitempty"`
}

type errEnvelope struct {
	Error *apperr.Error `json:"error"`
}

// JSON responde um recurso único.
func JSON(w http.ResponseWriter, status int, data any) {
	write(w, status, envelope{Data: data})
}

// List responde uma coleção paginada.
//
// A normalização mora aqui, e não em cada repositório, porque é aqui que a
// garantia passa a valer para toda listagem futura: nenhum handler precisa
// lembrar de trocar o slice nulo por vazio antes de responder.
func List(w http.ResponseWriter, data any, meta Meta) {
	if meta.PerPage > 0 {
		meta.TotalPages = int((meta.Total + int64(meta.PerPage) - 1) / int64(meta.PerPage))
	}
	write(w, http.StatusOK, envelope{Data: colecaoNaoNula(data), Meta: &meta})
}

// colecaoNaoNula troca coleção nula por coleção vazia.
//
// O contrato promete `data` como array em toda listagem. Um slice nulo dentro de
// um `any` não é "vazio" para o encoding/json — ele vira `"data": null`, e o
// `.map()` do painel estoura justamente no caso mais comum, a busca sem
// resultado. Slice vazio do mesmo tipo serializa `[]`.
func colecaoNaoNula(data any) any {
	if data == nil {
		return []any{}
	}
	v := reflect.ValueOf(data)
	switch v.Kind() {
	case reflect.Slice:
		if v.IsNil() {
			return reflect.MakeSlice(v.Type(), 0, 0).Interface()
		}
	case reflect.Map:
		if v.IsNil() {
			return reflect.MakeMap(v.Type()).Interface()
		}
	}
	return data
}

// Error traduz qualquer erro para o envelope de erro. A causa vai para o log,
// nunca para a resposta.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	e := apperr.From(err)
	if e.Status() >= http.StatusInternalServerError {
		slog.ErrorContext(r.Context(), "erro interno",
			"code", e.Code, "path", r.URL.Path, "method", r.Method, "err", e.Error())
	}
	write(w, e.Status(), errEnvelope{Error: e})
}

func write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("falha ao escrever resposta", "err", err)
	}
}
