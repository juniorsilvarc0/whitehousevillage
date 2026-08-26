package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/apperr"
)

type criarDeTeste struct {
	Nome  string      `json:"name" validate:"required,min=2"`
	Email string      `json:"email" validate:"required,email"`
	Senha string      `json:"password" validate:"required,min=8"`
	Extra Opt[string] `json:"extra"`
}

// Validar é o gancho para o que a tag não alcança — em especial os campos Opt.
func (c criarDeTeste) Validar() map[string]string {
	if c.Extra.DeveLimpar() {
		return map[string]string{"extra": "não pode ser nulo."}
	}
	return nil
}

func post(corpo string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(corpo))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func detalhes(t *testing.T, err error) map[string]string {
	t.Helper()

	e := apperr.From(err)
	if e.Code != "VALIDATION_ERROR" {
		t.Fatalf("code = %q, esperado VALIDATION_ERROR", e.Code)
	}
	// Os details viajam como `any`; o formato do contrato é {campo: mensagem}.
	bruto, _ := json.Marshal(e.Details)
	var out map[string]string
	if err := json.Unmarshal(bruto, &out); err != nil {
		t.Fatalf("details fora do formato {campo: mensagem}: %s", bruto)
	}
	return out
}

func TestDecodeAceitaCorpoValido(t *testing.T) {
	got, err := Decode[criarDeTeste](post(`{"name":"Ana","email":"ana@wh.com","password":"12345678"}`))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Nome != "Ana" || got.Email != "ana@wh.com" {
		t.Fatalf("corpo decodificado errado: %+v", got)
	}
}

func TestDecodeDevolveDetailsPorCampoEmPortugues(t *testing.T) {
	_, err := Decode[criarDeTeste](post(`{"name":"A","email":"nao-e-email","password":"123"}`))
	if err == nil {
		t.Fatal("corpo inválido deveria falhar")
	}

	d := detalhes(t, err)
	if d["name"] != "deve ter ao menos 2 caracteres." {
		t.Fatalf("details[name] = %q", d["name"])
	}
	if d["email"] != "e-mail inválido." {
		t.Fatalf("details[email] = %q", d["email"])
	}
	if d["password"] != "deve ter ao menos 8 caracteres." {
		t.Fatalf("details[password] = %q", d["password"])
	}
}

func TestDecodeAcumulaOsErrosDeTagEDeValidador(t *testing.T) {
	_, err := Decode[criarDeTeste](post(`{"name":"Ana","email":"ana@wh.com","password":"12345678","extra":null}`))
	if err == nil {
		t.Fatal("o gancho Validador deveria ter reprovado o campo extra")
	}
	if d := detalhes(t, err); d["extra"] == "" {
		t.Fatalf("details deveria citar extra: %v", d)
	}
}

func TestDecodeRecusaJSONMalformadoESegundoDocumento(t *testing.T) {
	if _, err := Decode[criarDeTeste](post(`{"name":`)); err == nil {
		t.Fatal("JSON truncado deveria falhar")
	}
	if _, err := Decode[criarDeTeste](post(`{"name":"Ana","email":"a@b.com","password":"12345678"}{"x":1}`)); err == nil {
		t.Fatal("segundo documento no corpo deveria falhar")
	}
}

func TestDecodeRecusaCorpoVazioMasDecodeOpcionalAceita(t *testing.T) {
	if _, err := Decode[criarDeTeste](post("")); err == nil {
		t.Fatal("corpo vazio deveria falhar no Decode")
	}

	tipoOpcional := struct {
		Token string `json:"refresh_token"`
	}{}
	_ = tipoOpcional

	got, err := DecodeOpcional[struct {
		Token string `json:"refresh_token"`
	}](post(""))
	if err != nil {
		t.Fatalf("DecodeOpcional com corpo vazio: %v", err)
	}
	if got.Token != "" {
		t.Fatalf("esperava zero value, veio %+v", got)
	}
}

// A matriz de permissões chega como array no topo do JSON; o validator só
// aceita struct, então o tipo nomeado valida pelo gancho.
type matrizDeTeste []string

func (m matrizDeTeste) Validar() map[string]string {
	if len(m) == 0 {
		return map[string]string{"permissions": "informe ao menos um item."}
	}
	return nil
}

func TestDecodeAceitaArrayNoTopo(t *testing.T) {
	got, err := Decode[matrizDeTeste](post(`["a","b"]`))
	if err != nil {
		t.Fatalf("Decode de array no topo: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("esperava 2 itens, veio %d", len(got))
	}

	if _, err := Decode[matrizDeTeste](post(`[]`)); err == nil {
		t.Fatal("o gancho Validador deveria ter reprovado o array vazio")
	}
}
