package users

import (
	"encoding/json"
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

func decodificar(t *testing.T, corpo string) Atualizar {
	t.Helper()

	var a Atualizar
	if err := json.Unmarshal([]byte(corpo), &a); err != nil {
		t.Fatalf("Unmarshal(%s): %v", corpo, err)
	}
	a.Normalizar()
	return a
}

// PATCH: ausente não muda, null limpa. É a diferença que o Opt carrega.
func TestPatchDistingueAusenteDeNulo(t *testing.T) {
	a := decodificar(t, `{"phone": null}`)
	if !a.Telefone.DeveLimpar() {
		t.Fatal(`{"phone": null} deveria pedir limpeza`)
	}
	if a.Nome.Set {
		t.Fatal("name ausente não pode aparecer como definido")
	}

	b := decodificar(t, `{}`)
	if b.Telefone.Set {
		t.Fatal("corpo vazio não deveria tocar em phone")
	}
}

// Telefone em branco é o mesmo pedido de "limpar" que o null — aceitar os dois
// evita que o front tenha de saber a diferença.
func TestTelefoneEmBrancoViraLimpeza(t *testing.T) {
	a := decodificar(t, `{"phone": "   "}`)
	if !a.Telefone.DeveLimpar() {
		t.Fatal("telefone em branco deveria virar limpeza")
	}
}

func TestNormalizarBaixaCaixaDoEmail(t *testing.T) {
	a := decodificar(t, `{"email": "  Ana@WH.COM "}`)
	if v, _ := a.Email.Definido(); v != "ana@wh.com" {
		t.Fatalf("email = %q, esperado ana@wh.com", v)
	}
}

// PUT é substituição integral: o que não veio volta ao padrão. A exceção é a
// senha — um PUT sem senha não pode zerar a credencial de quem foi editado.
func TestParaSubstituicaoAplicaOsPadroesDoPut(t *testing.T) {
	sub := decodificar(t, `{"name":"Ana","email":"ana@wh.com","role_id":"9f1c1e2a-0d3b-4f6a-9a1e-2b7c5d8e0f11"}`).
		ParaSubstituicao()

	if !sub.Telefone.DeveLimpar() {
		t.Fatal("phone ausente no PUT deveria virar nulo")
	}
	if v, ok := sub.Ativo.Definido(); !ok || !v {
		t.Fatal("active ausente no PUT deveria voltar a true")
	}
	if sub.Senha.Set {
		t.Fatal("password ausente no PUT NÃO pode virar alteração de senha")
	}
}

// PUT com active: false explícito continua valendo — a substituição não
// atropela o que o cliente mandou.
func TestParaSubstituicaoRespeitaOQueVeio(t *testing.T) {
	sub := decodificar(t, `{"name":"Ana","email":"a@b.com","role_id":"9f1c1e2a-0d3b-4f6a-9a1e-2b7c5d8e0f11","active":false,"phone":"+5585999990000"}`).
		ParaSubstituicao()

	if v, ok := sub.Ativo.Definido(); !ok || v {
		t.Fatal("active: false explícito deveria ser preservado")
	}
	if v, ok := sub.Telefone.Definido(); !ok || v != "+5585999990000" {
		t.Fatalf("phone = %q", v)
	}
}

func TestObrigatoriosDoPut(t *testing.T) {
	faltando := decodificar(t, `{"name":"Ana"}`).ObrigatoriosDoPut()
	if faltando["email"] == "" || faltando["role_id"] == "" {
		t.Fatalf("PUT sem email/role_id deveria acusar os dois: %v", faltando)
	}
	if faltando["name"] != "" {
		t.Fatalf("name veio no corpo e não deveria ser acusado: %v", faltando)
	}
}

// Campo que a coluna exige NOT NULL não pode receber `null`: um 422 legível vale
// mais que uma violação 23502 traduzida.
func TestValidarRecusaNuloEmCampoObrigatorio(t *testing.T) {
	erros := decodificar(t, `{"name": null, "email": null, "role_id": null, "active": null, "password": null}`).Validar()

	for _, campo := range []string{"name", "email", "role_id", "active", "password"} {
		if erros[campo] == "" {
			t.Errorf("%s com null deveria ser recusado", campo)
		}
	}
}

func TestValidarAplicaAsRegrasDeFormato(t *testing.T) {
	erros := decodificar(t, `{"name":"A","email":"nao-e-email","password":"1234"}`).Validar()

	if erros["name"] == "" || erros["email"] == "" || erros["password"] == "" {
		t.Fatalf("esperava erro nos três campos: %v", erros)
	}
}

func TestValidarAceitaCorpoBom(t *testing.T) {
	erros := decodificar(t, `{"name":"Ana Souza","email":"ana@wh.com","password":"12345678","phone":"+5585999990000"}`).Validar()
	if len(erros) != 0 {
		t.Fatalf("corpo válido não deveria gerar erro: %v", erros)
	}
}

func TestCriarAplicaODefaultDeActive(t *testing.T) {
	var c Criar
	if !c.AtivoOuPadrao() {
		t.Fatal("active ausente deveria virar true (default do contrato)")
	}

	falso := false
	c.Ativo = &falso
	if c.AtivoOuPadrao() {
		t.Fatal("active: false explícito deveria ser respeitado")
	}
}

func TestCriarNormalizaTelefoneVazioParaNulo(t *testing.T) {
	vazio := "  "
	c := Criar{Nome: " Ana ", Email: " ANA@WH.com ", Telefone: &vazio}
	c.Normalizar()

	if c.Nome != "Ana" || c.Email != "ana@wh.com" {
		t.Fatalf("normalização falhou: %+v", c)
	}
	if c.Telefone != nil {
		t.Fatalf("telefone em branco deveria virar nulo, veio %q", *c.Telefone)
	}
}

// Guarda-corpo do contrato do Opt dentro do DTO.
func TestOptDoDTOEhOMesmoDoHttpx(t *testing.T) {
	var a Atualizar
	a.Nome = httpx.De("Ana")
	if v, ok := a.Nome.Definido(); !ok || v != "Ana" {
		t.Fatal("Opt do DTO deveria aceitar o construtor do httpx")
	}
}
