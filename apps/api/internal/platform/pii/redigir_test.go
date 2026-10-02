package pii

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/juniorsilvarc0/whitehousevillage/apps/api/internal/platform/audit"
)

func TestRedigirMascaraOsCincoCamposDePessoa(t *testing.T) {
	campos := audit.Campos{
		"name":             "Ana Silva",
		"email":            "ana@exemplo.com",
		"phone_e164":       "+5585999990000",
		"doc_number":       "52998224725",
		"birth_date":       "1990-04-11",
		"notes":            "prefere o chalé de baixo",
		"lgpd_basis":       "contrato",
		"marketing_opt_in": true,
		"doc_type":         "cpf",
	}

	got := Redigir(campos)

	for _, campo := range []string{"name", "email", "phone_e164", "doc_number", "birth_date", "notes"} {
		if got[campo] != audit.Redigido {
			t.Errorf("%s = %v, esperado %q", campo, got[campo], audit.Redigido)
		}
	}
	if got["lgpd_basis"] != "contrato" || got["marketing_opt_in"] != true || got["doc_type"] != "cpf" {
		t.Errorf("campo não-pessoal mascarado à toa: %v — é o que a ANPD pergunta", got)
	}

	// O que interessa de verdade: o valor não sobrou em lugar nenhum do
	// documento serializado, nem sob outra chave.
	bruto, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("serializando: %v", err)
	}
	for _, valor := range []string{"Ana Silva", "ana@exemplo.com", "+5585999990000", "52998224725", "1990-04-11"} {
		if strings.Contains(string(bruto), valor) {
			t.Errorf("o valor %q vazou no documento: %s", valor, bruto)
		}
	}
}

// Nulo continua nulo: "o campo foi limpo" é informação de trilha e não é dado
// pessoal nenhum. Trocá-lo pela marca mostraria "havia algo aqui" onde não havia.
func TestRedigirPreservaNuloEPreservaOCampo(t *testing.T) {
	got := Redigir(audit.Campos{"email": nil, "name": "Ana"})

	if v, tem := got["email"]; !tem || v != nil {
		t.Errorf("email = %v (presente=%v), esperado nulo presente", v, tem)
	}
	if _, tem := got["name"]; !tem {
		t.Error("o campo `name` sumiu: a trilha precisa registrar QUE o nome mudou")
	}
}

// O vazamento que interessa está um nível abaixo: a rooming list manda a lista
// de hóspedes dentro do documento da reserva.
func TestRedigirDesceEmDocumentoAninhado(t *testing.T) {
	campos := audit.Campos{
		"unit_code": "AP-03",
		"guests": []any{
			map[string]any{"guest_name": "Ana Silva", "holder_doc_number": "52998224725", "adults": float64(2)},
		},
	}

	bruto, err := json.Marshal(Redigir(campos))
	if err != nil {
		t.Fatalf("serializando: %v", err)
	}
	for _, valor := range []string{"Ana Silva", "52998224725"} {
		if strings.Contains(string(bruto), valor) {
			t.Errorf("%q vazou de nível aninhado: %s", valor, bruto)
		}
	}
	if !strings.Contains(string(bruto), "AP-03") {
		t.Errorf("campo inocente aninhado foi perdido: %s", bruto)
	}
}

func TestSensivelPegaONomeExatoEOComposto(t *testing.T) {
	pessoais := []string{
		"name", "email", "phone_e164", "doc_number", "birth_date", "notes",
		"guest_name", "broker_name", "contact_email", "holder_doc_number", "Guest_Name",
	}
	for _, nome := range pessoais {
		if !Sensivel(nome) {
			t.Errorf("Sensivel(%q) = false: o valor iria inteiro para a trilha", nome)
		}
	}

	inocentes := []string{"doc_type", "lgpd_basis", "price_cents", "check_in", "status", "unit_id", "namespace"}
	for _, nome := range inocentes {
		if Sensivel(nome) {
			t.Errorf("Sensivel(%q) = true: campo comum sendo perdido da trilha", nome)
		}
	}
}

// A razão de este pacote existir, escrita como teste: o filtro de `audit`
// protege SEGREDO e não protege PESSOA. Se algum dia ele passar a proteger,
// este teste fica vermelho — e aí a resposta certa é apagar este pacote, não
// remendar o teste.
func TestAuditSozinhoPublicariaAPessoaInteira(t *testing.T) {
	for _, campo := range []string{"name", "email", "phone_e164", "doc_number", "birth_date"} {
		if audit.Sensivel(campo) {
			t.Errorf("audit.Sensivel(%q) = true: `pii` virou redundante", campo)
		}
	}

	semPii := audit.Redigir(audit.Campos{"name": "Ana Silva"})
	if semPii["name"] != "Ana Silva" {
		t.Fatalf("audit.Redigir mascarou `name` = %v", semPii["name"])
	}
}
