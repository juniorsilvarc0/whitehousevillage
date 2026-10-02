package audit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

// usuarioDeTeste é a struct de repositório típica: tem campo público e tem o
// hash da senha, que é exatamente o que não pode chegar em `audit_log`.
type usuarioDeTeste struct {
	Nome         string `json:"name"`
	Email        string `json:"email"`
	SenhaHash    string `json:"password_hash"`
	RefreshToken string `json:"refresh_token"`
	Interno      string `json:"-"`
}

const segredoDoTeste = "$argon2id$v=19$m=65536,t=3,p=2$NAOPODEVAZAR"

func TestRedigirEsconderCampoSensivel(t *testing.T) {
	c := Snapshot(usuarioDeTeste{
		Nome:         "Ana",
		Email:        "ana@exemplo.com",
		SenhaHash:    segredoDoTeste,
		RefreshToken: "rt_abc123",
		Interno:      "não trafega",
	})

	redigido := Redigir(c)

	if redigido["name"] != "Ana" {
		t.Errorf("campo comum foi perdido: %v", redigido["name"])
	}
	if redigido["password_hash"] != Redigido {
		t.Errorf("password_hash = %v, esperado %q", redigido["password_hash"], Redigido)
	}
	if redigido["refresh_token"] != Redigido {
		t.Errorf("refresh_token = %v, esperado %q", redigido["refresh_token"], Redigido)
	}
	if _, tem := redigido["Interno"]; tem {
		t.Error(`campo json:"-" não deveria aparecer na trilha`)
	}

	// O teste que interessa de verdade: o segredo não está em lugar nenhum do
	// documento serializado, nem sob outra chave.
	bruto, err := json.Marshal(redigido)
	if err != nil {
		t.Fatalf("serializando: %v", err)
	}
	if strings.Contains(string(bruto), segredoDoTeste) {
		t.Fatalf("o segredo vazou no documento: %s", bruto)
	}
}

func TestRedigirDesceEmDocumentoAninhado(t *testing.T) {
	c := Campos{
		"integracao": map[string]any{
			"nome": "uazapi",
			"credenciais": map[string]any{
				"api_token": "tk_secreto",
			},
		},
		"eventos": []any{
			map[string]any{"tipo": "envio", "authorization": "Bearer xyz"},
		},
	}

	bruto, err := json.Marshal(Redigir(c))
	if err != nil {
		t.Fatalf("serializando: %v", err)
	}
	for _, segredo := range []string{"tk_secreto", "Bearer xyz"} {
		if strings.Contains(string(bruto), segredo) {
			t.Errorf("segredo %q vazou de nível aninhado: %s", segredo, bruto)
		}
	}
	if !strings.Contains(string(bruto), "uazapi") {
		t.Errorf("campo inocente aninhado foi perdido: %s", bruto)
	}
}

// Nulo continua nulo, no filtro de segredo como no de PII: um nulo não carrega
// segredo nenhum, e "o campo foi limpo" é informação que a trilha registra.
func TestRedigirPreservaNuloNoCampoSensivel(t *testing.T) {
	got := Redigir(Campos{"password_hash": nil, "refresh_token": "rt_abc"})

	if v, tem := got["password_hash"]; !tem || v != nil {
		t.Errorf("password_hash = %v (presente=%v), esperado nulo presente", v, tem)
	}
	if got["refresh_token"] != Redigido {
		t.Errorf("refresh_token = %v, esperado %q", got["refresh_token"], Redigido)
	}
}

