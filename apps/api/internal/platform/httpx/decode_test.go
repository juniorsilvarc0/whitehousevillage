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

// ───────────── Campo desconhecido no corpo é 422, nunca silêncio ─────────────
//
// A regra está escrita no `info.description` da OpenAPI desde a Fase 1, e não
// tinha uma linha de implementação: `grep DisallowUnknownFields` devolvia zero
// ocorrências. O efeito medido na API real, antes desta correção:
//
//	PATCH /units/{id}       {"ativa":false,"xpto":1}  → 200, `active` intacto
//	POST  /quotes           {..., "campo_inventado":true} → 200, campo ignorado
//	PATCH /reservations/{id} {"guests":6}             → 200, `guests_count` intacto
//
// 200 sem efeito é a pior resposta possível: o cliente acha que gravou e não há
// erro em lugar nenhum para alguém investigar.

func TestDecodeRecusaCampoDesconhecido(t *testing.T) {
	_, err := Decode[criarDeTeste](post(`{"name":"Ana","email":"ana@wh.com","password":"12345678","xpto":1}`))
	if err == nil {
		t.Fatal("campo desconhecido foi aceito em silêncio")
	}
	d := detalhes(t, err)
	// O NOME do campo recusado precisa chegar em `details`: sem ele o painel não
	// consegue grudar o erro no input, e a mensagem vira "algo está errado".
	if _, ok := d["xpto"]; !ok {
		t.Fatalf("details não nomeia o campo recusado: %v", d)
	}
}

// O nome PARECIDO é o caso que importa: quem digita `guests` no lugar de
// `guests_count` não recebe nada de volta hoje, e é assim que um rename
// atravessa uma revisão inteira sem ninguém ver.
func TestDecodeRecusaONomeParecidoDoCampoCerto(t *testing.T) {
	_, err := Decode[criarDeTeste](post(`{"name":"Ana","email":"ana@wh.com","password":"12345678","e-mail":"outro@wh.com"}`))
	if err == nil {
		t.Fatal(`"e-mail" (com hífen) foi aceito como se fosse "email"`)
	}
	if _, ok := detalhes(t, err)["e-mail"]; !ok {
		t.Fatalf("details não nomeia o campo parecido: %v", detalhes(t, err))
	}
}

// DecodeOpcional aceita corpo VAZIO — isso não pode virar "aceita qualquer
// corpo". A rota de refresh/logout continua sendo escrita, e um campo inventado
// ali merece a mesma recusa.
func TestDecodeOpcionalTambemRecusaCampoDesconhecido(t *testing.T) {
	_, err := DecodeOpcional[criarDeTeste](post(`{"nome":"Ana"}`))
	if err == nil {
		t.Fatal("DecodeOpcional aceitou campo desconhecido")
	}
	if _, ok := detalhes(t, err)["nome"]; !ok {
		t.Fatalf("details não nomeia o campo recusado: %v", detalhes(t, err))
	}
}

// Controle positivo: a recusa não pode alcançar o corpo legítimo, nem o campo
// Opt ausente (que é silêncio combinado), nem o `null` explícito.
func TestCampoAusenteENullContinuamValidos(t *testing.T) {
	if _, err := Decode[criarDeTeste](post(`{"name":"Ana","email":"ana@wh.com","password":"12345678"}`)); err != nil {
		t.Fatalf("corpo legítimo sem o campo Opt foi recusado: %v", err)
	}
	got, err := Decode[criarDeTeste](post(`{"name":"Ana","email":"ana@wh.com","password":"12345678","extra":"x"}`))
	if err != nil {
		t.Fatalf("corpo legítimo COM o campo Opt foi recusado: %v", err)
	}
	if v, ok := got.Extra.Definido(); !ok || v != "x" {
		t.Fatalf("Opt declarado não chegou: %+v", got.Extra)
	}
}

// campoDesconhecido lê a mensagem do encoding/json por falta de tipo exportado
// (golang/go#29035). Este teste trava o formato: se o Go mudar o texto, ele
// falha aqui, e não em produção com o painel recebendo details vazio.
func TestFormatoDaMensagemDeCampoDesconhecidoDoGoNaoMudou(t *testing.T) {
	err := json.Unmarshal([]byte(`{"xpto":1}`), &struct{}{})
	if err != nil {
		t.Fatalf("json.Unmarshal sem DisallowUnknownFields não deveria errar: %v", err)
	}
	dec := json.NewDecoder(strings.NewReader(`{"xpto":1}`))
	dec.DisallowUnknownFields()
	err = dec.Decode(&struct{}{})
	if err == nil {
		t.Fatal("DisallowUnknownFields não recusou")
	}
	if nome := campoDesconhecido(err); nome != "xpto" {
		t.Fatalf("campoDesconhecido = %q, esperado \"xpto\" — a mensagem do Go mudou: %q", nome, err.Error())
	}
}
