package roles

import (
	"encoding/json"
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/auth"
)

func TestCodigoSegueOPadraoDoContrato(t *testing.T) {
	validos := []string{"recepcao", "corretor", "admin", "suporte_n1", "perfil2"}
	for _, codigo := range validos {
		c := Criar{Codigo: codigo, Nome: "Qualquer"}
		if erros := c.Validar(); len(erros) != 0 {
			t.Errorf("%q deveria ser válido: %v", codigo, erros)
		}
	}

	invalidos := []string{"A", "Recepcao", "1recepcao", "recepção", "com-hifen", "x", "com espaco"}
	for _, codigo := range invalidos {
		c := Criar{Codigo: codigo, Nome: "Qualquer"}
		if erros := c.Validar(); erros["code"] == "" {
			t.Errorf("%q deveria ser recusado", codigo)
		}
	}
}

func TestCriarNormalizaCodigo(t *testing.T) {
	c := Criar{Codigo: "  RECEPCAO ", Nome: "  Recepção  "}
	c.Normalizar()

	if c.Codigo != "recepcao" || c.Nome != "Recepção" {
		t.Fatalf("normalização falhou: %+v", c)
	}
}

// A PK é (role_id, resource_code, action): um par não pode ter dois escopos.
// Deixar passar faria o INSERT estourar 23505 no meio da transação.
func TestMatrizRecusaParRepetido(t *testing.T) {
	m := Matriz{
		{Resource: "reservations", Action: auth.AcaoVer, Scope: auth.EscopoAll},
		{Resource: "reservations", Action: auth.AcaoVer, Scope: auth.EscopoOwn},
	}

	erros := m.Validar()
	if erros["permissions[1]"] == "" {
		t.Fatalf("par repetido deveria ser acusado no índice 1: %v", erros)
	}
}

func TestMatrizRecusaAcaoEEscopoDesconhecidos(t *testing.T) {
	m := Matriz{
		{Resource: "reservations", Action: "aprovar", Scope: auth.EscopoAll},
		{Resource: "reservations", Action: auth.AcaoVer, Scope: "todos"},
		{Resource: "", Action: auth.AcaoVer, Scope: auth.EscopoAll},
	}

	erros := m.Validar()
	if erros["permissions[0].action"] == "" {
		t.Error("ação fora do catálogo deveria ser recusada")
	}
	if erros["permissions[1].scope"] == "" {
		t.Error("escopo fora de all|own deveria ser recusado")
	}
	if erros["permissions[2].resource"] == "" {
		t.Error("recurso vazio deveria ser recusado")
	}
}

// Escopo ausente vale como `all`, que é o default do contrato.
func TestMatrizPreencheEscopoPadrao(t *testing.T) {
	m := Matriz{{Resource: "reservations", Action: auth.AcaoVer}}

	if erros := m.Validar(); len(erros) != 0 {
		t.Fatalf("escopo ausente não deveria ser erro: %v", erros)
	}
	if got := m.ComEscopoPadrao()[0].Scope; got != auth.EscopoAll {
		t.Fatalf("escopo padrão = %q, esperado all", got)
	}
	// ComEscopoPadrao não pode mutar a matriz recebida.
	if m[0].Scope != "" {
		t.Fatal("ComEscopoPadrao alterou a matriz original")
	}
}

// O corpo chega como array no TOPO do JSON, não como objeto com uma chave.
func TestMatrizDecodificaArrayNoTopo(t *testing.T) {
	var m Matriz
	if err := json.Unmarshal([]byte(`[{"resource":"reservations","action":"ver","scope":"own"}]`), &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(m) != 1 || m[0].Scope != auth.EscopoOwn {
		t.Fatalf("matriz decodificada errada: %+v", m)
	}
}

func TestAtualizarRecusaNuloEValidaFormato(t *testing.T) {
	var a Atualizar
	if err := json.Unmarshal([]byte(`{"code": null, "name": null}`), &a); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	erros := a.Validar()
	if erros["code"] == "" || erros["name"] == "" {
		t.Fatalf("null em code/name deveria ser recusado: %v", erros)
	}

	var b Atualizar
	if err := json.Unmarshal([]byte(`{"code": "Com Maiuscula"}`), &b); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	b.Normalizar()
	if erros := b.Validar(); erros["code"] == "" {
		t.Fatalf("code fora do padrão deveria ser recusado: %v", erros)
	}
}

// Escopo `own` só faz sentido onde a linha tem dono; onde não há, a grade
// oferece apenas `all`.
//
// A resposta agora vem de `resources.supports_own` — a mesma coluna que o seed
// escreve. A lista em Go que existia aqui divergiu do banco (não conhecia
// `chat`, `agenda`, `quotes` nem `calendar`) e tornou o perfil Corretor
// impossível de salvar.
func TestEscoposDoRecursoSaemDoSupportsOwn(t *testing.T) {
	comDono := escoposDoRecurso(true)
	if len(comDono) != 2 || comDono[0] != auth.EscopoAll || comDono[1] != auth.EscopoOwn {
		t.Fatalf("recurso com dono deveria oferecer all e own: %v", comDono)
	}

	semDono := escoposDoRecurso(false)
	if len(semDono) != 1 || semDono[0] != auth.EscopoAll {
		t.Fatalf("recurso sem dono deveria oferecer só all: %v", semDono)
	}
}

// `actions` é subconjunto declarado no catálogo: o razão financeiro é
// append-only e mensagem enviada não se apaga, então "excluir" não existe
// nesses recursos e o PUT tem de recusá-lo.
func TestMetaRecursoSoOfereceOQueOCatalogoDeclara(t *testing.T) {
	chat := MetaRecurso{Acoes: []string{auth.AcaoVer, auth.AcaoCriar, auth.AcaoEditar}, SuportaOwn: true}

	for _, acao := range chat.Acoes {
		if !chat.Oferece(acao) {
			t.Errorf("Oferece(%q) = false, mas a ação está no catálogo", acao)
		}
	}
	if chat.Oferece(auth.AcaoExcluir) {
		t.Error("Oferece(\"excluir\") = true num recurso que não declara excluir")
	}
}
