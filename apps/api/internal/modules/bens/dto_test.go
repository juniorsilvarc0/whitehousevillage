package bens

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func decodificar[T any](t *testing.T, corpo string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(corpo), &v); err != nil {
		t.Fatalf("decodificando %s: %v", corpo, err)
	}
	return v
}

func TestAmbienteCriarValida(t *testing.T) {
	ok := AmbienteCriar{UnidadeID: uuid.New(), Nome: "Cozinha", Tipo: "cozinha"}
	if e := ok.Validar(); len(e) != 0 {
		t.Fatalf("corpo válido recusado: %v", e)
	}
	ruim := AmbienteCriar{Nome: "   ", Tipo: "Dormitório"}
	e := ruim.Validar()
	for _, campo := range []string{"unit_id", "name", "kind"} {
		if e[campo] == "" {
			t.Errorf("faltou erro em %s: %v", campo, e)
		}
	}
	if longo := (AmbienteCriar{UnidadeID: uuid.New(), Nome: strings.Repeat("á", 121), Tipo: "sala"}).Validar(); longo["name"] == "" {
		t.Error("nome de 121 caracteres deveria ser recusado (conta runa, não byte)")
	}
	if cabe := (AmbienteCriar{UnidadeID: uuid.New(), Nome: strings.Repeat("á", 120), Tipo: "sala"}).Validar(); len(cabe) != 0 {
		t.Errorf("nome de 120 caracteres acentuados deveria caber: %v", cabe)
	}
}

// PATCH: `null` em coluna NOT NULL é 422 legível, não 23502 traduzido.
func TestAmbienteAtualizarRecusaNuloEmColunaObrigatoria(t *testing.T) {
	p := decodificar[AmbienteAtualizar](t, `{"name": null, "active": null}`)
	e := p.Validar()
	if e["name"] == "" || e["active"] == "" {
		t.Fatalf("null em name/active deveria ser recusado: %v", e)
	}
	if e := decodificar[AmbienteAtualizar](t, `{}`).Validar(); len(e) != 0 {
		t.Fatalf("PATCH vazio é no-op, não erro: %v", e)
	}
}

// Custo zero é recusado: `null` é "não cotado", e zero seria um segundo jeito,
// errado, de escrever "não sei".
func TestBemRecusaCustoZero(t *testing.T) {
	zero := int64(0)
	if e := (BemCriar{Nome: "Prato", Categoria: "louca", CustoDeReposicaoCents: &zero}).Validar(); e["replacement_cost_cents"] == "" {
		t.Fatal("custo zero deveria ser 422")
	}
	if e := (BemCriar{Nome: "Prato", Categoria: "louca"}).Validar(); len(e) != 0 {
		t.Fatalf("custo nulo (não cotado) é válido: %v", e)
	}
	p := decodificar[BemAtualizar](t, `{"replacement_cost_cents": null, "description": null}`)
	if e := p.Validar(); len(e) != 0 {
		t.Fatalf("PATCH com null em custo e descrição LIMPA, não recusa: %v", e)
	}
	if e := decodificar[BemAtualizar](t, `{"replacement_cost_cents": 0}`).Validar(); e["replacement_cost_cents"] == "" {
		t.Fatal("PATCH com custo zero deveria ser 422")
	}
}

// `minProperties: 1`: corpo vazio é 422, mas nenhum campo é obrigatório — só
// `note` anota sem tocar na contagem, e `counted_qty: null` DESFAZ a contagem.
func TestContagemDaLinhaDistingueAusenteDeNulo(t *testing.T) {
	if e := decodificar[ContagemDaLinha](t, `{}`).Validar(); e["body"] == "" {
		t.Fatal("corpo vazio deveria ser 422")
	}
	so := decodificar[ContagemDaLinha](t, `{"note": "lascado"}`)
	if e := so.Validar(); len(e) != 0 || so.QtdContada.Set {
		t.Fatalf("só a nota é válido e não mexe na contagem: erros=%v opt=%+v", e, so.QtdContada)
	}
	nulo := decodificar[ContagemDaLinha](t, `{"counted_qty": null}`)
	if e := nulo.Validar(); len(e) != 0 || !nulo.QtdContada.DeveLimpar() {
		t.Fatalf("null desfaz a contagem: erros=%v opt=%+v", e, nulo.QtdContada)
	}
	if e := decodificar[ContagemDaLinha](t, `{"counted_qty": 0}`).Validar(); len(e) != 0 {
		t.Fatalf("zero é \"contei e não achei nenhum\", válido: %v", e)
	}
	if e := decodificar[ContagemDaLinha](t, `{"counted_qty": -1}`).Validar(); e["counted_qty"] == "" {
		t.Fatal("contagem negativa deveria ser 422")
	}
}

