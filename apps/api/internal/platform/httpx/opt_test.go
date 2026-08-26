package httpx

import (
	"encoding/json"
	"testing"
)

type corpoDeTeste struct {
	Nome     Opt[string] `json:"name"`
	Telefone Opt[string] `json:"phone"`
	Ativo    Opt[bool]   `json:"active"`
}

// Os três casos que o PATCH precisa distinguir e que um ponteiro não separa.
func TestOptDistingueAusenteNuloEValor(t *testing.T) {
	casos := []struct {
		nome        string
		json        string
		set         bool
		valid       bool
		valor       string
		deveLimpar  bool
		descricaoDe string
	}{
		{
			nome: "campo ausente", json: `{}`,
			set: false, valid: false, valor: "", deveLimpar: false,
			descricaoDe: "não mexer no campo",
		},
		{
			nome: "campo nulo", json: `{"phone": null}`,
			set: true, valid: false, valor: "", deveLimpar: true,
			descricaoDe: "gravar NULL",
		},
		{
			nome: "campo com valor", json: `{"phone": "+5585999990000"}`,
			set: true, valid: true, valor: "+5585999990000", deveLimpar: false,
			descricaoDe: "gravar o valor",
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			var corpo corpoDeTeste
			if err := json.Unmarshal([]byte(c.json), &corpo); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}

			if corpo.Telefone.Set != c.set {
				t.Fatalf("Set = %v, esperado %v (%s)", corpo.Telefone.Set, c.set, c.descricaoDe)
			}
			if corpo.Telefone.Valid != c.valid {
				t.Fatalf("Valid = %v, esperado %v (%s)", corpo.Telefone.Valid, c.valid, c.descricaoDe)
			}
			if corpo.Telefone.Value != c.valor {
				t.Fatalf("Value = %q, esperado %q", corpo.Telefone.Value, c.valor)
			}
			if corpo.Telefone.DeveLimpar() != c.deveLimpar {
				t.Fatalf("DeveLimpar = %v, esperado %v", corpo.Telefone.DeveLimpar(), c.deveLimpar)
			}

			valor, definido := corpo.Telefone.Definido()
			if definido != (c.set && c.valid) {
				t.Fatalf("Definido = %v, esperado %v", definido, c.set && c.valid)
			}
			if definido && valor != c.valor {
				t.Fatalf("Definido devolveu %q, esperado %q", valor, c.valor)
			}
		})
	}
}

// Um corpo real de PATCH mistura os três estados; nenhum campo pode contaminar
// o outro.
func TestOptIsolaOsCamposEntreSi(t *testing.T) {
	var corpo corpoDeTeste
	if err := json.Unmarshal([]byte(`{"name":"Ana","phone":null}`), &corpo); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if v, ok := corpo.Nome.Definido(); !ok || v != "Ana" {
		t.Fatalf("name deveria estar definido como Ana, veio (%q, %v)", v, ok)
	}
	if !corpo.Telefone.DeveLimpar() {
		t.Fatal("phone com null deveria pedir limpeza")
	}
	if corpo.Ativo.Set {
		t.Fatal("active ausente não pode aparecer como definido")
	}
}

func TestOptComTipoNaoTextual(t *testing.T) {
	var corpo corpoDeTeste
	if err := json.Unmarshal([]byte(`{"active": false}`), &corpo); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	valor, definido := corpo.Ativo.Definido()
	if !definido {
		t.Fatal("active: false é valor definido, não ausência — é a armadilha do zero value")
	}
	if valor {
		t.Fatal("active deveria ser false")
	}
}

func TestOptRecusaTipoErrado(t *testing.T) {
	var corpo corpoDeTeste
	if err := json.Unmarshal([]byte(`{"phone": 42}`), &corpo); err == nil {
		t.Fatal("número num campo de texto deveria falhar no decode")
	}
}

func TestOptAuxiliares(t *testing.T) {
	if v, ok := De("x").Definido(); !ok || v != "x" {
		t.Fatal("De deveria produzir um Opt definido")
	}
	if !Nulo[string]().DeveLimpar() {
		t.Fatal("Nulo deveria produzir o estado de limpeza")
	}
	var ausente Opt[string]
	if ausente.Set || ausente.DeveLimpar() {
		t.Fatal("o zero value do Opt é o campo ausente")
	}
	if ausente.Ou("padrão") != "padrão" {
		t.Fatal("Ou deveria devolver o padrão quando o campo está ausente")
	}
	if Nulo[string]().Ou("padrão") != "padrão" {
		t.Fatal("Ou deveria devolver o padrão quando o campo é nulo")
	}
}

func TestOptSerializaComoValorOuNulo(t *testing.T) {
	corpo := corpoDeTeste{Nome: De("Ana"), Telefone: Nulo[string]()}

	bruto, err := json.Marshal(corpo)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	const esperado = `{"name":"Ana","phone":null,"active":null}`
	if string(bruto) != esperado {
		t.Fatalf("JSON = %s, esperado %s", bruto, esperado)
	}
}