// RedigirCom é a mesma descida com outro critério: é o que impede
// `internal/platform/pii` de escrever uma segunda função de descida — e a
// segunda chance de esquecer de entrar numa lista aninhada.
func TestRedigirComTrocaOCriterioESegueDescendo(t *testing.T) {
	soONome := func(nome string) bool { return nome == "name" }

	got := RedigirCom(Campos{
		"password_hash": "não é segredo para este critério",
		"pessoa":        map[string]any{"name": "Ana"},
	}, soONome)

	if got["password_hash"] != "não é segredo para este critério" {
		t.Errorf("o critério passado foi ignorado: %v", got["password_hash"])
	}
	aninhado, _ := got["pessoa"].(Campos)
	if aninhado["name"] != Redigido {
		t.Errorf("nível aninhado = %v, esperado %q", aninhado["name"], Redigido)
	}
}

func TestSensivelCobreOsNomesReaisDoRepositorio(t *testing.T) {
	sensiveis := []string{
		"password_hash", "PasswordHash", "senha", "senha_atual",
		"token_hash", "refresh_token", "api_key", "apiKey",
		"client_secret", "authorization", "cookie", "private_key",
	}
	for _, nome := range sensiveis {
		if !Sensivel(nome) {
			t.Errorf("Sensivel(%q) = false, deveria ser filtrado", nome)
		}
	}

	inocentes := []string{"name", "email", "price_cents", "check_in", "status", "unit_id"}
	for _, nome := range inocentes {
		if Sensivel(nome) {
			t.Errorf("Sensivel(%q) = true, campo comum sendo perdido da trilha", nome)
		}
	}
}

func TestDiffGuardaSoOQueMudou(t *testing.T) {
	antes := Campos{"price_cents": float64(30000), "date_type": "weekday", "active": true}
	depois := Campos{"price_cents": float64(90000), "date_type": "weekday", "active": true}

	a, d := Diff(antes, depois)

	if len(a) != 1 || a["price_cents"] != float64(30000) {
		t.Errorf("before = %v, esperado só price_cents antigo", a)
	}
	if len(d) != 1 || d["price_cents"] != float64(90000) {
		t.Errorf("after = %v, esperado só price_cents novo", d)
	}
}

func TestDiffSemMudancaDevolveNada(t *testing.T) {
	c := Campos{"a": "x"}
	if a, d := Diff(c, Campos{"a": "x"}); a != nil || d != nil {
		t.Errorf("Diff sem mudança = (%v, %v), esperado (nil, nil)", a, d)
	}
}

func TestDiffMarcaCampoQueApareceOuSome(t *testing.T) {
	a, d := Diff(Campos{"a": "x", "sumiu": 1.0}, Campos{"a": "x", "novo": 2.0})
	if _, tem := a["sumiu"]; !tem {
		t.Errorf("campo removido não apareceu no before: %v", a)
	}
	if _, tem := d["novo"]; !tem {
		t.Errorf("campo acrescentado não apareceu no after: %v", d)
	}
}

func TestSnapshotDeValorNaoObjeto(t *testing.T) {
	if c := Snapshot("cancelada"); c["valor"] == nil {
		t.Errorf("Snapshot de string = %v, esperado sob a chave 'valor'", c)
	}
	var nulo *usuarioDeTeste
	if c := Snapshot(nulo); c != nil {
		t.Errorf("Snapshot de ponteiro nulo = %v, esperado nil", c)
	}
}

// A gramática do `action` é a MESMA que internal/modules/users já grava
// (`users.email_alterado`). O literal está repetido aqui de propósito: importar
// o módulo a partir da plataforma inverteria a dependência.
func TestAcaoMantemAGramaticaDoModuloQueJaAuditava(t *testing.T) {
	if got := Acao("users", "email_alterado"); got != "users.email_alterado" {
		t.Fatalf("Acao = %q, esperado %q — mudar isto quebra o filtro da tela de auditoria", got, "users.email_alterado")
	}
}