// A reserva entra por id OU por código, nunca os dois; no PATCH, mandar as
// duas chaves é 422 mesmo que uma seja `null`.
func TestReservaPorIdOuPorCodigo(t *testing.T) {
	base := AvariaCriar{AmbienteID: uuid.New(), BemID: uuid.New(), Tipo: "quebrado", Qtd: ptr(1)}
	ambos := base
	ambos.ReservaID, ambos.ReservaCodigo = ptr(uuid.New()), ptr("WH-2026-0142")
	if e := ambos.Validar(); e["reservation_code"] == "" {
		t.Fatal("id e código juntos deveria ser 422")
	}
	branco := base
	branco.ReservaCodigo = ptr("   ")
	if e := branco.Validar(); e["reservation_code"] == "" {
		t.Fatal("código em branco deveria ser 422")
	}
	so := base
	so.ReservaCodigo = ptr("wh-2026-0142")
	if e := so.Validar(); len(e) != 0 {
		t.Fatalf("só o código é válido: %v", e)
	}
	if NormalizarCodigoDaReserva(" wh-2026-0142 ") != "WH-2026-0142" {
		t.Fatal("o código digitado em minúscula é a mesma reserva")
	}

	if e := decodificar[AvariaAtualizar](t, `{"reservation_id": null, "reservation_code": "WH-1"}`).Validar(); e["reservation_code"] == "" {
		t.Fatal("PATCH com as duas chaves deveria ser 422")
	}
	if e := decodificar[AvariaAtualizar](t, `{"reservation_code": null}`).Validar(); len(e) != 0 {
		t.Fatalf("reservation_code null desvincula, é válido: %v", e)
	}
	sub := AvariaSubstituir{Tipo: "outro", Qtd: ptr(2), ReservaID: ptr(uuid.New()), ReservaCodigo: ptr("WH-1")}
	if e := sub.Validar(); e["reservation_code"] == "" {
		t.Fatal("PUT com as duas portas deveria ser 422")
	}
}

func TestGaleriaRecusaRepetidaComIndice(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	e := GaleriaDoBem{MidiaIDs: []uuid.UUID{a, b, a}}.Validar()
	if !strings.Contains(e["media_ids[2]"], "índice 0") {
		t.Fatalf("a repetida deveria apontar o índice anterior: %v", e)
	}
	if e := (GaleriaDoBem{MidiaIDs: []uuid.UUID{}}).Validar(); len(e) != 0 {
		t.Fatalf("lista vazia é aceita (bem sem foto é fila de trabalho): %v", e)
	}
	if e := (GaleriaDoBem{}).Validar(); e["media_ids"] == "" {
		t.Fatal("media_ids ausente deveria ser 422")
	}
	muitas := make([]uuid.UUID, maxFotosPorBem+1)
	for i := range muitas {
		muitas[i] = uuid.New()
	}
	if e := (GaleriaDoBem{MidiaIDs: muitas}).Validar(); e["media_ids"] == "" {
		t.Fatal("mais de 24 fotos deveria ser 422")
	}
}

func TestAvariaQuantidadeMaiorQueZero(t *testing.T) {
	base := AvariaCriar{AmbienteID: uuid.New(), BemID: uuid.New(), Tipo: "quebrado"}
	for _, q := range []int{0, -2} {
		c := base
		c.Qtd = &q
		if e := c.Validar(); e["qty"] == "" {
			t.Errorf("qty %d deveria ser 422", q)
		}
	}
	um := 1
	c := base
	c.Qtd = &um
	if e := c.Validar(); len(e) != 0 {
		t.Fatalf("qty 1 é válido: %v", e)
	}
	if e := decodificar[AvariaAtualizar](t, `{"resolution": null}`).Validar(); len(e) != 0 {
		t.Fatalf("resolution null REABRE, é válido: %v", e)
	}
	if e := decodificar[AvariaAtualizar](t, `{"resolution": "resolvido"}`).Validar(); e["resolution"] == "" {
		t.Fatal("desfecho fora do vocabulário deveria ser 422")
	}
}

func TestTextoDoOptTresEstados(t *testing.T) {
	atual := ptr("antiga")
	if v := textoDoOpt(decodificar[ContagemDaLinha](t, `{"counted_qty":1}`).Nota, atual); v != atual {
		t.Fatal("ausente deveria manter")
	}
	if v := textoDoOpt(decodificar[ContagemDaLinha](t, `{"counted_qty":1,"note":null}`).Nota, atual); v != nil {
		t.Fatal("null deveria limpar")
	}
	if v := textoDoOpt(decodificar[ContagemDaLinha](t, `{"counted_qty":1,"note":"  "}`).Nota, atual); v != nil {
		t.Fatal("texto em branco é ausência de valor (NULL)")
	}
	if v := textoDoOpt(decodificar[ContagemDaLinha](t, `{"counted_qty":1,"note":" nova "}`).Nota, atual); v == nil || *v != "nova" {
		t.Fatalf("valor deveria gravar aparado, veio %v", v)
	}
}
