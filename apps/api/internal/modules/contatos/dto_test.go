package contatos

import (
	"testing"
	"time"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/httpx"
)

func ptr[T any](v T) *T { return &v }

// A regra da LGPD que o schema sozinho não expressa: consentimento sem data não
// é consentimento, é afirmação.
func TestCriarRecusaOptInSemConsentimento(t *testing.T) {
	c := Criar{Nome: "Ana Silva", OptInMarketing: ptr(true)}
	if erros := c.Validar(); erros["consent_at"] == "" {
		t.Fatalf("marketing_opt_in=true sem consent_at deveria falhar; erros=%v", erros)
	}

	agora := time.Now()
	c.ConsentimentoEm = &agora
	if erros := c.Validar(); len(erros) > 0 {
		t.Fatalf("com consent_at não deveria haver erro: %v", erros)
	}
}

// Desligar o opt-in limpa a data de aceite na MESMA escrita: data sobrevivente
// faria o relatório dizer que a pessoa aceitou.
func TestNormalizarLimpaConsentimentoQuandoOptInDesliga(t *testing.T) {
	agora := time.Now()
	c := Criar{Nome: "Ana Silva", OptInMarketing: ptr(false), ConsentimentoEm: &agora}
	c.Normalizar()
	if c.ConsentimentoEm != nil {
		t.Fatal("opt-in desligado não pode carregar consent_at")
	}

	a := Atualizar{OptInMarketing: httpx.De(false), ConsentimentoEm: httpx.De(agora)}
	a.Normalizar()
	if !a.ConsentimentoEm.DeveLimpar() {
		t.Fatal("PATCH que desliga o opt-in deveria limpar consent_at na mesma escrita")
	}
}

func TestCriarNormalizaTelefoneEDocumento(t *testing.T) {
	c := Criar{
		Nome:            "  Ana   Maria  Silva ",
		Email:           ptr("  ANA@Exemplo.INVALID "),
		Telefone:        ptr("(85) 99999-0000"),
		TipoDeDocumento: ptr(DocCPF),
		Documento:       ptr("529.982.247-25"),
		Estado:          ptr(" ce "),
	}
	c.Normalizar()

	if c.Nome != "Ana Maria Silva" {
		t.Errorf("nome = %q", c.Nome)
	}
	if *c.Email != "ana@exemplo.invalid" {
		t.Errorf("email = %q", *c.Email)
	}
	if *c.Telefone != "+5585999990000" {
		t.Errorf("telefone = %q", *c.Telefone)
	}
	if *c.Documento != "52998224725" {
		t.Errorf("documento = %q", *c.Documento)
	}
	if *c.Estado != "CE" {
		t.Errorf("estado = %q", *c.Estado)
	}
}

// Número sem tipo não se valida nem se compara — e o tipo sozinho não
// identifica ninguém, então some junto na normalização.
func TestDocumentoSemTipoEhRecusadoETipoSemNumeroSome(t *testing.T) {
	c := Criar{Nome: "Ana Silva", Documento: ptr("52998224725")}
	if erros := c.Validar(); erros["doc_type"] == "" {
		t.Fatalf("número sem tipo deveria exigir doc_type; erros=%v", erros)
	}

	c = Criar{Nome: "Ana Silva", TipoDeDocumento: ptr(DocCPF)}
	c.Normalizar()
	if c.TipoDeDocumento != nil {
		t.Fatal("tipo sem número deveria ser descartado na normalização")
	}
}

func TestCriarValidaEnumsEDatas(t *testing.T) {
	futuro := time.Now().AddDate(1, 0, 0).Format(time.DateOnly)
	c := Criar{
		Nome:       "Ana Silva",
		BaseLegal:  ptr("qualquer_coisa"),
		Nascimento: ptr(futuro),
		Estado:     ptr("CEE"),
		Email:      ptr("nao-e-email"),
		Telefone:   ptr("99999"),
	}
	erros := c.Validar()
	for _, campo := range []string{"lgpd_basis", "birth_date", "state", "email", "phone_e164"} {
		if erros[campo] == "" {
			t.Errorf("esperava erro em %q; erros=%v", campo, erros)
		}
	}
}

// Campo que a coluna exige NOT NULL não pode receber `null`: sem esta
// checagem, {"name": null} chegaria ao UPDATE e estouraria 23502 — um 422
// legível vale mais que uma violação de constraint traduzida.
func TestAtualizarRecusaNuloEmCampoObrigatorio(t *testing.T) {
	a := Atualizar{Nome: httpx.Nulo[string](), OptInMarketing: httpx.Nulo[bool]()}
	erros := a.Validar()
	if erros["name"] == "" || erros["marketing_opt_in"] == "" {
		t.Fatalf("null em campo NOT NULL deveria falhar; erros=%v", erros)
	}
}

// String em branco é o mesmo pedido de "limpar" que o null: obrigar o painel a
// saber a diferença entre "" e null para apagar um telefone é armadilha.
func TestAtualizarTrataBrancoComoLimpeza(t *testing.T) {
	a := Atualizar{Telefone: httpx.De("   "), Email: httpx.De("")}
	a.Normalizar()
	if !a.Telefone.DeveLimpar() || !a.Email.DeveLimpar() {
		t.Fatal("valor em branco deveria virar limpeza explícita")
	}
}

func TestPedidoDeAnonimizacaoExigeMotivo(t *testing.T) {
	p := PedidoDeAnonimizacao{Motivo: "   "}
	if erros := p.Validar(); erros["reason"] == "" {
		t.Fatal("motivo em branco deveria ser recusado: a ação é irreversível")
	}
	p = PedidoDeAnonimizacao{Motivo: "pedido do titular em 20/08/2026"}
	if erros := p.Validar(); len(erros) > 0 {
		t.Fatalf("motivo válido recusado: %v", erros)
	}
}