func TestEnderecoIPRecusaOQueNaoEEndereco(t *testing.T) {
	// Coluna `inet`: string inválida vira 22P02 e ABORTA a transação do
	// negócio. Auditoria não pode derrubar a operação que testemunha.
	for _, ruim := range []string{"", "  ", "bufconn", "pipe", "192.168.0.256"} {
		if v := enderecoIP(ruim); v != nil {
			t.Errorf("enderecoIP(%q) = %v, esperado nil", ruim, v)
		}
	}
	for _, bom := range []string{"127.0.0.1", "200.150.10.7", "::1"} {
		if v := enderecoIP(bom); v == nil {
			t.Errorf("enderecoIP(%q) = nil, endereço válido descartado", bom)
		}
	}
}

func TestRegistrarRecusaEventoSemAcaoOuEntidade(t *testing.T) {
	// Sem exec: a validação tem de barrar antes de tocar o banco.
	err := Registrar(context.Background(), nil, Evento{Entidade: "units"})
	if err == nil {
		t.Fatal("evento sem action foi aceito")
	}
}

func TestMiddlewareLevaIPEUserAgentParaOContexto(t *testing.T) {
	var visto Origem
	h := Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		visto = OrigemDoContexto(r.Context())
	}))

	req := httptest.NewRequest(http.MethodPost, "/units", nil)
	req.RemoteAddr = "203.0.113.9:54321"
	req.Header.Set("User-Agent", "painel/1.0")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if visto.IP != "203.0.113.9" {
		t.Errorf("IP = %q, esperado 203.0.113.9 (sem a porta)", visto.IP)
	}
	if visto.UserAgent != "painel/1.0" {
		t.Errorf("UserAgent = %q", visto.UserAgent)
	}
}

func TestMiddlewareTruncaUserAgentGigante(t *testing.T) {
	var visto Origem
	h := Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		visto = OrigemDoContexto(r.Context())
	}))

	req := httptest.NewRequest(http.MethodPost, "/units", nil)
	req.Header.Set("User-Agent", strings.Repeat("A", 4096))
	h.ServeHTTP(httptest.NewRecorder(), req)

	if len([]rune(visto.UserAgent)) != tamanhoMaximoUserAgent {
		t.Errorf("user-agent com %d runas, esperado %d", len([]rune(visto.UserAgent)), tamanhoMaximoUserAgent)
	}
}

// O contrato do pacote: quem chama não digita ator, IP, user-agent nem
// request_id. Se este teste quebrar, os quatro módulos passam a ter de lembrar
// deles — que é como a Fase 1 chegou a zero linha auditada.
func TestOrigemVemDoContextoSemQuemChamaDigitar(t *testing.T) {
	usuario := &auth.Usuario{ID: uuid.New(), PropertyID: uuid.New()}

	ctx := auth.WithUser(context.Background(), usuario)
	ctx = httpx.ComRequestID(ctx, "req-123")
	ctx = ComOrigem(ctx, Origem{IP: "198.51.100.4", UserAgent: "painel/1.0"})

	ev := comOrigemDoContexto(ctx, Evento{Acao: "units.alterado", Entidade: "units"})

	if ev.AtorID == nil || *ev.AtorID != usuario.ID {
		t.Errorf("ator = %v, esperado %v", ev.AtorID, usuario.ID)
	}
	if ev.PropriedadeID != usuario.PropertyID {
		t.Errorf("propriedade = %v, esperado %v", ev.PropriedadeID, usuario.PropertyID)
	}
	if ev.RequestID != "req-123" || ev.IP != "198.51.100.4" || ev.UserAgent != "painel/1.0" {
		t.Errorf("origem incompleta: %+v", ev)
	}
}

func TestValorExplicitoDoChamadorVenceOContexto(t *testing.T) {
	// O worker sabe o ator real de uma ação disparada em outra requisição.
	real := uuid.New()
	ctx := auth.WithUser(context.Background(), &auth.Usuario{ID: uuid.New()})

	ev := comOrigemDoContexto(ctx, Evento{Acao: "a.b", Entidade: "a", AtorID: &real})
	if *ev.AtorID != real {
		t.Errorf("ator = %v, esperado o informado %v", *ev.AtorID, real)
	}
}
